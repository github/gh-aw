#!/usr/bin/env bash
# Start MCP Gateway
# This script starts the MCP gateway process that proxies MCP servers through a unified HTTP endpoint
# Following the MCP Gateway Specification: https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/mcp-gateway.md
# Per MCP Gateway Specification v1.0.0: Only container-based execution is supported.
#
# This script reads the MCP configuration from stdin and pipes it to the gateway container.

set -e

# Timing helper functions
print_timing() {
  local start_time=$1
  local label=$2
  local end_time=$(date +%s%3N)
  local duration=$((end_time - start_time))
  echo "⏱️  TIMING: $label took ${duration}ms"
}

# Required environment variables:
# - MCP_GATEWAY_DOCKER_COMMAND: Container image to run (required)
# - MCP_GATEWAY_API_KEY: API key for gateway authentication (required for converter scripts)

# Validate that container is specified (command execution is not supported per spec)
if [ -z "$MCP_GATEWAY_DOCKER_COMMAND" ]; then
  echo "ERROR: MCP_GATEWAY_DOCKER_COMMAND must be set (command-based execution is not supported per MCP Gateway Specification v1.0.0)"
  exit 1
fi

# Create logs directory for gateway
mkdir -p /tmp/gh-aw/mcp-logs
mkdir -p /tmp/gh-aw/mcp-config

# Validate container syntax first (before accessing files)
# Container should be a valid docker command starting with "docker run"
if ! echo "$MCP_GATEWAY_DOCKER_COMMAND" | grep -qE '^docker run'; then
  echo "ERROR: MCP_GATEWAY_DOCKER_COMMAND has incorrect syntax"
  echo "Expected: docker run command with image and arguments"
  echo "Got: $MCP_GATEWAY_DOCKER_COMMAND"
  exit 1
fi

# Validate container command includes required flags
if ! echo "$MCP_GATEWAY_DOCKER_COMMAND" | grep -qE -- '-i'; then
  echo "ERROR: MCP_GATEWAY_DOCKER_COMMAND must include -i flag for interactive mode"
  exit 1
fi

if ! echo "$MCP_GATEWAY_DOCKER_COMMAND" | grep -qE -- '--rm'; then
  echo "ERROR: MCP_GATEWAY_DOCKER_COMMAND must include --rm flag for cleanup"
  exit 1
fi

if ! echo "$MCP_GATEWAY_DOCKER_COMMAND" | grep -qE -- '--network'; then
  echo "ERROR: MCP_GATEWAY_DOCKER_COMMAND must include --network flag for networking"
  exit 1
fi

# Start overall timing
SCRIPT_START_TIME=$(date +%s%3N)

# Read MCP configuration from stdin
echo "Reading MCP configuration from stdin..."
CONFIG_READ_START=$(date +%s%3N)
MCP_CONFIG=$(cat)
print_timing $CONFIG_READ_START "Configuration read from stdin"
echo ""

# Log the configuration for debugging
echo "-------START MCP CONFIG-----------"
echo "$MCP_CONFIG"
echo "-------END MCP CONFIG-----------"
echo ""

# Validate configuration is valid JSON
CONFIG_VALIDATION_START=$(date +%s%3N)
if ! echo "$MCP_CONFIG" | jq empty 2>/tmp/gh-aw/mcp-config/jq-error.log; then
  echo "ERROR: Configuration is not valid JSON"
  echo ""
  echo "JSON validation error:"
  if [ -f /tmp/gh-aw/mcp-config/jq-error.log ]; then
    cat /tmp/gh-aw/mcp-config/jq-error.log
  fi
  echo ""
  echo "Configuration content:"
  echo "$MCP_CONFIG" | head -50
  if [ $(echo "$MCP_CONFIG" | wc -l) -gt 50 ]; then
    echo "... (truncated, showing first 50 lines)"
  fi
  exit 1
fi

