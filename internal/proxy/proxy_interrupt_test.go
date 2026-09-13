package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"proxy.local/app/internal/config"
	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/trimmer"
)

func TestProxyBlocksStaleNoNewMessageRequestAfterInterrupt(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.20")
		w.Header().Set("Anthropic-Ratelimit-Unified-7d-Utilization", "0.70")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Status", "allowed")
		w.Header().Set("Anthropic-Ratelimit-Unified-7d-Status", "allowed")
		w.Header().Set("Anthropic-Ratelimit-Unified-Status", "allowed")
		w.Header().Set("Anthropic-Ratelimit-Unified-Representative-Claim", "five_hour")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":0}}}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	proxy := newTestProxy(t, upstream.URL)

	baseBody := map[string]interface{}{
		"model":  "claude-test",
		"stream": true,
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "first user turn"},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "first assistant turn"},
				},
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "latest user turn"},
				},
			},
		},
	}

	convID := trimmer.SessionFingerprint(baseBody, 0)

	if rec, elapsed := performProxyRequest(t, proxy, withTestNonce(baseBody, "1")); rec.Code != http.StatusOK || elapsed > 2*time.Second {
		t.Fatalf("prime request failed code=%d elapsed=%s body=%s", rec.Code, elapsed, rec.Body.String())
	}
	proxy.activeSSE.Wait()
	time.Sleep(50 * time.Millisecond)
	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("expected first request to hit upstream once, got %d", got)
	}

	proxy.intBreaker.StreamAborted(convID)

	rec2, elapsed2 := performProxyRequest(t, proxy, withTestNonce(baseBody, "2"))
	if rec2.Code != http.StatusOK {
		t.Fatalf("stale request code=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "I'll handle this directly.") {
		t.Fatalf("expected fake end_turn body for blocked stale request, got %s", rec2.Body.String())
	}
	if elapsed2 > time.Second {
		t.Fatalf("expected stale blocked request to return quickly, took %s", elapsed2)
	}
	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("expected blocked stale request to avoid upstream, hits=%d", got)
	}

	rec3, elapsed3 := performProxyRequest(t, proxy, withTestNonce(baseBody, "3"))
	if rec3.Code != http.StatusOK {
		t.Fatalf("follow-up stale request code=%d body=%s", rec3.Code, rec3.Body.String())
	}
	if !strings.Contains(rec3.Body.String(), "I'll handle this directly.") {
		t.Fatalf("expected fake end_turn body for second blocked stale request, got %s", rec3.Body.String())
	}
	if elapsed3 > time.Second {
		t.Fatalf("expected second stale blocked request to avoid cold-gate wait, took %s", elapsed3)
	}
	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("expected follow-up stale request to avoid upstream, hits=%d", got)
	}
}

func newTestProxy(t *testing.T, upstreamURL string) *Proxy {
	t.Helper()

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

	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}

	proxy, err := New(Options{
		DefaultUpstreamURL:              upstreamURL,
		DefaultProxyDomain:              "proxy.test",
		DefaultUpstreamDomain:           parsed.Host,
		AnthropicMessagesUpstreamURL:    upstreamURL,
		AnthropicMessagesProxyDomain:    "proxy.test",
		AnthropicMessagesUpstreamDomain: parsed.Host,
		ConfigLoader:                    config.NewLoader(cfgPath),
		AllowDirectUpstream:             true,
		GlassConfig:                     &glassCfg,
	})
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	return proxy
}

func performProxyRequest(t *testing.T, proxy *Proxy, body map[string]interface{}) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.test/v1/messages", bytes.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:65535"
	rec := httptest.NewRecorder()
	start := time.Now()
	proxy.ServeHTTP(rec, req)
	return rec, time.Since(start)
}

func withTestNonce(body map[string]interface{}, nonce string) map[string]interface{} {
	clone := deepCopyBody(body)
	clone["test_nonce"] = nonce
	return clone
}

func deepCopyBody(body map[string]interface{}) map[string]interface{} {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	var clone map[string]interface{}
	if err := json.Unmarshal(data, &clone); err != nil {
		panic(err)
	}
	return clone
}
