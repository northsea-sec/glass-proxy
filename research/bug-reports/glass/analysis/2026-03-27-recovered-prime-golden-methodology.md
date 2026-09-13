# Recovered PRIME-GOLDEN Methodology

Date: 2026-03-27
Status: reconstructed from surviving docs, tests, transcripts, and analysis artifacts

## Purpose

This note reconstructs the methodological spine that repeatedly got rediscovered and then lost during degraded sessions.

It is not a new theory.

It is a recovery of the approach that most consistently produced real progress:

1. isolate one claim
2. verify against durable artifacts
3. test one variable at a time
4. separate local proof from live proof
5. write the result down before context compaction destroys continuity

## Primary sources used for this reconstruction

- `docs/2026-3-14/PRIME_TEST_METHOD.md`
- `docs/2026-3-14/BURN_RATE_ROOT_CAUSE_AND_PRIME_GOLDEN_TESTS.md`
- `internal/glass/prime_golden_test.go`
- `internal/glass/compression_golden_test.go`
- `text-GLASS-BACK2RESEARCH-8.md`
- `text-GLASS-BACK2RESEARCH-9.md`
- `analysis/snippet_backup_diffs.txt`
- `analysis/threshold_replay_comparison.txt`
- `analysis/compression_off_replay.txt`
- `analysis/interleaved_replay_results.txt`

## Phase 1: PRIME-GOLDEN discipline

The original PRIME-GOLDEN method was a test-first falsification loop.

Its structure was:

1. Hypothesis
   State one causal claim clearly enough that it can fail.

2. Baseline
   Record the current behavior before changing anything.

3. Simulation
   Build a focused reproduction or golden test that isolates the claim.

4. Metrics
   Decide in advance what will count as success or failure.

5. Comparison
   Compare the modified and unmodified paths against the same scenario.

6. Verdict
   Mark the claim as confirmed, contradicted, or still unresolved.

The surviving Go tests show this was not just prose. It became executable.

Key surviving test families:

- `TestPrimeGolden_BookmarkRoleCollision`
- `TestPrimeGolden_PrefixStabilityUnderEviction`
- `TestPrimeGolden_ConcurrentInterleavingRisk`
- `TestPrimeGolden_OrphanSanitizerBreakpointDrift`
- `TestPrimeGolden_FactOverlayCacheSafety`
- `TestPrimeGolden_EvictedSessionViewSizeBound`
- `TestPrimeGolden_ReingestBlocking`
- `TestPrimeGolden_BookmarkOnlyAnchor`

And the later compression suite extended the same spirit:

- selective stripping
- idempotency
- summary injection
- saturation detection
- endurance
- watermark prefix stability
- full lifecycle and integration checks

## Phase 2: Late-March replay-and-diff discipline

By March 25, the methodology matured into an artifact-first replay workflow.

The recovered tier order is:

1. Snippet backup diffs
   Treat snippet backups as incident-period version control.
   Before trusting any narrative, diff what actually changed.

2. Replay real captured fixtures
   Use real captured request sequences, not only synthetic single-loop tests.

3. Run config/code variants one at a time
   If the replay tool lacks a flag, rebuild variant binaries.
   Do not declare a variant "tested" because the tooling is inconvenient.

4. Compare interleaved vs sequential replay
   Aggregate DB reasoning is not a substitute for replaying actual ordering.

5. Keep local and live claims separate
   A passing local replay or unit test is not the same as a live production proof.

## The recovered tiered workflow

### Tier 0: Ground-truth inventory

Establish what evidence exists before theorizing.

Typical sources:

- replay fixtures
- DB rows
- shadow artifacts
- prefix-event logs
- snippet backups
- surviving binaries

### Tier 1: Diff the code that actually ran

Start with snippet backup diffs.

Questions answered here:

- what changed on the "golden" day
- what changed on the regression day
- whether a later agent attached the wrong feature to the wrong incident

This tier already corrected several false narratives:

- March 18 "golden" behavior was not about the later breakpoint threshold constant
- March 24 did include `breakpointAdvanceThreshold = 8 -> 80`
- `RawPrevBreakpointAnchor` was added after that regression window

### Tier 2: Replay current captures under controlled variants

Use the same captured fixtures while changing one variable at a time.

Examples already executed in the surviving artifacts:

- threshold variants
- compression on vs off
- interleaved vs sequential order

This tier answers:

- whether a candidate fix moves the replay outcome materially
- whether the dominant effect is local to one variable or not

### Tier 3: Promote only durable results

A result moves upward only if it survives:

- artifact check
- replay or test reproduction
- comparison against alternatives

Everything else stays provisional.

## Rules that repeatedly mattered

### 1. Never stack changes when isolating a cause

When multiple edits land together, the causal story becomes unreliable.
The method requires one variable at a time wherever possible.

### 2. Raw artifacts outrank elegant explanations

If a transcript says one thing and the snippet diff says another, the diff wins.

### 3. Synthetic tests are useful but incomplete

The March 25 research correctly identified a blind spot:

- single-loop synthetic tests can miss inter-request cache gaps

So synthetic tests are necessary, but not sufficient.

### 4. Local success does not equal live closure

This exact mistake recurred:

- code implemented
- focused tests pass
- report says "done"
- live validation still missing

The recovered method forbids collapsing those stages together.

### 5. Time-sensitive counts must keep their dates

Fixture counts, session counts, and corpus composition can drift.
Date-stamp inventory facts instead of treating them as timeless truths.

## What this means for future work

When a new cache-break claim appears, the correct order is:

1. find the relevant transcript window
2. find the snippet backups for that window
3. find the replay fixtures/logs/DB rows for that window
4. write the hypothesis
5. run one focused reproduction
6. compare variant A vs variant B
7. record the verdict to disk immediately

## What does not count as proof anymore

- "the agent remembers doing it"
- "a report says it was fixed"
- "the tests passed once" without saying which tests
- "the DB average suggests it" without replaying the actual sequence
- "the code looks right now" without reconstructing what changed when

## Short form

The recovered method is:

- diff first
- replay second
- vary one thing at a time
- separate local from live
- persist every finding immediately
