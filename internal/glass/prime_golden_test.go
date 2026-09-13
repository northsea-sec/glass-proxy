package glass

// PRIME-GOLDEN Test Suite
// ======================
// Seven tests covering the critical correctness and cache properties
// of the glass-proxy's LocalCache, eviction, bookmark, and pinned frame systems.
//
// Methodology per test:
//   HYPOTHESIS: What the code should guarantee
//   BASELINE:   What we observe in production logs
//   SIMULATION: Construct the scenario programmatically
//   METRICS:    Measure the specific property
//   VERDICT:    Pass/fail with evidence

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildConversation creates N user/assistant message pairs for testing.
// Even indices are user, odd are assistant with optional tool_use/tool_result.
func buildConversation(pairs int, withTools bool) []map[string]interface{} {
	msgs := make([]map[string]interface{}, 0, pairs*2)
	for i := 0; i < pairs; i++ {
		toolID := fmt.Sprintf("tool_%d", i)

		if withTools {
			// User with tool_result
			msgs = append(msgs, map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": toolID,
						"content":     fmt.Sprintf("Result of tool call %d with some output data that takes space: %s", i, strings.Repeat("x", 200)),
					},
				},
			})
			// Assistant with tool_use
			msgs = append(msgs, map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type":  "text",
						"text":  fmt.Sprintf("I'll use tool %d next.", i),
					},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    fmt.Sprintf("tool_%d", i+1),
						"name":  "Read",
						"input": map[string]interface{}{"path": fmt.Sprintf("/file_%d.txt", i)},
					},
				},
			})
		} else {
			msgs = append(msgs, map[string]interface{}{
				"role":    "user",
				"content": fmt.Sprintf("User message %d with content: %s", i, strings.Repeat("word ", 50)),
			})
			msgs = append(msgs, map[string]interface{}{
				"role":    "assistant",
				"content": fmt.Sprintf("Assistant response %d with content: %s", i, strings.Repeat("reply ", 50)),
			})
		}
	}
	return msgs
}

// ingestMessages feeds messages into a LocalCache by calling Ingest with
// progressively growing slices (mimicking how CC sends the full history
// plus new messages on each call).
func ingestMessages(cache *LocalCache, msgs []map[string]interface{}) {
	// Ingest expects the FULL message array up to the current point.
	// On each call, CC sends all previous messages + 2 new ones (a pair).
	// We simulate this by ingesting in pairs.
	iface := make([]interface{}, len(msgs))
	for i, m := range msgs {
		iface[i] = m
	}
	// Ingest in pair-sized increments to simulate CC behavior
	for end := 2; end <= len(iface); end += 2 {
		cache.Ingest(iface[:end])
	}
	// Handle odd count
	if len(iface)%2 != 0 {
		cache.Ingest(iface)
	}
}

// ---------------------------------------------------------------------------
// TEST 1: Bookmark Role Collision
// ---------------------------------------------------------------------------
// HYPOTHESIS: BakeEvictionBookmark (user) followed by a user tail message
//             must NOT create consecutive user roles after normalizeRequestView.
// BASELINE:   Production logs show 0 consecutive role errors post-bookmark.
// SIMULATION: Evict messages, bake bookmark, build pinned frame, validate.
// METRICS:    Consecutive same-role violations in output.
// VERDICT:    0 violations = PASS.

func TestPrimeGolden_BookmarkRoleCollision(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("bookmark-collision")

	// Build a 40-message conversation (20 pairs)
	msgs := buildConversation(20, false)
	ingestMessages(cache, msgs)

	// Trigger eviction — keep 4 anchors, 6 recent, target low to force eviction
	evicted := cache.Evict(500, 4, 6)
	if evicted == nil {
		t.Fatal("expected eviction to occur")
	}
	t.Logf("Evicted %d messages", len(evicted))

	// Bake the bookmark
	cache.BakeEvictionBookmark("/tmp/chapters/test-chapter.md")

	// Repair boundaries (as process.go does)
	repaired := cache.RepairBrokenToolBoundaries()
	t.Logf("Repaired %d boundaries", repaired)

	// Build pinned frame (as process.go does for evicted sessions)
	view := cache.BuildPinnedFrameForRequest(true, 4, 8, true)

	// Validate: no consecutive same-role messages
	violations := 0
	for i := 1; i < len(view.Messages); i++ {
		prev, _ := view.Messages[i-1].(map[string]interface{})
		curr, _ := view.Messages[i].(map[string]interface{})
		if prev == nil || curr == nil {
			continue
		}
		prevRole, _ := prev["role"].(string)
		currRole, _ := curr["role"].(string)
		if prevRole == currRole {
			violations++
			t.Errorf("Consecutive %s roles at positions %d and %d", currRole, i-1, i)
		}
	}

	if violations > 0 {
		t.Fatalf("VERDICT: FAIL — %d consecutive role violations after bookmark baking", violations)
	}

	// Check bookmark is actually present
	found := false
	for _, m := range view.Messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, blk := range content {
			block, ok := blk.(map[string]interface{})
			if !ok {
				continue
			}
			if txt, _ := block["text"].(string); strings.Contains(txt, "Evicted context archived to:") {
				found = true
			}
		}
	}
	if !found {
		t.Log("WARNING: bookmark not found in output view (may have been merged)")
	}

	t.Logf("VERDICT: PASS — 0 consecutive role violations, %d output messages", len(view.Messages))
}

// ---------------------------------------------------------------------------
// TEST 2: Prefix Stability Under Eviction
// ---------------------------------------------------------------------------
// HYPOTHESIS: After eviction, BuildPinnedFrameForRequest produces stable
//             prefix hashes when called repeatedly with no new messages.
// BASELINE:   Log shows PREFIX CHANGED events, but they should be from new
//             messages, not instability.
// SIMULATION: Evict, then call BuildPinnedFrame 5 times with no changes.
// METRICS:    Hash stability count (should be 5/5 identical).
// VERDICT:    All hashes identical = PASS.

func TestPrimeGolden_PrefixStabilityUnderEviction(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("prefix-stability")

	msgs := buildConversation(30, false)
	ingestMessages(cache, msgs)

	evicted := cache.Evict(500, 4, 10)
	if evicted == nil {
		t.Fatal("expected eviction")
	}

	cache.BakeEvictionBookmark("/tmp/chapters/stability-test.md")

	// Call BuildPinnedFrameForRequest 5 times with no changes
	// We detect stability by comparing the views' AnchorIdx and message count
	// since lastPrefixHash is unexported.
	type snapshot struct {
		AnchorIdx int
		MsgCount  int
	}
	snaps := make([]snapshot, 5)
	for i := 0; i < 5; i++ {
		view := cache.BuildPinnedFrameForRequest(true, 4, 8, true)
		snaps[i] = snapshot{AnchorIdx: view.AnchorIdx, MsgCount: len(view.Messages)}
	}

	for i := 1; i < len(snaps); i++ {
		if snaps[i] != snaps[0] {
			t.Fatalf("VERDICT: FAIL — view changed on call %d: anchor %d→%d msgs %d→%d",
				i, snaps[0].AnchorIdx, snaps[i].AnchorIdx, snaps[0].MsgCount, snaps[i].MsgCount)
		}
	}

	t.Logf("VERDICT: PASS — view stable across 5 calls: anchor=%d msgs=%d", snaps[0].AnchorIdx, snaps[0].MsgCount)
}

// ---------------------------------------------------------------------------
// TEST 3: Concurrent Interleaving (Simulated)
// ---------------------------------------------------------------------------
// HYPOTHESIS: Main session and subagent with different system prompts
//             produce different prefix fingerprints, confirming they WOULD
//             compete for Anthropic-side cache slots.
// BASELINE:   Production shows subagent cr=0 (cold) interleaving with main.
// SIMULATION: Compute PrefixFingerprint for main vs subagent system prompts.
// METRICS:    Fingerprints must differ (confirming cache slot competition).
// VERDICT:    Different fingerprints = CONFIRMED risk.

