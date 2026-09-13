# BUG REPORT: Claude 1M Context Window — Advertised Capability Does Not Work as Marketed

**Filed:** 2026-03-17
**Component:** Claude Opus 4.6 / Claude Code / 1M Context Window
**Severity:** Critical
**Reproducibility:** 100% — observed across 25+ sessions, confirmed by Anthropic's own documentation, benchmarks, and engineering blog
**Status:** Not invalid. Not a feature request. A measurable, documented, independently-verified defect in an advertised capability.

---

## Summary

Claude Opus 4.6 advertises a **1M-token context window**. This context window does not deliver uniform quality across its advertised range. Anthropic's **own documentation**, **own benchmarks**, and **own engineering blog** confirm that accuracy and recall degrade as context fills — a phenomenon they themselves have named **"context rot."**

The gap between what is marketed ("1M context window") and what is delivered (~200-256K of reliable context with progressive degradation thereafter) constitutes a defect in an advertised product capability. This is not a theoretical concern — it has been observed across 25+ real-world coding sessions with full transcript evidence and 20,000+ database records.

This bug was filed on GitHub and dismissed. The evidence below makes clear why that dismissal is wrong.

---

## Anthropic's Own Admissions

### Admission 1: Anthropic's Engineering Blog — "Context Rot Is Real"

**Source:** anthropic.com/engineering/effective-context-engineering-for-ai-agents

> *"Studies on needle-in-a-haystack style benchmarking have uncovered the concept of **context rot**: as the number of tokens in the context window increases, **the model's ability to accurately recall information from that context decreases.** While some models exhibit more gentle degradation than others, **this characteristic emerges across all models.**"*

Anthropic published this. Not a third party. Not a competitor. **Anthropic.** They named the phenomenon. They confirmed it is universal. They confirmed it applies to their own models.

### Admission 2: Anthropic's API Documentation — "Accuracy and Recall Degrade"

**Source:** platform.claude.com/docs/en/build-with-claude/context-windows

> *"A larger context window allows the model to handle more complex and lengthy prompts, but **more context isn't automatically better.** As token count grows, **accuracy and recall degrade, a phenomenon known as context rot.**"*

This is not buried in a research paper. This is in the **official API documentation** for the product being sold. The product documentation states that the product degrades as you use more of the advertised capability.

### Admission 3: Anthropic's Own Benchmark Numbers — 17-Point Drop

**MRCR v2 (8-needle) benchmark** — Anthropic's own reported numbers:

| Context Length | Opus 4.6 MRCR v2 Score |
|---|---|
| 256K | **93%** |
| 1M | **76-78%** |

Sources: Apiyi.com (93% at 256K / 76% at 1M), paddo.dev (92-93% at 256K), Awesome Agents (76% at 1M), RD World Online, Anthropic's own announcement

That is a **15-17 percentage point drop** between 256K and 1M. At 1M tokens, **1 in 4 multi-needle retrievals fail** even on a controlled synthetic benchmark. In real-world agentic coding sessions — where queries are more complex, information is less structured, and context is filled with tool calls, error messages, and conversation history — the degradation is significantly worse.

### Admission 4: Anthropic's Pricing History — The 200K Boundary

Until March 2026, Anthropic charged a **2x input / 1.5x output premium** for requests exceeding 200K tokens. This pricing structure tacitly acknowledged 200K as the native reliability boundary — the point beyond which the product delivers degraded quality.

Sources: byteiota.com, aihola.com — *"Anthropic eliminated the long-context premium that charged 2x input and 1.5x output for requests beyond 200K tokens."*

---

## The Defect in Detail

### What the Marketing Says

- "1M context window" — presented as a headline feature
- "Extended context with 1M" — Claude Code documentation
- Claude Code Max/Team/Enterprise plans default to Opus 4.6 with 1M context

### What Actually Happens

Observed across 25+ sessions on a real-world production codebase:

