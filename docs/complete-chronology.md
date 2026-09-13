# Complete Glass research chronology

This is the exhaustive public chronology of the documented Glass research program. It records designs, incidents, experiments, verified results, failed fixes, reversals, and provider-lane development. Private transcripts and runtime data are not reproduced; their source identities appear in the [complete source catalogue](source-catalogue.md).

A result described as verified was verified in the named historical record. A later regression is shown as a later event rather than used to erase that verification.

## Early 2025 — operator-recorded origin

- The operator records that the work that became Glass began in early 2025, before the surviving March 2026 Session Glass redesign document.
- The early research involved Claude Thinking Audit, Full Spectrum Analyzer, request/timing observation, middleware experiments, and the problem of retaining useful agent work across context pressure.
- The first Claude Thinking Audit publication is part of the origin record. The exact first-release artifact, owner/name transition, and relation to later copies must be presented from the original publication evidence when available; current-copy modification times do not date invention.
- This period is the project’s stated origin, while the detailed surviving local engineering records become much denser in January–April 2026.
- No public causal claim is made that Anthropic adopted the work. Chronological precedence, public exposure, data-use permission, technical similarity, and direct influence are treated as five separate propositions in [origin and contribution](origin-contribution-and-external-context.md).

## January 2026 — surviving instrumentation and context-control records

- Claude Thinking Audit and Route Inspector records preserve request/response timing, thinking, cache, model, and behavioral observation work.
- A January predecessor context trimmer used an estimated threshold, retained recent messages, stripped older thinking, and clipped tool/assistant output.
- Evidence exports and report variants began recording cache, timing, thinking, and quota observations.
- System-prompt modification research recorded that removal and same-or-shorter replacement succeeded on the tested path while length-increasing injection returned HTTP 400.
- Early cache investigations separated system, tools, and messages as different sources of prefix instability.
- Randomized system transforms, changing tool sets, and non-idempotent message transforms became distinct cache-break candidates rather than one generic “provider cache” failure.

## February 2026 — mutable middleware and anti-compaction

### February 14 — throughput and cost

- `api-cost-spike-2026-02-14.md` analyzed a spend spike under higher request throughput and large cached contexts.
- The report distinguished high cache efficiency from low total cost: repeated large cache reads can still consume substantial quota or money when call density rises.

### February 15 — triple-session failure chain

- `incident-2026-02-15-triple-session-death.md` recorded intertwined failures across three sessions.
- A syntax-invalid edit caused hot reload to remove a working trimmer before the replacement loaded.
- Stage 2 repeatedly fired against histories the client resent, shifting the request repeatedly instead of creating stable hysteresis.
- Aggressive dropping threatened message-role and tool-call/result structure.
- Shared bridge state lagged behind current request growth.
- Non-atomic writes could leave state files empty or invalid.
- High-throughput journald output did not preserve sufficient forensic detail.
- Recorded remedies included syntax-before-reload discipline, copy/edit/move operation, file logging, atomic state writes, and better session-scoped accounting.

### Stage 2 correction

- `stage2-hysteresis-fix-2026-02-15.md` recorded Stage 2 over-fire and a verified before/after cache improvement after disabling or widening its trigger.
- That result solved the repeated-drop failure for its recorded configuration; it did not eliminate the broader mutation/reconstruction problem.

### Stale bridge correction

- `004-bridge-staleness-session-death.md` recorded the failure of using the previous response’s tokens for a much larger next request.
- A growth-adjusted method was proposed and reported against the session evidence.

### Middleware mechanism accumulation

- Stage 1 compression removed old thinking and clipped old tool/assistant text.
- P4/DCP replaced stale tool results with fixed placeholders.
- Stage 2 removed message pairs when higher thresholds were crossed.
- Persistent drops attempted to stop client resend from restoring removed material.
- Watermarks grouped transformations into batches.
- Cooldowns and cycle detection attempted to prevent repeated destructive action.
- Usage spoofing delayed client compaction while separate telemetry retained native usage.
- Cache-control markers and tool-list stabilization attempted to preserve reusable regions.
- Every addition addressed a real observed failure, but their state and ordering interacted.

### February 27–28 — archive/reconstruction redesign

- `PLAN-session-context-store.md` proposed replacing in-flight mutation with “capture everything, reconstruct optimally.”
- It separated full per-session archive capture from the reduced provider-facing representation and preserved tool pairs and recent assistant state.
- `REPORT-FEB28-SESSION-ANALYSIS.md` identified a shared accounting value crossing process/session boundaries.
- `TESTPLAN-FEB28-COMPREHENSIVE.md` proposed process-scoped identity, watermark-aware transformations, fixed replacement text, and replay-based evaluation.
- These were proposals and verified historical observations, not yet Session Glass.

