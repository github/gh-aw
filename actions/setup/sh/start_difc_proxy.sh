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

DOCKER_NETWORK_ARGS=(--network host)
PROXY_LISTEN=127.0.0.1:18443
if [ "${GH_AW_NETWORK_ISOLATION:-false}" = "true" ]; then
  DOCKER_NETWORK_ARGS=(--network bridge -p 127.0.0.1:18443:18443)
  PROXY_LISTEN=0.0.0.0:18443
fi

docker run -d --name awmg-proxy "${DOCKER_NETWORK_ARGS[@]}" \
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
    --listen "$PROXY_LISTEN" \
    --log-dir "$MCP_LOG_DIR" \
    --tls --tls-dir "$PROXY_LOG_DIR/proxy-tls" \
    --guards-mode filter \
    --trusted-bots github-actions[bot],github-actions,dependabot[bot],copilot

# Wait for TLS cert + health check (up to 30s)
CA_INSTALLED=false
PROXY_READY=false
for i in $(seq 1 30); do
  if [ -f "$PROXY_LOG_DIR/proxy-tls/ca.crt" ]; then
    if [ "$CA_INSTALLED" = "false" ]; then
      if command -v sudo >/dev/null 2>&1 && command -v update-ca-certificates >/dev/null 2>&1; then
        if sudo cp "$PROXY_LOG_DIR/proxy-tls/ca.crt" /usr/local/share/ca-certificates/awmg-proxy-difc.crt && \
           sudo update-ca-certificates; then
          CA_INSTALLED=true
        else
          echo "::warning::Failed to install DIFC proxy CA into system trust store; continuing without system CA update"
        fi
      else
        echo "::warning::Cannot install DIFC proxy CA (missing sudo or update-ca-certificates); continuing without system CA update"
      fi
    fi
    if curl -sf "https://localhost:18443/api/v3/health" -o /dev/null 2>/dev/null; then
      echo "DIFC proxy ready on port 18443"
      git remote add proxy "https://localhost:18443/${GITHUB_REPOSITORY}.git" || true
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