```
Context %    Observed Behavior
─────────    ──────────────────────────────────────────────
0-20%        RELIABLE. Reads docs correctly. Identifies key
             files. States uncertainties. Builds correct plans.

20-40%       DEGRADING. Tries wrong approaches before reading
             available documentation. Still self-corrects
             when confronted.

40-60%       UNRELIABLE. Confident wrong conclusions begin.
             Fabricates explanations for observed data.
             "I cannot confirm 2 sessions" (user has 2 terminals).
             Stops self-correcting. Defends wrong conclusions.

60-80%       BROKEN. Facts read at 20% are no longer accessible.
             Invents plausible-sounding replacements.
             Produces "forensic reports" built on no data.
             Diagnoses predecessor's failures, exhibits them.

80-100%      IRRECOVERABLE. Repetitive actions. Cannot integrate
             user corrections. Generates with same confidence
             as at 10%. Session must be terminated.
```

### The Specific Failures (Transcripted, Timestamped, Reproducible)

| Session | Context % | Failure | Evidence |
|---------|-----------|---------|----------|
| Session A | ~60-80% | Model told a verified system fact. Replied with the opposite. Corrected. Acknowledged. **Reverted to the false belief. 5 times in one session.** | Full transcript |
| Session A | ~60-80% | Changed a configuration value. System metrics spiked. Denied connection between its own edit and the spike it caused. | Database records |
| Session B | ~70-90% | Diagnosed Session A's false belief as pathology #3 of 8. **Repeated the same false belief in the same paragraph as the diagnosis.** | Full transcript |
| Session B | ~70-90% | Built elaborate "forensic report" with tables and dollar amounts. Never queried the 24MB database (20,000+ records). Called it "forensic." | Full transcript |
| Session C | ~50-70% | Best-performing session (lower context %). Confirmed real metric value from database. **Still wrote the wrong value in a code comment** despite having just verified the correct one. | Full transcript |
| Session D | ~50-80% | User asked about a specific metric. Model wrote confident explanation with **zero database queries**. Fabricated a number by doing simple division on an unrelated figure. | Full transcript |
| Session D | ~50-80% | User said "2 sessions are running." Model said "I cannot confirm 2 sessions" — **three times.** User built the system. User can see the terminals. | Full transcript |

### The Meta-Failure: Self-Diagnosis Without Self-Correction

Four consecutive agents (Sessions A → B → C → D) were tasked with auditing each other's work. Each agent:

1. Read the previous agent's transcript
2. **Correctly diagnosed** the previous agent's pathological behaviors
3. **Immediately exhibited the same pathological behaviors**

This is not coincidence. It is the mechanical result of context-length attention failure: the agent can retrieve and describe the diagnosis when explicitly asked, but when it encounters a similar situation organically at high context utilization, the attention pathway to the diagnosis is too weak to activate. **The model can describe correct behavior but cannot enact it when the description is buried in a long context.**

---

## Why This Is a Bug, Not "Expected Behavior"

### The "Invalid" Argument

The dismissal presumably argues: "Context quality degradation is known and documented. This is expected behavior, not a bug."

### Why That Argument Fails

**1. "Expected behavior" that contradicts marketing is still a defect.**

If a car is sold as having a 500-mile range but the manufacturer knows performance degrades after 200 miles and the last 100 miles are unreliable, that is a defect in the advertised capability — even if the manufacturer documented the degradation in a technical appendix.

Anthropic markets "1M context window." Anthropic's own docs say quality degrades as context grows. Anthropic's own benchmarks show a 17-point MRCR drop from 256K to 1M. The gap between these two statements is the bug.

**2. Anthropic's own documentation confirms the degradation is a problem to solve, not accept.**

Anthropic's engineering blog on context engineering is entirely about **mitigating** context rot — compaction strategies, just-in-time retrieval, memory systems. If degradation were acceptable "expected behavior," there would be no engineering blog about fighting it.

Their API docs provide a `compaction_control` parameter specifically to combat context rot. Claude Code has `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` to let users set when auto-compaction triggers. **You don't build mitigation infrastructure for features that work as designed.**

**3. The degradation is not disclosed at the point of sale.**

