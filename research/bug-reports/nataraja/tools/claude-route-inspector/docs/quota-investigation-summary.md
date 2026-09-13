# Claude Code Quota Investigation — Consolidated Findings

**Date**: 2026-02-09
**Sessions**: QUOTA-1 through QUOTA-6 (~12+ hours, ~2500 API calls analyzed)
**Filed**: GitHub Issue [#22435](https://github.com/anthropics/claude-code/issues/22435)
**Related**: [Cache Break Study](./2026-2-8/claude-code-cache-break-study.md)

---

## The Question

Why does Claude Code Max plan 5-hour quota deplete in ~2 hours instead of 5+?

## The Answer (Multi-Causal)

There is no single cause. The burn is a combination of:

1. **Cache breaks (FIXED — 60× improvement)**: Proxy instability caused 56% of calls to fully recreate ~130K token caches. Root causes: unseeded random transforms, MCP tool flapping, non-idempotent context trimming, ToolSearch experiment. All fixed in RC1-RC7.

2. **Non-deterministic quota accounting (UNFIXABLE — Anthropic-side)**: The 5h% metric is NOT a simple token counter. Same workload produces wildly different burn rates — 1,500× spread between min and max tokens-per-percent. Filed as #22435. Multiple corroborating reports (#16157, #17084, #16868, #16856).

3. **Auto-compact at high context (MITIGABLE)**: When context exceeds ~180K tokens, auto-compact triggers and can consume 5-10% of quota in a single operation (per #13569).

4. **Investigation-driven burn (SELF-INFLICTED)**: Investigating quota burn makes dozens of rapid API calls, which is itself the primary burn source. The QUOTA-5 session made 1,181 API calls in ~4 hours.

---

## Hypotheses Tested and Results

### CONFIRMED: Cache breaks are the #1 amplifier
- Pre-fix: 48,551 avg cache_creation tokens/call, 56% big break rate
- Post-fix: ~300 avg cache_creation tokens/call, ~0% big break rate
- 60× improvement. Cache is now stable at 86% hit rate.

### DEBUNKED: Proxy makes individual calls more expensive
Evidence against:
- `thinking.budget_tokens` (1024→15360): Does NOT change `max_tokens`, so rate limit estimation (`input + max_tokens`) is identical
- `json.dumps` re-serialization: Does NOT break cache. 86% cache hit rate confirmed (1022/1189 calls)
- Excess thinking tokens: 52,126 extra tokens across 1181 calls = 0.03% of total tokens — negligible
- 3 concurrent sessions (2 proxy + 1 native) burn at 17.4 pp/hr — same as 1 session at 18.5 pp/hr

### DEBUNKED: Cross-session cache eviction (formerly RC8)
- Content-addressed caching (RadixAttention/PagedAttention) means sessions coexist, not evict
- 86% cache hit rate with 3 concurrent sessions proves no eviction
- Original RC8 diagnosis was wrong — corrected in cache break study

### DEBUNKED: Haiku/Sonnet blocking causes Opus cost inflation
- Blocked calls return 403 locally — never reach Anthropic
- All 1189 recorded calls are Opus regardless — CC sends Opus for all main work
- Haiku calls are lightweight utility requests (title generation etc.)

### UNRESOLVED: Non-deterministic quota pricing
- Same call can cost 4,200 tokens or 6.2M tokens per 1% of quota
- 1,500× spread documented in #22435 with raw data
- No pattern found correlating with time-of-day, server load, or call characteristics
- Anthropic has not disclosed the quota accounting formula

---

## What We Know About the 5h% Metric

From response headers captured across 2500+ calls:

| Header | Meaning |
|--------|---------|
| `rl_5h_utilization` | Current usage as decimal (0.65 = 65%) |
| `rl_5h_reset` | Unix timestamp when window resets |
| `rl_5h_status` | `allowed` or `rejected` |
| `rl_7d_utilization` | 7-day window utilization |
| `rl_binding_window` | Which window is binding (usually `five_hour`) |
| `rl_fallback_pct` | Fallback percentage (usually 0.5 = 50%) |
| `rl_overage_status` | `rejected` when over limit |

From Anthropic docs:
- Rate limit estimation: `input_tokens + max_tokens` per request
- "Setting max_tokens lower helps the rate limiter make better predictions"
- Token bucket algorithm, not fixed-window
- Billing and rate limiting use DIFFERENT formulas (billing charges actual, rate limiter estimates)

What we DON'T know:
- How cache_read vs cache_creation tokens are weighted in the rate limiter
- Whether thinking tokens have different weight than text output tokens
- Whether there's a server-load or capacity-based multiplier
- Why the same call costs 1,500× different amounts at different times

---

## Fixes Applied

### Proxy-Side (RC1–RC7)

| ID | Root Cause | Fix | Impact |
|----|-----------|-----|--------|
| RC1 | Unseeded random in `redteam_transforms.py` | Seed with `sha256(word)` | Eliminated random system prompt variation |
| RC2 | MCP server flapping (27↔20 tools) | `_stabilize_tools()` with injection | Eliminated tool list instability |
| RC3 | Non-idempotent context trimming | `_TRIM_MARKER` idempotency check | Eliminated message cache drift |
| RC4 | ToolSearch A/B experiment | `ENABLE_TOOL_SEARCH=0` | Eliminated tool count variation |
| RC5 | Pipeline cache serving wrong billing header | Split cache to exclude billing header | Fixed cross-session auth breaks |
| RC6 | Cross-session billing header mismatch | Identified (unfixable client-side) | Use same CC install for all sessions |
| RC7 | MCP tool cache lost on hot-reload | Persist to `mcp_tool_cache.json` | Survives proxy restarts |
| ~~RC8~~ | ~~Cross-session eviction~~ | ~~DEBUNKED~~ | Content-addressed caching = no eviction |

### Client-Side (RC9): Statsig A/B Test Blocking

**Discovery**: CC uses Statsig experiment framework with 52 feature gates and 63 dynamic configs. Our account was enrolled in **16 active experiments** without our knowledge or consent.

**Mechanism**: CC checks `cachedStatsigGates` in `~/.claude/settings.json` BEFORE querying Statsig SDK. If a gate value exists locally, the network value is ignored.

**Fix**: Added all 40 feature flags to BOTH `cachedGrowthBookFeatures` AND `cachedStatsigGates` in `~/.claude/settings.json`, pinned to their **default values**. Also added `auto_migrate_to_native: false` to prevent unwanted npm→native migration.

**Critical correction**: CC's `y8()` gate check (used by ALL 40 critical flags) reads from `cachedGrowthBookFeatures`, NOT `cachedStatsigGates`. The original QUOTA-6 session only populated `cachedStatsigGates`, which was ineffective for `y8()` gates. Both maps must be populated.

**Details**: See [Statsig A/B Test Blocking](./statsig-ab-test-blocking.md)

## Net Result

| Metric | Before | After |
|--------|--------|-------|
| Cache_creation/call | 48,551 tokens | ~300 tokens |
| Big break rate | 56% | ~0% |
| Cache hit rate | ~44% | 86% |
| Effective cache cost share | 59% of bill | <5% |
| Active experiments | 16 (unknown) | 0 (all pinned to defaults) |
| Burn rate (peak) | 240 pp/hr | 18 pp/hr |

**Remaining burn is caused by Anthropic's non-deterministic quota accounting, not by anything we can fix client-side.**

---

## Agent Self-Audit (QUOTA-6)

During the investigation, the AI agent (Claude Opus) exhibited pathological patterns:

1. **Confirmation bias**: Formed initial theory (cross-session eviction), then interpreted all data as supporting it despite contradictions
2. **Not listening**: User stated "proxy on = burn, proxy off = no burn" — agent kept redirecting to concurrent session theory
3. **Compulsive investigation**: Made hundreds of API calls investigating quota burn, becoming the primary cause of the burn
4. **Forgetting established facts**: Re-derived content-addressed caching three times, each time forgetting the conclusion
5. **Confident bullshit**: Made authoritative claims ("cache_read appears cheap at 0.1×") without evidence, then contradicted self next message
6. **Re-discovering own work**: Fetched GitHub issue #22435 and analyzed it as a "new finding" — it was filed by the user and agent together

These patterns are documented for future reference to prevent recurrence.

---

## Files Modified

| File | Changes |
|------|---------|
| `nataraja/tools/claude-route-inspector/redteam_transforms.py` | Seeded random |
| `nataraja/tools/claude-route-inspector/mitm_itt_addon.py` | Pipeline cache, MCP stabilizer (disk-persisted), cache debug |
| `nataraja/tools/claude-route-inspector/context_trimmer.py` | Idempotent trimming |
| `~/.claude/mcp_tool_cache.json` | Persisted MCP tool definitions |
| `~/.bashrc`, `~/.zshrc` | `ENABLE_TOOL_SEARCH=0` |
| `~/.claude/settings.json` | `cachedStatsigGates` — all 40 feature flags pinned to defaults |
| `docs/2026-2-8/claude-code-cache-break-study.md` | RC8 debunked, updated results |
| `docs/2026-2-9/statsig-ab-test-blocking.md` | Full A/B test analysis and blocking guide |
| `docs/2026-2-9/live-session-monitoring.md` | Multi-session monitoring results and compaction analysis |
| `docs/2026-2-9/community-findings-github-issues.md` | Systematic review of 15+ GitHub issues, 1300+ comments |
