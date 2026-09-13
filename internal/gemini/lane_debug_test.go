package gemini

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLaneAuthStatusReflectsCapturedAPIKeyAndSessions(t *testing.T) {
	tempDir := t.TempDir()
	h := NewHandler(Config{
		Upstream:             "https://generativelanguage.googleapis.com",
		ShadowDir:            tempDir,
		ExplicitCacheEnabled: true,
	})
	h.captureRequestAuth("gem-test-key", "")
	h.sessions.get("conv-a")

	status := h.LaneAuthStatus()
	if status.Lane != "gemini" {
		t.Fatalf("expected gemini lane, got %q", status.Lane)
	}
	if !status.CapturedAuthAvailable {
		t.Fatal("expected captured auth to be available")
	}
	if status.CapturedAuthType != "api_key" {
		t.Fatalf("expected api_key auth type, got %q", status.CapturedAuthType)
	}
	if !status.ExplicitCacheEnabled {
		t.Fatal("expected explicit_cache_enabled to be true")
	}
	if status.SessionCount != 1 {
		t.Fatalf("expected 1 session, got %d", status.SessionCount)
	}
	if status.ShadowDir != tempDir {
		t.Fatalf("expected shadow dir %q, got %q", tempDir, status.ShadowDir)
	}
	if !status.SupportsLiveSessionProbe {
		t.Fatal("expected gemini lane to report live session probe support")
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

	olderCreated := time.Date(2026, time.April, 11, 9, 0, 0, 0, time.UTC)
	olderUpdated := olderCreated.Add(4 * time.Minute)
	newerCreated := olderCreated.Add(12 * time.Minute)
	newerUpdated := newerCreated.Add(1 * time.Minute)

	older.mu.Lock()
	older.model = "gemini-2.5-pro"
	older.contents = []cachedContent{{Tokens: 11}, {Tokens: 13}}
	older.evictedCount = 2
	older.batchCount = 1
	older.compWatermark = 6
	older.lastAPIInput = 144
	older.lastAPIInputAt = olderUpdated.Add(-20 * time.Second)
	older.createdAt = olderCreated
	older.updatedAt = olderUpdated
	older.mu.Unlock()

	newer.mu.Lock()
	newer.model = "gemini-2.5-flash"
	newer.contents = []cachedContent{{Tokens: 4}}
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
	if err := os.WriteFile(filepath.Join(olderDir, "system_prompt.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write system_prompt.json: %v", err)
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
	if snapshot.ContentCount != 2 {
		t.Fatalf("expected 2 cached contents, got %d", snapshot.ContentCount)
	}
	if snapshot.Model != "gemini-2.5-pro" {
		t.Fatalf("expected model gemini-2.5-pro, got %q", snapshot.Model)
	}
	if snapshot.TotalTokens != 24 {
		t.Fatalf("expected 24 total tokens, got %d", snapshot.TotalTokens)
	}
	if snapshot.EvictedCount != 2 {
		t.Fatalf("expected evicted count 2, got %d", snapshot.EvictedCount)
	}
	if snapshot.BatchCount != 1 {
		t.Fatalf("expected batch count 1, got %d", snapshot.BatchCount)
	}
	if snapshot.CompressionWatermark != 6 {
		t.Fatalf("expected compression watermark 6, got %d", snapshot.CompressionWatermark)
	}
	if snapshot.LastAPIInput != 144 {
		t.Fatalf("expected last api input 144, got %d", snapshot.LastAPIInput)
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
	if snapshot.SystemPromptPath != filepath.Join(tempDir, "conv-older", "system_prompt.json") {
		t.Fatalf("unexpected system prompt path %q", snapshot.SystemPromptPath)
	}
	if snapshot.CreatedAt == "" || snapshot.UpdatedAt == "" || snapshot.LastAPIInputAt == "" {
		t.Fatal("expected timestamp fields to be populated")
	}
}

func TestLaneSessionReplayIncludesTemplateAndLiveContents(t *testing.T) {
	tempDir := t.TempDir()
	h := NewHandler(Config{ShadowDir: tempDir})

	sess := h.sessions.get("conv-replay")
	sess.mu.Lock()
	sess.model = "gemini-2.5-pro"
	sess.contents = []cachedContent{
		{Content: map[string]interface{}{"role": "user", "parts": []interface{}{map[string]interface{}{"text": "hello"}}}, Tokens: 4},
		{Content: map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "world"}}}, Tokens: 5},
	}
	sess.mu.Unlock()
	sess.captureReplayTemplate("/v1beta/models/gemini-2.5-pro:generateContent", map[string]interface{}{
		"systemInstruction": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": "stay focused"}}},
		"generationConfig":  map[string]interface{}{"temperature": 0.2},
	})
	h.captureRequestAuth("gem-live-key", "")

	replay, ok := h.LaneSessionReplay("conv-replay")
	if !ok {
		t.Fatal("expected replay to exist")
	}
	if replay.RequestURI != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("unexpected request URI %q", replay.RequestURI)
	}
	if len(replay.LiveContents) != 2 {
		t.Fatalf("expected 2 live contents, got %d", len(replay.LiveContents))
	}
	if !replay.CapturedAuthAvailable || replay.CapturedAuthType != "api_key" {
		t.Fatalf("expected captured api_key auth, got available=%v type=%q", replay.CapturedAuthAvailable, replay.CapturedAuthType)
	}
	if replay.Snapshot.Model != "gemini-2.5-pro" {
		t.Fatalf("expected replay snapshot model gemini-2.5-pro, got %q", replay.Snapshot.Model)
	}
}
