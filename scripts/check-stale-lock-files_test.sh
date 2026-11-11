#!/bin/bash
set +o histexpand

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STALE_SCRIPT="$SCRIPT_DIR/check-stale-lock-files.sh"

TESTS_PASSED=0
TESTS_FAILED=0

pass() { echo "PASS: $1"; TESTS_PASSED=$((TESTS_PASSED + 1)); }
fail() { echo "FAIL: $1"; echo "  $2"; TESTS_FAILED=$((TESTS_FAILED + 1)); }

# Initialise a minimal git repo and commit a matching .md/.lock.yml pair.
# All further per-test modifications are made in the working tree (not staged)
# so that git diff HEAD reports them as modified.
create_fixture_repo() {
    local repo_dir="$1"
    local base="${2:-example}"

    git -C "$repo_dir" init -q
    git -C "$repo_dir" config user.email "test@test.com"
    git -C "$repo_dir" config user.name "Test"

    mkdir -p "$repo_dir/.github/workflows"
    printf '%s\n' "# $base workflow" > "$repo_dir/.github/workflows/$base.md"
    printf '%s\n' "lock: original"   > "$repo_dir/.github/workflows/$base.lock.yml"
    git -C "$repo_dir" add .
    git -C "$repo_dir" commit -q -m "initial commit"
}

echo "Running check-stale-lock-files.sh tests..."
echo

TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT

# ---------------------------------------------------------------------------
# Test 1: no modified .md files — should exit 0 (clean working tree).
# ---------------------------------------------------------------------------
echo "Test 1: clean working tree exits 0..."
T1="$TMP_ROOT/t1"
mkdir -p "$T1"
create_fixture_repo "$T1"
T1_OUT="$TMP_ROOT/t1-output.txt"
if (cd "$T1" && bash "$STALE_SCRIPT" >"$T1_OUT" 2>&1); then
    pass "clean working tree exits 0"
else
    fail "clean working tree should exit 0" "$(cat "$T1_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 2: .md modified without recompiling .lock.yml — should exit 1 and
# name the file.
# ---------------------------------------------------------------------------
echo "Test 2: stale .md exits 1 and names the file..."
T2="$TMP_ROOT/t2"
mkdir -p "$T2"
create_fixture_repo "$T2" "stale-workflow"
# Modify the .md without touching the .lock.yml
printf '%s\n' "# stale-workflow (edited)" > "$T2/.github/workflows/stale-workflow.md"
T2_OUT="$TMP_ROOT/t2-output.txt"
if (cd "$T2" && bash "$STALE_SCRIPT" >"$T2_OUT" 2>&1); then
    fail "stale .md should exit 1" "$(cat "$T2_OUT")"
elif grep -q "stale-workflow.md" "$T2_OUT"; then
    pass "stale .md exits 1 and names the file"
else
    fail "stale .md output did not name the stale file" "$(cat "$T2_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 3: both .md and .lock.yml modified — should exit 0 (recompile was run).
# ---------------------------------------------------------------------------
echo "Test 3: both files modified exits 0..."
T3="$TMP_ROOT/t3"
mkdir -p "$T3"
create_fixture_repo "$T3" "recompiled-workflow"
printf '%s\n' "# recompiled-workflow (edited)" > "$T3/.github/workflows/recompiled-workflow.md"
printf '%s\n' "lock: updated"                  > "$T3/.github/workflows/recompiled-workflow.lock.yml"
T3_OUT="$TMP_ROOT/t3-output.txt"
if (cd "$T3" && bash "$STALE_SCRIPT" >"$T3_OUT" 2>&1); then
    pass "both files modified exits 0"
else
    fail "both files modified should exit 0" "$(cat "$T3_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 4: .md modified, .lock.yml missing — should exit 1 and name the .md.
