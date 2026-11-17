#!/usr/bin/env bash
set +o histexpand

# Convert MCP Gateway Configuration to Codex Format
# This script converts the gateway's standard HTTP-based MCP configuration
# to the TOML format expected by Codex

set -e

# Restrict default file creation mode to owner-only (rw-------) for all new files.
# This prevents the race window between file creation via output redirection and
# a subsequent chmod, which would leave credential-bearing files world-readable
# (mode 0644) with a typical umask of 022.
umask 077

# Required environment variables:
# - MCP_GATEWAY_OUTPUT: Path to gateway output configuration file
# - MCP_GATEWAY_DOMAIN: Domain to use for MCP server URLs (e.g., host.docker.internal)
# - MCP_GATEWAY_PORT: Port for MCP gateway (e.g., 80)

if [ -z "$MCP_GATEWAY_OUTPUT" ]; then
  echo "ERROR: MCP_GATEWAY_OUTPUT environment variable is required"
  exit 1
fi

if [ ! -f "$MCP_GATEWAY_OUTPUT" ]; then
  echo "ERROR: Gateway output file not found: $MCP_GATEWAY_OUTPUT"
  exit 1
fi

if [ -z "$MCP_GATEWAY_DOMAIN" ]; then
  echo "ERROR: MCP_GATEWAY_DOMAIN environment variable is required"
  exit 1
fi

if [ -z "$MCP_GATEWAY_PORT" ]; then
  echo "ERROR: MCP_GATEWAY_PORT environment variable is required"
  exit 1
fi

echo "Converting gateway configuration to Codex TOML format..."
echo "Input: $MCP_GATEWAY_OUTPUT"
echo "Target domain: $MCP_GATEWAY_DOMAIN:$MCP_GATEWAY_PORT"

# Convert gateway JSON output to Codex TOML format
# Gateway format (JSON):
# {
#   "mcpServers": {
#     "server-name": {
#       "type": "http",
#       "url": "http://domain:port/mcp/server-name",
#       "headers": {
#         "Authorization": "apiKey"
#       }
#     }
#   }
# }
#
# Codex format (TOML):
# [history]
# persistence = "none"
#
# [mcp_servers.server-name]
# url = "http://domain:port/mcp/server-name"
# http_headers = { Authorization = "apiKey" }
#
# Note: Codex doesn't use "type" or "tools" fields
# Note: Codex uses http_headers as an inline table, not a separate section
# Note: URLs must use the correct domain (host.docker.internal) for container access

# Build the correct URL prefix using the configured domain and port
# For host.docker.internal, resolve to the gateway IP to avoid DNS resolution issues in Rust
if [ "$MCP_GATEWAY_DOMAIN" = "host.docker.internal" ]; then
  # AWF network gateway IP is always 172.30.0.1
  RESOLVED_DOMAIN="172.30.0.1"
  echo "Resolving host.docker.internal to gateway IP: $RESOLVED_DOMAIN"
else
  RESOLVED_DOMAIN="$MCP_GATEWAY_DOMAIN"
fi
URL_PREFIX="http://${RESOLVED_DOMAIN}:${MCP_GATEWAY_PORT}"

# Create the TOML configuration
cat > "${GH_AW_RUNTIME_DIR:?}/mcp-config/config.toml" << 'TOML_EOF'
[history]
persistence = "none"

TOML_EOF

jq -r --arg urlPrefix "$URL_PREFIX" '
  .mcpServers | to_entries[] |
  "[mcp_servers.\(.key)]\n" +
  "url = \"" + ($urlPrefix + "/mcp/" + .key) + "\"\n" +
  "http_headers = { Authorization = \"\(.value.headers.Authorization)\" }\n"
' "$MCP_GATEWAY_OUTPUT" >> "${GH_AW_RUNTIME_DIR:?}/mcp-config/config.toml"

# Restrict permissions so only the runner process owner can read this file.
# config.toml contains the bearer token for the MCP gateway; an attacker
# who reads it could issue raw JSON-RPC calls directly to the gateway.
chmod 600 "${GH_AW_RUNTIME_DIR:?}/mcp-config/config.toml"

echo "Codex configuration written to ${GH_AW_RUNTIME_DIR:?}/mcp-config/config.toml"
echo ""
echo "Converted configuration:"
cat "${GH_AW_RUNTIME_DIR:?}/mcp-config/config.toml"
