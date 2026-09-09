#!/usr/bin/env bash
set +o histexpand

# Check MCP Server Functionality
# This script performs basic functionality checks on MCP servers configured by the MCP gateway
# It sends an MCP ping + initialize + tools/list request to each server to verify backend connectivity
#
# Resilience Features:
# - Progressive timeout: 10s, 20s, 30s across retry attempts
# - Progressive delay: 2s, 4s between retry attempts
# - Up to 3 retry attempts per server
# - Accommodates slow-starting MCP servers (gateway may take 40-50 seconds to start)
# Protocol: ping verifies basic connectivity; initialize establishes the session (capturing
# Mcp-Session-Id); tools/list confirms the backend container is truly ready (must be forwarded
# by the gateway, unlike ping which may be handled at the proxy layer).

set -e

# Timing helper functions
print_timing() {
  local start_time=$1
  local label=$2
  local end_time=$(date +%s%3N)
  local duration=$((end_time - start_time))
  echo "⏱️  TIMING: $label took ${duration}ms"
}

# Usage: check_mcp_servers.sh GATEWAY_CONFIG_PATH GATEWAY_URL GATEWAY_AGENT_ID
#
# Arguments:
#   GATEWAY_CONFIG_PATH : Path to the gateway output configuration file (gateway-output.json)
#   GATEWAY_URL         : The HTTP URL of the MCP gateway (e.g., http://localhost:8080)
#   GATEWAY_AGENT_ID    : Agent/session identifier for gateway authentication
#
# Exit codes:
#   0 - At least one server connected and no required servers failed (optional server failures logged as warnings)
#   1 - Invalid arguments, configuration file issues, no successful connections, or required server failures

if [ "$#" -ne 3 ]; then
  echo "Usage: $0 GATEWAY_CONFIG_PATH GATEWAY_URL GATEWAY_AGENT_ID" >&2
  exit 1
fi

GATEWAY_CONFIG_PATH="$1"
GATEWAY_URL="$2"
GATEWAY_AGENT_ID="$3"

# Optional comma-separated list of non-critical server names. The gateway output
# does not echo the `required` flag from the input configuration, so the caller
# forwards the names of servers declared with `required: false` here.
OPTIONAL_SERVERS="${GH_AW_MCP_OPTIONAL_SERVERS:-}"
DEFERRED_SERVERS="${GH_AW_MCP_DEFERRED_SERVERS:-}"

# server_in_list NAME LIST → 0 when LIST contains the exact comma-delimited name
server_in_list() {
  local name="$1"
  local remaining="$2"
  local entry
  [ -z "$remaining" ] && return 1
  while [ -n "$remaining" ]; do
    entry="${remaining%%,*}"
    if [ "$entry" = "$name" ]; then
      return 0
    fi
    if [ "$remaining" = "$entry" ]; then
      break
    fi
    remaining="${remaining#*,}"
  done
  return 1
}

is_optional_server() {
  server_in_list "$1" "$OPTIONAL_SERVERS"
}

is_deferred_server() {
  # Deferral is a closed compiler-owned lifecycle contract. Do not allow this
  # internal environment variable to suppress checks for arbitrary MCP servers.
  [ "$1" = "awf-enclave" ] || return 1
  server_in_list "$1" "$DEFERRED_SERVERS"
}

# Start overall timing
SCRIPT_START_TIME=$(date +%s%3N)

echo "Checking MCP servers..."
echo ""

