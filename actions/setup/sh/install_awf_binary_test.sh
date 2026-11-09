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
  local api_mode="${12:-}"
  local curl_failure_http="${13:-000}"
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
for argument in "$@"; do url="$argument"; done
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    output="$2"
    break
  fi
  shift
done
if [[ "$url" == https://api.github.com/*/releases/tags/* ]]; then
  if [ "$AWF_TEST_API_MODE" = metadata-failure ]; then
    echo "simulated API metadata failure" >&2
    exit 22
  fi
  case "$AWF_TEST_API_MODE" in
    missing-asset) printf '{"tag_name":"%s","assets":[]}\n' "$AWF_TEST_RELEASE_VERSION" > "$output" ;;
    invalid-id) printf '{"tag_name":"%s","assets":[{"name":"%s","id":"../../other"}]}\n' "$AWF_TEST_RELEASE_VERSION" "$AWF_TEST_CURL_FAILURE_ASSET" > "$output" ;;
    duplicate-asset) printf '{"tag_name":"%s","assets":[{"name":"%s","id":123},{"name":"%s","id":456}]}\n' "$AWF_TEST_RELEASE_VERSION" "$AWF_TEST_CURL_FAILURE_ASSET" "$AWF_TEST_CURL_FAILURE_ASSET" > "$output" ;;
    wrong-tag) printf '{"tag_name":"v9.9.9","assets":[{"name":"%s","id":123}]}\n' "$AWF_TEST_CURL_FAILURE_ASSET" > "$output" ;;
    *) printf '{"tag_name":"%s","assets":[{"name":"checksums.txt","id":123},{"name":"release-identity.txt","id":124},{"name":"awf-bundle.js","id":125},{"name":"awf-linux-x64","id":126}]}\n' "$AWF_TEST_RELEASE_VERSION" > "$output" ;;
  esac
  exit 0
