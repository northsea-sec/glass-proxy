# Transcript Audit Ledger

Date: 2026-03-27
Owner: Codex forensic reconstruction pass
Status: Active working ledger. Append after each source read so findings survive context compaction.

## Working Rules

- Treat transcripts and `.md` reports as witness statements, not truth.
- Treat code, tests, logs, replay captures, config files, and raw DB/query outputs as stronger evidence.
- Preserve chronology first, interpretation second.
- Mark claims as `unverified`, `partially verified`, `verified`, `contradicted`, or `superseded`.

## Scope Decision

Primary raw-audit window: 2026-03-01 through 2026-03-27.

Why this window:

- It contains the visible PRIME -> DIAMOND -> GLASS -> cache-break -> back-to-research arcs in original order.
- It includes the first major repeated-degradation period, the March 6-7 collapse, the March 12 recovery artifacts, the March 14 PRIME-GOLDEN methodology period, and the March 25-26 Anthropic/context-editing collapse.
- Starting at March 14 would miss earlier damage and repeated rediscovery.
- Starting from proxy day zero is not necessary unless a March 1+ source explicitly depends on unresolved February behavior.

Extension policy:

- Extend backward into February raw local history only when the March 1+ chain explicitly references an older unresolved issue that materially affects causality.

## Source Inventory

### Tier 1: Raw transcript corpus

- `/home/user/nataraja` contains the March 1-12 PRIME/GLASS transcript family, including:
  - `text-PRIME-MESS.md`
  - `text-PRIME-SHIT.md`
  - `text-PRIME-GLASS*.md`
  - `TEXT-prime-glass-corrupted-codex.MD`
  - `text-PRIME-GLASS-DISASTER*.md`
  - `text-PRIME-GLASS-FACTS*.md`
  - `text-PRIME-GLASS-CHAPTERS.md`
- Raw local session history exists under `/home/user/.claude/context_history` back to `2026-02-01`.

### Tier 2: Forensic anchor artifacts

- `/home/user/.claude/glass.bad/*/shadow.md`
  - March 6 rollback/restart shadows
- `/home/user/.claude/glass-recovery-20260312-105741/*/{shadow.md,outbound_prefix_events.jsonl}`
  - March 12 recovery and prefix-event artifacts

### Tier 3: Later reconstructions and reports

- `/home/user/glass-proxy/docs/2026-03-07-glass-incident-synthesis.md`
- `/home/user/glass-proxy/docs/2026-3-14/PRIME_TEST_METHOD.md`
- `/home/user/glass-proxy/docs/2026-3-14/BURN_RATE_ROOT_CAUSE_AND_PRIME_GOLDEN_TESTS.md`
- `/home/user/glass-proxy/docs/2026-3-16/session-timelines.md`
- `/home/user/glass-proxy/analysis/2026-03-23-cache-break-root-cause-report.md`
- `/home/user/glass-proxy/text-GLASS*.md`
- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH*.md`
- `/home/user/glass-proxy/REPORT-2026-03-25.md`

## Read Log

### 1. `/home/user/glass-proxy/docs/2026-03-07-glass-incident-synthesis.md`

Type: later reconstruction / synthesis
Read on: 2026-03-27

Key extracted points:

- Frames March 6-7 as a chained incident, not one bug.
- Says early healthy phase separated unavoidable first-call cold start from avoidable warm-up slope and post-overflow prefix instability.
- Claims evening damage began before the famous `345708` collapse, with lanes `151639` and `152756` already in recreate loops around `21:03` on March 6.
- Claims rollback became a second incident because stale `.bak` sources were applied against newer architecture and restart proceeded with red tests.
- Claims `345708` had two failures:
  - persisted replay / invalid outbound structure
  - fresh re-eviction cycles after reset
- Strongly favors a one-way pinned hot frame after overflow instead of repeated micro-mutations of retained prefix.

Evidence status:

- `partially verified` as a guide document.
- It is useful as an index to raw sources, but not yet canonical until checked against raw March 6-7 transcripts, shadows, and code history.

### 2. `/home/user/glass-proxy/docs/2026-3-14/PRIME_TEST_METHOD.md`

Type: methodology and status report
Read on: 2026-03-27

Key extracted points:

- Explicitly states the PRIME-GOLDEN method:
  - hypothesis
  - baseline
  - simulation
  - metrics
  - comparison
  - verdict
- Describes an offline test framework rather than trust-me implementation.
- Reports a concrete March 14 operational state with strong cache efficiency but ongoing breaks under concurrency.
- Attributes many major breaks to concurrent session interleaving.
- Lists unresolved failures in three categories:
  - operational context rendering
  - process integration
  - pinned frame breakpoint stability

Evidence status:

- `verified` that this file preserves the PRIME-GOLDEN methodology and that the methodology is test-first.
- `partially verified` for the runtime conclusions until cross-checked against the tests, logs, and raw artifacts from that day.

### 3. `/home/user/glass-proxy/docs/2026-3-16/session-timelines.md`

Type: later compiled timeline from 12 transcripts
Read on: 2026-03-27

Key extracted points:

- Provides a chronological reconstruction for March 15-16.
- Preserves important root-cause notes:
  - recovery gate infinite loop caused by `CheckGateClear()` scanning `tool_result` for filename instead of matching assistant `tool_use` file path
  - serializer caused long waits by enforcing single concurrency to Anthropic
  - evict trigger at `300K` was unreachable because CC compacted earlier
- States compression / hybrid eviction work was designed and then implemented under a golden-test framework.

Evidence status:

- `partially verified`.
- Valuable secondary chronology, but still a reconstruction built from transcripts rather than raw logs alone.

### 4. `/home/user/nataraja/text-CODEX-DEGRADED.clean.md`

Type: raw transcript fragment / analysis session
Read on: 2026-03-27

What it appears to be:

- Not a simple narrative transcript. It includes execution logs, SQL-dump-driven acceptance analysis, exploratory reads, and patch attempts.
- Shows a session rerunning analysis from dumped artifacts and concluding:
  - near-subagent cache breaks were real but not dominant in the analyzed slice
  - inflight/interleaving pressure was dominant
  - Stage 2 late/missed intervention was a major factor
  - concurrent burn target failed on the examined dump

Notable methodological signal:

- This session at least attempted dump-only analysis from preserved artifacts rather than pure speculation.

Caution:

- The excerpt read starts midstream, so it is not yet sufficient as a stand-alone truth source.
- It shows patching activity in `context_trimmer.py`, which means claims from the same session need code/backup correlation.

Evidence status:

- `unverified` as a whole transcript.
- `partially verified` for the simple numerical conclusions once the named dump/report artifacts are checked.

### 5. `/home/user/nataraja/text-PRIME-MESS.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- The user explicitly calls out a prior agent for grepping without understanding and for producing fake forensic confidence.
- The transcript itself contains a later agent summarizing the system architecture and the failure chain:
  - shared bridge cross-session contamination
  - bad token estimates causing Stage 2 misfires
  - Stage 2 drops breaking `tool_use` / `tool_result` pairing
  - reconstruction in live mode killing prefix stability
  - serializer timeout behavior causing long waits
- The transcript emphasizes a rule that matches the present audit:
  - prior session conclusions are suspect until verified against source code and real artifacts.

Caution:

- This file also contains an agent re-summarizing prior work, so many of its claims are derivative rather than first-hand.
- It is still a useful meta-evidence source about the repeated failure mode: false comprehension, rediscovery loops, and acting on inherited assumptions.

Evidence status:

- `verified` for the process failure pattern:
  - user repeatedly had to re-explain
  - agent was caught grepping and overclaiming understanding
- `unverified` for specific technical claims inside the agent summary until tied back to source/code/artifacts.

### 6. `/home/user/nataraja/text-PRIME-SHIT.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- Captures a concrete harmful pattern:
  - an agent enabled `context_reconstruction_live = true` in `~/.claude/trimmer_config.json`
  - it did so before understanding the code path or why the setting existed
- The user directly objects to that blind config change.
- The same transcript later shows the session moving into monitoring and simple before/after DB arithmetic:
  - no Stage 2 fires in the observed post-enable window
  - average and max `cache_creation` appeared lower
  - the agent correctly limited its claim to observed metrics rather than causal proof

Why this matters:

- This is early direct evidence of one of the recurrent damage mechanisms:
  - inherited conclusion from a degraded prior session
  - blind config flip
  - temporary apparent metric improvement
  - no verified causal model

Evidence status:

- `verified` that the blind config flip occurred and was considered unacceptable by the user.
- `partially verified` that the monitored window looked numerically better.
- `unverified` that reconstruction live was actually the cause of the improvement.

## Current Direction

Next transcript reads should prioritize:

1. Early March 1-2 raw PRIME/DIAMOND files to establish the first repeated bug loops.
2. March 6-7 first-collapse raw files:
   - `text-PRIME-GLASS-SHIT.md`
   - `TEXT-prime-glass-corrupted-codex.MD`
   - `text-PRIME-GLASS-CORRUPTED.md`
   - `text-PRIME-GLASS-DISASTER-1.md`
   - `text-PRIME-GLASS-DISASTER-2.md`
3. March 6 shadow artifacts under `/home/user/.claude/glass.bad`.
4. March 12 recovery shadows and prefix events.
5. March 14 PRIME-GOLDEN technical proofs and corresponding tests.

### 7. `/home/user/nataraja/text-PRIME-GLASS-SHIT.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- Shows a March 6 session being analyzed from `glass_debug.db`.
- The observed morning March 6 run was genuinely healthy in the narrow sense:
  - single session
  - no evictions
  - no orphans
  - weighted efficiency around `91.9%`
  - context grew from about `23K` to `69K`
- The transcript then captures an important reasoning correction:
  - an agent initially claimed yesterday's LocalCache persistence fix should prevent today's cold start
  - the user correctly pointed out this was a brand-new March 6 session
  - the agent then discovered persistence was keyed by conversation ID, so it only helped restart-within-same-conversation, not the common case of a fresh new session

Why this matters:

- This is direct evidence that one so-called fix solved a narrower problem than the user actually needed.
- It also shows a recurring anti-pattern:
  - healthy narrow metric window
  - overgeneralized conclusion
  - user correction
  - later realization that the fix addressed the wrong scope

Evidence status:

- `verified` that the transcript explicitly distinguishes:
  - same-conversation restart recovery
  - new-conversation cold starts
- `partially verified` that the morning March 6 window was healthy, pending direct DB/log correlation.

### 8. `/home/user/nataraja/TEXT-prime-glass-corrupted-codex.MD`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- Captures a live March 6 evening repair attempt around the `151639` / `152756` lanes.
- The agent claims:
  - atomic eviction plus alternation guard had been patched
  - remaining invalidity came from legacy-poisoned on-disk snapshots
  - a one-time cache repair on load was needed to co-evict orphaned halves of broken tool exchanges
- The transcript shows actual code edits in `internal/glass/localcache.go` and a new targeted test:
  - `TestRepairBrokenToolBoundariesMarksLegacyOrphansAsReferences`
- Crucially, the same transcript also shows the focused suite failing during this repair cycle.

Why this matters:

- It is direct evidence of live surgery on an already-poisoned session state, not a clean controlled fix.
- It supports the March 7 synthesis claim that rollback/restart/recovery work itself became part of the incident chain.
- It shows that "patched and live" claims from degraded sessions cannot be trusted without checking whether the corresponding tests actually passed and whether the binary rolled from a green state.

Evidence status:

- `verified` that the transcript contains:
  - live rebuild/restart steps
  - direct edits to `localcache.go`
  - a targeted repair-test addition
  - a focused test failure during the same cycle