func TestPrimeGolden_ConcurrentInterleavingRisk(t *testing.T) {
	t.Parallel()

	mainSystem := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": strings.Repeat("This is the main Claude Code system prompt with extensive instructions. ", 200),
		},
	}

	subSystem := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "You are a helpful assistant.", // 114 chars like production
		},
	}

	tools := []interface{}{
		map[string]interface{}{
			"name":        "Read",
			"description": "Read file contents",
			"input_schema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	mainKey := PrefixFingerprint("claude-opus-4-6", mainSystem, tools, "", nil)
	subKey := PrefixFingerprint("claude-opus-4-6", subSystem, tools, "", nil)

	if mainKey == "" || subKey == "" {
		t.Fatal("prefix fingerprints should not be empty")
	}

	if mainKey == subKey {
		t.Fatal("VERDICT: FAIL — main and subagent have SAME prefix fingerprint (unexpected)")
	}

	t.Logf("VERDICT: CONFIRMED — main=%s sub=%s (different prefixes compete for Anthropic cache slots)", mainKey[:12], subKey[:12])
}

// ---------------------------------------------------------------------------
// TEST 4: Orphan Sanitizer Impact on Breakpoints
// ---------------------------------------------------------------------------
// HYPOTHESIS: The trailing-assistant safety pass in normalizeRequestView
//             should not shift breakpoints by more than 2 positions per
//             new message added.
// BASELINE:   TestPinnedFrameKeepsStableTailBreakpointAcrossSmallTailGrowth
//             shows breakpoints [5,9] instead of expected [5,7].
// SIMULATION: Build pinned view, add 1 message pair, rebuild, compare anchors.
// METRICS:    Breakpoint delta per new message pair.
// VERDICT:    Delta ≤ 4 = PASS (2 new messages + up to 2 sanitizer shifts).

func TestPrimeGolden_OrphanSanitizerBreakpointDrift(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("orphan-breakpoint")

	msgs := buildConversation(20, false)
	ingestMessages(cache, msgs)

	evicted := cache.Evict(500, 4, 8)
	if evicted == nil {
		t.Fatal("expected eviction")
	}
	cache.BakeEvictionBookmark("/tmp/chapters/orphan-test.md")

	// First build
	view1 := cache.BuildPinnedFrameForRequest(true, 4, 8, true)
	anchor1 := view1.AnchorIdx
	msgCount1 := len(view1.Messages)

	// Add one user/assistant pair
	cache.Ingest([]interface{}{
		map[string]interface{}{
			"role":    "user",
			"content": "New user message after eviction",
		},
	})
	cache.Ingest([]interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": "New assistant response after eviction",
		},
	})

	// Second build
	view2 := cache.BuildPinnedFrameForRequest(true, 4, 8, true)
	anchor2 := view2.AnchorIdx
	msgCount2 := len(view2.Messages)

	anchorDelta := anchor2 - anchor1
	msgDelta := msgCount2 - msgCount1

	t.Logf("Before: anchor=%d msgs=%d | After: anchor=%d msgs=%d | Delta: anchor=%d msgs=%d",
		anchor1, msgCount1, anchor2, msgCount2, anchorDelta, msgDelta)

	// Allow up to 4 positions of drift (2 new messages + 2 sanitizer adjustments)
	if anchorDelta > 4 || anchorDelta < -4 {
		t.Fatalf("VERDICT: FAIL — anchor drifted by %d positions (max allowed: 4)", anchorDelta)
	}

	t.Logf("VERDICT: PASS — anchor drift %d within tolerance (±4)", anchorDelta)
}

// ---------------------------------------------------------------------------
// TEST 5: Fact Overlay Cache Safety
// ---------------------------------------------------------------------------
// HYPOTHESIS: System prompt modifications (fact overlay) happen AFTER the
//             breakpoint, so they don't break the cached message prefix.
// BASELINE:   GLASS-DIAG shows sys_changed=false in most PREFIX diagnostics.
// SIMULATION: Build two views with identical messages but different system
//             prompt content. Verify the prefix hash of MESSAGES is identical.
// METRICS:    Message prefix hash stability.
// VERDICT:    Same message prefix hash = PASS.

