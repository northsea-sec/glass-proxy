// Package trimmer ports context_trimmer.py to Go.
// Strips MCP tools, trims old messages, drops messages when context too large,
// injects cache_control breakpoints — all with watermark-batched boundaries.
package trimmer

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"proxy.local/app/internal/config"
)

const (
	charsPerToken = 3
	trimMarker    = "[...trimmed "
	maxCacheCtl   = 4 // Anthropic limit
	wmMaxAge      = 24 * time.Hour
)

// Watermark tracks the committed trim boundary per conversation.
type watermarkEntry struct {
	Idx int       `json:"idx"`
	Ts  time.Time `json:"ts"`
}

// Trimmer processes Anthropic API request bodies in-place.
type Trimmer struct {
	mu         sync.Mutex
	watermarks map[string]watermarkEntry
	lastInput  map[string]int // conv fingerprint -> last API input_tokens
	stats      Stats
	cacheTTL   string           // detected cache_control TTL (e.g. "1h" for Claude Max)
	pDrops     *PersistentDrops // tracks dropped message hashes for re-drop
}

// Stats tracks trimmer activity.
type Stats struct {
	TotalTrimmed  int                 `json:"total_trimmed"`
	TotalDropped  int                 `json:"total_dropped"`
	MCPServers    map[string][]string `json:"mcp_servers"`
	BuiltinTools  []string            `json:"builtin_tools"`
	ToolsStripped int                 `json:"tools_stripped"`
}

// New creates a Trimmer with empty state.
func New() *Trimmer {
	return &Trimmer{
		watermarks: make(map[string]watermarkEntry),
		lastInput:  make(map[string]int),
		stats: Stats{
			MCPServers: make(map[string][]string),
		},
		pDrops: NewPersistentDrops(),
	}
}

// SetCacheTTL sets the detected cache_control TTL from upstream requests.
// Must be called BEFORE Process() so breakpoints use the correct TTL.
func (t *Trimmer) SetCacheTTL(ttl string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cacheTTL = ttl
}

// SetLastInputTokens records actual API input tokens from a response (for drop decisions).
func (t *Trimmer) SetLastInputTokens(convID string, tokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastInput[convID] = tokens
}

// ApplyThinkingTransform strips or replaces assistant thinking blocks in-place
// without applying the rest of the trimming pipeline.
func ApplyThinkingTransform(body map[string]interface{}, cfg config.Config) int {
	if !cfg.StripOldThinking {
		return 0
	}
	rawMsgs, ok := body["messages"].([]interface{})
	if !ok {
		return 0
	}
	changed := 0
	for _, m := range rawMsgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		blocks, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		newContent := make([]interface{}, 0, len(blocks))
		localChanged := 0
		for _, block := range blocks {
			bm, ok := block.(map[string]interface{})
			if !ok {
				newContent = append(newContent, block)
				continue
			}
			btype, _ := bm["type"].(string)
			if btype == "thinking" {
				if cfg.ThinkingReplaceText != "" {
					bm["text"] = cfg.ThinkingReplaceText
					newContent = append(newContent, bm)
				}
				localChanged++
				continue
			}
			newContent = append(newContent, bm)
		}
		if localChanged > 0 {
			msg["content"] = newContent
			changed += localChanged
		}
	}
	return changed
}

// Process applies all trimming stages to a parsed API request body.
// Returns estimated tokens saved.
func (t *Trimmer) Process(body map[string]interface{}, cfg config.Config) int {
	if !cfg.Enabled {
		return 0
	}

	saved := 0

	// Stage 0: Strip MCP tools
	if cfg.StripMCPTools {
		saved += t.stripMCPTools(body, cfg)
	}

	// Stage 1: Trim old messages
	if cfg.TrimMessages {
		saved += t.trimMessages(body, cfg)
	}

	// Stage 2: Drop old messages if still too large
	saved += t.dropOldMessages(body, cfg)

	// Stage 3: Orphan sanitizer — fix structural violations created by trim/drop.
	// Must run AFTER all trimming: tool_result without matching tool_use,
	// non-user msg[0], consecutive same-role messages.
	orphanFixed := FixOrphanToolResults(body)
	if orphanFixed > 0 {
		log.Printf("[TRIM] Orphan sanitizer fixed %d issues", orphanFixed)
	}

	return saved
}

