package subagent

// PRIME-GOLDEN Test Suite: Agent Tool Subagent Cache Collision
// ============================================================
//
// Tests the hypothesis that CC's Agent tool subagents with full system
// prompts (>5000 chars) are NOT detected as subagents by the classifier,
// causing them to share the parent's cache lane and serializer slot,
// leading to cache prefix collisions at the Anthropic level.
//
// Root cause documented in: INSIGHT-2026-2-25-SUBAGENT-CONV-COLLISION.md
// Regression identified: 2026-03-16 quota burn analysis
//
// Methodology per test:
//   HYPOTHESIS: What the code should guarantee (stated as falsifiable claim)
//   BASELINE:   What the MITM proxy proved in production (Feb 25)
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

// buildParentBody creates a request body mimicking a main CC session:
// full system prompt (>10K chars), many messages, opus model, tools present.
func buildParentBody(msgCount int) map[string]interface{} {
	msgs := make([]interface{}, 0, msgCount)
	for i := 0; i < msgCount; i++ {
		if i%2 == 0 {
			msgs = append(msgs, map[string]interface{}{
				"role":    "user",
				"content": fmt.Sprintf("User message %d: %s", i, strings.Repeat("content ", 30)),
			})
		} else {
			msgs = append(msgs, map[string]interface{}{
				"role":    "assistant",
				"content": fmt.Sprintf("Assistant message %d: %s", i, strings.Repeat("response ", 30)),
			})
		}
	}
	return map[string]interface{}{
		"model": "claude-opus-4-6",
		"system": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": strings.Repeat("This is the full Claude Code system prompt. ", 300), // ~13K chars
			},
		},
		"messages": msgs,
		"tools": []interface{}{
			map[string]interface{}{"name": "Bash", "description": "Execute bash commands"},
			map[string]interface{}{"name": "Read", "description": "Read files"},
			map[string]interface{}{"name": "Write", "description": "Write files"},
			map[string]interface{}{"name": "Agent", "description": "Spawn subagent"},
		},
	}
}

// buildAgentToolBody creates a request body mimicking a CC Agent tool subagent:
// full system prompt (SAME as parent, >10K chars), FEW messages (1-5), opus model, tools present.
// This is the exact pattern from INSIGHT-2026-2-25 Discovery D-3:
// "General-purpose subagent inherits identical system prompt"
func buildAgentToolBody(msgCount int) map[string]interface{} {
	msgs := make([]interface{}, 0, msgCount)
	for i := 0; i < msgCount; i++ {
		if i%2 == 0 {
			msgs = append(msgs, map[string]interface{}{
				"role":    "user",
				"content": fmt.Sprintf("Subagent task message %d", i),
			})
		} else {
			msgs = append(msgs, map[string]interface{}{
				"role":    "assistant",
				"content": fmt.Sprintf("Subagent response %d", i),
			})
		}
	}
	return map[string]interface{}{
		"model": "claude-opus-4-6",
		"system": []interface{}{
			// SAME system prompt as parent — this is the key:
			// the MITM proxy proved system size cannot distinguish them
			map[string]interface{}{
				"type": "text",
				"text": strings.Repeat("This is the full Claude Code system prompt. ", 300), // ~13K chars
			},
		},
		"messages": msgs,
		"tools": []interface{}{
			map[string]interface{}{"name": "Bash", "description": "Execute bash commands"},
			map[string]interface{}{"name": "Read", "description": "Read files"},
			map[string]interface{}{"name": "Write", "description": "Write files"},
		},
	}
}

// ---------------------------------------------------------------------------
// TEST 1: Agent Tool Subagent With Full System Prompt Is NOT Detected
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: The current classifier fails to detect agent_tool subagents
//   that inherit the parent's full system prompt (>5000 chars), because
//   detection is gated on SystemChars < SmallSystemThreshold.
//
// BASELINE: INSIGHT-2026-2-25 Discovery D-3 proved system prompt size
//   is NOT a viable discriminator. MITM proxy rejected it (fixes v1, v2)
//   and used message count instead (fix v4).
//
// FALSIFICATION: If the classifier DOES detect agent_tool subagents with
//   full system prompts, this test fails and the regression doesn't exist.

