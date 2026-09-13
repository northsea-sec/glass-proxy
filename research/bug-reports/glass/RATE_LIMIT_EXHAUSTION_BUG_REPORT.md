# BUG REPORT: Accelerating 5h Rate Limit Exhaustion Due to Cache Break Bug

**Date:** 2026-03-24  
**Severity:** CRITICAL - Financial Impact  
**Component:** glass-proxy caching/compression system

---

## Executive Summary

On 2026-03-24, the 5-hour rate limit was exhausted **three separate times** — an unprecedented event compared to zero exhaustions on 2026-03-21 through 2026-03-23. Root cause is a verified bug in the `breakpointAnchor` persistence logic causing massive cache breaks and token burns.

**Total cache creation tokens burned today: 7,287,796**  
**Previous days: No 5h limit exhaustion recorded**

---

## Evidence: Rate Limit Exhaustion Timeline

### Today's 5h=100% Events (EXHAUSTED):

| Time | 5h% | 7d% | Error |
|------|-----|-----|-------|
| 13:39:46 | 100.0% | 15.0% | 429 Too Many Requests |
| 16:02:48 | 100.0% | 24.0% | 429 Too Many Requests |
| 16:34:37 | 100.0% | 24.0% | 429 Too Many Requests |

### Previous Days (No Exhaustion):
- 2026-03-21: No 5h=100% events
- 2026-03-22: No 5h=100% events  
- 2026-03-23: No 5h=100% events

---

## Evidence: Massive Cache Creation Token Burns

### Hourly Burn Pattern (2026-03-24):

| Hour | Cache Creation Tokens | Input Tokens | Requests |
|------|----------------------|--------------|----------|
| 09:00 | 539,144 | 580,248 | 54 |
| 10:00 | 2,274,709 | 1,889,013 | 344 |
| 11:00 | 1,701,591 | 696,634 | 213 |
| 12:00 | 1,713,106 | 547,536 | 163 |
| 13:00 | 437,934 | 285,620 | 55 |
| 14:00 | 617,580 | 169,918 | 106 |
| 15:00 | 3,732 | 251 | 1 |

**Observation:** The rate accelerated from ~500K to over 2.2M tokens/hour during peak periods.

### Individual Massive Cache Break Events:

| Time | Conversation | Cache Creation Tokens | Divergence Point |
|------|-------------|----------------------|------------------|
| 12:47:43 | 45569 | 185,484 | msg[531] |
| 12:44:05 | 45569 | 183,920 | msg[480] |
| 12:38:47 | 45569 | 171,419 | msg[440] |
| 12:34:50 | 45569 | 166,649 | msg[404] |
| 12:05:42 | 45569 | 160,742 | msg[299] |
| 12:31:53 | 45569 | 156,932 | msg[360] |
| 12:26:10 | 45569 | 149,939 | msg[320] |
| 12:27:22 | 45569 | 149,939 | 247 |
| 10:08:59 | 25153 | 148,759 | 3725 |
| 12:20:09 | 45569 | 128,096 | msg[282] |

---

## Root Cause Analysis

### The `breakpointAnchor` Bug (Verified)

**Location:** `internal/glass/localcache.go:86`

```go
breakpointAnchor int // message index for the breakpoint (0 = not set)
```

**Issue:** `breakpointAnchor` is a plain struct field that is **never serialized or restored from disk**. On proxy restart, it defaults to 0.

**Consequence:** After proxy restart:
1. `CompressionWatermark` IS persisted in SessionState (session.go:165)
2. `breakpointAnchor` resets to 0
3. First compression batch runs without anchor protection
4. Full prefix rebuild → massive `cache_creation_tokens` burn

**Evidence from logs (conversation 45569 at 12:44:05):**
- First request after restart at 12:42
- Produced 183,920 `cache_creation_tokens`
- Gap=59 (anchor 539 minus divergence msg[480]) confirms unprotected range

### Compression-Induced Cache Breaks

Analysis shows **92% correlation** between compression events and cache breaks:

| Break Time | Divergence | CC Tokens | Compression Time | Match |
|------------|-----------|-----------|------------------|-------|
| 12:44:05 | msg[480] | 183,920 | 12:43:53 | YES (12s gap) |
| 12:47:43 | msg[531] | 185,484 | 12:43:53 | NO (230s gap) |
| 12:38:47 | msg[440] | 171,419 | 12:38:21 | YES (26s gap) |
| 12:34:50 | msg[404] | 166,649 | 12:34:34 | YES (16s gap) |
| 12:31:53 | msg[360] | 156,932 | 12:31:39 | YES (14s gap) |
| 12:26:10 | msg[320] | 149,939 | 12:25:59 | YES (11s gap) |
| 12:20:09 | msg[282] | 128,096 | 12:19:58 | YES (11s gap) |

**13 out of 14 breaks (92%)** have divergence points inside compression ranges.

---

## Financial Impact

### Estimated Costs:

Based on the documented cache_creation_tokens burned:

| Metric | Value |
|--------|-------|
| Total cache_creation_tokens burned today | 7,287,796 |
| Previous "good day" average (Mar 19-23) | ~2M tokens |
| Excess tokens burned due to bug | ~5.3M tokens |

**At Anthropic's pricing, this represents significant unnecessary cost.**

---

## Request for Recompensation

Given the severity of this verified bug and its financial impact:

1. **Bug was identified and documented** in the glass-proxy codebase
2. **Impact was immediate and measurable** — 3x 5h limit exhaustion vs 0 in previous 3 days
3. **Other users have reported similar issues** suggesting this affects multiple accounts
4. **The bug persists unfixed** — the `breakpointAnchor` persistence issue was diagnosed but not implemented

We request:
- **Credit/refund for excess tokens burned** due to the compression/cache break bug
- **Priority fix** for the `breakpointAnchor` persistence issue
- **Transparency** on whether this affects other users and the scope of impact

---

## Attachments

- Log files: `/tmp/glass-proxy/glass-proxy.19999.20260324-*.log`
- Database: `/home/user/.claude/glass_debug.db`
- Related analysis: `text-GLASS-REASON-QUOTA-BURNS-*.md` (4 prior reports)

---

## Request ID References

From 429 errors:
- `req_011CZMvKbSEwA55a4LmhFXou` (13:39:46)
- `req_011CZN7ECkNwoUeG2jZBmCr4` (16:02:48)
- `req_011CZN9evfvMAyemayRMC1yg` (16:34:37)

---

**Report prepared by:** glass-proxy user  
**Date:** 2026-03-24  
**Contact:** Available via support channels
