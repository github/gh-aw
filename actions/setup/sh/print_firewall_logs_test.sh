#!/usr/bin/env bash
set +o histexpand

# Test script for print_firewall_logs.sh
# Run: bash print_firewall_logs_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/print_firewall_logs.sh"

TESTS_PASSED=0
TESTS_FAILED=0
WORKSPACE="$(mktemp -d "${PWD}/.firewall-log-tests.XXXXXX")"
export GH_AW_RUNTIME_DIR="$WORKSPACE"

cleanup() {
  rm -rf "${WORKSPACE}"
}
trap cleanup EXIT

assert() {
  local name="$1"
  local condition="$2"
  if eval "${condition}" 2>/dev/null; then
    echo "  ✓ ${name}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  ✗ ${name}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

echo "Testing print_firewall_logs.sh"
echo ""

# ── Test 1: Script syntax is valid ──────────────────────────────────────────
echo "Test 1: Script syntax is valid"
assert "script passes bash -n" "bash -n '${SCRIPT}'"
echo ""

# ── Test 2: Unknown argument exits 1 ────────────────────────────────────────
echo "Test 2: Unknown argument exits 1"
set +e
AWF_LOGS_DIR="/tmp/logs" GITHUB_STEP_SUMMARY="/dev/null" bash "${SCRIPT}" --unknown-flag 2>/dev/null
UNKNOWN_EXIT=$?
set -e
assert "exits non-zero for unknown argument" "[ '${UNKNOWN_EXIT}' -ne 0 ]"
echo ""

# ── Test 3: AWF not installed prints informational message ──────────────────
echo "Test 3: AWF not installed prints informational message"
FIREWALL_DIR="${WORKSPACE}/test3/sandbox/firewall"
mkdir -p "${FIREWALL_DIR}/logs"
set +e
OUTPUT="$(
  PATH="/usr/bin:/bin" \
  AWF_LOGS_DIR="${FIREWALL_DIR}/logs" \
  GITHUB_STEP_SUMMARY="${WORKSPACE}/test3-summary.md" \
  bash "${SCRIPT}" 2>&1
)"
EXIT_CODE=$?
set -e
assert "exits successfully when awf is not installed" "[ '${EXIT_CODE}' -eq 0 ]"
assert "prints 'AWF binary not installed' message" "printf '%s' \"${OUTPUT}\" | grep -q 'AWF binary not installed'"
assert "identifies incomplete gateway startup when access log is missing" "printf '%s' \"${OUTPUT}\" | grep -q 'MCP gateway did not complete startup'"
echo ""

# ── Test 4: Missing access log distinguishes a started gateway ───────────────
echo "Test 4: Missing access log distinguishes a started gateway"
FIREWALL_DIR="${WORKSPACE}/test4/sandbox/firewall"
GATEWAY_MARKER="${WORKSPACE}/test4/mcp-gateway-started"
mkdir -p "${FIREWALL_DIR}/logs"
touch "${GATEWAY_MARKER}"
set +e
OUTPUT="$(
  PATH="/usr/bin:/bin" \
  AWF_LOGS_DIR="${FIREWALL_DIR}/logs" \
  MCP_GATEWAY_STARTUP_MARKER="${GATEWAY_MARKER}" \
  GITHUB_STEP_SUMMARY="${WORKSPACE}/test4-summary.md" \
  bash "${SCRIPT}" 2>&1
)"
EXIT_CODE=$?
set -e
assert "exits successfully when gateway started without traffic" "[ '${EXIT_CODE}' -eq 0 ]"
assert "identifies started gateway without auditable traffic" "printf '%s' \"${OUTPUT}\" | grep -q 'MCP gateway started but produced no auditable egress traffic'"
echo ""

# ── Test 5: --rootless flag is accepted without error ───────────────────────
echo "Test 5: --rootless flag is accepted without error"
FIREWALL_DIR="${WORKSPACE}/test4/sandbox/firewall"
mkdir -p "${FIREWALL_DIR}/logs"
set +e
OUTPUT="$(
  PATH="/usr/bin:/bin" \
  AWF_LOGS_DIR="${FIREWALL_DIR}/logs" \
  GITHUB_STEP_SUMMARY="${WORKSPACE}/test4-summary.md" \
  bash "${SCRIPT}" --rootless 2>&1
)"
EXIT_CODE=$?
set -e
assert "exits successfully with --rootless" "[ '${EXIT_CODE}' -eq 0 ]"
assert "prints 'AWF binary not installed' message (not an arg-parse error)" "printf '%s' \"${OUTPUT}\" | grep -q 'AWF binary not installed'"
echo ""

