package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"proxy.local/app/internal/config"
)

func TestServeHTTPRoutesCodexWebSocketUpgradeToLane(t *testing.T) {
	cfgPath := writeProxyTestConfig(t)

	called := false
	p := &Proxy{
		configLoader: config.NewLoader(cfgPath),
		codexHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "/responses", nil)
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected websocket GET /responses to route to the Codex lane")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestServeHTTPRoutesCodexResponsesPostsToLane(t *testing.T) {
	cfgPath := writeProxyTestConfig(t)

	called := false
	p := &Proxy{
		configLoader: config.NewLoader(cfgPath),
		codexHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusAccepted)
		}),
	}

	body := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected POST /responses to route to the Codex lane")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestServeHTTPRoutesCodexCompactPostsToLane(t *testing.T) {
	cfgPath := writeProxyTestConfig(t)

	called := false
	p := &Proxy{
		configLoader: config.NewLoader(cfgPath),
		codexHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusCreated)
		}),
	}

	body := []byte(`{"response_id":"resp_test","target_token_limit":500}`)
	req := httptest.NewRequest(http.MethodPost, "/responses/compact", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected POST /responses/compact to route to the Codex lane")
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestJoinURLPath(t *testing.T) {
	tests := []struct {
		base string
		req  string
		want string
	}{
		{base: "/backend-api/codex", req: "/responses", want: "/backend-api/codex/responses"},
		{base: "/backend-api/codex/", req: "/responses", want: "/backend-api/codex/responses"},
		{base: "/backend-api/codex", req: "responses", want: "/backend-api/codex/responses"},
	}

	for _, tt := range tests {
		if got := joinURLPath(tt.base, tt.req); got != tt.want {
			t.Fatalf("joinURLPath(%q, %q) = %q, want %q", tt.base, tt.req, got, tt.want)
		}
	}
}

func writeProxyTestConfig(t *testing.T) string {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), "glass_config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"enabled":true}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
