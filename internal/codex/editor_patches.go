package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	codexSyspromptPatchesFile = "codex_sysprompt_patches.json"
	codexContextPatchesFile   = "codex_context_patches.json"
)

type codexSyspromptPatchEnvelope struct {
	Patches []codexSyspromptPatch `json:"patches"`
}

type codexSyspromptPatch struct {
	SessionID string `json:"session_id"`
	OldHash   string `json:"old_hash"`
	NewText   string `json:"new_text"`
}

type codexContextPatchEnvelope struct {
	Patches []codexContextPatch `json:"patches"`
}

type codexContextPatch struct {
	SessionID        string `json:"session_id"`
	TargetKind       string `json:"target_kind"`
	ItemIndex        int    `json:"item_index"`
	ItemType         string `json:"item_type"`
	Role             string `json:"role"`
	ContentPartIndex *int   `json:"content_part_index,omitempty"`
	FieldPath        string `json:"field_path"`
	OldHash          string `json:"old_hash"`
	NewText          string `json:"new_text"`
}

func codexEditorPatchPath(filename string) (string, error) {
	return filepath.Join(DefaultStorageRoot(), filename), nil
}

func loadCodexSyspromptPatch(sessionID string) (*codexSyspromptPatch, error) {
	path, err := codexEditorPatchPath(codexSyspromptPatchesFile)
	if err != nil {
		return nil, err
	}
	var envelope codexSyspromptPatchEnvelope
	if err := loadCodexPatchEnvelope(path, &envelope); err != nil {
		return nil, err
	}
	for _, patch := range envelope.Patches {
		if strings.TrimSpace(patch.SessionID) != sessionID {
			continue
		}
		patchCopy := patch
		return &patchCopy, nil
	}
	return nil, nil
}

func loadCodexContextPatches(sessionID string) ([]codexContextPatch, error) {
	path, err := codexEditorPatchPath(codexContextPatchesFile)
	if err != nil {
		return nil, err
	}
	var envelope codexContextPatchEnvelope
	if err := loadCodexPatchEnvelope(path, &envelope); err != nil {
		return nil, err
	}
	patches := make([]codexContextPatch, 0, len(envelope.Patches))
	for _, patch := range envelope.Patches {
		if strings.TrimSpace(patch.SessionID) != sessionID {
			continue
		}
		patches = append(patches, patch)
	}
	return patches, nil
}

func loadCodexPatchEnvelope(path string, dst interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func codexTextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func codexHashMatches(oldHash, currentText string) bool {
	oldHash = strings.TrimSpace(oldHash)
	if oldHash == "" {
		return true
	}
	currentHash := codexTextHash(currentText)
	if oldHash == currentHash {
		return true
	}
	return oldHash == strings.TrimPrefix(currentHash, "sha256:")
}

func applyCodexSyspromptPatch(sessionID string, body map[string]interface{}) (bool, error) {
	patch, err := loadCodexSyspromptPatch(sessionID)
	if err != nil || patch == nil {
		return false, err
	}
	current, _ := body["instructions"].(string)
	if !codexHashMatches(patch.OldHash, current) {
		return false, nil
	}
	body["instructions"] = patch.NewText
	return true, nil
}

func deepCopyCodexInputItems(items []interface{}) []interface{} {
	if len(items) == 0 {
		return nil
	}
	out := make([]interface{}, len(items))
	for i, item := range items {
		if mapped, ok := item.(map[string]interface{}); ok {
			out[i] = deepCopyItem(mapped)
			continue
		}
		out[i] = item
	}
	return out
}

func applyCodexContextPatches(sessionID string, items []interface{}) (int, error) {
	patches, err := loadCodexContextPatches(sessionID)
	if err != nil || len(patches) == 0 {
		return 0, err
	}
	applied := 0
	for _, patch := range patches {
		if targetKind := strings.TrimSpace(patch.TargetKind); targetKind != "" && targetKind != "input_item" {
			continue
		}
		if patch.ItemIndex < 0 || patch.ItemIndex >= len(items) {
			continue
		}
		item, ok := items[patch.ItemIndex].(map[string]interface{})
		if !ok {
			continue
		}
		if patch.ItemType != "" {
			itemType, _ := item["type"].(string)
			if itemType != patch.ItemType {
				continue
			}
		}
		if patch.Role != "" {
			role, _ := item["role"].(string)
			if role != patch.Role {
				continue
			}
		}
		if applyCodexContextPatchToItem(item, patch) {
			applied++
		}
	}
	return applied, nil
}

func applyCodexContextPatchToItem(item map[string]interface{}, patch codexContextPatch) bool {
	itemType, _ := item["type"].(string)
	fieldPath := strings.TrimSpace(patch.FieldPath)
	if fieldPath == "" {
		switch itemType {
		case "message":
			fieldPath = "content"
		case "function_call_output":
			fieldPath = "output"
		default:
			return false
		}
	}

	switch {
	case fieldPath == "content":
		current, ok := item["content"].(string)
		if !ok || !codexHashMatches(patch.OldHash, current) {
			return false
		}
		item["content"] = patch.NewText
		return true
	case strings.HasPrefix(fieldPath, "content[") && strings.HasSuffix(fieldPath, "].text"):
		partIndex, ok := parseCodexContentPartIndex(fieldPath)
		if !ok {
			return false
		}
		content, ok := item["content"].([]interface{})
		if !ok || partIndex < 0 || partIndex >= len(content) {
			return false
		}
		block, ok := content[partIndex].(map[string]interface{})
		if !ok {
			return false
		}
		blockType, _ := block["type"].(string)
		if blockType != "input_text" && blockType != "output_text" && blockType != "text" {
			return false
		}
		current, ok := block["text"].(string)
		if !ok || !codexHashMatches(patch.OldHash, current) {
			return false
		}
		block["text"] = patch.NewText
		return true
	case fieldPath == "output":
		currentText := renderCodexTextValue(item["output"])
		if !codexHashMatches(patch.OldHash, currentText) {
			return false
		}
		item["output"] = coerceCodexOutputValue(item["output"], patch.NewText)
		return true
	default:
		return false
	}
}

func parseCodexContentPartIndex(fieldPath string) (int, bool) {
	if !strings.HasPrefix(fieldPath, "content[") || !strings.HasSuffix(fieldPath, "].text") {
		return 0, false
	}
	inner := strings.TrimPrefix(fieldPath, "content[")
	inner = strings.TrimSuffix(inner, "].text")
	idx, err := strconv.Atoi(inner)
	if err != nil {
		return 0, false
	}
	return idx, true
}

func renderCodexTextValue(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	if value == nil {
		return ""
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return fmt.Sprintf("%v", value)
	}
	return strings.TrimSpace(buf.String())
}

func coerceCodexOutputValue(current interface{}, newText string) interface{} {
	if _, ok := current.(string); ok {
		return newText
	}
	if strings.TrimSpace(newText) == "" {
		return newText
	}
	var decoded interface{}
	if err := json.Unmarshal([]byte(newText), &decoded); err == nil {
		return decoded
	}
	return newText
}
