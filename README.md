# Glass Proxy

Glass is a longitudinal prompt-cache, context-management, recovery, client-reverse-engineering, and provider-protocol research project embodied in a production proxy for AI coding agents.

It was built around one practical question:

> How can a coding agent preserve the decisions and evidence needed to continue useful work when active context is finite, client history is repeatedly resent or compacted, and changing old content can destroy prompt-cache reuse?

The project did not reach its architecture in one pass. It moved through mutable middleware, Stage 1/P4/Stage 2 context surgery, watermarks, session-owned state, whole-message eviction, shadows, facts, chapters, pinned frames, summaries, identity splits, replay-led falsification, Tengu/client analysis, and provider-native lanes. The public record includes the failures, regressions, reversals, and unresolved questions—not only the final source tree.

## Origin and contribution

The operator records that the work began in **early 2025** through Claude Thinking Audit, Full Spectrum Analyzer, and predecessor proxy/instrumentation research. The dense surviving engineering corpus is concentrated in January–April 2026; the March 4 Session Glass plan is a major formal redesign, not necessarily the invention date.

Glass’s defensible contribution is the integrated engineering program:

- proxy-owned canonical conversation state;
- explicit system, tools, and message cache-sensitive planes;
- deliberate mutation boundaries and watermark/anchor experiments;
- bounded pinned frames with external recovery material;
- separation of history, chapters, summaries, indexes, facts, and bookmarks;
- SessionKey, RequestKey, AffinityKey, and PrefixKey ownership;
- PID/parent-aware subagent identity and fallback analysis;
- replay and incident-period diff methodology;
- versioned Tengu/client feature-gate research;
- capture-first Codex protocol correction;
- provider-native Claude, Codex, Gemini, and OpenAI-compatible lanes;
- mechanical evidence/status discipline learned through repeated operational failure.

Independent prior work exists for external memory, long-context evaluation, compaction, observation masking, and prompt caching. The publication separates independent development and technical similarity from any claim of direct provider influence. See [Origin and contribution](docs/origin-contribution-and-external-context.md).

## The central engineering conflict

A long agent session accumulates tool output, corrections, failed experiments, decisions, and unfinished work.

- Keeping all history eventually exhausts the active context and buries useful signal.
- Summarizing everything can lose ordering, authority, qualifications, and negative results.
- Rewriting older content can reduce prompt size while changing bytes covered by a reusable prompt prefix.
- A stable local hash does not guarantee provider cache reuse.
- An archive existing on disk does not guarantee that the agent reads or correctly applies it.

Glass treats active context, prompt-cache reuse, durable history, recovery, and session quality as different resources that must be coordinated.

## Current architecture in brief

The Anthropic path owns a per-session canonical message cache. It ingests new positions, applies configured normalization, batches selected compression, manages message breakpoints, performs initial eviction and later pinned-frame overflow, writes shadow/chapter recovery material, and builds a bounded provider-facing view. Optional summaries and a recovery gate support navigation without making a summary the only historical record.

> **Facts are bookmarks; chapters are memory.**

That maxim is a design objective, not a claim of lossless implementation. Current shadow and chapter rendering has documented fidelity limits, and archive writes are not transactionally required before eviction proceeds. See [Memory and recovery](docs/memory-and-recovery.md).

## Provider surfaces

| Surface | Current role |
|---|---|
| Anthropic / Claude Messages | Session Glass canonical cache, compression, eviction, pinned frames, chapter/shadow recovery, cache modes |
| Codex Responses over HTTP | Item-aware local context management and telemetry |
| Codex Responses over WebSocket | Native frame relay with telemetry and optional capture; separate from HTTP mutation |
| Codex compact endpoint | Native compact forwarding |
| Gemini `generateContent` | Contents/parts context management and optional explicit CachedContent |
| OpenAI-compatible Chat Completions | Request-authoritative context budgeting, cleanup, eviction, and streaming relay |
| Ollama | Control-plane label over the shared OpenAI-compatible owner |
| OpenRouter | Dedicated forwarding route, not a state-owning context lane |

