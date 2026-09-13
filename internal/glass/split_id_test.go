package glass

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxy.local/app/internal/subagent"
)

func TestPrefixFingerprintIgnoresBillingAndNormalizesBetas(t *testing.T) {
	t.Parallel()

	systemA := []interface{}{
		map[string]interface{}{"type": "text", "text": "<x-anthropic-billing-header>session-a</x-anthropic-billing-header>"},
		map[string]interface{}{"type": "text", "text": strings.Repeat("stable prefix ", 500)},
	}
	systemB := []interface{}{
		map[string]interface{}{"type": "text", "text": "<x-anthropic-billing-header>session-b</x-anthropic-billing-header>"},
		map[string]interface{}{"type": "text", "text": strings.Repeat("stable prefix ", 500)},
	}
	tools := []interface{}{
		map[string]interface{}{
			"name":        "probe_tool",
			"description": "verifies shared prefix identity",
			"input_schema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	keyA := PrefixFingerprint("claude-opus-4-1", systemA, tools, "", []string{"beta-two", "beta-one", "beta-two"})
	keyB := PrefixFingerprint("claude-opus-4-1", systemB, tools, "2023-06-01", []string{"beta-one", "beta-two"})
	if keyA == "" {
		t.Fatal("expected shared prefix key")
	}
	if keyA != keyB {
		t.Fatalf("billing header / beta order should not affect prefix key: %s vs %s", keyA, keyB)
	}

	systemC := []interface{}{
		map[string]interface{}{"type": "text", "text": "<x-anthropic-billing-header>session-a</x-anthropic-billing-header>"},
		map[string]interface{}{"type": "text", "text": strings.Repeat("different stable prefix ", 400)},
	}
	keyC := PrefixFingerprint("claude-opus-4-1", systemC, tools, "2023-06-01", []string{"beta-one", "beta-two"})
	if keyC == keyA {
		t.Fatal("changing shared system content should change prefix key")
	}
}

func TestPrefixWarmerObserveStartsSingleLoop(t *testing.T) {
	t.Parallel()

	type hit struct {
		Auth string
		Beta string
		Body map[string]interface{}
	}

	hits := make(chan hit, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode warm body: %v", err)
		}
		hits <- hit{
			Auth: r.Header.Get("x-api-key"),
			Beta: r.Header.Get("anthropic-beta"),
			Body: body,
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"cache_read_input_tokens":12,"cache_creation_input_tokens":0}}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	pw := NewPrefixWarmer(PrefixWarmerConfig{
		IntervalSec: 3600,
		Upstream:    server.URL,
		ShadowDir:   tmpDir,
	})
	if pw == nil {
		t.Fatal("expected prefix warmer")
	}
	defer pw.Stop()

	system := mainSessionSystem()
	tools := sharedTools()
	meta := RequestMeta{
		APIKey:           "sk-test",
		AnthropicVersion: "2023-06-01",
		Betas:            []string{"beta-two", "beta-one"},
	}

	prefixKey := pw.Observe(meta, system, tools, "claude-opus-4-1")
	if prefixKey == "" {
		t.Fatal("expected prefix key from observe")
	}

	first := waitWarmHit(t, hits, "first warmer ping")
	if first.Auth != "sk-test" {
		t.Fatalf("expected api key propagation, got %q", first.Auth)
	}
	if first.Beta != "beta-one,beta-two,"+ExtendedCacheTTLBeta {
		t.Fatalf("expected normalized beta header, got %q", first.Beta)
	}

	systemBlocks, _ := first.Body["system"].([]interface{})
	if len(systemBlocks) != 1 {
		t.Fatalf("expected billing header stripped from warm body, got %d system blocks", len(systemBlocks))
	}
	lastSystem := systemBlocks[len(systemBlocks)-1].(map[string]interface{})
	systemCC, ok := lastSystem["cache_control"].(map[string]interface{})
	if !ok {
		t.Fatal("expected cache_control on last system block")
	}
	if got, _ := systemCC["ttl"].(string); got != ExtendedCacheTTL {
		t.Fatalf("expected system cache_control ttl %q, got %q", ExtendedCacheTTL, got)
	}

	toolBlocks, _ := first.Body["tools"].([]interface{})
	if len(toolBlocks) != 1 {
		t.Fatalf("expected tools preserved in warm body, got %d", len(toolBlocks))
	}
	lastTool := toolBlocks[len(toolBlocks)-1].(map[string]interface{})
	toolCC, ok := lastTool["cache_control"].(map[string]interface{})
	if !ok {
		t.Fatal("expected cache_control on last tool block")
	}
	if got, _ := toolCC["ttl"].(string); got != ExtendedCacheTTL {
		t.Fatalf("expected tool cache_control ttl %q, got %q", ExtendedCacheTTL, got)
	}

	pw.Observe(meta, system, tools, "claude-opus-4-1")
	select {
	case dup := <-hits:
		t.Fatalf("duplicate warm loop started for same prefix: %#v", dup)
	case <-time.After(250 * time.Millisecond):
	}

	registryPath := filepath.Join(tmpDir, "prefixes", "registry.json")
	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read prefix registry: %v", err)
	}
	var registry map[string]PrefixProfile
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("unmarshal prefix registry: %v", err)
	}
	profile, ok := registry[prefixKey]
	if !ok {
		t.Fatalf("missing prefix %s in registry", prefixKey)
	}
	if profile.LastWarmRead != 12 || profile.LastStatusCode != 200 {
		t.Fatalf("unexpected warm metrics: status=%d read=%d", profile.LastStatusCode, profile.LastWarmRead)
	}
}

