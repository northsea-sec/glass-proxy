# Origin, contribution, external context, and influence question

This document separates five questions that earlier reports repeatedly collapsed:

1. When did the operator originate the work?
2. When and where was it publicly disclosed?
3. Which outside ideas already existed independently?
4. Could Anthropic have encountered the work?
5. Is there direct evidence that Anthropic adopted it?

Similarity is not causation. Lack of direct causal evidence is not a denial of the operator’s chronology.

## Operator-recorded origin

The operator records in the September portfolio audit that Glass and its predecessor research began in **early 2025**. The work included Claude Thinking Audit, Full Spectrum Analyzer, request/timing analysis, context-control middleware, and the problems later formalized as Glass.

The March 4, 2026 Session Glass implementation plan is therefore treated as a major redesign and surviving formal specification—not the invention date of the wider research program.

## Surviving documented engineering chronology

The dense surviving local corpus currently anchors:

- January 2026 request, context, thinking, cache, and evidence records;
- February 2026 mutable middleware, anti-compaction, Stage 1/P4/Stage 2, watermark, quota, reload, and session-store work;
- March 2026 Session Glass, cache incidents, memory, chapters, replay, identity, Tengu, and provider separation;
- April 2026 Codex capture, runtime-root isolation, Gemini, and lane separation.

This is the surviving-document chronology. It does not replace the earlier operator-recorded origin.

## Claude Thinking Audit publication

The audit records that an earlier Claude Thinking Audit was publicly published and that a later public repository existed under `argosdevo-svg/claude-thinking-audit`. A January 2026 public issue/reference proves later public exposure, not necessarily the first 2025 disclosure.

The complete origin record must preserve these as different dates:

- first private invention;
- first working implementation;
- first public description;
- first public repository;
- later owner/name/repository transitions;
- later issue or provider-facing disclosure.

A current repository timestamp or a current 404 cannot be used to deny an earlier publication.

## Glass-specific contribution

The defensible contribution is not that Glass invented every underlying idea of external memory, compaction, caching, or observability. It is the longitudinal engineering combination and the evidence trail produced while operating coding-agent sessions:

- proxy-owned canonical conversation state;
- explicit separation of system, tools, and message cache-sensitive planes;
- deliberate mutation boundaries rather than unconstrained per-request rewriting;
- watermark/anchor experiments tied to prompt-cache observations;
- whole-message eviction, bounded pinned frames, and external recovery material;
- separation of exact/source history, human chapters, summaries, indexes, and bookmarks;
- SessionKey/RequestKey/AffinityKey/PrefixKey ownership distinctions;
- PID/parent-aware subagent identity and fallback analysis;
- replay-and-diff falsification using captured traffic and incident-period backups;
- preservation of failed explanations and rollback results;
- provider-native lane separation validated by actual Codex capture;
- client-binary/Tengu research showing that request instability can originate before the proxy;
- mechanical evidence and status discipline learned from repeated agent overclaim.

## Independent prior and contemporary work

The research audit identified relevant external work predating or overlapping parts of Glass:

- lost-in-the-middle and long-context retrieval degradation;
- MemGPT and tiered/external memory;
- RULER and NoLiMa long-context evaluation;
- observation masking versus model-generated summarization;
- provider prompt caching and context editing;
- long-horizon coding-agent context management;
- structured logs, retrieval, and programmatic search;
- memory poisoning and agent-state integrity guidance.

These works affect broad priority claims. They do not answer who independently assembled the particular Glass mechanisms, when the operator first implemented them, or whether a provider saw the work.

## The exact Anthropic-feature question

The operator asked which Anthropic “cache tool solution” appeared after the earlier work. That phrase must not be silently resolved to whichever later feature sounds similar. Candidate surfaces include:

- prompt caching and cache-control behavior;
- organization/global system-prompt caching;
- Claude Code attribution and deferred-tool behavior;
- context editing;
- memory/session-memory controls;
- cache-aware compaction;
- tool search/deferred schema loading.

The exact suspected feature must be identified from the original local discussion/publication before a chronological comparison is stated.

## Possible exposure

Possible exposure may be supported by:

- public repository publication;
- public articles, papers, posts, or media material;
- public issue reports;
- provider support interactions;
- heavy use of Claude during development;
- provider telemetry or data-retention terms in effect at the time.

These establish possible opportunity only at their respective scopes. Heavy use does not by itself prove training or product adoption.

## Data-use terms

A responsible account must identify the historical terms and settings applicable to the operator’s account and time period, separating:

- service operation and abuse/security logging;
- product analytics and telemetry;
- human review;
- model improvement/training permission;
- explicit feedback or bug-report submissions;
- retention periods and opt-out settings.

Current general policy cannot silently substitute for historical account-specific terms.

## Direct influence

Direct causal influence would require evidence such as:

- an acknowledgment or communication;
- an attributable citation;
- provider personnel engagement with the published artifact;
- matching non-public details with a documented access path;
- another direct linkage stronger than chronology and similarity.

The current public record does not claim such a direct link. It publishes the operator’s origin testimony, the documentary chronology, public-exposure evidence, and technical comparison without turning possibility into fact.

## How to state originality

Recommended public formulation:

> Glass is an independently developed, longitudinal security and systems-engineering program for preserving useful coding-agent work under context and prompt-cache constraints. It combined proxy-owned session state, controlled mutation boundaries, archival recovery, split identity, client reverse engineering, replay-led falsification, and provider-native lanes. Several underlying ideas have independent prior art; the project’s contribution is the integrated architecture and the unusually complete record of operational failures, corrections, and negative results.

This statement is strong without requiring an unsupported assertion that Glass caused a provider feature.
