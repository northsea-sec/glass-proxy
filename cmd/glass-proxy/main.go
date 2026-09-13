// Glass Proxy — single-user multi-lane proxy with an Anthropic /v1/messages
// reverse-proxy core plus explicit Codex, Gemini, and OpenAI-compatible lane
// handlers. Stripped from aletheia for nataraja's Session Glass architecture.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"encoding/json"

	claudelane "proxy.local/app/internal/claude"
	"proxy.local/app/internal/codex"
	"proxy.local/app/internal/config"
	"proxy.local/app/internal/debug"
	"proxy.local/app/internal/forcemode"
	geminilane "proxy.local/app/internal/gemini"
	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/guard"
	"proxy.local/app/internal/proxy"
	"proxy.local/app/internal/runtimepaths"
	"proxy.local/app/internal/serializer"
	"proxy.local/app/internal/spoofer"
	"proxy.local/app/internal/sysprompt"
)

func writeJSONResponse(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONErrorResponse(w http.ResponseWriter, status int, message string) {
	writeJSONResponse(w, status, map[string]string{"error": message})
}

func buildProxyModePayload(startupMode, defaultUpstreamURL, defaultProxyDomain, defaultUpstreamDomain, anthropicMessagesUpstreamURL, anthropicMessagesProxyDomain, anthropicMessagesUpstreamDomain, codexUpstream, geminiUpstream, openAICompatibleUpstream string, claudePrefixWarmerEnabled, claudeRollingSummarizerEnabled bool) map[string]interface{} {
	return map[string]interface{}{
		"mode":                               startupMode,
		"default_upstream":                   defaultUpstreamURL,
		"default_proxy_domain":               defaultProxyDomain,
		"default_upstream_domain":            defaultUpstreamDomain,
		"anthropic_messages_upstream":        anthropicMessagesUpstreamURL,
		"anthropic_messages_proxy_domain":    anthropicMessagesProxyDomain,
		"anthropic_messages_upstream_domain": anthropicMessagesUpstreamDomain,
		"codex_upstream":                     codexUpstream,
		"gemini_upstream":                    geminiUpstream,
		"openai_compatible_upstream":         openAICompatibleUpstream,
		"claude_prefix_warmer_enabled":       claudePrefixWarmerEnabled,
		"claude_rolling_summarizer_enabled":  claudeRollingSummarizerEnabled,
	}
}

type proxyStartupTargets struct {
	defaultUpstreamURL              string
	defaultProxyDomain              string
	defaultUpstreamDomain           string
	anthropicMessagesUpstreamURL    string
	anthropicMessagesProxyDomain    string
	anthropicMessagesUpstreamDomain string
}

func resolveProxyStartupTargets(startupMode, configuredUpstreamURL, configuredClaudeUpstreamURL, codexUpstream string) proxyStartupTargets {
	defaultUpstreamURL := strings.TrimSpace(configuredUpstreamURL)
	if startupMode == "codex" && (defaultUpstreamURL == "" || defaultUpstreamURL == "https://api.anthropic.com") {
		defaultUpstreamURL = codexUpstream
	}
	if defaultUpstreamURL == "" {
		defaultUpstreamURL = "https://api.anthropic.com"
	}

	anthropicMessagesUpstreamURL := strings.TrimSpace(configuredClaudeUpstreamURL)
	if anthropicMessagesUpstreamURL == "" {
		if startupMode == "codex" {
			anthropicMessagesUpstreamURL = "https://api.anthropic.com"
		} else {
			anthropicMessagesUpstreamURL = strings.TrimSpace(configuredUpstreamURL)
		}
	}
	if anthropicMessagesUpstreamURL == "" {
		anthropicMessagesUpstreamURL = "https://api.anthropic.com"
	}

	targets := proxyStartupTargets{
		defaultUpstreamURL:              defaultUpstreamURL,
		defaultProxyDomain:              "api.anthropic.com",
		defaultUpstreamDomain:           "api.anthropic.com",
		anthropicMessagesUpstreamURL:    anthropicMessagesUpstreamURL,
		anthropicMessagesProxyDomain:    "api.anthropic.com",
		anthropicMessagesUpstreamDomain: "api.anthropic.com",
	}
	if startupMode == "codex" {
		targets.defaultProxyDomain = "api.openai.com"
		targets.defaultUpstreamDomain = "api.openai.com"
	}
	return targets
}

func main() {
	var (
		listenAddr     = flag.String("listen", ":18888", "Listen address")
		unixSocket     = flag.String("unix-socket", "", "Optional AF_UNIX socket path to listen on in parallel with TCP")
		upstreamURL    = flag.String("upstream", "https://api.anthropic.com", "Default reverse-proxy upstream API URL")
		claudeUpstream = flag.String("claude-upstream", "", "Claude lane / Anthropic /v1/messages upstream API URL")
		configPath     = flag.String("config", "", "Path to glass_config.json")
		apiKey         = flag.String("api-key", "", "Default reverse-proxy API key (or ANTHROPIC_API_KEY env)")
		allowDirect    = flag.Bool("allow-direct", false, "Allow direct upstream without egress/sidecar")
		mode           = flag.String("mode", "default", "Startup mode: default or codex")
	)
	flag.Parse()

	startupMode := strings.ToLower(strings.TrimSpace(*mode))
	switch startupMode {
	case "", "default":
		startupMode = "default"
	case "codex":
		startupMode = "codex"
	default:
		log.Fatalf("Unsupported mode %q (expected default or codex)", *mode)
	}
	if startupMode == "codex" && strings.TrimSpace(os.Getenv(runtimepaths.RuntimeLaneEnv)) == "" {
		_ = os.Setenv(runtimepaths.RuntimeLaneEnv, "codex")
	}
	paths := runtimepaths.Current()

	// Resolve config path
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = os.Getenv("GLASS_CONFIG")
	}
	if cfgPath == "" {
		cfgPath = paths.ConfigPath
	}

	// Load config with hot-reload
	cfgLoader := config.NewLoader(cfgPath)
	cfgLoader.StartPolling(2 * time.Second)

	// System prompt pipeline
	cfg := cfgLoader.Get()
	if err := cfg.ValidateRuntimeSupport(); err != nil {
		log.Fatalf("[CONFIG] %v", err)
	}
	sysPipe := sysprompt.NewPipeline(false)
	sysPipe.SyncConfig(cfg.SpoofSystemPrompt, cfg.SyspromptPatchFile, cfg.SyspromptReplaceFile)

	// Embedded security guard sidecar.
	var guardServer *http.Server
	var guardSvc *guard.Server
	if cfg.SecurityGuardEnabled {
		guardCfg := guard.ServerConfig{
			BindAddr:                   cfg.SecurityGuardBind,
			URLhausAuthKey:             os.Getenv("URLHAUS_AUTH_KEY"),
			PhishTankAppKey:            os.Getenv("PHISHTANK_APP_KEY"),
			PackageMinAgeHours:         cfg.SecurityGuardPackageMinAgeHours,
			RequireVerifiedAttestation: cfg.SecurityGuardRequireVerifiedAttestation,
			CacheTTL:                   guard.DefaultCacheTTL,
			OSVEnabled:                 cfg.SecurityGuardOSVEnabled,
			GuardDogEnabled:            cfg.SecurityGuardGuardDogEnabled,
			DnstwistEnabled:            cfg.SecurityGuardDnstwistEnabled,
			JSXRayEnabled:              cfg.SecurityGuardJSXRayEnabled,
			LLMGuardEnabled:            cfg.SecurityGuardLLMGuardEnabled,
			LLMGuardBind:               cfg.SecurityGuardLLMGuardBind,
			ProtectedDomains:           cfg.SecurityGuardProtectedDomains,
			VenvPath:                   cfg.SecurityGuardVenvPath,
		}
		guardSvc = guard.NewServer(guardCfg)
		guardServer = guardSvc.NewHTTPServer()
		go func() {
			log.Printf("[GUARD] Starting embedded guard on %s", guardServer.Addr)
			if err := guardServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[GUARD] Server failed: %v", err)
			}
		}()
	}

	if cfg.SyspromptPatchFile != "" {
		log.Printf("[GLASS] Sysprompt patch file: %s", cfg.SyspromptPatchFile)
	}
	if cfg.SyspromptReplaceFile != "" {
		log.Printf("[GLASS] Sysprompt replace file: %s", cfg.SyspromptReplaceFile)
	}

	// Usage spoofer (writes to the active runtime root)
	usageSpoofer := spoofer.NewBridge(paths.UsageBridgePath)
	// Rate-limit header spoofer + usage cap
	rateLimitSpoofer := spoofer.New(true, 0)
	log.Printf("[GLASS] Usage spoofer + rate-limit spoofer initialized")

	// Glass config — read from GLASS_CONFIG if present, else defaults
	glassCfg := glass.DefaultGlassConfig()
	if data, err := os.ReadFile(cfgPath); err == nil {
		var raw map[string]json.RawMessage
		keepaliveSet := false
		if err := json.Unmarshal(data, &raw); err == nil {
			_, keepaliveSet = raw["keepalive_interval_sec"]
		}
		json.Unmarshal(data, &glassCfg) // overlay JSON onto defaults
		log.Printf("[GLASS] Loaded glass config: evict=%d/%d, sysrem=%v, thinking=%v",
			glassCfg.EvictTriggerTokens, glassCfg.EvictTargetTokens,
			glassCfg.StripSysReminders, glassCfg.StripThinking)
		if glassCfg.OpenAIUpstream != "" {
			oaiT := glassCfg.OpenAIThresholds()
			log.Printf("[GLASS] OpenAI lane: upstream=%s, evict=%d/%d",
				glassCfg.OpenAIUpstream, oaiT.Trigger, oaiT.Target)
		}
		if !keepaliveSet && glassCfg.KeepaliveIntervalSec == 0 {
			glassCfg.KeepaliveIntervalSec = 240
			log.Printf("[GLASS] keepalive_interval_sec missing — defaulting to %d", glassCfg.KeepaliveIntervalSec)
		}
	}
	// Restore shadow_dir default if config left it empty (json.Unmarshal overwrites defaults).
	if glassCfg.ShadowDir == "" {
		glassCfg.ShadowDir = paths.GlassShadowDir
		log.Printf("[GLASS] shadow_dir was empty — defaulting to %s", glassCfg.ShadowDir)
	}
	if cfgLoader.Get().SpoofUsageCap > 0 && glassCfg.SpoofUsageCap == 0 {
		glassCfg.SpoofUsageCap = cfgLoader.Get().SpoofUsageCap
	}

	// Debug database (unified observability)
	debugDB, err := debug.OpenDB(paths.DebugDBPath)
	if err != nil {
		log.Fatalf("Failed to open debug DB: %v", err)
	}
	defer debugDB.Close()
	debugRecorder := debug.NewRecorder(debugDB)
	debugAPI := debug.NewAPIHandler(debugRecorder, debugDB)

	// Load serializer config from same glass config file (ser_* keys).
	serCfg := serializer.DefaultConfig()
	if data, err := os.ReadFile(cfgPath); err == nil {
		json.Unmarshal(data, &serCfg) // overlay ser_* keys onto defaults
	}
	log.Printf("[GLASS] Serializer: enabled=%v batch=%d idle=%.1fs", serCfg.Enabled, serCfg.BatchSize, serCfg.IdleTimeoutSec)

	// Create Codex lane handler (for OpenAI Codex CLI via Responses API).
	codexUpstream := glassCfg.CodexUpstream
	if codexUpstream == "" {
		codexUpstream = "https://api.openai.com"
	}
	configuredClaudeUpstream := strings.TrimSpace(*claudeUpstream)
	if configuredClaudeUpstream == "" {
		configuredClaudeUpstream = glassCfg.ClaudeUpstream
	}
	startupTargets := resolveProxyStartupTargets(startupMode, *upstreamURL, configuredClaudeUpstream, codexUpstream)
	effectiveUpstreamURL := startupTargets.defaultUpstreamURL
	anthropicMessagesUpstreamURL := startupTargets.anthropicMessagesUpstreamURL
	codexCfg := codex.DefaultConfig()
	codexCfg.Upstream = codexUpstream
	if strings.TrimSpace(glassCfg.CodexShadowDir) != "" {
		codexCfg.ShadowDir = glassCfg.CodexShadowDir
	}
	if glassCfg.CodexEvictTriggerTokens != 0 {
		codexCfg.EvictTriggerTokens = glassCfg.CodexEvictTriggerTokens
	}
	if glassCfg.CodexEvictTargetTokens != 0 {
		codexCfg.EvictTargetTokens = glassCfg.CodexEvictTargetTokens
	}
	codexCfg.SummarizeEnabled = glassCfg.CodexSummarizeEnabled
	if glassCfg.CodexSummarizeInterval != 0 {
		codexCfg.SummarizeInterval = glassCfg.CodexSummarizeInterval
	}
	if strings.TrimSpace(glassCfg.CodexSummarizeModel) != "" {
		codexCfg.SummarizeModel = glassCfg.CodexSummarizeModel
	}
	codexCfg.SyspromptFunc = sysPipe.Process
	codexCfg.DebugRecorder = debugRecorder
	codexLane := codex.NewHandler(codexCfg)
	defer codexLane.Close()
	var codexHandler http.Handler = codexLane
	log.Printf("[CODEX] Lane enabled: upstream=%s shadow=%s evict=%d/%d summarize=%v",
		codexCfg.Upstream, codexCfg.ShadowDir, codexCfg.EvictTriggerTokens, codexCfg.EvictTargetTokens, codexCfg.SummarizeEnabled)

	// Create Gemini lane handler (for Google Gemini CLI via generateContent API).
	geminiUpstream := glassCfg.GeminiUpstream
	if geminiUpstream == "" {
		geminiUpstream = "https://generativelanguage.googleapis.com"
	}
	geminiCfg := geminilane.Config{
		Upstream:             geminiUpstream,
		ShadowDir:            glassCfg.ShadowDir + "-gemini",
		EvictTriggerTokens:   glassCfg.GeminiEvictTriggerTokens,
		EvictTargetTokens:    glassCfg.GeminiEvictTargetTokens,
		SummarizeEnabled:     glassCfg.GeminiSummarizeEnabled,
		SummarizeInterval:    glassCfg.GeminiSummarizeInterval,
		SummarizeModel:       glassCfg.GeminiSummarizeModel,
		ExplicitCacheEnabled: glassCfg.GeminiExplicitCacheEnabled,
		CacheTTLSec:          glassCfg.GeminiCacheTTLSec,
		SyspromptFunc:        sysPipe.Process,
		DebugRecorder:        debugRecorder,
	}
	if geminiCfg.EvictTriggerTokens == 0 {
		geminiCfg.EvictTriggerTokens = 700000
	}
	if geminiCfg.EvictTargetTokens == 0 {
		geminiCfg.EvictTargetTokens = 500000
	}
	geminiLane := geminilane.NewHandler(geminiCfg)
	var geminiHandler http.Handler = geminiLane
	log.Printf("[GEMINI] Lane enabled: upstream=%s evict=%d/%d summarize=%v cache=%v",
		geminiCfg.Upstream, geminiCfg.EvictTriggerTokens, geminiCfg.EvictTargetTokens,
		geminiCfg.SummarizeEnabled, geminiCfg.ExplicitCacheEnabled)

	defaultProxyDomain := startupTargets.defaultProxyDomain
	defaultUpstreamDomain := startupTargets.defaultUpstreamDomain
	anthropicMessagesProxyDomain := startupTargets.anthropicMessagesProxyDomain
	anthropicMessagesUpstreamDomain := startupTargets.anthropicMessagesUpstreamDomain

	// Create proxy (single-user mode — no tenants, no Tor, no WG, no dashboard)
	p, err := proxy.New(proxy.Options{
		DefaultUpstreamURL:              effectiveUpstreamURL,
		DefaultProxyDomain:              defaultProxyDomain,
		DefaultUpstreamDomain:           defaultUpstreamDomain,
		AnthropicMessagesUpstreamURL:    anthropicMessagesUpstreamURL,
		AnthropicMessagesProxyDomain:    anthropicMessagesProxyDomain,
		AnthropicMessagesUpstreamDomain: anthropicMessagesUpstreamDomain,
		ConfigLoader:                    cfgLoader,
		ForceMode:                       forcemode.LoadFromEnv(),
		SyspromptPipeline:               sysPipe,
		Spoofer:                         usageSpoofer,
		UsageSpoofer:                    rateLimitSpoofer,
		DefaultAPIKey:                   *apiKey,
		AnthropicMessagesAPIKey:         *apiKey,
		AllowDirectUpstream:             *allowDirect,
		GlassConfig:                     &glassCfg,
		SerializerConfig:                serCfg,
		OpenAIUpstream:                  glassCfg.OpenAIUpstream,
		CodexHandler:                    codexHandler,
		GeminiHandler:                   geminiHandler,
		DebugRecorder:                   debugRecorder,
	})
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}

	claudeServices := claudelane.NewServices(claudelane.ServicesConfig{
		Enabled:                 startupMode != "codex",
		Upstream:                anthropicMessagesUpstreamURL,
		ShadowDir:               glassCfg.ShadowDir,
		PrefixWarmerIntervalSec: glassCfg.KeepaliveIntervalSec,
		RollingSummarizer: glass.SummarizerConfig{
			Enabled:       glassCfg.RollingSummarizeEnabled,
			IntervalToken: glassCfg.RollingSummarizeInterval,
			Model:         glassCfg.RollingSummarizeModel,
			GateEnabled:   glassCfg.RecoveryGateEnabled,
		},
	})
	claudeServices.Attach(p.GlassEngine())
	p.SetAnthropicReplayRecorder(claudeServices)
	defer claudeServices.Close()

	claudePrefixWarmerEnabled := claudeServices.PrefixWarmerEnabled()
	claudeRollingSummarizerEnabled := claudeServices.RollingSummarizerEnabled()
	if startupMode == "codex" {
		if !claudePrefixWarmerEnabled {
			log.Printf("[CLAUDE] Prefix warmer disabled in codex startup mode")
		}
		if !claudeRollingSummarizerEnabled {
			log.Printf("[CLAUDE] Rolling summarizer disabled in codex startup mode")
		}
	}
	if claudeRollingSummarizerEnabled {
		log.Printf("[CLAUDE] Rolling summarizer enabled: interval=%d model=%s gate=%v (auth: captured from first request)",
			glassCfg.RollingSummarizeInterval, glassCfg.RollingSummarizeModel, glassCfg.RecoveryGateEnabled)
	}

	// HTTP server
	mux := http.NewServeMux()
	openAICompatibleLane := p.OpenAICompatibleLaneService()
	laneAdapters := buildLaneDebugAdapters(claudeServices, openAICompatibleLane, p, codexLane, geminiLane)
	debugAPI.RegisterRoutes(mux) // /debug/* endpoints (must register before catch-all)
	mux.HandleFunc("/debug/serializer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(p.SerializerHealthJSON())
	})
	mux.HandleFunc("/debug/proxy-mode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSONResponse(w, http.StatusOK, buildProxyModePayload(
			startupMode,
			effectiveUpstreamURL,
			defaultProxyDomain,
			defaultUpstreamDomain,
			anthropicMessagesUpstreamURL,
			anthropicMessagesProxyDomain,
			anthropicMessagesUpstreamDomain,
			codexCfg.Upstream,
			geminiCfg.Upstream,
			openAICompatibleLane.Upstream(),
			claudePrefixWarmerEnabled,
			claudeRollingSummarizerEnabled,
		))
	})
	mux.HandleFunc("/debug/claude-auth-status", func(w http.ResponseWriter, r *http.Request) {
		handleDebugLaneAuthStatus(w, r, laneAdapters, "claude")
	})
	mux.HandleFunc("/debug/claude-models", func(w http.ResponseWriter, r *http.Request) {
		handleDebugLaneModels(w, r, laneAdapters, "claude")
	})
	mux.HandleFunc("/debug/lane-models", func(w http.ResponseWriter, r *http.Request) {
		handleDebugLaneModels(w, r, laneAdapters, "")
	})
	mux.HandleFunc("/debug/claude-forward", func(w http.ResponseWriter, r *http.Request) {
		handleDebugLaneForward(w, r, laneAdapters, "claude")
	})
	mux.HandleFunc("/debug/lane-auth-status", func(w http.ResponseWriter, r *http.Request) {
		handleDebugLaneAuthStatus(w, r, laneAdapters, "")
	})
	mux.HandleFunc("/debug/lane-sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		adapter, ok := lookupLaneDebugAdapter(laneAdapters, r.URL.Query().Get("lane"))
		if !ok {
			writeJSONErrorResponse(w, http.StatusBadRequest, laneQueryParamError)
			return
		}
		writeJSONResponse(w, http.StatusOK, map[string]interface{}{"lane": adapter.lane, "sessions": adapter.sessions()})
	})
	mux.HandleFunc("/debug/lane-session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		lane := r.URL.Query().Get("lane")
		sessionID := r.URL.Query().Get("id")
		if sessionID == "" {
			writeJSONErrorResponse(w, http.StatusBadRequest, "id query param is required")
			return
		}
		adapter, ok := lookupLaneDebugAdapter(laneAdapters, lane)
		if !ok {
			writeJSONErrorResponse(w, http.StatusBadRequest, laneQueryParamError)
			return
		}
		if snapshot, ok := adapter.session(sessionID); ok {
			writeJSONResponse(w, http.StatusOK, snapshot)
			return
		}
		writeJSONErrorResponse(w, http.StatusNotFound, adapter.lane+" lane session not found")
	})
	mux.HandleFunc("/debug/lane-session-replay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		lane := r.URL.Query().Get("lane")
		sessionID := r.URL.Query().Get("id")
		if sessionID == "" {
			writeJSONErrorResponse(w, http.StatusBadRequest, "id query param is required")
			return
		}
		adapter, ok := lookupLaneDebugAdapter(laneAdapters, lane)
		if !ok {
			writeJSONErrorResponse(w, http.StatusBadRequest, laneQueryParamError)
			return
		}
		if replay, ok := adapter.replay(sessionID); ok {
			writeJSONResponse(w, http.StatusOK, replay)
			return
		}
		writeJSONErrorResponse(w, http.StatusNotFound, adapter.lane+" lane replay not found")
	})
	mux.HandleFunc("/debug/lane-forward", func(w http.ResponseWriter, r *http.Request) {
		handleDebugLaneForward(w, r, laneAdapters, "")
	})
	log.Printf("[DEBUG-API] Mounted proxy debug endpoints: /debug/serializer, /debug/claude-auth-status, /debug/claude-models, /debug/claude-forward, /debug/lane-models, /debug/lane-auth-status, /debug/lane-sessions, /debug/lane-session, /debug/lane-session-replay, /debug/lane-forward")
	mux.Handle("/", p)

	server := &http.Server{
		Addr:         *listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute, // long for SSE
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[GLASS] Startup mode: %s", startupMode)
	log.Printf("[GLASS] Starting proxy on %s", *listenAddr)
	log.Printf("[GLASS] Upstream: %s", effectiveUpstreamURL)
	log.Printf("[GLASS] Config: %s", cfgPath)

	// Optional parallel AF_UNIX listener. safe-shell uses this as the private
	// sidecar socket behind its filtered broker.
	var unixListener net.Listener
	if *unixSocket != "" {
		socketPath := *unixSocket
		if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
			log.Fatalf("unix-socket: failed to create parent dir: %v", err)
		}
		// Remove stale socket from previous run, if any.
		if _, err := os.Stat(socketPath); err == nil {
			if err := os.Remove(socketPath); err != nil {
				log.Fatalf("unix-socket: failed to remove stale socket %s: %v", socketPath, err)
			}
		}
		ln, err := net.Listen("unix", socketPath)
		if err != nil {
			log.Fatalf("unix-socket: listen failed on %s: %v", socketPath, err)
		}
		if err := os.Chmod(socketPath, 0o600); err != nil {
			log.Fatalf("unix-socket: chmod 0600 failed: %v", err)
		}
		unixListener = ln
		log.Printf("[GLASS] Parallel unix-socket listener: %s (mode 0600)", socketPath)
	}

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	if unixListener != nil {
		go func() {
			if err := server.Serve(unixListener); err != nil && err != http.ErrServerClosed {
				log.Fatalf("unix-socket serve failed: %v", err)
			}
		}()
	}

	sig := <-stop
	log.Printf("[GLASS] Received %v — shutting down", sig)

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer drainCancel()
	p.Shutdown(drainCtx)

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := server.Shutdown(shutCtx); err != nil {
		log.Printf("[GLASS] Shutdown error: %v", err)
	}
	if guardServer != nil {
		if err := guardServer.Shutdown(shutCtx); err != nil {
			log.Printf("[GUARD] Shutdown error: %v", err)
		}
	}
	if guardSvc != nil {
		guardSvc.StopSubprocesses()
	}

	log.Printf("[GLASS] Shutdown complete")
}