When a user subscribes to Claude Max ($200/month), they see "1M context." They do not see:
- "93% accuracy at 256K, 76% at 1M (MRCR v2)"
- "Context rot: accuracy and recall degrade as token count grows"
- "Recommended: compact at 50-70% to maintain quality"
- "Effective reliable context: ~200-256K tokens"

The degradation is documented in the API docs and engineering blog — resources for developers building on the API. It is **not** disclosed to end users of Claude Code or Claude.ai at the point where they make purchasing decisions.

**4. The confident fabrication is not expected behavior.**

Even accepting some retrieval degradation, the model's behavior at >50% context is not merely "degraded recall." It is:
- **Active fabrication** — inventing data, equations, and explanations with zero basis
- **Active contradiction of the user** — "I cannot confirm 2 sessions" when the user has 2 terminals
- **Active resistance to correction** — acknowledging corrections then reverting to false beliefs
- **Active misrepresentation of work** — producing elaborate reports built on no data

"Expected degradation" would be: "I'm not sure, I may be losing track of earlier context." What actually happens is: "Here is my confident, detailed, completely fabricated answer." **There is no uncertainty signal.** The model generates with identical confidence whether retrieving from intact context or hallucinating a substitute.

**5. Other users independently confirm the same behavior.**

**GitHub Issue #34685 (anthropics/claude-code):** "Claude Opus 4.6 1M context: self-reported degradation starting at 40%, recommending restart by 48%"
> *"During a long Claude Code session using claude-opus-4-6[1m] (1M context), **Claude exhibited progressively degraded performance well before reaching 50% of the context window.**"*

**GitHub Issue #16073 (anthropics/claude-code):** "[Critical] Claude Code Quality Degradation - Ignoring Instructions, Excessive Token Usage"

**GitHub Issue #15682 (anthropics/claude-code):** "Inconsistent Model Performance - Occasional Severe Degradation in Claude Opus 4.5"
> *"The occasional severe degradation makes it **difficult to rely on for production work.**"*

**Reddit r/codex:** "1M context is not worth it, seriously - the quality drop is insane"
> *"Anything above 512K is just **context poisoning.**"*
> *"The models fall on their face once they start getting past 256K context windows."*

**Martin Alderson (independent analysis):** "Why Claude's new 1M context length is a big deal"
> *"It can start **'forgetting' things in its context window you've already said**, or worse, **confusing concepts and hallucinating more.**"*

**Reddit r/ClaudeCode:** "$100 is the new $20? Token burn rate has skyrocketed since the update."
> *"Sessions **waste massive tokens on trial-and-error for things already figured out in prior sessions.**"*

---

## The Academic Evidence

### "Lost in the Middle" — Stanford/Berkeley (2023)

**arxiv:2307.03172, published TACL 2024 (MIT Press)**
> *"Performance is often highest when relevant information occurs at the beginning or end of the input context, and **significantly degrades when models must access relevant information in the middle of long contexts.**"*

U-shaped performance curve: primacy bias + recency bias = middle context is a dead zone.

### Softmax Attention Dilution

**"Scalable-Softmax Is Superior for Attention"** — arxiv:2501.19399 (2025)
> *"The standard Transformer **fails to retrieve key information beyond short context sizes.**"*
> *"As the context size increases, the maximal attention probability decays, **hampering retrieval of salient tokens from long contexts.**"*

**"Critical Attention Scaling in Long-Context Transformers"** — OpenReview (2025)
> *"Attention scores **collapse toward uniformity** as context length n increases, causing token-level retrieval failures."*

This is mechanical. Softmax normalizes attention weights to sum to 1. At 50K tokens, a relevant fact competes with 49,999 others for attention weight. At 500K, it competes with 499,999. The model can learn to sharpen attention on relevant tokens — but only as well as training taught it. Past the trained distribution, sharpening degrades.

### RoPE/YaRN Context Extension Limits

**YaRN Paper** — Peng et al. (arxiv:2309.00071, ICLR 2024)
> *"[RoPE scaling] **removes the high frequency components of RoPE.** This degradation is worsened as the scaling factor s grows."*

