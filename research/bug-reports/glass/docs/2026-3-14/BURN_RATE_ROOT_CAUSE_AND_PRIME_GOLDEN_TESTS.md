# Burn Rate Root Cause Analysis + PRIME-GOLDEN Test Suite
## Date: 2026-03-14 15:00 UTC+1

---

## PART 1: 40 pp/hr Burn Rate — Root Cause

### The Smoking Gun

At 14:56:31, the main session's `cache_read` dropped from **154,039 → 18,804** (lost 135K tokens) with `cache_create=114,113`. This single event represents **$0.43 in cache_create costs** and forced a near-complete prefix rebuild.

### Direct Cause: Subagent Interleaving Destroys Anthropic-Side KV Cache

The pattern is unambiguous:

```
14:54:49  MAIN  cr=154039  cc=0        ← fully warmed, 154K cached prefix
14:55:25  SUB   cr=0       cc=14444    ← COLD subagent, creates NEW prefix (evicts main!)
14:55:29  MAIN  cr=154039  cc=0        ← survived this time (2 cache slots?)
14:56:09  SUB   cr=14771   cc=0        ← sub hits its own cache
14:56:31  MAIN  cr=18804   cc=114113   ← CATASTROPHIC: 135K tokens evicted
```

The subagent (conv `e0a2ba8019e9_77556`, type `small_system`, 114 chars) has a **completely different system prompt** than the main session (~14.5K chars). This creates a different cache prefix on Anthropic's side. When Anthropic's LRU cache has ~2-3 concurrent prefix slots (per CACHE_KV_PREFIX.md), the subagent's different prefix evicts the main session's slot.

### Evidence Chain

| Evidence | Source | Finding |
|----------|--------|---------|
| 3 subagent cold starts (cr=0) | STREAM log | Subagent creates fresh cache entries competing with main |
| Main cr drop 154K→18K | STREAM log | Main session's Anthropic-side cache was evicted |
| 114-char system prompt on sub | SER log | Completely different prefix hash from main |
| cc=114113 rebuild | STREAM log | Near-full prefix rebuild at $3.75/Mtok write cost |
| COLD-GATE WARMING | GLASS log | First request after eviction triggered warming |
| warmer=true on evicted session | GLASS log | Session had evicted_total=18, using pinned frame |

### Cost Breakdown: Current Session (14:43-14:58, ~15 minutes)

| Category | Tokens | Cost | % of Total |
|----------|--------|------|------------|
| Input (uncached) | 376,552 | $5.65 | 40.1% |
| Cache_read | 3,904,447 | $5.86 | 41.6% |
| Output | 19,056 | $1.43 | 10.2% |
| Cache_create | 303,756 | $1.14 | 8.1% |
| **TOTAL** | | **$14.07** | |
| **Projected hourly** | | **$56.28** | |

Of the $1.14 cache_create cost, **$0.43 (38%)** came from the single 114K rebuild event caused by subagent interleaving.

### Why This Matches Historical Findings

This is the **exact same pattern** documented across 7+ postmortems:

| Document | Key Finding | Relevance |
|----------|-------------|-----------|
| CACHE_KV_PREFIX.md | "subagent calls with different system prompts create different cache entries that compete" | Exact match |
| POSTMORTEM-FEB23 | "Anthropic supports ~2-3 concurrent cached prefixes. When a 4th arrives, LRU evicted" | Explains the eviction |
| RESEARCH-INTERLEAVING-FIX | "each session switch forces a cache rebuild" | Confirmed by our data |
| POSTMORTEM-FEB19 | "Cache breaks have 4x write multiplier" | 114K × $3.75/M = $0.43 |
| INSIGHT-DIAMOND11 | "CC removes cache_control from prev assistant, breaking prefix" | Different mechanism, same effect |

### Root Cause: Glass-Proxy Lacks Session Serializer for Subagents

The nataraja/mitmproxy system had a `BatchAffinitySerializer` with `idle_timeout=15s` that serialized requests per session. Glass-proxy has a cold-gate mechanism but it only blocks when `newMsgsAdded == 0` (subagent replay pattern). The `small_system` subagents used by CC's Task() feature are NOT serialized — they go directly to Anthropic interleaved with the main session.

### Immediate Mitigations Available

