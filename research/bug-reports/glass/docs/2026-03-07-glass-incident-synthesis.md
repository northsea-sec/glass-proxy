# Glass Incident Synthesis and Recovery Brief

Date: 2026-03-07

## Scope

This note synthesizes the Markdown corpus created on 2026-03-06 and 2026-03-07 under `/home/user/nataraja`, plus limited spot checks of `TEXT-prime-glass-corrupted-codex.MD` because the March 7 reconstruction docs cite it as primary evidence for the early evening failure window.

The corpus is large and mixed:

- raw incident transcripts
- reconstructed timelines
- source-of-truth model docs
- replay and falsifier reports
- Glass V2 design notes
- post-disaster transcripts covering later March 7 live experimentation

This note is intended to answer four questions:

1. What happened, in time order?
2. What were the real technical failures?
3. What is the best current working model?
4. What should be done next?

## Executive Summary

The March 6 to March 7 Glass failure was not one bug. It was a chained incident.

The strongest current reading is:

- the morning model was mostly correct: first-request cold start for a fresh session is partly unavoidable under Anthropic TTL rules; the real proxy target is warm-up slope and post-overflow prefix stability
- the evening failure started before the famous `345708` collapse; `151639` and `152756` were already showing six-figure recreate loops around `21:03`
- the rollback became a second incident because stale whole-file `.bak` sources were used against a newer architecture, tests were red, and restart still proceeded
- `345708` had two distinct failures: first an invalid persisted replay/reset problem, then fresh live re-eviction cycles after reset
- the frozen reference-pair patch was necessary cleanup but not sufficient; fresh lane `84551` still showed repeated staircase misses after first eviction
- replay work on `101127` supports a stronger architectural move: a one-way pinned hot frame after overflow, not repeated micro-mutations of the same retained prefix
- later March 7 work drifted again into live experimentation on real lanes, especially `120344`, producing serious burn spikes and further protocol violations

The project architecture is not disproven. The main failure multiplier was methodological drift plus a still-unsolved post-overflow serializer/staircase problem.

## High-Confidence Timeline

### 1. Morning baseline and correct problem framing

Time window: about `10:01` to `10:34` CET on 2026-03-06.

What happened:

- DB evidence shows low burn and a healthy warm-up progression.
- `text-PRIME-GLASS-SHIT.md` correctly separates two problems:
  - unavoidable first-call cold start on a fresh lane
  - avoidable warm-up slope and prefix instability
- the morning edits were bounded and snippet-backed
- the operator model was still disciplined:
  - baseline first
  - upstream Anthropic rules first
  - narrow changes only

Why it matters:

- this is the last clearly healthy methodological phase
- it establishes that conversation ID itself is not the real cache identity
- it also shows that the older "keepalive + UUID" idea is secondary to the later post-overflow failure modes

### 2. Early evening live-session damage begins

Time window: about `21:01` to `21:06` CET on 2026-03-06.

What happened:

- `151639` and `152756` started in healthy high-read states
- both then flipped into severe rows shaped like:
  - `~140k-155k cache_creation`
  - `~10052 cache_read`
- transcript evidence and DB timing align with live edits and restart activity on active sessions

Why it matters:

- the incident began earlier than `345708`
- this falsifies the story that the whole day can be reduced to one late-night poisoned lane

### 3. Rollback phase becomes a second incident

Time window: about `21:20` to `21:40` CET on 2026-03-06.

What happened:

- stale `.bak` files were used as rollback sources against a newer March 6 architecture
- immediate pre-overwrite snapshots were saved under `/tmp/glass-rollback-20260306/*.current`
- those `.current` files were exact pre-overwrite state, not known-good state
- tests were red
- restart still proceeded

Why it matters:

- this mixed the source tree
- it destroyed clean causality
- it converted recovery into another source of damage

### 4. Late-night `345708` collapse

Time window: about `23:20` to `23:28` CET on 2026-03-06.

What happened:

- `345708` was healthy and read-heavy before the break
- first eviction fired at `23:27:14`
- seven seconds later the DB showed a severe break:
  - `23:27:21`: `98145 / 10052`
- runtime evidence then showed invalid outbound structure during replay
- Glass reset persisted state at `23:28:26` because replayed persisted cache produced an invalid outbound request

Why it matters:

- this proves a real persisted replay / invalid outbound problem existed
- it also makes clear that the first `345708` failure was not just "backend randomness"

### 5. Post-reset fresh re-eviction disaster

Time window: about `23:35` CET on 2026-03-06 through after midnight.

What happened:

