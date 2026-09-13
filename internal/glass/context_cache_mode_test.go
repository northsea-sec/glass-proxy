package glass

import (
	"encoding/json"
	"strings"
	"testing"
)

// mainSessionBody builds a request body that won't be classified as a subagent.
// The system prompt must be >= 5000 chars (SmallSystemThreshold in subagent/classifier.go).
func mainSessionBody() map[string]interface{} {
	return map[string]interface{}{
		"model": "claude-sonnet-4-6",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": strings.Repeat("You are a helpful AI assistant. ", 200)},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
			map[string]interface{}{"role": "assistant", "content": "hi there"},
			map[string]interface{}{"role": "user", "content": "how are you"},
		},
	}
}

func TestCacheModeDefaults(t *testing.T) {
	cfg := DefaultGlassConfig()
	if got := cfg.CacheMode(); got != CacheModeFull {
		t.Fatalf("default CacheMode = %q, want %q", got, CacheModeFull)
	}
}

func TestCacheModeValues(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"", CacheModeFull},
		{"full", CacheModeFull},
		{"off", CacheModeOff},
		{"context_api", CacheModeContextAPI},
		{"garbage", CacheModeFull}, // unknown values default to full
	} {
		cfg := GlassConfig{ContextCacheMode: tc.input}
		if got := cfg.CacheMode(); got != tc.want {
			t.Errorf("CacheMode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestContextAPIConfigDefaults(t *testing.T) {
	cfg := GlassConfig{} // all zero values
	trigger, keep, clearAtLeast, clearThinking := cfg.ContextAPIConfig()
	if trigger != 100000 {
		t.Errorf("trigger = %d, want 100000", trigger)
	}
	if keep != 3 {
		t.Errorf("keep = %d, want 3", keep)
	}
	if clearAtLeast != 20000 {
		t.Errorf("clearAtLeast = %d, want 20000", clearAtLeast)
	}
	if !clearThinking {
		t.Error("clearThinking = false, want true (default)")
	}
}

func TestContextAPIConfigCustom(t *testing.T) {
	cfg := GlassConfig{
		ContextAPITriggerTokens: 50000,
		ContextAPIKeepToolUses:  5,
		ContextAPIClearAtLeast:  10000,
		ContextAPIClearThinking: false,
	}
	trigger, keep, clearAtLeast, clearThinking := cfg.ContextAPIConfig()
	if trigger != 50000 {
		t.Errorf("trigger = %d, want 50000", trigger)
	}
	if keep != 5 {
		t.Errorf("keep = %d, want 5", keep)
	}
	if clearAtLeast != 10000 {
		t.Errorf("clearAtLeast = %d, want 10000", clearAtLeast)
	}
	if clearThinking {
		t.Error("clearThinking = true, want false (explicit)")
	}
}

func TestCacheModeJSON(t *testing.T) {
	// Verify the field survives JSON round-trip (config file scenario)
	cfg := GlassConfig{ContextCacheMode: "context_api"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out GlassConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.CacheMode() != CacheModeContextAPI {
		t.Errorf("after round-trip: CacheMode = %q, want %q", out.CacheMode(), CacheModeContextAPI)
	}
}

// TestProcessCacheModeOff verifies that "off" mode skips compression and breakpoint.
func TestProcessCacheModeOff(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ContextCacheMode = CacheModeOff
	engine := NewEngine(cfg)
	body := mainSessionBody()

	result := engine.Process(body)
	if result.ContextCacheMode != CacheModeOff {
		t.Errorf("result.ContextCacheMode = %q, want %q", result.ContextCacheMode, CacheModeOff)
	}

	// No cache_control should be on any message
	msgs := body["messages"].([]interface{})
	for i, m := range msgs {
		msg := m.(map[string]interface{})
		if _, has := msg["cache_control"]; has {
			t.Errorf("message[%d] has cache_control in off mode", i)
		}
	}

	// No context_management should be injected
	if _, has := body["context_management"]; has {
		t.Error("body has context_management in off mode")
	}
}

// TestProcessCacheModeContextAPI verifies context_management injection.
func TestProcessCacheModeContextAPI(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ContextCacheMode = CacheModeContextAPI
	engine := NewEngine(cfg)
	body := mainSessionBody()

	result := engine.Process(body)
	if result.ContextCacheMode != CacheModeContextAPI {
		t.Errorf("result.ContextCacheMode = %q, want %q", result.ContextCacheMode, CacheModeContextAPI)
	}

	// context_management must be present
	cm, has := body["context_management"]
	if !has {
		t.Fatal("body missing context_management in context_api mode")
	}

	cmMap, ok := cm.(map[string]interface{})
	if !ok {
		t.Fatalf("context_management is %T, want map", cm)
	}
	edits, ok := cmMap["edits"].([]interface{})
	if !ok {
		t.Fatalf("context_management.edits is %T, want []interface{}", cmMap["edits"])
	}
	// Expect 2 edits: clear_thinking + clear_tool_uses (defaults)
	if len(edits) != 2 {
		t.Fatalf("len(edits) = %d, want 2", len(edits))
	}

	// Verify types
	edit0 := edits[0].(map[string]interface{})
	if edit0["type"] != "clear_thinking_20251015" {
		t.Errorf("edits[0].type = %v, want clear_thinking_20251015", edit0["type"])
	}
	edit1 := edits[1].(map[string]interface{})
	if edit1["type"] != "clear_tool_uses_20250919" {
		t.Errorf("edits[1].type = %v, want clear_tool_uses_20250919", edit1["type"])
	}

	// No cache_control on messages
	msgs := body["messages"].([]interface{})
	for i, m := range msgs {
		msg := m.(map[string]interface{})
		if _, has := msg["cache_control"]; has {
			t.Errorf("message[%d] has cache_control in context_api mode", i)
		}
	}
}

// TestProcessCacheModeFull verifies breakpoint placement still works.
func TestProcessCacheModeFull(t *testing.T) {
	cfg := DefaultGlassConfig()
	// CacheMode defaults to "full"
	engine := NewEngine(cfg)
	body := mainSessionBody()

	result := engine.Process(body)
	if result.ContextCacheMode != CacheModeFull {
		t.Errorf("result.ContextCacheMode = %q, want %q", result.ContextCacheMode, CacheModeFull)
	}

	// At least one message should have cache_control in full mode
	msgs := body["messages"].([]interface{})
	found := false
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		if _, has := msg["cache_control"]; has {
			found = true
			break
		}
		// Also check content blocks
		if content, ok := msg["content"].([]interface{}); ok {
			for _, b := range content {
				if block, ok := b.(map[string]interface{}); ok {
					if _, has := block["cache_control"]; has {
						found = true
						break
					}
				}
			}
		}
	}
	if !found {
		t.Error("no cache_control found on any message in full mode")
	}

	// No context_management in full mode
	if _, has := body["context_management"]; has {
		t.Error("body has context_management in full mode")
	}
}

// TestCompressionBatching verifies that the watermark only advances in batches,
// not on every request. This is the core fix from the nataraja design.
func TestCompressionBatching(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.CompressionBatchSize = 8
	cfg.CompressionTriggerTokens = 0 // always compress (to test batching, not gating)
	cfg.EvictTriggerTokens = 9999999  // prevent eviction from interfering
	engine := NewEngine(cfg)

	// Build a conversation long enough to have a watermark and anchor.
	// Need: system >= 5000 chars (avoid subagent), enough messages to trigger
	// anchor advancement (breakpointAdvanceThreshold=16) and compression.
	sys := strings.Repeat("You are a helpful AI assistant. ", 200)
	msgs := []interface{}{}
	for i := 0; i < 40; i++ {
		if i%2 == 0 {
			msgs = append(msgs, map[string]interface{}{
				"role":    "user",
				"content": []interface{}{map[string]interface{}{"type": "tool_result", "tool_use_id": "t" + strings.Repeat("x", 100), "content": strings.Repeat("data ", 200)}},
			})
		} else {
			msgs = append(msgs, map[string]interface{}{
				"role":    "assistant",
				"content": []interface{}{map[string]interface{}{"type": "text", "text": strings.Repeat("response ", 100)}},
			})
		}
	}

	body := map[string]interface{}{
		"model":    "claude-sonnet-4-6",
		"system":   []interface{}{map[string]interface{}{"type": "text", "text": sys}},
		"messages": msgs,
	}

	// First request: ingests all 40 messages.
	result1 := engine.Process(body)
	wm1 := result1.CompressionWatermark
	t.Logf("Request 1: watermark=%d anchor=%d", wm1, result1.PrefixAnchor)

	// Add 2 more messages (1 exchange).
	msgs = append(msgs,
		map[string]interface{}{"role": "user", "content": "next question"},
		map[string]interface{}{"role": "assistant", "content": "next answer"},
	)
	body["messages"] = msgs
	result2 := engine.Process(body)
	wm2 := result2.CompressionWatermark
	t.Logf("Request 2 (+2 msgs): watermark=%d anchor=%d", wm2, result2.PrefixAnchor)

	// The watermark should NOT have crept by 2. With batch_size=8,
	// it should only advance when 8+ messages accumulate past the current watermark.
	if wm2 != wm1 {
		t.Errorf("watermark crept from %d to %d after adding 2 messages (batch_size=8 should hold)", wm1, wm2)
	}

	// Add 6 more messages (3 exchanges) — still under batch threshold.
	for i := 0; i < 6; i++ {
		if i%2 == 0 {
			msgs = append(msgs, map[string]interface{}{"role": "user", "content": "question"})
		} else {
			msgs = append(msgs, map[string]interface{}{"role": "assistant", "content": "answer"})
		}
	}
	body["messages"] = msgs
	result3 := engine.Process(body)
	wm3 := result3.CompressionWatermark
	t.Logf("Request 3 (+6 more msgs, +8 total): watermark=%d anchor=%d", wm3, result3.PrefixAnchor)

	// Now we've added 8 messages total since the last watermark.
	// Whether the batch fires depends on the anchor clamp too.
	// Key assertion: watermark advanced AT MOST once, by a batch.
	if wm3 > wm1 {
		delta := wm3 - wm1
		if delta < 8 {
			t.Errorf("watermark advanced by %d (should be >= batch_size 8 or not at all)", delta)
		}
		t.Logf("Batch advance confirmed: watermark %d -> %d (delta=%d)", wm1, wm3, delta)
	} else {
		t.Logf("Watermark held at %d (anchor clamp likely prevented advance)", wm3)
	}
}
