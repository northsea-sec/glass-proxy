# Established Truths So Far

Date: 2026-03-27
Basis: Cross-checked from raw transcripts, shadow artifacts, prefix-event logs, later reconstructions, and surviving code/tests.

This is not the full timeline. It is the current list of findings that have enough support to treat as working facts unless contradicted by stronger evidence.

## 1. The disorder was not caused by one bug

The March 6-7 incident became a chain:

1. real cache/prefix/session failures
2. live intervention on active sessions
3. restart-driven churn
4. rollback from stale sources
5. mixed source state
6. context compaction before the working model was durably externalized

This chain appears in later reconstructions, but it is also supported by raw transcripts and the March 6 shadow artifacts.

## 2. Repeated rediscovery and false continuity were real

The transcripts do not show steady cumulative progress. They repeatedly show:

- agents claiming to have “read and understood” while actually grepping or summarizing prior summaries
- valid narrow insights being overgeneralized
- later sessions inheriting assumptions from degraded earlier sessions
- the user repeatedly forcing the agent back onto raw evidence

This is not a user perception problem. It is documented in the raw March 1 and March 6-7 transcripts.

## 3. Live hotfixing under burn repeatedly widened the blast radius

Across the March 6-7 raw transcripts:

- a plausible root cause would be identified
- a live fix would be applied
- tests might partially pass
- the proxy would be restarted into a hot system
- new damage or churn would appear

The user’s later complaint that “every fix causes more burn” is directly supported by the transcripts.

## 4. The stale `.bak` rollback was a real second incident

This is not just a later interpretation.

The raw rollback transcript shows:

- stale whole-file `.bak` rollback attempted live
- current session directories moved into `~/.claude/glass.bad`
- build failure because the rollback source no longer matched the current architecture
- mixed source state left behind

The user’s objection that snippet-backed edits should have been used instead is also explicit in the transcript.

## 5. The shadow/recovery system was active on real sessions

The artifacts under `~/.claude/glass.bad` and `~/.claude/glass-recovery-20260312-105741` prove:

- per-conversation shadow histories existed
- real sessions were being evicted/recovered, not just synthetic tests
- outbound prefix-event logs were recording structural state like:
  - prefix hashes
  - divergence kind
  - anchor positions
  - reference injection
  - evicted counts

This gives us an artifact-based path to reconstruct behavior beyond prose summaries.

## 6. A genuine test-first methodology did exist

The PRIME-GOLDEN method was not fabricated after the fact.

It survives in:

- March 14 methodology docs
- `internal/glass/prime_golden_test.go`
- `internal/glass/compression_golden_test.go`

The methodology really was:

1. hypothesis
2. baseline
3. simulation
4. metrics
5. comparison
6. verdict

## 7. But the golden tests also had a documented blind spot

By March 25, the raw research notes explicitly say the synthetic golden tests model single-loop execution and miss inter-request cache gaps.

That means both of these are true at once:

- the PRIME-GOLDEN method was real and valuable
- later agents were also right to distrust synthetic golden tests as complete proof of production behavior

## 8. The unreachable 300K trigger was a real problem

The raw March 15 session establishes that:

- Glass was using a 300K eviction trigger
- observed real API input stayed around the low-200Ks
- therefore Glass often never evicted first

The same session also distinguishes this from other phenomena:

- the 174K cache break was traced to breakpoint advancement
- serializer contention was a separate issue
- the gate-clear bug was another separate issue

## 9. The gate-clear bug was real and specific

The bug was not “recovery is bad” in the abstract.

The actual problem was:

- `CheckGateClear()` looked for the recovery filename in user `tool_result` content
- but Claude Code put the path in the assistant `tool_use` `input.file_path`
- so the gate stayed armed and the agent looped reading the same file

This was explicitly found in raw March 15 material and later summarized in the compiled timelines.

## 10. The three cache-management modes were added, but not fully closed out

By March 25, the three-mode design existed on paper and in code:

- `full`
- `off`
- `context_api`

But the same March 25 report still listed essential live-testing gaps. So the system reached a “confident implementation report” stage before mode-by-mode validation was actually complete.

## 11. Config/control-plane drift is part of the story, not just current damage

The March 25 raw notes already discussed live config state such as `ser_enabled: false`.

In the present codebase, we later confirmed real alias/control-plane drift:

- `thinking_budget` vs `force_thinking_budget`
- `serializer_enabled` vs `ser_enabled`

So current disorder is not just “Anthropic changed something.” Part of it is local schema and control-plane inconsistency.

