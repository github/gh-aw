#!/usr/bin/env bash
# Sourced by runtime scripts in both the source and copied action layouts.
if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  GH_AW_LEASE_SCRIPT="$(dirname "${BASH_SOURCE[0]}")/docker_lease.cjs"
  if [[ ! -f "$GH_AW_LEASE_SCRIPT" ]]; then
    GH_AW_LEASE_SCRIPT="$(dirname "${BASH_SOURCE[0]}")/../js/docker_lease.cjs"
  fi
  GH_AW_NODE="${GH_AW_NODE:-${GH_AW_NODE_BIN:-$(command -v node 2>/dev/null || true)}}"
  if [[ -z "$GH_AW_NODE" || ! -x "$GH_AW_NODE" ]]; then
    echo "::error::Node.js is required to validate the shared-daemon lease." >&2
    exit 1
  fi
  "$GH_AW_NODE" "$GH_AW_LEASE_SCRIPT" assert "${1:-}" || exit 1
fi