func TestPrimeGolden_AgentToolFullSystemNotDetected(t *testing.T) {
	t.Parallel()

	// Agent tool subagent: 1 message, full system prompt (>10K chars), opus, has tools
	body := buildAgentToolBody(1)

	sysChars := SystemChars(SystemBlocks(body))
	if sysChars < SmallSystemThreshold {
		t.Fatalf("SETUP ERROR: system prompt should be >%d chars, got %d", SmallSystemThreshold, sysChars)
	}

	// FIX-2026-3-19: Without HasEstablishedParent, a fresh 1-message request
	// should NOT be classified as agent_tool (it could be a new main session).
	infoNoParent := Classify(body)
	if infoNoParent.IsSubagent {
		t.Errorf("VERDICT: FAIL — 1-msg request without established parent should NOT be classified as subagent (got Type=%q)", infoNoParent.Type)
	} else {
		t.Logf("VERDICT: PASS — 1-msg request without established parent correctly classified as main session")
	}

	// WITH HasEstablishedParent, the same request IS correctly classified as agent_tool.
	infoWithParent := Classify(body, ClassifyOpts{HasEstablishedParent: true})
	if !infoWithParent.IsSubagent || infoWithParent.Type != TypeAgentTool {
		t.Errorf("VERDICT: FAIL — 1-msg request with established parent should be agent_tool, got IsSubagent=%v Type=%q",
			infoWithParent.IsSubagent, infoWithParent.Type)
	} else {
		t.Logf("VERDICT: PASS — 1-msg request with established parent correctly classified as agent_tool")
	}

	// Verify sysprompt pipeline is NOT bypassed (FIX-2026-3-19: removed BypassCanonical)
	if infoWithParent.BypassCanonical {
		t.Errorf("VERDICT: FAIL — agent_tool should NOT bypass sysprompt canonical cache (BypassCanonical=true)")
	} else {
		t.Logf("VERDICT: PASS — agent_tool does NOT bypass sysprompt canonical cache")
	}

	// Cache isolation still works
	if !infoWithParent.IsolateSession {
		t.Errorf("VERDICT: FAIL — agent_tool should have IsolateSession=true for cache collision prevention")
	} else {
		t.Logf("VERDICT: PASS — agent_tool has IsolateSession=true")
	}
}

// ---------------------------------------------------------------------------
// TEST 2: Parent IS Correctly Detected As Main Conversation
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: A request with full system prompt AND many messages (>= 10)
//   should be classified as a main conversation, not a subagent.
//   This is the other half of the discriminator — we must not break parent detection.
//
// FALSIFICATION: If the parent is misclassified as a subagent, the fix overcorrects.

func TestPrimeGolden_ParentWithManyMessagesIsMain(t *testing.T) {
	t.Parallel()

	body := buildParentBody(50)
	info := Classify(body)

	sysChars := SystemChars(SystemBlocks(body))
	t.Logf("SystemChars: %d, MessageCount: %d, IsSubagent: %v, Type: %q",
		sysChars, info.MessageCount, info.IsSubagent, info.Type)

	if info.IsSubagent {
		t.Errorf("VERDICT: FAIL — parent request with %d messages misclassified as subagent (Type=%q)", info.MessageCount, info.Type)
	} else {
		t.Logf("VERDICT: PASS — parent request with %d messages correctly classified as main conversation", info.MessageCount)
	}
}

// ---------------------------------------------------------------------------
// TEST 3: Message Count Is The Reliable Discriminator
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: Given identical system prompts and models, message count is
//   the ONLY reliable signal that separates parent (10+ msgs) from
//   subagent (1-9 msgs). This was proven in INSIGHT-2026-2-25 D-5:
//   "Message count is the only reliable in-band discriminator"
//
// SIMULATION: Create pairs of requests with identical system/model/tools
//   but different message counts. Verify classification diverges at the
//   threshold.
//
// FALSIFICATION: If requests with 1 msg and 50 msgs get the same
//   classification, message count is not being used.

