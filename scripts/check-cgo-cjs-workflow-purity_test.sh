#!/bin/bash
set +o histexpand

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PURITY_SCRIPT="$SCRIPT_DIR/check-cgo-cjs-workflow-purity.sh"

TESTS_PASSED=0
TESTS_FAILED=0

pass() { echo "PASS: $1"; TESTS_PASSED=$((TESTS_PASSED + 1)); }
fail() { echo "FAIL: $1"; echo "  $2"; TESTS_FAILED=$((TESTS_FAILED + 1)); }

write_workflow() {
  local path="$1"
  local body="$2"
  mkdir -p "$(dirname "$path")"
  cat > "$path" <<EOF
name: Test
on: push
jobs:
  test:
    runs-on: ubuntu-latest
$body
EOF
}

echo "Running check-cgo-cjs-workflow-purity.sh tests..."
echo

TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT

echo "Test 1: allowed GITHUB_TOKEN syntax and actions write permission pass..."
T1="$TMP_ROOT/t1"
T1_CGO="$T1/.github/workflows/cgo.yml"
T1_CJS="$T1/.github/workflows/cjs.yml"
T1_ACTIONS_WRITE="$T1/.github/workflows/actions-write.yml"
write_workflow "$T1_CGO" "    permissions:
      contents: read
    steps:
      - run: echo \"\${{ secrets.GITHUB_TOKEN }} \${{ condition && secrets['GITHUB_TOKEN'] }}\""
write_workflow "$T1_CJS" $'    permissions: { contents: read, actions: read }\n    steps:\n      - run: echo "${{ secrets["GITHUB_TOKEN"] }}"'
write_workflow "$T1_ACTIONS_WRITE" "    permissions:
      actions: write
    steps:
      - run: echo ok"
T1_OUT="$TMP_ROOT/t1-output.txt"
if (cd "$T1" && bash "$PURITY_SCRIPT" >"$T1_OUT" 2>&1); then
  pass "allowed GITHUB_TOKEN syntax and actions write permission pass"
else
  fail "allowed secrets and actions write permission should pass" "$(cat "$T1_OUT")"
fi

echo "Test 2: forbidden nested and bracket secrets fail..."
T2="$TMP_ROOT/t2"
T2_CGO="$T2/cgo.yml"
T2_CJS="$T2/cjs.yml"
write_workflow "$T2_CGO" "    permissions:
      contents: read
    steps:
      - run: echo \"\${{ condition && secrets.DEPLOY_KEY }} \${{ condition && secrets.SCIENCE }}\""
write_workflow "$T2_CJS" $'    permissions:\n      contents: read\n    steps:\n      - run: echo "${{ secrets[\'PROD_TOKEN\'] }} ${{ secrets["PROD_TOKEN_2"] }}"'
T2_OUT="$TMP_ROOT/t2-output.txt"
if (cd "$T2" && bash "$PURITY_SCRIPT" cgo.yml cjs.yml >"$T2_OUT" 2>&1); then
  fail "forbidden secrets should exit 1" "$(cat "$T2_OUT")"
elif grep -q "secrets.DEPLOY_KEY" "$T2_OUT" && grep -q "secrets.SCIENCE" "$T2_OUT" && grep -q "PROD_TOKEN" "$T2_OUT" && grep -q "PROD_TOKEN_2" "$T2_OUT"; then
  pass "forbidden nested and bracket secrets fail"
else
  fail "forbidden secret output was incorrect" "$(cat "$T2_OUT")"
fi

echo "Test 3: computed secret keys fail..."
T3="$TMP_ROOT/t3"
T3_CGO="$T3/cgo.yml"
write_workflow "$T3_CGO" "    permissions:
      contents: read
    steps:
      - run: echo \"\${{ secrets[matrix.secret_name] }}\""
T3_OUT="$TMP_ROOT/t3-output.txt"
if (cd "$T3" && bash "$PURITY_SCRIPT" cgo.yml >"$T3_OUT" 2>&1); then
  fail "computed secret key should exit 1" "$(cat "$T3_OUT")"
elif grep -q "computed secrets key" "$T3_OUT"; then
  pass "computed secret keys fail"
else
  fail "computed secret output was incorrect" "$(cat "$T3_OUT")"
fi

echo "Test 4: block, flow, and quoted write permissions fail..."
T4="$TMP_ROOT/t4"
T4_CGO="$T4/block.yml"
T4_CJS="$T4/flow.yml"
T4_SCALAR="$T4/scalar.yml"
write_workflow "$T4_CGO" "    permissions:
      contents: \"write\"
    steps:
      - run: echo ok"
write_workflow "$T4_CJS" "    permissions: { contents: read, issues: 'write' }
    steps:
      - run: echo ok"
write_workflow "$T4_SCALAR" "    permissions: 'write-all'
    steps:
      - run: echo ok"
T4_OUT="$TMP_ROOT/t4-output.txt"
if (cd "$T4" && bash "$PURITY_SCRIPT" block.yml flow.yml scalar.yml >"$T4_OUT" 2>&1); then
  fail "write permissions should exit 1" "$(cat "$T4_OUT")"
elif grep -q "contents: \"write\"" "$T4_OUT" && grep -q "issues: 'write'" "$T4_OUT" && grep -q "write-all" "$T4_OUT"; then
  pass "block, flow, and quoted write permissions fail"
else
  fail "write permission output was incorrect" "$(cat "$T4_OUT")"
fi

echo "Test 5: multiple inputs report missing files while scanning existing files..."
T5="$TMP_ROOT/t5"
T5_CGO="$T5/cgo.yml"
write_workflow "$T5_CGO" "    permissions:
      contents: read
    steps:
      - run: echo \"\${{ secrets.DEPLOY_KEY }}\""
T5_OUT="$TMP_ROOT/t5-output.txt"
if (cd "$T5" && bash "$PURITY_SCRIPT" cgo.yml missing.yml >"$T5_OUT" 2>&1); then
  fail "missing file and forbidden secret should exit 1" "$(cat "$T5_OUT")"
elif grep -q "Missing workflow file: missing.yml" "$T5_OUT" && grep -q "secrets.DEPLOY_KEY" "$T5_OUT"; then
  pass "multiple inputs report missing files while scanning existing files"
else
  fail "multiple input output was incorrect" "$(cat "$T5_OUT")"
fi

echo
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
