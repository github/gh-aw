#!/usr/bin/env bash
set +o histexpand

# Test script for create_gh_aw_tmp_dir.sh
# Run: bash create_gh_aw_tmp_dir_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/create_gh_aw_tmp_dir.sh"

TESTS_PASSED=0
TESTS_FAILED=0

cleanup() {
  # Restore /tmp/gh-aw to a clean, writable state before removing it.
  chmod -R u+rw /tmp/gh-aw 2>/dev/null || true
  rm -rf /tmp/gh-aw 2>/dev/null || true
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

echo "Testing create_gh_aw_tmp_dir.sh"
echo ""

# ── Test 1: Script syntax is valid ──────────────────────────────────────────
echo "Test 1: Script syntax is valid"
assert "script passes bash -n" "bash -n '${SCRIPT}'"
echo ""

# ── Test 2: Creates expected directories when starting clean ────────────────
echo "Test 2: Creates expected directories when starting clean"
rm -rf /tmp/gh-aw
bash "${SCRIPT}" >/dev/null 2>&1
assert "creates /tmp/gh-aw/agent" "[ -d /tmp/gh-aw/agent ]"
assert "creates /tmp/gh-aw/memory-validation" "[ -d /tmp/gh-aw/memory-validation ]"
assert "creates /tmp/gh-aw/sandbox/firewall/logs" "[ -d /tmp/gh-aw/sandbox/firewall/logs ]"
assert "creates /tmp/gh-aw/sandbox/firewall/audit" "[ -d /tmp/gh-aw/sandbox/firewall/audit ]"
echo ""

# ── Test 3: No-op when guarded dirs already exist and are writable ───────────
echo "Test 3: No-op (no reclaim message) when guarded dirs are already writable"
rm -rf /tmp/gh-aw
mkdir -p /tmp/gh-aw/memory-validation /tmp/gh-aw/sandbox/firewall/logs /tmp/gh-aw/sandbox/firewall/audit
set +e
OUTPUT="$(bash "${SCRIPT}" 2>&1)"
EXIT_CODE=$?
set -e
assert "exits 0 when guarded dirs are writable" "[ '${EXIT_CODE}' -eq 0 ]"
assert "does not print reclaim message" "! printf '%s' \"${OUTPUT}\" | grep -q 'Pre-flight'"
echo ""

# ── Test 4: Guard fires when firewall parent dir is not writable ─────────────
echo "Test 4: Guard fires when firewall dir is not writable"
rm -rf /tmp/gh-aw
mkdir -p /tmp/gh-aw/sandbox/firewall
chmod 000 /tmp/gh-aw/sandbox/firewall
set +e
OUTPUT="$(bash "${SCRIPT}" 2>&1)"
EXIT_CODE=$?
set -e
chmod u+rwx /tmp/gh-aw/sandbox/firewall 2>/dev/null || true
assert "prints Pre-flight reclaim message when parent is non-writable" "printf '%s' \"${OUTPUT}\" | grep -q 'Pre-flight'"
echo ""

# ── Test 5: Guard fires when subdirs are non-writable (parent is writable) ───
echo "Test 5: Guard fires when a firewall subdir is non-writable even if parent is writable"
rm -rf /tmp/gh-aw
mkdir -p /tmp/gh-aw/sandbox/firewall/logs /tmp/gh-aw/sandbox/firewall/audit
chmod 000 /tmp/gh-aw/sandbox/firewall/logs  # parent writable, subdir not
set +e
OUTPUT="$(bash "${SCRIPT}" 2>&1)"
EXIT_CODE=$?
set -e
chmod u+rwx /tmp/gh-aw/sandbox/firewall/logs 2>/dev/null || true
assert "prints Pre-flight reclaim message when subdir is non-writable" "printf '%s' \"${OUTPUT}\" | grep -q 'Pre-flight'"
echo ""

# ── Test 6: Guard fires when validation marker dir is not writable ───────────
echo "Test 6: Guard fires when validation marker dir is not writable"
rm -rf /tmp/gh-aw
mkdir -p /tmp/gh-aw/memory-validation
chmod 000 /tmp/gh-aw/memory-validation
set +e
OUTPUT="$(bash "${SCRIPT}" 2>&1)"
EXIT_CODE=$?
set -e
assert "validation marker dir is writable after reclaim" "[ -w /tmp/gh-aw/memory-validation ]"
chmod u+rwx /tmp/gh-aw/memory-validation 2>/dev/null || true
assert "exits 0 when validation marker dir is reclaimed" "[ '${EXIT_CODE}' -eq 0 ]"
assert "prints Pre-flight reclaim message when validation marker dir is non-writable" "printf '%s' \"${OUTPUT}\" | grep -q '/tmp/gh-aw/memory-validation'"
echo ""

echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
