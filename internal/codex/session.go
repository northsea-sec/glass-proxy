package codex

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

// cachedItem is a single Responses API input item stored in the session cache.
type cachedItem struct {
	Item       map[string]interface{} // original item (immutable after ingest)
	Type       string                 // "message", "function_call", "function_call_output", "reasoning"
	Role       string                 // "user", "assistant", "developer", "" for non-message
	Tokens     int                    // estimated token count
	Hash       string                 // SHA-256 of JSON, first 16 hex chars
	Compressed bool                   // true if content was truncated
	OrigTokens int                    // pre-compression token count (0 if never compressed)
}

// session holds per-conversation state for a Codex lane session.
type session struct {
	mu              sync.Mutex
	items           []cachedItem
	convID          string
	model           string
	requestURI      string
	requestTemplate map[string]interface{}
	lastAPIInput    int // last real input_tokens from upstream
	lastAPIInputAt  time.Time
	evictedCount    int // total items evicted across all batches
	batchCount      int // number of eviction batches
	compWatermark   int // compression watermark position
	createdAt       time.Time
	updatedAt       time.Time
}

// sessionStore manages per-conversation sessions.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	baseDir  string // shadow base dir
}

type LaneSessionSnapshot struct {
	ConvID               string `json:"conv_id"`
	Model                string `json:"model"`
	ItemCount            int    `json:"item_count"`
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
	LiveInput             []interface{}          `json:"live_input"`
	Snapshot              LaneSessionSnapshot    `json:"snapshot"`
	CapturedAuthAvailable bool                   `json:"captured_auth_available"`
	CapturedAuthType      string                 `json:"captured_auth_type"`
}

// newSessionStore creates a sessionStore rooted at the given shadow directory.
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

