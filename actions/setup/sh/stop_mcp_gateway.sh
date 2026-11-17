#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/runtime_docker.sh"
CONTAINER_NAME="$(gh_aw_container_name awmg-mcpg)"

# A stale numeric PID can belong to another job. Only stop the exact container
# whose immutable ID and ownership label match this setup invocation.
CONTAINER_ID="$(gh_aw_owned_container_id "$CONTAINER_NAME")" || exit 0
if [[ -n "${MCP_GATEWAY_PORT:-}" && -n "${MCP_GATEWAY_AGENT_ID:-}" ]]; then
  curl -fsS --max-time 10 -X POST \
    -H "Authorization: ${MCP_GATEWAY_AGENT_ID}" \
    "http://localhost:${MCP_GATEWAY_PORT}/close" >/dev/null 2>&1 || true
fi
docker rm -f "$CONTAINER_ID" 2>/dev/null || true
