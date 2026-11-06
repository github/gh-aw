#!/usr/bin/env bash
set +o histexpand

# Tests for compiler-pinned threat-detect installation.
# Run: bash install_threat_detect_binary_test.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_SCRIPT="${SCRIPT_DIR}/install_threat_detect_binary.sh"

TESTS_PASSED=0
TESTS_FAILED=0

pass() { echo "PASS: $1"; TESTS_PASSED=$((TESTS_PASSED + 1)); }
fail() { echo "FAIL: $1"; echo "  $2"; TESTS_FAILED=$((TESTS_FAILED + 1)); }

# run_installer OS ARCH MODE [BASE_URL] [EXTRA_ARGS...]
# MODE is valid, tampered, missing, malformed, or uppercase.
run_installer() {
  local fake_os="$1"
  local fake_arch="$2"
  local mode="$3"
  local base_url="${4:-https://github.com/github/gh-aw-threat-detection/releases/download}"
  shift 4 || true

  local sandbox
  sandbox=$(mktemp -d)
  mkdir -p "${sandbox}/bin" "${sandbox}/home/.local/bin" "${sandbox}/privileged-bin"
  # A native non-Debian runner: no sudo or package manager is available on PATH.
  local tool
  for tool in bash cat mkdir chmod mktemp rm awk sha256sum; do
    ln -s "$(command -v "$tool")" "${sandbox}/bin/${tool}"
  done
  local real_mv
  real_mv=$(command -v mv)
  cat >"${sandbox}/bin/mv" <<'EOF'
#!/usr/bin/env bash
for arg in "$@"; do
  if [[ "$arg" = /usr/local/bin/* ]]; then
    echo "/usr/local/bin is not writable by the runner" >&2
    exit 1
  fi
done
exec "$SANDBOX_REAL_MV" "$@"
EOF

  cat >"${sandbox}/bin/uname" <<EOF
#!/usr/bin/env bash
case "\$1" in
  -s) echo "${fake_os}" ;;
  -m) echo "${fake_arch}" ;;
  *) echo "${fake_os}" ;;
esac
EOF

  cat >"${sandbox}/bin/curl" <<'EOF'
#!/usr/bin/env bash
out=""
url=""
prev=""
https_only=false
https_redirects=false
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
  elif [ "${arg#-}" = "$arg" ]; then
    url="$arg"
  fi
  if [ "$prev" = "--proto" ] && [ "$arg" = "=https" ]; then https_only=true; fi
  if [ "$prev" = "--proto-redir" ] && [ "$arg" = "=https" ]; then https_redirects=true; fi
  prev="$arg"
done
if [ "$https_only" != true ] || [ "$https_redirects" != true ]; then
  echo "missing HTTPS-only initial/redirect policy" >&2
  exit 96
fi
echo "$url" >>"${SANDBOX_URL_LOG}"
printf '%s\n' '#!/usr/bin/env bash' 'echo verified >>"${SANDBOX_EXECUTION_LOG}"' >"$out"
EOF

  # A pre-existing unverified binary must never run after an install failure.
  cat >"${sandbox}/bin/threat-detect" <<'EOF'
#!/usr/bin/env bash
echo unverified >>"${SANDBOX_EXECUTION_LOG}"
EOF
  local install_flags=(--rootless)
  if [ "${INSTALL_MODE:-rootless}" = privileged ]; then
    install_flags=()
    cat >"${sandbox}/bin/sudo" <<'EOF'
#!/usr/bin/env bash
echo "$*" >>"${SANDBOX_SUDO_LOG}"
if [[ "$#" != 3 || "$1" != mv || "$3" != /usr/local/bin/threat-detect ]]; then
  echo "unexpected privileged operation" >&2
  exit 1
fi
exec "$SANDBOX_REAL_MV" "$2" "${SANDBOX_PRIVILEGED_BIN}/threat-detect"
EOF
    chmod +x "${sandbox}/bin/sudo"
  fi
  chmod +x "${sandbox}/bin/uname" "${sandbox}/bin/curl" "${sandbox}/bin/threat-detect" "${sandbox}/bin/mv"

  local payload_hash
  # Hash the literal script payload; its environment variable expands only when run.
  # shellcheck disable=SC2016
  payload_hash=$(printf '%s\n' '#!/usr/bin/env bash' 'echo verified >>"${SANDBOX_EXECUTION_LOG}"' | sha256sum | awk '{print $1}')
  local bad_hash
  bad_hash=$(printf '0%.0s' {1..64})
  local amd64_hash="$payload_hash"
  local arm64_hash="$payload_hash"
  local pin_args=(--sha256-amd64 "$amd64_hash" --sha256-arm64 "$arm64_hash")
  case "$mode" in
    tampered) pin_args=(--sha256-amd64 "$bad_hash" --sha256-arm64 "$bad_hash") ;;
    missing) pin_args=() ;;
    malformed) pin_args=(--sha256-amd64 abc --sha256-arm64 "$arm64_hash") ;;
    uppercase) pin_args=(--sha256-amd64 "${amd64_hash^^}" --sha256-arm64 "$arm64_hash") ;;
  esac

  local url_log="${sandbox}/url.log"
  local execution_log="${sandbox}/execution.log"
  local github_path="${sandbox}/github-path"
  local github_output="${sandbox}/github-output"
  local sudo_log="${sandbox}/sudo.log"
  : >"$url_log"
  : >"$execution_log"
  : >"$github_path"
  : >"$github_output"
  : >"$sudo_log"

  local script="$INSTALL_SCRIPT"
  if [ "${INSTALL_MODE:-rootless}" = privileged ]; then
    # Redirect only the final version invocation; sudo file operations are mocked.
    script="${sandbox}/installer.sh"
    sed 's|^"${THREAT_DETECT_INSTALL_DIR}/${THREAT_DETECT_INSTALL_NAME}" --version$|"${SANDBOX_PRIVILEGED_BIN}/threat-detect" --version|' "$INSTALL_SCRIPT" >"$script"
  fi
  RUN_OUTPUT=$(cd "$sandbox" && env PATH="${sandbox}/bin" HOME="${sandbox}/home" \
    SANDBOX_URL_LOG="$url_log" SANDBOX_EXECUTION_LOG="$execution_log" GITHUB_PATH="$github_path" GITHUB_OUTPUT="$github_output" \
    SANDBOX_REAL_MV="$real_mv" SANDBOX_PRIVILEGED_BIN="${sandbox}/privileged-bin" SANDBOX_SUDO_LOG="$sudo_log" \
    bash "$script" v0.5.2 "${install_flags[@]}" --artifact-base-url "$base_url" "${pin_args[@]}" "$@" 2>&1)
  RUN_STATUS=$?
  RUN_URLS=$(cat "$url_log")
  RUN_EXECUTIONS=$(cat "$execution_log")
  RUN_BINARY_PATH=$(sed -n 's/^binary-path=//p' "$github_output")
  RUN_EXPECTED_BINARY_PATH="${sandbox}/home/.local/bin/threat-detect"
  local installed_path="$RUN_EXPECTED_BINARY_PATH"
  if [ "${INSTALL_MODE:-rootless}" = privileged ]; then
    RUN_EXPECTED_BINARY_PATH="/usr/local/bin/threat-detect"
    installed_path="${sandbox}/privileged-bin/threat-detect"
  fi
  RUN_PATH=$(cat "$github_path")
  RUN_SUDO=$(cat "$sudo_log")
  RUN_INSTALLED_EXECUTABLE=false
  if [ -x "$installed_path" ]; then
    RUN_INSTALLED_EXECUTABLE=true
  fi

  RUN_NEXT_STATUS=0
  if [ "$RUN_STATUS" -eq 0 ] && [ "${INSTALL_MODE:-rootless}" = rootless ]; then
    # Actions prepends GITHUB_PATH for later steps; it must beat the stale PATH copy.
    env PATH="${RUN_PATH}:${sandbox}/bin" SANDBOX_EXECUTION_LOG="$execution_log" threat-detect >/dev/null 2>&1
    RUN_NEXT_STATUS=$?
    RUN_EXECUTIONS=$(cat "$execution_log")
  fi
  # Simulate the subsequent detection step after a failed tolerated install.
  if [ "$RUN_STATUS" -ne 0 ] && [ -x "${sandbox}/home/.local/bin/threat-detect" ]; then
    env PATH="${sandbox}/home/.local/bin:${sandbox}/bin:${PATH}" \
      SANDBOX_EXECUTION_LOG="$execution_log" threat-detect >/dev/null 2>&1 || true
    RUN_EXECUTIONS_AFTER_FAILURE=$(cat "$execution_log")
  else
    RUN_EXECUTIONS_AFTER_FAILURE="$RUN_EXECUTIONS"
  fi

  rm -rf "$sandbox"
}

assert_success() {
  local description="$1"
  shift
  run_installer "$@"
  if [ "$RUN_STATUS" -ne 0 ]; then
    fail "$description" "installer exited with ${RUN_STATUS}: ${RUN_OUTPUT}"
  elif [ "$RUN_BINARY_PATH" != "$RUN_EXPECTED_BINARY_PATH" ] || [ "$RUN_INSTALLED_EXECUTABLE" != true ]; then
    fail "$description" "installer must export the exact executable verified path: ${RUN_BINARY_PATH}"
  else
    pass "$description"
  fi
}

assert_failure_before_download() {
  local description="$1"
  local expected="$2"
  shift 2
  run_installer "$@"
  if [ "$RUN_STATUS" -eq 0 ]; then
    fail "$description" "installer unexpectedly succeeded"
  elif ! grep -qF "$expected" <<<"$RUN_OUTPUT"; then
    fail "$description" "expected '${expected}' in: ${RUN_OUTPUT}"
  elif [ -n "$RUN_URLS" ]; then
    fail "$description" "installer downloaded before rejecting input: ${RUN_URLS}"
  else
    pass "$description"
  fi
}

echo "Running install_threat_detect_binary.sh tests..."

assert_success "Linux/x86_64 verifies amd64 pin" Linux x86_64 valid \
  https://github.com/github/gh-aw-threat-detection/releases/download
if [ "$RUN_NEXT_STATUS" -ne 0 ] || [ "$RUN_EXECUTIONS" != $'verified\nverified' ] || [ -z "$RUN_PATH" ] || [ -n "$RUN_SUDO" ]; then
  fail "Native no-sudo install propagates verified binary through GITHUB_PATH" "path=${RUN_PATH}, executions=${RUN_EXECUTIONS}, sudo=${RUN_SUDO}"
else
  pass "Native no-sudo install propagates verified binary through GITHUB_PATH"
fi
if ! grep -qF "/v0.5.2/threat-detect-linux-amd64" <<<"$RUN_URLS"; then
  fail "Default source selects amd64 asset" "unexpected URL: ${RUN_URLS}"
elif grep -qF "checksums.txt" <<<"$RUN_URLS"; then
  fail "Installer avoids runtime checksums" "unexpected URL: ${RUN_URLS}"
else
  pass "Default source selects amd64 asset without checksums.txt"
fi

assert_success "Linux/aarch64 verifies arm64 pin" Linux aarch64 valid \
  https://github.com/github/gh-aw-threat-detection/releases/download
if grep -qF "/v0.5.2/threat-detect-linux-arm64" <<<"$RUN_URLS"; then
  pass "Linux/aarch64 selects arm64 asset"
else
  fail "Linux/aarch64 selects arm64 asset" "unexpected URL: ${RUN_URLS}"
fi

assert_success "HTTPS mirror supplies pinned asset" Linux arm64 valid \
  https://mirror.example.com/detector
if grep -qF "https://mirror.example.com/detector/v0.5.2/threat-detect-linux-arm64" <<<"$RUN_URLS"; then
  pass "HTTPS mirror URL retains pinned tag and asset"
else
  fail "HTTPS mirror URL retains pinned tag and asset" "unexpected URL: ${RUN_URLS}"
fi

assert_failure_before_download "HTTP mirror is rejected" "must use HTTPS" \
  Linux x86_64 valid http://mirror.example.com/detector
assert_failure_before_download "Missing pins are rejected" "pin is required for linux-amd64" \
  Linux x86_64 missing https://mirror.example.com/detector
assert_failure_before_download "Malformed pins are rejected" "pin is required for linux-amd64" \
  Linux x86_64 malformed https://mirror.example.com/detector
assert_failure_before_download "Uppercase pins are rejected" "pin is required for linux-amd64" \
  Linux x86_64 uppercase https://mirror.example.com/detector
assert_failure_before_download "Duplicate pins are rejected" "Duplicate threat-detect SHA256 pin" \
  Linux x86_64 valid https://mirror.example.com/detector --sha256-amd64 "$(printf '1%.0s' {1..64})"

run_installer Linux x86_64 tampered https://mirror.example.com/detector
if [ "$RUN_STATUS" -eq 0 ]; then
  fail "Tampered bytes are rejected" "installer unexpectedly succeeded"
elif ! grep -qF "Checksum verification failed" <<<"$RUN_OUTPUT"; then
  fail "Tampered bytes are rejected" "unexpected output: ${RUN_OUTPUT}"
elif [ -n "$RUN_EXECUTIONS" ]; then
  fail "Tampered bytes are not executed" "execution log: ${RUN_EXECUTIONS}"
elif [ -n "$RUN_BINARY_PATH" ]; then
  fail "Failed verification publishes no binary path" "unexpected path: ${RUN_BINARY_PATH}"
elif grep -qF "unverified" <<<"$RUN_EXECUTIONS_AFTER_FAILURE"; then
  fail "Failed verification blocks PATH fallback" "execution log: ${RUN_EXECUTIONS_AFTER_FAILURE}"
else
  pass "Tampered bytes fail closed without PATH fallback"
fi

assert_failure_before_download "Darwin remains unsupported" "macOS is not a supported platform" \
  Darwin arm64 valid https://mirror.example.com/detector
assert_failure_before_download "Unknown Linux architecture is rejected" "Unsupported Linux architecture" \
  Linux riscv64 valid https://mirror.example.com/detector

INSTALL_MODE=privileged assert_success "Privileged installation retains sudo and system path" \
  Linux x86_64 valid https://github.com/github/gh-aw-threat-detection/releases/download
if [ -n "$RUN_PATH" ] || ! grep -qF "/usr/local/bin/threat-detect" <<<"$RUN_SUDO"; then
  fail "Privileged installation uses sudo without rootless PATH propagation" "path=${RUN_PATH}, sudo=${RUN_SUDO}"
else
  pass "Privileged installation uses sudo without rootless PATH propagation"
fi

echo
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"
if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi
