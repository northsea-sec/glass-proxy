# PID Fallback Replay Audit

Date: 2026-03-27

Purpose:

- audit the current Glass proxy's PID-dependent fallback paths
- prefer replay over pure static reasoning wherever practical
- record findings durably as the audit proceeds

## Scope

The main PID-dependent fallback paths identified so far are:

1. ingress classification depending on `HasEstablishedParent`
2. parent-affinity mapping depending on `clientPID`
3. serializer behavior for unknown-PID or unmapped subagents
4. Glass restart/persisted-lane reuse depending on `meta.ClientPID`
5. SSE / telemetry fallback that drops PID and suffix information when request context lacks a conv id

## Core code anchors

- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
- [classifier.go](/home/user/glass-proxy/internal/subagent/classifier.go)
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go)
- [process.go](/home/user/glass-proxy/internal/glass/process.go)
- [sim.go](/home/user/glass-proxy/internal/replay/sim.go)
- [fixture.go](/home/user/glass-proxy/internal/replay/fixture.go)

## Replay corpus notes

### Corpus A: `/home/user/.claude/glass-replay-capture-20260325`

Initial verified facts:

- file count on 2026-03-27: `270`
- captured fixture metadata includes:
  - `client_pid`
  - `request_session_key`
  - `session_key`
  - `request_key`
  - `affinity_key`
  - captured `subagent` classification
- replay-derived census:
  - all captured fixtures have positive `client_pid`
  - captured `subagent.type` distribution under `captured_meta` is:
    - `none=103`
    - `small_system=158`
    - `agent_tool=9`

Interpretation:

- this corpus is strong for auditing PID loss / key drift
- it also contains meaningful captured subagent traffic, so it is useful for testing parent-memory-sensitive fallback behavior

### Additional corpora to inspect next

- `/home/user/.claude/glass-replay-fixtures`
- `/home/user/.claude/glass-replay-current`

These may contain older incident-era fixtures with different classification patterns.

## Replay engine constraints

The offline replay engine in [sim.go](/home/user/glass-proxy/internal/replay/sim.go) preserves captured `MetaSnapshot` by default.

This means:

- it is excellent for "replay the captured path as-recorded"
- it does **not** automatically simulate ingress-time PID failure, because the captured `Subagent`, `SessionKey`, `RequestKey`, and `AffinityKey` are already baked into the fixture

Therefore the PID-fallback audit needs two layers:

1. baseline replay using captured metadata
2. variant replay that recomputes ingress metadata under altered PID assumptions

## First concrete findings

1. The current replay tooling already provides a good execution core, so the cleanest next step is a narrow analysis-only driver that mutates fixture metadata before calling the replay engine, rather than changing production code first.
2. Captured fixture metadata is authoritative for "what the proxy actually recorded," but it is not enough to audit PID fallback by itself because the dangerous branch is ingress recomputation under missing PID or missing parent memory.

## Analysis Driver

Added on 2026-03-27:

- [main.go](/home/user/glass-proxy/cmd/pid-fallback-replay/main.go)

Purpose:

- run the existing replay engine against the same fixture corpus under multiple metadata assumptions
- separate "clear `ClientPID` after the fact" from "recompute ingress metadata without PID or without parent memory"

Scenarios:

1. `captured_meta`
   - replay the fixture exactly as captured
2. `captured_meta_pid_zero`
   - clear `ClientPID` only, preserve the captured keys and captured classification
3. `recomputed_ingress_pid`
   - recompute classification and keys using current ingress logic with PID and parent memory
4. `recomputed_ingress_pid_no_parent`
   - recompute with PID but deny `HasEstablishedParent`
5. `recomputed_ingress_no_pid`
   - recompute with PID forced to zero

The analysis driver mirrors the current ingress primitives:

- `subagent.Classify(..., HasEstablishedParent=...)`
- `trimmer.SessionFingerprint(...)`
- session-suffix isolation
- parent-affinity reuse
- request-key derivation

## Replay Findings: March 25 Capture Corpus

Sources:

- [2026-03-27-pid-fallback-replay-capture-20260325.json](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-replay-capture-20260325.json)
- fixture dir: `/home/user/.claude/glass-replay-capture-20260325`

Baseline:

- `captured_meta`
  - `270` fixtures
  - `7` unique PIDs
  - keys:
    - `request_session=11`
    - `session=14`
    - `request=11`
    - `affinity=11`
  - subagent types:
    - `none=103`
    - `small_system=158`
    - `agent_tool=9`
  - replay economics:
    - `total=15,620,259`
    - `read=12,212,487`
    - `create=1,416,751`
    - `uncached=3,407,772`
    - `reuse=78.18%`

Key comparisons:

1. `captured_meta_pid_zero`
   - clearing `ClientPID` alone changed the stored PID count, but left keys, classification, and replay economics unchanged
   - conclusion:
     - post-capture PID loss by itself is not the dangerous branch

