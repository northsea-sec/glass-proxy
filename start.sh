#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}"
BINARY="${REPO_ROOT}/glass-proxy"

MODE="default"
PORT="18888"
FORCE_BUILD="false"

usage() {
  cat <<USAGE
Usage:
  ./start.sh [--mode default|codex] [--port PORT] [--build]
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      MODE="$2"
      shift 2
      ;;
    --port)
      PORT="$2"
      shift 2
      ;;
    --build)
      FORCE_BUILD="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown arg: $1" >&2
      usage
      exit 1
      ;;
  esac
done

case "${MODE}" in
  default|codex) ;;
  *)
    echo "Unsupported mode: ${MODE}" >&2
    exit 1
    ;;
esac

if [[ "${FORCE_BUILD}" == "true" || ! -x "${BINARY}" ]]; then
  echo "[GLASS] Building binary..."
  (cd "${REPO_ROOT}" && go build -o "${BINARY}" ./cmd/glass-proxy)
fi

CONFIG_ROOT="${GLASS_RUNTIME_ROOT:-${HOME}/.claude}"
if [[ "${MODE}" == "codex" ]]; then
  CONFIG_ROOT="${GLASS_RUNTIME_ROOT:-${CODEX_HOME:-${HOME}/.codex}/glass-proxy}"
  export GLASS_RUNTIME_LANE="${GLASS_RUNTIME_LANE:-codex}"
else
  export GLASS_RUNTIME_LANE="${GLASS_RUNTIME_LANE:-claude}"
fi

mkdir -p "${CONFIG_ROOT}/glass"
GLASS_CONFIG_PATH="${GLASS_CONFIG:-${CONFIG_ROOT}/glass_config.json}"

if [[ ! -f "${GLASS_CONFIG_PATH}" ]]; then
  mkdir -p "$(dirname "${GLASS_CONFIG_PATH}")"
  cat > "${GLASS_CONFIG_PATH}" <<JSON
{
  "enabled": true,
  "strip_system_reminders": true,
  "strip_thinking_blocks": true,
  "glass_passthrough": false
}
JSON
fi

UPSTREAM="https://api.anthropic.com"
if [[ "${MODE}" == "codex" ]]; then
  UPSTREAM="https://api.openai.com"
fi

echo "[GLASS] Repo root: ${REPO_ROOT}"
echo "[GLASS] Mode: ${MODE}"
echo "[GLASS] Listen: :${PORT}"
echo "[GLASS] Config: ${GLASS_CONFIG_PATH}"
echo "[GLASS] Upstream: ${UPSTREAM}"

exec "${BINARY}" \
  -listen ":${PORT}" \
  -mode "${MODE}" \
  -upstream "${UPSTREAM}" \
  -config "${GLASS_CONFIG_PATH}" \
  -allow-direct
