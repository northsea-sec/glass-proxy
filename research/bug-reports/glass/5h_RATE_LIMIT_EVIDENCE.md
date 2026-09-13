# 5h Rate Limit Acceleration Evidence

**Date:** 2026-03-24

---

## The Claim

The 5-hour rate limit is being exhausted significantly faster today than under comparable workloads on previous days.

---

## Evidence 1: Workload Comparison

| Date | Total Requests | Input Tokens | Output Tokens |
|------|---------------|--------------|---------------|
| 2026-03-23 | 908 | 3,402,759 | [baseline] |
| 2026-03-24 | 936 (+3.1%) | 4,169,220 (+22.5%) | [comparable] |

**Workload increased by only 3-23%, not enough to explain 3x exhaustion events.**

---

## Evidence 2: Rate Limit Exhaustion Events

| Date | Times 5h=100% Reached | First Exhaustion Time | 7d% at Exhaustion |
|------|----------------------|----------------------|-------------------|
| 2026-03-21 | 0 | N/A | N/A |
| 2026-03-22 | 0 | N/A | N/A |
| 2026-03-23 | 0 | N/A | N/A |
| **2026-03-24** | **3** | **~4.5 hours after start** | 15-24% |

---

## Evidence 3: Rate Limit Progression (2026-03-24)

Log entries showing 5h window filling:

| Time (UTC) | 5h% | 7d% |
|------------|-----|-----|
| 09:24:19 | 1% | — |
| 10:09:42 | 11% | — |
| 11:00:00 | ~25% | — |
| 12:00:00 | ~35% | — |
| **13:39:46** | **100%** | **15%** |
| 15:11:09 | 57% (reset) | 20% |
| **16:02:48** | **100%** | **24%** |
| 16:34:37 | 100% | 24% |

**Time to first exhaustion: ~4.5 hours**
**Second exhaustion: ~51 minutes after reset**

---

## Evidence 4: 429 Error Log Entries

```
2026/03/24 13:39:46 [UPSTREAM] 429 Too Many Requests
2026/03/24 16:02:48 [UPSTREAM] 429 Too Many Requests  
2026/03/24 16:34:37 [UPSTREAM] 429 Too Many Requests
```

---

## Evidence 5: Cache Efficiency Comparison

| Date | Cache Creation Tokens | Cache Read Tokens |
|------|----------------------|-------------------|
| 2026-03-23 | 5,698,495 | 39,026,052 |
| **2026-03-24** | **7,287,796** (+28%) | **81,640,102** (+109%) |

**Same workload burning 28% more cache creation tokens.**

---

## Conclusion

With comparable workload (936 vs 908 requests), the 5h rate limit was exhausted **3 times on 2026-03-24** versus **0 times on 2026-03-23**.

The time-to-exhaustion dropped from "never" to **4.5 hours** for first exhaustion, then **51 minutes** for second.

---

## Request IDs for Reference

- req_011CZMvKbSEwA55a4LmhFXou (13:39:46 UTC)
- req_011CZN7ECkNwoUeG2jZBmCr4 (16:02:48 UTC)
- req_011CZN9evfvMAyemayRMC1yg (16:34:37 UTC)