2. `recomputed_ingress_pid`
   - all `270` fixtures changed key strings relative to the captured metadata
   - but subagent types and replay economics stayed identical to the captured baseline
   - conclusion:
     - current ingress recomputation with PID and parent memory is semantically equivalent to the captured path for this corpus

3. `recomputed_ingress_pid_no_parent`
   - `session_key` count dropped from `14` to `11`
   - `agent_tool` disappeared entirely: `9` requests dropped
   - replay economics shifted only slightly:
     - `total=15,472,575`
     - `read=12,070,857`
     - `create=1,335,073`
     - `uncached=3,401,718`
     - `reuse=78.01%`
   - conclusion:
     - parent-memory loss changes real classification and lane structure even when the headline replay reuse barely moves

4. `recomputed_ingress_no_pid`
   - keys collapsed hard:
     - `request_session=4`
     - `session=4`
     - `request=4`
     - `affinity=4`
   - the same `9` `agent_tool` classifications disappeared
   - replay economics looked superficially better on cache reuse:
     - `total=25,025,097`
     - `read=21,026,135`
     - `create=838,075`
     - `uncached=3,998,962`
     - `reuse=84.02%`
   - conclusion:
     - full PID loss can create a false-positive "cache win" while actually merging unrelated traffic into fewer lanes and increasing total prompt mass

Most important pairwise diffs:

- `recomputed_ingress_pid -> recomputed_ingress_pid_no_parent`
  - `session_key_changes=9`
  - `subagent_type_changes=9`
  - `subagent_dropped=9`
- `recomputed_ingress_pid -> recomputed_ingress_no_pid`
  - `request_session_key_changes=270`
  - `session_key_changes=270`
  - `request_key_changes=270`
  - `affinity_key_changes=270`
  - `subagent_dropped=9`

## Replay Findings: March 7 Current-State Corpus

Sources:

- [2026-03-27-pid-fallback-replay-current.json](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-replay-current.json)
- fixture dir: `/home/user/.claude/glass-replay-current`

Baseline:

- `captured_meta`
  - `115` fixtures
  - `3` unique PIDs
  - keys:
    - `request_session=3`
    - `session=3`
    - `request=3`
    - `affinity=3`
  - subagent types:
    - `none=115`
  - replay economics:
    - `total=9,766,196`
    - `read=6,776,758`
    - `create=1,048,124`
    - `uncached=2,989,438`
    - `reuse=69.38%`

Key comparisons:

1. `captured_meta_pid_zero`
   - again, clearing `ClientPID` alone did not change keys, classification, or replay economics

2. `recomputed_ingress_pid`
   - recomputing ingress with PID and parent memory surfaced `22` `agent_tool` requests that the captured metadata had preserved as `none`
   - keys expanded:
     - `request_session=4`
     - `session=7`
     - `request=4`
     - `affinity=4`
   - replay economics materially improved:
     - `total=10,101,115`
     - `read=7,582,311`
     - `create=731,930`
     - `uncached=2,518,804`
     - `reuse=75.06%`
   - conclusion:
     - this corpus proves the captured metadata itself can already miss the subagent split that current ingress logic would apply

3. `recomputed_ingress_pid_no_parent`
   - all `22` `agent_tool` detections disappeared
   - session lanes fell from `7` back to `4`
   - replay economics returned exactly to the captured baseline
   - conclusion:
     - `HasEstablishedParent` is the decisive ingredient for those `agent_tool` splits

4. `recomputed_ingress_no_pid`
   - keys collapsed back to `3 / 3 / 3 / 3`
   - all `22` `agent_tool` detections disappeared
   - replay economics degraded sharply:
     - `total=14,037,227`
     - `read=6,890,043`
     - `create=364,558`
     - `uncached=7,147,184`
     - `reuse=49.08%`
   - conclusion:
     - in this corpus, PID loss is unambiguously harmful and destroys a beneficial `agent_tool` split

Most important pairwise diffs:

- `recomputed_ingress_pid -> recomputed_ingress_pid_no_parent`
  - `session_key_changes=22`
  - `subagent_type_changes=22`
  - `subagent_dropped=22`
- `recomputed_ingress_pid -> recomputed_ingress_no_pid`
  - `request_session_key_changes=115`
  - `session_key_changes=115`
  - `request_key_changes=115`
  - `affinity_key_changes=115`
  - `subagent_dropped=22`
- `captured_meta -> recomputed_ingress_pid`
  - `subagent_gained=22`

Concrete hot lanes in this corpus:

- `2a0db920f14e_120344`
- `c0f862de3e3a_120197`
- `14b18fd054bf_120197`

These are the specific affinity/request lanes where PID- and parent-sensitive `agent_tool` detection changed the outcome.

