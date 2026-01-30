#!/usr/bin/env bash
# Check MCP Server Functionality
# This script performs basic functionality checks on MCP servers configured by the MCP gateway
# It sends a ping message to each server to verify connectivity
#
# Resilience Features:
# - Progressive timeout: 10s, 20s, 30s across retry attempts
# - Progressive delay: 2s, 4s between retry attempts
# - Up to 3 retry attempts per server ping request
# - Accommodates slow-starting MCP servers (gateway may take 40-50 seconds to start)

set -e

# Timing helper functions
print_timing() {
  local start_time=$1
  local label=$2
  local end_time=$(date +%s%3N)
  local duration=$((end_time - start_time))
  echo "⏱️  TIMING: $label took ${duration}ms"
}

# Usage: check_mcp_servers.sh GATEWAY_CONFIG_PATH GATEWAY_URL GATEWAY_API_KEY
#
# Arguments:
#   GATEWAY_CONFIG_PATH : Path to the gateway output configuration file (gateway-output.json)
#   GATEWAY_URL         : The HTTP URL of the MCP gateway (e.g., http://localhost:8080)
#   GATEWAY_API_KEY     : API key for gateway authentication
#
# Exit codes:
#   0 - All HTTP servers successfully checked (skipped servers logged as warnings)
#   1 - Invalid arguments, configuration file issues, or server connection failures

if [ "$#" -ne 3 ]; then
  echo "Usage: $0 GATEWAY_CONFIG_PATH GATEWAY_URL GATEWAY_API_KEY" >&2
  exit 1
fi

GATEWAY_CONFIG_PATH="$1"
GATEWAY_URL="$2"
GATEWAY_API_KEY="$3"

# Start overall timing
SCRIPT_START_TIME=$(date +%s%3N)

echo "Checking MCP servers..."
echo ""

# Validate configuration file exists
CONFIG_VALIDATION_START=$(date +%s%3N)
if [ ! -f "$GATEWAY_CONFIG_PATH" ]; then
  echo "ERROR: Gateway configuration file not found: $GATEWAY_CONFIG_PATH" >&2
  exit 1
fi

# Parse the mcpServers section from gateway-output.json
if ! MCP_SERVERS=$(jq -r '.mcpServers' "$GATEWAY_CONFIG_PATH" 2>/dev/null); then
  echo "ERROR: Failed to parse mcpServers from configuration file" >&2
  exit 1
fi

# Check if mcpServers is null or empty
if [ "$MCP_SERVERS" = "null" ] || [ "$MCP_SERVERS" = "{}" ]; then
  echo "No MCP servers configured"
  exit 0
fi

# Get list of server names
SERVER_NAMES=$(echo "$MCP_SERVERS" | jq -r 'keys[]' 2>/dev/null)

if [ -z "$SERVER_NAMES" ]; then
  echo "No MCP servers found"
  exit 0
fi

print_timing $CONFIG_VALIDATION_START "Configuration validation"
echo ""

# Track overall results
SERVERS_CHECKED=0
SERVERS_SUCCEEDED=0
SERVERS_FAILED=0
SERVERS_SKIPPED=0

# Retry configuration for slow-starting servers
# Gateway may take 40-50 seconds to start all MCP servers (per start_mcp_gateway.sh)
MAX_RETRIES=3

