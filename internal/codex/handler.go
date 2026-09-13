// handler.go — HTTP handler for the Codex CLI lane.
// Intercepts Codex /responses traffic, strips server-side compaction,
// applies Glass-style context management, and relays Responses API traffic.
package codex

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"proxy.local/app/internal/replay"
	"proxy.local/app/internal/requestbody"
	"proxy.local/app/internal/runtimepaths"
)

// Config configures the Codex lane handler.
type Config struct {
	Upstream           string // e.g. "https://api.openai.com"
	ChatGPTUpstream    string // e.g. "https://chatgpt.com/backend-api/codex"
	ShadowDir          string // e.g. "~/.codex/glass-proxy/shadow/"
	EvictTriggerTokens int    // default 200000
	EvictTargetTokens  int    // default 150000
	KeepRecentItems    int    // default 20
	MaxFuncOutputChars int    // default 700
	MaxAssistantChars  int    // default 500
	CompBatchSize      int    // default 40
	FlushTokens        int    // default 200000 (saturation flush threshold)
	SummarizeEnabled   bool
	SummarizeInterval  int    // default 50000
	SummarizeModel     string // default "gpt-5.4"
	// SyspromptFunc, if set, processes system prompt fragments through the
	// glass sysprompt pipeline. Signature matches sysprompt.Pipeline.Process.
	SyspromptFunc func([]interface{}) ([]interface{}, int)
	DebugRecorder telemetryRecorder
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	return Config{
		Upstream:           defaultOpenAIUpstream,
		ChatGPTUpstream:    defaultChatGPTCodexUpstream,
		ShadowDir:          DefaultShadowDir(),
		EvictTriggerTokens: 200000,
		EvictTargetTokens:  150000,
		KeepRecentItems:    20,
		MaxFuncOutputChars: 700,
		MaxAssistantChars:  500,
		CompBatchSize:      40,
		FlushTokens:        200000,
		SummarizeEnabled:   false,
		SummarizeInterval:  50000,
		SummarizeModel:     "gpt-5.4",
	}
}

// Handler handles Codex CLI /responses requests.
type Handler struct {
	upstream         string
	chatgptUpstream  string
	chatgptTransport http.RoundTripper
	sessions         *sessionStore
	summarizer       *rollingSummarizer
	fixtureCapture   *replay.Capture
	debugRecorder    telemetryRecorder
	cfg              Config
	authMu           sync.Mutex
	authHeader       string
	orgHeader        string
	projHeader       string
}

type LaneAuthStatus struct {
	Lane                     string `json:"lane"`
	Upstream                 string `json:"upstream"`
	UpstreamConfigured       bool   `json:"upstream_configured"`
	ShadowDir                string `json:"shadow_dir"`
	RequestAuthPassthrough   bool   `json:"request_auth_passthrough"`
	CapturedAuthAvailable    bool   `json:"captured_auth_available"`
	CapturedAuthType         string `json:"captured_auth_type"`
	SummarizerEnabled        bool   `json:"summarizer_enabled"`
	SessionOwner             string `json:"session_owner"`
	SessionPersistence       string `json:"session_persistence"`
	SupportsStatelessProbe   bool   `json:"supports_stateless_probe"`
	SupportsLiveSessionProbe bool   `json:"supports_live_session_probe"`
	SupportsResumableSession bool   `json:"supports_resumable_session"`
	SessionCount             int    `json:"session_count"`
}

// NewHandler creates a handler with sessionStore and rollingSummarizer.
func NewHandler(cfg Config) *Handler {
	cfg = withDefaults(cfg)
	sumCfg := summarizerConfig{
		Enabled:         cfg.SummarizeEnabled,
		IntervalToken:   cfg.SummarizeInterval,
		Model:           cfg.SummarizeModel,
		Upstream:        cfg.Upstream,
		ChatGPTUpstream: cfg.ChatGPTUpstream,
		ShadowDir:       cfg.ShadowDir,
	}
	chatgptTransport := newChatGPTUpstreamTransport()
	return &Handler{
		upstream:         cfg.Upstream,
		chatgptUpstream:  cfg.ChatGPTUpstream,
		chatgptTransport: chatgptTransport,
		sessions:         newSessionStore(cfg.ShadowDir),
		summarizer:       newRollingSummarizer(sumCfg, nil),
		fixtureCapture:   replay.NewCaptureFromEnv(),
		debugRecorder:    cfg.DebugRecorder,
		cfg:              cfg,
	}
}

func (h *Handler) Close() {
	if h == nil || h.fixtureCapture == nil {
		return
	}
	h.fixtureCapture.Close()
}

func withDefaults(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.Upstream == "" {
		cfg.Upstream = defaults.Upstream
	}
	if cfg.ChatGPTUpstream == "" {
		cfg.ChatGPTUpstream = defaults.ChatGPTUpstream
	}
	if cfg.ShadowDir == "" {
		cfg.ShadowDir = defaults.ShadowDir
	}
	if cfg.EvictTriggerTokens == 0 {
		cfg.EvictTriggerTokens = defaults.EvictTriggerTokens
	}
	if cfg.EvictTargetTokens == 0 {
		cfg.EvictTargetTokens = defaults.EvictTargetTokens
	}
	if cfg.KeepRecentItems == 0 {
		cfg.KeepRecentItems = defaults.KeepRecentItems
	}
	if cfg.MaxFuncOutputChars == 0 {
		cfg.MaxFuncOutputChars = defaults.MaxFuncOutputChars
	}
	if cfg.MaxAssistantChars == 0 {
		cfg.MaxAssistantChars = defaults.MaxAssistantChars
	}
	if cfg.CompBatchSize == 0 {
		cfg.CompBatchSize = defaults.CompBatchSize
	}
	if cfg.FlushTokens == 0 {
		cfg.FlushTokens = defaults.FlushTokens
	}
	if cfg.SummarizeInterval == 0 {
		cfg.SummarizeInterval = defaults.SummarizeInterval
	}
	if cfg.SummarizeModel == "" {
		cfg.SummarizeModel = defaults.SummarizeModel
	}
	return cfg
}

