package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"proxy.local/app/internal/config"
	"proxy.local/app/internal/glass"
)

func TestMatchLaneRouteSelectsOpenAIChatCompletions(t *testing.T) {
	cfgPath := writeProxyTestConfig(t)
	p := &Proxy{configLoader: config.NewLoader(cfgPath)}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)))
	route, ok := p.matchLaneRoute(req)
	if !ok {
		t.Fatal("expected openai lane route to match")
	}
	if route.lane != "openai" {
		t.Fatalf("expected openai lane, got %q", route.lane)
	}
}

func TestMatchLaneRouteSelectsGeminiGenerateContent(t *testing.T) {
	cfgPath := writeProxyTestConfig(t)
	p := &Proxy{
		configLoader:  config.NewLoader(cfgPath),
		geminiHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	}

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)))
	route, ok := p.matchLaneRoute(req)
	if !ok {
		t.Fatal("expected gemini lane route to match")
	}
	if route.lane != "gemini" {
		t.Fatalf("expected gemini lane, got %q", route.lane)
	}
}

func TestMatchLaneRouteSelectsClaudeMessages(t *testing.T) {
	cfgPath := writeProxyTestConfig(t)
	p := &Proxy{configLoader: config.NewLoader(cfgPath)}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}`)))
	route, ok := p.matchLaneRoute(req)
	if !ok {
		t.Fatal("expected claude lane route to match")
	}
	if route.lane != "claude" {
		t.Fatalf("expected claude lane, got %q", route.lane)
	}
}

func TestMatchLaneRouteReturnsFalseForUnknownPath(t *testing.T) {
	cfgPath := writeProxyTestConfig(t)
	p := &Proxy{configLoader: config.NewLoader(cfgPath)}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	if _, ok := p.matchLaneRoute(req); ok {
		t.Fatal("expected unknown path to fall through to reverse proxy")
	}
}

func TestClaudeMessagesLaneUsesAnthropicTargetSeparateFromDefaultReverseProxy(t *testing.T) {
	var defaultHits int
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultHits++
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer defaultUpstream.Close()

	var anthropicHits int
	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicHits++
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("anthropic lane path=%q want /v1/messages", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer anthropicUpstream.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(config.DefaultConfig)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = filepath.Join(t.TempDir(), "glass")
	glassCfg.KeepaliveIntervalSec = 0

	defaultParsed, err := url.Parse(defaultUpstream.URL)
	if err != nil {
		t.Fatalf("parse default upstream url: %v", err)
	}
	anthropicParsed, err := url.Parse(anthropicUpstream.URL)
	if err != nil {
		t.Fatalf("parse anthropic upstream url: %v", err)
	}

	p, err := New(Options{
		DefaultUpstreamURL:              defaultUpstream.URL,
		DefaultProxyDomain:              "proxy.test",
		DefaultUpstreamDomain:           defaultParsed.Host,
		AnthropicMessagesUpstreamURL:    anthropicUpstream.URL,
		AnthropicMessagesProxyDomain:    "proxy.test",
		AnthropicMessagesUpstreamDomain: anthropicParsed.Host,
		ConfigLoader:                    config.NewLoader(cfgPath),
		AllowDirectUpstream:             true,
		GlassConfig:                     &glassCfg,
	})
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}

	msgReq := httptest.NewRequest(http.MethodPost, "http://proxy.test/v1/messages", bytes.NewReader([]byte(`{"model":"claude-test","stream":false,"messages":[{"role":"user","content":"hi"}]}`)))
	msgRec := httptest.NewRecorder()
	p.ServeHTTP(msgRec, msgReq)
	if msgRec.Code != http.StatusOK {
		t.Fatalf("claude lane status=%d body=%s", msgRec.Code, msgRec.Body.String())
	}
	if anthropicHits != 1 {
		t.Fatalf("anthropic lane hits=%d want 1", anthropicHits)
	}
	if defaultHits != 0 {
		t.Fatalf("default reverse-proxy hits=%d want 0 after claude lane request", defaultHits)
	}

	fallbackReq := httptest.NewRequest(http.MethodGet, "http://proxy.test/healthz", nil)
	fallbackRec := httptest.NewRecorder()
	p.ServeHTTP(fallbackRec, fallbackReq)
	if fallbackRec.Code != http.StatusNoContent {
		t.Fatalf("fallback status=%d body=%s", fallbackRec.Code, fallbackRec.Body.String())
	}
	if defaultHits != 1 {
		t.Fatalf("default reverse-proxy hits=%d want 1 after fallback request", defaultHits)
	}
	if anthropicHits != 1 {
		t.Fatalf("anthropic lane hits=%d want 1 after fallback request", anthropicHits)
	}
}
