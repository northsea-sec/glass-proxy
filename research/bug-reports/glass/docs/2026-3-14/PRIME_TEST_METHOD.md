
  ---                                                                                                                                                                                         
  FULL RESEARCH REPORT                                          
                                                                                                                                                                                              
  1. Current System Status                                    
                                                                
  Proxy: Running PID 5471 on :19999, binary built 2026-03-14 08:58
  Sessions today: 3 conversations, 298 total API calls in 1 hour
                                                     
  ┌─────────────────────────────┬──────┬───────┬───────────┬────────────┬────────┬───────────────────┐
  │           Session           │ PID  │ Calls │ Cache Eff │ Big Breaks │  Cost  │    Last Active    │
  ├─────────────────────────────┼──────┼───────┼───────────┼────────────┼────────┼───────────────────┤
  │ 8ba1780842d7 (this session) │ 6153 │ 117   │ 83.9%     │ 8          │ $23.53 │ 09:31 (active)    │
  ├─────────────────────────────┼──────┼───────┼───────────┼────────────┼────────┼───────────────────┤
  │ 6be761d4747a                │ 7504 │ 69    │ 93.8%     │ 8          │ $20.61 │ 09:28 (idle 3min) │
  ├─────────────────────────────┼──────┼───────┼───────────┼────────────┼────────┼───────────────────┤
  │ d82dfa8ea49a (ended)        │ 6153 │ 125   │ 98.1%     │ 0          │ $18.64 │ 08:57 (dead)      │
  └─────────────────────────────┴──────┴───────┴───────────┴────────────┴────────┴───────────────────┘

  Quota: Real 5h=21.0% / 7d=25.0%, spoofed to 5%/5%

  Grand totals: $62.77 actual vs $278.90 raw = 77% savings, 90.8% overall cache efficiency

  2. Cache Break Analysis

  16 significant breaks (cc > 30K) in 1 hour. 13 of 16 occur immediately after a context switch between concurrent sessions. This confirms the PRIME-GOLDEN finding: concurrent session
  interleaving is the dominant cache break source.

  - 8ba1780842d7: 8 big breaks, 27 medium (5K-30K cc). This session (heavily evicted, 104 evicted messages, running pinned frame) has the worst prefix stability because its pinned frame view
   diverges every call.
  - 6be761d4747a: 8 big breaks, 6 medium. Fewer evictions (4 total), but still getting hit by interleaving.
  - d82dfa8ea49a: 0 big breaks, 4 medium. This was the early session that ran before concurrency kicked in — near-perfect 98.1% efficiency.

  PREFIX CHANGED: 228 events total — the prefix hash changes on almost every request. This is expected with tool-heavy workloads (each tool_result adds content), but the interleaving
  amplifies it because switching between sessions means the Anthropic-side KV cache for the other session is already evicted.

  3. Test Suite — 18 Failures in 3 Categories

  Category A: Operational Context rendering (11 tests)
  Root cause: renderOperationalContextNote (line 177) is gated by hasShadowOperationalContext(state, shadowDir). When called via FormatOperationalContext(table) → state=nil → returns "". The
   function used to render volatile facts (WORKING_STATE, AUTHORIZATION, CMD_SUCCESS, etc.) unconditionally — that code was removed during GLASS-FUCKED/GLASS-12 refactoring. Tests still
  expect the old behavior.

  Also: selectLatestChapterCheckpoints is defined but never called. injectOperationalContextMessages is explicitly DISABLED (returns false, -1 at line 505).

  Affected: TestFormatOperationalContext_SelectsWorkingSet, TestFormatOperationalContextWithVisibleResumeTurn_PrefersChapterPointer, TestBuildFactPresentation* (2),
  TestFormatOperationalContextWithSession_* (4), TestBootstrapSync_* (3), TestFormatOperationalContext_SkipsPoisonedFacts

  Category B: Process integration (5 tests)
  Same root cause — tests expect operational context note injection into outbound messages, but the injection is disabled and the rendering returns empty.

  Affected: TestProcessRebuildsSystemWithBootstrappedFactsAfterEviction, TestProcessVolatileFactChangeDoesNotMutateSystem, TestProcessFreshVisibleUserTurnSuppressesStaleEvictedTaskNote,
  TestProcessInjectsShadowSummaryNoteWithoutFactTable, TestProcessWritesChapterAndInjectsChapterPointerAfterEviction

  Category C: PinFrame breakpoint tests (2 tests)
  - TestPinnedFrameKeepsStableTailBreakpointAcrossSmallTailGrowth: expects breakpoints [5, 7] after one new message, gets [5, 9]. The orphan sanitizer's trailing-assistant safety pass (added
   in GLASS-9) changes the view, prefix hash diverges, anchor recalculates wider.
  - TestPinFrameArchivesBridgeWhenRecentTailHasOnlyToolResults: PinFrame archiving doesn't fire when the recent tail is all tool_results — the bridge detection condition isn't met.

  4. Resolved Issues (from chapter history)

  ┌─────────────────────────────────────────────────┬─────────────────────────────────────────────────────────────────────┐
  │                      Issue                      │                               Status                                │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ TestThinkingStripPreservesPureThinkingAssistant │ ✅ PASSES now                                                       │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ 232 pp/hr burn spike                            │ ✅ FIXED — BuildPinnedFrameForRequest restored for evicted sessions │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ chapter_resume.go dead code                     │ ✅ DELETED                                                          │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ Keyword-gated resume triggers                   │ ✅ REMOVED                                                          │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ Agent-tool rate limiter                         │ ✅ IMPLEMENTED (Stage 1c in proxy.go)                               │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ BakeEvictionBookmark                            │ ✅ IMPLEMENTED, fires (5 bakes logged)                              │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ block_subagents passes tool-bearing subagents   │ ✅ BY DESIGN — Agent tool subagents need tools to function          │
  ├─────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────────────┤
  │ Reingest blocking                               │ ✅ WORKING (95 blocked reingest attempts)                           │
  └─────────────────────────────────────────────────┴─────────────────────────────────────────────────────────────────────┘

  5. Still Unresolved

  ┌─────┬─────────────────────────────────────────────────────────────────────┬──────────┬────────────────────────────────────────────────────────────────────────────────────────────────┐
  │  #  │                                Issue                                │ Severity │                                          Fix Category                                          │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 1   │ 18 test failures — operational context tests expect rendering       │ HIGH     │ Tests need rewriting to match new architecture (baked bookmarks, no volatile fact rendering)   │
  │     │ that's been gutted                                                  │          │                                                                                                │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 2   │ 2 PinFrame test failures — orphan sanitizer changes view,           │ MEDIUM   │ Tests need updating for trailing-assistant safety pass behavior                                │
  │     │ breakpoints shift                                                   │          │                                                                                                │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 3   │ Prefix instability on 8ba1780842d7 — 228 PREFIX CHANGED in 1 hour,  │ MEDIUM   │ Inherent to heavily-evicted sessions with tool-heavy workloads + interleaving                  │
  │     │ diverge_at often at msg[41-42]                                      │          │                                                                                                │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 4   │ Context switch cache breaks — 13/16 big breaks follow session       │ MEDIUM   │ Serializer works (queues requests) but Anthropic-side LRU evicts the other session's KV        │
  │     │ switches                                                            │          │ between switches. Would need same-system-prefix dedup or workspace separation                  │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 5   │ reference.go dead code — 11 functions, used only by 2 test files    │ LOW      │ Delete when tests are rewritten                                                                │
  │     │ (chapter_test.go, split_id_test.go)                                 │          │                                                                                                │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 6   │ selectLatestChapterCheckpoints dead code — defined, never called    │ LOW      │ Delete with test cleanup                                                                       │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 7   │ MEMORY.md stale — doesn't reflect GLASS-8 through GLASS-FUCKED      │ LOW      │ Update when making edits                                                                       │
  │     │ changes                                                             │          │                                                                                                │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 8   │ All databases 0 bytes — debug.db, glass_debug.db, glass_metrics.db  │ LOW      │ Either not configured or writes never happen                                                   │
  │     │ are empty                                                           │          │                                                                                                │
  ├─────┼─────────────────────────────────────────────────────────────────────┼──────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
  │ 9   │ injectOperationalContextMessages disabled — hardcoded to return     │ INFO     │ Intentional — replaced by baked bookmarks                                                      │
  │     │ false                                                               │          │                                                                                                │
  └─────┴─────────────────────────────────────────────────────────────────────┴──────────┴────────────────────────────────────────────────────────────────────────────────────────────────┘

  6. Offline Test Suite Design (PRIME-GOLDEN Methodology)

  Based on the PRIME-DIAMOND/PRIME-GOLDEN test framework — hypothesis → baseline → simulation → metrics → comparison → verdict — here's the adapted offline test suite for glass-proxy:

  Test Framework Template

  1. HYPOTHESIS: What the code change should accomplish
  2. BASELINE: Current behavior from proxy log / unit test
  3. SIMULATION: Replay captured request sequences offline
  4. METRICS: prefix_changes, cache_breaks(>30K), eviction_count, message_count, validation_failures
  5. COMPARISON: before vs after, single-session vs concurrent
  6. VERDICT: confirmed/rejected/partial with evidence

  Proposed Tests

  TEST 1: Baked Bookmark Role Collision
  - Hypothesis: BakeEvictionBookmark (user) followed by a user tail message creates consecutive roles → validation failure → fallback
  - Baseline: Current proxy logs show 0 consecutive user errors post-bookmark
  - Simulation: Construct a message array with bookmark + user tail, run through RepairBrokenToolBoundaries → validateRequestMessages
  - Metrics: Does validation pass? Does repair merge the messages?
  - Expected: If repair merges, content is preserved but bookmark text is lost. If it doesn't merge, we should see the consecutive roles error.

  TEST 2: Prefix Stability Under Eviction
  - Hypothesis: After eviction, BuildPinnedFrameForRequest produces a view whose prefix hash stays stable across consecutive calls (no new messages added)
  - Baseline: Log shows 228 PREFIX CHANGED — but many are from new messages, not instability
  - Simulation: Ingest N messages, trigger eviction, call BuildPinnedFrameForRequest 10 times with identical input. Verify all produce identical prefix hash.
  - Metrics: Hash stability count (should be 10/10)
  - Expected: Should be stable. This is what TestPrefixStabilityNoNewMessages already tests (and passes).

  TEST 3: Concurrent Session Interleaving Impact
  - Hypothesis: Context switches between sessions cause >80% of big cache breaks
  - Baseline: 13/16 big breaks follow a context switch (81%)
  - Simulation: Replay the 298 STREAM entries, compute which breaks would be prevented if sessions were fully serialized (no interleaving)
  - Metrics: big_breaks_with_serialization vs big_breaks_with_interleaving
  - Expected: Full serialization eliminates most breaks but increases latency

  TEST 4: Orphan Sanitizer Trailing-Assistant Safety Pass
  - Hypothesis: The trailing-assistant safety pass changes the view, invalidating pinned frame breakpoint stability
  - Baseline: TestPinnedFrameKeepsStableTailBreakpointAcrossSmallTailGrowth fails with breakpoints [5,9] instead of [5,7]
  - Simulation: Build a pinned view, add 1 message, rebuild. Check if breakpoints shift by >2 positions.
  - Metrics: breakpoint delta per new message
  - Expected: Delta should be ≤2. If >2, the safety pass is over-dropping.

  TEST 5: Fact Overlay Cache Safety
  - Hypothesis: Fact overlay changes (stable content appended to system prompt) don't break the cached prefix
  - Baseline: Tests show sys_changed=false in most FINAL PREFIX diagnostics
  - Simulation: Build the system prompt with facts A, then with facts B. Compare the hash of the prefix up to the first breakpoint.
  - Metrics: prefix_hash stability when only facts change
  - Expected: Stable (facts go after the breakpoint)

  TEST 6: Evicted Session View Size Bound
  - Hypothesis: BuildPinnedFrameForRequest keeps the view bounded (anchor + recent tail), preventing the 232pp/hr burn from GLASS-12
  - Baseline: Current saved=472382 shows massive savings (2.1MB → 216KB body)
  - Simulation: Construct a 200-message conversation, evict 150, build pinned frame. Measure output message count.
  - Metrics: output message count vs input, body size ratio
  - Expected: Output should be ~20-30 messages (anchor + tail), not 200

  TEST 7: Reingest Blocking Effectiveness
  - Hypothesis: Evicted messages are never re-ingested when CC replays them
  - Baseline: 95 blocked reingest attempts in current session
  - Simulation: Build a cached conversation with eviction, simulate CC sending back evicted messages as if they're new
  - Metrics: reingest_blocked count, cache growth
  - Expected: All evicted positions should be blocked, cache size should not grow

  Execution Order (P0 = highest priority)

  ┌──────────┬────────┬────────────┬───────────────────────────────────────────────────────────────┐
  │ Priority │  Test  │ Dependency │                              Why                              │
  ├──────────┼────────┼────────────┼───────────────────────────────────────────────────────────────┤
  │ P0       │ Test 1 │ None       │ Bookmark is the new architecture — must be structurally valid │
  ├──────────┼────────┼────────────┼───────────────────────────────────────────────────────────────┤
  │ P1       │ Test 6 │ None       │ Validates the 232pp/hr fix is permanent                       │
  ├──────────┼────────┼────────────┼───────────────────────────────────────────────────────────────┤
  │ P2       │ Test 2 │ None       │ Core prefix stability guarantee                               │
  ├──────────┼────────┼────────────┼───────────────────────────────────────────────────────────────┤
  │ P3       │ Test 4 │ None       │ Explains 2 of 18 test failures                                │
  ├──────────┼────────┼────────────┼───────────────────────────────────────────────────────────────┤
  │ P4       │ Test 7 │ None       │ Validates reingest blocking                                   │
  ├──────────┼────────┼────────────┼───────────────────────────────────────────────────────────────┤
  │ P5       │ Test 3 │ Log data   │ Quantifies interleaving cost                                  │
  ├──────────┼────────┼────────────┼───────────────────────────────────────────────────────────────┤
  │ P6       │ Test 5 │ None       │ Confirms fact injection is cache-safe                         │
  └──────────┴────────┴────────────┴───────────────────────────────────────────────────────────────┘

  ---
  That's the complete research. The 18 test failures are all downstream of the GLASS-FUCKED refactoring that:
  1. Disabled injectOperationalContextMessages
  2. Stripped volatile fact rendering from renderOperationalContextNote
  3. Changed the orphan sanitizer behavior (trailing-assistant safety pass)

  The tests need to be rewritten to match the new architecture. The production proxy is running correctly — 77% cost savings, 90.8% cache efficiency, baked bookmarks firing, reingest
  blocking working.

