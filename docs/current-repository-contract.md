# Current repository contract

**Confidence label:** Verified in source

This document defines what the repository currently implements, using only in-tree source and scripts.

## Runtime identity

- **Executable entrypoint:** `/home/runner/work/glass-proxy/glass-proxy/cmd/glass-proxy/main.go`
- **Module:** `proxy.local/app` in `/home/runner/work/glass-proxy/glass-proxy/go.mod`
- **Primary runtime packages:** `/home/runner/work/glass-proxy/glass-proxy/internal/*`

## Active lane surfaces

Implemented lane dispatch lives in `/home/runner/work/glass-proxy/glass-proxy/internal/proxy/proxy.go`.

Current route families:
- OpenRouter (`/openrouter/*`)
- OpenAI-compatible chat completions (`/chat/completions`)
- Codex responses (`/responses`, `/responses/compact`, websocket upgrades)
- Gemini generate/streamGenerateContent
- Anthropic Messages (`/v1/messages`)

## Configuration surface

Hot-reload config is implemented in:
- `/home/runner/work/glass-proxy/glass-proxy/internal/config/config.go`

The config includes:
- core trim/transform toggles,
- cache/context mode controls,
- lane-specific thresholds,
- guard/scanner controls,
- request mutation controls,
- transport modifiers.

## Debug/control surface

Mounted in process assembly:
- `/home/runner/work/glass-proxy/glass-proxy/cmd/glass-proxy/main.go`
- `/home/runner/work/glass-proxy/glass-proxy/internal/debug/api.go`

Debug endpoints include `/debug/status`, `/debug/latest`, `/debug/session`, `/debug/history`, `/debug/query`, lane auth/session endpoints, and serializer/proxy-mode endpoints.

## Security model (current scope)

**Confidence label:** Verified in source + explicit boundary docs

- Single-user process model.
- Optional embedded guard subsystem via `/home/runner/work/glass-proxy/glass-proxy/internal/guard`.
- Route-specific behavior is not equivalent to universal security filtering.
- Deployment controls (listener binding, host firewall, filesystem permissions) remain required.

See `/home/runner/work/glass-proxy/glass-proxy/SECURITY.md`.

## Test surface

Tests currently present under:
- `/home/runner/work/glass-proxy/glass-proxy/cmd/**/*_test.go`
- `/home/runner/work/glass-proxy/glass-proxy/internal/**/*_test.go`

This repository currently contains 56 Go test files.

## Documentation authority rule

- This file and linked current-implementation docs are authoritative for current runtime shape.
- Historical docs are preserved but non-authoritative for present behavior unless re-verified against current source.
