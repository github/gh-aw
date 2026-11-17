#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

: "${GH_AW_RUNTIME_DIR:?Setup must allocate a private runtime first}"
if [[ ! -d "$GH_AW_RUNTIME_DIR" || -L "$GH_AW_RUNTIME_DIR" || ! -O "$GH_AW_RUNTIME_DIR" ]]; then
  echo "::error::Private runtime must be a real directory owned by the runner" >&2
  exit 1
fi

# Do not reclaim stale state or broaden the 0700 runtime boundary.
for relative in agent memory-validation sandbox sandbox/agent sandbox/agent/logs sandbox/firewall sandbox/firewall/logs sandbox/firewall/audit; do
  directory="${GH_AW_RUNTIME_DIR}/${relative}"
  if [[ -L "$directory" || ( -e "$directory" && ( ! -O "$directory" || ! -w "$directory" ) ) ]]; then
    echo "::error::Refusing foreign or symlinked runtime state: ${directory}" >&2
    exit 1
  fi
  mkdir -p -- "$directory"
done
echo "Created private agentic workflow runtime directories"
