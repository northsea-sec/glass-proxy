package glass

import (
	"strings"
	"testing"
)

func TestValidateMessageStructureDetectsSeparatedToolResults(t *testing.T) {
	msgs := []interface{}{
		userTextMessage("start"),
		assistantToolUseMessage("toolu_a", "bash"),
		userTextMessage("just text"),
		assistantTextMessage("status"),
		userToolResultMessage("toolu_a", "done"),
	}

	issues := validateMessageStructure(msgs)
	if len(issues) == 0 {
		t.Fatal("expected invalid tool-use structure to be detected")
	}
}

func TestValidateOutboundRequestMessagesRejectsAssistantFinal(t *testing.T) {
	msgs := []interface{}{
		userTextMessage("start"),
		assistantTextMessage("prefill"),
	}

	issues := validateOutboundRequestMessages(msgs)
	if len(issues) == 0 {
		t.Fatal("expected outbound validation to reject assistant-final request")
	}
	if !strings.Contains(strings.Join(issues, "; "), "conversation must end with user") {
		t.Fatalf("expected assistant-final issue, got %v", issues)
	}
}

func TestFallbackToOriginalMessagesIfInvalidRestoresOriginal(t *testing.T) {
	original := []interface{}{
		userTextMessage("start"),
		assistantToolUseMessage("toolu_a", "bash"),
		userToolResultMessage("toolu_a", "done"),
		assistantTextMessage("status"),
	}
	body := map[string]interface{}{
		"messages": []interface{}{
			userTextMessage("start"),
			assistantToolUseMessage("toolu_a", "bash"),
			userTextMessage("just text"),
			assistantTextMessage("status"),
			userToolResultMessage("toolu_a", "done"),
		},
	}

	restored, finalIssues, originalIssues := fallbackToOriginalMessagesIfInvalid(body, original, false)
	if !restored {
		t.Fatal("expected fallback to restore original messages")
	}
	if len(finalIssues) == 0 {
		t.Fatal("expected invalid final issues to be reported")
	}
	if len(originalIssues) != 0 {
		t.Fatalf("expected original messages to validate cleanly, got %v", originalIssues)
	}

	msgs, ok := body["messages"].([]interface{})
	if !ok {
		t.Fatal("messages missing after fallback")
	}
	if got, want := len(validateMessageStructure(msgs)), 0; got != want {
		t.Fatalf("expected restored messages to be valid, got %d issues", got)
	}

	roleAt2, _ := msgs[2].(map[string]interface{})["role"].(string)
	if roleAt2 != "user" {
		t.Fatalf("expected msg[2] role=user after fallback, got %q", roleAt2)
	}
	content, _ := msgs[2].(map[string]interface{})["content"].([]interface{})
	block, _ := content[0].(map[string]interface{})
	if got, _ := block["type"].(string); got != "tool_result" {
		t.Fatalf("expected msg[2] to be tool_result after fallback, got %q", got)
	}
}

func TestFallbackToOriginalMessagesIfInvalidRejectsOversizeRestore(t *testing.T) {
	huge := strings.Repeat("x", 900000)
	original := []interface{}{
		userTextMessage(huge),
		assistantTextMessage("ok"),
	}
	current := []interface{}{
		userTextMessage("start"),
		assistantToolUseMessage("toolu_a", "bash"),
		userTextMessage("just text"),
		assistantTextMessage("status"),
		userToolResultMessage("toolu_a", "done"),
	}
	body := map[string]interface{}{
		"messages": current,
	}

	restored, finalIssues, originalIssues := fallbackToOriginalMessagesIfInvalid(body, original, false)
	if restored {
		t.Fatal("expected oversize fallback to be rejected")
	}
	if len(finalIssues) == 0 {
		t.Fatal("expected invalid final issues to be reported")
	}
	if len(originalIssues) != 1 || !strings.Contains(originalIssues[0], "fallback request too large") {
		t.Fatalf("expected oversize reason, got %v", originalIssues)
	}
	msgs, _ := body["messages"].([]interface{})
	if len(msgs) != len(current) {
		t.Fatalf("expected current messages to remain in place, got %d", len(msgs))
	}
}

func TestFallbackToOriginalMessagesIfInvalidRestoresAssistantFinalOriginal(t *testing.T) {
	original := []interface{}{
		userTextMessage("start"),
		assistantTextMessage("status"),
		userTextMessage("continue"),
	}
	body := map[string]interface{}{
		"messages": []interface{}{
			userTextMessage("start"),
			assistantTextMessage("status"),
		},
	}

	restored, finalIssues, originalIssues := fallbackToOriginalMessagesIfInvalid(body, original, true)
	if !restored {
		t.Fatal("expected assistant-final fallback to restore original user-final request")
	}
	if len(finalIssues) == 0 {
		t.Fatal("expected assistant-final issues to be reported")
	}
	if len(originalIssues) != 0 {
		t.Fatalf("expected original messages to validate cleanly, got %v", originalIssues)
	}

	msgs, _ := body["messages"].([]interface{})
	if issues := validateOutboundRequestMessages(msgs); len(issues) > 0 {
		t.Fatalf("expected restored outbound request to validate cleanly, got %v", issues)
	}
}

func assistantToolUseMessage(id, name string) map[string]interface{} {
	return map[string]interface{}{
		"role": "assistant",
		"content": []interface{}{
			map[string]interface{}{
				"type": "tool_use",
				"id":   id,
				"name": name,
				"input": map[string]interface{}{
					"command": "echo test",
				},
			},
		},
	}
}

func userToolResultMessage(id, result string) map[string]interface{} {
	return map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": id,
				"content":     result,
			},
		},
	}
}