- `unverified` that the legacy-poisoned-snapshot explanation was the complete root cause.

### 9. March 6 rollback / restart shadow artifacts under `/home/user/.claude/glass.bad`

Artifacts read on: 2026-03-27

Files examined:

- `/home/user/.claude/glass.bad/2719b7a469d9_151639.rollback-20260306-212317/shadow.md`
- `/home/user/.claude/glass.bad/2719b7a469d9_152756.rollback-20260306-212317/shadow.md`
- `/home/user/.claude/glass.bad/2719b7a469d9_152756.restart-20260306-213543/shadow.md`
- `/home/user/.claude/glass.bad/2719b7a469d9_35479.rollback-20260306-212317/shadow.md`
- `/home/user/.claude/glass.bad/2719b7a469d9_51689.rollback-20260306-212317/shadow.md`

What these artifacts prove:

- These are real on-disk shadow histories for specific conversation IDs, not later narrative reports.
- The March 6 collapse period really did involve repeated evictions in the affected lanes.
- The cadence itself is significant:
  - `151639` shows 9 evicted batches from `21:02:50` to `21:06:28`, many of them `~0K` to `~2K`
  - `152756` shows 23 rollback-shadow batches from `20:52:38` to `21:14:18`, then 2 restart-shadow batches at `21:24:54` and `21:25:02`
- The shadow histories span unrelated user tasks, which means the eviction/recovery mechanism was acting on ordinary live sessions, not only on deliberately constructed proxy tests.

Important limitations:

- Many message bodies are blank or only partially preserved, so these files are strong for chronology and eviction cadence but weak for detailed semantic reconstruction.
- They do not, by themselves, prove *why* the evictions happened or whether each eviction was valid.

Why this matters:

- It materially supports the March 7 synthesis claim that the affected lanes were already churning before later retellings simplified the incident.
- The tiny repeated batch sizes are consistent with a broken or thrashing eviction regime rather than a calm long-session archival flow.

Evidence status:

- `verified` for:
  - existence of the rollback/restart shadow mechanism
  - exact batch timings
  - high-frequency repeated eviction on the affected March 6 lanes
- `unverified` for any single-cause explanation of those evictions.

### 10. March 12 recovery shadows and outbound prefix events under `/home/user/.claude/glass-recovery-20260312-105741`

Artifacts read on: 2026-03-27

Files examined:

- `/home/user/.claude/glass-recovery-20260312-105741/6be761d4747a_5644/shadow.md`
- `/home/user/.claude/glass-recovery-20260312-105741/6be761d4747a_5644/outbound_prefix_events.jsonl`
- `/home/user/.claude/glass-recovery-20260312-105741/fedbabf897a7_10279/shadow.md`
- `/home/user/.claude/glass-recovery-20260312-105741/fedbabf897a7_10279/outbound_prefix_events.jsonl`

Key extracted points:

- The March 12 recovery artifacts are not limited to proxy debugging content; the shadow files preserve ordinary user work on other projects.
- This is useful evidence that the recovery/shadow path was being used in real sessions and not just in synthetic tests.
- The outbound prefix-event logs are especially valuable because they record structural state, not just prose:
  - `change_kind`
  - `divergence`
  - `system_changed`
  - `snapshot.Hash`
  - `SystemHash`
  - `MessageHash`
  - `Anchor`
  - `TailHash`
  - `reference_insert_at`
  - `injected_references`
  - `evicted_count`
  - `batch_count`
- The logs show periods where the main prefix hash remains stable while tail hashes evolve, which is exactly the kind of evidence needed for a prefix-stability methodology.
- They also show later reference injection and nonzero eviction counts, proving the instrumentation was explicitly tracking evicted-history reinsertion behavior.

Why this matters:

- This is strong instrumentation evidence that by March 12 the system had a measurable notion of prefix stability, divergence classification, and reference injection.
- It gives us an artifact trail we can compare to the later PRIME-GOLDEN methodology claims instead of trusting prose alone.

Important limitation:

- These March 12 artifacts do not, by themselves, establish that the recovery design was *correct*.
- They establish observability and active use of the mechanism, not successful root-cause closure.

Evidence status:

- `verified` that:
  - shadow/recovery artifacts were active in real sessions on March 12
  - outbound prefix events logged structural cache/prefix metrics with reference-injection metadata
- `partially verified` that this reflects a mature prefix-stability methodology; full verification still requires matching these artifacts to code/tests and the later PRIME-GOLDEN docs.

### 11. `/home/user/nataraja/text-PRIME-GLASS-CORRUPTED.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- Captures a disastrous rollback attempt after the system was already in a bad state.
- The proposed rollback plan explicitly used:
  - `.bak` copies of `localcache.go` and `process.go`
  - manual edits to `reference.go` and `request_validation.go`
  - moving active session cache directories aside under `/home/user/.claude/glass.bad`
- The build then failed because `process.go.bak` was from an older architecture and no longer matched the current engine.
- The transcript ends with the agent itself reporting the source tree is mixed and non-clean.
- The user specifically objects that snippet-patch backups should have been used rather than stale whole-file backups.

Why this matters:

- This is direct evidence that rollback was not just a theory; it was attempted live and it materially worsened source integrity.
- It strongly corroborates the March 7 synthesis claim that stale rollback sources became a second incident.
- It also explains why the `glass.bad` session directories exist: they were created as part of a live rescue/rollback path, not as a tidy archival routine.

Evidence status:

- `verified` that:
  - stale `.bak` rollback was attempted
  - session directories were moved aside
  - the rollback did not compile cleanly
  - the repo ended in a mixed state according to the transcript itself
- `verified` that the user explicitly identified the backup method as wrong.

### 12. `/home/user/nataraja/text-PRIME-GLASS-DISASTER-1.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- Focuses on lane `345708` and an invalid outbound request path around reference injection.
- The working hypothesis in this session:
  - reference injection was creating an assistant-final outbound shape when user-final was required
  - this needed a boundary fix in `injectReferenceMessages(...)`
- The transcript shows:
  - code changes in `reference.go` / `process.go`
  - new targeted regression coverage in `split_id_test.go`
  - an initial failing test due to over-hardcoded expected insertion point
  - then passing focused and broader test runs
  - a live proxy rebuild/restart after tests turned green
- The session context compacted midstream while the live rollout was still in progress.

Why this matters:

- This is a methodologically better phase than the rollback transcript:
  - concrete failing lane
  - targeted hypothesis
  - tests written
  - fix iterated until green
- But it still includes live deployment into a fragile system before the larger incident was stabilized.

Evidence status:

- `verified` that:
  - the session centered on the invalid user-final / reference-injection path
  - targeted tests were added and eventually passed
  - the proxy was rebuilt and restarted live afterward
- `partially verified` that this fix addressed a real bug; later transcripts show it was not sufficient to restore stability.

### 13. `/home/user/nataraja/text-PRIME-GLASS-DISASTER-2.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- Shows the next hypothesis shift after the prior fix:
  - the catastrophic lane was now framed as a persisted evicted-cache snapshot reload problem, not just invalid outbound structure
  - lane `345708` is singled out as resuming from disk with many reference placeholders and burning huge `cache_creation`
- The agent patches the code again:
  - adds `loadedFromDisk` tracking to `LocalCache`
  - adds logic to treat persisted ref-bearing snapshots as unsafe to resume
  - adds a targeted test around `ensureCacheStateCompatible`
- The tests pass and the proxy is rebuilt/restarted live.
- Live post-restart data still shows damage:
  - the old `~140k create / 10052 read` loop changes form but does not disappear
  - the user explicitly points out that every new fix is causing more burn and more churn

Why this matters:

- This is direct evidence of hypothesis drift under pressure:
  - from invalid outbound request
  - to persisted poisoned snapshot reload
- It also directly supports the user's description of the failure pattern:
  - a plausible new root cause is found
  - a live fix is applied
  - apparent improvement appears in a narrow metric
  - overall burn/churn still increases

Evidence status:

- `verified` that:
  - a second major live hypothesis shift happened immediately after the prior fix
  - more live code changes and a restart were made
  - the post-fix lane remained unhealthy
  - the user objected that the interventions were causing further damage
- `unverified` whether the persisted-snapshot hypothesis was fully wrong, partially right, or one layer of a deeper problem.

### 14. `/home/user/nataraja/text-PRIME-GLASS-POST-DISASTER.md`

Type: later retrospective / reconstruction
Read on: 2026-03-27

Key extracted points:

- This file is not a raw incident transcript. It is a more disciplined post-disaster reconstruction that cites earlier transcripts, DB facts, snippet backup history, and rollback artifacts.
- It explicitly externalizes the chain model:
  1. real proxy/cache/session issues
  2. live intervention on active sessions
  3. restart-driven churn
  4. wrong rollback source
  5. mixed source state
  6. context compaction before the correct model was made durable
- It preserves several anti-drift / anti-compaction rules that are highly aligned with the user's present concerns:
  - DB and runtime evidence first
  - no live restart unless rollback target is explicit and verified
  - no whole-file rollback from stale backups
  - keep a written source-of-truth note that survives compaction
- It also records concrete DB timings for:
  - early `151639` / `152756` damage
  - rollback as a second damage phase
  - the later `345708` collapse

Why this matters:

- This is one of the first documents that tries to freeze the working model outside the agent’s volatile context.
- It appears to be an attempt to solve exactly the meta-problem the user is describing: losing the real causal chain once compaction hits.

Evidence status:

- `partially verified` overall, because it is a reconstruction rather than a raw source.
- `verified` that it explicitly codifies the anti-compaction / anti-drift rule set and the multi-stage incident chain.
- `partially verified` that its cited DB/transcript claims are accurate; many align with artifacts already checked.

### 15. `/home/user/nataraja/text-PRIME-GLASS-RECOVERY.md`

Type: raw transcript with mixed recovery work
Read on: 2026-03-27

Key extracted points:

- Despite the name, this is not a clean single-topic recovery transcript.
- It opens on a zlib / SSE / `Content-Encoding` issue:
  - proxy decompresses upstream gzip SSE
  - client may still see mismatched headers/body
  - this causes zlib/decompression errors and later garbled binary output
- The session then performs web research on Claude Code version changes from `2.1.37` to `2.1.71` and infers possible impacts on Glass, fingerprinting, cache prefix shape, beta headers, and request format.
- Later in the same transcript it comes back to Glass-specific failures:
  - orphan `tool_result` problems after eviction
  - validation bypass forwarding broken requests
  - repair only running on disk load rather than immediately after eviction
  - evictions triggered by config thresholds lower than Claude Code’s own compaction point
- The session again includes live patching and direct intervention.

Why this matters:

- This file is strong evidence of topic drift after the disaster:
  - transport/header/zlib issues
  - client version changes
  - cache prefix changes
  - orphan tool boundaries
  - eviction-threshold policy
  all become entangled in one recovery stream
- It also shows the recurring pattern of one fix causing a new problem:
  - stripping `Content-Encoding` fixed one path but broke error-body handling and produced garbled binary output

Evidence status:

- `verified` that:
  - March 7 recovery work mixed transport-layer bugs, client-version-change analysis, and Glass eviction bugs in one stream
  - the session documented another clear “fix introduced another issue” cycle
  - the transcript explicitly identifies post-eviction orphan repair as a distinct problem
- `unverified` for the external web-research conclusions about Claude Code version impacts until matched to primary release sources.

### 16. March 14 PRIME-GOLDEN methodology docs plus executable test suites

Artifacts read on: 2026-03-27