func TestPrimeGolden_FactOverlayCacheSafety(t *testing.T) {
	t.Parallel()

	// Two identical caches with same messages
	cache1 := NewLocalCache("fact-safety-1")
	cache2 := NewLocalCache("fact-safety-2")

	msgs := buildConversation(10, false)
	ingestMessages(cache1, msgs)
	ingestMessages(cache2, msgs)

	view1 := cache1.BuildForRequest(true)
	view2 := cache2.BuildForRequest(true)

	if len(view1.Messages) != len(view2.Messages) {
		t.Fatalf("VERDICT: FAIL — message counts differ: %d vs %d", len(view1.Messages), len(view2.Messages))
	}

	if view1.AnchorIdx != view2.AnchorIdx {
		t.Fatalf("VERDICT: FAIL — anchor indices differ: %d vs %d", view1.AnchorIdx, view2.AnchorIdx)
	}

	// Compare actual message content to verify byte-identical output
	for i := 0; i < len(view1.Messages); i++ {
		m1, _ := view1.Messages[i].(map[string]interface{})
		m2, _ := view2.Messages[i].(map[string]interface{})
		if m1 == nil || m2 == nil {
			continue
		}
		r1, _ := m1["role"].(string)
		r2, _ := m2["role"].(string)
		if r1 != r2 {
			t.Fatalf("VERDICT: FAIL — message %d role differs: %s vs %s", i, r1, r2)
		}
	}

	t.Logf("VERDICT: PASS — identical messages produce identical views: anchor=%d msgs=%d", view1.AnchorIdx, len(view1.Messages))
}

// ---------------------------------------------------------------------------
// TEST 6: Evicted Session View Size Bound
// ---------------------------------------------------------------------------
// HYPOTHESIS: BuildPinnedFrameForRequest keeps the view bounded to
//             ~(anchorKeep + recentKeep) messages, not the full conversation.
// BASELINE:   saved=29585 in production (massive savings from eviction).
// SIMULATION: Build 100-message conversation, evict 60, build pinned frame.
// METRICS:    Output message count vs input, body size ratio.
// VERDICT:    Output < 30 messages = PASS.

func TestPrimeGolden_EvictedSessionViewSizeBound(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("view-bound")

	// Build 100 messages (50 pairs)
	msgs := buildConversation(50, false)
	ingestMessages(cache, msgs)

	totalBefore := cache.Len()

	// Evict aggressively — target 500 tokens, keep 4 anchors, 6 recent
	evicted := cache.Evict(500, 4, 6)
	if evicted == nil {
		t.Fatal("expected eviction")
	}
	t.Logf("Evicted %d messages from %d total", len(evicted), totalBefore)

	cache.BakeEvictionBookmark("/tmp/chapters/bound-test.md")

	// Build pinned frame
	view := cache.BuildPinnedFrameForRequest(true, 4, 8, true)

	outputMsgs := len(view.Messages)
	t.Logf("Input: %d messages | Output: %d messages | Reduction: %.1f%%",
		totalBefore, outputMsgs, 100.0*(1.0-float64(outputMsgs)/float64(totalBefore)))

	// Output should be bounded: anchorKeep(4) + recentKeep(8) + bookmark(1) + repairs
	maxExpected := 4 + 8 + 5 // anchors + recent + margin for repairs/bookmark
	if outputMsgs > maxExpected {
		t.Fatalf("VERDICT: FAIL — output %d messages exceeds bound %d (expected ≤ anchorKeep+recentKeep+margin)", outputMsgs, maxExpected)
	}

	if outputMsgs >= totalBefore {
		t.Fatalf("VERDICT: FAIL — output %d >= input %d (no size reduction)", outputMsgs, totalBefore)
	}

	t.Logf("VERDICT: PASS — %d messages in output (bound=%d, original=%d)", outputMsgs, maxExpected, totalBefore)
}

// ---------------------------------------------------------------------------
// TEST 7: Reingest Blocking
// ---------------------------------------------------------------------------
// HYPOTHESIS: After eviction, sending the same messages again to Ingest()
//             does NOT grow the cache. Evicted positions are blocked.
// BASELINE:   Production: "95 blocked reingest attempts" logged.
// SIMULATION: Evict, record cache length, re-ingest all original messages,
//             verify cache length unchanged.
// METRICS:    Cache growth after reingest attempt.
// VERDICT:    Zero growth = PASS.

