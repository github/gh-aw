#!/usr/bin/env bash
set +o histexpand

# Tests for save_base_github_folders.sh
# Run: bash save_base_github_folders_test.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SAVE_SCRIPT="${SCRIPT_DIR}/save_base_github_folders.sh"

TESTS_PASSED=0
TESTS_FAILED=0

assert() {
  local name="$1"
  local condition="$2"
  if eval "${condition}"; then
    echo "✓ ${name}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "✗ ${name}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

# Simulated engine-registry values (matches what the Go compiler would emit)
AGENT_FOLDERS=".agents .claude .codex .gemini .github"
AGENT_FILES="AGENTS.md CLAUDE.md GEMINI.md"

REAL_DEST="/tmp/gh-aw/base"

cleanup() {
  rm -rf "${TEST_WORKSPACE:-}" "${REAL_DEST}"
}
trap cleanup EXIT

echo "Testing save_base_github_folders.sh..."
echo ""

# ── Test 1: Core folders and root files are saved ────────────────────────────
echo "Test 1: All agent config folders and root files saved"
TEST_WORKSPACE=$(mktemp -d)
mkdir -p "${TEST_WORKSPACE}/.github/skills"
echo "skill content" >"${TEST_WORKSPACE}/.github/skills/SKILL.md"
mkdir -p "${TEST_WORKSPACE}/.agents"
echo "agent content" >"${TEST_WORKSPACE}/.agents/agent.md"
mkdir -p "${TEST_WORKSPACE}/.claude/commands"
echo "claude cmd" >"${TEST_WORKSPACE}/.claude/commands/cmd.md"
mkdir -p "${TEST_WORKSPACE}/.claude/workflows/references"
echo 'export const meta = { name: "review", description: "Review" }' >"${TEST_WORKSPACE}/.claude/workflows/review.js"
echo "reference" >"${TEST_WORKSPACE}/.claude/workflows/references/.context"
echo "agents instructions" >"${TEST_WORKSPACE}/AGENTS.md"
echo "claude instructions" >"${TEST_WORKSPACE}/CLAUDE.md"
rm -rf "${REAL_DEST}"

GH_AW_AGENT_FOLDERS="${AGENT_FOLDERS}" GH_AW_AGENT_FILES="${AGENT_FILES}" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${SAVE_SCRIPT}" >/dev/null 2>&1

assert "saves .github" "[ -d '${REAL_DEST}/.github' ]"
assert "saves SKILL.md" "[ -f '${REAL_DEST}/.github/skills/SKILL.md' ]"
assert "saves .agents" "[ -d '${REAL_DEST}/.agents' ]"
assert "saves .claude/commands/cmd.md" "[ -f '${REAL_DEST}/.claude/commands/cmd.md' ]"
assert "saves Claude workflow script" "[ -f '${REAL_DEST}/.claude/workflows/review.js' ]"
assert "saves nested hidden workflow resource" "[ -f '${REAL_DEST}/.claude/workflows/references/.context' ]"
assert "saves AGENTS.md" "[ -f '${REAL_DEST}/AGENTS.md' ]"
assert "saves CLAUDE.md" "[ -f '${REAL_DEST}/CLAUDE.md' ]"
rm -rf "${TEST_WORKSPACE}" "${REAL_DEST}"
echo ""

# ── Test 2: Absent items are skipped without error ───────────────────────────
echo "Test 2: Only .github present → only .github saved, exits 0"
TEST_WORKSPACE=$(mktemp -d)
mkdir -p "${TEST_WORKSPACE}/.github/instructions"
echo "instructions" >"${TEST_WORKSPACE}/.github/instructions/README.md"
rm -rf "${REAL_DEST}"

GH_AW_AGENT_FOLDERS="${AGENT_FOLDERS}" GH_AW_AGENT_FILES="${AGENT_FILES}" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${SAVE_SCRIPT}" >/dev/null 2>&1
EXIT_CODE=$?

assert "exits 0" "[ ${EXIT_CODE} -eq 0 ]"
assert "saves .github" "[ -d '${REAL_DEST}/.github' ]"
assert ".agents not created when absent" "[ ! -d '${REAL_DEST}/.agents' ]"
assert ".claude not created when absent" "[ ! -d '${REAL_DEST}/.claude' ]"
assert "AGENTS.md not created when absent" "[ ! -f '${REAL_DEST}/AGENTS.md' ]"
rm -rf "${TEST_WORKSPACE}" "${REAL_DEST}"
echo ""

# ── Test 3: Empty env vars → nothing saved, exits 0 ─────────────────────────
echo "Test 3: Empty env vars → nothing saved, exits 0"
TEST_WORKSPACE=$(mktemp -d)
mkdir -p "${TEST_WORKSPACE}/.github"
rm -rf "${REAL_DEST}"

GH_AW_AGENT_FOLDERS="" GH_AW_AGENT_FILES="" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${SAVE_SCRIPT}" >/dev/null 2>&1
EXIT_CODE=$?

assert "exits 0 with empty env vars" "[ ${EXIT_CODE} -eq 0 ]"
assert "nothing saved when env vars empty" "[ ! -d '${REAL_DEST}' ]"
rm -rf "${TEST_WORKSPACE}" "${REAL_DEST}"
echo ""

# ── Test 4: Re-run clears stale snapshot (idempotent) ────────────────────────
echo "Test 4: Re-run overwrites stale snapshot (idempotent)"
TEST_WORKSPACE=$(mktemp -d)
mkdir -p "${TEST_WORKSPACE}/.github"
echo "new content" >"${TEST_WORKSPACE}/.github/new.md"
# Pre-create a stale snapshot with different content
mkdir -p "${REAL_DEST}/.github"
echo "stale content" >"${REAL_DEST}/.github/stale.md"

GH_AW_AGENT_FOLDERS="${AGENT_FOLDERS}" GH_AW_AGENT_FILES="${AGENT_FILES}" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${SAVE_SCRIPT}" >/dev/null 2>&1

assert "new file present after re-run" "[ -f '${REAL_DEST}/.github/new.md' ]"
assert "stale file removed on re-run" "[ ! -f '${REAL_DEST}/.github/stale.md' ]"
rm -rf "${TEST_WORKSPACE}" "${REAL_DEST}"
echo ""

# ── Test 5: Unset env vars → graceful no-op ──────────────────────────────────
echo "Test 5: Unset GH_AW_AGENT_FOLDERS/FILES → exits 0"
TEST_WORKSPACE=$(mktemp -d)
rm -rf "${REAL_DEST}"

EXIT_CODE=0
GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${SAVE_SCRIPT}" >/dev/null 2>&1 || EXIT_CODE=$?

assert "exits 0 when env vars unset" "[ ${EXIT_CODE} -eq 0 ]"
assert "nothing saved when env vars unset" "[ ! -d '${REAL_DEST}' ]"
rm -rf "${TEST_WORKSPACE}"
echo ""

# ── Summary ──────────────────────────────────────────────────────────────────
echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
