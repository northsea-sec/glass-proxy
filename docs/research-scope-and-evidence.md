# Research scope and evidence rules

## Publication promise

The public research record includes every in-scope documentary source at catalogue level, every actual filed bug/incident report, every documented problem family, every solution or attempted solution, every material experiment/result, and the full dated chronology. It does not cherry-pick only successful or flattering outcomes.

Private source bodies remain private because they include raw sessions, machine state, request identifiers, credentials, and operational security material.

## Source classes

| Class | What it establishes |
|---|---|
| Original design | What was proposed: ownership, invariants, interfaces, algorithms, phases, and acceptance conditions. |
| Filed bug/incident report | What the report records as symptom, evidence, diagnosis, impact, remedy, verification, and status. |
| Incident reconstruction | A later chronology or causal account; constituent evidence and later corrections remain visible. |
| Research report | Experiments, comparisons, reverse engineering, and evaluated alternatives. |
| Replay/data result | Behavior of the named fixture, model, query, or captured corpus. |
| Transcript | Operator instructions, chronological actions, failures, corrections, and embedded historical output. |
| Current-source observation | What the published source snapshot implements at the inspected path. |
| Dated live verification | The recorded runtime result for that dated procedure and configuration. |
| Operator testimony | The operator’s direct account of invention, usefulness, chronology, cost, or failure. |
| External primary source | Provider contract, paper, public release, issue, or policy at its dated scope. |

## Verification rule

When a report states that a result was verified, the public record states that it was verified in that report’s recorded setup. No fresh rerun is required merely to repeat the historical result.

A later contradictory result is recorded as one of:

- regression under a later version/configuration;
- narrower scope than initially claimed;
- corrected diagnosis;
- different experiment or metric;
- explicit supersession.

It does not silently erase the earlier verified event.

## No multiplication of evidence

Copied report variants, shared tables, and generated summaries are all catalogued, but they are not counted as independent repetitions of one experiment. Each file remains visible and its relationship is published.

## Negative results

The publication retains:

- failed implementations;
- tests that passed locally but failed live;
- unauthorized or contaminated experiments where they affected the history;
- reverted changes;
- dormant features;
- unsupported provider explanations;
- stale binaries and configuration drift;
- archive/recovery failures;
- incomplete provider parity;
- unresolved questions.

A negative result is part of the contribution.

## Terminology boundaries

- **Prompt cache:** provider-observable create/read behavior and documented contract.
- **KV cache, slots, LRU, backend:** provider-internal descriptions only when externally sourced; otherwise historical project hypotheses.
- **Prefix hash:** Glass-local measurement, not proof of provider hit.
- **Cache break:** always accompanied by its metric definition or historical usage.
- **Session:** must identify the relevant key/PID/lane, not only a model or system hash.
- **Archive:** identifies the representation—raw request, local cache snapshot, shadow, chapter, summary, or bookmark.
- **Verified:** qualified by source/version/procedure when necessary, not replaced with “unverified” merely because this publication did not rerun it.

## Public/private boundary

Public:

- sanitized chronology;
- source titles and roles;
- problem/solution/result registers;
- architecture and research methods;
- provider/public-paper citations;
- historical numerical results when safe and properly scoped.

Private:

- raw transcripts;
- credentials and credential paths;
- live prompts and system dumps;
- request IDs and full payloads;
- used configuration, databases, logs, captures, sessions, and archives;
- operational evasion instructions;
- unrelated project material.

## Completeness cross-check

The final documentation set must satisfy:

1. all 190 documentary candidates appear in `source-catalogue.md`;
2. all 118 bug/incident-bearing sources appear in the problem register’s coverage table;
3. every actual filed report appears in `filed-bug-reports.md`;
4. every deduplicated problem family maps to one or more solutions;
5. all documented solution attempts appear in `solution-register.md`;
6. every material experiment appears in `experiments-and-results.md`;
7. every dated event appears in `complete-chronology.md` or its linked source record;
8. every public narrative claim points to a register or source class;
9. exclusions have content-based reasons;
10. no private source body is published merely to prove completeness.
