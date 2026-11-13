#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/copy_gh_aw_binary_for_mcp.sh"

TESTS_PASSED=0
TESTS_FAILED=0

pass() { echo "PASS: $1"; TESTS_PASSED=$((TESTS_PASSED + 1)); }
fail() { echo "FAIL: $1"; TESTS_FAILED=$((TESTS_FAILED + 1)); }

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

make_stub_gh() {
  local target="$1"
  mkdir -p "${target}"
  cat > "${target}/gh" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "aw" ] && [ "${2:-}" = "--version" ]; then
  echo "gh-aw test"
  exit 0
fi
exit 0
EOF
  chmod +x "${target}/gh"
}

new_env() {
  local name="$1"
  local root="${WORK_DIR}/${name}"
  mkdir -p "${root}/runner-temp" "${root}/workspace" "${root}/home" "${root}/path"
  make_stub_gh "${root}/path"
  echo "${root}"
}

run_script() {
  local root="$1"
  local path_prefix="${2:-}"
  local gh_config_dir="${3:-}"
  local runner_temp="${4:-${root}/runner-temp}"
  set +e
  PATH="${path_prefix}:${root}/path:${PATH}" \
    RUNNER_TEMP="${runner_temp}" \
    HOME="${root}/home" \
    GITHUB_WORKSPACE="${root}/workspace" \
    GH_CONFIG_DIR="${gh_config_dir}" \
    bash "${SCRIPT}" > "${root}/out.log" 2>&1
  local code=$?
  set -e
  echo "${code}"
}

echo "Running copy_gh_aw_binary_for_mcp.sh tests..."

if bash -n "${SCRIPT}" >/dev/null 2>&1; then
  pass "script syntax is valid"
else
  fail "script syntax is invalid"
fi

root="$(new_env path_binary)"
cat > "${root}/path/gh-aw" <<'EOF'
#!/usr/bin/env bash
echo "path gh-aw"
EOF
chmod +x "${root}/path/gh-aw"
code="$(run_script "${root}")"
if [ "${code}" -eq 0 ] && [ -x "${root}/runner-temp/gh-aw/gh-aw" ]; then
  pass "copies gh-aw binary from PATH"
else
  cat "${root}/out.log"
  fail "failed to copy gh-aw binary from PATH"
fi

root="$(new_env runner_temp_fallback)"
mkdir -p "${root}/runner-temp/gh-aw/bin"
cat > "${root}/runner-temp/gh-aw/bin/gh-aw-custom" <<'EOF'
#!/usr/bin/env bash
echo "runner-temp gh-aw"
EOF
chmod +x "${root}/runner-temp/gh-aw/bin/gh-aw-custom"
code="$(run_script "${root}" "${root}/empty-path")"
if [ "${code}" -eq 0 ] && [ -x "${root}/runner-temp/gh-aw/gh-aw" ]; then
  pass "copies executable gh-aw* variant from setup-cli shared path"
else
  cat "${root}/out.log"
  fail "failed to copy gh-aw variant from setup-cli shared path"
fi

root="$(new_env runner_temp_exact)"
mkdir -p "${root}/runner-temp/gh-aw/bin"
cat > "${root}/runner-temp/gh-aw/bin/gh-aw" <<'EOF'
#!/usr/bin/env bash
echo "runner-temp exact gh-aw"
EOF
chmod +x "${root}/runner-temp/gh-aw/bin/gh-aw"
code="$(run_script "${root}" "${root}/empty-path")"
if [ "${code}" -eq 0 ] && [ -x "${root}/runner-temp/gh-aw/gh-aw" ]; then
  pass "copies exact gh-aw binary from setup-cli shared path"
else
  cat "${root}/out.log"
  fail "failed to copy exact gh-aw binary from setup-cli shared path"
fi