## Early March 2026 — Session Glass

### March 3–4 — ownership cutover

- The March 4 `SESSION-GLASS-IMPLEMENTATION-PLAN.md` is a major redesign, not necessarily the project’s origin.
- It proposed replacing seven interacting mutation mechanisms with proxy-owned per-session state.
- The proxy would ingest client history once, preserve positions, keep coherent whole messages, evict selected units, and serve its own frozen request view.
- Evicted material would be written to a shadow archive and index.
- Stable reference messages would point to displaced history.
- System, tools, and messages became separately managed cache-sensitive planes.
- The design selected Go to avoid the predecessor hot-reload/lifecycle hazards.

### March 5 — early local-cache operation

- Early operation recorded many evictions and repeated first-hour churn.
- Aggregate “four hours” or “millions of tokens evicted” figures described activity, not four hours of uninterrupted useful continuity.
- The experience established that eviction count and useful session recovery are different outcomes.

### March 6–7 — chained Glass incident

- `docs/2026-03-07-glass-incident-synthesis.md` reconstructed multiple damaged and fresh lanes.
- A mutable reference near the front of the request invalidated later reusable content.
- Restoring stale whole-file code mixed generations of architecture.
- Persisted replay state could remain structurally invalid.
- Fresh lanes could enter repeated small post-overflow evictions.
- A window-start pointer did not freeze the retained bridge itself.
- Invalid message structures and fallback/reset paths reintroduced full history.
- Some severe misses appeared despite locally stable measured prefixes, preventing one local-mutation explanation from covering every event.
- The emerging correction was a one-way post-overflow pinned frame plus external history, not another trim pass.
- The methodological correction was replay-first analysis before further live edits.

## March 9–12 — memory, facts, and chapters

### March 9 — operational facts

- `glass-operational-memory-plan.md` proposed extracting authorizations, commands, decisions, credentials, discoveries, and other operational facts from evicted history.
- Facts would be stored per session, compacted, and injected into a replaceable system-prefix budget.
- The intent was to recover state before the model knew what it needed to ask for.

### March 10–11 — fact timing and cache effects

- Asynchronous fact publication could finish after the first request that needed the result.
- Changing the fact block created punctuated prefix resets.
- Stable fact content did not cause continuous mutation; the update event caused the transition.
- `COUNTER_AMNESIA.md` and `fact-fix-impact-analysis.md` explored ordering and cache impact.

### March 12 — chapters become memory authority

- In `GLASS_BOOK.md`, the operator proposed book-like chapters preserving session history.
- The design distinction became: facts and summaries are navigation; chapters are memory.
- Chapters should preserve request order, decisions, failures, corrections, and authority rather than only selected snippets.
- Indexes and bookmarks should support targeted reading without replaying every chapter into active context.

## March 14–16 — pinned frames, structure, summarization, and scheduling

### March 14 — first eviction and structural failures

- Raw March 14 logs recorded first eviction, shadow/bookmark writes, a `pinnedTailStartPosLocked` panic, retries, role repair, and large cache creation.
- Non-contiguous anchor+tail projection created same-role adjacency.
- Blind same-role repair could drop an assistant tool call and orphan the following tool result.
- Pair-aware repair and structural validation became necessary together.
- `PRIME_TEST_METHOD.md` and the burn-rate report formalized hypothesis → baseline → simulation → metrics → comparison → verdict.

### Chapter recursion failure

- A “verbatim” chapter approach recursively embedded large tool-read contents.
- One archive reached hundreds of kilobytes and was too large for straightforward recovery.
- Repeated reads of that archive did not reliably recover the governing task.
- A terminal-like projection later retained user/model prose and concise tool descriptions while omitting raw tool-result bodies.
- This reduced file size but introduced an explicit fidelity limit: human-readable chapters were not lossless API transcripts.

### Recovery gate failure

- A recovery gate repeatedly demanded `recovery-001.md` and `recovery-002.md` after read attempts.
- The path existed in the assistant Read `tool_use` input while the gate expected it in returned content.
- The loop consumed further context and quota instead of restoring productive work.
- Later gate logic recognized both the tool invocation and tool-result representation.

### Rolling summarizer failure

