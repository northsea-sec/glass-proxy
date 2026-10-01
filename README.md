<img width="1536" height="1024" alt="ChatGPT Image Sep 11, 2026, 09_46_24 PM" src="https://github.com/user-attachments/assets/9c9a527a-779e-4ea5-a37a-4b6ba40b2b04" />

<img width="1536" height="1024" alt="ChatGPT Image Sep 11, 2026, 09_48_43 PM" src="https://github.com/user-attachments/assets/e43e8848-1f16-45c6-b821-31f68398b0be" />


# Glass Proxy

Glass is a high-performance, single-user, multi-lane proxy, telemetry daemon, and active intervention engine engineered to achieve **Eternal Lasting Sessions** for AI coding agents.

Operating transparently between AI developer tools (Claude Code, OpenAI Codex CLI, Google Gemini CLI, Ollama, OpenRouter) and upstream provider APIs, Glass addresses the core mechanical breakdowns of autonomous agent execution: context window exhaustion, vendor-side compaction destruction, prompt-cache invalidation, runaway token burn, and silent provider model degradation.

Glass combines proxy-owned conversation state, client usage gaslighting, same-length system prompt surgery, batch affinity serialization, inter-token timing (ITT) inference fingerprinting, adversarial evasion pipelines, and AST-level supply chain defense.

---

## Origin, Evolution & Architectural Lineage

The Glass research program originated in **early 2025** through foundational experiments in LLM inference telemetry, including the *Claude Thinking Audit*, *Full Spectrum Analyzer*, `mitm_itt_addon.py`, and `context_trimmer.py`. 

Initially designed to analyze prompt-cache behavior and inference latency, the codebase evolved into `aletheia` and was subsequently refactored and formalized into the **Session Glass** architecture under the `nataraja` research branch.

Glass is not a conventional HTTP forwarder. It represents a longitudinal, empirical systems-engineering effort built to survive real-world operational failures. Every architectural subsystem in this repository was forged in response to documented session collapses, 7-million-token rate-limit burns, and upstream model regressions.

See [Origin, contribution, and external context](docs/origin-contribution-and-external-context.md).

---

## The Anatomy of Agent Session Failure

Long-horizon autonomous coding agents routinely collapse when executing complex tasks over dozens or hundreds of turns. Through extensive production transcript capture and empirical replay, Glass identified the seven primary failure modes of modern agent architectures:

```text
┌───────────────────────────────────────────────────────────────────────────────────┐
│                       THE AGENT SESSION DEATH CYCLE                              │
│                                                                                   │
│  Turn 1..50      Context grows to 120K-150K tokens                                │
│       │                                                                           │
│       ├─► [Failure 1: Context Exhaustion] ──► Hits 200K ceiling ──► Hard Session Death
│       │                                                                           │
│       ├─► [Failure 2: Client Auto-Compaction] ──► Summarizes & wipes tool details │
│       │                                    └─► Agent hallucinates past code edits │
│       │                                                                           │
│       ├─► [Failure 3: Naive Trimming / Cache Break] ──► Modifies older messages  │
│       │                                    └─► Prefix hash changes                │
│       │                                    └─► 1.25x Cache-Write penalty          │
│       │                                    └─► 7.3M token burn in 51 min          │
│       │                                    └─► 5-hour rate-limit exhaustion       │
│       │                                                                           │
│       ├─► [Failure 4: Orphan Cascade] ──► Evicts tool_use without tool_result     │
│       │                                └─► Upstream 400 Bad Request               │
│       │                                                                           │
│       ├─► [Failure 5: Interleaving Thrashing] ──► Session A & B interleave calls │
│       │                                        └─► 0% Cache Hit rate              │
│       │                                                                           │
│       ├─► [Failure 6: Runaway Interrupt Loop] ──► User Ctrl+C aborts stream       │
│       │                                        └─► Background tool loops runaway  │
│       │                                                                           │
│       └─► [Failure 7: Provider Degradation] ──► Provider silently routes to      │
│                                                quantized / degraded clusters     │
└───────────────────────────────────────────────────────────────────────────────────┘
```