root="$(new_env gh_config_fallback)"
mkdir -p "${root}/config/extensions/gh-aw"
cat > "${root}/config/extensions/gh-aw/gh-aw-alt" <<'EOF'
#!/usr/bin/env bash
echo "config gh-aw"
EOF
chmod +x "${root}/config/extensions/gh-aw/gh-aw-alt"
code="$(run_script "${root}" "${root}/empty-path" "${root}/config")"
if [ "${code}" -eq 0 ] && [ -x "${root}/runner-temp/gh-aw/gh-aw" ]; then
  pass "copies gh-aw variant from GH_CONFIG_DIR fallback"
else
  cat "${root}/out.log"
  fail "failed to copy gh-aw variant from GH_CONFIG_DIR fallback"
fi

root="$(new_env home_extensions_fallback)"
mkdir -p "${root}/home/.local/share/gh/extensions/gh-aw"
cat > "${root}/home/.local/share/gh/extensions/gh-aw/gh-aw-alt" <<'EOF'
#!/usr/bin/env bash
echo "home gh-aw"
EOF
chmod +x "${root}/home/.local/share/gh/extensions/gh-aw/gh-aw-alt"
code="$(run_script "${root}" "${root}/empty-path")"
if [ "${code}" -eq 0 ] && [ -x "${root}/runner-temp/gh-aw/gh-aw" ]; then
  pass "copies gh-aw variant from HOME extensions fallback"
else
  cat "${root}/out.log"
  fail "failed to copy gh-aw variant from HOME extensions fallback"
fi

root="$(new_env missing_binary)"
code="$(run_script "${root}" "${root}/empty-path")"
if [ "${code}" -ne 0 ] && grep -q "Failed to find gh-aw binary for MCP Server" "${root}/out.log"; then
  pass "fails with clear error when no binary is available"
else
  cat "${root}/out.log"
  fail "did not fail cleanly when no binary was available"
fi

root="$(new_env symlinked_output)"
mkdir -p "${root}/runner-temp/gh-aw"
printf 'external sentinel\n' > "${root}/sentinel"
ln -s "${root}/sentinel" "${root}/runner-temp/gh-aw/gh-aw"
cat > "${root}/path/gh-aw" <<'EOF'
#!/usr/bin/env bash
echo "path gh-aw"
EOF
chmod +x "${root}/path/gh-aw"
code="$(run_script "${root}")"
if [ "${code}" -ne 0 ] && [ "$(cat "${root}/sentinel")" = "external sentinel" ] && [ -L "${root}/runner-temp/gh-aw/gh-aw" ]; then
  pass "rejects a symlinked output binary without changing its target"
else
  cat "${root}/out.log"
  fail "did not safely reject a symlinked output binary"
fi

root="$(new_env symlinked_output_directory)"
printf 'external sentinel\n' > "${root}/sentinel"
ln -s "${root}" "${root}/runner-temp/gh-aw"
code="$(run_script "${root}")"
if [ "${code}" -ne 0 ] && [ "$(cat "${root}/sentinel")" = "external sentinel" ] && [ -L "${root}/runner-temp/gh-aw" ]; then
  pass "rejects a symlinked output directory"
else
  cat "${root}/out.log"
  fail "did not safely reject a symlinked output directory"
fi

root="$(new_env symlinked_runner_temp_parent)"
mkdir -p "${root}/real-runner-temp"
printf 'external sentinel\n' > "${root}/sentinel"
ln -s "${root}/real-runner-temp" "${root}/linked-runner-temp"
cat > "${root}/path/gh-aw" <<'EOF'
#!/usr/bin/env bash
echo "path gh-aw"
EOF
chmod +x "${root}/path/gh-aw"
code="$(run_script "${root}" "" "" "${root}/linked-runner-temp")"
if [ "${code}" -ne 0 ] && [ "$(cat "${root}/sentinel")" = "external sentinel" ] && [ ! -e "${root}/real-runner-temp/gh-aw" ]; then
  pass "rejects a symlinked RUNNER_TEMP parent"
else
  cat "${root}/out.log"
  fail "did not safely reject a symlinked RUNNER_TEMP parent"
fi

echo
echo "Tests passed: ${TESTS_PASSED}"
echo "Tests failed: ${TESTS_FAILED}"

if [ "${TESTS_FAILED}" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
