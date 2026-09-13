// orphan.go — Orphan tool_result sanitizer.
// Ported from context_trimmer.py _fix_orphan_tool_results.
//
// After message dropping/trimming, structural violations can occur:
//   - tool_result blocks with no matching tool_use in the preceding assistant message
//   - First message is not role:user (Anthropic API rejects this)
//   - Consecutive same-role messages (API requirement: strict alternation)
//
// Each fix can create new violations (dropping a tool_result msg can expose a
// new non-user msg[0]), so this runs iteratively up to maxIterations.
package trimmer

import "log"

const maxOrphanIterations = 50
const minOrphanMessages = 5

// FixOrphanToolResults is the post-trim sanitizer. Runs AFTER all trimming/dropping.
// Returns count of issues fixed.
func FixOrphanToolResults(body map[string]interface{}) int {
	rawMsgs, ok := body["messages"]
	if !ok {
		return 0
	}
	msgs, ok := rawMsgs.([]interface{})
	if !ok || len(msgs) == 0 {
		return 0
	}

	fixed := 0

	for iteration := 0; iteration < maxOrphanIterations; iteration++ {
		if len(msgs) <= minOrphanMessages {
			break
		}
		changedThisPass := false

		// Pass 1: First message must be role:user
		for len(msgs) > minOrphanMessages {
			first, ok := msgs[0].(map[string]interface{})
			if !ok {
				break
			}
			role, _ := first["role"].(string)
			if role == "user" {
				break
			}
			msgs = msgs[1:]
			fixed++
			changedThisPass = true
			log.Printf("[TRIM] Orphan sanitizer: dropped non-user msg[0] (role=%s)", role)
		}
		if len(msgs) == 0 {
			break
		}

		// Pass 2: Strip orphan tool_result blocks (adjacency check)
		keep := make([]interface{}, 0, len(msgs))
		for i, rawMsg := range msgs {
			m, ok := rawMsg.(map[string]interface{})
			if !ok {
				keep = append(keep, rawMsg)
				continue
			}
			role, _ := m["role"].(string)
			if role != "user" {
				keep = append(keep, rawMsg)
				continue
			}
			content, ok := m["content"].([]interface{})
			if !ok {
				keep = append(keep, rawMsg)
				continue
			}

			// Check if this user message has any tool_result blocks
			hasToolResult := false
			for _, blk := range content {
				bm, ok := blk.(map[string]interface{})
				if !ok {
					continue
				}
				if btype, _ := bm["type"].(string); btype == "tool_result" {
					hasToolResult = true
					break
				}
			}
			if !hasToolResult {
				keep = append(keep, rawMsg)
				continue
			}

			// Collect tool_use IDs from the immediately preceding assistant message
			prevToolIDs := make(map[string]bool)
			if i > 0 {
				prevMsg, ok := msgs[i-1].(map[string]interface{})
				if ok {
					prevRole, _ := prevMsg["role"].(string)
					if prevRole == "assistant" {
						if prevContent, ok := prevMsg["content"].([]interface{}); ok {
							for _, blk := range prevContent {
								bm, ok := blk.(map[string]interface{})
								if !ok {
									continue
								}
								if btype, _ := bm["type"].(string); btype == "tool_use" {
									if id, _ := bm["id"].(string); id != "" {
										prevToolIDs[id] = true
									}
								}
							}
						}
					}
				}
			}

			// Strip tool_result blocks whose tool_use_id is not in prevToolIDs
			cleaned := make([]interface{}, 0, len(content))
			strippedCount := 0
			for _, blk := range content {
				bm, ok := blk.(map[string]interface{})
				if !ok {
					cleaned = append(cleaned, blk)
					continue
				}
				btype, _ := bm["type"].(string)
				if btype == "tool_result" {
					tuID, _ := bm["tool_use_id"].(string)
					if !prevToolIDs[tuID] {
						strippedCount++
						continue // orphan — drop it
					}
				}
				cleaned = append(cleaned, blk)
			}

			if strippedCount > 0 {
				changedThisPass = true
				fixed += strippedCount
				if len(cleaned) > 0 {
					m["content"] = cleaned
					keep = append(keep, rawMsg)
					log.Printf("[TRIM] Stripped %d orphan tool_result(s) from msg[%d]", strippedCount, i)
				} else {
					log.Printf("[TRIM] Dropped msg[%d] (only orphan tool_results)", i)
				}
				continue
			}

			keep = append(keep, rawMsg)
		}
		msgs = keep

		// Pass 3: Strip DANGLING tool_use blocks (tool_use with no matching tool_result in next msg)
		// API RULE: if assistant msg has tool_use, the NEXT msg MUST have tool_result for each.
		// Without this, the API returns 400 "tool use concurrency" error.
		keep3 := make([]interface{}, 0, len(msgs))
		for i, rawMsg := range msgs {
			m, ok := rawMsg.(map[string]interface{})
			if !ok {
				keep3 = append(keep3, rawMsg)
				continue
			}
			role, _ := m["role"].(string)
			if role != "assistant" {
				keep3 = append(keep3, rawMsg)
				continue
			}
			content, ok := m["content"].([]interface{})
			if !ok {
				keep3 = append(keep3, rawMsg)
				continue
			}
			// Collect tool_use IDs from this assistant message
			var toolUseIDs []string
			for _, blk := range content {
				bm, ok := blk.(map[string]interface{})
				if !ok {
					continue
				}
				if btype, _ := bm["type"].(string); btype == "tool_use" {
					if id, _ := bm["id"].(string); id != "" {
						toolUseIDs = append(toolUseIDs, id)
					}
				}
			}
			if len(toolUseIDs) == 0 {
				keep3 = append(keep3, rawMsg)
				continue
			}
			// Skip the last message — CC expects to provide tool_results for it
			if i == len(msgs)-1 {
				keep3 = append(keep3, rawMsg)
				continue
			}
			// Collect tool_result IDs from the NEXT message
			nextResultIDs := make(map[string]bool)
			if i+1 < len(msgs) {
				nextMsg, ok := msgs[i+1].(map[string]interface{})
				if ok {
					nextRole, _ := nextMsg["role"].(string)
					if nextRole == "user" {
						if nextContent, ok := nextMsg["content"].([]interface{}); ok {
							for _, blk := range nextContent {
								bm, ok := blk.(map[string]interface{})
								if !ok {
									continue
								}
								if btype, _ := bm["type"].(string); btype == "tool_result" {
									if tuID, _ := bm["tool_use_id"].(string); tuID != "" {
										nextResultIDs[tuID] = true
									}
								}
							}
						}
					}
				}
			}
			// Strip tool_use blocks with no matching tool_result
			var dangling int
			cleaned := make([]interface{}, 0, len(content))
			for _, blk := range content {
				bm, ok := blk.(map[string]interface{})
				if !ok {
					cleaned = append(cleaned, blk)
					continue
				}
				if btype, _ := bm["type"].(string); btype == "tool_use" {
					if id, _ := bm["id"].(string); !nextResultIDs[id] {
						dangling++
						continue // dangling — drop it
					}
				}
				cleaned = append(cleaned, blk)
			}
			if dangling > 0 {
				changedThisPass = true
				fixed += dangling
				if len(cleaned) > 0 {
					m["content"] = cleaned
					keep3 = append(keep3, rawMsg)
					log.Printf("[TRIM] Stripped %d dangling tool_use(s) from msg[%d]", dangling, i)
				} else {
					log.Printf("[TRIM] Dropped msg[%d] (only dangling tool_uses)", i)
				}
				continue
			}
			keep3 = append(keep3, rawMsg)
		}
		if len(keep3) != len(msgs) {
			msgs = keep3
		}

		// Pass 4: Remove consecutive same-role messages (API requirement: strict alternation)
		if len(msgs) >= 2 {
			deduped := make([]interface{}, 0, len(msgs))
			deduped = append(deduped, msgs[0])
			for _, rawMsg := range msgs[1:] {
				m, ok := rawMsg.(map[string]interface{})
				if !ok {
					deduped = append(deduped, rawMsg)
					continue
				}
				role, _ := m["role"].(string)
				lastMsg, ok := deduped[len(deduped)-1].(map[string]interface{})
				if !ok {
					deduped = append(deduped, rawMsg)
					continue
				}
				lastRole, _ := lastMsg["role"].(string)
				if role == lastRole {
					changedThisPass = true
					fixed++
					log.Printf("[TRIM] Orphan sanitizer: dropped consecutive %s message", role)
					continue
				}
				deduped = append(deduped, rawMsg)
			}
			msgs = deduped
		}

		if !changedThisPass {
			break
		}
	}

	// Final safety: if msg[0] is still non-user after all passes, force-drop
	// leading non-user messages. A non-user msg[0] is a guaranteed 400 error,
	// which is worse than a shorter conversation.
	for len(msgs) > 1 {
		first, ok := msgs[0].(map[string]interface{})
		if !ok {
			break
		}
		if role, _ := first["role"].(string); role == "user" {
			break
		}
		msgs = msgs[1:]
		fixed++
		log.Printf("[TRIM] Orphan sanitizer: force-dropped non-user msg[0] (below minOrphanMessages)")
	}

	// Final safety: if last msg is not user, drop trailing non-user messages.
	// Exception: a pure-thinking assistant (all content blocks are type "thinking"
	// or "redacted_thinking") must be preserved — stripping its thinking would
	// leave it with empty content, which is worse than a trailing assistant.
	for len(msgs) > 1 {
		last, ok := msgs[len(msgs)-1].(map[string]interface{})
		if !ok {
			break
		}
		if role, _ := last["role"].(string); role == "user" {
			break
		}
		if isPureThinkingMessage(last) {
			break
		}
		msgs = msgs[:len(msgs)-1]
		fixed++
		log.Printf("[TRIM] Orphan sanitizer: force-dropped non-user last msg")
	}

	if fixed > 0 {
		body["messages"] = msgs
		log.Printf("[TRIM] Orphan sanitizer: fixed %d issues total", fixed)
	}
	return fixed
}

// isPureThinkingMessage reports whether every content block in msg is of type
// "thinking" or "redacted_thinking". Such messages must not be force-dropped
// because removing their content would leave an empty assistant turn.
func isPureThinkingMessage(msg map[string]interface{}) bool {
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) == 0 {
		return false
	}
	for _, blk := range content {
		block, ok := blk.(map[string]interface{})
		if !ok {
			return false
		}
		t, _ := block["type"].(string)
		if t != "thinking" && t != "redacted_thinking" {
			return false
		}
	}
	return true
}