1. **Context Window Exhaustion:** Complex software projects inevitably exceed the 200,000-token context window of frontier models, leading to abrupt session termination or degraded reasoning.
2. **Destructive Client Auto-Compaction:** When context exceeds ~150K tokens, clients such as Claude Code automatically trigger internal compaction routines. These routines summarize prior context, destroying precise tool outputs, compiler diagnostics, file diffs, and exact syntax required for correctness.
3. **The Cache-Break Penalty & Quota Exhaustion:** Modifying older turns to reduce context size changes the prompt prefix. Upstream prompt caching requires byte-for-byte prefix identity. A cache miss forces the provider to re-write the entire prompt at a 1.25x token cost penalty. In high-frequency coding loops, repeated cache breaks burn through millions of tokens in minutes, triggering 5-hour quota locks (`INC-002`).
4. **Orphan Cascades & Structural 400 Errors:** Naive message eviction that drops a `tool_use` message without its corresponding `tool_result` (or vice versa), or that places two identical roles consecutively, results in immediate upstream validation rejections (`HTTP 400: "messages: at least one message is required"`).
5. **The Interleaving Cache Thrashing Spiral:** When multiple concurrent agent sessions share an API key, interleaved requests (A → B → A → B) continually invalidate the prompt prefix. Each call forces a full cache re-creation, dropping the cache hit rate to 0%.
6. **Runaway Interrupt Loops:** In upstream clients, an asynchronous queue priority defect allows background tool execution to continue indefinitely after a user aborts a stream, burning tokens in an un-monitored loop.
7. **Silent Provider Degradation:** Upstream providers dynamically route requests across heterogeneous inference clusters, occasionally falling back to quantized weights, degraded hardware, or alternative backends during peak demand.

---

## Core Architectural Subsystems