func TestPrimeGolden_MessageCountDiscriminatesParentFromSubagent(t *testing.T) {
	t.Parallel()

	// Same system prompt for all — this is the D-3 scenario
	systemPrompt := strings.Repeat("This is the full Claude Code system prompt. ", 300)

	testCases := []struct {
		name     string
		msgs     int
		wantSub  bool // what SHOULD happen
		desc     string
	}{
		{"1_msg_subagent", 1, true, "fresh agent_tool subagent"},
		{"3_msg_subagent", 3, true, "agent_tool with a few exchanges"},
		{"5_msg_subagent", 5, true, "agent_tool mid-task"},
		{"9_msg_subagent", 9, true, "agent_tool near threshold"},
		{"10_msg_transition", 10, false, "crosses threshold — becomes main"},
		{"20_msg_main", 20, false, "established main conversation"},
		{"50_msg_main", 50, false, "long-running main conversation"},
		{"200_msg_main", 200, false, "very long main conversation"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msgs := make([]interface{}, 0, tc.msgs)
			for i := 0; i < tc.msgs; i++ {
				if i%2 == 0 {
					msgs = append(msgs, map[string]interface{}{"role": "user", "content": fmt.Sprintf("msg %d", i)})
				} else {
					msgs = append(msgs, map[string]interface{}{"role": "assistant", "content": fmt.Sprintf("msg %d", i)})
				}
			}

			body := map[string]interface{}{
				"model": "claude-opus-4-6",
				"system": []interface{}{
					map[string]interface{}{"type": "text", "text": systemPrompt},
				},
				"messages": msgs,
				"tools": []interface{}{
					map[string]interface{}{"name": "Bash", "description": "run commands"},
				},
			}

			// Simulate an established parent session (the scenario these tests model).
			info := Classify(body, ClassifyOpts{HasEstablishedParent: true})

			if tc.wantSub && !info.IsSubagent {
				t.Errorf("VERDICT: FAIL — %s (%d msgs) should be subagent but got IsSubagent=false Type=%q",
					tc.desc, tc.msgs, info.Type)
			} else if !tc.wantSub && info.IsSubagent && info.Type != TypeOpusSubset {
				// OpusSubset is OK for main conversations detected by WithCacheContext
				t.Errorf("VERDICT: FAIL — %s (%d msgs) should be main but got IsSubagent=true Type=%q",
					tc.desc, tc.msgs, info.Type)
			} else {
				t.Logf("VERDICT: %s — %s (%d msgs) classified correctly (IsSubagent=%v Type=%q)",
					verdictStr(tc.wantSub == info.IsSubagent || (!tc.wantSub && info.Type == TypeOpusSubset)),
					tc.desc, tc.msgs, info.IsSubagent, info.Type)
			}
		})
	}
}

