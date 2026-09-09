#!/usr/bin/env bash
set +o histexpand

# Stop MCP Gateway
# This script stops the MCP gateway process using the /close endpoint for graceful shutdown,
# falling back to kill signals if the endpoint is unavailable or fails.
# Per MCP Gateway Specification v1.1.0 section 5.1.3

set -e

# Get PID from command line argument (passed from step output)
GATEWAY_PID="$1"

# Register an EXIT trap to ensure the named gateway container is always removed,
# regardless of the exit path (missing PID, process already gone, successful /close,
# kill fallback, etc.).  Using a trap means the graceful /close path is still
# attempted first when the gateway is running and reachable, while still
# guaranteeing that the host port is freed on every exit — including the case
# where the start step never captured a PID.
cleanup_container() {
  echo "Cleaning up awmg-mcpg container..."
  docker stop awmg-mcpg 2>/dev/null || docker rm -f awmg-mcpg 2>/dev/null || true
}
trap cleanup_container EXIT

if [ -z "$GATEWAY_PID" ]; then
  echo "Gateway PID not provided"
  echo "Gateway may not have been started or PID was not captured"
  exit 0
fi

echo "Stopping MCP gateway (PID: $GATEWAY_PID)..."

# Check if process is still running
if ! ps -p "$GATEWAY_PID" > /dev/null 2>&1; then
  echo "Gateway process (PID: $GATEWAY_PID) is not running"
  exit 0
fi

# Try graceful shutdown via /close endpoint if gateway variables are available
# Per MCP Gateway Specification v1.1.0, the /close endpoint:
# - Requires authentication with agent ID
# - Returns 200 OK on success
# - Returns 410 Gone if already closed (idempotent)
# - Gracefully terminates containers and cleans up resources
if [ -n "$MCP_GATEWAY_PORT" ] && [ -n "$MCP_GATEWAY_AGENT_ID" ]; then
  echo "Attempting graceful shutdown via /close endpoint..."
  
  # Use localhost for health check since:
  # 1. This script runs on the host (not in a container)
  # 2. The gateway uses --network host, so it's accessible on localhost
  CLOSE_URL="http://localhost:${MCP_GATEWAY_PORT}/close"
  
  # Try to invoke the /close endpoint (with timeout)
  # Per spec, the endpoint requires Authorization header with the agent ID
  CLOSE_RESPONSE=$(curl -f -s -m 10 -X POST -H "Authorization: ${MCP_GATEWAY_AGENT_ID}" "$CLOSE_URL" 2>&1) && {
    echo "Gateway accepted close request"
    echo "Response: $CLOSE_RESPONSE"
    
    # Wait up to 10 seconds for the gateway process to exit after accepting close
    for i in {1..10}; do
      if ! ps -p "$GATEWAY_PID" > /dev/null 2>&1; then
        echo "Gateway stopped gracefully via /close endpoint"
        exit 0
      fi
      sleep 1
    done
    
    echo "Gateway accepted close request but process did not exit within 10 seconds"
    echo "Falling back to kill signal..."
  } || {
    echo "Failed to invoke /close endpoint (curl exit: $?)"
    echo "Response: $CLOSE_RESPONSE"
    echo "Falling back to kill signal..."
  }
else
  echo "Gateway environment variables not available (MCP_GATEWAY_PORT or MCP_GATEWAY_AGENT_ID missing)"
  echo "Falling back to kill signal..."
fi

# Fallback: Use kill signal if /close endpoint was not successful
echo "Gateway process is still running, sending termination signal..."
kill "$GATEWAY_PID" 2>/dev/null || true

# Wait up to 5 seconds for graceful shutdown via SIGTERM
for i in {1..5}; do
  if ! ps -p "$GATEWAY_PID" > /dev/null 2>&1; then
    echo "Gateway stopped successfully"
    exit 0
  fi
  sleep 1
done

# Force kill if still running
if ps -p "$GATEWAY_PID" > /dev/null 2>&1; then
  echo "Gateway did not stop gracefully, forcing termination..."
  kill -9 "$GATEWAY_PID" 2>/dev/null || true
  sleep 1
  
  if ps -p "$GATEWAY_PID" > /dev/null 2>&1; then
    echo "Warning: Failed to stop gateway process"
    exit 1
  fi
fi

echo "Gateway stopped successfully"
exit 0
