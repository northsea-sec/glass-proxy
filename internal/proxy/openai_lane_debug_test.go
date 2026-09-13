package proxy

import (
	"testing"
	"time"
)

func TestOpenAILaneStatusAndSessionSnapshots(t *testing.T) {
	svc := NewOpenAICompatibleLaneService("http://openai.test")

	older := svc.getSession("conv-older")
	newer := svc.getSession("conv-newer")

	olderCreated := time.Date(2026, time.April, 11, 10, 0, 0, 0, time.UTC)
	olderUpdated := olderCreated.Add(2 * time.Minute)
	newerCreated := olderCreated.Add(5 * time.Minute)
	newerUpdated := newerCreated.Add(1 * time.Minute)

	older.mu.Lock()
	older.model = "gpt-4o"
	older.messages = []openAIMsg{
		{Msg: map[string]interface{}{"role": "user", "content": "hello"}, Tokens: 7},
		{Msg: map[string]interface{}{"role": "assistant", "content": "world"}, Tokens: 5},
	}
	older.evictedCount = 2
	older.lastAPIInput = 42
	older.lastAPIInputAt = olderUpdated.Add(-30 * time.Second)
	older.createdAt = olderCreated
	older.updatedAt = olderUpdated
	older.mu.Unlock()

	newer.mu.Lock()
	newer.model = "gpt-4o-mini"
	newer.messages = []openAIMsg{
		{Msg: map[string]interface{}{"role": "user", "content": "newer"}, Tokens: 3},
	}
	newer.createdAt = newerCreated
	newer.updatedAt = newerUpdated
	newer.mu.Unlock()

	svc.ObserveAuth("Bearer sk-test")

	status := svc.AuthStatus("openai")
	if status.Lane != "openai" {
		t.Fatalf("expected openai lane, got %q", status.Lane)
	}
	if !status.AuthObserved {
		t.Fatal("expected auth observation to be recorded")
	}
	if status.LastSeenAuthType != "bearer" {
		t.Fatalf("expected bearer auth type, got %q", status.LastSeenAuthType)
	}
	if !status.CapturedAuthAvailable {
		t.Fatal("expected openai lane to report captured auth")
	}
	if status.CapturedAuthType != "bearer" {
		t.Fatalf("expected bearer captured auth type, got %q", status.CapturedAuthType)
	}
	if status.SessionPersistence != "live_memory_with_replay_template" {
		t.Fatalf("expected live_memory_with_replay_template persistence, got %q", status.SessionPersistence)
	}
	if !status.SupportsLiveSessionProbe {
		t.Fatal("expected openai lane to report live session probe support")
	}
	if status.SessionCount != 2 {
		t.Fatalf("expected 2 sessions, got %d", status.SessionCount)
	}

	snapshots := svc.Sessions()
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}
	if snapshots[0].ConvID != "conv-newer" {
		t.Fatalf("expected newest snapshot first, got %q", snapshots[0].ConvID)
	}

	snapshot, ok := svc.Session("conv-older")
	if !ok {
		t.Fatal("expected conv-older snapshot to exist")
	}
	if snapshot.TotalTokens != 12 {
		t.Fatalf("expected 12 total tokens, got %d", snapshot.TotalTokens)
	}
	if snapshot.MessageCount != 2 {
		t.Fatalf("expected 2 messages, got %d", snapshot.MessageCount)
	}
	if snapshot.EvictedCount != 2 {
		t.Fatalf("expected evicted count 2, got %d", snapshot.EvictedCount)
	}
	if snapshot.LastAPIInput != 42 {
		t.Fatalf("expected last api input 42, got %d", snapshot.LastAPIInput)
	}
	if snapshot.CreatedAt == "" || snapshot.UpdatedAt == "" || snapshot.LastAPIInputAt == "" {
		t.Fatal("expected timestamp fields to be populated")
	}
}

