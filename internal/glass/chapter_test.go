package glass

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChapterWriterUpdate_WritesChapterAndIndex(t *testing.T) {
	shadowDir := filepath.Join(t.TempDir(), "glass")
	writer := NewChapterWriter(shadowDir)
	cache := NewLocalCache("chapter-conv")

	messages := []interface{}{
		userTextMessage(strings.Repeat("first user request ", 20)),
		assistantTextMessage(strings.Repeat("first assistant reply ", 20)),
		userTextMessage(strings.Repeat("second user request ", 20)),
		assistantTextMessage(strings.Repeat("second assistant reply ", 20)),
		userTextMessage(strings.Repeat("third user request ", 20)),
	}
	cache.Ingest(messages)
	evicted := cache.Evict(120, 1, 2)
	if len(evicted) == 0 {
		t.Fatal("expected eviction to produce archived messages")
	}

	state := &SessionState{
		ConvID:        "chapter-conv",
		EvictedHashes: make(map[string]bool),
		EvictedCount:  len(evicted),
		BatchCount:    1,
		CreatedAt:     time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC),
	}

	if err := writer.Update("chapter-conv", state, cache, evicted); err != nil {
		t.Fatalf("chapter update error: %v", err)
	}

	// ChapterMDPath now points at the session directory
	if state.ChapterMDPath == "" || state.ChapterJSONPath == "" {
		t.Fatalf("expected chapter paths to be recorded in session state, got %+v", state)
	}

	// The session dir should exist
	if fi, err := os.Stat(state.ChapterMDPath); err != nil || !fi.IsDir() {
		t.Fatalf("expected chapter session dir to exist: %v", err)
	}
	// The JSON file should exist
	if _, err := os.Stat(state.ChapterJSONPath); err != nil {
		t.Fatalf("expected chapter json to exist: %v", err)
	}

	// Read the chapter-001.md file inside the session dir
	mdPath := filepath.Join(state.ChapterMDPath, "chapter-001.md")
	chapterData, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read chapter markdown: %v", err)
	}
	chapterText := string(chapterData)
	// Evicted messages should be in the chapter (exact content depends on eviction order)
	if !strings.Contains(chapterText, "Message") {
		t.Fatalf("expected chapter markdown to contain archived messages, got:\n%s", chapterText)
	}
	if !strings.Contains(chapterText, "Eviction 1") {
		t.Fatalf("expected chapter markdown to reference eviction number, got:\n%s", chapterText)
	}

	// Index should exist in the parent dir
	indexPath := filepath.Join(filepath.Dir(state.ChapterMDPath), "index.md")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read chapter index: %v", err)
	}
	if !strings.Contains(string(indexData), "chapter-conv") {
		t.Fatalf("expected chapter index to reference the session, got:\n%s", string(indexData))
	}
}

func TestEnsureReferencePair_PrefersChapterPathWithoutSummary(t *testing.T) {
	state := &SessionState{
		ConvID:        "chapter-ref",
		EvictedCount:  12,
		BatchCount:    1,
		ChapterMDPath: "/tmp/glass-books/global/2026-03-12/chapter-ref",
		EvictedHashes: make(map[string]bool),
	}

	ensureReferencePair(state, "/tmp/glass")
	if !strings.Contains(state.ReferenceUser, state.ChapterMDPath) {
		t.Fatalf("expected reference note to point to chapter path, got:\n%s", state.ReferenceUser)
	}
	for _, forbidden := range []string{
		"Frozen summary of the first evicted batch",
		"shadow.md",
	} {
		if strings.Contains(state.ReferenceUser, forbidden) {
			t.Fatalf("did not expect %q in reference note, got:\n%s", forbidden, state.ReferenceUser)
		}
	}
}

func TestChapterWriterSeal_IsNoop(t *testing.T) {
	shadowDir := filepath.Join(t.TempDir(), "glass")
	writer := NewChapterWriter(shadowDir)
	cache := NewLocalCache("sealed-chapter")

	// Need enough content to actually trigger eviction
	msgs := []interface{}{
		userTextMessage(strings.Repeat("sealed user request ", 30)),
		assistantTextMessage(strings.Repeat("sealed assistant reply ", 30)),
		userTextMessage(strings.Repeat("sealed user followup ", 30)),
		assistantTextMessage(strings.Repeat("sealed assistant followup ", 30)),
		userTextMessage(strings.Repeat("sealed final question ", 30)),
	}
	cache.Ingest(msgs)
	evicted := cache.Evict(120, 1, 2)
	if len(evicted) == 0 {
		t.Fatal("expected eviction to produce messages")
	}
	state := &SessionState{
		ConvID:        "sealed-chapter",
		EvictedHashes: make(map[string]bool),
		EvictedCount:  len(evicted),
		BatchCount:    1,
		CreatedAt:     time.Date(2026, 3, 12, 11, 0, 0, 0, time.UTC),
	}
	if err := writer.Update("sealed-chapter", state, cache, evicted); err != nil {
		t.Fatalf("chapter update error: %v", err)
	}
	// Chapters are sealed on write — status should already be sealed
	if state.ChapterStatus != "sealed" {
		t.Fatalf("expected sealed chapter status after write, got %q", state.ChapterStatus)
	}
	// Seal is now a no-op, should not error
	if err := writer.Seal("sealed-chapter", state, cache, "explicit reset"); err != nil {
		t.Fatalf("chapter seal error: %v", err)
	}
}

