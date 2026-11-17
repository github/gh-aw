#!/usr/bin/env bash
set +o histexpand

# Stop DIFC proxy for pre-agent gh CLI steps
# This script stops only the invocation-owned proxy and removes its local git CA setting.
# The proxy must be stopped before the MCP gateway starts to avoid double-filtering traffic.
#
# This script does NOT modify $GITHUB_ENV. The proxy routing env vars (GH_HOST,
# GITHUB_API_URL, etc.) are injected as step-level env by the compiler and are
# never written to $GITHUB_ENV, so no restore/clear is needed here.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/runtime_docker.sh"
CONTAINER_NAME="$(gh_aw_container_name awmg-proxy)"
gh_aw_remove_container "$CONTAINER_NAME" || true
git remote remove proxy 2>/dev/null || true
git config --local --unset-all "http.https://localhost:${GH_AW_CLI_PROXY_PORT:?}/.sslCAInfo" 2>/dev/null || true

echo "DIFC proxy stopped"
