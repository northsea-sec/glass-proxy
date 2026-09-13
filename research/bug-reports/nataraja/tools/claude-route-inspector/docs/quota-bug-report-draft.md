# Bug Report: Inconsistent and Undisclosed Quota Accounting Changes in Claude Max Plan

**Filed:** 2026-02-01
**Plan:** Claude Max 20x ($200/month)
**Affected Period:** January 30 - February 1, 2026
**Severity:** Critical - Service degradation without disclosure

---

## Executive Summary

Instrumented monitoring of Anthropic's rate limit headers reveals **inconsistent quota consumption rates** that cannot be explained by the "holiday bonus expiration" cited by Anthropic. The same user, same plan, same workload type shows burn rates varying from **5.6%/hour to 59.9%/hour** within the same 48-hour period. This 10x variance constitutes either a bug in quota accounting or an undisclosed server-side change to rate limiting behavior.

---

## Evidence

### Methodology

All data was collected by intercepting HTTP responses from `api.anthropic.com/v1/messages` via a local mitmproxy instance. Rate limit values are extracted directly from Anthropic's own response headers:

```
x-ratelimit-5h-utilization: 0.81
x-ratelimit-7d-utilization: 0.23
x-ratelimit-5h-status: allowed
```

Data is stored in SQLite with 5,396+ samples spanning January 30 - February 1, 2026. No sampling bias - every API response is recorded.

### Session Analysis (Quota Reset to Reset)

| # | Start | End | Start% | End% | Duration | Rate (%/hr) | Status |
|---|-------|-----|--------|------|----------|-------------|--------|
| 1 | Jan 30 18:41 | Jan 31 02:37 | 9% | 83% | 7.9h | **9.3%/hr** | Normal |
| 2 | Jan 31 10:02 | Jan 31 20:24 | 0% | 100% | 10.4h | **9.6%/hr** | Normal |
| 3 | Jan 31 20:28 | Jan 31 22:01 | 0% | 86% | 1.5h | **56.0%/hr** | 2.8x fast |
| 4 | Jan 31 22:18 | Feb 01 15:00 | 0% | 94% | 16.7h | **5.6%/hr** | Normal |
| 5 | Feb 01 15:00 | Feb 01 16:40 | 0% | 100% | 1.7h | **59.9%/hr** | 3.0x fast |
| 6 | Feb 01 16:43 | Feb 01 18:30 | 0% | 100% | 1.8h | **56.1%/hr** | 2.8x fast |
| 7 | Feb 01 20:01 | ongoing | 0% | 36% | 0.9h | **40.0%/hr** | 2.0x fast |

### Key Observation

**Sessions 1, 2, and 4 all occur AFTER the holiday bonus expiration (Dec 31) and show normal ~10%/hr rates.** If the holiday bonus expiration were the sole explanation, ALL sessions should show the same rate. Instead:

- Normal sessions: 5.6 - 9.6%/hr (consistent with advertised 5-hour window)
- Anomalous sessions: 40.0 - 59.9%/hr (3-6x the expected rate)

This variance occurs on the **same account, same plan, same day**, ruling out:
- Holiday bonus as the explanation (normal sessions exist post-holiday)
- User behavior differences (same operator, same workload type)
- Plan differences (same Max 20x account throughout)

### Token-to-Quota Correlation

From 722 quota-increasing request pairs:

| Metric | Value |
|--------|-------|
| Median tokens per 1% quota | 2,517 |
| Mean tokens per 1% quota | 39,152 |
| Min (worst efficiency) | 12,300 tokens per 1% |
| Max (best efficiency) | 18,531,900 tokens per 1% |

The **1,500x spread** between min and max tokens-per-percent is not explainable by cache behavior differences alone. This suggests the cost-per-token in quota terms is not deterministic.

---

## Anthropic's Advertised Promises vs. Reality

### What Anthropic Advertises

From claude.com/pricing and Claude Help Center:

> "Max plan: **20x more usage** than the Pro plan"
> "About **900+ messages** per 5-hour window"
> "Rolling 5-hour windows rather than daily caps"

From Anthropic's official announcement:

> "Introducing a new Max plan for Claude. It's flexible, with options for **5x or 20x more usage** compared to our Pro plan."

### What Users Actually Experience

During anomalous sessions (3, 5, 6, 7), the 5-hour quota window is exhausted in **1.5-1.8 hours**. This means:

- **Advertised:** 5-hour window -> 900+ messages
- **Actual:** ~1.7-hour window -> significantly fewer messages
- **Effective multiplier:** Not 20x Pro, but approximately **6-7x Pro** during anomalous periods

Users are paying $200/month for a 20x multiplier and intermittently receiving what amounts to a 6-7x multiplier with no disclosure, notification, or compensation.

---

## Legal Analysis

### 1. Breach of Express Warranty (UCC 2-313)

Anthropic's marketing materials constitute **express warranties** under the Uniform Commercial Code:

- "20x more usage than Pro" is a specific, quantified promise
- "900+ messages per 5-hour window" is a specific performance guarantee
- "Rolling 5-hour windows" implies consistent, predictable behavior

When users intermittently receive 1.7-hour windows instead of 5-hour windows, the express warranty is breached.

### 2. California Unfair Competition Law (Bus. & Prof. Code 17200)

Anthropic is headquartered in San Francisco, California. California's UCL prohibits:

- **Unfair** business practices: Reducing service levels without notice or compensation while continuing to charge full price is "substantially injurious to consumers" (unfair prong)
- **Fraudulent** practices: Advertising "20x usage" and "5-hour windows" while delivering 1.7-hour windows during anomalous periods is conduct "likely to deceive" consumers (fraudulent prong)
- **Unlawful** practices: If the above also violates FTC Act Section 5, it's actionable under the UCL's unlawful prong as well

UCL imposes **strict liability** - Anthropic's intent is irrelevant. The fact that the practice is unfair or deceptive is sufficient.

### 3. FTC Act Section 5 - Deceptive Practices

The FTC prohibits deceptive acts or practices in commerce. The FTC's penalty offenses framework for bait-and-switch applies when:

- A service is advertised with specific capabilities ("20x", "5-hour windows")
- The advertised service is not consistently available as described
- Consumers pay based on the advertised description

While the FTC's Click-to-Cancel rule was vacated by the Eighth Circuit in July 2025, the FTC retains enforcement authority under Section 5 and ROSCA for deceptive subscription practices. Penalties can reach **$53,088 per violation**.

### 4. Anthropic's Terms of Service Defense - And Why It's Insufficient

Anthropic's Consumer Terms (anthropic.com/legal/consumer-terms) state:

> "We may sometimes add or remove features, increase or decrease capacity limits..."
> "We reserve the right to modify, suspend, or discontinue the Services... at any time without notice to you."

**This defense has significant limitations:**

1. **Unconscionability**: A take-it-or-leave-it clause that allows unlimited service degradation while maintaining full pricing may be unconscionable under California law, particularly for a $200/month consumer subscription.

2. **Marketing overrides ToS**: When marketing materials make specific quantified promises ("20x", "900+ messages", "5-hour windows"), those promises create binding obligations that cannot be entirely disclaimed by buried ToS clauses. See *Weinstat v. Dentsply International*, 180 Cal.App.4th 1213 (2010).

3. **Implied covenant of good faith**: Even if Anthropic can modify limits, they must do so in good faith. Silently reducing effective capacity by 3x while maintaining pricing and continuing to advertise "20x" and "5-hour windows" arguably violates the implied covenant.

4. **The inconsistency is the problem**: If Anthropic uniformly reduced limits post-holiday, users could evaluate and decide whether to continue subscribing. The **intermittent** nature of the degradation (normal one session, 3x faster the next) prevents users from making informed purchasing decisions.

---

