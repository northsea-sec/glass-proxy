# Small-System Repeat Cache Experiment

Date: 2026-03-27

Purpose:

- reduce the verified `small_system` burn without re-enabling the global serializer
- keep subagent isolation intact
- only allow Anthropic-side caching for repeated isolated `small_system` lanes

## Why this was added

Live evidence on 2026-03-27 showed that the main burn source was no longer PID fallback or serializer bypass.

The repeated cost center was `small_system` traffic:

- repeated isolated subagent lanes
- `0` upstream cache read
- `0` upstream cache create
- all input billed at full rate

Two hot repeated lanes were observed from the debug DB:

- one repeated more than 30 times with roughly `970k` input tokens
- another repeated more than 30 times with roughly `439k` input tokens

That means the dominant problem was not "one-shot tiny tool calls."

It was repeated traffic on isolated lanes that remained intentionally uncached forever.

## What changed

### 1. Restarts now rebuild when source is newer than the binary

Updated:

- [start.sh](/home/user/glass-proxy/start.sh)
- [start-stack.sh](/home/user/glass-proxy/start-stack.sh)
- [codex-glass](/home/user/glass-proxy/bin/codex-glass)

Behavior:

- build if the binary is missing
- build if `--build` was requested
- build if any `cmd/**/*.go`, `internal/**/*.go`, `go.mod`, or `go.sum` file is newer than the binary
- resolve Go from `$GO_BIN`, `PATH`, or `/usr/local/go/bin/go`

Why:

- earlier verification showed that plain restarts were reusing stale binaries
- this caused live/runtime truth to drift from source truth

### 2. Added a guarded repeat-cache experiment for isolated `small_system` lanes

Added config fields:

- `small_system_repeat_cache_enabled`
- `small_system_repeat_cache_min_hits`
- `small_system_repeat_cache_window_sec`

Wired through:

- [config.go](/home/user/glass-proxy/internal/config/config.go)
- [classifier.go](/home/user/glass-proxy/internal/subagent/classifier.go)
- [process.go](/home/user/glass-proxy/internal/glass/process.go)
- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
- [small_system_cache.go](/home/user/glass-proxy/internal/proxy/small_system_cache.go)
- [main.go](/home/user/glass-proxy/cmd/glass-proxy/main.go)

Behavior:

- default is off
- only applies to `small_system` requests that are already isolated
- the proxy tracks repeated isolated lanes in a rolling time window
- once a lane reaches the configured hit threshold, the proxy clears the upstream-cache ban for that lane only
- the lane stays isolated; this is not a return to parent-shared caching

Important detail:

- the Glass engine previously forced `DisableUpstreamCaching=true` for any `_sub_` session
- the experiment adds an explicit classification flag so repeated-cache-eligible lanes are not forcibly downgraded again inside Glass

### 3. Added live observability for the experiment

New debug endpoint:

- `/debug/small-system-cache`

Exposes:

- whether the experiment is enabled
- hit threshold
- window
- observed requests
- upgraded requests
- active lanes
- eligible lanes
- top repeated lanes with hit counts and last prefix hash

## Verification

Passed:

- `bash -n start.sh start-stack.sh bin/codex-glass`
- `python3 -m py_compile config_server.py`
- `/usr/local/go/bin/go test ./internal/config ./internal/proxy ./cmd/glass-proxy`
- `/usr/local/go/bin/go test ./internal/glass -run 'TestSmallSystemBypassesGlassCacheAndStripsAllCacheControl|TestSmallSystemRepeatCacheEligibleSkipsSubagentOriginDisable' -count=1 -v`
- `/usr/local/go/bin/go test ./internal/glass`

Added regression coverage:

- [config_test.go](/home/user/glass-proxy/internal/config/config_test.go)
- [small_system_cache_test.go](/home/user/glass-proxy/internal/proxy/small_system_cache_test.go)
- [split_id_test.go](/home/user/glass-proxy/internal/glass/split_id_test.go)

Rebuilt binary:

- [/home/user/glass-proxy/glass-proxy](/home/user/glass-proxy/glass-proxy)
- mtime after rebuild: `2026-03-27 13:32:28 +0100`

## Current caveat

This was implemented as a guarded experiment, not enabled globally.

So:

- the code is present
- the next restart will pick it up automatically
- but live behavior will not change until `small_system_repeat_cache_enabled` is set true

