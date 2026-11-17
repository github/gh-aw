#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/create_gh_aw_tmp_dir.sh"
TEST_ROOT="$(mktemp -d "${PWD}/.runtime-directory-tests.XXXXXX")"
export GH_AW_RUNTIME_DIR="${TEST_ROOT}/runtime"
trap 'chmod -R u+rwX "$TEST_ROOT"; rm -rf "$TEST_ROOT"' EXIT

bash -n "$SCRIPT"
mkdir -m 700 "$GH_AW_RUNTIME_DIR"
bash "$SCRIPT"
for directory in agent memory-validation sandbox/agent/logs sandbox/firewall/logs sandbox/firewall/audit; do
  test -d "${GH_AW_RUNTIME_DIR}/${directory}"
done
mode="$(node -e 'process.stdout.write(String(require("fs").statSync(process.env.GH_AW_RUNTIME_DIR).mode & 0o777))')"
test "$mode" = 448
bash "$SCRIPT"

# Ancestor symlinks must never redirect runtime writes into another invocation.
rm -rf "${GH_AW_RUNTIME_DIR}/sandbox"
mkdir "${TEST_ROOT}/foreign"
printf 'sentinel\n' > "${TEST_ROOT}/foreign/sentinel"
ln -s "${TEST_ROOT}/foreign" "${GH_AW_RUNTIME_DIR}/sandbox"
if bash "$SCRIPT"; then
  echo "Expected symlinked runtime state to be rejected" >&2
  exit 1
fi
test "$(cat "${TEST_ROOT}/foreign/sentinel")" = sentinel
test ! -e "${TEST_ROOT}/foreign/firewall"
echo "Private runtime directory tests passed"