- Initial summary production ran synchronously inside request processing and stalled the user.
- Moving normal chunk generation to a background goroutine addressed that path.
- Eviction-time stitching/delta work remained a distinct synchronous boundary.
- Other sessions recorded summarizer HTTP 400 responses caused by request/config/auth/beta uncertainty.

### Threshold and serializer findings

- A reported 300K eviction trigger sat above the observed useful/client ceiling in some sessions.
- A global serializer grouped calls by session but could delay other sessions 40–250 seconds.
- The March 16 aggregate analysis reported fewer switches with no aggregate break-rate improvement and higher cost per switch.
- That weakened the claimed benefit of the tested serializer configuration without proving every scheduler useless.

## March 18–20 — provider separation, hybrid compression, and authentication

### OpenAI-compatible split

- `2_LANE_OAI_ANTRO.md` prescribed a native Chat Completions lane rather than translating through Anthropic roles, cache controls, and SSE behavior.
- Per-lane thresholds and request formats became explicit.

### Compression/saturation V3

- A hybrid design selectively compressed old content, tracked information-loss ratio, generated rolling summaries, and flushed compressed history at saturation.
- Seven PRIME-GOLDEN tests were designed for reduction, idempotence, structure, saturation, post-flush validity, multi-cycle bounds, and session isolation.
- Those tests used mocked summaries and local estimates; they verified structural behavior, not semantic recovery or provider billing.

### Authentication scheme correction

- Prefix warmer and fact-extractor requests replayed an OAuth bearer value as `x-api-key`, producing 401 responses.
- The recorded fix carried authentication type as well as value.
- March 20 logs later showed repeated warmer 401s after captured OAuth expiry, demonstrating that correct scheme and credential freshness are separate requirements.

## March 19–23 — identity, Tengu, subagents, and cache incident

### PID-qualified identity

- System-prompt hashing alone assigned multiple Claude Code processes the same conversation identity.
- PID-qualified identity produced distinct recorded session tracks.
- A bounded live observation then showed two tracks maintaining high recorded cache efficiency; it did not settle every provider cache mechanism.

### Subagent blocking and classification

- Model-routed Haiku/Sonnet calls were blocked with synthetic successful responses to avoid retry storms in one historical configuration.
- Full-system agent-tool children required established-parent context because a fresh main session can also begin with few messages.
- Small-system, agent-tool, and main-session traffic became distinct policy classes.

### Serializer starvation

- Parent-affine subagents refreshed activity and kept in-flight counts nonzero.
- Another session waited until the 120-second queue timeout.
- Separate subagent in-flight accounting addressed the priority inversion.
- Faster switching was followed by higher cache creation, showing that latency and cache economics were coupled.

### Tengu/client binary audit

- Static reverse engineering compared Claude Code v2.1.58 and v2.1.74.
- The audit recorded 74 feature gates, nine boolean gates, five dynamic configs, and extensive telemetry calls in the inspected build.
- It identified deferred tools, global system-prompt cache, cache-aware compaction, effort, tool-result formatting, session-memory, strict-schema, streamed execution, and other client-side controls.
- Several opaque flag purposes remained unknown or inferred from code context.
- A per-conversation attribution header, changing deferred-tool set, and default-off global cache became client-side prefix research leads.
- The transcript requesting exact meanings of all newly active flags ended unfinished; the public Tengu chapter preserves that limit.

### March 23 burn incident

- `analysis/2026-03-23-cache-break-root-cause-report.md` correlated request rows with classifier, serializer, compression, concurrency, and code-patch windows.
- Its initial provider-LRU headline was revised inside the report toward subagent lane collision and scheduling interactions.
- Agent-tool children inherited shared system material, so system-derived isolated suffixes could merge divergent children.
- Several code patches and restarts landed during the incident, contaminating causal attribution.
- Cause weighting was explicitly left for replay-level validation.

## March 24–25 — quota exhaustion and controlled replay

### Filed quota incident

- Three five-hour-limit exhaustion events were recorded on March 24.
- Shared report data recorded a first exhaustion after roughly 4.5 hours and a second after approximately 51 minutes following reset.
- Cache-creation totals increased relative to the preceding comparison day.
- The filed reports requested investigation and credits.
- One report identified non-persisted breakpoint-anchor state as verified; later research added threshold, watermark, request density, and other contributors rather than erasing that recorded diagnosis.

### Compression/anchor falsification sequence

