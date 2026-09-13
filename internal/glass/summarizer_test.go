package glass

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizerStatePersistedAndLoaded(t *testing.T) {
	dir := t.TempDir()
	cfg := SummarizerConfig{
		Enabled:       true,
		IntervalToken: 50000,
		Model:         "test-model",
		GateEnabled:   true,
	}
	rs := &RollingSummarizer{
		states:    make(map[string]*SummarizerState),
		cfg:       cfg,
		shadowDir: dir,
		booksDir:  filepath.Join(dir, "glass-books"),
	}

	st := rs.getState("test-conv")
	st.LastSummarizedIdx = 42
	st.TokensSummarized = 10000
	st.Chunks = append(st.Chunks, RollingChunk{
		ChunkNum:    1,
		MsgIdxStart: 0,
		MsgIdxEnd:   20,
	})
	if err := rs.saveState(st); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload
	rs2 := &RollingSummarizer{
		states:    make(map[string]*SummarizerState),
		cfg:       cfg,
		shadowDir: dir,
		booksDir:  filepath.Join(dir, "glass-books"),
	}
	st2 := rs2.getState("test-conv")
	if st2.LastSummarizedIdx != 42 {
		t.Errorf("LastSummarizedIdx = %d, want 42", st2.LastSummarizedIdx)
	}
	if st2.TokensSummarized != 10000 {
		t.Errorf("TokensSummarized = %d, want 10000", st2.TokensSummarized)
	}
	if len(st2.Chunks) != 1 {
		t.Errorf("Chunks = %d, want 1", len(st2.Chunks))
	}
}

func TestRecoveryGateArmedAndCleared(t *testing.T) {
	dir := t.TempDir()
	cfg := SummarizerConfig{
		Enabled:       true,
		IntervalToken: 50000,
		Model:         "test-model",
		GateEnabled:   true,
	}
	rs := &RollingSummarizer{
		states:    make(map[string]*SummarizerState),
		cfg:       cfg,
		shadowDir: dir,
		booksDir:  filepath.Join(dir, "glass-books"),
	}

	// Arm the gate manually
	st := rs.getState("gate-test")
	st.RecoveryGateArmed = true
	st.RecoveryFilePath = "/some/path/recovery-001.md"

	if !rs.IsGateArmed("gate-test") {
		t.Fatal("gate should be armed")
	}

	// Send messages without the recovery file — gate stays armed
	msgs := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type":    "tool_result",
					"content": "some unrelated content",
				},
			},
		},
	}
	cleared := rs.CheckGateClear("gate-test", msgs)
	if cleared {
		t.Fatal("gate should NOT be cleared by unrelated tool_result")
	}
	if !rs.IsGateArmed("gate-test") {
		t.Fatal("gate should still be armed")
	}

	// Send assistant tool_use with Read(file_path=recovery-001.md) — gate clears
	msgs2 := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type": "tool_use",
					"id":   "toolu_read_recovery",
					"name": "Read",
					"input": map[string]interface{}{
						"file_path": "/some/path/recovery-001.md",
					},
				},
			},
		},
	}
	cleared2 := rs.CheckGateClear("gate-test", msgs2)
	if !cleared2 {
		t.Fatal("gate should be cleared when assistant calls Read on recovery file")
	}
	if rs.IsGateArmed("gate-test") {
		t.Fatal("gate should NOT be armed after clearing via tool_use")
	}

	// Re-arm and verify tool_result path still works too
	st.RecoveryGateArmed = true
	st.RecoveryFilePath = "/some/path/recovery-001.md"
	msgs3 := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type":    "tool_result",
					"content": "     1→# Recovery Summary — Eviction 1\n     2→recovery-001.md contents here",
				},
			},
		},
	}
	cleared3 := rs.CheckGateClear("gate-test", msgs3)
	if !cleared3 {
		t.Fatal("gate should be cleared when tool_result contains recovery file name")
	}
	if rs.IsGateArmed("gate-test") {
		t.Fatal("gate should NOT be armed after clearing via tool_result")
	}
}

func TestStitchProducesRecoveryFile(t *testing.T) {
	dir := t.TempDir()
	booksDir := filepath.Join(dir, "glass-books")
	cfg := SummarizerConfig{
		Enabled:       true,
		IntervalToken: 50000,
		Model:         "test-model",
		GateEnabled:   true,
	}
	rs := &RollingSummarizer{
		states:    make(map[string]*SummarizerState),
		cfg:       cfg,
		shadowDir: dir,
		booksDir:  booksDir,
	}

	// Pre-create a chunk file
	st := rs.getState("stitch-test")
	sessionDir := rs.sessionDir("stitch-test")
	os.MkdirAll(sessionDir, 0755)
	chunkPath := filepath.Join(sessionDir, "summary-chunk-001.md")
	os.WriteFile(chunkPath, []byte("## Chunk 1 (messages 1–10)\n\n### User Requests\n- [msg 1]: > \"fix the bug\"\n"), 0644)
	st.Chunks = append(st.Chunks, RollingChunk{
		ChunkNum:    1,
		MsgIdxStart: 1,
		MsgIdxEnd:   10,
		SummaryPath: chunkPath,
	})
	st.LastSummarizedIdx = 10
	st.TokensSummarized = 5000

	// Evict with empty batch (no delta to summarize)
	state := &SessionState{
		ConvID:       "stitch-test",
		EvictedCount: 10,
		BatchCount:   1,
	}
	recoveryPath := rs.StitchAndArm("stitch-test", nil, nil, 1, state)
	if recoveryPath == "" {
		t.Fatal("expected recovery path")
	}
	if !strings.HasSuffix(recoveryPath, "recovery-001.md") {
		t.Errorf("unexpected path: %s", recoveryPath)
	}

	data, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatalf("read recovery: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Recovery Summary — Eviction 1") {
		t.Error("missing header")
	}
	if !strings.Contains(content, "fix the bug") {
		t.Error("missing chunk content")
	}

	// Gate should be armed
	if !rs.IsGateArmed("stitch-test") {
		t.Error("gate should be armed after stitch")
	}
}