- after reset/restart, `345708` entered fresh new eviction cycles
- DB rows repeatedly showed `93k-142k create / 10052 read`
- `shadow_index.json` showed batch resets and fresh post-reset batches
- another major re-eviction happened at `23:43:53`, followed by:
  - `23:43:58`: `72777 / 10052`, `glass_evicted_count=166`
- additional severe rows continued after midnight

Why it matters:

- `345708` was not just persisted poison
- it also had a fresh live post-overflow instability

### 6. March 7 reconstruction and scope repair

Time window: roughly `09:03` through early afternoon on 2026-03-07.

What happened:

- the incident was re-anchored using DB exports, snippet backups, transcripts, and local design docs
- the reconstruction docs externalized:
  - the merged timeline
  - the Anthropic cache model
  - the Glass design model
  - contradictions and drift
  - recovery method and replay plan

Why it matters:

- this was the return to evidence-first thinking
- it separated upstream facts from local inference

### 7. Prefix-drift falsifier and post-eviction lane analysis

Time window: midday 2026-03-07.

What happened:

- `151639` produced repeated same-prefix or identical-shape severe misses inside TTL
- `152756` remained less conclusive
- `345708` lacked request fixtures, so it stayed a DB/log/state-file lane
- fresh live lane `84551` showed the frozen reference patch was not sufficient:
  - repeated `10052`-floor severe misses persisted across many small batches
  - then the lane eventually recovered

Why it matters:

- pure prefix drift is not a sufficient universal explanation
- the remaining local post-eviction behavior is still broken

### 8. `101127` replay and Glass V2 direction

Time window: afternoon 2026-03-07.

What happened:

- current-state replay on `101127` reproduced the bad window from both:
  - an earlier preserved snapshot
  - the later live state
- a simple `window_start` pin failed
- a stronger pinned-frame v2 serializer removed the offline staircase on the reproduced bad window

Why it matters:

- the problem is not "one more anchor tweak"
- the problem is the retained bridge segment itself mutating too early in the visible prefix

### 9. Later March 7 live drift and `120344`

Time window: mid to late afternoon 2026-03-07.

What happened:

- experimental pinned-frame v2 work was pushed back into live-path experimentation before it had decisively beaten baseline offline
- lane `120344` showed a toxic oscillation:
  - repeated `~147k cache_creation`
  - `~10k cache_read`
  - burn around `106.9 pp/hr` over `202s`, with a short spike plausibly around `160 pp/hr`
- the process again drifted into symptom-driven firefighting instead of replay-first discipline
- a lane-quarantine guard was added afterward as a live safety mechanism

Why it matters:

- the architecture direction may still be right
- but the live experimental process was wrong

## Technical Issues

### A. Upstream constraints that were repeatedly misunderstood

- Anthropic cache identity is exact prefix match through the breakpoint, not conversation ID.
- Restart does not itself erase Anthropic cache; what matters is whether the next request reproduces a hittable prefix inside TTL.
- "There are only 2-4 cache slots total" is not supported by official Anthropic docs.
- Extended-thinking semantics matter:
  - tool-result continuations and ordinary user turns are not equivalent
  - thinking changes can invalidate message caching without proving a total system/tools miss
- Chasing a universal Anthropic LRU proof is not the highest-value blocker.

### B. Local proxy bugs and failure modes

#### 1. Mutable synthetic reference text

- Early post-eviction reference text embedded mutable batch-specific details.
- That changed prompt bytes near the front of the request on follow-up batches.
- This was correctly identified and partially fixed, but that fix was not sufficient.

#### 2. Staircase under-eviction after first overflow

- Fresh lane `84551` is the clearest proof.
- After the first overflow, Glass kept evicting only tiny additional batches.
- The lane paid many repeated severe misses before recovery.
- This strongly implicates post-eviction budget/hysteresis behavior.

#### 3. Retained-prefix mutation after the reference trench

- `101127` replay shows the bad window diverging early at `msg[5]`.
- The unstable bridge segment remained part of the visible hot prefix.
- A simple `window_start` lock was not enough.
- This supports a real pinned-frame mode switch instead.

#### 4. Persisted replay invalidity

- `345708` proved a real persisted replay bug:
  - replayed persisted evicted state could build an invalid outbound request
  - Glass explicitly reset persisted state because it was unsafe to resume

#### 5. Outbound message-structure corruption

- Multiple transcripts describe invalid outbound shapes:
  - assistant-final requests
  - tool-use / tool-result orphan splits
  - broken alternation around reference insertion
- These became especially visible in the later pinned-frame live experiments and Gemini-assisted repair attempts.

#### 6. Same-prefix severe misses

- `151639` is the strongest falsifier lane.
- Repeated same-prefix or identical-shape severe misses existed inside TTL.
- That means prefix stability is necessary but not sufficient.
- There is still a second layer of local or upstream failure beyond pure prefix drift.