// get returns the session for convID, creating one if absent.
func (s *sessionStore) get(convID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[convID]
	if !ok {
		now := time.Now()
		sess = &session{
			convID:    convID,
			createdAt: now,
			updatedAt: now,
		}
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

// estimateTokens estimates token count from JSON-serialized size / 4.
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

// hashItem returns SHA-256 of JSON-serialized item, first 16 hex chars.
func hashItem(item map[string]interface{}) string {
	b, err := json.Marshal(item)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)[:16]
}

// deepCopyItem performs a marshal/unmarshal deep copy of an item map.
func deepCopyItem(item map[string]interface{}) map[string]interface{} {
	b, err := json.Marshal(item)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// ingest adds new items from the input array into the session cache.
// Compares by position: only items beyond the current cache length are added.
// Strips cache_control and compaction items. Returns the count of new items added.
func (sess *session) ingest(items []interface{}) int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(items) <= len(sess.items) {
		return 0
	}

	added := 0
	for i := len(sess.items); i < len(items); i++ {
		raw, ok := items[i].(map[string]interface{})
		if !ok {
			continue
		}

		// Strip compaction items — encrypted opaque items from server-side context management.
		itemType, _ := raw["type"].(string)
		if itemType == "compaction" {
			continue
		}

		copied := deepCopyItem(raw)
		if copied == nil {
			continue
		}

		// Strip cache_control from the copied item.
		delete(copied, "cache_control")

		role, _ := copied["role"].(string)

		ci := cachedItem{
			Item:   copied,
			Type:   itemType,
			Role:   role,
			Tokens: estimateTokens(copied),
			Hash:   hashItem(copied),
		}
		sess.items = append(sess.items, ci)
		added++
	}

	sess.updatedAt = time.Now()
	return added
}

// totalTokens returns the sum of estimated tokens across all cached items.
func (sess *session) totalTokens() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	total := 0
	for i := range sess.items {
		total += sess.items[i].Tokens
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
	systemPromptPath := filepath.Join(baseDir, sess.convID, "system_prompt.txt")
	totalTokens := 0
	for _, ci := range sess.items {
		totalTokens += ci.Tokens
	}

	return LaneSessionSnapshot{
		ConvID:               sess.convID,
		Model:                sess.model,
		ItemCount:            len(sess.items),
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
	sess.requestTemplate = deepCopyItem(template)
	sess.updatedAt = time.Now()
}

func (sess *session) replay(baseDir string) LaneSessionReplay {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	template := deepCopyItem(sess.requestTemplate)
	liveInput := make([]interface{}, len(sess.items))
	for i, ci := range sess.items {
		liveInput[i] = deepCopyItem(ci.Item)
	}

	return LaneSessionReplay{
		Lane:          "codex",
		ConvID:        sess.convID,
		RequestURI:    sess.requestURI,
		ProxyTemplate: template,
		LiveInput:     liveInput,
		Snapshot:      sess.snapshotLocked(baseDir),
	}
}

// evict removes the oldest items from the middle of the cache (after the first
// developer/system-equivalent message, before the last keepRecent items) until
// total tokens is at or below targetTokens. Returns evicted items.
func (sess *session) evict(targetTokens, keepRecent int) []map[string]interface{} {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	total := 0
	for i := range sess.items {
		total += sess.items[i].Tokens
	}
	if total <= targetTokens {
		return nil
	}

	// Find the boundary after the first developer/system message.
	evictStart := 0
	for i, ci := range sess.items {
		if ci.Role == "developer" || ci.Role == "system" {
			evictStart = i + 1
			break
		}
	}

	// Protect the last keepRecent items.
	evictEnd := len(sess.items) - keepRecent
	if evictEnd <= evictStart {
		return nil // nothing evictable
	}

	var evicted []map[string]interface{}
	// Evict from evictStart forward until under budget or we hit evictEnd.
	for total > targetTokens && evictStart < evictEnd {
		evicted = append(evicted, sess.items[evictStart].Item)
		total -= sess.items[evictStart].Tokens
		// Remove the item by splicing.
		sess.items = append(sess.items[:evictStart], sess.items[evictStart+1:]...)
		evictEnd-- // the boundary shifted
		sess.evictedCount++
	}

	if len(evicted) > 0 {
		sess.batchCount++
	}

	sess.updatedAt = time.Now()
	return evicted
}

// compressTruncate truncates text to maxChars using 2/3 head + 1/3 tail with
// a compression marker in the middle. Returns the result and whether truncation occurred.
func compressTruncate(text string, maxChars int) (string, bool) {
	if len(text) <= maxChars {
		return text, false
	}
	// Already compressed — idempotency guard.
	if strings.Contains(text, "[...compressed") {
		return text, false
	}

	dropped := len(text) - maxChars
	marker := fmt.Sprintf("[...compressed %d chars...]", dropped)

	// Allocate head/tail from the budget that remains after the marker.
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

// compressOld compresses items before watermark position.
// For function_call_output: truncates the output field.
// For assistant messages: truncates text content entries.
// Idempotent: skips already-compressed items.
// Returns (tokensSaved, itemsCompressed).
func (sess *session) compressOld(watermark int, maxFuncOutputChars, maxAssistantChars int) (int, int) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if watermark > len(sess.items) {
		watermark = len(sess.items)
	}

	tokensSaved := 0
	itemsCompressed := 0

	for i := 0; i < watermark; i++ {
		ci := &sess.items[i]
		if ci.Compressed {
			continue
		}

		changed := false

		switch ci.Type {
		case "function_call_output":
			output, ok := ci.Item["output"].(string)
			if ok {
				result, truncated := compressTruncate(output, maxFuncOutputChars)
				if truncated {
					ci.Item["output"] = result
					changed = true
				}
			}

		case "message":
			if ci.Role == "assistant" {
				changed = compressMessageContent(ci.Item, maxAssistantChars)
			}
		}

		if changed {
			origTokens := ci.Tokens
			ci.OrigTokens = origTokens
			ci.Tokens = estimateTokens(ci.Item)
			ci.Compressed = true
			tokensSaved += origTokens - ci.Tokens
			itemsCompressed++
		}
	}

	sess.updatedAt = time.Now()
	return tokensSaved, itemsCompressed
}

// compressMessageContent truncates text content within a message item.
// Handles both string content and []interface{} content arrays.
func compressMessageContent(item map[string]interface{}, maxChars int) bool {
	content := item["content"]

	switch c := content.(type) {
	case string:
		result, truncated := compressTruncate(c, maxChars)
		if truncated {
			item["content"] = result
			return true
		}
	case []interface{}:
		any := false
		for _, entry := range c {
			block, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			// Only compress text-type content blocks.
			blockType, _ := block["type"].(string)
			if blockType != "input_text" && blockType != "output_text" && blockType != "text" {
				continue
			}
			text, ok := block["text"].(string)
			if !ok {
				continue
			}
			result, truncated := compressTruncate(text, maxChars)
			if truncated {
				block["text"] = result
				any = true
			}
		}
		return any
	}
	return false
}

// buildItems returns all cached items as []interface{} for the request body.
func (sess *session) buildItems() []interface{} {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	out := make([]interface{}, len(sess.items))
	for i, ci := range sess.items {
		out[i] = ci.Item
	}
	return out
}

func (sess *session) appendOutputItems(items []map[string]interface{}) int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	added := 0
	for _, raw := range items {
		copied := deepCopyItem(raw)
		if copied == nil {
			continue
		}
		itemType, _ := copied["type"].(string)
		role, _ := copied["role"].(string)
		if itemType == "" && role != "" && copied["content"] != nil {
			itemType = "message"
		}
		sess.items = append(sess.items, cachedItem{
			Item:   copied,
			Type:   itemType,
			Role:   role,
			Tokens: estimateTokens(copied),
			Hash:   hashItem(copied),
		})
		added++
	}
	if added > 0 {
		sess.updatedAt = time.Now()
	}
	return added
}

// flushCompressed removes all compressed items from the cache and injects a
// summary as a developer message at position 0 (after any existing uncompressed
// developer items). Returns the count of items removed.
func (sess *session) flushCompressed(summary string) int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Remove compressed items.
	kept := make([]cachedItem, 0, len(sess.items))
	removed := 0
	for _, ci := range sess.items {
		if ci.Compressed {
			removed++
		} else {
			kept = append(kept, ci)
		}
	}

	if removed == 0 {
		return 0
	}

	// Build the summary item as a developer message.
	summaryItem := map[string]interface{}{
		"type": "message",
		"role": "developer",
		"content": []interface{}{
			map[string]interface{}{
				"type": "input_text",
				"text": summary,
			},
		},
	}

	summaryCI := cachedItem{
		Item:   summaryItem,
		Type:   "message",
		Role:   "developer",
		Tokens: estimateTokens(summaryItem),
		Hash:   hashItem(summaryItem),
	}

	// Insert after existing developer items at the front.
	insertPos := 0
	for i, ci := range kept {
		if ci.Role == "developer" || ci.Role == "system" {
			insertPos = i + 1
		} else {
			break
		}
	}

	// Splice in the summary item.
	result := make([]cachedItem, 0, len(kept)+1)
	result = append(result, kept[:insertPos]...)
	result = append(result, summaryCI)
	result = append(result, kept[insertPos:]...)
	sess.items = result

	sess.updatedAt = time.Now()
	return removed
}