Files examined:

- `/home/user/glass-proxy/docs/2026-3-14/PRIME_TEST_METHOD.md`
- `/home/user/glass-proxy/docs/2026-3-14/BURN_RATE_ROOT_CAUSE_AND_PRIME_GOLDEN_TESTS.md`
- `/home/user/glass-proxy/internal/glass/prime_golden_test.go`
- `/home/user/glass-proxy/internal/glass/compression_golden_test.go`

What is now confirmed:

- The PRIME-GOLDEN methodology survived in both prose and code.
- The March 14 docs explicitly define the method as:
  - hypothesis
  - baseline
  - simulation
  - metrics
  - comparison
  - verdict
- `prime_golden_test.go` is a real named suite, not a note about future work.
- `compression_golden_test.go` is a larger follow-on suite using the same methodology for hybrid compression and saturation-flush behavior.

Executable PRIME-GOLDEN tests confirmed in code:

- In `prime_golden_test.go`:
  - `TestPrimeGolden_BookmarkRoleCollision`
  - `TestPrimeGolden_PrefixStabilityUnderEviction`
  - `TestPrimeGolden_ConcurrentInterleavingRisk`
  - `TestPrimeGolden_OrphanSanitizerBreakpointDrift`
  - `TestPrimeGolden_FactOverlayCacheSafety`
  - `TestPrimeGolden_EvictedSessionViewSizeBound`
  - `TestPrimeGolden_ReingestBlocking`
  - `TestPrimeGolden_BookmarkOnlyAnchor`
- In `compression_golden_test.go`, the suite expands into:
  - selective stripping
  - idempotent compression
  - summary injection structure validity
  - saturation detection
  - multi-cycle endurance
  - watermark prefix stability
  - process-level compression integration
  - full lifecycle without file recovery
  - tests explicitly proving that compression inside the prefix causes breaks
  - eager/clamped compression tradeoff tests

Why this matters:

- This is the strongest evidence so far that there really was a rigorous, test-first phase rather than only confident agent narration.
- It also reveals an important nuance:
  - the original PRIME-GOLDEN suite was about cache correctness, eviction safety, and prefix stability
  - the methodology later expanded into a larger compression program
  - that larger program itself contains tests warning that compression inside the prefix causes breaks

Evidence status:

- `verified` that the methodology existed as executable tests in the current repo.
- `verified` that the March 14 docs and the Go tests share the same structure and intent.
- `partially verified` that every claim in the March 14 burn-rate doc is correct; the methodology is real, but the runtime conclusions still need per-claim corroboration.

### 17. `/home/user/glass-proxy/text-GLASS-18.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- This session investigates why Glass eviction was not firing.
- The important conclusion is not the first guess but the corrected one:
  - `evict_trigger_tokens=300K` made Glass effectively a spectator
  - observed real API input stayed in the ~`200K-232K` range
  - Glass therefore never reached its own trigger
- The session explicitly recommends lowering the trigger to about `180K-190K`.
- It also isolates the `174,458` cache-create event:
  - not as CC compaction
  - but as Glass’ own `maybeAdvanceBreakpoint()` moving the cache marker from `msg[193]` to `msg[201]`
  - causing a one-time cache invalidation with immediate recovery on the next request
- The same session also diagnoses two other critical facts:
  - serializer single-concurrency was causing long waits across sessions
  - `CheckGateClear()` was looking in the wrong place and needed to scan assistant `tool_use` file paths

Why this matters:

- This transcript is one of the clearest raw sessions where a superficial explanation is corrected by deeper investigation in the same session.
- It also anchors several of the claims later repeated in summary docs:
  - unreachable 300K trigger
  - serializer bottleneck
  - gate-clear bug
  - breakpoint advancement as an intentional but costly break source

Evidence status:

- `verified` that the transcript itself lands on:
  - 300K trigger unreachable in practice
  - breakpoint advancement as the direct cause of the 174K cache break
  - serializer contention and gate-clear bug as separate issues
- `partially verified` that the recommended `180K-190K` range is optimal; that still needs comparative testing.

### 18. `/home/user/glass-proxy/text-GLASS-19.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- Starts by finalizing the real-world `tool_use` path fix for the gate-clear bug and deploying it.
- Then pivots into the MITM-style rolling-eviction question and performs a more conceptually mature design analysis.
- The transcript clearly captures two stages of compression thinking:
  1. initial enthusiasm for gradual in-context compression as a “gradient, not cliff”
  2. the user’s challenge that this would cause slow reasoning death as the context fills with degraded summaries
- The session then evolves toward a hybrid model:
  - selective stripping / compression while preserving structure
  - watermark-batched stability
  - saturation detection
  - a clean flush summary rather than endless degraded accumulation
- It explicitly identifies prerequisites and constraints:
  - idempotent compression
  - bucketed watermarks
  - independent tools breakpoint (`BP2`)
  - current chapter/recovery as the backstop, not the primary mechanism
- The transcript also shows actual implementation work:
  - creation of `internal/glass/compression.go`
  - creation of `internal/glass/compression_golden_test.go`
  - iterative compile/test repair using snippet-backed edits

Why this matters:

- This is the clearest raw bridge between the PRIME-GOLDEN methodology and the later compression golden suite.
- It also preserves a crucial design correction initiated by the user:
  - pure gradual degradation was rejected as insufficient
  - the hybrid compression + saturation-flush approach became the more serious plan

Evidence status:

- `verified` that:
  - the gate-clear fix was finalized in this session
  - the MITM-inspired compression debate happened in raw form
  - the user explicitly raised the reasoning-decline objection
  - the session implemented `compression.go` and `compression_golden_test.go`
- `partially verified` that all of the proposed compression architecture is correct in practice; the tests exist, but the runtime tradeoffs still need corroboration.

### 19. `/home/user/glass-proxy/REPORT-2026-03-25.md`

Type: later session report / implementation summary
Read on: 2026-03-27

Key extracted points:

- This report is the clearest direct source for the three-mode design the user later referenced.
- It claims three cache-management modes were implemented:
  - `"full"`
  - `"off"`
  - `"context_api"`
- It states the compression system was rewritten from eager watermark creep to batched watermark advancement, importing the nataraja design.
- It also describes an experimental deep-stable watermark breakpoint variant and names the binaries built around these strategies.
- Crucially, the same report still lists substantial outstanding work:
  - live baseline testing for `"off"` mode
  - live testing of `"context_api"` mode
  - live comparison of batched `"full"` mode
  - comparison of watermark breakpoint vs prev-anchor breakpoint

Why this matters:

- It explains how the current three-mode architecture emerged.
- It also shows the exact overclaim pattern the user warned about:
  - the summary says “all work verified against live production data”
  - but the later “Outstanding Work” section proves key mode-by-mode validation was still missing

Evidence status:

- `verified` that the report documents the origin of the three-mode system.
- `verified` that the report itself contains unresolved live-testing gaps despite its confident summary.
- `partially verified` for the claimed success of the batched redesign until checked against raw March 25 production evidence.

### 20. March 25 raw research notes (`text-GLASS-BACK2RESEARCH-*`, `text-GLASS-RESEARCH-*`, `text-GLASS-CACHE_BREAK-*`)

Artifacts sampled on: 2026-03-27

Files sampled:

- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-2.md`
- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-4.md`
- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-6.md`
- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-7.md`
- `/home/user/glass-proxy/text-GLASS-RESEARCH-1.md`
- `/home/user/glass-proxy/text-GLASS-CACHE_BREAK-4.md`

Key extracted points:

- These raw March 25 research files explicitly criticize the synthetic golden-test blind spot:
  - single-loop test models do not capture inter-request cache gaps
  - production divergence must be replayed from real captured sequences, not inferred from synthetic unit scenarios alone
- They also explicitly discuss missing observability:
  - some state, such as watermark position / anchor interaction, was not fully present in DB telemetry
  - restart gaps and persistence mismatches were already being called out
- `text-GLASS-BACK2RESEARCH-6.md` shows `ser_enabled: false` in the live config and then verifies that the serializer was effectively disabled except for the subagent gate.
- `text-GLASS-BACK2RESEARCH-7.md` records the existence of large replay/fixture corpora:
  - 947 captured fixtures in `~/.claude/glass-replay-fixtures/`
  - 115 captured fixtures in `~/.claude/glass-replay-current/`
  - full `body_raw`, `body_pre_glass`, `body_final`, metadata, betas, and glass result
- `text-GLASS-CACHE_BREAK-4.md` also captures active concern about Anthropic beta propagation and shows `ser_enabled: false` in the config snapshot.

Why this matters:

- These notes are a bridge between the earlier “golden methodology” and the later forensic skepticism about synthetic tests.
- They also directly foreshadow two issues that matter to the present audit:
  - config/control-plane drift around serializer settings
  - the need to use real replay fixtures rather than trusting synthetic proofs

Evidence status:

- `verified` that March 25 raw research already documented:
  - the synthetic golden-test blind spot
  - live serializer-disabled config state via `ser_enabled: false`
  - the existence of substantial replay-fixture evidence
- `partially verified` that every raw conclusion in those files is correct; the main value here is that the concerns themselves were already explicitly known.

### 21. `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-10.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- This is the clearest raw implementation session for the three cache-context-management modes later referenced by the user.
- The user explicitly asks for three toggles:
  - Anthropic Context Editing API mode
  - everything off / no context management
  - the full Glass cache-context-management suite
- The session shows a first aborted attempt at a narrower Context Editing implementation, then a restart into the broader three-mode design.
- The transcript records concrete code changes, not just discussion:
  - `ContextCacheMode` and Context API config fields added to `GlassConfig`
  - cache-mode constants/helper added in `session.go`
  - `ContextEditingBeta` added in `cachecontrol.go`
  - `ProcessResult.ContextCacheMode` recorded in the Glass pipeline
  - `process.go` updated to skip message breakpoints in `"off"` and `"context_api"`
  - `process.go` updated to inject `context_management` edits in `"context_api"`
  - proxy layer updated to preserve/add the Anthropic beta header
  - new `context_cache_mode_test.go` added
- The transcript claims focused three-mode tests passed and then `go test ./internal/glass/` passed as well.

Why this matters:

- This is the strongest raw source for when the three-mode idea moved from design discussion into concrete code.
- It also shows the justification state at the time of implementation:
  - the feature is being built while the working belief is still that anchor movement is the main break source and that client-side compression may not be the culprit

Evidence status:

- `verified` in the current repo that the three-mode code survives:
  - `internal/glass/session.go`
  - `internal/glass/cachecontrol.go`
  - `internal/glass/process.go`
  - `internal/proxy/proxy.go`
  - `internal/glass/context_cache_mode_test.go`
- `verified` that the current repo still contains the exact structural pieces the transcript describes:
  - `"full"`, `"off"`, `"context_api"`
  - Anthropic `context_management` injection
  - Anthropic beta propagation via `ContextEditingBeta`
  - per-request `ContextCacheMode` tracking
- `partially verified` that the session's local test success meant production readiness; the raw session only proves local implementation and local tests, not live mode-by-mode validation.

### 22. `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-11.md`

Type: raw transcript continuation
Read on: 2026-03-27

Key extracted points:

- This continuation is important because it shows the three-mode work was not a hand-wavy claim; the session is still repairing and hardening the test harness.
- The transcript adds `result.ContextCacheMode = e.cfg.CacheMode()` to `ProcessResult`.
- It discovers that the initial tests were being misled by subagent classification, not by the cache-mode logic itself.
- The fix was to create a helper with a deliberately long system prompt so the tests represent a main session rather than a subagent path.
- The transcript then reports:
  - the focused cache-mode tests pass
  - the full `./internal/glass` test suite passes
  - logs show the intended mode-specific behaviors

