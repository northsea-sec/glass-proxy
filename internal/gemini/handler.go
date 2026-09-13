// handler.go — HTTP handler for the Gemini CLI lane.
//
// Intercepts POST /models/{model}:generateContent and :streamGenerateContent,
// applies Glass-style context management (cache, compress, evict, summarize),
// manages explicit CachedContent for prefix caching (90% discount),
// and relays SSE responses.
package gemini

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Config configures the Gemini lane handler.
type Config struct {
	Upstream             string // e.g. "https://generativelanguage.googleapis.com"
	ShadowDir            string // e.g. "~/.claude/glass-gemini/"
	EvictTriggerTokens   int    // default 700000
	EvictTargetTokens    int    // default 500000
	KeepRecentContents   int    // default 20
	MaxFuncRespChars     int    // default 700
	MaxModelTextChars    int    // default 500
	CompBatchSize        int    // default 40
	FlushTokens          int    // default 600000 (saturation flush threshold)
	SummarizeEnabled     bool
	SummarizeInterval    int    // default 80000
	SummarizeModel       string // default "gemini-2.5-pro"
	ExplicitCacheEnabled bool
	CacheTTLSec          int // default 3600
	// SyspromptFunc, if set, processes system prompt fragments through the
	// glass sysprompt pipeline. Signature matches sysprompt.Pipeline.Process.
	SyspromptFunc func([]interface{}) ([]interface{}, int)
	DebugRecorder telemetryRecorder
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Upstream:           "https://generativelanguage.googleapis.com",
		ShadowDir:          home + "/.claude/glass-gemini",
		EvictTriggerTokens: 700000,
		EvictTargetTokens:  500000,
		KeepRecentContents: 20,
		MaxFuncRespChars:   700,
		MaxModelTextChars:  500,
		CompBatchSize:      40,
		FlushTokens:        600000,
		SummarizeEnabled:   false,
		SummarizeInterval:  80000,
		SummarizeModel:     "gemini-2.5-pro",
		CacheTTLSec:        3600,
	}
}

// Handler handles Gemini CLI generateContent/streamGenerateContent requests.
type Handler struct {
	upstream   string
	sessions   *sessionStore
	summarizer *rollingSummarizer
	cacheMgr   *cacheManager
	recorder   telemetryRecorder
	cfg        Config
	authMu     sync.Mutex
	authKind   string
	authValue  string
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
	ExplicitCacheEnabled     bool   `json:"explicit_cache_enabled"`
	SessionOwner             string `json:"session_owner"`
	SessionPersistence       string `json:"session_persistence"`
	SupportsStatelessProbe   bool   `json:"supports_stateless_probe"`
	SupportsLiveSessionProbe bool   `json:"supports_live_session_probe"`
	SupportsResumableSession bool   `json:"supports_resumable_session"`
	SessionCount             int    `json:"session_count"`
}

// NewHandler creates the Gemini lane handler.
func NewHandler(cfg Config) *Handler {
	if cfg.Upstream == "" {
		cfg.Upstream = "https://generativelanguage.googleapis.com"
	}
	sumCfg := summarizerConfig{
		Enabled:       cfg.SummarizeEnabled,
		IntervalToken: cfg.SummarizeInterval,
		Model:         cfg.SummarizeModel,
		Upstream:      cfg.Upstream,
		ShadowDir:     cfg.ShadowDir,
	}
	return &Handler{
		upstream:   cfg.Upstream,
		sessions:   newSessionStore(cfg.ShadowDir),
		summarizer: newRollingSummarizer(sumCfg, nil),
		cacheMgr:   newCacheManager(cfg.ExplicitCacheEnabled, cfg.Upstream, cfg.CacheTTLSec),
		recorder:   cfg.DebugRecorder,
		cfg:        cfg,
	}
}

func sanitizeReplayRequestURI(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	query := clone.Query()
	query.Del("key")
	clone.RawQuery = query.Encode()
	return clone.RequestURI()
}

func (h *Handler) captureRequestAuth(apiKey, authHeader string) {
	if h == nil {
		return
	}
	h.authMu.Lock()
	defer h.authMu.Unlock()
	switch {
	case strings.TrimSpace(apiKey) != "":
		h.authKind = "api_key"
		h.authValue = apiKey
	case strings.TrimSpace(authHeader) != "":
		h.authKind = "bearer"
		h.authValue = authHeader
	}
}

