package proxy

import (
	"encoding/json"
	"os"
	"testing"

	"proxy.local/app/internal/runtimepaths"
)

func TestApplyContextPatchesScopesBySessionAndAcceptsLegacyHashes(t *testing.T) {
	t.Setenv(runtimepaths.RuntimeLaneEnv, "anthropic")
	t.Setenv(runtimepaths.RuntimeRootEnv, t.TempDir())

	writeClaudeContextPatches(t, claudeContextPatchEnvelope{
		Patches: []claudeContextPatch{
			{
				SessionID:  "other-session",
				Op:         "replace",
				Index:      0,
				Role:       "user",
				OldHash:    claudeTextHash("original user text"),
				NewContent: "should not apply",
			},
			{
				SessionID:  "target-session",
				Op:         "replace",
				Index:      0,
				Role:       "user",
				OldHash:    claudeLegacySimpleHash("original user text"),
				NewContent: "patched user text",
			},
		},
	})

	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "original user text"},
				},
			},
		},
	}

	applyContextPatches(body, "target-session")

	msg := body["messages"].([]interface{})[0].(map[string]interface{})
	if got := extractMessageText(msg); got != "patched user text" {
		t.Fatalf("extractMessageText() = %q, want patched user text", got)
	}
}

func TestApplyContextPatchesInsertAfterAddsSyntheticAssistant(t *testing.T) {
	t.Setenv(runtimepaths.RuntimeLaneEnv, "anthropic")
	t.Setenv(runtimepaths.RuntimeRootEnv, t.TempDir())

	writeClaudeContextPatches(t, claudeContextPatchEnvelope{
		Patches: []claudeContextPatch{
			{
				SessionID:  "target-session",
				Op:         "insert_after",
				Index:      0,
				Role:       "user",
				OldHash:    claudeTextHash("anchor request"),
				NewContent: "synthetic assistant bridge",
				InsertRole: "assistant",
			},
		},
	})

	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "anchor request"},
				},
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "follow up"},
				},
			},
		},
	}

	applyContextPatches(body, "target-session")

	messages := body["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(messages))
	}
	inserted := messages[1].(map[string]interface{})
	if role, _ := inserted["role"].(string); role != "assistant" {
		t.Fatalf("inserted role = %q, want assistant", role)
	}
	if got := extractMessageText(inserted); got != "synthetic assistant bridge" {
		t.Fatalf("extractMessageText(inserted) = %q", got)
	}
}

func TestIsAnthropicUsagePolicyErrorDetectsCyberBlock(t *testing.T) {
	body := []byte(`{"error":{"message":"This request triggered restrictions on violative cyber content and was blocked under Anthropic's Usage Policy."}}`)
	if !isAnthropicUsagePolicyError(400, body) {
		t.Fatal("expected usage policy error detection")
	}
}

func writeClaudeContextPatches(t *testing.T, payload claudeContextPatchEnvelope) {
	t.Helper()
	path := runtimepaths.Current().ContextPatchesPath
	if err := os.MkdirAll(runtimepaths.Current().RootDir, 0o755); err != nil {
		t.Fatalf("mkdir runtime root: %v", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write context patches: %v", err)
	}
}
