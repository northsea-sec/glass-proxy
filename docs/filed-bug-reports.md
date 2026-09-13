# Filed bug reports and incident records

This document catalogues the actual filed reports and incident records used in the Glass history. It does not replace them with invented issue names. When a report states that a result was verified, this catalogue records it as verified for the version, evidence, and scope described by that report. Later corrections or regressions are shown as later events rather than used to erase the original verification.

## Long-context and reasoning reports

| Source report | Reported subject | Recorded status and relationship |
|---|---|---|
| `BUG_REPORT.md` | Long-context reasoning degradation across four sessions | Filed problem/evidence report; overlaps the dated and one-million-context versions below. |
| `1M_CONTEXT_BUG_REPORT.md` | Non-uniform useful quality inside the advertised one-million-token window | Filed product-capability report with transcript, benchmark, and external-research support. |
| `docs/2026-3-16/BUG-REPORT-1M-CONTEXT-DEGRADATION.md` | Fabrication, correction resistance, retrieval failure, and process degradation | Dated filed variant; records database-backed corrections alongside session failures. |
| `ANTHROPIC_MODEL_DEGRADATION_REPORT.md` | Model degradation, confident fabrication, and product-policy concerns | Advocacy/report variant; project observations and causal/provider claims remain distinguishable. |
| `1M-LIE.md` | Discussion of lost-in-the-middle and confident substitution | Supporting transcript, not an independent filed technical incident. |

## Rate-limit and quota reports

| Source report | Reported subject | Verification and relationship |
|---|---|---|
| `RATE_LIMIT_BUG.md` | Five-hour rate-limit exhaustion under comparable workload | Filed issue-form report. |
| `GITHUB_BUG_REPORT_5h_RATE_LIMIT.md` | Three March 24 exhaustions and rapid re-exhaustion | Filed public-form report with 429 excerpts and request identifiers in the private original. |
| `RATE_LIMIT_EXHAUSTION_REPORT.md` | Three exhaustion events versus preceding days | Incident report; one timing row conflicts with the 51-minute calculation in sibling reports. |
| `RATE_LIMIT_EXHAUSTION_BUG_REPORT.md` | Breakpoint-anchor persistence and compression correlation | Records its diagnosis as verified and its proposed persistence remedy as not yet implemented at that point. Later research broadened the causal account. |
| `5h_RATE_LIMIT_EVIDENCE.md` | Workload, exhaustion, 429, cache-create, and cache-read comparison | Supporting evidence extract shared by the filed reports; not a fifth independent incident. |

## Glass cache, eviction, and recovery reports

| Source report | Reported subject | Recorded status and relationship |
|---|---|---|
| `AUDIT.md` | Thirteen cache, eviction, validation, archive, and recovery issues | Multi-issue audit with resolved, partial, and unresolved labels; every issue is expanded in the problem register. |
| `REPORT-2026-03-25.md` | Compression, anchor, watermark, cache modes, and outstanding comparisons | Mixed investigation/implementation report. It records verified findings and also identifies live comparisons still outstanding. |
| `docs/2026-03-07-glass-incident-synthesis.md` | March 6–7 chained failure across persisted and fresh lanes | Reconstruction report: stale rollback, replay invalidity, staircase eviction, structure errors, and pinned-frame direction. |
| `docs/2026-3-14/BURN_RATE_ROOT_CAUSE_AND_PRIME_GOLDEN_TESTS.md` | Cache-read collapse, subagent interaction, and proposed golden tests | Incident analysis plus verification-method prescription. |
| `docs/2026-3-14/PRIME_TEST_METHOD.md` | March 14 failure inventory and hypothesis-test method | Related but not byte-identical to the preceding report in the Glass corpus; retains its own status contradictions. |
| `docs/2026-3-16/session-timelines.md` | March 15–16 recovery gate, summarizer, serializer, compression, and context failures | Derived timeline from twelve sessions; detailed chronology source, not an extra independent execution. |
| `analysis/2026-03-23-cache-break-root-cause-report.md` | March 23 cache/burn correlation with code changes and traffic classes | Report contains an initial cause weighting and a corrected cause section; both are preserved chronologically. |
| `analysis/2026-03-27-opus-current-state-verification.md` | March 27 source/runtime/telemetry state | Dated verification report that explicitly rejects a broader healthy-system conclusion. |
| `analysis/2026-03-27-pid-fallback-replay-audit.md` | PID loss, parent-memory loss, reclassification, and lane collapse | Replay-backed audit; distinguishes captured metadata from recomputed ingress. |
| `analysis/2026-03-27-pid-fallback-hardening.md` | Identity propagation, fallback self-gating, counters, and stale-binary deployment | Implementation/verification report with residual cache-health risk explicitly retained. |
| `analysis/2026-03-27-small-system-repeat-cache-experiment.md` | Repeat-cache promotion for isolated small-system traffic | Experiment report: initial benefit was unproven, first live payoff failed, diagnosis evolved, and the experiment was later retired. |

