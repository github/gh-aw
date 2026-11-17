#!/usr/bin/env bash
set +o histexpand

# Start DIFC proxy on the host for AWF CLI proxy sidecar
# This script starts the awmg proxy container so AWF's cli-proxy container
# can connect to its invocation-private host port for gh CLI access.
#
# This script exports GH_HOST (and related vars) within the script for use when
# launching the proxy container, but does NOT write to $GITHUB_ENV and the
# exports do not persist beyond this script.
#
# Environment:
#   CLI_PROXY_POLICY    - JSON guard policy string
#   CLI_PROXY_IMAGE     - Container image to use (e.g., ghcr.io/github/gh-aw-mcpg:v0.2.2)
#   GH_TOKEN            - GitHub token passed to the proxy container
#   GITHUB_SERVER_URL   - GitHub server URL for upstream routing
#   HTTP_PROXY, HTTPS_PROXY, NO_PROXY (and lowercase forms) - Optional upstream proxy settings
#   GH_AW_SECRET_NAMES  - Optional comma-separated custom secret names to redact
#   SECRET_*            - Additional supplied secret values to redact

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/runtime_docker.sh"
CONTAINER_NAME="$(gh_aw_container_name awmg-cli-proxy)"
PROXY_PORT="${GH_AW_CLI_PROXY_PORT:?}"

# Diagnostics remain within this invocation and are deleted before artifact upload.
if ! DIAGNOSTIC_DIR="$(mktemp -d "${GH_AW_RUNTIME_DIR:?}/.cli-proxy.XXXXXX" 2>/dev/null)"; then
  echo "::error::CLI proxy diagnostics could not be captured safely"
  exit 1
fi
validate_diagnostic_directory() {
  DIAGNOSTIC_DIR="$(cd "$DIAGNOSTIC_DIR" && pwd -P)" || return 1
  [[ "${DIAGNOSTIC_DIR%/*}" == "${GH_AW_RUNTIME_DIR:?}" && ! -L "$DIAGNOSTIC_DIR" && -O "$DIAGNOSTIC_DIR" ]]
}
if ! validate_diagnostic_directory 2>/dev/null; then
  rmdir "$DIAGNOSTIC_DIR" 2>/dev/null || true
  echo "::error::CLI proxy diagnostics could not be captured safely"
  exit 1
fi
if ! mkfifo "$DIAGNOSTIC_DIR/capture" 2>/dev/null; then
  rmdir "$DIAGNOSTIC_DIR" 2>/dev/null || true
  echo "::error::CLI proxy diagnostics could not be captured safely"
  exit 1
fi
(
  capture_status=0
  node "${SCRIPT_DIR}/redact_cli_proxy_diagnostics.cjs" --capture || capture_status=$?
  cat > /dev/null
  exit "$capture_status"
) < "$DIAGNOSTIC_DIR/capture" > "$DIAGNOSTIC_DIR/startup.log" 2>/dev/null &
CAPTURE_PID=$!
exec 3>&1 4>&2
publish_diagnostics() {
  local status=$?
  local capture_status=0
  exec 1>&3 2>&4
  wait "$CAPTURE_PID" || capture_status=$?
  if [ "$capture_status" -eq 0 ]; then
    cat "$DIAGNOSTIC_DIR/startup.log" >&3
    printf '\n' >&3
    if [ "$status" -ne 0 ]; then
      echo "::error::CLI proxy startup failed (exit status $status)" >&4
    fi
  elif [ "$capture_status" -eq 2 ]; then
    echo "::error::CLI proxy diagnostics withheld because capture exceeded the byte limit" >&4
    status=1
  else
    echo "::error::CLI proxy diagnostics withheld because secret redaction failed" >&4
    status=1
  fi
  if ! rm -rf "$DIAGNOSTIC_DIR" >/dev/null 2>&1; then
    echo "::warning::CLI proxy diagnostic cleanup failed" >&4
    status=1
  fi
  exit "$status"
}
trap publish_diagnostics EXIT
exec >"$DIAGNOSTIC_DIR/capture" 2>&1