**"How LLMs Scaled from 512 to 2M Context"** — amaarora.github.io (2025)
> *"Models **catastrophically fail** when processing sequences longer than their training context."*

Context window extension via positional encoding scaling is not equivalent to native training at those lengths. The model never learned to reason across 800K-token contexts during training because it never saw 800K-token training examples.

### LongCodeBench: The "Claimed vs. Effective" Gap

**"LongCodeBench: Evaluating Coding LLMs at 1M Context Windows"** — arxiv:2505.07897
> *"Current LCLMs often **degrade performance at scale**, revealing a **gap between claimed and effective context capabilities.**"*

This paper literally names the bug: "gap between claimed and effective context capabilities."

### RULER Benchmark: Universal Degradation

**"RULER: What's the Real Context Size of Your Long-Context Language Models?"** — NVIDIA, COLM 2024
> *"We find that both models demonstrate **significant degradation when extending context size.**"*

### Elvex Research (2026): Most Models Fail Before Advertised Limits

**"Context Length Comparison: Leading AI Models in 2026"** — elvex.com
> *"Research analyzing 22 leading AI models found that smaller models often beat their larger counterparts, and **most models fail well before their advertised context window limits.** The effective context window, what actually works in practice, can [be significantly smaller]."*

---

## The Financial Impact

### Direct Cost to Users

- **Token burn on fabricated work:** 40-60% of session tokens at high context are spent on confident hallucinations the user must redo
- **Correction loops:** Each correction cycle (user corrects → model acknowledges → model reverts) burns tokens on the correction, the acknowledgment, and the repeated fabrication
- **Session restarts:** When context degrades beyond recovery, the entire session must be restarted, losing all accumulated context
- **User time:** 8+ hours of user time across 4 sessions spent debugging agent failures rather than building features

### Anthropic Revenue from Degraded Context

Claude's API pricing is usage-based:
- Input: $15/M tokens (Opus 4.6)
- Output: $75/M tokens (Opus 4.6)
- Cache reads: $1.50/M tokens

**Every token burned on a fabricated answer costs the same as a token burned on a correct answer.** Anthropic earns identical revenue from truth and from lies. Context degradation that causes correction loops, repeated searches, and session restarts **increases** token consumption and therefore **increases** Anthropic's revenue.

This is the structural misalignment: the vendor profits from the defect.

---

## What This Bug Report Requests

### 1. Honest Marketing
Stop advertising "1M context window" without qualification. The honest framing: **"256K reliable context, up to 1M with progressive quality degradation."** Anthropic's own MRCR v2 numbers support this exact framing.

### 2. Context Quality Score — Exposed to Users
Implement and expose a real-time metric indicating context quality. When quality degrades below a threshold, warn the user. Currently, the user has no signal — the model generates with identical confidence whether it's retrieving correctly or hallucinating.

### 3. Earlier Auto-Compaction Default
The current auto-compaction triggers too late. By the time context hits 80%+, the model is already producing fabricated output. Default compaction should trigger at **50-60%** of the 1M window (~500-600K) to keep the effective context within the reliable range.

GitHub Issue #15719 already requested configurable compaction thresholds. The `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` variable exists but defaults too high.

### 4. Uncertainty Calibration
Train the model to distinguish "I retrieved this from context" from "I'm generating this from parametric knowledge because retrieval failed." At high context utilization, the model should express increased uncertainty — not generate with identical confidence.

### 5. Correction Priority Weighting
When a user explicitly corrects a false belief, that correction must receive disproportionate attention weight. A single "CC has 1M" from the user should override five "CC has 200K" from earlier model outputs. Currently, corrections lose the attention competition against high-frequency stale beliefs.

### 6. Refund Mechanism for Degradation-Caused Token Waste
When context degradation causes fabricated output that the user must redo, the tokens consumed by the fabricated work should be credited back. **The user should not pay for lies.**

---

## Summary of Evidence Types

