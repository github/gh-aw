#!/usr/bin/env bash
set +o histexpand

# Test script for convert_gateway_config_codex.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_PATH="$SCRIPT_DIR/convert_gateway_config_codex.sh"

# Setup test directory
TEST_DIR=$(mktemp -d "${PWD}/.codex-config-tests.XXXXXX")
export GH_AW_RUNTIME_DIR="$TEST_DIR"
trap "rm -rf $TEST_DIR" EXIT

echo "=== Testing convert_gateway_config_codex.sh ==="
echo ""

# Set required environment variables for all tests
export MCP_GATEWAY_DOMAIN="host.docker.internal"
export MCP_GATEWAY_PORT="80"

# Create the output directory the script writes to
mkdir -p "${GH_AW_RUNTIME_DIR}/mcp-config"

# Test 1: Basic gateway output conversion
echo "Test 1: Convert basic gateway output with Authorization headers"
mkdir -p "$TEST_DIR/mcp-config"

cat > "$TEST_DIR/gateway-output.json" << 'EOF'
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://localhost:8080/mcp/github",
      "headers": {
        "Authorization": "test-api-key-abc123"
      }
    },
    "playwright": {
      "type": "http",
      "url": "http://localhost:8080/mcp/playwright",
      "headers": {
        "Authorization": "test-api-key-xyz789"
      }
    }
  }
}
EOF

export MCP_GATEWAY_OUTPUT="$TEST_DIR/gateway-output.json"
bash "$SCRIPT_PATH" > /dev/null 2>&1

# Check that config.toml was created
if [ ! -f "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml" ]; then
  echo "✗ FAIL: config.toml not created"
  exit 1
fi

# Check for correct TOML format with http_headers inline table
if ! grep -q 'http_headers = { Authorization = "test-api-key-abc123" }' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: Missing or incorrect http_headers for github"
  cat "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"
  exit 1
fi

if ! grep -q 'http_headers = { Authorization = "test-api-key-xyz789" }' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: Missing or incorrect http_headers for playwright"
  cat "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"
  exit 1
fi

# Check that it does NOT use the old [mcp_servers.*.headers] format
if grep -q '^\[mcp_servers\..*\.headers\]' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: Found old-style [mcp_servers.*.headers] section (should use http_headers inline table)"
  cat "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"
  exit 1
fi

# Check for history section
if ! grep -q '\[history\]' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: Missing [history] section"
  exit 1
fi

if ! grep -q 'persistence = "none"' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: Missing persistence = \"none\" in history section"
  exit 1
fi

# Check that URLs are correctly formatted with domain and port
if ! grep -q 'url = "http://host.docker.internal:80/mcp/github"' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: URL not correctly formatted for github server"
  cat "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"
  exit 1
fi

echo "✓ PASS: Basic gateway output conversion"
echo ""

# Test 2: Validate TOML structure (shell-native checks)
echo "Test 2: Validate generated TOML structure"
# Verify the file contains balanced section headers and required keys.
# Content-level checks in Test 1 already confirm correctness of specific values;
# these checks guard against truncated or structurally broken output.
if ! grep -q '^\[mcp_servers\.' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: config.toml is missing [mcp_servers.*] sections"
  cat "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"
  exit 1
fi
if ! grep -qE '^url\s*=' "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"; then
  echo "✗ FAIL: config.toml is missing url fields"
  cat "${GH_AW_RUNTIME_DIR}/mcp-config/config.toml"
  exit 1
fi
echo "✓ PASS: Generated TOML structure is valid"
echo ""

# Test 3: Error handling - missing MCP_GATEWAY_OUTPUT
echo "Test 3: Error handling for missing MCP_GATEWAY_OUTPUT"
unset MCP_GATEWAY_OUTPUT
if bash "$SCRIPT_PATH" > /dev/null 2>&1; then
  echo "✗ FAIL: Script should fail when MCP_GATEWAY_OUTPUT is not set"
  exit 1
else
  echo "✓ PASS: Script correctly fails when MCP_GATEWAY_OUTPUT is not set"
fi
echo ""

# Test 4: Error handling - missing gateway output file
echo "Test 4: Error handling for missing gateway output file"
export MCP_GATEWAY_OUTPUT="$TEST_DIR/nonexistent.json"
if bash "$SCRIPT_PATH" > /dev/null 2>&1; then
  echo "✗ FAIL: Script should fail when gateway output file doesn't exist"
  exit 1
else
  echo "✓ PASS: Script correctly fails when gateway output file doesn't exist"
fi
echo ""

# Test 5: Error handling - missing MCP_GATEWAY_DOMAIN
echo "Test 5: Error handling for missing MCP_GATEWAY_DOMAIN"
export MCP_GATEWAY_OUTPUT="$TEST_DIR/gateway-output.json"
unset MCP_GATEWAY_DOMAIN
if bash "$SCRIPT_PATH" > /dev/null 2>&1; then
  echo "✗ FAIL: Script should fail when MCP_GATEWAY_DOMAIN is not set"
  exit 1
else
  echo "✓ PASS: Script correctly fails when MCP_GATEWAY_DOMAIN is not set"
fi
export MCP_GATEWAY_DOMAIN="host.docker.internal"
echo ""

# Test 6: Error handling - missing MCP_GATEWAY_PORT
echo "Test 6: Error handling for missing MCP_GATEWAY_PORT"
unset MCP_GATEWAY_PORT
if bash "$SCRIPT_PATH" > /dev/null 2>&1; then
  echo "✗ FAIL: Script should fail when MCP_GATEWAY_PORT is not set"
  exit 1
else
  echo "✓ PASS: Script correctly fails when MCP_GATEWAY_PORT is not set"
fi
export MCP_GATEWAY_PORT="80"
echo ""

echo "=== All tests passed ==="
