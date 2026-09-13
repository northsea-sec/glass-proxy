# Deep Evidence Unearth Report (2026-02-13)

## Objective
Build a comprehensive, low-assumption evidence map across: research markdown, database telemetry, runtime logs, and Claude CLI logs for the period 2026-01-30 through 2026-02-13.

## Anti-Assumption Method (Systematic Pipeline)
1. Inventory first, interpretation second.
2. Time-box all queries to the exact window (2026-01-30 to 2026-02-13).
3. Separate source classes: docs/transcripts vs DB vs runtime JSON stats vs CLI debug logs.
4. Use low-threshold relevance scoring on markdown to maximize recall, then manually inspect outliers.
5. Prefer aggregates and repeatable SQL over anecdotal transcript claims.
6. Mark contradictions explicitly instead of forcing a single-cause story.
7. Preserve traceability: every claim ties back to concrete files/tables.

## Source Inventory
- Relevant markdown files read end-to-end: 86
- Inspector docs markdown: 59
- Recent nataraja docs markdown (window): 27
- Excluded after scoring/manual check: 1
- Fingerprint DB samples total: 45557
- Fingerprint DB samples in window: 29422 (2026-01-30T09:37:47.425368 to 2026-02-12T22:53:07.371181)
- Quota log rows in window: 6142
- Cache debug JSON files in window: 138447
- Context history JSONL files in window: 545 (total 1182890195 bytes)
- Claude CLI debug files in window: 331

## Database Evidence Snapshot (`~/.claude/fingerprint.db`)
- Binding window split (calls, avg CC_new, break% where CC_new>=50k):
  - five_hour: calls=26201, avg_cc_new=12716.4, break_pct_50k=10.10%
  - seven_day: calls=2674, avg_cc_new=22401.5, break_pct_50k=18.18%
  - (null): calls=501, avg_cc_new=2919.2, break_pct_50k=1.40%
  - : calls=46, avg_cc_new=6154.0, break_pct_50k=4.35%
- Daily break peaks (highest break% days):
  - 2026-02-02: calls=1472, breaks_50k=542, break_pct_50k=36.82%, avg_cc_new=42255.0, avg_cr=76186.9
  - 2026-02-08: calls=1618, breaks_50k=519, break_pct_50k=32.08%, avg_cc_new=34804.2, avg_cr=78623.3
  - 2026-02-03: calls=1155, breaks_50k=306, break_pct_50k=26.49%, avg_cc_new=35244.1, avg_cr=80607.8
  - 2026-02-01: calls=2390, breaks_50k=622, break_pct_50k=26.03%, avg_cc_new=29825.6, avg_cr=76585.7
  - 2026-01-31: calls=2390, breaks_50k=338, break_pct_50k=14.14%, avg_cc_new=16816.2, avg_cr=80117.3
- Backend split (calls, avg CC_new, break%):
  - tpu: calls=14651, avg_cc_new=13500.3, break_pct_50k=10.78%
  - trainium: calls=8324, avg_cc_new=13245.6, break_pct_50k=10.72%
  - gpu: calls=6418, avg_cc_new=13521.0, break_pct_50k=10.42%
  - unknown: calls=29, avg_cc_new=24.7, break_pct_50k=0.00%

## Runtime/Log Evidence Snapshot
- `~/.claude/dedup_stats.json`: {"blocked": 19, "coalesced": 0, "inflight": 6, "t": 1770933188.0960166}
- `~/.claude/proxy_cache_stats.json`: {"hits": 0, "misses": 78, "size": 54, "max": 64, "rate": 0.0, "t": 1770933187.3686032}
- `~/.claude/trimmer_stats.json`: calls_processed=255, messages_trimmed_total=73, tokens_saved_messages=6898380
- Cache debug req/post balance: req=69285, post=69162, delta=123
- Cache debug duplicate pressure: interevent<10ms=36610 / 136437 pairs; req-req<10ms=7945
- Largest context history files: /home/user/.claude/context_history/d333ce174bd0.jsonl (869684027 bytes), /home/user/.claude/context_history/2ef2121199db.jsonl (187583605 bytes)

## High-Level Findings (Evidence-Based)
1. The evidence supports a mixed-cause model, not a single bug model.
2. Break behavior changes materially across days/sessions; peaks cluster around specific windows with high avg CC_new and high break%.
3. Duplicate/near-duplicate request timing is frequent in cache-debug artifacts and must be considered in any dedup strategy analysis.
4. Context-history growth is massive in the period (1.18GB total recent JSONL), supporting prior claims that naive full-history export is problematic for `/save`.
5. Transcript narratives contain contradictions; DB aggregates are the stability anchor for cross-session conclusions.

