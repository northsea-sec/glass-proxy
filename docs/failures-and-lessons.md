# Failures and lessons

The useful result of Glass research is not a single explanation for every cache miss. It is a set of distinctions learned by testing explanations against incidents, request structure, and later evidence. Source identifiers below refer to the [provenance catalogue](research-method-and-provenance.md#historical-source-catalogue).

## Per-request transformation as an optimization

**Why it looked attractive:** every request offered another chance to remove old material and reduce context size.

**What went wrong:** dynamic reconstruction and repeated mutation changed bytes already eligible for cache reuse. The client could also resend pre-transformation history, forcing the middleware to track persistent drops and reconcile its own edits. The early middleware accumulated mechanisms that interacted rather than composing cleanly. [R1, R2]

**Lesson:** proxy ownership and explicit mutation boundaries are more tractable than independently optimizing every resubmitted request. Deterministic output after a transformation matters as much as the amount removed.

## Compressing inside the cached prefix

**Why it looked attractive:** shrinking old messages reduced nominal input size on every request.

**What went wrong:** the pre-Glass middleware advanced a trim watermark without any awareness of what the provider had already cached. Each watermark advance sent a batch of previously uncompressed messages in compressed form for the first time, directly inside the byte-prefix region—one guaranteed break per batch. The watermark limited break frequency; it did not prevent breaks. [R14]

**Lesson:** compression policy is a cache policy. Glass clamps its compression watermark below the breakpoint anchor so that compression never changes bytes the provider may have cached uncompressed, and it batches watermark advances so the remaining cost is one transition per batch instead of one per request.

## Eager compression that passed its tests

**Why it looked attractive:** advancing the watermark every request, clamped below the anchor, eliminated compression-caused inter-request breaks in golden tests.

**What went wrong:** in live traffic the clamp used the new anchor position and compressed messages the provider had already cached uncompressed. Measured break counts rose from roughly forty to over three hundred, and the variant was reverted to batch advancement. Single-loop tests could not see the inter-request gap between “first sent” and “first compressed.” [R1, R12, R14]

**Lesson:** a cache invariant must be stated for sequences of requests, not single requests. The surviving design encodes this as an explicit batch invariant: between watermark advances, all affected prefix bytes must remain identical.

## Treating any cache break as a defect

**Why it looked attractive:** cache creation was expensive, so reducing anchor movement appeared beneficial.

**What went wrong:** the larger-threshold experiment described in the March 25 report left too little history behind the reusable anchor. Preventing transitions also prevented useful prefix growth. Compression had a related unavoidable transition: content sent uncompressed on one turn could be compressed on a later turn. [R1]

**Lesson:** measure the size and reuse of the cached region, not merely the number of changes. Idempotence prevents repeated shortening; it does not make the initial change invisible.

## Compression disabled as the answer

**Why it looked attractive:** if compression transitions cause breaks, removing compression should remove the breaks.

**What went wrong:** replaying a captured corpus with compression disabled removed the transition events but more than doubled total prompt mass, because the uncompressed tail rode along on every request. The create/read mix changed, but the cost did not disappear—it moved. [R14]

**Lesson:** compression is a tradeoff between transition cost and steady-state prompt size. The correct question is the cadence of transitions against the size of the reusable region, not a binary choice between compressing and not.

## A frozen reference as the complete overflow fix

**Why it looked attractive:** changing a reference near the front of the request plainly invalidated later prefix reuse.

**What went wrong:** removing that mutation did not remove the retained bridge or repeated small evictions. Fresh sessions still showed post-overflow instability. A window-start pin alone also failed in the reported replay. [R3]

**Lesson:** after overflow, the visible working-set policy may need to change as a whole. The pinned-frame direction came from this distinction, not from renaming the existing trim operation.

## One cause for every severe miss

**Why it looked attractive:** repeated misses near the same point suggested a common mechanism, such as prefix drift, interleaving, or a provider backend change.

**What went wrong:** the incident record contained invalid local replay, changing prefixes, fresh eviction loops, and apparently stable-prefix misses. None of those observations justified collapsing every event into the same category. [R3]

**Lesson:** first establish the final request shape, local mutation event, and native usage response. A provider-internal explanation is not established by eliminating only one local hypothesis.

## Shared usage state as session truth

**Why it looked attractive:** a common usage bridge made token accounting available to multiple local components.

**What went wrong:** the February analysis attributed an eviction cascade to cross-process contamination of shared usage state. The wrong observation could drive a destructive context decision. Reconstruction was not active on that path during the event. [R2]

**Lesson:** accounting needs the same ownership discipline as messages. A shared file or convenient status surface is not automatically authoritative for a particular session.

## Facts as primary memory

**Why it looked attractive:** a compact fact table could preserve useful operational state at low context cost.

**What went wrong:** recency-based snippets omitted sequence, qualifications, decisions, and the connection between an instruction and its correction. Asynchronous extraction could also arrive after the first post-eviction request. [R4, R5]

**Lesson:** relevance hints and recovery sources are different things. Chapters preserve more of the ordered conversation; facts and summaries can help navigate them. Reading a chapter remains a deliberate recovery action, not an automatic consequence of storing one.

## Dynamic facts in the system prefix

**Why it looked attractive:** system placement made selected context consistently visible.

**What went wrong:** stable fact blocks could coexist with cache reuse, but changes to those blocks produced punctuated cache resets. The problem was placement and change cadence, not evidence that every request leaked continuously. [R4]

**Lesson:** content can be operationally useful and still be a poor fit for a cache-sensitive region. A memory update policy needs a cache-update policy.

## Serialization as proof of a cache-slot model

**Why it looked attractive:** grouping sessions should reduce switching if interleaving was causing cache replacement.

**What went wrong:** the March 16 analysis reported fewer switches without an aggregate improvement in its break-rate comparison. Some confident explanations in the same report still depended on undocumented cache-slot assumptions. [R6]

**Lesson:** a measured scheduling effect is not proof of the proposed backend mechanism. The evidence weakened the claimed benefit of that configuration; it did not establish universal behavior for all serializers or providers.

## Cross-session interleaving as the break explanation

**Why it looked attractive:** concurrent sessions alternated requests, and cache reads dropped after switches; a provider-side slot/LRU story explained both.

**What went wrong:** three independent tests cut against it. Replaying the same captured corpus interleaved versus sequentially produced identical prefix stability request-for-request, so ordering alone explained nothing in that corpus. Day-level data showed no break-rate increase on days with more concurrent main sessions. The severe-burn day's own root-cause study ended by replacing its interleaving headline with a lane-collision and scheduling explanation. [R14, R16]

**Lesson:** an explanation that fits a timeline must also survive a reordering test. When replaying the identical requests in a different order changes nothing, the ordering is not the cause.

## Subagents sharing one isolated lane

**Why it looked attractive:** isolating subagent traffic from the main session protected the main prefix; deriving the lane from the system prompt gave all subagents of one parent a stable identity.

**What went wrong:** agent-tool subagents from the same client inherited the parent's system prompt, so the prompt-hashed suffix put all of them into one lane. They were isolated from the main session but not from each other, and their diverging message tails thrashed the shared lane on every request. The configuration was efficient under sequential subagent load and destructive under concurrent load. [R16]

**Lesson:** isolation is a relation, not a flag. A lane identifier derived from shared material must not be used as if it distinguished traffic that differs elsewhere.

## A repeat-cache experiment promoted by logic alone

**Why it looked attractive:** repeated isolated small-system lanes were billed at full rate forever; promoting a lane to upstream caching after repeated hits should have paid for itself.

**What went wrong:** the first live sample after enablement showed added cache creation and zero read-side payoff, and the experiment was disabled again. The logic was plausible; the payoff was not measured before promotion. [R18]

**Lesson:** a guarded experiment still needs its payoff measured before the guard is widened. “Should help” is a hypothesis, not a result.

## Attributing a restart spike to byte drift

**Why it looked attractive:** a large burn spike followed a proxy restart, and restarts had previously caused state damage.

**What went wrong:** prefix snapshots proved the hot lanes' bytes were identical across the restart and local state restored coherently. The cold recreates happened after idle gaps longer than the requested one-hour provider cache TTL. The spike was upstream expiry across the idle gap, with the experiment's extra cache creation additive—not Glass byte drift. [R18]

**Lesson:** before blaming local mutation, compare the bytes. A restart and a TTL expiry can occupy the same window and still be different causes.

## Control-plane and runtime schema drift

**Why it looked attractive:** a control plane with its own field names is convenient to maintain separately from the runtime.

**What went wrong:** legacy and current key names disagreed for the thinking budget and serializer enablement; a config-save path rebuilt state from defaults and silently dropped unknown keys; a force-thinking fallback injected a budget without raising max tokens, which surfaced as upstream 400 errors. Each layer was locally consistent; the composition was not. [R12]

**Lesson:** configuration identity is the same class of problem as conversation identity—every layer must share the same representation, and a save path that can lose unknown keys is a latent regression.

## “Restart succeeded” as proof of deployment

**Why it looked attractive:** the process came back and answered requests.

**What went wrong:** a restarted process was caught serving a stale binary whose startup scripts only rebuild when the binary is missing or explicitly requested. The missing debug route exposed it. Live verification required comparing binary timestamps, route surface, and startup log signatures together. [R12, R18]

**Lesson:** for a running system, “the new code is live” is a claim that needs its own evidence, distinct from “the process is running.”

## Telemetry windows as truth

**Why it looked attractive:** short-window burn rates and aggregate efficiency gave immediate operational signals.

**What went wrong:** a burn estimator anchored its window to wall-clock time while the newest sample stayed fixed, recomputing the same quota jump over a shrinking denominator and reporting alarming rates during calm periods; the fix anchored the window to the latest sample with a freshness requirement. An earlier efficiency formula divided by input tokens alone and produced garbage values that persisted in stored rows. Separate status surfaces disagreed about where burn data came from. Reports built on calm slices overgeneralized to “no anomalous breaks” while live health checks disagreed. [R12, R14, R18]

**Lesson:** metric definitions, window anchoring, and data sources are part of the research record. Time-local metric values need dates, and an aggregate trend is not a substitute for replaying the actual sequence.

## Live experimentation and stale recovery

**Why it looked attractive:** active sessions exposed the failure quickly, and older backup files appeared to offer a quick way back.

**What went wrong:** live edits, restarts, and stale whole-file rollback introduced additional changes during an incident. Request validity, cache behavior, and recovery state became harder to attribute. [R3]

**Lesson:** a recovery source must match the architecture being recovered. Offline replay can establish local byte and structure changes before live experiments assess remote cache behavior. Replay results themselves do not predict every provider-side outcome.

## Assuming Codex was another full-history message API

**Why it looked attractive:** the early lane design could be modeled as a list of messages to compress and resend.

**What went wrong:** the observed client used WebSockets, continuation IDs, incremental tool-result input, a separate compact endpoint, and opaque post-compaction material. The absence of a field in one sample had been generalized to the client as a whole. [R9]

**Lesson:** a transport branch is part of the semantic contract. In the current source, HTTP response requests pass through local context mutation, while WebSocket frames and native compact requests follow different paths. A lane-wide claim that all Codex traffic is rebuilt or all compaction is removed would be incorrect.

## What these lessons do not establish

The records do not provide a universal optimal batch size, guaranteed cache-hit percentage, semantic fidelity score, or proof of provider LRU topology. They also do not establish that a larger advertised context window is always better or always worse for every task. The contribution is a concrete architecture and a documented reasoning trail—not a benchmark extrapolated beyond its evidence.


## Archive before mutation without an archive transaction

**Why it looked complete:** Session Glass had a shadow writer, chapter writer, indexes, and bookmarks.

**What went wrong:** current rendered shadows clip long text, chapters omit tool-result bodies, and compression can precede eviction. The write path selects/removes the in-memory batch, attempts archive writes, logs errors, and continues. A pre-compression snapshot was introduced as a design response, but the documentary audit did not establish it as the archive writer’s effective source.

**Lesson:** archive existence, archive fidelity, and archive commit ordering are three separate properties. The original exact-archive design remains stronger than the published implementation boundary.

## Passing transport checks while breaking the consumer

**Why it looked attractive:** an exact captured request replay returned HTTP 200 after the OMP harness prompt was removed and tool results were compacted.

**What went wrong:** the real OMP client lost cwd and tool-contract behavior and could no longer perform ordinary work reliably.

**Lesson:** status code and stream validity do not prove agent/tool parity. Consumer-visible invariants belong in the acceptance condition.

## Treating a Read event as recovered understanding

**Why it looked attractive:** a recovery gate could observe a Read call and then release the session.

**What went wrong:** historical sessions read large or recursive archives without recovering the actual task. The gate’s first version did not even observe the correct protocol field.

**Lesson:** file contact, successful tool invocation, model comprehension, and correct next action are four different results.

## Scanner files as end-to-end security

**Why it looked attractive:** OSV, GuardDog, dnstwist, JS-X-Ray, and LLM Guard wrappers compiled and individual tools produced output.

**What went wrong:** some dependencies timed out or defaulted off, failures could be treated as no finding inside individual scanners, dependencies were tied to an adjacent environment, and the host package barrier was a separate deployment step.

**Lesson:** a security claim requires enabled policy, available analyzer, explicit unavailable/error semantics, route coverage, and execution enforcement. Source presence is only one layer.

## Documentation publication as research completion

**Why it looked attractive:** public documents existed, links worked, API publication returned success, and static blobs matched.

**What went wrong:** earlier agents used publication success to mark unread research, internal-ledger work, and missing Tengu/origin questions complete. Concise narrative files then became a substitute for the 190-source research terrain.

**Lesson:** transfer integrity proves what was published, not that the research was complete. This publication adds exhaustive source, report, problem, solution, result, and chronology registers so narrative concision cannot erase coverage.

## Status labels as proof

**Why it looked attractive:** plans and reports used “approved,” “complete,” “verified,” “fixed,” or “production correct.”

**What went wrong:** some documents contained unresolved failures or outstanding comparisons alongside those labels. Later configurations could also regress a result verified earlier.

**Lesson:** retain verified results at their tested scope and publish later regression as a new event. Do not erase verification, and do not universalize it.

## Complete failure coverage

This narrative cannot carry every source-level event by itself. The exhaustive coverage is maintained in:

- [Filed bug reports](filed-bug-reports.md)
- [Complete problem register](problem-register.md)
- [Complete solution register](solution-register.md)
- [Experiments and results](experiments-and-results.md)
- [Problem-to-solution lineage](problem-solution-lineage.md)
- [Complete chronology](complete-chronology.md)
- [Complete source catalogue](source-catalogue.md)