# shellcheck source=proxy_env_lib.sh
source "${SCRIPT_DIR}/proxy_env_lib.sh"

POLICY="${CLI_PROXY_POLICY:-}"
CONTAINER_IMAGE="${CLI_PROXY_IMAGE:-}"

if [ -z "$CONTAINER_IMAGE" ]; then
  echo "::warning::CLI proxy container image not specified, skipping proxy start"
  exit 0
fi

TLS_DIR="${GH_AW_RUNTIME_DIR:?}/difc-proxy-tls"
MCP_LOG_DIR="${GH_AW_RUNTIME_DIR:?}/mcp-logs"

mkdir -p "$TLS_DIR" "$MCP_LOG_DIR"

derive_proxy_upstream_env

echo "Starting CLI proxy container: $CONTAINER_IMAGE"
echo "Using CLI proxy upstream host: ${GH_HOST} (API: ${GITHUB_API_URL})"

# Build docker run command arguments
POLICY_ARGS=()
if [ -n "$POLICY" ]; then
  POLICY_ARGS=(--policy "$POLICY")
fi

DOCKER_NETWORK_ARGS=(--network host)
if [ "${GH_AW_NETWORK_ISOLATION:-false}" = "true" ]; then
  DOCKER_NETWORK_ARGS=(--network bridge -p "127.0.0.1:${PROXY_PORT}:${PROXY_PORT}")
fi

PROXY_ENV_ARGS=()
for proxy_var in HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy; do
  if [ "${!proxy_var+x}" = x ]; then
    PROXY_ENV_ARGS+=(-e "$proxy_var")
  fi
done

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
  "${PROXY_ENV_ARGS[@]}" \
  -e DEBUG='*' \
  -v "$TLS_DIR:$TLS_DIR" \
  -v "$MCP_LOG_DIR:$MCP_LOG_DIR" \
  "$CONTAINER_IMAGE" proxy \
    "${POLICY_ARGS[@]}" \
    --listen "0.0.0.0:${PROXY_PORT}" \
    --log-dir "$MCP_LOG_DIR" \
    --tls --tls-dir "$TLS_DIR" \
    --guards-mode filter \
    --trusted-bots github-actions[bot],github-actions,dependabot[bot],copilot,copilot-pull-request-reviewer,github-agentic-autofix[bot],github-agentic-autofix

# Wait for TLS cert + health check (up to 30s)
PROXY_READY=false
for i in $(seq 1 30); do
  if [ -f "$TLS_DIR/ca.crt" ]; then
    if curl -sf --noproxy '*' --connect-timeout 2 --max-time 2 --cacert "$TLS_DIR/ca.crt" "https://localhost:${PROXY_PORT}/api/v3/health" -o /dev/null 2>/dev/null; then
      PROXY_READY=true
      break
    fi
  fi
  sleep 1
done

if [ "$PROXY_READY" = "false" ]; then
  echo "::error::CLI proxy failed to start within 30s" >&4
  docker logs "$CONTAINER_NAME" 2>&1 || true
  gh_aw_remove_container "$CONTAINER_NAME" 2>/dev/null || true
  exit 1
fi

# Exercise the proxy's upstream HTTP client, not just its local health endpoint.
UPSTREAM_READY=false
for attempt in 1 2 3; do
  if curl -sf --noproxy '*' --connect-timeout 5 --max-time 15 \
    --cacert "$TLS_DIR/ca.crt" "https://localhost:${PROXY_PORT}/api/v3/rate_limit" -o /dev/null 2>/dev/null; then
    UPSTREAM_READY=true
    break
  fi
  if [ "$attempt" -lt 3 ]; then
    sleep 1
  fi
done

if [ "$UPSTREAM_READY" = "false" ]; then
  echo "::error::CLI proxy could not reach the upstream GitHub API. Check runner connectivity and HTTP(S)_PROXY/NO_PROXY settings." >&4
  docker logs "$CONTAINER_NAME" 2>&1 || true
  gh_aw_remove_container "$CONTAINER_NAME" 2>/dev/null || true
  exit 1
fi

echo "CLI proxy ready on port ${PROXY_PORT}"
