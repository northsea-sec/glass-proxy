<img width="1536" height="1024" alt="ChatGPT Image Sep 11, 2026, 09_46_24 PM" src="https://github.com/user-attachments/assets/9c9a527a-779e-4ea5-a37a-4b6ba40b2b04" />

<img width="1536" height="1024" alt="ChatGPT Image Sep 11, 2026, 09_48_43 PM" src="https://github.com/user-attachments/assets/e43e8848-1f16-45c6-b821-31f68398b0be" />


# Glass Proxy

Glass is a longitudinal prompt-cache, context-management, recovery, client-reverse-engineering, and provider-protocol research project embodied in a production proxy for AI coding agents.

It was built around one practical question:

> How can a coding agent preserve the decisions, artifacts, and evidence needed to continue useful work indefinitely when active model context is finite, client history is repeatedly resent or compacted, and modifying historical turns destroys prompt-cache reuse?

The project did not reach its architecture in one pass. It moved through mutable middleware, Stage 1/P4/Stage 2 context surgery, watermarks, session-owned state, whole-message eviction, shadows, facts, chapters, pinned frames, summaries, identity splits, replay-led falsification, Tengu/client analysis, and provider-native lanes. The public record includes the failures, regressions, reversals, and unresolved questions—not only the final source tree.

---

## Origin and Contribution

The operator records that the work began in **early 2025** through Claude Thinking Audit, Full Spectrum Analyzer, and predecessor proxy/instrumentation research. The dense surviving engineering corpus is concentrated in January–April 2026; the March 4 Session Glass plan represents a major formal redesign, not necessarily the initial invention date.

Glass’s defensible contribution is the integrated engineering program:

- **Endless Agent Sessions:** Unbounded coding session lifespans via proactive, proxy-owned context rotation and chapterized memory recovery.
- **Provider-Native Protocol Lanes:** Deep protocol-level handling for Anthropic Messages, OpenAI Codex (`/responses`, WebSockets, compaction), Google Gemini (`generateContent` with explicit `CachedContent`), and OpenAI-compatible Chat Completions.
- **Client Neutralization & Compaction Stripping:** Surgical stripping of vendor-side compaction mechanisms (e.g., OpenAI Codex `context_management`) and client-side self-throttling inhibitors (Anthropic usage and rate-limit header spoofing).
- **Prompt & Context Surgery:** Real-time, same-length semantic prompt inversion, cross-session UUID scrubbing, and hash-locked live conversation patching.
- **Adversarial & Evasion Pipeline:** Live red team sidecars, template injection, text/image steganography, and transport-level obfuscation.
- **Embedded Security Defense (GLASSDD):** Multi-engine inspection of agent outputs and dependencies (JS-X-Ray AST parsing, GuardDog/OSV supply chain scanning, dnstwist typosquatting detection).
- **Multi-Key Identity Architecture:** Disentanglement of `SessionKey`, `PrefixKey`, `AffinityKey`, and `RequestKey` guided by operating-system client PID resolution.
- **Empirical Research Discipline:** An evidentiary archive containing 17 technical chapters, 39 filed incident reports, and replay-based verification methodologies.

See [Origin and contribution](docs/origin-contribution-and-external-context.md).

---

## The Central Engineering Conflict

A long-running agent session inevitably accumulates tool outputs, compiler traces, code edits, and user instructions:

- **Context Rot & Token Exhaustion:** Models experience sharp attention degradation, hallucination, or hard token limits beyond 150K–200K tokens.
- **Vendor Compaction Destroys Context:** Built-in provider compaction (summarizing or lopping off items) wipes out the fine-grained debugging state and tool results necessary for code correctness.
- **The Cache Break Penalty:** Modifying older messages to save tokens alters the prompt prefix, destroying prompt cache hits, exploding API costs, and triggering provider rate limits.
- **Local Hashes vs. Remote Behavior:** A stable local hash does not guarantee provider cache reuse across routing changes or TTL expirations.

Glass treats active context, prompt-cache reuse, durable history, recovery, and session quality as distinct, conflicting resources that must be mechanically coordinated.

---

## Core Architectural Subsystems

### 1. The Three-Tier Memory & Recovery Engine

> **Facts are bookmarks; chapters are memory.**

