package glass_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxy.local/app/internal/glass"
)

func TestEvictionBasic(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = tmpDir
	cfg.EvictTriggerTokens = 1000 // low trigger for testing
	cfg.EvictTargetTokens = 500
	cfg.AnchorKeepMsgs = 2
	cfg.RecentKeepMsgs = 2

	engine := glass.NewEngine(cfg)

	// Build a body with system prompt and messages
	body := map[string]interface{}{
		"system":   testMainSystem(),
		"messages": buildTestMessages(20),
	}

	result := engine.Process(body)

	// Check that messages were evicted
	msgs := body["messages"].([]interface{})
	if len(msgs) >= 20 {
		t.Errorf("Expected eviction to reduce messages from 20, got %d", len(msgs))
	}
	t.Logf("Messages: 20 → %d, saved ~%d tokens", len(msgs), result.TokensSaved)

	// Check shadow file was written
	entries, _ := os.ReadDir(tmpDir)
	if len(entries) == 0 {
		t.Error("Expected shadow directory to be created")
	}

	// Verify state file exists
	for _, entry := range entries {
		if entry.IsDir() {
			statePath := filepath.Join(tmpDir, entry.Name(), "state.json")
			if _, err := os.Stat(statePath); err != nil {
				t.Errorf("Expected state.json: %v", err)
			}
			shadowPath := filepath.Join(tmpDir, entry.Name(), "shadow.md")
			if _, err := os.Stat(shadowPath); err != nil {
				t.Errorf("Expected shadow.md: %v", err)
			}
		}
	}
}

func TestThinkingStrip(t *testing.T) {
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999 // no eviction
	cfg.StripThinking = true

	engine := glass.NewEngine(cfg)

	body := map[string]interface{}{
		"system": testMainSystem(),
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "thinking", "thinking": "let me think..."},
					map[string]interface{}{"type": "text", "text": "response 1"},
				},
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "continue"},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "thinking", "thinking": "thinking again..."},
					map[string]interface{}{"type": "text", "text": "response 2"},
				},
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "final question"},
				},
			},
		},
	}

	engine.Process(body)

	msgs := body["messages"].([]interface{})
	// First assistant (index 1) should have thinking stripped
	msg1 := msgs[1].(map[string]interface{})
	content1 := msg1["content"].([]interface{})
	for _, b := range content1 {
		block := b.(map[string]interface{})
		if block["type"] == "thinking" {
			t.Error("First assistant message should have thinking stripped")
		}
	}

	// Second assistant (index 3) should also have thinking stripped to keep
	// request bytes stable across turns.
	msg3 := msgs[3].(map[string]interface{})
	content3 := msg3["content"].([]interface{})
	for _, b := range content3 {
		block := b.(map[string]interface{})
		if block["type"] == "thinking" {
			t.Error("Second assistant message should have thinking stripped")
		}
	}
}

func TestSysReminderStrip(t *testing.T) {
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999
	cfg.StripSysReminders = true

	engine := glass.NewEngine(cfg)

	body := map[string]interface{}{
		"system": testMainSystem(),
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "hello <system-reminder>noise here</system-reminder> world",
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hi"},
				},
			},
		},
	}

	engine.Process(body)

	msgs := body["messages"].([]interface{})
	msg0 := msgs[0].(map[string]interface{})
	content := msg0["content"].(string)
	if content != "hello  world" {
		t.Errorf("Expected system-reminder stripped, got: %q", content)
	}
}

func buildTestMessages(n int) []interface{} {
	msgs := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		// Make each message ~200 chars to test token estimation
		text := ""
		for j := 0; j < 50; j++ {
			text += "word "
		}
		msgs = append(msgs, map[string]interface{}{
			"role": role,
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": text,
				},
			},
		})
	}
	return msgs
}

func prettyJSON(v interface{}) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return string(data)
}

// --- Multi-call tests: catch the sabotage bugs ---

