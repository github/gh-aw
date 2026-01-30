#!/usr/bin/env bash
# Verify MCP Gateway Health
# This script verifies that the MCP gateway is running and healthy

set -e

# Timing helper functions
print_timing() {
  local start_time=$1
  local label=$2
  local end_time=$(date +%s%3N)
  local duration=$((end_time - start_time))
  echo "⏱️  TIMING: $label took ${duration}ms"
}

# Usage: verify_mcp_gateway_health.sh GATEWAY_URL MCP_CONFIG_PATH LOGS_FOLDER
#
# Arguments:
#   GATEWAY_URL      : The HTTP URL of the MCP gateway (e.g., http://localhost:8080)
#   MCP_CONFIG_PATH  : Path to the MCP configuration file
#   LOGS_FOLDER      : Path to the gateway logs folder
#
# Exit codes:
#   0 - Gateway is healthy and ready
#   1 - Gateway failed to start or configuration is invalid

if [ "$#" -ne 3 ]; then
  echo "Usage: $0 GATEWAY_URL MCP_CONFIG_PATH LOGS_FOLDER" >&2
  exit 1
fi

gateway_url="$1"
mcp_config_path="$2"
logs_folder="$3"

# Start overall timing
SCRIPT_START_TIME=$(date +%s%3N)

echo 'Waiting for MCP Gateway to be ready...'
echo ''
echo '=== File Locations ==='
echo "Gateway URL: $gateway_url"
echo "MCP Config Path: $mcp_config_path"
echo "Logs Folder: $logs_folder"
echo "Gateway Log: ${logs_folder}/gateway.log"
echo ''

# Check for gateway logs early
echo '=== Gateway Logs Check ==='
if [ -f "${logs_folder}/gateway.log" ]; then
  echo "✓ Gateway log file exists at: ${logs_folder}/gateway.log"
  echo "Log file size: $(stat -f%z "${logs_folder}/gateway.log" 2>/dev/null || stat -c%s "${logs_folder}/gateway.log" 2>/dev/null || echo 'unknown') bytes"
  echo "Last few lines of gateway log:"
  tail -10 "${logs_folder}/gateway.log" 2>/dev/null || echo "Could not read log tail"
else
  echo "⚠ Gateway log file NOT found at: ${logs_folder}/gateway.log"
fi
echo ''

# Wait for gateway to be ready FIRST before checking config
echo '=== Testing Gateway Health ==='
HEALTH_CHECK_START=$(date +%s%3N)

# Capture both response body and HTTP code with custom retry loop
echo "Calling health endpoint: ${gateway_url}/health"
echo "Retrying up to 120 times with 1s delay (120s total timeout)"
echo ""

# Custom retry loop with progress indication: 120 attempts with 1 second delay = 120s total
MAX_RETRIES=120
RETRY_DELAY=1
RETRY_COUNT=0
http_code=""
health_response=""

echo "=== Health Check Progress ==="
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  RETRY_COUNT=$((RETRY_COUNT + 1))
  
  # Calculate elapsed time since health check started
  ELAPSED_MS=$(($(date +%s%3N) - HEALTH_CHECK_START))
  ELAPSED_SEC=$((ELAPSED_MS / 1000))
  
  # Show progress every 10 retries or on first attempt
  if [ $((RETRY_COUNT % 10)) -eq 1 ] || [ $RETRY_COUNT -eq 1 ]; then
    echo "Attempt $RETRY_COUNT/$MAX_RETRIES (${ELAPSED_SEC}s elapsed)..."
  fi
  
  # Try to connect to health endpoint
  response=$(curl -s --max-time 2 --connect-timeout 1 -w "\n%{http_code}" "${gateway_url}/health" 2>&1)
  
  # Parse response
  http_code=$(echo "$response" | tail -n 1)
  health_response=$(echo "$response" | head -n -1)
  
  # Check if we got a successful response
  if [ "$http_code" = "200" ]; then
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

# Always log the health response for debugging
echo "Final HTTP code: $http_code"
echo "Total attempts: $RETRY_COUNT"
if [ -n "$health_response" ]; then
  echo "Health response body: $health_response"
else
  echo "Health response body: (empty)"
fi

if [ "$http_code" = "200" ]; then
  echo "✓ MCP Gateway is ready!"
  print_timing $HEALTH_CHECK_START "Health endpoint polling"
else
  echo ''
  echo "✗ Error: MCP Gateway failed to start"
  echo "Last HTTP code: $http_code"
  echo "Last health response: ${health_response:-(empty)}"
  echo ''
  echo '=== Gateway Logs (Full) ==='
  cat "${logs_folder}/gateway.log" || echo 'No gateway logs found'
  exit 1
