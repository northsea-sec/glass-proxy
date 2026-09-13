package glass

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

const compressTrimMarker = "[...compressed "

// CompressionOpts controls what gets compressed in old messages.
type CompressionOpts struct {
	MaxToolResultChars int // truncate tool_result content to this many chars (0 = no limit)
	MaxAssistantChars  int // truncate old assistant text blocks (0 = no limit)
}

// DefaultCompressionOpts returns production-tuned defaults based on MITM research.
func DefaultCompressionOpts() CompressionOpts {
	return CompressionOpts{
		MaxToolResultChars: 700,
		MaxAssistantChars:  500,
	}
}

// CompressOldMessages applies selective, idempotent compression to messages
// before the watermark position. Thinking blocks are already stripped at ingestion.
// This truncates tool_result content and (optionally) assistant text blocks,
// preserving the conversation thread while dramatically reducing token count.
//
// Returns (tokens_saved, messages_compressed).
// Safe to call repeatedly — already-compressed content is left unchanged.
func (lc *LocalCache) CompressOldMessages(watermark int, opts CompressionOpts) (int, int) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if watermark <= 0 || watermark > len(lc.messages) {
		return 0, 0
	}

	totalSaved := 0
	compressed := 0

	for i := 0; i < watermark; i++ {
		cm := &lc.messages[i]
		if cm.IsReference || cm.IsCompressed {
			continue
		}

		saved := lc.compressMessageLocked(cm, opts)
		if saved > 0 {
			totalSaved += saved
			compressed++
		}
	}

	if compressed > 0 {
		log.Printf("[COMPRESS] conv=%s compressed %d messages before watermark %d (~%d tokens saved)",
			lc.convID, compressed, watermark, totalSaved)
	}

	return totalSaved, compressed
}

// compressMessageLocked applies in-place compression to a single message.
// Returns tokens saved. Idempotent — already-trimmed content is unchanged.
func (lc *LocalCache) compressMessageLocked(cm *CachedMsg, opts CompressionOpts) int {
	if cm.Msg == nil {
		return 0
	}

	beforeTokens := cm.Tokens
	modified := false

	role, _ := cm.Msg["role"].(string)
	content, ok := cm.Msg["content"].([]interface{})
	if !ok {
		// String content (simple assistant/user text)
		if s, ok := cm.Msg["content"].(string); ok && role == "assistant" && opts.MaxAssistantChars > 0 {
			if trimmed, did := compressTruncate(s, opts.MaxAssistantChars); did {
				cm.Msg["content"] = trimmed
				modified = true
			}
		}
		if modified {
			cm.OrigTokens = beforeTokens
			cm.Tokens = estimateMessageTokens(cm.Msg)
			cm.IsCompressed = true
			cm.Hash = hashMessage(cm.Msg)
			return beforeTokens - cm.Tokens
		}
		return 0
	}

	newContent := make([]interface{}, 0, len(content))
	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			newContent = append(newContent, raw)
			continue
		}

		btype, _ := block["type"].(string)

		switch {
		case btype == "tool_result" && opts.MaxToolResultChars > 0:
			result, changed := compressToolResult(block, opts.MaxToolResultChars)
			newContent = append(newContent, result)
			if changed {
				modified = true
			}

		case btype == "text" && role == "assistant" && opts.MaxAssistantChars > 0:
			txt, _ := block["text"].(string)
			if trimmed, did := compressTruncate(txt, opts.MaxAssistantChars); did {
				newBlock := copyBlock(block)
				newBlock["text"] = trimmed
				newContent = append(newContent, newBlock)
				modified = true
			} else {
				newContent = append(newContent, block)
			}

		default:
			newContent = append(newContent, block)
		}
	}

	if modified {
		cm.Msg["content"] = newContent
		cm.OrigTokens = beforeTokens
		cm.Tokens = estimateMessageTokens(cm.Msg)
		cm.IsCompressed = true
		cm.Hash = hashMessage(cm.Msg)
		return beforeTokens - cm.Tokens
	}

	return 0
}

// compressToolResult truncates tool_result content idempotently.
func compressToolResult(block map[string]interface{}, maxChars int) (map[string]interface{}, bool) {
	inner := block["content"]
	switch c := inner.(type) {
	case string:
		if trimmed, did := compressTruncate(c, maxChars); did {
			out := copyBlock(block)
			out["content"] = trimmed
			return out, true
		}
	case []interface{}:
		newInner := make([]interface{}, 0, len(c))
		changed := false
		for _, sub := range c {
			subBlock, ok := sub.(map[string]interface{})
			if ok && subBlock["type"] == "text" {
				txt, _ := subBlock["text"].(string)
				if trimmed, did := compressTruncate(txt, maxChars); did {
					nb := copyBlock(subBlock)
					nb["text"] = trimmed
					newInner = append(newInner, nb)
					changed = true
					continue
				}
			}
			newInner = append(newInner, sub)
		}
		if changed {
			out := copyBlock(block)
			out["content"] = newInner
			return out, true
		}
	}
	return block, false
}

// compressTruncate truncates text to maxChars (2/3 head + 1/3 tail).
// Idempotent — if already trimmed, returns (original, false).
func compressTruncate(text string, maxChars int) (string, bool) {
	if len(text) <= maxChars {
		return text, false
	}
	if strings.Contains(text, compressTrimMarker) {
		return text, false // already compressed
	}
	head := maxChars * 2 / 3
	tail := maxChars / 3
	cut := len(text) - head - tail
	return text[:head] + fmt.Sprintf("\n[...compressed %d chars...]\n", cut) + text[len(text)-tail:], true
}

