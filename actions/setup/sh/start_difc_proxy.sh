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
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  # shellcheck source=assert_docker_lease.sh
  source "${SCRIPT_DIR}/assert_docker_lease.sh" awmg-proxy
fi
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
HOST_STATE_FILE="${RUNNER_TEMP}/gh-aw/actions/proxy-host-state"
CA_CERT="/usr/local/share/ca-certificates/awmg-proxy-difc.crt"

mkdir -p "$PROXY_LOG_DIR" "$MCP_LOG_DIR"

derive_proxy_upstream_env

echo "Starting DIFC proxy container: $CONTAINER_IMAGE"
echo "Using DIFC proxy upstream host: ${GH_HOST} (API: ${GITHUB_API_URL})"

# Remove any existing container to avoid name conflicts on cancelled/retried jobs.
docker rm -f awmg-proxy 2>/dev/null || true

DOCKER_NETWORK_ARGS=(--network host)
if [ "${GH_AW_NETWORK_ISOLATION:-false}" = "true" ]; then
  DOCKER_NETWORK_ARGS=(--network bridge -p 127.0.0.1:18443:18443)
fi

docker run -d --name awmg-proxy "${DOCKER_NETWORK_ARGS[@]}" \
  --label "io.github.gh-aw.invocation=${GH_AW_DOCKER_OWNER:-}" \
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

# Wait for TLS cert + health check (up to 30s)
CA_INSTALLED=false
CA_HASH=""
REMOTE_ADDED=false
PROXY_READY=false
persist_host_state() {
  if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" && ( "$CA_INSTALLED" == "true" || "$REMOTE_ADDED" == "true" ) ]]; then
    local existing_owner tmp_file
    if [[ -L "${RUNNER_TEMP}/gh-aw/actions" || -L "$HOST_STATE_FILE" || ( -e "$HOST_STATE_FILE" && ! -f "$HOST_STATE_FILE" ) ]]; then
      echo "::error::Refusing to write an unsafe DIFC proxy host-state file." >&2
      return 1
    fi
    if [[ -f "$HOST_STATE_FILE" ]]; then
      IFS=$'\t' read -r existing_owner _ < "$HOST_STATE_FILE"
      if [[ "$existing_owner" != "$GH_AW_DOCKER_OWNER" ]]; then
        echo "::error::Existing DIFC proxy host-state belongs to another invocation." >&2
        return 1
      fi
    fi
    tmp_file="${HOST_STATE_FILE}.${GH_AW_DOCKER_OWNER}.tmp"
    if [[ -e "$tmp_file" || -L "$tmp_file" ]] || ! (set -o noclobber; printf '%s\t%s\t%s\n' "${GH_AW_DOCKER_OWNER}" "${CA_HASH}" "$REMOTE_ADDED" > "$tmp_file"); then
      echo "::error::Unable to stage DIFC proxy host-state." >&2
      return 1
    fi
    chmod 600 "$tmp_file"
    mv -f "$tmp_file" "$HOST_STATE_FILE"
  fi
}
for i in $(seq 1 30); do
  if [ -f "$PROXY_LOG_DIR/proxy-tls/ca.crt" ]; then
    if [ "$CA_INSTALLED" = "false" ]; then
      if command -v sudo >/dev/null 2>&1 && command -v update-ca-certificates >/dev/null 2>&1; then
        if [ -e "$CA_CERT" ]; then
          echo "::warning::Existing DIFC proxy CA was not replaced; preserving runner trust state"
        elif [[ ! -e "$CA_CERT" ]]; then
          CA_INSTALLED=true
          if command -v sha256sum >/dev/null 2>&1; then
            CA_HASH="$(sha256sum "$PROXY_LOG_DIR/proxy-tls/ca.crt" | awk '{print $1}')"
          else
            CA_HASH="$(shasum -a 256 "$PROXY_LOG_DIR/proxy-tls/ca.crt" | awk '{print $1}')"
          fi
          persist_host_state
          if sudo cp "$PROXY_LOG_DIR/proxy-tls/ca.crt" "$CA_CERT"; then
            sudo update-ca-certificates || echo "::warning::Failed to refresh the system trust store"
          else
            echo "::warning::Failed to install DIFC proxy CA into system trust store; continuing without system CA update"
          fi
        else
          echo "::warning::Failed to install DIFC proxy CA into system trust store; continuing without system CA update"
        fi
      else
        echo "::warning::Cannot install DIFC proxy CA (missing sudo or update-ca-certificates); continuing without system CA update"
      fi
    fi
    if curl -sf "https://localhost:18443/api/v3/health" -o /dev/null 2>/dev/null; then
      echo "DIFC proxy ready on port 18443"
      if ! git remote get-url proxy >/dev/null 2>&1 && git remote add proxy "https://localhost:18443/${GITHUB_REPOSITORY}.git"; then
        REMOTE_ADDED=true
        persist_host_state
      fi
      PROXY_READY=true
      break
    fi
  fi
  sleep 1
done

persist_host_state

if [ "$PROXY_READY" = "false" ]; then
  echo "::warning::DIFC proxy failed to start, falling back to direct API access"
  docker logs awmg-proxy 2>&1 | tail -20 || true
  docker rm -f awmg-proxy 2>/dev/null || true
fi