# ── Test 6: FIREWALL_DIR is computed as dirname of AWF_LOGS_DIR ─────────────
echo "Test 6: FIREWALL_DIR is computed as dirname of AWF_LOGS_DIR"
LOGS_DIR="${WORKSPACE}/test5/sandbox/firewall/logs"
mkdir -p "${LOGS_DIR}"
set +e
OUTPUT="$(
  PATH="/usr/bin:/bin" \
  AWF_LOGS_DIR="${LOGS_DIR}" \
  GITHUB_STEP_SUMMARY="${WORKSPACE}/test5-summary.md" \
  bash "${SCRIPT}" 2>&1
)"
set -e
EXPECTED_DIR="${WORKSPACE}/test5/sandbox/firewall"
assert "script does not error on a valid logs dir" "[ -d '${EXPECTED_DIR}' ]"
echo ""

# ── Test 7: Rootless mode - non-sudo chmod fallback succeeds when sudo absent ─
echo "Test 7: Rootless mode - non-sudo chmod fallback succeeds when sudo unavailable"
FIREWALL_DIR="${WORKSPACE}/test6/sandbox/firewall"
mkdir -p "${FIREWALL_DIR}/logs"
chmod 500 "${FIREWALL_DIR}"  # owner read+exec only — chmod a+rX fallback must run
set +e
OUTPUT="$(
  PATH="/usr/bin:/bin" \
  AWF_LOGS_DIR="${FIREWALL_DIR}/logs" \
  GITHUB_STEP_SUMMARY="${WORKSPACE}/test6-summary.md" \
  bash "${SCRIPT}" --rootless 2>&1
)"
EXIT_CODE=$?
set -e
chmod -R u+rwx "${FIREWALL_DIR}" 2>/dev/null || true
assert "exits 0 in rootless mode when sudo unavailable" "[ '${EXIT_CODE}' -eq 0 ]"
assert "prints 'AWF binary not installed' (not a chown/chmod crash)" "printf '%s' \"${OUTPUT}\" | grep -q 'AWF binary not installed'"
echo ""

# ── Test 8: Default mode - exits 0 when sudo unavailable (|| true catches it) ─
echo "Test 8: Default mode (non-rootless) - exits 0 even when sudo unavailable"
FIREWALL_DIR="${WORKSPACE}/test7/sandbox/firewall"
mkdir -p "${FIREWALL_DIR}/logs"
set +e
OUTPUT="$(
  PATH="/usr/bin:/bin" \
  AWF_LOGS_DIR="${FIREWALL_DIR}/logs" \
  GITHUB_STEP_SUMMARY="${WORKSPACE}/test7-summary.md" \
  bash "${SCRIPT}" 2>&1
)"
EXIT_CODE=$?
set -e
assert "exits 0 in default mode when sudo unavailable" "[ '${EXIT_CODE}' -eq 0 ]"
assert "prints 'AWF binary not installed' (not a sudo crash)" "printf '%s' \"${OUTPUT}\" | grep -q 'AWF binary not installed'"
echo ""

# ── Test 9: GITHUB_STEP_SUMMARY guard — no error when variable is unset ──────
echo "Test 9: GITHUB_STEP_SUMMARY guard - no error or tee failure when variable is unset"
FIREWALL_DIR="${WORKSPACE}/test8/sandbox/firewall"
MOCK_BIN="${WORKSPACE}/bin8"
mkdir -p "${FIREWALL_DIR}/logs" "${MOCK_BIN}"
cat > "${MOCK_BIN}/awf" << 'AWFMOCK'
#!/usr/bin/env bash
if [[ "$1 $2" == "logs summary" ]]; then echo "mock firewall summary"; fi
AWFMOCK
chmod +x "${MOCK_BIN}/awf"
set +e
# Run WITHOUT GITHUB_STEP_SUMMARY set - should not error
OUTPUT="$(
  PATH="${MOCK_BIN}:/usr/bin:/bin" \
  AWF_LOGS_DIR="${FIREWALL_DIR}/logs" \
  bash "${SCRIPT}" 2>&1
)"
EXIT_CODE=$?
set -e
assert "exits 0 when GITHUB_STEP_SUMMARY is unset" "[ '${EXIT_CODE}' -eq 0 ]"
assert "prints summary output to stdout" "printf '%s' \"${OUTPUT}\" | grep -q 'mock firewall summary'"
assert "no tee error about empty filename" "! printf '%s' \"${OUTPUT}\" | grep -q 'No such file or directory'"
echo ""

echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"