# Iterate through each server
while IFS= read -r SERVER_NAME; do
  SERVERS_CHECKED=$((SERVERS_CHECKED + 1))
  SERVER_START_TIME=$(date +%s%3N)
  
  # Extract server configuration
  SERVER_CONFIG=$(echo "$MCP_SERVERS" | jq -r ".\"$SERVER_NAME\"" 2>/dev/null)
  
  if [ "$SERVER_CONFIG" = "null" ]; then
    echo "⚠ $SERVER_NAME: configuration is null"
    SERVERS_FAILED=$((SERVERS_FAILED + 1))
    continue
  fi
  
  # Extract server URL (should be HTTP URL pointing to gateway)
  SERVER_URL=$(echo "$SERVER_CONFIG" | jq -r '.url // empty' 2>/dev/null)
  
  if [ -z "$SERVER_URL" ] || [ "$SERVER_URL" = "null" ]; then
    echo "⚠ $SERVER_NAME: skipped (not HTTP)"
    SERVERS_SKIPPED=$((SERVERS_SKIPPED + 1))
    continue
  fi
  
  # Extract authentication headers from gateway configuration
  AUTH_HEADER=""
  if echo "$SERVER_CONFIG" | jq -e '.headers.Authorization' >/dev/null 2>&1; then
    AUTH_HEADER=$(echo "$SERVER_CONFIG" | jq -r '.headers.Authorization' 2>/dev/null)
  fi
  
  # Send MCP ping request with retry logic
  PING_PAYLOAD='{"jsonrpc":"2.0","id":1,"method":"ping"}'
  
  # Retry logic for slow-starting servers
  RETRY_COUNT=0
  PING_SUCCESS=false
  LAST_ERROR=""
  
  while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    # Calculate timeout based on retry attempt (10s, 20s, 30s)
    TIMEOUT=$((10 + RETRY_COUNT * 10))
    
    if [ $RETRY_COUNT -gt 0 ]; then
      # Progressive delay between retries (2s, 4s)
      DELAY=$((2 * RETRY_COUNT))
      echo "  Retry $RETRY_COUNT/$MAX_RETRIES after ${DELAY}s delay (timeout: ${TIMEOUT}s)..."
      sleep $DELAY
    else
      echo "  Attempting connection (timeout: ${TIMEOUT}s)..."
    fi
    
    # Make the request with proper headers and progressive timeout
    if [ -n "$AUTH_HEADER" ]; then
      PING_RESPONSE=$(curl -s -w "\n%{http_code}" --max-time $TIMEOUT -X POST "$SERVER_URL" \
        -H "Content-Type: application/json" \
        -H "Authorization: $AUTH_HEADER" \
        -d "$PING_PAYLOAD" 2>&1 || echo -e "\n000")
    else
      PING_RESPONSE=$(curl -s -w "\n%{http_code}" --max-time $TIMEOUT -X POST "$SERVER_URL" \
        -H "Content-Type: application/json" \
        -d "$PING_PAYLOAD" 2>&1 || echo -e "\n000")
    fi
    
    PING_HTTP_CODE=$(echo "$PING_RESPONSE" | tail -n 1)
    PING_BODY=$(echo "$PING_RESPONSE" | head -n -1)
    
    # Check if ping succeeded
    if [ "$PING_HTTP_CODE" = "200" ]; then
      # Check for JSON-RPC error in response
      if ! echo "$PING_BODY" | jq -e '.error' >/dev/null 2>&1; then
        PING_SUCCESS=true
        break
      else
        LAST_ERROR="JSON-RPC error: $(echo "$PING_BODY" | jq -r '.error.message // .error' 2>/dev/null)"
      fi
    else
      LAST_ERROR="HTTP ${PING_HTTP_CODE}"
      if [ "$PING_HTTP_CODE" = "000" ]; then
        # Connection error or timeout
        if echo "$PING_BODY" | grep -q "Connection refused"; then
          LAST_ERROR="Connection refused"
        elif echo "$PING_BODY" | grep -q "timed out"; then
          LAST_ERROR="Connection timeout"
        elif echo "$PING_BODY" | grep -q "Could not resolve host"; then
          LAST_ERROR="DNS resolution failed"
        else
          LAST_ERROR="Connection error: $(echo "$PING_BODY" | head -c 100)"
        fi
      fi
    fi
    
    RETRY_COUNT=$((RETRY_COUNT + 1))
  done
  
  if [ "$PING_SUCCESS" = true ]; then
    echo "✓ $SERVER_NAME: connected"
    SERVERS_SUCCEEDED=$((SERVERS_SUCCEEDED + 1))
  else
    echo "✗ $SERVER_NAME: failed to connect"
    echo "  URL: $SERVER_URL"
    echo "  Last error: $LAST_ERROR"
    echo "  Retries attempted: $MAX_RETRIES"
    SERVERS_FAILED=$((SERVERS_FAILED + 1))
  fi
  
  print_timing $SERVER_START_TIME "Server check for $SERVER_NAME"
  echo ""
  
done <<< "$SERVER_NAMES"

# Print summary
print_timing $SCRIPT_START_TIME "Overall MCP server checks"
echo ""
if [ $SERVERS_FAILED -gt 0 ]; then
  echo "ERROR: $SERVERS_FAILED of $SERVERS_CHECKED server(s) failed connectivity check"
  echo "Succeeded: $SERVERS_SUCCEEDED, Failed: $SERVERS_FAILED, Skipped: $SERVERS_SKIPPED"
  echo ""
  echo "This indicates that one or more MCP servers failed to respond to ping requests"
  echo "after multiple retry attempts with progressive timeouts (10s, 20s, 30s)."
  echo ""
  echo "Common causes:"
  echo "  - MCP server container failed to start or crashed"
  echo "  - Network connectivity issues between gateway and server"
  echo "  - Server initialization taking longer than expected (>30s)"
  echo "  - Server port not properly exposed or accessible"
  echo ""
  echo "Check the gateway logs and individual server logs for more details:"
  echo "  /tmp/gh-aw/mcp-logs/stderr.log"
  echo "  /tmp/gh-aw/mcp-logs/start-gateway.log"
  exit 1
elif [ $SERVERS_SUCCEEDED -eq 0 ]; then
  echo "ERROR: No HTTP servers were successfully checked"
  echo "This could indicate:"
  echo "  - No HTTP-type MCP servers were configured"
  echo "  - All configured servers are stdio-type (which are skipped by this check)"
  echo ""
  echo "If you expected HTTP servers to be configured, check the gateway configuration."
  exit 1
else
  echo "✓ All checks passed ($SERVERS_SUCCEEDED succeeded, $SERVERS_SKIPPED skipped)"
  exit 0
fi