// TestPrefixStabilityNoNewMessages verifies that repeated Process() calls with
// the SAME messages produce byte-identical serialized output (frozen prefix).
func TestPrefixStabilityNoNewMessages(t *testing.T) {
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999
	cfg.StripThinking = true

	engine := glass.NewEngine(cfg)

	msgs := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hello"},
			},
		},
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "let me think..."},
				map[string]interface{}{"type": "text", "text": "response 1"},
			},
		},
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "continue"},
			},
		},
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "more thinking..."},
				map[string]interface{}{"type": "text", "text": "response 2"},
			},
		},
	}

	system := testMainSystem()

	// Call 1: initial ingestion
	body1 := map[string]interface{}{
		"system":   system,
		"messages": msgs,
	}
	engine.Process(body1)
	out1, _ := json.Marshal(body1["messages"])

	// Calls 2-10: same messages, no new content
	for i := 2; i <= 10; i++ {
		body := map[string]interface{}{
			"system":   system,
			"messages": msgs,
		}
		engine.Process(body)
		out, _ := json.Marshal(body["messages"])

		if string(out) != string(out1) {
			t.Fatalf("Call %d produced different output than call 1.\nCall 1: %s\nCall %d: %s",
				i, string(out1), i, string(out))
		}
	}
	t.Logf("10 calls with same messages: all byte-identical")
}

// TestCacheControlNoAccumulation verifies that cache_control markers do NOT
// accumulate across multiple Process() calls with new messages added each time.
// This is the exact bug that caused the API 400 "maximum of 4 blocks" error.
func TestCacheControlNoAccumulation(t *testing.T) {
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999
	cfg.StripThinking = false

	engine := glass.NewEngine(cfg)

	system := []interface{}{
		map[string]interface{}{
			"type":          "text",
			"text":          strings.Repeat("Shared main-session instructions. ", 220),
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
		map[string]interface{}{
			"type":          "text",
			"text":          strings.Repeat("Shared main-session instructions tail. ", 80),
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
	}

	// Simulate 10 exchanges, each adding a user+assistant pair
	allMsgs := []interface{}{}
	for round := 0; round < 10; round++ {
		allMsgs = append(allMsgs,
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "question " + string(rune(65+round))},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "answer " + string(rune(65+round))},
				},
			},
		)

		body := map[string]interface{}{
			"system":   system,
			"messages": copyMsgs(allMsgs),
		}
		engine.Process(body)

		// Count cache_control blocks in messages
		ccInMsgs := countCacheControlInMsgs(body["messages"].([]interface{}))
		// Count cache_control in system
		ccInSys := countCacheControlInMsgs(body["system"].([]interface{}))
		total := ccInMsgs + ccInSys

		if total > 4 {
			t.Fatalf("Round %d: total cache_control = %d (sys=%d, msgs=%d) — EXCEEDS 4-BLOCK LIMIT",
				round+1, total, ccInSys, ccInMsgs)
		}
		if ccInMsgs > 2 {
			t.Fatalf("Round %d: cache_control in messages = %d — should be at most 2",
				round+1, ccInMsgs)
		}
		t.Logf("Round %d: %d messages, cache_control: sys=%d msgs=%d total=%d",
			round+1, len(body["messages"].([]interface{})), ccInSys, ccInMsgs, total)
	}
}

// TestBuildImmutability verifies that Build() returns copies that do not
// mutate the canonical cache when downstream code modifies them.
func TestBuildImmutability(t *testing.T) {
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999
	cfg.StripThinking = true

	engine := glass.NewEngine(cfg)

	system := testMainSystem()
	msgs := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hello"},
			},
		},
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "deep thought"},
				map[string]interface{}{"type": "text", "text": "hi there"},
			},
		},
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "thanks"},
			},
		},
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "more thought"},
				map[string]interface{}{"type": "text", "text": "welcome"},
			},
		},
	}

	// Call 1: Process and capture output
	body1 := map[string]interface{}{
		"system":   system,
		"messages": copyMsgs(msgs),
	}
	engine.Process(body1)
	out1, _ := json.Marshal(body1["messages"])

	// Deliberately mutate the output from call 1 (simulate downstream code)
	outMsgs := body1["messages"].([]interface{})
	if len(outMsgs) > 0 {
		if m, ok := outMsgs[0].(map[string]interface{}); ok {
			m["SABOTAGE"] = "this should not appear in call 2"
		}
	}

	// Call 2: Process again with same messages
	body2 := map[string]interface{}{
		"system":   system,
		"messages": copyMsgs(msgs),
	}
	engine.Process(body2)
	out2, _ := json.Marshal(body2["messages"])

	// Outputs must be identical — mutation of call 1 output must not affect call 2
	if string(out1) != string(out2) {
		t.Fatalf("Build() mutation leaked into cache.\nCall 1: %s\nCall 2: %s",
			string(out1), string(out2))
	}
	t.Log("Downstream mutation did not affect canonical cache")
}