# Validate configuration file exists
CONFIG_VALIDATION_START=$(date +%s%3N)
if [ ! -f "$GATEWAY_CONFIG_PATH" ]; then
  echo "ERROR: Gateway configuration file not found: ${GATEWAY_CONFIG_PATH@Q}" >&2
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
SERVERS_DEFERRED=0
REQUIRED_SERVERS_FAILED=0

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

  if is_deferred_server "$SERVER_NAME"; then
    echo "↷ $SERVER_NAME: deferred until its AWF-owned backend starts"
    SERVERS_DEFERRED=$((SERVERS_DEFERRED + 1))
    continue
  fi

  # Check whether server is marked optional in configuration JSON or via the
  # GH_AW_MCP_OPTIONAL_SERVERS environment variable.
  # Servers are required by default; set required: false to degrade failures to warnings.
  REQUIRED=true
  if echo "$SERVER_CONFIG" | jq -e '.required == false' >/dev/null 2>&1; then
    REQUIRED=false
  elif is_optional_server "$SERVER_NAME"; then
    REQUIRED=false
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
  
  # MCP protocol sequence: ping → initialize → tools/list.
  # ping verifies basic connectivity (may be handled by the gateway proxy).
  # initialize establishes protocol version and may return a Mcp-Session-Id header
  # that must be included in subsequent requests (for stateful HTTP transports).
  # tools/list must be forwarded to the backend container, confirming the backend is ready.
  PING_PAYLOAD='{"jsonrpc":"2.0","id":1,"method":"ping"}'
  INIT_PAYLOAD='{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"capabilities":{},"clientInfo":{"name":"check-mcp-servers","version":"1.0.0"},"protocolVersion":"2024-11-05"}}'
  TOOLS_LIST_PAYLOAD='{"jsonrpc":"2.0","id":3,"method":"tools/list"}'
  
  # Retry logic for slow-starting servers
  RETRY_COUNT=0
  CHECK_SUCCESS=false
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
    
    # Step 1: Send ping to verify basic connectivity
    CURL_ARGS=(-s -w "\n%{http_code}" --max-time "$TIMEOUT" -X POST "$SERVER_URL"
      -H "Content-Type: application/json")
    [ -n "$AUTH_HEADER" ] && CURL_ARGS+=(-H "Authorization: $AUTH_HEADER")
    CURL_ARGS+=(-d "$PING_PAYLOAD")
    PING_RESPONSE=$(curl "${CURL_ARGS[@]}" 2>&1 || echo -e "\n000")
    PING_HTTP_CODE=$(echo "$PING_RESPONSE" | tail -n 1)
    
    if [ "$PING_HTTP_CODE" != "200" ]; then
      LAST_ERROR="Ping failed: HTTP ${PING_HTTP_CODE:-000}"
      RETRY_COUNT=$((RETRY_COUNT + 1))
      continue
    fi
    
    # Step 2: Send MCP initialize request, capturing response headers for Mcp-Session-Id
    CURL_ARGS=(-s -D - --max-time "$TIMEOUT" -X POST "$SERVER_URL"
      -H "Content-Type: application/json")
    [ -n "$AUTH_HEADER" ] && CURL_ARGS+=(-H "Authorization: $AUTH_HEADER")
    CURL_ARGS+=(-d "$INIT_PAYLOAD")
    INIT_RESPONSE=$(curl "${CURL_ARGS[@]}" 2>&1 || true)
    
    INIT_HTTP_CODE=$(echo "$INIT_RESPONSE" | grep -m1 '^HTTP/' | awk '{print $2}')
    SESSION_ID=$(echo "$INIT_RESPONSE" | grep -i '^Mcp-Session-Id:' | awk '{print $2}' | tr -d '\r')
    
    if [ "$INIT_HTTP_CODE" != "200" ]; then
      LAST_ERROR="Initialize failed: HTTP ${INIT_HTTP_CODE:-000}"
      RETRY_COUNT=$((RETRY_COUNT + 1))
      continue
    fi
    
    # Step 3: Send tools/list, including Mcp-Session-Id header if returned by initialize
    CURL_ARGS=(-s -w "\n%{http_code}" --max-time "$TIMEOUT" -X POST "$SERVER_URL"
      -H "Content-Type: application/json")
    [ -n "$AUTH_HEADER" ] && CURL_ARGS+=(-H "Authorization: $AUTH_HEADER")
    [ -n "$SESSION_ID" ] && CURL_ARGS+=(-H "Mcp-Session-Id: $SESSION_ID")
    CURL_ARGS+=(-d "$TOOLS_LIST_PAYLOAD")
    CHECK_RESPONSE=$(curl "${CURL_ARGS[@]}" 2>&1 || echo -e "\n000")
    
    CHECK_HTTP_CODE=$(echo "$CHECK_RESPONSE" | tail -n 1)
    CHECK_BODY=$(echo "$CHECK_RESPONSE" | head -n -1)
    
    # Check if tools/list succeeded
    if [ "$CHECK_HTTP_CODE" = "200" ]; then
      # Check for JSON-RPC error in response
      if ! echo "$CHECK_BODY" | jq -e '.error' >/dev/null 2>&1; then
        CHECK_SUCCESS=true
        break
      else
        LAST_ERROR="JSON-RPC error: $(echo "$CHECK_BODY" | jq -r '.error.message // .error' 2>/dev/null)"
      fi
    else
      LAST_ERROR="HTTP ${CHECK_HTTP_CODE}"
      if [ "$CHECK_HTTP_CODE" = "000" ]; then
        # Connection error or timeout
        if echo "$CHECK_BODY" | grep -q "Connection refused"; then
          LAST_ERROR="Connection refused"
        elif echo "$CHECK_BODY" | grep -q "timed out"; then
          LAST_ERROR="Connection timeout"
        elif echo "$CHECK_BODY" | grep -q "Could not resolve host"; then
          LAST_ERROR="DNS resolution failed"
        else
          LAST_ERROR="Connection error: $(echo "$CHECK_BODY" | head -c 100)"
        fi
      fi
    fi
    
    RETRY_COUNT=$((RETRY_COUNT + 1))
  done
  
  if [ "$CHECK_SUCCESS" = true ]; then
    echo "✓ $SERVER_NAME: connected"
    SERVERS_SUCCEEDED=$((SERVERS_SUCCEEDED + 1))
  else
    if [ "$REQUIRED" = true ]; then
      echo "✗ $SERVER_NAME: failed to connect (required)"
      REQUIRED_SERVERS_FAILED=$((REQUIRED_SERVERS_FAILED + 1))
    else
      echo "⚠ $SERVER_NAME: non-critical MCP server unavailable, continuing without it"
    fi
    echo "  URL: ${SERVER_URL@Q}"
    echo "  Last error: ${LAST_ERROR@Q}"
    echo "  Retries attempted: $MAX_RETRIES"
    SERVERS_FAILED=$((SERVERS_FAILED + 1))
  fi
  
  print_timing $SERVER_START_TIME "Server check for $SERVER_NAME"
  echo ""
  
