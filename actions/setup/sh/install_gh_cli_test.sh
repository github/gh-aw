#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Offline tests prove fail-closed key rotation, not freshness of the remote key.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_ROOT="${PWD}/.install-gh-cli-test-$$"
mkdir "$TEST_ROOT"
export GH_AW_RUNTIME_DIR="$TEST_ROOT"
trap 'rm -rf "$TEST_ROOT"' EXIT
mkdir "${TEST_ROOT}/bin" "${TEST_ROOT}/installer"

# Keep the installer off the host PATH, including package and privilege tools.
for tool in bash basename dirname head rm; do
  ln -s "$(command -v "$tool")" "${TEST_ROOT}/bin/${tool}"
done
if command -v sha256sum >/dev/null 2>&1; then
  HASH_TOOL=sha256sum
else
  HASH_TOOL=shasum
fi
ln -s "$(command -v "$HASH_TOOL")" "${TEST_ROOT}/bin/${HASH_TOOL}"

printf 'reviewed test signing key\n' >"${TEST_ROOT}/key.payload"
if [ "$HASH_TOOL" = sha256sum ]; then
  digest=$(sha256sum "${TEST_ROOT}/key.payload")
else
  digest=$(shasum -a 256 "${TEST_ROOT}/key.payload")
fi
digest="${digest%% *}"
# Substitute only the trusted literal in a copy; production accepts no digest override.
sed -E "s/^(verify_release_digest .* )[0-9a-f]{64}$/\1${digest}/" \
  "${SCRIPT_DIR}/install_gh_cli.sh" >"${TEST_ROOT}/installer/install_gh_cli.sh"
cp "${SCRIPT_DIR}/release_integrity.sh" "${TEST_ROOT}/installer/"
grep -qF "verify_release_digest \"\$key_file\" ${digest}" \
  "${TEST_ROOT}/installer/install_gh_cli.sh"

cat >"${TEST_ROOT}/bin/uname" <<'EOF'
#!/usr/bin/env bash
echo Linux
EOF
cat >"${TEST_ROOT}/bin/gh" <<'EOF'
#!/usr/bin/env bash
echo 'gh version test'
EOF
cat >"${TEST_ROOT}/bin/dpkg" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == --print-architecture ]] || exit 95
echo amd64
EOF
cat >"${TEST_ROOT}/bin/apt-get" <<'EOF'
#!/usr/bin/env bash
echo "apt-get must only be called through mocked sudo" >&2
exit 95
EOF
cat >"${TEST_ROOT}/bin/mktemp" <<'EOF'
#!/usr/bin/env bash
[[ "$#" -eq 0 ]] || exit 95
printf '%s\n' "${TEST_ROOT}/downloaded-key"
EOF
cat >"${TEST_ROOT}/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 8 && "$1" == --proto && "$2" == =https &&
   "$3" == --proto-redir && "$4" == =https && "$5" == -fsSL &&
   "$6" == https://cli.github.com/packages/githubcli-archive-keyring.gpg &&
   "$7" == -o && "$8" == "${TEST_ROOT}/downloaded-key" ]] || exit 95
if [[ "$KEY_MODE" == tampered ]]; then
  printf 'tampered signing key\n' >"$8"
else
  /bin/cp "${TEST_ROOT}/key.payload" "$8"
fi
EOF
cat >"${TEST_ROOT}/bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${TEST_ROOT}/sudo.log"
case "$*" in
  'apt-get update -qq'|'apt-get install -y gh') ;;
  "cp ${TEST_ROOT}/downloaded-key /usr/share/keyrings/githubcli-archive-keyring.gpg")
    /bin/cp "$2" "${TEST_ROOT}/installed-key"
    ;;
  'chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg')
    /bin/chmod go+r "${TEST_ROOT}/installed-key"
    ;;
  'tee /etc/apt/sources.list.d/github-cli.list')
    while IFS= read -r line; do
      printf '%s\n' "$line" >>"${TEST_ROOT}/source.list"
    done
    ;;
  *) echo "Unexpected privileged operation: $*" >&2; exit 95 ;;
esac
EOF
chmod +x "${TEST_ROOT}/bin/"*
export TEST_ROOT

run_installer() {
  rm -f "${TEST_ROOT}/installed-key" "${TEST_ROOT}/source.list"
  : >"${TEST_ROOT}/sudo.log"
  RUN_STATUS=0
  RUN_OUTPUT=$(PATH="${TEST_ROOT}/bin" KEY_MODE="$1" \
    "${BASH}" "${TEST_ROOT}/installer/install_gh_cli.sh" 2>&1) || RUN_STATUS=$?
}

run_installer valid
if [ "$RUN_STATUS" -ne 0 ]; then
  printf 'FAIL: matching key rejected: %s\n' "$RUN_OUTPUT" >&2
  exit 1
fi
cmp "${TEST_ROOT}/key.payload" "${TEST_ROOT}/installed-key"
grep -qxF 'deb [arch=amd64 signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main' \
  "${TEST_ROOT}/source.list"
grep -qxF 'apt-get install -y gh' "${TEST_ROOT}/sudo.log"
[ ! -e "${TEST_ROOT}/downloaded-key" ]
echo 'PASS: matching trusted pin installs keyring and configures the repository'

run_installer tampered
if [ "$RUN_STATUS" -eq 0 ] ||
  ! grep -qF 'ERROR: Trusted SHA256 verification failed' <<<"$RUN_OUTPUT"; then
  printf 'FAIL: tampered key did not fail closed: %s\n' "$RUN_OUTPUT" >&2
  exit 1
fi
# The initial package-list refresh is allowed, but no trust-store operation is.
[ "$(cat "${TEST_ROOT}/sudo.log")" = 'apt-get update -qq' ]
[ ! -e "${TEST_ROOT}/installed-key" ]
[ ! -e "${TEST_ROOT}/source.list" ]
[ ! -e "${TEST_ROOT}/downloaded-key" ]
echo 'PASS: tampered key reports ERROR before sudo cp or package installation'
