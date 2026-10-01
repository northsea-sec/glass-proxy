#!/usr/bin/env bash
# start-glass-proxy.sh — Start the Glass proxy for the selected runtime lane
# Replaces mitmproxy-fingerprint service for context management
#
# Usage:
#   ./start-glass-proxy.sh                # Start on default port (18888)
#   ./start-glass-proxy.sh --port 18889   # Start on alternate port (parallel testing)
#   ./start-glass-proxy.sh --build        # Force rebuild before starting
#
# Logging:
#   Default mode logs under /tmp/glass-proxy/.
#   Codex mode logs under the active Codex runtime root's logs/ directory.
#   Both modes update a stable current-<port>.log symlink for the newest log.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GLASS_DIR="/home/user/glass-proxy"
BINARY="${GLASS_DIR}/glass-proxy"
MODE="default"
CONFIG_DIR=""
GLASS_CONFIG=""

default_runtime_root() {
    if [ "${MODE}" = "codex" ]; then
        local codex_home="${CODEX_HOME:-${HOME}/.codex}"
        printf '%s\n' "${GLASS_RUNTIME_ROOT:-${codex_home}/glass-proxy}"
        return 0
    fi
    printf '%s\n' "${GLASS_RUNTIME_ROOT:-${HOME}/.claude}"
}

resolve_go_bin() {
    if [ -n "${GO_BIN:-}" ] && [ -x "${GO_BIN}" ]; then
        printf '%s\n' "${GO_BIN}"
        return 0
    fi
    if command -v go >/dev/null 2>&1; then
        command -v go
        return 0
    fi
    if [ -x /usr/local/go/bin/go ]; then
        printf '%s\n' /usr/local/go/bin/go
        return 0
    fi
    return 1
}

needs_build() {
    if [ ! -f "${BINARY}" ] || [ "${FORCE_BUILD}" = true ]; then
        return 0
    fi
    if [ "${GLASS_DIR}/go.mod" -nt "${BINARY}" ] || [ "${GLASS_DIR}/go.sum" -nt "${BINARY}" ]; then
        return 0
    fi
    if find "${GLASS_DIR}/cmd" "${GLASS_DIR}/internal" -type f -name '*.go' -newer "${BINARY}" -print -quit | grep -q .; then
        return 0
    fi
    return 1
}

# Source API keys
if [ -f "${SCRIPT_DIR}/api-keys.env" ]; then
    source "${SCRIPT_DIR}/api-keys.env"
fi

# Parse args
PORT=18888
FORCE_BUILD=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --port) PORT="$2"; shift 2 ;;
        --build) FORCE_BUILD=true; shift ;;
        --mode) MODE="$2"; shift 2 ;;
        *) echo "Unknown arg: $1"; exit 1 ;;
    esac
done

case "${MODE}" in
    default|codex) ;;
    *) echo "Unknown mode: ${MODE}" >&2; exit 1 ;;
esac

CONFIG_DIR="$(default_runtime_root)"
GLASS_CONFIG="${GLASS_CONFIG:-${CONFIG_DIR}/glass_config.json}"
if [ -z "${GLASS_RUNTIME_LANE:-}" ]; then
    if [ "${MODE}" = "codex" ]; then
        export GLASS_RUNTIME_LANE="codex"
    else
        export GLASS_RUNTIME_LANE="claude"
    fi
fi
export GLASS_RUNTIME_ROOT="${GLASS_RUNTIME_ROOT:-${CONFIG_DIR}}"
export GLASS_CONFIG
export GLASS_REPLAY_CAPTURE_DIR="${GLASS_REPLAY_CAPTURE_DIR:-${CONFIG_DIR}/glass-replay-capture-current}"

if [ "${MODE}" = "codex" ]; then
    LOG_DIR="${GLASS_LOG_DIR:-${CONFIG_DIR}/logs}"
else
    LOG_DIR="${GLASS_LOG_DIR:-${TMPDIR:-/tmp}/glass-proxy}"
fi
LOG_STAMP="$(date +%Y%m%d-%H%M%S)"
LOG_PATH="${LOG_DIR}/glass-proxy.${PORT}.${LOG_STAMP}.log"
CURRENT_LOG="${LOG_DIR}/current-${PORT}.log"