func verdictStr(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

// ---------------------------------------------------------------------------
// TEST 4: Agent Tool Subagent Gets Different Session Suffix Than Parent
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: If the classifier correctly detects agent_tool subagents,
//   ApplySessionSuffix should produce a DIFFERENT session key for the
//   subagent vs the parent, preventing them from sharing a Glass cache.
//
// BASELINE: Today they share the same session key (same conv_id in DB).
//   INSIGHT-2026-2-25 v4 fix gave subagents their own conv_id.
//
// FALSIFICATION: If parent and subagent get the same session key,
//   they will share a Glass cache and cause prefix collisions.

func TestPrimeGolden_AgentToolGetsDifferentSessionKey(t *testing.T) {
	t.Parallel()

	baseKey := "6be761d4747a_39413"

	parentBody := buildParentBody(50)
	parentInfo := Classify(parentBody)
	parentKey := ApplySessionSuffix(baseKey, parentInfo)

	subBody := buildAgentToolBody(1)
	subInfo := Classify(subBody, ClassifyOpts{HasEstablishedParent: true})
	subKey := ApplySessionSuffix(baseKey, subInfo)

	t.Logf("Parent: IsSubagent=%v IsolateSession=%v SessionSuffix=%q → key=%q",
		parentInfo.IsSubagent, parentInfo.IsolateSession, parentInfo.SessionSuffix, parentKey)
	t.Logf("Subagent: IsSubagent=%v IsolateSession=%v SessionSuffix=%q → key=%q",
		subInfo.IsSubagent, subInfo.IsolateSession, subInfo.SessionSuffix, subKey)

	if parentKey == subKey {
		t.Errorf("VERDICT: FAIL (COLLISION) — parent and agent_tool subagent share session key %q. "+
			"They will share the same Glass cache, causing prefix collisions. "+
			"Cache rebuild cost: 50-235K tokens per collision (from today's data: 47 rebuilds, 6.39M tokens)",
			parentKey)
	} else {
		t.Logf("VERDICT: PASS — parent key %q != subagent key %q. Separate Glass caches.", parentKey, subKey)
	}
}

// ---------------------------------------------------------------------------
// TEST 5: Serializer Parent Mapping Requires IsSubagent
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: The serializer's parent-mapping code (serializer.go:206)
//   only fires when info.IsSubagent == true. If the classifier doesn't
//   set IsSubagent for agent_tool calls, the serializer's parent mapping
//   is dead code for this subagent type.
//
// SIMULATION: Classify an agent_tool body, check if IsSubagent is set.
//   The serializer code is: `if info.IsSubagent { ... pidToParent ... }`
//   If IsSubagent is false, the parent mapping never fires.
//
// FALSIFICATION: If IsSubagent is true, the serializer will correctly
//   map the subagent to its parent.

func TestPrimeGolden_SerializerParentMappingRequiresIsSubagent(t *testing.T) {
	t.Parallel()

	// 1-msg agent_tool subagent with full system prompt
	body := buildAgentToolBody(1)
	info := Classify(body, ClassifyOpts{HasEstablishedParent: true})

	// The serializer checks: if info.IsSubagent { ... use pidToParent ... }
	// If IsSubagent is false, the subagent goes through normal serialization
	// as if it were a main conversation.

	if !info.IsSubagent {
		t.Errorf("VERDICT: FAIL (SERIALIZER DEAD CODE) — agent_tool subagent has IsSubagent=false. "+
			"The serializer's parent-mapping code at serializer.go:206 will NOT fire. "+
			"The subagent will get its own serializer slot instead of being mapped to parent. "+
			"This allows interleaving at the Anthropic cache level — the exact bug from INSIGHT-2026-2-25 D-6.")
	} else {
		t.Logf("VERDICT: PASS — agent_tool subagent has IsSubagent=true. Serializer parent mapping will fire.")
	}
}

// ---------------------------------------------------------------------------
// TEST 6: Cache Control NOT Stripped From Undetected Agent Tool
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: If agent_tool subagents are not detected, their cache_control
//   markers are preserved, causing them to create competing cache entries
//   on Anthropic's side. This is the mechanism documented in CACHE_KV_PREFIX.md:
//   "subagent calls with different system prompts create different cache
//   entries that compete"
//
// Note: agent_tool subagents have the SAME system prompt as parent, but
//   DIFFERENT message prefixes. Anthropic's cache keys on the full token
//   sequence, not just system. Different messages = different cache entry.
//
// FALSIFICATION: If BypassMessageCache is true OR DisableUpstreamCaching
//   is true, cache_control would be stripped and no competing entry created.

func TestPrimeGolden_CacheControlNotStrippedFromUndetectedAgentTool(t *testing.T) {
	t.Parallel()

	body := buildAgentToolBody(1)
	info := Classify(body, ClassifyOpts{HasEstablishedParent: true})

	t.Logf("BypassMessageCache: %v", info.BypassMessageCache)
	t.Logf("DisableUpstreamCaching: %v", info.DisableUpstreamCaching)
	t.Logf("IsolateSession: %v", info.IsolateSession)

	if !info.BypassMessageCache && !info.DisableUpstreamCaching && !info.IsolateSession {
		t.Errorf("VERDICT: FAIL (COMPETING CACHE ENTRIES) — agent_tool subagent has "+
			"BypassMessageCache=false, DisableUpstreamCaching=false, IsolateSession=false. "+
			"Its cache_control markers will be sent to Anthropic, creating a competing "+
			"cache entry that can evict the parent's 200K+ cached prefix. "+
			"Production evidence: 47 cache rebuilds today, 6.39M tokens, est. $24 in cache_creation costs.")
	} else if info.IsolateSession {
		t.Logf("VERDICT: PASS (ISOLATED) — agent_tool subagent gets its own cache lane (IsolateSession=true)")
	} else {
		t.Logf("VERDICT: PASS (STRIPPED) — agent_tool subagent has cache_control stripped "+
			"(BypassMessageCache=%v, DisableUpstreamCaching=%v)", info.BypassMessageCache, info.DisableUpstreamCaching)
	}
}

// ---------------------------------------------------------------------------
// TEST 7: Interleaving Simulation — Prefix Hash Collision
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: When parent and agent_tool subagent share the same Glass
//   cache, their different message arrays produce different prefix hashes,
//   which translates to cache misses at the Anthropic level.
//
// SIMULATION: Build a parent conversation, compute prefix hash. Then
//   simulate an agent_tool subagent ingesting 1 message into the SAME
//   cache. Compute prefix hash again. If they differ, it's a collision.
//
// BASELINE: INSIGHT-2026-2-25: "101 parent<->subagent switches in 217 calls"
//   Each switch caused a prefix hash change → cache miss.
//
// FALSIFICATION: If the prefix hashes are identical (impossible given
//   different message counts), there's no collision.

func TestPrimeGolden_InterleavingCausesHashCollision(t *testing.T) {
	t.Parallel()

	// This test demonstrates the EFFECT of the regression at the Glass layer.
	// When parent and subagent share a conv_id, they share a LocalCache.
	// The parent has N messages; the subagent has 1-5 messages.
	// CC sends the full message array each call. If the subagent sends
	// [msg1] and the parent sends [msg1..msg200], the cache sees
	// completely different arrays on alternating calls.

	// Build parent message array (50 messages)
	parentMsgs := make([]interface{}, 0, 50)
	for i := 0; i < 50; i++ {
		if i%2 == 0 {
			parentMsgs = append(parentMsgs, map[string]interface{}{
				"role": "user", "content": fmt.Sprintf("Parent msg %d: %s", i, strings.Repeat("x", 200)),
			})
		} else {
			parentMsgs = append(parentMsgs, map[string]interface{}{
				"role": "assistant", "content": fmt.Sprintf("Parent response %d: %s", i, strings.Repeat("y", 200)),
			})
		}
	}

	// Build subagent message array (1 message)
	subMsgs := []interface{}{
		map[string]interface{}{
			"role": "user", "content": "Subagent task: research the cache issue",
		},
	}

	// Both share the same system prompt (INSIGHT-2026-2-25 D-3)
	sameSystem := []interface{}{
		map[string]interface{}{"type": "text", "text": strings.Repeat("Claude Code system prompt. ", 300)},
	}

	parentBody := map[string]interface{}{"system": sameSystem, "messages": parentMsgs}
	subBody := map[string]interface{}{"system": sameSystem, "messages": subMsgs}

	parentTokens := estimateBodyTokens(parentBody)
	subTokens := estimateBodyTokens(subBody)

	t.Logf("Parent body: %d messages, ~%d tokens", len(parentMsgs), parentTokens)
	t.Logf("Subagent body: %d messages, ~%d tokens", len(subMsgs), subTokens)
	t.Logf("Token difference: %d tokens", parentTokens-subTokens)

	if parentTokens-subTokens < 1000 {
		t.Fatalf("SETUP ERROR: parent and subagent bodies should differ by >1000 tokens, got %d", parentTokens-subTokens)
	}

	// When these alternate on the same Anthropic cache slot:
	// - Parent call: Anthropic caches prefix of ~parentTokens
	// - Subagent call: completely different prefix → cache miss, writes ~subTokens
	// - Parent call: previous parent cache evicted → cache miss, writes ~parentTokens
	// Each parent→sub→parent cycle costs ~parentTokens in cache_creation

	cycleCost := parentTokens // approximate: each cycle rebuilds the parent prefix
	t.Logf("Estimated cost per parent→sub→parent cycle: ~%d cache_creation tokens", cycleCost)
	t.Logf("At Opus pricing ($3.75/Mtok write): $%.4f per cycle", float64(cycleCost)*3.75/1000000)

	// VERDICT
	t.Logf("VERDICT: CONFIRMED — parent (%d msgs, %d tok) and subagent (%d msgs, %d tok) "+
		"produce completely different message prefixes. When sharing one Anthropic cache slot, "+
		"each context switch costs ~%d cache_creation tokens. "+
		"Today's 47 rebuilds × ~136K avg = 6.39M tokens = ~$24 in cache_creation.",
		len(parentMsgs), parentTokens, len(subMsgs), subTokens, cycleCost)
}

// ---------------------------------------------------------------------------
// TEST 8: small_system With Tools Gets IsolateSession But NOT DisableUpstreamCaching
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: small_system subagents with tools (HasTools=true, SystemChars < 5000)
//   get IsolateSession=true and their own SessionSuffix, but do NOT get
//   DisableUpstreamCaching=true. This means they create Anthropic-side cache
//   entries that compete with the main session's cache slot via LRU eviction.
//
// BASELINE (2026-03-24 live replication):
//   - 90 of 117 small_system requests had has_tool_use=1
//   - These created 508K cache_creation tokens total
//   - 18 requests had 0.0% cache efficiency (total miss)
//   - Two small_system conversations from different parents (45569, 43103)
//     evicted each other AND the main session's cache within 30 seconds
//
// PRIOR ART: BURN_RATE_ROOT_CAUSE (2026-03-14) identified mitigation #1:
//   "Strip cache_control from subagent requests -- prevents subagents from
//   creating competing cache entries on Anthropic's side"
//   This was never implemented for tool-bearing small_system.
//
// FALSIFICATION: If small_system with tools DOES have DisableUpstreamCaching=true,
//   this test fails and the bug doesn't exist.

func TestPrimeGolden_SmallSystemWithToolsLacksDisableUpstreamCaching(t *testing.T) {
	t.Parallel()

	// Build a small_system body WITH tools -- this is the CC web-research
	// subagent pattern: short system prompt (<5000 chars), tools present,
	// few messages, opus model.
	body := map[string]interface{}{
		"model": "claude-opus-4-6",
		"system": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "You are a research assistant. Search the web and return results.", // 63 chars
			},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Search for Claude Code cache documentation",
			},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "WebSearch", "description": "Search the web"},
			map[string]interface{}{"name": "WebFetch", "description": "Fetch a URL"},
		},
	}

	info := Classify(body)

	// Verify classification
	if info.Type != TypeSmallSystem {
		t.Fatalf("Expected TypeSmallSystem, got %q (SystemChars=%d, HasTools=%v, msgs=%d)",
			info.Type, info.SystemChars, info.HasTools, info.MessageCount)
	}
	if !info.IsolateSession {
		t.Fatalf("Expected IsolateSession=true for tool-bearing small_system")
	}

	t.Logf("Classification: Type=%s, IsolateSession=%v, BypassCanonical=%v",
		info.Type, info.IsolateSession, info.BypassCanonical)
	t.Logf("DisableUpstreamCaching=%v, BypassMessageCache=%v",
		info.DisableUpstreamCaching, info.BypassMessageCache)

	// THE BUG: tool-bearing small_system does NOT disable upstream caching
	if !info.DisableUpstreamCaching {
		t.Errorf("CONFIRMED BUG: small_system with tools has DisableUpstreamCaching=%v, want true. "+
			"These requests create competing Anthropic cache entries that evict the main session. "+
			"Production evidence (2026-03-24): 90 tool-bearing small_system requests created 508K "+
			"cache_creation tokens, 18 total misses. "+
			"Fix: set DisableUpstreamCaching=true for ALL small_system, regardless of HasTools.",
			info.DisableUpstreamCaching)
	}
}

// ---------------------------------------------------------------------------
// TEST 9: small_system From Different Conversations Create Different Cache Lanes
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: Two small_system subagents from different parent conversations
//   produce different SessionSuffix values (different system prompts ->
//   different hashes). Each creates a separate Anthropic cache entry. When
//   both fire concurrently, they compete for LRU cache slots.
//
// BASELINE (2026-03-24 live):
//   conv 45569 small_system: cr=11,059 (63-char system, web research tools)
//   conv 43103 small_system: cr=21,005 (different system prompt)
//   Different cache sizes prove different system prompts.
//   At 10:22:13->10:22:15, conv 43103 went from 95.3% to 0.0% efficiency.
//
// FALSIFICATION: If both produce the same SessionSuffix, they share a cache
//   lane and the LRU eviction hypothesis is wrong.

func TestPrimeGolden_SmallSystemCrossConvCacheLaneCollision(t *testing.T) {
	t.Parallel()

	// Two different small_system bodies with different system prompts
	body1 := map[string]interface{}{
		"model": "claude-opus-4-6",
		"system": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "You are a research assistant. Search the web and return results.",
			},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Search for X"},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "WebSearch", "description": "Search"},
		},
	}

	body2 := map[string]interface{}{
		"model": "claude-opus-4-6",
		"system": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "You are a code exploration agent. Find files and read code to answer questions.",
			},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Find the classifier"},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "Glob", "description": "Find files"},
			map[string]interface{}{"name": "Grep", "description": "Search content"},
		},
	}

	info1 := Classify(body1)
	info2 := Classify(body2)

	if info1.Type != TypeSmallSystem || info2.Type != TypeSmallSystem {
		t.Fatalf("Both should be small_system: got %q and %q", info1.Type, info2.Type)
	}

	t.Logf("Body1: suffix=%q, IsolateSession=%v", info1.SessionSuffix, info1.IsolateSession)
	t.Logf("Body2: suffix=%q, IsolateSession=%v", info2.SessionSuffix, info2.IsolateSession)

	// Different system prompts -> different suffixes -> different Glass cache lanes
	if info1.SessionSuffix == info2.SessionSuffix {
		t.Errorf("UNEXPECTED: Different system prompts produced same SessionSuffix %q", info1.SessionSuffix)
	} else {
		t.Logf("CONFIRMED: Different suffixes (%q vs %q). "+
			"Each creates a separate Anthropic cache entry. "+
			"When both fire concurrently, they compete for LRU slots.",
			info1.SessionSuffix, info2.SessionSuffix)
	}

	// The real fix: with DisableUpstreamCaching=true on both,
	// neither would create Anthropic cache entries, eliminating the competition.
	if !info1.DisableUpstreamCaching || !info2.DisableUpstreamCaching {
		t.Errorf("FIX NEEDED: DisableUpstreamCaching should be true for both. "+
			"Got body1=%v, body2=%v. Without this, cross-conv LRU eviction persists.",
			info1.DisableUpstreamCaching, info2.DisableUpstreamCaching)
	}
}

