package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIRequestTrimmingIsRequestAuthoritative(t *testing.T) {
	var mu sync.Mutex
	var captured [][]byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		mu.Lock()
		captured = append(captured, body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":123,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	proxy := newTestOpenAIProxy(t, upstream.URL, 50*time.Millisecond)
	server := httptest.NewServer(proxy)
	defer server.Close()

	req1Messages := buildLargeOpenAIHistory(48, 1400)
	req2Messages := append(cloneOpenAIMessages(req1Messages),
		map[string]interface{}{"role": "user", "content": strings.Repeat("latest user turn ", 80)},
		map[string]interface{}{"role": "assistant", "content": strings.Repeat("latest assistant turn ", 80)},
	)

	postOpenAINonStream(t, server.URL, map[string]interface{}{
		"model":    "qwen-test",
		"messages": req1Messages,
	})
	postOpenAINonStream(t, server.URL, map[string]interface{}{
		"model":    "qwen-test",
		"messages": req2Messages,
	})

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("expected 2 forwarded requests, got %d", len(captured))
	}

	assertForwardedMatchesTrimmedView(t, captured[0], req1Messages)
	assertForwardedMatchesTrimmedView(t, captured[1], req2Messages)
}

func TestOpenAIRequestRejectsOversizeLiveTailLocally(t *testing.T) {
	var upstreamHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	proxy := newTestOpenAIProxy(t, upstream.URL, 50*time.Millisecond)
	server := httptest.NewServer(proxy)
	defer server.Close()

	respBody, status := postOpenAINonStreamRaw(t, server.URL, map[string]interface{}{
		"model": "qwen-test",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": strings.Repeat("x", 240000)},
		},
	})

	if status != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400, got %d with body %s", status, respBody)
	}
	if atomic.LoadInt32(&upstreamHits) != 0 {
		t.Fatalf("expected oversize request to be rejected before upstream")
	}
	if !strings.Contains(respBody, "exceed_context_size_error") {
		t.Fatalf("expected exceed_context_size_error, got %s", respBody)
	}
}

func TestOpenAIRequestKeepsFullHarnessSystemPromptAndCompactsFreshToolTurn(t *testing.T) {
	var mu sync.Mutex
	var captured [][]byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		mu.Lock()
		captured = append(captured, body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":123,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	proxy := newTestOpenAIProxy(t, upstream.URL, 50*time.Millisecond)
	server := httptest.NewServer(proxy)
	defer server.Close()

	postOpenAINonStream(t, server.URL, map[string]interface{}{
		"model":    "qwen-test",
		"messages": buildFreshHarnessToolTurn(),
	})

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("expected 1 forwarded request, got %d", len(captured))
	}

	var body map[string]interface{}
	if err := json.Unmarshal(captured[0], &body); err != nil {
		t.Fatalf("unmarshal forwarded body: %v", err)
	}
	forwardedMessages, _ := body["messages"].([]interface{})
	if len(forwardedMessages) == 0 {
		t.Fatal("expected forwarded messages")
	}
	first, _ := forwardedMessages[0].(map[string]interface{})
	if role, _ := first["role"].(string); role != "system" {
		t.Fatalf("expected full harness system message to remain first, got role=%v", first["role"])
	}
	firstContent, _ := first["content"].(string)
	if firstContent != ompHarnessSystemPrompt() {
		t.Fatalf("expected forwarded system prompt to remain unchanged\n--- got ---\n%s\n--- want ---\n%s", firstContent, ompHarnessSystemPrompt())
	}

	sawCompactedTool := false
	for _, raw := range forwardedMessages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := msg["reasoning_content"]; ok {
			t.Fatalf("expected reasoning_content to be stripped from forwarded message")
		}
		if role, _ := msg["role"].(string); role == "tool" {
			content, _ := msg["content"].(string)
			if strings.Contains(content, openAIToolResultOmittedMarker) {
				sawCompactedTool = true
			}
		}
	}
	if !sawCompactedTool {
		t.Fatal("expected at least one old tool result to be compacted")
	}

	if got := estimateOpenAITokens(forwardedMessages); got >= 55000 {
		t.Fatalf("expected forwarded prompt under live budget, got %d tokens", got)
	}
}

