#!/usr/bin/env bash
set +o histexpand

# Tests for install_awf_binary.sh flag-parsing and variable-override logic.
# Run: bash install_awf_binary_test.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TESTS_PASSED=0
TESTS_FAILED=0

pass() { echo "PASS: $1"; TESTS_PASSED=$((TESTS_PASSED + 1)); }
fail() { echo "FAIL: $1"; echo "  $2"; TESTS_FAILED=$((TESTS_FAILED + 1)); }

# Inline the flag-parsing and directory-override block from install_awf_binary.sh
# in a subshell so we can vary the arguments without touching the real filesystem.
parse_and_override() {
  local args=("$@")
  bash -c '
    AWF_INSTALL_DIR="/usr/local/bin"
    AWF_LIB_DIR="/usr/local/lib/awf"
    ROOTLESS=false
    for arg in "${@:1}"; do
      case "$arg" in
        --rootless) ROOTLESS=true ;;
      esac
    done
    if [ "$ROOTLESS" = "true" ]; then
      AWF_USER_PREFIX="${HOME}/.local"
      AWF_INSTALL_DIR="${AWF_USER_PREFIX}/bin"
      AWF_LIB_DIR="${AWF_USER_PREFIX}/lib/awf"
    fi
    echo "AWF_INSTALL_DIR=${AWF_INSTALL_DIR}"
    echo "AWF_LIB_DIR=${AWF_LIB_DIR}"
  ' -- "${args[@]}"
}

echo "Running install_awf_binary.sh tests..."
echo

# Test 1: rootless flag sets AWF_INSTALL_DIR to $HOME/.local/bin
echo "Test 1: --rootless sets AWF_INSTALL_DIR to \$HOME/.local/bin..."
result=$(parse_and_override --rootless)
expected_install_dir="${HOME}/.local/bin"
if echo "$result" | grep -q "AWF_INSTALL_DIR=${expected_install_dir}"; then
  pass "AWF_INSTALL_DIR is ${expected_install_dir}"
else
  fail "AWF_INSTALL_DIR was not ${expected_install_dir}" "$result"
fi

# Test 2: rootless flag sets AWF_LIB_DIR to $HOME/.local/lib/awf
echo "Test 2: --rootless sets AWF_LIB_DIR to \$HOME/.local/lib/awf..."
expected_lib_dir="${HOME}/.local/lib/awf"
if echo "$result" | grep -q "AWF_LIB_DIR=${expected_lib_dir}"; then
  pass "AWF_LIB_DIR is ${expected_lib_dir}"
else
  fail "AWF_LIB_DIR was not ${expected_lib_dir}" "$result"
fi

# Test 3: without --rootless, AWF_INSTALL_DIR stays /usr/local/bin
echo "Test 3: without --rootless, AWF_INSTALL_DIR stays /usr/local/bin..."
result=$(parse_and_override)
if echo "$result" | grep -q "AWF_INSTALL_DIR=/usr/local/bin"; then
  pass "AWF_INSTALL_DIR is /usr/local/bin"
else
  fail "AWF_INSTALL_DIR was not /usr/local/bin" "$result"
fi

# Test 4: without --rootless, AWF_LIB_DIR stays /usr/local/lib/awf
echo "Test 4: without --rootless, AWF_LIB_DIR stays /usr/local/lib/awf..."
if echo "$result" | grep -q "AWF_LIB_DIR=/usr/local/lib/awf"; then
  pass "AWF_LIB_DIR is /usr/local/lib/awf"
else
  fail "AWF_LIB_DIR was not /usr/local/lib/awf" "$result"
fi

# Test 5: GITHUB_PATH export — install dir is written when GITHUB_PATH is set
echo "Test 5: AWF_INSTALL_DIR is appended to GITHUB_PATH in rootless mode..."
FAKE_GITHUB_PATH=$(mktemp)
bash -c '
  AWF_INSTALL_DIR="${HOME}/.local/bin"
  ROOTLESS=true
  GITHUB_PATH="'"${FAKE_GITHUB_PATH}"'"
  if [ "$ROOTLESS" = "true" ]; then
    if [ -n "${GITHUB_PATH:-}" ]; then
      echo "${AWF_INSTALL_DIR}" >> "${GITHUB_PATH}"
    else
      echo "WARNING: --rootless install complete but \$GITHUB_PATH is unset; add ${AWF_INSTALL_DIR} to PATH manually" >&2
    fi
  fi
'
expected_path_entry="${HOME}/.local/bin"
if grep -qF "${expected_path_entry}" "${FAKE_GITHUB_PATH}"; then
  pass "${expected_path_entry} written to GITHUB_PATH"