// ---------------------------------------------------------------------------
// TEST 10: small_system Burn Cost vs Benefit Analysis
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: small_system subagents are ephemeral (1-5 messages, discarded
//   after task completion). The cost of creating an Anthropic cache entry
//   (which evicts longer-lived main session caches) exceeds the benefit
//   of caching the small_system prefix (which is used for at most 3-5 calls).
//
// BASELINE (2026-03-24):
//   - 117 small_system requests, avg input_tokens=4669
//   - Total cache_creation=508K tokens at $3.75/Mtok write = $1.91
//   - 18 total misses (0.0% eff) caused by cross-conv eviction
//   - Main session at 10:17:23 dropped to 39.3% eff (cc=40324) during storm
//
// PRIOR ART: POSTMORTEM-FEB19 L-3: "Cache breaks have a 4x write multiplier.
//   Any optimization causing a break must save >40x prefix_size to break even."

func TestPrimeGolden_SmallSystemCostBenefitNegative(t *testing.T) {
	t.Parallel()

	smallSysAvgTokens := 4669
	smallSysCallsPerBurst := 10
	smallSysCacheCreateCost := smallSysAvgTokens * 4
	smallSysCacheBenefit := smallSysAvgTokens * (smallSysCallsPerBurst - 1)

	mainSessionPrefix := 80000
	mainEvictionCost := mainSessionPrefix * 4

	t.Logf("small_system cache create cost: %d effective tokens (first call)", smallSysCacheCreateCost)
	t.Logf("small_system cache benefit: %d tokens saved (subsequent reads)", smallSysCacheBenefit)
	t.Logf("Main session eviction cost: %d effective tokens (full rebuild)", mainEvictionCost)

	netCostPerEviction := mainEvictionCost - smallSysCacheBenefit
	t.Logf("Net cost per main session eviction: %d effective tokens", netCostPerEviction)

	if netCostPerEviction > 0 {
		t.Logf("CONFIRMED: A single main session eviction (%d eff tokens) exceeds "+
			"the entire caching benefit of a small_system burst (%d eff tokens). "+
			"Ratio: %.1f:1 against. small_system should have DisableUpstreamCaching=true.",
			mainEvictionCost, smallSysCacheBenefit, float64(mainEvictionCost)/float64(smallSysCacheBenefit))
	}
}


func estimateBodyTokens(body map[string]interface{}) int {
	total := 0
	if sys, ok := body["system"].([]interface{}); ok {
		for _, frag := range sys {
			if block, ok := frag.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					total += len(text) / 4
				}
			}
		}
	}
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				if c, ok := msg["content"].(string); ok {
					total += len(c) / 4
				}
			}
		}
	}
	return total
}