func estimateTokens(obj interface{}) int {
	data, _ := json.Marshal(obj)
	return len(data) / charsPerToken
}

// --- Stage 0: Strip MCP tools ---

func (t *Trimmer) stripMCPTools(body map[string]interface{}, cfg config.Config) int {
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		return 0
	}

	disabled := make(map[string]bool)
	for _, d := range cfg.MCPDisabled {
		disabled[d] = true
	}

	kept := make([]interface{}, 0, len(tools))
	stripped := 0

	for _, tool := range tools {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			kept = append(kept, tool)
			continue
		}
		name, _ := tm["name"].(string)
		if strings.HasPrefix(name, "mcp__") {
			parts := strings.SplitN(name, "__", 3)
			server := ""
			if len(parts) >= 3 {
				server = parts[1]
			}
			if len(disabled) > 0 && disabled[server] {
				stripped++
				continue
			}
		}
		kept = append(kept, tool)
	}

	if stripped > 0 {
		body["tools"] = kept
		t.stats.ToolsStripped += stripped
	}
	return stripped * 200 // rough estimate per tool schema
}

// --- Stage 1: Trim old messages with watermark batching ---

func (t *Trimmer) trimMessages(body map[string]interface{}, cfg config.Config) int {
	msgs, ok := body["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		return 0
	}

	totalEst := estimateTokens(body)
	if totalEst < cfg.TrimThresholdTokens {
		return 0
	}

	keepRecent := cfg.TrimKeepRecent
	batchSize := cfg.TrimBatchSize
	if len(msgs) <= keepRecent {
		return 0
	}

	oldEnd := len(msgs) - keepRecent
	convID := ConvFingerprint(body)

	t.mu.Lock()
	wm := t.watermarks[convID]
	watermark := wm.Idx

	var effectiveEnd int
	if oldEnd <= watermark {
		effectiveEnd = watermark
	} else if oldEnd-watermark >= batchSize {
		effectiveEnd = oldEnd
		t.watermarks[convID] = watermarkEntry{Idx: oldEnd, Ts: time.Now()}
		log.Printf("[TRIM] Watermark advanced: %d -> %d (batch %d, conv=%s)",
			watermark, oldEnd, oldEnd-watermark, convID)
	} else {
		effectiveEnd = watermark
	}
	t.mu.Unlock()

	if effectiveEnd <= 0 {
		return 0
	}
	if effectiveEnd > len(msgs) {
		effectiveEnd = len(msgs)
	}

	tokensBefore := estimateTokens(msgs[:effectiveEnd])

	for i := 0; i < effectiveEnd; i++ {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		t.trimMessage(msg, cfg)
	}

	tokensAfter := estimateTokens(msgs[:effectiveEnd])
	saved := tokensBefore - tokensAfter
	if saved < 0 {
		saved = 0
	}

	// Cache breakpoint injection at watermark boundary
	if cfg.InjectWatermarkBreakpoint && effectiveEnd < len(msgs) {
		t.injectBreakpoint(body, effectiveEnd)
	}

	t.stats.TotalTrimmed += saved
	return saved
}

func (t *Trimmer) trimMessage(msg map[string]interface{}, cfg config.Config) {
	content := msg["content"]
	if content == nil {
		return
	}

	role, _ := msg["role"].(string)

	switch c := content.(type) {
	case string:
		if role == "assistant" && len(c) > cfg.TrimMaxAssistantChars && !strings.Contains(c, trimMarker) {
			msg["content"] = truncateText(c, cfg.TrimMaxAssistantChars)
		}
	case []interface{}:
		newContent := make([]interface{}, 0, len(c))
		for _, block := range c {
			bm, ok := block.(map[string]interface{})
			if !ok {
				newContent = append(newContent, block)
				continue
			}
			btype, _ := bm["type"].(string)

			// Strip or replace thinking blocks
			if btype == "thinking" && cfg.StripOldThinking {
				if cfg.ThinkingReplaceText != "" {
					// Replace thinking content instead of stripping
					bm["text"] = cfg.ThinkingReplaceText
					newContent = append(newContent, bm)
				}
				// else: strip (continue without appending)
				continue
			}

			// Trim tool_result
			if btype == "tool_result" {
				trimContentBlock(bm, cfg.TrimMaxToolResultChars)
				newContent = append(newContent, bm)
				continue
			}

			// Trim assistant text
			if btype == "text" && role == "assistant" {
				trimContentBlock(bm, cfg.TrimMaxAssistantChars)
				newContent = append(newContent, bm)
				continue
			}

			newContent = append(newContent, bm)
		}
		msg["content"] = newContent
	}
}