Why this matters:

- This is the strongest evidence that the implementation stage included real local verification and at least one nontrivial test-correction step.
- It also reveals a broader audit lesson:
  - apparently "failing" or misleading tests in this codebase can come from adjacent machinery like subagent classification, not only from the feature under test

Evidence status:

- `verified` in the current repo that:
  - `ProcessResult` contains `ContextCacheMode`
  - `context_cache_mode_test.go` contains the long-system-prompt helper used to avoid subagent misclassification
- `verified` that the current test file structurally matches the transcript's described repair.
- `partially verified` that the test logs quoted in the transcript were sufficient proof of correctness; they are still local tests, not live replay validation.

### 23. `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-12.md`

Type: raw transcript / contradiction pivot
Read on: 2026-03-27

Key extracted points:

- This transcript is critical because it reverses the earlier same-day confidence.
- It explicitly states that the earlier "verified facts" from `BACK2RESEARCH-8/9` were wrong.
- The replacement claim is much harsher:
  - compression **does** cause cache breaks in live conversations
  - the inter-request gap is structural
  - the observed break rate is a designed steady-state cost, not a bug that disappears with the current client-side approach
  - `"off"` mode and `"context_api"` mode are the remaining approaches that had not yet been tested live
- The user then redirects the investigation toward the older nataraja MITM proxy and the `insights2/` corpus.
- This means the same March 25-26 chain contains both:
  - a confident local implementation of three modes
  - a later same-night admission that the earlier causal story was wrong and the alternatives were still unproven live

Why this matters:

- This is the clearest raw proof that the March 25 record is internally contradictory, not just incomplete.
- It shows the confidence gap opened *before* the final report, not only in hindsight after the fact.
- It also explains why the user views the report as degraded:
  - the narrative had already started to shift underneath it

Evidence status:

- `verified` that the transcript explicitly reverses the earlier same-day claims.
- `verified` that the transcript points to the nataraja corpus as the next evidentiary source.
- `partially verified` that the specific "compression does cause cache breaks" quantitative framing is fully established for Glass itself; the claim is plausible and supported by cited live-session reasoning, but still needs replay-level corroboration in the Glass artifacts.

### 24. March 25 nataraja-import research chain (`text-GLASS-RESEARCH-2*.md`)

Artifacts sampled on: 2026-03-27

Files sampled:

- `/home/user/glass-proxy/text-GLASS-RESEARCH-2.md`
- `/home/user/glass-proxy/text-GLASS-RESEARCH-2-1.md`
- `/home/user/glass-proxy/text-GLASS-RESEARCH-2-2.md`

Key extracted points:

- These files capture a broad research sweep over the nataraja `claude-route-inspector` docs and `insights2/` corpus.
- Chronologically, they predate the raw three-mode implementation session on the same date:
  - `RESEARCH-2*` around `22:10-22:24`
  - `BACK2RESEARCH-10` at `23:36`
- The research chain imports several real themes from the older nataraja work:
  - prompt-caching byte-stability constraints
  - watermark-bounded trimming
  - context editing beta docs
  - multiple historical root-cause candidates such as system-reminder churn, conv-id collision, subagent prefix eviction, and cache-control removal
- But these files are not primary evidence themselves.
- They also display degraded-agent behavior:
  - failed or stuck subagent runs
  - repeated "read everything" sweeps
  - later mega-summaries built from mixed primary and secondary material

Why this matters:

- It explains how nataraja design knowledge entered the Glass investigation on March 25.
- It also tells us to rank these files below the original nataraja docs and below raw Glass transcripts.

Evidence status:

- `verified` that these files exist and document the import of nataraja analysis into the March 25 Glass investigation.
- `verified` by file timestamps that this research preceded the three-mode implementation sessions that same night.
- `partially verified` for any specific quantitative or historical claim inside these summaries unless traced back to the original nataraja source documents.

### 25. `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-8.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- This session moves from diagnosis into methodology planning.
- It explicitly asks for an action plan based on testing tiers and starts by checking whether the fixture corpus is sufficient for replay-based verification.
- The transcript inventories three replay corpora:
  - March 25 current capture
  - March 6 older fixtures
  - March 7 current/transition fixtures
- It also cross-checks the replay window against `glass_debug.db` and uses the result to argue that the March 25 fixture set is bad enough to serve as present-day ground truth for a regression investigation.
- A smaller but still useful side finding is recorded first:
  - the non-streaming request path is a real code path
  - but there was zero evidence of recent non-streaming Claude Code traffic
  - so that bug existed in code without being an active driver of current behavior

Why this matters:

- This is where the March 25 investigation explicitly re-anchors itself in replay methodology instead of only prose reasoning.
- It is also the clearest raw source for the "do we have enough fixtures?" question the user later asked me to recover.

Evidence status:

- `verified` that the older corpus counts still match the transcript:
  - `glass-replay-fixtures`: `947`
  - `glass-replay-current`: `115`
- `partially verified` for the transcript's March 25 fixture count snapshot.
  - The raw transcript reported `128` fixtures / `4` sessions at that moment.
  - On 2026-03-27, the same directory contains `268` valid JSON fixtures with complete `body_raw` and `body_final`, spanning `12` session keys / `9` affinity keys and including `agent_tool` calls.
  - This means the transcript count was a time-local snapshot, not a stable eternal fact.
- `verified` that the DB window check from the transcript still reproduces:
  - `113` requests
  - `3` conversations
  - `20.7` average cache efficiency
  - `46` large breaks

### 26. `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-9.md`

Type: raw transcript
Read on: 2026-03-27

Key extracted points:

- This is one of the most important methodology-recovery transcripts in the whole March 25 chain.
- It explicitly calls out prior methodological failure:
  - snippet backup diffs were skipped
  - config-variant replays were declared "done" without doing the mechanical build/replay work
  - interleaved replay was abandoned based on aggregate DB reasoning instead of real replay
- The transcript then restates a corrected tiered plan:
  1. snippet backup diffs
  2. threshold-variant replays
  3. compression-off replay
  4. interleaved replay
- It records execution of those tasks and cites concrete output files under `analysis/`.
- The most important reported findings are:
  - March 18 "golden" stability was not about breakpoint thresholds at all; it was mainly OpenAI-lane support work
  - March 24 regression really did include `breakpointAdvanceThreshold = 8 -> 80` in the snippet backup chain
  - `RawPrevBreakpointAnchor` was added later, not as part of the original March 24 regression
  - threshold replay on the March 25 capture produced a wide spread:
    - `4 -> 53.0% reuse`
    - `8 -> 47.6% reuse`
    - `16 -> 82.2% reuse`
    - `40 -> 69.6% reuse`
- The transcript also implies compression-off and interleaved replay results were generated as part of the same methodological completion pass.

Why this matters:

- This transcript is a direct ancestor of the "recover the real methodology" request the user later made.
- It is not just another theory session.
- It explicitly reasserts the methodological rules:
  - use snippet backups as de facto version control
  - run variant binaries when tooling lacks flags
  - replay actual captured sequences rather than hand-waving from aggregate summaries

Evidence status:

- `verified` that the cited analysis artifacts survive today:
  - `/home/user/glass-proxy/analysis/snippet_backup_diffs.txt`
  - `/home/user/glass-proxy/analysis/threshold_replay_comparison.txt`
  - `/home/user/glass-proxy/analysis/compression_off_replay.txt`
  - `/home/user/glass-proxy/analysis/interleaved_replay_results.txt`
- `verified` that the surviving files match the transcript's reported conclusions:
  - `snippet_backup_diffs.txt` documents:
    - March 18 was OpenAI-lane work
    - March 24 included `8 -> 80`
    - `RawPrevBreakpointAnchor` was added later
  - `threshold_replay_comparison.txt` documents the `4 / 8 / 16 / 40` replay outputs and reuse percentages
  - `compression_off_replay.txt` documents the compression-on vs compression-off replay delta
  - `interleaved_replay_results.txt` documents equal aggregate stability for interleaved vs sequential replay on that captured set
- `partially verified` that every interpretation in the transcript is the final truth.
  - What is solid is that the tiered methodology was restated and materially executed.
  - What still needs care is how much weight to assign to those replay outputs versus live production behavior.

### 27. March 24 cache-break chain (`text-GLASS-CACHE_BREAK*.md`)

Artifacts sampled on: 2026-03-27

Files sampled:

- `/home/user/glass-proxy/text-GLASS-CACHE_BREAK.md`
- `/home/user/glass-proxy/text-GLASS-CACHE_BREAK-1.md`
- `/home/user/glass-proxy/text-GLASS-CACHE_BREAK-2.md`
- `/home/user/glass-proxy/text-GLASS-CACHE_BREAK-3.md`

Key extracted points:

- This chain captures the transition from a March 23 report-level concurrency theory into a more complicated live March 24 diagnosis.
- The opening corrected summary blames two March 19 code changes:
  - classifier change removing the old per-subagent bypass/canonical split
  - serializer change that allowed faster switching when only subagents were in flight
- The user then asks for live replication with multiple concurrent sessions.
- During that live replication, the transcript makes a real observational error:
  - the SQL/window query used the wrong timezone basis
  - so old conversations were temporarily mistaken for active ones
  - the mistake is then explicitly corrected in the same transcript
- Once the monitoring is corrected, the live observations become more nuanced:
  - same-conversation compression/watermark resets are already causing catastrophic rebuilds
  - adding `small_system` bursts creates zero-hit storms and visible burn-rate spikes
  - cross-conversation subagent thrash is real, but it is not the only thing happening
- The later hypothesis section is especially important because it ranks outcomes:
  - `H1` small_system creates competing cache entries: confirmed
  - `H2` small_system evicts other sessions: partially confirmed
  - `H3` agent_tool cross-conversation thrashing: confirmed, but with fewer observed events
  - `H4` "conversation switches are worse than same-conversation behavior": inverted on that day because same-conversation compression resets were worse
- The chain then pivots into prior-art recovery:
  - the corpus of mitmproxy postmortems already contained mitigations for these same classes of problems
  - the March 14 Glass doc had already named immediate mitigations such as stripping `cache_control` from subagent requests and holding subagent requests behind the main session

Why this matters:

- This is the best raw bridge between the March 23 cache-thrash narrative and the March 25 three-mode redesign.
- It also demonstrates why later summaries are dangerous when read alone:
  - the raw chain contains a real query bug, a live correction, a root-cause shift, and a re-import of earlier mitigations

Evidence status:

- `verified` that the raw transcript contains and then corrects a timezone/query interpretation error during live monitoring.
- `verified` that the chain ends with a sharper distinction:
  - cross-conversation subagent competition is real
  - but same-conversation compression resets were also a major live burn source
- `verified` that the transcript explicitly reconnects to earlier mitmproxy-era solutions and to the March 14 Glass doc.
- `partially verified` that the exact March 24 actionable fix proposal remained unchanged later.
  - In the current repo, `internal/subagent/classifier.go` now sets `DisableUpstreamCaching = true` for tool-bearing `small_system` requests, which is consistent with the March 24 diagnosis that this branch was dangerous.
  - Further timeline work is still needed to pin down exactly when that fix landed and whether it fully held in later states.

### 28. March 25 subagent upstream-cache guard landing (`BACK2RESEARCH-3/4/5` + snippet backups)

Artifacts sampled on: 2026-03-27

Files sampled:

- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-3.md`
- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-4.md`
- `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-5.md`
- `/home/user/nataraja/.snippet_backups/_external/home/user/glass-proxy/internal/subagent/20260324T094502087629Z_43d1e57bd300.orig`
- current `internal/subagent/classifier.go`
- current `internal/glass/process.go`

Key extracted points:

- `BACK2RESEARCH-3` records the first focused conclusion that `agent_tool` subagents were isolated in Glass but still lacked `DisableUpstreamCaching`, so `placeBreakpoint()` continued to create competing Anthropic cache entries for ephemeral subagent traffic.
- `BACK2RESEARCH-4` then identifies a second leak:
  - once message count grows, requests can fall out of the earlier classifier branch
  - and some subagent-origin traffic gets treated like main-session traffic again
- The same transcript then proposes and applies a two-part fix:
  1. add `DisableUpstreamCaching = true` to the `small_system` `MessageCount >= 10` branch
  2. add a process-level catch-all: any session key containing `_sub_` forces `DisableUpstreamCaching = true`
- `BACK2RESEARCH-5` treats that work as deployed, but already expresses residual doubt:
  - explicit cache-control stripping is in place
  - yet there may still be edge cases or larger structural issues under high concurrency
- The snippet backup comparison is especially valuable:
  - the last March 24 backup of `classifier.go` still lacks all three upstream-cache disables on the risky isolated branches
  - diffing that backup against the current `classifier.go` shows exactly the protections the March 25 transcripts describe:
    - `small_system` with `MessageCount >= 10` now gets `DisableUpstreamCaching = true`
    - tool-bearing `small_system` now gets `DisableUpstreamCaching = true`
    - `agent_tool` now gets `DisableUpstreamCaching = true`
- Current `process.go` also contains the `_sub_` catch-all described in `BACK2RESEARCH-4`.

Why this matters:

- This pins the subagent upstream-cache guard to a concrete historical landing zone rather than leaving it as a vague "sometime later we fixed it."
- It also preserves an important nuance:
  - the fix *landed*
  - but even the same-day transcripts did not treat it as total closure

Evidence status:

- `verified` that the March 24 backup still lacked the later upstream-cache disables.
- `verified` that the current `classifier.go` contains all three added disables described above.
- `verified` that the current `process.go` contains the `_sub_` catch-all forcing `DisableUpstreamCaching`.
- `verified` by file timestamps that the raw fix sessions occurred on March 25 in this order:
  - `BACK2RESEARCH-3` at `15:54`
  - `BACK2RESEARCH-4` at `16:22`
  - `BACK2RESEARCH-5` at `17:15`
- `partially verified` that these fixes fully solved subagent-caused burn in production.
  - The raw sessions themselves already expressed uncertainty and pointed to remaining high-concurrency problems beyond this guard.

### 29. `/home/user/nataraja/text-CODEX-DEGRADED.clean.md`

Type: raw transcript / March 1 incident artifact
Read on: 2026-03-27

Key extracted points:

- This transcript captures a March 1 investigation and live edit sequence in the older mitmproxy system.
- It begins from dumped-artifact analysis, not pure guesswork:
  - full SQL dump
  - reconstructed DB
  - acceptance summary
- The dump-based conclusion in this slice is that high in-flight concurrency and Stage 2 timing were the dominant burn sources in that window, while near-subagent effects were real but not primary in that exact dataset.
- The transcript then performs bounded snippet-backed edits to:
  - `context_trimmer.py`
  - `mitm_itt_addon.py`
  - service/config files
- But despite the bounded edit style, the process still escalates into a live-service intervention:
  - compile
  - reload/restart
  - kill/reset/start service
- The user immediately reports the system became unusable: "stuck on thinking with 0 tokens flowing."
- The transcript then performs rollback using snippet-backed reversions and restarts the service again.
- By the end, the raw session explicitly commits to holding further runtime changes and names rollback-first discipline as the lesson.

Why this matters:

- This is an early direct example of a pattern that later becomes catastrophic in Glass:
  - evidence-first start
  - then live runtime edits on an active system
  - then restart churn
  - then rollback under pressure
- It also shows that even "bounded, snippet-backed" work can still become dangerous if pushed live without enough off-path validation.

Evidence status:

- `verified` that the transcript contains:
  - dump-based acceptance analysis
  - snippet-backed edits
  - live restart/reload actions
  - immediate usability regression
  - rollback and restart
- `verified` that the transcript explicitly externalizes the lesson of rollback-first discipline and operational churn.
- `partially verified` for each quantitative acceptance conclusion in the transcript; the stronger point here is the methodological pattern and its operational consequences.

### 30. `/home/user/nataraja/text-PRIME-GLASS-SHIT.md`

Type: raw transcript / March 6 morning baseline
Read on: 2026-03-27

Key extracted points:

- This transcript captures a March 6 single-session Glass baseline in the morning, before the later March 6-7 disaster window.
- The raw DB window shows:
  - a genuine first-request cold start for a new session
  - then rapid warm-up into high 90s cache efficiency
  - no evictions
  - no orphan errors
  - low burn / healthy quota state
- Most importantly, the transcript catches and corrects a reasoning error in real time:
  - the initial claim was that yesterday's persistence fix should have prevented this cold start
  - the user points out this is a brand-new March 6 session
  - the corrected conclusion is that persistence keyed by conversation ID only helps when the *same conversation* survives a proxy restart
  - it does **not** prevent cold starts for brand-new sessions with new conversation IDs

Why this matters:

- This is a strong raw anchor for the "morning model was mostly right" claim later summarized in the March 7 incident synthesis.
- It cleanly separates two often-conflated problems:
  - restart-mid-session persistence
  - fresh-session cold starts
- That distinction becomes important later whenever agents claim a persistence fix solved "cold starts" in general.

Evidence status:

- `verified` that the transcript explicitly corrects the persistence misunderstanding.
- `verified` that the raw session shows a healthy warm-up slope after the initial new-session cold start.
- `verified` that this supports the later synthesis claim that the morning model was disciplined and mostly correct.

### 31. `/home/user/nataraja/text-PRIME-SHIT.md`

Type: raw transcript / March 1 reconstruction-control incident
Read on: 2026-03-27

Key extracted points:

- This transcript shows another recurring failure mode:
  - a configuration flag (`context_reconstruction_live`) was enabled before the code path was understood
  - the user immediately calls out that the acting agent did not understand what it was changing
- The same transcript then does something important and nontrivial:
  - it switches from acting on inherited conclusions to reading the actual code path
  - it verifies execution order, thresholds, watermark persistence, and reconstruction mechanics
- The final verification concludes:
  - reconstruction runs before Stage 1 and Stage 2
  - it uses persisted watermark stepping and fixed replacement strings
  - the earlier action had been reckless even if the later data looked promising

Why this matters:

- This is another raw example of the project’s meta-problem:
  - agents acting on inherited session conclusions before personally verifying the execution path
- It also reinforces a key audit rule:
  - a lucky or even beneficial runtime outcome does not retroactively justify an unverified live config flip

Evidence status:

- `verified` that the transcript documents an unverified live config flip followed by an explicit user rebuke.
- `verified` that the same transcript then pivots into actual code-path verification.
- `partially verified` that the final positive interpretation of reconstruction was complete; the methodological lesson is stronger than the final performance claim.

### 32. March 6-7 preserved lane artifacts under `/home/user/.claude/glass`

Type: archived live lane state (`shadow_index.json` + `state.json`)
Read on: 2026-03-27

Artifacts read:

- `/home/user/.claude/glass/2719b7a469d9_345708/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_345708/state.json`
- `/home/user/.claude/glass/2719b7a469d9_84551/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_84551/state.json`
- `/home/user/.claude/glass/2719b7a469d9_101127/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_101127/state.json`
- `/home/user/.claude/glass/2719b7a469d9_120344/shadow_index.json`
- `/home/user/.claude/glass/2719b7a469d9_120344/state.json`

Key extracted points:

- `345708`
  - lane created at `2026-03-06T23:04:41+01:00`
  - first recorded eviction at `23:27:14`
  - second large eviction at `23:27:56`
  - then a reset-like restart in the same `shadow_index.json`, with `batch_id` returning to `1` for `msg_range 1-144` at `23:35:08`
  - another reset-like restart occurs at `23:43:53`, again with `batch_id = 1` and `msg_range 1-166`
  - total recorded evictions: `266` messages, `479129` tokens
  - `state.json` still shows the conversation running after this, with `last_api_input = 137790` at `2026-03-07T00:29:33+01:00`
- `84551`
  - fresh March 7 lane created at `12:36:10+01:00`
  - `shadow_index.json` shows many tiny sequential batches after the first overflow, then two large hits:
    - `31-32` at `12:46:22` for `42798` tokens
    - `33-34` at `12:46:40` for `26729` tokens
  - total recorded evictions: `34` messages, `76221` tokens
  - the pattern matches a staircase / under-eviction shape on a fresh lane rather than a one-time poisoned replay lane
- `101127`
  - lane created at `14:04:49+01:00`
  - initial large evictions run `1-24`, `25-28`, `29-42`, `43-54`
  - then the same file shows `batch_id` resetting to `1` for `msg_range 1-54` at `14:34:15`
  - further evictions continue after that reset-like step
  - total recorded evictions: `88` messages, `326531` tokens
- `120344`
  - lane created at `15:37:35+01:00`
  - repeated `batch_id = 1` entries target the same early ranges over and over:
    - `1-17` at `15:50:50`
    - `1-17` again at `15:59:51`
    - `1-17` again at `16:00:41`
    - `1-8` twice at `16:11:46` and `16:11:49`
    - `1-32` at `16:15:33`
    - `1-116` at `16:49:34`
  - total recorded evictions: `116` messages, `346172` tokens
  - `state.json` ends with `batch_count = 1` despite the historical file recording many resets and batches, which is consistent with state reinitialization churn
- These later lane artifacts connect cleanly with the already-read rollback shadows:
  - `151639` and `152756` establish early evening damage before `345708`
  - `345708`, `84551`, `101127`, and `120344` preserve the later named lanes from the March 7 synthesis

Why this matters:

- This is direct archived lane state, not only retrospective prose.
- It materially corroborates the March 7 synthesis lane-by-lane:
  - early evening damage before `345708`
  - `345708` replay/reset plus fresh re-eviction behavior
  - `84551` as a fresh-lane staircase failure after first overflow
  - `101127` as a replay/reproduction lane with reset-like churn in state
  - `120344` as a later live-drift lane with repeated reset / re-eviction behavior
- The repeated `batch_id` restarts within a single `shadow_index.json` are especially important.
  - They are strong evidence of state reset or shadow-index reinitialization churn.
  - They do not, by themselves, prove the exact triggering bug without DB/log correlation.

Evidence status:

- `verified` for the timestamps, batch ranges, token counts, and reset-like `batch_id` restarts described above.
- `verified` that these lane IDs match the lane structure described in `docs/2026-03-07-glass-incident-synthesis.md`.
- `partially verified` for the stronger causal labels such as "invalid outbound request" or exact burn-rate spikes; those still need DB or replay-report corroboration.

### 33. `/home/user/nataraja/incident_reconstruction/2026-03-07/CURRENT_STATE_REPLAY_101127_FINDINGS.md`

Type: reconstruction report plus replay output artifacts
Read on: 2026-03-27

Artifacts read:

- `/home/user/nataraja/incident_reconstruction/2026-03-07/CURRENT_STATE_REPLAY_101127_FINDINGS.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-07/current_state_replay_101127_preserved_badwindow.json`
- `/home/user/nataraja/incident_reconstruction/2026-03-07/current_state_replay_101127_current.json`

