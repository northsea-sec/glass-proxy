# Cache and context mechanics

Glass coordinates five resources that earlier versions repeatedly conflated:

1. active context sent to the model;
2. provider-observable prompt-cache creation and reads;
3. local canonical session state;
4. durable recovery material;
5. the model’s ability to recover and apply governing prior decisions.

Saving one does not automatically save the others.

## Provider-visible prefix contract

Anthropic documents prompt caching over cumulative request content in the order tools, system, and messages through cache-control breakpoints. Exact eligibility, minimum size, TTL, isolation, and usage reporting are provider contracts.

Glass observes outgoing content and provider-reported usage. It does not inspect the provider’s internal KV allocation, hardware, cache slots, or replacement policy. Historical LRU/backend explanations remain hypotheses unless an external primary source establishes them.

A local prefix hash proves local equality for the hashed representation. It does not guarantee a remote hit after TTL expiry, routing changes, different request settings, or provider decisions.

## The three cache-sensitive planes

### System

System fragments can change through client version, workspace state, feature gates, automatic memory, configured patches, and proxy normalization. Same-length replacement can satisfy the tested historical validation path while still changing content.

### Tools

Tool schemas and deferred-tool lists can change when integrations connect, disconnect, or are loaded on demand. MCP/tool persistence attempted to stabilize this plane. Client deferred-tool behavior remained an independent source of change.

### Messages

Messages grow each turn and can change through thinking removal, tool-result reduction, compression, eviction, role repair, references, summaries, and client compaction. Most local prefix divergence events in the recorded census occurred in this plane.

## Why a smaller request can cost more

```text
A: stable prefix P + tail T1
B: stable prefix P + tail T1 + T2
C: transformed P' + tail T1 + T2 + T3
```

`P'` may be shorter than `P`, but the first transition can require new cache creation. Later requests may reuse `P'` if it remains stable. This is why Glass distinguishes mutation cost from reuse after mutation.

## Canonical ingestion

The Anthropic LocalCache deep-copies newly encountered message positions and applies configured normalization once. It does not constitute a byte-exact archive of the inbound request. It is a proxy-owned working representation.

Canonicalization reduces repeated client-side variation. Compression is a later lossy transformation. Eviction changes visibility. Archival rendering records another representation. These are separate operations.

## Stage 1, P4/DCP, and Stage 2 lineage

The predecessor stack used:

- Stage 1 to strip old thinking and clip old tool/assistant text;
- P4/DCP to replace stale tool results with fixed placeholders;
- Stage 2 to drop whole message pairs near a higher threshold;
- persistent drops to stop client resend from restoring removed material;
- watermarks and cooldowns to control mutation cadence.

Stage 2 hysteresis recorded a verified improvement for its corrected configuration. The stack still required multiple interacting states. Session Glass later replaced client-resend mutation as the primary ownership model rather than simply selecting another Stage 2 threshold.

## Compression watermark

The watermark identifies an older region eligible for transformation. Batch advancement groups mutations and makes the transformed output stable between advances.

The precise historical claim is:

- idempotent compression preserves the same transformed bytes after a message has been compressed;
- a watermark advance changes newly included messages;
- the initial uncompressed-to-compressed transition remains a cache-sensitive change;
- local tests can verify sequence equality for their modeled request order;
- they cannot guarantee provider cache hits.

The current batch design is the survivor of a failed eager variant. It should not be described as “zero breaks” or “100% provider hits.”

## Cache breakpoint anchor

The anchor marks a message prefix selected for explicit reuse. Advancing it can create a transition and can also grow useful reusable coverage.

The threshold-80 experiment demonstrated the opposing costs: fewer movements but a tiny reusable prefix and large tail. Lower thresholds grew coverage faster but transitioned more often. Replays did not establish one universal optimum.

A previous anchor and a mapped watermark checkpoint can preserve layered regions when cache-control slots permit. Marker availability and exact request sequence remain part of the outcome.

## Compression experiments

### Eager compression

A three-variant local test recorded seven modeled compression changes for the old ordering and zero for the eager ordering. The eager implementation and regression test passed locally. Later live traffic regressed because the operational ordering/clamp still transformed material already observed in an earlier request. The variant was reverted.