# ---------------------------------------------------------------------------
echo "Test 4: missing .lock.yml exits 1 and names the .md file..."
T4="$TMP_ROOT/t4"
mkdir -p "$T4"
create_fixture_repo "$T4" "no-lock"
# Remove the lock file so it appears missing AND edit the .md
rm "$T4/.github/workflows/no-lock.lock.yml"
printf '%s\n' "# no-lock (edited)" > "$T4/.github/workflows/no-lock.md"
T4_OUT="$TMP_ROOT/t4-output.txt"
if (cd "$T4" && bash "$STALE_SCRIPT" >"$T4_OUT" 2>&1); then
    fail "missing lock should exit 1" "$(cat "$T4_OUT")"
elif grep -q "no-lock.md" "$T4_OUT"; then
    pass "missing .lock.yml exits 1 and names the .md file"
else
    fail "missing .lock.yml output did not name the .md file" "$(cat "$T4_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 5: mixed — one stale, one up to date — only the stale one is reported.
# ---------------------------------------------------------------------------
echo "Test 5: mixed pair only reports the stale file..."
T5="$TMP_ROOT/t5"
mkdir -p "$T5"
create_fixture_repo "$T5" "ok-workflow"
# Add a second workflow pair in the same commit
printf '%s\n' "# stale-workflow" > "$T5/.github/workflows/stale-workflow.md"
printf '%s\n' "lock: original"   > "$T5/.github/workflows/stale-workflow.lock.yml"
git -C "$T5" add .
git -C "$T5" commit -q -m "add stale-workflow"
# Modify only the stale one's .md
printf '%s\n' "# stale-workflow (edited)" > "$T5/.github/workflows/stale-workflow.md"
T5_OUT="$TMP_ROOT/t5-output.txt"
if (cd "$T5" && bash "$STALE_SCRIPT" >"$T5_OUT" 2>&1); then
    fail "mixed pair should exit 1" "$(cat "$T5_OUT")"
elif grep -q "stale-workflow.md" "$T5_OUT" && ! grep -q "ok-workflow.md" "$T5_OUT"; then
    pass "mixed pair only reports the stale file"
else
    fail "mixed pair reported unexpected files" "$(cat "$T5_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 6: remediation message includes 'make recompile'.
# ---------------------------------------------------------------------------
echo "Test 6: remediation message mentions 'make recompile'..."
T6="$TMP_ROOT/t6"
mkdir -p "$T6"
create_fixture_repo "$T6" "stale-workflow"
printf '%s\n' "# stale-workflow (edited)" > "$T6/.github/workflows/stale-workflow.md"
T6_OUT="$TMP_ROOT/t6-output.txt"
(cd "$T6" && bash "$STALE_SCRIPT" >"$T6_OUT" 2>&1) || true
if grep -Fq "make recompile" "$T6_OUT" && grep -Fq ".github/workflows/*.md" "$T6_OUT"; then
    pass "remediation message mentions 'make recompile' and workflow markdown edits"
else
    fail "remediation message did not mention the workflow markdown reminder" "$(cat "$T6_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 7: --dir flag points to a custom directory.
# ---------------------------------------------------------------------------
echo "Test 7: --dir flag works with a custom directory..."
T7="$TMP_ROOT/t7"
mkdir -p "$T7"
git -C "$T7" init -q
git -C "$T7" config user.email "test@test.com"
git -C "$T7" config user.name "Test"
mkdir -p "$T7/custom/workflows"
printf '%s\n' "# custom-workflow"  > "$T7/custom/workflows/custom-workflow.md"
printf '%s\n' "lock: original"     > "$T7/custom/workflows/custom-workflow.lock.yml"
git -C "$T7" add .
git -C "$T7" commit -q -m "initial"
printf '%s\n' "# custom-workflow (edited)" > "$T7/custom/workflows/custom-workflow.md"
T7_OUT="$TMP_ROOT/t7-output.txt"
if (cd "$T7" && bash "$STALE_SCRIPT" --dir custom/workflows >"$T7_OUT" 2>&1); then
    fail "custom --dir with stale file should exit 1" "$(cat "$T7_OUT")"
elif grep -q "custom-workflow.md" "$T7_OUT"; then
    pass "--dir flag works with a custom directory"