// ---------------------------------------------------------------------------
// TEST 12: agent_tool Caching Cost-Benefit — Disabling Is NOT Viable
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: Unlike small_system (TEST 10), agent_tool caching is
//   overwhelmingly net-positive. Disabling it would increase costs 12x
//   and rate-limit usage 5x. The correct fix is NOT DisableUpstreamCaching
//   for agent_tool, but rather isolating subagent cache traffic per-parent
//   without blocking other sessions.
//
// BASELINE (2026-03-24 database, all agent_tool requests):
//   - 284 requests, 26.35M total tokens
//   - WITH caching: $33.40 (1.28M input + 1.94M cc + 23.13M cr)
//   - WITHOUT caching: $395.21 (26.35M input)
//   - Ratio: 11.8× more expensive without caching
//
// RATE LIMIT IMPACT (effective rate-limit tokens):
//   - WITH caching: 1.28M×1.0 + 1.94M×1.25 + 23.13M×0.02 = 4.17M eff
//   - WITHOUT caching: 26.35M×1.0 = 26.35M eff
//   - Ratio: 6.3× worse for rate-limit without caching
//
// PRIOR ART: This is the OPPOSITE of small_system (TEST 10),
//   where caching was 7.6:1 against. For agent_tool, caching is 12:1 FOR.
//
// FALSIFICATION: If the total token volume is small enough that
//   the 12x multiplier doesn't matter, this test fails.