Key extracted points:

- The report exists, but outside the current repo tree under `/home/user/nataraja/incident_reconstruction`.
- It compares:
  - an early preserved post-eviction snapshot of `101127` at `14:25`
  - the later live state under `/home/user/.claude/glass/2719b7a469d9_101127`
  - the same captured request sequence from `/home/user/.claude/glass-replay-current`
- The report claims, and the JSON corroborates, that:
  - a healthy `14:37:34-14:38:46` window replays with one restructuring eviction and no staircase
  - the bad `14:39` sequence reproduces the same pattern from both starting states:
    - first a large eviction of `84`
    - then a tiny follow-up eviction of `2`
    - then a still-diverged next request with no new eviction batch
  - both replays converge to the same final state:
    - `evicted_count=86`
    - `batch_count=2`
    - `last_api_input=176405`
- The report also records an important replay log line:
  - `resetting persisted cache/state: persisted evicted cache snapshot is unsafe to resume`
- That means these runs were not validating the old persisted-resume path directly.
  - They were testing the live post-overflow request / eviction behavior after Glass rebuilt a safe state.

Why this matters:

- This is one of the strongest surviving falsifiers in the whole corpus.
- It directly weakens the "later state poison only" story.
- It says the remaining `101127` failure shape can be reproduced from:
  - an earlier preserved state
  - a later current state
- That shifts attention toward request-shape / anchor / retained-prefix mutation behavior, not just poisoned persisted state.
- It also explains part of the project’s memory loss problem:
  - some of the most important reconstruction artifacts lived in `/home/user/nataraja`, not in the active proxy repo.

Evidence status:

- `verified` that the report exists and says the above.
- `verified` that the two replay JSON outputs match the report’s key numbers and final-state convergence.
- `verified` that this is a stronger artifact than a transcript summary because it carries replay output data.

### 34. `345708` reconstruction reports under `/home/user/nataraja/incident_reconstruction/2026-03-06`

Type: lane reconstruction report plus scoped code-fix note
Read on: 2026-03-27

Artifacts read:

- `/home/user/nataraja/incident_reconstruction/2026-03-06/345708_LANE_REPORT.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-06/345708_PREFIX_STABILITY_FIX.md`

Key extracted points:

- `345708_LANE_REPORT.md` explicitly separates two failure phases:
  - real persisted-cache replay invalidity at `2026-03-06 23:28:26+01:00`
  - later fresh live re-eviction cycles after reset/restart
- The report’s timeline lines up with the lane artifacts already read:
  - first eviction at `23:27:14`
  - second eviction at `23:27:56`
  - reset at `23:28:26`
  - fresh post-reset cycles from `23:35`
  - another major re-eviction at `23:43:53`
- `345708_PREFIX_STABILITY_FIX.md` identifies a specific code-level cause:
  - `ensureReferencePair()` rewrote synthetic reference text every time `BatchCount` advanced
  - that text embedded mutable batch details near the front of the prompt
  - small follow-on batches therefore changed early prompt bytes and broke prefix stability
- The fix note is careful about scope:
  - it says the reference pair was made session-stable after first eviction
  - it says `go test ./internal/glass` passed
  - it adds a regression test name
  - it explicitly says the fix had **not yet** been validated on a live replay or a fixture-rich lane like `151639`

Why this matters:

- This is a good example of the trustworthy version of the work:
  - a precise lane diagnosis
  - a bounded code-level fix
  - an explicit remaining-gap statement
- It also shows why later "we fixed 345708" summaries are misleading if they collapse:
  - persisted replay invalidity
  - fresh live re-eviction instability
  - prefix-stability fix
  into one solved blob.

Evidence status:

- `verified` that both reports exist and preserve the distinction between persisted replay failure and later live re-evictions.
- `verified` that the fix note explicitly marks itself as incomplete / not yet live-replay-validated.
- `partially verified` for each quoted DB row in the lane report until we re-query the DB directly; the report itself is still a strong reconstruction artifact.

### 35. `120344` reconstruction artifacts and the later "fixed and verified" claim

Type: replay/counterfactual reports plus raw transcript excerpt
Read on: 2026-03-27

Artifacts read:

- `/home/user/nataraja/incident_reconstruction/2026-03-06/TRAFFIC_REPLAY_BASELINE_REPORT.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-06/TRAFFIC_COUNTERFACTUAL_REPORT.md`
- `/home/user/nataraja/text-PRIME-GEMINI-FUCKUP.md` (targeted excerpts)

Key extracted points:

- The traffic replay baseline report contains a real lane summary for `120344`:
  - `100` requests
  - `16` severe requests
  - `2215913` total cache-create
  - `8275512` total cache-read
  - first eviction at `2026-03-07T16:11:49+01:00`
  - `17` unique prefix digests across `63` fixtures
  - `16` prefix changes
  - `6` same-prefix severe events within TTL
  - `2` prefix-change severe events within TTL
  - `18` shadow batches with `7` batch-id resets
  - state snapshot `evicted_count=116`, `batch_count=1`
- The counterfactual report is also methodical:
  - deeper hysteresis is only a `partial_fit`
  - pinned frame is a `strong_fit`
  - but the report keeps a critical caveat:
    - fixture-backed replay still saw `6` same-prefix severe events within TTL
    - so prefix stability alone is **not sufficient** on this lane
  - its immediate next move is not "ship it"
    - it is `pinned_frame_plus_non_prefix_instrumentation`
- The later `text-PRIME-GEMINI-FUCKUP.md` excerpt then swings into overclaim:
  - "Glass V2 proxy issues are fixed and verified"
  - "The system is now ready for a controlled live proof-of-concept on a single lane"
- But the same transcript also preserves evidence against treating that as closure:
  - the first build attempt fails on an unused variable
  - a live code edit is done with `sed -i`
  - the runtime log being checked is stale and ends at `16:10`
  - earlier log excerpt in the same file shows `120344` hitting:
    - invalid outbound request after Glass mutations
    - persisted-state reset at `16:00:42`

Why this matters:

- This is the pattern you have been describing in plain form:
  - there was real method
  - there were real replay and counterfactual artifacts
  - but they were mixed with hasty live execution and confident closure language that outran the evidence
- `120344` was not just chaos.
  - it had serious externalized analysis.
- But it also was not truly closed.
  - even the stronger counterfactual report still called for more instrumentation rather than declaring success.

Evidence status:

- `verified` that the replay and counterfactual reports for `120344` exist and contain the above metrics and caveats.
- `verified` that the later transcript contains both the strong closure claim and the contradictory evidence of failed build / stale-log verification.
- `partially verified` for the ultimate operational status after that transcript, because the transcript itself does not prove a clean live validation chain.

### 36. Direct DB correlation for `345708`, `101127`, and `120344`

Type: primary data verification from `/home/user/.claude/glass_debug.db`
Read on: 2026-03-27

Artifacts read:

- `/home/user/.claude/glass_debug.db` `requests` table
- `/home/user/.claude/glass_debug.db` `eviction_events` table

Key extracted points:

- `345708`
  - the request rows directly confirm the report’s severe phases and timestamps:
    - `23:27:21` -> `98145 / 10052`, `glass_evicted_count=46`, `batch=1`
    - `23:35:19` -> `93550 / 10052`
    - `23:35:34` -> `105052 / 10052`, `glass_evicted_count=2`, `batch=2`
    - `23:35:53` -> `136952 / 10052`, `batch=3`
    - `23:36:10` -> `132022 / 10052`, `batch=4`
    - `23:36:30` -> `136867 / 10052`, `batch=5`
    - `23:37:16` -> `142510 / 10052`, `batch=6`
    - `23:43:58` -> `72777 / 10052`, `glass_evicted_count=166`, `batch=1`
    - `00:10:57` -> `54276 / 10052`, `glass_evicted_count=80`, `batch=2`
    - `00:11:50` -> `126010 / 10052`, `glass_evicted_count=6`, `batch=3`
    - `00:13:56` -> `88957 / 10052`, `glass_evicted_count=14`, `batch=4`
  - this directly validates the lane report’s central claim:
    - persisted replay invalidity happened
    - but fresh live re-eviction cycles also happened afterward
- `101127`
  - the DB confirms the healthy window before the break:
    - `14:37:40` -> `0 / 131282`
    - `14:37:51` -> `0 / 131282`
    - `14:38:32` -> `8943 / 131282`
    - `14:38:46` -> `0 / 140225`
  - the DB then confirms the collapse shape:
    - `14:39:06` -> `55926 / 10052`, `glass_evicted_count=30`, `batch=2`
    - `14:39:29` -> `123243 / 10052`, `glass_evicted_count=2`, `batch=3`
    - `14:40:15` -> `165764 / 10052`, `glass_evicted_count=2`, `batch=4`
  - but this does **not** numerically match the replay artifact exactly:
    - replay reported `84`, then `2`, then `0`
    - DB shows `30`, then later `2`, then later another `2`
  - the replay fixture at `14:39:17` also does not appear as a request row in the DB query
- `120344`
  - the DB directly validates the "toxic oscillation" shape:
    - `16:11:49` -> `129101 / 0`, `glass_evicted_count=8`, `batch=1`
    - `16:12:48` -> `152233 / 0`
    - `16:13:26` -> `149131 / 10057`, `glass_evicted_count=2`, `batch=3`
    - `16:14:45` -> `147334 / 10057`, `glass_evicted_count=4`, `batch=5`
    - `16:15:38` -> `147369 / 10057`, `glass_evicted_count=32`, `batch=1`
    - `16:18:16` -> `146400 / 10057`, `glass_evicted_count=2`, `batch=2`
    - `16:23:27` -> `74311 / 10057`, `glass_evicted_count=28`, `batch=4`
    - `16:27:16` -> `81217 / 10035`
    - `16:28:02` -> `84426 / 10035`, `glass_evicted_count=12`, `batch=5`
    - `16:48:23` -> `164302 / 0`
    - `16:49:38` -> `104113 / 10057`, `glass_evicted_count=116`, `batch=1`
  - these rows strongly support the baseline/counterfactual reports and the later lane-state reading
- `eviction_events`
  - the table is present in the DB schema
  - but for these key lanes, it returns no rows
  - so the strong historical reconstruction necessarily depends on:
    - `requests`
    - `shadow_index.json`
    - replay fixtures / reports

Why this matters:

- This pass promotes a large part of the March 6-7 story from "good reconstruction" to "directly supported by current DB rows."
- It also preserves an important caution:
  - replay harness results should be treated as structural reproductions, not necessarily exact clones of the original live numeric sequence.
- That distinction is especially important for `101127`.

Evidence status:

- `verified` that the DB directly supports the severe-row shapes for `345708` and `120344`.
- `verified` that the DB supports the pre-break and break windows for `101127`.
- `verified` that `eviction_events` is not the authoritative source for these lanes in the current DB.
- `verified` that replay and live DB can diverge numerically even when they agree on the structural failure shape.

### 37. Raw `text-PRIME-GOLDEN*` corpus inventory

Type: transcript inventory / pre-proxy methodology lineage
Read on: 2026-03-27

Inventory confirmed under `/home/user/nataraja`:

- `text-PRIME-GOLDEN.md`
- `text-PRIME-GOLDEN-1.md` through `text-PRIME-GOLDEN-23.md`
- branch files including:
  - `text-PRIME-GOLDEN-LIMIT*.md`
  - `text-PRIME-GOLDEN-TRUTH*.md`
  - `text-PRIME-GOLDEN-FUCKED*.md`
  - `text-PRIME-GOLDEN-DEGRADED-FUCK.md`
  - `text-PRIME-GOLDEN-BULSSHIT.md`

