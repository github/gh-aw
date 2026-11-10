#!/usr/bin/env bash
set +o histexpand

# Tests for install_awf_binary.sh flags, install paths, and bounded download retries.
# Run: bash install_awf_binary_test.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=actions/setup/sh/release_integrity.sh
source "${SCRIPT_DIR}/release_integrity.sh"

TESTS_PASSED=0
TESTS_FAILED=0
TEST_ROOT="${PWD}/.awf-installer-test-$$-${RANDOM}"
mkdir -m 700 "$TEST_ROOT"
trap 'rm -rf "$TEST_ROOT"' EXIT

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
FAKE_GITHUB_PATH="${TEST_ROOT}/github-path"
: > "$FAKE_GITHUB_PATH"
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
  local curl_help_all="$1"
  local expected="$2"
  local help_all_fails="${3:-false}"
  local curl_help="${4:-${curl_help_all}}"
  local node_version="${5:-v18.0.0}"
  local platform="${6:-Linux}"
  local release_version="${7:-v0.28.62}"
  local attestation_failure="${8:-}"
  local identity_version="${9:-$release_version}"
  local curl_failure_asset="${10:-}"
  local expect_download_failure="${11:-false}"
  local test_dir
  local option_index
  local -a actual_options
  local -a common_options=(-fsSL --proto '=https' --proto-redir '=https' --retry 8 --retry-max-time 180 --connect-timeout 15)
  local -a expected_options
  TEST_FAILURE_REASON=""
  test_dir="${TEST_ROOT}/case-${RANDOM}"
  mkdir -m 700 "$test_dir"
  mkdir -p "${test_dir}/bin" "${test_dir}/home"
  cat > "${test_dir}/bin/curl" <<'EOF'
#!/usr/bin/env bash
if [ "$1" = "--help" ]; then
  if [ "$2" = "all" ] && [ "${CURL_HELP_ALL_FAILS}" = "true" ]; then
    exit 1
  fi
  if [ "$2" = "all" ]; then
    printf '%s\n' "${CURL_HELP_ALL_OUTPUT}"
  else
    printf '%s\n' "${CURL_HELP_OUTPUT}"
  fi
  exit 0
fi
count_file="${CURL_ARGS_FILE}.count"
count=$(cat "${count_file}" 2>/dev/null || echo 0)
count=$((count + 1))
printf '%s\n' "${count}" > "${count_file}"
printf '%s\n' "$@" > "${CURL_ARGS_FILE}.${count}"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    output="$2"
    break
  fi
  shift
done
case "${output:-}" in
  */"${AWF_TEST_CURL_FAILURE_ASSET:-__never__}")
    echo "simulated curl network failure" >&2
    exit 7
    ;;
esac
if [[ "${output}" == *checksums.txt ]]; then
  printf '%064d awf-linux-x64\n%064d awf-darwin-x64\n%064d awf-bundle.js\n' 0 0 0 > "${output}"
elif [[ "${output}" == *release-identity.txt ]]; then
  printf '%s  refs/tags/%s\n' "$AWF_TEST_MANIFEST_SHA256" "$AWF_TEST_IDENTITY_VERSION" > "$output"
else
  printf '#!/usr/bin/env bash\nexit 0\n' > "${output}"
fi
EOF
  cat > "${test_dir}/bin/gh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$AWF_TEST_ATTESTATION_LOG"
test "$1" = attestation && test "$2" = verify || exit 91
test "$4" = --hostname && test "$5" = github.com || exit 92
test "$6" = --repo && test "$7" = github/gh-aw-firewall || exit 93
test "$8" = --signer-workflow && test "$9" = github/gh-aw-firewall/.github/workflows/release.yml || exit 94
test "${10}" = --cert-oidc-issuer && test "${11}" = https://token.actions.githubusercontent.com || exit 95
test "${12}" = --deny-self-hosted-runners || exit 96
test "${3##*/}" != "${AWF_TEST_ATTESTATION_FAILURE:-}"
EOF
  cat > "${test_dir}/bin/node" <<'EOF'
#!/usr/bin/env bash
echo "$AWF_TEST_NODE_VERSION"
EOF
  cat > "${test_dir}/bin/sha256sum" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == *checksums.txt ]]; then
  printf '%s %s\n' "$AWF_TEST_MANIFEST_SHA256" "$1"
else
  printf '%064d %s\n' 0 "$1"
fi
EOF
  cat > "${test_dir}/bin/uname" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  -s) echo "$AWF_TEST_OS" ;;
  -m) echo x86_64 ;;
esac
EOF
  printf '#!/usr/bin/env bash\nexit 0\n' >"${test_dir}/bin/sudo"
  printf '#!/usr/bin/env bash\nexit 0\n' >"${test_dir}/bin/find"
  chmod +x "${test_dir}/bin/"*
  local installer_exit=0
  AWF_TEST_MANIFEST_SHA256="$(release_artifact_sha256 github/gh-aw-firewall "$release_version" checksums.txt)" \
    CURL_HELP_ALL_OUTPUT="${curl_help_all}" CURL_HELP_OUTPUT="${curl_help}" CURL_HELP_ALL_FAILS="${help_all_fails}" CURL_ARGS_FILE="${test_dir}/curl-args" \
    AWF_TEST_ATTESTATION_LOG="${test_dir}/attestations" AWF_TEST_ATTESTATION_FAILURE="$attestation_failure" AWF_TEST_IDENTITY_VERSION="$identity_version" \
    AWF_TEST_CURL_FAILURE_ASSET="$curl_failure_asset" \
    AWF_TEST_NODE_VERSION="$node_version" AWF_TEST_OS="$platform" HOME="${test_dir}/home" GITHUB_PATH="${test_dir}/github-path" \
    RUNNER_TEMP="$test_dir" PATH="${test_dir}/bin:${PATH}" bash "${SCRIPT_DIR}/install_awf_binary.sh" "$release_version" --rootless >"${test_dir}/output" 2>&1 || installer_exit=$?
  if [ "$expect_download_failure" = true ]; then
    if [ "$installer_exit" -eq 0 ]; then
      TEST_FAILURE_REASON="installer succeeded after a required download failed"
      rm -rf "$test_dir"
      return 1
    fi
    if ! grep -qF "simulated curl network failure" "${test_dir}/output" ||
      ! grep -qF "ERROR: Failed to download ${curl_failure_asset}" "${test_dir}/output" ||
      ! grep -qF "(HTTP 000, curl exit 7)." "${test_dir}/output"; then
      TEST_FAILURE_REASON="required download failure diagnostics did not include the curl error, asset, HTTP status, and exit code"
      cat "${test_dir}/output"
      rm -rf "$test_dir"
      return 1
    fi
    rm -rf "$test_dir"
    return 0
  fi
  if [ -n "$attestation_failure" ] || [ "$identity_version" != "$release_version" ]; then
    if [ "$installer_exit" -eq 0 ] || [ -e "${test_dir}/home/.local/bin/awf" ]; then
      TEST_FAILURE_REASON="unattested or version-mismatched release was installed"
      rm -rf "$test_dir"
      return 1
    fi
    if [ "$attestation_failure" = awf-bundle.js ] && grep -q '/awf-linux-' "${test_dir}"/curl-args.[0-9]*; then
      TEST_FAILURE_REASON="bundle attestation failure attempted a platform fallback"
      rm -rf "$test_dir"
      return 1
    fi
    if { [ "$attestation_failure" = checksums.txt ] || [ "$attestation_failure" = release-identity.txt ] || [ "$identity_version" != "$release_version" ]; } &&
      grep -qE '/awf-(bundle|linux|darwin)' "${test_dir}"/curl-args.[0-9]*; then
      TEST_FAILURE_REASON="payload was downloaded before authenticating release metadata"
      rm -rf "$test_dir"
      return 1
    fi
    rm -rf "$test_dir"
    return 0
  fi
  if [ "$installer_exit" -ne 0 ]; then
    TEST_FAILURE_REASON="installer exited unsuccessfully"
    cat "${test_dir}/output"
    rm -rf "${test_dir}"
    return 1
  fi
  local curl_args
  local -a curl_args_files
  shopt -s nullglob
  curl_args_files=("${test_dir}"/curl-args.[0-9]*)
  shopt -u nullglob
  local expected_downloads=2
  if [ -n "$curl_failure_asset" ]; then
    expected_downloads=$((expected_downloads + 1))
    if ! grep -qF "simulated curl network failure" "${test_dir}/output" ||
      ! grep -qF "ERROR: Failed to download ${curl_failure_asset}" "${test_dir}/output" ||
      ! grep -qF "(HTTP 000, curl exit 7)." "${test_dir}/output"; then
      TEST_FAILURE_REASON="failed curl output was not included in the installer diagnostics"
      rm -rf "$test_dir"
      return 1
    fi
  fi
  if [ "$release_version" = v0.29.5 ] || [ "$release_version" = v0.29.6 ] || [ "$release_version" = v0.29.7 ]; then
    expected_downloads=$((expected_downloads + 1))
    if [ "$(wc -l < "${test_dir}/attestations")" -ne 3 ]; then
      TEST_FAILURE_REASON="expected identity, checksums, and payload attestations"
      rm -rf "$test_dir"
      return 1
    fi
    if ! head -n 1 "${test_dir}/attestations" | grep -q release-identity.txt ||
      ! sed -n '2p' "${test_dir}/attestations" | grep -q checksums.txt; then
      TEST_FAILURE_REASON="release identity and manifest must be authenticated before the payload"
      rm -rf "$test_dir"
      return 1
    fi
  fi
  if [ "${#curl_args_files[@]}" -ne "$expected_downloads" ]; then
    TEST_FAILURE_REASON="expected $expected_downloads downloads, got ${#curl_args_files[@]}"
    rm -rf "${test_dir}"
    return 1
  fi
  for curl_args in "${curl_args_files[@]}"; do
    mapfile -t actual_options < "${curl_args}"
    expected_options=("${common_options[@]}")
    case "${actual_options[${#actual_options[@]}-1]}" in
      */awf-linux-*|*/awf-darwin-*) expected_options+=(--speed-limit 1 --speed-time 60) ;;
      *) expected_options+=(--max-time 60) ;;
    esac
    if [ "$expected" = true ] && [[ "${actual_options[${#actual_options[@]}-1]}" != */awf-bundle.js ]]; then
      expected_options+=(--retry-all-errors)
    fi
    for option_index in "${!expected_options[@]}"; do
      if [ "${actual_options[option_index]:-}" != "${expected_options[option_index]}" ]; then
        TEST_FAILURE_REASON="expected curl argument ${expected_options[option_index]} at position ${option_index}"
        rm -rf "${test_dir}"
        return 1
      fi
    done
    # The option list must end here: the next argument is the download target, so any
    # extra retry flag (such as an unconditional --retry-all-errors) is caught.
    if [ "${actual_options[${#expected_options[@]}]:-}" != "-o" ]; then
      TEST_FAILURE_REASON="expected -o at position ${#expected_options[@]}, got ${actual_options[${#expected_options[@]}]:-<none>}"
      rm -rf "${test_dir}"
      return 1
    fi
    if [ "${actual_options[${#expected_options[@]}+2]:-}" != "--write-out" ] ||
      [ "${actual_options[${#expected_options[@]}+3]:-}" != '%{http_code}' ]; then
      TEST_FAILURE_REASON="download did not capture the final HTTP status"
      rm -rf "${test_dir}"
      return 1
    fi
  done
  rm -rf "${test_dir}"
}