func (h *Handler) captureRequestAuth(authHeader, orgHeader, projHeader string) {
	if h == nil {
		return
	}
	h.authMu.Lock()
	defer h.authMu.Unlock()
	if strings.TrimSpace(authHeader) != "" {
		h.authHeader = authHeader
	}
	if strings.TrimSpace(orgHeader) != "" {
		h.orgHeader = orgHeader
	}
	if strings.TrimSpace(projHeader) != "" {
		h.projHeader = projHeader
	}
}

func (h *Handler) capturedAuthStatus() (bool, string) {
	if h == nil {
		return false, "unavailable"
	}
	h.authMu.Lock()
	defer h.authMu.Unlock()
	authHeader := strings.TrimSpace(h.authHeader)
	if authHeader == "" {
		return false, "unavailable"
	}
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return true, "bearer"
	}
	return true, "authorization"
}

func (h *Handler) ApplyCapturedAuth(req *http.Request) error {
	if h == nil {
		return fmt.Errorf("codex lane unavailable")
	}
	h.authMu.Lock()
	authHeader := h.authHeader
	orgHeader := h.orgHeader
	projHeader := h.projHeader
	h.authMu.Unlock()
	if strings.TrimSpace(authHeader) == "" {
		return fmt.Errorf("captured codex auth unavailable")
	}
	req.Header.Set("Authorization", authHeader)
	if strings.TrimSpace(orgHeader) != "" {
		req.Header.Set("OpenAI-Organization", orgHeader)
	}
	if strings.TrimSpace(projHeader) != "" {
		req.Header.Set("OpenAI-Project", projHeader)
	}
	return nil
}

func (h *Handler) LaneAuthStatus() LaneAuthStatus {
	sessionCount := 0
	if h != nil && h.sessions != nil {
		sessionCount = len(h.sessions.snapshots())
	}
	captured, capturedType := h.capturedAuthStatus()
	upstream := ""
	shadowDir := ""
	summarizerEnabled := false
	if h != nil {
		upstream = h.upstream
		shadowDir = h.cfg.ShadowDir
		summarizerEnabled = h.summarizer != nil
	}
	return LaneAuthStatus{
		Lane:                     "codex",
		Upstream:                 upstream,
		UpstreamConfigured:       strings.TrimSpace(upstream) != "",
		ShadowDir:                shadowDir,
		RequestAuthPassthrough:   true,
		CapturedAuthAvailable:    captured,
		CapturedAuthType:         capturedType,
		SummarizerEnabled:        summarizerEnabled,
		SessionOwner:             "glass_codex_lane",
		SessionPersistence:       "live_memory_with_disk_snapshot",
		SupportsStatelessProbe:   true,
		SupportsLiveSessionProbe: true,
		SupportsResumableSession: false,
		SessionCount:             sessionCount,
	}
}

func (h *Handler) LaneSessions() []LaneSessionSnapshot {
	if h == nil || h.sessions == nil {
		return nil
	}
	return h.sessions.snapshots()
}

func (h *Handler) LaneSession(convID string) (LaneSessionSnapshot, bool) {
	if h == nil || h.sessions == nil {
		return LaneSessionSnapshot{}, false
	}
	return h.sessions.snapshot(convID)
}

func (h *Handler) LaneSessionReplay(convID string) (LaneSessionReplay, bool) {
	if h == nil || h.sessions == nil {
		return LaneSessionReplay{}, false
	}
	replay, ok := h.sessions.replay(convID)
	if !ok {
		return LaneSessionReplay{}, false
	}
	replay.CapturedAuthAvailable, replay.CapturedAuthType = h.capturedAuthStatus()
	return replay, true
}

// maxBodySize is the hard limit on incoming request bodies (10 MB).
const maxBodySize = 10 * 1024 * 1024