else
    fail "--dir output did not name the stale file" "$(cat "$T7_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 8: missing workflows directory is an error.
# ---------------------------------------------------------------------------
echo "Test 8: missing workflows directory exits 1 with an error..."
T8="$TMP_ROOT/t8"
mkdir -p "$T8"
git -C "$T8" init -q
T8_OUT="$TMP_ROOT/t8-output.txt"
if (cd "$T8" && bash "$STALE_SCRIPT" >"$T8_OUT" 2>&1); then
    fail "missing directory should exit 1" "$(cat "$T8_OUT")"
elif grep -qi "not found\|no such" "$T8_OUT"; then
    pass "missing workflows directory exits with an error"
else
    fail "missing directory error message was unexpected" "$(cat "$T8_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 9: --base-ref detects stale committed markdown changes on clean tree.
# ---------------------------------------------------------------------------
echo "Test 9: --base-ref catches stale committed markdown changes..."
T9="$TMP_ROOT/t9"
mkdir -p "$T9"
create_fixture_repo "$T9" "base-ref-workflow"
T9_BASE_COMMIT=$(git -C "$T9" rev-parse HEAD)
git -C "$T9" checkout -q -b feature
printf '%s\n' "# base-ref-workflow (edited)" > "$T9/.github/workflows/base-ref-workflow.md"
git -C "$T9" add .github/workflows/base-ref-workflow.md
git -C "$T9" commit -q -m "edit workflow markdown only"
T9_OUT="$TMP_ROOT/t9-output.txt"
if (cd "$T9" && bash "$STALE_SCRIPT" --base-ref "$T9_BASE_COMMIT" >"$T9_OUT" 2>&1); then
    fail "--base-ref stale check should exit 1" "$(cat "$T9_OUT")"
elif grep -q "base-ref-workflow.md" "$T9_OUT"; then
    pass "--base-ref catches stale committed markdown changes"
else
    fail "--base-ref stale output did not name the .md file" "$(cat "$T9_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 10: --base-ref passes when markdown + lock were both committed.
# ---------------------------------------------------------------------------
echo "Test 10: --base-ref passes when markdown + lock both changed..."
T10="$TMP_ROOT/t10"
mkdir -p "$T10"
create_fixture_repo "$T10" "base-ref-ok"
T10_BASE_COMMIT=$(git -C "$T10" rev-parse HEAD)
git -C "$T10" checkout -q -b feature
printf '%s\n' "# base-ref-ok (edited)" > "$T10/.github/workflows/base-ref-ok.md"
printf '%s\n' "lock: updated"          > "$T10/.github/workflows/base-ref-ok.lock.yml"
git -C "$T10" add .github/workflows/base-ref-ok.md .github/workflows/base-ref-ok.lock.yml
git -C "$T10" commit -q -m "edit workflow markdown and lock"
T10_OUT="$TMP_ROOT/t10-output.txt"
if (cd "$T10" && bash "$STALE_SCRIPT" --base-ref "$T10_BASE_COMMIT" >"$T10_OUT" 2>&1); then
    pass "--base-ref passes when markdown + lock both changed"
else
    fail "--base-ref should pass when markdown + lock changed" "$(cat "$T10_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 11: .md files in excluded shared/ subdirectory are not flagged.
# ---------------------------------------------------------------------------
echo "Test 11: .md files in shared/ subdirectory are excluded..."
T11="$TMP_ROOT/t11"
mkdir -p "$T11"
git -C "$T11" init -q
git -C "$T11" config user.email "test@test.com"
git -C "$T11" config user.name "Test"
mkdir -p "$T11/.github/workflows/shared"
printf '%s\n' "# shared-tool" > "$T11/.github/workflows/shared/tools.md"
git -C "$T11" add .
git -C "$T11" commit -q -m "initial"
printf '%s\n' "# shared-tool (edited)" > "$T11/.github/workflows/shared/tools.md"
T11_OUT="$TMP_ROOT/t11-output.txt"
if (cd "$T11" && bash "$STALE_SCRIPT" >"$T11_OUT" 2>&1); then
    pass "shared/ .md files are excluded (exits 0)"
