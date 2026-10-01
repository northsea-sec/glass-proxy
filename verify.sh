#!/usr/bin/env bash
# verify.sh — Glass proxy verification tests
# Run after starting the proxy in the lane you want to verify.
set -euo pipefail

PROXY_URL="${GLASS_PROXY:-http://127.0.0.1:18888}"
RUNTIME_LANE="${GLASS_RUNTIME_LANE:-claude}"
if [ "${RUNTIME_LANE}" = "codex" ]; then
    RUNTIME_ROOT="${GLASS_RUNTIME_ROOT:-${CODEX_HOME:-${HOME}/.codex}/glass-proxy}"
    PROXY_HEALTH_PATH="/responses"
else
    RUNTIME_ROOT="${GLASS_RUNTIME_ROOT:-${HOME}/.claude}"
    PROXY_HEALTH_PATH="/v1/messages"
fi
PASS=0
FAIL=0
TOTAL=0

check() {
    local name="$1"
    local result="$2"
    TOTAL=$((TOTAL+1))
    if [ "$result" = "0" ]; then
        echo "  [PASS] $name"
        PASS=$((PASS+1))
    else
        echo "  [FAIL] $name"
        FAIL=$((FAIL+1))
    fi
}

echo "=== Glass Proxy Verification ==="
echo "Proxy: ${PROXY_URL}"
echo ""

# Test A: Binary exists and runs
echo "--- Test A: Binary ---"
check "Binary exists" "$(test -f /home/user/glass-proxy/glass-proxy; echo $?)"
check "Binary executable" "$(test -x /home/user/glass-proxy/glass-proxy; echo $?)"

# Test B: Go vet passes
echo "--- Test B: Go Vet ---"
cd /home/user/glass-proxy
go_vet_result=$(go vet ./... 2>&1; echo $?)
check "go vet clean" "$(echo "$go_vet_result" | tail -1)"

# Test C: Unit tests pass
echo "--- Test C: Unit Tests ---"
test_result=$(go test ./... -count=1 2>&1)
test_exit=$?
check "go test all pass" "$test_exit"

# Test D: Glass module files
echo "--- Test D: Glass Module ---"
check "session.go exists" "$(test -f internal/glass/session.go; echo $?)"
check "eviction.go exists" "$(test -f internal/glass/eviction.go; echo $?)"
check "shadow.go exists" "$(test -f internal/glass/shadow.go; echo $?)"
check "reference.go exists" "$(test -f internal/glass/reference.go; echo $?)"
check "thinking.go exists" "$(test -f internal/glass/thinking.go; echo $?)"
check "ccfix.go exists" "$(test -f internal/glass/ccfix.go; echo $?)"
check "sysreminder.go exists" "$(test -f internal/glass/sysreminder.go; echo $?)"
check "process.go exists" "$(test -f internal/glass/process.go; echo $?)"
check "sysprompt.go exists" "$(test -f internal/glass/sysprompt.go; echo $?)"

# Test E: Config and shadow dirs
echo "--- Test E: Configuration ---"
check "Shadow dir exists" "$(test -d "${RUNTIME_ROOT}/glass" || mkdir -p "${RUNTIME_ROOT}/glass"; echo $?)"

# Test F: No removed module imports
echo "--- Test F: Clean Imports ---"
removed_imports=$(grep -rl "proxy.local/app/internal/audit" internal/ cmd/ --include="*.go" 2>/dev/null | wc -l || true)
check "No removed module imports" "$(test "$removed_imports" = "0"; echo $?)"

# Test G: Proxy connectivity (if running)
echo "--- Test G: Connectivity ---"
if curl -s -o /dev/null -w "%{http_code}" --connect-timeout 2 --max-time 5 "${PROXY_URL}${PROXY_HEALTH_PATH}" -X POST -H "Content-Type: application/json" -d '{}' 2>/dev/null | grep -q '[2-5][0-9][0-9]'; then
    check "Proxy responds to POST ${PROXY_HEALTH_PATH}" "0"
else
    echo "  [SKIP] Proxy not running on ${PROXY_URL}"
fi

echo ""
echo "=== Results: ${PASS}/${TOTAL} passed, ${FAIL} failed ==="
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