if test_curl_retry_all_errors '--retry-all-errors' true; then
  pass "supported curl receives --retry-all-errors"
else
  fail "supported curl retry options were incorrect" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '' true true '--retry-all-errors'; then
  pass "curl help fallback detects --retry-all-errors"
else
  fail "curl help fallback retry options were incorrect" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '' true false '--retry-all-errors'; then
  pass "curl help fallback detects an option missing from help all"
else
  fail "curl help fallback did not detect an option missing from help all" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '' false; then
  pass "unsupported curl omits --retry-all-errors"
else
  fail "unsupported curl retry options were incorrect" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0; then
  pass "optional bundle uses standard retries without retrying HTTP 404"
else
  fail "optional bundle retry options were incorrect" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v18.0.0 Darwin; then
  pass "macOS binary uses a stall timeout without a total transfer limit"
else
  fail "macOS binary timeout options were incorrect" "${TEST_FAILURE_REASON}"
fi
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.29.5; then
  pass "helper-capable bundle authenticates release identity, checksums, and payload"
else
  fail "modern release bundle verification failed" "$TEST_FAILURE_REASON"
fi
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v18.0.0 Linux v0.29.5; then
  pass "helper-capable binary authenticates release identity, checksums, and payload"
else
  fail "modern release binary verification failed" "$TEST_FAILURE_REASON"