Both results are retained: verified local model success and later live regression.

### Compression disabled

Across 138 recorded fixtures, compression-off more than doubled modeled total prompt mass and increased modeled uncached and cache-creation totals. Both on/off variants had zero exact captured-output matches. The result supports a comparative local tradeoff, not an exact provider billing prediction.

### Threshold replay

Threshold 16 produced the highest modeled reuse in the recorded comparison; threshold 40 produced the lowest modeled create/uncached totals; every tested threshold had zero exact matches. No single metric selected the winner.

## Eviction and pinned frames

First overflow selects a target-based eviction. Post-overflow pinned frames address a different problem: stopping an old bridge from remaining visible and changing before the hot tail.

A pinned frame combines anchors, a recovery entrance, and a bounded recent tail. Message structure must remain valid after non-contiguous selection. Role alternation and `tool_use`/`tool_result` pairing are one coupled invariant.

## Validation fallback

Restoring the original client messages can avoid forwarding an invalid transformed request. It can also undo eviction and resend an oversized history. Historical loops combined invalid view, fallback, reset, and reingest.

The public problem and solution registers retain this tradeoff rather than calling fallback automatically safe.

## Session and prefix identity

- `SessionKey` owns conversation-local messages and archive state.
- `RequestKey` identifies request/subagent bookkeeping.
- `AffinityKey` groups related scheduling/transport work.
- `PrefixKey` identifies genuinely shared model/system/tools/protocol material.

A shared prefix never authorizes shared conversation history. Losing PID or parent memory can collapse these distinctions and produce misleading cache metrics.

## Subagent cache policy

The recorded policy history includes:

- model-only detection;
- small-system classification;
- full-system agent-tool classification with established-parent context;
- isolated child state;
- parent affinity;
- disabled upstream message caching;
- per-parent gates;
- repeat-cache promotion and rollback;
- shared prefix warming;
- hot message-lane warming;
- unknown-PID self-gating.

Each solved a different relation. No one boolean describes the whole policy.

## Interleaving and serializer

Serializer batching was motivated by provider slot/LRU hypotheses. The recorded aggregate showed fewer switches without lower break rate, while some configurations caused long waits. Interleaved versus sequential replay produced identical local transitions in its corpus.

The correct conclusion is bounded: ordering did not cause local transformation differences in that replay; the tested scheduler did not show the expected aggregate benefit; hidden provider behavior was not directly observed.

## Client-side factors

Tengu/client research records changing attribution metadata, deferred tools, effort, compaction, memory, strict-schema, streaming, and global-cache controls. These can alter prompt shape, request volume, tool behavior, or output without a Glass source change.

Any experiment comparing days or sessions therefore needs client version, active tools, feature assignment where known, model/effort, and request class.

## Quota and metrics

Cache read, cache creation, uncached input, output, thinking, request rate, and quota percentage are different observables. Provider quota weighting remained unresolved in the reports.

Metric corrections include:

- replacing the invalid historical `read/input` efficiency formula;
- anchoring burn windows to the latest fresh sample;
- treating break/severe/cold values as thresholds;
- counting subagent request events separately from logical agents;
- retaining timezone offsets;
- carrying lane/session/request identity.

A high cache percentage can coexist with high total spend under high request density and large cached contexts.

## Context modes

| Mode | Client-side compression | Message cache controls | Additional behavior |
|---|---|---|---|
| `full` | Glass conditions apply | Glass manages message checkpoints | Local cache/context path |
| `off` | Skipped | No Glass message checkpoint | Local ingestion and budget eviction still operate |
| `context_api` | Skipped | No Glass message checkpoint | Adds Anthropic context-editing directives |

`glass_passthrough` is a separate diagnostic/native control that bypasses the Glass engine and many mutation stages. It is not another spelling of `off`.

## Recovery mechanics

Compression, eviction, shadow rendering, chapter projection, summarization, bookmark navigation, and recovery-gate state are distinct. Token savings do not measure semantic retention. A read event does not prove comprehension.

See [Memory and recovery](memory-and-recovery.md), [Experiments and results](experiments-and-results.md), and the [complete solution register](solution-register.md).