// InformationLossRatio returns the fraction of original tokens that have been
// compressed. 0.0 = nothing compressed, 0.85 = 85% of original info compressed.
func (lc *LocalCache) InformationLossRatio() float64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	var origCompressed, origTotal int
	for _, cm := range lc.messages {
		if cm.IsReference {
			continue
		}
		if cm.IsCompressed && cm.OrigTokens > 0 {
			origCompressed += cm.OrigTokens
			origTotal += cm.OrigTokens
		} else {
			origTotal += cm.Tokens
		}
	}
	if origTotal == 0 {
		return 0
	}
	return float64(origCompressed) / float64(origTotal)
}

// FlushCompressed removes all compressed messages and injects a summary
// message at position 0 (after any reference stubs). Returns the number
// of messages removed.
func (lc *LocalCache) FlushCompressed(summary string) int {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Separate: references, compressed, and retained (full recent messages)
	var retained []CachedMsg
	removed := 0

	for _, cm := range lc.messages {
		if cm.IsReference {
			retained = append(retained, cm) // keep reference stubs
			continue
		}
		if cm.IsCompressed {
			removed++
			continue
		}
		retained = append(retained, cm)
	}

	if removed == 0 {
		return 0
	}

	// Build summary message
	summaryMsg := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": summary,
			},
		},
	}

	summaryCM := CachedMsg{
		Msg:    summaryMsg,
		Hash:   hashMessage(summaryMsg),
		Role:   "user",
		Tokens: estimateMessageTokens(summaryMsg),
	}

	// Build a bridge assistant message so the retained tail (which starts
	// with a user tool_result) has correct role alternation:
	// summary(user) → bridge(assistant) → retained tail...
	bridgeMsg := map[string]interface{}{
		"role": "assistant",
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "Understood. Continuing from the session summary above.",
			},
		},
	}
	bridgeCM := CachedMsg{
		Msg:    bridgeMsg,
		Hash:   hashMessage(bridgeMsg),
		Role:   "assistant",
		Tokens: estimateMessageTokens(bridgeMsg),
	}

	// Insert summary + bridge after references, before retained messages.
	// Skip leading retained messages that are orphaned tool_results
	// (their matching tool_use was in the compressed zone that got removed).
	var result []CachedMsg
	insertedSummary := false
	skippedOrphans := 0
	for _, cm := range retained {
		if cm.IsReference {
			result = append(result, cm)
			continue
		}
		if !insertedSummary {
			result = append(result, summaryCM)
			result = append(result, bridgeCM)
			insertedSummary = true
		}
		// After inserting summary+bridge, skip boundary orphans whose
		// matching tool_use/tool_result partner was in the flushed zone.
		// A user tool_result is orphan if the preceding message in result
		// is NOT an assistant with a matching tool_use.
		if isOrphanAtBoundary(cm, result) {
			skippedOrphans++
			removed++
			continue
		}
		result = append(result, cm)
	}
	if !insertedSummary {
		result = append(result, summaryCM)
		result = append(result, bridgeCM)
	}

	lc.messages = result

	// Reset breakpoint state (prefix changed entirely)
	lc.breakpointAnchor = 0
	lc.msgsAtLastAnchor = 0
	lc.prevBreakpointAnchor = -1
	lc.lastPrefixHash = ""
	lc.lastPrefixMsgHashes = nil
	lc.maybeAdvanceBreakpoint()

	log.Printf("[COMPRESS] conv=%s saturation flush: removed %d compressed messages, injected summary (%d tokens), %d messages remain",
		lc.convID, removed, summaryCM.Tokens, len(lc.messages))

	return removed
}

// HasCompressedMessages returns true if any messages are compressed.
func (lc *LocalCache) HasCompressedMessages() bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, cm := range lc.messages {
		if cm.IsCompressed {
			return true
		}
	}
	return false
}

// PrefixHash returns the current prefix hash for cache stability checks.
func (lc *LocalCache) PrefixHash() string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.lastPrefixHash
}

// isOrphanAtBoundary checks if a message would be orphaned in the context
// of the result slice built so far. A user tool_result is orphan if the
// last message in result is not an assistant with a matching tool_use.
// An assistant is orphan if the last message in result is also assistant
// (consecutive assistants).
func isOrphanAtBoundary(cm CachedMsg, result []CachedMsg) bool {
	if len(result) == 0 {
		return false
	}
	last := result[len(result)-1]

	// Consecutive same role = orphan
	if cm.Role == last.Role {
		return true
	}

	// User tool_result whose tool_use_ids do not match the preceding assistant
	if cm.Role == "user" && hasOnlyToolResults(cm.Msg) {
		if last.Role != "assistant" {
			return true
		}
		// Check if the tool_result IDs match the assistant tool_use IDs
		resultIDs := collectOrderedToolResultIDs(cm.Msg["content"].([]interface{}))
		prevUses := collectToolUseSet(last.Msg["content"])
		for _, id := range resultIDs {
			if !prevUses[id] {
				return true // at least one orphan
			}
		}
	}
	return false
}

// hasOnlyToolResults checks if a message contains only tool_result blocks.
func hasOnlyToolResults(msg map[string]interface{}) bool {
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) == 0 {
		return false
	}
	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			return false
		}
		if tp, _ := block["type"].(string); tp != "tool_result" {
			return false
		}
	}
	return true
}

// copyBlock shallow-copies a map[string]interface{}.
func copyBlock(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// hashMessage computes a content hash for a message.
func hashMessage(msg map[string]interface{}) string {
	data, _ := json.Marshal(msg)
	// Use first 12 hex chars of a simple hash
	h := uint64(0)
	for _, b := range data {
		h = h*31 + uint64(b)
	}
	return fmt.Sprintf("%012x", h)
}
