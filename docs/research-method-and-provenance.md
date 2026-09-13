# Research method and provenance

The public research documents synthesize design notes, incident analyses, experiment reports, and the current source. They preserve the development of the ideas without distributing private working sessions or production state.

## Evidence classes

- **Operator testimony** establishes the operator's experienced chronology, usefulness, failure, cost burden, and stated invention/publication history. It is not silently converted into a benchmark or provider-causation claim.

- **Provider documentation** establishes an external API contract, subject to provider revisions.
- **Current source** establishes what this snapshot implements, including branch-specific behavior and configuration conditions.
- **Historical observations** describe what a dated analysis recorded. They are not new experiments performed for this publication.
- **Hypotheses and proposals** explain the investigation's reasoning but do not prove an implementation or a provider-internal mechanism.
- **Later corrections** narrow or reject prior explanations. They must be read alongside the original claim.

A single document can contain several classes. For example, the serializer analysis includes measured comparisons and speculative cache-slot explanations. The Codex findings document accumulated later observations that superseded some earlier negative findings within the same file. The cache/compression report marks some backend explanations as constraints while elsewhere qualifying their causal support. The public synthesis follows the evidence, not the confidence of a heading.

## Historical source catalogue

These identifiers are used throughout the public chapters. The original documents remain in the private project corpus; their titles are supplied for provenance, not as links to files included in this repository.

| ID | Source record | Contribution |
|---|---|---|
| R1 | `CACHE_COMPRESSION_RESEARCH_REPORT.md`, March 25, 2026 | Middleware history, compression/cache tension, breakpoint-threshold regression, inter-request compression gap, unresolved explanations |
| R2 | `REPORT-FEB28-SESSION-ANALYSIS.md` and `TESTPLAN-FEB28-COMPREHENSIVE.md`, February 28, 2026 | Shared accounting failure, reconstruction's actual activation state, watermark and replay proposals |
| R3 | `2026-03-07-glass-incident-synthesis.md`, March 7, 2026 | Chained incident reconstruction, reference mutation, replay invalidity, post-overflow staircase, pinned-frame direction, same-prefix anomalies, live-experiment failures |
| R4 | `glass-operational-memory-plan.md`, March 9; `COUNTER_AMNESIA.md` and `fact-fix-impact-analysis.md`, March 11, 2026 | Fact injection, first-post-eviction timing, working-state reconstruction, cache resets when dynamic facts change |
| R5 | `GLASS_BOOK.md`, March 12, 2026 | Critique of fact-collector primacy and the proposal for chapter-based recovery |
| R6 | `serializer-analysis.md` and `session-timelines.md`, March 16, 2026 | Historical scheduling rationale and aggregate evidence limiting its claimed benefit |
| R7 | `2_LANE_OAI_ANTRO.md`, March 18, 2026 | Separation of OpenAI-compatible and Anthropic request paths |
| R8 | `CROSS_SESSION_PREFIX_ARCHITECTURE.md` and `SPLIT_ID_DIVERGENCE_MATRIX.md` | Session/prefix ownership split and shared-prefix architecture |
| R9 | `CODEX_PHASE0_FINDINGS_2026-04-19.md` and related capture-discovery notes | Native WebSockets, continuation, compact endpoint and response objects, client/runtime delegation observations |
| R10 | Codex runtime-root isolation plan and verification record, April 19; `LANE_SEPARATION_IMPLEMENTATION_PLAN_2026-04-20.md` | Provider storage/default-path separation and lane-owned service assembly |
| R11 | `2026-03-23-cache-break-root-cause-report.md`, March 23, 2026 | Subagent lane-collision via shared session suffix, classifier/serializer change interaction under concurrency, patch-window correlation |
| R12 | `2026-03-27-established-truths.md`, `2026-03-27-verified-timeline.md`, and `2026-03-27-claim-ledger.md`, March 27, 2026 | Cross-era synthesis of 53 verified findings, longitudinal evidence classification (artifact hierarchy), config/schema drift, March 6–7 lane-level corroboration, March 24–25 contradiction chain |
| R13 | `2026-03-27-recovered-prime-golden-methodology.md` and `docs/2026-3-14/PRIME_TEST_METHOD.md`, March 2026 | PRIME-GOLDEN test-first discipline, March 25 replay-and-diff tiered recovery, golden-test blind spot, method continuity between eras |
| R14 | `FULL_INVESTIGATION_RESULTS.txt`, `snippet_backup_diffs.txt`, `threshold_replay_comparison.txt`, `compression_off_replay.txt`, `interleaved_replay_results.txt`, `prefix_event_analysis.txt`, `cache_efficiency_audit.txt`, and related March 25–27 analysis artifacts | Compression frequency comparison (Glass vs. predecessor middleware), snippet-backup corrections to the threshold narrative, interleaved replay disproof, compression on/off replay tradeoff, burn spike census, formula history, watermark lineage |
| R15 | `TENGU-FLAGS-REPORT.md`, March 23, 2026 | Static client-binary audit (87 gates, 74 feature gates), per-conversation attribution-header hash, deferred-tools gate, default-off global cache, cache-impact rankings |
| R16 | `context_editing_api_research.txt`, `2026-03-23-cache-break-root-cause-report.md`, and captured fixture analysis, March 2026 | Context-editing beta behavior and cache interaction, captured fixture betas (no context-management beta in use), superseded LRU-interleaving explanation |
| R17 | `2026-03-27-pid-fallback-replay-audit.md`, `2026-03-27-pid-fallback-replay-current.json`, `2026-03-27-pid-fallback-replay-capture-20260325.json`, `2026-03-27-pid-fallback-replay-fixtures.json`, and `2026-03-27-pid-fallback-hardening.md`, March 27, 2026 | Three-corpus replay audit of PID-dependent fallback paths, parent-memory loss erasing agent_tool splits, false-positive cache metrics under PID loss, streaming/serializer hardening pass, fallback counters |
| R18 | `2026-03-27-small-system-repeat-cache-experiment.md`, March 27, 2026 | Guarded repeat-cache experiment and rollback, restart-spike TTL-expiry root cause (prefix bytes identical across restart), warming architecture limits, lane-warmer addition, stale-binary restart detection, burn-estimator telemetry fixes |


