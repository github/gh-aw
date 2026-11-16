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

echo "Test 6: CJS rejects Copilot write permission even with its built-in token..."
T6="$TMP_ROOT/t6"
T6_CJS="$T6/.github/workflows/cjs.yml"
write_workflow "$T6_CJS" "    permissions:
      contents: read
      copilot-requests: write
    steps:
      - run: echo ok
        env:
          GITHUB_TOKEN: \${{ github.token }}"
T6_OUT="$TMP_ROOT/t6-output.txt"
if (cd "$T6" && bash "$PURITY_SCRIPT" .github/workflows/cjs.yml >"$T6_OUT" 2>&1); then
  fail "CJS Copilot write permission should exit 1" "$(cat "$T6_OUT")"
elif grep -q "copilot-requests: write" "$T6_OUT"; then
  pass "CJS rejects Copilot write permission even with its built-in token"
else
  fail "CJS write permission output was incorrect" "$(cat "$T6_OUT")"
fi

echo "Test 7: non-selector CGO jobs still reject Copilot write permission..."
T7="$TMP_ROOT/t7"
T7_CGO="$T7/cgo.yml"
write_workflow "$T7_CGO" "    permissions: { contents: read, copilot-requests: write }
    steps:
      - run: echo ok"
T7_OUT="$TMP_ROOT/t7-output.txt"
if (cd "$T7" && bash "$PURITY_SCRIPT" cgo.yml >"$T7_OUT" 2>&1); then
  fail "CGO Copilot write permission should exit 1" "$(cat "$T7_OUT")"
elif grep -q "copilot-requests: write" "$T7_OUT"; then
  pass "CGO still rejects Copilot write permission"
else
  fail "CGO write permission output was incorrect" "$(cat "$T7_OUT")"
fi

echo "Test 8: CJS rejects repository writes and PAT secrets..."
T8="$TMP_ROOT/t8"
T8_CJS="$T8/.github/workflows/cjs.yml"
write_workflow "$T8_CJS" "    permissions:
      copilot-requests: write
      contents: write
    steps:
      - run: echo \"\${{ secrets.COPILOT_GITHUB_TOKEN }}\""
T8_OUT="$TMP_ROOT/t8-output.txt"
if (cd "$T8" && bash "$PURITY_SCRIPT" .github/workflows/cjs.yml >"$T8_OUT" 2>&1); then
  fail "CJS repository writes and PAT secrets should exit 1" "$(cat "$T8_OUT")"
elif grep -q "contents: write" "$T8_OUT" && grep -q "secrets.COPILOT_GITHUB_TOKEN" "$T8_OUT"; then
  pass "CJS rejects repository writes and PAT secrets"
else
  fail "CJS repository write and secret output was incorrect" "$(cat "$T8_OUT")"
fi

echo "Test 9: other CJS paths reject Copilot write permission..."
T9="$TMP_ROOT/t9"
write_workflow "$T9/cjs.yml" "    permissions:
      copilot-requests: write
    steps:
      - run: echo ok"
write_workflow "$T9/other/cjs.yml" "    permissions: { contents: read, copilot-requests: write }
    steps:
      - run: echo ok"
T9_OUT="$TMP_ROOT/t9-output.txt"
if (cd "$T9" && bash "$PURITY_SCRIPT" cjs.yml other/cjs.yml >"$T9_OUT" 2>&1); then
  fail "other CJS paths should exit 1" "$(cat "$T9_OUT")"
elif grep -q "^cjs.yml:.*copilot-requests: write" "$T9_OUT" && grep -q "^other/cjs.yml:.*copilot-requests: write" "$T9_OUT"; then
  pass "other CJS paths reject Copilot write permission"
else
  fail "other CJS path permission output was incorrect" "$(cat "$T9_OUT")"
fi

echo "Test 10: only the dedicated CGO selector can request Copilot inference..."
T10="$TMP_ROOT/t10"
for style in block flow; do
  T10_DIR="$T10/$style"
  mkdir -p "$T10_DIR"
  if [ "$style" = block ]; then
    body=$'    permissions:\n      contents: read\n      copilot-requests: write'
  else
    body='    permissions: { contents: read, copilot-requests: write }'
  fi
  write_workflow "$T10_DIR/cgo.yml" "$body"
  sed 's/^  test:/  front-door-decision:/' "$T10_DIR/cgo.yml" > "$T10_DIR/selector.yml"
  mv "$T10_DIR/selector.yml" "$T10_DIR/cgo.yml"
  T10_OUT="$TMP_ROOT/t10-$style-output.txt"
  if (cd "$T10_DIR" && bash "$PURITY_SCRIPT" cgo.yml >"$T10_OUT" 2>&1); then
    pass "CGO dedicated selector permits $style inference permission"
  else
    fail "CGO selector inference permission should pass" "$(cat "$T10_OUT")"
  fi
  cp "$T10_DIR/cgo.yml" "$T10_DIR/cjs.yml"
  if (cd "$T10_DIR" && bash "$PURITY_SCRIPT" cjs.yml >"$T10_OUT" 2>&1); then
    fail "CJS selector must still reject inference permission" "$(cat "$T10_OUT")"
  else
    pass "CJS selector rejects $style inference permission"
  fi
done

echo "Test 11: workflow-wide Copilot permission remains forbidden..."
T11="$TMP_ROOT/t11"
mkdir -p "$T11"
printf 'name: Test\non: push\npermissions: { copilot-requests: write }\njobs:\n  checkout-cache:\n    runs-on: ubuntu-latest\n' > "$T11/cgo.yml"
T11_OUT="$TMP_ROOT/t11-output.txt"
if (cd "$T11" && bash "$PURITY_SCRIPT" cgo.yml >"$T11_OUT" 2>&1); then
  fail "workflow-wide inference permission should fail" "$(cat "$T11_OUT")"
else
  pass "workflow-wide Copilot permission remains forbidden"
fi

echo "Test 12: the broad checkout-cache job cannot request Copilot inference..."
T12="$TMP_ROOT/t12"
mkdir -p "$T12"
for style in block flow; do
  if [ "$style" = block ]; then
    body=$'    permissions:\n      contents: read\n      copilot-requests: write'
  else
    body='    permissions: { contents: read, copilot-requests: write }'
  fi
  write_workflow "$T12/source.yml" "$body"
  sed 's/^  test:/  checkout-cache:/' "$T12/source.yml" > "$T12/cgo.yml"
  T12_OUT="$TMP_ROOT/t12-$style-output.txt"
  if (cd "$T12" && bash "$PURITY_SCRIPT" cgo.yml >"$T12_OUT" 2>&1); then
    fail "checkout-cache inference permission should fail" "$(cat "$T12_OUT")"
  elif grep -q "copilot-requests: write" "$T12_OUT"; then
    pass "checkout-cache rejects $style inference permission"
  else
    fail "checkout-cache permission output was incorrect" "$(cat "$T12_OUT")"
  fi
done

echo
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
