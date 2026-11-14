#!/usr/bin/env bash
set +o histexpand

# Test script for audit_pre_agent_workspace.sh
# Run: bash audit_pre_agent_workspace_test.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_PATH="${SCRIPT_DIR}/audit_pre_agent_workspace.sh"

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Temporary workspace for tests
TEST_ROOT="$(mktemp -d)"

cleanup() {
  rm -rf "${TEST_ROOT}"
}
trap cleanup EXIT

assert() {
  local name="$1"
  shift
  if "$@"; then
    echo "✓ ${name}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "✗ ${name}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

assert_not() {
  local name="$1"
  shift
  if "$@"; then
    echo "✗ ${name}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  else
    echo "✓ ${name}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  fi
}

echo "Testing audit_pre_agent_workspace.sh..."
echo ""

# ── Test 1: Script syntax is valid ──────────────────────────────────────────
echo "Test 1: Script syntax is valid"
if bash -n "${SCRIPT_PATH}" 2>/dev/null; then
  assert "Script syntax is valid" true
else
  assert "Script has syntax errors" false
fi
echo ""

# ── Test 2: Writes audit file to /tmp/gh-aw/pre-agent-audit.txt ─────────────
echo "Test 2: Audit file is written to the correct path"
WORKSPACE="${TEST_ROOT}/workspace-2"
mkdir -p "${WORKSPACE}/.github/agents"
echo "agent.md" > "${WORKSPACE}/.github/agents/my.md"
TMPDIR="${TEST_ROOT}/tmp2"
mkdir -p "${TMPDIR}/gh-aw"
GH_OUT="${TEST_ROOT}/github_output2"
touch "${GH_OUT}"
rm -f /tmp/gh-aw/pre-agent-audit.txt
OUTPUT="$(GITHUB_WORKSPACE="${WORKSPACE}" HOME="${TEST_ROOT}/home2" RUNNER_TEMP="${TMPDIR}" GITHUB_OUTPUT="${GH_OUT}" bash "${SCRIPT_PATH}" 2>&1)"
assert "Audit file created at /tmp/gh-aw/pre-agent-audit.txt" test -f /tmp/gh-aw/pre-agent-audit.txt
assert "Audit file is non-empty" test -s /tmp/gh-aw/pre-agent-audit.txt
echo ""

# ── Test 3: Audit file contains expected section headers ────────────────────
echo "Test 3: Section headers are present in audit output"
assert "Contains workspace agents header" grep -q 'Workspace agents' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace skills header" grep -q 'Workspace skills' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace copilot header" grep -q 'Workspace copilot' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace claude header" grep -q 'Workspace claude' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace codex header" grep -q 'Workspace codex' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace gemini header" grep -q 'Workspace gemini' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace crush header" grep -q 'Workspace crush' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace opencode header" grep -q 'Workspace opencode' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains workspace pi header" grep -q 'Workspace pi' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains agent user home header" grep -q 'Agent user home' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains gh extensions header" grep -q 'gh extensions' /tmp/gh-aw/pre-agent-audit.txt
assert "Contains gh-aw temp header" grep -q 'gh-aw temp directory' /tmp/gh-aw/pre-agent-audit.txt
echo ""

# ── Test 4: Lists files that exist ──────────────────────────────────────────
echo "Test 4: Existing agent file appears in audit output"
assert "Agent file listed in audit" grep -q 'my.md' /tmp/gh-aw/pre-agent-audit.txt
echo ""

# ── Test 5: Missing directories are noted as (not found) ────────────────────
echo "Test 5: Missing directories show (not found)"
assert "Missing directory noted" grep -q '(not found)' /tmp/gh-aw/pre-agent-audit.txt
echo ""

# ── Test 6: GITHUB_OUTPUT contains pre-agent-audit-file entry ───────────────
echo "Test 6: GITHUB_OUTPUT is written"
assert "pre-agent-audit-file in GITHUB_OUTPUT" grep -q 'pre-agent-audit-file=' "${GH_OUT}"
assert "pre-agent-audit-line-count in GITHUB_OUTPUT" grep -q 'pre-agent-audit-line-count=' "${GH_OUT}"
echo ""

# ── Test 7: Excludes node_modules ───────────────────────────────────────────
echo "Test 7: node_modules directory is excluded"
WORKSPACE="${TEST_ROOT}/workspace-7"
mkdir -p "${WORKSPACE}/.github/agents/node_modules/some-pkg"
echo "index.js" > "${WORKSPACE}/.github/agents/node_modules/some-pkg/index.js"
echo "agent.md" > "${WORKSPACE}/.github/agents/agent.md"
TMPDIR="${TEST_ROOT}/tmp7"
mkdir -p "${TMPDIR}/gh-aw"
GH_OUT7="${TEST_ROOT}/github_output7"
touch "${GH_OUT7}"
rm -f /tmp/gh-aw/pre-agent-audit.txt
GITHUB_WORKSPACE="${WORKSPACE}" HOME="${TEST_ROOT}/home7" RUNNER_TEMP="${TMPDIR}" GITHUB_OUTPUT="${GH_OUT7}" bash "${SCRIPT_PATH}" >/dev/null 2>&1
assert_not "node_modules excluded from listing" grep -q 'node_modules/some-pkg' /tmp/gh-aw/pre-agent-audit.txt
assert "agent.md still listed despite node_modules sibling" grep -q 'agent.md' /tmp/gh-aw/pre-agent-audit.txt
echo ""

# ── Test 8: Collapsed step summary tree is rendered ─────────────────────────
echo "Test 8: Step summary contains a collapsed tree"
WORKSPACE="${TEST_ROOT}/workspace-8"
mkdir -p "${WORKSPACE}/.github/agents"
echo "agent" > "${WORKSPACE}/.github/agents/agent.md"
TMPDIR="${TEST_ROOT}/tmp8"
mkdir -p "${TMPDIR}/gh-aw"
GH_OUT8="${TEST_ROOT}/github_output8"
SUMMARY8="${TEST_ROOT}/step_summary8"
FENCE='```'
touch "${GH_OUT8}" "${SUMMARY8}"
GITHUB_WORKSPACE="${WORKSPACE}" HOME="${TEST_ROOT}/home8" RUNNER_TEMP="${TMPDIR}" GITHUB_OUTPUT="${GH_OUT8}" GITHUB_STEP_SUMMARY="${SUMMARY8}" bash "${SCRIPT_PATH}" >/dev/null 2>&1
assert "Summary uses a text code fence" grep -Fq "${FENCE}text" "${SUMMARY8}"
assert "Summary closes the code fence" grep -Fq "${FENCE}" "${SUMMARY8}"
assert "Summary uses details" grep -Fq '<details>' "${SUMMARY8}"
assert "Summary closes details" grep -Fq '</details>' "${SUMMARY8}"
assert "Summary title is in the details summary" grep -Fq '<summary>Pre-agent workspace audit</summary>' "${SUMMARY8}"
assert "Summary contains tree branches" grep -Fq '├── Copilot engine' "${SUMMARY8}"
assert "Summary lists agent.md in the tree" grep -Fq "└── ${WORKSPACE}/.github/agents/agent.md" "${SUMMARY8}"
assert_not "Summary does not use a table" grep -Fq '| Section | Path | Size |' "${SUMMARY8}"
assert_not "Summary does not use ASCII branches" grep -Fq '|-- Copilot engine' "${SUMMARY8}"
echo ""

# ── Test 9: Works without GITHUB_STEP_SUMMARY set ──────────────────────────
echo "Test 9: Script succeeds when GITHUB_STEP_SUMMARY is unset"
TMPDIR="${TEST_ROOT}/tmp9"
mkdir -p "${TMPDIR}/gh-aw"
GH_OUT9="${TEST_ROOT}/github_output9"
touch "${GH_OUT9}"
if GITHUB_WORKSPACE="${TEST_ROOT}/workspace-8" HOME="${TEST_ROOT}/home9" RUNNER_TEMP="${TMPDIR}" GITHUB_OUTPUT="${GH_OUT9}" bash "${SCRIPT_PATH}" >/dev/null 2>&1; then
  assert "Script succeeds without GITHUB_STEP_SUMMARY" true
else
  assert "Script succeeds without GITHUB_STEP_SUMMARY" false
fi
echo ""

# ── Test 10: Excludes large installed tool caches ────────────────────────────
echo "Test 10: Installed tool-cache trees are excluded"
TMPDIR="${TEST_ROOT}/tmp10"
mkdir -p "${TMPDIR}/gh-aw/tool-cache/dotnet" "${TMPDIR}/gh-aw/tool-cache/node"
touch "${TMPDIR}/gh-aw/tool-cache/dotnet/installed-toolchain-library-with-a-long-name-for-summary-size-regression-"{1..5000}.dll
touch "${TMPDIR}/gh-aw/tool-cache/node/installed-toolchain-module-with-a-long-name-for-summary-size-regression-"{1..5000}.js
touch "${TMPDIR}/gh-aw/agent-config.json"
GH_OUT10="${TEST_ROOT}/github_output10"
SUMMARY10="${TEST_ROOT}/step_summary10"
touch "${GH_OUT10}" "${SUMMARY10}"
GITHUB_WORKSPACE="${TEST_ROOT}/workspace-8" HOME="${TEST_ROOT}/home10" RUNNER_TEMP="${TMPDIR}" GITHUB_OUTPUT="${GH_OUT10}" GITHUB_STEP_SUMMARY="${SUMMARY10}" bash "${SCRIPT_PATH}" >/dev/null 2>&1
assert_not "tool-cache excluded from audit listing" grep -q '/tool-cache' /tmp/gh-aw/pre-agent-audit.txt
assert_not "tool-cache excluded from summary" grep -q '/tool-cache' "${SUMMARY10}"
assert "Sibling file still listed in audit" grep -Fq "${TMPDIR}/gh-aw/agent-config.json" /tmp/gh-aw/pre-agent-audit.txt
assert "Sibling file still listed in summary" grep -Fq "${TMPDIR}/gh-aw/agent-config.json" "${SUMMARY10}"
SUMMARY_SIZE=$(wc -c < "${SUMMARY10}")
assert "Summary remains below the 1 MiB limit" test "${SUMMARY_SIZE}" -lt 1048576
AUDIT_LINE_COUNT=$(wc -l < /tmp/gh-aw/pre-agent-audit.txt | tr -d ' ')
assert "Reported line count matches audit" grep -Fxq "pre-agent-audit-line-count=${AUDIT_LINE_COUNT}" "${GH_OUT10}"
echo ""

# ── Test 11: Rejects a symlinked GitHub output file ───────────────────────────
echo "Test 11: Symlinked GitHub output is not written"
SENTINEL11="${TEST_ROOT}/sentinel11"
GH_OUT11="${TEST_ROOT}/github_output11"
printf 'external sentinel\n' > "${SENTINEL11}"
ln -s "${SENTINEL11}" "${GH_OUT11}"
if GITHUB_WORKSPACE="${TEST_ROOT}/workspace-8" HOME="${TEST_ROOT}/home11" RUNNER_TEMP="${TEST_ROOT}/tmp11" GITHUB_OUTPUT="${GH_OUT11}" bash "${SCRIPT_PATH}" >/dev/null 2>&1; then
  assert "rejects symlinked GITHUB_OUTPUT" "false"
else
  assert "rejects symlinked GITHUB_OUTPUT" "[ \"\$(cat '${SENTINEL11}')\" = 'external sentinel' ] && [ -L '${GH_OUT11}' ]"
fi
echo ""

# ── Summary ──────────────────────────────────────────────────────────────────
echo "Results: ${TESTS_PASSED} passed, ${TESTS_FAILED} failed"
if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi
exit 0