const (
	supportedDebugLaneList = "claude, anthropic, openai, codex, gemini, ollama"
	laneQueryParamError    = "lane query param must be one of: " + supportedDebugLaneList
	laneBodyError          = "lane must be one of: " + supportedDebugLaneList
)

type laneDebugAdapter struct {
	lane             string
	authStatus       func() interface{}
	legacyAuthStatus func() (int, interface{})
	sessions         func() interface{}
	session          func(string) (interface{}, bool)
	replay           func(string) (interface{}, bool)
	models           func() (int, interface{})
	legacyModels     func() (int, interface{})
	forward          func(remoteAddr, path string, body json.RawMessage) (*laneForwardResponse, int, error)
}

type laneForwardResponse struct {
	status int
	header http.Header
	body   []byte
}

func handleDebugLaneAuthStatus(w http.ResponseWriter, r *http.Request, adapters map[string]laneDebugAdapter, fixedLane string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	lane := fixedLane
	if lane == "" {
		lane = r.URL.Query().Get("lane")
	}
	adapter, ok := lookupLaneDebugAdapter(adapters, lane)
	if !ok {
		writeJSONErrorResponse(w, http.StatusBadRequest, laneQueryParamError)
		return
	}
	if fixedLane != "" && adapter.legacyAuthStatus != nil {
		status, payload := adapter.legacyAuthStatus()
		writeJSONResponse(w, status, payload)
		return
	}
	writeJSONResponse(w, http.StatusOK, adapter.authStatus())
}

func handleDebugLaneModels(w http.ResponseWriter, r *http.Request, adapters map[string]laneDebugAdapter, fixedLane string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	lane := fixedLane
	if lane == "" {
		lane = r.URL.Query().Get("lane")
	}
	adapter, ok := lookupLaneDebugAdapter(adapters, lane)
	if !ok {
		writeJSONErrorResponse(w, http.StatusBadRequest, laneQueryParamError)
		return
	}
	if fixedLane != "" && adapter.legacyModels != nil {
		status, payload := adapter.legacyModels()
		writeJSONResponse(w, status, payload)
		return
	}
	status, payload := adapter.models()
	writeJSONResponse(w, status, payload)
}

func handleDebugLaneForward(w http.ResponseWriter, r *http.Request, adapters map[string]laneDebugAdapter, fixedLane string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	type laneForwardAliasRequest struct {
		Path string          `json:"path"`
		Body json.RawMessage `json:"body"`
	}
	type laneForwardRequest struct {
		Lane string          `json:"lane"`
		Path string          `json:"path"`
		Body json.RawMessage `json:"body"`
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	lane := fixedLane
	path := ""
	body := json.RawMessage(nil)
	if fixedLane != "" {
		var req laneForwardAliasRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			writeJSONErrorResponse(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		path = strings.TrimSpace(req.Path)
		body = req.Body
	} else {
		var req laneForwardRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			writeJSONErrorResponse(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		lane = strings.TrimSpace(req.Lane)
		path = strings.TrimSpace(req.Path)
		body = req.Body
	}

	if lane == "" || path == "" {
		writeJSONErrorResponse(w, http.StatusBadRequest, "lane and path are required")
		return
	}
	if !strings.HasPrefix(path, "/") {
		writeJSONErrorResponse(w, http.StatusBadRequest, "path must start with /")
		return
	}
	adapter, ok := lookupLaneDebugAdapter(adapters, lane)
	if !ok {
		writeJSONErrorResponse(w, http.StatusBadRequest, laneBodyError)
		return
	}
	response, status, err := adapter.forward(r.RemoteAddr, path, body)
	if err != nil {
		writeJSONErrorResponse(w, status, err.Error())
		return
	}
	writeLaneForwardResponse(w, response)
}

func canonicalLaneName(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "anthropic", "claude":
		return "claude", true
	case "openai":
		return "openai", true
	case "codex":
		return "codex", true
	case "gemini":
		return "gemini", true
	case "ollama":
		return "ollama", true
	default:
		return "", false
	}
}

func lookupLaneDebugAdapter(adapters map[string]laneDebugAdapter, lane string) (laneDebugAdapter, bool) {
	canonical, ok := canonicalLaneName(lane)
	if !ok {
		return laneDebugAdapter{}, false
	}
	adapter, ok := adapters[canonical]
	return adapter, ok
}

func buildLaneDebugAdapters(claudeServices *claudelane.Services, openAICompatibleLane *proxy.OpenAICompatibleLaneService, p *proxy.Proxy, codexLane *codex.Handler, geminiLane *geminilane.Handler) map[string]laneDebugAdapter {
	return map[string]laneDebugAdapter{
		"claude": {
			lane:       "claude",
			authStatus: func() interface{} { return claudeServices.AuthStatus() },
			legacyAuthStatus: func() (int, interface{}) {
				if !claudeServices.RollingSummarizerEnabled() {
					return http.StatusServiceUnavailable, map[string]string{"error": "rolling summarizer unavailable"}
				}
				return http.StatusOK, claudeServices.AuthStatus()
			},
			sessions: func() interface{} { return claudeServices.LaneSessions() },
			session: func(convID string) (interface{}, bool) {
				snapshot, ok := claudeServices.LaneSession(convID)
				if !ok {
					return nil, false
				}
				return snapshot, true
			},
			replay: func(convID string) (interface{}, bool) {
				replay, ok := claudeServices.LaneSessionReplay(convID)
				if !ok {
					return nil, false
				}
				return replay, true
			},
			models: func() (int, interface{}) {
				return http.StatusOK, map[string]interface{}{
					"lane":           "claude",
					"data":           observedClaudeLaneModels(claudeServices.LaneSessions()),
					"source":         "observed_session",
					"provider_error": "captured provider model listing unavailable",
				}
			},
			legacyModels: func() (int, interface{}) {
				if !claudeServices.RollingSummarizerEnabled() {
					return http.StatusServiceUnavailable, map[string]string{"error": "rolling summarizer unavailable"}
				}
				body, status, err := claudeServices.ModelsWithCapturedAuth()
				if err != nil {
					return http.StatusServiceUnavailable, map[string]string{"error": err.Error()}
				}
				var payload interface{}
				if err := json.Unmarshal(body, &payload); err != nil {
					return http.StatusBadGateway, map[string]string{"error": "invalid provider response"}
				}
				return status, payload
			},
			forward: func(_ string, path string, body json.RawMessage) (*laneForwardResponse, int, error) {
				if !strings.HasPrefix(path, "/v1/") {
					return nil, http.StatusBadRequest, errors.New("claude lane forward path must target /v1/*")
				}
				responseBody, status, err := claudeServices.ForwardWithCapturedAuth(http.MethodPost, path, body)
				if err != nil {
					return nil, http.StatusServiceUnavailable, err
				}
				return &laneForwardResponse{
					status: status,
					header: http.Header{"Content-Type": []string{"application/json"}},
					body:   responseBody,
				}, 0, nil
			},
		},
		"openai": buildOpenAICompatibleLaneAdapter("openai", openAICompatibleLane, p),
		"ollama": buildOpenAICompatibleLaneAdapter("ollama", openAICompatibleLane, p),
		"codex": {
			lane:       "codex",
			authStatus: func() interface{} { return codexLane.LaneAuthStatus() },
			sessions:   func() interface{} { return codexLane.LaneSessions() },
			session: func(convID string) (interface{}, bool) {
				snapshot, ok := codexLane.LaneSession(convID)
				if !ok {
					return nil, false
				}
				return snapshot, true
			},
			replay: func(convID string) (interface{}, bool) {
				replay, ok := codexLane.LaneSessionReplay(convID)
				if !ok {
					return nil, false
				}
				return replay, true
			},
			models: func() (int, interface{}) {
				return http.StatusOK, map[string]interface{}{
					"lane":   "codex",
					"data":   observedCodexLaneModels(codexLane.LaneSessions()),
					"source": "observed_session",
				}
			},
			forward: func(remoteAddr, path string, body json.RawMessage) (*laneForwardResponse, int, error) {
				if !strings.HasSuffix(path, "/responses") {
					return nil, http.StatusBadRequest, errors.New("codex lane forward path must target /responses")
				}
				return forwardLaneRequest(p, remoteAddr, path, body, codexLane.ApplyCapturedAuth)
			},
		},
		"gemini": {
			lane:       "gemini",
			authStatus: func() interface{} { return geminiLane.LaneAuthStatus() },
			sessions:   func() interface{} { return geminiLane.LaneSessions() },
			session: func(convID string) (interface{}, bool) {
				snapshot, ok := geminiLane.LaneSession(convID)
				if !ok {
					return nil, false
				}
				return snapshot, true
			},
			replay: func(convID string) (interface{}, bool) {
				replay, ok := geminiLane.LaneSessionReplay(convID)
				if !ok {
					return nil, false
				}
				return replay, true
			},
			models: func() (int, interface{}) {
				return http.StatusOK, map[string]interface{}{
					"lane":   "gemini",
					"data":   observedGeminiLaneModels(geminiLane.LaneSessions()),
					"source": "observed_session",
				}
			},
			forward: func(remoteAddr, path string, body json.RawMessage) (*laneForwardResponse, int, error) {
				if !strings.Contains(path, ":generateContent") && !strings.Contains(path, ":streamGenerateContent") {
					return nil, http.StatusBadRequest, errors.New("gemini lane forward path must target generateContent or streamGenerateContent")
				}
				return forwardLaneRequest(p, remoteAddr, path, body, geminiLane.ApplyCapturedAuth)
			},
		},
	}
}

func buildOpenAICompatibleLaneAdapter(lane string, laneService *proxy.OpenAICompatibleLaneService, p *proxy.Proxy) laneDebugAdapter {
	return laneDebugAdapter{
		lane: lane,
		authStatus: func() interface{} {
			return laneService.AuthStatus(lane)
		},
		sessions: func() interface{} { return laneService.Sessions() },
		session: func(convID string) (interface{}, bool) {
			snapshot, ok := laneService.Session(convID)
			if !ok {
				return nil, false
			}
			return snapshot, true
		},
		replay: func(convID string) (interface{}, bool) {
			replay, ok := laneService.SessionReplay(lane, convID)
			if !ok {
				return nil, false
			}
			return replay, true
		},
		models: func() (int, interface{}) {
			return http.StatusOK, map[string]interface{}{
				"lane":   lane,
				"data":   observedOpenAILaneModels(laneService.Sessions()),
				"source": "observed_session",
			}
		},
		forward: func(remoteAddr, path string, body json.RawMessage) (*laneForwardResponse, int, error) {
			if path != "/v1/chat/completions" {
				return nil, http.StatusBadRequest, errors.New(lane + " lane forward path must target /v1/chat/completions")
			}
			return forwardLaneRequest(p, remoteAddr, path, body, func(req *http.Request) error {
				laneService.ApplyCapturedAuth(req)
				return nil
			})
		},
	}
}

func forwardLaneRequest(handler http.Handler, remoteAddr, path string, body json.RawMessage, applyAuth func(*http.Request) error) (*laneForwardResponse, int, error) {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	if applyAuth != nil {
		if err := applyAuth(req); err != nil {
			return nil, http.StatusServiceUnavailable, err
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	result := recorder.Result()
	defer result.Body.Close()

	responseBody, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return &laneForwardResponse{
		status: result.StatusCode,
		header: result.Header.Clone(),
		body:   responseBody,
	}, 0, nil
}

func writeLaneForwardResponse(w http.ResponseWriter, response *laneForwardResponse) {
	for header, values := range response.header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}
	w.WriteHeader(response.status)
	_, _ = w.Write(response.body)
}

func appendObservedModel(rows []map[string]interface{}, seen map[string]struct{}, model string) []map[string]interface{} {
	model = strings.TrimSpace(model)
	if model == "" {
		return rows
	}
	if _, ok := seen[model]; ok {
		return rows
	}
	seen[model] = struct{}{}
	return append(rows, map[string]interface{}{
		"id":           model,
		"display_name": model,
		"source":       "observed_session",
	})
}

func observedClaudeLaneModels(sessions []claudelane.LaneSessionSnapshot) []map[string]interface{} {
	seen := make(map[string]struct{})
	rows := make([]map[string]interface{}, 0, len(sessions))
	for _, session := range sessions {
		rows = appendObservedModel(rows, seen, session.Model)
	}
	return rows
}

func observedOpenAILaneModels(sessions []proxy.OpenAILaneSessionSnapshot) []map[string]interface{} {
	seen := make(map[string]struct{})
	rows := make([]map[string]interface{}, 0, len(sessions))
	for _, session := range sessions {
		rows = appendObservedModel(rows, seen, session.Model)
	}
	return rows
}

func observedCodexLaneModels(sessions []codex.LaneSessionSnapshot) []map[string]interface{} {
	seen := make(map[string]struct{})
	rows := make([]map[string]interface{}, 0, len(sessions))
	for _, session := range sessions {
		rows = appendObservedModel(rows, seen, session.Model)
	}
	return rows
}

func observedGeminiLaneModels(sessions []geminilane.LaneSessionSnapshot) []map[string]interface{} {
	seen := make(map[string]struct{})
	rows := make([]map[string]interface{}, 0, len(sessions))
	for _, session := range sessions {
		rows = appendObservedModel(rows, seen, session.Model)
	}
	return rows
}
