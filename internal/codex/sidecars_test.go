package codex

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServeHTTPWritesCodexSidecarFiles(t *testing.T) {
	recorder := &stubTelemetryRecorder{}
	runtimeRoot := t.TempDir()
	t.Setenv("GLASS_RUNTIME_LANE", "codex")
	t.Setenv("GLASS_RUNTIME_ROOT", runtimeRoot)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Codex-Plan-Type", "pro")
		w.Header().Set("X-Codex-Primary-Used-Percent", "26")
		w.Header().Set("X-Codex-Primary-Window-Minutes", "300")
		w.Header().Set("X-Codex-Secondary-Used-Percent", "30")
		w.Header().Set("X-Codex-Secondary-Window-Minutes", "10080")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model":  "gpt-5.4",
			"status": "completed",
			"usage": map[string]interface{}{
				"input_tokens":  25,
				"output_tokens": 8,
				"input_tokens_details": map[string]interface{}{
					"cached_tokens": 11,
				},
				"output_tokens_details": map[string]interface{}{
					"reasoning_tokens": 2,
				},
			},
			"output": []interface{}{
				map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "output_text",
							"text": "SIDECAR_OK",
						},
					},
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
						"text": "write sidecars",
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

	convID := computeFingerprint(reqBody)
	bridgePath := filepath.Join(runtimeRoot, "api_usage_shared.json")
	bridgeData := map[string]map[string]interface{}{}
	readJSONFile(t, bridgePath, &bridgeData)
	entry := bridgeData["conv_"+convID]
	if entry == nil {
		t.Fatalf("expected usage entry for conv_%s in %s", convID, bridgePath)
	}
	if int(entry["input_tokens"].(float64)) != 25 || int(entry["output_tokens"].(float64)) != 8 {
		t.Fatalf("unexpected usage bridge entry: %#v", entry)
	}
	if int(entry["cache_read_input_tokens"].(float64)) != 11 {
		t.Fatalf("expected cached token count in usage bridge, got %#v", entry)
	}

	var samples []map[string]interface{}
	readJSONFile(t, filepath.Join(runtimeRoot, "glass_quota_samples.json"), &samples)
	if len(samples) != 1 {
		t.Fatalf("expected 1 quota sample, got %d", len(samples))
	}
	if samples[0]["bind"] != "seven_day" {
		t.Fatalf("expected seven_day binding window, got %#v", samples[0]["bind"])
	}

	burn := map[string]interface{}{}
	readJSONFile(t, filepath.Join(runtimeRoot, "burn_telemetry_health.json"), &burn)
	if burn["headers_present"] != true {
		t.Fatalf("expected burn headers_present=true, got %#v", burn)
	}
	if burn["status"] != "allowed" {
		t.Fatalf("expected burn status allowed, got %#v", burn)
	}

	interleaving := map[string]interface{}{}
	readJSONFile(t, filepath.Join(runtimeRoot, "interleaving_health.json"), &interleaving)
	if int(interleaving["active_inflight"].(float64)) != 1 {
		t.Fatalf("expected active_inflight=1, got %#v", interleaving)
	}

	if len(recorder.quotaWrites) != 1 {
		t.Fatalf("expected 1 quota DB write, got %d", len(recorder.quotaWrites))
	}
	if recorder.requests[0].CacheReadTokens != 11 {
		t.Fatalf("expected request event cache_read_tokens=11, got %d", recorder.requests[0].CacheReadTokens)
	}
	if recorder.requests[0].RLOverall != "allowed" {
		t.Fatalf("expected request event overall quota status allowed, got %q", recorder.requests[0].RLOverall)
	}
}

func TestWebSocketTelemetryCollectorWritesQuotaSidecarsFromRateLimitEvents(t *testing.T) {
	recorder := &stubTelemetryRecorder{}
	tempDir := t.TempDir()
	runtimeRoot := filepath.Join(tempDir, "runtime")
	t.Setenv("GLASS_RUNTIME_LANE", "codex")
	t.Setenv("GLASS_RUNTIME_ROOT", runtimeRoot)

	collector := newWebSocketTelemetryCollector(recorder, newSessionStore(tempDir), tempDir, "/responses")
	start := time.Date(2026, time.April, 19, 20, 0, 0, 0, time.UTC)
	times := []time.Time{
		start,
		start.Add(50 * time.Millisecond),
		start.Add(125 * time.Millisecond),
		start.Add(200 * time.Millisecond),
		start.Add(250 * time.Millisecond),
	}
	collector.now = func() time.Time {
		if len(times) == 0 {
			return start.Add(300 * time.Millisecond)
		}
		next := times[0]
		times = times[1:]
		return next
	}

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
	collector.Observe("upstream_to_client", 1, mustJSON(t, map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":     "resp_test",
			"model":  "gpt-5.4",
			"status": "in_progress",
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
				"input_tokens_details": map[string]interface{}{
					"cached_tokens": 9,
				},
			},
		},
	}))
	collector.Observe("upstream_to_client", 1, mustJSON(t, map[string]interface{}{
		"type":      "codex.rate_limits",
		"plan_type": "pro",
		"rate_limits": map[string]interface{}{
			"allowed":       true,
			"limit_reached": false,
			"primary": map[string]interface{}{
				"used_percent":   27,
				"window_minutes": 300,
			},
			"secondary": map[string]interface{}{
				"used_percent":   31,
				"window_minutes": 10080,
			},
		},
	}))

	convID := computeFingerprint(map[string]interface{}{
		"model":        "gpt-5.4",
		"instructions": "capture me",
	})
	bridgePath := filepath.Join(runtimeRoot, "api_usage_shared.json")
	bridgeData := map[string]map[string]interface{}{}
	readJSONFile(t, bridgePath, &bridgeData)
	entry := bridgeData["conv_"+convID]
	if entry == nil {
		t.Fatalf("expected websocket usage entry for conv_%s in %s", convID, bridgePath)
	}
	if int(entry["cache_read_input_tokens"].(float64)) != 9 {
		t.Fatalf("expected websocket cached token count, got %#v", entry)
	}

	var samples []map[string]interface{}
	readJSONFile(t, filepath.Join(runtimeRoot, "glass_quota_samples.json"), &samples)
	if len(samples) != 1 {
		t.Fatalf("expected 1 websocket quota sample, got %d", len(samples))
	}
	if samples[0]["status"] != "allowed" {
		t.Fatalf("expected websocket quota sample status allowed, got %#v", samples[0])
	}

	burn := map[string]interface{}{}
	readJSONFile(t, filepath.Join(runtimeRoot, "burn_telemetry_health.json"), &burn)
	if burn["headers_present"] != true {
		t.Fatalf("expected websocket burn headers_present=true, got %#v", burn)
	}

	if len(recorder.quotaWrites) != 1 {
		t.Fatalf("expected 1 websocket quota DB write, got %d", len(recorder.quotaWrites))
	}
	if len(recorder.quotaBackfills) != 1 {
		t.Fatalf("expected 1 websocket request quota backfill, got %d", len(recorder.quotaBackfills))
	}
	if recorder.requests[0].RLOverall != "allowed" || recorder.requests[0].RLBindingWindow != "seven_day" {
		t.Fatalf("expected late quota backfill on request row, got %+v", recorder.requests[0])
	}
	if recorder.statuslineWrites != 2 {
		t.Fatalf("expected statusline snapshot rewrite after late quota backfill, got %d", recorder.statuslineWrites)
	}
}

func readJSONFile(t *testing.T, path string, target interface{}) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}
