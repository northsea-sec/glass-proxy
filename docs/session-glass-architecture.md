# Session Glass architecture

This document describes the published implementation and makes design/implementation differences explicit. For the complete origin and evolution, see the [complete chronology](complete-chronology.md), [problem register](problem-register.md), and [solution register](solution-register.md).

## Architectural objectives

Session Glass was designed to:

- give canonical session state one proxy owner;
- preserve provider-valid message and tool structure;
- keep cache-sensitive regions stable between deliberate changes;
- bound active working context;
- preserve displaced history outside that context;
- separate conversation identity from reusable prefix identity;
- expose evidence rather than infer one universal provider mechanism.

## Process and component ownership

| Component | Responsibility |
|---|---|
| `cmd/glass-proxy/main.go` | Process assembly, configuration, listeners, provider services, diagnostics, lifecycle |
| `internal/proxy/proxy.go` | Route selection and Anthropic integration |
| `internal/glass/process.go` | Session identity, canonical ingestion, compression, budget, eviction, archive/recovery integration, final request view |
| `internal/glass/localcache.go` | Canonical message positions, references, anchors, pinned views, snapshots, structure repair |
| `internal/glass/session.go` | Session metadata, eviction state, observed token state, watermark, recovery metadata |
| `internal/glass/shadow.go` | Append-oriented rendered shadow and batch index |
| `internal/glass/chapter.go` | Numbered chapter JSON/Markdown and chapter index |
| `internal/glass/summarizer.go` | Optional rolling chunks, stitched recovery file, recovery-gate state |
| `internal/glass/prefix_registry.go` | Reusable model/system/tools/protocol profiles |
| `internal/claude/services.go` | Claude-only warmer and summarizer ownership |
| `internal/codex/`, `internal/gemini/` | Provider-native lane state and handlers |
| `internal/proxy/openai_handler.go` | OpenAI-compatible/Ollama state, budgeting, and streaming |

## Anthropic request path

```text
request normalization and dedup
→ PID/parent-aware classification and identity
→ model/subagent gates
→ per-affinity MCP/tool handling
→ Session Glass or explicit passthrough
→ canonical system/message processing
→ batched compression and optional summary-assisted flush
→ first eviction or later pinned-frame overflow
→ shadow/chapter/recovery writes
→ bookmark and bounded request view
→ cache-control placement or context_api directives
→ final structure validation
→ configured post-Glass transforms
→ upstream forwarding and native usage/telemetry handling
```

Branches differ for small-system traffic, isolated/frozen subagents, passthrough, replay probes, context modes, streaming, and invalid requests.

## Canonical session state

`LocalCache.Ingest` deep-copies newly encountered messages, applies canonical cleanup, and retains proxy-owned positions when the client resends history. References preserve position/state rather than allowing previously evicted content to be reintroduced blindly.

Cache position, original source position, and outbound index can diverge after references, merged turns, pinned-frame selection, or repair. Breakpoint calculation must therefore follow the built view rather than raw message count.

## Compression and overflow

In `full` mode, compression advances in batches under configured watermark and anchor constraints. Idempotence makes already-transformed content stable after transformation. Every batch transition remains a deliberate change.

The first overflow uses a token-targeted eviction. Once a session is shadow-backed, later overflow can pin the bridge between fixed anchors and the recent tail. Post-overflow requests use a bounded pinned view rather than serving every retained message.

## Structure and fallback

The builder repairs tool boundaries and same-role adjacency created by non-contiguous selection. Final validation checks provider message requirements.

If a transformed view is invalid, the fallback logic may restore the original client messages when they are valid and within its size guard. This is a documented safety/continuity tradeoff: it avoids one malformed request but can undermine bounded-context intent. The problem/solution registers preserve the historical reset/reingest loop that led to current guards.

## Identity split

`RequestMeta` carries identity outside provider JSON:

- `SessionKey` — conversation-local state and history;
- `RequestKey` — request/subagent bookkeeping;
- `AffinityKey` — related scheduling/transport work;
- PID and parent/classification metadata;
- auth scheme and protocol fields.

`PrefixKey` derives from material intended to be reusable—model, normalized system, tools, and protocol metadata—without message history. A shared prefix profile cannot retrieve or merge another session’s conversation.

## Archive and memory layers

| Layer | Representation | Authority/limit |
|---|---|---|
| Inbound request | Client/provider-native object | Original request at that event; not retained publicly |
| LocalCache snapshot | Canonical proxy-owned state | Contains transformed/reference/compression state, not raw inbound bytes |
| Shadow Markdown | Extracted text by eviction batch | Long content may be clipped |
| Chapter JSON/Markdown | Ordered message roles/numbers, text, concise tool descriptions | Tool-result bodies become omission markers; not a lossless transcript |
| Chapter index | Paths, ranges, dates, batch metadata | Navigation only |
| Recovery summary | Model-generated structured account | Derived and lossy |
| Bookmark | Stable pointer/read instruction | Not the history |

The original design aimed for exact archival authority. The published implementation does not fully meet that ideal: compression may precede archival selection, rendered layers omit or clip content, and write success is not a transaction precondition for eviction.

## Recovery

The chapter writer creates numbered files by eviction. A bookmark points to the chapter directory. Optional rolling summaries accumulate bounded chunks and stitch a recovery file at eviction.

Recovery-gate state can clear when either the assistant Read call or its user tool-result contains the expected recovery file identity. This verifies protocol contact, not understanding or correct resumption.

## Prefix warming and cold coordination

Shared-prefix warming maintains model/system/tools profiles after real request auth has been captured. It does not warm every message history and cannot make remote cache entries permanent.

Session cold gates coordinate requests that share one local cache state. The ordinary serializer, per-parent subagent gates, shared prefix warmer, and hot message-lane warmer address different scopes and must not be conflated.

## Context modes and passthrough

- `full`: Glass compression and message cache-control path.
- `off`: no client-side compression or Glass message checkpoints; local ownership/eviction remains.
- `context_api`: provider context-editing directives replace local compression for selected clearing.
- `glass_passthrough`: bypasses the Anthropic Glass engine and many mutation stages; separate diagnostic/native control.

## Provider lanes

The assembled process supports:

- Anthropic Messages;
- Codex HTTP Responses;
- Codex WebSocket relay/capture;
- Codex native compact forwarding;
- Gemini generateContent;
- OpenAI-compatible Chat Completions/Ollama;
- OpenRouter forwarding.

A route’s presence does not imply identical context management, persistence, security guard coverage, or recovery semantics.

## Persistence and runtime roots

Anthropic LocalCache/session state has disk persistence and compatibility checks. Unsafe evicted/reference-bearing state may be reset rather than resumed unconditionally.

Codex and Gemini report live memory with disk snapshots while resumable sessions remain unsupported in their lane contracts. OpenAI-compatible session ownership is in-memory.

Runtime paths depend on selected lane/root and process assembly. Codex has a dedicated root design and recorded live isolation. Gemini/OpenAI root resolution and wrapper/process choices must be considered together.

## Design ideal versus published boundary

| Design ideal | Published implementation boundary |
|---|---|
| Exact archive before loss | Shadow/chapter are rendered and write failures do not transactionally block eviction |
| Stable bytes between deliberate changes | Compression, anchor movement, eviction, client changes, and TTL still create transitions |
| Chapters as memory authority | Recovery summary/gate and omission-based chapter projection coexist |
| Provider-native parity | Native handlers exist, but transport branches and capabilities remain unequal |
| Complete security gate | Guard availability, enablement, dependencies, route coverage, and host barrier determine actual protection |
| Useful effectively continuous sessions | Architectural goal and operator-observed success; not a universal duration/quality guarantee |