mkdir -p "${LOG_DIR}"
touch "${LOG_PATH}"
ln -sfn "${LOG_PATH}" "${CURRENT_LOG}"

# Mirror stdout/stderr to both the terminal and the per-start log file.
exec > >(tee -a "${LOG_PATH}") 2>&1

# Build if binary is missing, explicitly requested, or source is newer.
if needs_build; then
    GO_BUILD_BIN="$(resolve_go_bin || true)"
    if [ -z "${GO_BUILD_BIN}" ]; then
        echo "[GLASS] Cannot build glass-proxy: Go toolchain not found."
        echo "  Checked \$GO_BIN, PATH, and /usr/local/go/bin/go."
        exit 1
    fi
    echo "[GLASS] Building glass-proxy with ${GO_BUILD_BIN}..."
    cd "${GLASS_DIR}"
    "${GO_BUILD_BIN}" build -o "${BINARY}" ./cmd/glass-proxy/
    echo "[GLASS] Built: ${BINARY}"
else
    echo "[GLASS] Binary is up to date: ${BINARY}"
fi

# Create default config if missing
if [ ! -f "${GLASS_CONFIG}" ]; then
    mkdir -p "$(dirname "${GLASS_CONFIG}")"
    echo "[GLASS] Creating default config: ${GLASS_CONFIG}"
    cat > "${GLASS_CONFIG}" <<JSON
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
  "sysprompt_patch_file": "${CONFIG_DIR}/glass_sysprompt_patches.json",
  "sysprompt_replace_file": "${CONFIG_DIR}/glass_sysprompt_replace.json",
  "auto_refusal_rewrite_enabled": false,
  "auto_refusal_rewrite_strategy": "full_comply",
  "auto_usage_policy_rewrite_enabled": false,
  "tool_def_freeze": true,
  "mcp_tool_cache": true,
  "serializer_enabled": true
}
JSON
fi

# Ensure shadow directory exists
mkdir -p "${CONFIG_DIR}/glass"

# Resolve API key (optional — empty means passthrough from CC)
API_KEY_FLAG=""
if [ "${MODE}" != "codex" ] && [ -n "${ANTHROPIC_API_KEY:-}" ]; then
    API_KEY_FLAG="-api-key ${ANTHROPIC_API_KEY}"
    echo "[GLASS] API key: configured (will override client auth)"
else
    echo "[GLASS] API key: passthrough"
fi

echo "[GLASS] Starting glass-proxy on :${PORT}"
if [ "${MODE}" = "codex" ]; then
    echo "  Upstream: https://api.openai.com"
    echo "  Claude lane upstream: glass_config claude_upstream or https://api.anthropic.com"
else
    echo "  Upstream: https://api.anthropic.com"
fi
echo "  Config:   ${GLASS_CONFIG}"
echo "  Shadow:   ${CONFIG_DIR}/glass/"
echo "  Log:      ${LOG_PATH}"
echo "  Current:  ${CURRENT_LOG}"
echo ""
if [ "${MODE}" = "codex" ]; then
    echo "  To use with Codex:"
    echo "    export OPENAI_BASE_URL=http://127.0.0.1:${PORT}"
else
    echo "  To use with Claude Code (reverse proxy mode):"
    echo "    export ANTHROPIC_BASE_URL=http://127.0.0.1:${PORT}"
fi
echo ""
echo "  NOTE: Glass is a reverse proxy, not a CONNECT proxy."
if [ "${MODE}" = "codex" ]; then
    echo "  Use OPENAI_BASE_URL or codex-glass (not HTTPS_PROXY)."
else
    echo "  Use ANTHROPIC_BASE_URL (not HTTPS_PROXY)."
fi
echo ""

exec "${BINARY}" \
    -listen ":${PORT}" \
    -mode "${MODE}" \
    -upstream "$([ "${MODE}" = "codex" ] && printf '%s' "https://api.openai.com" || printf '%s' "https://api.anthropic.com")" \
    -config "${GLASS_CONFIG}" \
    ${API_KEY_FLAG} \
    -allow-direct