These surfaces do not imply identical cache behavior, persistence, security filtering, or feature parity. See [Provider lanes](docs/provider-lanes.md).

## Complete research record

Start here:

1. [Complete chronology](docs/complete-chronology.md) — every documented era, incident, experiment, correction, regression, and provider transition.
2. [Filed bug reports](docs/filed-bug-reports.md) — actual report files and their relationships.
3. [Problem register](docs/problem-register.md) — thirty cross-source mechanisms plus all 118 bug/incident-bearing source records.
4. [Solution register](docs/solution-register.md) — seventy proposed, verified, failed, reverted, superseded, dormant, and surviving solutions.
5. [Problem-to-solution lineage](docs/problem-solution-lineage.md) — every problem family connected to its response chain.
6. [Experiments and results](docs/experiments-and-results.md) — tests, database analyses, replays, captures, and live observations with recorded outcomes.
7. [Complete source catalogue](docs/source-catalogue.md) — all 190 documentary candidates, including exclusions and evidence limits.

Technical chapters:

8. [Cache and context mechanics](docs/cache-and-context-mechanics.md)
9. [Memory and recovery](docs/memory-and-recovery.md)
10. [Tengu and coding-client research](docs/tengu-and-client-research.md)
11. [Session Glass architecture](docs/session-glass-architecture.md)
12. [Provider lanes](docs/provider-lanes.md)
13. [Failures and lessons](docs/failures-and-lessons.md)
14. [Research chronology narrative](docs/research-chronology.md)
15. [Origin, contribution, and external context](docs/origin-contribution-and-external-context.md)
16. [Research scope and evidence rules](docs/research-scope-and-evidence.md)
17. [Research method and provenance](docs/research-method-and-provenance.md)

## What the record establishes

- The project developed and operated multiple generations of context and cache control.
- Several report-local fixes were verified in their recorded configurations.
- Later configurations sometimes regressed those behaviors; both events are retained.
- Replay directly corrected several local causal explanations.
- Tengu/client behavior accounted for request changes outside Glass control.
- Codex wire capture overturned important protocol assumptions.
- Session ownership, split identity, provider-native lanes, and capture-first analysis survive as central architectural ideas.
- The research produced useful long-session operation according to operator experience, but the public record does not convert that testimony into a universal benchmark.

## What the record does not claim

- that every cache miss has one root cause;
- that provider-internal cache topology or hardware routing is directly visible;
- that Glass guarantees an eternal or lossless session;
- that every archive is exact or transactionally committed;
- that every provider route has identical context, persistence, security, or recovery behavior;
- that static client gates were active for every account;
- that security sidecars or behavioral interventions guarantee protection;
- that Anthropic adopted Glass research without direct causal evidence.

## Repository map

- `cmd/glass-proxy/` — process assembly, listeners, provider services, and debug/control endpoints.
- `internal/glass/` — Anthropic session state, compression, eviction, pinned frames, archival recovery, and shared-prefix coordination.
- `internal/proxy/` — route selection, Anthropic integration, OpenAI-compatible handling, streaming, and OpenRouter forwarding.
- `internal/claude/`, `internal/codex/`, `internal/gemini/` — provider-owned services and protocol paths.
- `internal/runtimepaths/` — runtime-root resolution.
- `internal/guard/` — embedded guard and scanner integration.
- `bin/`, `start.sh`, `verify.sh` — retained production scripts.

## Deployment and publication boundary

Glass is a single-user proxy that processes credentials and conversation content and exposes diagnostic/control surfaces. Read [SECURITY.md](SECURITY.md) before deployment.

The production source, tests, scripts, and existing assets are preserved. Used configuration, databases, credentials, sessions, logs, captures, and private transcripts are excluded. This documentation publication uses static content/link/sensitive-boundary checks; it does not build, test, execute, restart, or probe Glass.

## Rights

All rights reserved. No software license is granted by this publication.
