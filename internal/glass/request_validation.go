package glass

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxFallbackRequestTokens = 190000

// deepCopyMessages clones the full message slice so fallback can restore the
// exact CC-provided structure after Glass mutates its working copy.
func deepCopyMessages(msgs []interface{}) []interface{} {
	data, err := json.Marshal(msgs)
	if err != nil {
		return append([]interface{}(nil), msgs...)
	}
	var copied []interface{}
	if err := json.Unmarshal(data, &copied); err != nil {
		return append([]interface{}(nil), msgs...)
	}
	return copied
}

// validateMessageStructure checks the message sequence rules Anthropic enforces
// for tool-use conversations.
func validateMessageStructure(msgs []interface{}) []string {
	if len(msgs) == 0 {
		return []string{"no messages"}
	}

	var issues []string

	first, ok := msgs[0].(map[string]interface{})
	if !ok {
		return []string{"msg[0] is not an object"}
	}
	if role, _ := first["role"].(string); role != "user" {
		issues = append(issues, fmt.Sprintf("msg[0] must be user, got %q", role))
	}

	for i := 1; i < len(msgs); i++ {
		prev, okPrev := msgs[i-1].(map[string]interface{})
		curr, okCurr := msgs[i].(map[string]interface{})
		if !okPrev || !okCurr {
			continue
		}
		prevRole, _ := prev["role"].(string)
		currRole, _ := curr["role"].(string)
		if prevRole != "" && prevRole == currRole {
			issues = append(issues, fmt.Sprintf("consecutive %s messages at msg[%d]/msg[%d]", currRole, i-1, i))
		}
	}

	for i, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].([]interface{})

		if role == "assistant" {
			toolUseIDs := collectToolUseIDs(content)
			if len(toolUseIDs) == 0 {
				continue
			}
			if i == len(msgs)-1 {
				issues = append(issues, fmt.Sprintf("assistant tool_use at msg[%d] has no following user tool_result", i))
				continue
			}
			nextMsg, ok := msgs[i+1].(map[string]interface{})
			if !ok {
				issues = append(issues, fmt.Sprintf("assistant tool_use at msg[%d] followed by non-object msg[%d]", i, i+1))
				continue
			}
			nextRole, _ := nextMsg["role"].(string)
			if nextRole != "user" {
				issues = append(issues, fmt.Sprintf("assistant tool_use at msg[%d] not followed by user", i))
				continue
			}
			nextResults := collectToolResultIDs(nextMsg["content"])
			var missing []string
			for _, id := range toolUseIDs {
				if !nextResults[id] {
					missing = append(missing, id)
				}
			}
			if len(missing) > 0 {
				issues = append(issues, fmt.Sprintf("assistant tool_use at msg[%d] missing tool_results: %s", i, previewIDs(missing)))
			}
			continue
		}

		if role == "user" {
			resultIDs := collectOrderedToolResultIDs(content)
			if len(resultIDs) == 0 {
				continue
			}
			if i == 0 {
				issues = append(issues, fmt.Sprintf("user tool_results at msg[%d] have no preceding assistant tool_use", i))
				continue
			}
			prevMsg, ok := msgs[i-1].(map[string]interface{})
			if !ok {
				issues = append(issues, fmt.Sprintf("user tool_results at msg[%d] preceded by non-object msg[%d]", i, i-1))
				continue
			}
			prevRole, _ := prevMsg["role"].(string)
			if prevRole != "assistant" {
				issues = append(issues, fmt.Sprintf("user tool_results at msg[%d] not preceded by assistant", i))
				continue
			}
			prevToolUses := collectToolUseSet(prevMsg["content"])
			var orphan []string
			for _, id := range resultIDs {
				if !prevToolUses[id] {
					orphan = append(orphan, id)
				}
			}
			if len(orphan) > 0 {
				issues = append(issues, fmt.Sprintf("user tool_results at msg[%d] orphan ids: %s", i, previewIDs(orphan)))
			}
		}
	}

	return issues
}