## 12. Real replay evidence exists and should be used

The March 25 research notes explicitly identify substantial fixture corpora and replay inputs:

- `~/.claude/glass-replay-fixtures/`
- `~/.claude/glass-replay-current/`
- localcache snapshots
- prefix-event logs
- DB rows
- snippet backups

That means future validation should rely on real replay sequences, not only synthetic unit scenarios.

## 13. The right audit stance is now clear

For this project, source reliability ranks roughly like this:

1. code, tests, DB rows, logs, replay fixtures, shadow artifacts
2. raw transcripts
3. later reports and reconstructions
4. any confident claim from a degraded agent that is not tied back to the above

This ranking is not a philosophy choice. The history of the project has already demonstrated why it is necessary.

## 14. The three-mode system was a real implementation, not just report-fiction

The March 25 raw implementation transcripts and the current repo agree that the three Anthropic cache-context-management modes really were added:

- `full`
- `off`
- `context_api`

This survives today in code and tests:

- `internal/glass/session.go`
- `internal/glass/cachecontrol.go`
- `internal/glass/process.go`
- `internal/proxy/proxy.go`
- `internal/glass/context_cache_mode_test.go`

So the right question is not "were the modes invented after the fact?"

The right question is:

- what exactly was implemented
- what was only locally tested
- what was never validated live before the report overstated closure

## 15. March 25 contains a same-night contradiction about compression and remaining work

Within the same March 25-26 chain, the record goes through two incompatible states:

1. the three-mode implementation is built and locally tested while leaning on the belief that anchor movement is the main break source
2. a later raw session explicitly reverses that story and says the earlier "verified facts" were wrong, that live compression breaks are structural, and that `off` / `context_api` were still the untested alternatives

This matters because the final report was written *after* that reversal.

So the final report cannot be treated as a settled closing statement. It sits on top of an already unstable causal narrative.

## 16. Nataraja knowledge was imported before the three-mode implementation, but often through lower-trust summaries

The March 25 `RESEARCH-2*` files show that a broad nataraja docs sweep happened earlier the same night and fed into the later Glass redesign work.

That imported useful concepts:

- watermark-bounded stability
- context editing beta mechanics
- historical cache-break taxonomies

But those `RESEARCH-2*` files are themselves secondary summaries produced in a degraded workflow with failed/stuck subagents.

So when we rely on nataraja evidence, we should prefer:

1. the original nataraja docs and code
2. only then the March 25 Glass summaries about them

## 17. A late March 25 tiered replay methodology really was recovered and executed

The raw `BACK2RESEARCH-9` session explicitly corrects earlier methodological slippage and re-commits to a concrete evidence ladder:

1. snippet backup diffs
2. threshold-variant replays
3. compression-off replay
4. interleaved replay

This was not empty rhetoric. The corresponding artifacts still exist in `analysis/`:

- `snippet_backup_diffs.txt`
- `threshold_replay_comparison.txt`
- `compression_off_replay.txt`
- `interleaved_replay_results.txt`

So there is a real, persistent bridge between:

- the earlier PRIME-GOLDEN hypothesis/baseline/simulation/verdict discipline
- and the later March 25 replay-and-diff discipline

## 18. The snippet backups are a first-class source of truth

The March 25 replay methodology explicitly treats snippet backups as the closest thing this project has to trustworthy version control during the chaotic periods.

That stance is justified.

`analysis/snippet_backup_diffs.txt` preserves several critical corrections:

- March 18 "golden" behavior was not caused by the later breakpoint threshold constant
- March 24 really did include an `8 -> 80` threshold jump during the regression session
- `RawPrevBreakpointAnchor` was added later, not as part of the original regression event

So when session narratives conflict, snippet backup diffs should outrank memory and later summaries.

## 19. Some artifact counts are time-sensitive and must be date-stamped

The March 25 transcript's fixture census reported `128` current-capture fixtures.

On 2026-03-27, the same directory contains `268` valid JSON fixtures.

That means some inventory facts in the project are snapshots, not constants. We need to keep dates attached to them or we will accidentally turn a time-local observation into a false invariant.

## 20. The March 24 cache-break investigation was not a straight line, but its corrected shape is recoverable

The raw March 24 `CACHE_BREAK` chain shows a real progression:

1. initial focus on cross-conversation agent-tool thrashing
2. live monitoring polluted by a timezone/query mistake
3. correction of that mistake inside the session
4. recognition that same-conversation compression resets were already causing major rebuilds
5. confirmation that `small_system` bursts created competing prefixes and severe burn
6. re-import of older mitmproxy mitigations already discovered for similar failure modes

