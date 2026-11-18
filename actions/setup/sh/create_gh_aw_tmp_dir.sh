#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUNTIME_HELPER="${SCRIPT_DIR}/runtime_isolation.cjs"
if [[ ! -f "${RUNTIME_HELPER}" ]]; then
  RUNTIME_HELPER="${SCRIPT_DIR}/../js/runtime_isolation.cjs"
fi

# Allocation is done before any consumer. Never reclaim legacy or stale paths.
node "${RUNTIME_HELPER}" validate
for directory in agent memory-validation sandbox/agent/logs sandbox/firewall/logs sandbox/firewall/audit; do
  target="${GH_AW_RUNTIME_ROOT}/${directory}"
  current="${GH_AW_RUNTIME_ROOT}"
  IFS=/ read -r -a components <<< "${directory}"
  for component in "${components[@]}"; do
    current="${current}/${component}"
    if [[ -L "${current}" || ( -e "${current}" && ( ! -d "${current}" || ! -O "${current}" || ! -w "${current}" ) ) ]]; then
      echo "::error::Invocation runtime directory is unsafe or not writable: ${current}" >&2
      exit 1
    fi
    mkdir -p "${current}"
  done
  echo "Prepared ${target}"
done