// TestThinkingStripAcrossCalls verifies thinking strip behavior when new
// assistant messages are added across calls.
func TestThinkingStripAcrossCalls(t *testing.T) {
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999
	cfg.StripThinking = true

	engine := glass.NewEngine(cfg)

	system := testMainSystem()

	// Call 1: 3 messages (user + assistant with thinking + user)
	msgs1 := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hello"},
			},
		},
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "thought 1"},
				map[string]interface{}{"type": "text", "text": "response 1"},
			},
		},
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "more"},
			},
		},
	}
	body1 := map[string]interface{}{"system": system, "messages": copyMsgs(msgs1)}
	engine.Process(body1)

	// msg[1] should already have thinking stripped on the first call.
	out1Msgs := body1["messages"].([]interface{})
	asst1 := out1Msgs[1].(map[string]interface{})
	asst1Content := asst1["content"].([]interface{})
	if hasBlockType(asst1Content, "thinking") {
		t.Fatal("Call 1: assistant thinking should be stripped immediately")
	}

	// Call 1b: same messages again — output must be identical
	body1b := map[string]interface{}{"system": system, "messages": copyMsgs(msgs1)}
	engine.Process(body1b)
	out1bJSON, _ := json.Marshal(body1b["messages"])
	out1JSON, _ := json.Marshal(body1["messages"])
	if string(out1JSON) != string(out1bJSON) {
		t.Fatal("Call 1b: repeated call produced different output")
	}

	// Call 2: add assistant response + new user turn
	// msgs1 was [user(0), assistant(1), user(2)]
	// msgs2 becomes [user(0), assistant(1), user(2), assistant(3), user(4)]
	msgs2 := append(copyMsgs(msgs1),
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "thought 2"},
				map[string]interface{}{"type": "text", "text": "response 2"},
			},
		},
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "another question"},
			},
		},
	)
	body2 := map[string]interface{}{"system": system, "messages": msgs2}
	engine.Process(body2)

	out2Msgs := body2["messages"].([]interface{})
	// msg[1] remains stripped
	asst1After := out2Msgs[1].(map[string]interface{})
	asst1AfterContent := asst1After["content"].([]interface{})
	if hasBlockType(asst1AfterContent, "thinking") {
		t.Fatal("Call 2: first assistant should remain stripped")
	}
	// msg[3] should also be stripped immediately so the tail does not mutate
	// later when another assistant message is added.
	asst2 := out2Msgs[3].(map[string]interface{})
	asst2Content := asst2["content"].([]interface{})
	if hasBlockType(asst2Content, "thinking") {
		t.Fatal("Call 2: last assistant should also have thinking stripped")
	}

	// Call 2b: same messages again — output must be identical to call 2
	body2b := map[string]interface{}{"system": system, "messages": copyMsgs(msgs2)}
	engine.Process(body2b)
	out2bJSON, _ := json.Marshal(body2b["messages"])
	out2JSON, _ := json.Marshal(body2["messages"])
	if string(out2JSON) != string(out2bJSON) {
		t.Fatal("Call 2b: repeated call produced different output")
	}

	t.Log("Thinking strip across calls: correct behavior")
}

func TestThinkingStripPreservesPureThinkingAssistant(t *testing.T) {
	cfg := glass.DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999
	cfg.StripThinking = true

	engine := glass.NewEngine(cfg)

	body := map[string]interface{}{
		"system": testMainSystem(),
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "thinking", "thinking": "only thought"},
				},
			},
		},
	}

	engine.Process(body)

	msgs := body["messages"].([]interface{})
	asst := msgs[1].(map[string]interface{})
	content := asst["content"].([]interface{})
	if !hasBlockType(content, "thinking") {
		t.Fatal("pure-thinking assistant should be preserved to avoid empty content")
	}
}

// --- Test helpers ---

func copyMsgs(msgs []interface{}) []interface{} {
	data, _ := json.Marshal(msgs)
	var out []interface{}
	json.Unmarshal(data, &out)
	return out
}

func countCacheControlInMsgs(msgs []interface{}) int {
	count := 0
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if _, has := msg["cache_control"]; has {
			count++
		}
		if content, ok := msg["content"].([]interface{}); ok {
			for _, b := range content {
				if block, ok := b.(map[string]interface{}); ok {
					if _, has := block["cache_control"]; has {
						count++
					}
				}
			}
		}
	}
	return count
}

func hasBlockType(content []interface{}, blockType string) bool {
	for _, b := range content {
		if block, ok := b.(map[string]interface{}); ok {
			if tp, _ := block["type"].(string); tp == blockType {
				return true
			}
		}
	}
	return false
}

func testMainSystem() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": strings.Repeat("Main-session shared system prefix. ", 220),
		},
	}
}