1. **Strip cache_control from subagent requests** — prevents subagents from creating competing cache entries on Anthropic's side
2. **Hold subagent requests** — delay subagent API calls until no main-session request is in-flight (session affinity)
3. **Increase serializer idle_timeout** — if a serializer exists, tune it per POSTMORTEM-FEB23 findings (15s sweet spot)

---

## PART 2: Full Day Telemetry Dump

Files written to `/home/user/glass-proxy/docs/2026-3-14/`:

| File | Rows | Content |
|------|------|---------|
| `full_day_dump.csv` | 695 | All STREAM entries: timestamp, conv, model, tokens, cache, stop, timing |
| `full_day_events.csv` | 2231 | All GLASS state events: prefix changes, anchors, eviction, repairs |

### Daily Summary (3 proxy restarts, all logs combined)

| Metric | Value |
|--------|-------|
| Total requests | 695 |
| Input tokens | 2,702,531 ($40.54) |
| Output tokens | 261,460 ($19.61) |
| Cache read | 44,670,098 ($67.01) |
| Cache create | 4,824,155 ($18.09) |
| **Total cost** | **$145.24** |
| Cache efficiency | 90.3% |
| Unique conversations | ~8 |
| Subagent requests | ~60 |

---

## PART 3: Insights Research Summary

### Documents Read (insights2/ corpus)

| Document | Key Insight for Glass-Proxy |
|----------|---------------------------|
| **CACHE_KV_PREFIX.md** | Anthropic's cache is PREFIX-based, not conversation-based. All convs with same system+tools share ONE cache entry. Subagent prefixes compete with main. Strip cache_control from subagents to prevent them creating competing entries. |
| **INSIGHT-DIAMOND11** | CC removes `cache_control` from previous last assistant on every call, breaking ~55 bytes → prefix miss. Fix: re-add cache_control at previous position. Glass-proxy's thinking strip already helps here. |
| **POSTMORTEM-FEB19** | Three-layer context control stack (truncation → supersede → dropping). Watermark is THE cache boundary. "messages[0:N] must be BYTE-IDENTICAL between watermark advances." Any optimization causing a break must save >40× prefix_size to break even. |
| **POSTMORTEM-FEB21** | Stage 2 death spiral: drops are temporary (CC re-sends full history), orphan cascade amplifies damage, bridge oscillation from multi-instance. Token-weighted dropping + bridge proximity gate + dangling tool_use detection. |
| **POSTMORTEM-FEB23** | Serializer `idle_timeout=15s` is Goldilocks value (A/B tested: 3s=90.9%, 15s=99.4%, 30s=85.2%). Break signature: `cr≈29,293` = system+tools only, all messages evicted. |
| **POSTMORTEM-FEB27** | Reconstruction deployed live → cache collapsed from 99% to 23% in 16 minutes. $4.25 wasted. Same bug as P4 DCP: per-call boundary changes break prefix. |
| **RESEARCH-INTERLEAVING-FIX** | At 4.9 calls/min, need 75 tok/call growth for 9hr sessions. 200K wall immovable. Session-affinity serializer is the recommended solution. |
| **CC_CACHE_REPORT** | Anthropic treats cache misses as production incidents. 4 breakpoint slots per request. Hash-based prefix matching. No raw text stored in cache. |

### Critical Rules (from corpus, applicable to Glass-Proxy)

1. **Cache Stability Invariant**: `messages[0:N]` must be BYTE-IDENTICAL between breakpoint advances
2. **40:1 Break Cost**: Any optimization causing a break must save >40× prefix_size tokens
3. **cr≈29K = Full Rebuild**: System+tools only survived; all message cache evicted
4. **Subagent Cold Starts**: Different system prompt → different prefix → LRU eviction of main
5. **Bridge=none Guard**: Never fire Stage 2 on estimated-only data; wait for real bridge
6. **15s Serializer Idle Timeout**: A/B tested optimal for 3 concurrent sessions
7. **NEVER deploy per-call boundary changes live**: P4 DCP and reconstruction both proved this

---

## PART 4: PRIME-GOLDEN Test Suite

Seven tests implementing the PRIME methodology: **Hypothesis → Baseline → Simulation → Metrics → Comparison → Verdict**.

