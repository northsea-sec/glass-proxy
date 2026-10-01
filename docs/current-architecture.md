# Current architecture (source aligned)

## Confidence labels used here

- **Verified in source**: directly supported by current files in this repository.
- **Historical claim**: retained record from prior periods, not current-source authority.
- **External reference**: provider/public docs outside this repository.

---

## 1) Process assembly

**Label:** Verified in source

The process is assembled in `/home/runner/work/glass-proxy/glass-proxy/cmd/glass-proxy/main.go`:
- flag parsing and startup mode handling,
- config loader and polling,
- lane handler construction (Codex, Gemini, OpenAI-compatible service, Anthropic path),
- debug route registration,
- TCP and optional Unix socket listeners,
- shutdown/drain behavior.

## 2) Route and lane dispatch

**Label:** Verified in source

Lane matching and dispatch are implemented in `/home/runner/work/glass-proxy/glass-proxy/internal/proxy/proxy.go`.

Priority order is explicit via `laneRoutes()` and includes:
1. OpenRouter,
2. OpenAI-compatible,
3. Codex,
4. Gemini,
5. Anthropic/Claude messages,
6. fallback reverse-proxy path.

## 3) Anthropic pipeline and Session Glass

**Label:** Verified in source

Anthropic message handling is in `/home/runner/work/glass-proxy/glass-proxy/internal/proxy/proxy.go` and `/home/runner/work/glass-proxy/glass-proxy/internal/glass/*`.

Active stages include:
- request normalization,
- dedup/session identity/subagent handling,
- model/subagent gates,
- MCP cache injection,
- Glass process path (or passthrough/probe path),
- optional post-Glass mutation stages,
- serializer scheduling,
- upstream forwarding and stream handling.

## 4) Non-Anthropic lane behavior

**Label:** Verified in source

- Codex lane: `/home/runner/work/glass-proxy/glass-proxy/internal/codex/handler.go`
- Gemini lane: `/home/runner/work/glass-proxy/glass-proxy/internal/gemini/handler.go`
- OpenAI-compatible lane: `/home/runner/work/glass-proxy/glass-proxy/internal/proxy/openai_handler.go`
- OpenRouter forwarding: `/home/runner/work/glass-proxy/glass-proxy/internal/proxy/openrouter_handler.go`

Each lane owns its protocol-specific parsing and session behavior; parity is not assumed.

## 5) Active mutation/transformation stages

**Label:** Verified in source

Anthropic path currently includes optional transforms controlled by config:
- Redteam sidecar
- Template injection
- ASCII steganography
- LSB image steganography
- Bypass framework transform

Implementations live in `/home/runner/work/glass-proxy/glass-proxy/internal/proxy/proxy.go`.

## 6) Config + runtime state

**Label:** Verified in source

- Config schema and defaults: `/home/runner/work/glass-proxy/glass-proxy/internal/config/config.go`
- Runtime root helpers: `/home/runner/work/glass-proxy/glass-proxy/internal/runtimepaths/runtimepaths.go`
- Debug DB APIs: `/home/runner/work/glass-proxy/glass-proxy/internal/debug/*`

## 7) Debug and risk boundary

**Label:** Verified in source + policy boundary

Debug/control routes are mounted by default process wiring and can expose operational state.

Deployment requirements are documented in `/home/runner/work/glass-proxy/glass-proxy/SECURITY.md` and are operationally mandatory.

## 8) Historical vs current authority

**Label:** Verified policy in this repository

Historical chronology and research artifacts are retained for provenance and lineage but do not override current source.

See `/home/runner/work/glass-proxy/glass-proxy/docs/historical-archive-boundary.md`.
