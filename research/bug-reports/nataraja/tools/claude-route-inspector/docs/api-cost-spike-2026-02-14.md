# API Cost Spike Analysis (Opus 4.6) — 2026-02-14

Date: 2026-02-14

This note explains why first‑party API spend on `claude-opus-4-6` was high even after the cache-break fixes, and why the “last ~$25” depleted faster than earlier blocks.

## Data Sources (Ground Truth)

- Fingerprint metrics DB: `/home/user/.claude/fingerprint.db`
  - Tables used: `request_events_v2`, `request_prefix_hashes_v2`
- Thinking + output quality audit DB: `/home/user/.claude-audit/thinking_audit.db`
  - Table used: `audit_samples` (joins to `request_events_v2` by `request_id`)
- CLI debug logs (forked agents only): `/home/user/.claude/debug/*.txt`
  - Files observed for today’s sessions: `4f5eca31-*.txt`, `e9280882-*.txt`, `8cdab125-*.txt`, `856d8d95-*.txt`, `9851ade5-*.txt`
- Session cost snapshots captured in repo: `API_COST.md`
- Investigation writeups used as interpretation context:
  - `tools/claude-route-inspector/docs/quota-investigation-summary.md`
  - `tools/claude-route-inspector/docs/claude-code-cache-break-study.md`
  - `docs/2026-2-12/cache-break-investigation-findings.md`
  - `docs/2026-2-12/PRIME-investigation-complete.md`
  - `docs/2026-2-13/05-operator-implementation-and-root-cause-isolation-plan.md`
  - `docs/2026-2-13/interleaving-and-burn-detectors.md`
  - `docs/2026-2-13/01-contradiction-matrix.md`

## “Why Is Spend Still High Today?” (Main Finding)

On 2026-02-14 (UTC), the system is *not* primarily failing due to cache breaks. It’s expensive because it is doing **a high number of Opus calls** against **a large cached context**.

From `/home/user/.claude/fingerprint.db` (`request_events_v2`) for 2026-02-14:

- Calls: `861` (all `claude-opus-4-6`)
- Sum tokens:
  - `cache_read_tokens`: `98,756,066` (dominant)
  - `cache_creation_tokens`: `3,128,429`
  - `output_tokens`: `374,672`
  - `input_tokens`: `2,738` (negligible)
- Avg per call:
  - `cache_read_tokens/call`: ~`114,699` (meaning ~115K tokens are re-read every request)
  - `cache_creation_tokens/call`: ~`3,633`
  - `output_tokens/call`: ~`435`
- Cache efficiency:
  - `AVG(cache_efficiency)`: `99.419%`

### Cost Breakdown (Estimated From Tokens)

Using Opus caching rates (per existing writeups / CLI summaries):

- Cache read: ~$0.50 / 1M tokens
- Cache creation (write): ~$6.25 / 1M tokens
- Output: ~$25.00 / 1M tokens
- Input (uncached): ~$5.00 / 1M tokens

Estimated 2026-02-14 cost for these 861 main calls:

- Cache read: `98.756M * 0.50` ≈ **$49.38**
- Cache creation: `3.128M * 6.25` ≈ **$19.55**
- Output: `0.375M * 25` ≈ **$9.37**
- Input: `0.0027M * 5` ≈ **$0.01**

Total ≈ **$78.31** (main calls only, from 11:24 to 16:17 UTC).

This means that even with a *very healthy cache hit rate*, the workload can burn ~$15–$25/hour if:

- you make hundreds of requests/hour, and
- each request re-reads ~100K+ cached tokens.

## “Why Did the Last ~$25 Drain Faster?”

The last ~$25 aligns with a later session window with materially higher **throughput** (more calls/min), driven by higher **in-flight concurrency** (“interleaving”).

### Session Comparison (Same Day)

Session `20260214_112159` (11:24→13:15 UTC):

- Calls: `308`
- Duration: `110.6` minutes
- Est. cost: **$28.35**
- Est. cost/hour: **$15.38/hr**
- Avg in-flight: `5.81`
- Cache read tokens/min: ~`280,804`