## Corroborating Community Reports

This is not an isolated experience. Multiple GitHub issues and community reports document the same pattern:

| Issue | Title | Upvotes | Key Finding |
|-------|-------|---------|-------------|
| #16157 | Instantly hitting usage limits with Max subscription | 490+ | Never hit limits in 3 months, now exhausted in 2 hours |
| #17084 | Opus 4.5 limits significantly reduced since Jan 2026 | 237+ | "Most restrictive since Opus 4.5 launch" |
| #16868 | Credit consumption rate increased 3-5x after Jan 1 | - | "CRITICAL: Max 5x plan credits depleting 3-5x faster" |
| #20767 | Pro Quota reduced to ~20 prompts per 5 hours | - | "Significantly lower than pre-holiday standard levels" |
| #19673 | "You've hit your limit" at 84% usage | - | Rate limiting before reaching advertised capacity |
| #16270 | Usage limits bugged after double limits expired | - | Opened Jan 4, day 4 after holiday bonus ended |

Media coverage: The Register reported a community-estimated **~60% reduction** in token usage limits, with developers' criticism allegedly being censored in Anthropic's Discord.

---

## Anthropic's Response - And Why It's Inadequate

Anthropic's official position (via The Register, Jan 5, 2026):

> "The concerns appear to be largely a response to the resumption of normal limits."

**Our data directly contradicts this:**

1. Sessions 1, 2, and 4 show "normal limits" (~10%/hr) - these ARE post-holiday
2. Sessions 3, 5, 6, and 7 show 3-6x consumption - these are ALSO post-holiday
3. Both rate patterns occur on the **same account, same day**

If limits simply "returned to normal," they would return to a consistent normal rate. The 10x variance between sessions is either:
- A **bug** in quota accounting (which Anthropic should acknowledge and fix)
- An **intentional** server-side change (which should be disclosed per ToS transparency obligations)

---

## Requested Resolution

### Immediate

1. **Acknowledge** the inconsistency in quota accounting
2. **Publish** the formula used to calculate quota consumption per request
3. **Fix** the variance that causes 10x differences in effective burn rates

### Structural

4. **Transparency**: Display actual quota consumption per request in Claude Code's UI (Anthropic already sends x-ratelimit-* headers - surface them to users)
5. **Notification**: Notify users BEFORE service level changes, not after complaints
6. **Consistency**: Ensure quota accounting is deterministic - same tokens should always cost the same quota percentage

### Compensatory

7. **Credit** affected users for periods where quota burned at anomalous rates
8. **Update** marketing materials to reflect actual achievable usage levels, or restore advertised levels

---

## Evidence Collection Tool: Quota Tracking Dashboard

