package gemini

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxy.local/app/internal/debug"
)

type stubTelemetryRecorder struct {
	requests         []*debug.RequestEvent
	statuslineWrites int
	sessionUpdates   []string
}

func (s *stubTelemetryRecorder) RecordRequest(evt *debug.RequestEvent) error {
	copied := *evt
	s.requests = append(s.requests, &copied)
	return nil
}

func (s *stubTelemetryRecorder) UpdateSession(sessionID, backend string) {
	s.sessionUpdates = append(s.sessionUpdates, sessionID+":"+backend)
}

func (s *stubTelemetryRecorder) WriteStatuslineSnapshot() {
	s.statuslineWrites++
}

func TestServeHTTPRecordsGeminiRequestEvent(t *testing.T) {
	recorder := &stubTelemetryRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"modelVersion": "gemini-2.5-pro",
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":        21,
				"candidatesTokenCount":    8,
				"cachedContentTokenCount": 5,
			},
			"candidates": []interface{}{
				map[string]interface{}{
					"finishReason": "STOP",
					"content": map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{"text": "working"},
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "searchDocs",
									"args": map[string]interface{}{"query": "lane telemetry"},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.Upstream = upstream.URL
	cfg.ShadowDir = t.TempDir()
	cfg.DebugRecorder = recorder
	h := NewHandler(cfg)

	reqBody := map[string]interface{}{
		"systemInstruction": map[string]interface{}{
			"parts": []interface{}{map[string]interface{}{"text": "stay focused"}},
		},
		"contents": []interface{}{
			map[string]interface{}{
				"role":  "user",
				"parts": []interface{}{map[string]interface{}{"text": "hello gemini"}},
			},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=gem-test", bytes.NewReader(data))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(recorder.requests) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(recorder.requests))
	}
	evt := recorder.requests[0]
	if evt.RequestLane != "gemini" || evt.RequestTransport != "gemini_generate" {
		t.Fatalf("unexpected lane metadata: %+v", evt)
	}
	if evt.ClassifiedBackend != "gemini" {
		t.Fatalf("expected gemini backend, got %q", evt.ClassifiedBackend)
	}
	if evt.ModelRequested != "gemini-2.5-pro" || evt.ModelResponse != "gemini-2.5-pro" {
		t.Fatalf("unexpected model telemetry: requested=%q response=%q", evt.ModelRequested, evt.ModelResponse)
	}
	if evt.InputTokens != 21 || evt.OutputTokens != 8 || evt.CacheReadTokens != 5 {
		t.Fatalf("unexpected token telemetry: input=%d output=%d cache=%d", evt.InputTokens, evt.OutputTokens, evt.CacheReadTokens)
	}
	if evt.UserPreview != "hello gemini" {
		t.Fatalf("expected user preview, got %q", evt.UserPreview)
	}
	if !strings.Contains(evt.OutputPreview, "searchDocs") {
		t.Fatalf("expected output preview to mention tool call, got %q", evt.OutputPreview)
	}
	if !evt.HasToolUse {
		t.Fatal("expected tool-use telemetry to be true")
	}
	if evt.StopReason != "STOP" {
		t.Fatalf("expected stop reason STOP, got %q", evt.StopReason)
	}
	if recorder.statuslineWrites != 1 {
		t.Fatalf("expected 1 statusline write, got %d", recorder.statuslineWrites)
	}
	if len(recorder.sessionUpdates) != 1 {
		t.Fatalf("expected 1 session update, got %d", len(recorder.sessionUpdates))
	}
}
