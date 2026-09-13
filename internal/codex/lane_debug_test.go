package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLaneAuthStatusReflectsCapturedBearerAndSessions(t *testing.T) {
	tempDir := t.TempDir()
	h := NewHandler(Config{
		Upstream:  "https://api.openai.com",
		ShadowDir: tempDir,
	})
	h.captureRequestAuth("Bearer sk-test", "org-test", "proj-test")
	h.sessions.get("conv-a")

	status := h.LaneAuthStatus()
	if status.Lane != "codex" {
		t.Fatalf("expected codex lane, got %q", status.Lane)
	}
	if !status.CapturedAuthAvailable {
		t.Fatal("expected captured auth to be available")
	}
	if status.CapturedAuthType != "bearer" {
		t.Fatalf("expected bearer auth type, got %q", status.CapturedAuthType)
	}
	if status.SummarizerEnabled {
		t.Fatal("expected summarizer_enabled to be false")
	}
	if status.SessionCount != 1 {
		t.Fatalf("expected 1 session, got %d", status.SessionCount)
	}
	if status.ShadowDir != tempDir {
		t.Fatalf("expected shadow dir %q, got %q", tempDir, status.ShadowDir)
	}
	if !status.SupportsLiveSessionProbe {
		t.Fatal("expected codex lane to report live session probe support")
	}
	if status.SessionPersistence != "live_memory_with_disk_snapshot" {
		t.Fatalf("expected live memory + disk snapshot persistence, got %q", status.SessionPersistence)
	}
}

func TestLaneSessionsSnapshotReportsPersistenceArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	h := NewHandler(Config{ShadowDir: tempDir})

	older := h.sessions.get("conv-older")
	newer := h.sessions.get("conv-newer")

	olderCreated := time.Date(2026, time.April, 11, 8, 0, 0, 0, time.UTC)
	olderUpdated := olderCreated.Add(3 * time.Minute)
	newerCreated := olderCreated.Add(10 * time.Minute)
	newerUpdated := newerCreated.Add(1 * time.Minute)

	older.mu.Lock()
	older.model = "gpt-5.4"
	older.items = []cachedItem{{Tokens: 5}, {Tokens: 7}}
	older.evictedCount = 3
	older.batchCount = 1
	older.compWatermark = 4
	older.lastAPIInput = 91
	older.lastAPIInputAt = olderUpdated.Add(-15 * time.Second)
	older.createdAt = olderCreated
	older.updatedAt = olderUpdated
	older.mu.Unlock()

	newer.mu.Lock()
	newer.model = "gpt-5.4-mini"
	newer.items = []cachedItem{{Tokens: 2}}
	newer.createdAt = newerCreated
	newer.updatedAt = newerUpdated
	newer.mu.Unlock()

	olderDir := filepath.Join(tempDir, "conv-older")
	if err := os.MkdirAll(olderDir, 0o755); err != nil {
		t.Fatalf("mkdir older dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(olderDir, "cache.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write cache.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(olderDir, "system_prompt.txt"), []byte("system prompt"), 0o644); err != nil {
		t.Fatalf("write system_prompt.txt: %v", err)
	}

	snapshots := h.LaneSessions()
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}
	if snapshots[0].ConvID != "conv-newer" {
		t.Fatalf("expected newest snapshot first, got %q", snapshots[0].ConvID)
	}

	snapshot, ok := h.LaneSession("conv-older")
	if !ok {
		t.Fatal("expected conv-older snapshot to exist")
	}
	if snapshot.ItemCount != 2 {
		t.Fatalf("expected 2 cached items, got %d", snapshot.ItemCount)
	}
	if snapshot.Model != "gpt-5.4" {
		t.Fatalf("expected model gpt-5.4, got %q", snapshot.Model)
	}
	if snapshot.TotalTokens != 12 {
		t.Fatalf("expected 12 total tokens, got %d", snapshot.TotalTokens)
	}
	if snapshot.EvictedCount != 3 {
		t.Fatalf("expected evicted count 3, got %d", snapshot.EvictedCount)
	}
	if snapshot.BatchCount != 1 {
		t.Fatalf("expected batch count 1, got %d", snapshot.BatchCount)
	}
	if snapshot.CompressionWatermark != 4 {
		t.Fatalf("expected compression watermark 4, got %d", snapshot.CompressionWatermark)
	}
	if snapshot.LastAPIInput != 91 {
		t.Fatalf("expected last api input 91, got %d", snapshot.LastAPIInput)
	}
	if !snapshot.CachePersisted {
		t.Fatal("expected cache persistence flag to be true")
	}
	if !snapshot.SystemPromptCaptured {
		t.Fatal("expected system prompt capture flag to be true")
	}
	if snapshot.CachePath != filepath.Join(tempDir, "conv-older", "cache.json") {
		t.Fatalf("unexpected cache path %q", snapshot.CachePath)
	}
	if snapshot.SystemPromptPath != filepath.Join(tempDir, "conv-older", "system_prompt.txt") {
		t.Fatalf("unexpected system prompt path %q", snapshot.SystemPromptPath)
	}
	if snapshot.CreatedAt == "" || snapshot.UpdatedAt == "" || snapshot.LastAPIInputAt == "" {
		t.Fatal("expected timestamp fields to be populated")
	}
}

func TestLaneSessionReplayIncludesTemplateAndLiveInput(t *testing.T) {
	tempDir := t.TempDir()
	h := NewHandler(Config{ShadowDir: tempDir})

	sess := h.sessions.get("conv-replay")
	sess.mu.Lock()
	sess.model = "gpt-5.4"
	sess.items = []cachedItem{
		{Item: map[string]interface{}{"type": "message", "role": "user", "content": "hello"}, Tokens: 4},
		{Item: map[string]interface{}{"type": "message", "role": "assistant", "content": "world"}, Tokens: 5},
	}
	sess.mu.Unlock()
	sess.captureReplayTemplate("/v1/responses", map[string]interface{}{
		"model":        "gpt-5.4",
		"instructions": "stay focused",
		"stream":       true,
	})
	h.captureRequestAuth("Bearer sk-replay", "", "")

	replay, ok := h.LaneSessionReplay("conv-replay")
	if !ok {
		t.Fatal("expected replay to exist")
	}
	if replay.RequestURI != "/v1/responses" {
		t.Fatalf("expected request URI /v1/responses, got %q", replay.RequestURI)
	}
	if replay.ProxyTemplate["model"] != "gpt-5.4" {
		t.Fatalf("expected replay model gpt-5.4, got %#v", replay.ProxyTemplate["model"])
	}
	if len(replay.LiveInput) != 2 {
		t.Fatalf("expected 2 live input items, got %d", len(replay.LiveInput))
	}
	if !replay.CapturedAuthAvailable || replay.CapturedAuthType != "bearer" {
		t.Fatalf("expected captured bearer auth, got available=%v type=%q", replay.CapturedAuthAvailable, replay.CapturedAuthType)
	}
	if replay.Snapshot.ConvID != "conv-replay" {
		t.Fatalf("expected snapshot conv-replay, got %q", replay.Snapshot.ConvID)
	}
}