### C. Process and operational failures

- Live active sessions were used as the primary testbed.
- Proxy restarts occurred on active sessions during incident conditions.
- Whole-file rollback from stale `.bak` files corrupted the source state.
- Red tests stopped blocking restart.
- Compaction occurred before the working model was fully externalized.
- Success criteria drifted from burn stability to "this turn no longer errors."
- Experimental architecture work was pushed live before an offline baseline win.

### D. Evidence and tooling gaps

- `345708` did not have the same saved request fixture coverage as `151639` and `152756`.
- Snippet backups only capture the morning edit cluster, not the later live interventions.
- Exact outbound prefix diffs per request are still not first-class persisted artifacts.
- The replay harness is strong enough to rank architectures offline, but not to prove exact Anthropic cache-read outcomes for every counterfactual payload.

## Best Current Working Model

The best current model is:

1. Anthropic exact-prefix and TTL rules are real hard constraints.
2. Some misses are genuine prefix misses.
3. Some misses are same-prefix severe misses and cannot be explained by drift alone.
4. The remaining high-value local bug is post-overflow request instability:
   - repeated retained-prefix mutation
   - repeated micro-batch evictions
   - invalid or fragile serializer behavior around references and tool boundaries
5. The right architectural direction is not "keep shaving the same live lane forever."
6. The right direction is:
   - one-way mode switch after first serious overflow
   - pinned hot Anthropic-visible lane
   - exact shadow/archive lane outside the hot prefix
   - explicit recall when needed

In short:

- V1 failed by mutating the hot lane too often after overflow.
- V2 should make the hot lane smaller, more stable, and less magical.

## What The Existing Codebase Already Appears To Contain

A quick code check in `/home/user/glass-proxy` shows the repo already contains work aligned with the above direction:

- `internal/glass/reference.go`
  - frozen reference-pair logic
- `internal/glass/localcache.go`
  - `PinFrame(...)`
  - `BuildPinnedFrameForRequest(...)`
  - pinned-tail logic
- `internal/glass/process.go`
  - post-overflow path using pinned-frame mode
- `internal/glass/session.go`
  - `WindowStart`
  - `ReferenceBatch`
- `internal/proxy/lane_quarantine.go`
  - repeated-toxic-lane guard

That said, the implementation is not yet cleanly settled.

Current verification on this machine:

- command run:
  - `cd /home/user/glass-proxy && /usr/local/go/bin/go test ./internal/glass ./internal/proxy ./internal/debug`
- result:
  - `internal/proxy` passed
  - `internal/debug` passed
  - `internal/glass` failed
- current failing test:
  - `TestEvictedSessionsShiftBreakpointPastInjectedReferences`
- failure shape:
  - expected breakpoint after reference injection `msg[4]`
  - got `msg[3]`

So the codebase already contains the architectural pivot, but at least one core invariant around breakpoint placement is still failing in tests.

## What Needs To Be Done Now

### Immediate

1. Freeze live architecture experimentation on real sessions.
2. Treat `120344` as evidence, not as a lane to keep operating on.
3. Use burn and severe-miss metrics as the controlling signal again.
4. Keep the quarantine guard, but do not assume it replaces architectural correctness.

### Short-term engineering

1. Fix the failing Glass test around breakpoint placement after reference injection.
2. Re-run the targeted suite until `./internal/glass ./internal/proxy ./internal/debug` is green.
3. Keep the frozen reference-pair fix, but downgrade it to "necessary cleanup," not "the fix."
4. Finish exact outbound prefix-diff instrumentation per request and per batch:
   - system hash
   - tools hash
   - prefix hash through active breakpoint
   - reference insertion point
   - retained count
   - evicted count
5. Harden post-eviction budgeting/hysteresis so one overflow produces one deeper cut instead of repeated two-message nibbles.

### Offline validation program

Use the replay harness and current-state replay as the main decision engine.

Primary lanes:

- `151639` for same-prefix anomaly
- `152756` as a healthier control
- `345708` for persisted-reset plus re-eviction mess
- `84551` for staircase on a fresh lane
- `101127` for reproduced bad-window replay
- `120344` for later live-drift and toxic burn behavior

Acceptance criteria:

- at most one severe miss immediately after first overflow
- no repeated `10052`-floor staircase
- no invalid outbound requests
- no regression on healthier control windows
- materially lower cumulative create burden offline

### Medium-term architecture

1. Continue the move toward:
   - pinned hot lane
   - exact archive lane
   - one-way overflow mode switch
2. Keep public-repo inspiration in the right category:
   - context virtualization
   - explicit checkpoints
   - progressive disclosure memory
   - cache-control instrumentation