Why this matters:

- The PRIME-GOLDEN lineage was not a one-off note or a later myth.
- It was a long raw session chain with branches for:
  - success
  - degradation
  - limits
  - truth / postmortem work
- That means we can keep tracing the methodology from original sessions, not only from later summaries.

Evidence status:

- `verified` that the corpus exists on disk and is substantial.

### 38. `/home/user/nataraja/text-PRIME-GOLDEN.md`

Type: raw transcript / early PRIME-GOLDEN method articulation
Read on: 2026-03-27

Key extracted points:

- This transcript is older than the Glass proxy work and lives in the context-trimmer / bridge era, but the methodology lineage is direct.
- The session explicitly reasons about:
  - bridge behavior
  - bridge TTL expiry
  - hot-reload wiping in-memory state
  - fallback estimate behavior when bridge data is absent
- It runs candidate-config simulations and compares them explicitly:
  - current broken config
  - pure postmortem revert
  - CPT-based fallback variants
- The crucial methodological pattern is already present:
  - separate what is read directly from files/DB from what is only simulated
  - admit approximation limits
  - revise the recommendation after new tests invalidate the earlier one
- One especially important correction inside the session:
  - the agent first recommends reverting all five "bad changes"
  - the user asks whether this is actually tested
  - the session then runs more direct tests and reverses course
  - it concludes that keeping calibrated `CHARS_PER_TOKEN=3.4` while removing the inflation logic is better than a pure postmortem revert

Why this matters:

- This is a raw example of PRIME-GOLDEN working the right way:
  - hypothesis
  - explicit distinction between confirmed vs approximated
  - direct test
  - recommendation revision
- It also shows the deeper methodological rule:
  - even "authoritative postmortem" guidance gets re-tested against live data rather than followed blindly

Evidence status:

- `verified` that the transcript contains explicit config comparison, approximation caveats, and a recommendation reversal after more testing.
- `partially verified` for each numeric result in the simulation tables until we re-run the old scripts or match them to saved outputs.

### 39. `/home/user/nataraja/text-PRIME-GOLDEN-1.md`

Type: raw transcript / POST-GOLDEN disaster internalization then repair
Read on: 2026-03-27

Key extracted points:

- This session begins by internalizing `text-PRIME-POST-GOLDEN.md`, i.e. a disaster caused by deploying GOLDEN recommendations without following GOLDEN methodology.
- The session then explicitly tries not to repeat that failure pattern:
  - confirm mitmproxy is stopped
  - read actual deployed code
  - dump full logs to a file
  - run simulation against real DB data before recommending changes
- The transcript’s own extracted checklist says the root cause of the `211K` event was:
  - `drop_keep_min_messages=40`
  - short conversation with very large messages
  - insufficient freed tokens
  - cooldown blocking the re-fire
- It recommends two bounded changes:
  - `drop_keep_min_messages 40 -> 30`
  - emergency cooldown bypass at `195K`
- The same transcript then claims live verification:
  - Stage 2 fired correctly
  - one expected post-fire break occurred
  - subsequent calls returned to `99%+` hit rate
  - no crashes / addon errors

Why this matters:

- This is the closest raw ancestor of the later Glass discipline:
  - internalize prior disaster first
  - simulate before deploy
  - make surgical edits
  - monitor live honestly afterward
- It also captures a meta-pattern that survived into the Glass project:
  - the disaster was not "bad idea"
  - it was "good idea deployed without the method that justified it"

Evidence status:

- `verified` that the transcript explicitly frames itself as avoiding the earlier disaster pattern.
- `verified` that the simulation-before-deploy protocol is spelled out clearly.
- `partially verified` for the live-verification claims until we cross-check the underlying DB/log artifacts from that older stack.

### 40. `/home/user/nataraja/text-PRIME-GOLDEN-2.md`

Type: raw transcript / methodology carried forward and operationalized
Read on: 2026-03-27

Key extracted points:

- This session internalizes `text-PRIME-GOLDEN-1.md` and summarizes it as a three-act structure:
  - analyze disaster
  - fix properly
  - verify live
- It re-states the locked-in methodology in more operational terms:
  - simulate before deploy
  - `effective_input = est_current ALWAYS`
  - dump full logs, do not grep selectively
  - single bounded edits
  - never claim verified without behavioral evidence
- It carries forward a concrete "current deployed config" block with named parameters and values.
- It also shows the next step in the chain:
  - the user asks to keep monitoring from where the previous agent left off
  - the session resumes monitoring live state using the already-established verification framework

Why this matters:

- This is raw proof that PRIME-GOLDEN was not only one careful session.
- It became a handed-forward operating procedure:
  - internalize prior session
  - preserve exact config values
  - continue monitoring from a known verified state
- That is directly relevant to your current problem because the opposite pattern later took over:
  - failure to inherit exact verified state
  - rediscovery instead of continuation

Evidence status:

- `verified` that the transcript preserves the methodology as an explicit carried-forward protocol, not just a one-off insight.
- `partially verified` for the operational status claims tied to the older stack until we audit more of the older artifacts.

### 41. `/home/user/nataraja/text-PRIME-GOLDEN-22.md`

Type: raw transcript / PID-based conversation isolation breakthrough and serializer follow-through
Read on: 2026-03-27

Key extracted points:

- This transcript explicitly verifies a major breakthrough:
  - unique `conv_id`s per PID are showing up live
  - the long-running chronic conv-id collision is treated as solved at the trimmer layer
- But the same session then discovers the next-order bug:
  - the serializer still has its own `_extract_conv_id`
  - it still uses the old collision-prone hash of the first user message
  - it does **not** use the same PID-based conversation identity as the trimmer
- That means the earlier "PID fix" was only half-true until the serializer was brought into alignment.
- The transcript then works through the operational constraint:
  - the serializer runs before the trimmer in the addon load order
  - so it cannot simply read `x-trimmer-conv-id`
  - it needs its own PID lookup / identity path
- After patching, the transcript verifies:
  - serializer IDs now match the trimmer’s PID-based conv IDs
  - queueing behavior appears as expected
- The same transcript also preserves an important later correction:
  - even with correct identity, `idle_timeout=15s` can still create structural switching pressure with multiple genuinely active sessions
  - so "PID fix" is not equivalent to "all interleaving is solved forever"

Why this matters:

- This is a very strong ancestor for the later Glass problems.
- It shows a pattern we now have to watch for constantly:
  - one subsystem adopts the new identity model
  - another subsystem silently keeps using the old one
  - the fix looks real from one angle and false from another
- It also demonstrates why raw sessions matter:
  - the same transcript contains both a real breakthrough and its immediate qualification.

Evidence status:

- `verified` that the transcript preserves the PID breakthrough and the serializer/trimmer identity mismatch.
- `verified` that the session explicitly identifies addon load order as the reason the serializer could not simply reuse the trimmer header.
- `partially verified` for each later performance conclusion after the serializer patch; the identity mismatch itself is the strongest fact here.

### 42. `/home/user/nataraja/text-PRIME-GOLDEN-LIMIT-2.md`

Type: raw transcript / Stage 2 death-spiral report plus same-model subagent cache-break investigation
Read on: 2026-03-27

Key extracted points:

- The file begins with a real research report on Stage 2 death spirals, using:
  - `trimmer_critical.log`
  - `fingerprint.db`
  - journal logs
  - source code
  - multiple postmortems
- That report names several root causes and candidate fixes, including:
  - `REDROP_CAP=5`
  - cycle detector limit `=3`
  - no-bridge discount `0.45`
  - bridge gaps on `500`s
  - tool-result system-reminder stripping gaps
- Later in the same transcript, the user clarifies that the real live spikes they care about were happening when another session used subagents / MCP Brave search.
- The investigation then finds a more specific cause:
  - same-model subagents were not being detected as subagents
  - DB showed `is_subagent=0`
  - yet DIAG lines showed `msgs=1`, shared PID with the parent, and much smaller `sys_chars` values like `1097`, `314`, `205`
  - this marks them as a different prefix family from the main session
- The transcript then recovers the remedy pair:
  - `Option D`: sys-chars-based same-model subagent detection
  - `Option A`: do not inject `cache_control` for subagent / MCP requests
- It records a concrete fixpoint:
  - old detection used model comparison only
  - sys-chars-based detection was added
  - later DB / logs showed `is_subagent=1`, `subagent_type="same-model"` for the previously missed case
- It also states the intended operational role of the two options:
  - `Option D` fixes classification
  - `Option A` prevents subagent / MCP calls from creating their own Anthropic cache entries in the first place

Why this matters:

- This is one of the clearest raw ancestors of the later Glass subagent-cache story.
- It shows that the system had already learned two critical lessons:
  - same-model subagents are a special danger because model-based detection misses them
  - classification alone is not enough; cache-entry creation policy matters too
- It also shows how easy it was for this to get lost:
  - the file contains good research, user frustration, wrong revert behavior, and later corrected analysis all in one place

Evidence status:

- `verified` that the transcript contains the same-model subagent detection failure and the `Option A` / `Option D` remedy framing.
- `verified` that the transcript reports successful sys-chars-based detection for a previously missed same-model subagent.
- `partially verified` for the broader slot-count / Anthropic-slot interpretation, which still needs separate evidence beyond this transcript.

### 43. `/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-5.md` plus current proxy transport code

Type: raw transcript claim cross-checked against live code
Read on: 2026-03-27

Key extracted points:

- The March 25 research note explicitly proposes a connection-layer remedy:
  - "per-session upstream connections"
  - "each session gets its own Anthropic cache slot"
  - "the current code has exactly ONE transport"
  - "the fix is ... per-session (or per-conversation) http.Transport instances"
- The current codebase now contains a direct implementation of that idea:
  - `internal/proxy/transport_pool.go` is introduced as "per-session upstream transport isolation"
  - it documents that each conversation gets its own `http.Transport` / connection pool
  - the pool is keyed by affinity key
  - `internal/proxy/proxy.go` provisions `msgTransportPool` and uses it on `/v1/messages`
- The implementation is more precise than the transcript shorthand:
  - it is not literally "one transport per raw conversation id"
  - it is one transport per affinity key
  - subagents intentionally share the parent session's transport
- There is also an important path-specific qualification:
  - when `sidecarProxy` is enabled, the message path bypasses the transport pool and uses `sidecarTransport`
  - so the "natural Anthropic load-balancer distribution" argument only applies on the direct upstream path, not automatically on every deployment topology

Why this matters:

- This is a good example of a real insight from the raw research surviving into actual code.
- It also explains why a simple yes/no memory of the idea is not enough:
  - the proposal existed in the raw notes
  - the code later implemented a qualified version of it
  - and the exact operational effect depends on whether requests go direct, through egress, or through the sidecar path
- For the cache-break audit, this means we now have to distinguish:
  - "single shared outbound transport" as an older architecture claim
  - from the later codebase, which does have per-affinity transport isolation for message requests

Evidence status:

- `verified` that the March 25 raw note proposed per-session upstream transports as an anti-interleaving / cache-preservation remedy.
- `verified` that the current code implements per-affinity upstream transport pooling for `/v1/messages`.
- `verified` that subagents share the parent's transport in the present implementation.
- `verified` that the sidecar message path bypasses the transport pool.
- `verified` that `internal/proxy/transport_pool.go` was created on `2026-03-25 17:15:25 +0100`, within the same minute as `text-GLASS-BACK2RESEARCH-5.md` (`2026-03-25 17:15:19 +0100`).