fi
for node_version in v24.0.0 v18.0.0; do
  if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' "$node_version" Linux v0.29.7; then
    pass "default release authenticates release identity, checksums, and payload with $node_version"
  else
    fail "default release verification failed" "$TEST_FAILURE_REASON"
  fi
done
for rejected_asset in checksums.txt release-identity.txt awf-bundle.js awf-linux-x64; do
  node_version=v24.0.0
  if [ "$rejected_asset" = awf-linux-x64 ]; then node_version=v18.0.0; fi
  if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' "$node_version" Linux v0.29.5 "$rejected_asset"; then
    pass "failed $rejected_asset attestation prevents installation"
  else
    fail "unattested $rejected_asset was accepted" "$TEST_FAILURE_REASON"
  fi
done
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.29.5 '' v0.29.4; then
  pass "attested identity for another tag prevents installation"
else
  fail "mismatched release identity was accepted" "$TEST_FAILURE_REASON"
fi
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.29.5 '' v0.29.5 awf-bundle.js; then
  pass "optional bundle failure diagnostics are retained before fallback"
else
  fail "optional bundle failure diagnostics were not preserved" "$TEST_FAILURE_REASON"
fi
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.28.62 '' v0.28.62 checksums.txt true; then
  pass "required download failure diagnostics include the curl error and exit status"
else
  fail "required download failure diagnostics were incomplete" "$TEST_FAILURE_REASON"
fi