else
  fail "${expected_path_entry} not found in GITHUB_PATH" "$(cat "${FAKE_GITHUB_PATH}")"
fi
rm -f "${FAKE_GITHUB_PATH}"

# Test 6: --retry-all-errors is used only when curl supports it
echo "Test 6: --retry-all-errors is conditional on curl support..."
test_curl_retry_all_errors() {
  local curl_help="$1"
  local expected="$2"
  local help_all_fails="${3:-false}"
  local test_dir
  local actual
  TEST_FAILURE_REASON=""
  test_dir=$(mktemp -d)
  mkdir -p "${test_dir}/bin" "${test_dir}/home"
  cat > "${test_dir}/bin/curl" <<'EOF'
#!/usr/bin/env bash
if [ "$1" = "--help" ]; then
  if [ "$2" = "all" ] && [ "${CURL_HELP_ALL_FAILS}" = "true" ]; then
    exit 1
  fi
  printf '%s\n' "${CURL_HELP_OUTPUT}"
  exit 0
fi
printf '%s\n' "$@" >> "${CURL_ARGS_FILE}"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    output="$2"
    break
  fi
  shift
done
if [[ "${output}" == *checksums.txt ]]; then
  printf 'checksum awf-linux-x64\n' > "${output}"
else
  printf '#!/usr/bin/env bash\nexit 0\n' > "${output}"
fi
EOF
  cat > "${test_dir}/bin/node" <<'EOF'
#!/usr/bin/env bash
echo v18.0.0
EOF
  cat > "${test_dir}/bin/sha256sum" <<'EOF'
#!/usr/bin/env bash
printf 'checksum %s\n' "$1"
EOF
  cat > "${test_dir}/bin/uname" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  -s) echo Linux ;;
  -m) echo x86_64 ;;
esac
EOF
  chmod +x "${test_dir}/bin/"*
  CURL_HELP_OUTPUT="${curl_help}" CURL_HELP_ALL_FAILS="${help_all_fails}" CURL_ARGS_FILE="${test_dir}/curl-args" \
    HOME="${test_dir}/home" GITHUB_PATH="${test_dir}/github-path" \
    PATH="${test_dir}/bin:/usr/bin:/bin" bash "${SCRIPT_DIR}/install_awf_binary.sh" vtest --rootless >/dev/null
  if [ ! -f "${test_dir}/curl-args" ]; then
    TEST_FAILURE_REASON="installer did not invoke a download"
    rm -rf "${test_dir}"
    return 1
  fi
  if grep -qxF -- '--retry-all-errors' "${test_dir}/curl-args"; then
    actual=true
  else
    actual=false
  fi
  for retry_option in --retry 5 --retry-delay 10 --retry-max-time 180; do
    if ! grep -qxF -- "${retry_option}" "${test_dir}/curl-args"; then
      TEST_FAILURE_REASON="fixed retry option ${retry_option} was not passed to curl"
      rm -rf "${test_dir}"
      return 1
    fi
  done
  rm -rf "${test_dir}"
  if [ "${actual}" != "${expected}" ]; then
    TEST_FAILURE_REASON="expected --retry-all-errors=${expected}, got ${actual}"
    return 1
  fi
}

if test_curl_retry_all_errors '--retry-all-errors' true; then
  pass "supported curl receives --retry-all-errors"
else
  fail "supported curl retry options were incorrect" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '--retry-all-errors' true true; then
  pass "curl help fallback detects --retry-all-errors"
else
  fail "curl help fallback retry options were incorrect" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '' false; then
  pass "unsupported curl omits --retry-all-errors"
else
  fail "unsupported curl retry options were incorrect" "${TEST_FAILURE_REASON}"
fi

# Test 7: warning emitted when GITHUB_PATH is unset in rootless mode
echo "Test 7: warning emitted when GITHUB_PATH is unset in rootless mode..."
warning_output=$(bash -c '
  AWF_INSTALL_DIR="${HOME}/.local/bin"
  ROOTLESS=true
  unset GITHUB_PATH
  if [ "$ROOTLESS" = "true" ]; then
    if [ -n "${GITHUB_PATH:-}" ]; then
      echo "${AWF_INSTALL_DIR}" >> "${GITHUB_PATH}"
    else
      echo "WARNING: --rootless install complete but \$GITHUB_PATH is unset; add ${AWF_INSTALL_DIR} to PATH manually" >&2
    fi
  fi
' 2>&1)
if echo "$warning_output" | grep -q "WARNING"; then
  pass "WARNING emitted when GITHUB_PATH is unset"
else
  fail "No WARNING when GITHUB_PATH is unset" "$warning_output"
fi

echo
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