That is intentional.

The verified safe baseline remains:

- `ser_enabled=false`
- subagent gate remains active
- no change to main-session cache behavior

## Live enablement

At `2026-03-27 15:18:40 +0100`, the live config was updated and the proxy was restarted on `:19999`.

Verified live facts after restart:

- [/home/user/.claude/glass_config.json](/home/user/.claude/glass_config.json) now contains:
  - `small_system_repeat_cache_enabled=true`
  - `small_system_repeat_cache_min_hits=2`
  - `small_system_repeat_cache_window_sec=1800`
- the running process is `/home/user/glass-proxy/glass-proxy -listen :19999 -upstream https://api.anthropic.com -config /home/user/.claude/glass_config.json -allow-direct`
- [/tmp/glass-proxy/current-19999.log](/tmp/glass-proxy/current-19999.log) shows the new startup at `2026-03-27 15:18:41 +0100`
- `curl http://127.0.0.1:19999/debug/small-system-cache` returned:
  - `enabled=true`
  - `min_hits=2`
  - `window_sec=1800`

Important limit at the time of verification:

- there was no new Anthropic traffic after the restart yet
- `observed_requests=0` and `upgraded_requests=0` on `/debug/small-system-cache`
- so live config enablement is verified, but live lane promotion still needs a post-restart request window to confirm

## First live traffic after enablement

Verified at `2026-03-27 15:44:52 +0100`:

- `/debug/small-system-cache` reported:
  - `enabled=true`
  - `observed_requests=4`
  - `eligible_lanes=1`
  - `upgraded_requests=3`
  - hot lane `e0a2ba8019e9_47276_sub_753d64f0` with `hits=4`
- `/tmp/glass-proxy/current-19999.log` contains:
  - `[SMALL-SYS-CACHE] Enabled upstream cache for lane=e0a2ba8019e9_47276_sub_753d64f0 hits=2`
  - repeated enable logs again at hits `3` and `4`

Observed DB impact after restart (`timestamp >= 2026-03-27T15:18:40`):

- `small_system`: `4` requests, `24,466` input, `46,014` cache create, `0` cache read, `0.0%` avg efficiency
- `agent_tool`: `4` requests, `80,259` input, `195,645` cache create, `0` cache read in the first four rows; a later row at `15:44:52` then showed `64,173` cache read and `0` cache create

Interpretation:

- the repeat-cache experiment is definitely active and affecting live requests
- in this first sample, it caused `small_system` lanes to create upstream cache entries
- but no `small_system` upstream cache reads were observed yet in the same window
- so the experiment is not yet proven beneficial; the first measured sample increased billed cache creation before any read-side payoff appeared

At the same timestamp, `/debug/status` showed a very spiky short-window burn estimate (`180 pp/hr`, 1-minute window, 3 samples), while `/debug/cache-health` remained unhealthy due recent large cache-create events. The dominant immediate burn source was fresh reseeding on `agent_tool` lane `28a034b5507a_45068`, not only `small_system`.

## Verified regression cause

The post-enable burn spike was not caused by one thing.

What is verified:

- the large immediate burn after the `15:18` restart was dominated by restart-time reseeding of hot `agent_tool` lanes, not by `small_system` alone
- after restart, main lanes restored message history from local disk, but still came back as `FINAL PREFIX INIT` and had to recreate large upstream prefixes
- the `small_system` experiment added extra cache-create cost on top of that, before any read-side payoff was observed

Strong evidence:

- [/tmp/glass-proxy/glass-proxy.19999.20260327-151840.log](/tmp/glass-proxy/glass-proxy.19999.20260327-151840.log) shows:
  - `15:23:42` `ddbbe66d7d3b_47276` loaded from disk, then `FINAL PREFIX INIT`
  - `15:43:42` `28a034b5507a_45068` loaded from disk, then `FINAL PREFIX INIT`
- corresponding DB rows after restart:
  - `ddbbe66d7d3b_47276`: `33,556` create at `15:23:48`, then `33,721` create at `15:25:13`, only later `33,556` read at `15:45:06`
  - `28a034b5507a_45068`: `64,195` create at `15:43:52`, then `64,173` create at `15:44:12`, only later `64,173` read at `15:44:52`
- `small_system` after enablement:
  - `4` requests
  - `46,014` cache-create
  - `0` cache-read