fi

# Parse and display version information from health response
echo ''
if [ -n "$health_response" ]; then
  # Extract version information using jq if available
  if command -v jq >/dev/null 2>&1; then
    spec_version=$(echo "$health_response" | jq -r '.specVersion // "unknown"')
    gateway_version=$(echo "$health_response" | jq -r '.gatewayVersion // "unknown"')
    
    echo "MCP Gateway Protocol Version: $spec_version"
    echo "MCP Gateway Implementation Version: $gateway_version"
  else
    echo "Note: jq not available, cannot parse version information"
  fi
fi
echo ''

# Now that gateway is ready, check the config file
echo '=== MCP Configuration File ==='
CONFIG_CHECK_START=$(date +%s%3N)
if [ -f "$mcp_config_path" ]; then
  echo "✓ Config file exists at: $mcp_config_path"
  echo "File size: $(stat -f%z "$mcp_config_path" 2>/dev/null || stat -c%s "$mcp_config_path" 2>/dev/null || echo 'unknown') bytes"
  echo "Last modified: $(stat -f%Sm "$mcp_config_path" 2>/dev/null || stat -c%y "$mcp_config_path" 2>/dev/null || echo 'unknown')"
else
  echo "✗ Config file NOT found at: $mcp_config_path"
  exit 1
fi
echo ''

# Show MCP config file content
echo '=== MCP Configuration Content ==='
cat "$mcp_config_path" || { echo 'ERROR: Failed to read MCP config file'; exit 1; }
echo ''

# Verify safeinputs and safeoutputs are present in config
echo '=== Verifying Required Servers ==='
if ! grep -q '"safeinputs"' "$mcp_config_path"; then
  echo '✗ ERROR: safeinputs server not found in MCP configuration'
  exit 1
fi
echo '✓ safeinputs server found in configuration'

if ! grep -q '"safeoutputs"' "$mcp_config_path"; then
  echo '✗ ERROR: safeoutputs server not found in MCP configuration'
  exit 1
fi
echo '✓ safeoutputs server found in configuration'
print_timing $CONFIG_CHECK_START "Configuration file verification"
echo ''

# Fetch and display gateway servers list
echo '=== Gateway Servers List ==='
echo "Fetching servers from: ${gateway_url}/servers"
curl -s "${gateway_url}/servers" || echo "✗ Could not fetch servers list"
echo ''

# Test MCP server connectivity through gateway
echo '=== Testing MCP Server Connectivity ==='
CONNECTIVITY_TEST_START=$(date +%s%3N)

# Extract first external MCP server name from config (excluding safeinputs/safeoutputs)
mcp_server=$(jq -r '.mcpServers | to_entries[] | select(.key != "safeinputs" and .key != "safeoutputs") | .key' "$mcp_config_path" | head -n 1)
if [ -n "$mcp_server" ]; then
  echo "Testing connectivity to MCP server: $mcp_server"
  mcp_url="${gateway_url}/mcp/${mcp_server}"
  echo "MCP URL: $mcp_url"
  echo ''
  
  # Check if server was rewritten in config
  echo "Checking if '$mcp_server' was rewritten to use gateway..."
  server_config=$(jq -r ".mcpServers.\"$mcp_server\"" "$mcp_config_path")
  echo "Server config for '$mcp_server':"
  echo "$server_config" | jq '.' 2>/dev/null || echo "$server_config"
  
  if echo "$server_config" | grep -q "gateway"; then
    echo "✓ Server appears to be configured for gateway"
  else
    echo "⚠ Server may not be configured for gateway (no 'gateway' field found)"
  fi
  echo ''
  
  # Test with MCP initialize call
  echo "Sending MCP initialize request..."
  response=$(curl -s -w "\n%{http_code}" -X POST "$mcp_url" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}')
  
  http_code=$(echo "$response" | tail -n 1)
  body=$(echo "$response" | head -n -1)
  
  echo "HTTP Status: $http_code"
  echo "Response: $body"
  echo ''
  
  if [ "$http_code" = "200" ]; then
    echo "✓ MCP server connectivity test passed"
  else
    echo "⚠ MCP server returned HTTP $http_code (may need authentication or different request)"
    echo ''
    echo "Gateway logs (last 20 lines):"
    tail -20 "${logs_folder}/gateway.log" 2>/dev/null || echo "Could not read gateway logs"
  fi
  print_timing $CONNECTIVITY_TEST_START "MCP server connectivity test"
else
  echo "No external MCP servers configured for testing"
fi

print_timing $SCRIPT_START_TIME "Overall gateway health verification"
echo ""

exit 0