func TestPrefixWarmerPingStripsExistingCacheControlBeforeReapplying(t *testing.T) {
	t.Parallel()

	type hit struct {
		Body map[string]interface{}
	}

	hits := make(chan hit, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode warm body: %v", err)
		}
		hits <- hit{Body: body}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"cache_read_input_tokens":8,"cache_creation_input_tokens":0}}`)
	}))
	defer server.Close()

	pw := NewPrefixWarmer(PrefixWarmerConfig{
		IntervalSec: 3600,
		Upstream:    server.URL,
		ShadowDir:   t.TempDir(),
	})
	if pw == nil {
		t.Fatal("expected prefix warmer")
	}
	defer pw.Stop()

	system := []interface{}{
		map[string]interface{}{
			"type":          "text",
			"text":          "system one",
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
		map[string]interface{}{
			"type":          "text",
			"text":          "system two",
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
	}
	tools := []interface{}{
		map[string]interface{}{
			"name":          "tool_one",
			"description":   "first tool",
			"input_schema":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
		map[string]interface{}{
			"name":          "tool_two",
			"description":   "second tool",
			"input_schema":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
	}

	meta := RequestMeta{
		APIKey:           "sk-test",
		AnthropicVersion: "2023-06-01",
	}
	prefixKey := pw.Observe(meta, system, tools, "claude-opus-4-6")
	if prefixKey == "" {
		t.Fatal("expected shared prefix key")
	}

	first := waitWarmHit(t, hits, "cache-control cleanup warm ping")

	systemBlocks, _ := first.Body["system"].([]interface{})
	systemCCCount := 0
	for i, raw := range systemBlocks {
		block, _ := raw.(map[string]interface{})
		_, has := block["cache_control"]
		if has {
			systemCCCount++
			if i != len(systemBlocks)-1 {
				t.Fatalf("expected system cache_control only on last block, found at %d", i)
			}
		}
	}
	if systemCCCount != 1 {
		t.Fatalf("expected exactly one system cache_control after cleanup, got %d", systemCCCount)
	}

	toolBlocks, _ := first.Body["tools"].([]interface{})
	toolCCCount := 0
	for i, raw := range toolBlocks {
		block, _ := raw.(map[string]interface{})
		_, has := block["cache_control"]
		if has {
			toolCCCount++
			if i != len(toolBlocks)-1 {
				t.Fatalf("expected tool cache_control only on last tool, found at %d", i)
			}
		}
	}
	if toolCCCount != 1 {
		t.Fatalf("expected exactly one tool cache_control after cleanup, got %d", toolCCCount)
	}
}

func TestProcessSplitIDKeepsSessionLocalCacheButSharesPrefix(t *testing.T) {
	t.Parallel()

	hits := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"cache_read_input_tokens":7,"cache_creation_input_tokens":3}}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = tmpDir
	cfg.EvictTriggerTokens = 999999

	engine := NewEngine(cfg)
	pw := NewPrefixWarmer(PrefixWarmerConfig{
		IntervalSec: 3600,
		Upstream:    server.URL,
		ShadowDir:   tmpDir,
	})
	if pw == nil {
		t.Fatal("expected prefix warmer")
	}
	defer pw.Stop()
	engine.SetPrefixWarmer(pw)

	// Use 10+ messages so the classifier treats these as main conversations,
	// not agent_tool subagents (which get isolated caches instead of shared prefixes).
	// See INSIGHT-2026-2-25: message count < 10 with full system prompt = subagent.
	msgsA := make([]interface{}, 10)
	msgsB := make([]interface{}, 10)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			msgsA[i] = userTextMessage(fmt.Sprintf("session one msg %d", i))
			msgsB[i] = userTextMessage(fmt.Sprintf("session two msg %d", i))
		} else {
			msgsA[i] = map[string]interface{}{"role": "assistant", "content": fmt.Sprintf("response A-%d", i)}
			msgsB[i] = map[string]interface{}{"role": "assistant", "content": fmt.Sprintf("response B-%d", i)}
		}
	}
	bodyA := map[string]interface{}{
		"model":    "claude-opus-4-1",
		"system":   mainSessionSystem(),
		"tools":    sharedTools(),
		"messages": msgsA,
	}
	bodyB := map[string]interface{}{
		"model":    "claude-opus-4-1",
		"system":   mainSessionSystem(),
		"tools":    sharedTools(),
		"messages": msgsB,
	}

	metaA := RequestMeta{SessionKey: "session-A", APIKey: "sk-test", AnthropicVersion: "2023-06-01", Betas: []string{"beta-two", "beta-one"}}
	metaB := RequestMeta{SessionKey: "session-B", APIKey: "sk-test", AnthropicVersion: "2023-06-01", Betas: []string{"beta-one", "beta-two"}}

	resultA := engine.Process(bodyA, metaA)
	resultB := engine.Process(bodyB, metaB)

	if resultA.SessionKey != "session-A" || resultB.SessionKey != "session-B" {
		t.Fatalf("expected explicit session keys to be preserved: %+v %+v", resultA, resultB)
	}
	if resultA.PrefixKey == "" || resultA.PrefixKey != resultB.PrefixKey {
		t.Fatalf("expected shared prefix key across sessions: %q vs %q", resultA.PrefixKey, resultB.PrefixKey)
	}

	waitWarmHit(t, hits, "shared prefix warm ping")
	select {
	case <-hits:
		t.Fatal("expected one shared warm loop for shared prefix")
	case <-time.After(250 * time.Millisecond):
	}

	snapA := loadSnapshot(t, filepath.Join(tmpDir, "session-A", "localcache.json"))
	snapB := loadSnapshot(t, filepath.Join(tmpDir, "session-B", "localcache.json"))
	if snapA.ConvID != "session-A" || snapB.ConvID != "session-B" {
		t.Fatalf("unexpected snapshot conv IDs: %q %q", snapA.ConvID, snapB.ConvID)
	}
	if len(snapA.Messages) != 10 || len(snapB.Messages) != 10 {
		t.Fatalf("expected independent 10-message caches, got %d and %d", len(snapA.Messages), len(snapB.Messages))
	}

	msgAText := firstTextBlock(t, snapA.Messages[0].Msg)
	msgBText := firstTextBlock(t, snapB.Messages[0].Msg)
	if msgAText == msgBText {
		t.Fatalf("expected session-local message content, got same text %q", msgAText)
	}

	registryData, err := os.ReadFile(filepath.Join(tmpDir, "prefixes", "registry.json"))
	if err != nil {
		t.Fatalf("read split-id registry: %v", err)
	}
	var registry map[string]PrefixProfile
	if err := json.Unmarshal(registryData, &registry); err != nil {
		t.Fatalf("unmarshal split-id registry: %v", err)
	}
	if len(registry) != 1 {
		t.Fatalf("expected one shared prefix entry, got %d", len(registry))
	}
}

func TestSmallSystemBypassesGlassCacheAndStripsAllCacheControl(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = tmpDir
	cfg.EvictTriggerTokens = 999999

	engine := NewEngine(cfg)
	body := map[string]interface{}{
		"cache_control": map[string]interface{}{"type": "ephemeral"},
		"model":         "claude-opus-4-1",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "tiny child prompt", "cache_control": map[string]interface{}{"type": "ephemeral"}},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"name":          "probe_tool",
				"description":   "verifies no-cache path",
				"cache_control": map[string]interface{}{"type": "ephemeral"},
				"input_schema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role":          "user",
				"cache_control": map[string]interface{}{"type": "ephemeral"},
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "world", "cache_control": map[string]interface{}{"type": "ephemeral"}},
				},
			},
		},
	}
	meta := RequestMeta{
		SessionKey:  "child-session",
		RequestKey:  "child-session_sub_deadbeef",
		AffinityKey: "parent-session",
		Subagent:    subagent.Classify(body),
	}

	result := engine.Process(body, meta)
	if result.SessionKey != "child-session" {
		t.Fatalf("expected child request session key, got %q", result.SessionKey)
	}
	if result.RequestKey != "child-session_sub_deadbeef" {
		t.Fatalf("expected request key to be preserved, got %q", result.RequestKey)
	}
	if result.AffinityKey != "parent-session" {
		t.Fatalf("expected parent affinity key, got %q", result.AffinityKey)
	}
	// Tool-bearing small_system subagents now use IsolateSession (not BypassMessageCache),
	// so they DO create LocalCache entries for Anthropic prefix caching.
	if len(engine.caches) != 1 {
		t.Fatalf("tool-bearing small-system should create 1 LocalCache entry, got %d", len(engine.caches))
	}
	// Top-level cache_control is left intact — Glass adds its own explicit
	// breakpoints during Build, which override automatic caching anyway.
}

func TestReferencePairRemainsStableAcrossLastAPIInputChanges(t *testing.T) {
	t.Parallel()

	state := &SessionState{
		ConvID:       "conv-stable-ref",
		EvictedCount: 47,
		BatchCount:   3,
		LastAPIInput: 160340,
		TopicHints: []string{
			"Messages 1-19: research remaining issue",
			"Messages 20-35: verify broader items",
		},
	}

	user1, asst1 := buildReferenceMessages(state, "/tmp/glass")
	if user1 == nil || asst1 == nil {
		t.Fatal("expected frozen reference pair")
	}

	state.LastAPIInput = 999999
	user2, asst2 := buildReferenceMessages(state, "/tmp/glass")
	if got, want := user2["content"].([]interface{})[0].(map[string]interface{})["text"], user1["content"].([]interface{})[0].(map[string]interface{})["text"]; got != want {
		t.Fatalf("reference user text changed across LastAPIInput update:\nold=%q\nnew=%q", want, got)
	}
	if got, want := asst2["content"].([]interface{})[0].(map[string]interface{})["text"], asst1["content"].([]interface{})[0].(map[string]interface{})["text"]; got != want {
		t.Fatalf("reference assistant text changed across LastAPIInput update:\nold=%q\nnew=%q", want, got)
	}
}

func TestReferencePairRemainsStableAcrossAdditionalEvictionBatches(t *testing.T) {
	t.Parallel()

	state := &SessionState{
		ConvID:       "conv-stable-batches",
		EvictedCount: 10,
		BatchCount:   1,
		TopicHints: []string{
			"Messages 1-10: initial hint",
		},
	}

	user1, asst1 := buildReferenceMessages(state, "/tmp/glass")
	if user1 == nil || asst1 == nil {
		t.Fatal("expected frozen reference pair")
	}

	userText1 := user1["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(userText1, "/tmp/glass/conv-stable-batches/shadow.md") {
		t.Fatalf("expected reference text to point at shadow file, got %q", userText1)
	}
	for _, forbidden := range []string{
		"Messages 1-",
		"Topics covered in evicted history",
		"Current conversation continues from message",
	} {
		if strings.Contains(userText1, forbidden) {
			t.Fatalf("reference text should not include mutable batch metadata %q: %q", forbidden, userText1)
		}
	}

	state.EvictedCount = 80
	state.BatchCount = 5
	state.TopicHints = append(state.TopicHints, "Messages 11-80: later hint")

	user2, asst2 := buildReferenceMessages(state, "/tmp/glass")
	userText2 := user2["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	asstText1 := asst1["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	asstText2 := asst2["content"].([]interface{})[0].(map[string]interface{})["text"].(string)

	if userText2 != userText1 {
		t.Fatalf("reference user text changed across additional eviction batches:\nold=%q\nnew=%q", userText1, userText2)
	}
	if asstText2 != asstText1 {
		t.Fatalf("reference assistant text changed across additional eviction batches:\nold=%q\nnew=%q", asstText1, asstText2)
	}
	if got, want := state.ReferenceBatch, 1; got != want {
		t.Fatalf("expected first rendering batch to stay fixed at %d, got %d", want, got)
	}
}

func TestReferencePairRefreshesPoisonedStoredSummary(t *testing.T) {
	t.Parallel()

	state := &SessionState{
		ConvID:         "conv-refresh-ref",
		EvictedCount:   228,
		BatchCount:     2,
		ReferenceBatch: 1,
		ReferenceUser:  "CONTEXT NOTE: Earlier conversation history has been evicted from the visible window.\nFrozen summary of the first evicted batch: . now research what is needed to execute the plan and rpeort back [Request interrupted by user] proc...\nDurable facts from evicted history may appear in the system prompt under '# Durable operational context'. A recent 'Operational Context' note may also appear later in the conversation.\nFull history is saved to: /tmp/glass/conv-refresh-ref/shadow.md\nUse the Read tool to inspect that file when older details are needed.",
		ReferenceAsst:  referenceAssistantText,
		TopicHints: []string{
			"Messages 1-228: .\nnow research what is needed to execute the plan and rpeort back\n[Request interrupted by user]\nproceed",
		},
	}

	user, _ := buildReferenceMessages(state, "/tmp/glass")
	userText := user["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if strings.Contains(userText, "[Request interrupted by user]") {
		t.Fatalf("expected reference text to drop interrupt marker, got %q", userText)
	}
	for _, needle := range []string{
		"Relevant prior history is saved to: /tmp/glass/conv-refresh-ref/shadow.md",
		"Do not infer missing details from bookmarks alone.",
	} {
		if !strings.Contains(userText, needle) {
			t.Fatalf("expected refreshed reference text to contain %q, got %q", needle, userText)
		}
	}
	if strings.Contains(userText, "Frozen summary of the first evicted batch") {
		t.Fatalf("did not expect frozen summary in reference text, got %q", userText)
	}
}

func TestGenerateTopicHints_StripsInterruptArtifacts(t *testing.T) {
	t.Parallel()

	evicted := []map[string]interface{}{
		userTextMessage(".\nnow research what is needed to execute the plan and rpeort back\n[Request interrupted by user]\nproceed"),
		assistantTextMessage("working on it"),
	}

	hints := generateTopicHints(evicted, 1, 8)
	if len(hints) != 1 {
		t.Fatalf("expected one hint, got %#v", hints)
	}
	if strings.Contains(hints[0], "[Request interrupted by user]") {
		t.Fatalf("expected hint to strip interrupt marker, got %q", hints[0])
	}
	if !strings.Contains(hints[0], "now research what is needed to execute the plan and rpeort back") {
		t.Fatalf("expected cleaned hint text, got %q", hints[0])
	}
}

func TestStablePrefixHashIgnoresMutableTailGrowth(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("stable-prefix")
	cache.Ingest(testConversation(10))
	cache.Build()
	initialHash := cache.lastPrefixHash
	if initialHash == "" {
		t.Fatal("expected initial prefix hash")
	}
	initialAnchor := cache.breakpointAnchor

	cache.Ingest(testConversation(12))
	cache.Build()

	if cache.breakpointAnchor != initialAnchor {
		t.Fatalf("expected anchor to stay stable before threshold advance: %d -> %d", initialAnchor, cache.breakpointAnchor)
	}
	if cache.lastPrefixHash != initialHash {
		t.Fatalf("expected stable prefix hash to ignore tail growth: %s -> %s", initialHash, cache.lastPrefixHash)
	}
}

func TestEvictedSessionsHaveBreakpointAfterEviction(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = tmpDir
	cfg.EvictTriggerTokens = 1000
	cfg.EvictTargetTokens = 500
	cfg.AnchorKeepMsgs = 4
	cfg.RecentKeepMsgs = 6

	engine := NewEngine(cfg)
	body := map[string]interface{}{
		"system":   mainSessionSystem(),
		"messages": testConversation(20),
	}

	engine.Process(body, RequestMeta{SessionKey: "session-evict"})

	// After eviction, message count must be well below 20.
	msgs := body["messages"].([]interface{})
	if len(msgs) >= 20 {
		t.Fatalf("expected eviction to reduce messages, got %d", len(msgs))
	}

	// At least one breakpoint must exist.
	anchors := breakpointIndexes(msgs)
	if len(anchors) == 0 {
		t.Fatalf("expected at least one breakpoint after eviction, got none")
	}
}

func TestBuildForRequestPreservesStableAnchorWithoutSanitizer(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("normalized-anchor")
	cache.Ingest(testConversation(12))

	view := cache.BuildForRequest(false)
	if got, want := len(view.Messages), 11; got != want {
		t.Fatalf("expected message count %d, got %d", want, got)
	}
	if got, want := view.AnchorIdx, 10; got != want {
		t.Fatalf("expected anchor %d, got %d", want, got)
	}
	for i, raw := range view.Messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("message %d is not a map", i)
		}
		if _, ok := msg[requestSourceIndexKey]; ok {
			t.Fatalf("normalized message %d leaked internal source marker", i)
		}
	}

	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.EvictTriggerTokens = 999999
	cfg.StripThinking = false
	engine := NewEngine(cfg)

	body := map[string]interface{}{
		"system":   mainSessionSystem(),
		"messages": testConversation(12),
	}

	engine.Process(body, RequestMeta{SessionKey: "normalized-anchor"})
	actualAnchors := breakpointIndexes(body["messages"].([]interface{}))
	found := false
	for _, idx := range actualAnchors {
		if idx == 10 {
			found = true
		}
		if idx == 9 {
			t.Fatalf("breakpoint fell back to tail index 9 instead of anchor 10: %v", actualAnchors)
		}
	}
	if !found {
		t.Fatalf("expected breakpoint at anchor 10, got %v", actualAnchors)
	}
}

func TestBreakpointPreservesPreviousStableAnchorWhenAdvancing(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("preserve-prev-anchor")
	cache.Ingest(testConversation(10))
	firstAnchor := cache.BreakpointAnchor()
	if firstAnchor < 0 {
		t.Fatalf("expected initial anchor, got %d", firstAnchor)
	}

	// Add enough messages to exceed breakpointAdvanceThreshold (8) and trigger advance.
	cache.Ingest(testConversation(10 + breakpointAdvanceThreshold + 2))
	view := cache.BuildForRequest(false)
	if got, want := view.PrevAnchorIdx, firstAnchor; got != want {
		t.Fatalf("expected previous anchor %d, got %d", want, got)
	}
	if view.AnchorIdx <= view.PrevAnchorIdx {
		t.Fatalf("expected current anchor to advance past previous anchor, got current=%d prev=%d", view.AnchorIdx, view.PrevAnchorIdx)
	}

	body := map[string]interface{}{
		"messages": view.Messages,
	}
	placeBreakpoint(body, view.AnchorIdx, view.PrevAnchorIdx, -1)
	anchors := breakpointIndexes(body["messages"].([]interface{}))
	if len(anchors) != 2 {
		t.Fatalf("expected two preserved breakpoints, got %v", anchors)
	}
	if anchors[0] != view.PrevAnchorIdx || anchors[1] != view.AnchorIdx {
		t.Fatalf("expected breakpoints at prev=%d and current=%d, got %v", view.PrevAnchorIdx, view.AnchorIdx, anchors)
	}
	for _, idx := range anchors {
		msg := body["messages"].([]interface{})[idx].(map[string]interface{})
		content := msg["content"].([]interface{})
		block := content[len(content)-1].(map[string]interface{})
		cc, ok := block["cache_control"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected cache_control block at anchor %d", idx)
		}
		if got, _ := cc["ttl"].(string); got != ExtendedCacheTTL {
			t.Fatalf("expected anchor %d ttl %q, got %q", idx, ExtendedCacheTTL, got)
		}
	}
}

func TestPlaceBreakpointUpgradesEarlierCacheControlTTL(t *testing.T) {
	t.Parallel()

	body := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{
				"type":          "text",
				"text":          "system",
				"cache_control": map[string]interface{}{"type": "ephemeral"},
			},
		},
		"messages": []interface{}{
			userTextMessage("hello"),
			assistantTextMessage("world"),
		},
	}

	placeBreakpoint(body, 1, -1, -1)

	system := body["system"].([]interface{})
	systemCC := system[0].(map[string]interface{})["cache_control"].(map[string]interface{})
	if got, _ := systemCC["ttl"].(string); got != ExtendedCacheTTL {
		t.Fatalf("expected system ttl %q, got %q", ExtendedCacheTTL, got)
	}

	msg := body["messages"].([]interface{})[1].(map[string]interface{})
	block := msg["content"].([]interface{})[0].(map[string]interface{})
	msgCC := block["cache_control"].(map[string]interface{})
	if got, _ := msgCC["ttl"].(string); got != ExtendedCacheTTL {
		t.Fatalf("expected message ttl %q, got %q", ExtendedCacheTTL, got)
	}
}

func TestReferenceInjectionDoesNotSplitToolUseAndToolResult(t *testing.T) {
	t.Parallel()

	cfg := DefaultGlassConfig()
	cfg.AnchorKeepMsgs = 4
	state := &SessionState{
		ConvID:       "tool-boundary",
		EvictedCount: 10,
		BatchCount:   1,
	}
	body := map[string]interface{}{
		"messages": []interface{}{
			userTextMessage("anchor 0"),
			assistantTextMessage("anchor 1"),
			userTextMessage("anchor 2"),
			assistantToolUseMessage("toolu_boundary", "bash"),
			userToolResultMessage("toolu_boundary", "done"),
			assistantTextMessage("after tool"),
		},
	}

	if !injectReferenceMessages(body, state, cfg, false) {
		t.Fatal("expected reference injection to occur")
	}

	msgs := body["messages"].([]interface{})
	if got, want := len(validateMessageStructure(msgs)), 0; got != want {
		t.Fatalf("expected valid structure after reference injection, got %v", validateMessageStructure(msgs))
	}
	roles := make([]string, 0, len(msgs))
	for _, raw := range msgs {
		msg := raw.(map[string]interface{})
		roles = append(roles, msg["role"].(string))
	}
	if roles[4] != "user" {
		t.Fatalf("expected original tool_result to remain immediately after tool_use, got roles %v", roles[:7])
	}
	refUserText := msgs[len(msgs)-2].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["text"]
	if refUserText != state.ReferenceUser {
		t.Fatalf("expected reference user message near tail, got %q", refUserText)
	}
}

func TestReferenceInjectionAvoidsAssistantFinalWhenUserFinalRequired(t *testing.T) {
	t.Parallel()

	cfg := DefaultGlassConfig()
	cfg.AnchorKeepMsgs = 4
	state := &SessionState{
		ConvID:       "user-final-boundary",
		EvictedCount: 10,
		BatchCount:   1,
	}
	body := map[string]interface{}{
		"messages": []interface{}{
			userTextMessage("anchor 0"),
			assistantTextMessage("anchor 1"),
			userTextMessage("anchor 2"),
			assistantToolUseMessage("toolu_boundary", "bash"),
			userToolResultMessage("toolu_boundary", "done"),
		},
	}

	if !injectReferenceMessages(body, state, cfg, true) {
		t.Fatal("expected reference injection to occur")
	}

	msgs := body["messages"].([]interface{})
	if issues := validateOutboundRequestMessages(msgs); len(issues) > 0 {
		t.Fatalf("expected valid outbound structure after reference injection, got %v", issues)
	}
	lastRole, _ := msgs[len(msgs)-1].(map[string]interface{})["role"].(string)
	if lastRole != "user" {
		t.Fatalf("expected final message to remain user, got %q", lastRole)
	}
	found := -1
	for i, raw := range msgs {
		msg := raw.(map[string]interface{})
		content, _ := msg["content"].([]interface{})
		if len(content) == 0 {
			continue
		}
		block, _ := content[0].(map[string]interface{})
		text, _ := block["text"].(string)
		if text == state.ReferenceUser {
			found = i
			break
		}
	}
	if found < 0 {
		t.Fatal("expected reference user message to be injected")
	}
	if found == len(msgs)-2 {
		t.Fatalf("expected user-final injection to avoid appending the reference pair at the tail, got index %d", found)
	}
}

func TestEvictKeepsImmediateToolUseResultPairAtomic(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("atomic-tool-pair")
	cache.Ingest([]interface{}{
		userTextMessage(strings.Repeat("anchor ", 40)),
		assistantToolUseMessage("toolu_atomic", "bash"),
		userToolResultMessage("toolu_atomic", strings.Repeat("done ", 40)),
		assistantTextMessage(strings.Repeat("after tool ", 40)),
		userTextMessage(strings.Repeat("recent one ", 40)),
		assistantTextMessage(strings.Repeat("recent two ", 40)),
	})

	evicted := cache.Evict(300, 1, 2)
	if len(evicted) == 0 {
		t.Fatal("expected eviction to occur")
	}

	view := cache.BuildForRequest(false)
	if issues := validateMessageStructure(view.Messages); len(issues) > 0 {
		t.Fatalf("expected retained view to remain valid, got %v", issues)
	}
}

func TestPinFrameArchivesBridgeAndBuildsPinnedView(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("pinned-frame-view")
	cache.Ingest(numberedConversation(18))

	evicted := cache.PinFrame(4, 4, false)
	if got, want := len(evicted), 10; got != want {
		t.Fatalf("expected %d bridge messages archived, got %d", want, got)
	}

	view := cache.BuildPinnedFrameForRequest(false, 4, 4, false)
	if got, want := len(view.Messages), 7; got != want {
		t.Fatalf("expected pinned view size %d, got %d", want, got)
	}
	if got, want := view.AnchorIdx, 3; got != want {
		t.Fatalf("expected pinned anchor %d, got %d", want, got)
	}
	if got, want := view.ReferenceInsertAt, 4; got != want {
		t.Fatalf("expected reference insert point %d, got %d", want, got)
	}

	body := map[string]interface{}{"messages": view.Messages}
	state := &SessionState{ConvID: "pinned-frame-view", EvictedCount: len(evicted), BatchCount: 1}
	cfg := DefaultGlassConfig()
	cfg.AnchorKeepMsgs = 4
	injected, insertAt := injectReferenceMessagesAt(body, state, cfg, false, view.ReferenceInsertAt)
	if !injected {
		t.Fatal("expected pinned view to inject frozen reference pair")
	}
	if got, want := insertAt, 4; got != want {
		t.Fatalf("expected stable insertion point %d, got %d", want, got)
	}

	msgs := body["messages"].([]interface{})
	if got, want := firstTextBlock(t, msgs[6].(map[string]interface{})), "msg 14"; got != want {
		t.Fatalf("expected hot tail to start at %q, got %q", want, got)
	}
}

func TestPinnedFrameKeepsStableTailBreakpointAcrossSmallTailGrowth(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("pinned-stable-tail")
	cache.Ingest(numberedConversation(18))

	evicted := cache.PinFrame(4, 4, false)
	if len(evicted) == 0 {
		t.Fatal("expected pinned frame to archive bridge messages")
	}

	cfg := DefaultGlassConfig()
	cfg.AnchorKeepMsgs = 4
	state := &SessionState{ConvID: "pinned-stable-tail", EvictedCount: len(evicted), BatchCount: 1}

	buildAnchors := func(view requestView) []int {
		body := map[string]interface{}{"messages": view.Messages}
		injected, insertAt := injectReferenceMessagesAt(body, state, cfg, false, view.ReferenceInsertAt)
		if !injected {
			t.Fatal("expected reference injection for pinned frame")
		}
		msgs := body["messages"].([]interface{})
		anchorIdx := insertAt + 1
		prevAnchorIdx := adjustBreakpointForReferenceInjection(view.AnchorIdx, injected, insertAt, len(msgs))
		if prevAnchorIdx <= anchorIdx {
			prevAnchorIdx = -1
		}
		placeBreakpoint(body, anchorIdx, prevAnchorIdx, -1)
		return breakpointIndexes(body["messages"].([]interface{}))
	}

	initialAnchors := buildAnchors(cache.BuildPinnedFrameForRequest(false, 4, 4, false))
	if got, want := initialAnchors, []int{5}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected initial pinned breakpoints %v, got %v", want, got)
	}

	cache.Ingest(numberedConversation(19))

	nextAnchors := buildAnchors(cache.BuildPinnedFrameForRequest(false, 4, 4, false))
	if len(nextAnchors) == 0 || nextAnchors[0] != 5 {
		t.Fatalf("expected first breakpoint at 5 to remain stable after tail growth, got %v", nextAnchors)
	}
}

func TestPinnedFrameRequestViewStaysBoundedAsTailGrows(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("pinned-bounded-tail")
	cache.Ingest(numberedConversation(18))

	evicted := cache.PinFrame(4, 4, false)
	if len(evicted) == 0 {
		t.Fatal("expected pinned frame to archive bridge messages")
	}

	for n := 19; n <= 26; n++ {
		cache.Ingest(numberedConversation(n))
	}

	view := cache.BuildPinnedFrameForRequest(false, 4, 4, false)
	if got, want := len(view.Messages), 7; got != want {
		t.Fatalf("expected bounded pinned view size %d after tail growth, got %d", want, got)
	}

	body := map[string]interface{}{"messages": view.Messages}
	state := &SessionState{ConvID: "pinned-bounded-tail", EvictedCount: len(evicted), BatchCount: 1}
	cfg := DefaultGlassConfig()
	cfg.AnchorKeepMsgs = 4

	injected, insertAt := injectReferenceMessagesAt(body, state, cfg, false, view.ReferenceInsertAt)
	if !injected {
		t.Fatal("expected pinned view to inject frozen reference pair")
	}
	if got, want := insertAt, 4; got != want {
		t.Fatalf("expected stable insertion point %d, got %d", want, got)
	}

	msgs := body["messages"].([]interface{})
	if got, want := firstTextBlock(t, msgs[6].(map[string]interface{})), "msg 22"; got != want {
		t.Fatalf("expected bounded hot tail to start at %q, got %q", want, got)
	}

	anchorIdx := insertAt + 1
	prevAnchorIdx := adjustBreakpointForReferenceInjection(view.AnchorIdx, injected, insertAt, len(msgs))
	if prevAnchorIdx <= anchorIdx {
		prevAnchorIdx = -1
	}
	placeBreakpoint(body, anchorIdx, prevAnchorIdx, -1)

	if got, want := breakpointIndexes(msgs), []int{5}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected pinned breakpoints %v after tail growth, got %v", want, got)
	}
}

func TestPinFrameReturnsNilForToolResultHeavyTail(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("pinned-tool-result-tail")
	cache.Ingest(toolResultHeavyConversation(6))

	// PinFrame correctly returns nil when the entire non-anchor section
	// is tool_use/tool_result pairs — there's no safe split point that
	// won't orphan tool results.
	evicted := cache.PinFrame(4, 4, false)
	if len(evicted) != 0 {
		t.Fatalf("expected PinFrame to refuse splitting tool-result-heavy tail, got %d evicted", len(evicted))
	}

	// Regular BuildForRequest should still produce a valid view.
	view := cache.BuildForRequest(false)
	if issues := validateMessageStructure(view.Messages); len(issues) > 0 {
		t.Fatalf("expected regular view to validate cleanly, got %v", issues)
	}
}

func TestAdjustBreakpointForReferenceInjectionUsesActualInsertAt(t *testing.T) {
	t.Parallel()

	cfg := DefaultGlassConfig()
	cfg.AnchorKeepMsgs = 4
	state := &SessionState{ConvID: "shifted-reference-boundary", EvictedCount: 2, BatchCount: 1}

	body := map[string]interface{}{
		"messages": []interface{}{
			userTextMessage("anchor 0"),
			assistantTextMessage("anchor 1"),
			userTextMessage("anchor 2"),
			assistantTextMessage("anchor 3"),
			assistantToolUseMessage("toolu_shift", "bash"),
			userToolResultMessage("toolu_shift", "done"),
			assistantTextMessage("tail assistant"),
			userTextMessage("tail user"),
			assistantTextMessage("tail final"),
		},
	}

	injected, insertAt := injectReferenceMessagesAt(body, state, cfg, false, cfg.AnchorKeepMsgs)
	if !injected {
		t.Fatal("expected reference injection to occur")
	}
	if got, want := insertAt, 7; got != want {
		t.Fatalf("expected shifted insertion point %d, got %d", want, got)
	}

	msgs := body["messages"].([]interface{})
	if got, want := adjustBreakpointForReferenceInjection(6, injected, insertAt, len(msgs)), 6; got != want {
		t.Fatalf("expected anchor before shifted insertion to stay at %d, got %d", want, got)
	}
	if got, want := adjustBreakpointForReferenceInjection(7, injected, insertAt, len(msgs)), 9; got != want {
		t.Fatalf("expected anchor at shifted insertion to move to %d, got %d", want, got)
	}
	if got, want := adjustBreakpointForReferenceInjection(8, injected, insertAt, len(msgs)), 10; got != want {
		t.Fatalf("expected anchor after shifted insertion to move to %d, got %d", want, got)
	}

	if got := adjustBreakpointForReferenceInjection(6, injected, cfg.AnchorKeepMsgs, len(msgs)); got == 6 {
		t.Fatal("expected old anchor-keep translation to differ once insertion shifts")
	}
}

func TestProcessEvictedSessionBuildsValidOutboundRequest(t *testing.T) {
	t.Parallel()

	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	cfg.AnchorKeepMsgs = 4
	cfg.RecentKeepMsgs = 4
	cfg.EvictTriggerTokens = 999999

	engine := NewEngine(cfg)
	cache := NewLocalCache("evicted-process")
	cache.Ingest(numberedConversation(18))
	evicted := cache.Evict(200, cfg.AnchorKeepMsgs, cfg.RecentKeepMsgs)
	if len(evicted) == 0 {
		t.Fatal("expected eviction to occur")
	}
	engine.caches["evicted-process"] = cache
	engine.sessions.sessions["evicted-process"] = &SessionState{
		ConvID:        "evicted-process",
		EvictedHashes: make(map[string]bool),
		EvictedCount:  len(evicted),
		BatchCount:    1,
	}

	body := map[string]interface{}{
		"system":   mainSessionSystem(),
		"messages": numberedConversation(19),
	}

	result := engine.Process(body, RequestMeta{SessionKey: "evicted-process", RequestKey: "evicted-process"})
	if result.InvalidRequestError != "" {
		t.Fatalf("expected valid outbound request after eviction, got %q", result.InvalidRequestError)
	}

	msgs := body["messages"].([]interface{})
	if issues := validateOutboundRequestMessages(msgs); len(issues) > 0 {
		t.Fatalf("expected valid outbound structure after eviction, got %v", issues)
	}
	anchors := breakpointIndexes(msgs)
	if len(anchors) == 0 {
		t.Fatal("expected at least one cache breakpoint")
	}
	last := msgs[len(msgs)-1].(map[string]interface{})
	if got := last["role"]; got != "user" {
		t.Fatalf("expected outbound request to end with user, got %v", got)
	}
}

func TestRepairBrokenToolBoundariesMarksLegacyOrphansAsReferences(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("legacy-repair")
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("anchor"), Role: "user", Tokens: 10},
		{Msg: assistantToolUseMessage("toolu_legacy", "bash"), Role: "assistant", Tokens: 10},
		{
			Msg:         map[string]interface{}{"role": "user", "content": "[Message 2 moved to shadow file - 10 tokens]"},
			Role:        "user",
			IsReference: true,
			Tokens:      50,
		},
		{Msg: assistantTextMessage("after"), Role: "assistant", Tokens: 10},
	}

	cache.mu.Lock()
	repaired := cache.repairBrokenToolBoundariesLocked()
	cache.mu.Unlock()
	if repaired == 0 {
		t.Fatal("expected legacy repair to mark orphaned tool boundary")
	}

	view := cache.BuildForRequest(false)
	if issues := validateMessageStructure(view.Messages); len(issues) > 0 {
		t.Fatalf("expected repaired view to validate cleanly, got %v", issues)
	}
}

// TestRepairMergesConsecutiveSameRoleAfterOrphanMarking verifies that when
// repairBrokenToolBoundariesLocked marks a user tool_result as reference
// (because its paired tool_use was evicted), the resulting consecutive
// assistant messages are merged into a single message.
func TestRepairMergesConsecutiveSameRoleAfterOrphanMarking(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("merge-consecutive")
	cache.messages = []CachedMsg{
		// msg[0]: user anchor
		{Msg: userTextMessage("hello"), Role: "user", Tokens: 10},
		// msg[1]: assistant text (no tool_use)
		{Msg: assistantTextMessage("first reply"), Role: "assistant", Tokens: 10},
		// msg[2]: user with orphan tool_result (matching tool_use was evicted)
		{Msg: userToolResultMessage("toolu_gone", "some result"), Role: "user", Tokens: 10},
		// msg[3]: assistant text -- will become consecutive with msg[1] after repair
		{Msg: assistantTextMessage("second reply"), Role: "assistant", Tokens: 10},
		// msg[4]: user to close the conversation
		{Msg: userTextMessage("thanks"), Role: "user", Tokens: 10},
	}

	cache.mu.Lock()
	repaired := cache.repairBrokenToolBoundariesLocked()
	cache.mu.Unlock()
	if repaired == 0 {
		t.Fatal("expected repair to fix orphan tool_result + merge consecutive")
	}

	view := cache.BuildForRequest(false)
	if issues := validateMessageStructure(view.Messages); len(issues) > 0 {
		t.Fatalf("expected merged view to have no structural issues, got %v", issues)
	}
	// Should be 3 messages: user, assistant(merged), user
	if got := len(view.Messages); got != 3 {
		t.Fatalf("expected 3 messages after merge, got %d", got)
	}
	// The merged assistant should contain content from both "first reply" and "second reply"
	merged := view.Messages[1].(map[string]interface{})
	content, _ := merged["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected merged assistant to have 2 content blocks, got %d", len(content))
	}
	block0, _ := content[0].(map[string]interface{})
	block1, _ := content[1].(map[string]interface{})
	if got := block0["text"]; got != "first reply" {
		t.Fatalf("expected first block text 'first reply', got %q", got)
	}
	if got := block1["text"]; got != "second reply" {
		t.Fatalf("expected second block text 'second reply', got %q", got)
	}
}

func TestIngestMergesSplitUserToolResultAndTrailingUserPrompt(t *testing.T) {
	cache := NewLocalCache("conv")

	added := cache.Ingest([]interface{}{
		userTextMessage("start"),
		assistantToolUseMessage("toolu_a", "bash"),
		userToolResultMessage("toolu_a", "done"),
		userTextMessage("continue"),
	})
	if got, want := added, 4; got != want {
		t.Fatalf("expected %d ingested messages, got %d", want, got)
	}
	if !cache.messages[3].IsReference {
		t.Fatal("expected trailing user prompt to be absorbed and marked reference")
	}
	if got, want := cache.Len(), 3; got != want {
		t.Fatalf("expected %d retained messages after merge, got %d", want, got)
	}

	view := cache.BuildForRequest(false)
	if issues := validateOutboundRequestMessages(view.Messages); len(issues) > 0 {
		t.Fatalf("expected merged request view to validate cleanly, got %v", issues)
	}

	last := view.Messages[len(view.Messages)-1].(map[string]interface{})
	content, _ := last["content"].([]interface{})
	if got, want := len(content), 2; got != want {
		t.Fatalf("expected merged user message to have %d content blocks, got %d", want, got)
	}
	first, _ := content[0].(map[string]interface{})
	second, _ := content[1].(map[string]interface{})
	if got, _ := first["type"].(string); got != "tool_result" {
		t.Fatalf("expected merged user message to keep tool_result first, got %q", got)
	}
	if got, _ := second["type"].(string); got != "text" {
		t.Fatalf("expected merged user message to append user text, got %q", got)
	}
}

func waitWarmHit[T any](t *testing.T, ch <-chan T, label string) T {
	t.Helper()
	select {
	case hit := <-ch:
		return hit
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		var zero T
		return zero
	}
}

func loadSnapshot(t *testing.T, path string) localCacheSnapshot {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot %s: %v", path, err)
	}
	var snap localCacheSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal snapshot %s: %v", path, err)
	}
	return snap
}

func firstTextBlock(t *testing.T, msg map[string]interface{}) string {
	t.Helper()
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("message missing structured content: %#v", msg)
	}
	block, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("invalid content block: %#v", content[0])
	}
	text, _ := block["text"].(string)
	return text
}

func cachedTestMsg(msg map[string]interface{}, tokens int) CachedMsg {
	role, _ := msg["role"].(string)
	return CachedMsg{
		Msg:    msg,
		Hash:   msgHash(msg),
		Role:   role,
		Tokens: tokens,
	}
}

func userTextMessage(text string) map[string]interface{} {
	return map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": text},
		},
	}
}

func assistantTextMessage(text string) map[string]interface{} {
	return map[string]interface{}{
		"role": "assistant",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": text},
		},
	}
}

func breakpointIndexes(msgs []interface{}) []int {
	var idxs []int
	for i, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := msg["cache_control"]; ok {
			idxs = append(idxs, i)
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, blockRaw := range content {
			block, ok := blockRaw.(map[string]interface{})
			if !ok {
				continue
			}
			if _, ok := block["cache_control"]; ok {
				idxs = append(idxs, i)
				break
			}
		}
	}
	return idxs
}

func countCacheControlsBody(body map[string]interface{}) int {
	count := 0
	if _, ok := body["cache_control"]; ok {
		count++
	}
	count += countCacheControlsArray(body["system"])
	count += countCacheControlsArray(body["tools"])
	msgs, _ := body["messages"].([]interface{})
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := msg["cache_control"]; ok {
			count++
		}
		if content, ok := msg["content"].([]interface{}); ok {
			count += countCacheControlsArray(content)
		}
	}
	return count
}

func countCacheControlsArray(v interface{}) int {
	arr, ok := v.([]interface{})
	if !ok {
		return 0
	}
	count := 0
	for _, raw := range arr {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := m["cache_control"]; ok {
			count++
		}
		if content, ok := m["content"].([]interface{}); ok {
			count += countCacheControlsArray(content)
		}
	}
	return count
}

func testConversation(n int) []interface{} {
	msgs := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, map[string]interface{}{
			"role": role,
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": strings.Repeat("stable tail ", 16),
				},
			},
		})
	}
	return msgs
}

func numberedConversation(n int) []interface{} {
	msgs := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, map[string]interface{}{
			"role": role,
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": fmt.Sprintf("msg %02d", i),
				},
			},
		})
	}
	return msgs
}

func toolResultHeavyConversation(toolPairs int) []interface{} {
	msgs := []interface{}{
		userTextMessage("anchor 0"),
		assistantTextMessage("anchor 1"),
		userTextMessage("anchor 2"),
		assistantTextMessage("anchor 3"),
		userTextMessage("start tool loop"),
	}
	for i := 0; i < toolPairs; i++ {
		id := fmt.Sprintf("toolu_tail_%d", i)
		msgs = append(msgs,
			assistantToolUseMessage(id, "bash"),
			userToolResultMessage(id, fmt.Sprintf("done %d", i)),
		)
	}
	return msgs
}

func mainSessionSystem() []interface{} {
	return []interface{}{
		map[string]interface{}{"type": "text", "text": "<x-anthropic-billing-header>session-local</x-anthropic-billing-header>"},
		map[string]interface{}{"type": "text", "text": strings.Repeat("Main-session shared prefix. ", 250)},
	}
}

func sharedTools() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"name":        "probe_tool",
			"description": "shared tool definition",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"value": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

func TestFixConsecutiveRolesDropsAdjacentSameRole(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "user", "content": "u1"},
		map[string]interface{}{"role": "assistant", "content": "a1"},
		map[string]interface{}{"role": "assistant", "content": "a2"},
		map[string]interface{}{"role": "user", "content": "u2"},
		map[string]interface{}{"role": "user", "content": "u3"},
		map[string]interface{}{"role": "assistant", "content": "a3"},
	}
	fixed, dropped := fixConsecutiveRoles(msgs)
	if dropped != 2 {
		t.Errorf("expected 2 dropped, got %d", dropped)
	}
	if len(fixed) != 4 {
		t.Fatalf("expected 4 messages after fix, got %d", len(fixed))
	}
	roles := make([]string, len(fixed))
	for i, m := range fixed {
		roles[i], _ = m.(map[string]interface{})["role"].(string)
	}
	expected := []string{"user", "assistant", "user", "assistant"}
	for i, r := range roles {
		if r != expected[i] {
			t.Errorf("msg[%d] role=%s, expected %s", i, r, expected[i])
		}
	}
}

func TestFixConsecutiveRolesNoOp(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "user", "content": "u1"},
		map[string]interface{}{"role": "assistant", "content": "a1"},
		map[string]interface{}{"role": "user", "content": "u2"},
	}
	fixed, dropped := fixConsecutiveRoles(msgs)
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}
	if len(fixed) != 3 {
		t.Errorf("expected 3 messages, got %d", len(fixed))
	}
}

func TestFixConsecutiveRolesPreservesAssistantToolUseBoundary(t *testing.T) {
	msgs := []interface{}{
		userTextMessage("u1"),
		assistantTextMessage("anchor assistant"),
		assistantToolUseMessage("toolu_boundary", "bash"),
		userToolResultMessage("toolu_boundary", "done"),
		assistantTextMessage("after"),
	}

	fixed, repaired := fixConsecutiveRoles(msgs)
	if repaired != 1 {
		t.Fatalf("expected 1 repair, got %d", repaired)
	}
	if issues := validateMessageStructure(fixed); len(issues) > 0 {
		t.Fatalf("expected repaired messages to validate cleanly, got %v", issues)
	}

	content, _ := fixed[1].(map[string]interface{})["content"].([]interface{})
	block, _ := content[0].(map[string]interface{})
	if got, _ := block["type"].(string); got != "tool_use" {
		t.Fatalf("expected later assistant tool_use to be preserved, got %q", got)
	}
}

func TestBuildPinnedFrameDoesNotOrphanToolResultAtGapBoundary(t *testing.T) {
	cache := NewLocalCache("pinned-tool-boundary")
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("u0"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("a1"), Role: "assistant", Tokens: 10},
		{Msg: userTextMessage("u2"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("a3"), Role: "assistant", Tokens: 10},
		{Msg: userTextMessage("gap"), Role: "user", Tokens: 10, IsReference: true},
		{Msg: assistantToolUseMessage("toolu_gap", "bash"), Role: "assistant", Tokens: 10},
		{Msg: userToolResultMessage("toolu_gap", "done"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("after"), Role: "assistant", Tokens: 10},
	}

	view := cache.BuildPinnedFrameForRequest(false, 4, 4, false)
	if issues := validateMessageStructure(view.Messages); len(issues) > 0 {
		t.Fatalf("expected pinned frame to validate cleanly, got %v", issues)
	}
	if got, want := len(view.Messages), 5; got != want {
		t.Fatalf("expected pinned frame length %d, got %d", want, got)
	}

	// Find the tool_use and tool_result pair - they must be adjacent
	foundToolUseIdx := -1
	for i, msg := range view.Messages {
		m, _ := msg.(map[string]interface{})
		content, _ := m["content"].([]interface{})
		if len(content) > 0 {
			block, _ := content[0].(map[string]interface{})
			if typ, _ := block["type"].(string); typ == "tool_use" {
				foundToolUseIdx = i
				break
			}
		}
	}
	if foundToolUseIdx < 0 {
		t.Fatalf("expected boundary assistant tool_use to be preserved")
	}
	if foundToolUseIdx+1 >= len(view.Messages) {
		t.Fatalf("expected tool_result after tool_use but tool_use is at last position")
	}
	toolResult := view.Messages[foundToolUseIdx+1].(map[string]interface{})
	toolResultContent, _ := toolResult["content"].([]interface{})
	toolResultBlock, _ := toolResultContent[0].(map[string]interface{})
	if got, _ := toolResultBlock["type"].(string); got != "tool_result" {
		t.Fatalf("expected matching user tool_result adjacent to tool_use, got %q", got)
	}
}
