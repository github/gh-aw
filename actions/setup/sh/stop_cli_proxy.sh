#!/usr/bin/env bash
set +o histexpand

# Stop CLI proxy container after AWF execution
# This script removes the awmg-cli-proxy container started by start_cli_proxy.sh.

set -e

# Do not remove the ingress filter if the credential-bearing listener might
# still be running. A missing container is already stopped.
CONTAINER_ID="$(docker container ls -a --filter 'name=^/awmg-cli-proxy$' --format '{{.ID}}')"
if [ -z "$CONTAINER_ID" ]; then
  echo "CLI proxy container is absent; retaining any legacy ingress filter"
  exit 0
fi
NETWORK_MODE="$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$CONTAINER_ID")"
docker rm -f "$CONTAINER_ID"

if [ "$NETWORK_MODE" = host ]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  # shellcheck source=proxy_network_lib.sh
  source "${SCRIPT_DIR}/proxy_network_lib.sh"
  if ! remove_cli_proxy_ingress_filter; then
    echo "::error::Failed to remove legacy CLI proxy ingress filter"
    exit 1
  fi
fi