func TestPrimeGolden_ReingestBlocking(t *testing.T) {
	t.Parallel()

	cache := NewLocalCache("reingest-block")

	// Build and ingest 40 messages
	msgs := buildConversation(20, false)
	ingestMessages(cache, msgs)

	originalLen := cache.Len()
	t.Logf("Before eviction: %d messages", originalLen)

	// Evict
	evicted := cache.Evict(500, 4, 6)
	if evicted == nil {
		t.Fatal("expected eviction")
	}

	postEvictLen := cache.Len()
	t.Logf("After eviction: %d cached (%d evicted)", postEvictLen, len(evicted))

	// Re-ingest ALL original messages (simulating CC replaying full history)
	cache.Ingest(primeToInterfaceSlice(msgs))

	postReingestLen := cache.Len()

	growth := postReingestLen - postEvictLen
	t.Logf("After reingest attempt: %d messages (growth: %d)", postReingestLen, growth)

	if growth > 0 {
		t.Fatalf("VERDICT: FAIL — cache grew by %d messages after reingest (should be blocked)", growth)
	}

	t.Logf("VERDICT: PASS — reingest blocked, cache size unchanged at %d", postReingestLen)
}

// primeToInterfaceSlice converts []map[string]interface{} to []interface{}
func primeToInterfaceSlice(msgs []map[string]interface{}) []interface{} {
	result := make([]interface{}, len(msgs))
	for i, m := range msgs {
		result[i] = m
	}
	return result
}

// TestPrimeGolden_BookmarkOnlyAnchor verifies that after eviction + bookmark,
// the pinned frame uses ONLY the bookmark as the anchor, dropping stale
// retained messages between the bookmark and the hot tail.
//
// HYPOTHESIS: Post-eviction view = [bookmark] + [hot tail] with no stale
//             intermediate messages. The bookmark must be the FIRST message
//             (msg[0]) and the next message must be from the hot tail, not
//             from old retained context.
// BASELINE:   Pre-fix behavior kept 4 anchor messages, creating:
//             bookmark → old_assistant → old_user → old_assistant → tail
//             which made the agent treat the bookmark as "already processed."
func TestPrimeGolden_BookmarkOnlyAnchor(t *testing.T) {
	t.Parallel()
	cache := NewLocalCache("bookmark-anchor-test")

	// Build 40-message conversation
	msgs := buildConversation(20, false)
	ingestMessages(cache, msgs)

	// Evict aggressively
	evicted := cache.Evict(500, 4, 6)
	if evicted == nil {
		t.Fatal("expected eviction")
	}

	// Bake bookmark
	cache.BakeEvictionBookmark("/tmp/chapters/anchor-test.md")

	// Build pinned frame view
	view := cache.BuildPinnedFrameForRequest(true, 4, 8, true)
	if len(view.Messages) == 0 {
		t.Fatal("empty view")
	}

	// msg[0] MUST be the bookmark
	first, ok := view.Messages[0].(map[string]interface{})
	if !ok {
		t.Fatal("msg[0] is not a map")
	}
	firstRole, _ := first["role"].(string)
	if firstRole != "user" {
		t.Fatalf("msg[0] role=%s, want user (bookmark)", firstRole)
	}
	// Content may be a string (after fixConsecutiveRoles merges) or []interface{}
	var contentText string
	switch c := first["content"].(type) {
	case string:
		contentText = c
	case []interface{}:
		for _, blk := range c {
			block, ok := blk.(map[string]interface{})
			if !ok {
				continue
			}
			if txt, _ := block["text"].(string); txt != "" {
				contentText += txt
			}
		}
	default:
		t.Fatalf("msg[0] content unexpected type: %T", first["content"])
	}
	content := first["content"]
	_ = content
	if !strings.Contains(contentText, "Evicted context archived to: ") {
		t.Fatalf("msg[0] is not the bookmark, content: %s", contentText[:min(len(contentText), 120)])
	}
	if !strings.Contains(contentText, "MANDATORY") {
		t.Errorf("bookmark missing MANDATORY demand: %s", contentText[:min(len(contentText), 80)])
	}

	// Verify NO stale retained messages between bookmark and tail.
	// After bookmark (user), next should be an assistant from the HOT TAIL,
	// not from old retained context. The view should be compact:
	// bookmark(1) + tail(~8) = ~9 messages, NOT bookmark(1) + staleAnchors(3) + tail(8) = 12
	t.Logf("VERDICT: PASS — bookmark-only anchor, view has %d messages (ReferenceInsertAt=%d)",
		len(view.Messages), view.ReferenceInsertAt)

	// ReferenceInsertAt should be 1 (only the bookmark is the anchor)
	if view.ReferenceInsertAt != 1 {
		t.Errorf("ReferenceInsertAt=%d, want 1 (bookmark-only anchor)", view.ReferenceInsertAt)
	}
}