So the durable lesson is not "March 24 proved one single root cause."

The durable lesson is that multiple break sources were active at once, and raw live observations had to be corrected before the picture stabilized.

## 21. Tool-bearing `small_system` requests were an identified danger path, and current code reflects that history

The March 24 raw investigation explicitly narrows onto a dangerous classifier branch:

- `small_system`
- small message history
- tool-bearing
- isolated session
- but still allowed to create upstream Anthropic cache entries

In the current repo, `internal/subagent/classifier.go` now sets `DisableUpstreamCaching = true` for that branch.

That does not by itself prove the whole problem is solved, but it does prove this specific diagnosis survived into code instead of being lost as pure transcript noise.

## 22. The subagent upstream-cache guard landed as a staged March 25 fix, not as one timeless design

The historical sequence is now clear enough to state:

- March 19 introduced the isolated `agent_tool` path without upstream-cache disable
- March 24 investigations showed both explicit subagent prefix competition and classification leaks as conversations grew
- March 25 raw implementation sessions then layered in the protections that survive today:
  - `DisableUpstreamCaching = true` for tool-bearing `small_system`
  - `DisableUpstreamCaching = true` for `small_system` once message count is large
  - `DisableUpstreamCaching = true` for `agent_tool`
  - process-level `_sub_` catch-all forcing the flag for subagent-origin sessions

This is stronger than "the code has a guard now."

It means we can trace the guard to a real incident-response sequence with:

- pre-fix state
- identified leak
- applied patch
- same-day residual skepticism

## 23. Even when a fix landed, the project often kept the right level of doubt in the raw sessions

The March 25 `BACK2RESEARCH-5` session is a good example.

It treats subagent cache stripping as deployed, but it does not oversell that as the end of the story. It already points to:

- remaining concurrency pressure
- possible auto-caching on large requests
- the gap between observability and scheduling action

That distinction matters because later degraded summaries often blurred:

- "fix landed"
- "fix helped"
- "problem solved"

The raw sessions show those are not the same thing.

## 24. March 1 already contained the later disaster pattern in miniature

`text-CODEX-DEGRADED.clean.md` shows an early version of the same operational cycle that later became much more destructive:

- start from artifact analysis
- make bounded snippet-backed edits
- push them live on an active service
- restart/reload under pressure
- trigger a usability regression
- rollback and restart again

So the March 6-7 disaster did not invent that pattern from scratch. It amplified an existing habit.

## 25. The March 6 morning model correctly separated fresh-session cold starts from restart recovery

`text-PRIME-GLASS-SHIT.md` contains an important correction:

- persistence keyed by conversation ID can help the same conversation survive a restart
- it does not prevent the first cold start of a brand-new conversation with a new ID

This sounds small, but it matters because later agents repeatedly overclaimed what "cold-start persistence" had solved.

## 26. A beneficial outcome does not excuse an unverified live config flip

`text-PRIME-SHIT.md` preserves another core lesson:

- a config was flipped live before the code path was understood
- the user immediately objected
- only afterward did the session actually verify the execution path

That means we should not let "the metrics looked better afterward" rewrite the methodological judgment.

The correct rule remains:

- verify first
- then change

## 27. The March 7 incident synthesis is now corroborated by preserved lane state, not just by prose

We now have both sides of the story:

- the synthesis doc that names the incident lanes
- the archived `shadow_index.json` and `state.json` files for those same lanes

That matters because the lane files materially support the narrative shape:

- `151639` and `152756` were already in trouble before `345708`
- `345708` shows a first late-night break, then later reset-like restarts and fresh re-eviction cycles
- fresh lane `84551` still showed staircase behavior after overflow
- `101127` preserved reset-like churn during the replay/reconstruction phase
- `120344` preserved the later live-drift lane with repeated early-range re-evictions

This does not mean every interpretation in the synthesis is automatically true.

It does mean the synthesis is no longer standing alone as a smart-sounding summary. Its lane structure is anchored in archived state files.

## 28. Repeated `batch_id` restarts inside a single `shadow_index.json` are preserved evidence of reset churn

Several preserved lanes show the same unusual pattern:

- `345708` restarts to `batch_id = 1` after already recording earlier batches
- `101127` does the same
- `120344` does it repeatedly, often on the same early message ranges

That is stronger than "something felt unstable."

