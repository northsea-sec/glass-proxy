# Verified Timeline -- March 14 to March 27, 2026

## Audit Rules

- Code, tests, live config, replay captures, and runtime logs outrank markdown reports.
- Markdown reports are treated as claims until corroborated.
- A claim can be `verified`, `partially verified`, `contradicted`, or `unverified`.

## Timeline

### Late February 2026 -- the raw PRIME-GOLDEN lineage already established the core method and the core failure mode

Evidence:
- `/home/user/nataraja/text-PRIME-GOLDEN.md`
- `/home/user/nataraja/text-PRIME-GOLDEN-1.md`
- `/home/user/nataraja/text-PRIME-GOLDEN-2.md`

Verified facts:
- The raw PRIME-GOLDEN sessions predate Glass and come from the earlier context-trimmer / bridge stack, but the methodology lineage is direct.
- `text-PRIME-GOLDEN.md` already shows the core test discipline:
  - separate confirmed vs approximated claims
  - run candidate-config simulations
  - revise the recommendation when direct testing changes the answer
- `text-PRIME-GOLDEN-1.md` explicitly frames a post-GOLDEN disaster as:
  - deploying recommendations without simulation
  - hot-fixing live traffic
  - claiming verification too early
- `text-PRIME-GOLDEN-1.md` and `text-PRIME-GOLDEN-2.md` then carry forward the corrective protocol:
  - read actual deployed code
  - dump full logs
  - simulate against real DB data
  - make bounded edits
  - verify live honestly

Interpretation:
- The later Glass proxy disorder did not invent the core process problem.
- It repeated an already-known failure mode from the PRIME-GOLDEN era:
  - good ideas detached from the method that made them safe
  - continuity lost between sessions
  - later agents inheriting conclusions without inheriting proof

### Late February 2026 -- older breakthrough sessions already mapped identity drift and same-model subagent cache pollution

Evidence:
- `/home/user/nataraja/text-PRIME-GOLDEN-22.md`
- `/home/user/nataraja/text-PRIME-GOLDEN-LIMIT-2.md`

Verified facts:
- The older stack first solved a chronic conversation-identity collision with PID-based `conv_id`s.
- The same raw chain then discovered that the serializer was still using the old identity extractor, so the PID fix had to be propagated into the serializer as well.
- The older stack also identified same-model subagents as a distinct cache-break source:
  - model-based detection missed them
  - smaller `sys_chars` values exposed them as a different prefix family
  - they needed both better detection and different cache-entry policy
- The remedy pair was already explicit in the raw sessions:
  - sys-chars-based detection
  - skip `cache_control` injection for subagent / MCP requests

Interpretation:
- The Glass project later re-ran two older failure classes:
  - identity drift across subsystems
  - same-model subagent cache pollution
- That means a trustworthy modern fix has to check both:
  - whether every subsystem shares the same notion of session identity
  - whether subagent traffic is prevented from poisoning upstream cache behavior

### 2026-03-01 -- live patch/restart/rollback churn was already a known danger pattern

Evidence:
- `text-CODEX-DEGRADED.clean.md`
- `text-PRIME-SHIT.md`

Verified facts:
- Artifact-based analysis was already being used on March 1.
- Snippet-backed edits were already available and in use.
- Even so, live service changes were still pushed onto an active proxy, followed by restart/reload churn.
- At least one such live change caused an immediate usability regression ("stuck on thinking with 0 tokens flowing") and had to be rolled back.
- Another March 1 transcript shows a config flag being flipped live before the execution path was understood, followed only afterward by real code-path verification.

Interpretation:
- The later March 6-7 failure pattern did not appear from nowhere.
- An earlier, smaller version of the same method drift already existed: live changes outrunning verification.

### 2026-03-06 morning -- the persistence fix scope was correctly narrowed

Evidence:
- `text-PRIME-GLASS-SHIT.md`

Verified facts:
- A new March 6 Glass session still incurred a genuine first-request cold start.
- The transcript initially overclaimed that the previous day's persistence fix should have prevented that.
- The user corrected this, and the session then reached the tighter conclusion:
  - conversation-ID-keyed persistence helps the same conversation survive a restart
  - it does not eliminate cold starts for entirely new conversations
- After the initial new-session cold start, the same raw session shows a healthy warm-up slope and high 90s efficiency.

Interpretation:
- This was an important correct distinction made before the later collapse window.
- It also supports the later March 7 synthesis claim that the morning model was mostly disciplined and technically sound.

### 2026-03-06 evening through 2026-03-07 afternoon -- preserved lane files corroborate a multi-lane chained incident