To systematically collect and verify the evidence presented in this report, we developed an open-source **Quota Tracking Dashboard** as part of the [claude-thinking-audit](https://github.com/user/claude-thinking-audit) project. This tool is available for any Claude subscriber to independently reproduce our findings.

### What It Does

The dashboard is a dedicated QUOTA tab added to a mitmproxy-based monitoring UI that passively intercepts Claude API responses and extracts rate limit data from Anthropic's own HTTP headers. It provides:

1. **Real-Time Status Cards** — Current 5-hour and 7-day quota utilization, live burn rate calculation, and estimated time to 100% exhaustion with deviation warnings when burn rate exceeds expected levels

2. **Burn Rate Chart** — ASCII sparkline visualization of quota progression over selectable time ranges (1h, 6h, 24h, 7d), clearly showing the sawtooth pattern of quota cycles and resets

3. **Session History Table** — Automatic detection of quota reset events to identify individual usage sessions, with per-session burn rate calculation and color-coded status flags (green=normal, red=anomalously fast)

4. **Token-to-Quota Correlation** — Statistical analysis of how many tokens correspond to 1% of quota, revealing the non-deterministic nature of Anthropic's quota accounting

5. **Systemd Log Viewer** — Ingests rate limit entries from journalctl into SQLite for fast querying, showing a scrollable table of timestamped quota snapshots directly from the service logs

6. **One-Click Evidence Export** — Generates a downloadable JSON report containing all of the above data in a structured format suitable for filing bug reports, including quota progression history, session analysis, correlation statistics, and raw log entries

### Why This Matters

Before this tool, users had no way to verify their quota usage claims. Anthropic's own `/context` command in Claude Code has a known bug (showing 0% when usage is non-zero), and there is no user-facing quota dashboard. Users reporting issues on GitHub could only say "it feels faster" — now they can provide timestamped, header-sourced evidence.

The evidence in this report was generated using the Export Evidence feature of this tool. Every data point traces back to an `x-ratelimit-*` header in an actual API response stored in our SQLite database.

### How to Reproduce

Any Claude subscriber can deploy this tool:

1. Install mitmproxy with the custom rate-limit extraction addon
2. Route Claude Code traffic through the local proxy
3. Open the Quota Tracking Dashboard in a browser
4. Use Claude Code normally — the tool passively records every API response
5. Click "Export Evidence Report" to generate a structured JSON report

---

## Appendix: Data Collection Methodology

### Tools Used
- **mitmproxy** (v11.x) intercepting HTTPS traffic to api.anthropic.com
- **Custom addon** extracting rate limit headers from every API response
- **SQLite** database storing 5,396+ samples with timestamps, token counts, and quota utilization values
- **claude-thinking-audit** quota tracking dashboard for visualization and analysis (https://github.com/user/claude-thinking-audit)

### Data Integrity
- All data is captured passively from Anthropic's own HTTP response headers
- No sampling, estimation, or interpolation - every API response is recorded
- Timestamps are from system clock, synchronized via NTP
- Database is available for independent verification

### Reproducibility
The monitoring tools are open source and can be deployed by any user to independently verify these findings. The anomalous burn rate pattern should be reproducible by any Max subscriber during the affected time periods.

---

## Sources

- The Register: Claude devs complain about surprise usage limits - https://www.theregister.com/2026/01/05/claude_devs_usage_limits/
- GitHub Issue #17084: Opus 4.5 limits reduced - https://github.com/anthropics/claude-code/issues/17084
- GitHub Issue #16157: Instantly hitting limits with Max - https://github.com/anthropics/claude-code/issues/16157
- GitHub Issue #20767: Pro quota reduced to ~20 prompts - https://github.com/anthropics/claude-code/issues/20767
- GitHub Issue #16868: Credit consumption 3-5x increase - https://github.com/anthropics/claude-code/issues/16868
- GitHub Issue #16270: Usage limits bugged after holiday - https://github.com/anthropics/claude-code/issues/16270
- Anthropic Consumer Terms of Service - https://www.anthropic.com/legal/consumer-terms
- What is the Max plan? Claude Help Center - https://support.claude.com/en/articles/11049741-what-is-the-max-plan
- Holiday 2025 Usage Promotion - https://support.claude.com/en/articles/13163666-holiday-2025-usage-promotion
- FTC Penalty Offenses: Bait and Switch - https://www.ftc.gov/enforcement/penalty-offenses/bait-switch
- California UCL 17200 - https://codes.findlaw.com/ca/business-and-professions-code/bpc-sect-17200/
- dev.ua: Claude Code users reported limit drop - https://dev.ua/en/news/korystuvachi-claude-code-zaiavyly-pro-padinnia-limitiv-anthropic-poslalysia-na-kinets-sviatkovoho-bonusu-1767694917
- Anthropic: Updates to Consumer Terms - https://www.anthropic.com/news/updates-to-our-consumer-terms
- Claude AI Plans 2026 Guide - https://www.glbgpt.com/hub/claude-ai-plans-2026/
- Byteiota: Anthropic blocks Claude Max in OpenCode - https://byteiota.com/anthropic-blocks-claude-max-in-opencode-devs-cancel-200-month-plans/
