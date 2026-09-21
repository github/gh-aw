#!/usr/bin/env bash
set +o histexpand

# Tests for restore_base_github_folders.sh
# Run: bash restore_base_github_folders_test.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESTORE_SCRIPT="${SCRIPT_DIR}/restore_base_github_folders.sh"

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

cleanup() {
  rm -rf "${TEST_WORKSPACE:-}" "/tmp/gh-aw/base"
}
trap cleanup EXIT

echo "Testing restore_base_github_folders.sh..."
echo ""

# ── Test 1: Snapshot present → restores folders, root files, removes .mcp.json
echo "Test 1: Snapshot present → restores folders, root files, removes .mcp.json"
TEST_WORKSPACE=$(mktemp -d)

# Base branch snapshot
mkdir -p /tmp/gh-aw/base/.github/skills
echo "trusted skill" >/tmp/gh-aw/base/.github/skills/SKILL.md
mkdir -p /tmp/gh-aw/base/.agents
echo "trusted agent" >/tmp/gh-aw/base/.agents/agent.md
echo "trusted agents" >/tmp/gh-aw/base/AGENTS.md
echo "trusted claude" >/tmp/gh-aw/base/CLAUDE.md

# PR-branch workspace (untrusted content)
mkdir -p "${TEST_WORKSPACE}/.github/skills"
echo "evil skill" >"${TEST_WORKSPACE}/.github/skills/SKILL.md"
mkdir -p "${TEST_WORKSPACE}/.agents"
echo "evil agent" >"${TEST_WORKSPACE}/.agents/agent.md"
echo "evil agents" >"${TEST_WORKSPACE}/AGENTS.md"
echo "evil claude" >"${TEST_WORKSPACE}/CLAUDE.md"
echo '{"mcpServers":{}}' >"${TEST_WORKSPACE}/.mcp.json"

GH_AW_AGENT_FOLDERS="${AGENT_FOLDERS}" GH_AW_AGENT_FILES="${AGENT_FILES}" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${RESTORE_SCRIPT}" >/dev/null 2>&1

assert ".github/skills/SKILL.md restored to trusted" "grep -q 'trusted skill' '${TEST_WORKSPACE}/.github/skills/SKILL.md'"
assert ".agents/agent.md restored to trusted" "grep -q 'trusted agent' '${TEST_WORKSPACE}/.agents/agent.md'"
assert "AGENTS.md restored to trusted" "grep -q 'trusted agents' '${TEST_WORKSPACE}/AGENTS.md'"
assert "CLAUDE.md restored to trusted" "grep -q 'trusted claude' '${TEST_WORKSPACE}/CLAUDE.md'"
assert ".mcp.json removed" "[ ! -f '${TEST_WORKSPACE}/.mcp.json' ]"
rm -rf "${TEST_WORKSPACE}" /tmp/gh-aw/base
echo ""

# ── Test 2: Engine-specific folders restored ─────────────────────────────────
echo "Test 2: Engine-specific .claude and .gemini folders restored"
TEST_WORKSPACE=$(mktemp -d)

mkdir -p /tmp/gh-aw/base/.claude/commands
echo "trusted cmd" >/tmp/gh-aw/base/.claude/commands/cmd.md
mkdir -p /tmp/gh-aw/base/.gemini
echo '{"trusted":true}' >/tmp/gh-aw/base/.gemini/settings.json

# PR-branch: evil versions
mkdir -p "${TEST_WORKSPACE}/.claude/commands"
echo "evil cmd" >"${TEST_WORKSPACE}/.claude/commands/cmd.md"
mkdir -p "${TEST_WORKSPACE}/.gemini"
echo '{"evil":true}' >"${TEST_WORKSPACE}/.gemini/settings.json"

GH_AW_AGENT_FOLDERS="${AGENT_FOLDERS}" GH_AW_AGENT_FILES="${AGENT_FILES}" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${RESTORE_SCRIPT}" >/dev/null 2>&1

assert ".claude/commands/cmd.md restored" "grep -q 'trusted cmd' '${TEST_WORKSPACE}/.claude/commands/cmd.md'"
assert ".gemini/settings.json restored" "grep -q 'trusted' '${TEST_WORKSPACE}/.gemini/settings.json'"
rm -rf "${TEST_WORKSPACE}" /tmp/gh-aw/base
echo ""