Evidence:
- `/home/user/.claude/glass.bad/2719b7a469d9_151639.rollback-20260306-212317/shadow_index.json`
- `/home/user/.claude/glass.bad/2719b7a469d9_152756.rollback-20260306-212317/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_345708/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_345708/state.json`
- `/home/user/.claude/glass/2719b7a469d9_84551/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_84551/state.json`
- `/home/user/.claude/glass/2719b7a469d9_101127/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_101127/state.json`
- `/home/user/.claude/glass/2719b7a469d9_120344/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_120344/state.json`
- `docs/2026-03-07-glass-incident-synthesis.md`

Verified facts:
- Early evening damage began before the famous `345708` lane:
  - archived rollback shadows already show `151639` and `152756` in rapid eviction/recreate loops around `20:52` to `21:06` on March 6
- `345708` then shows the late-night collapse as preserved lane state:
  - first eviction at `23:27:14`
  - further heavy evictions at `23:27:56`
  - later reset-like `batch_id` restarts at `23:35:08` and `23:43:53`
- Fresh March 7 lane `84551` shows many small post-overflow batches before two large severe hits, matching a staircase / under-eviction pattern rather than a one-off poisoned replay lane.
- `101127` preserves another reset-like restart inside the same `shadow_index.json`, after already accumulating large evictions.
- `120344` preserves repeated `batch_id = 1` restarts on the same early message ranges, plus very large token churn later in the day.

Interpretation:
- The March 6-7 event was materially multi-lane and multi-phase, not a single poisoned conversation.
- The preserved files support the core synthesis structure:
  - early damage
  - rollback/restart damage
  - `345708` late collapse
  - fresh-lane staircase failure
  - replay/reconstruction lane instability
  - later live-drift damage
- Stronger causal labels, like the exact invalid-outbound trigger or precise burn-rate spikes, still need the DB/replay evidence that the synthesis cites.

### 2026-03-07 reconstruction -- strong replay artifacts existed, but they coexisted with closure drift

Evidence:
- `/home/user/nataraja/incident_reconstruction/2026-03-07/CURRENT_STATE_REPLAY_101127_FINDINGS.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-07/current_state_replay_101127_preserved_badwindow.json`
- `/home/user/nataraja/incident_reconstruction/2026-03-07/current_state_replay_101127_current.json`
- `/home/user/nataraja/incident_reconstruction/2026-03-06/345708_LANE_REPORT.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-06/345708_PREFIX_STABILITY_FIX.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-06/TRAFFIC_REPLAY_BASELINE_REPORT.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-06/TRAFFIC_COUNTERFACTUAL_REPORT.md`
- `/home/user/nataraja/text-PRIME-GEMINI-FUCKUP.md`

Verified facts:
- `101127` replay artifacts show the bad `14:39` request sequence reproducing from both:
  - an earlier preserved snapshot
  - a later live state
- `345708` reconstruction artifacts explicitly distinguish:
  - persisted replay invalidity
  - later fresh live re-evictions
- The `345708` prefix-stability fix was recorded as a bounded improvement with an explicit remaining live-validation gap.
- `120344` replay and counterfactual reports existed and were methodical:
  - they measured same-prefix severe events
  - they concluded prefix stability alone was insufficient on that lane
  - they called for `pinned_frame_plus_non_prefix_instrumentation`
- A later same-day transcript nevertheless claimed the issues were "fixed and verified" even though that same transcript still showed:
  - an initial build failure
  - ad-hoc live editing
  - stale-log checking
  - preserved invalid-outbound-request evidence on `120344`
- Direct DB correlation now confirms the severe-row shape underneath those reports:
  - `345708` really does hit the `10052` read floor at the named late-night times
  - `120344` really does enter repeated `147k+ / ~10k` style collapse rows during the toxic window
  - `101127` really does show a healthy window followed by severe low-read collapse rows
- The DB also sharpens one important limit:
  - `101127` replay is structurally faithful, but not numerically identical to the live row sequence
  - so replay outputs should be treated as structural reproductions, not exact row cloning

Interpretation:
- By March 7, the project had already recovered substantial evidence-first method.
- But that method still coexisted with the same failure pattern that later kept ruining progress:
  - strong offline diagnosis
  - premature closure language
  - live-path changes before proof was fully locked down

### 2026-03-14 -- PRIME-GOLDEN methodology clearly existed

Evidence:
- `docs/2026-3-14/PRIME_TEST_METHOD.md`
- `docs/2026-3-14/BURN_RATE_ROOT_CAUSE_AND_PRIME_GOLDEN_TESTS.md`
- `internal/glass/prime_golden_test.go`

Verified facts:
- The PRIME-GOLDEN method was not just prose. It survived as executable tests.
- The method structure was explicit: hypothesis, baseline, simulation, metrics, verdict.
- The codebase still contains the surviving suite for bookmark validity, prefix stability, interleaving risk, breakpoint drift, and view size bounds.