## Relevant Markdown Files Read
### A) Nataraja Docs (Window-Scoped)
- /home/user/nataraja/docs/2026-01-31/proxy-context-trimmer.md
- /home/user/nataraja/docs/2026-02-01/quota-bug-report-draft.md
- /home/user/nataraja/docs/2026-02-01/quota-tracking-tab-implementation.md
- /home/user/nataraja/docs/2026-02-01/redteam-tab-implementation.md
- /home/user/nataraja/docs/2026-02-01/researcher-report.md
- /home/user/nataraja/docs/2026-02-02/RED.md
- /home/user/nataraja/docs/2026-02-02/redteam-evasion-engine-plan.md
- /home/user/nataraja/docs/2026-1-30/FINGERPRINT_PIPELINE_FIXES.md
- /home/user/nataraja/docs/2026-1-30/RATE-LIMIT-INTEGRATION-PLAN.md
- /home/user/nataraja/docs/2026-1-31/CLAUDE_INTERNALS.md
- /home/user/nataraja/docs/2026-1-31/EDITABLE-MESSAGES-PLAN.md
- /home/user/nataraja/docs/2026-2-1/memento-mori-tab.md
- /home/user/nataraja/docs/2026-2-1/subagent_anatomy.md
- /home/user/nataraja/docs/2026-2-1/system-prompt-editor.md
- /home/user/nataraja/docs/2026-2-11/CORUPT.md
- /home/user/nataraja/docs/2026-2-11/PRIME.md
- /home/user/nataraja/docs/2026-2-11/cache-break-attribution-header-fix.md
- /home/user/nataraja/docs/2026-2-11/epistemological-priming-implementation.md
- /home/user/nataraja/docs/2026-2-11/epistemological-priming-research.md
- /home/user/nataraja/docs/2026-2-11/global-cache-backend-routing-ab-fix.md
- /home/user/nataraja/docs/2026-2-11/memento-mori-v3-king-of-kings.md
- /home/user/nataraja/docs/2026-2-12/PRIME-investigation-complete.md
- /home/user/nataraja/docs/2026-2-12/cache-break-investigation-findings.md
- /home/user/nataraja/docs/2026-2-12/research-plan-cache-security.md
- /home/user/nataraja/docs/2026-2-12/session-insights-cache-security.md
- /home/user/nataraja/docs/2026-2-13/text-prime-canonical-timeline.md
- /home/user/nataraja/docs/claude-code-tools-reference.md

### B) Inspector Docs and Transcripts
- /home/user/nataraja/tools/claude-route-inspector/docs/claude-code-cache-break-study.md
- /home/user/nataraja/tools/claude-route-inspector/docs/community-findings-github-issues.md
- /home/user/nataraja/tools/claude-route-inspector/docs/live-session-monitoring.md
- /home/user/nataraja/tools/claude-route-inspector/docs/quota-investigation-summary.md
- /home/user/nataraja/tools/claude-route-inspector/docs/resume-vs-fresh-session-burn.md
- /home/user/nataraja/tools/claude-route-inspector/docs/statsig-ab-test-blocking.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-1.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-2.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-3.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-4.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-5.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-6.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-NOT-fixed.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA-fixed.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-QUOTA.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector--1.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-1.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-10.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-11.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-12.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-13.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-14.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-15.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-16.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-17.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-18.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-19.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-2.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-20.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-21.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-22.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-23.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-24.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-25-recon.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-26.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-27.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-28.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-29.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-3.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-30.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-31.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-32.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-33.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-34.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-35.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-36.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-37.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-38.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-39.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-4.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-5.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-6.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-7.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-8.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-9.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-FUCKED-1.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-FUCKED-2.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector-FUCKED.md
- /home/user/nataraja/tools/claude-route-inspector/docs/transcripts/text-claude-inspector.md

## Considered but Excluded
- /home/user/nataraja/docs/2026-02-01/COUNTER_CLASSIFIER.md (outside cache/quota/inspector evidence lane for this investigation)

## Notes on Path Interpretation
- The user-provided `claude-inspector/docs` path does not exist in this workspace.
- The active materials are under `/home/user/nataraja/tools/claude-route-inspector/docs`.
