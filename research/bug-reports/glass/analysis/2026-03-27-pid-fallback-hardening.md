# PID Fallback Hardening

Date: 2026-03-27

Purpose:

- turn the replay audit into concrete hardening passes
- remove side paths that were recomputing weaker request identity than ingress
- replace the most dangerous serializer fallback with a safer one
- keep the change set narrow and regression-tested

## What was hardened

### 1. Streaming subagent handling now prefers ingress / Glass classification

Added helper:

- [request_keys.go](/home/user/glass-proxy/internal/proxy/request_keys.go#L38)

Behavior:

- `effectiveStreamingSubagent(...)` now prefers `glass.ProcessResult.Subagent` from request context
- only falls back to `subagent.Classify(reqBody)` when the stronger ingress-derived result is unavailable

Wiring:

- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go#L1039)

Why this matters:

- the streaming path was previously re-classifying with less context than ingress
- that weaker path cannot see `HasEstablishedParent`
- the replay audit showed that parent-memory loss erases `agent_tool` splits

### 2. SSE / streaming conv-id fallback now prefers request-context identity over pidless hashing

Added helper:

- [request_keys.go](/home/user/glass-proxy/internal/proxy/request_keys.go#L28)

Behavior:

- `effectiveStreamingConvID(...)` now uses:
  1. context conv id
  2. `glass.ProcessResult` request/session/affinity key
  3. only then `SessionFingerprint(reqBody, 0)`

Wiring:

- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go#L1044)
- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go#L1206)

Why this matters:

- the old fallback could silently drop PID and subagent suffix information
- the replay audit showed that pidless ingress recomputation can collapse lanes and create misleading cache metrics

### 3. Serializer pre-Glass conv-id derivation now has an ingress-aware path

Added helpers:

- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L590)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L629)

Wiring:

- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go#L586)

Behavior:

- proxy ingress now passes its already-computed `subagentInfo` into serializer conv-id derivation
- this removes one more place where the request body was being re-classified with less context than ingress

Why this matters:

- full-system `agent_tool` requests depend on `HasEstablishedParent`
- the old serializer conv-id helper could only see `subagent.Classify(body)` without that context

### 4. Unknown-PID and unmapped subagents now use a self-gate fallback instead of full serializer bypass

Added helpers:

- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L217)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L233)

Behavior:

- when PID → parent mapping exists, subagents still gate on the parent and fall through to the parent-aware serializer path
- when parent mapping is unavailable, subagents no longer fully bypass coordination
- instead they acquire a self-gate keyed by their own serializer convID, then pass through without joining the global cross-session batch queue
- this behavior now applies in both enabled and disabled serializer modes

Wiring:

- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L261)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L303)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L453)

Why this matters:

- it preserves the intentional "do not queue behind unrelated main-session traffic" behavior
- but removes the most fragile part of the old fallback: repeated unknown-PID subagents from the same fallback lane can no longer interleave freely
- that is a better fit with the replay audit, which showed PID loss as dangerous primarily because it weakens lane identity and subagent isolation

### 5. Serializer health now exposes fallback-path counters

Wiring:

- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L97)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go#L678)
- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
- [main.go](/home/user/glass-proxy/cmd/glass-proxy/main.go)

Added counters:

- `requests_subagent_fallback`
  - incremented when serializer uses the self-gate fallback for an unknown-PID or unmapped subagent
- `requests_subagent_no_gate`
  - incremented only for the true last-resort passthrough when no usable gate key exists at all

Why this matters:

- we no longer have to infer fallback frequency from logs alone
- this gives us a direct way to tell whether the live proxy is mostly running on healthy parent-affinity paths or often dropping into degraded fallback handling
- the running proxy previously had no HTTP route exposing serializer health, so live verification of these counters required this extra endpoint wiring

Live inspection route:

- `/debug/serializer`

## Regression Coverage

Added tests:

- [serializer_test.go](/home/user/glass-proxy/internal/serializer/serializer_test.go#L283)
  - proves the ingress-aware serializer conv id separates `agent_tool` from parent scope where the legacy helper collapses them
- [serializer_test.go](/home/user/glass-proxy/internal/serializer/serializer_test.go#L115)
  - proves unknown-PID subagents still bypass the active global batch
- [serializer_test.go](/home/user/glass-proxy/internal/serializer/serializer_test.go#L148)
  - proves the fallback path now self-gates repeated unknown-PID subagents instead of letting them interleave freely
- [serializer_test.go](/home/user/glass-proxy/internal/serializer/serializer_test.go#L189)
  - proves the same fallback gate remains active when `ser_enabled=false`
- [serializer_test.go](/home/user/glass-proxy/internal/serializer/serializer_test.go#L219)
  - proves the true no-gate passthrough is still tracked separately
- [stream_identity_test.go](/home/user/glass-proxy/internal/proxy/stream_identity_test.go#L13)
  - proves streaming conv-id uses `glass.ProcessResult` before a pidless fallback
- [stream_identity_test.go](/home/user/glass-proxy/internal/proxy/stream_identity_test.go#L46)
  - proves context conv id still wins when present
- [stream_identity_test.go](/home/user/glass-proxy/internal/proxy/stream_identity_test.go#L59)
  - proves streaming subagent classification prefers the stronger Glass result over pidless re-classification

Verified on 2026-03-27:

```text
ok  	proxy.local/app/internal/serializer
ok  	proxy.local/app/internal/proxy
```

Commands:

- `/usr/local/go/bin/go test ./internal/serializer -run 'TestConvID|PrimeGolden'`
- `/usr/local/go/bin/go test ./internal/serializer -run 'TestSerializerSubagentUnknownPID|TestSerializerDisabledSubagentUnknownPID|TestSerializerSubagentUnknownPIDBypassesActiveBatch|PrimeGolden|TestConvID'`
- `/usr/local/go/bin/go test ./internal/proxy -run 'TestEffectiveStreaming|TestProxyBlocksStaleNoNewMessageRequestAfterInterrupt|TestDeriveRequestKey|TestShouldUpdateGlassTokens|TestEnsureAnthropicBeta'`
- `/usr/local/go/bin/go test ./internal/serializer`
- `/usr/local/go/bin/go test ./internal/proxy`

## What remains risky

This is still not full closure.

The most important serializer branch is improved, but not eliminated.

What remains as the last-resort behavior:

- if a subagent has no usable gate key at all, serializer still preserves the non-blocking passthrough fallback

The broader unresolved risk is now less about this one explicit bypass and more about how often PID/parent affinity is absent in real traffic and whether additional observability or replay should be added around those fallback events.

## Live verification: restarted process was still serving a stale binary

Verified on 2026-03-27 after a user restart:

- the running process on `:19999` was `/home/user/glass-proxy/glass-proxy`
- the serving binary mtime was still `2026-03-27 12:16:22 +0100`
- the new runtime log began at `2026-03-27 12:53:37 +0100`
- startup logged `[DEBUG-API] Registered /debug/* endpoints (13 routes)`
- `GET /debug/serializer` returned `HTTP/1.1 404 Not Found`

This proves the restart did not pick up the later serializer debug endpoint wiring, even though the source had already been updated.

The cause is also verified in the startup scripts:

- [start.sh](/home/user/glass-proxy/start.sh)
- [start-stack.sh](/home/user/glass-proxy/start-stack.sh)
- [codex-glass](/home/user/glass-proxy/bin/codex-glass)

All three only rebuild when the binary is missing, or when an explicit build flag is used. A normal restart therefore reuses whatever `/home/user/glass-proxy/glass-proxy` binary already exists.

One more operational detail was also verified:

- plain `go build ...` failed in the shell environment with `go: command not found`
- rebuilding required the explicit path `/usr/local/go/bin/go`

That means "restart succeeded" is not enough evidence that new code is live. For this proxy, live verification must include at least:

- binary mtime
- route surface or handler presence
- runtime log startup signature

After this check, the on-disk binary was rebuilt explicitly with:

```bash
/usr/local/go/bin/go build -o /home/user/glass-proxy/glass-proxy ./cmd/glass-proxy
```

The rebuilt binary mtime became `2026-03-27 12:56:13 +0100`, so the next restart can pick up the current source.

## Live verification: rebuilt binary is now serving traffic

Verified after the next restart on 2026-03-27:

- process on `:19999` was `pid=49047`
- executable path was `/home/user/glass-proxy/glass-proxy`
- executable mtime was `2026-03-27 12:56:13 +0100`
- `/debug/serializer` returned `HTTP/1.1 200 OK`

Live serializer payload:

```json
{
  "active_conv":"",
  "batch_remaining":0,
  "batch_size":5,
  "batch_switches":0,
  "enabled":false,
  "queue_depth":0,
  "requests_passthrough":0,
  "requests_queued":0,
  "requests_released":0,
  "requests_subagent":0,
  "requests_subagent_fallback":0,
  "requests_subagent_gate_wait":0,
  "requests_subagent_no_gate":0,
  "requests_timeout":0,
  "requests_total":0
}
```

What that does and does not prove:

- it proves the new debug endpoint is live
- it proves the running process is the rebuilt binary
- it does NOT prove the PID-fallback hardening path has been exercised since restart

The fresh runtime log showed only post-restart main-session traffic so far:

- `SER-DEBUG ... pid=45068 sub=false`
- local cache restored for `conv=28a034b5507a_45068`
- transport-pool created `session 28a034b5507a`
- force-thinking set `budget=31999`
- second request raised `max_tokens` to `32000`

So the thinking-budget fix is live and active on the rebuilt process.

However, those post-restart upstream requests hit `529 overloaded_error`, and the debug DB still had no request rows newer than the pre-restart timestamp `2026-03-27T12:57:33.599321468+01:00`.

That means:

- `/debug/latest` and parts of `/debug/status` are still reflecting the last completed request from the previous process
- the restarted process has not yet produced a completed fresh request that updates the DB-backed status surfaces
- serializer counters staying at zero is currently consistent with "no subagent fallback path exercised yet" and "serializer disabled mode with only main-session traffic observed"

## Live verification: completed post-restart traffic now confirms subagent gate behavior

Verified shortly after on 2026-03-27:

- `/debug/serializer` still showed `enabled=false`
- `/debug/serializer` showed `requests_subagent_gate_wait=9`
- `/debug/serializer` still showed `requests_subagent_fallback=0`
- `/debug/serializer` still showed `requests_subagent_no_gate=0`

This is a strong result.

It means the restarted proxy is not taking the degraded fallback path in the observed traffic. It is using the intended disabled-mode per-parent subagent gate.

The log evidence matches the counter exactly:

- nine `SUBAGENT_GATE_DISABLED_WAIT` events were logged
- repeated matching `SUBAGENT_GATE_DISABLED_ACQUIRED` events followed

Representative log lines:

- `13:05:19.894325 [SER] SUBAGENT_GATE_DISABLED_WAIT pid=47276 gate=2479d66b`
- `13:05:21.342277 [SER] SUBAGENT_GATE_DISABLED_ACQUIRED pid=47276 gate=2479d66b wait=1.4s`
- `13:05:27.703477 [SER] SUBAGENT_GATE_DISABLED_ACQUIRED pid=47276 gate=2479d66b wait=7.8s`
- `13:05:36.530071 [SER] SUBAGENT_GATE_DISABLED_ACQUIRED pid=47276 gate=2479d66b wait=15.0s`
- `13:05:59.647690 [SER] SUBAGENT_GATE_DISABLED_ACQUIRED pid=47276 gate=2479d66b wait=38.0s`
- `13:06:23.824475 [SER] SUBAGENT_GATE_DISABLED_ACQUIRED pid=47276 gate=2479d66b wait=55.6s`

The debug DB also confirms completed post-restart requests:

- `25862` `2026-03-27T13:05:43.137269425+01:00`
  - `conv=28a034b5507a_45068`
  - `subagent_type=agent_tool`
  - `cache_read=53332`
  - `cache_create=0`
  - `cache_efficiency=83.95%`
  - `thinking_budget=31999`
  - `thinking_tokens_used=23`
- `25864` `2026-03-27T13:06:23.838182692+01:00`
  - `conv=e0a2ba8019e9_47276`
  - `subagent_type=small_system`
  - `cache_read=0`
  - `cache_create=0`
  - `stop_reason=end_turn`
  - `thinking_budget=31999`
  - `thinking_tokens_used=14`

So, as of this verification window:

- the max-tokens / thinking-budget fix is live
- the rebuilt binary is live
- subagent traffic is being gated under `ser_enabled=false`
- the degraded PID fallback path is not what is handling the observed requests

What is still not solved:

- cache health remains unhealthy overall
- `/debug/cache-health` still reported `healthy=false`
- repeated large cache creates are still present in the 15-minute window
- one recent `agent_tool` request still showed `cache_create=22552` with `cache_read=0`

So the hardening changes hold water for the specific PID/subagent-gate issue, but they do not by themselves eliminate the broader cache-instability problem.