Additional research included the Session Glass implementation plan, March burn-rate/replay-method reports, Codex and Gemini lane studies, rate-limit analyses, and system-prompt capture planning. Their contents helped reconcile terminology and current source but are not offered as independent public benchmark datasets.

## Public primary references

- [Anthropic prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching): prefix composition, cache controls, lifetime, eligibility, and reported usage.
- [Anthropic context windows](https://platform.claude.com/docs/en/build-with-claude/context-windows): context accounting and model-specific constraints.
- [Anthropic context editing](https://platform.claude.com/docs/en/build-with-claude/context-editing): clearing strategies and their interaction with cached prefixes.

These sources describe provider behavior. They do not independently validate Glass's performance, reconstruct the private incidents, or establish the internal topology of a provider's KV cache.

## From incident to claim

The research method that emerged from the incidents is:

1. Identify the relevant request, state owner, and actual active code path.
2. Separate what the client sent, what Glass changed, and what reached the provider.
3. Compare cache-sensitive regions and message validity across the transition.
4. Attribute native response usage to the correct request/session.
5. Use replay to establish local transformation behavior and isolate a hypothesis.
6. Use controlled live observations, where authorized, to investigate effects that replay cannot determine.
7. Retain counterexamples and revise the explanation when they conflict with it.

The strongest local evidence is often the first divergent region or an invalid tool boundary, not a large aggregate counter. Conversely, a stable local prefix does not prove a particular cause for a remote miss.

Historical aggregate comparisons remain observational. Configuration changes, workload changes, and concurrent interventions can confound them. No private quota histories, cost figures, or cache-efficiency percentages are promoted here as generally reproducible performance claims.

## Current-source reconciliation

The current source and the design history answer different questions. Important examples include:

- Chapter-based recovery survives, but chapter rendering is not a lossless API transcript.
- Fact-overlay support remains, but current production engine calls supply no fact payload.
- Codex HTTP mutation, WebSocket relay, and native compact forwarding are different branches.
- Disk snapshots do not establish a supported resumable session.
- Provider cache modes do not turn every lane into the same implementation.
- The retained serializer does not establish the old cache-slot explanation.

These boundaries are documented in the architecture and provider chapters, where the relevant implementation files are linked.

## Publication method

The production source closure is copied unchanged: 176 source, test, manifest, script, and required asset files. New public material consists of the README, six research/architecture documents, security policy, and ignore rules.

Publication integrity is checked statically by comparing donor/staged bytes and modes, checking the allowlisted file inventory, and comparing the remote tree's blob identities and executable modes with local files. No Git or other VCS commands are used for this publication.

No Glass build, test suite, formatter, linter, binary, wrapper, guard, provider request, or runtime process is executed as part of this release procedure. Static identity checks verify the copy and publication, not runtime behavior.

The public tree excludes used configuration, credentials, databases, session archives, cache state, logs, live request captures, and private research/transcript bundles. One pre-existing behavior-bearing fixture under `internal/proxy/testdata/` is retained as part of the unchanged test source; it is not a fresh capture or an imported runtime directory. Existing source literals and fixture data have not been editorially rewritten.

## Rights

All rights reserved. No software license is granted by this publication. Third-party dependencies retain their respective licenses and notices.


## Complete publication registers

The R1–R18 table above is a compact cross-document bibliography. It is not the complete corpus. Exhaustive coverage is published separately:

- [Complete source catalogue](source-catalogue.md): all 190 documentary candidates, roles, EOF boundaries, contributions, and content-based exclusions.
- [Filed bug reports](filed-bug-reports.md): actual filed report identities and relationships.
- [Complete problem register](problem-register.md): thirty cross-source mechanisms plus all 118 bug/incident-bearing source records.
- [Complete solution register](solution-register.md): seventy design, implementation, verification, failure, rollback, dormancy, and survivor entries.
- [Experiments and results](experiments-and-results.md): forty-two material tests, queries, replays, captures, and recorded verification results.
- [Complete chronology](complete-chronology.md): origin, predecessor systems, Glass incidents, Tengu, recovery, provider lanes, and publication history.

## Quarantined and derived research artifacts

Several private ledgers and generated syntheses were useful as navigation and audit evidence. Their phase labels and conclusions were not allowed to certify their own sources. Public claims are tied back to actual design, report, data, transcript, source, capture, or operator evidence classes.

## Reading and citation boundary

- A source catalogue row proves that the file is represented; it does not create independent corroboration.
- A transcript is cited only for chronology, operator correction, actions, or historical output.
- A report-local verified status is preserved without a new rerun.
- A later correction or regression is linked rather than used to delete the earlier result.
- External product facts require dated primary sources when stated as current facts.
- Private source bodies remain unpublished even though every one is catalogued.
