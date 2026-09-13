# Insight 004: Bridge Staleness Causes Session Death

**Date:** 2026-02-22
**Severity:** Critical (sessions killed)
**Status:** Fixed + verified via simulation
**Session:** 20260222_154806 (1027 calls, 3.75 hours, $82.95 burn)

---

## 1. The Problem

Sessions die with "Context limit reached" even when:
- True context is at 53-80% (106-160K tokens)
- CC sees 30% (spoofed to 60K)
- BLOCKING_LIMIT_OVERRIDE=999999 (client-side block impossible)
- Trimmer is alive and healthy (no syntax errors, no crashes)
- Stage 2 trigger at 175K was never reached according to bridge

## 2. Root Cause: Bridge Is Stale By One Call

Stage 2 `_drop_old_messages()` decides whether to fire using `effective_input`, which prefers the **bridge** (real API token count from the previous response) over the char-based estimate. This was correct per the 2026-02-21 fix (Insight 10: est overestimates 5x for JSON-heavy bodies, causing cascading drops).

**The gap:** Bridge reflects the PREVIOUS response. Between responses, CC can add 30-60K tokens of tool_results (file reads, command output). The bridge does not account for these additions.

### Kill chain:
```
1. Last API response: 155K tokens -> bridge stores 155K
2. CC executes tool_use -> reads large file -> adds 50K tokens of tool_result
3. CC sends next request -> trimmer checks Stage 2
4. Bridge = 155K < 175K trigger -> NO DROP
5. Actual request to Anthropic: 155K + 50K = 205K tokens
6. Anthropic returns 400: "prompt is too long"
7. CC catches error -> displays "Context limit reached"
8. ITT addon skips error response (first_chunk_time=0) -> NO DB RECORD
9. No bridge update -> next attempt hits the same wall
```

### Evidence from session data:
| Timestamp | Conv | Bridge (prev) | Real (this call) | Growth | Outcome |
|-----------|------|---------------|-------------------|--------|---------|
| 18:38:27 | 0d4ec7c8 | 167,968 | 199,935 | +31,967 | Anthropic accepted (barely) |
| 18:32:59 | 0d4ec7c8 | 125,274 | 178,032 | +52,758 | Stage 2 missed (125K < 175K) |
| 19:14:43 | 2ef21211 | 163,472 | 175,958 | +12,486 | Stage 2 caught this one |

Conv `0d4ec7c87726` hit **200,286 total context** -- 286 tokens OVER the 200K limit. The call succeeded (Anthropic has a small grace zone), but subsequent calls with larger growth would be rejected.

## 3. Why max(bridge, est_current) Does Not Work

The 2026-02-21 fix (code comment line 596-599) documents why:

> CPT=3.35 overestimates 4-5x for JSON-heavy bodies (tools, structured content) because JSON keys average 10-15 chars/token, not 3.35. BUG FIX: Using est_current as trigger caused cascading drops: est=500K while real=115K -> Stage 2 fires every call -> perpetual message loss.

Re-introducing est_current into the trigger would recreate this cascading-drops bug.

## 4. The Fix: Growth-Adjusted Effective Input

**Key insight:** Comparing two consecutive `est_current` values **cancels** the systematic JSON bias. Tools and system prompt do not change between calls, so `est_current[n] - est_current[n-1]` reflects only message growth. The messages-only estimation ratio is 0.95x (not the 5x full-body overestimate).

### Implementation (context_trimmer.py):
```python
# Module level:
_last_est_current = {}  # conv_id -> est_current from last request

# In _drop_old_messages(), after effective_input decision:
_prev_est = _last_est_current.get(conv_id, est_current)
_est_growth = max(0, est_current - _prev_est)
_last_est_current[conv_id] = est_current
_adjusted_effective = (last_input + _est_growth) if last_input > 0 else est_current

if _adjusted_effective > effective_input and _adjusted_effective >= 185000:
    effective_input = _adjusted_effective  # Override with growth-adjusted value
```

