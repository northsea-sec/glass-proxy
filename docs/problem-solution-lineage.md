# Problem-to-solution lineage

This map connects every cross-source problem family to the documented attempts in the [solution register](solution-register.md). It is an index, not a replacement for the filed reports, complete chronology, or source catalogue.

| Problem family | Main documented solution path | Outcome boundary |
|---|---|---|
| INC-001 Long-context retrieval and reasoning degradation | SOL-016–SOL-032, SOL-053 | Storage, bounded context, and recovery mechanisms were built; semantic quality was not reducible to context length alone. |
| INC-002 March 24 quota exhaustion acceleration | SOL-033–SOL-051, SOL-065 | Several cache, identity, telemetry, and config corrections followed; provider quota weighting remained a separate question. |
| INC-003 Deferred compression changes observed prefixes | SOL-007, SOL-035–SOL-039 | Batch cadence survived; eager compression passed local tests then regressed live and was reverted. |
| INC-004 Restart anchor persistence asymmetry | SOL-033–SOL-036, SOL-064 | Reported persistence diagnosis led to anchor/state scrutiny; later chronology added threshold and watermark causes. |
| INC-005 Non-contiguous views create same-role adjacency | SOL-024–SOL-028 | Blind repair failed; tool-aware repair, validation, and pinned frames followed. |
| INC-006 Eviction severs tool pairs | SOL-017, SOL-024–SOL-028 | Pair-aware eviction/repair and structural validation became explicit invariants. |
| INC-007 Validator fallback restores original history | SOL-026–SOL-028 | Size/state guards constrained fallback; the fallback remained a documented risk. |
| INC-008 Recovery gate loops | SOL-030–SOL-031 | Gate-clear logic expanded to actual Read tool-use and result representations. |
| INC-009 Archives recursively huge or lossy | SOL-019–SOL-022, SOL-029–SOL-032 | One giant transcript evolved into batch chapters, omission markers, indexes, bookmarks, and separate summaries. |
| INC-010 Token accounting and thresholds mislead | SOL-012–SOL-013, SOL-033–SOL-039, SOL-053 | Real-input feedback and lane-specific budgets replaced one universal estimate/threshold. |
| INC-011 Summarizer blocks or sends invalid requests | SOL-030–SOL-032, SOL-065 | Background chunks and model/config fixes were added; eviction-time delta work remained a distinct latency boundary. |
| INC-012 Serializer starvation | SOL-040–SOL-041, SOL-050–SOL-051 | Subagent accounting reduced priority inversion; cache/latency tradeoffs prevented a simple universal scheduler verdict. |
| INC-013 Subagent prefixes interfere with parent reuse | SOL-042–SOL-051 | Identity, isolation, cache suppression, gated experiments, and warming were tried; no single policy covered every request class. |
| INC-014 Session identity collapses without PID/parent context | SOL-042–SOL-045, SOL-051 | Ingress-derived keys and fallback self-gates were the surviving correction. |
| INC-015 Repeated small-system traffic is costly | SOL-046–SOL-049 | No-cache policy protected parent entries; repeat-cache promotion failed its first payoff and was retired. |
| INC-016 Interrupt breaker blocks tool continuation | SOL-066 | Tool-result freshness was added while stale-loop blocking remained. |
| INC-017 Bookmark/context injection mutates structure | SOL-021–SOL-029 | Dynamic overlays were replaced by stable bookmarks, final repair, and bounded frames. |
| INC-018 Source, binary, and process diverge | SOL-064 | Source-newer rebuild checks and route/mtime/startup-signature evidence became deployment requirements. |
| INC-019 Context-mode and thinking-budget contracts drift | SOL-039, SOL-062–SOL-065 | Explicit modes and alias/max-token normalization corrected the named integration failures. |
| INC-020 Codex WebSocket falls through | SOL-055–SOL-059 | Capture-first native WebSocket routing replaced the REST-only assumption. |
| INC-021 Codex output leaks Claude schema | SOL-058, SOL-061 | Root isolation succeeded before control-schema isolation; lane-neutral adapters were the later direction. |
| INC-022 OpenAI/OMP history reingest | SOL-052–SOL-053 | Request-authoritative bounded reconstruction stopped the immediate byte-growth spiral. |
| INC-023 OpenAI stream stalls before first event | SOL-054 | Empty OpenAI events replaced ignored comments; historical real-client closure remained incomplete. |
| INC-024 Removing OMP harness cripples behavior | SOL-052–SOL-054 | The failed removal established that transport success must preserve cwd/tool contracts. |
| INC-025 UI reads obsolete schema/state | SOL-062–SOL-065 | Runtime-path and schema-driven remapping followed; public docs retain its mixed-generation limits. |
| INC-026 GLASSDD lacks enabled end-to-end parity | SOL-067 | Two-layer design and scanner wrappers exist; dependency and deployment states remain explicit. |
| INC-027 Prompt proof confused with model behavior | SOL-009–SOL-010, SOL-068–SOL-069 | Outbound capture—not model self-report—became the transport proof. |
| INC-028 Internal services replay wrong auth scheme | SOL-048, SOL-065 | Auth-kind metadata was carried with captured credentials in the recorded fix. |
| INC-029 Telemetry misattributes sessions and health | SOL-012–SOL-014, SOL-042–SOL-043, SOL-051, SOL-061–SOL-065 | Formula, identity, time, lane, and deployment provenance became mandatory. |
| INC-030 Change/measure loops contaminate causality | SOL-059, SOL-064–SOL-065, SOL-070 | Capture-first, replay-first, one-variable analysis, deployment proof, and mechanical oversight became the method correction. |

## Reading order

For any row: read the named filed report, then its exact source record in the source catalogue, then each solution entry, then the dated events in the complete chronology. This keeps a concise map from replacing the actual history.