else
    fail "shared/ .md files should be excluded (should exit 0)" "$(cat "$T11_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 12: .md files in excluded skills/ subdirectory are not flagged.
# ---------------------------------------------------------------------------
echo "Test 12: .md files in skills/ subdirectory are excluded..."
T12="$TMP_ROOT/t12"
mkdir -p "$T12"
git -C "$T12" init -q
git -C "$T12" config user.email "test@test.com"
git -C "$T12" config user.name "Test"
mkdir -p "$T12/.github/workflows/skills"
printf '%s\n' "# skill-doc" > "$T12/.github/workflows/skills/example.md"
git -C "$T12" add .
git -C "$T12" commit -q -m "initial"
printf '%s\n' "# skill-doc (edited)" > "$T12/.github/workflows/skills/example.md"
T12_OUT="$TMP_ROOT/t12-output.txt"
if (cd "$T12" && bash "$STALE_SCRIPT" >"$T12_OUT" 2>&1); then
    pass "skills/ .md files are excluded (exits 0)"
else
    fail "skills/ .md files should be excluded (should exit 0)" "$(cat "$T12_OUT")"
fi

# ---------------------------------------------------------------------------
# Test 13: unknown --base-ref falls back to working-tree diff and still
# catches a stale .md in the working tree.
# ---------------------------------------------------------------------------
echo "Test 13: unknown --base-ref falls back gracefully and still catches stale .md..."
T13="$TMP_ROOT/t13"
mkdir -p "$T13"
create_fixture_repo "$T13" "fallback-workflow"
# Modify the .md in the working tree (not committed)
printf '%s\n' "# fallback-workflow (edited)" > "$T13/.github/workflows/fallback-workflow.md"
T13_OUT="$TMP_ROOT/t13-output.txt"
if (cd "$T13" && bash "$STALE_SCRIPT" --base-ref "nonexistent-sha-12345" >"$T13_OUT" 2>&1); then
    fail "unknown base-ref with stale .md should exit 1" "$(cat "$T13_OUT")"
elif grep -q "fallback-workflow.md" "$T13_OUT"; then
    pass "unknown --base-ref falls back gracefully and catches stale .md"
else
    fail "unknown --base-ref fallback output did not name the stale file" "$(cat "$T13_OUT")"
fi

echo "Test 14: connector declarations do not require standalone locks..."
T14="$TMP_ROOT/t14"
mkdir -p "$T14"
create_fixture_repo "$T14" "consumer"
mkdir -p "$T14/.github/workflows/connectors"
printf '%s\n' "---" "connectors:" "  example:" "    experimental: true" "---" > "$T14/.github/workflows/connectors/example.md"
git -C "$T14" add .
git -C "$T14" commit -q -m "add connector declaration"
printf '%s\n' "# declaration documentation" >> "$T14/.github/workflows/connectors/example.md"
T14_OUT="$TMP_ROOT/t14-output.txt"
if (cd "$T14" && bash "$STALE_SCRIPT" >"$T14_OUT" 2>&1); then
    pass "connectors/ declarations are excluded"
else
    fail "connector declarations should not need standalone locks" "$(cat "$T14_OUT")"
fi

echo "Test 15: a changed connector does not hide a stale consumer workflow..."
printf '%s\n' "# consumer (edited)" > "$T14/.github/workflows/consumer.md"
if (cd "$T14" && bash "$STALE_SCRIPT" >"$T14_OUT" 2>&1); then
    fail "stale consumer should still fail" "$(cat "$T14_OUT")"
elif grep -q "consumer.md" "$T14_OUT" && ! grep -q "connectors/example.md" "$T14_OUT"; then
    pass "stale consumer is still reported"
else
    fail "stale consumer output named unexpected files" "$(cat "$T14_OUT")"
fi

echo
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [ "$TESTS_FAILED" -gt 0 ]; then
    exit 1
fi

echo "✓ All tests passed!"
