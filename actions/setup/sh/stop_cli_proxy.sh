#!/usr/bin/env bash
set +o histexpand

# Stop CLI proxy container after AWF execution
# This script removes the awmg-cli-proxy container started by start_cli_proxy.sh.

set -e

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  # shellcheck source=assert_docker_lease.sh
  source "$(dirname "${BASH_SOURCE[0]}")/assert_docker_lease.sh" awmg-cli-proxy
fi

docker rm -f awmg-cli-proxy 2>/dev/null || true
