package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	claudelane "proxy.local/app/internal/claude"
	"proxy.local/app/internal/codex"
	geminilane "proxy.local/app/internal/gemini"
	"proxy.local/app/internal/proxy"
)

func TestObservedLaneModelsDeduplicateAndSkipEmpty(t *testing.T) {
	claudeModels := observedClaudeLaneModels([]claudelane.LaneSessionSnapshot{
		{Model: "claude-opus-4-1"},
		{Model: "claude-opus-4-1"},
		{Model: ""},
		{Model: "claude-sonnet-4-5"},
	})
	if len(claudeModels) != 2 {
		t.Fatalf("expected 2 Claude models, got %#v", claudeModels)
	}
	if claudeModels[0]["id"] != "claude-opus-4-1" || claudeModels[1]["id"] != "claude-sonnet-4-5" {
		t.Fatalf("unexpected Claude model order %#v", claudeModels)
	}

	openAIModels := observedOpenAILaneModels([]proxy.OpenAILaneSessionSnapshot{
		{Model: "gpt-4o"},
		{Model: "gpt-4o"},
		{Model: "gpt-5.4"},
	})
	if len(openAIModels) != 2 {
		t.Fatalf("expected 2 OpenAI models, got %#v", openAIModels)
	}

	codexModels := observedCodexLaneModels([]codex.LaneSessionSnapshot{
		{Model: "gpt-5.4"},
		{Model: ""},
		{Model: "gpt-5.4-mini"},
	})
	if len(codexModels) != 2 {
		t.Fatalf("expected 2 Codex models, got %#v", codexModels)
	}

	geminiModels := observedGeminiLaneModels([]geminilane.LaneSessionSnapshot{
		{Model: "gemini-2.5-pro"},
		{Model: "gemini-2.5-pro"},
	})
	if len(geminiModels) != 1 {
		t.Fatalf("expected 1 Gemini model, got %#v", geminiModels)
	}
	if geminiModels[0]["source"] != "observed_session" {
		t.Fatalf("expected observed_session source, got %#v", geminiModels[0])
	}
}

func TestCanonicalLaneName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantFound bool
	}{
		{name: "anthropic alias", input: "anthropic", want: "claude", wantFound: true},
		{name: "claude", input: "claude", want: "claude", wantFound: true},
		{name: "trimmed openai", input: " openai ", want: "openai", wantFound: true},
		{name: "codex", input: "codex", want: "codex", wantFound: true},
		{name: "gemini", input: "gemini", want: "gemini", wantFound: true},
		{name: "ollama", input: "ollama", want: "ollama", wantFound: true},
		{name: "unknown", input: "local", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := canonicalLaneName(tt.input)
			if found != tt.wantFound {
				t.Fatalf("found=%v want %v", found, tt.wantFound)
			}
			if got != tt.want {
				t.Fatalf("canonical lane=%q want %q", got, tt.want)
			}
		})
	}
}

func TestLookupLaneDebugAdapterCanonicalizesAnthropic(t *testing.T) {
	adapters := map[string]laneDebugAdapter{
		"claude": {lane: "claude"},
		"openai": {lane: "openai"},
		"ollama": {lane: "ollama"},
	}

	adapter, ok := lookupLaneDebugAdapter(adapters, "anthropic")
	if !ok {
		t.Fatal("expected anthropic alias to resolve")
	}
	if adapter.lane != "claude" {
		t.Fatalf("resolved lane=%q want claude", adapter.lane)
	}

	if _, ok := lookupLaneDebugAdapter(adapters, "missing"); ok {
		t.Fatal("expected unknown lane lookup to fail")
	}

	adapter, ok = lookupLaneDebugAdapter(adapters, "ollama")
	if !ok {
		t.Fatal("expected ollama lane to resolve")
	}
	if adapter.lane != "ollama" {
		t.Fatalf("resolved lane=%q want ollama", adapter.lane)
	}
}

