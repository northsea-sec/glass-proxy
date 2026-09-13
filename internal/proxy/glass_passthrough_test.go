package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"proxy.local/app/internal/config"
	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/spoofer"
	"proxy.local/app/internal/trimmer"
)

func TestGlassPassthroughPreservesNativeCacheControlsAndSkipsMCPCache(t *testing.T) {
	captured := make(chan map[string]interface{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		var body map[string]interface{}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatalf("unmarshal upstream body: %v", err)
		}
		captured <- body
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Anthropic-Ratelimit-Unified-Status", "allowed")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.42")
		w.Header().Set("Anthropic-Ratelimit-Unified-7d-Utilization", "0.69")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	forceThinking := true
	forceBudget := 31999
	cfg := config.Config{
		GlassPassthrough:     true,
		StripSystemReminders: true,
		StripThinkingBlocks:  true,
		SpoofUsageCap:        1,
		ForceThinking:        &forceThinking,
		ForceThinkingBudget:  &forceBudget,
	}
	p := newClaudePassthroughTestProxy(t, upstream.URL, cfg)

	body := smallSystemToolRequest("hello")
	body["context_management"] = map[string]interface{}{
		"edits": []interface{}{map[string]interface{}{"type": "clear_tool_uses_20250919"}},
	}
	body["thinking"] = map[string]interface{}{
		"type":          "adaptive",
		"budget_tokens": 123,
	}
	convID := trimmer.SessionFingerprint(body, 0)
	p.mcpCache.Observe(convID, []interface{}{
		anthropicTool("mcp__missing__tool", false),
	})

	reqBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.test/v1/messages", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Anthropic-Ratelimit-Unified-5h-Utilization"); got != "0.42" {
		t.Fatalf("passthrough spoofed rate-limit header=%q, want real 0.42", got)
	}

	forwarded := <-captured
	tools, _ := forwarded["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("passthrough injected or dropped tools: got %d tools, want 1", len(tools))
	}
	tool, _ := tools[0].(map[string]interface{})
	if name, _ := tool["name"].(string); name != "mcp__test__tool" {
		t.Fatalf("forwarded tool name=%q, want original mcp__test__tool", name)
	}
	assertHasCacheControl(t, tool, "tool")

	systemBlocks, _ := forwarded["system"].([]interface{})
	if len(systemBlocks) != 1 {
		t.Fatalf("system blocks=%d, want 1", len(systemBlocks))
	}
	systemBlock, _ := systemBlocks[0].(map[string]interface{})
	assertHasCacheControl(t, systemBlock, "system block")

	messages, _ := forwarded["messages"].([]interface{})
	msg, _ := messages[0].(map[string]interface{})
	content, _ := msg["content"].([]interface{})
	textBlock, _ := content[0].(map[string]interface{})
	assertHasCacheControl(t, textBlock, "message text block")

	if _, ok := forwarded["context_management"].(map[string]interface{}); !ok {
		t.Fatalf("passthrough stripped context_management: %#v", forwarded)
	}
	thinking, _ := forwarded["thinking"].(map[string]interface{})
	if got, _ := thinking["type"].(string); got != "adaptive" {
		t.Fatalf("passthrough force-mutated thinking.type=%q, want adaptive", got)
	}
	switch got := thinking["budget_tokens"].(type) {
	case float64:
		if int(got) != 123 {
			t.Fatalf("passthrough force-mutated thinking.budget_tokens=%v, want 123", got)
		}
	default:
		t.Fatalf("unexpected thinking.budget_tokens type %T", got)
	}
}

func TestSmallSystemToolSubagentsAreRateLimited(t *testing.T) {
	var upstreamHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	cfg := config.Config{GlassPassthrough: true}
	p := newClaudePassthroughTestProxy(t, upstream.URL, cfg)

	for i := 0; i < agentToolRateMax+1; i++ {
		body := smallSystemToolRequest(fmt.Sprintf("loop turn %d", i))
		reqBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request %d: %v", i, err)
		}
		req := httptest.NewRequest(http.MethodPost, "http://proxy.test/v1/messages", bytes.NewReader(reqBody))
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}

	if got := atomic.LoadInt32(&upstreamHits); got != agentToolRateMax {
		t.Fatalf("upstream hits=%d, want %d; small_system tool loop was not capped", got, agentToolRateMax)
	}
}

func newClaudePassthroughTestProxy(t *testing.T, upstreamURL string, cfg config.Config) *Proxy {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), "glass_config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}

	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = filepath.Join(t.TempDir(), "glass")
	glassCfg.KeepaliveIntervalSec = 0

	p, err := New(Options{
		DefaultUpstreamURL:              upstreamURL,
		DefaultProxyDomain:              "proxy.test",
		DefaultUpstreamDomain:           parsed.Host,
		AnthropicMessagesUpstreamURL:    upstreamURL,
		AnthropicMessagesProxyDomain:    "proxy.test",
		AnthropicMessagesUpstreamDomain: parsed.Host,
		ConfigLoader:                    config.NewLoader(cfgPath),
		AllowDirectUpstream:             true,
		GlassConfig:                     &glassCfg,
		UsageSpoofer:                    spoofer.New(true, 0),
	})
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	return p
}

func smallSystemToolRequest(userText string) map[string]interface{} {
	return map[string]interface{}{
		"model":  "claude-test",
		"stream": false,
		"system": []interface{}{
			map[string]interface{}{
				"type":          "text",
				"text":          "short diagnostic system",
				"cache_control": map[string]interface{}{"type": "ephemeral", "ttl": "5m"},
			},
		},
		"tools": []interface{}{
			anthropicTool("mcp__test__tool", true),
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type":          "text",
						"text":          userText,
						"cache_control": map[string]interface{}{"type": "ephemeral", "ttl": "5m"},
					},
				},
			},
		},
	}
}

func anthropicTool(name string, withCacheControl bool) map[string]interface{} {
	tool := map[string]interface{}{
		"name":        name,
		"description": "test tool",
		"input_schema": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	if withCacheControl {
		tool["cache_control"] = map[string]interface{}{"type": "ephemeral", "ttl": "5m"}
	}
	return tool
}

func assertHasCacheControl(t *testing.T, block map[string]interface{}, label string) {
	t.Helper()
	if _, ok := block["cache_control"].(map[string]interface{}); !ok {
		t.Fatalf("%s lost cache_control: %#v", label, block)
	}
}