func (h *Handler) capturedAuthStatus() (bool, string) {
	if h == nil {
		return false, "unavailable"
	}
	h.authMu.Lock()
	defer h.authMu.Unlock()
	if strings.TrimSpace(h.authValue) == "" {
		return false, "unavailable"
	}
	if h.authKind == "" {
		return true, "authorization"
	}
	return true, h.authKind
}

func (h *Handler) ApplyCapturedAuth(req *http.Request) error {
	if h == nil {
		return fmt.Errorf("gemini lane unavailable")
	}
	h.authMu.Lock()
	authKind := h.authKind
	authValue := h.authValue
	h.authMu.Unlock()
	if strings.TrimSpace(authValue) == "" {
		return fmt.Errorf("captured gemini auth unavailable")
	}
	switch authKind {
	case "api_key":
		req.Header.Set("x-goog-api-key", authValue)
	case "bearer":
		req.Header.Set("Authorization", authValue)
	default:
		return fmt.Errorf("unsupported gemini auth type %q", authKind)
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
	explicitCacheEnabled := false
	if h != nil {
		upstream = h.upstream
		shadowDir = h.cfg.ShadowDir
		summarizerEnabled = h.summarizer != nil
		explicitCacheEnabled = h.cacheMgr != nil
	}
	return LaneAuthStatus{
		Lane:                     "gemini",
		Upstream:                 upstream,
		UpstreamConfigured:       strings.TrimSpace(upstream) != "",
		ShadowDir:                shadowDir,
		RequestAuthPassthrough:   true,
		CapturedAuthAvailable:    captured,
		CapturedAuthType:         capturedType,
		SummarizerEnabled:        summarizerEnabled,
		ExplicitCacheEnabled:     explicitCacheEnabled,
		SessionOwner:             "glass_gemini_lane",
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

const maxBodySize = 10 * 1024 * 1024

// modelPattern extracts the model name from Gemini API paths.
// e.g. "/v1beta/models/gemini-2.5-pro:generateContent" -> "gemini-2.5-pro"
var modelPattern = regexp.MustCompile(`/models/([^/:]+)`)

// ServeHTTP implements the full Gemini lane pipeline.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()

	// 1. Read body.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		geminiError(w, "failed to read request body", http.StatusInternalServerError)
		return
	}
	if len(body) > maxBodySize {
		geminiError(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// 2. Parse JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		geminiError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Extract model from URL path.
	model := extractModel(r.URL.Path)
	parsed["_glass_model"] = model // used by computeFingerprint

	// 3. Compute session ID.
	convID := computeFingerprint(parsed)
	delete(parsed, "_glass_model")
	reqTelemetry := buildGeminiRequestTelemetryAt(convID, model, parsed, startedAt)

	isStreaming := strings.Contains(r.URL.Path, ":streamGenerateContent")
	log.Printf("[GEMINI] %s conv=%s stream=%v from %s", model, convID, isStreaming, r.RemoteAddr)

	apiKey := r.URL.Query().Get("key")
	if apiKey == "" {
		apiKey = r.Header.Get("x-goog-api-key")
	}
	authHeader := r.Header.Get("Authorization")
	h.captureRequestAuth(apiKey, authHeader)

	// 4. Get or create session.
	sess := h.sessions.get(convID)
	sess.mu.Lock()
	sess.model = model
	sess.updatedAt = time.Now()
	sess.mu.Unlock()
	if template := deepCopyContent(parsed); template != nil {
		delete(template, "contents")
		delete(template, "cachedContent")
		sess.captureReplayTemplate(sanitizeReplayRequestURI(r.URL), template)
	}

	// 5. Capture auth for summarizer and cache manager.
	if apiKey != "" {
		if h.summarizer != nil {
			h.summarizer.captureAuth(apiKey)
		}
		if h.cacheMgr != nil {
			h.cacheMgr.captureAuth(apiKey)
		}
	}

	// 5b. Apply sysprompt pipeline to systemInstruction.
	if h.cfg.SyspromptFunc != nil {
		if si, ok := parsed["systemInstruction"].(map[string]interface{}); ok {
			if parts, ok := si["parts"].([]interface{}); ok && len(parts) > 0 {
				// Convert Gemini parts to Anthropic fragment format.
				var fragments []interface{}
				for _, p := range parts {
					if part, ok := p.(map[string]interface{}); ok {
						if text, ok := part["text"].(string); ok {
							fragments = append(fragments, map[string]interface{}{"type": "text", "text": text})
						}
					}
				}
				if len(fragments) > 0 {
					processed, mods := h.cfg.SyspromptFunc(fragments)
					if mods > 0 {
						var newParts []interface{}
						for _, frag := range processed {
							if block, ok := frag.(map[string]interface{}); ok {
								if text, ok := block["text"].(string); ok && text != "" {
									newParts = append(newParts, map[string]interface{}{"text": text})
								}
							}
						}
						si["parts"] = newParts
						log.Printf("[GEMINI] conv=%s sysprompt: %d modifications applied", convID, mods)
					}
				}
			}
		}
	}

	// 5c. Capture and persist system prompt.
	captureGeminiSystemPrompt(h.cfg.ShadowDir, convID, parsed)

	// 6. Extract contents.
	contents, hasContents := parsed["contents"].([]interface{})

	// 7-11: Context management pipeline.
	if hasContents && len(contents) > 0 {
		// 7. Ingest new contents.
		newCount := sess.ingest(contents)

		// 8. Progressive compression (watermark-based).
		sess.mu.Lock()
		itemCount := len(sess.contents)
		currentWM := sess.compWatermark
		sess.mu.Unlock()

		newWatermark := (itemCount / h.cfg.CompBatchSize) * h.cfg.CompBatchSize
		safeLimit := itemCount - h.cfg.KeepRecentContents
		if safeLimit < 0 {
			safeLimit = 0
		}
		if newWatermark > currentWM && newWatermark-currentWM >= h.cfg.CompBatchSize && safeLimit > currentWM {
			end := newWatermark
			if end > safeLimit {
				end = safeLimit
			}
			saved, compressed := sess.compressOld(end, h.cfg.MaxFuncRespChars, h.cfg.MaxModelTextChars)
			if saved > 0 {
				log.Printf("[GEMINI] conv=%s compressed %d contents (~%d tokens saved, watermark->%d)",
					convID, compressed, saved, end)
			}
			sess.mu.Lock()
			sess.compWatermark = end
			sess.mu.Unlock()
		}

		// 9. Saturation flush.
		totalTok := sess.totalTokens()
		lossRatio := sess.informationLossRatio()
		if (lossRatio >= 0.85 || totalTok > h.cfg.FlushTokens) && h.summarizer != nil {
			st := h.summarizer.getState(convID)
			if st != nil && len(st.Chunks) > 0 {
				summary := h.summarizer.stitchSummary(convID)
				if summary != "" {
					fullSummary := "## Session Recovery (auto-flush)\n\n" +
						"Context was compressed and flushed. " +
						"CONTINUE the work described below.\n\n" + summary
					removed := sess.flushCompressed(fullSummary)
					if removed > 0 {
						log.Printf("[GEMINI] conv=%s saturation flush: ratio=%.2f tokens=%d, removed %d",
							convID, lossRatio, totalTok, removed)
						h.summarizer.resetForConv(convID)
						sess.mu.Lock()
						sess.compWatermark = 0
						sess.mu.Unlock()
					}
				}
			}
		}

		// 10. Eviction.
		totalTok = sess.totalTokens()
		if totalTok > h.cfg.EvictTriggerTokens {
			evicted := sess.evict(h.cfg.EvictTargetTokens, h.cfg.KeepRecentContents)
			if len(evicted) > 0 {
				sess.mu.Lock()
				batchNum := sess.batchCount
				sess.mu.Unlock()

				log.Printf("[GEMINI] conv=%s evicted %d contents (%d tokens > %d trigger, batch=%d)",
					convID, len(evicted), totalTok, h.cfg.EvictTriggerTokens, batchNum)

				if err := writeShadow(h.cfg.ShadowDir, convID, evicted, batchNum); err != nil {
					log.Printf("[GEMINI] shadow write error: %v", err)
				}

				// Inject summary at eviction.
				if h.cfg.SummarizeEnabled && h.summarizer != nil {
					st := h.summarizer.getState(convID)
					if st != nil && len(st.Chunks) > 0 {
						summary := h.summarizer.stitchSummary(convID)
						if summary != "" {
							fullSummary := "## Session Recovery (eviction batch " +
								fmt.Sprintf("%d", batchNum) + ")\n\n" +
								"Prior context was evicted. CONTINUE the work described below.\n\n" + summary
							sess.injectSummaryExchange(fullSummary)
							h.summarizer.resetForConv(convID)
						}
					}
				}

				if err := sess.save(h.cfg.ShadowDir); err != nil {
					log.Printf("[GEMINI] cache save error: %v", err)
				}
			}
		}

		// 11. Rolling summarization (background).
		if h.cfg.SummarizeEnabled && h.summarizer != nil && newCount > 0 {
			go h.summarizer.maybeChunk(convID, sess)
		}
	}

	// 12. Rebuild contents from session cache.
	if hasContents {
		parsed["contents"] = sess.buildContents()
	}

	// 13. Explicit CachedContent for prefix caching (90% discount).
	if h.cacheMgr != nil && apiKey != "" {
		cacheName := h.cacheMgr.ensureCache(
			convID, model,
			parsed["systemInstruction"],
			parsed["tools"],
		)
		if cacheName != "" {
			parsed["cachedContent"] = cacheName
			// When using cachedContent, the cached fields must NOT be in the request body.
			delete(parsed, "systemInstruction")
			delete(parsed, "tools")
		}
	}

	// 14. Log session stats.
	sess.mu.Lock()
	contentsLen := len(sess.contents)
	evictedTotal := sess.evictedCount
	sess.mu.Unlock()
	log.Printf("[GEMINI] conv=%s contents=%d tokens=%d evicted=%d loss=%.2f",
		convID, contentsLen, sess.totalTokens(), evictedTotal, sess.informationLossRatio())

	// 15. Re-serialize body.
	outBody, err := json.Marshal(parsed)
	if err != nil {
		geminiError(w, "failed to serialize request", http.StatusInternalServerError)
		return
	}

	// 16. Forward to upstream, preserving the original path and query string.
	upstreamURL := strings.TrimRight(h.upstream, "/") + r.URL.RequestURI()
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, strings.NewReader(string(outBody)))
	if err != nil {
		geminiError(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	// Forward auth headers.
	if v := r.Header.Get("x-goog-api-key"); v != "" {
		upReq.Header.Set("x-goog-api-key", v)
	}
	if authHeader != "" {
		upReq.Header.Set("Authorization", authHeader)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(upReq)
	if err != nil {
		log.Printf("[GEMINI] upstream error: %v", err)
		geminiError(w, fmt.Sprintf("upstream connection failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("[GEMINI] upstream responded %d %s", resp.StatusCode, resp.Status)

	// 17. Relay response.
	var respTelemetry geminiResponseTelemetry
	if isStreaming && resp.StatusCode == http.StatusOK {
		respTelemetry = h.relaySSE(w, resp, sess, convID, startedAt)
	} else {
		respTelemetry = h.relayNonStreaming(w, resp, sess, startedAt)
		if resp.StatusCode >= http.StatusBadRequest && respTelemetry.StopReason == "" {
			respTelemetry.StopReason = fmt.Sprintf("http_%d", resp.StatusCode)
		}
	}
	if h.recorder != nil {
		recordGeminiRequestEvent(h.recorder, reqTelemetry, sess, respTelemetry)
	}
}

// relaySSE streams Gemini SSE events (data: lines) to the client.
func (h *Handler) relaySSE(w http.ResponseWriter, resp *http.Response, sess *session, convID string, startedAt time.Time) geminiResponseTelemetry {
	var telemetry geminiResponseTelemetry
	flusher, ok := w.(http.Flusher)
	if !ok {
		geminiError(w, "streaming not supported", http.StatusInternalServerError)
		telemetry.StopReason = "streaming_unsupported"
		return telemetry
	}

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastModelContent map[string]interface{}
	var sawUpstreamChunk bool

	for scanner.Scan() {
		line := scanner.Text()
		w.Write([]byte(line + "\n"))

		if line == "" {
			flusher.Flush()
			continue
		}

		// Extract usage from data: lines. Gemini puts usageMetadata in the final chunk.
		if strings.HasPrefix(line, "data: ") {
			if !sawUpstreamChunk {
				sawUpstreamChunk = true
				telemetry.TTFT = time.Since(startedAt)
			}
			var chunk map[string]interface{}
			if json.Unmarshal([]byte(line[6:]), &chunk) == nil {
				telemetry.merge(extractGeminiResponseTelemetry(chunk))
				if content := extractCandidateContent(chunk); content != nil {
					lastModelContent = content
				}
			}
		}
	}
	flusher.Flush()

	if telemetry.InputTokens > 0 {
		sess.mu.Lock()
		sess.lastAPIInput = telemetry.InputTokens
		sess.lastAPIInputAt = time.Now()
		sess.mu.Unlock()
		log.Printf("[GEMINI] conv=%s stream done: promptTokens=%d", convID, telemetry.InputTokens)
	}
	if sess.appendModelContent(lastModelContent) {
		log.Printf("[GEMINI] conv=%s cached streamed model content", convID)
	}
	return telemetry
}

// relayNonStreaming forwards a non-streaming response, extracting usage.
func (h *Handler) relayNonStreaming(w http.ResponseWriter, resp *http.Response, sess *session, startedAt time.Time) geminiResponseTelemetry {
	telemetry := geminiResponseTelemetry{TTFT: time.Since(startedAt)}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		geminiError(w, "failed to read upstream response", http.StatusBadGateway)
		telemetry.StopReason = "read_error"
		return telemetry
	}

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	var respParsed map[string]interface{}
	if json.Unmarshal(respBody, &respParsed) == nil {
		telemetry.merge(extractGeminiResponseTelemetry(respParsed))
		if telemetry.InputTokens > 0 {
			sess.mu.Lock()
			sess.lastAPIInput = telemetry.InputTokens
			sess.lastAPIInputAt = time.Now()
			sess.mu.Unlock()
		}
		if sess.appendModelContent(extractCandidateContent(respParsed)) {
			log.Printf("[GEMINI] cached non-stream model content")
		}
	}
	return telemetry
}

// IsGeminiRequest returns true if the body has a "contents" field and no "messages"/"input" field.
func IsGeminiRequest(body map[string]interface{}) bool {
	_, hasContents := body["contents"]
	_, hasMessages := body["messages"]
	_, hasInput := body["input"]
	return hasContents && !hasMessages && !hasInput
}

// extractModel pulls the model name from a Gemini API URL path.
func extractModel(path string) string {
	matches := modelPattern.FindStringSubmatch(path)
	if len(matches) >= 2 {
		return matches[1]
	}
	return "gemini-2.5-pro"
}

func extractCandidateContent(resp map[string]interface{}) map[string]interface{} {
	candidates, ok := resp["candidates"].([]interface{})
	if !ok {
		return nil
	}
	for _, raw := range candidates {
		candidate, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := candidate["content"].(map[string]interface{})
		if ok {
			return content
		}
	}
	return nil
}

// geminiError writes a JSON error response in Gemini API format.
func geminiError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    status,
			"message": message,
			"status":  http.StatusText(status),
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// captureGeminiSystemPrompt persists the Gemini systemInstruction for debugging and audit.
// First-request capture: writes to {shadowDir}/{convID}/system_prompt.json.
// Every-request dump: writes to /tmp/glass-proxy/outbound_gemini_system_instruction.json.
func captureGeminiSystemPrompt(shadowDir, convID string, body map[string]interface{}) {
	si, ok := body["systemInstruction"].(map[string]interface{})
	if !ok {
		return
	}
	data, err := json.MarshalIndent(si, "", "  ")
	if err != nil {
		return
	}

	// Per-session capture (first request only).
	captureDir := filepath.Join(shadowDir, convID)
	promptPath := filepath.Join(captureDir, "system_prompt.json")
	if _, statErr := os.Stat(promptPath); os.IsNotExist(statErr) {
		_ = os.MkdirAll(captureDir, 0o755)
		if writeErr := os.WriteFile(promptPath, data, 0o644); writeErr == nil {
			log.Printf("[GEMINI] conv=%s captured system prompt (%d bytes)", convID, len(data))
		}
	}

	// Debug dump (every request).
	_ = os.MkdirAll("/tmp/glass-proxy", 0o755)
	_ = os.WriteFile("/tmp/glass-proxy/outbound_gemini_system_instruction.json", data, 0o644)
}
