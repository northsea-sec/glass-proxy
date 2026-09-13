# Current Glass vs Older PRIME Lessons

This file compares the current Glass codepath against the two older lessons that mattered most in the raw PRIME-GOLDEN lineage:

1. identity consistency across subsystems
2. same-model / agent-tool subagents must be classified correctly and prevented from creating competing Anthropic cache entries

The goal is not to retell the whole history. It is to answer a narrower question:

What survived?
What drifted?
What was re-broken?

## Evidence Used

- [text-PRIME-GOLDEN-22.md](/home/user/nataraja/text-PRIME-GOLDEN-22.md)
- [text-PRIME-GOLDEN-LIMIT-2.md](/home/user/nataraja/text-PRIME-GOLDEN-LIMIT-2.md)
- [serializer-analysis.md](/home/user/glass-proxy/docs/2026-3-16/serializer-analysis.md)
- [classifier.go](/home/user/glass-proxy/internal/subagent/classifier.go)
- [process.go](/home/user/glass-proxy/internal/glass/process.go)
- [request_meta.go](/home/user/glass-proxy/internal/glass/request_meta.go)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go)
- [fingerprint.go](/home/user/glass-proxy/internal/trimmer/fingerprint.go)
- [transport_pool.go](/home/user/glass-proxy/internal/proxy/transport_pool.go)
- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
- focused Go tests run on 2026-03-27

## Lesson A: Identity Consistency Across Subsystems

### The old lesson

The old system learned, the hard way, that "conversation identity" could not be allowed to diverge between the trimmer and serializer.

The classic failure was simple:

- one subsystem adopted PID-aware identity
- another kept using an older collision-prone identity model
- the fix looked real in one subsystem and false in another

### What survived

The current Glass stack does preserve the spirit of that lesson.

The biggest surviving improvements are:

- `SessionFingerprint(...)` and `ConvIDWithPID(...)` now share the same base primitive: `promptscope.Signature(...)`
- both append PID when available
- subagent classification is computed at proxy ingress and passed into Glass via `RequestMeta`
- Glass persistence logic also uses PID-aware state (`resolvePersistedSessionKey`, `pidSessionPins`, `seenClientPIDs`)

That is much better than the old "independent guessing" model.

### What changed

The current system does not use one universal `conv_id`.

Instead it uses several request identities on purpose:

- `requestSessionKey`
  Proxy ingress identity from `SessionFingerprint(...)`
- `sessionKey`
  Glass session lane, possibly with a subagent suffix
- `requestKey`
  Request-scoped key used to avoid collisions for uncached child traffic
- `affinityKey`
  Parent-affine key for transport and some shared resources
- `preGlassConvID`
  Serializer lane identity derived from the pre-Glass request body

This is not automatically bad.

It means the old "single stale hash" bug has been replaced by a role-based identity architecture.

### What survived cleanly

Proxy ingress:

- `requestSessionKey := trimmer.SessionFingerprint(body, clientPID)`
- `affinityKey` defaults to the same value
- `sessionKey` becomes `requestSessionKey + SessionSuffix` for isolated subagents
- classification is passed to Glass in `RequestMeta`

Glass:

- honors the passed classification instead of recomputing blindly
- persists by `sessionKey`
- keeps `AffinityKey` and `RequestKey` explicit in `ProcessResult`
- uses PID-aware session reuse logic on restart

Serializer:

- uses `ConvIDWithPID(...)`
- registers PID -> parent mappings
- gates subagents per parent
- batches by the pre-Glass identity, not by a stale post-mutation fingerprint

### Where drift still exists

The current design is more capable, but also more fragile.

The main drift points are:

- `preGlassConvID` is not the same as `sessionKey`
  That is intentional, but it means serializer correctness now depends on semantic alignment, not on a single shared id.
- unknown-PID or unmapped subagents bypass serializer batching entirely
  This is an explicit codepath in `serializer.Acquire(...)`.
- the streaming handler re-classifies with `subagent.Classify(reqBody)` and does not pass `HasEstablishedParent`
  That creates a side-path that is not using the exact same classification inputs as ingress.
- SSE/telemetry fallback uses `SessionFingerprint(reqBody, 0)` if request context lacks a conv id
  That drops PID and any subagent suffix.

### Verdict

This lesson mostly survived.

It survived as:

- stronger shared primitives
- explicit identity roles
- fewer independent guesses

It did not survive as:

- a single unified identity used everywhere

So the current risk is no longer "serializer still uses the old hash."
The current risk is "one of the intentional identity layers drifts semantically from the others."

## Lesson B: Same-Model Subagents Must Be Classified and Kept Out of Anthropic Cache Creation

### The old lesson

The older system had already learned two key truths:

- model-based subagent detection misses same-model subagents
- classification alone is not enough; those subagents must also be prevented from creating Anthropic cache entries

That older remedy pair was:

- detect correctly
- strip or avoid `cache_control`

### What survived

This lesson is strongly present in the current code.

Classifier:

- tool-bearing `small_system` requests with `< 10` messages get:
  - `IsSubagent = true`
  - `IsolateSession = true`
  - `DisableUpstreamCaching = true`
- full-system `agent_tool` requests with an established parent get:
  - `IsSubagent = true`
  - `Type = agent_tool`
  - `IsolateSession = true`
  - `DisableUpstreamCaching = true`

Proxy:

- after Glass and other mutations, it runs a safety net:
  - if `glassResult.Subagent.DisableUpstreamCaching`
  - then `glass.StripAllCacheControl(body)`

Glass:

- `BypassMessageCache` subagents get uncached pass-through
- isolated subagents get their own Glass lane
- any session key containing `_sub_` is forced to keep `DisableUpstreamCaching = true`

Serializer:

- subagents serialize with parent mapping when PID is known
- per-parent subagent gate limits concurrent child requests

Transport:

- per-affinity upstream transport pooling reduces cross-session transport-layer contention

### What improved

This lesson is actually embodied more completely now than in the older raw sessions.

The strongest improvements are:

- the shared classifier is threaded through proxy and Glass
- there is a post-Glass cache-control stripping safety net
- tests now explicitly cover agent-tool and small-system behavior

Focused tests that passed on 2026-03-27:

- `./internal/subagent -run 'PrimeGolden|AgentTool|Classifier'`
- `./internal/serializer -run 'PrimeGolden|AgentTool|Serializer|Config'`
- `./internal/glass -run 'PrimeGolden|Split|Process'`
- `./internal/proxy -run 'RequestKey|Lane|Interrupt|Subagent|Transport'`

### Residual risk

The key remaining risk is classification that depends on runtime context.

Examples:

- `agent_tool` detection with full system prompt depends on `HasEstablishedParent`
- if PID resolution fails or parent mapping is unavailable, that exact branch can be missed at ingress
- serializer explicitly bypasses batching for unknown-PID or unmapped subagents

This does not mean the older lesson was lost.
It means the current implementation still depends on runtime PID and parent-state quality.

### Verdict

This lesson survived well.

In fact, it survived better than the identity lesson.

The remaining weakness is not conceptual.
It is operational:

- PID-resolution quality
- parent-affinity continuity
- side paths that re-classify without the same context as ingress

## Transport-Isolation Timeline

This matters because it bridges design, implementation, and deployment.

- March 16:
  `serializer-analysis.md` already says per-session isolation can work now because Glass canonicalizes system bytes.
- March 25:
  `text-GLASS-BACK2RESEARCH-5.md` rediscovers that conclusion and calls out the single-transport problem.
- March 25, 17:15:25 +0100:
  `transport_pool.go` is created.
- March 27:
  current logs show `[TRANSPORT-POOL] Created transport ...` and `[WARN] Running without egress/sidecar`, meaning the direct path is live and the pool is active.

So the tight forensic statement is:

- known in design by March 16
- implemented on March 25
- active on the current direct deployment path

## Bottom Line

The current Glass codepath did not forget the older PRIME lessons.

What happened instead is more subtle:

- the key lessons survived
- some were implemented better than before
- the architecture became more layered
- and the remaining risk shifted from "obvious forgotten fix" to "boundary mismatch between multiple valid layers"

The most important current residual risks are:

- PID-dependent fallbacks
- side-path reclassification that does not use the same context as ingress
- observability fallbacks that can drop PID/suffix information
- deployment-path differences, especially when sidecar bypasses the transport pool

## Replay Addendum

The targeted replay audit on 2026-03-27 materially sharpened the bottom line.

What it showed:

- clearing `ClientPID` after capture is not the dangerous branch by itself
- the real break shows up when ingress metadata is recomputed without PID or without `HasEstablishedParent`
- that recomputation can:
  - erase `agent_tool` classification
  - collapse session lanes
  - merge affinity/request identities that should stay distinct

This matters because it confirms the current residual risk is exactly where the static audit suggested:

- ingress classification context
- parent-affinity continuity
- serializer fallback when parent mapping is unavailable
- side paths that re-classify without the same inputs as the main ingress path

So the old PRIME lessons survived, but the surviving weak point is now better defined:

- not a forgotten concept
- not one stale hash
- a live dependency on PID and parent-memory quality at subsystem boundaries