# Validate gateway section exists and has required fields
echo "Validating gateway configuration..."
if ! echo "$MCP_CONFIG" | jq -e '.gateway' >/dev/null 2>&1; then
  echo "ERROR: Configuration is missing required 'gateway' section"
  echo "Per MCP Gateway Specification v1.0.0 section 4.1.3, the gateway section is required"
  exit 1
fi

if ! echo "$MCP_CONFIG" | jq -e '.gateway.port' >/dev/null 2>&1; then
  echo "ERROR: Gateway configuration is missing required 'port' field"
  exit 1
fi

if ! echo "$MCP_CONFIG" | jq -e '.gateway.domain' >/dev/null 2>&1; then
  echo "ERROR: Gateway configuration is missing required 'domain' field"
  exit 1
fi

if ! echo "$MCP_CONFIG" | jq -e '.gateway.apiKey' >/dev/null 2>&1; then
  echo "ERROR: Gateway configuration is missing required 'apiKey' field"
  exit 1
fi

echo "Configuration validated successfully"
print_timing $CONFIG_VALIDATION_START "Configuration validation"
echo ""

# Set MCP_GATEWAY_LOG_DIR environment variable for use by the gateway
export MCP_GATEWAY_LOG_DIR="/tmp/gh-aw/mcp-logs/"

# Start gateway process with container
echo "Starting gateway with container: $MCP_GATEWAY_DOCKER_COMMAND"
echo "Full docker command: $MCP_GATEWAY_DOCKER_COMMAND"
echo ""
GATEWAY_START_TIME=$(date +%s%3N)
# Note: MCP_GATEWAY_DOCKER_COMMAND is the full docker command with all flags, mounts, and image
# Pass MCP_GATEWAY_LOG_DIR to the container via -e flag
echo "$MCP_CONFIG" | MCP_GATEWAY_LOG_DIR="$MCP_GATEWAY_LOG_DIR" $MCP_GATEWAY_DOCKER_COMMAND \
  > /tmp/gh-aw/mcp-config/gateway-output.json 2> /tmp/gh-aw/mcp-logs/stderr.log &

GATEWAY_PID=$!
echo "Gateway started with PID: $GATEWAY_PID"
print_timing $GATEWAY_START_TIME "Gateway container launch"
echo "Verifying gateway process is running..."
if ps -p $GATEWAY_PID > /dev/null 2>&1; then
  echo "Gateway process confirmed running (PID: $GATEWAY_PID)"
else
  echo "ERROR: Gateway process exited immediately after start"
  echo ""
  echo "Gateway stdout output:"
  cat /tmp/gh-aw/mcp-config/gateway-output.json 2>/dev/null || echo "No stdout output available"
  echo ""
  echo "Gateway stderr logs:"
  cat /tmp/gh-aw/mcp-logs/stderr.log 2>/dev/null || echo "No stderr logs available"
  exit 1
fi
echo ""

# Wait a few seconds for gateway to initialize before checking health
# This helps catch early failures before curl retries start
echo "Waiting for gateway to initialize..."
sleep 5
echo "Checking if gateway process is still alive after initialization..."
if ! ps -p $GATEWAY_PID > /dev/null 2>&1; then
  echo "ERROR: Gateway process (PID: $GATEWAY_PID) exited during initialization"
  WAIT_STATUS=$(wait $GATEWAY_PID 2>/dev/null; echo $?)
  echo "Gateway exit status: $WAIT_STATUS"
  echo ""
  echo "Gateway stdout (errors are written here per MCP Gateway Specification):"
  cat /tmp/gh-aw/mcp-config/gateway-output.json 2>/dev/null || echo "No stdout output available"
  echo ""
  echo "Gateway stderr logs (debug output):"
  cat /tmp/gh-aw/mcp-logs/stderr.log || echo "No stderr logs available"
  exit 1
fi
echo "Gateway process is still running (PID: $GATEWAY_PID)"
echo ""