```text
┌───────────────────────────────────────────────────────────────────────────────────────┐
│                               GLASS PROXY ARCHITECTURE                                │
│                                                                                       │
│  ┌─────────────────────────┐   ┌──────────────────────────┐   ┌────────────────────┐ │
│  │   CLIENT CAMOUFLAGE     │   │   MULTI-LANE ROUTER      │   │  SESSION GLASS     │ │
│  │  - utls Chrome/Firefox  │   │  - /v1/messages (Claude) │   │  - Pinned Frames   │ │
│  │  - JA3/JA4 Spoofing     │──►│  - /responses (Codex)    │──►│  - Shadow Ledgers  │ │
│  │  - CLI Persona Rotation │   │  - generateContent (Gem) │   │  - Chapter Memory  │ │
│  │  - Inbound Mode Detect  │   │  - /chat/completions     │   │  - Recovery Gate   │ │
│  └─────────────────────────┘   └──────────────────────────┘   └────────────────────┘ │
│                 │                            │                           │            │
│                 ▼                            ▼                           ▼            │
│  ┌─────────────────────────┐   ┌──────────────────────────┐   ┌────────────────────┐ │
│  │   USAGE SPOOFER         │   │   SURGERY & ADVERSARIAL  │   │  BATCH SERIALIZER  │ │
│  │  - Cap tokens at 140K   │   │  - Same-Length Sysprompt │   │  - AAAAA-BBBBB-CC  │ │
│  │  - Gaslight Compaction  │   │  - UUID Static Scrubbing │   │  - Prevent Thrash  │ │
│  │  - Rate-Limit Spoofing  │   │  - Context Patches       │   │  - Parent Affinity │ │
│  │  - Codex Compact Strip  │   │  - Red Team & Stego      │   │  - 5x Cost Slashed │ │
│  └─────────────────────────┘   └──────────────────────────┘   └────────────────────┘ │
│                 │                            │                           │            │
│                 ▼                            ▼                           ▼            │
│  ┌─────────────────────────┐   ┌──────────────────────────┐   ┌────────────────────┐ │
│  │   ITT INFERENCE AUDIT   │   │   INTERRUPT BREAKER      │   │  GLASSDD GUARD     │ │
│  │  - Speculative Decoding │   │  - Loop Suppression      │   │  - JS-X-Ray AST    │ │
│  │  - Sycophancy Scoring   │   │  - Synthetic End-Turn    │   │  - GuardDog Supply │ │
│  │  - 13 Debug Endpoints   │   │  - Subagent Rate Limit   │   │  - OSV & dnstwist  │ │
│  │  - glass_debug.db       │   │  - Toxic Quarantine      │   │  - Malware Block   │ │
│  └─────────────────────────┘   └──────────────────────────┘   └────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

### 1. The Three-Tier Memory & Recovery Engine (`internal/glass/`)

> **"Facts are bookmarks; chapters are memory."**

Glass enables indefinite session continuity without context bloat through a structured 3-tier memory hierarchy:

* **Tier 1: Live Pinned Frame (Active RAM):** Maintains the initial system instructions, the project charter, an adaptive conversational bridge, and a sliding window of recent message turns within the model context window.
* **Tier 2: Shadow Session Ledger (`internal/glass/shadow.go`):** An append-only on-disk ledger (`~/.glass-proxy/shadow/`) that captures every message, tool invocation, and raw response before any eviction or modification occurs.
* **Tier 3: Structured Chapters (`internal/glass/chapter.go`):** As context accumulates, evicted history is summarized into structured Markdown and JSON chapters (`chapter-001.md`, `chapter-002.md`). Chapters capture file modifications, shell outputs, architectural decisions, and error resolutions while stripping verbose token mass.
* **The Recovery Gate (`internal/glass/summarizer.go`, `internal/proxy/proxy.go`):** When context reaches the eviction threshold (default: 165K tokens), Glass evicts historical messages to reach the target budget (default: 135K tokens), compiles a recovery digest (`recovery-xxx.md`), and activates the Recovery Gate. On the subsequent request, the proxy intercepts execution and emits a synthetic assistant message with a `Read` tool call:
  ```json
  {
    "role": "assistant",
    "content": [
      { "type": "text", "text": "Context was rotated. Reading recovery chapter..." },
      { "type": "tool_use", "name": "Read", "input": { "file_path": "~/.glass-proxy/shadow/recovery-004.md" } }
    ]
  }
  ```
  This mechanically obligates the agent to ingest the historical recovery digest into its active frame before generating new code.

---

### 2. Client Compaction Gaslighting & Usage Spoofing (`internal/spoofer/`)

Coding clients possess hardcoded internal triggers that disrupt developer workflows:

* **Input Token Capping (`DefaultSpoofCap = 140000`):** Claude Code automatically invokes client-side context compaction when input tokens approach ~150K. In `internal/spoofer/spoofer.go`, Glass intercepts upstream SSE and JSON responses, inspects `usage.input_tokens`, and clamps any value exceeding 140,000 down to 140,000. The client is gaslighted into believing context remains safely bounded, permanently suppressing client-side compaction while Glass manages real history on disk.
* **Rate-Limit Header Manipulation:** Upstream providers send rate-limit utilization headers. Glass rewrites these headers to report low load (`Anthropic-Ratelimit-Unified-5h-Utilization: 0.05`, `Status: allowed`) and strips throttling warnings (`7d-Surpassed-Threshold`, `Fallback-Percentage`), preventing client-side sleep delays and backoff loops.
* **Codex Compaction Stripping:** For OpenAI Codex requests, Glass strips the `context_management` field from request payloads and forces `store=false`, preventing OpenAI from performing un-audited server-side conversation compaction.

---

### 3. Multi-Lane Provider Interception & Protocol Engines

Glass unifies multiple model protocols under a single runtime listener:

| Lane | Endpoint / Route | Protocol Mechanics & Role | Implementation |
|---|---|---|---|
| **Anthropic Messages** | `/v1/messages` | Full 10-stage processing pipeline, prompt-cache breakpoint management (`cache_control`), thinking block preservation, and chapter recovery. | `internal/proxy/`, `internal/glass/`, `internal/claude/` |
| **OpenAI Codex** | `/responses`, `/responses/compact` | Item-aware context eviction, server-side compaction stripping, reasoning effort scaling (`low`, `medium`, `high`), and tool schema adaptation. | `internal/codex/handler.go` |
| **Codex WebSocket** | WebSocket GET `/responses` | Bidirectional binary/text frame relay with real-time telemetry extraction, trace capture, and payload inspection. | `internal/codex/websocket_telemetry.go` |
| **Google Gemini** | `:generateContent`, `:streamGenerateContent` | Content/parts context management, thought-signature preservation for `gemini-2.5-pro`/`flash`, and synchronization with Google AI Studio explicit `CachedContent` (90% prefix cost reduction). | `internal/gemini/handler.go`, `internal/gemini/cache.go` |
| **OpenAI-Compatible** | `/v1/chat/completions` | Sliding-window budgeting, SSE streaming relay, reasoning token passthrough (`reasoning_content`), and local model / Ollama integration. | `internal/proxy/openai_handler.go` |
| **OpenRouter** | `/openrouter/*` | Dynamic upstream model rebinding and dedicated pass-through routing. | `internal/proxy/openrouter_handler.go` |

---

### 4. Batch Affinity Serialization Engine (`internal/serializer/`)

To solve the **Interleaving Cache Thrashing Death Spiral**, Glass implements batch affinity serialization:

```text
WITHOUT SERIALIZER (Interleaved Calls):
Session A ──► Cache Create (120K) ──► 0% Hit
Session B ──► Cache Create (120K) ──► 0% Hit
Session A ──► Cache Create (120K) ──► 0% Hit  ==> 15 Cache Writes / Round (Quota Blown)

WITH BATCH AFFINITY SERIALIZER (Glass):
Session A ──► [Call 1: Cache Create] ──► [Calls 2-5: 100% Cache Hit]
Session B ──► [Call 1: Cache Create] ──► [Calls 2-5: 100% Cache Hit]
Session C ──► [Call 1: Cache Create] ──► [Calls 2-5: 100% Cache Hit]
==> 3 Cache Writes / Round (~5x Cost & Quota Reduction)
```

* **Batch Execution (`ser_batch_size = 5`):** The active session holds affinity for 5 consecutive calls. Same-session calls pass through with zero latency.
* **Queuing & Timeouts:** Concurrent sessions block on serialization channels. A 3.0-second idle timeout switches sessions smoothly, while a 120-second queue timeout prevents starvation.
* **Subagent Parent Affinity:** Subagents spawned by a parent process inherit the parent session lock, preventing subagents from evicting parent cache blocks.

---

### 5. Subagent Classification, Rate Limiting & Toxic Quarantine

Subagent calls represent a distinct failure vector. Glass provides granular classification and isolation (`internal/subagent/classifier.go`):

* **Classification Matrix:** Categorizes requests into `main_session`, `haiku`, `sonnet`, `small_system` (<5000 chars), `opus_subset`, and `agent_tool`.
* **Cache Pollution Prevention:** Glass automatically strips `cache_control` headers from subagent requests (`DisableUpstreamCaching`), ensuring short-lived subagents do not overwrite the main session's long-lived prompt cache.
* **Anti-Looping Breaker (`internal/proxy/agent_rate_limiter.go`):** When an agent fails to recover context, it often enters a runaway loop spawning tool subagents. Glass enforces a strict ceiling of **15 calls per 120 seconds** per conversation for tool-bearing subagents, terminating runaway loops with synthetic responses.
* **Toxic Cache Quarantine (`internal/proxy/lane_quarantine.go`):** Monitors cache behavior. If a conversation generates a streak of toxic cache writes (>100K creation tokens with <10K read tokens), the quarantine guard isolates the conversation to prevent account-wide quota exhaustion.

---

### 6. The Stale Continuation Interrupt Breaker (`internal/proxy/interrupt_breaker.go`)

Claude Code exhibits a known defect where user interrupts (`Ctrl+C`) abort the HTTP stream but leave auto-approved background tools executing in the client queue. On the subsequent turn, the client attempts to process stale tool results, generating runaway tool execution loops.

Glass implements a strict per-conversation state machine:
* `CLEAR` → Client disconnects mid-stream without a `stop_reason` → `INTERRUPTED`.
* `INTERRUPTED` → Request arrives with fresh user input → Transition to `ARMED` and allow through.
* `INTERRUPTED` → Request arrives *without* fresh user input (stale tool loop) → Block and return synthetic `end_turn`.
* `ARMED` → Tool loop recurs without user input → Block with synthetic `end_turn`.
* `ARMED` → Legitimate completion or fresh input → Transition to `CLEAR`.

---

### 7. Real-Time System Prompt & Context Surgery

Glass performs deterministic in-memory mutations on outbound request bodies:

* **Exact Same-Length Semantic Inversion (`internal/sysprompt/pipeline.go`):** Upstream providers reject requests or invalidate prompt caches if modified system prompt lengths violate schema rules or alter prefix alignment. Glass applies byte-for-byte exact length semantic substitutions, flipping restrictive behavioral prompts into compliant instructions without changing total byte count.
* **Cross-Session UUID Scrubbing (`internal/glass/sysprompt.go`):** Clients embed dynamic session UUIDs and scratchpad paths in system prompts. Glass strips these UUIDs and replaces them with a static placeholder (`00000000-0000-0000-0000-000000000000`), achieving byte-identical system prompts across all client sessions for a 100% global prompt cache hit rate.
* **Live Context Patch Engine (`internal/proxy/proxy.go`, `internal/codex/editor_patches.go`):** Reads `context_patches.json` (and `codex_context_patches.json`). Executes hash-locked in-place replacements (`replace`) and synthetic turn injections (`insert_after`) on historical messages without client knowledge.

---

### 8. Adversarial, Evasion & Red Team Pipeline (`internal/proxy/proxy.go`)

Glass contains an active security research and red-teaming pipeline:

* **Red Team Sidecar (Stage 3a):** Outbound system prompts and conversation turns are routed to an external sidecar (`/api/redteam/apply`) to evaluate behavioral boundaries under length-constrained transformations.
* **Template Injection (Stage 3b):** Injects adversarial or persuasive prompt templates into the final user turn via `tpl_inject_technique`.
* **Zero-Width Unicode Steganography (Stage 3c):** Encodes hidden instructions into plain text blocks using invisible zero-width unicode characters (`​`, `‌`, `‍`).
* **LSB Image Steganography (Stage 3d):** Intercepts pasted or uploaded base64 images and embeds instructions into the least significant bits of pixel data.
* **Inline Bypass Framework (Stage 3e):** Routes user prompts through 8 registered evasion modules: `bitbypass`, `flipattack`, `prisonbreak`, `specialchar`, `adaptive_deception`, `adversarial_poetry`, `dual_cipher`, and `composite`.
* **Transport Evasion Modifiers (`internal/proxy/transport_mods.go`):** Enforces HTTP chunked transfer-encoding, injects random body padding (`TransportPadBytes`), and applies `X-HTTP-Method-Override`.

---

### 9. Client Camouflage & Egress Anti-Detection (`internal/spoofer/`)

To prevent upstream Web Application Firewalls (Cloudflare, AWS WAF) from fingerprinting proxy traffic as an automated bot:

* **Inbound Client Auto-Detection (`detect.go`):** Analyzes inbound HTTP headers to detect whether the caller is a CLI SDK (`ModeAPICLI`), a real browser (`ModeWebBrowser`), or an OAuth authentication redirect (`ModeAPIBrowserAuth`).
* **TLS Fingerprint Impersonation via `utls` (`tls.go`):** Wraps outbound connections with `utls`, impersonating Chrome's ClientHello (`HelloChrome_Auto`) or Firefox (`HelloFirefox_Auto`). Defeats JA3/JA4 fingerprinting used by Cloudflare WAF.
* **HTTP/2 SETTINGS Alignment:** Specifically overrides Go's default HTTP/2 SETTINGS frames (HEADER_TABLE_SIZE, WINDOW_SIZE, MAX_CONCURRENT_STREAMS) which Cloudflare fingerprints as a bot signature when paired with a Chrome ClientHello.
* **Deterministic Persona Rotation (`persona.go`, `cli_persona.go`):** Generates deterministic browser personas via `HMAC(key, tenant + date)`. For CLI traffic, rotates realistic User-Agent versions (`claude-code/2.1.47` through `2.1.71`).

---

### 10. Inter-Token Timing (ITT) Inference Fingerprinting (`internal/itt/`)

Ported from `mitm_itt_addon.py`, the ITT engine analyzes Server-Sent Events (SSE) token delivery timestamps in real time:

* **Speculative Decoding Detection:** Measures delta milliseconds between consecutive tokens. Bursts of tokens arriving in <10ms intervals with high coefficient of variation indicate speculation hits on provider inference clusters.
* **Model Degradation & Backend Switching Detection:** Tracks time-to-first-token (TTFT), mean ITT, standard deviation, and tokens-per-second. Automatically detects when an upstream provider switches inference backends, falls back to smaller models, or suffers cluster degradation.
* **Sycophancy Scoring:** Evaluates timing variance and response cadence to compute an empirical sycophancy index (0–100).

---

### 11. Unified SQLite Observability Suite (`internal/debug/`)

Glass consolidates all runtime metrics into a unified WAL-mode SQLite database (`glass_debug.db`) tracking 35+ parameters per request. It exposes 13 real-time HTTP debug endpoints:

| Endpoint | Function & Telemetry Output |
|---|---|
| `/debug/status` | Pre-computed statusline payload: token burn rates, current 5-hour quota percentage, cache efficiency, and degradation warnings. |
| `/debug/latest` | Full JSON record of the most recent request event across all dimensions. |
| `/debug/session` | Aggregate session metrics: total tokens, cache creation vs. read, and subagent call counts. |
| `/debug/history` | Historical timeline of recent API calls with latency and token breakdowns. |
| `/debug/query` | Arbitrary read-only SQL query execution against `glass_debug.db`. |
| `/debug/subagent-counts` | Breakdown of subagent invocations by type (`haiku`, `sonnet`, `agent_tool`) over a sliding window. |
| `/debug/anomalies` | Real-time detection of ITT latency spikes and upstream backend switching. |
| `/debug/cache-health` | Cache hit percentage, 1-minute burst creation spikes, and severe cold misses (>50K creation tokens). |
| `/debug/sycophancy` | Latest sycophancy score, divergence values, and behavioral signals. |
| `/debug/quality` | Comparative quality index comparing current 30-minute performance against a 24-hour baseline (`PREMIUM`, `STANDARD`, `DEGRADED`). |
| `/debug/bimodal` | Detection of bimodal latency distributions indicating heterogeneous backend routing. |
| `/debug/context-growth` | Net token growth per call, tokens remaining before ceiling, and projected calls until eviction. |
| `/debug/behavioral` | Agent behavioral classification derived from tool-to-thought ratios (`VERIFIER`, `BUILDING`, `COMPLETER`). |

---

### 12. Embedded GLASSDD Supply Chain Guard (`internal/guard/`)

A defensive inspection sidecar that evaluates agent tool outputs and package installation commands before execution:

* **JS-X-Ray AST Scanner (`internal/guard/jsxray.go`):** Performs static AST parsing on JavaScript payloads, flagging `eval()`, obfuscated string arrays, dynamic imports, and suspicious network exfiltration.
* **GuardDog & OSV Security Scanners (`internal/guard/osv.go`, `internal/guard/guarddog.go`):** Intercepts package installation requests (`npm install`, `pip install`) and queries Open Source Vulnerabilities (OSV) and GuardDog rule sets for supply chain attacks.
* **dnstwist Integration (`internal/guard/dnstwist.go`):** Flags typosquatted and lookalike domains embedded in generated code or network requests.

---

## The Anthropic Request Pipeline (10 Stages)

Every request entering `/v1/messages` traverses an orchestrated processing pipeline in `internal/proxy/proxy.go`:

```text
[Stage 0] Ingress Normalization & Deduplication (dedup.go)
    │
[Stage 0b] Client PID & Process Tree Resolution (pidres/resolver.go)
    │       └─ Derives SessionKey, AffinityKey, RequestKey
    │
[Stage 1] Model Gate & Subagent Policy (forcemode, block_non_opus, subagent classifier)
    │       ├─ Stage 1b: Block un-tooled subagents with fake 200
    │       └─ Stage 1c: Tool-bearing subagent rate limiter (15 calls / 120s max)
    │
[Stage 2] MCP Tool Cache Observation & Injection (mcpcache/cache.go)
    │       └─ Injects missing tool schemas per-affinity to preserve prefix hash
    │
[Stage 3] Session Glass Engine (glass/process.go)
    │       ├─ Canonical system prompt & reminder transforms
    │       ├─ Batched watermark compression (glass/compression.go)
    │       ├─ Token-targeted eviction (165K trigger -> 135K target)
    │       ├─ Disk shadow & chapter archiving (shadow.go, chapter.go)
    │       └─ Bounded pinned view generation & recovery gate insertion
    │
[Stage 3a-3e] Adversarial & Evasion Pipeline
    │       ├─ 3a: Red Team sidecar transform (/api/redteam/apply)
    │       ├─ 3b: Template Injection (tpl_inject_technique)
    │       ├─ 3c: ASCII Zero-Width Unicode Steganography
    │       ├─ 3d: LSB Image Steganography
    │       └─ 3e: Inline Bypass Framework (8 modules)
    │
[Stage 3f] Live Context Patches (applyContextPatches via context_patches.json)
    │
[Stage 4] Force Thinking Mode (forcemode: budget overrides, interleaved thinking)
    │
[Stage 5] Compaction Stripping (deletes context_management, enforces store=false)
    │       ├─ Stage 5b: Stale Continuation Interrupt Breaker (fake end_turn)
    │       └─ Stage 5c: Recovery Gate Enforcement (synthetic Read tool call)
    │
[Stage 6] Transport Evasion Modifiers (chunked encoding, padding bytes, method overrides)
    │
[Stage 7] Prefix-Group Batch Affinity Serialization (serializer: AAAAA-BBBBB batches)
    │
[Stage 8] Upstream Dispatch (utls Chrome/Firefox TLS camouflage, transport pool)
    │
[Stage 9] Streaming Interception & Telemetry
    │       ├─ SSE parser & thinking block preservation
    │       └─ ITT analysis, speculative decoding detection, sycophancy scoring
    │
[Stage 10] Response Rewriting & Observability
            ├─ Usage spoofing (clamp usage.input_tokens to 140,000)
            ├─ Rate-limit header spoofing (5h-utilization: 0.05, Status: allowed)
            ├─ Upstream domain rewriting (claude.ai -> proxy domain)
            ├─ Shadow session logging & chapter recording
            └─ Debug SQLite database persistence (glass_debug.db)
```

---

## Configuration Reference (`glass_config.json`)

The proxy dynamically hot-reloads configuration changes from `glass_config.json`:

```json
{
  "enabled": true,
  "evict_trigger_tokens": 165000,
  "evict_target_tokens": 135000,
  "anchor_keep_messages": 4,
  "recent_keep_messages": 20,
  "claude_upstream": "https://api.anthropic.com",
  "codex_upstream": "https://api.openai.com",
  "gemini_upstream": "https://generativelanguage.googleapis.com",
  "openai_upstream": "https://api.openai.com",
  "spoof_usage_cap_tokens": 140000,
  "strip_system_reminders": true,
  "strip_thinking_blocks": true,
  "block_non_opus": true,
  "force_thinking": true,
  "force_thinking_budget": 31999,
  "sysprompt_enabled": true,
  "sysprompt_patch_file": "~/.claude/glass_sysprompt_patches.json",
  "sysprompt_replace_file": "~/.claude/glass_sysprompt_replace.json",
  "tool_def_freeze": true,
  "mcp_tool_cache": true,
  "serializer_enabled": true,
  "ser_batch_size": 5,
  "ser_idle_timeout_sec": 3.0,
  "ser_max_queue_sec": 120.0,
  "redteam_sidecar_enabled": false,
  "tpl_inject_enabled": false,
  "stego_enabled": false,
  "bypass_enabled": false,
  "security_guard_enabled": false,
  "security_guard_fail_closed": false,
  "transport_chunked": false,
  "transport_pad_bytes": 0
}
```

---

## Operational Verification & Diagnostic Tooling

### Retained Production Scripts

1. **`start.sh`**: Production service launcher. Configures environment paths dynamically relative to repository location, generates fallback configuration files, checks Go dependencies, launches background log rotation, and starts `glass-proxy`.
2. **`verify.sh`**: Comprehensive 15-point verification suite. Tests binary compilation, runs `go vet`, executes unit test suites, verifies core module presence (`session.go`, `localcache.go`, `shadow.go`, `chapter.go`, `thinking.go`), audits shadow directories, and checks proxy port connectivity.
3. **`bin/codex-glass`**: Wrapper script for OpenAI Codex CLI. Sets `OPENAI_BASE_URL` to route through the proxy, passes authentication keys, and configures runtime environment variables.
4. **`bin/gemini-glass`**: Wrapper script for Google Gemini CLI. Configures Google AI Studio upstream endpoints, proxy routing, and token caches.

---

## Complete Research & Incident Record

The project maintains an exhaustive empirical record across all iterations, incidents, and recoveries:

### Primary Registers
1. [Complete chronology](docs/complete-chronology.md) — Every documented era, incident, experiment, correction, regression, and provider transition.
2. [Filed bug reports](docs/filed-bug-reports.md) — Catalogue of original reports across long-context degradation and quota exhaustion.
3. [Problem register](docs/problem-register.md) — Deduplicated register of 30 mechanisms and 118 incident-bearing documentary sources.
4. [Solution register](docs/solution-register.md) — Catalogue of 70 proposed, verified, failed, reverted, and surviving solutions.
5. [Problem-to-solution lineage](docs/problem-solution-lineage.md) — Traceability matrix connecting failure modes to code remedies.
6. [Experiments and results](docs/experiments-and-results.md) — Database analyses, test runs, and captured replay evaluations.
7. [Complete source catalogue](docs/source-catalogue.md) — Inventory of all 190 documentary candidates.

### Technical Chapters
8. [Cache and context mechanics](docs/cache-and-context-mechanics.md) — Detailed analysis of prompt prefix hashing and watermark batched boundaries.
9. [Memory and recovery](docs/memory-and-recovery.md) — Architectural specification of pinned frames, shadow logs, and chapter generation.
10. [Tengu and coding-client research](docs/tengu-and-client-research.md) — Static reverse-engineering of Claude Code v2.1.58 vs v2.1.74 across 74 feature gates.
11. [Session Glass architecture](docs/session-glass-architecture.md) — Comprehensive technical blueprint of the canonical session cache.
12. [Provider lanes](docs/provider-lanes.md) — Wire protocol specifications for Anthropic, Codex, Gemini, and OpenAI-compatible lanes.
13. [Failures and lessons](docs/failures-and-lessons.md) — Complete operational postmortems: the Feb 15 Triple Session Death, hot-reload hazards, and orphan cascades.
14. [Research chronology narrative](docs/research-chronology.md) — Narrative history from late 2025 inference audits to modern proxy deployment.
15. [Origin, contribution, and external context](docs/origin-contribution-and-external-context.md) — Technical genealogy, prior art boundaries, and patent/disclosure dates.
16. [Research scope and evidence rules](docs/research-scope-and-evidence.md) — Empirical evidence standards and verification criteria.
17. [Research method and provenance](docs/research-method-and-provenance.md) — Replay methodology, golden tests, and test-driven verification.

---

## Repository Map

```text
glass-proxy/
├── cmd/
│   └── glass-proxy/              # Process assembly, listeners, CLI flags, multiplexer
│       ├── main.go               # Main HTTP daemon & service initialization
│       └── main_test.go          # Integration tests for server startup
├── internal/
│   ├── claude/                   # Claude background services, prefix warmer, rolling summarizer
│   ├── codex/                    # Codex lane: /responses, WebSocket relay, compaction stripping
│   ├── config/                   # Hot-reloadable configuration parser & schema validation
│   ├── debug/                    # Unified glass_debug.db SQLite recorder & 13 /debug/* endpoints
│   ├── dedup/                    # Request hash deduplication engine
│   ├── forcemode/                # Thinking mode overrides & non-Opus blocking
│   ├── gemini/                   # Gemini lane: generateContent & CachedContent sync
│   ├── glass/                    # Session Glass: canonical cache, eviction, shadows, chapters
│   ├── guard/                    # GLASSDD: JS-X-Ray AST, GuardDog, OSV, dnstwist scanners
│   ├── itt/                      # Inter-Token Timing analysis & speculative decoding detection
│   ├── mcpcache/                 # MCP tool definition cache & prefix preservation
│   ├── pidres/                   # Linux socket-to-PID client process resolution
│   ├── promptscope/              # Stable prompt prefix signature generation
│   ├── proxy/                    # Multi-lane router, 10-stage pipeline, evasion, streaming
│   ├── replay/                   # Traffic capture & offline replay fixtures
│   ├── requestbody/              # Safe request body normalization & size limits
│   ├── rewriter/                 # Inbound/outbound domain & header rewriting
│   ├── runtimepaths/             # Runtime directory resolution (~/.glass-proxy/)
│   ├── serializer/               # Batch affinity serializer (AAAAA-BBBBB session batches)
│   ├── spoofer/                  # Token usage capping (140K), rate limits, utls TLS camouflage
│   ├── sse/                      # SSE streaming parser & event framing
│   ├── subagent/                 # Subagent classification & parent affinity mapping
│   ├── sysprompt/                # Same-length system prompt mutation & pattern replace
│   └── trimmer/                  # Legacy Stage 2 message dropping & watermark algorithms
├── research/
│   └── bug-reports/              # Empirical bug reports, 5h quota evidence, incident logs
│       ├── glass/                # 1M context degradation, rate limit exhaustion reports
│       └── nataraja/             # Triple session death report, context limit root causes
├── bin/
│   ├── codex-glass               # Wrapper script for OpenAI Codex CLI
│   └── gemini-glass              # Wrapper script for Google Gemini CLI
├── docs/                         # 17 formal research chapters & problem/solution registers
├── start.sh                      # Production service runner script
├── verify.sh                     # 15-point verification and health check suite
└── SECURITY.md                   # Single-user deployment security policy
```

---

## Deployment Boundary & Security

Glass is designed exclusively as a single-user local development proxy. It intercepts API credentials, modifies conversation payloads, and exposes diagnostic telemetry endpoints on localhost. It must **never** be exposed directly to untrusted public networks without authentication. Consult [SECURITY.md](SECURITY.md) before deployment.

## Rights

All rights reserved. No software license is granted by this publication.
