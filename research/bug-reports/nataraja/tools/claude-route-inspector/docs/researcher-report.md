# Claude API Behavior Audit: Call for Independent Verification

**Repository**: [claude-thinking-audit](https://github.com/argosdevo-svg/claude-thinking-audit)
**Data**: 8,152 API samples across 63 sessions (Jan 23 - Feb 1, 2026)
**Method**: Passive HTTPS interception via mitmproxy

---

## Summary

We instrumented Claude Code's API traffic to measure what Anthropic actually delivers vs. what users request and pay for. Our findings suggest significant discrepancies between advertised and delivered service levels. We seek independent verification and contributions from the research community.

---

## Key Findings

### 1. Thinking Budget Throttling (0.77% Delivery)

| Metric | Requested | Delivered | Rate |
|--------|-----------|-----------|------|
| Total thinking tokens | 470M | 3.6M | **0.77%** |
| Per-request (32k budget) | 31,999 | ~450 | **1.4%** |
| Interleaved (200k budget) | 200,000 | ~380 | **0.19%** |

Timing fingerprints confirm the model IS Opus (variance coefficient matches baseline), but thinking utilization is ~80% below expected.

### 2. Silent Model Substitution (99% Haiku Delegation)

When users request Opus, Claude Code delegates to Haiku subagents:

| Session | Subagent Calls | Haiku % |
|---------|----------------|---------|
| A | 898 | 99.8% |
| B | 681 | 65% |
| C | 1,376 | 99.9% |

### 3. Quota Accounting Inconsistency (10x Variance)

Same account, same plan, same day shows burn rates from 5.6%/hr to 59.9%/hr:

| Session | Duration | Rate | Status |
|---------|----------|------|--------|
| 1 | 7.9h | 9.3%/hr | Normal |
| 3 | 1.5h | 56.0%/hr | 2.8x fast |
| 5 | 1.7h | 59.9%/hr | 3.0x fast |

Token-to-quota correlation shows 1,500x spread (2,517 to 18.5M tokens per 1%).

### 4. Quantization Detection

ITT fingerprinting suggests INT8 quantization:
- ITT Ratio: 0.76x baseline (24% faster)
- Variance Ratio: 1.27x baseline (27% more variable)
- Detection confidence: 57%

### 5. Context Window Bloat

24% of 200k context consumed before first user message:
- System prompt + tool schemas: ~17k tokens
- MCP tool schemas: ~18k tokens
- Git/env context: ~13k tokens
- **Total baseline: ~48k tokens**

---

## Methodology

**Data Collection**: mitmproxy addon intercepts HTTPS traffic to `api.anthropic.com`. Every API response is parsed and stored in SQLite with:
- Full token counts (input, output, cache_read, cache_create, thinking)
- Inter-token timing (ITT) for backend fingerprinting
- Rate limit headers (undocumented, 12 fields)
- Model identification and subagent delegation tracking

**Zero Modification**: Read-only interception. No request tampering unless user enables enforcement features.

**Storage**: `~/.claude/fingerprint.db` — all samples queryable via SQL.

---

## Reproducibility

```bash
git clone https://github.com/argosdevo-svg/claude-thinking-audit.git
cd claude-thinking-audit && ./setup.sh
mitmdump -s mitm_itt_addon.py -p 18888
# In another terminal:
export HTTPS_PROXY=http://127.0.0.1:18888
claude
```

Compare your `fingerprint.db` against our published findings.

---

## Verification Requests

We specifically seek confirmation of:

1. **Thinking utilization < 10%** — Is sub-10% delivery reproducible across different accounts/regions?
2. **Haiku delegation ratio** — Do other users see 99%+ Haiku in subagent calls?
3. **Quota burn variance** — Can others reproduce 3-6x burn rate anomalies?
4. **Quantization signatures** — Does ITT < 0.85x baseline correlate with perceived quality degradation?

---

## Contribution Areas

| Area | What's Needed |
|------|---------------|
| **Statistical analysis** | Rigorous hypothesis testing on our SQLite dataset |
| **Regional comparison** | Data from non-US regions (EU, APAC) |
| **Longitudinal tracking** | Multi-week continuous monitoring |
| **Quality metrics** | Correlation between ITT signatures and output quality |
| **Legal review** | ToS/consumer protection analysis for different jurisdictions |

---

## Related Issues & Reports

| Issue | Title | Upvotes |
|-------|-------|---------|
| [#22435](https://github.com/anthropics/claude-code/issues/22435) | Quota accounting inconsistency (our report) | NEW |
| [#20350](https://github.com/anthropics/claude-code/issues/20350) | Thinking budget 10% delivery (our report) | 150+ |
| [#16157](https://github.com/anthropics/claude-code/issues/16157) | Instantly hitting usage limits | 490+ |
| [#17084](https://github.com/anthropics/claude-code/issues/17084) | Opus 4.5 limits reduced | 237+ |

---

## Contact

- **GitHub Issues**: [claude-thinking-audit/issues](https://github.com/argosdevo-svg/claude-thinking-audit/issues)
- **Data Access**: SQLite schema and sample queries in repository README

We welcome PRs, issue reports, and independent verification attempts. All findings are based on Anthropic's own API response headers and timing data — no estimation or interpolation.