# Wait for gateway to be ready using /health endpoint
# Note: Gateway may take 40-50 seconds when starting multiple MCP servers
# (e.g., serena alone takes ~22 seconds to start)
echo "Waiting for gateway to be ready..."
HEALTH_CHECK_START=$(date +%s%3N)
# Use localhost for health check since:
# 1. This script runs on the host (not in a container)
# 2. The gateway uses --network host, so it's accessible on localhost
# Note: MCP_GATEWAY_DOMAIN may be set to host.docker.internal for use by containers,
# but the health check should always use localhost since we're running on the host.
HEALTH_CHECK_HOST="localhost"
echo "Health endpoint: http://${HEALTH_CHECK_HOST}:${MCP_GATEWAY_PORT}/health"
echo "(Note: MCP_GATEWAY_DOMAIN is '${MCP_GATEWAY_DOMAIN}' for container access)"
echo "Retrying up to 120 times with 1s delay (120s total timeout)"
echo ""

# Check health endpoint using localhost (since we're running on the host)
# Per MCP Gateway Specification v1.3.0, /health must return HTTP 200 with JSON body containing specVersion and gatewayVersion
# Custom retry loop with progress indication: 120 attempts with 1 second delay = 120s total
# Note: Disable errexit temporarily to capture curl exit code
set +e

MAX_RETRIES=120
RETRY_DELAY=1
RETRY_COUNT=0
HTTP_CODE=""
HEALTH_RESPONSE=""
CURL_EXIT_CODE=1

echo "=== Health Check Progress ==="
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  RETRY_COUNT=$((RETRY_COUNT + 1))
  RETRY_START=$(date +%s%3N)
  
  # Calculate elapsed time since health check started
  ELAPSED_MS=$(($(date +%s%3N) - HEALTH_CHECK_START))
  ELAPSED_SEC=$((ELAPSED_MS / 1000))
  
  # Show progress every 10 retries or on first attempt
  if [ $((RETRY_COUNT % 10)) -eq 1 ] || [ $RETRY_COUNT -eq 1 ]; then
    echo "Attempt $RETRY_COUNT/$MAX_RETRIES (${ELAPSED_SEC}s elapsed)..."
  fi
  
  # Try to connect to health endpoint
  RESPONSE=$(curl -s --max-time 2 --connect-timeout 1 -w "\n%{http_code}" "http://${HEALTH_CHECK_HOST}:${MCP_GATEWAY_PORT}/health" 2>&1)
  CURL_EXIT_CODE=$?
  
  # Parse response
  HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
  HEALTH_RESPONSE=$(echo "$RESPONSE" | head -n -1)
  
  # Check if we got a successful response
  if [ "$HTTP_CODE" = "200" ] && [ -n "$HEALTH_RESPONSE" ]; then
    echo "✓ Health check succeeded on attempt $RETRY_COUNT (${ELAPSED_SEC}s elapsed)"
    break
  fi
  
  # If this is not the last attempt, wait before retrying
  if [ $RETRY_COUNT -lt $MAX_RETRIES ]; then
    sleep $RETRY_DELAY
  fi
done
echo "=== End Health Check Progress ==="
echo ""

set -e

# Always log the health response for debugging
echo "Final curl exit code: $CURL_EXIT_CODE"
echo "Final HTTP code: $HTTP_CODE"
echo "Total attempts: $RETRY_COUNT"
if [ -n "$HEALTH_RESPONSE" ]; then
  echo "Health response body: $HEALTH_RESPONSE"
else
  echo "Health response body: (empty)"
fi

if [ "$HTTP_CODE" = "200" ] && [ -n "$HEALTH_RESPONSE" ]; then
  echo "Gateway is ready!"
  print_timing $HEALTH_CHECK_START "Health check wait"
