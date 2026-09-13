// session.go — per-conversation cache, eviction, and compression for the Gemini lane.
//
// Gemini contents are {role: "user"|"model", parts: [...]} objects.
// Parts contain {text}, {functionCall: {name, args}}, {functionResponse: {name, response}}.
package gemini

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// cachedContent is a single Gemini Content object stored in the session cache.
type cachedContent struct {
	Content    map[string]interface{} // original Content object (immutable after ingest)
	Role       string                 // "user" or "model"
	Tokens     int                    // estimated token count
	Hash       string                 // SHA-256 of JSON, first 16 hex chars
	Compressed bool                   // true if content was truncated
	OrigTokens int                    // pre-compression token count (0 if never compressed)
}

// session holds per-conversation state for a Gemini lane session.
type session struct {
	mu              sync.Mutex
	contents        []cachedContent
	convID          string
	model           string
	requestURI      string
	requestTemplate map[string]interface{}
	lastAPIInput    int // last real promptTokenCount from upstream
	lastAPIInputAt  time.Time
	evictedCount    int // total contents evicted across all batches
	batchCount      int // number of eviction batches
	compWatermark   int // compression watermark position
	createdAt       time.Time
	updatedAt       time.Time
}

// sessionStore manages per-conversation sessions.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	baseDir  string
}