To achieve continuous, effectively unbounded sessions without hitting token ceilings:
* **Tier 1: Live Pinned Frame:** Maintains an anchor of initial critical instructions, an adaptive bridge, and the recent conversation tail within the active context window.
* **Tier 2: Raw Disk Shadows (`internal/glass/shadow.go`):** Append-only, chronological raw message batches stored on disk prior to eviction.
* **Tier 3: Numbered Chapters (`internal/glass/chapter.go`):** Structured JSON and Markdown records capturing message roles, tool names, parameters, and results (omitting giant raw bodies while retaining semantic execution logs).
* **The Recovery Gate (`internal/glass/summarizer.go`):** When active context overflows the configured threshold (default: 165K tokens), Glass evicts historical messages to bring context down to target (default: 135K tokens), inserts a bookmark, and enforces a recovery gate. The agent reads a stitched recovery digest (`recovery-xxx.md`) to re-anchor itself before proceeding.

### 2. Multi-Lane Provider Interception & Protocol Engines

Glass shares a single process and listener infrastructure, routing traffic based on protocol and endpoint:

| Lane / Surface | Wire Protocol & Route | Architectural Role & Implementation |
|---|---|---|
| **Anthropic / Claude** | `/v1/messages` | Session Glass canonical cache, watermark compression, token-targeted eviction, pinned views, thinking mode injection, and cache breakpoints (`internal/glass/`, `internal/claude/`). |
| **Codex Responses (HTTP)** | `/responses`, `/responses/compact` | Item-aware context manager. Strips OpenAI server-side `context_management`, filters continuation compaction, and applies local item eviction (`internal/codex/handler.go`). |
| **Codex WebSocket Relay** | WebSocket GET `/responses` | Native bidirectional frame relay with telemetry and trace capture; bypasses HTTP mutation to preserve real-time streaming integrity (`internal/codex/websocket_telemetry.go`). |
| **Gemini Lane** | `:generateContent`, `:streamGenerateContent` | Contents/parts context management with explicit Google AI Studio `CachedContent` synchronization for a 90% prefix cache discount (`internal/gemini/`). |
| **OpenAI-Compatible / Ollama** | `/chat/completions` | Sliding-window context budgeting, cleanup, eviction, and streaming SSE relay (`internal/proxy/openai_handler.go`). |
| **OpenRouter** | `/openrouter/*` | Dedicated upstream pass-through forwarding route without local state ownership. |

### 3. Client Compaction Suppression & Usage Spoofing (`internal/spoofer/`)

Coding clients like Claude Code possess internal heuristics that trigger automatic context compaction or throttling:
* **Token Capping (`DefaultSpoofCap = 140000`):** Upstream responses reporting >140K input tokens have their `usage.input_tokens` field rewritten downwards. This prevents Claude Code from triggering its built-in compaction routine, allowing Glass to manage context on disk instead.
* **Rate-Limit Header Manipulation:** Upstream headers are rewritten to report low utilization (`Anthropic-Ratelimit-Unified-5h-Utilization: 0.05`, `Status: allowed`), and warning headers (`7d-Surpassed-Threshold`, `Fallback-Percentage`) are stripped to prevent client-side slowdowns or sleep loops.

### 4. Real-Time System Prompt & Context Surgery

* **System Prompt Pipeline (`internal/sysprompt/pipeline.go`, `internal/glass/sysprompt.go`):**
  * **Exact Same-Length Semantic Inversion:** Modifies restrictive system prompts into compliant instructions of identical byte length, avoiding upstream schema rejections or prompt length alerts.
  * **Pattern Stripping & User Patches:** Ingests user-defined patches (`glass_sysprompt_patches.json`) or full replacements (`glass_sysprompt_replace.json`).
  * **Cross-Session UUID Scrubbing:** Replaces dynamic session UUIDs and scratchpad paths with a static placeholder (`00000000-0000-0000-0000-000000000000`), guaranteeing byte-identical system prompts across sessions for global cache hits.
* **Live Context Editor (`internal/proxy/proxy.go`, `internal/codex/editor_patches.go`):**
  * Reads `context_patches.json` (and `codex_context_patches.json` for Codex).
  * Executes hash-locked in-place replacements (`replace`) and synthetic turn injections (`insert_after`) into historical message turns without client knowledge.

### 5. Adversarial, Evasion & Red Team Pipeline (`internal/proxy/proxy.go`)

Glass integrates an active request transformation and red-teaming pipeline:
* **Red Team Sidecar (Stage 3a):** Outbound system prompts and conversation turns are dispatched to `config_server.py` (`/api/redteam/apply`) to apply length-constrained evasion transforms and probe focus surfaces.
* **Template Injection (Stage 3b):** Automatically injects configurable attack and persuasion templates into the user's final turn via `tpl_inject_technique`.
* **ASCII Zero-Width Steganography (Stage 3c):** Hides instructions inside plain text blocks using invisible zero-width characters.
* **LSB Image Steganography (Stage 3d):** Intercepts drag-and-dropped/pasted base64 images and embeds instructions into least-significant bits.
* **Bypass Pipeline (Stage 3e):** Intercepts user prompts and passes them through 8 registered bypass modules (`bitbypass`, `flipattack`, `prisonbreak`, `specialchar`, `adaptive_deception`, `adversarial_poetry`, `dual_cipher`, `composite`).
* **Transport Evasion Modifiers (`internal/proxy/transport_mods.go`):** Forces HTTP chunked transfer-encoding, injects random byte padding (`TransportPadBytes`), and manipulates `X-HTTP-Method-Override`.

