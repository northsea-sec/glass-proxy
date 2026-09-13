package codex

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"proxy.local/app/internal/debug"
)

type stubTelemetryRecorder struct {
	requests         []*debug.RequestEvent
	quotaWrites      []stubQuotaWrite
	quotaBackfills   []stubQuotaBackfill
	statuslineWrites int
	sessionUpdates   []string
}

type stubQuotaWrite struct {
	BindingWindow string
	Util5h        float64
	Util7d        float64
	Status5h      string
	Status7d      string
	Overall       string
}

type stubQuotaBackfill struct {
	RequestID     string
	BindingWindow string
	Util5h        float64
	Util7d        float64
	Status5h      string
	Status7d      string
	Overall       string
}

func (s *stubTelemetryRecorder) RecordRequest(evt *debug.RequestEvent) error {
	copied := *evt
	s.requests = append(s.requests, &copied)
	return nil
}

func (s *stubTelemetryRecorder) UpdateSession(sessionID, backend string) {
	s.sessionUpdates = append(s.sessionUpdates, sessionID+":"+backend)
}

func (s *stubTelemetryRecorder) RecordQuota(bindingWindow string, util5h, util7d float64, status5h, status7d, overall string) error {
	s.quotaWrites = append(s.quotaWrites, stubQuotaWrite{
		BindingWindow: bindingWindow,
		Util5h:        util5h,
		Util7d:        util7d,
		Status5h:      status5h,
		Status7d:      status7d,
		Overall:       overall,
	})
	return nil
}

func (s *stubTelemetryRecorder) UpdateRequestQuota(requestID, bindingWindow string, util5h, util7d float64, status5h, status7d, overall string) error {
	s.quotaBackfills = append(s.quotaBackfills, stubQuotaBackfill{
		RequestID:     requestID,
		BindingWindow: bindingWindow,
		Util5h:        util5h,
		Util7d:        util7d,
		Status5h:      status5h,
		Status7d:      status7d,
		Overall:       overall,
	})
	for _, req := range s.requests {
		if req.RequestID != requestID {
			continue
		}
		req.RLBindingWindow = bindingWindow
		req.RL5hUtil = util5h
		req.RL5hStatus = status5h
		req.RL7dUtil = util7d
		req.RL7dStatus = status7d
		req.RLOverall = overall
	}
	return nil
}

func (s *stubTelemetryRecorder) WriteStatuslineSnapshot() {
	s.statuslineWrites++
}

func TestServeHTTPRecordsCodexRequestEvent(t *testing.T) {
	recorder := &stubTelemetryRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model":  "gpt-5.4",
			"status": "completed",
			"usage": map[string]interface{}{
				"input_tokens":  12,
				"output_tokens": 7,
			},
			"output": []interface{}{
				map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "output_text",
							"text": "answer text",
						},
					},
				},
				map[string]interface{}{
					"type":      "function_call",
					"name":      "exec_command",
					"arguments": "{\"cmd\":\"pwd\"}",
				},
			},
		})
	}))
	defer upstream.Close()

	h := NewHandler(Config{
		Upstream:      upstream.URL,
		ShadowDir:     t.TempDir(),
		DebugRecorder: recorder,
	})

	reqBody := map[string]interface{}{
		"model":        "gpt-5.4",
		"instructions": "stay focused",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "input_text",
						"text": "hello codex",
					},
				},
			},
		},
		"stream": false,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(data))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(recorder.requests) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(recorder.requests))
	}
	evt := recorder.requests[0]
	if evt.ClassifiedBackend != "codex" {
		t.Fatalf("expected codex backend, got %q", evt.ClassifiedBackend)
	}
	if evt.ModelRequested != "gpt-5.4" || evt.ModelResponse != "gpt-5.4" {
		t.Fatalf("unexpected model telemetry: requested=%q response=%q", evt.ModelRequested, evt.ModelResponse)
	}
	if evt.InputTokens != 12 || evt.OutputTokens != 7 {
		t.Fatalf("unexpected token telemetry: input=%d output=%d", evt.InputTokens, evt.OutputTokens)
	}
	if evt.UserPreview != "hello codex" {
		t.Fatalf("expected user preview, got %q", evt.UserPreview)
	}
	if evt.OutputPreview != "answer text" {
		t.Fatalf("expected output preview, got %q", evt.OutputPreview)
	}
	if !evt.HasToolUse {
		t.Fatal("expected tool-use telemetry to be true")
	}
	if evt.StopReason != "completed" {
		t.Fatalf("expected stop reason completed, got %q", evt.StopReason)
	}
	if recorder.statuslineWrites != 1 {
		t.Fatalf("expected 1 statusline write, got %d", recorder.statuslineWrites)
	}
	if len(recorder.sessionUpdates) != 1 {
		t.Fatalf("expected 1 session update, got %d", len(recorder.sessionUpdates))
	}
}
