# Claim Ledger -- March 27, 2026

## Status Key

- `verified`: corroborated by code, tests, logs, config, or replay capture
- `partially verified`: some supporting evidence exists, but not end-to-end
- `contradicted`: evidence directly conflicts with the claim
- `unverified`: not yet checked deeply enough

| Claim | Status | Evidence | Notes |
|------|--------|----------|-------|
| March 1 was only chaos with no durable methodological signal | contradicted | `text-CODEX-DEGRADED.clean.md`, `text-PRIME-SHIT.md` | The day already shows artifact-based analysis, snippet-backed edits, explicit rollback, and verification-first lessons, even though execution drifted |
| The March 5/6 persistence fix eliminated cold starts in general | contradicted | `text-PRIME-GLASS-SHIT.md` | It only helped restart recovery for the same conversation; it did not solve brand-new-session cold starts |
| A later positive metric trend can justify an earlier unverified live config flip | contradicted | `text-PRIME-SHIT.md` | The transcript itself preserves the opposite lesson: verify the code path first |
| PRIME-GOLDEN was only preserved later as polished docs, not in raw sessions | contradicted | raw corpus under `/home/user/nataraja/text-PRIME-GOLDEN*`, especially `text-PRIME-GOLDEN.md`, `text-PRIME-GOLDEN-1.md`, `text-PRIME-GOLDEN-2.md` | The raw lineage survived and already contains the method in session form |
| The evening March 6 incident began only with `345708` | contradicted | `docs/2026-03-07-glass-incident-synthesis.md`, archived rollback shadows for `151639` and `152756` | Preserved March 6 shadow artifacts show damage already underway before the late-night `345708` collapse |
| PRIME-GOLDEN methodology existed and was methodical | verified | `docs/2026-3-14/PRIME_TEST_METHOD.md`, `docs/2026-3-14/BURN_RATE_ROOT_CAUSE_AND_PRIME_GOLDEN_TESTS.md`, `internal/glass/prime_golden_test.go` | Survives as both prose and executable tests |
| The later Glass process failure had no clear precursor in the older GOLDEN work | contradicted | `text-PRIME-GOLDEN-1.md`, `text-PRIME-GOLDEN-2.md` | The older stack had already identified the same meta-failure: deploying conclusions without simulation and losing methodological continuity |
| The PID-based conv-id fix solved the whole older system by itself | contradicted | `text-PRIME-GOLDEN-22.md` | The serializer was still using the old collision-prone identity extractor and had to be fixed separately |
| The remaining `101127` problem was mainly a uniquely poisoned later state | contradicted | `/home/user/nataraja/incident_reconstruction/2026-03-07/CURRENT_STATE_REPLAY_101127_FINDINGS.md`, `current_state_replay_101127_preserved_badwindow.json`, `current_state_replay_101127_current.json` | The same bad sequence reproduces the same collapse from both an earlier preserved snapshot and the later live state |
| Replay artifacts can be treated as exact numeric reconstructions of the original live rows | contradicted | `CURRENT_STATE_REPLAY_101127_FINDINGS.md`, replay JSON outputs, direct DB query of `requests` for `2719b7a469d9_101127` | Replay and live DB agree on the structural failure shape, but not on exact per-step eviction counts |
| Compression-inside-prefix was discovered before March 25 | verified | `docs/2026-3-16/session-timelines.md` | This was not a brand-new March 25 discovery |
| `345708` can be explained entirely by persisted replay poison | contradicted | `/home/user/nataraja/incident_reconstruction/2026-03-06/345708_LANE_REPORT.md`, archived `345708` lane files | The better reconstruction explicitly separates persisted replay invalidity from later fresh live re-evictions |
| March 24 live replication cleanly proved one single cache-break cause | contradicted | `text-GLASS-CACHE_BREAK*.md` | The raw chain includes a monitoring/query mistake, then converges on multiple active causes: same-conversation compression resets, small_system prefix competition, and some cross-conversation thrash |
| March 25 replay/diff methodology was only proposed, not executed | contradicted | `text-GLASS-BACK2RESEARCH-9.md`, `analysis/snippet_backup_diffs.txt`, `analysis/threshold_replay_comparison.txt`, `analysis/compression_off_replay.txt`, `analysis/interleaved_replay_results.txt` | The tiered method was materially executed and persisted to disk |
| March 25 added three cache modes in code | verified | `REPORT-2026-03-25.md`, `internal/glass/session.go`, `internal/glass/context_cache_mode_test.go` | Code and tests support the claim |
| March 25 three-mode work was fully live-validated before the report | contradicted | `REPORT-2026-03-25.md`, `text-GLASS-BACK2RESEARCH-12.md` | The report itself lists outstanding live work, and the raw same-night sessions say `off` / `context_api` remained the untested alternatives |
| The March 25 "verified facts" that compression was not the culprit held through the night | contradicted | `text-GLASS-BACK2RESEARCH-12.md`, `docs/2026-3-16/session-timelines.md` | The same-night raw chain explicitly withdraws those facts; earlier March 16 material had already implicated compression inside the prefix |
| March 25 fixes were fully operationalized end-to-end | contradicted | `config_server.py`, `/home/user/.claude/glass_config.json`, `/tmp/glass-proxy/current-19999.log` | Control-plane/runtime schema drift remained live |
| Live control plane preserved unknown config keys on save | contradicted | `config_server.py` before March 27 patch | Pre-patch save path rebuilt config from `DEFAULT_CONFIG` and could wipe fields like `context_cache_mode` |
| Live config/runtime used a clean force-thinking budget path | contradicted | `/home/user/.claude/glass_config.json`, `/tmp/glass-proxy/current-19999.log`, `text-0auth-token-expiry-time.md` | Runtime was still falling back to injected default budget |
| The current `max_tokens` 400 is likely a local compatibility issue | verified | `internal/forcemode/thinking.go`, `/tmp/glass-proxy/current-19999.log`, March 27 replay captures, `text-GLASS-BACK2RESEARCH-4.md` | Pre-patch fallback path could inject `budget_tokens=31999` without also raising `max_tokens` |
| Serializer config keys were consistent across runtime and startup tooling | contradicted | `internal/serializer/serializer.go`, `start.sh`, `start-stack.sh` | Runtime read `ser_enabled`; startup defaults wrote `serializer_enabled` |
| Config server and runtime used the same thinking-budget key | contradicted | `config_server.py`, `internal/config/config.go` before March 27 patch | UI/control plane used `thinking_budget`; runtime merge expected `force_thinking_budget` |
| March 23 catastrophe report contains useful timeline anchors | partially verified | `analysis/2026-03-23-cache-break-root-cause-report.md` | Patch-window and DB correlation are useful; weighting of causes still needs replay-level validation |
| Tool-bearing `small_system` requests were identified as a dangerous no-guard branch | verified | `text-GLASS-CACHE_BREAK-2.md`, `text-GLASS-CACHE_BREAK-3.md`, `internal/subagent/classifier.go` | The raw March 24 investigation narrows onto this path; the current classifier now disables upstream caching for it |
| Fresh lane `84551` showed the frozen reference patch was insufficient | verified | `docs/2026-03-07-glass-incident-synthesis.md`, `/home/user/.claude/glass/2719b7a469d9_84551/shadow_index.json`, `/home/user/.claude/glass/2719b7a469d9_84551/state.json` | Archived fresh-lane state shows staircase-like small post-overflow evictions followed by large severe hits |
| `120344` was only live-fire chaos with no serious offline analysis | contradicted | `/home/user/nataraja/incident_reconstruction/2026-03-06/TRAFFIC_REPLAY_BASELINE_REPORT.md`, `/home/user/nataraja/incident_reconstruction/2026-03-06/TRAFFIC_COUNTERFACTUAL_REPORT.md` | The lane had real replay and counterfactual analysis, including a conclusion that prefix stability alone was not sufficient |
| The March 7 `120344` phase ended with full verified closure | contradicted | `/home/user/nataraja/incident_reconstruction/2026-03-06/TRAFFIC_COUNTERFACTUAL_REPORT.md`, `/home/user/nataraja/text-PRIME-GEMINI-FUCKUP.md` | The counterfactual report still asked for more instrumentation, while the later closure transcript shows failed build / stale-log verification and preserved invalid-outbound evidence |
| Later March 7 `120344` live drift exists only as report prose | contradicted | `docs/2026-03-07-glass-incident-synthesis.md`, `/home/user/.claude/glass/2719b7a469d9_120344/shadow_index.json`, `/home/user/.claude/glass/2719b7a469d9_120344/state.json` | The archived lane files preserve repeated reset-like re-evictions and large token churn; the exact burn-rate interpretation still needs DB support |
| The severe March 6-7 `345708` and `120344` rows are only reconstruction prose | contradicted | direct DB query of `/home/user/.claude/glass_debug.db` `requests` for `2719b7a469d9_345708` and `2719b7a469d9_120344` | The current DB directly preserves the severe low-read / high-create rows at the key times |
| Same-model subagents were not a known issue before Glass | contradicted | `text-PRIME-GOLDEN-LIMIT-2.md` | The older stack had already identified same-model subagent detection failure and a paired remedy (`Option D` + `Option A`) |
| The March 25 per-session upstream transport idea was only a proposal and never landed | contradicted | `text-GLASS-BACK2RESEARCH-5.md`, `internal/proxy/transport_pool.go`, `internal/proxy/proxy.go` | The raw note proposes it; the current proxy implements per-affinity transport pooling for `/v1/messages` |
| Per-session upstream transport isolation was only discovered on March 25 | contradicted | `docs/2026-3-16/serializer-analysis.md`, `text-GLASS-BACK2RESEARCH-5.md`, `internal/proxy/transport_pool.go` | The design conclusion already existed on March 16; March 25 is when it was materially implemented |
| Current transport isolation is literally one unique transport per raw conversation, with no exceptions | contradicted | `internal/proxy/transport_pool.go`, `internal/proxy/proxy.go` | The pool is keyed by affinity key, subagents share the parent transport, and the sidecar message path bypasses the pool |
| Current Glass forgot the older same-model subagent lessons entirely | contradicted | `internal/subagent/classifier.go`, `internal/glass/process.go`, `internal/proxy/proxy.go`, focused Go tests on 2026-03-27 | The current stack explicitly classifies `small_system` / `agent_tool`, disables upstream caching, strips cache_control, and preserves parent-aware gating |
| Zeroing `ClientPID` in captured replay fixtures is enough to reproduce the dangerous PID fallback behavior | contradicted | `analysis/2026-03-27-pid-fallback-replay-capture-20260325.json`, `analysis/2026-03-27-pid-fallback-replay-current.json` | In both audited corpora, `captured_meta_pid_zero` matched the captured baseline exactly; the dangerous behavior appears when ingress metadata is recomputed without PID or parent memory |
| `HasEstablishedParent` is only a telemetry hint and does not materially affect replayed behavior | contradicted | `analysis/2026-03-27-pid-fallback-replay-current.json`, `analysis/2026-03-27-pid-fallback-replay-capture-20260325.json` | Removing parent memory dropped `agent_tool` detections and collapsed session lanes in both corpora |
| Higher cache reuse under no-PID replay proves the fallback is healthier | contradicted | `analysis/2026-03-27-pid-fallback-replay-capture-20260325.json` | March 25 capture corpus showed higher apparent reuse after PID loss, but only because lane collapse merged traffic and total prompt mass ballooned from `15.6M` to `25.0M` tokens |
| Captured replay metadata fully preserves the beneficial `agent_tool` split in the March 7 current-state corpus | contradicted | `analysis/2026-03-27-pid-fallback-replay-current.json` | Recomputed ingress with PID and parent memory surfaced `22` `agent_tool` requests that the captured metadata had preserved as `none` |
| Streaming-path re-classification and SSE conv-id fallback were still using weaker context than ingress | verified | `internal/proxy/proxy.go` before 2026-03-27 hardening, `analysis/2026-03-27-pid-fallback-replay-audit.md`, `analysis/2026-03-27-pid-fallback-hardening.md` | The streaming path re-classified with less context and could fall back to pidless hashing; the March 27 hardening pass removed those weaker late-path fallbacks |
| Unknown-PID or unmapped subagents still fully bypass serializer coordination in the common fallback path | contradicted | `internal/serializer/serializer.go`, `internal/serializer/serializer_test.go`, `analysis/2026-03-27-pid-fallback-hardening.md` | The second March 27 hardening pass replaced the common fallback with a self-gate keyed by the subagent's serializer convID; the true last-resort passthrough now only remains when no usable gate key exists |
| "Everything after the last report was fixed" | contradicted | live log, live config, control-plane schema drift | At minimum, the force-thinking and config-preservation paths were not actually fixed end-to-end |

## Immediate Remediations Applied On March 27

| Problem | Fix | Evidence |
|--------|-----|----------|
| Legacy `thinking_budget` not honored by runtime merge | Added alias normalization in config loader | `internal/config/config.go`, `internal/config/config_test.go` |
| Fallback budget path could still violate `max_tokens > budget_tokens` | Added final `max_tokens` safety raise | `internal/forcemode/thinking.go`, `internal/forcemode/thinking_test.go` |
| Serializer alias drift (`serializer_enabled` vs `ser_enabled`) | Added serializer config alias support | `internal/serializer/serializer.go`, `internal/serializer/serializer_test.go` |
| Config save could wipe newer keys and drift aliases apart | Save path now preserves existing config and synchronizes aliases | `config_server.py` |

## Focused Test Result

Verified on 2026-03-27:

```text
ok  	proxy.local/app/internal/config
ok  	proxy.local/app/internal/forcemode
ok  	proxy.local/app/internal/serializer
ok  	proxy.local/app/internal/proxy
```
