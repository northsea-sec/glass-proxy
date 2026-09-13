package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxy.local/app/internal/config"
	"proxy.local/app/internal/glass"
)

func TestOpenAIStreamingSendsHeartbeatBeforeUpstreamResponds(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-real\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"qwen-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	proxy := newTestOpenAIProxy(t, upstream.URL, 50*time.Millisecond)
	server := httptest.NewServer(proxy)
	defer server.Close()

	resp, elapsed := postOpenAIStream(t, server.URL, `{"model":"qwen-test","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("expected early headers before upstream response, took %s", elapsed)
	}

	reader := bufio.NewReader(resp.Body)
	firstEvent := strings.Join(readSSEEvent(t, reader), "\n")
	if !strings.Contains(firstEvent, "chatcmpl-glass-heartbeat-") {
		t.Fatalf("expected heartbeat event first, got %s", firstEvent)
	}
	if !strings.Contains(firstEvent, "\"object\":\"chat.completion.chunk\"") {
		t.Fatalf("expected OpenAI chunk heartbeat, got %s", firstEvent)
	}

	sawRealChunk := false
	for i := 0; i < 10; i++ {
		event := strings.Join(readSSEEvent(t, reader), "\n")
		if strings.Contains(event, "\"chatcmpl-real\"") {
			sawRealChunk = true
			break
		}
		if strings.Contains(event, "data: [DONE]") {
			break
		}
	}
	if !sawRealChunk {
		t.Fatalf("expected forwarded upstream chunk after heartbeat")
	}
}

func TestOpenAIStreamingWrapsUpstreamErrorsAsSSEAfterEarly200(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model 'missing' not found","type":"invalid_request_error"}}`))
	}))
	defer upstream.Close()

	proxy := newTestOpenAIProxy(t, upstream.URL, 50*time.Millisecond)
	server := httptest.NewServer(proxy)
	defer server.Close()

	resp, elapsed := postOpenAIStream(t, server.URL, `{"model":"missing","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 after early stream commit, got %d", resp.StatusCode)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("expected early headers before upstream error, took %s", elapsed)
	}

	reader := bufio.NewReader(resp.Body)
	sawError := false
	sawDone := false
	for i := 0; i < 10; i++ {
		event := strings.Join(readSSEEvent(t, reader), "\n")
		if strings.Contains(event, "\"type\":\"upstream_error\"") {
			sawError = true
		}
		if strings.Contains(event, "data: [DONE]") {
			sawDone = true
			break
		}
	}
	if !sawError {
		t.Fatalf("expected upstream error to be wrapped as SSE event")
	}
	if !sawDone {
		t.Fatalf("expected stream terminator after wrapped upstream error")
	}
}

func newTestOpenAIProxy(t *testing.T, upstreamURL string, heartbeatInterval time.Duration) *Proxy {
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
		OpenAIUpstream:                  upstreamURL,
		OpenAIHeartbeatInterval:         heartbeatInterval,
	})
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	return proxy
}

func postOpenAIStream(t *testing.T, baseURL string, body string) (*http.Response, time.Duration) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test")

	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post openai stream: %v", err)
	}
	return resp, time.Since(start)
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) []string {
	t.Helper()

	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && len(lines) > 0 {
				return lines
			}
			t.Fatalf("read sse event: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(lines) == 0 {
				continue
			}
			return lines
		}
		lines = append(lines, line)
	}
}
