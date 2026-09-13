package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"proxy.local/app/internal/debug"
)

type stubOpenAIDebugRecorder struct {
	requests         []*debug.RequestEvent
	statuslineWrites int
	sessionUpdates   []string
}

func (s *stubOpenAIDebugRecorder) RecordRequest(evt *debug.RequestEvent) error {
	copied := *evt
	s.requests = append(s.requests, &copied)
	return nil
}

func (s *stubOpenAIDebugRecorder) RecordSubagentEvent(*debug.SubagentEvent) error {
	return nil
}

func (s *stubOpenAIDebugRecorder) RecordDedupEvent(*debug.DedupEvent) error {
	return nil
}

func (s *stubOpenAIDebugRecorder) UpdateSession(sessionID, backend string) {
	s.sessionUpdates = append(s.sessionUpdates, sessionID+":"+backend)
}

func (s *stubOpenAIDebugRecorder) RecordQuota(string, float64, float64, string, string, string) error {
	return nil
}

func (s *stubOpenAIDebugRecorder) WriteStatuslineSnapshot() {
	s.statuslineWrites++
}

func TestOpenAINonStreamingRecordsRequestEvent(t *testing.T) {
	recorder := &stubOpenAIDebugRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "chatcmpl-1",
			"model": "gpt-4o",
			"usage": map[string]interface{}{
				"prompt_tokens":     18,
				"completion_tokens": 6,
				"prompt_tokens_details": map[string]interface{}{
					"cached_tokens": 4,
				},
			},
			"choices": []interface{}{
				map[string]interface{}{
					"index":         0,
					"finish_reason": "tool_calls",
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "openai answer",
						"tool_calls": []interface{}{
							map[string]interface{}{
								"id":   "call_1",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "exec_command",
									"arguments": "{\"cmd\":\"pwd\"}",
								},
							},
						},
					},
				},
			},
		})
	}))
	defer upstream.Close()

	proxy := newTestOpenAIProxy(t, upstream.URL, 50)
	proxy.debugRecorder = recorder
	server := httptest.NewServer(proxy)
	defer server.Close()

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "stay focused"},
			map[string]interface{}{"role": "user", "content": "hello openai"},
		},
		"stream": false,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(recorder.requests) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(recorder.requests))
	}
	evt := recorder.requests[0]
	if evt.RequestLane != "openai" || evt.RequestTransport != "openai_chat" {
		t.Fatalf("unexpected lane metadata: %+v", evt)
	}
	if evt.ClassifiedBackend != "openai" {
		t.Fatalf("expected openai backend, got %q", evt.ClassifiedBackend)
	}
	if evt.ModelRequested != "gpt-4o" || evt.ModelResponse != "gpt-4o" {
		t.Fatalf("unexpected model telemetry: requested=%q response=%q", evt.ModelRequested, evt.ModelResponse)
	}
	if evt.InputTokens != 18 || evt.OutputTokens != 6 || evt.CacheReadTokens != 4 {
		t.Fatalf("unexpected token telemetry: input=%d output=%d cache=%d", evt.InputTokens, evt.OutputTokens, evt.CacheReadTokens)
	}
	if evt.UserPreview != "hello openai" {
		t.Fatalf("expected user preview, got %q", evt.UserPreview)
	}
	if evt.OutputPreview == "" {
		t.Fatal("expected output preview")
	}
	if !evt.HasToolUse {
		t.Fatal("expected tool-use telemetry to be true")
	}
	if evt.StopReason != "tool_calls" {
		t.Fatalf("expected stop reason tool_calls, got %q", evt.StopReason)
	}
	if recorder.statuslineWrites != 1 {
		t.Fatalf("expected 1 statusline write, got %d", recorder.statuslineWrites)
	}
	if len(recorder.sessionUpdates) != 1 {
		t.Fatalf("expected 1 session update, got %d", len(recorder.sessionUpdates))
	}
}