func TestForwardLaneRequestAppliesAuthAndCopiesResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization=%q want Bearer test-token", got)
		}
		if got := r.RemoteAddr; got != "127.0.0.1:18888" {
			t.Fatalf("remote addr=%q want 127.0.0.1:18888", got)
		}
		w.Header().Add("X-Debug", "lane")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	response, status, err := forwardLaneRequest(
		handler,
		"127.0.0.1:18888",
		"/v1/chat/completions",
		json.RawMessage(`{"model":"gpt-5.4"}`),
		func(req *http.Request) error {
			req.Header.Set("Authorization", "Bearer test-token")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("forwardLaneRequest returned error: %v", err)
	}
	if status != 0 {
		t.Fatalf("forwardLaneRequest status=%d want 0 on success", status)
	}
	if response.status != http.StatusCreated {
		t.Fatalf("response status=%d want %d", response.status, http.StatusCreated)
	}
	if got := response.header.Get("X-Debug"); got != "lane" {
		t.Fatalf("response header X-Debug=%q want lane", got)
	}
	if string(response.body) != `{"ok":true}` {
		t.Fatalf("response body=%q want {\"ok\":true}", string(response.body))
	}
}

func TestForwardLaneRequestSurfacesAuthError(t *testing.T) {
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	response, status, err := forwardLaneRequest(
		handler,
		"127.0.0.1:18888",
		"/v1/chat/completions",
		nil,
		func(req *http.Request) error {
			return errors.New("missing captured auth")
		},
	)
	if err == nil {
		t.Fatal("expected auth error")
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want %d", status, http.StatusServiceUnavailable)
	}
	if response != nil {
		t.Fatalf("expected nil response, got %#v", response)
	}
	if handlerCalled {
		t.Fatal("handler should not be called when auth application fails")
	}
}

func TestWriteLaneForwardResponseCopiesHeadersAndBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeLaneForwardResponse(recorder, &laneForwardResponse{
		status: http.StatusAccepted,
		header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Trace":      []string{"abc123"},
		},
		body: []byte(`{"queued":true}`),
	})

	result := recorder.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d want %d", result.StatusCode, http.StatusAccepted)
	}
	if got := result.Header.Get("X-Trace"); got != "abc123" {
		t.Fatalf("X-Trace=%q want abc123", got)
	}
	if got := recorder.Body.String(); got != `{"queued":true}` {
		t.Fatalf("body=%q want {\"queued\":true}", got)
	}
}

func TestBuildProxyModePayloadUsesDefaultAndLaneScopedFieldsAndDropsLegacyAliases(t *testing.T) {
	payload := buildProxyModePayload(
		"default",
		"https://api.anthropic.com",
		"proxy.example",
		"api.anthropic.com",
		"https://api.anthropic.com",
		"api.anthropic.com",
		"api.anthropic.com",
		"https://api.openai.com",
		"https://generativelanguage.googleapis.com",
		"http://127.0.0.1:8080",
		true,
		false,
	)

	if got := payload["claude_prefix_warmer_enabled"]; got != true {
		t.Fatalf("claude_prefix_warmer_enabled=%v want true", got)
	}
	if got := payload["claude_rolling_summarizer_enabled"]; got != false {
		t.Fatalf("claude_rolling_summarizer_enabled=%v want false", got)
	}
	if got := payload["default_upstream"]; got != "https://api.anthropic.com" {
		t.Fatalf("default_upstream=%v want https://api.anthropic.com", got)
	}
	if got := payload["default_proxy_domain"]; got != "proxy.example" {
		t.Fatalf("default_proxy_domain=%v want proxy.example", got)
	}
	if got := payload["default_upstream_domain"]; got != "api.anthropic.com" {
		t.Fatalf("default_upstream_domain=%v want api.anthropic.com", got)
	}
	if got := payload["anthropic_messages_upstream"]; got != "https://api.anthropic.com" {
		t.Fatalf("anthropic_messages_upstream=%v want https://api.anthropic.com", got)
	}
	if got := payload["anthropic_messages_proxy_domain"]; got != "api.anthropic.com" {
		t.Fatalf("anthropic_messages_proxy_domain=%v want api.anthropic.com", got)
	}
	if got := payload["anthropic_messages_upstream_domain"]; got != "api.anthropic.com" {
		t.Fatalf("anthropic_messages_upstream_domain=%v want api.anthropic.com", got)
	}
	if got := payload["codex_upstream"]; got != "https://api.openai.com" {
		t.Fatalf("codex_upstream=%v want https://api.openai.com", got)
	}
	if got := payload["gemini_upstream"]; got != "https://generativelanguage.googleapis.com" {
		t.Fatalf("gemini_upstream=%v want https://generativelanguage.googleapis.com", got)
	}
	if got := payload["openai_compatible_upstream"]; got != "http://127.0.0.1:8080" {
		t.Fatalf("openai_compatible_upstream=%v want http://127.0.0.1:8080", got)
	}
	if _, ok := payload["upstream"]; ok {
		t.Fatalf("unexpected generic upstream key in payload: %#v", payload)
	}
	if _, ok := payload["proxy_domain"]; ok {
		t.Fatalf("unexpected generic proxy_domain key in payload: %#v", payload)
	}
	if _, ok := payload["upstream_domain"]; ok {
		t.Fatalf("unexpected generic upstream_domain key in payload: %#v", payload)
	}
	if _, ok := payload["anthropic_upstream"]; ok {
		t.Fatalf("unexpected anthropic_upstream key in payload: %#v", payload)
	}
	if _, ok := payload["anthropic_proxy_domain"]; ok {
		t.Fatalf("unexpected anthropic_proxy_domain key in payload: %#v", payload)
	}
	if _, ok := payload["anthropic_upstream_domain"]; ok {
		t.Fatalf("unexpected anthropic_upstream_domain key in payload: %#v", payload)
	}
	if _, ok := payload["shared_prefix_warmer_enabled"]; ok {
		t.Fatalf("unexpected shared_prefix_warmer_enabled alias in payload: %#v", payload)
	}
	if _, ok := payload["shared_rolling_summarizer_enabled"]; ok {
		t.Fatalf("unexpected shared_rolling_summarizer_enabled alias in payload: %#v", payload)
	}
}