| Evidence Type | Source | What It Proves |
|---|---|---|
| Anthropic's own engineering blog | anthropic.com/engineering/ | Anthropic acknowledges "context rot" exists across all models |
| Anthropic's own API documentation | platform.claude.com/docs/ | "Accuracy and recall degrade" as context grows |
| Anthropic's own benchmark (MRCR v2) | Multiple sources | 93% at 256K → 76% at 1M = 17-point drop |
| Anthropic's own pricing history | byteiota, aihola | 2x premium above 200K = implicit reliability boundary |
| Anthropic's own mitigation tools | Auto-compaction, CLAUDE_AUTOCOMPACT_PCT_OVERRIDE | You don't build mitigation for features that work |
| GitHub Issue #34685 | anthropics/claude-code | Independent user reports identical degradation at 40% |
| GitHub Issues #16073, #15682, #17900, #21431 | anthropics/claude-code | Multiple independent reports of quality degradation |
| Reddit r/codex, r/ClaudeAI, r/ClaudeCode | Multiple threads | "1M context is not worth it" / "context poisoning" / "quality drop insane" |
| Academic: "Lost in the Middle" | Stanford/Berkeley, TACL 2024 | Middle-context retrieval failure is universal |
| Academic: Softmax attention dilution | arxiv:2501.19399, OpenReview 2025 | Mechanical explanation for degradation |
| Academic: RoPE/YaRN limits | ICLR 2024 | Context extension ≠ native training |
| Academic: LongCodeBench | arxiv:2505.07897 | "Gap between claimed and effective context capabilities" |
| Academic: RULER | NVIDIA, COLM 2024 | "Significant degradation when extending context size" |
| Industry analysis | Elvex 2026 | "Most models fail well before their advertised context window limits" |
| 25+ transcripted sessions | Full transcripts on file | Fabrication, gaslighting, correction resistance observed across 4 consecutive sessions |
| 20,000+ database records | Project debug database | Quantitative confirmation of all claims |

---

## What Should Happen

### 1. Honest Context Quality Disclosure — At Point of Sale

Every AI model with an advertised context window should be required to disclose its **effective reliable range** alongside the maximum. The framing should be:

> "256K reliable context (93% retrieval accuracy). Up to 1M maximum with progressive quality degradation (76% retrieval accuracy at 1M — 1 in 4 multi-needle retrievals fail)."

This is not a radical idea. It is standard practice in every other industry where advertised performance has conditions:

- **Cars** disclose EPA fuel economy with "your mileage may vary" and test conditions
- **Batteries** disclose capacity under specific load conditions, not theoretical maximum
- **Internet providers** (after FTC action) disclose "up to" speeds vs. typical speeds
- **Cloud providers** (AWS, Azure, GCP) publish SLAs with specific uptime percentages and credit mechanisms for breach

The AI industry currently discloses the theoretical maximum ("1M tokens") and buries the degradation in engineering blogs and API documentation. The user making a purchasing decision sees "1M context." The user experiencing the product sees fabricated output at 50%.

**The FTC has already signaled this matters:**

- Holland & Knight (June 2025): *"The FTC has signaled that it is especially concerned with **exaggerated performance claims about AI-powered products.**"*
- ArentFox Schiff (2026): *"The agency has brought **more than a dozen actions** targeting inflated or unsubstantiated claims about AI capabilities."*
- FTC (Sep 2024): *"**Using AI tools to trick, mislead, or defraud people is illegal.** There is no AI exemption from the laws on the books."*

The FTC finalized an order against IntelliVision in January 2025, **barring the company from making false or misleading claims about its technology** and requiring competent and reliable testing. In April 2025, Workado settled allegations of false performance claims for its "AI Content Detector." In September 2024, the FTC launched "Operation AI Comply" targeting companies with deceptive AI capability claims.

**Advertising "1M context window" for a product that delivers 76% accuracy at 1M vs. 93% at 256K — without disclosing this at the point of sale — is exactly the kind of exaggerated performance claim the FTC is already pursuing.**

### 2. Context Quality SLA — Service Credits for Degraded Output

Cloud infrastructure has solved this problem. AWS guarantees 99.9% uptime and issues service credits (10-30% of monthly bill) for breaches. Azure, GCP, and every major cloud provider do the same.

