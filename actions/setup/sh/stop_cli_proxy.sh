#!/usr/bin/env bash
set +o histexpand

# Stop CLI proxy container after AWF execution
# This script removes the awmg-cli-proxy container started by start_cli_proxy.sh.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/runtime_docker.sh"
CONTAINER_NAME="$(gh_aw_container_name awmg-cli-proxy)"
gh_aw_remove_container "$CONTAINER_NAME" || true