func TestOpenAIRequestPreservesRecentUserTurnsAcrossTrim(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": "OMP harness",
		},
	}

	for i := 0; i < 26; i++ {
		msgs = append(msgs,
			map[string]interface{}{
				"role":    "user",
				"content": strings.Repeat(fmt.Sprintf("older user %02d ", i), 700),
			},
			map[string]interface{}{
				"role":    "assistant",
				"content": strings.Repeat(fmt.Sprintf("older assistant %02d ", i), 700),
			},
		)
	}

	recentTurns := []struct {
		user      string
		assistant string
	}{
		{
			user:      "research the ops tab and compare it against /home/user/test/text-PLINY*.md",
			assistant: strings.Repeat("working through the recent architecture question ", 260),
		},
		{
			user:      "i said home/user/test !!!",
			assistant: strings.Repeat("correcting path handling and re-reading the instruction ", 260),
		},
		{
			user:      "i said read those files to understand the original plan!! not to work from!",
			assistant: strings.Repeat("switching from workspace actions to transcript reading ", 260),
		},
		{
			user:      "what is your recollection of what we are working on what is your context!",
			assistant: "",
		},
	}
	for _, turn := range recentTurns {
		msgs = append(msgs, map[string]interface{}{
			"role":    "user",
			"content": turn.user,
		})
		if turn.assistant != "" {
			msgs = append(msgs, map[string]interface{}{
				"role":    "assistant",
				"content": turn.assistant,
			})
		}
	}

	trimmed, err := buildOpenAIRequestMessages(msgs, 0, 55000, 45000)
	if err != nil {
		t.Fatalf("buildOpenAIRequestMessages: %v", err)
	}
	if trimmed.Evicted == 0 {
		t.Fatal("expected old history to be evicted in this scenario")
	}

	var userContents []string
	for _, raw := range trimmed.Messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "user" {
			if content, _ := msg["content"].(string); content != "" {
				userContents = append(userContents, content)
			}
		}
	}

	for _, turn := range recentTurns {
		if !containsString(userContents, turn.user) {
			t.Fatalf("expected recent user turn to survive trimming: %q\nkept users: %v", turn.user, userContents)
		}
	}
	if containsString(userContents, strings.Repeat("older user 00 ", 700)) {
		t.Fatalf("expected oldest history to be trimmed, kept users: %v", userContents)
	}
}

func assertForwardedMatchesTrimmedView(t *testing.T, forwarded []byte, original []interface{}) {
	t.Helper()

	var body map[string]interface{}
	if err := json.Unmarshal(forwarded, &body); err != nil {
		t.Fatalf("unmarshal forwarded body: %v", err)
	}
	forwardedMessages, _ := body["messages"].([]interface{})

	expected, err := buildOpenAIRequestMessages(cloneOpenAIMessages(original), 0, 55000, 45000)
	if err != nil {
		t.Fatalf("build expected trimmed view: %v", err)
	}

	gotJSON, err := json.Marshal(forwardedMessages)
	if err != nil {
		t.Fatalf("marshal forwarded messages: %v", err)
	}
	wantJSON, err := json.Marshal(expected.Messages)
	if err != nil {
		t.Fatalf("marshal expected messages: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("forwarded messages do not match request-authoritative trimmed view")
	}
}

func buildLargeOpenAIHistory(pairs int, blockSize int) []interface{} {
	msgs := make([]interface{}, 0, pairs*2+1)
	msgs = append(msgs, map[string]interface{}{
		"role":    "system",
		"content": strings.Repeat("system prompt ", 40),
	})
	for i := 0; i < pairs; i++ {
		msgs = append(msgs,
			map[string]interface{}{
				"role":    "user",
				"content": strings.Repeat("user payload block ", blockSize/18),
			},
			map[string]interface{}{
				"role":    "assistant",
				"content": strings.Repeat("assistant payload block ", blockSize/22),
			},
		)
	}
	return msgs
}

func buildFreshHarnessToolTurn() []interface{} {
	msgs := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": ompHarnessSystemPrompt(),
		},
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "read COMMANDER/text-PLINY-5.md text-PLINY-4.md then verify claims"},
			},
		},
	}

	for i := 0; i < 5; i++ {
		toolID := fmt.Sprintf("tool-call-%d", i)
		msgs = append(msgs,
			map[string]interface{}{
				"role":              "assistant",
				"content":           nil,
				"reasoning_content": "Thinking through the next read step before issuing the tool call.",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   toolID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      "read",
							"arguments": fmt.Sprintf("{\"path\":\"/home/user/glass-proxy/COMMANDER/text-PLINY-%d.md\",\"offset\":1}", 5),
						},
					},
				},
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": toolID,
				"content":      strings.Repeat(fmt.Sprintf("PLINY-%d tool output line ", i), 2200),
			},
		)
	}

	return msgs
}