type LaneSessionSnapshot struct {
	ConvID               string `json:"conv_id"`
	Model                string `json:"model"`
	ContentCount         int    `json:"content_count"`
	TotalTokens          int    `json:"total_tokens"`
	EvictedCount         int    `json:"evicted_count"`
	BatchCount           int    `json:"batch_count"`
	CompressionWatermark int    `json:"compression_watermark"`
	LastAPIInput         int    `json:"last_api_input"`
	LastAPIInputAt       string `json:"last_api_input_at"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
	CachePath            string `json:"cache_path"`
	CachePersisted       bool   `json:"cache_persisted"`
	SystemPromptPath     string `json:"system_prompt_path"`
	SystemPromptCaptured bool   `json:"system_prompt_captured"`
}

type LaneSessionReplay struct {
	Lane                  string                 `json:"lane"`
	ConvID                string                 `json:"conv_id"`
	RequestURI            string                 `json:"request_uri"`
	ProxyTemplate         map[string]interface{} `json:"proxy_template"`
	LiveContents          []interface{}          `json:"live_contents"`
	Snapshot              LaneSessionSnapshot    `json:"snapshot"`
	CapturedAuthAvailable bool                   `json:"captured_auth_available"`
	CapturedAuthType      string                 `json:"captured_auth_type"`
}

func newSessionStore(baseDir string) *sessionStore {
	return &sessionStore{
		sessions: make(map[string]*session),
		baseDir:  baseDir,
	}
}

func formatLaneTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (s *sessionStore) get(convID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[convID]
	if !ok {
		now := time.Now()
		sess = &session{convID: convID, createdAt: now, updatedAt: now}
		s.sessions[convID] = sess
	}
	return sess
}

func (s *sessionStore) snapshots() []LaneSessionSnapshot {
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	baseDir := s.baseDir
	s.mu.Unlock()

	snapshots := make([]LaneSessionSnapshot, 0, len(sessions))
	for _, sess := range sessions {
		snapshots = append(snapshots, sess.snapshot(baseDir))
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].UpdatedAt > snapshots[j].UpdatedAt
	})
	return snapshots
}

func (s *sessionStore) snapshot(convID string) (LaneSessionSnapshot, bool) {
	s.mu.Lock()
	sess, ok := s.sessions[convID]
	baseDir := s.baseDir
	s.mu.Unlock()
	if !ok {
		return LaneSessionSnapshot{}, false
	}
	return sess.snapshot(baseDir), true
}

func (s *sessionStore) replay(convID string) (LaneSessionReplay, bool) {
	s.mu.Lock()
	sess, ok := s.sessions[convID]
	baseDir := s.baseDir
	s.mu.Unlock()
	if !ok {
		return LaneSessionReplay{}, false
	}
	return sess.replay(baseDir), true
}

// --- Helpers ---

func estimateTokens(v interface{}) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	n := len(b) / 4
	if n == 0 && len(b) > 0 {
		return 1
	}
	return n
}

func hashContent(c map[string]interface{}) string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)[:16]
}

func deepCopyContent(c map[string]interface{}) map[string]interface{} {
	b, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// computeFingerprint computes a session ID from systemInstruction + model.
// SHA-256 truncated to 12 hex chars.
func computeFingerprint(body map[string]interface{}) string {
	h := sha256.New()

	// Hash systemInstruction text.
	if si, ok := body["systemInstruction"].(map[string]interface{}); ok {
		if parts, ok := si["parts"].([]interface{}); ok {
			for _, p := range parts {
				if part, ok := p.(map[string]interface{}); ok {
					if text, ok := part["text"].(string); ok {
						if len(text) > 512 {
							text = text[:512]
						}
						h.Write([]byte(text))
					}
				}
			}
		}
	}

	// Hash model name from the URL path (passed separately by handler).
	if model, ok := body["_glass_model"].(string); ok {
		h.Write([]byte(model))
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// --- Session methods ---

// ingest adds new contents from the request into the session cache.
// Compares by position: only contents beyond current cache length are added.
func (sess *session) ingest(contents []interface{}) int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(contents) <= len(sess.contents) {
		return 0
	}

	added := 0
	for i := len(sess.contents); i < len(contents); i++ {
		raw, ok := contents[i].(map[string]interface{})
		if !ok {
			continue
		}

		copied := deepCopyContent(raw)
		if copied == nil {
			continue
		}

		role, _ := copied["role"].(string)

		sess.contents = append(sess.contents, cachedContent{
			Content: copied,
			Role:    role,
			Tokens:  estimateTokens(copied),
			Hash:    hashContent(copied),
		})
		added++
	}

	sess.updatedAt = time.Now()
	return added
}

// totalTokens returns the sum of estimated tokens across all cached contents.
func (sess *session) totalTokens() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	total := 0
	for i := range sess.contents {
		total += sess.contents[i].Tokens
	}
	return total
}

func (sess *session) snapshot(baseDir string) LaneSessionSnapshot {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.snapshotLocked(baseDir)
}

func (sess *session) snapshotLocked(baseDir string) LaneSessionSnapshot {
	cachePath := filepath.Join(baseDir, sess.convID, "cache.json")
	systemPromptPath := filepath.Join(baseDir, sess.convID, "system_prompt.json")
	totalTokens := 0
	for _, cc := range sess.contents {
		totalTokens += cc.Tokens
	}

	return LaneSessionSnapshot{
		ConvID:               sess.convID,
		Model:                sess.model,
		ContentCount:         len(sess.contents),
		TotalTokens:          totalTokens,
		EvictedCount:         sess.evictedCount,
		BatchCount:           sess.batchCount,
		CompressionWatermark: sess.compWatermark,
		LastAPIInput:         sess.lastAPIInput,
		LastAPIInputAt:       formatLaneTime(sess.lastAPIInputAt),
		CreatedAt:            formatLaneTime(sess.createdAt),
		UpdatedAt:            formatLaneTime(sess.updatedAt),
		CachePath:            cachePath,
		CachePersisted:       pathExists(cachePath),
		SystemPromptPath:     systemPromptPath,
		SystemPromptCaptured: pathExists(systemPromptPath),
	}
}

func (sess *session) captureReplayTemplate(requestURI string, template map[string]interface{}) {
	if requestURI == "" || template == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.requestURI = requestURI
	sess.requestTemplate = deepCopyContent(template)
	sess.updatedAt = time.Now()
}

func (sess *session) replay(baseDir string) LaneSessionReplay {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	template := deepCopyContent(sess.requestTemplate)
	liveContents := make([]interface{}, len(sess.contents))
	for i, cc := range sess.contents {
		liveContents[i] = deepCopyContent(cc.Content)
	}

	return LaneSessionReplay{
		Lane:          "gemini",
		ConvID:        sess.convID,
		RequestURI:    sess.requestURI,
		ProxyTemplate: template,
		LiveContents:  liveContents,
		Snapshot:      sess.snapshotLocked(baseDir),
	}
}

// evict removes oldest contents from the middle of the cache until under targetTokens.
// Preserves first content (system-equivalent) and last keepRecent contents.
func (sess *session) evict(targetTokens, keepRecent int) []map[string]interface{} {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	total := 0
	for i := range sess.contents {
		total += sess.contents[i].Tokens
	}
	if total <= targetTokens {
		return nil
	}

	// Protect index 0 (often the first user prompt with system context).
	evictStart := 1
	evictEnd := len(sess.contents) - keepRecent
	if evictEnd <= evictStart {
		return nil
	}

	var evicted []map[string]interface{}
	for total > targetTokens && evictStart < evictEnd {
		evicted = append(evicted, sess.contents[evictStart].Content)
		total -= sess.contents[evictStart].Tokens
		sess.contents = append(sess.contents[:evictStart], sess.contents[evictStart+1:]...)
		evictEnd--
		sess.evictedCount++
	}

	if len(evicted) > 0 {
		sess.batchCount++
	}
	sess.updatedAt = time.Now()
	return evicted
}

// compressOld compresses contents before watermark position.
// For model text: truncates text parts. For functionResponse: truncates response content.
// Idempotent: skips already-compressed contents.
func (sess *session) compressOld(watermark, maxFuncRespChars, maxModelTextChars int) (int, int) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if watermark > len(sess.contents) {
		watermark = len(sess.contents)
	}

	tokensSaved := 0
	compressed := 0

	for i := 0; i < watermark; i++ {
		cc := &sess.contents[i]
		if cc.Compressed {
			continue
		}

		changed := false
		parts, ok := cc.Content["parts"].([]interface{})
		if !ok {
			continue
		}

		for _, raw := range parts {
			part, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}

			// Compress functionResponse.response (tool output equivalent).
			if fr, ok := part["functionResponse"].(map[string]interface{}); ok && maxFuncRespChars > 0 {
				if resp, ok := fr["response"].(map[string]interface{}); ok {
					// Response content is often {"result": "..."} or similar.
					for k, v := range resp {
						if s, ok := v.(string); ok {
							if result, truncated := compressTruncate(s, maxFuncRespChars); truncated {
								resp[k] = result
								changed = true
							}
						}
					}
				}
			}

			// Compress model text parts.
			if cc.Role == "model" && maxModelTextChars > 0 {
				if text, ok := part["text"].(string); ok {
					if result, truncated := compressTruncate(text, maxModelTextChars); truncated {
						part["text"] = result
						changed = true
					}
				}
			}
		}

		if changed {
			origTokens := cc.Tokens
			cc.OrigTokens = origTokens
			cc.Tokens = estimateTokens(cc.Content)
			cc.Compressed = true
			tokensSaved += origTokens - cc.Tokens
			compressed++
		}
	}

	sess.updatedAt = time.Now()
	return tokensSaved, compressed
}

// compressTruncate truncates text to maxChars using 2/3 head + 1/3 tail.
// Idempotent: returns (text, false) if already compressed or short enough.
func compressTruncate(text string, maxChars int) (string, bool) {
	if len(text) <= maxChars {
		return text, false
	}
	if strings.Contains(text, "[...compressed") {
		return text, false
	}

	dropped := len(text) - maxChars
	marker := fmt.Sprintf("[...compressed %d chars...]", dropped)
	budget := maxChars - len(marker)
	if budget < 0 {
		budget = 0
	}
	headLen := budget * 2 / 3
	tailLen := budget - headLen

	var b strings.Builder
	b.Grow(headLen + len(marker) + tailLen)
	b.WriteString(text[:headLen])
	b.WriteString(marker)
	if tailLen > 0 {
		b.WriteString(text[len(text)-tailLen:])
	}
	return b.String(), true
}

// buildContents returns all cached contents as []interface{} for the request body.
func (sess *session) buildContents() []interface{} {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out := make([]interface{}, len(sess.contents))
	for i, cc := range sess.contents {
		out[i] = cc.Content
	}
	return out
}

func (sess *session) appendModelContent(content map[string]interface{}) bool {
	copied := deepCopyContent(content)
	if copied == nil {
		return false
	}
	role, _ := copied["role"].(string)
	if role == "" {
		role = "model"
		copied["role"] = role
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.contents = append(sess.contents, cachedContent{
		Content: copied,
		Role:    role,
		Tokens:  estimateTokens(copied),
		Hash:    hashContent(copied),
	})
	sess.updatedAt = time.Now()
	return true
}

// informationLossRatio returns the fraction of original tokens compressed away.
func (sess *session) informationLossRatio() float64 {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	var origTotal, currentTotal int
	for _, cc := range sess.contents {
		if cc.Compressed && cc.OrigTokens > 0 {
			origTotal += cc.OrigTokens
			currentTotal += cc.Tokens
		}
	}
	if origTotal == 0 {
		return 0
	}
	return float64(origTotal-currentTotal) / float64(origTotal)
}

// flushCompressed removes all compressed contents and injects a summary
// as a user+model exchange at the front (after any leading uncompressed contents).
func (sess *session) flushCompressed(summary string) int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	kept := make([]cachedContent, 0, len(sess.contents))
	removed := 0
	for _, cc := range sess.contents {
		if cc.Compressed {
			removed++
		} else {
			kept = append(kept, cc)
		}
	}
	if removed == 0 {
		return 0
	}

	// Create a user message with the summary and a model acknowledgment.
	userMsg := map[string]interface{}{
		"role":  "user",
		"parts": []interface{}{map[string]interface{}{"text": summary}},
	}
	modelMsg := map[string]interface{}{
		"role":  "model",
		"parts": []interface{}{map[string]interface{}{"text": "Understood. Continuing from the session summary above."}},
	}

	userCC := cachedContent{Content: userMsg, Role: "user", Tokens: estimateTokens(userMsg), Hash: hashContent(userMsg)}
	modelCC := cachedContent{Content: modelMsg, Role: "model", Tokens: estimateTokens(modelMsg), Hash: hashContent(modelMsg)}

	// Insert after first content (preserve original opening exchange).
	insertPos := 0
	if len(kept) > 0 {
		insertPos = 1
	}

	result := make([]cachedContent, 0, len(kept)+2)
	result = append(result, kept[:insertPos]...)
	result = append(result, userCC, modelCC)
	result = append(result, kept[insertPos:]...)
	sess.contents = result

	sess.updatedAt = time.Now()
	return removed
}

// injectSummaryExchange injects a user+model summary exchange after the first content.
func (sess *session) injectSummaryExchange(summary string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	userMsg := map[string]interface{}{
		"role":  "user",
		"parts": []interface{}{map[string]interface{}{"text": summary}},
	}
	modelMsg := map[string]interface{}{
		"role":  "model",
		"parts": []interface{}{map[string]interface{}{"text": "Understood. Continuing from the session summary above."}},
	}

	userCC := cachedContent{Content: userMsg, Role: "user", Tokens: estimateTokens(userMsg), Hash: hashContent(userMsg)}
	modelCC := cachedContent{Content: modelMsg, Role: "model", Tokens: estimateTokens(modelMsg), Hash: hashContent(modelMsg)}

	insertPos := 0
	if len(sess.contents) > 0 {
		insertPos = 1
	}

	result := make([]cachedContent, 0, len(sess.contents)+2)
	result = append(result, sess.contents[:insertPos]...)
	result = append(result, userCC, modelCC)
	result = append(result, sess.contents[insertPos:]...)
	sess.contents = result
	sess.updatedAt = time.Now()
}

// save persists the session cache to disk.
func (sess *session) save(baseDir string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	dir := filepath.Join(baseDir, sess.convID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	raw := make([]interface{}, len(sess.contents))
	for i, cc := range sess.contents {
		raw[i] = map[string]interface{}{
			"content":     cc.Content,
			"role":        cc.Role,
			"tokens":      cc.Tokens,
			"hash":        cc.Hash,
			"compressed":  cc.Compressed,
			"orig_tokens": cc.OrigTokens,
		}
	}

	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "cache.json"), b, 0o644)
}

// writeShadow writes evicted contents to a shadow markdown file.
func writeShadow(baseDir, convID string, evicted []map[string]interface{}, batchNum int) error {
	if len(evicted) == 0 {
		return nil
	}
	dir := filepath.Join(baseDir, convID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	filename := filepath.Join(dir, fmt.Sprintf("shadow-%03d.md", batchNum))
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Shadow Eviction Batch %d\n\n", batchNum))
	b.WriteString(fmt.Sprintf("Evicted %d contents at %s\n\n", len(evicted), time.Now().UTC().Format(time.RFC3339)))

	for i, content := range evicted {
		role, _ := content["role"].(string)
		b.WriteString(fmt.Sprintf("## Content %d [%s]\n\n", i+1, role))

		parts, _ := content["parts"].([]interface{})
		for _, raw := range parts {
			part, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				if len(text) > 500 {
					b.WriteString(text[:500])
					b.WriteString("\n...(truncated)\n\n")
				} else {
					b.WriteString(text)
					b.WriteString("\n\n")
				}
			} else if fc, ok := part["functionCall"].(map[string]interface{}); ok {
				name, _ := fc["name"].(string)
				b.WriteString(fmt.Sprintf("**functionCall:** `%s`\n\n", name))
			} else if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
				name, _ := fr["name"].(string)
				b.WriteString(fmt.Sprintf("**functionResponse:** `%s`\n\n", name))
			}
		}
		b.WriteString("---\n\n")
	}

	return os.WriteFile(filename, []byte(b.String()), 0o644)
}

// extractContentText extracts human-readable text from a Gemini Content for summarization.
func extractContentText(content map[string]interface{}) string {
	role, _ := content["role"].(string)
	parts, _ := content["parts"].([]interface{})
	var texts []string
	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if text, ok := part["text"].(string); ok {
			texts = append(texts, text)
		} else if fc, ok := part["functionCall"].(map[string]interface{}); ok {
			name, _ := fc["name"].(string)
			texts = append(texts, fmt.Sprintf("[call %s]", name))
		} else if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
			name, _ := fr["name"].(string)
			texts = append(texts, fmt.Sprintf("[response %s]", name))
		}
	}
	if role != "" && len(texts) > 0 {
		return role + ": " + strings.Join(texts, " ")
	}
	return strings.Join(texts, " ")
}
