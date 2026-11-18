#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURE="$(mktemp -d "${SCRIPT_DIR}/.runtime-directory-test.XXXXXX")"
trap 'chmod -R u+rwX "${FIXTURE}"; rm -rf -- "${FIXTURE}"' EXIT
mkdir -p "${FIXTURE}/bin" "${FIXTURE}/runtime" "${FIXTURE}/other"
printf 'other invocation' > "${FIXTURE}/other/marker"
printf '#!/bin/sh\nexit 0\n' > "${FIXTURE}/bin/node"
printf '#!/bin/sh\necho "unexpected sudo" >&2\nexit 99\n' > "${FIXTURE}/bin/sudo"
chmod +x "${FIXTURE}/bin/node" "${FIXTURE}/bin/sudo"
export PATH="${FIXTURE}/bin:${PATH}"
export GH_AW_RUNTIME_ROOT="${FIXTURE}/runtime"

bash -n "${SCRIPT_DIR}/create_gh_aw_tmp_dir.sh"
bash "${SCRIPT_DIR}/create_gh_aw_tmp_dir.sh" >/dev/null
test -d "${GH_AW_RUNTIME_ROOT}/sandbox/firewall/logs"
test -d "${GH_AW_RUNTIME_ROOT}/memory-validation"
test "$(cat "${FIXTURE}/other/marker")" = 'other invocation'

# Unexpected symlink and nonwritable state must fail instead of being reclaimed.
rm -rf -- "${GH_AW_RUNTIME_ROOT}/memory-validation"
ln -s "${FIXTURE}/other" "${GH_AW_RUNTIME_ROOT}/memory-validation"
if bash "${SCRIPT_DIR}/create_gh_aw_tmp_dir.sh" >/dev/null 2>&1; then
  echo "Expected symlink rejection" >&2
  exit 1
fi
test "$(cat "${FIXTURE}/other/marker")" = 'other invocation'
rm -- "${GH_AW_RUNTIME_ROOT}/memory-validation"
mkdir "${GH_AW_RUNTIME_ROOT}/memory-validation"
chmod 000 "${GH_AW_RUNTIME_ROOT}/memory-validation"
if [[ "$(id -u)" != 0 ]] && bash "${SCRIPT_DIR}/create_gh_aw_tmp_dir.sh" >/dev/null 2>&1; then
  echo "Expected nonwritable directory rejection" >&2
  exit 1
fi
test -d "${GH_AW_RUNTIME_ROOT}/memory-validation"
echo "Private runtime directory checks passed"
