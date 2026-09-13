# Provider lanes

Glass shares a process and some supporting components, but request ownership follows protocol and transport. The route table is in [proxy.go](../internal/proxy/proxy.go), and service assembly is in [main.go](../cmd/glass-proxy/main.go).

## Route and behavior map

| Route | Behavior in the published source |
|---|---|
| Anthropic message requests | Anthropic proxy pipeline and Session Glass engine, unless configured for passthrough |
| HTTP POST ending in `/responses`, with `input` rather than `messages` | Codex HTTP item-management pipeline |
| WebSocket GET upgrade ending in `/responses` | Codex native frame relay, with telemetry/optional trace capture |
| HTTP POST ending in `/responses/compact` | Native compact payload forwarded upstream |
| Gemini `:generateContent` / `:streamGenerateContent`, with `contents` | Gemini content-management pipeline |
| HTTP POST ending in `/chat/completions` | OpenAI-compatible context-management path |
| HTTP POST under `/openrouter/` | OpenRouter forwarding route |

More specific route checks precede the ordinary reverse-proxy path. Startup mode affects defaults and services; it does not mean that only the named route exists.

## Anthropic / Claude

The state object is a Messages-style conversation: `system`, `tools`, and `messages`. The [Glass engine](../internal/glass/process.go) owns canonical ingestion, selective compression, eviction, pinned views, and chapter/shadow recovery. Its cache modes are detailed in [cache and context mechanics](cache-and-context-mechanics.md#anthropic-cache-modes).

Claude services own the prefix warmer and rolling summarizer. These are not generic background services applied to every provider. In Codex startup mode, the process disables those Claude service attachments while continuing to assemble the Claude route.

Native Anthropic usage separates cache reads, cache creation, and uncached input. Glass also contains client-facing usage/header rewriting. Research and operational diagnosis must distinguish native upstream usage from a rewritten display surface.

## Codex: three paths, not one

Source: [handler.go](../internal/codex/handler.go), [session.go](../internal/codex/session.go), and [WebSocket telemetry](../internal/codex/websocket_telemetry.go).

### HTTP Responses pipeline

The normal POST path parses `instructions` and an `input` item array. Items include messages, function calls, function-call outputs, and other Responses objects. This path removes `context_management` and `previous_response_id`, sets `store=false`, filters compaction items, ingests local state, and applies compression/eviction before rebuilding the outgoing input.

That is a local semantic override, not a description of every native Codex request. The April capture record observed native continuation through `previous_response_id` and incremental tool output. [R9](research-method-and-provenance.md#historical-source-catalogue)

### WebSocket path

The handler branches to WebSocket forwarding before the normal POST mutation pipeline. With tracing or debug recording active, frame payloads are observed and then relayed; otherwise a reverse proxy handles the upgrade. `proxyWebSocketFrames` writes the received payload to the peer without passing it through the HTTP context-rebuild path.

Consequently, the HTTP branch's removal of continuation IDs and compaction items must not be attributed to WebSocket traffic. Telemetry can observe native `response.create`, response phases, tool-call events, token details, and rate-limit events without changing their payloads.

### Native compact endpoint

`handleCompactRequest` forwards the compact body and returns the upstream result. It does not route that body through the ordinary item-compression pipeline.

The historical captures observed `response.compaction` with an `output` array, and later continuation containing an opaque `compaction` item. A literal field named `replacement_history` was not established by those samples. This protocol history explains why wrappers' broad anti-compaction descriptions should not be read as an exact description of every route in the current handler.

### State and accounting

The managed session owner uses live memory with disk snapshots and shadow batches. Its public lane status reports resumable sessions as unsupported. Snapshot files and diagnostic replay templates should not be described as automatic live-session restoration.

Responses usage contains cached-input and reasoning-token details; the observed client also emits `codex.rate_limits`. Those differ from Anthropic headers and cache-creation counters. Glass retains native telemetry handling rather than inventing one interchangeable provider metric.

## Gemini

Sources: [handler.go](../internal/gemini/handler.go), [session.go](../internal/gemini/session.go), and [cache.go](../internal/gemini/cache.go).

Gemini uses `contents` with `user`/`model` roles and `parts` such as text, function calls, and function responses. Its handler owns a separate content store, compression/eviction path, snapshots, optional summaries, and optional explicit CachedContent management.

CachedContent is an explicit provider object, not an Anthropic message breakpoint under another name. The presence of an enable flag or handler does not make that feature active in every deployment. The lane contract describes live memory with disk snapshots and does not advertise resumable sessions.

Gemini shadow rendering is a compact human-readable view: text can be clipped and function objects reduced to descriptions. Do not treat it as a lossless input archive.

## OpenAI-compatible and Ollama

Sources: [openai_handler.go](../internal/proxy/openai_handler.go) and [openai_lane_service.go](../internal/proxy/openai_lane_service.go).

This path handles Chat Completions messages with an in-memory session owner, context-budget checks, selected cleanup/compaction, eviction, and streaming or non-streaming relay. Tool allowlisting, when configured, is applied before the budget decision.

It does not run the Anthropic classifier, canonical system processor, serializer, cold gates, prefix warmer, or usage-spoofing pipeline. Its session state and replay template are live-memory state, not a disk-backed resumable conversation.

Ollama is a control-plane label over this shared implementation. It is not an additional independent handler with a separate caching algorithm. A local model server's actual cache behavior remains that server's concern.

## OpenRouter

[openrouter_handler.go](../internal/proxy/openrouter_handler.go) provides a dedicated forwarding prefix and passes client authentication upstream. It does not invoke the OpenAI-compatible context owner merely because its payload may resemble Chat Completions.

## Runtime and security boundaries

`GLASS_RUNTIME_LANE`, `GLASS_RUNTIME_ROOT`, and `GLASS_CAPTURE_ROOT` configure runtime path resolution. The production wrappers also retain their own path choices, and assembled providers can derive storage paths from the process configuration. Review [runtimepaths.go](../internal/runtimepaths/runtimepaths.go), process assembly, and the relevant wrapper together rather than assuming route names alone isolate all files.

The process is single-user. Lane separation is not a multi-tenant security boundary. Credentials, debug forwarding, conversation snapshots, and optional capture files must remain private. See [SECURITY.md](../SECURITY.md).


## Capability matrix

| Surface | Context owner | Persistence | Archive/recovery | Native continuation/compact | Recorded verification boundary |
|---|---|---|---|---|---|
| Anthropic Messages | Session Glass LocalCache | Session/cache snapshots with compatibility resets | Shadow, chapters, bookmarks, optional recovery summaries/gate | Anthropic messages plus optional context editing | Extensive historical live incidents and source paths; no universal long-session benchmark |
| Codex HTTP Responses | Codex item session | Live memory with disk snapshots | Shadow and optional summary injection | Glass strips selected native continuation/context fields on managed POST path | Captured protocol corrected earlier assumptions; compatibility override remains explicit |
| Codex WebSocket | Native frame relay with telemetry observation | Telemetry/session observations | Capture when configured; no HTTP item mutation | Native `response.create` continuation | Phase 0 live capture and later telemetry tests recorded |
| Codex compact | Native forwarding | Provider response plus capture state | Not the ordinary local compression path | Native `/responses/compact` | Phase 0 compact exchange recorded; breadth remains version/path specific |
| Gemini | Gemini contents session | Live memory with disk snapshots | Compact shadow and optional summaries | Provider-native generate/stream; optional CachedContent | Source/build/startup records; no authenticated long-session proof in historical transcript |
| OpenAI-compatible/Ollama | Request-authoritative/in-memory message owner | In-memory | Historical target for chapters, not Anthropic-equivalent recovery | Chat Completions stream | OMP bounded-view and heartbeat experiments recorded; final real stall remains in history |
| OpenRouter | Forwarding route | None owned by this route | None | Upstream-defined | Route/source behavior only |

## Security-guard coverage

The shared process includes guard integration for Anthropic and OpenAI-compatible ingress and tool responses. The public source record does not establish identical guard coverage for every Codex/Gemini/WebSocket/compact/OpenRouter branch. Actual protection depends on route, enabled configuration, scanner availability, and the separate host execution barrier described by GLASSDD.

## Lane-separation failures retained

- Codex WebSocket originally fell through an Anthropic/default route.
- Partial Codex defaults produced a zero batch-size panic.
- Root isolation succeeded before status/debug schema isolation.
- Codex output later contained Claude-specific Haiku/Sonnet fields.
- Gemini design/build records did not include a complete authenticated long-session proof.
- OpenAI-compatible and native Codex paths were repeatedly confused despite different protocols.
- Startup mode and route availability are different; one multi-lane process can still expose all routes.

See the [complete chronology](complete-chronology.md) and [problem-to-solution lineage](problem-solution-lineage.md).
