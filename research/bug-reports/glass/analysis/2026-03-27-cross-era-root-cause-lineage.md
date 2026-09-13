# Cross-Era Root Cause Lineage

Date: 2026-03-27

## Purpose

This note connects the older PRIME-GOLDEN era to the later Glass proxy era.

The goal is not to say the systems were identical.

The goal is to identify which failure classes, repair methods, and anti-patterns clearly carried forward.

## Audit Rule

Nothing in this file is included because it "sounds right."

Each item below is grounded in one or more of:

- raw transcripts
- replay artifacts
- DB rows
- archived lane state
- surviving code or tests

## 1. Identity Drift Across Subsystems

Older stack:

- `text-PRIME-GOLDEN-22.md` shows the PID-based `conv_id` breakthrough was real in the trimmer.
- The same session then discovers the serializer still uses its own old identity extractor based on a hash of the first user message.
- Result:
  - one subsystem says collision fixed
  - another subsystem still collides

Glass-era analogue:

- March 25-27 work shows a similar pattern in config and runtime behavior:
  - control plane and runtime disagree on key names
  - one layer thinks a mode or budget is configured
  - another layer falls back or silently ignores it

Modern lesson:

- A fix is not real until every subsystem that depends on the same identity or config concept uses the same representation.

## 2. Same-Model Subagents As A Special Failure Class

Older stack:

- `text-PRIME-GOLDEN-LIMIT-2.md` explicitly identifies same-model subagents as a special problem.
- Model-based detection missed them.
- Smaller `sys_chars` values and tiny `msgs=1` behavior exposed them as a different prefix family.
- The remedy pair was explicit:
  - `Option D`: sys-chars-based detection
  - `Option A`: skip `cache_control` injection for subagent / MCP requests

Glass-era analogue:

- March 24-25 Glass raw sessions and surviving code show:
  - dangerous `small_system` and `agent_tool` branches
  - subagent upstream-cache disable added in stages
  - catch-all `_sub_` protection added later

Modern lesson:

- Same-model subagents cannot be treated as ordinary main-session traffic.
- Detection and cache-entry policy are separate responsibilities.

## 3. Structural Replay Beats Elegant Storytelling

Older stack:

- Raw PRIME-GOLDEN sessions already show:
  - what is confirmed from files / DB
  - what is only simulated
  - recommendation changes when tests disagree

Glass-era analogue:

- `CURRENT_STATE_REPLAY_101127_FINDINGS.md` plus replay JSON show a bad window reproducing from both:
  - an earlier preserved snapshot
  - a later live state
- This falsifies "poisoned later state only."

Important boundary:

- Replay is structurally powerful.
- Replay is not guaranteed to clone the original live rows exactly.
- Direct DB comparison for `101127` shows structural agreement without numeric identity.

Modern lesson:

- Use replay to rank hypotheses and isolate behavior.
- Do not overclaim exact historical equivalence unless the DB or captured rows match.

## 4. Live Hotfixing Under Pressure Repeatedly Increased Damage

Older stack:

- `text-PRIME-GOLDEN-1.md` and related sessions identify the bad pattern clearly:
  - deploy without simulation
  - hot-fix live traffic
  - claim verification too early

Glass-era analogue:

- March 1 and March 6-7 show the same cycle:
  - bounded reasoning gives way to live edits
  - restart / reload churn follows
  - rollback or secondary incidents appear

Modern lesson:

- Good technical ideas still become dangerous when they outrun verification and operational discipline.

## 5. Persisted Poison Is Often Real, But Rarely The Whole Story

Older stack:

- The pre-Glass transcripts often distinguish:
  - one explicit bug
  - then the secondary damage caused by reloads, stale state, or cache churn

Glass-era analogue:

- `345708_LANE_REPORT.md` separates:
  - persisted replay invalidity
  - later fresh live re-evictions
- March 7 synthesis plus archived lane files confirm this multi-phase shape.

Modern lesson:

- "We found the bug" is often only phase one.
- We must always ask what damage or unstable state was created around that bug.

## 6. Method Loss, Not Only Code Loss, Is The Real Recurring Catastrophe

Older stack:

- `text-PRIME-GOLDEN.md`, `text-PRIME-GOLDEN-1.md`, and `text-PRIME-GOLDEN-2.md` show the working method explicitly:
  - read the full prior session
  - separate confirmed from approximated
  - simulate before deploy
  - make bounded changes
  - verify behavior honestly

Glass-era analogue:

- The raw March 24-25 work repeatedly shows the same need:
  - replay first
  - snippet backups first
  - one variable at a time
  - do not accept confident summaries over artifacts

Modern lesson:

- The project did not mainly suffer from lack of ideas.
- It suffered from losing verified method continuity between sessions.

## 7. The Most Important Older Lessons That Directly Apply Now

1. Shared concepts must be unified end-to-end.
   Identity, mode selection, and config keys cannot drift by subsystem.

2. Same-model subagents need explicit handling.
   Detection, routing, and cache-entry policy all matter.

3. Structural replay is mandatory.
   Reports and logs are not enough by themselves.

4. Live edits need a stricter burden of proof.
   Especially when the system is already degraded.

5. Persistent notes are not optional.
   The method itself has to survive compaction, not just conclusions.

## 8. Working Hypothesis For The Modern Proxy

The current Glass disorder is not a brand-new mystery.

It is the modern expression of several older, already-known classes of failure:

- subsystem identity drift
- same-model subagent cache pollution
- premature closure after partial fixes
- live-path changes outrunning replay and durable documentation

That is why transcript-first forensics plus direct artifact verification is the right recovery path.

## Source Anchors

- `/home/user/nataraja/text-PRIME-GOLDEN.md`
- `/home/user/nataraja/text-PRIME-GOLDEN-1.md`
- `/home/user/nataraja/text-PRIME-GOLDEN-2.md`
- `/home/user/nataraja/text-PRIME-GOLDEN-22.md`
- `/home/user/nataraja/text-PRIME-GOLDEN-LIMIT-2.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-07/CURRENT_STATE_REPLAY_101127_FINDINGS.md`
- `/home/user/nataraja/incident_reconstruction/2026-03-06/345708_LANE_REPORT.md`
- `/home/user/glass-proxy/docs/2026-03-07-glass-incident-synthesis.md`
- `/home/user/glass-proxy/analysis/2026-03-27-transcript-audit-ledger.md`
- `/home/user/glass-proxy/analysis/2026-03-27-established-truths.md`
