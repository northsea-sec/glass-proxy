package codex

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"proxy.local/app/internal/replay"
)

func TestServeHTTPCapturesCodexFixtureWhenReplayCaptureEnabled(t *testing.T) {
	t.Setenv(replay.CaptureEnvVar, t.TempDir())

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"usage": map[string]interface{}{
				"input_tokens": 12,
			},
			"output": []interface{}{
				map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "output_text",
							"text": "ok",
						},
					},
				},
			},
		})
	}))
	defer upstream.Close()

	h := NewHandler(Config{
		Upstream:  upstream.URL,
		ShadowDir: t.TempDir(),
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
						"text": "hello",
					},
				},
			},
		},
		"stream": false,
		"context_management": map[string]interface{}{
			"edits": []interface{}{},
		},
		"previous_response_id": "resp_123",
		"store":                true,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer sk-test")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	h.Close()

	fixtures, err := replay.LoadFixtures(getReplayCaptureDir(t))
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	if len(fixtures) != 1 {
		t.Fatalf("expected 1 fixture, got %d", len(fixtures))
	}

	fixture := fixtures[0]
	if fixture.RequestURI != "/v1/responses" {
		t.Fatalf("RequestURI = %q, want /v1/responses", fixture.RequestURI)
	}
	if len(fixture.BodyRaw) == 0 || len(fixture.BodyPreGlass) == 0 || len(fixture.BodyFinal) == 0 {
		t.Fatalf("expected raw/pre/final bodies to be captured: raw=%d pre=%d final=%d", len(fixture.BodyRaw), len(fixture.BodyPreGlass), len(fixture.BodyFinal))
	}

	var pre map[string]interface{}
	if err := json.Unmarshal(fixture.BodyPreGlass, &pre); err != nil {
		t.Fatalf("unmarshal pre-glass body: %v", err)
	}
	if _, ok := pre["context_management"]; !ok {
		t.Fatal("expected pre-glass body to retain context_management")
	}
	if _, ok := pre["previous_response_id"]; !ok {
		t.Fatal("expected pre-glass body to retain previous_response_id")
	}

	var final map[string]interface{}
	if err := json.Unmarshal(fixture.BodyFinal, &final); err != nil {
		t.Fatalf("unmarshal final body: %v", err)
	}
	if _, ok := final["context_management"]; ok {
		t.Fatal("expected final body to strip context_management")
	}
	if _, ok := final["previous_response_id"]; ok {
		t.Fatal("expected final body to strip previous_response_id")
	}
	if got, _ := final["store"].(bool); got {
		t.Fatal("expected final body to force store=false")
	}
}

func TestServeHTTPForwardsCodexCompactRequestAndCapturesFixture(t *testing.T) {
	t.Setenv(replay.CaptureEnvVar, t.TempDir())

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses/compact" {
			t.Fatalf("unexpected upstream compact path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	h := NewHandler(Config{
		Upstream:  upstream.URL,
		ShadowDir: t.TempDir(),
	})

	body := []byte(`{"response_id":"resp_test","target_token_limit":500}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-test")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ServeHTTP compact status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Fatalf("compact response body = %q", got)
	}

	h.Close()

	fixtures, err := replay.LoadFixtures(getReplayCaptureDir(t))
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	if len(fixtures) != 1 {
		t.Fatalf("expected 1 fixture, got %d", len(fixtures))
	}
	fixture := fixtures[0]
	if fixture.RequestURI != "/v1/responses/compact" {
		t.Fatalf("RequestURI = %q, want /v1/responses/compact", fixture.RequestURI)
	}
	want := map[string]interface{}{}
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatalf("unmarshal expected body: %v", err)
	}
	for label, raw := range map[string][]byte{
		"raw":       fixture.BodyRaw,
		"pre_glass": fixture.BodyPreGlass,
		"final":     fixture.BodyFinal,
	} {
		got := map[string]interface{}{}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal %s fixture body: %v", label, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s fixture body mismatch: got=%v want=%v", label, got, want)
		}
	}
}

func getReplayCaptureDir(t *testing.T) string {
	t.Helper()
	return os.Getenv(replay.CaptureEnvVar)
}