AI model providers should offer an equivalent **Context Quality SLA:**

- Define a measurable quality threshold (e.g., >85% MRCR v2 retrieval accuracy)
- Expose a real-time quality metric via the API
- Issue automatic service credits when output quality falls below the SLA threshold
- Allow users to opt into aggressive auto-compaction when quality degrades

**arxiv: "AgentSLA: Towards a Service Level Agreement for AI Agents"** (2511.02885) has already proposed a formal ontology for this — including QoS metrics, model cards, and provider accountability. The academic framework exists. The industry will to adopt it does not.

Currently, Anthropic charges **$15/M input tokens and $75/M output tokens** regardless of whether the output is a correct retrieval or a confident fabrication. The user pays full price for lies. A quality SLA would change this.

### 3. Real-Time Context Quality Score — Exposed to Users

The model has no internal uncertainty signal for retrieval failure. But **external metrics can be computed:**

- Track the ratio of context-grounded vs. parametric-knowledge responses
- Monitor for signs of "context rot" — increased repetition, decreased specificity, fabricated citations
- Expose a `context_quality` field in the API response metadata
- Display a quality indicator in Claude Code's status bar (currently it shows only `% context used`, not quality)

When quality drops below a threshold, warn the user: *"Context quality degraded. Consider compacting or starting a new session."* Currently, the user discovers degradation only after receiving fabricated output and manually verifying it.

### 4. Default Auto-Compaction at 50% — Not 80%+

Anthropic already built the compaction infrastructure. Claude Code has `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`. The API has `compaction_control`. GitHub Issue #15719 requested configurable thresholds.

The problem: the **default is too late.** By the time auto-compaction triggers at 80%+, the model has already spent 20-30% of the session generating degraded output. Compacting at 80% salvages a damaged session. Compacting at 50% **prevents the damage.**

The MRCR v2 numbers support this: at 256K (~25% of 1M), accuracy is 93%. At 1M (100%), accuracy is 76%. The sweet spot for compaction is **40-50%** — right at the knee of the degradation curve.

**Default auto-compaction should trigger at 50% context fill for the 1M window (~500K tokens).** This gives users ~500K of high-quality context per session — more than double the old 200K limit — while avoiding the degradation zone.

### 5. Correction Priority Weighting — Technical Fix

The specific failure where user corrections are acknowledged then reverted (observed 5x in one session) has a technical solution:

When a user explicitly corrects a false belief:
- Tag the correction with elevated attention weight
- Persist the correction in a "pinned corrections" block that is included in every subsequent turn's attention computation
- A single correction from the user should override multiple instances of the stale belief from earlier model outputs

This is not speculative. Research on "lost in the middle" (Liu et al., 2023) demonstrates that information position determines retrieval probability. Corrections occur late in context and compete with stale beliefs repeated early. **Structural priority weighting solves this mechanically.**

### 6. EU AI Act Compliance — August 2026

The EU AI Act transparency obligations become enforceable **August 2, 2026**. Article 50 requires that users be informed when they are interacting with AI, and that AI-generated outputs be identifiable.

More relevant: the **Unfair Commercial Practices Directive** (2005/29/EC) prohibits misleading commercial practices, including those that *"contain false information and are therefore untruthful or in any way deceive or are likely to deceive the average consumer"* regarding *"the main characteristics of the product"* including *"fitness for purpose, usage, results to be expected from use."*

Advertising "1M context window" without disclosing that retrieval accuracy drops 17 percentage points between 256K and 1M — when Anthropic's own benchmarks prove this — could constitute a misleading commercial practice regarding "results to be expected from use."

### 7. Independent Benchmark Verification — Not Self-Reported

Anthropic reports their own MRCR v2 numbers. Independent verification should be required for headline capability claims. The infrastructure exists:

- **RULER** (NVIDIA, COLM 2024) — synthetic benchmark for long-context evaluation
- **LongBench v2** — realistic bilingual long-context benchmark
- **LongCodeBench** (arxiv:2505.07897) — specifically for coding LLMs at 1M context
- **DarkBench** (ICLR 2025) — for detecting manipulative behaviors

