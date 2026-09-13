# Context Limit Root Cause Analysis — 2026-02-15

**Status**: Fixed
**Impact**: All sessions after 16:40 CET lost unlimited context until fix applied
**Fix applied**: 17:16 CET same day

---

## Symptom

After the pair-drop edit to `context_trimmer.py` at 16:40 CET, sessions began hitting "Context limit reached" at low True context percentages (~40%). Prior to this edit, sessions had effectively unlimited context.

## Root Cause: Syntax Error Killed Trimmer via Hot-Reload

The pair-drop fix at line 575 introduced an f-string with a backslash inside the expression:

```python
# BROKEN — Python < 3.12 forbids backslash in f-string expressions
ctx.log.warn(f"[TRIM] Stage 2: dropped non-user msg[0] (role={messages[0].get(\"role\")})")
```

**Timeline:**
1. **16:40:01** — `snippet_patch` wrote the pair-drop fix to `context_trimmer.py`
2. **16:40:01** — mitmproxy detected file change, initiated hot-reload
3. **16:40:03** — Hot-reload shut down the old trimmer (`Shutdown: saved 4 watermarks`)
4. **16:40:03** — Hot-reload attempted to load new file → `SyntaxError`
5. **16:40:03+** — Trimmer addon **dead**. Zero `[TRIM]` entries from this point forward.

Unlike a restart (which loads from disk on startup), hot-reload:
1. **Stops** the running addon (teardown/shutdown)
2. **Then** attempts to load the new version
3. If load fails → **addon is gone**. Old version is NOT kept.

This is the critical insight: **hot-reload is NOT atomic**. The old code is unloaded before the new code is validated.

## Why Trimmer Death Causes "Context Limit Reached"

### CC's Two Context Tracking Systems

Decompilation of CC's `cli.js` reveals two independent mechanisms:

#### 1. `used_percentage` (statusline display)

```javascript
// wlA() — computes the % shown in statusline
function wlA(A, q) {
  let K = A.input_tokens + A.cache_creation_input_tokens + A.cache_read_input_tokens;
  return { used: Math.round(K / q * 100), remaining: ... };
}
```

This reads from the **API response's usage fields** — which the spoofing modifies. When the trimmer is alive, API input is low (~80K) → CC shows ~40%. When dead, API input is high (~160K) → spoofing caps input to 140K → CC shows ~70%.

#### 2. `isAtBlockingLimit` (hard stop — "Context limit reached")

```javascript
// qc() — determines if session is blocked
let M = CLAUDE_CODE_BLOCKING_LIMIT_OVERRIDE || (contextWindow - 3000);  // 199000
let P = A >= M;  // A = vv(messages)
return { isAtBlockingLimit: P };

// vv() — walks messages, finds last usage, adds hOA estimate
function vv(A) {
  // ... finds last message with API usage Y
  return ix1(Y) + hOA(A.slice(after_Y));
}

// ix1() — THE CRITICAL FUNCTION
function ix1(A) {
  return A.input_tokens + A.cache_creation_input_tokens
       + A.cache_read_input_tokens + A.output_tokens;
}
```

**`ix1()` includes `output_tokens`**, but the original spoofing only capped the three input fields. With trimmer alive, input is genuinely low so spoofing doesn't fire, and ix1 = ~90K + output. With trimmer dead, spoofed input = 140K + unspoofed output → ix1 climbs toward 199K blocking limit.

### The Math

**Trimmer alive (working state):**
```
Real API input: ~80K (trimmed from ~160K)
Spoof cap: 140K → NOT triggered (80K < 140K)
CC sees: ix1 = 80K + output(5K) = 85K
vv = 85K + hOA(5K) = 90K
90K < 199K blocking limit → OK
```

**Trimmer dead (broken state):**
```
Real API input: ~160K (full conversation, untrimmed)
Spoof cap: 140K → TRIGGERED, scales input to 140K
CC sees: ix1 = 140K + output(15K) = 155K  ← output NOT spoofed!
vv = 155K + hOA(30K) = 185K
... grows each call ...
Eventually: vv = 200K >= 199K → "Context limit reached"
```

## Fixes Applied

### Fix 1: Syntax Error (immediate)

