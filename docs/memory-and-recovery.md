# Memory, archives, chapters, and recovery

Glass memory research was not one feature. It was a sequence of competing representations of prior work: full turn storage, shadow text, extracted facts, references, chapters, indexes, summaries, bookmarks, and recovery gates. Each solved a different part of the problem and introduced different fidelity and cache costs.

## The original continuity problem

Long coding sessions accumulated requirements, operator corrections, commands, tool output, failed experiments, and unfinished plans. Client compaction could shorten that history into a summary that omitted the relationship between an instruction and its correction. Retaining everything indefinitely exhausted active context and amplified low-value tool noise.

The goal was therefore not merely more tokens. It was preserving the state needed to make the correct next decision.

## Per-session context-store proposal

The February context-store plan proposed:

- capturing complete per-session turns outside the provider request;
- retaining full thinking and assistant/tool structure;
- reconstructing a tiered live view;
- keeping recent turns fuller and reducing older material;
- preserving the last assistant message and tool pairs;
- separating archival capture from outbound reconstruction.

This was the first explicit “capture everything, reconstruct optimally” branch. It remained a proposal and used reduction tiers that later Session Glass would simplify.

## Session Glass shadow archive

The March Session Glass design moved conversation ownership into the proxy. When selected messages left the live set, the design wrote them into per-session shadow history and retained stable references.

The intended invariant was archive before loss: displaced content must remain recoverable somewhere outside the bounded request.

The later implementation record exposes limits:

- shadow rendering extracts text rather than preserving the exact inbound API object;
- very long text can be clipped;
- compression may happen before eviction;
- archive writes are attempted after the in-memory selection/removal and errors are logged while processing continues;
- therefore archive materialization is not transactionally guaranteed.

These limits do not erase the original design. They describe the documented divergence between intended exact archival authority and the surviving rendered archive.

## Operational facts

The operational-memory plan attempted to solve first-post-eviction amnesia. An agent that forgot a decision could not know which archive passage to request, so Glass would extract facts itself.

Proposed fact categories included:

- authorization and scope;
- commands and paths;
- decisions;
- credentials and operational parameters;
- discoveries and failed approaches;
- current state and next actions.

Facts were ranked, persisted per session, compacted, and injected into a replaceable system-prompt budget.

### What facts solved

- immediate access to selected operational state;
- compact representation;
- no requirement that the model first know what it had forgotten;
- a bridge across the first request after eviction.

### What facts lost

- sequence and causality;
- the operator’s exact authority and corrections;
- adjacency between proposal, rejection, and replacement;
- negative results and why a path was abandoned;
- full tool and evidentiary context.

Changing the fact block also caused punctuated cache-sensitive prefix transitions. Facts became useful bookmarks but an unsafe primary memory authority.

## Chapters

The operator’s chapter design corrected the fact-first model:

> Facts are bookmarks; chapters are memory.

A chapter should preserve the ordered session: what the operator requested, what was attempted, what failed, what was corrected, and what remained open. An index should identify where relevant material lives without replacing it.

The intended architecture became:

```text
bounded live working context
+ stable chapter/bookmark pointer
+ immutable ordered chapter archive
+ compact index/facts for navigation
+ optional grounded recovery summary
```

## The recursive archive failure

The first attempt to make chapters “verbatim” copied large tool-read outputs into the chapter. A chapter could recursively contain another large transcript or archive read, producing hundreds of kilobytes of repeated material.

The recorded consequences were:

- files too large for one normal read;
- repeated searches instead of coherent recovery;
- recursive “hall of mirrors” archives;
- a present implementation plan still being missed;
- more tool output and context pressure during attempted recovery.

This showed that byte-for-byte tool payload inclusion and useful human recovery were not the same requirement.

## Human-readable chapter projection

The later chapter renderer retained:

- message order and numbers;
- user/model prose;
- roles;
- concise tool-call descriptions;
- message ranges and eviction batch identity;
- paths and indexes.

It omitted or reduced:

- raw tool-result bodies;
- many tool arguments and non-text blocks;
- NUL bytes and some formatting;
- content already compressed before archival selection.

This made chapters smaller and navigable, but not exact transcripts. The public documentation therefore distinguishes:

1. exact original request/turn evidence;
2. canonical local-cache state;
3. shadow rendering;
4. chapter projection;
5. summary/index/bookmark navigation.

No one layer is described as all five.

## Bookmarks and reference placement

Early references were dynamically injected and could change near the front of the request. Later work attempted to bake a chapter-directory bookmark into a stable slot.

Incorrect placement caused:

- user/user adjacency;
- merge of recent tail content into the bookmark;
- a second user operational-context message;
- final validation failure;
- restoration of the original unbounded request.

The correction required reference placement, bounded-frame selection, role alternation, and tool-pair preservation to be designed together.

## Bounded pinned frame

After initial overflow, the system should not continue exposing a changing bridge of old messages. The pinned-frame direction retained:

- an early task/authority anchor;
- a stable recovery entrance;
- a bounded recent working tail;
- the displaced bridge in external history.

This was a mode transition after overflow, not another small trim. The design directly addressed staircase eviction and repeated reingest.

## Rolling summaries

Rolling summaries attempted to prevent one enormous summary call at eviction:

1. summarize bounded chunks during growth;
2. preserve chunk records;
3. summarize any remaining eviction delta;
4. stitch a recovery file;
5. point back to chapter material for detail.

The research required summaries to carry sections for context, decisions, actions, state, and open work, with message references and verbatim anchors.

Summaries remained derived and lossy. Passing a structural summary test did not prove retention of governing decisions or semantic quality.

## Recovery gate

The recovery gate blocked continuation until the agent read the recovery file. Its original implementation observed the wrong signal: the filename lived in the assistant Read tool call while the gate looked for it in the user’s returned content.

The result was dozens of repeated reads of `recovery-001.md` and `recovery-002.md` without productive continuation.

Later logic checked both:

- assistant `tool_use` input containing the recovery path;
- user `tool_result` content containing the path or filename.

That verifies a protocol event. It still does not prove comprehension or correct application of the recovered instructions.

## Recovery quality failures

The corpus records several distinct failures:

- archive file existed but agent did not read it;
- archive was read but governing task was not recovered;
- archive was too large and recursively polluted;
- summary omitted the specific implementation plan;
- recovery gate never cleared;
- synchronous summary work blocked the request;
- chapter bookmark damaged message structure;
- validator fallback restored the original oversized history;
- facts surfaced stale or incomplete authority;
- high context itself reduced the reliability of the agent interpreting the recovery material.

These cannot be reduced to “memory file present.”

## Cross-provider scope

- **Anthropic:** LocalCache, eviction, pinned frames, shadow/chapter, optional rolling summary, and recovery gate.
- **Codex HTTP:** Responses-item local state, compression/eviction, shadows, and optional summaries; native WebSocket and compact paths are distinct.
- **Gemini:** contents/parts state, compression/eviction, snapshots, optional summaries, and optional CachedContent.
- **OpenAI-compatible/Ollama:** request-authoritative messages and in-memory state; historical work proposed chapters but did not establish the same recovery system as Anthropic.

Memory concepts can be shared. Wire objects, continuation semantics, persistence, and recovery guarantees cannot be copied mechanically.

## Documented final boundary

Glass’s distinctive memory contribution is the separation of active context, durable history, navigation, and recovery authority. The records also show that the implementation did not produce a single lossless, transactional, automatically comprehended memory system. That limit belongs in the contribution, not hidden from it.