# Test 6b: attestation verification handles GitHub API quota exhaustion
echo "Test 6b: attestation verification waits for or reports GitHub API quota exhaustion..."
# Public asset downloads succeed while the attestations API answers with a quota
# 403. Uses a fake clock (date/sleep) instead of a live quota experiment.
#   $1 scenario: quota | permission
#   $2 number of quota failures before verification succeeds
#   $3 reset offset in seconds from the fake clock ("" omits the reset header)
#   $4 expected installer exit status
#   $5 expected blocked reason ("" when the install must succeed or fail without a quota stop)
test_attestation_quota() {
  local scenario="$1" failures="$2" reset_offset="$3" expected_exit="$4" expected_reason="$5"
  local test_dir installer_exit=0 reset=""
  TEST_FAILURE_REASON=""
  test_dir="${TEST_ROOT}/quota-${RANDOM}"
  mkdir -m 700 "$test_dir"
  mkdir -p "${test_dir}/bin" "${test_dir}/home"
  printf '1000\n' > "${test_dir}/clock"
  printf '%s\n' "$failures" > "${test_dir}/failures"
  if [ -n "$reset_offset" ]; then reset=$((1000 + reset_offset)); fi
  cat > "${test_dir}/bin/curl" <<'EOF'
#!/usr/bin/env bash
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then output="$2"; break; fi
  shift
done
[ -n "${output:-}" ] || exit 0
if [[ "${output}" == *checksums.txt ]]; then
  printf '%064d awf-linux-x64\n%064d awf-darwin-x64\n%064d awf-bundle.js\n' 0 0 0 > "${output}"
elif [[ "${output}" == *release-identity.txt ]]; then
  printf '%s  refs/tags/%s\n' "$AWF_TEST_MANIFEST_SHA256" v0.29.5 > "$output"
else
  printf '#!/usr/bin/env bash\nexit 0\n' > "${output}"
fi
EOF
  cat > "${test_dir}/bin/gh" <<'EOF'
#!/usr/bin/env bash
if [ "$1" = api ]; then
  printf '%s\n' "$*" >> "${AWF_TEST_DIR}/probes"
  printf 'HTTP/2.0 403 Forbidden\r\n'
  printf 'x-ratelimit-resource: core\r\nx-ratelimit-limit: 15000\r\nx-ratelimit-remaining: 0\r\nx-ratelimit-used: 15000\r\n'
  [ -n "$AWF_TEST_RESET" ] && printf 'x-ratelimit-reset: %s\r\n' "$AWF_TEST_RESET"
  printf '\r\n{"message":"API rate limit exceeded for installation."}\n'
  exit 1
fi
test "$1" = attestation && test "$2" = verify || exit 91
test "${12}" = --deny-self-hosted-runners || exit 96
printf '%s at %s\n' "$3" "$(cat "${AWF_TEST_DIR}/clock")" >> "${AWF_TEST_DIR}/attestations"
remaining=$(cat "${AWF_TEST_DIR}/failures")
if [ "$remaining" -gt 0 ]; then
  printf '%s\n' $((remaining - 1)) > "${AWF_TEST_DIR}/failures"
  if [ "$AWF_TEST_SCENARIO" = permission ]; then
    echo "HTTP 403: Resource not accessible by integration (https://api.github.com/repos/github/gh-aw-firewall/attestations/sha256:0)" >&2
  else
    echo "HTTP 403: API rate limit exceeded for installation ID 1. (https://api.github.com/repos/github/gh-aw-firewall/attestations/sha256:0)" >&2
  fi
  exit 1
fi
exit 0
EOF
  cat > "${test_dir}/bin/date" <<'EOF'
#!/usr/bin/env bash
if [ "$1" = +%s ]; then cat "${AWF_TEST_DIR}/clock"; exit 0; fi
exec /bin/date "$@"
EOF
  cat > "${test_dir}/bin/sleep" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$1" >> "${AWF_TEST_DIR}/sleeps"
printf '%s\n' $(( $(cat "${AWF_TEST_DIR}/clock") + $1 )) > "${AWF_TEST_DIR}/clock"
EOF
  printf '#!/usr/bin/env bash\necho v18.0.0\n' > "${test_dir}/bin/node"
  cat > "${test_dir}/bin/sha256sum" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == *checksums.txt ]]; then
  printf '%s %s\n' "$AWF_TEST_MANIFEST_SHA256" "$1"
else
  printf '%064d %s\n' 0 "$1"