// save persists the session cache to {baseDir}/{convID}/cache.json.
func (sess *session) save(baseDir string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	dir := filepath.Join(baseDir, sess.convID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Serialize the items slice.
	raw := make([]interface{}, len(sess.items))
	for i, ci := range sess.items {
		raw[i] = map[string]interface{}{
			"item":        ci.Item,
			"type":        ci.Type,
			"role":        ci.Role,
			"tokens":      ci.Tokens,
			"hash":        ci.Hash,
			"compressed":  ci.Compressed,
			"orig_tokens": ci.OrigTokens,
		}
	}

	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	path := filepath.Join(dir, "cache.json")
	return os.WriteFile(path, b, 0o644)
}

// informationLossRatio returns the fraction of original tokens that have been
// compressed away. Returns 0.0 if no compression has occurred.
func (sess *session) informationLossRatio() float64 {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	var origTotal, currentTotal int
	for _, ci := range sess.items {
		if ci.Compressed && ci.OrigTokens > 0 {
			origTotal += ci.OrigTokens
			currentTotal += ci.Tokens
		}
	}
	if origTotal == 0 {
		return 0.0
	}
	return float64(origTotal-currentTotal) / float64(origTotal)
}

// writeShadow writes evicted items to {baseDir}/{convID}/shadow-{batchNum:03d}.md
// in human-readable markdown format.
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
	b.WriteString(fmt.Sprintf("Evicted %d items at %s\n\n", len(evicted), time.Now().UTC().Format(time.RFC3339)))

	for i, item := range evicted {
		itemType, _ := item["type"].(string)
		role, _ := item["role"].(string)

		b.WriteString(fmt.Sprintf("## Item %d", i+1))
		if itemType != "" {
			b.WriteString(fmt.Sprintf(" [%s", itemType))
			if role != "" {
				b.WriteString(fmt.Sprintf("/%s", role))
			}
			b.WriteString("]")
		}
		b.WriteString("\n\n")

		switch itemType {
		case "message":
			writeMessageContent(&b, item)
		case "function_call":
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			b.WriteString(fmt.Sprintf("**Function:** `%s`\n\n", name))
			if len(args) > 500 {
				b.WriteString("```\n")
				b.WriteString(args[:500])
				b.WriteString("\n...(truncated)\n```\n\n")
			} else {
				b.WriteString(fmt.Sprintf("```\n%s\n```\n\n", args))
			}
		case "function_call_output":
			output, _ := item["output"].(string)
			if len(output) > 500 {
				b.WriteString("```\n")
				b.WriteString(output[:500])
				b.WriteString("\n...(truncated)\n```\n\n")
			} else {
				b.WriteString(fmt.Sprintf("```\n%s\n```\n\n", output))
			}
		default:
			raw, _ := json.MarshalIndent(item, "", "  ")
			b.WriteString(fmt.Sprintf("```json\n%s\n```\n\n", string(raw)))
		}

		b.WriteString("---\n\n")
	}

	return os.WriteFile(filename, []byte(b.String()), 0o644)
}

// writeMessageContent formats a message item's content into the markdown builder.
func writeMessageContent(b *strings.Builder, item map[string]interface{}) {
	content := item["content"]
	switch c := content.(type) {
	case string:
		b.WriteString(c)
		b.WriteString("\n\n")
	case []interface{}:
		for _, entry := range c {
			block, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			text, ok := block["text"].(string)
			if ok {
				b.WriteString(text)
				b.WriteString("\n\n")
			}
		}
	}
}
