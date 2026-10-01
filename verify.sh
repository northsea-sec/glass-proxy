#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}"
BINARY="${REPO_ROOT}/glass-proxy"

PASS=0
FAIL=0
TOTAL=0

check() {
  local name="$1"
  local rc="$2"
  TOTAL=$((TOTAL+1))
  if [[ "${rc}" == "0" ]]; then
    echo "  [PASS] ${name}"
    PASS=$((PASS+1))
  else
    echo "  [FAIL] ${name}"
    FAIL=$((FAIL+1))
  fi
}

echo "=== Glass Proxy Verification ==="
echo "Repo: ${REPO_ROOT}"

bash -n "${REPO_ROOT}/start.sh" >/dev/null 2>&1; check "start.sh syntax" "$?"
bash -n "${REPO_ROOT}/verify.sh" >/dev/null 2>&1; check "verify.sh syntax" "$?"

[[ -f "${REPO_ROOT}/cmd/glass-proxy/main.go" ]]; check "main.go present" "$?"
[[ -f "${REPO_ROOT}/internal/proxy/proxy.go" ]]; check "proxy.go present" "$?"
[[ -f "${REPO_ROOT}/internal/glass/process.go" ]]; check "glass process present" "$?"
[[ -f "${REPO_ROOT}/internal/config/config.go" ]]; check "config surface present" "$?"

if (cd "${REPO_ROOT}" && go test ./... -count=1 >/tmp/glass-proxy-test.log 2>&1); then
  check "go test ./..." "0"
else
  check "go test ./..." "1"
fi

if [[ -x "${BINARY}" ]]; then
  check "binary exists" "0"
else
  if (cd "${REPO_ROOT}" && go build -o "${BINARY}" ./cmd/glass-proxy >/tmp/glass-proxy-build.log 2>&1); then
    check "build binary" "0"
  else
    check "build binary" "1"
  fi
fi

echo "=== Results: ${PASS}/${TOTAL} passed, ${FAIL} failed ==="
if [[ "${FAIL}" -gt 0 ]]; then
  echo "Test logs: /tmp/glass-proxy-test.log /tmp/glass-proxy-build.log" >&2
  exit 1
fi
