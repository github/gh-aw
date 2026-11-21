#!/usr/bin/env bash
# Sourced by runtime scripts in both the source and copied action layouts.
if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  GH_AW_LEASE_SCRIPT="$(dirname "${BASH_SOURCE[0]}")/docker_lease.cjs"
  if [[ ! -f "$GH_AW_LEASE_SCRIPT" ]]; then
    GH_AW_LEASE_SCRIPT="$(dirname "${BASH_SOURCE[0]}")/../js/docker_lease.cjs"
  fi
  node "$GH_AW_LEASE_SCRIPT" assert "${1:-}" || exit 1
fi