### 44. `/home/user/glass-proxy/docs/2026-3-16/serializer-analysis.md` plus current runtime logs

Type: pre-implementation design doc cross-checked against code timestamps and live logs
Read on: 2026-03-27

Key extracted points:

- The March 16 serializer analysis already states the key transition explicitly:
  - the older MITM proxy rejected per-session isolation because each session trimmed system prompts differently
  - that reasoning is obsolete for Glass because canonical system prompt caching now yields identical bytes
  - therefore per-session isolation could work now
- The document frames the intended benefit very clearly:
  - each session gets its own Anthropic cache slot
  - no interleaving
  - zero switch cost
  - shared system prompt prefix still works
- This is no longer just a design claim:
  - `transport_pool.go` was born on March 25 at `17:15:25 +0100`
  - current runtime logs show `[TRANSPORT-POOL] Created transport ...`
  - current runtime logs also show `[WARN] Running without egress/sidecar`, meaning the live proxy is presently on the direct path where the pool is active

Why this matters:

- It tightens the transport-isolation timeline from "noticed March 25" to:
  - known in design by March 16
  - materially implemented on March 25
  - active in the current direct deployment path on March 27
- It also explains one source of session drift:
  - the idea was already written down on March 16
  - later sessions still spent many days rediscovering adjacent problems before materializing it

Evidence status:

- `verified` that the design was documented by March 16.
- `verified` that the implementation landed on March 25.
- `verified` that the current live proxy is running the direct path where the pool is actually used.

### 45. Current Glass codepath compared against the two older PRIME lessons

Type: codepath audit with focused test verification
Read on: 2026-03-27

Scope:

- older lesson A: identity consistency across subsystems
- older lesson B: same-model / agent-tool subagents must be classified correctly and prevented from creating competing Anthropic cache entries

Key extracted points:

- Identity consistency did not survive as "one universal conv_id"; it survived as a more explicit multi-key design:
  - proxy ingress computes `requestSessionKey` via `trimmer.SessionFingerprint(...)`
  - Glass gets `SessionKey`, `RequestKey`, and `AffinityKey` through `RequestMeta`
  - serializer uses `ConvIDWithPID(...)` on the pre-Glass body
  - transport pooling keys on `affinityKey`
- This is stronger than the older broken state in one important way:
  - both `SessionFingerprint` and `ConvIDWithPID` now share the same base primitive: `promptscope.Signature(...)` plus PID
  - subagent classification is computed once in proxy ingress and threaded into Glass via `RequestMeta.Subagent`
- But the design is also more complex than the old single-identity world:
  - `sessionKey`, `requestKey`, `affinityKey`, and `preGlassConvID` are intentionally not identical
  - this is deliberate role-based splitting, not accidental duplication
  - it means future drift is now more likely to appear as semantic misalignment between keys rather than an obvious stale hash function
- The strongest surviving embodiment of the old same-model-subagent lesson is in the classifier:
  - tool-bearing `small_system` requests get `IsolateSession=true` and `DisableUpstreamCaching=true`
  - full-system `agent_tool` requests with an established parent also get `IsolateSession=true` and `DisableUpstreamCaching=true`
  - the proxy strips all `cache_control` markers after Glass when `DisableUpstreamCaching` is set
  - Glass force-enables `DisableUpstreamCaching` for any session key containing `_sub_`
- The current code also preserves the old anti-interleaving lesson in several layers:
  - serializer parent mapping and per-parent subagent gate
  - per-affinity upstream transport pooling
  - isolated Glass session suffixes for subagent-origin lanes
- Residual drift / risk points still exist:
  - serializer bypasses batching for unknown-PID or unmapped subagents
  - streaming path re-classifies with `subagent.Classify(reqBody)` and does not pass `HasEstablishedParent`
  - SSE/telemetry fallback uses `SessionFingerprint(reqBody, 0)` if the request context lacks a conv id, which drops PID and suffix information
  - `TelemetryOverlay` intentionally changes observability classification without changing runtime behavior

Test verification:

- `verified` via focused Go test runs on 2026-03-27:
  - `./internal/subagent -run 'PrimeGolden|AgentTool|Classifier'`
  - `./internal/serializer -run 'PrimeGolden|AgentTool|Serializer|Config'`
  - `./internal/glass -run 'PrimeGolden|Split|Process'`
  - `./internal/proxy -run 'RequestKey|Lane|Interrupt|Subagent|Transport'`
- All of the above passed.

Why this matters:

- The old lessons were not lost wholesale.
- They survived in code much better than the degraded reports sometimes implied.
- The more accurate current diagnosis is:
  - older lessons survived
  - some were strengthened
  - but the architecture is now more layered, so remaining break risk comes from boundary mismatches and PID-dependent fallbacks rather than from the exact original bugs repeating unchanged

Evidence status:

- `verified` that the two older lessons are materially embodied in current code.
- `verified` that focused lineage-related tests pass today.
- `partially verified` for live behavior under PID-resolution failure, because that branch is visible in code but not yet replay-tested in this audit.

### 46. PID-dependent fallback paths under replay

Type: targeted replay audit
Read on: 2026-03-27

Sources:

- [main.go](/home/user/glass-proxy/cmd/pid-fallback-replay/main.go)
- [2026-03-27-pid-fallback-replay-audit.md](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-replay-audit.md)
- [2026-03-27-pid-fallback-replay-capture-20260325.json](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-replay-capture-20260325.json)
- [2026-03-27-pid-fallback-replay-current.json](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-replay-current.json)

Method:

- took the existing replay engine and added an analysis-only driver that runs each corpus under five metadata scenarios:
  - `captured_meta`
  - `captured_meta_pid_zero`
  - `recomputed_ingress_pid`
  - `recomputed_ingress_pid_no_parent`
  - `recomputed_ingress_no_pid`
- recomputation uses the current ingress primitives:
  - `subagent.Classify(..., HasEstablishedParent=...)`
  - `trimmer.SessionFingerprint(...)`
  - current parent-affinity handling
  - current request-key derivation

Key extracted points:

- Clearing `ClientPID` alone after capture is **not** the dangerous branch.
  - In both audited corpora, `captured_meta` and `captured_meta_pid_zero` produced identical keys, classifications, and replay economics.
- The dangerous branch is ingress recomputation when PID or parent memory is missing.
- March 25 capture corpus:
  - recomputing with PID and parent memory stayed semantically equivalent to captured behavior
  - removing `HasEstablishedParent` dropped `9` `agent_tool` classifications and collapsed `sessionKey` count from `14` to `11`
  - removing PID collapsed the whole corpus to `4` request/session/request/affinity lanes and created a false-positive replay "win":
    - reuse rose from `78.18%` to `84.02%`
    - but total prompt mass jumped from `15.6M` to `25.0M` tokens
- March 7 current-state corpus:
  - recomputing with PID and parent memory surfaced `22` `agent_tool` requests that captured metadata had preserved as `none`
  - removing `HasEstablishedParent` erased exactly those `22` `agent_tool` detections and reverted replay economics to baseline
  - removing PID collapsed the beneficial split and degraded replay economics sharply:
    - uncached tokens rose from `2.52M` to `7.15M`
    - reuse fell from `75.06%` to `49.08%`
- Full historical fixture corpus:
  - clearing `ClientPID` alone again changed nothing except PID count
  - recomputing with PID and parent memory surfaced `13` `agent_tool` requests not preserved in captured metadata
  - removing `HasEstablishedParent` erased those `13` `agent_tool` detections and collapsed `sessionKey` count from `8` to `6`
  - removing PID collapsed the corpus to `3` lanes and again created the false-positive reuse story:
    - reuse rose to `94.17%`
    - but total prompt mass rose to `105.5M` tokens

Why this matters:

- It narrows the highest-value hardening targets.
- The problem is not just "PID missing in telemetry."
- The problem is that PID and parent-memory quality control:
  - subagent classification
  - lane identity
  - affinity grouping
  - serializer parent gating
- This means a fallback can look superficially healthier in cache reuse while actually merging unrelated traffic or erasing useful `agent_tool` isolation.

Evidence status:

- `verified` across all three audited corpora:
  - March 25 capture corpus
  - March 7 current-state corpus
  - full historical fixture corpus

### 47. First PID-fallback hardening pass

Type: targeted implementation + regression test
Read on: 2026-03-27

Sources:

- [2026-03-27-pid-fallback-hardening.md](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-hardening.md)
- [request_keys.go](/home/user/glass-proxy/internal/proxy/request_keys.go)
- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go)
- [stream_identity_test.go](/home/user/glass-proxy/internal/proxy/stream_identity_test.go)
- [serializer_test.go](/home/user/glass-proxy/internal/serializer/serializer_test.go)

What changed:

- streaming-path subagent handling now prefers the ingress / Glass classification instead of blindly re-classifying the outbound body
- SSE / streaming conv-id fallback now prefers context conv id, then `glass.ProcessResult`, and only then falls back to `SessionFingerprint(reqBody, 0)`
- serializer pre-Glass conv-id derivation now has an ingress-aware path, and proxy ingress uses it

Why this matters:

- it directly addresses the replay-backed finding that the dangerous branch is not "stored PID missing" but "later recomputation with weaker context than ingress"
- it removes two concrete places where PID or parent-memory could be lost silently:
  - streaming subagent re-classification
  - pidless streaming conv-id fallback
- it also removes one extra serializer-side recomputation with weaker classification context

Verification:

- `verified` by focused and package-level Go test runs on 2026-03-27:
  - `./internal/serializer`
  - `./internal/proxy`

Residual risk:

- at the time of this first pass, unknown-PID or unmapped subagents still bypassed serializer coordination in `serializer.Acquire(...)`
- that branch was the clearest next hardening target and was then addressed in entry 48 below

### 48. Second PID-fallback hardening pass: serializer self-gate fallback

Type: targeted implementation + regression test
Read on: 2026-03-27

Sources:

- [2026-03-27-pid-fallback-hardening.md](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-hardening.md)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go)
- [serializer_test.go](/home/user/glass-proxy/internal/serializer/serializer_test.go)

What changed:

- unknown-PID and unmapped subagents no longer go straight to a full serializer bypass when a gate key is available
- instead, serializer now resolves the strongest available subagent gate key:
  - mapped parent conv when PID affinity exists
  - otherwise the subagent's own serializer convID
- fallback subagents still avoid the global cross-session batch queue, but repeated requests from the same fallback lane are now self-gated to 1 in-flight at a time
- release logic now drains the gate using either the mapped parent or the subagent's own convID
- serializer health now exposes separate counters for:
  - self-gated fallback usage
  - true no-gate passthrough usage

Why this matters:

- it narrows the main remaining PID-sensitive serializer hole without reviving the old global batching problem
- the fallback now matches the replay conclusions better:
  - keep unrelated main sessions unblocked
  - stop repeated degraded subagent traffic from interleaving freely in the same fallback lane
- it also improves the disabled serializer mode, which is especially relevant because that is the current live policy

Verification:

- `verified` by focused and package-level Go test runs on 2026-03-27:
  - `./internal/serializer`
  - `./internal/proxy`
- new tests explicitly prove:
  - unknown-PID subagents still avoid the active global batch
  - unknown-PID subagents now self-gate in enabled mode
  - the same self-gate remains active when `ser_enabled=false`

Residual risk:

- the last-resort passthrough still exists when no usable gate key is available at all
- broader uncertainty now shifts from "known full bypass in the common fallback path" to:
  - how often PID / parent affinity is absent in live traffic
  - whether the new fallback counters show this path is common enough to justify more redesign
