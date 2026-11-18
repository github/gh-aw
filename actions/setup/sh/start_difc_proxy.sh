#!/usr/bin/env bash
set +o histexpand

# Start DIFC proxy for pre-agent gh CLI steps
# This script starts the awmg proxy container that routes gh CLI calls
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

PROXY_LOG_DIR=/tmp/gh-aw/proxy-logs
MCP_LOG_DIR=/tmp/gh-aw/mcp-logs

mkdir -p "$PROXY_LOG_DIR" "$MCP_LOG_DIR"

derive_proxy_upstream_env

echo "Starting DIFC proxy container: $CONTAINER_IMAGE"
echo "Using DIFC proxy upstream host: ${GH_HOST} (API: ${GITHUB_API_URL})"

# Remove any existing container to avoid name conflicts on cancelled/retried jobs.
docker rm -f awmg-proxy 2>/dev/null || true

DOCKER_NETWORK_ARGS=(--network bridge -p 127.0.0.1::18443)

OWNER_ARGS=()
if [[ -n "${GH_AW_RUNTIME_ROOT:-}" && -f "$GH_AW_RUNTIME_ROOT/.gh-aw-runtime-owner" ]]; then
  OWNER_ARGS=(--label "com.gh.aw.owner=$(<"$GH_AW_RUNTIME_ROOT/.gh-aw-runtime-owner")")
fi

docker run -d --name awmg-proxy "${DOCKER_NETWORK_ARGS[@]}" "${OWNER_ARGS[@]}" \
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
    --listen 0.0.0.0:18443 \
    --log-dir "$MCP_LOG_DIR" \
    --tls --tls-dir "$PROXY_LOG_DIR/proxy-tls" \
    --guards-mode filter \
    --trusted-bots github-actions[bot],github-actions,dependabot[bot],copilot,github-agentic-autofix[bot],github-agentic-autofix

PROXY_PORT="$(docker port awmg-proxy 18443/tcp | head -1)"
PROXY_PORT="${PROXY_PORT##*:}"
if [[ ! "${PROXY_PORT}" =~ ^[0-9]+$ ]]; then
  echo "::error::Unable to determine invocation-private DIFC proxy port"
  exit 1
fi
printf 'GH_AW_DIFC_PROXY_PORT=%s\n' "${PROXY_PORT}" >> "${GITHUB_ENV}"

# Wait for TLS cert + health check (up to 30s). Trust the CA only for this
# invocation; never install it into the shared host trust store.
PROXY_READY=false
for i in $(seq 1 30); do
  if [ -f "$PROXY_LOG_DIR/proxy-tls/ca.crt" ]; then
    if curl -sf --cacert "$PROXY_LOG_DIR/proxy-tls/ca.crt" "https://localhost:${PROXY_PORT}/api/v3/health" -o /dev/null 2>/dev/null; then
      echo "DIFC proxy ready on port ${PROXY_PORT}"
      git remote add proxy "https://localhost:${PROXY_PORT}/${GITHUB_REPOSITORY}.git" || true
      PROXY_READY=true
      break
    fi
  fi
  sleep 1
done

if [ "$PROXY_READY" = "false" ]; then
  echo "::warning::DIFC proxy failed to start, falling back to direct API access"
  docker logs awmg-proxy 2>&1 | tail -20 || true
  docker rm -f awmg-proxy 2>/dev/null || true
fi