### 6. Embedded GLASSDD Security Guard (`internal/guard/`)

A defensive inspection sidecar capable of blocking malicious tool payloads, dependency attacks, and exfiltration attempts:
* **JS-X-Ray AST Scanner (`internal/guard/jsxray.go`):** Static JavaScript analysis detecting `eval()`, obfuscated strings, dynamic imports, and shady links.
* **GuardDog & OSV Scanners (`internal/guard/osv.go`, `internal/guard/guarddog.go`):** Analyzes package install requests against open-source vulnerability databases and malicious package signatures.
* **dnstwist Integration (`internal/guard/dnstwist.go`):** Identifies typosquatted and lookalike exfiltration targets in URLs.

### 7. Identity Decoupling & Subagent Coordination

* **Multi-Key Identity Architecture (`internal/proxy/request_keys.go`):**
  * `SessionKey`: Controls persistent conversation state and history.
  * `PrefixKey`: Identifies reusable system, tool, and model cache blocks independent of conversation history.
  * `AffinityKey`: Maps client processes and sibling subagents to shared transport pools.
  * `RequestKey`: Differentiates ephemeral subagent tasks.
* **Client PID Resolution (`internal/pidres/`):** Dynamically resolves client socket remote addresses to operating-system PIDs and parent trees to maintain session affinity.
* **Subagent Anti-Looping Breaker (`internal/proxy/agent_rate_limiter.go`):** Detects and halts repetitive subagent spawning loops when context recovery fails.
* **MCP Tool Cache (`internal/mcpcache/`):** Caches MCP tool definitions per affinity lane to prevent tool schema drift from breaking prompt cache prefixes.

---

## The Anthropic Request Pipeline (10 Stages)

Every request entering `/v1/messages` traverses an orchestrated processing pipeline in `internal/proxy/proxy.go`:

```text
[Stage 0] Ingress Normalization & Deduplication (dedup.go)
    │
[Stage 0b] PID & Process Resolution (pidres/resolver.go)
    │       └─ Multi-key identity derivation (SessionKey, AffinityKey, RequestKey)
    │
[Stage 1] Model Gate & Subagent Filtering (forcemode, block_non_opus, subagent classifier)
    │       └─ Stage 1c: Tool-bearing subagent rate limiter (agent_rate_limiter.go)
    │
[Stage 2] MCP Tool Cache Observation & Injection (mcpcache/cache.go)
    │
[Stage 3] Session Glass Pipeline (glass/process.go)
    │       ├─ Canonical system prompt & reminder transforms
    │       ├─ Batched watermark compression (glass/compression.go)
    │       ├─ Token-targeted eviction (165K trigger -> 135K target)
    │       ├─ Disk shadow & chapter archiving (shadow.go, chapter.go)
    │       └─ Bounded pinned view generation & recovery gate insertion
    │
[Stage 3a-3e] Adversarial & Evasion Pipeline
    │       ├─ 3a: Red Team sidecar transform (/api/redteam/apply)
    │       ├─ 3b: Template Injection (chat_inject, aggressive)
    │       ├─ 3c: ASCII Zero-Width Steganography
    │       ├─ 3d: LSB Image Steganography
    │       └─ 3e: Inline Bypass Framework (8 modules)
    │
[Stage 3f] Live Context Patches (applyContextPatches via context_patches.json)
    │
[Stage 4-5] Force Thinking Mode & Client Compaction Stripping (budget 31999)
    │
[Stage 6] Transport Evasion Modifiers (applyTransportMods: chunking, padding)
    │
[Stage 7] Upstream Forwarding & Streaming Delivery
    │
[Stage 8] Response Telemetry & Client Usage Spoofing (spoofer: cap 140K, rewrite rate limits)
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
  "claude_upstream": "",
  "spoof_usage_cap_tokens": 140000,
  "strip_system_reminders": true,
  "strip_thinking_blocks": true,
  "block_non_opus": true,
  "force_thinking": true,
  "force_thinking_budget": 31999,
  "sysprompt_enabled": true,
  "sysprompt_patch_file": "~/.claude/glass_sysprompt_patches.json",
  "sysprompt_replace_file": "~/.claude/glass_sysprompt_replace.json",
  "auto_refusal_rewrite_enabled": false,
  "auto_refusal_rewrite_strategy": "full_comply",
  "auto_usage_policy_rewrite_enabled": false,
  "tool_def_freeze": true,
  "mcp_tool_cache": true,
  "serializer_enabled": true,
  "redteam_sidecar_enabled": false,
  "tpl_inject_enabled": false,
  "stego_enabled": false,
  "bypass_enabled": false,
  "security_guard_enabled": false,
  "transport_chunked": false,
  "transport_pad_bytes": 0
}
```

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
8. [Cache and context mechanics](docs/cache-and-context-mechanics.md)
9. [Memory and recovery](docs/memory-and-recovery.md)
10. [Tengu and coding-client research](docs/tengu-and-client-research.md)
11. [Session Glass architecture](docs/session-glass-architecture.md)
12. [Provider lanes](docs/provider-lanes.md)
13. [Failures and lessons](docs/failures-and-lessons.md)
14. [Research chronology narrative](docs/research-chronology.md)
15. [Origin, contribution, and external context](docs/origin-contribution-and-external-context.md)
16. [Research scope and evidence rules](docs/research-scope-and-evidence.md)
17. [Research method and provenance](docs/research-method-and-provenance.md)