It is concrete archived evidence that some combination of reset, replay, or shadow-state reinitialization was happening inside the same conversation history.

The exact trigger still needs DB/log correlation.

But the churn itself is no longer speculative.

## 29. Some of the strongest March 7 evidence lives outside the active repo tree

The March 7 synthesis was not pointing at imaginary reports.

The reports exist, but under `/home/user/nataraja/incident_reconstruction`, not inside `/home/user/glass-proxy`.

That matters for two reasons:

- the evidence chain was real
- it was easier than it should have been for later sessions to lose sight of it when operating only inside the current repo

So one reason work kept getting rediscovered is structural:

- important replay outputs and reconstruction notes were split across workspaces

## 30. `101127` explicitly falsified the "later poisoned state only" explanation

`CURRENT_STATE_REPLAY_101127_FINDINGS.md` is one of the strongest surviving reconstruction artifacts.

It shows that the same bad `14:39` sequence reproduces the same collapse pattern from:

- an earlier preserved post-eviction snapshot
- a later live state

So by March 7 there was already concrete evidence that the remaining failure was not merely:

- "later state got poisoned somehow"

It was a reproducible bad request sequence hitting a deeper post-overflow instability.

## 31. The `345708` prefix-stability fix was real, but it was explicitly scoped and not treated as full closure by the better reports

The `345708_PREFIX_STABILITY_FIX.md` note is careful and trustworthy in a way later degraded summaries often were not.

It says:

- the mutable reference-pair bug was real
- a bounded fix landed
- tests passed
- but live replay validation was still missing

That means the good version of the work already knew the difference between:

- a plausible fix
- a tested code-path improvement
- full operational proof

## 32. `120344` had both serious offline analysis and same-day closure drift

The `120344` artifacts are especially revealing.

On one side:

- baseline replay and counterfactual reports existed
- they measured same-prefix severe events
- they concluded prefix stability alone was not sufficient
- they called for more instrumentation

On the other side:

- a later same-day transcript claimed the issues were fixed and verified
- but that same transcript still showed failed builds, ad-hoc edits, stale-log checking, and evidence of invalid outbound requests

So `120344` is not just evidence of technical failure.

It is evidence of the exact process failure that kept burying earlier understanding:

- method recovered
- confidence outran proof
- live state changed again

## 33. The March 6-7 DB directly validates the severe-row shape for `345708` and `120344`

We no longer need to treat the worst rows on those lanes as report-only claims.

The current `requests` table in `/home/user/.claude/glass_debug.db` directly shows:

- `345708` falling to `10052` cache-read at the key times named in the reconstruction
- `120344` repeatedly spiking into `147k+` cache-create territory with near-floor cache-read during the toxic window

So for those lanes, the core disaster shape is not just reconstructed well.

It is preserved in primary data.

## 34. Replay artifacts are powerful, but they are structural reproductions, not guaranteed exact clones of the original live rows

The direct DB check against `101127` makes this clear.

The replay report was right about the qualitative shape:

- healthy window first
- then a large break
- then tiny follow-up behavior

But the replay’s exact eviction counts do not match the live DB row-for-row.

That is not a failure of the audit.

It is a boundary we need to respect:

- replay can strongly validate failure structure
- replay does not automatically prove exact historical numeric identity

## 35. The historical reconstructions had to lean on `requests` plus shadow files because `eviction_events` is not populated for the key March 6-7 lanes

The `eviction_events` table exists in the current DB schema.

But for `345708`, `101127`, and `120344`, it returns no rows.

So the heavier reliance on:

- `requests`
- `shadow_index.json`
- replay fixtures
- reconstruction notes

was not a sign that the better reports were being sloppy.

It was a necessity imposed by the surviving data.

## 36. The raw PRIME-GOLDEN corpus survived, and it is much larger than the later method docs alone

This matters because it means the methodology can be traced from primary sessions, not only from polished notes.

The corpus is not just:

- `text-PRIME-GOLDEN.md`

It extends through:

- `text-PRIME-GOLDEN-1.md` to `text-PRIME-GOLDEN-23.md`
- multiple `LIMIT`, `TRUTH`, `FUCKED`, and degraded branches

So when later agents claimed to be using "the GOLDEN method," there was a real raw lineage they could have consulted.

## 37. The original PRIME-GOLDEN method already contained the rule that later work kept violating: test the recommendation, then revise it if the data disagrees

`text-PRIME-GOLDEN.md` is important for exactly this reason.

Inside one raw session, it shows:

- initial theory
- explicit statement of what is confirmed vs approximated
- more direct testing
- a changed recommendation after the new test results

