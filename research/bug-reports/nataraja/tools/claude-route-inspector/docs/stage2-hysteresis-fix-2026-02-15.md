# Stage 2 Drop Hysteresis — Fix Report

**Date:** 2026-02-15
**Status:** FIXED and VERIFIED
**Config change:** `drop_trigger_messages: 9999` (effectively disables Stage 2)

---

## Problem

Stage 2 (`_drop_old_messages`) fired on **every single API call**, causing the Anthropic cache prefix to break every time.

### Mechanism

1. Claude Code (CC) sends its **entire conversation history** on every API call (~590 messages)
2. Stage 2 trigger: `len(messages) > drop_trigger` → always true when conversation > trigger
3. Stage 2 drops to `drop_target` (80) messages → msg[0] shifts by 2 positions each call
4. Shifted msg[0] = different cache prefix = full cache rebuild (~60-80K CC_new tokens)
5. Next call: CC sends 592 messages (it doesn't know about drops) → goto 1

### Why the initial hysteresis fix (drop_trigger=200, drop_target=80) didn't work

The hysteresis model assumed the proxy could "accumulate" messages between drops. It can't — CC always resends its full context. With 590 messages permanently exceeding trigger=200, Stage 2 fired every call. The gap (200→80) only works for conversations shorter than 200 messages.

### Evidence (from journal logs)

```
Stage 2 DROP: removed 510 oldest messages (~238129 tok freed, 77 remaining, trigger=200, target=80)
Stage 2 DROP: removed 512 oldest messages (~238412 tok freed, 77 remaining, trigger=200, target=80)
Stage 2 DROP: removed 514 oldest messages (~238587 tok freed, 77 remaining, trigger=200, target=80)
Stage 2 DROP: removed 516 oldest messages ...
Stage 2 DROP: removed 518 oldest messages ...
Stage 2 DROP: removed 520 oldest messages ...
Stage 2 DROP: removed 522 oldest messages ...
```

Monotonically increasing drop count = CC adding 2 messages per call, Stage 2 dropping them every time.

---

## Fix Applied

Set `drop_trigger_messages: 9999` in `~/.claude/trimmer_config.json`.

This effectively disables Stage 2. Stage 1 truncation + `strip_old_thinking: true` handles context reduction — even 590 messages stays well under the 199K API limit after truncation.

### Why it's safe

- Stage 1 truncates old messages to ~500-700 char stubs
- `strip_old_thinking` removes 31K-token thinking blocks from old turns
- 590 truncated messages ~ 85K tokens + 30K system/tools + 50K recent = ~165K (under 199K limit)
- If a conversation ever grows so long Stage 1 can't keep it under 199K, that's a signal to start a new session

---

## Verified Results

Measured from `fingerprint.db`, same session, before/after config change:

| Metric | Before (Stage 2 active, 14:14-14:28) | After (Stage 2 disabled, 14:31+) |
|---|---|---|
| Calls | 33 | 22 |
| Avg CC_new | **20,921** tokens/call | **2,269** tokens/call |
| Avg CC_read | 52,228 tokens/call | **105,571** tokens/call |
| Cache hit rate | **71%** | **98%** |
| Total CC_new | 690,391 tokens | **49,907** tokens |

**89% reduction in cache writes. Hit rate: 71% -> 98%.**

### Verification query

```sql
-- Before fix
SELECT COUNT(*), ROUND(AVG(cache_creation_tokens),0), ROUND(AVG(cache_read_tokens),0),
       ROUND(SUM(cache_read_tokens)*100.0/(SUM(cache_read_tokens)+SUM(cache_creation_tokens)+0.1),0)
FROM samples WHERE strftime('%H:%M', timestamp) BETWEEN '14:14' AND '14:28'
AND timestamp LIKE '2026-02-15%';

-- After fix
SELECT COUNT(*), ROUND(AVG(cache_creation_tokens),0), ROUND(AVG(cache_read_tokens),0),
       ROUND(SUM(cache_read_tokens)*100.0/(SUM(cache_read_tokens)+SUM(cache_creation_tokens)+0.1),0)
FROM samples WHERE strftime('%H:%M', timestamp) >= '14:31'
AND timestamp LIKE '2026-02-15%';
```

---

## Config Change

```json
{
  "drop_trigger_messages": 9999,
  "drop_target_messages": 80
}
```

Config is hot-reloaded by `_load_config()` on every request (checks file mtime). No service restart required.

---

## Related Files

- `context_trimmer.py` — `_drop_old_messages()` function (Stage 2 implementation)
- `~/.claude/trimmer_config.json` — runtime config (hot-reloaded)
- `~/.claude/fingerprint.db` -> `samples` table -> `cache_creation_tokens`, `cache_read_tokens`
- Previous report: `cache-optimization-report.md` S12 (Two-Stage Context Trimmer)