func TestChapterWriterMultipleEvictions_SeparateFiles(t *testing.T) {
	shadowDir := filepath.Join(t.TempDir(), "glass")
	writer := NewChapterWriter(shadowDir)
	cache := NewLocalCache("multi-evict")

	// Fill and evict twice
	msgs := make([]interface{}, 0, 10)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			msgs = append(msgs, userTextMessage(strings.Repeat("user msg ", 20)))
		} else {
			msgs = append(msgs, assistantTextMessage(strings.Repeat("asst msg ", 20)))
		}
	}
	cache.Ingest(msgs)

	state := &SessionState{
		ConvID:        "multi-evict",
		EvictedHashes: make(map[string]bool),
		CreatedAt:     time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC),
	}

	// First eviction
	evicted1 := cache.Evict(200, 1, 2)
	state.EvictedCount += len(evicted1)
	state.BatchCount++
	if err := writer.Update("multi-evict", state, cache, evicted1); err != nil {
		t.Fatalf("first chapter write error: %v", err)
	}
	firstJSON := state.ChapterJSONPath

	// Second eviction
	evicted2 := cache.Evict(100, 1, 2)
	if len(evicted2) == 0 {
		t.Skip("not enough messages for second eviction")
	}
	state.EvictedCount += len(evicted2)
	state.BatchCount++
	if err := writer.Update("multi-evict", state, cache, evicted2); err != nil {
		t.Fatalf("second chapter write error: %v", err)
	}
	secondJSON := state.ChapterJSONPath

	// Should be different files
	if firstJSON == secondJSON {
		t.Fatalf("expected different chapter files for each eviction, both are %s", firstJSON)
	}

	// Both should exist
	if _, err := os.Stat(firstJSON); err != nil {
		t.Fatalf("first chapter file missing: %v", err)
	}
	if _, err := os.Stat(secondJSON); err != nil {
		t.Fatalf("second chapter file missing: %v", err)
	}

	// Both should be in the same session dir
	if filepath.Dir(firstJSON) != filepath.Dir(secondJSON) {
		t.Fatalf("expected same session dir, got %s vs %s", filepath.Dir(firstJSON), filepath.Dir(secondJSON))
	}
}

func TestChapterMessageContent_StripsToolResultsAndKeepsReasoning(t *testing.T) {
	assistant := map[string]interface{}{
		"role":              "assistant",
		"content":           nil,
		"reasoning_content": "Need to verify the file before making any edits.",
	}
	gotAssistant := chapterMessageContent(assistant)
	if !strings.Contains(gotAssistant, "Need to verify the file") {
		t.Fatalf("expected reasoning_content to be preserved, got %q", gotAssistant)
	}

	openAITool := map[string]interface{}{
		"role":         "tool",
		"tool_call_id": "call-1234567890",
		"content":      strings.Repeat("raw tool payload ", 200),
	}
	gotTool := chapterMessageContent(openAITool)
	if strings.Contains(gotTool, "raw tool payload") {
		t.Fatalf("expected raw OpenAI tool payload to be stripped, got %q", gotTool)
	}
	if !strings.Contains(gotTool, "tool result omitted") {
		t.Fatalf("expected stripped OpenAI tool marker, got %q", gotTool)
	}

	anthropicTool := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "toolu_abc123",
				"content":     strings.Repeat("anthropic tool payload ", 120),
			},
		},
	}
	gotAnthropicTool := chapterMessageContent(anthropicTool)
	if strings.Contains(gotAnthropicTool, "anthropic tool payload") {
		t.Fatalf("expected raw tool_result payload to be stripped, got %q", gotAnthropicTool)
	}
	if !strings.Contains(gotAnthropicTool, "tool result omitted") {
		t.Fatalf("expected stripped tool_result marker, got %q", gotAnthropicTool)
	}
}