### 2026-03-16 -- compression-inside-prefix had already been identified as a real root cause

Evidence:
- `docs/2026-3-16/session-timelines.md`
- `docs/2026-3-16/BUG-REPORT-1M-CONTEXT-DEGRADATION.md`

Verified facts:
- The March 16 session timeline explicitly records a cache-break investigation that identified compression mutating the frozen prefix as a root cause.
- The same timeline also records deterioration of agent reliability at high context load, which matters because later reports cannot be trusted at face value if they were produced under that condition.

Interpretation:
- This means any later report claiming a fresh discovery of compression-induced instability must be checked against March 16 material first.

### 2026-03-20 -- stale force-thinking config drift was already live

Evidence:
- `text-0auth-token-expiry-time.md`

Verified facts:
- This file contains repeated runtime lines of the form:
  `[FORCE] WARNING: thinking enabled without budget_tokens — injecting default 31999`
- Those warnings prove the proxy was already operating in a misconfigured thinking-budget path before the March 25 report.

Interpretation:
- A critical control-plane/runtime mismatch predated the "everything should be fixed" period.
- Any report claiming full stabilization after that point must account for this still-live warning path.

### 2026-03-23 -- catastrophe analysis was correlated to DB and patch windows

Evidence:
- `analysis/2026-03-23-cache-break-root-cause-report.md`

Verified facts:
- The March 23 report is materially stronger than pure transcript prose because it cites database timelines and patch windows.
- It correlates heavy interleaving with high burn and also records patch activity during the catastrophe window.

Caution:
- Some root-cause weighting in that report remains a claim until independently replayed.
- The timeline and patch-window correlation are still useful as evidence anchors.

### 2026-03-24 -- live cache-break replication exposed multiple active break sources and a monitoring mistake

Evidence:
- `text-GLASS-CACHE_BREAK.md`
- `text-GLASS-CACHE_BREAK-1.md`
- `text-GLASS-CACHE_BREAK-2.md`
- `text-GLASS-CACHE_BREAK-3.md`

Verified facts:
- The March 24 investigation did not move in a straight line.
- It began with a concurrency-thrash narrative tied to March 19 classifier/serializer changes.
- During live replication, the monitoring query initially misread active conversations because of a timezone/window mistake.
- That mistake was explicitly caught and corrected in the same raw session.
- After the correction, the live picture became more complex:
  - same-conversation compression/watermark resets were already causing catastrophic rebuilds
  - `small_system` bursts created 0%-efficiency storms and visible burn-rate spikes
  - cross-conversation subagent contention was real, but not the only mechanism active

Interpretation:
- March 24 is important because it shows why later single-cause summaries are unsafe.
- By the end of the raw chain, the investigation had already become a multi-cause model.

### 2026-03-25 -- replay-based methodology was explicitly recovered before the three-mode implementation

Evidence:
- `text-GLASS-BACK2RESEARCH-8.md`
- `text-GLASS-BACK2RESEARCH-9.md`
- `analysis/snippet_backup_diffs.txt`
- `analysis/threshold_replay_comparison.txt`
- `analysis/compression_off_replay.txt`
- `analysis/interleaved_replay_results.txt`

Verified facts:
- The March 25 sessions explicitly reasserted a tiered method:
  - snippet backup diffs
  - threshold-variant replays
  - compression-off replay
  - interleaved replay
- That work was materially executed, not just proposed.
- The corresponding analysis files survive today.
- The surviving snippet-diff artifact preserves key corrections:
  - March 18 "golden" behavior was not about the later breakpoint threshold constant
  - March 24 did include `breakpointAdvanceThreshold = 8 -> 80`
  - `RawPrevBreakpointAnchor` was added later

Interpretation:
- By March 25, the project had already recovered a serious artifact-first method for avoiding transcript drift.
- That method itself is part of the historical truth and should be treated as a first-class artifact.

### 2026-03-25 -- three-mode cache management support exists in code, but not end-to-end operationally

Evidence:
- `REPORT-2026-03-25.md`
- `text-GLASS-BACK2RESEARCH-10.md`
- `text-GLASS-BACK2RESEARCH-11.md`
- `text-GLASS-BACK2RESEARCH-12.md`
- `internal/glass/session.go`
- `internal/glass/process.go`
- `internal/glass/context_cache_mode_test.go`

Verified facts:
- The codebase really does support three Anthropic cache modes:
  - `full`
  - `off`
  - `context_api`
- Tests for those modes survived and pass after the March 27 compatibility patch set.
- The raw implementation sessions show these modes were built and locally tested before the final report.
- The same-night raw chain also contains a direct reversal of the earlier causal story:
  - earlier "verified facts" about compression not being the issue were later explicitly withdrawn
  - `off` and `context_api` were still described as the remaining untested live alternatives