fi
if [[ "$url" == https://api.github.com/*/releases/assets/* ]]; then
  if [ "$AWF_TEST_API_MODE" = asset-failure ]; then
    echo partial > "$output"
    echo "simulated API asset failure" >&2
    exit 22
  fi
else
  if [ "$AWF_TEST_CURL_FAILURE_ASSET" = all ] ||
    [ "${output##*/}" = "${AWF_TEST_CURL_FAILURE_ASSET:-__never__}" ]; then
    echo "simulated curl network failure" >&2
    if [ "$AWF_TEST_CURL_FAILURE_HTTP" != 000 ]; then
      printf '%s' "$AWF_TEST_CURL_FAILURE_HTTP"
      exit 22
    fi
    exit 7
  fi
fi
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
  if [ "$AWF_TEST_API_MODE" = corrupt-manifest ]; then
    printf '%064d %s\n' 0 "$1"
  else
    printf '%s %s\n' "$AWF_TEST_MANIFEST_SHA256" "$1"
  fi
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
  if [ "$api_mode" = asset-failure ]; then
    cat > "${test_dir}/bin/rm" <<'EOF'
#!/usr/bin/env bash
/bin/rm "$@" || exit $?
if [ "$1" = -f ] && [[ "$2" == */checksums.txt ]] && [ ! -e "$2" ]; then
  echo removed > "${CURL_ARGS_FILE}.partial-removed"
fi
EOF
  fi
  chmod +x "${test_dir}/bin/"*
  local installer_path="${test_dir}/bin:${PATH}"
  if [ "$api_mode" = missing-jq ]; then
    local tool
    for tool in bash dirname mkdir grep awk mktemp stat cat rm; do
      ln -s "$(command -v "$tool")" "${test_dir}/bin/$tool"
    done
    installer_path="${test_dir}/bin"
  fi
  local installer_exit=0
  AWF_TEST_MANIFEST_SHA256="$(release_artifact_sha256 github/gh-aw-firewall "$release_version" checksums.txt)" \
    CURL_HELP_ALL_OUTPUT="${curl_help_all}" CURL_HELP_OUTPUT="${curl_help}" CURL_HELP_ALL_FAILS="${help_all_fails}" CURL_ARGS_FILE="${test_dir}/curl-args" \
    AWF_TEST_ATTESTATION_LOG="${test_dir}/attestations" AWF_TEST_ATTESTATION_FAILURE="$attestation_failure" AWF_TEST_IDENTITY_VERSION="$identity_version" \
    AWF_TEST_CURL_FAILURE_ASSET="$curl_failure_asset" AWF_TEST_CURL_FAILURE_HTTP="$curl_failure_http" \
    AWF_TEST_API_MODE="$api_mode" AWF_TEST_RELEASE_VERSION="$release_version" \
    AWF_TEST_NODE_VERSION="$node_version" AWF_TEST_OS="$platform" HOME="${test_dir}/home" GITHUB_PATH="${test_dir}/github-path" \
    GITHUB_ENV="" GH_AW_RUNTIME_TMP_DIR="" GH_AW_RUNTIME_TMP_ID="" TMPDIR="$test_dir" \
    RUNNER_TEMP="$test_dir" PATH="$installer_path" bash "${SCRIPT_DIR}/install_awf_binary.sh" "$release_version" --rootless >"${test_dir}/output" 2>&1 || installer_exit=$?
  local expected_api_calls=0
  if [[ "$curl_failure_http" =~ ^5[0-9]{2}$ ]]; then
    expected_api_calls=2
    case "$api_mode" in
      metadata-failure|missing-asset|invalid-id|duplicate-asset|wrong-tag) expected_api_calls=1 ;;
      missing-jq) expected_api_calls=0 ;;
    esac
    if [ "$curl_failure_asset" = all ]; then expected_api_calls=4; fi
  fi
  local api_calls=0 metadata_calls=0
  local recorded_call request_url
  for recorded_call in "${test_dir}"/curl-args.[0-9]*; do
    request_url="$(tail -n 1 "$recorded_call")"
    if [[ "$request_url" == https://api.github.com/* ]]; then
      api_calls=$((api_calls + 1))
      if grep -qi authorization "$recorded_call" ||
        ! grep -qx -- '--retry-max-time' "$recorded_call" ||
        ! grep -qx -- '--proto-redir' "$recorded_call"; then
        TEST_FAILURE_REASON="API fallback must remain bounded, HTTPS-only, and unauthenticated"
        rm -rf "$test_dir"
        return 1
      fi
      case "$request_url" in
        "https://api.github.com/repos/github/gh-aw-firewall/releases/tags/$release_version") metadata_calls=$((metadata_calls + 1)) ;;
        https://api.github.com/repos/github/gh-aw-firewall/releases/assets/12[3-6]) ;;
        *)
          TEST_FAILURE_REASON="API fallback used an unexpected host, repository, version, or asset ID"
          rm -rf "$test_dir"
          return 1
          ;;
      esac
    fi
  done
  if [ "$api_calls" -ne "$expected_api_calls" ]; then
    TEST_FAILURE_REASON="expected $expected_api_calls API calls, got $api_calls"
    cat "${test_dir}/output"
    rm -rf "$test_dir"
    return 1
  fi
  if [ "$expected_api_calls" -gt 0 ] && [ "$metadata_calls" -ne 1 ]; then
    TEST_FAILURE_REASON="release metadata must be fetched only once per install"
    rm -rf "$test_dir"
    return 1
  fi
  if [ "$api_mode" = asset-failure ] && [ ! -e "${test_dir}/curl-args.partial-removed" ]; then
    TEST_FAILURE_REASON="partial API asset was not removed before staging cleanup"
    rm -rf "$test_dir"
    return 1
  fi
  if [ "$api_mode" = missing-jq ] &&
    { ! grep -qF 'WARNING: Download of checksums.txt returned HTTP 504' "${test_dir}/output" ||
      ! grep -qF 'Release-assets API fallback requires jq' "${test_dir}/output"; }; then
    TEST_FAILURE_REASON="missing jq did not report the fallback warning and prerequisite error"
    cat "${test_dir}/output"
    rm -rf "$test_dir"
    return 1
  fi
  if [ "$api_mode" = metadata-failure ] &&
    ! grep -qF 'rate-limited (HTTP 403; 60 requests/hour per IP)' "${test_dir}/output"; then
    TEST_FAILURE_REASON="metadata failure did not explain unauthenticated API rate limits"
    rm -rf "$test_dir"
    return 1
  fi
  local expected_curl_exit=7
  if [ "$curl_failure_http" != 000 ]; then expected_curl_exit=22; fi
  if [ "$expect_download_failure" = true ]; then
    if [ "$installer_exit" -eq 0 ] || [ -e "${test_dir}/home/.local/bin/awf" ]; then
      TEST_FAILURE_REASON="installer succeeded after a required download failed"
      rm -rf "$test_dir"
      return 1
    fi
    if ! grep -qF "simulated curl network failure" "${test_dir}/output" ||
      ! grep -qF "ERROR: Failed to download ${curl_failure_asset}" "${test_dir}/output" ||
      ! grep -qF "(HTTP $curl_failure_http, curl exit $expected_curl_exit)." "${test_dir}/output"; then
      TEST_FAILURE_REASON="required download failure diagnostics did not include the curl error, asset, HTTP status, and exit code"
      cat "${test_dir}/output"
      rm -rf "$test_dir"
      return 1
    fi
    rm -rf "$test_dir"
    return 0
  fi
  if [ "$api_mode" = corrupt-manifest ]; then
    if [ "$installer_exit" -eq 0 ] || [ -e "${test_dir}/home/.local/bin/awf" ] ||
      ! grep -q 'Trusted SHA256 verification failed' "${test_dir}/output" ||
      grep -qE '/awf-(bundle|linux|darwin)' "${test_dir}"/curl-args.[0-9]*; then
      TEST_FAILURE_REASON="corrupt fallback manifest did not stop installation before payload download"
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
  local expected_downloads=2 expected_diagnostic
  if [ -n "$curl_failure_asset" ]; then
    if [ "$expected_api_calls" -gt 0 ]; then
      expected_downloads=$((expected_downloads + expected_api_calls))
      expected_diagnostic="trying the release-assets API"
    else
      expected_downloads=$((expected_downloads + 1))
      expected_diagnostic="(HTTP $curl_failure_http, curl exit $expected_curl_exit)."
    fi
    if ! grep -qF "simulated curl network failure" "${test_dir}/output" ||
      ! grep -qF "$expected_diagnostic" "${test_dir}/output"; then
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
    if [[ "${actual_options[${#actual_options[@]}-1]}" == https://api.github.com/* ]]; then
      expected_options=(-fsSL --proto '=https' --proto-redir '=https' --retry 2 --retry-max-time 30 --connect-timeout 15)
      case "${actual_options[${#actual_options[@]}-1]}" in
        */releases/tags/*) expected_options+=(--max-time 30 -H 'Accept: application/vnd.github+json') ;;
        *)
          case "${actual_options[${#actual_options[@]}-1]}" in
            */assets/126) expected_options+=(--speed-limit 1 --speed-time 60) ;;
            *) expected_options+=(--max-time 60) ;;
          esac
          expected_options+=(-H 'Accept: application/octet-stream')
          ;;
      esac
    else
      expected_options=("${common_options[@]}")
      case "${actual_options[${#actual_options[@]}-1]}" in
        */awf-linux-*|*/awf-darwin-*) expected_options+=(--speed-limit 1 --speed-time 60) ;;
        *) expected_options+=(--max-time 60) ;;
      esac
      if [ "$expected" = true ] && [[ "${actual_options[${#actual_options[@]}-1]}" != */awf-bundle.js ]]; then
        expected_options+=(--retry-all-errors)
      fi
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
    if [[ "${actual_options[${#actual_options[@]}-1]}" != https://api.github.com/* ]] &&
      { [ "${actual_options[${#expected_options[@]}+2]:-}" != "--write-out" ] ||
      [ "${actual_options[${#expected_options[@]}+3]:-}" != '%{http_code}' ]; }; then
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

for asset in checksums.txt release-identity.txt awf-bundle.js awf-linux-x64; do
  node_version=v24.0.0
  if [ "$asset" = awf-linux-x64 ]; then node_version=v18.0.0; fi
  if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' "$node_version" Linux v0.29.5 '' v0.29.5 "$asset" false success 504; then
    pass "HTTP 504 for $asset recovers through the API with integrity checks"
  else
    fail "HTTP 504 API recovery failed for $asset" "$TEST_FAILURE_REASON"
  fi
done
for node_version in v24.0.0 v18.0.0; do
  if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' "$node_version" Linux v0.29.5 '' v0.29.5 all false success 504; then
    pass "multiple HTTP 504 downloads reuse release metadata with $node_version"
  else
    fail "release metadata was not reused" "$TEST_FAILURE_REASON"
  fi
done
for api_mode in metadata-failure asset-failure missing-asset invalid-id duplicate-asset wrong-tag missing-jq; do
  if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.29.5 '' v0.29.5 checksums.txt true "$api_mode" 504; then
    pass "API $api_mode fails closed and retains original HTTP 504 diagnostics"
  else
    fail "API $api_mode did not fail closed" "$TEST_FAILURE_REASON"
  fi
done
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.29.5 '' v0.29.5 checksums.txt false corrupt-manifest 504; then
  pass "API-retrieved manifest still requires the trusted digest"
else
  fail "corrupt API manifest was accepted" "$TEST_FAILURE_REASON"
fi
if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.29.5 awf-bundle.js v0.29.5 awf-bundle.js false success 504; then
  pass "API-retrieved bundle still requires attestation and cannot bypass it through binary fallback"
else
  fail "unattested API bundle was accepted" "$TEST_FAILURE_REASON"
fi
for http_status in 401 403 404; do
  if test_curl_retry_all_errors '--retry-all-errors' true false '--retry-all-errors' v24.0.0 Linux v0.29.5 '' v0.29.5 checksums.txt true success "$http_status"; then
    pass "HTTP $http_status does not trigger API fallback"
  else
    fail "HTTP $http_status incorrectly triggered API fallback" "$TEST_FAILURE_REASON"
  fi
done

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