// ServeHTTP implements the full Codex lane pipeline:
// parse → strip → ingest → compress → evict → summarize → rebuild → forward → relay.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	openAIOrganization := r.Header.Get("OpenAI-Organization")
	openAIProject := r.Header.Get("OpenAI-Project")
	h.captureRequestAuth(authHeader, openAIOrganization, openAIProject)
	if h.summarizer != nil && authHeader != "" {
		h.summarizer.captureAuth(authHeader, openAIOrganization, openAIProject)
	}

	if r.Method == http.MethodGet && isWebSocketUpgrade(r) {
		log.Printf("[CODEX] websocket route auth=%s upstream=%s", authRoutingMode(authHeader), resolveCodexUpstreamBase(h.upstream, h.chatgptUpstream, authHeader))
		h.proxyWebSocket(w, r, authHeader)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/responses/compact") {
		h.handleCompactRequest(w, r, authHeader, openAIOrganization, openAIProject)
		return
	}
	if r.Method != http.MethodPost {
		proxyErrorJSON(w, "invalid_request_error", "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Read body.
	body, err := requestbody.ReadAndNormalize(r, maxBodySize)
	if err != nil {
		if errors.Is(err, requestbody.ErrTooLarge) {
			proxyErrorJSON(w, "invalid_request_error", "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		proxyErrorJSON(w, "invalid_request_error", "request body unreadable", http.StatusBadRequest)
		return
	}

	// 2. Parse JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		proxyErrorJSON(w, "invalid_request_error", "invalid JSON body", http.StatusBadRequest)
		return
	}

	// 3. Compute session ID from instructions + model.
	convID := computeFingerprint(parsed)
	model, _ := parsed["model"].(string)
	log.Printf("[CODEX] %s conv=%s from %s", model, convID, r.RemoteAddr)

	// 4. Get or create session.
	sess := h.sessions.get(convID)
	sess.mu.Lock()
	sess.model = model
	sess.updatedAt = time.Now()
	sess.mu.Unlock()
	if template := deepCopyItem(parsed); template != nil {
		delete(template, "input")
		sess.captureReplayTemplate(r.URL.RequestURI(), template)
	}

	// Capture the normalized inbound request body before Codex-lane mutations.
	preGlassBody, err := json.Marshal(parsed)
	if err != nil {
		proxyErrorJSON(w, "server_error", "failed to serialize pre-glass request", http.StatusInternalServerError)
		return
	}

	// 5b. Apply sysprompt pipeline to instructions.
	if h.cfg.SyspromptFunc != nil {
		if inst, ok := parsed["instructions"].(string); ok && inst != "" {
			processed, mods := h.cfg.SyspromptFunc([]interface{}{
				map[string]interface{}{"type": "text", "text": inst},
			})
			if mods > 0 {
				var parts []string
				for _, frag := range processed {
					if block, ok := frag.(map[string]interface{}); ok {
						if text, ok := block["text"].(string); ok && text != "" {
							parts = append(parts, text)
						}
					}
				}
				if len(parts) > 0 {
					parsed["instructions"] = strings.Join(parts, "\n")
					log.Printf("[CODEX] conv=%s sysprompt: %d modifications applied", convID, mods)
				}
			}
		}
	}

	// 5c. Apply per-session Codex editor overlays before capture/forwarding.
	if applied, err := applyCodexSyspromptPatch(convID, parsed); err != nil {
		log.Printf("[CODEX] conv=%s sysprompt patch load error: %v", convID, err)
	} else if applied {
		log.Printf("[CODEX] conv=%s applied sysprompt editor patch", convID)
	}

	// 5d. Capture and persist system prompt.
	captureSystemPrompt(h.cfg.ShadowDir, convID, parsed)

	// 6. Strip fields that enable server-side compaction.
	delete(parsed, "context_management")
	delete(parsed, "previous_response_id")
	parsed["store"] = false

	// Extract input items and strip compaction entries.
	inputItems, hasInput := extractAndStripInput(parsed)

	// 7–11: Context management pipeline (only if we have items to work with).
	if hasInput && len(inputItems) > 0 {
		// 7. Ingest new items into the session cache.
		// Session methods acquire their own locks — no outer lock needed.
		newCount := sess.ingest(inputItems)

		// 8. Progressive compression (watermark-based).
		sess.mu.Lock()
		itemCount := len(sess.items)
		currentWM := sess.compWatermark
		sess.mu.Unlock()

		newWatermark := (itemCount / h.cfg.CompBatchSize) * h.cfg.CompBatchSize
		safeLimit := itemCount - h.cfg.KeepRecentItems
		if safeLimit < 0 {
			safeLimit = 0
		}
		if newWatermark > currentWM && newWatermark-currentWM >= h.cfg.CompBatchSize && safeLimit > currentWM {
			end := newWatermark
			if end > safeLimit {
				end = safeLimit
			}
			saved, compressed := sess.compressOld(end, h.cfg.MaxFuncOutputChars, h.cfg.MaxAssistantChars)
			if saved > 0 {
				log.Printf("[CODEX] conv=%s compressed %d items (~%d tokens saved, watermark→%d)",
					convID, compressed, saved, end)
			}
			sess.mu.Lock()
			sess.compWatermark = end
			sess.mu.Unlock()
		}

		// 9. Saturation flush: high loss ratio or token overflow with summaries available.
		totalTok := sess.totalTokens()
		lossRatio := sess.informationLossRatio()
		if (lossRatio >= 0.85 || totalTok > h.cfg.FlushTokens) && h.summarizer != nil {
			st := h.summarizer.getState(convID)
			if st != nil && len(st.Chunks) > 0 {
				summary := h.summarizer.stitchSummary(convID)
				if summary != "" {
					fullSummary := "## Session Recovery (auto-flush)\n\n" +
						"Context was compressed and flushed. " +
						"CONTINUE the work described below — do NOT ask what to do next.\n\n" +
						summary
					removed := sess.flushCompressed(fullSummary)
					if removed > 0 {
						log.Printf("[CODEX] conv=%s saturation flush: ratio=%.2f tokens=%d, removed %d compressed items",
							convID, lossRatio, totalTok, removed)
						h.summarizer.resetForConv(convID)
						sess.mu.Lock()
						sess.compWatermark = 0
						sess.mu.Unlock()
					}
				}
			}
		}

		// 10. Eviction threshold check.
		totalTok = sess.totalTokens()
		if totalTok > h.cfg.EvictTriggerTokens {
			evicted := sess.evict(h.cfg.EvictTargetTokens, h.cfg.KeepRecentItems)
			if len(evicted) > 0 {
				sess.mu.Lock()
				batchNum := sess.batchCount
				sess.mu.Unlock()

				log.Printf("[CODEX] conv=%s evicted %d items (%d tokens > %d trigger, batch=%d)",
					convID, len(evicted), totalTok, h.cfg.EvictTriggerTokens, batchNum)

				if err := writeShadow(h.cfg.ShadowDir, convID, evicted, batchNum); err != nil {
					log.Printf("[CODEX] shadow write error: %v", err)
				}

				// Inject summary from rolling summarizer at eviction.
				if h.cfg.SummarizeEnabled && h.summarizer != nil {
					st := h.summarizer.getState(convID)
					if st != nil && len(st.Chunks) > 0 {
						summary := h.summarizer.stitchSummary(convID)
						if summary != "" {
							fullSummary := "## Session Recovery (eviction batch " + fmt.Sprintf("%d", batchNum) + ")\n\n" +
								"Prior context was evicted. " +
								"CONTINUE the work described below — do NOT ask what to do next.\n\n" +
								summary
							sess.injectDeveloperMessage(fullSummary)
							h.summarizer.resetForConv(convID)
						}
					}
				}

				if err := sess.save(h.cfg.ShadowDir); err != nil {
					log.Printf("[CODEX] cache save error: %v", err)
				}
			}
		}

		// 11. Rolling summarization (background).
		if h.cfg.SummarizeEnabled && h.summarizer != nil && newCount > 0 {
			go h.summarizer.maybeChunk(convID, sess)
		}
	}

	// 12. Rebuild input from session cache.
	if hasInput {
		rebuiltInput := deepCopyCodexInputItems(sess.buildItems())
		parsed["input"] = rebuiltInput
		if applied, err := applyCodexContextPatches(convID, rebuiltInput); err != nil {
			log.Printf("[CODEX] conv=%s context patch load error: %v", convID, err)
		} else if applied > 0 {
			log.Printf("[CODEX] conv=%s applied %d context editor patches", convID, applied)
		}
	}

	// 13. Log session stats.
	sess.mu.Lock()
	itemsLen := len(sess.items)
	evictedTotal := sess.evictedCount
	sess.mu.Unlock()
	log.Printf("[CODEX] conv=%s items=%d tokens=%d evicted=%d loss=%.2f",
		convID, itemsLen, sess.totalTokens(), evictedTotal, sess.informationLossRatio())

	// 14. Re-serialize body.
	outBody, err := json.Marshal(parsed)
	if err != nil {
		proxyErrorJSON(w, "server_error", "failed to serialize request", http.StatusInternalServerError)
		return
	}

	if h.fixtureCapture != nil {
		stream, _ := parsed["stream"].(bool)
		authPresent := strings.TrimSpace(authHeader) != ""
		captureID := fmt.Sprintf("codex_%s_%d", convID, time.Now().UnixNano())
		h.fixtureCapture.Record(replay.Fixture{
			Version:    replay.FixtureVersion,
			CaptureID:  captureID,
			RequestURI: r.URL.RequestURI(),
			Header: replay.HeaderSnapshot{
				Stream:    stream,
				HasAPIKey: authPresent,
			},
			Meta: replay.MetaSnapshot{
				RequestSessionKey: convID,
				SessionKey:        convID,
				RequestKey:        convID,
			},
			BodyRaw:      append([]byte(nil), body...),
			BodyPreGlass: append([]byte(nil), preGlassBody...),
			BodyFinal:    append([]byte(nil), outBody...),
		})
	}

	// 15. Forward to upstream.
	upstreamURL := resolveResponsesUpstream(h.upstream, h.chatgptUpstream, authHeader)
	log.Printf("[CODEX] responses route method=%s auth=%s upstream=%s", r.Method, authRoutingMode(authHeader), upstreamURL)
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(outBody))
	if err != nil {
		proxyErrorJSON(w, "server_error", "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		upReq.Header.Set("Authorization", authHeader)
	}
	// Forward provider-specific headers.
	if openAIOrganization != "" {
		upReq.Header.Set("OpenAI-Organization", openAIOrganization)
	}
	if openAIProject != "" {
		upReq.Header.Set("OpenAI-Project", openAIProject)
	}

	client := &http.Client{
		Timeout:   10 * time.Minute,
		Transport: h.transportForAuth(authHeader),
	}
	resp, err := client.Do(upReq)
	if err != nil {
		log.Printf("[CODEX] upstream error: %v", err)
		proxyErrorJSON(w, "server_error", fmt.Sprintf("upstream connection failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("[CODEX] upstream responded %d %s", resp.StatusCode, resp.Status)

	// Determine if streaming (default true for Responses API).
	streaming := true
	if s, ok := parsed["stream"].(bool); ok {
		streaming = s
	}
	telemetry := buildCodexRequestTelemetry(convID, parsed)

	if streaming && resp.StatusCode == http.StatusOK {
		h.relaySSE(w, resp, sess, convID, telemetry)
	} else {
		h.relayNonStreaming(w, resp, sess, telemetry)
	}
}

func (h *Handler) handleCompactRequest(w http.ResponseWriter, r *http.Request, authHeader, openAIOrganization, openAIProject string) {
	body, err := requestbody.ReadAndNormalize(r, maxBodySize)
	if err != nil {
		if errors.Is(err, requestbody.ErrTooLarge) {
			proxyErrorJSON(w, "invalid_request_error", "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		proxyErrorJSON(w, "invalid_request_error", "request body unreadable", http.StatusBadRequest)
		return
	}

	// Phase 0 capture: preserve native compact payloads exactly as the client sent them.
	if h.fixtureCapture != nil {
		captureID := fmt.Sprintf("codex_compact_%d", time.Now().UnixNano())
		h.fixtureCapture.Record(replay.Fixture{
			Version:    replay.FixtureVersion,
			CaptureID:  captureID,
			RequestURI: r.URL.RequestURI(),
			Header: replay.HeaderSnapshot{
				HasAPIKey: strings.TrimSpace(authHeader) != "",
			},
			BodyRaw:      append([]byte(nil), body...),
			BodyPreGlass: append([]byte(nil), body...),
			BodyFinal:    append([]byte(nil), body...),
		})
	}

	targetURL := strings.TrimRight(resolveResponsesUpstream(h.upstream, h.chatgptUpstream, authHeader), "/") + "/compact"
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		proxyErrorJSON(w, "server_error", "failed to build compact upstream request", http.StatusInternalServerError)
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		upReq.Header.Set("Authorization", authHeader)
	}
	if openAIOrganization != "" {
		upReq.Header.Set("OpenAI-Organization", openAIOrganization)
	}
	if openAIProject != "" {
		upReq.Header.Set("OpenAI-Project", openAIProject)
	}

	client := &http.Client{
		Timeout:   10 * time.Minute,
		Transport: h.transportForAuth(authHeader),
	}
	resp, err := client.Do(upReq)
	if err != nil {
		log.Printf("[CODEX] compact upstream error: %v", err)
		proxyErrorJSON(w, "server_error", fmt.Sprintf("compact upstream connection failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		proxyErrorJSON(w, "server_error", "failed to read compact upstream response", http.StatusBadGateway)
		return
	}

	if captureDir := strings.TrimSpace(os.Getenv(replay.CaptureEnvVar)); captureDir != "" {
		responseJSON, responseBase64, responsePreview := replay.EncodeHTTPBodyPreview(respBody, 4096)
		requestHeader := map[string]string{
			"auth_routing": authRoutingMode(authHeader),
		}
		responseHeader := map[string]string{}
		if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
			responseHeader["content_type"] = ct
		}
		if ray := strings.TrimSpace(resp.Header.Get("cf-ray")); ray != "" {
			responseHeader["cf_ray"] = ray
		}
		_ = replay.WriteHTTPExchange(captureDir, replay.HTTPExchange{
			Version:         replay.HTTPExchangeVersion,
			CaptureID:       fmt.Sprintf("codex_compact_response_%d", time.Now().UnixNano()),
			RequestURI:      r.URL.RequestURI(),
			UpstreamURL:     targetURL,
			RequestMethod:   http.MethodPost,
			RequestHeader:   requestHeader,
			ResponseStatus:  resp.StatusCode,
			ResponseHeader:  responseHeader,
			RequestBody:     append([]byte(nil), body...),
			ResponseBody:    responseJSON,
			ResponseBase64:  responseBase64,
			ResponsePreview: responsePreview,
		})
	}

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (h *Handler) transportForAuth(authHeader string) http.RoundTripper {
	if h != nil && shouldUseChatGPTUpstream(authHeader) && h.chatgptTransport != nil {
		return h.chatgptTransport
	}
	return http.DefaultTransport
}

func (h *Handler) proxyWebSocket(w http.ResponseWriter, r *http.Request, authHeader string) {
	if captureDir := strings.TrimSpace(os.Getenv(replay.CaptureEnvVar)); captureDir != "" || h.debugRecorder != nil {
		h.proxyCapturedWebSocket(w, r, authHeader, captureDir)
		return
	}

	h.proxyWebSocketReverseProxy(w, r, authHeader)
}

func (h *Handler) proxyWebSocketReverseProxy(w http.ResponseWriter, r *http.Request, authHeader string) {
	upstreamBase := resolveCodexUpstreamBase(h.upstream, h.chatgptUpstream, authHeader)
	target, err := url.Parse(upstreamBase)
	if err != nil {
		log.Printf("[CODEX] invalid websocket upstream %q: %v", upstreamBase, err)
		proxyErrorJSON(w, "server_error", "invalid websocket upstream", http.StatusInternalServerError)
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = joinCodexPath(target.Path, req.URL.Path)
			if req.URL.RawPath != "" {
				req.URL.RawPath = joinCodexPath(target.Path, req.URL.RawPath)
			}
		},
		Transport:     h.transportForAuth(authHeader),
		FlushInterval: -1,
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			log.Printf("[CODEX] websocket proxy error: %v", err)
			proxyErrorJSON(rw, "server_error", fmt.Sprintf("websocket proxy failed: %v", err), http.StatusBadGateway)
		},
	}
	capture := &captureResponseWriter{ResponseWriter: w}
	proxy.ServeHTTP(capture, r)
	if capture.status == 0 {
		log.Printf("[CODEX] websocket proxy completed without explicit status")
		return
	}
	if capture.status == http.StatusSwitchingProtocols {
		log.Printf("[CODEX] websocket upstream responded %d Switching Protocols", capture.status)
		return
	}
	snippet := strings.TrimSpace(capture.body.String())
	if len(snippet) > 512 {
		snippet = snippet[:512]
	}
	log.Printf("[CODEX] websocket upstream responded %d bytes=%d body=%q", capture.status, capture.bytes, snippet)
}

func (h *Handler) proxyCapturedWebSocket(w http.ResponseWriter, r *http.Request, authHeader, captureDir string) {
	upstreamBase := resolveCodexUpstreamBase(h.upstream, h.chatgptUpstream, authHeader)
	target, err := url.Parse(upstreamBase)
	if err != nil {
		log.Printf("[CODEX] invalid websocket upstream %q: %v", upstreamBase, err)
		proxyErrorJSON(w, "server_error", "invalid websocket upstream", http.StatusInternalServerError)
		return
	}

	upstreamURL := resolveCodexWebSocketURL(target, r)
	captureID := fmt.Sprintf("codex_ws_%d", time.Now().UnixNano())
	var trace *webSocketTraceRecorder
	if captureDir != "" {
		trace = newWebSocketTraceRecorder(captureDir, captureID, r.URL.RequestURI(), upstreamURL, r.Header, authHeader)
		defer trace.Write()
	}
	telemetry := newWebSocketTelemetryCollector(h.debugRecorder, h.sessions, h.cfg.ShadowDir, r.URL.RequestURI())

	subprotocols := websocket.Subprotocols(r)
	dialer := h.websocketDialerForAuth(authHeader, subprotocols)
	upHeaders := websocketUpstreamHeaders(r.Header, authHeader)

	upConn, resp, err := dialer.Dial(upstreamURL, upHeaders)
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			log.Printf("[CODEX] websocket dial failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		log.Printf("[CODEX] websocket dial error: %v", err)
		proxyErrorJSON(w, "server_error", fmt.Sprintf("websocket dial failed: %v", err), http.StatusBadGateway)
		return
	}
	defer upConn.Close()
	if trace != nil {
		trace.SetSelectedProto(upConn.Subprotocol())
	}

	upgrader := websocket.Upgrader{
		CheckOrigin:       func(_ *http.Request) bool { return true },
		EnableCompression: true,
	}
	if proto := upConn.Subprotocol(); proto != "" {
		upgrader.Subprotocols = []string{proto}
	}

	downConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[CODEX] websocket client upgrade failed: %v", err)
		return
	}
	defer downConn.Close()

	errCh := make(chan error, 2)
	go proxyWebSocketFrames(upConn, downConn, "client_to_upstream", trace, telemetry, errCh)
	go proxyWebSocketFrames(downConn, upConn, "upstream_to_client", trace, telemetry, errCh)

	firstErr := <-errCh
	_ = downConn.Close()
	_ = upConn.Close()
	secondErr := <-errCh

	if firstErr != nil && !isExpectedWebSocketClose(firstErr) {
		log.Printf("[CODEX] captured websocket proxy ended with error: %v", firstErr)
	}
	if secondErr != nil && !isExpectedWebSocketClose(secondErr) {
		log.Printf("[CODEX] captured websocket peer shutdown error: %v", secondErr)
	}
}

func (h *Handler) websocketDialerForAuth(authHeader string, subprotocols []string) *websocket.Dialer {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 45 * time.Second
	dialer.EnableCompression = true
	dialer.Subprotocols = append([]string(nil), subprotocols...)

	if transport, ok := h.transportForAuth(authHeader).(*http.Transport); ok {
		if transport.Proxy != nil {
			dialer.Proxy = transport.Proxy
		}
		if transport.TLSClientConfig != nil {
			dialer.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		if transport.DialContext != nil {
			dialer.NetDialContext = transport.DialContext
		}
		if transport.TLSHandshakeTimeout > 0 {
			dialer.HandshakeTimeout = transport.TLSHandshakeTimeout
		}
	}

	return &dialer
}

func websocketUpstreamHeaders(src http.Header, authHeader string) http.Header {
	dst := make(http.Header)
	for key, values := range src {
		switch http.CanonicalHeaderKey(key) {
		case "Connection", "Upgrade", "Sec-Websocket-Key", "Sec-Websocket-Version", "Sec-Websocket-Extensions", "Sec-Websocket-Accept", "Sec-Websocket-Protocol":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	if strings.TrimSpace(authHeader) != "" {
		dst.Set("Authorization", authHeader)
	}
	return dst
}

func resolveCodexWebSocketURL(base *url.URL, req *http.Request) string {
	u := *base
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = joinCodexPath(base.Path, req.URL.Path)
	if req.URL.RawPath != "" {
		u.RawPath = joinCodexPath(base.Path, req.URL.RawPath)
	}
	u.RawQuery = req.URL.RawQuery
	return u.String()
}

const (
	maxCapturedWebSocketBytes     = 8 << 20
	maxCapturedWebSocketFrameSize = 128 << 10
)

type webSocketTraceRecorder struct {
	mu         sync.Mutex
	captureDir string
	trace      replay.WebSocketTrace
}

func newWebSocketTraceRecorder(captureDir, captureID, requestURI, upstreamURL string, headers http.Header, authHeader string) *webSocketTraceRecorder {
	headerSummary := map[string]string{
		"auth_routing": authRoutingMode(authHeader),
	}
	if ua := strings.TrimSpace(headers.Get("User-Agent")); ua != "" {
		headerSummary["user_agent"] = ua
	}
	if clientUA := strings.TrimSpace(headers.Get("X-OpenAI-Client-User-Agent")); clientUA != "" {
		headerSummary["x_openai_client_user_agent"] = clientUA
	}
	if subprotocols := requestedWebSocketSubprotocols(headers); subprotocols != "" {
		headerSummary["requested_subprotocols"] = subprotocols
	}
	return &webSocketTraceRecorder{
		captureDir: captureDir,
		trace: replay.WebSocketTrace{
			Version:     replay.WebSocketTraceVersion,
			CaptureID:   captureID,
			RequestURI:  requestURI,
			UpstreamURL: upstreamURL,
			Header:      headerSummary,
		},
	}
}

func (w *webSocketTraceRecorder) SetSelectedProto(proto string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trace.SelectedProto = proto
}

func (w *webSocketTraceRecorder) Record(direction string, messageType int, payload []byte) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	w.trace.BytesSeen += len(payload)
	if w.trace.Truncated {
		w.trace.DroppedFrames++
		return
	}

	payloadBase64, payloadText, payloadTruncated, size := replay.EncodeWebSocketFramePayload(payload, maxCapturedWebSocketFrameSize)
	frameCost := len(payloadBase64)
	if w.trace.BytesCaptured+frameCost > maxCapturedWebSocketBytes {
		w.trace.Truncated = true
		w.trace.DroppedFrames++
		return
	}

	w.trace.BytesCaptured += frameCost
	w.trace.Frames = append(w.trace.Frames, replay.WebSocketFrame{
		Timestamp:        time.Now().Format(time.RFC3339Nano),
		Direction:        direction,
		MessageType:      messageType,
		SizeBytes:        size,
		PayloadBase64:    payloadBase64,
		PayloadText:      payloadText,
		PayloadTruncated: payloadTruncated,
	})
}

func (w *webSocketTraceRecorder) Write() {
	if w == nil {
		return
	}
	w.mu.Lock()
	trace := w.trace
	w.mu.Unlock()
	if err := replay.WriteWebSocketTrace(w.captureDir, trace); err != nil {
		log.Printf("[CODEX] websocket trace write failed: %v", err)
	}
}

func proxyWebSocketFrames(dst, src *websocket.Conn, direction string, trace *webSocketTraceRecorder, telemetry *webSocketTelemetryCollector, errCh chan<- error) {
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		if trace != nil {
			trace.Record(direction, messageType, payload)
		}
		if telemetry != nil {
			telemetry.Observe(direction, messageType, payload)
		}
		if err := dst.WriteMessage(messageType, payload); err != nil {
			errCh <- err
			return
		}
	}
}

func isExpectedWebSocketClose(err error) bool {
	if err == nil {
		return true
	}
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) || errors.Is(err, net.ErrClosed)
}

type captureResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	body   bytes.Buffer
}

func (w *captureResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if remaining := 4096 - w.body.Len(); remaining > 0 {
		if len(p) > remaining {
			w.body.Write(p[:remaining])
		} else {
			w.body.Write(p)
		}
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (w *captureResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *captureResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return h.Hijack()
}

func (w *captureResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	type writerOnly struct {
		io.Writer
	}
	return io.Copy(writerOnly{w}, r)
}

func (w *captureResponseWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

// relaySSE streams SSE events from upstream to the client, extracting usage
// from the response.completed event.
func (h *Handler) relaySSE(w http.ResponseWriter, resp *http.Response, sess *session, convID string, telemetry codexRequestTelemetry) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		proxyErrorJSON(w, "server_error", "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Copy response headers.
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)

	// 1 MB scanner buffer for large SSE events.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	quotaSnapshot, quotaAvailable := extractCodexQuotaHeaders(resp.Header)
	var usage codexUsageStats
	var responseModel string
	var stopReason string
	var outputItems []map[string]interface{}
	var firstDataAt time.Time

	for scanner.Scan() {
		line := scanner.Text()
		// Write line verbatim plus newline.
		w.Write([]byte(line + "\n"))

		if line == "" {
			// Blank line = event boundary; flush to client.
			flusher.Flush()
			continue
		}

		// Parse data lines for usage extraction.
		if strings.HasPrefix(line, "data: ") {
			if firstDataAt.IsZero() {
				firstDataAt = time.Now()
			}
			data := line[6:] // len("data: ") == 6
			var evt map[string]interface{}
			if json.Unmarshal([]byte(data), &evt) == nil && evt["type"] == "response.completed" {
				if respObj, ok := evt["response"].(map[string]interface{}); ok {
					usage = extractCodexUsage(respObj)
					responseModel, stopReason = extractCodexResponseMetadata(respObj)
					outputItems = extractOutputItemsFromResponse(respObj)
				}
			}
		}
	}
	// Final flush.
	flusher.Flush()

	// Update session with real token count from upstream.
	if usage.InputTokens > 0 {
		sess.mu.Lock()
		sess.lastAPIInput = usage.InputTokens
		sess.lastAPIInputAt = time.Now()
		sess.mu.Unlock()
		log.Printf("[CODEX] conv=%s stream done: input_tokens=%d", convID, usage.InputTokens)
	}
	if added := sess.appendOutputItems(outputItems); added > 0 {
		log.Printf("[CODEX] conv=%s cached %d response output items", convID, added)
	}
	emitCodexUsageSidecar(convID, firstNonEmpty(responseModel, telemetry.ModelRequested), usage)
	if quotaAvailable {
		emitCodexQuotaSidecars(h.debugRecorder, quotaSnapshot, 1)
	}
	recordCodexRequestEvent(
		h.debugRecorder,
		telemetry,
		sess,
		responseModel,
		stopReason,
		usage,
		quotaPointer(quotaSnapshot, quotaAvailable),
		outputItems,
		firstDataAt.Sub(telemetry.StartedAt),
	)
}

// relayNonStreaming forwards a non-streaming response, extracting usage.
func (h *Handler) relayNonStreaming(w http.ResponseWriter, resp *http.Response, sess *session, telemetry codexRequestTelemetry) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		proxyErrorJSON(w, "server_error", "failed to read upstream response", http.StatusBadGateway)
		return
	}

	// Copy response headers.
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	// Extract usage from the response body.
	var respParsed map[string]interface{}
	quotaSnapshot, quotaAvailable := extractCodexQuotaHeaders(resp.Header)
	var usage codexUsageStats
	var responseModel string
	var stopReason string
	var outputItems []map[string]interface{}
	if json.Unmarshal(respBody, &respParsed) == nil {
		usage = extractCodexUsage(respParsed)
		responseModel, stopReason = extractCodexResponseMetadata(respParsed)
		if usage.InputTokens > 0 {
			sess.mu.Lock()
			sess.lastAPIInput = usage.InputTokens
			sess.lastAPIInputAt = time.Now()
			sess.mu.Unlock()
		}
		outputItems = extractOutputItemsFromResponse(respParsed)
		if added := sess.appendOutputItems(outputItems); added > 0 {
			log.Printf("[CODEX] cached %d non-stream response output items", added)
		}
	}
	emitCodexUsageSidecar(telemetry.ConversationID, firstNonEmpty(responseModel, telemetry.ModelRequested), usage)
	if quotaAvailable {
		emitCodexQuotaSidecars(h.debugRecorder, quotaSnapshot, 1)
	}
	recordCodexRequestEvent(
		h.debugRecorder,
		telemetry,
		sess,
		responseModel,
		stopReason,
		usage,
		quotaPointer(quotaSnapshot, quotaAvailable),
		outputItems,
		0,
	)
}

func extractOutputItemsFromResponse(resp map[string]interface{}) []map[string]interface{} {
	raw, ok := resp["output"].([]interface{})
	if !ok {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		items = append(items, item)
	}
	return items
}

// extractAndStripInput pulls the input array from the request body,
// removes any compaction items, and returns the cleaned list.
// Returns (nil, false) if no input field is present.
func extractAndStripInput(body map[string]interface{}) ([]interface{}, bool) {
	raw, ok := body["input"]
	if !ok {
		return nil, false
	}
	arr, ok := raw.([]interface{})
	if !ok {
		// Input might be a string (simple prompt form); leave as-is.
		return nil, false
	}

	// Strip compaction items — encrypted opaque items from server-side context management.
	cleaned := make([]interface{}, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			cleaned = append(cleaned, item)
			continue
		}
		if m["type"] == "compaction" {
			continue
		}
		cleaned = append(cleaned, item)
	}
	body["input"] = cleaned
	return cleaned, true
}

// computeFingerprint computes a session ID from instructions + model.
// SHA-256 truncated to 12 hex chars.
func computeFingerprint(body map[string]interface{}) string {
	h := sha256.New()

	if inst, ok := body["instructions"].(string); ok {
		// Use first 512 chars to keep fingerprint stable across minor prompt variations.
		if len(inst) > 512 {
			inst = inst[:512]
		}
		h.Write([]byte(inst))
	}
	if model, ok := body["model"].(string); ok {
		h.Write([]byte(model))
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// IsCodexRequest returns true if the body represents a Codex Responses API
// request: it has an "input" field and does NOT have a "messages" field.
func IsCodexRequest(body map[string]interface{}) bool {
	_, hasInput := body["input"]
	_, hasMessages := body["messages"]
	return hasInput && !hasMessages
}

// injectDeveloperMessage adds a summary as a developer message item at the front of
// the session cache (after any existing developer/system items).
func (sess *session) injectDeveloperMessage(summary string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	item := map[string]interface{}{
		"type": "message",
		"role": "developer",
		"content": []interface{}{
			map[string]interface{}{
				"type": "input_text",
				"text": summary,
			},
		},
	}

	ci := cachedItem{
		Item:   item,
		Type:   "message",
		Role:   "developer",
		Tokens: estimateTokens(item),
		Hash:   hashItem(item),
	}

	// Insert after leading developer/system items.
	insertPos := 0
	for i, c := range sess.items {
		if c.Role == "developer" || c.Role == "system" {
			insertPos = i + 1
		} else {
			break
		}
	}

	result := make([]cachedItem, 0, len(sess.items)+1)
	result = append(result, sess.items[:insertPos]...)
	result = append(result, ci)
	result = append(result, sess.items[insertPos:]...)
	sess.items = result
	sess.updatedAt = time.Now()
}

// proxyErrorJSON writes a JSON error response matching OpenAI's error format.
func proxyErrorJSON(w http.ResponseWriter, errorType, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errorType,
			"message": message,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// captureSystemPrompt persists the Codex instructions to disk for debugging and audit.
// First-request capture: writes to {shadowDir}/{convID}/system_prompt.txt.
// Every-request dump: writes to the active runtime capture root.
func captureSystemPrompt(shadowDir, convID string, body map[string]interface{}) {
	inst, ok := body["instructions"].(string)
	if !ok || inst == "" {
		return
	}

	// Per-session capture (first request only).
	captureDir := expandHome(shadowDir) + "/" + convID
	promptPath := captureDir + "/system_prompt.txt"
	if _, err := os.Stat(promptPath); os.IsNotExist(err) {
		_ = os.MkdirAll(captureDir, 0o755)
		if err := os.WriteFile(promptPath, []byte(inst), 0o644); err == nil {
			log.Printf("[CODEX] conv=%s captured system prompt (%d bytes)", convID, len(inst))
		}
	}

	// Debug dump (every request).
	captureRoot := runtimepaths.Current().LiveCaptureDir
	_ = os.MkdirAll(captureRoot, 0o755)
	_ = os.WriteFile(filepath.Join(captureRoot, "outbound_codex_instructions.txt"), []byte(inst), 0o644)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func joinCodexPath(basePath, reqPath string) string {
	if reqPath == "" {
		return basePath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(reqPath, "/")
}
