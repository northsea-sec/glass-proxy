package glass

import (
	"strings"
	"testing"
)

func TestDebugSessionsExposePersistedStateAndMessages(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	body := map[string]interface{}{
		"model": "claude-opus-4-1",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": strings.Repeat("Follow the operator instructions carefully. ", 180)},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello from debug"},
				},
			},
		},
	}

	result := engine.Process(body, RequestMeta{
		SessionKey:  "conv-debug",
		RequestKey:  "conv-debug",
		AffinityKey: "conv-debug",
	})
	if result.SessionKey != "conv-debug" {
		t.Fatalf("expected session conv-debug, got %+v", result)
	}
	engine.UpdateAPITokens("conv-debug", 123)

	snapshots := engine.DebugSessions()
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.ConvID != "conv-debug" {
		t.Fatalf("expected conv-debug snapshot, got %+v", snapshot)
	}
	if snapshot.MessageCount != 1 {
		t.Fatalf("expected 1 retained message, got %+v", snapshot)
	}
	if snapshot.TotalTokens <= 0 {
		t.Fatalf("expected token count to be populated, got %+v", snapshot)
	}
	if snapshot.LastAPIInput != 123 {
		t.Fatalf("expected last api input 123, got %+v", snapshot)
	}
	if !snapshot.CachePersisted || !snapshot.StatePersisted {
		t.Fatalf("expected persisted cache and state, got %+v", snapshot)
	}
	if snapshot.CachePath == "" || snapshot.StatePath == "" {
		t.Fatalf("expected cache and state paths, got %+v", snapshot)
	}

	messages, ok := engine.DebugSessionMessages("conv-debug")
	if !ok {
		t.Fatal("expected live messages to be available")
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 live message, got %#v", messages)
	}
	msg, _ := messages[0].(map[string]interface{})
	if role, _ := msg["role"].(string); role != "user" {
		t.Fatalf("expected user message, got %#v", msg)
	}

	restarted := NewEngine(cfg)
	restartedSnapshot, ok := restarted.DebugSession("conv-debug")
	if !ok {
		t.Fatal("expected persisted snapshot after restart")
	}
	if restartedSnapshot.ConvID != "conv-debug" || restartedSnapshot.MessageCount != 1 {
		t.Fatalf("unexpected restarted snapshot %+v", restartedSnapshot)
	}
}
