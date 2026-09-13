package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultStorageRootUsesCodexHomeWhenPresent(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(tempDir, "custom-codex-home"))

	got := DefaultStorageRoot()
	want := filepath.Join(tempDir, "custom-codex-home", "glass-proxy")
	if got != want {
		t.Fatalf("DefaultStorageRoot() = %q, want %q", got, want)
	}
}

func TestApplyCodexSyspromptPatchMatchesSessionAndHash(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("CODEX_HOME", "")
	writeCodexPatchFile(t, codexSyspromptPatchesFile, codexSyspromptPatchEnvelope{
		Patches: []codexSyspromptPatch{
			{
				SessionID: "conv-test",
				OldHash:   codexTextHash("original instructions"),
				NewText:   "patched instructions",
			},
		},
	})

	body := map[string]interface{}{"instructions": "original instructions"}
	applied, err := applyCodexSyspromptPatch("conv-test", body)
	if err != nil {
		t.Fatalf("applyCodexSyspromptPatch error: %v", err)
	}
	if !applied {
		t.Fatal("expected sysprompt patch to apply")
	}
	if got := body["instructions"]; got != "patched instructions" {
		t.Fatalf("expected patched instructions, got %#v", got)
	}
}

func TestApplyCodexSyspromptPatchRejectsStaleHash(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("CODEX_HOME", "")
	writeCodexPatchFile(t, codexSyspromptPatchesFile, codexSyspromptPatchEnvelope{
		Patches: []codexSyspromptPatch{
			{
				SessionID: "conv-test",
				OldHash:   codexTextHash("older instructions"),
				NewText:   "patched instructions",
			},
		},
	})

	body := map[string]interface{}{"instructions": "original instructions"}
	applied, err := applyCodexSyspromptPatch("conv-test", body)
	if err != nil {
		t.Fatalf("applyCodexSyspromptPatch error: %v", err)
	}
	if applied {
		t.Fatal("expected stale sysprompt patch to be skipped")
	}
	if got := body["instructions"]; got != "original instructions" {
		t.Fatalf("expected original instructions to remain, got %#v", got)
	}
}

func TestApplyCodexContextPatchesMessageText(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("CODEX_HOME", "")
	writeCodexPatchFile(t, codexContextPatchesFile, codexContextPatchEnvelope{
		Patches: []codexContextPatch{
			{
				SessionID:  "conv-test",
				TargetKind: "input_item",
				ItemIndex:  0,
				ItemType:   "message",
				Role:       "user",
				FieldPath:  "content[0].text",
				OldHash:    codexTextHash("hello"),
				NewText:    "patched hello",
			},
		},
	})

	items := []interface{}{
		map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "input_text", "text": "hello"},
			},
		},
	}

	applied, err := applyCodexContextPatches("conv-test", items)
	if err != nil {
		t.Fatalf("applyCodexContextPatches error: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected 1 applied patch, got %d", applied)
	}
	item := items[0].(map[string]interface{})
	content := item["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if got := block["text"]; got != "patched hello" {
		t.Fatalf("expected patched block text, got %#v", got)
	}
}

func TestApplyCodexContextPatchesFunctionCallOutput(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("CODEX_HOME", "")
	writeCodexPatchFile(t, codexContextPatchesFile, codexContextPatchEnvelope{
		Patches: []codexContextPatch{
			{
				SessionID:  "conv-test",
				TargetKind: "input_item",
				ItemIndex:  0,
				ItemType:   "function_call_output",
				FieldPath:  "output",
				OldHash:    codexTextHash("tool result"),
				NewText:    "patched result",
			},
		},
	})

	items := []interface{}{
		map[string]interface{}{
			"type":   "function_call_output",
			"output": "tool result",
		},
	}

	applied, err := applyCodexContextPatches("conv-test", items)
	if err != nil {
		t.Fatalf("applyCodexContextPatches error: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected 1 applied patch, got %d", applied)
	}
	item := items[0].(map[string]interface{})
	if got := item["output"]; got != "patched result" {
		t.Fatalf("expected patched output, got %#v", got)
	}
}

func TestDeepCopyCodexInputItemsLeavesOriginalUntouched(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "input_text", "text": "hello"},
			},
		},
	}

	copied := deepCopyCodexInputItems(items)
	item := copied[0].(map[string]interface{})
	block := item["content"].([]interface{})[0].(map[string]interface{})
	block["text"] = "patched"

	orig := items[0].(map[string]interface{})
	origBlock := orig["content"].([]interface{})[0].(map[string]interface{})
	if got := origBlock["text"]; got != "hello" {
		t.Fatalf("expected original items to remain unchanged, got %#v", got)
	}
}

func writeCodexPatchFile(t *testing.T, filename string, payload interface{}) {
	t.Helper()
	dir := DefaultStorageRoot()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}
