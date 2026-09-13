# Tengu and coding-client research

Tengu is the project’s static reverse-engineering and client-behavior research branch. It matters because the proxy cannot stabilize or explain a request without accounting for what the coding client changes before the request reaches Glass.

## Inspected client versions

The principal report compares Claude Code v2.1.58 and v2.1.74. For v2.1.74 it records:

- 74 `XA()` feature gates;
- 9 `CM()` boolean gates;
- 5 `wC()` dynamic configurations;
- approximately 400 telemetry call sites;
- 32 new flags, 16 removed flags, and 42 carried forward relative to v2.1.58.

These are static findings for the inspected binary. Literal defaults, environment overrides, server assignments, runtime-computed defaults, and actual account assignment are separate states.

## Default-on findings recorded in the audit

The report examines default-on gates affecting:

- attribution/billing request metadata;
- deferred tool loading;
- subagent instruction text;
- default effort selection;
- fast-mode availability and error handling;
- legacy model remapping;
- prompt suggestions;
- agent-team availability.

Opaque codenames are not treated as semantics. A purpose is published only where the report records code context; unknown codenames remain unknown.

## Attribution header

The audited client constructs an attribution/billing header containing client version, entrypoint, workload, and a per-conversation value derived from the first user message.

The research treated this changing request metadata as a cache-fragmentation candidate and excluded recognized billing material from Glass’s shared `PrefixKey` derivation. The public record does not assert that an HTTP header is part of the provider’s documented prompt-prefix key unless provider evidence says so. It records the client difference, the project hypothesis, and the local identity response separately.

## Deferred tools

A default-on deferred-tools gate keeps many full tool schemas out of the initial prompt and exposes names until ToolSearch loads a schema.

Consequences investigated by Glass:

- lower initial prompt size;
- a dynamic available/deferred tool list;
- client-controlled system/tool changes when integrations connect or disconnect;
- internal ToolSearch/model behavior affecting WebSearch and WebFetch;
- a prefix change that may originate before proxy processing.

## Global system-prompt cache

The inspected build contained an organization-scoped global system-prompt-cache path behind a default-off gate or environment override. The report describes a static/dynamic boundary marker and a cache scope.

This was relevant to Glass’s `SessionKey`/`PrefixKey` split: conversation history remains local even when static system/tool material can be shared. Presence in the binary did not prove the gate was active for the operator’s account.

## Client compaction

The Tengu/client research identifies several distinct compaction controls:

- cache-aware compact prefix;
- streaming compact retry;
- session-memory compaction;
- model/context estimates and prior usage;
- environment controls;
- provider context-editing betas;
- native Codex compact requests.

These are not one mechanism. A live incident must identify which client/provider/proxy path acted rather than infer from the existence of a flag.

## Tool-result formatting and strict schemas

The audit records gates that can:

- shorten edit/write tool-result text;
- move deferred-tool notices between tag/surface forms;
- enable strict tool schema validation;
- stream tool input or execute tools while input is still streaming;
- alter merging of text and `tool_result` blocks.

These client decisions affect message bytes, token mass, tool structure, and the proxy’s classifier assumptions.

## Effort and output behavior

The inspected client can select or downgrade effort for supported models and contains output-style/briefness experiments. These settings can change reasoning/output volume without changing Glass code. They therefore belong in any workload or quota comparison.

## Session memory and auto-background behavior

The report identifies session-memory, session-memory compaction, auto-background-agent, remote-backend, and agent-team gates. Static presence does not establish that a feature was enabled. It does establish that apparent “proxy behavior” may originate in the client or its experiments.

## Retry and dedup

Historical client analysis found that retryable responses can amplify duplicate handling. Returning a retryable status for a duplicate may create another duplicate. Request coalescing or replay of the original result was conceptually safer, while streaming made response replay difficult.

Glass’s dedup behavior must therefore be documented together with client retry semantics rather than as a simple request counter.

## Interrupt handling

A v2.1.71 report records a concrete priority/yield defect:

- user interrupts entered a default next-turn queue;
- the abort path checked a different priority;
- auto-approved tools could continue without a yield;
- interrupted-response material could pollute the next turn.

This was a client regression, not a Glass root cause. Glass later added its own stale-continuation interrupt breaker, creating two separate layers that must not be conflated.

## WebSearch and WebFetch

Primary transcripts record external fetches succeeding while client-internal processing returned no or partial results because an Opus-only proxy policy blocked an internal Haiku step.

The correct evidence split is:

1. external HTTP availability;
2. tool invocation;
3. internal client model selection;
4. proxy model/subagent policy;
5. tool result returned to the main model.

“No search results” did not prove the external source was unavailable.

## Context editing

Research into Anthropic context editing records `clear_tool_uses` and `clear_thinking` strategies, keep/trigger/clear-at-least controls, and cache invalidation when content is cleared.

Server-side editing changes who performs the deletion; it does not remove the context-versus-cache tradeoff. It is distinct from local compression and client auto-compaction.

## Codex capture as client research

The same capture-first method later corrected Codex assumptions:

- WebSocket `response.create` rather than REST-only traffic;
- inline instructions;
- native `previous_response_id` on continuation;
- incremental function outputs;
- native rate-limit events;
- `/responses/compact` and `response.compaction`;
- local child/delegation state with ordinary child response traffic.

This reinforced the Tengu lesson: inspect the actual client boundary before making a proxy or provider claim.

## Backend and timing labels

The predecessor fingerprint research inferred TPU/GPU/Trainium-style classes from inter-token timing, latency, speculative decoding, and cache patterns because response headers did not directly identify hardware.

Those labels are classifiers with confidence, not provider hardware attestation. They can support correlation research but cannot independently prove backend routing or cache causality.

## Unfinished work

The Tengu transcript ended after the operator asked for exact meanings of newly active flags. Many code-context findings were produced, but opaque flags and actual account assignments remained incomplete.

The public contribution is therefore:

- a versioned client gate inventory;
- concrete code-context findings for cache, tool, effort, compaction, memory, and execution paths;
- identification of client-origin request instability;
- a requirement to include client version and flag state in proxy experiments;
- explicit preservation of unknown or unobserved gate behavior.