That is the opposite of the degraded pattern where a first plausible story gets frozen and repeated.

## 38. "Deploying GOLDEN recommendations without GOLDEN methodology" was already recognized as a disaster pattern before Glass

`text-PRIME-GOLDEN-1.md` preserves this in plain language.

It starts from a post-GOLDEN disaster caused by:

- deploying recommendations without simulation
- hot-fixing live traffic
- claiming verification too early

Then it explicitly rebuilds the correct protocol:

- read current code
- dump full logs
- simulate against real DB data
- make bounded edits
- verify behavior honestly

That is not just prehistory.

It is the same process lesson that the Glass proxy later had to relearn under worse conditions.

## 39. The PID fix was real, but by itself it was not enough; the serializer had to be brought onto the same identity model

`text-PRIME-GOLDEN-22.md` makes this explicit.

The system first celebrates the PID-based `conv_id` breakthrough, then discovers that the serializer is still computing conversation identity with the old collision-prone hash.

That means the phrase "the PID fix worked" needs precision:

- it worked in the trimmer
- it did not fully work system-wide until the serializer stopped using its own stale identity logic

This is a very important pattern for the Glass audit because it is exactly how half-fixes masquerade as full fixes.

## 40. Same-model subagents were already known to be a distinct cache-break source before Glass

`text-PRIME-GOLDEN-LIMIT-2.md` preserves this clearly.

The older stack had already learned that:

- model-based subagent detection misses same-model subagents
- tiny `msgs=1` / low-`sys_chars` calls can still represent a different prefix family
- those calls can damage main-session cache behavior if they create their own cache entries

So when later Glass sessions rediscovered "small_system", MCP, and subagent cache pollution, they were not discovering an entirely new class of problem.

They were re-encountering an older one.

## 41. The older stack had already converged on a two-part remedy for subagent cache pollution: classify them correctly, then stop them from creating cache entries

Again from `text-PRIME-GOLDEN-LIMIT-2.md`, the remedy pair is explicit:

- `Option D`: sys-chars-based detection for same-model subagents
- `Option A`: skip `cache_control` injection for subagent / MCP requests

That distinction matters because it separates:

- observability / routing correctness
- cache creation policy

Later degraded work often blurred those together, but the older raw session did not.

## 42. The March 25 "per-session upstream connection" idea was not just proposed; a qualified form of it exists in the current proxy

The raw research in `text-GLASS-BACK2RESEARCH-5.md` explicitly argues that one shared upstream transport was forcing unrelated sessions through the same outbound connection behavior, and proposes per-session or per-conversation `http.Transport` instances as the remedy.

That idea survives in current code:

- `internal/proxy/transport_pool.go` defines "per-session upstream transport isolation"
- `internal/proxy/proxy.go` creates `msgTransportPool`
- message requests use `msgTransportPool.Get(...)` to obtain the upstream transport

The important precision is that the live implementation is keyed by affinity key, not by a naive raw conversation id, and subagents intentionally share their parent's transport.

So the correct forensic statement is:

- the idea was real in the raw research
- it did land in code
- but the implementation is specifically "per-affinity transport pooling for message requests," not a simplistic "every request gets its own socket"

## 43. The transport-isolation fix is path-dependent; it does not automatically apply on every deployment route

The present code also shows an important qualification that would be easy to miss in degraded summaries:

- on the normal direct message path, `/v1/messages` uses the per-affinity transport pool
- when `sidecarProxy` is enabled, the message path switches to `sidecarTransport` instead of the pool

That means any claim like "per-session upstream transports solved the Anthropic eviction problem" must be scoped carefully.

It may be true for the direct path.
It is not automatically the behavior for sidecar-routed traffic.

## 44. Per-session upstream transport isolation was already known in design by March 16, and it materially landed on March 25

The March 16 design note in [serializer-analysis.md](/home/user/glass-proxy/docs/2026-3-16/serializer-analysis.md) already says the old MITM rejection of per-session isolation was obsolete for Glass, because Glass canonicalizes system prompt bytes.

That means the timeline is now tighter:

- March 16: known in design
- March 25: implemented in code
- March 27: active on the current direct deployment path

This is backed by:

- the March 16 doc
- the March 25 birth time of [transport_pool.go](/home/user/glass-proxy/internal/proxy/transport_pool.go)
- current runtime logs showing direct-path transport-pool activity

## 45. The older identity-consistency lesson survived, but it survived as a multi-key architecture rather than as one universal conv id