## Provider and control-plane incident records

| Source record | Reported subject | Recorded result |
|---|---|---|
| `docs/2026-4-19/CODEX_PHASE0_CAPTURE_DISCOVERY.md` | Missing Codex wire truth and incomplete capture | Records the capture matrix and later additions while retaining open delegation/compaction breadth. |
| `docs/2026-4-19/CODEX_PHASE0_FINDINGS_2026-04-19.md` | Native WebSocket, continuation, compact, usage, tool, and delegation behavior | Direct recorded Phase 0 findings; corrects older REST/stateless assumptions. |
| `docs/2026-4-19/CODEX_RUNTIME_ROOT_ISOLATION_LIVE_VERIFICATION_2026-04-19.md` | Codex state isolation from Claude state | Records successful before/after root verification and separately preserves missing usage/quota sidecars. |
| `text-GL-CX.md` | Claude-specific status/debug schema leaking into Codex output | Supporting incident transcript; the attempted repair was rejected and interrupted. |
| `text-GLASS-CODEX-FIRTS-SERIOUS-FUCKUP.md` | OpenAI-compatible harness prompt removal breaking cwd/tool behavior | Supporting incident transcript showing transport success without consumer parity. |
| `text-GLASS-OMP.md`, `text-GLASS-OMP-1.md`, `text-GLASS-PI-1.md` | OpenAI-compatible overflow, reingest, and bounded-view work | One incident chain; later requests stopped the immediate size spiral but did not prove endless sessions. |
| `text-PI-BIN-OPENAI-ERRor.md` | OpenAI-compatible stream stalls before the first event | Attempted heartbeat fixes and synthetic first-byte result; real client failure remained at the end. |
| `text-CONFIG-1.md` through `text-CONFIG-5.md`, plus `text-CONFIG-5-1.md` | Schema, UI, passthrough, compatibility, and broad-blocking damage | Configuration/control-plane incident chain preserving operator corrections and later reversals. |
| `text-GLASSDD-1.md`, `text-GLASSDD-2.md` | GLASSDD implementation coverage and dependency gaps | Survey followed by implementation attempt; timeouts/default-off/end-to-end gaps remain part of its recorded result. |

## Predecessor Route Inspector incident reports

The historical predecessor corpus contains additional actual incident/report files. Their source bodies remain private, but their report identities and roles are part of the Glass lineage:

- `incident-2026-02-15-triple-session-death.md` — chained three-session failure involving reload, threshold, watermark, state, and logging defects.
- `context-limit-root-cause-2026-02-15.md` — syntax-invalid hot reload, trimmer loss, usage-spoof gap, and stale-bridge follow-up.
- `stage2-hysteresis-fix-2026-02-15.md` — repeated Stage 2 over-fire and the recorded before/after cache result.
- `api-cost-spike-2026-02-14.md` — concurrency, throughput, cached-context volume, and cost spike.
- `cc_interrupt_bug_report.md` — client interrupt priority/yield regression.
- `004-bridge-staleness-session-death.md` — prior-response bridge staleness and growth-adjusted correction.
- `full-system-report-2026-02-15.md` — broad system state, issues, and claimed fixes.
- `cache-optimization-report.md` — cache/trimmer findings and dated optimization results.
- `quota-investigation-summary.md` — consolidated quota/cache hypotheses, corrections, and debunked explanations.
- `deep-evidence-unearth-report.md` and `researcher-report.md` — evidence maps and independent-verification reports.
- `quota-bug-report-draft.md` — draft filed quota report.
- `evidence_report_20260122_122409.md`, `evidence_report_20260122_123613.md`, and `evidence_report_20260122_123619.md` — related generated evidence exports, not three separate root causes.

## Supporting transcripts

The private corpus contains 109 transcript-class candidates. They are used when they preserve an operator correction, a reproduced failure, an implementation attempt, or the order in which a claim was superseded. They are all indexed in [the source catalogue](source-catalogue.md); the transcript bodies are not republished.

## Reading the reports correctly

- A report-local `verified` result is published as verified for that report's recorded version and procedure.
- A later regression does not erase the earlier verified result; it becomes the next event in the chronology.
- Shared tables and copied issue variants are related evidence, not multiplied corroboration.
- A report can contain several issues, several fixes, and conflicting status sections. The problem and solution registers expand those parts rather than reducing the report to one verdict.