else
  echo ""
  echo "ERROR: Gateway failed to become ready"
  echo "Last HTTP code: $HTTP_CODE"
  echo "Last health response: ${HEALTH_RESPONSE:-(empty)}"
  echo ""
  echo "Checking if gateway process is still alive..."
  if ps -p $GATEWAY_PID > /dev/null 2>&1; then
    echo "Gateway process (PID: $GATEWAY_PID) is still running"
  else
    echo "Gateway process (PID: $GATEWAY_PID) has exited"
    WAIT_STATUS=$(wait $GATEWAY_PID 2>/dev/null; echo $?)
    echo "Gateway exit status: $WAIT_STATUS"
  fi
  echo ""
  echo "Docker container status:"
  docker ps -a 2>/dev/null | head -20 || echo "Could not list docker containers"
  echo ""
  echo "Gateway stdout (errors are written here per MCP Gateway Specification):"
  cat /tmp/gh-aw/mcp-config/gateway-output.json 2>/dev/null || echo "No stdout output available"
  echo ""
  echo "Gateway stderr logs (debug output):"
  cat /tmp/gh-aw/mcp-logs/stderr.log || echo "No stderr logs available"
  echo ""
  echo "Checking network connectivity to gateway port..."
  netstat -tlnp 2>/dev/null | grep ":${MCP_GATEWAY_PORT}" || ss -tlnp 2>/dev/null | grep ":${MCP_GATEWAY_PORT}" || echo "Port ${MCP_GATEWAY_PORT} does not appear to be listening"
  kill $GATEWAY_PID 2>/dev/null || true
  exit 1
fi
echo ""

# Wait for gateway output (rewritten configuration)
echo "Reading gateway output configuration..."
OUTPUT_WAIT_START=$(date +%s%3N)
WAIT_ATTEMPTS=10
WAIT_ATTEMPT=0
while [ $WAIT_ATTEMPT -lt $WAIT_ATTEMPTS ]; do
  if [ -s /tmp/gh-aw/mcp-config/gateway-output.json ]; then
    echo "Gateway output received!"
    break
  fi
  WAIT_ATTEMPT=$((WAIT_ATTEMPT + 1))
  if [ $WAIT_ATTEMPT -lt $WAIT_ATTEMPTS ]; then
    sleep 1
  fi
done
print_timing $OUTPUT_WAIT_START "Gateway output wait"
echo ""

# Verify output was written
if [ ! -s /tmp/gh-aw/mcp-config/gateway-output.json ]; then
  echo "ERROR: Gateway did not write output configuration"
  echo ""
  echo "Gateway stdout (should contain error or config):"
  cat /tmp/gh-aw/mcp-config/gateway-output.json 2>/dev/null || echo "No stdout output available"
  echo ""
  echo "Gateway stderr logs:"
  cat /tmp/gh-aw/mcp-logs/stderr.log || echo "No stderr logs available"
  kill $GATEWAY_PID 2>/dev/null || true
  exit 1
fi

# Check if output contains an error payload instead of valid configuration
# Per MCP Gateway Specification v1.0.0 section 9.1, errors are written to stdout as error payloads
if jq -e '.error' /tmp/gh-aw/mcp-config/gateway-output.json >/dev/null 2>&1; then
  echo "ERROR: Gateway returned an error payload instead of configuration"
  echo ""
  echo "Gateway error details:"
  cat /tmp/gh-aw/mcp-config/gateway-output.json
  echo ""
  echo "Gateway stderr logs:"
  cat /tmp/gh-aw/mcp-logs/stderr.log || echo "No stderr logs available"
  kill $GATEWAY_PID 2>/dev/null || true
  exit 1
fi

# Convert gateway output to agent-specific format
echo "Converting gateway configuration to agent format..."
CONFIG_CONVERT_START=$(date +%s%3N)
export MCP_GATEWAY_OUTPUT=/tmp/gh-aw/mcp-config/gateway-output.json

# Validate MCP_GATEWAY_API_KEY is set (required by converter scripts)
if [ -z "$MCP_GATEWAY_API_KEY" ]; then
  echo "ERROR: MCP_GATEWAY_API_KEY environment variable must be set for converter scripts"
  echo "This variable should be set in the workflow before calling start_mcp_gateway.sh"
  exit 1
fi

# Determine which agent-specific converter to use based on engine type
# Check for engine-specific indicators and call appropriate converter
if [ -n "$GH_AW_ENGINE" ]; then
  ENGINE_TYPE="$GH_AW_ENGINE"
elif [ -f "/home/runner/.copilot" ] || [ -n "$GITHUB_COPILOT_CLI_MODE" ]; then
  ENGINE_TYPE="copilot"
elif [ -f "/tmp/gh-aw/mcp-config/config.toml" ]; then
  ENGINE_TYPE="codex"
elif [ -f "/tmp/gh-aw/mcp-config/mcp-servers.json" ]; then
  ENGINE_TYPE="claude"
else
  ENGINE_TYPE="unknown"
fi

echo "Detected engine type: $ENGINE_TYPE"

case "$ENGINE_TYPE" in
  copilot)
    echo "Using Copilot converter..."
    bash /opt/gh-aw/actions/convert_gateway_config_copilot.sh
    ;;
  codex)
    echo "Using Codex converter..."
    bash /opt/gh-aw/actions/convert_gateway_config_codex.sh
    ;;
  claude)
    echo "Using Claude converter..."
    bash /opt/gh-aw/actions/convert_gateway_config_claude.sh
    ;;
  *)
    echo "No agent-specific converter found for engine: $ENGINE_TYPE"
    echo "Using gateway output directly"
    # Default fallback - copy to most common location
    mkdir -p /home/runner/.copilot
    cp /tmp/gh-aw/mcp-config/gateway-output.json /home/runner/.copilot/mcp-config.json
    cat /home/runner/.copilot/mcp-config.json
    ;;
esac
print_timing $CONFIG_CONVERT_START "Configuration conversion"
echo ""

# Check MCP server functionality
echo "Checking MCP server functionality..."
MCP_CHECK_START=$(date +%s%3N)
if [ -f /opt/gh-aw/actions/check_mcp_servers.sh ]; then
  echo "Running MCP server checks..."
  # Store check diagnostic logs in /tmp/gh-aw/mcp-logs/start-gateway.log for artifact upload
  # Use tee to output to both stdout and the log file
  # Enable pipefail so the exit code comes from check_mcp_servers.sh, not tee
  set -o pipefail
  if ! bash /opt/gh-aw/actions/check_mcp_servers.sh \
    /tmp/gh-aw/mcp-config/gateway-output.json \
    "http://localhost:${MCP_GATEWAY_PORT}" \
    "${MCP_GATEWAY_API_KEY}" 2>&1 | tee /tmp/gh-aw/mcp-logs/start-gateway.log; then
    echo "ERROR: MCP server checks failed - no servers could be connected"
    echo "Gateway process will be terminated"
    kill $GATEWAY_PID 2>/dev/null || true
    exit 1
  fi
  set +o pipefail
  print_timing $MCP_CHECK_START "MCP server connectivity checks"
else
  echo "WARNING: MCP server check script not found at /opt/gh-aw/actions/check_mcp_servers.sh"
  echo "Skipping MCP server functionality checks"
fi
echo ""

# Delete gateway configuration file after conversion and checks are complete
echo "Cleaning up gateway configuration file..."
if [ -f /tmp/gh-aw/mcp-config/gateway-output.json ]; then
  rm /tmp/gh-aw/mcp-config/gateway-output.json
  echo "Gateway configuration file deleted"
else
  echo "Gateway configuration file not found (already deleted or never created)"
fi
echo ""

echo "MCP gateway is running:"
echo "  - From host: http://localhost:${MCP_GATEWAY_PORT}"
echo "  - From containers: http://${MCP_GATEWAY_DOMAIN}:${MCP_GATEWAY_PORT}"
echo "Gateway PID: $GATEWAY_PID"

print_timing $SCRIPT_START_TIME "Overall gateway startup"
echo ""

# Output PID as GitHub Actions step output for use in cleanup
# Output port and API key for use in stop script (per MCP Gateway Specification v1.1.0)
{
  echo "gateway-pid=$GATEWAY_PID"
  echo "gateway-port=${MCP_GATEWAY_PORT}"
  echo "gateway-api-key=${MCP_GATEWAY_API_KEY}"
} >> $GITHUB_OUTPUT