- Initial analysis blamed anchor movement and treated compression as irrelevant.
- Database/log correlation then aligned compression watermark events with many large cache creations.
- A fixed-anchor test demonstrated that compression alone could change the local prefix.
- Anchor-only and combined events were also recorded.
- The operator clarified the intended invariant: transform a message before the provider first sees it, not merely replace it with equal-length bytes later.

### Eager compression

- A three-variant local test compared no compression, post-build batch compression, and pre-build eager compression.
- It recorded seven modeled within-sequence changes for the old order and zero for the eager order.
- The eager variant was implemented and its golden test passed.
- Later live traffic showed severe regression because operational ordering/clamping still compressed material already observed by the provider.
- The change was reverted to batch watermark advancement.
- This became the canonical example of a locally verified result that failed under a more complete live sequence.

### Threshold replay

- Thresholds 4, 8, 16, and 40 produced different modeled reuse, creation, uncached mass, and total prompt mass.
- Threshold 16 had the highest modeled reuse; threshold 40 had the lowest modeled creation/uncached totals.
- All variants had zero exact replay matches against captured final output.
- No single threshold was established as universally optimal.

### Compression-off replay

- Across 138 fixtures, disabling compression more than doubled modeled total prompt mass and increased modeled uncached and cache-creation totals.
- Compression-on and compression-off both had zero exact final replay matches against captured output.
- The result established a local comparative tradeoff, not exact provider billing.

### Interleaved versus sequential replay

- The same 138-request corpus produced identical per-lane local hash transitions in timestamp-interleaved and per-conversation sequential order.
- This falsified ordering as the cause of local transformation differences in that corpus.
- It did not prove the absence of every provider-side concurrency or residency effect.

## March 25–27 — evidence recovery and hardening

### Replay and diff discipline

- Existing request triples, cache snapshots, prefix events, DB rows, and snippet backups were located after earlier claims said fixtures did not exist.
- Short fixtures were rejected as incapable of exercising compression/batching.
- Production configuration was required instead of a stale synthetic config.
- Snippet backups established exactly when threshold and anchor changes entered or left the code.
- Live experiments were stopped when cost rose; offline analysis became the default for local causal questions.

### Metric corrections

- Historical cache-efficiency values using `read / input` produced impossible percentages.
- Later rows matched `read / (read + create + input)`.
- Break/severe/cold status fields were thresholds, not direct provider invalidation events.
- Short burn windows could continue reporting extreme slopes from a fixed quota jump over a shrinking denominator.
- A corrected burn estimator anchored its window to the newest sample and required freshness.
- “Subagents” counted request and blocked events, not distinct logical agents.
- UTC/local-time confusion caused old rows to be treated as current traffic.

### PID fallback replay

- Clearing a captured PID after capture left already-derived keys unchanged.
- Recomputing ingress without established-parent memory erased agent-tool classification.
- Recomputing without PID collapsed unrelated lanes.
- One collapsed scenario appeared to improve cache reuse while greatly increasing modeled prompt mass—a false-positive metric win.

### PID/identity hardening

- Streaming identity began preferring request-context and Glass-derived keys over pidless hashing.
- Serializer key derivation accepted ingress classification.
- Unknown-PID/unmapped subagents used a self-gate instead of unrestricted coordination bypass.
- Fallback/no-gate counters exposed degraded paths.
- A normal restart initially served a stale binary; explicit build plus restart exposed the new debug route.
- Post-restart observations recorded intended parent-gate use and zero fallback/no-gate counts in the observed sample.
- The report explicitly retained broader unhealthy cache behavior as outside that narrow fix.

### Small-system repeat-cache experiment

- Repeated isolated small-system traffic was intentionally uncached and therefore paid full input repeatedly.
- A guarded promotion experiment enabled upstream caching after repeat hits.
- The first live sample added creation without a read-side payoff.
- The experiment was disabled and later made an explicitly rejected runtime option.
- Later investigation separated prefix identity, restart restoration, provider TTL expiry, shared-prefix warming limits, and message-prefix mutation.

### Warming

- Shared warming kept only system/tools prefix profiles active.
- It required credentials captured from real traffic and did not warm full message history.
- Idle gaps beyond the requested provider TTL caused cold recreation despite byte-identical local prefixes.
- A hot message-lane warmer was added for runtime message prefixes; restart-time warming remained limited until auth capture.

## March 27 — OpenAI-compatible OMP/Qwen incidents