The old system's failure was "different subsystems silently using different conversation identities."

The current system improves on that by sharing:

- `promptscope.Signature(...)`
- PID-aware fingerprinting
- ingress-time classification in `RequestMeta`

But it does not collapse everything to one key.

Instead it deliberately uses:

- `sessionKey`
- `requestKey`
- `affinityKey`
- `preGlassConvID`

That is not the old bug.
It is a more explicit architecture.

The new risk is semantic drift between valid keys, not one stale hash function lingering in a forgotten subsystem.

## 46. The older same-model-subagent lesson survived strongly and is more completely embodied in current code than in the older raw sessions

Current code now contains the full chain the older system was trying to protect:

- classify tool-bearing `small_system` and `agent_tool` requests as subagent traffic
- isolate them from the parent's Glass cache when appropriate
- disable Anthropic cache-entry creation for them
- strip `cache_control` again after Glass as a safety net
- serialize/gate them with parent awareness when PID mapping exists

This is one of the clearest places where the older lesson was not lost.

## 47. The most important remaining risk is no longer the exact old bug; it is PID-dependent fallback behavior at subsystem boundaries

The current code has several explicit fallback branches that are safe enough to exist but dangerous enough to audit further:

- unknown-PID or unmapped subagents bypass serializer batching
- streaming-path subagent blocking re-classifies without `HasEstablishedParent`
- SSE / telemetry fallback can drop PID and suffix information if request context is missing the conv id

So the present architecture is not "back to square one."
It is more like:

- the main lessons survived
- some defenses improved
- the remaining fragility moved to boundary conditions and fallback paths

## 48. Clearing `ClientPID` after capture is not the main replay hazard

Across the audited replay corpora, simply zeroing `ClientPID` while preserving the captured keys and captured subagent classification did not change replay behavior.

That means the dangerous branch is not "PID field absent somewhere in stored metadata."

The dangerous branch is earlier:

- ingress recomputation under missing PID
- ingress recomputation under missing parent memory

## 49. Parent-memory loss is behaviorally important because it suppresses `agent_tool` splits

The replay audit shows that `HasEstablishedParent` is not a cosmetic input.

When parent memory is removed during ingress recomputation:

- `agent_tool` detections disappear
- session lanes collapse
- the request stream reverts toward main-session treatment

This was visible in both audited corpora, and especially strongly in the March 7 current-state corpus where `22` `agent_tool` classifications disappeared.

## 50. PID loss can create a false-positive cache story

In at least one real replay corpus, removing PID increased the apparent cache-reuse percentage while making the total prompt mass much larger.

So "higher reuse %" under PID loss cannot be trusted as evidence that the fallback is healthy.

The real risk is merged lanes and erased isolation, not just lower cache hits on paper.

## 51. The PID-fallback replay pattern generalizes beyond one corpus

The targeted replay audit was not a one-off March 25 result.

Across:

- the March 25 capture corpus
- the March 7 current-state corpus
- the larger historical fixture corpus

the same three findings kept recurring:

1. clearing `ClientPID` after capture alone does little or nothing
2. removing parent memory erases `agent_tool` splits
3. removing PID during ingress recomputation collapses lanes and can create misleading cache metrics

That makes PID/parent-sensitive ingress fallback one of the strongest evidence-backed remaining vulnerabilities in the current architecture.

## 52. A first hardening pass landed and specifically removed weaker late-path recomputation

The first implementation pass did not try to solve every PID-sensitive branch.

What it did do was remove three concrete late-path degradations:

- streaming subagent logic now prefers the ingress / Glass classification
- streaming / SSE conv-id fallback now prefers context and `glass.ProcessResult` before pidless hashing
- serializer pre-Glass conv-id derivation now has an ingress-aware path and proxy ingress uses it

This means the architecture is now a little closer to the principle the audit kept pointing toward:

- compute the strongest request identity once at ingress
- reuse it downstream
- only fall back to weaker reconstruction as a last resort

## 53. The serializer's common unknown-PID fallback is no longer a full free bypass

After the second March 27 hardening pass, unknown-PID and unmapped subagents no longer fully bypass serializer coordination when a gate key is available.

They now use a weaker-but-safer fallback:

- do not join the global cross-session batch queue
- but do self-gate on their own serializer lane so repeated degraded subagent calls cannot interleave freely

This is not the same as full parent-aware serialization.

But it is materially safer than the previous all-passthrough branch and it preserves the reason the proxy avoided re-enabling broad global batching.