Session `20260214_145700` (15:14→16:17 UTC):

- Calls: `279`
- Duration: `63.3` minutes
- Est. cost: **$24.30**
- Est. cost/hour: **$23.02/hr**
- Avg in-flight: `7.35` (max observed `53`)
- Cache read tokens/min: ~`508,112` (≈1.81× higher than earlier)

**Root cause of “faster last $25”:**
More concurrency → more API calls per wall-time → more cached-context re-reads per minute → more $/hour.

## What We Solved vs What’s Still Driving Burn

### Solved (Major)

The Feb 8–9 fixes eliminated the huge cache-break amplification where *most calls* recreated a ~100K+ prefix:

- Random proxy transforms (system prompt instability)
- MCP tool flapping (tool list instability)
- Non-idempotent trimming (message instability)
- ToolSearch experiment-induced tool churn
- Statsig/feature-flag drift effects via local pinning (partial)

See:
- `tools/claude-route-inspector/docs/claude-code-cache-break-study.md`
- `tools/claude-route-inspector/docs/quota-investigation-summary.md`

### Still Present (Primary for Today’s API Spend)

1. **High call volume**
   - 861 Opus calls in ~5 hours is “agent loop” scale.
   - Each tool-use step is a billable model call, even if cached.

2. **Large cached context size**
   - ~115K tokens re-read per call is expensive at scale even at 0.1× price.

3. **Interleaving / concurrency pressure**
   - `interleaving_detected=1` for 831/861 calls today
   - Avg in-flight ~6.34; peak 53
   - This is the best-explaining driver for why cost/hour increased later in the day.

4. Residual big breaks still exist, but are not the dominant cost driver today
   - Today: 20 calls had `cache_creation_tokens >= 50k` (2.3%).
   - Those 20 calls account for ~71% of *all* cache creation tokens today (2.218M of 3.128M).

## Reasoning/Quality Degradation Signals (Not Just Tokens)

From `/home/user/.claude-audit/thinking_audit.db` on 2026-02-14:

- Total samples: 869
- Avg `sycophancy_score`: `0.083`
- High-sycophancy events (`>=0.5`): `51` (5.9%)

Quality degraded sharply in the late window that also burned money faster:

- By hour (UTC), average `sycophancy_score` jumped to `0.152` at 15:00 with 31 high-syc events in that hour.
- By session, `20260214_145700` had avg sycophancy `0.131` with `40` high-syc events, vs `20260214_112159` avg `0.067` with `4` high-syc events.

User interruption is a strong correlate:

- Samples containing “Request interrupted by user”: avg sycophancy `0.304` vs `0.074` otherwise.

Interpretation:
- The best predictors of degraded behavior today are **interruption + interleaving pressure**, not raw “thinking budget” (which is constant at 31,999 today and not a major cost driver vs cache read).

## Forked-Agent Overhead (Prompt Suggestions)

CLI debug logs show `Forked agent [prompt_suggestion] ... totalUsage: ...` lines that are *not represented* in `request_events_v2`.

Across the five session debug logs above (today only):

- totalUsage lines: 59
- totals: `cacheRead=6,187,399`, `cacheCreate=39,030`, `output=12,538`, `input=408`
- estimated Opus-equivalent cost: **~$3.65**

This helps explain why invoice spend can exceed the “main calls” estimate.

## Actionable Takeaway (If Goal Is Lower $/Hour)

If you want API spend to drop materially, caching fixes are necessary but not sufficient.
The dominating levers are:

1. Reduce concurrency (cap in-flight) to reduce calls/minute.
2. Reduce call volume (batch tool actions; fewer tool-use loops).
3. Reduce context length carried forward (earlier compaction/summarization; smaller tool outputs).
4. Use cheaper models for “tool orchestration” phases and reserve Opus for synthesis.

The operator plan already anticipated this failure mode:
`docs/2026-2-13/05-operator-implementation-and-root-cause-isolation-plan.md` section 2.