done <<< "$SERVER_NAMES"

# Print summary
print_timing $SCRIPT_START_TIME "Overall MCP server checks"
echo ""
if [ $REQUIRED_SERVERS_FAILED -gt 0 ]; then
  echo "ERROR: $REQUIRED_SERVERS_FAILED required server(s) failed connectivity check"
  echo "Succeeded: $SERVERS_SUCCEEDED, Failed: $SERVERS_FAILED, Skipped: $SERVERS_SKIPPED, Deferred: $SERVERS_DEFERRED"
  echo ""
  echo "One or more startup-critical MCP servers failed ping/initialize/tools/list"
  echo "after multiple retry attempts with progressive timeouts (10s, 20s, 30s)."
  echo ""
  echo "Common causes:"
  echo "  - Invalid or expired credentials (for example HTTP 401/403 on initialize)"
  echo "  - MCP server unavailable or rejecting requests"
  echo "  - Network connectivity or DNS issues"
  echo ""
  echo "Check the MCP server output above and individual server logs for more details."
  exit 1
elif [ $SERVERS_SUCCEEDED -eq 0 ] && [ $SERVERS_FAILED -eq 0 ] && [ $SERVERS_DEFERRED -gt 0 ]; then
  echo "✓ Deferred $SERVERS_DEFERRED late-starting server(s) to their owner readiness checks"
  exit 0
elif [ $SERVERS_SUCCEEDED -eq 0 ] && [ $SERVERS_FAILED -eq 0 ]; then
  echo "ERROR: No HTTP servers were successfully checked"
  echo "This could indicate:"
  echo "  - No HTTP-type MCP servers were configured"
  echo "  - All configured servers are stdio-type (which are skipped by this check)"
  echo ""
  echo "If you expected HTTP servers to be configured, check the gateway configuration."
  exit 1
elif [ $SERVERS_SUCCEEDED -eq 0 ]; then
  echo "ERROR: All $SERVERS_FAILED optional server(s) failed; no successful connections"
  echo "Succeeded: 0, Failed: $SERVERS_FAILED, Skipped: $SERVERS_SKIPPED, Deferred: $SERVERS_DEFERRED"
  echo ""
  echo "All configured HTTP MCP servers are optional but none connected successfully."
  echo "At least one server must connect for the gateway to be considered healthy."
  echo ""
  echo "Check the MCP server output above and individual server logs for more details."
  exit 1
else
  if [ $SERVERS_FAILED -gt 0 ]; then
    echo "WARNING: $SERVERS_FAILED optional server(s) failed connectivity check; continuing startup"
    echo "✓ Checks completed with warnings ($SERVERS_SUCCEEDED succeeded, $SERVERS_FAILED failed, $SERVERS_SKIPPED skipped, $SERVERS_DEFERRED deferred)"
  else
    echo "✓ All checks passed ($SERVERS_SUCCEEDED succeeded, $SERVERS_SKIPPED skipped, $SERVERS_DEFERRED deferred)"
  fi
  exit 0
fi