fi
EOF
  printf '#!/usr/bin/env bash\ncase "$1" in -s) echo Linux ;; -m) echo x86_64 ;; esac\n' > "${test_dir}/bin/uname"
  printf '#!/usr/bin/env bash\nexit 0\n' > "${test_dir}/bin/sudo"
  printf '#!/usr/bin/env bash\nexit 0\n' > "${test_dir}/bin/find"
  chmod +x "${test_dir}/bin/"*
  AWF_TEST_MANIFEST_SHA256="$(release_artifact_sha256 github/gh-aw-firewall v0.29.5 checksums.txt)" \
    AWF_TEST_DIR="$test_dir" AWF_TEST_SCENARIO="$scenario" AWF_TEST_RESET="$reset" \
    GH_AW_GITHUB_API_QUOTA_MAX_WAIT_SECONDS=300 GITHUB_STEP_SUMMARY="${test_dir}/summary" GITHUB_ENV="${test_dir}/github-env" \
    HOME="${test_dir}/home" GITHUB_PATH="${test_dir}/github-path" RUNNER_TEMP="$test_dir" PATH="${test_dir}/bin:${PATH}" \
    bash "${SCRIPT_DIR}/install_awf_binary.sh" v0.29.5 --rootless >"${test_dir}/output" 2>&1 || installer_exit=$?
  if [ "$installer_exit" -ne "$expected_exit" ]; then
    TEST_FAILURE_REASON="expected exit ${expected_exit}, got ${installer_exit}: $(tail -n 5 "${test_dir}/output")"
    rm -rf "$test_dir"; return 1
  fi
  if [ "$expected_exit" -eq 0 ]; then
    if [ ! -e "${test_dir}/home/.local/bin/awf" ] || [ "$(wc -l < "${test_dir}/attestations")" -ne $((3 + failures)) ]; then
      TEST_FAILURE_REASON="AWF was not installed after every attestation was re-verified"
      rm -rf "$test_dir"; return 1
    fi
    if [ "$failures" -gt 0 ]; then
      # No verification may run before the server reset.
      if [ "$(sed -n '2p' "${test_dir}/attestations" | awk '{print $NF}')" -le "$reset" ] ||
        [ "$(cat "${test_dir}/sleeps")" != "$((reset_offset + 1))" ]; then
        TEST_FAILURE_REASON="verification was retried before the server reset"
        rm -rf "$test_dir"; return 1
      fi
    fi
    if grep -q GH_AW_GITHUB_API_QUOTA_BLOCKED "${test_dir}/github-env" 2>/dev/null; then
      TEST_FAILURE_REASON="a recovered quota stop was reported as blocked"
      rm -rf "$test_dir"; return 1
    fi
    rm -rf "$test_dir"; return 0
  fi
  if [ -e "${test_dir}/home/.local/bin/awf" ]; then
    TEST_FAILURE_REASON="an unverified AWF release was installed"
    rm -rf "$test_dir"; return 1
  fi
  if [ -n "$expected_reason" ]; then
    if ! grep -qF "### GitHub API quota exhausted" "${test_dir}/summary" ||
      ! grep -qF "| Remaining | 0 |" "${test_dir}/summary" ||
      ! grep -qF "GH_AW_GITHUB_API_QUOTA_BLOCKED=resource=core; limit=15000; remaining=0; used=15000; reset=" "${test_dir}/github-env" ||
      ! grep -qF "reason=${expected_reason}" "${test_dir}/github-env" ||
      ! grep -qF "GitHub API quota exhausted" "${test_dir}/output"; then
      TEST_FAILURE_REASON="quota-blocked summary or environment was incomplete: $(cat "${test_dir}/summary" "${test_dir}/github-env" 2>/dev/null)"
      rm -rf "$test_dir"; return 1
    fi
  else
    if [ -e "${test_dir}/probes" ] || [ -s "${test_dir}/summary" ] || grep -q GH_AW_GITHUB_API_QUOTA_BLOCKED "${test_dir}/github-env" 2>/dev/null; then
      TEST_FAILURE_REASON="a non-quota attestation failure was reclassified as quota exhaustion"
      rm -rf "$test_dir"; return 1
    fi
  fi
  rm -rf "$test_dir"
}

if test_attestation_quota quota 1 120 0 ""; then
  pass "quota 403 with reset inside the budget waits, re-verifies, and installs AWF"
else
  fail "quota 403 inside the wait budget did not recover" "$TEST_FAILURE_REASON"
fi
if test_attestation_quota quota 1 1200 75 reset_exceeds_budget; then
  pass "quota 403 with reset beyond the budget blocks installation with a quota summary"
else
  fail "quota 403 beyond the wait budget was not reported as blocked" "$TEST_FAILURE_REASON"
fi
if test_attestation_quota quota 2 60 75 renewed_exhaustion; then
  pass "renewed quota exhaustion after the reset blocks installation"
else
  fail "renewed quota exhaustion was not reported as blocked" "$TEST_FAILURE_REASON"
fi
if test_attestation_quota quota 1 "" 75 unknown_reset; then
  pass "quota 403 without a reset blocks installation without waiting"
else
  fail "quota 403 without a reset was not reported as blocked" "$TEST_FAILURE_REASON"
fi
if test_attestation_quota permission 1 120 1 ""; then
  pass "permission 403 from attestation verification is not reclassified as quota exhaustion"
else
  fail "permission 403 was treated as quota exhaustion" "$TEST_FAILURE_REASON"
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