## Replay Findings: Full Historical Fixture Corpus

Sources:

- [2026-03-27-pid-fallback-replay-fixtures.json](/home/user/glass-proxy/analysis/2026-03-27-pid-fallback-replay-fixtures.json)
- fixture dir: `/home/user/.claude/glass-replay-fixtures`

Baseline:

- `captured_meta`
  - `947` fixtures
  - `4` unique PIDs
  - keys:
    - `request_session=5`
    - `session=5`
    - `request=6`
    - `affinity=5`
  - subagent types:
    - `none=927`
    - `small_system=18`
    - `agent_tool=0`
  - replay economics:
    - `total=90,155,510`
    - `read=81,273,621`
    - `create=5,819,440`
    - `uncached=8,881,889`
    - `reuse=90.14%`

Key comparisons:

1. `captured_meta_pid_zero`
   - again, clearing `ClientPID` alone changed nothing except the PID count

2. `recomputed_ingress_pid`
   - all `947` fixtures changed key strings relative to the captured baseline
   - subagent classification changed on `15` fixtures and subagent shape changed on `33`
   - `13` `agent_tool` requests appeared under current ingress recomputation
   - keys expanded:
     - `request_session=6`
     - `session=8`
     - `request=6`
     - `affinity=6`
   - replay economics stayed close to baseline:
     - `total=90,002,889`
     - `read=80,935,357`
     - `create=5,881,838`
     - `uncached=9,067,532`
     - `reuse=89.92%`
   - conclusion:
     - the full corpus agrees that recomputation with PID and parent memory is close to the captured path, but not identical
     - it also surfaces a non-trivial `agent_tool` slice that the captured metadata did not fully preserve

3. `recomputed_ingress_pid_no_parent`
   - `13` `agent_tool` requests disappeared again
   - `sessionKey` count fell from `8` to `6`
   - replay economics changed only slightly:
     - `total=90,674,656`
     - `read=81,580,048`
     - `create=5,909,257`
     - `uncached=9,094,608`
     - `reuse=89.97%`
   - conclusion:
     - parent-memory loss keeps reproducing as the thing that erases `agent_tool` isolation

4. `recomputed_ingress_no_pid`
   - the corpus collapsed to only `3` request/session/request/affinity lanes
   - the same `13` `agent_tool` detections disappeared
   - replay economics again produced the dangerous false-positive pattern:
     - `total=105,457,186`
     - `read=99,312,229`
     - `create=3,606,971`
     - `uncached=6,144,957`
     - `reuse=94.17%`
   - conclusion:
     - no-PID replay looks "better" on reuse percentage only because it merges far more traffic into far fewer lanes

Most important pairwise diffs:

- `recomputed_ingress_pid -> recomputed_ingress_pid_no_parent`
  - `session_key_changes=13`
  - `subagent_type_changes=13`
  - `subagent_dropped=13`
- `recomputed_ingress_pid -> recomputed_ingress_no_pid`
  - `request_session_key_changes=946`
  - `session_key_changes=946`
  - `request_key_changes=946`
  - `affinity_key_changes=946`
  - `subagent_dropped=13`
- `captured_meta -> recomputed_ingress_pid`
  - `subagent_gained=13`
  - `subagent_dropped=2`

Concrete hot lanes in this corpus:

- `c21431128281_151639`
- `e8929af0f3f8_152756`

These are the lanes where the recomputed `agent_tool` branch shows up repeatedly in the preserved historical fixtures.

## Engineering Conclusion

The replay audit changes the risk ranking.

What did **not** reproduce as the main hazard:

- clearing `ClientPID` after the proxy had already captured keys and classification

What **did** reproduce as the main hazard:

1. ingress recomputation without parent memory
2. ingress recomputation without PID
3. side paths that re-classify without the same `HasEstablishedParent` context as ingress

So the live danger is not simply "PID missing in telemetry."

The live danger is:

- PID / parent-memory quality changes subagent classification
- that changes `sessionKey`, `requestKey`, and `affinityKey`
- and those changes can either collapse unrelated traffic together or erase useful `agent_tool` isolation

## Most Vulnerable Current Code Paths

These replay findings line up with the static audit:

- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
  - streaming path re-classifies with `subagent.Classify(reqBody)` and does not pass `HasEstablishedParent`
- [serializer.go](/home/user/glass-proxy/internal/serializer/serializer.go)
  - unknown-PID or unmapped subagents bypass serializer batching
- [proxy.go](/home/user/glass-proxy/internal/proxy/proxy.go)
  - SSE / telemetry fallback uses `SessionFingerprint(reqBody, 0)` when request context lacks a conv id

These are now the most evidence-backed candidates for the next hardening pass.

## Status

- March 25 capture corpus: audited
- March 7 current-state corpus: audited
- full historical fixture corpus: audited
