package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebSocketTelemetryCollectorRecordsCodexRequestEvent(t *testing.T) {
	recorder := &stubTelemetryRecorder{}
	tempDir := t.TempDir()
	runtimeRoot := filepath.Join(tempDir, "runtime")
	t.Setenv("GLASS_RUNTIME_LANE", "codex")
	t.Setenv("GLASS_RUNTIME_ROOT", runtimeRoot)

	collector := newWebSocketTelemetryCollector(recorder, newSessionStore(tempDir), tempDir, "/responses")
	start := time.Date(2026, time.April, 19, 18, 0, 0, 0, time.UTC)
	times := []time.Time{
		start,
		start.Add(50 * time.Millisecond),
		start.Add(125 * time.Millisecond),
		start.Add(300 * time.Millisecond),
	}
	collector.now = func() time.Time {
		if len(times) == 0 {
			return start.Add(500 * time.Millisecond)
		}
		next := times[0]
		times = times[1:]
		return next
	}

	collector.Observe("client_to_upstream", 1, mustJSON(t, map[string]interface{}{
		"type":         "response.create",
		"model":        "gpt-5.4",
		"instructions": "stay focused",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "hello over websocket"},
				},
			},
		},
	}))
	collector.Observe("upstream_to_client", 1, mustJSON(t, map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":     "resp_test",
			"model":  "gpt-5.4",
			"status": "in_progress",
		},
	}))
	collector.Observe("upstream_to_client", 1, mustJSON(t, map[string]interface{}{
		"type": "response.output_item.done",
		"item": map[string]interface{}{
			"id":     "msg_test",
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []interface{}{
				map[string]interface{}{"type": "output_text", "text": "WS_OK"},
			},
		},
	}))
	collector.Observe("upstream_to_client", 1, mustJSON(t, map[string]interface{}{
		"type": "response.completed",
		"response": map[string]interface{}{
			"id":     "resp_test",
			"model":  "gpt-5.4",
			"status": "completed",
			"usage": map[string]interface{}{
				"input_tokens":  12,
				"output_tokens": 3,
			},
		},
	}))

	if len(recorder.requests) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(recorder.requests))
	}
	evt := recorder.requests[0]
	if evt.ClassifiedBackend != "codex" {
		t.Fatalf("expected codex backend, got %q", evt.ClassifiedBackend)
	}
	if evt.ModelRequested != "gpt-5.4" || evt.ModelResponse != "gpt-5.4" {
		t.Fatalf("unexpected models: requested=%q response=%q", evt.ModelRequested, evt.ModelResponse)
	}
	if evt.InputTokens != 12 || evt.OutputTokens != 3 {
		t.Fatalf("unexpected token telemetry: input=%d output=%d", evt.InputTokens, evt.OutputTokens)
	}
	if evt.UserPreview != "hello over websocket" {
		t.Fatalf("expected websocket user preview, got %q", evt.UserPreview)
	}
	if evt.OutputPreview != "WS_OK" {
		t.Fatalf("expected websocket output preview, got %q", evt.OutputPreview)
	}
	if evt.StopReason != "completed" {
		t.Fatalf("expected completed stop reason, got %q", evt.StopReason)
	}
	if evt.TTFT <= 0 {
		t.Fatalf("expected TTFT > 0, got %f", evt.TTFT)
	}
	if recorder.statuslineWrites != 1 {
		t.Fatalf("expected 1 statusline write, got %d", recorder.statuslineWrites)
	}
}

func TestWebSocketTelemetryCollectorCapturesInstructions(t *testing.T) {
	recorder := &stubTelemetryRecorder{}
	tempDir := t.TempDir()
	runtimeRoot := filepath.Join(tempDir, "runtime")
	t.Setenv("GLASS_RUNTIME_LANE", "codex")
	t.Setenv("GLASS_RUNTIME_ROOT", runtimeRoot)

	collector := newWebSocketTelemetryCollector(recorder, newSessionStore(tempDir), tempDir, "/responses")
	collector.Observe("client_to_upstream", 1, mustJSON(t, map[string]interface{}{
		"type":         "response.create",
		"model":        "gpt-5.4",
		"instructions": "capture me",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "hello"},
				},
			},
		},
	}))

	convID := computeFingerprint(map[string]interface{}{
		"model":        "gpt-5.4",
		"instructions": "capture me",
	})
	shadowPromptPath := filepath.Join(tempDir, convID, "system_prompt.txt")
	if data, err := os.ReadFile(shadowPromptPath); err != nil {
		t.Fatalf("expected shadow system prompt capture at %s: %v", shadowPromptPath, err)
	} else if string(data) != "capture me" {
		t.Fatalf("unexpected shadow prompt contents %q", string(data))
	}

	livePromptPath := filepath.Join(runtimeRoot, "live", "outbound_codex_instructions.txt")
	if data, err := os.ReadFile(livePromptPath); err != nil {
		t.Fatalf("expected live instructions capture at %s: %v", livePromptPath, err)
	} else if string(data) != "capture me" {
		t.Fatalf("unexpected live instructions contents %q", string(data))
	}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}