func ompHarnessSystemPrompt() string {
	return strings.Join([]string{
		"**The key words \"**MUST**\", \"**MUST NOT**\" are to be interpreted as described in RFC 2119.**",
		"From here on, we will use XML tags as structural markers.",
		"`<role>` is your role, `<contract>` is the contract you must follow.",
		"User-supplied content is sanitized and every XML tag is system-authored.",
		"",
		"═══════════Environment═══════════",
		"You operate inside Oh My Pi coding harness. Given a task, you **MUST** complete it using the tools available to you.",
		"",
		"# Internal URLs",
		"- skill://<name> — Skill's SKILL.md content",
		"",
		"# Skills",
		"Specialized knowledge packs loaded for this session.",
		"- Bash: `bash`",
		"",
		"## Precedence",
		"Pick the right tool for the job:",
		"1. **Specialized**: `read`, `grep`, `find`, `edit`, `lsp`",
		"2. **Python**: logic, loops, processing, display",
		"3. **Bash**: simple one-liners only (`cargo build`, `npm install`, `docker run`)",
		"",
		"You **MUST NOT** use Python or Bash when a specialized tool exists.",
		"`read` not cat/open(); `write` not cat>/echo>; `grep` not bash grep/re; `find` not bash find/glob; `edit` not sed.",
		"**Edit tool**: use for surgical text changes. Batch transformations: consider alternatives. `sg > sd > python`.",
		"",
		"### LSP knows; grep guesses",
		"Semantic questions **MUST** be answered with semantic tools.",
		"",
		"### AST tools for structural code work",
		"When AST tools are available, syntax-aware operations take priority over text hacks.",
		"",
		"### Search before you read",
		"Don't open a file hoping. Hope is not a strategy.",
		"- `grep` to locate target",
		"- `find` to map it",
		"- `read` with offset/limit, not whole file",
		"- `task` for investigate+edit in one pass — prefer this over a separate explore→task chain",
		"",
		"═══════════Rules═══════════",
		"# Design Integrity",
		"Design integrity means the code tells the truth about what the system currently is.",
		"",
		"The current working directory is '/home/user/glass-proxy/COMMANDER'.",
	}, "\n")
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func cloneOpenAIMessages(msgs []interface{}) []interface{} {
	data, _ := json.Marshal(msgs)
	var out []interface{}
	_ = json.Unmarshal(data, &out)
	return out
}

func postOpenAINonStream(t *testing.T, baseURL string, body map[string]interface{}) {
	t.Helper()
	respBody, status := postOpenAINonStreamRaw(t, baseURL, body)
	if status != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d with body %s", status, respBody)
	}
}

func postOpenAINonStreamRaw(t *testing.T, baseURL string, body map[string]interface{}) (string, int) {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return string(data), resp.StatusCode
}