func TestOpenAILaneReplayIncludesTemplateMessagesAndCapturedAuth(t *testing.T) {
	svc := NewOpenAICompatibleLaneService("http://127.0.0.1:11434")

	sess := svc.getSession("conv-replay")
	sess.mu.Lock()
	sess.model = "gpt-4o"
	sess.messages = []openAIMsg{
		{Msg: map[string]interface{}{"role": "system", "content": "stay focused"}, Tokens: 5},
		{Msg: map[string]interface{}{"role": "user", "content": "hello"}, Tokens: 4},
		{Msg: map[string]interface{}{"role": "assistant", "content": "world"}, Tokens: 5},
	}
	sess.mu.Unlock()
	sess.captureReplayTemplate("/v1/chat/completions", map[string]interface{}{
		"model":       "gpt-4o",
		"temperature": 0.2,
		"stream":      true,
	})
	svc.ObserveAuth("Bearer sk-replay")

	replay, ok := svc.SessionReplay("openai", "conv-replay")
	if !ok {
		t.Fatal("expected replay to exist")
	}
	if replay.RequestURI != "/v1/chat/completions" {
		t.Fatalf("unexpected request URI %q", replay.RequestURI)
	}
	if replay.ProxyTemplate["model"] != "gpt-4o" {
		t.Fatalf("expected replay model gpt-4o, got %#v", replay.ProxyTemplate["model"])
	}
	if len(replay.LiveMessages) != 3 {
		t.Fatalf("expected 3 live messages, got %d", len(replay.LiveMessages))
	}
	if !replay.CapturedAuthAvailable || replay.CapturedAuthType != "bearer" {
		t.Fatalf("expected captured bearer auth, got available=%v type=%q", replay.CapturedAuthAvailable, replay.CapturedAuthType)
	}
	if replay.Snapshot.Model != "gpt-4o" {
		t.Fatalf("expected snapshot model gpt-4o, got %q", replay.Snapshot.Model)
	}
}

func TestOpenAICompatibleLaneServiceSupportsOllamaLabelingOverSharedImplementation(t *testing.T) {
	svc := NewOpenAICompatibleLaneService("http://127.0.0.1:11434")
	svc.ObserveAuth("Bearer sk-ollama")

	sess := svc.getSession("conv-ollama")
	sess.mu.Lock()
	sess.model = "qwen2.5"
	sess.messages = []openAIMsg{
		{Msg: map[string]interface{}{"role": "user", "content": "ping"}, Tokens: 3},
	}
	sess.mu.Unlock()
	sess.captureReplayTemplate("/v1/chat/completions", map[string]interface{}{
		"model":  "qwen2.5",
		"stream": false,
	})

	status := svc.AuthStatus("ollama")
	if status.Lane != "ollama" {
		t.Fatalf("expected ollama lane label, got %q", status.Lane)
	}
	if !status.AuthlessUpstreamLikely {
		t.Fatal("expected local upstream to be treated as likely authless")
	}

	replay, ok := svc.SessionReplay("ollama", "conv-ollama")
	if !ok {
		t.Fatal("expected ollama replay to exist")
	}
	if replay.Lane != "ollama" {
		t.Fatalf("expected ollama replay lane, got %q", replay.Lane)
	}
	if replay.ProxyTemplate["model"] != "qwen2.5" {
		t.Fatalf("unexpected replay model %#v", replay.ProxyTemplate["model"])
	}
}

func TestOpenAILaneStatusIsNilSafe(t *testing.T) {
	var svc *OpenAICompatibleLaneService

	status := svc.AuthStatus("openai")
	if status.Lane != "openai" {
		t.Fatalf("expected openai lane, got %q", status.Lane)
	}
	if status.SessionCount != 0 {
		t.Fatalf("expected zero sessions, got %d", status.SessionCount)
	}
	if status.LastSeenAuthType != "unknown" {
		t.Fatalf("expected unknown auth type, got %q", status.LastSeenAuthType)
	}
	if sessions := svc.Sessions(); sessions != nil {
		t.Fatalf("expected nil sessions slice, got %#v", sessions)
	}
	if _, ok := svc.Session("missing"); ok {
		t.Fatal("expected missing session lookup to return false")
	}
	if _, ok := svc.SessionReplay("openai", "missing"); ok {
		t.Fatal("expected missing replay lookup to return false")
	}
}
