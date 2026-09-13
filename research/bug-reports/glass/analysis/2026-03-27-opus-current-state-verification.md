# Opus Current-State Verification

Verified on 2026-03-27 against:

- live endpoints on `http://127.0.0.1:19999`
- current-process log `/tmp/glass-proxy/current-19999.log`
- debug DB `/home/user/.claude/glass_debug.db`

## Verdict

The report mixes:

- some correct structural observations
- some snapshot-specific numbers that were already outdated
- some conclusions that overstate health

## Claims that verified

### 1. Small-system subagents are intentionally uncached

Verified.

Recent 6-hour aggregation from `requests`:

- `small_system`
  - `reqs=48`
  - `avg_eff=0.0`
  - `total_create=0`
  - `total_read=0`
  - `total_input=980078`

This is consistent with intentional isolation and disabled upstream caching for this class of request.

### 2. The `28a034b5507a_45068` agent-tool lane did have a cold-start then warm reads

Verified in the DB.

Key rows:

- `2026-03-27T12:50:55.945236191+01:00`
  - `cache_create=74528`
  - `cache_read=0`
  - `eff=0.0`
- `2026-03-27T12:51:51.475012261+01:00`
  - `cache_create=13997`
  - `cache_read=78457`
  - `eff=78.6`
- `2026-03-27T12:52:00.623733037+01:00`
  - `cache_create=8074`
  - `cache_read=92454`
  - `eff=88.8`
- `2026-03-27T12:52:34.036436945+01:00`
  - `cache_create=3927`
  - `cache_read=100528`
  - `eff=95.9`
- `2026-03-27T12:52:39.01171477+01:00`
  - `cache_create=599`
  - `cache_read=104455`
  - `eff=99.0`
- `2026-03-27T12:52:48.501952449+01:00`
  - `cache_create=566`
  - `cache_read=105054`
  - `eff=98.6`
- `2026-03-27T12:56:11.878118415+01:00`
  - `cache_create=0`
  - `cache_read=105620`
  - `eff=95.0`
- `2026-03-27T12:57:03.291670439+01:00`
  - `cache_create=0`
  - `cache_read=105642`
  - `eff=92.8`

The exact ramp in the report was directionally right.

### 3. No active eviction events in the current session

Verified from `eviction_events`.

The newest stored eviction rows are still on `2026-03-14`.

### 4. No serializer queue / timeout / switch events

Partially verified.

Current-process log counts:

- `SER.*SWITCH = 0`
- `SER.*QUEUE = 0`
- `SER.*TIMEOUT = 0`

But there are many live subagent gate waits:

- `SER.*WAIT = 49`

So "no queue or timeout events" is true, but "serializer is idle" is false.

## Claims that do not verify cleanly

### 1. Quota numbers are no longer current

Not verified as current.

Live at `2026-03-27T13:12:40.469989725+01:00`:

- `5h utilization = 16%`
- `7d utilization = 56%`
- `status = allowed / allowed`
- statusline burn estimate `72.3 pp/hr`
- statusline hours left `1.16h`

So the report's `6% / 55% / ~17 pp/hr / ~5.4h left` was a time-local snapshot, not a stable statement about current state.

### 2. The 6-hour aggregate numbers in the report are outdated

Current 6-hour aggregation from `requests`:

- `reqs=87`
- `cache_read=1506883`
- `cache_create=371123`
- `input_tokens=1305178`
- `overall_read_pct=47.3`

The report claimed:

- `51 requests`
- `cache_read ~1.4M`
- `cache_create ~349K`
- `input ~583K`
- `overall read % 60.0`

Those numbers do not match the current DB.

### 3. The per-type breakdown is no longer current

Current 6-hour breakdown:

- `main_session`
  - `reqs=21`
  - `avg_eff=62.6`
  - `total_create=203536`
  - `total_read=580832`
- `agent_tool`
  - `reqs=18`
  - `avg_eff=55.7`
  - `total_create=167587`
  - `total_read=926051`
- `small_system`
  - `reqs=48`
  - `avg_eff=0.0`
  - `total_create=0`
  - `total_read=0`

The report's `15 / 15 / 21` request split was an earlier snapshot.

### 4. Prefix-break summary does not match the current-process log

Current-process log counts:

- `prefix_changed = 0`
- `tail_changed = 4`

There are several `FINAL PREFIX INIT` lines, but no live `PREFIX CHANGED` lines in the current restarted log.

So the report's "2 PREFIX CHANGED events and 13 TAIL CHANGED events in the current log" does not match the current log now.

### 5. "No anomalous cache breaks detected" is not supported by current live signals

Current live signals do not support that conclusion.

At `2026-03-27T13:12:40.469989725+01:00`:

- `/debug/cache-health` returned `healthy=false`
- `max_cc_15m=22552`
- `severe_breaks_15m=1`
- `cache_hit_pct=0`

At the same time:

- the live status session average cache efficiency was only `4.47%`
- the current latest request was a `small_system` request with `cache_read=0`, `cache_create=0`, `eff=0`

So the system cannot honestly be described as "operating within expected parameters" without narrowing that claim to a much earlier slice.

## Best interpretation

The report was not pure fabrication. It captured several real structural truths:

- small-system isolation is working as designed
- the main `28a...` agent-tool lane did warm up after a cold start
- no new eviction events have been recorded in `eviction_events`

But it overgeneralized from a narrower and calmer slice of data.

The current state is harsher:

- quota is rising much faster than the report said
- small-system traffic volume is much higher
- live serializer gate activity is significant
- cache-health is still unhealthy overall

So the right reading is:

- parts of the report were accurate for a moment
- its final verdict was too optimistic
