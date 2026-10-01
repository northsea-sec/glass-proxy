# Glass Proxy

Glass Proxy is a **single-user, multi-lane AI proxy** for controlled request routing, context management, and lane-specific protocol handling.

This repository is published as a **source-of-truth snapshot of what exists in-tree now**. Historical research narrative is retained, but separated from current implementation authority.

## What this repository is

- A Go executable at `/home/runner/work/glass-proxy/glass-proxy/cmd/glass-proxy`.
- A multi-lane proxy with distinct handlers for:
  - Anthropic Messages (`/v1/messages`)
  - Codex Responses (`/responses`, `/responses/compact`, websocket `/responses`)
  - Gemini generateContent / streamGenerateContent
  - OpenAI-compatible Chat Completions (`/chat/completions`)
  - OpenRouter (`/openrouter/*`)
- A hot-reloadable config surface in `/home/runner/work/glass-proxy/glass-proxy/internal/config/config.go`.
- A debug/control surface mounted under `/debug/*`.

## What this repository is not

- Not a multi-tenant platform.
- Not a managed credential broker.
- Not a guarantee that all historical experiments are active in current runtime.
- Not a claim that all historical incident artifacts are included publicly.

## Security first

Read `/home/runner/work/glass-proxy/glass-proxy/SECURITY.md` before deployment.

Important boundary:
- The process can expose debug and forwarding endpoints.
- It processes credentials and conversation payloads.
- You must bind privately and apply network + filesystem controls.

## Quickstart (current repo)

### 1) Build

```bash
cd /home/runner/work/glass-proxy/glass-proxy
go build -o glass-proxy ./cmd/glass-proxy
```

### 2) Start directly (example)

```bash
./glass-proxy -listen :18888 -mode default
```

Optional key flags are in `/home/runner/work/glass-proxy/glass-proxy/cmd/glass-proxy/main.go`:
- `-listen`
- `-unix-socket`
- `-upstream`
- `-claude-upstream`
- `-config`
- `-api-key`
- `-allow-direct`
- `-mode`

### 3) Start via helper script

```bash
./start.sh --mode default --port 18888
```

### 4) Run verification helper

```bash
./verify.sh
```

## Documentation map

### Current implementation (authoritative)

- `/home/runner/work/glass-proxy/glass-proxy/docs/current-repository-contract.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/current-architecture.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/provider-lanes.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/cache-and-context-mechanics.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/session-glass-architecture.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/docs-governance.md`

### Historical archive (non-authoritative for current runtime)

- `/home/runner/work/glass-proxy/glass-proxy/docs/historical-archive-boundary.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/complete-chronology.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/research-chronology.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/problem-register.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/solution-register.md`
- `/home/runner/work/glass-proxy/glass-proxy/docs/source-catalogue.md`

### Missing/withheld public artifacts

- `/home/runner/work/glass-proxy/glass-proxy/docs/missing-withheld-artifacts.md`

## Repository layout

- `/home/runner/work/glass-proxy/glass-proxy/cmd/glass-proxy/` — process assembly and endpoint wiring.
- `/home/runner/work/glass-proxy/glass-proxy/internal/proxy/` — lane routing and request handling.
- `/home/runner/work/glass-proxy/glass-proxy/internal/glass/` — Anthropic lane state, processing, and recovery structures.
- `/home/runner/work/glass-proxy/glass-proxy/internal/codex/` — Codex lane.
- `/home/runner/work/glass-proxy/glass-proxy/internal/gemini/` — Gemini lane.
- `/home/runner/work/glass-proxy/glass-proxy/internal/config/` — runtime config model.
- `/home/runner/work/glass-proxy/glass-proxy/internal/debug/` — debug API/data.
- `/home/runner/work/glass-proxy/glass-proxy/internal/guard/` — guard/scanner integration.

## License and rights

See repository policy documents. This publication preserves implementation and research records; it does not grant additional security or compatibility guarantees beyond documented scope.