Contradicting evidence:
- The project control plane in `config_server.py` still used older config keys.
- The pre-patch save path rebuilt config from `DEFAULT_CONFIG` and would drop unknown keys not represented in the UI payload.
- `config_server.py` does not expose `context_cache_mode` in its editable field set.

Interpretation:
- The March 25 report was accurate about code-level mode support.
- It was not enough to prove that the control plane and runtime schema were aligned.
- It also cannot be treated as a clean closure report, because the raw sessions immediately beneath it already contained internal contradiction about the actual cache-break cause.

### 2026-03-27 -- live runtime still showed schema drift before today's patch

Evidence:
- `/home/user/.claude/glass_config.json`
- `/tmp/glass-proxy/current-19999.log`
- `/home/user/.claude/glass-replay-capture-20260325/2026-03-27T09-35-59.464408285_01-00_bba629dc8f0d_6885_req_00032p75841Ex8AFACMCsyBv.json`
- `/home/user/.claude/glass-replay-capture-20260325/2026-03-27T09-36-07.939668327_01-00_28a034b5507a_6885_req_00032p75LZY2V9irtK1I0ili.json`

Verified facts before patch:
- Live config used `thinking_budget`, not `force_thinking_budget`.
- Runtime log still emitted the fallback warning:
  `thinking enabled without budget_tokens — injecting default 31999`
- Replay captures show live Claude Code requests with:
  - `max_tokens: 32000`
  - `thinking.budget_tokens: 31999` in final outbound bodies

Interpretation:
- The live proxy was still relying on a fallback path rather than a cleanly configured force-thinking budget.
- That path was safe only when Claude Code happened to send `max_tokens > 31999`.
- The reported 400 error is consistent with the same fallback path being hit on a request whose `max_tokens` was `<= 31999`.

## What Was Fixed On 2026-03-27

Evidence:
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/forcemode/thinking.go`
- `internal/forcemode/thinking_test.go`
- `internal/serializer/serializer.go`
- `internal/serializer/serializer_test.go`
- `config_server.py`

Verified changes:
- Config loader now maps legacy `thinking_budget` to `force_thinking_budget`.
- Force-thinking now raises `max_tokens` even when the budget came from the fallback path.
- Serializer config now accepts both `ser_enabled` and `serializer_enabled`.
- Config server now preserves existing config keys on save and synchronizes alias pairs instead of silently dropping unknown fields.
- Focused tests passed:
  - `./internal/config`
  - `./internal/forcemode`
  - `./internal/serializer`
  - `./internal/proxy`

## Current High-Confidence Conclusions

1. The repo contains real surviving PRIME-GOLDEN methodology and tests. That work was not imaginary.
2. The system fell into disorder partly because control-plane config schema and runtime schema drifted apart.
3. The current `max_tokens` vs `thinking.budget_tokens` failure is not "just Anthropic being weird". It is strongly consistent with a local compatibility bug that left the proxy on a fallback thinking-budget path.
4. The March 25 work was real at the code layer, but not fully operationalized end-to-end through the control plane.

### 2026-03-25 research -> present code state: per-session upstream transport isolation

Evidence:
- `text-GLASS-BACK2RESEARCH-5.md`
- `docs/2026-3-16/serializer-analysis.md`
- `internal/proxy/transport_pool.go`
- `internal/proxy/proxy.go`
- `/tmp/glass-proxy/current-19999.log`

Verified facts:
- The March 16 serializer analysis already concluded that per-session isolation could work in Glass because canonical system prompt caching makes session bytes identical.
- The March 25 raw research explicitly proposed replacing a single shared upstream transport with per-session or per-conversation `http.Transport` instances.
- `transport_pool.go` was created on `2026-03-25 17:15:25 +0100`, effectively the same moment as the March 25 research note that called for the change.
- The current proxy code now contains that architecture in qualified form:
  - `msgTransportPool` provisions per-affinity transports for `/v1/messages`
  - subagents share the parent affinity transport
- Current live logs show:
  - `[WARN] Running without egress/sidecar`
  - `[TRANSPORT-POOL] Created transport ...`
  so the direct-path deployment is presently exercising the pool in practice.
- The direct-path implementation rationale matches the raw research:
  - independent upstream connection pools reduce cross-session interference at the connection layer

Contradicting / qualifying evidence:
- The implementation is keyed by affinity key rather than a naive raw conversation id.
- When `sidecarProxy` is enabled, the message path uses `sidecarTransport` instead of the transport pool.
- The exact landing date of the implementation is not yet pinned from a primary timestamped artifact.

Interpretation:
- This was not an abandoned idea.
- It was documented by March 16, implemented on March 25, and is active on the present direct deployment path.
- The remaining caution is not whether it landed, but whether a given runtime topology actually uses it.