func TestMaybeChunkTriggersAtInterval(t *testing.T) {
	// Set up a mock Opus server
	opusCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opusCalled = true
		resp := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "## Chunk 1 (messages 0–5)\n\n### User Requests\n- [msg 0]: > \"test request\"\n\n### Assessment\nTest summary.\n",
				},
			},
			"usage": map[string]interface{}{
				"input_tokens":  float64(100),
				"output_tokens": float64(50),
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dir := t.TempDir()
	rs := NewRollingSummarizer(SummarizerConfig{
		Enabled:       true,
		IntervalToken: 100, // very low threshold for testing
		Model:         "test-model",
		GateEnabled:   true,
	}, server.URL, dir, nil)

	if rs == nil {
		t.Fatal("summarizer should not be nil")
	}

	// Capture auth like the proxy would on first request
	rs.CaptureAuth(RequestMeta{
		APIKey:           "test-key",
		AuthIsBearer:     false,
		AnthropicVersion: "2023-06-01",
	})

	// Create a cache with enough messages to trigger
	cache := NewLocalCache("chunk-test")
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		cache.messages = append(cache.messages, CachedMsg{
			Msg: map[string]interface{}{
				"role": role,
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": fmt.Sprintf("Message %d with some content to hit the token threshold", i),
					},
				},
			},
			Hash:   fmt.Sprintf("hash_%d", i),
			Role:   role,
			Tokens: 50, // 50 tokens each, 10 messages = 500 tokens > 100 interval
		})
	}

	produced := rs.MaybeChunk("chunk-test", cache)
	if !produced {
		t.Error("expected chunk to be produced")
	}
	if !opusCalled {
		t.Error("expected Opus API to be called")
	}

	// Verify chunk file exists
	st := rs.getState("chunk-test")
	if len(st.Chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(st.Chunks))
	}
	if st.LastSummarizedIdx < 1 {
		t.Errorf("LastSummarizedIdx should have advanced, got %d", st.LastSummarizedIdx)
	}

	// Verify file on disk
	if len(st.Chunks) > 0 {
		data, err := os.ReadFile(st.Chunks[0].SummaryPath)
		if err != nil {
			t.Fatalf("read chunk file: %v", err)
		}
		if !strings.Contains(string(data), "test request") {
			t.Error("chunk file should contain Opus response")
		}
	}
}

func TestAuthStatusReflectsCapturedAuth(t *testing.T) {
	dir := t.TempDir()
	rs := NewRollingSummarizer(SummarizerConfig{
		Enabled:       true,
		IntervalToken: 100,
		Model:         "test-model",
		GateEnabled:   true,
	}, "https://api.anthropic.com", dir, nil)
	if rs == nil {
		t.Fatal("summarizer should not be nil")
	}

	status := rs.AuthStatus()
	if status.CapturedAuthAvailable {
		t.Fatal("captured auth should not be available before capture")
	}
	if status.AuthType != "missing" {
		t.Fatalf("expected missing auth type before capture, got %q", status.AuthType)
	}

	rs.CaptureAuth(RequestMeta{
		APIKey:           "oauth-token",
		AuthIsBearer:     true,
		AnthropicVersion: "2023-06-01",
		Betas:            []string{"beta-one", "beta-two"},
	})

	status = rs.AuthStatus()
	if !status.CapturedAuthAvailable {
		t.Fatal("captured auth should be available after capture")
	}
	if !status.Captured {
		t.Fatal("captured flag should be true after capture")
	}
	if status.AuthType != "bearer" {
		t.Fatalf("expected bearer auth type after capture, got %q", status.AuthType)
	}
	if status.AnthropicVersion != "2023-06-01" {
		t.Fatalf("unexpected anthropic version %q", status.AnthropicVersion)
	}
	if len(status.Betas) != 2 || status.Betas[0] != "beta-one" || status.Betas[1] != "beta-two" {
		t.Fatalf("unexpected betas: %#v", status.Betas)
	}
}

func TestNilSummarizerIsNoOp(t *testing.T) {
	var rs *RollingSummarizer
	// All methods should be safe on nil
	if rs.IsGateArmed("x") {
		t.Error("nil summarizer should not have gate armed")
	}
	if rs.RecoveryFilePath("x") != "" {
		t.Error("nil summarizer should return empty path")
	}
	if rs.MaybeChunk("x", nil) {
		t.Error("nil summarizer should not produce chunk")
	}
	if rs.CheckGateClear("x", nil) {
		t.Error("nil summarizer should not clear gate")
	}
	path := rs.StitchAndArm("x", nil, nil, 1, nil)
	if path != "" {
		t.Error("nil summarizer should return empty path")
	}
}