### Why this works:
- Bridge is accurate (real tokens from Anthropic)
- est_growth = delta of two estimates with the SAME bias -> bias cancels
- adjusted_effective = bridge + real_growth (approximately)
- Only fires when bridge + growth exceeds 185K safety cap

### Simulation results (1027 calls from session 20260222_154806):

| Growth Cap | Catches (200K+) | False Fires | Missed | Ratio range tested |
|------------|-----------------|-------------|--------|--------------------|
| 185K | 1/1 (100%) | 0 | 0 | 0.8x - 1.1x |
| 190K | 1/1 (100%) | 0 | 0 | 0.9x - 1.1x |
| 195K | 1/1 (100%) | 0 | 0 | 0.95x - 1.1x |

At ratio=0.8 (worst case code-heavy underestimate):
- Bridge=168K + growth=32K*0.8=25.6K -> adjusted=193.6K > 185K -> CAUGHT

At ratio=1.1 (overestimate):
- Bridge=168K + growth=32K*1.1=35.2K -> adjusted=203.2K > 185K -> CAUGHT

**Zero false fires at any ratio.** Normal calls have growth <10K per call. Bridge=140K + 10K = 150K < 185K -> never triggers.

## 5. The Observability Fix: ITT Error Logging (Insight 12)

**Problem:** When Anthropic returns a non-streaming error (400/429), the ITT addon `first_chunk_time` stays at 0. The response hook skips the entire sample -- no DB record, no bridge update, no logging.

**Fix (mitm_itt_addon.py):** Before the skip, check `flow.response.status_code`. If >= 400, log the error to `trimmer_critical.log` (file-based, survives journald rate limiting).

```python
if capture.first_chunk_time == 0:
    _resp_status = flow.response.status_code if flow.response else 0
    if _resp_status >= 400:
        _err_body = (flow.response.content or b"").decode("utf-8", errors="replace")[:500]
        _err_msg = "[ITT] API-ERROR: status=" + str(_resp_status) + " body=" + _err_body
        ctx.log.warn(_err_msg)
        logging.getLogger("trimmer_critical").warning(_err_msg)
    return
```

Next time a session dies from "prompt is too long", the error will be recorded in `trimmer_critical.log` with the full Anthropic error message and status code. No more blind spot.

## 6. Messages-Only Estimation Accuracy

Analysis of 4003 bridge/estimate pairs from this session:

### Full-body estimate (system + tools + messages):
| Metric | Value | Note |
|--------|-------|------|
| Median ratio (bridge/est) | 0.964 | 4% overestimate |
| p10 | 0.076 | Massive overestimate (JSON-heavy) |
| p90 | 1.18 | Slight underestimate |
| Max | 3.031 | 3x underestimate |

### Messages-only estimate (excluding tools/system overhead ~81K est):
| Metric | Value | Note |
|--------|-------|------|
| Median ratio | 0.949 | 5% overestimate -- close to real |
| p90 | 1.176 | 18% overestimate |
| Max | 1.273 | 27% overestimate max |
| >2x overestimate | 0% | No extreme overestimates |
| >1.5x overestimate | 4.3% | Rare |

The 5x JSON overestimate problem is entirely from tools/system. Messages-only estimation is reliable (within 27% worst case). The growth-delta approach avoids even this by canceling the bias entirely.

## 7. CC Source Code: Two Paths to "Context limit reached"

Decompiled from `cli.js` (11.4MB):

### Path 1: Client-side blocking (ELIMINATED by BLOCKING_LIMIT_OVERRIDE=999999)
```javascript
// vv(G) = ix1(last_spoofed_usage) + hOA(messages_since_last_call)
// ix1 = input + cache_creation + cache_read + output (all spoofed to ~60K total)
// With BLOCKING_LIMIT_OVERRIDE=999999: isAtBlockingLimit = vv(G) >= 999999
// vv() ~ 65K << 999999 -> NEVER triggers
let {isAtBlockingLimit: V1} = qc(vv(G), model);
if (V1) { yield UY({content: lU, error: "invalid_request"}); return; }
```