func TestResolveProxyStartupTargetsSeparatesAnthropicLaneFromCodexDefaultCore(t *testing.T) {
	targets := resolveProxyStartupTargets("codex", "https://api.anthropic.com", "", "https://api.openai.com")

	if targets.defaultUpstreamURL != "https://api.openai.com" {
		t.Fatalf("defaultUpstreamURL=%q want https://api.openai.com", targets.defaultUpstreamURL)
	}
	if targets.defaultProxyDomain != "api.openai.com" {
		t.Fatalf("defaultProxyDomain=%q want api.openai.com", targets.defaultProxyDomain)
	}
	if targets.defaultUpstreamDomain != "api.openai.com" {
		t.Fatalf("defaultUpstreamDomain=%q want api.openai.com", targets.defaultUpstreamDomain)
	}
	if targets.anthropicMessagesUpstreamURL != "https://api.anthropic.com" {
		t.Fatalf("anthropicMessagesUpstreamURL=%q want https://api.anthropic.com", targets.anthropicMessagesUpstreamURL)
	}
	if targets.anthropicMessagesProxyDomain != "api.anthropic.com" {
		t.Fatalf("anthropicMessagesProxyDomain=%q want api.anthropic.com", targets.anthropicMessagesProxyDomain)
	}
	if targets.anthropicMessagesUpstreamDomain != "api.anthropic.com" {
		t.Fatalf("anthropicMessagesUpstreamDomain=%q want api.anthropic.com", targets.anthropicMessagesUpstreamDomain)
	}
}

func TestResolveProxyStartupTargetsRespectsExplicitClaudeUpstreamOverride(t *testing.T) {
	targets := resolveProxyStartupTargets("codex", "https://api.openai.com", "https://claude-upstream.example", "https://api.openai.com")

	if targets.defaultUpstreamURL != "https://api.openai.com" {
		t.Fatalf("defaultUpstreamURL=%q want https://api.openai.com", targets.defaultUpstreamURL)
	}
	if targets.anthropicMessagesUpstreamURL != "https://claude-upstream.example" {
		t.Fatalf("anthropicMessagesUpstreamURL=%q want https://claude-upstream.example", targets.anthropicMessagesUpstreamURL)
	}
	if targets.anthropicMessagesProxyDomain != "api.anthropic.com" {
		t.Fatalf("anthropicMessagesProxyDomain=%q want api.anthropic.com", targets.anthropicMessagesProxyDomain)
	}
	if targets.anthropicMessagesUpstreamDomain != "api.anthropic.com" {
		t.Fatalf("anthropicMessagesUpstreamDomain=%q want api.anthropic.com", targets.anthropicMessagesUpstreamDomain)
	}
}

