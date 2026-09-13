package glass

import (
	"log"
)

// stripThinkingFromMsg removes thinking/redacted_thinking blocks from a single
// assistant message. Called at ingestion time so the cache stores clean messages,
// making build output deterministic regardless of what CC sends.
func stripThinkingFromMsg(msg map[string]interface{}) {
	if msg == nil {
		return
	}
	if role, _ := msg["role"].(string); role != "assistant" {
		return
	}
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) == 0 {
		return
	}

	nonThinking := 0
	for _, b := range content {
		block, ok := b.(map[string]interface{})
		if !ok {
			nonThinking++
			continue
		}
		btype, _ := block["type"].(string)
		if btype != "thinking" && btype != "redacted_thinking" {
			nonThinking++
		}
	}
	if nonThinking == 0 {
		return // don't strip if it would leave an empty message
	}

	filtered := make([]interface{}, 0, len(content))
	for _, b := range content {
		block, ok := b.(map[string]interface{})
		if !ok {
			filtered = append(filtered, b)
			continue
		}
		btype, _ := block["type"].(string)
		if btype == "thinking" || btype == "redacted_thinking" {
			continue
		}
		filtered = append(filtered, b)
	}
	if len(filtered) != len(content) {
		msg["content"] = filtered
	}
}

// stripThinkingBlocks removes "thinking" and "redacted_thinking" content blocks
// from assistant messages when doing so still leaves visible content.
//
// This keeps the serialized request stable across turns: the same assistant
// message is rendered the same way whether or not it is currently the tail.
// Messages that consist only of thinking are left intact so we do not emit an
// invalid empty assistant message.
func stripThinkingBlocks(body map[string]interface{}) int {
	msgs, ok := body["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		return 0
	}

	stripped := 0
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}

		nonThinking := 0
		for _, b := range content {
			block, ok := b.(map[string]interface{})
			if !ok {
				nonThinking++
				continue
			}
			btype, _ := block["type"].(string)
			if btype != "thinking" && btype != "redacted_thinking" {
				nonThinking++
			}
		}
		if nonThinking == 0 {
			continue
		}

		// Filter out thinking blocks while preserving at least one visible block.
		filtered := make([]interface{}, 0, len(content))
		for _, b := range content {
			block, ok := b.(map[string]interface{})
			if !ok {
				filtered = append(filtered, b)
				continue
			}
			btype, _ := block["type"].(string)
			if btype == "thinking" || btype == "redacted_thinking" {
				stripped++
				continue
			}
			filtered = append(filtered, b)
		}

		if len(filtered) != len(content) {
			msg["content"] = filtered
		}
	}

	if stripped > 0 {
		log.Printf("[GLASS] Stripped %d thinking blocks from assistant messages", stripped)
	}
	return stripped
}