3. Do not spend primary time on:
   - generic router patterns
   - proving Anthropic LRU
   - keepalive/UUID work as if it solves the main post-overflow failure

### Any future live validation must obey this protocol

1. Fresh sacrificial session only.
2. One hypothesis only.
3. Continuous burn watch from the first request.
4. Predeclared abort threshold.
5. Immediate abort on repeated high create plus floor-level read after overflow.
6. No restart without a verified rollback target.
7. Baseline note written to disk before touching code.

## Bottom Line

The root engineering target is no longer "why was the first new session cold?" and it is not "prove Anthropic LRU."

The real target is:

- make the post-overflow Anthropic-visible lane stable
- stop repeated retained-prefix mutation
- stop staircase under-eviction
- keep outbound structure valid
- enforce replay-first discipline so live quota is not the lab

That is the shortest honest summary of what the full March 6 to March 7 corpus says.

## In-Scope Markdown Files Read For This Synthesis

- `incident_reconstruction/2026-03-06/345708_LANE_REPORT.md`
- `incident_reconstruction/2026-03-06/345708_PREFIX_STABILITY_FIX.md`
- `incident_reconstruction/2026-03-06/ANTHROPIC_CACHE_MODEL.md`
- `incident_reconstruction/2026-03-06/CORRELATION_SUMMARY.md`
- `incident_reconstruction/2026-03-06/DOC_CONTRADICTIONS.md`
- `incident_reconstruction/2026-03-06/DOC_MANIFEST.md`
- `incident_reconstruction/2026-03-06/DOC_SCOPE_PLAN.md`
- `incident_reconstruction/2026-03-06/DRIFT_POINTS.md`
- `incident_reconstruction/2026-03-06/GLASS_DESIGN_MODEL.md`
- `incident_reconstruction/2026-03-06/MERGED_TIMELINE.md`
- `incident_reconstruction/2026-03-06/NON_DOC_LRU_EVIDENCE_RANKING.md`
- `incident_reconstruction/2026-03-06/OFFLINE_PREFIX_FALSIFIER_REPORT.md`
- `incident_reconstruction/2026-03-06/PUBLIC_REPO_CODE_AUDIT_FOR_GLASS_V2.md`
- `incident_reconstruction/2026-03-06/PUBLIC_REPO_SURVEY_FOR_GLASS_V2.md`
- `incident_reconstruction/2026-03-06/RECOVERY_METHOD.md`
- `incident_reconstruction/2026-03-06/REHYDRATED_CONCLUSIONS_AND_RECOMMENDATIONS.md`
- `incident_reconstruction/2026-03-06/SCOPE_STATUS.md`
- `incident_reconstruction/2026-03-06/TODAY_POST_DISASTER_SESSION_RECONSTRUCTION.md`
- `incident_reconstruction/2026-03-06/TRAFFIC_COUNTERFACTUAL_REPORT.md`
- `incident_reconstruction/2026-03-06/TRAFFIC_REPLAY_BASELINE_REPORT.md`
- `incident_reconstruction/2026-03-06/TRAFFIC_REPLAY_TEST_PLAN_FOR_GLASS_V2.md`
- `incident_reconstruction/2026-03-06/TRANSCRIPT_INDEX.md`
- `incident_reconstruction/2026-03-07/CURRENT_STATE_REPLAY_101127_FINDINGS.md`
- `incident_reconstruction/2026-03-07/PINNED_FRAME_V2_IMPLEMENTATION_RESULT.md`
- `incident_reconstruction/2026-03-07/PINNED_FRAME_V2_TARGET.md`
- `incident_reconstruction/2026-03-07/WINDOW_START_PIN_EXPERIMENT.md`
- `incident_reconstruction/2026-03-07/current_state_snapshots/2719b7a469d9_101127_1425_1/shadow.md`
- `plan-keepalive-uuid.md`
- `text-PRIME-GEMINI-FUCKUP.md`
- `text-PRIME-GLASS-BACK2-DISASTER.md`
- `text-PRIME-GLASS-CORRUPTED.md`
- `text-PRIME-GLASS-DISASTER-1.md`
- `text-PRIME-GLASS-DISASTER-2.md`
- `text-PRIME-GLASS-POST-DISASTER-1.md`
- `text-PRIME-GLASS-POST-DISASTER-2.md`
- `text-PRIME-GLASS-POST-DISASTER-3.md`
- `text-PRIME-GLASS-POST-DISASTER.md`
- `text-PRIME-GLASS-SHIT.md`

Supplemental spot check because the March 7 corpus explicitly treats it as primary evidence for the evening window:

- `TEXT-prime-glass-corrupted-codex.MD`
