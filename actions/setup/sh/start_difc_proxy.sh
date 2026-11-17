#!/usr/bin/env bash
set +o histexpand

# Start DIFC proxy for pre-agent gh CLI steps
# This script starts an invocation-namespaced proxy container routing gh CLI calls
# through DIFC integrity filtering before the agent runs.
#
# This script does NOT modify $GITHUB_ENV. The compiler injects step-level
# env: blocks (GH_HOST, GITHUB_API_URL, GITHUB_GRAPHQL_URL, NODE_EXTRA_CA_CERTS,
# GH_REPO) directly on each custom step so that the proxy address is scoped to
# proxied steps only and does not overwrite GHE host values.
#
# Environment:
#   DIFC_PROXY_POLICY   - JSON guard policy string
#   DIFC_PROXY_IMAGE    - Container image to use (e.g., ghcr.io/github/gh-aw-mcpg:v0.2.2)
#   GH_TOKEN            - GitHub token passed to the proxy container
#   GITHUB_SERVER_URL   - GitHub server URL for upstream routing (e.g. https://github.com or https://TENANT.ghe.com)
#   GITHUB_REPOSITORY   - Repository name (owner/repo) for git remote

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/runtime_docker.sh"
CONTAINER_NAME="$(gh_aw_container_name awmg-proxy)"
PROXY_PORT="${GH_AW_CLI_PROXY_PORT:?}"
# shellcheck source=proxy_env_lib.sh
source "${SCRIPT_DIR}/proxy_env_lib.sh"

POLICY="${DIFC_PROXY_POLICY:-}"
CONTAINER_IMAGE="${DIFC_PROXY_IMAGE:-}"

if [ -z "$POLICY" ]; then
  echo "::warning::DIFC proxy policy not specified, skipping proxy start"
  exit 0
fi

if [ -z "$CONTAINER_IMAGE" ]; then
  echo "::warning::DIFC proxy container image not specified, skipping proxy start"
  exit 0
fi

PROXY_LOG_DIR="${GH_AW_RUNTIME_DIR:?}/proxy-logs"
MCP_LOG_DIR="${GH_AW_RUNTIME_DIR:?}/mcp-logs"

mkdir -p "$PROXY_LOG_DIR" "$MCP_LOG_DIR"

derive_proxy_upstream_env

echo "Starting DIFC proxy container: $CONTAINER_IMAGE"
echo "Using DIFC proxy upstream host: ${GH_HOST} (API: ${GITHUB_API_URL})"

DOCKER_NETWORK_ARGS=(--network host)
if [ "${GH_AW_NETWORK_ISOLATION:-false}" = "true" ]; then
  DOCKER_NETWORK_ARGS=(--network bridge -p "127.0.0.1:${PROXY_PORT}:${PROXY_PORT}")
fi

docker run -d --name "$CONTAINER_NAME" --label "gh-aw.runtime-id=${GH_AW_RUNTIME_ID:?}" "${DOCKER_NETWORK_ARGS[@]}" \
  --user "$(id -u):$(id -g)" \
  -e GH_TOKEN \
  -e GH_HOST \
  -e GITHUB_HOST \
  -e GITHUB_ENTERPRISE_HOST \
  -e GITHUB_SERVER_URL \
  -e GITHUB_API_URL \
  -e GITHUB_GRAPHQL_URL \
  -e GITHUB_COPILOT_BASE_URL \
  -e DEBUG='*' \
  -v "$PROXY_LOG_DIR:$PROXY_LOG_DIR" \
  -v "$MCP_LOG_DIR:$MCP_LOG_DIR" \
  "$CONTAINER_IMAGE" proxy \
    --policy "$POLICY" \
    --listen "0.0.0.0:${PROXY_PORT}" \
    --log-dir "$MCP_LOG_DIR" \
    --tls --tls-dir "$PROXY_LOG_DIR/proxy-tls" \
    --guards-mode filter \
    --trusted-bots github-actions[bot],github-actions,dependabot[bot],copilot,github-agentic-autofix[bot],github-agentic-autofix

# Wait for TLS cert + health check (up to 30s)
PROXY_READY=false
for i in $(seq 1 30); do
  if [ -f "$PROXY_LOG_DIR/proxy-tls/ca.crt" ]; then
    if curl -sf --cacert "$PROXY_LOG_DIR/proxy-tls/ca.crt" "https://localhost:${PROXY_PORT}/api/v3/health" -o /dev/null 2>/dev/null; then
      echo "DIFC proxy ready on port ${PROXY_PORT}"
      git config --local "http.https://localhost:${PROXY_PORT}/.sslCAInfo" "$PROXY_LOG_DIR/proxy-tls/ca.crt"
      git remote add proxy "https://localhost:${PROXY_PORT}/${GITHUB_REPOSITORY}.git" || true
      PROXY_READY=true
      break
    fi
  fi
  sleep 1
done

if [ "$PROXY_READY" = "false" ]; then
  echo "::warning::DIFC proxy failed to start, falling back to direct API access"
  docker logs "$CONTAINER_NAME" 2>&1 | tail -20 || true
  gh_aw_remove_container "$CONTAINER_NAME" 2>/dev/null || true
fi