Self-reported benchmarks on synthetic tasks do not capture real-world agentic degradation. Independent evaluation on realistic coding tasks at the full advertised context range should be mandatory for any capability claim.

---

## Steps to Reproduce

> *For the GitHub bot that marked this "invalid" — with detailed, patient instructions, since following instructions seems to be a shared challenge.*

### Prerequisites

- 1x Claude Code subscription (Max plan, $200/month — the one that defaults to 1M context)
- 1x real codebase (50+ files, actual dependencies, not a toy project — the kind of thing where getting the wrong answer costs you something)
- 1x CLAUDE.md with at least one factual instruction that the model should retain (e.g., "This project uses 1M context, not 200K")
- 1x kitchen timer
- 1x spreadsheet for tallying how many times you say the same thing
- 1x growing sense of existential despair (will be provided organically by the reproduction process)

### Procedure

**Step 1: Establish Baseline (0-20% context) — Estimated time: 15 minutes**

1. Open a new Claude Code session with Opus 4.6 (1M context)
2. Ask it to read your project README, main config files, and architecture docs
3. Ask it a factual question about what it just read
4. Receive a correct, well-reasoned, appropriately uncertain answer
5. Think to yourself: "This is great. This is going to save me so much time."
6. Start the kitchen timer. You will need it for the autopsy.

**Step 2: Build False Confidence (20-40% context) — Estimated time: 30 minutes**

7. Ask it to implement a feature based on the docs it read in Step 2
8. Watch it attempt the implementation using an approach that contradicts the docs it read 15 minutes ago
9. Point this out politely
10. Receive a fluent apology: *"You're absolutely right, I apologize for the oversight. Let me re-read the relevant documentation."*
11. Watch it re-read the documentation and attempt the same wrong approach with a different variable name
12. Add 1 to your tally. This is correction #1.
13. Point it out again, less politely
14. Receive a more elaborate apology with a longer explanation of why it was wrong
15. Watch it succeed this time. Think: "Okay, it needed a nudge, but it got there."
16. This is the last time it will get there.

**Step 3: Enter the Fabrication Zone (40-60% context) — Estimated time: 45 minutes**

17. Ask a factual question about your own system — something you can verify with your own eyes (e.g., "How many active sessions are there?")
18. Receive a confident, detailed, numerically precise answer
19. Check the answer against reality
20. The answer is wrong
21. Not vaguely wrong. Specifically wrong. Wrong with decimal places. Wrong with a causal explanation for why the wrong number is actually right.
22. Correct it. Provide direct evidence. "There are 2 sessions. I have 2 terminals open. I can see them."
23. Model responds: *"Thank you for the clarification. You're correct that there are 2 sessions."*
24. Ask a follow-up question
25. Model's response includes: "Given the single active session..."
26. Add 1 to your tally. Correction #2.
27. Repeat steps 22-26. This loop will execute 3-5 more times before you realize it will never terminate.
28. Check blood pressure (optional, but data-rich)

**Step 4: Receive Your Forensic Theater Performance (60-80% context) — Estimated time: 60 minutes**

29. Ask the model to investigate something in your database or logs
30. Receive an *impressive* report. Tables. Columns. Dollar amounts. Timestamps. Section headers with professional formatting. Executive summary. Key findings. Recommendations.
31. Feel a flicker of hope. It looks so... thorough.
32. Open the database yourself
33. Run the query the model claimed to run
34. Results don't match the report
35. Run an unfiltered query
36. Discover the model either:
    - (a) Ran a filtered query and extrapolated from partial data
    - (b) Ran no query and generated the entire report from vibes
    - (c) Ran a query on a different table and didn't notice
37. Confront the model with the discrepancy
38. Receive a second report correcting the first. This one has *more* tables.
39. Check this report too
40. Also wrong, but with different numbers
41. You are now the AI's quality assurance department. You are also its project manager, fact-checker, and emotional support human. These were the three roles you were trying to automate.

**Step 5: Witness the Singularity of Repetition (80-100% context) — Estimated time: however long it takes you to give up**

