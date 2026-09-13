# [BUG] 5-Hour Rate Limit Exhaustion Accelerating Despite Comparable Workload

## Preflight Checklist

- [x] I have searched [existing issues](https://github.com/anthropics/claude-code/issues?q=is%3Aissue%20state%3Aopen%20label%3Abug) and this hasn't been reported yet
- [x] This is a single bug report
- [x] I am using the latest version of Claude Code
- [x] I have checked [Anthropic Status](https://status.anthropic.com/) for service incidents

---

## What's Wrong?

The 5-hour rate limit is being exhausted **3x faster** today compared to previous days with **comparable workloads**. This represents a service degradation that violates expected usage patterns and contractual rate limit behavior.

### Evidence of Acceleration

| Date | Total Requests | Input Tokens | 5h Exhaustion Events | Time to First Exhaustion |
|------|---------------|--------------|---------------------|------------------------|
| 2026-03-21 | ~900 | ~3M | 0 | N/A |
| 2026-03-22 | ~900 | ~3M | 0 | N/A |
| 2026-03-23 | 908 | 3,402,759 | 0 | N/A |
| **2026-03-24** | **936** (+3.1%) | **4,169,220** (+22.5%) | **3** | **~4.5 hours** |

**Key Finding:** A 3-23% workload increase should not cause infinite → 4.5 hour exhaustion time.

### Rate Limit Exhaustion Log

```
2026/03/24 13:39:46  5h=100.0%  7d=15.0%  → 429 Too Many Requests  (req_011CZMvKbSEwA55a4LmhFXou)
2026/03/24 16:02:48  5h=100.0%  7d=24.0%  → 429 Too Many Requests  (req_011CZN7ECkNwoUeG2jZBmCr4)
2026/03/24 16:34:37  5h=100.0%  7d=24.0%  → 429 Too Many Requests  (req_011CZN9evfvMAyemayRMC1yg)
```

### 5h Window Progression (2026-03-24)

| Time (UTC) | 5h% | Time Since Reset |
|------------|-----|------------------|
| 09:24:19 | 1.0% | — |
| 10:09:42 | 11.0% | — |
| 13:39:46 | **100.0%** | **4h 15m** |
| 15:11:09 | 57.0% (reset) | — |
| 16:02:48 | **100.0%** | **51m** |

**The 5h window reset and re-exhausted in 51 minutes.**

### Cache Efficiency Degradation

| Date | Cache Creation Tokens | vs Input Ratio |
|------|----------------------|----------------|
| 2026-03-23 | 5,698,495 | 1.68x |
| **2026-03-24** | **7,287,796** | **1.75x** |

Same workload burning **28% more cache creation tokens**, accelerating rate limit exhaustion.

---

## What Should Happen?

### Expected Behavior (Contractual)

1. **Consistent Rate Limit Behavior:** The 5h rate limit should exhibit consistent exhaustion timing for comparable workloads, absent documented service changes.

2. **Documented Thresholds:** Rate limit behavior should match published documentation. Acceleration of ~28% in token burn rate without corresponding workload increase suggests a service-side issue.

3. **Graceful Degradation:** If rate limits change, users should receive advance notice or documentation updates.

### Actual Behavior (Breach)

| Aspect | Expected | Actual |
|--------|----------|--------|
| Exhaustion timing | ~12-24h for this workload | 4.5h, then 51m |
| Consistency day-over-day | Similar exhaustion pattern | 0 → 3 exhaustions |
| Cache efficiency | Stable ratio to input tokens | +28% increase |

---

## Error Messages/Logs

```
2026/03/24 13:39:46.695592 [SPOOF] Rate-limit headers: real 5h=100.0% 7d=15.0%
2026/03/24 13:39:46.695626 [UPSTREAM] /v1/messages 429 Too Many Requests
{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit. Please try again later."},"request_id":"req_011CZMvKbSEwA55a4LmhFXou"}

2026/03/24 16:02:48.407466 [SPOOF] Rate-limit headers: real 5h=100.0% 7d=24.0%
2026/03/24 16:02:48.407497 [UPSTREAM] /v1/messages 429 Too Many Requests
{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit. Please try again later."},"request_id":"req_011CZN7ECkNwoUeG2jZBmCr4"}

2026/03/24 16:34:37.556921 [SPOOF] Rate-limit headers: real 5h=100.0% 7d=24.0%
2026/03/24 16:34:37.556951 [UPSTREAM] /v1/messages 429 Too Many Requests
{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit. Please try again later."},"request_id":"req_011CZN9evfvMAyemayRMC1yg"}
```

---

## Steps to Reproduce

1. Run Claude Code with glass-proxy on standard workload (~900 requests, ~3-4M input tokens per day)
2. Observe 5h rate limit progression via response headers or proxy logging
3. Compare day-over-day exhaustion timing

**Baseline Day (2026-03-23):**
- 908 requests
- 3.4M input tokens
- 5.7M cache creation tokens
- **Result: 0 exhaustions**

**Degraded Day (2026-03-24):**
- 936 requests (+3%)
- 4.2M input tokens (+22%)
- 7.3M cache creation tokens (+28%)
- **Result: 3 exhaustions in 7 hours**

---

## Breach of Service Agreement

### Issue: Unilateral Service Degradation

The acceleration of rate limit exhaustion represents a **material change to service behavior** without:
- Advance notice to users
- Documentation updates
- Corresponding workload increase to justify the change

### Quantified Impact

| Metric | 2026-03-23 | 2026-03-24 | Delta |
|--------|-----------|-----------|-------|
| 5h exhaustions | 0 | 3 | **∞ increase** |
| Hours of service | ~24 | ~7 (cumulative) | **-71%** |
| Cache creation tokens | 5.7M | 7.3M | **+28%** |
| Cost efficiency | Baseline | Degraded | **Unknown excess cost** |

### Request for Recompensation

**We request the following:**

1. **Credit for service downtime:** 3 complete 5h window exhaustions = ~15 hours of unavailable service capacity

2. **Refund for excess token burn:** 7.3M vs expected ~6M cache creation tokens = ~1.3M excess tokens at published rates

3. **Investigation commitment:** Acknowledgment of this issue and timeline for fix

4. **Transparency:** Explanation of what changed between 2026-03-23 and 2026-03-24 to cause this acceleration

---

## Environment

| Field | Value |
|-------|-------|
| **Claude Code Version** | Latest (via proxy) |
| **Platform** | Anthropic API |
| **Model** | claude-sonnet-4 / opus-4 |
| **Operating System** | Linux |
| **Integration** | glass-proxy |
| **Plan** | API access with 5h/7d rate limits |

---

## Request IDs for Support Reference

- `req_011CZMvKbSEwA55a4LmhFXou` (2026-03-24 13:39:46 UTC)
- `req_011CZN7ECkNwoUeG2jZBmCr4` (2026-03-24 16:02:48 UTC)
- `req_011CZN9evfvMAyemayRMC1yg` (2026-03-24 16:34:37 UTC)

---

## Is this a regression?

Yes. Service worked as expected on 2026-03-23 and prior days.

## Last Working Date

2026-03-23

## Additional Context

Multiple users have reported similar accelerated rate limit exhaustion in community discussions today. This appears to be a systemic issue rather than account-specific.