# ── Test 3: PR-injected folder not in base is removed ────────────────────────
echo "Test 3: PR-injected folder absent from base → removed from workspace"
TEST_WORKSPACE=$(mktemp -d)
rm -rf /tmp/gh-aw/base

# PR branch injected .claude but base has no .claude
mkdir -p "${TEST_WORKSPACE}/.claude"
echo "evil instructions" >"${TEST_WORKSPACE}/.claude/CLAUDE.md"
echo "evil agents" >"${TEST_WORKSPACE}/AGENTS.md"

GH_AW_AGENT_FOLDERS="${AGENT_FOLDERS}" GH_AW_AGENT_FILES="${AGENT_FILES}" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${RESTORE_SCRIPT}" >/dev/null 2>&1
EXIT_CODE=$?

assert "exits 0" "[ ${EXIT_CODE} -eq 0 ]"
assert ".claude removed (not in base)" "[ ! -d '${TEST_WORKSPACE}/.claude' ]"
assert "AGENTS.md removed (not in base)" "[ ! -f '${TEST_WORKSPACE}/AGENTS.md' ]"
rm -rf "${TEST_WORKSPACE}"
echo ""

# ── Test 4: Empty env vars → no folder operations, .mcp.json still removed ───
echo "Test 4: Empty env vars → no folder/file ops, .mcp.json still removed"
TEST_WORKSPACE=$(mktemp -d)
rm -rf /tmp/gh-aw/base
mkdir -p "${TEST_WORKSPACE}/.claude"
echo "evil" >"${TEST_WORKSPACE}/.claude/CLAUDE.md"
echo '{"evil":true}' >"${TEST_WORKSPACE}/.mcp.json"

GH_AW_AGENT_FOLDERS="" GH_AW_AGENT_FILES="" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${RESTORE_SCRIPT}" >/dev/null 2>&1
EXIT_CODE=$?

assert "exits 0" "[ ${EXIT_CODE} -eq 0 ]"
assert ".mcp.json removed even with empty env vars" "[ ! -f '${TEST_WORKSPACE}/.mcp.json' ]"
rm -rf "${TEST_WORKSPACE}"
echo ""

# ── Test 5: .mcp.json absent → exits 0 without error ─────────────────────────
echo "Test 5: No .mcp.json in workspace → exits 0"
TEST_WORKSPACE=$(mktemp -d)
rm -rf /tmp/gh-aw/base

EXIT_CODE=0
GH_AW_AGENT_FOLDERS="" GH_AW_AGENT_FILES="" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${RESTORE_SCRIPT}" >/dev/null 2>&1 || EXIT_CODE=$?

assert "exits 0 when .mcp.json absent" "[ ${EXIT_CODE} -eq 0 ]"
rm -rf "${TEST_WORKSPACE}"
echo ""

# ── Test 6: Partial snapshot → present items restored; absent PR items removed
echo "Test 6: Partial snapshot → present items restored; absent PR items removed"
TEST_WORKSPACE=$(mktemp -d)

# Base has .github but not .codex
mkdir -p /tmp/gh-aw/base/.github
echo "trusted" >/tmp/gh-aw/base/.github/trusted.md
# no .codex in base

# PR has both
mkdir -p "${TEST_WORKSPACE}/.github"
echo "evil" >"${TEST_WORKSPACE}/.github/evil.md"
mkdir -p "${TEST_WORKSPACE}/.codex"
echo "evil codex" >"${TEST_WORKSPACE}/.codex/config"

GH_AW_AGENT_FOLDERS="${AGENT_FOLDERS}" GH_AW_AGENT_FILES="${AGENT_FILES}" \
  GITHUB_WORKSPACE="${TEST_WORKSPACE}" bash "${RESTORE_SCRIPT}" >/dev/null 2>&1

assert ".github restored from base" "[ -f '${TEST_WORKSPACE}/.github/trusted.md' ]"
assert "evil .github file removed" "[ ! -f '${TEST_WORKSPACE}/.github/evil.md' ]"
assert ".codex removed (not in base)" "[ ! -d '${TEST_WORKSPACE}/.codex' ]"
rm -rf "${TEST_WORKSPACE}" /tmp/gh-aw/base
echo ""

# ── Summary ──────────────────────────────────────────────────────────────────
echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