func truncateText(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	if strings.Contains(text, trimMarker) {
		return text // idempotent
	}
	head := maxChars * 2 / 3
	tail := maxChars / 3
	cut := len(text) - head - tail
	return text[:head] + fmt.Sprintf("\n[...trimmed %d chars...]\n", cut) + text[len(text)-tail:]
}

func trimContentBlock(block map[string]interface{}, maxChars int) {
	if text, ok := block["text"].(string); ok {
		if len(text) > maxChars && !strings.Contains(text, trimMarker) {
			block["text"] = truncateText(text, maxChars)
		}
		return
	}
	// tool_result with string content
	if content, ok := block["content"].(string); ok {
		if len(content) > maxChars && !strings.Contains(content, trimMarker) {
			block["content"] = truncateText(content, maxChars)
		}
		return
	}
	// tool_result with array content (each sub-block may have "text")
	if contentArr, ok := block["content"].([]interface{}); ok {
		for _, sub := range contentArr {
			sm, ok := sub.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := sm["text"].(string); ok {
				if len(text) > maxChars && !strings.Contains(text, trimMarker) {
					sm["text"] = truncateText(text, maxChars)
				}
			}
		}
	}
}

// --- Cache breakpoint injection ---

func countCacheControlBlocks(body map[string]interface{}) int {
	count := 0
	// system blocks
	if sys, ok := body["system"].([]interface{}); ok {
		for _, s := range sys {
			if sm, ok := s.(map[string]interface{}); ok {
				if _, has := sm["cache_control"]; has {
					count++
				}
			}
		}
	}
	// tools
	if tools, ok := body["tools"].([]interface{}); ok {
		for _, tool := range tools {
			if tm, ok := tool.(map[string]interface{}); ok {
				if _, has := tm["cache_control"]; has {
					count++
				}
			}
		}
	}
	// messages
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, msg := range msgs {
			mm, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			if content, ok := mm["content"].([]interface{}); ok {
				for _, blk := range content {
					if bm, ok := blk.(map[string]interface{}); ok {
						if _, has := bm["cache_control"]; has {
							count++
						}
					}
				}
			}
		}
	}
	return count
}

func (t *Trimmer) injectBreakpoint(body map[string]interface{}, msgIdx int) {
	msgs, ok := body["messages"].([]interface{})
	if !ok || msgIdx < 0 || msgIdx >= len(msgs) {
		return
	}

	existing := countCacheControlBlocks(body)
	if existing >= maxCacheCtl {
		log.Printf("[TRIM] SKIP breakpoint: already %d cache_control blocks (limit=%d)", existing, maxCacheCtl)
		return
	}

	msg, ok := msgs[msgIdx].(map[string]interface{})
	if !ok {
		return
	}

	ccBlock := map[string]interface{}{"type": "ephemeral"}
	t.mu.Lock()
	ttl := t.cacheTTL
	t.mu.Unlock()
	if ttl != "" {
		ccBlock["ttl"] = ttl
	}

	content := msg["content"]
	switch c := content.(type) {
	case []interface{}:
		// Find last non-thinking block
		for i := len(c) - 1; i >= 0; i-- {
			bm, ok := c[i].(map[string]interface{})
			if !ok {
				continue
			}
			btype, _ := bm["type"].(string)
			if btype == "thinking" || btype == "redacted_thinking" {
				continue
			}
			if _, has := bm["cache_control"]; !has {
				bm["cache_control"] = ccBlock
				log.Printf("[TRIM] Injected cache_control breakpoint at msg[%d]", msgIdx)
			}
			break
		}
	case string:
		if strings.TrimSpace(c) != "" {
			msg["content"] = []interface{}{
				map[string]interface{}{
					"type":          "text",
					"text":          c,
					"cache_control": ccBlock,
				},
			}
			log.Printf("[TRIM] Injected cache_control breakpoint at msg[%d] (string->block)", msgIdx)
		}
	}
}

// Stage 2 (dropOldMessages) is in stage2.go
