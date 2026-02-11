#!/bin/bash
set -e

# validate_gatewayed_server.sh - Validate that an MCP server is correctly gatewayed
#
# Usage: validate_gatewayed_server.sh SERVER_NAME MCP_CONFIG_PATH GATEWAY_URL
#
# Arguments:
#   SERVER_NAME      : Name of the MCP server to validate (e.g., "github", "playwright")
#   MCP_CONFIG_PATH  : Path to the MCP configuration file
#   GATEWAY_URL      : Expected gateway base URL (e.g., "http://localhost:8080")
#
# Validation checks:
#   1. Server exists in MCP config
#   2. Server has HTTP URL
#   3. Server type is "http"
#   4. URL points to gateway
#
# Exit codes:
#   0 - Server is correctly gatewayed
#   1 - Validation failed

# Parse arguments
if [ "$#" -ne 3 ]; then
  echo "Usage: $0 SERVER_NAME MCP_CONFIG_PATH GATEWAY_URL" >&2
  exit 1
fi

SERVER_NAME="$1"
MCP_CONFIG_PATH="$2"
GATEWAY_URL="$3"

# Validate that MCP config file exists
validate_config_file_exists() {
  if [ ! -f "$MCP_CONFIG_PATH" ]; then
    echo "ERROR: MCP config file not found: ${MCP_CONFIG_PATH@Q}" >&2
    return 1
  fi
  return 0
}

# Check if server exists in config
validate_server_exists() {
  if ! grep -q "\"${SERVER_NAME}\"" "$MCP_CONFIG_PATH"; then
    echo "ERROR: ${SERVER_NAME} server not found in MCP configuration" >&2
    echo "Available servers:" >&2
    jq -r '.mcpServers | keys[]' "$MCP_CONFIG_PATH" 2>/dev/null || echo "(could not list servers)" >&2
    return 1
  fi
  return 0
}

# Extract and validate server configuration
validate_server_config() {
  local server_config
  server_config=$(jq -r ".mcpServers.\"${SERVER_NAME}\"" "$MCP_CONFIG_PATH" 2>/dev/null)
  
  if [ -z "$server_config" ] || [ "$server_config" = "null" ]; then
    echo "ERROR: ${SERVER_NAME} server configuration is null or empty" >&2
    return 1
  fi
  
  echo "$server_config"
  return 0
}

# Verify server has HTTP URL
validate_server_url() {
  local server_config="$1"
  local server_url
  
  server_url=$(echo "$server_config" | jq -r '.url // empty')
  
  if [ -z "$server_url" ] || [ "$server_url" = "null" ]; then
    echo "ERROR: ${SERVER_NAME} server does not have HTTP URL (not gatewayed correctly)" >&2
    echo "Config: $server_config" >&2
    return 1
  fi
  
  echo "$server_url"
  return 0
}

# Verify server type is "http"
validate_server_type() {
  local server_config="$1"
  local server_type
  
  server_type=$(echo "$server_config" | jq -r '.type // empty')
  
  if [ "$server_type" != "http" ]; then
    echo "ERROR: ${SERVER_NAME} server type is not \"http\" (expected for gatewayed servers)" >&2
    echo "Type: $server_type" >&2
    echo "Config: $server_config" >&2
    return 1
  fi
  
  return 0
}

# Verify URL points to gateway
validate_gateway_url() {
  local server_url="$1"
  
  if ! echo "$server_url" | grep -q "$GATEWAY_URL"; then
    echo "ERROR: ${SERVER_NAME} server URL does not point to gateway" >&2
    echo "Expected gateway URL: ${GATEWAY_URL@Q}" >&2
    echo "Actual URL: ${server_url@Q}" >&2
    return 1
  fi
  
  return 0
}

# Main validation flow
main() {
  # Step 1: Validate config file exists
  if ! validate_config_file_exists; then
    return 1
  fi
  
  # Step 2: Check if server exists
  if ! validate_server_exists; then
    return 1
  fi
  
  # Step 3: Extract server configuration
  local server_config
  if ! server_config=$(validate_server_config); then
    return 1
  fi
  
  # Step 4: Validate server has URL
  local server_url
  if ! server_url=$(validate_server_url "$server_config"); then
    return 1
  fi
  
  # Step 5: Validate server type
  if ! validate_server_type "$server_config"; then
    return 1
  fi
  
  # Step 6: Validate URL points to gateway
  if ! validate_gateway_url "$server_url"; then
    return 1
  fi
  
  # All checks passed
  echo "✓ ${SERVER_NAME} server is correctly gatewayed"
  return 0
}

# Run main validation
main