Conclusion:

- the regression trigger was the restart path exposing incomplete restoration of warm upstream prefix state for hot main lanes
- the `small_system` repeat-cache experiment worsened short-term burn, but it was not the primary cause of the large spike
- this means the next safe action is to disable the `small_system` experiment again and investigate why restart restores local conversation cache but not reusable warm prefix state

## Refined root-cause after prefix comparison

Further verification narrowed the main cause.

What was checked:

- persisted outbound prefix snapshots before and after restart for the hot lanes
- persisted `state.json` and `localcache.json`
- Anthropic extended cache TTL configuration in code
- actual time gaps between the last warm pre-restart request and the first post-restart request

Verified findings:

- hot main-lane prefix bytes were stable across restart
  - `28a034b5507a_45068` had the same prefix hash `6cd1063ffb4bdd807b28fca7285f64fd` before and after restart
  - `ddbbe66d7d3b_47276` had the same prefix hash `446b2f6844b3e607dbef826a38e79b8b` before and after restart
- persisted local state was present and coherent
  - `localcache.json` and `state.json` both existed for the hot main lanes
  - anchor and compression watermark were restored from disk
- normal requests do request Anthropic's 1-hour cache TTL
  - `ExtendedCacheTTL = "1h"` in [cachecontrol.go](/home/user/glass-proxy/internal/glass/cachecontrol.go)
  - proxy injects `extended-cache-ttl-2025-04-11` on live Anthropic requests in [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
- the post-restart cold creates happened after gaps longer than the requested TTL
  - `28a034b5507a_45068`: last warm request before the later restart window was around `13:59:34`; first post-gap request was `15:43:42` — about `1h44m`
  - `ddbbe66d7d3b_47276`: last warm request before the later restart window was around `14:00:31`; first post-gap request was `15:23:42` — about `1h23m`

Updated conclusion:

- the main burn spike after restart was not caused by byte drift in Glass's restored prefix
- it was primarily caused by upstream Anthropic cache expiry across idle gaps longer than the 1-hour TTL
- the `small_system` experiment still worsened short-term burn, but it was additive, not the primary root cause
- the remaining architectural limitation is that current warming protects shared system/tools prefixes, not full per-conversation message prefixes, and passthrough-auth mode leaves the older dedicated keepalive path effectively unused

## Why "keepalive should have prevented this" did not hold

Verified against code and logs:

- the old dedicated keepalive implementation still exists in [keepalive.go](/home/user/glass-proxy/internal/glass/keepalive.go)
- but the current binary does **not** instantiate it in [main.go](/home/user/glass-proxy/cmd/glass-proxy/main.go)
- instead, the live runtime instantiates only the shared [PrefixWarmer](/home/user/glass-proxy/internal/glass/prefix_warmer.go)

Important behavioral differences:

- `Keepalive` was designed to hold one captured `system+tools+model` prefix alive
- `PrefixWarmer` also warms only shared `system+tools` prefixes, never full per-conversation message prefixes
- `PrefixWarmer` stores auth only in memory from the most recent real Anthropic request
- after a proxy restart, `PrefixWarmer` loads prefix profiles from disk, but its `latestKey` is empty until a new real Anthropic request arrives
- in that state, `ping()` returns early and no warming happens

Live evidence:

- there are no `[KEEPALIVE]` lines in the current runtime logs for these sessions
- the logs show `[PREFIX] Shared warmer started`, confirming the active path is the shared prefix warmer
- some older shared-prefix pings succeeded and showed warm reads:
  - `Ping 5d6741a1b459 200 — cc_read=28878 cc_new=0`
  - `Ping 0a757d9da48a 200 — cc_read=21544 cc_new=0`
- but later restart windows had no such successful pre-traffic warming before the cold reseed requests
- some ping attempts also failed with upstream `529 overloaded_error`

Practical conclusion:

- the current system does not guarantee "proxy is on, therefore hot Anthropic prefix stays warm"
- what it actually guarantees today is weaker:
  - if the process remains alive
  - and has already captured Anthropic auth in memory
  - and the shared system/tools prefix is registered
  - and ping calls are not failing upstream
  - then shared prefix warming may help
- it does **not** preserve full lane warmth across restart or long idle periods for message-bearing conversation prefixes

## Implemented architectural fix

Date: `2026-03-27 16:12 +0100`

Implemented:

- a new hot-lane warmer in [lane_warmer.go](/home/user/glass-proxy/internal/proxy/lane_warmer.go)
- wiring in [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
- live debug endpoint `/debug/lane-warmer` in [main.go](/home/user/glass-proxy/cmd/glass-proxy/main.go)
- focused tests in [lane_warmer_test.go](/home/user/glass-proxy/internal/proxy/lane_warmer_test.go)

Design:

- observe the exact final Anthropic request body after Glass, force-thinking, and cache-control placement
- register only non-isolated lanes with a sufficiently large stable prefix
- retain just the stable message prefix up to the Glass anchor, not the whole mutable tail
- warm only when a lane has been idle long enough to approach Anthropic TTL expiry (`45m`)
- prune inactive lanes after `2h`
- use the lane's own transport affinity key when warming
- append a synthetic final user `ping` only when the stable prefix ends on an assistant message

Important scope limit:

- this fix improves warming for hot message-bearing lanes **while the proxy remains running and has seen real Anthropic auth**
- it does not yet persist auth across restarts
- so it reduces the architectural mismatch that caused the large idle-gap reseeds, but it cannot pre-warm immediately after restart in passthrough-auth mode before the first real Anthropic request arrives

Verification:

- `go test ./internal/proxy ./cmd/glass-proxy` passed
- rebuilt binary:
  - [/home/user/glass-proxy/glass-proxy](/home/user/glass-proxy/glass-proxy)
  - mtime `2026-03-27 16:12:31 +0100`

Runtime note:

- the currently running proxy process has **not** been restarted onto this build yet
- the new lane warmer will become live after the next proxy restart

## 2026-03-27 16:21 CET: Statusline source and current burn verified

Verified directly from the live statusline files:

- burn is computed in [statusline.py](/home/user/.claude/statusline.py) from [/home/user/.claude/quota_samples.json](/home/user/.claude/quota_samples.json)
- cache/session/status metrics are read from [/home/user/.claude/statusline_snapshot.json](/home/user/.claude/statusline_snapshot.json)
- the statusline does **not** read burn from the proxy request DB

Current live values at `2026-03-27T16:21:23+01:00`:

- `pp_hr=4.2`
- `window_min=14.4`
- `samples_used=9`
- `current_5h_pct=11`

Important nuance:

- the request DB remains useful for correlating completed expensive requests with earlier burn spikes
- but the user was correct that the live statusline burn is sourced from the streamed quota sample file, not from the DB
- the quota sample stream still contains intermittent outlier samples like `h5=0.20 / d7=0.70`, which do not line up with the completed post-restart proxy requests and need separate writer-level investigation

## 2026-03-27 16:40 CET: Stale-window burn spike root cause

A second telemetry defect was verified in the live burn estimator itself.

Root cause:

- the burn estimator selected all quota samples from the last 15 minutes relative to wall-clock `now`
- then it computed slope using the oldest and newest sample in that shrinking set
- if traffic stopped after a burst, the left edge of the set kept sliding forward while the newest sample stayed fixed
- the same quota jump could therefore be recomputed over a smaller and smaller denominator, producing an inflated `pp/hr`

This is why a calm period could show a scary short-window burn like `84.8 pp/hr` without any new completed requests.

There was also a helper-side time bug:

- [statusline.py](/home/user/.claude/statusline.py) could refresh proxy quota samples using the current wall-clock time instead of the proxy snapshot timestamp

Implemented fix:

- in [statusline.py](/home/user/.claude/statusline.py):
  - anchor the burn window to the latest sample, not to `now`
  - require the latest sample to be fresh before reporting burn
  - write refreshed proxy quota samples using the snapshot timestamp
- in [burn.go](/home/user/glass-proxy/internal/debug/burn.go):
  - use the same latest-sample anchored window and freshness rule for proxy-side burn reporting

Verification:

- real statusline entrypoint after patch:
  - `python3 ~/.claude/statusline.py`
  - reported `Burn: 8.0 pp/hr | Window: 15m`
- before the patch, the same entrypoint had reported `84.8 pp/hr | Window: 2m`
- `go test ./internal/debug` passed
- rebuilt binary:
  - [/home/user/glass-proxy/glass-proxy](/home/user/glass-proxy/glass-proxy)

## 2026-03-27 16:46 CET: Real burn burst on one hot lane

A later `50.7 pp/hr` reading was verified as a real short burst, not a telemetry artifact.

Evidence:

- real statusline entrypoint:
  - `Burn: 50.7 pp/hr | Window: 5m`
- proxy snapshot:
  - `rl_5h_utilization=22`
  - `statusline_burn.pp_hr=50.7`
  - `statusline_burn.window_min=4.7`
- quota snapshot progression:
  - `16:41:30` → `18%`
  - `16:43:46` → `19%`
  - `16:44:06` → `20%`
  - `16:44:14` → `22%`
  - `16:46:13` → `22%`

The burst was driven by a single `agent_tool` conversation lane: `ddbbe66d7d3b_47276`.

Correlated request rows:

- `16:41:29` — `cache_create=112119`, `cache_read=26772`, `eff=19.3%`
- `16:43:46` — `cache_create=103077`, `cache_read=20804`, `eff=16.2%`
- `16:44:06` — `cache_create=103077`, `cache_read=20782`, `eff=16.1%`
- `16:44:14` — `cache_create=0`, `cache_read=123859`, `eff=95.7%`
- `16:44:20` — `cache_create=0`, `cache_read=123859`, `eff=94.1%`
- `16:46:13` — `cache_create=0`, `cache_read=123859`, `eff=93.1%`

Interpretation:

- the lane was warm again by the time the user inspected it (`93%` cache on the latest call)
- but the prior 5-minute window still contained two large back-to-back recache calls, so burn remained high for that short interval

Most important forensic signal:

- this burst was **not** caused by a long idle TTL gap
- it followed a real stable-prefix rewrite on that lane:
  - `[LOCALCACHE] PREFIX CHANGED ... diverge_at=msg[18]`
  - `[GLASS-DIAG] FINAL PREFIX CHANGED ... diverge_at=msg[16]`
  - `sys_changed=false`
  - `tools_changed=false`

That points to a message-body change inside the anchored prefix for an `agent_tool` lane. The next root-cause target is the message-prefix rewrite path, not serializer, not `small_system`, and not the burn estimator.

## 2026-03-27 17:05 CET: PRIME-GOLDEN method vs current production compression

Read-only comparison shows the current production behavior matches the older bounded-break batch model, not the stronger eager-compression method later proven in PRIME-GOLDEN tests.

What PRIME-GOLDEN proved:

- [compression_golden_test.go](/home/user/glass-proxy/internal/glass/compression_golden_test.go) Test 17:
  compressing messages Anthropic already saw causes a prefix break
- Test 19:
  clamped batch compression gives bounded extra breaks at watermark advances
- Test 21:
  eager compression before Anthropic ever sees those messages eliminates compression-caused inter-request breaks

Current production behavior:

- [session.go](/home/user/glass-proxy/internal/glass/session.go) defaults `CompressionBatch()` to `16`
- live config has no `compression_batch_size` override, so batch `16` is active
- [process.go](/home/user/glass-proxy/internal/glass/process.go) still advances the watermark only in batch jumps and then compresses in place

Recovered historical drift:

- [text-GLASS-BACK2RESEARCH-5.md](/home/user/glass-proxy/text-GLASS-BACK2RESEARCH-5.md) records a Mar 24 change:
  "Implemented eager compression in process.go:809-840. Wrote Test 21 ... Cache A = 7 breaks, Cache B = 0 breaks."
- The same document records that the eager fix then regressed badly in live use:
  break count rising from `~43` to `315`, with the stated root cause that the clamp used the new anchor and compressed messages that Anthropic had already cached uncompressed.
- [FULL_INVESTIGATION_RESULTS.txt](/home/user/glass-proxy/analysis/FULL_INVESTIGATION_RESULTS.txt) and [snippet_backup_diffs.txt](/home/user/glass-proxy/analysis/snippet_backup_diffs.txt) both preserve the Mar 24 note:
  `"FIX-2026-3-24-v2: Replaced batch-boundary watermark advancement with ..."`
- Current code no longer matches that eager variant; it is back on default batch-watermark advancement.

Conclusion:

- relative to the current code, the burst behavior is an intentional consequence of batch watermark advances
- relative to the strongest PRIME-GOLDEN result, production is not on the zero-extra-break eager method
- the historical record shows eager compression was tried, declared correct in tests, then reported as a live regression and abandoned or overwritten later