- An OMP request exceeded a 65,536-token local-model context at a reported 67,745 tokens.
- Shared-history reconstruction reintroduced messages already removed from the bounded view.
- A historical “fix” dropped the entire OMP harness prompt; HTTP replay passed but the live harness lost cwd and tool-contract behavior.
- This established that transport success is not behavioral parity.
- Request-authoritative trimming then stopped the immediate 178 → 246 → 302 → 344 KB growth spiral.
- Eight recorded requests stayed around 178–186 KB and returned HTTP 200.
- The transcript explicitly retained a slowly growing bounded runway rather than claiming endless operation.
- Long local-model thinking produced no early SSE event.
- Comment-only keepalives could be ignored by the client.
- Empty OpenAI-shaped heartbeat chunks were introduced before the upstream response.
- Synthetic time-to-first-byte improved, while the final historical OMP transcript still recorded a real stall; both results remain published.

## April 19 — Codex capture corrects the design

- Early Codex design modeled Responses traffic as a REST/full-input lane.
- Phase 0 captured a real WebSocket `response.create` path.
- Captures showed inline `instructions`, mixed input items, developer roles, native `previous_response_id`, rate-limit events, token/reasoning details, structured tool-call events, and ordinary child response traffic.
- `/responses/compact` returned `response.compaction` with output and summary material.
- Post-compaction continuation included an opaque compaction item.
- The captures contradicted earlier no-continuation/stateless and REST-only assumptions.
- Local `spawn_agent`/child state existed, while no separate upstream delegation object was observed in the sampled traffic.

### Codex runtime-root isolation

- The design required Codex config, DB, telemetry, shadow, capture, and patches under a Codex-owned root.
- The live verification report recorded no Claude-root mutation and successful Codex-root artifact creation.
- WebSocket and direct Responses flows succeeded in the recorded run.
- Missing usage/quota sidecar files remained explicitly outside that success.

## April 20 — lane separation

- The architecture defined explicit Claude, Codex, Gemini, and OpenAI-compatible owners plus a lane-neutral operator control plane.
- Claude-specific prefix warming and rolling summarization were separated from other provider services.
- Request lane and transport became explicit telemetry dimensions.
- OpenAI and Ollama shared one compatible transport/state implementation unless a real divergence required separation.
- Gemini received native `contents`, `systemInstruction`, function-part, streaming, snapshot, summarization, and optional CachedContent handling.
- Shared operator APIs were intended to normalize controls, not provider wire semantics.
- Later Codex snapshots still exposed Claude-specific Haiku/Sonnet fields, proving route/root separation did not automatically clean the shared schema.

## Security and behavioral branches

- GLASSDD specified a proxy policy gate, heavy analysis sidecar, URL/content/package checks, and a separate host execution barrier.
- Later implementation sessions added OSV, GuardDog, dnstwist, JS-X-Ray, and LLM Guard wrappers.
- Recorded builds and individual tool outputs succeeded in parts of that work; dnstwist and full LLM Guard installation/proof timed out, most deep scanners defaulted off, and no complete end-to-end host barrier result was recorded.
- Template-injection, steganography, and evasion plans are retained as dual-use historical research without publishing operational attack recipes.
- Behavioral research evolved from phrase-level warnings toward telemetry, classifier confidence, action graphs, hard read-before-edit/completion gates, and user-visible oversight.
- Epistemological priming remained a measured hypothesis, not a behavioral guarantee.

## Publication and documentation history

- The production source was copied unchanged into the public repository.
- Raw transcripts, used configuration, runtime databases, logs, captures, credentials, and session archives were excluded.
- An initial research narrative omitted or compressed important Tengu, early-origin, problem/solution, and evidence-boundary material.
- A later documentation-only update expanded selected research chapters but incorrectly promoted publication success into research completion.
- The September recovery read and classified 190 documentary candidates and audited the portfolio sessions that exposed those process failures.
- This publication adds complete source, bug-report, problem, solution, experiment, lineage, memory, Tengu, provider, origin, and chronology registers without changing the production source or image assets.

## Current documented position

The record supports Glass as a longitudinal engineering program that developed proxy-owned session state, explicit mutation boundaries, archival recovery, split session/prefix identity, replay-first falsification, provider-native lanes, and mechanical evidence discipline. It also records lossy archives, non-transactional archival writes, deliberate cache transitions, stale-binary hazards, unresolved provider quota/cache internals, incomplete semantic recovery measurement, and incomplete parity on some provider/security surfaces.

Nothing in the chronology turns “verified in one recorded configuration” into “permanently solved everywhere.” Conversely, later regressions do not erase the verified historical result that preceded them.
