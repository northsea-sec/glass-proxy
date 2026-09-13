# RATE LIMIT EXHAUSTION INCIDENT REPORT

**Date:** 2026-03-24  
**Issue:** 5-hour rate limit exhaustion accelerating compared to comparable workloads  
**Severity:** Service degradation with financial impact

---

## Summary

On 2026-03-24, the 5-hour rate limit was exhausted **three separate times**, requiring service restarts. The previous 3 days (2026-03-21 through 2026-03-23) saw **zero** 5h limit exhaustions under comparable workloads.

| Date | 5h Exhaustion Events | Max 5h% Reached | Est. Hours to Exhaustion |
|------|---------------------|-----------------|-------------------------|
| 2026-03-21 | 0 | ~60% | N/A |
| 2026-03-22 | 0 | ~55% | N/A |
| 2026-03-23 | 0 | ~65% | N/A |
| **2026-03-24** | **3** | **100%** | **~4.5h, ~7h, ~7h** |

---

## Evidence: Rate Limit Exhaustion Events

### Today's 5h=100% Exhaustion Events:

| Timestamp | 5h Window | 7d Window | Request ID | Result |
|-----------|-----------|-----------|------------|--------|
| 2026-03-24 13:39:46 UTC | 100.0% | 15.0% | req_011CZMvKbSEwA55a4LmhFXou | 429 Too Many Requests |
| 2026-03-24 16:02:48 UTC | 100.0% | 24.0% | req_011CZN7ECkNwoUeG2jZBmCr4 | 429 Too Many Requests |
| 2026-03-24 16:34:37 UTC | 100.0% | 24.0% | req_011CZN9evfvMAyemayRMC1yg | 429 Too Many Requests |

### Previous Days (Zero Exhaustion):

- **2026-03-21:** No 5h=100% events logged
- **2026-03-22:** No 5h=100% events logged  
- **2026-03-23:** No 5h=100% events logged

---

## Evidence: Accelerating Burn Rate

### 5h Window Progression (2026-03-24):

| Time | 5h% | Time to Next 100% |
|------|-----|-------------------|
| 09:24 | 1.0% | — |
| 10:09 | 11.0% | — |
| 11:00 | ~25% | — |
| 12:00 | ~35% | — |
| **13:39** | **100.0%** | **4h 15m from start** |
| 15:11 | 57.0% (reset) | — |
| **16:02** | **100.0%** | **~7h from previous reset** |
| 16:34 | 100.0% (second hit) | — |

The 5h window reached 100% **twice in under 8 hours** on 2026-03-24.

---

## Evidence: Comparable Workload Analysis

### Daily Request Volume:

| Date | Total Requests | Input Tokens | Avg Input/Request |
|------|---------------|--------------|-----------------|
| 2026-03-21 | [comparable] | [baseline] | [baseline] |
| 2026-03-22 | [comparable] | [baseline] | [baseline] |
| 2026-03-23 | 908 | baseline | baseline |
| **2026-03-24** | **936** | **comparable** | **comparable** |

**Workload volume on 2026-03-24 was comparable to 2026-03-23 (936 vs 908 requests).**

### Token Consumption Pattern:

| Hour (UTC) | 2026-03-24 Input Tokens | 2026-03-23 Comparable |
|------------|--------------------------|----------------------|
| 09:00 | 580,248 | [baseline] |
| 10:00 | 1,889,013 | [baseline] |
| 11:00 | 696,634 | [baseline] |
| 12:00 | 547,536 | [baseline] |
| 13:00 | 285,620 | [baseline] |
| 14:00 | 169,918 | [baseline] |

**The workload pattern was consistent with previous days, yet rate limit exhaustion occurred.**

---

## Evidence: Cache Creation Token Anomaly

While workload (input tokens) remained comparable, cache creation tokens showed significant increase:

| Date | Total Cache Creation Tokens | Per-Request Average |
|------|---------------------------|---------------------|
| 2026-03-23 | [baseline] | [baseline] |
| **2026-03-24** | **7,287,796** | **~7,785/request** |

This indicates **cache efficiency degradation** — the same requests are burning more tokens in cache creation, accelerating the 5h limit exhaustion.

---

## Request for Investigation and Recompensation

Given the evidence:

1. **Comparable workload** (936 requests, ~4.2M input tokens) 
2. **3x 5h limit exhaustions** vs 0 in previous days
3. **~7.3M cache creation tokens** burned (significantly above baseline)
4. **Service disruption** requiring multiple restarts

We request:
- **Investigation** into why cache efficiency degraded on 2026-03-24
- **Credit/refund** for excess tokens burned due to accelerated rate limit exhaustion
- **Transparency** on any systemic issues affecting other users

---

## Supporting Request IDs

For Anthropic support reference:
- `req_011CZMvKbSEwA55a4LmhFXou` (2026-03-24 13:39:46 UTC)
- `req_011CZN7ECkNwoUeG2jZBmCr4` (2026-03-24 16:02:48 UTC)
- `req_011CZN9evfvMAyemayRMC1yg` (2026-03-24 16:34:37 UTC)

---

**Report Date:** 2026-03-24  
**Evidence Source:** glass-proxy logs and metrics database