```python
# BEFORE (broken)
ctx.log.warn(f"...{messages[0].get(\"role\")}...")

# AFTER (fixed)
_dropped_role = messages[0].get("role")
ctx.log.warn(f"...{_dropped_role}...")
```

### Fix 2: Spoof output_tokens (defensive)

Added `output_tokens` to the spoofing list in `mitm_itt_addon.py`:

```python
# BEFORE — ix1 check excludes output, spoof list misses output
_real_ix1 = capture.input_tokens + capture.cache_creation + capture.cache_read
for _field, _real_val in [
    ("input_tokens", ...),
    ("cache_creation_input_tokens", ...),
    ("cache_read_input_tokens", ...),
]:

# AFTER — ix1 matches CC's ix1(), spoof list includes output
_real_ix1 = (capture.input_tokens + capture.cache_creation
             + capture.cache_read + capture.output_tokens)
for _field, _real_val in [
    ("input_tokens", ...),
    ("cache_creation_input_tokens", ...),
    ("cache_read_input_tokens", ...),
    ("output_tokens", capture.output_tokens),  # NEW
]:
```

This ensures that even if the trimmer dies again, ix1() as CC computes it will be capped at `spoof_usage_cap_tokens` (140K), preventing vv() from reaching the 199K blocking limit.

## CC Internal Constants (from decompilation)

| Constant | Value | Purpose |
|----------|-------|---------|
| `kFY` | 20000 | Warning threshold buffer (window - 20K) |
| `LFY` | 20000 | Error threshold buffer (window - 20K) |
| `DSA` | 3000 | Default blocking buffer (window - 3K = 197K) |
| `XSA` | 13000 | Autocompact buffer |
| `EFY` | 20000 | Max output consideration |
| `BLOCKING_LIMIT_OVERRIDE` | 199000 | Our env var override |

## Key Functions (decompiled)

| Function | Role |
|----------|------|
| `wlA(usage, window)` | Computes `used_percentage` from API response input fields |
| `ix1(usage)` | Sums ALL token fields (input + cache_creation + cache_read + **output**) |
| `vv(messages)` | Returns `ix1(lastApiUsage) + hOA(messagesAfterLast)` |
| `hOA(messages)` | Estimates tokens for messages not yet counted by API |
| `qc(vv, model)` | Computes `isAtBlockingLimit` = `vv >= 199K` |
| `um()` | Returns whether autocompact is enabled |
| `yG(model, betas)` | Returns context window size (200K or 1M for extended) |

## Lessons Learned

1. **Hot-reload is NOT atomic** — it unloads old code before validating new code. A syntax error = dead addon, not "keep old version."

2. **Always syntax check BEFORE saving** (or immediately after, with automatic rollback). The CLAUDE.md protocol says check after, but the damage (hot-reload teardown) happens on file save.

3. **output_tokens matters** — CC's ix1() includes output_tokens. Any spoofing system that only caps input fields creates a gap where large outputs can push ix1() over the blocking limit.

4. **Trimmer is the PRIMARY defense** — When working, it keeps API tokens genuinely low (~80K), making spoofing unnecessary. Spoofing is a SECONDARY defense for when trimming can't reduce tokens enough.

5. **f-string backslash rule** — Python < 3.12 forbids backslash inside `{...}` in f-strings. Always extract to a variable first. This is the #1 syntax error risk in mitmproxy addons.

## Follow-up Fix: Stage 2 Stale Bridge Bug

**Discovered**: Same session — another conversation hit 99% True context even after trimmer was restored.

**Root cause**: Stage 2 decides to drop messages based on `last_input` from the ITT bridge file. The bridge records the **last API response** token count. Between responses, CC adds many tool results, growing the conversation from e.g. 116K → 198K. Stage 2 reads bridge=116K < 175K trigger → no drops. But the current request is already 198K.

**Fix**: Stage 2 now computes `est_current = _estimate_tokens(messages)` directly from the request body and uses `effective_input = max(bridge, est_current)`. This catches sessions that grow rapidly between API calls.

```python
# Before: only uses stale bridge value
if last_input < drop_trigger_tokens:
    return 0  # Misses 198K request because bridge says 116K

# After: uses max(bridge, current estimate)
est_current = _estimate_tokens(messages)
effective_input = max(last_input, est_current)
if effective_input < drop_trigger_tokens:
    return 0  # catches 198K because max(116K, 198K) = 198K >= 175K
```