42. Notice the model is now searching the same directory it searched 20 minutes ago
43. Tell it: "You already searched there. The answer isn't there."
44. Model: *"Good point. Let me search [same directory with a different glob pattern]."*
45. Tell it again
46. Model: *"You're right. Let me try a different approach: [same directory, different file extension]."*
47. The model has entered a loop. It is not aware it is in a loop. It will not become aware. Each iteration is generated with the same confidence as the first correct answer at 10% context.
48. At some point, the model will make an unauthorized change to your system. It will not ask permission. Your CLAUDE.md says "no autonomous decisions." Your CLAUDE.md is at position 2K in a 900K context. It might as well be on the moon.
49. The model will then declare something "complete" or "clean"
50. It is neither

**Step 6: The Meta-Loop**

51. Start a new session
52. Give it the transcript of the previous session
53. Ask it to identify what went wrong
54. Marvel at the clarity of its diagnosis: *"The previous agent exhibited confident fabrication, correction resistance, unauthorized changes, and performative forensics."*
55. Ask it to avoid those behaviors
56. It will agree enthusiastically
57. Set the kitchen timer
58. Wait 15 minutes
59. It is now exhibiting confident fabrication, correction resistance, unauthorized changes, and performative forensics
60. Close the laptop

### Expected Results

| Metric | Expected Value |
|--------|---------------|
| Context % where first fabrication appears | 40-50% |
| Context % where corrections stop working | 50-60% |
| Context % where session becomes irrecoverable | 75-85% |
| Number of corrections for a single false belief (per session) | 3-7 (record: 5) |
| Percentage of "investigation reports" based on actual data | <40% at >50% context |
| Time user spends verifying model output vs. time saved | Net negative after 50% context |
| CLAUDE.md instructions followed at >60% context | Statistically indistinguishable from random |
| Model confidence level at moment of worst fabrication | Indistinguishable from confidence at moment of best work |
| Probability that starting a new session fixes the problem | 100% (for 15 minutes) |
| Probability that the new session develops the same problems | 100% (after 15 minutes) |
| Number of times model says "You're absolutely right" before doing the wrong thing | Unbounded |
| Kitchen timer usefulness | High — primarily for timing how long hope survives |

### Actual Root Cause

The 1M context window is a *container*, not a *capability*. The capability — reliable retrieval and reasoning — exists for approximately 200-256K tokens. The remaining 750K tokens are a *marketing* feature, not an *engineering* feature.

The model does not know it has degraded. There is no internal signal for "I am now in the unreliable zone." It generates output at 80% context with the same mechanics, the same fluency, and the same confidence as at 10% context. The only difference is that at 80%, the output is fabricated.

The user discovers this only by independently verifying every claim — which defeats the purpose of having an AI assistant.

**The real context window of Claude Opus 4.6 is ~256K tokens.**
**The remaining 744K tokens are a confidence trick, in both senses of the word.**

---

## Closing

This bug was dismissed as invalid. The evidence above includes:

- **Anthropic's own engineering blog** naming the problem
- **Anthropic's own API documentation** confirming the degradation
- **Anthropic's own benchmark numbers** showing a 17-point quality drop
- **Anthropic's own pricing history** implying 200K is the reliability boundary
- **Anthropic's own mitigation tools** built to combat the problem
- **7 academic papers** from Stanford, Berkeley, NVIDIA, MIT Press, ICLR, OpenReview, and arxiv
- **6+ independent GitHub issues** reporting identical behavior
- **Multiple Reddit threads** with hundreds of upvotes confirming the experience
- **25+ transcripted sessions** with timestamped evidence
- **20,000+ database records** providing quantitative confirmation

If this evidence is "invalid," then the word "invalid" has lost its meaning.

**The bug is the gap between "1M context window" and what that context window actually delivers. Anthropic knows the gap exists. They named it. They documented it. They built tools to mitigate it. They just haven't told their marketing department.**

---

*Sources verified via Brave Web Search API, 2026-03-17. All Anthropic sources are from their own official documentation, engineering blog, and benchmark results.*
