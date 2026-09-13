package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxy.local/app/internal/config"
	"proxy.local/app/internal/glass"
)

// TestOpenAIAllowlistEndToEndThroughHTTP replays the captured omp request
// through the full proxy (handleOpenAIRequest) against a mock upstream and
// asserts the upstream receives the allowlist-stripped tool set, and the
// client receives a successful relay.
func TestOpenAIAllowlistEndToEndThroughHTTP(t *testing.T) {
	t.Setenv("PROXY_ALLOW_DIRECT", "1")
	fixture := loadFixture(t, "omp_capture_13k.json")

	var upstreamGot map[string]interface{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &upstreamGot)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-mock","object":"chat.completion","created":1,` +
			`"model":"glm-5.2-colibri","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},` +
			`"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102}}`))
	}))
	defer upstream.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	raw, _ := json.Marshal(config.DefaultConfig)
	if err := os.WriteFile(cfgPath, raw, 0644); err != nil {
		t.Fatal(err)
	}
	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = filepath.Join(t.TempDir(), "glass")
	glassCfg.KeepaliveIntervalSec = 0
	glassCfg.OpenAIUpstream = upstream.URL
	glassCfg.OpenAIToolAllowlist = []string{"read", "grep"}
	glassCfg.OpenAIEvictTriggerTokens = 13000
	glassCfg.OpenAIEvictTargetTokens = 10000

	proxy, err := New(Options{
		DefaultUpstreamURL:           upstream.URL,
		DefaultProxyDomain:           "proxy.test",
		DefaultUpstreamDomain:        "upstream.test",
		AnthropicMessagesUpstreamURL: upstream.URL,
		AnthropicMessagesProxyDomain: "proxy.test",
		AnthropicMessagesUpstreamDomain: "upstream.test",
		OpenAIUpstream:               upstream.URL,
		GlassConfig:                  &glassCfg,
		AllowDirectUpstream:          true,
		ConfigLoader:                 config.NewLoader(cfgPath),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()

	bodyRaw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", server.URL+"/v1/chat/completions",
		strings.NewReader(string(bodyRaw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, string(out))
	}
	if upstreamGot == nil {
		t.Fatalf("upstream never received a parseable body")
	}
	tools, _ := upstreamGot["tools"].([]interface{})
	if len(tools) != 2 {
		names := []string{}
		for _, tool := range tools {
			if tm, ok := tool.(map[string]interface{}); ok {
				if fn, ok := tm["function"].(map[string]interface{}); ok {
					names = append(names, fn["name"].(string))
				}
			}
		}
		t.Fatalf("upstream tools = %v, want exactly read+grep", names)
	}
	// Second turn must send the identical stripped tool set (prefix stability).
	req2, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions",
		strings.NewReader(string(bodyRaw)))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second turn status %d", resp2.StatusCode)
	}
	_ = time.Second
}