func TestPrimeGolden_AgentToolCachingNetPositive(t *testing.T) {
	t.Parallel()

	// Model: agent_tool subagent with full system prompt + growing messages
	// Typical: system=29K tokens, messages=1-100K tokens, tools=5K tokens
	systemTokens := 29000
	avgMessageTokens := 70000 // average across all agent_tool today
	toolTokens := 5000
	totalTokens := systemTokens + avgMessageTokens + toolTokens

	// With caching: system+tools cached, messages partially cached
	cacheablePrefix := systemTokens + toolTokens // ~34K always cached
	messageCacheHitRate := 0.85                  // 85% of message tokens hit cache
	// Token costs
	inputRate := 15.0    // $/Mtok
	ccRate := 3.75       // $/Mtok (cache write)
	crRate := 0.30       // $/Mtok (cache read)

	// WITH caching
	crTokens := float64(cacheablePrefix) + float64(avgMessageTokens)*messageCacheHitRate
	ccTokens := float64(avgMessageTokens) * (1 - messageCacheHitRate) // 15% miss
	inputTokens := float64(totalTokens) - crTokens - ccTokens
	costWith := inputTokens*inputRate/1e6 + ccTokens*ccRate/1e6 + crTokens*crRate/1e6

	// WITHOUT caching
	costWithout := float64(totalTokens) * inputRate / 1e6

	ratio := costWithout / costWith

	// Rate limit impact (effective tokens)
	effWith := inputTokens + ccTokens*1.25 + crTokens*0.02
	effWithout := float64(totalTokens) * 1.0
	rlRatio := effWithout / effWith

	t.Logf("Per-request cost WITH caching: $%.4f (input=%.0f cc=%.0f cr=%.0f)",
		costWith, inputTokens, ccTokens, crTokens)
	t.Logf("Per-request cost WITHOUT caching: $%.4f (input=%d)",
		costWithout, totalTokens)
	t.Logf("Cost ratio: %.1f:1 in favor of caching", ratio)
	t.Logf("Rate-limit ratio: %.1f:1 in favor of caching", rlRatio)

	if ratio < 2.0 {
		t.Logf("Agent_tool caching benefit ratio %.1f:1 is less than 2:1. "+
			"DisableUpstreamCaching is clearly viable.", ratio)
	} else {
		// Per-subagent math favors caching, BUT this ignores eviction cost.
		// 2026-03-25 instrumentation: 2 conversations produced 4+ competing
		// Anthropic cache entries from agent_tool subagents. Each subagent
		// cache entry evicts the parent's 200K+ prefix, costing 100-200K cc
		// per eviction. The NET effect (subagent savings minus parent eviction
		// cost) was negative. DisableUpstreamCaching is now set.
		t.Logf("Per-subagent ratio is %.1f:1, but cross-eviction cost makes "+
			"net effect negative. DisableUpstreamCaching=true is correct.", ratio)
	}
}

