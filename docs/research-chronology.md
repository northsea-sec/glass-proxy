# Research chronology: the architectural story

This chapter explains the major architectural turns. The [complete chronology](complete-chronology.md) records every documented incident, experiment, correction, and provider transition; the [filed bug reports](filed-bug-reports.md), [problem register](problem-register.md), and [solution register](solution-register.md) prevent this narrative from selecting only successful milestones.

## Origin before the formal Session Glass plan

The operator records an early-2025 origin through Claude Thinking Audit, Full Spectrum Analyzer, and predecessor proxy/instrumentation work. The surviving local record is densest from January 2026 onward. The March 4, 2026 Session Glass document is the clearest formal redesign, not the beginning of every idea in the program.

The early work asked how a coding agent could retain useful working state under finite context while also observing thinking, timing, model behavior, cache usage, tool activity, and quota. That broad instrumentation effort supplied the measurements and failure reports that later made a context/cache proxy possible.

## Mutable middleware

The predecessor middleware transformed every request. Stage 1 compressed old content, P4/DCP superseded stale tool results, Stage 2 dropped message pairs, persistent state tried to stop client resend from undoing those drops, and watermarks grouped transformations.

Each mechanism addressed a real failure. Together they created another problem: the proxy had to reconcile what the client resent, what it had previously changed, what it had dropped, and which provider-visible prefix had already been observed.

February incidents established several independent failures:

- syntax-invalid hot reload removed a working trimmer;
- Stage 2 fired repeatedly against resent history;
- bridge data described the previous response rather than current request growth;
- non-atomic writes damaged sidecar state;
- throughput and large cached contexts drove cost even with good hit percentages;
- retryable duplicate handling could amplify retries.

The verified Stage 2 hysteresis result remains part of the history. It did not make the entire mutable stack simple.

## Session Glass: change ownership, not another threshold

Session Glass moved canonical conversation ownership into the proxy. Instead of asking how to mutate each client-resubmitted body, Glass would ingest new positions once, preserve coherent messages, evict selected units, externalize displaced history, and build a stable provider-facing view.

The architectural shift produced four durable ideas:

1. session-local state has one owner;
2. system, tools, and messages are separate cache-sensitive planes;
3. mutation happens at deliberate boundaries;
4. displaced context needs a recoverable source, not only deletion.

The design then had to survive real overflow. March 6–7 records showed that a frozen reference alone did not freeze a retained bridge; stale rollback mixed architectures; invalid role/tool structure triggered fallback and reset; fresh lanes entered staircase eviction; and some severe misses occurred despite locally stable measured prefixes.

The resulting direction was a one-way post-overflow pinned frame: bounded anchors, a stable recovery entrance, and a bounded hot tail.

## Memory changed from facts to chapters

Operational facts attempted to restore the state an agent no longer knew to request. They were compact and useful, but lost sequence, corrections, qualifications, and the relationship between an instruction and its reversal. Updating facts near the front also created punctuated prefix transitions.

The operator’s chapter design made ordered history authoritative:

> Facts are bookmarks; chapters are memory.

Implementation experiments then exposed the other extreme. Recursively “verbatim” chapters copied giant tool outputs and became hundreds of kilobytes. Human-readable chapters therefore evolved toward ordered prose, roles, ranges, and concise tool descriptions while omitting raw result bodies. The research contribution includes this unresolved fidelity boundary: exact evidence and usable recovery projection are different layers.

Recovery summaries and gates were added as navigation. A gate initially observed the wrong half of the Read protocol and looped dozens of times. Later logic recognized both tool invocation and result. That proves a read event, not comprehension.

## Compression is a sequence problem

Compression reduces prompt mass but changes content. Idempotence keeps already-compressed bytes stable; it does not erase the first original-to-compressed transition.

The research moved through several explanations:

- anchor movement was blamed alone;
- production correlation implicated compression events independently;
- fixed-anchor testing showed compression alone changed the local prefix;
- threshold 80 reduced anchor movement but also stopped useful prefix growth;
- eager pre-build compression passed a golden test and later regressed live;
- batch advancement returned as the surviving cadence;
- compression-off replay removed compression transitions but more than doubled modeled prompt mass.

The final lesson is not “compression good” or “compression bad.” The design controls when a transformation occurs, how much stable coverage grows, and how much uncompressed tail is carried between transitions.

## Identity and subagents

System-prompt hashing could not distinguish multiple client processes. PID qualification separated sessions, but later replay showed that PID and established-parent information had to survive every path.

Subagent handling evolved repeatedly:

- model-based detection missed same-model children;
- small-system and agent-tool requests became distinct classes;
- isolation protected parent state but shared-material suffixes could merge divergent children;
- upstream cache suppression prevented short-lived entries but charged repeated full input;
- parent-affinity and serializer gates introduced starvation and switching tradeoffs;
- repeat-cache promotion failed its first payoff and was retired;
- ingress identity propagation and fallback self-gates became the durable correction.

No single “subagent caching is always on/off” rule survived every workload.

## Interleaving and serializer correction

A provider LRU/slot explanation initially made session interleaving the central cause. Serializer batching reduced switches, but an aggregate analysis recorded no break-rate improvement and higher cost per switch. Some configurations delayed other sessions tens or hundreds of seconds.

Later replay of identical requests in interleaved and sequential order produced identical local prefix transitions. This rejected ordering as the cause of local transformation differences in that corpus. It did not expose every hidden provider residency policy. The public record therefore preserves both the weakened hypothesis and the bounded replay result.

## Tengu: the client is part of the experiment

Static reverse engineering of Claude Code v2.1.74 identified dozens of feature gates affecting attribution metadata, deferred tools, effort, compaction, session memory, tool-result formatting, strict schemas, streamed execution, and global system-prompt cache behavior.

The important shift was methodological: not every request change originates in Glass. Client version, feature assignment, connected tools, internal model calls, retry policy, and compaction paths are experimental variables.

The Tengu work remains incomplete for opaque gates and actual account assignment. Its full contribution is recorded in [Tengu and coding-client research](tengu-and-client-research.md).

## Replay and incident-period diff discipline

Under quota pressure, agents repeatedly changed code, restarted processes, and explained sparse samples. The recovered method reversed that order:

1. identify the exact active version from snippet backups and binary/runtime evidence;
2. use captured fixtures;
3. vary one factor at a time;
4. compare interleaved and sequential order;
5. preserve local model output as local evidence;
6. use live evidence only for provider effects that local replay cannot decide;
7. retain counterexamples.

This method corrected threshold chronology, exposed replay non-equivalence, separated PID-zero metadata from recomputed ingress, and stopped a plausible repeat-cache optimization after its first failed payoff.

## Provider-native lanes

OpenAI-compatible traffic first established that non-Anthropic formats needed their own state and streaming behavior. Codex then demonstrated the cost of designing from assumptions: live capture found WebSocket `response.create`, continuation IDs, native compact requests, opaque compaction items, quota events, and local child state. Gemini added another distinct content/parts and explicit-cache model.

The resulting architecture shares operator-facing control only where the meaning is actually shared. Wire formats, continuation, context mutation, cache semantics, persistence, and telemetry remain lane-owned.

## What survived

The published architecture retains proxy-owned session state, controlled mutation boundaries, pinned frames, external recovery material, split identity, PID-aware ingress metadata, provider-specific handlers, Codex WebSocket telemetry, runtime-root controls, and explicit configuration modes.

The public record also retains loss and uncertainty: archives are not lossless network transcripts, archive writes are not transactionally required before eviction, summaries are derived, deliberate prefix transitions remain, provider internals are hidden, some parity/security surfaces are incomplete, and semantic long-session quality lacks one universal benchmark.

That combination—the architecture and the record of how plausible explanations failed—is the research project.