func TestResolveProxyStartupTargetsDefaultModeKeepsClaudeOnConfiguredDefaultUpstream(t *testing.T) {
	targets := resolveProxyStartupTargets("default", "https://anthropic-relay.example", "", "https://api.openai.com")

	if targets.defaultUpstreamURL != "https://anthropic-relay.example" {
		t.Fatalf("defaultUpstreamURL=%q want https://anthropic-relay.example", targets.defaultUpstreamURL)
	}
	if targets.anthropicMessagesUpstreamURL != "https://anthropic-relay.example" {
		t.Fatalf("anthropicMessagesUpstreamURL=%q want https://anthropic-relay.example", targets.anthropicMessagesUpstreamURL)
	}
	if targets.defaultProxyDomain != "api.anthropic.com" || targets.defaultUpstreamDomain != "api.anthropic.com" {
		t.Fatalf("unexpected default domains: %+v", targets)
	}
	if targets.anthropicMessagesProxyDomain != "api.anthropic.com" || targets.anthropicMessagesUpstreamDomain != "api.anthropic.com" {
		t.Fatalf("unexpected anthropic domains: %+v", targets)
	}
}

func TestHandleDebugLaneAuthStatusPrefersLegacyAliasPayloadForFixedLane(t *testing.T) {
	adapters := map[string]laneDebugAdapter{
		"claude": {
			lane:       "claude",
			authStatus: func() interface{} { return map[string]string{"mode": "generic"} },
			legacyAuthStatus: func() (int, interface{}) {
				return http.StatusServiceUnavailable, map[string]string{"error": "rolling summarizer unavailable"}
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/debug/claude-auth-status", nil)
	recorder := httptest.NewRecorder()
	handleDebugLaneAuthStatus(recorder, req, adapters, "claude")

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode auth status payload: %v", err)
	}
	if payload["error"] != "rolling summarizer unavailable" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestHandleDebugLaneModelsUsesGenericPayloadForLaneRoute(t *testing.T) {
	adapters := map[string]laneDebugAdapter{
		"claude": {
			lane: "claude",
			models: func() (int, interface{}) {
				return http.StatusOK, map[string]interface{}{"lane": "claude", "source": "observed_session"}
			},
			legacyModels: func() (int, interface{}) {
				return http.StatusServiceUnavailable, map[string]string{"error": "legacy only"}
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/debug/lane-models?lane=anthropic", nil)
	recorder := httptest.NewRecorder()
	handleDebugLaneModels(recorder, req, adapters, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want %d", recorder.Code, http.StatusOK)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models payload: %v", err)
	}
	if payload["source"] != "observed_session" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestHandleDebugLaneModelsPrefersLegacyAliasPayloadForFixedLane(t *testing.T) {
	adapters := map[string]laneDebugAdapter{
		"claude": {
			lane: "claude",
			models: func() (int, interface{}) {
				return http.StatusOK, map[string]interface{}{"lane": "claude", "source": "observed_session"}
			},
			legacyModels: func() (int, interface{}) {
				return http.StatusServiceUnavailable, map[string]string{"error": "rolling summarizer unavailable"}
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/debug/claude-models", nil)
	recorder := httptest.NewRecorder()
	handleDebugLaneModels(recorder, req, adapters, "claude")

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode legacy models payload: %v", err)
	}
	if payload["error"] != "rolling summarizer unavailable" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestHandleDebugLaneForwardUsesFixedLaneAliasWithoutLaneField(t *testing.T) {
	called := false
	adapters := map[string]laneDebugAdapter{
		"claude": {
			lane: "claude",
			forward: func(remoteAddr, path string, body json.RawMessage) (*laneForwardResponse, int, error) {
				called = true
				if path != "/v1/messages" {
					t.Fatalf("path=%q want /v1/messages", path)
				}
				if string(body) != `{"model":"claude-opus-4-1"}` {
					t.Fatalf("body=%q", string(body))
				}
				if remoteAddr != "192.0.2.10:18888" {
					t.Fatalf("remoteAddr=%q", remoteAddr)
				}
				return &laneForwardResponse{
					status: http.StatusOK,
					header: http.Header{"Content-Type": []string{"application/json"}},
					body:   []byte(`{"ok":true}`),
				}, 0, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/debug/claude-forward", strings.NewReader(`{"path":"/v1/messages","body":{"model":"claude-opus-4-1"}}`))
	req.RemoteAddr = "192.0.2.10:18888"
	recorder := httptest.NewRecorder()
	handleDebugLaneForward(recorder, req, adapters, "claude")

	if !called {
		t.Fatal("expected fixed-lane forward adapter to be called")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != `{"ok":true}` {
		t.Fatalf("body=%q want {\"ok\":true}", got)
	}
}