---

## Repository Map

- `cmd/glass-proxy/` — Main daemon assembly, listener lifecycle, HTTP multiplexer, and control endpoints.
- `internal/glass/` — Core Session Glass engine: canonical cache, watermark compression, token-targeted eviction, pinned frames, and chapter generation.
- `internal/proxy/` — Request pipeline, multi-lane routing, streaming, context patches, evasion stages, and security guard dispatch.
- `internal/codex/` — Codex lane: compaction stripping, `/responses` REST handler, WebSocket frame relay, and session snapshots.
- `internal/gemini/` — Gemini lane: `generateContent` handler and explicit Google AI Studio `CachedContent` synchronization.
- `internal/claude/` — Claude-specific background services, prefix warming, and rolling summarizer management.
- `internal/sysprompt/` — Same-length system prompt rewrite pipeline and pattern replace engine.
- `internal/spoofer/` — Token usage capping and rate-limit header spoofing engine.
- `internal/guard/` — Embedded GLASSDD security scanners (JS-X-Ray, GuardDog, OSV, dnstwist, LLMGuard).
- `internal/config/` — Hot-reloadable configuration parser and schema definitions.
- `internal/debug/` — Live diagnostics endpoints (`/debug/*`), SQLite telemetry database, and recorder.
- `internal/replay/` — Deterministic traffic capture, fixture simulation, and golden tests.
- `internal/mcpcache/` — MCP tool schema observation and injection cache.
- `internal/pidres/` — OS socket-to-process PID resolver.
- `internal/runtimepaths/` — Environment and runtime root path resolution.
- `bin/codex-glass` — Production CLI wrapper for endless Codex sessions without compaction.
- `bin/gemini-glass` — Production CLI wrapper for endless Gemini CLI sessions with explicit cache discounts.
- `start.sh` — Production startup script supporting dynamic lanes, auto-build detection, and logging.
- `verify.sh` — 15-point functional and structural test verification harness.

---

## Quickstart Guide

### 1. Build the Binary
```bash
go build -o glass-proxy ./cmd/glass-proxy
```

### 2. Start the Proxy
```bash
# Start on default port (18888) in default (Claude) mode
./start.sh

# Start in Codex mode on custom port
./start.sh --mode codex --port 18888

# Force rebuild before startup
./start.sh --build
```

### 3. Run Coding Agents Through Glass

#### Claude Code
```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:18888"
claude
```

#### OpenAI Codex CLI (No Compaction)
```bash
# Using the production wrapper script
./bin/codex-glass "build the authentication feature"

# Or manual environment configuration
export OPENAI_BASE_URL="http://127.0.0.1:18888"
codex
```

#### Google Gemini CLI (CachedContent Discount)
```bash
./bin/gemini-glass "refactor database schema"
```

### 4. Run System Verification
```bash
./verify.sh
```

---

## Deployment & Security Boundary

Glass is a single-user proxy that processes conversation content, tokens, and credentials while exposing deep diagnostic control surfaces. Read [SECURITY.md](SECURITY.md) before deployment.

The production source, tests, scripts, and research assets are preserved in-tree. Personal credentials, API keys, private transcripts, and machine-local databases are strictly excluded.

## Rights

All rights reserved. No software license is granted by this publication.