func validateOutboundRequestMessages(msgs []interface{}) []string {
	issues := validateMessageStructure(msgs)
	if len(msgs) == 0 {
		return issues
	}
	lastIdx := len(msgs) - 1
	last, ok := msgs[lastIdx].(map[string]interface{})
	if !ok {
		return append(issues, fmt.Sprintf("msg[%d] is not an object", lastIdx))
	}
	role, _ := last["role"].(string)
	if role != "user" {
		issues = append(issues, fmt.Sprintf("conversation must end with user, got %q at msg[%d]", role, lastIdx))
	}
	return issues
}

func originalRequestRequiresUserFinal(msgs []interface{}) bool {
	if len(msgs) == 0 {
		return false
	}
	last, ok := msgs[len(msgs)-1].(map[string]interface{})
	if !ok {
		return false
	}
	role, _ := last["role"].(string)
	return role == "user"
}

func collectToolUseIDs(content []interface{}) []string {
	var ids []string
	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		tp, _ := block["type"].(string)
		// server_tool_use (.69+ advanced tool use) also requires tool_result pairing
		if tp == "tool_use" || tp == "server_tool_use" {
			if id, _ := block["id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func collectToolUseSet(raw interface{}) map[string]bool {
	content, _ := raw.([]interface{})
	set := make(map[string]bool)
	for _, id := range collectToolUseIDs(content) {
		set[id] = true
	}
	return set
}

func collectToolResultIDs(raw interface{}) map[string]bool {
	content, _ := raw.([]interface{})
	set := make(map[string]bool)
	for _, id := range collectOrderedToolResultIDs(content) {
		set[id] = true
	}
	return set
}

func collectOrderedToolResultIDs(content []interface{}) []string {
	var ids []string
	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if tp, _ := block["type"].(string); tp == "tool_result" {
			if id, _ := block["tool_use_id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func previewIDs(ids []string) string {
	if len(ids) == 0 {
		return "[]"
	}
	if len(ids) > 3 {
		return fmt.Sprintf("%q, %q, %q (+%d more)", ids[0], ids[1], ids[2], len(ids)-3)
	}
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, fmt.Sprintf("%q", id))
	}
	return strings.Join(quoted, ", ")
}

func fallbackToOriginalMessagesIfInvalid(body map[string]interface{}, originalMessages []interface{}, requireUserFinal bool) (bool, []string, []string) {
	current, _ := body["messages"].([]interface{})
	finalIssues := validateRequestMessages(current, requireUserFinal)
	if len(finalIssues) == 0 {
		return false, nil, nil
	}
	if len(originalMessages) == 0 {
		return false, finalIssues, nil
	}
	restored := deepCopyMessages(originalMessages)
	originalIssues := validateRequestMessages(restored, requireUserFinal)
	if len(originalIssues) > 0 {
		return false, finalIssues, originalIssues
	}
	candidate := shallowCopyBody(body)
	candidate["messages"] = restored
	if approxTokens := estimateTokens(candidate); approxTokens > maxFallbackRequestTokens {
		return false, finalIssues, []string{
			fmt.Sprintf("fallback request too large: approx %d tokens > %d", approxTokens, maxFallbackRequestTokens),
		}
	}
	body["messages"] = restored
	return true, finalIssues, nil
}

func validateRequestMessages(msgs []interface{}, requireUserFinal bool) []string {
	if requireUserFinal {
		return validateOutboundRequestMessages(msgs)
	}
	return validateMessageStructure(msgs)
}

func shallowCopyBody(body map[string]interface{}) map[string]interface{} {
	copied := make(map[string]interface{}, len(body))
	for k, v := range body {
		copied[k] = v
	}
	return copied
}

func summarizeValidationIssues(issues []string) string {
	if len(issues) == 0 {
		return ""
	}
	if len(issues) > 3 {
		return strings.Join(issues[:3], "; ") + fmt.Sprintf(" (+%d more)", len(issues)-3)
	}
	return strings.Join(issues, "; ")
}

func lastExplicitCacheControlAnchor(body map[string]interface{}) int {
	msgs, _ := body["messages"].([]interface{})
	last := -1
	for i, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := msg["cache_control"]; ok {
			last = i
		}
		content, _ := msg["content"].([]interface{})
		for _, blk := range content {
			block, ok := blk.(map[string]interface{})
			if !ok {
				continue
			}
			if _, ok := block["cache_control"]; ok {
				last = i
			}
		}
	}
	return last
}