### Path 2: API error catch (THE ACTUAL KILLER)
```javascript
// When Anthropic returns HTTP 400 with "prompt is too long":
if (A instanceof Error && A.message.toLowerCase().includes("prompt is too long"))
    return UY({content: lU, error: "invalid_request"});
```

Both paths produce `lU = "Prompt is too long"` which renders as `"Context limit reached"`.

With BLOCKING_LIMIT_OVERRIDE=999999, Path 1 is impossible. All session deaths come from Path 2: Anthropic rejecting oversized requests that the trimmer failed to catch.

## 8. DCP Behavioral Contamination (Bonus finding)

The `[previous output superseded]` DCP marker was found echoed by the MODEL as its own response text in the brutus-ai session (conversation file line 1229). When the model sees hundreds of DCP markers in its context, it mimics the pattern and outputs the marker as response text.

Evidence: brutus-ai/85714931 L1229: `role=assistant block_type=text content="[previous output superseded]"` -- the model output ONLY the marker as its entire text block, after receiving a tool_result.

## 9. Session Economics

Session 20260222_154806 (1027 calls, 3.75 hours):

| Component | Tokens | Cost | % of Total |
|-----------|--------|------|------------|
| cache_creation | 3,953,268 | $24.71 | 29.8% |
| cache_read | 101,840,974 | $50.92 | 61.4% |
| input (uncached) | 9,990 | $0.04 | 0.0% |
| output | 388,296 | $7.28 | 8.8% |
| **Total** | -- | **$82.95** | -- |
| **Burn rate** | -- | **$22.12/hr** | -- |

5-hour quota utilization went from 48% to 56% in 21 minutes (22.8%/hr burn rate).

## 10. What We Still Do Not Know

1. **Exact error response for each death:** The ITT error logging fix is prospective only. Today deaths are unrecoverable -- error responses were never recorded.

2. **Anthropic hard limit vs grace zone:** Conv 0d4ec7c87726 successfully processed a 200,286-token request. The limit might be slightly above 200K, or per-model, or variable.

3. **Role of concurrent sessions:** 5 conversations shared one proxy session. Bridge data per-conversation is correct (keyed by conv_id), but all conversations compete for the same 5-hour quota.

## 11. Configuration

The growth cap is configurable via `~/.claude/trimmer_config.json`:

```json
{
    "drop_growth_cap_tokens": 185000
}
```

Default: 185000 (validated against 1027 calls: 0 false fires, 100% catch rate).

## 12. Files Changed

| File | Change | Lines |
|------|--------|-------|
| context_trimmer.py | Added _last_est_current dict + growth-adjusted safety cap in _drop_old_messages() | +18 |
| mitm_itt_addon.py | Added error response logging at first_chunk_time == 0 skip | +14 |

Both changes deployed via hot-reload (no mitmproxy restart, no session disruption).

## 13. Cross-References

- **Insight 9:** Bridge staleness (this insight provides the root cause analysis and fix)
- **Insight 10:** est_current 5x overestimate for JSON (why naive max(bridge,est) fails)
- **Insight 12:** ITT blind spot for error responses (now fixed)
- **Insight 17:** Emergency bypass at 185K (existed but only bypassed cooldown, not trigger)
- **Insight 22:** Stage 2 drops are persistent (CC state shrinks permanently after drops)
- **anti-compaction-system.md:** Defense-in-depth layers (trimmer primary, spoof secondary)
- **context-limit-root-cause-2026-02-15.md:** Previous death was trimmer crash; today is trimmer alive but bridge stale
- **incident-2026-02-15-triple-session-death.md:** Previous triple death from syntax error; today from estimation gap
