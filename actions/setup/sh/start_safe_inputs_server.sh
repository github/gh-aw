#!/usr/bin/env bash
# Start Safe Inputs MCP HTTP Server
# This script starts the safe-inputs MCP server and waits for it to become ready

set -e

cd /opt/gh-aw/safe-inputs || exit 1

# Verify required files exist
echo "Verifying safe-inputs setup..."

# Check core configuration files
if [ ! -f mcp-server.cjs ]; then
  echo "ERROR: mcp-server.cjs not found in /opt/gh-aw/safe-inputs"
  ls -la /opt/gh-aw/safe-inputs/
  exit 1
fi
if [ ! -f tools.json ]; then
  echo "ERROR: tools.json not found in /opt/gh-aw/safe-inputs"
  ls -la /opt/gh-aw/safe-inputs/
  exit 1
fi

# Check required dependency files for the MCP server
# These files are required by safe_inputs_mcp_server_http.cjs and its dependencies
REQUIRED_DEPS=(
  "safe_inputs_mcp_server_http.cjs"
  "mcp_http_transport.cjs"
  "safe_inputs_validation.cjs"
  "mcp_enhanced_errors.cjs"
  "mcp_logger.cjs"
  "safe_inputs_bootstrap.cjs"
  "error_helpers.cjs"
  "mcp_server_core.cjs"
  "safe_inputs_config_loader.cjs"
  "read_buffer.cjs"
  "mcp_handler_shell.cjs"
  "mcp_handler_python.cjs"
  "mcp_handler_go.cjs"
  "mcp_handler_javascript.cjs"
)

MISSING_FILES=()
for dep in "${REQUIRED_DEPS[@]}"; do
  if [ ! -f "$dep" ]; then
    MISSING_FILES+=("$dep")
  fi
done

if [ ${#MISSING_FILES[@]} -gt 0 ]; then
  echo "ERROR: Missing required dependency files in /opt/gh-aw/safe-inputs/"
  for file in "${MISSING_FILES[@]}"; do
    echo "  - $file"
  done
  echo
  echo "Current directory contents:"
  ls -la /opt/gh-aw/safe-inputs/
  echo
  echo "These files should have been copied by the Setup Scripts action."
  echo "This usually indicates a problem with the actions/setup step."
  exit 1
fi

echo "Configuration files verified"
echo "All ${#REQUIRED_DEPS[@]} required dependency files present"

# Log environment configuration
echo "Server configuration:"
echo "  Port: $GH_AW_SAFE_INPUTS_PORT"
echo "  API Key: ${GH_AW_SAFE_INPUTS_API_KEY:0:8}..."
echo "  Working directory: $(pwd)"

# Ensure logs directory exists
mkdir -p /tmp/gh-aw/safe-inputs/logs

# Create initial server.log file for artifact upload
{
  echo "Safe Inputs MCP Server Log"
  echo "Start time: $(date)"
  echo "==========================================="
  echo ""
} > /tmp/gh-aw/safe-inputs/logs/server.log

# Start the HTTP server in the background with DEBUG enabled
echo "Starting safe-inputs MCP HTTP server..."
DEBUG="*" node mcp-server.cjs >> /tmp/gh-aw/safe-inputs/logs/server.log 2>&1 &
SERVER_PID=$!
echo "Started safe-inputs MCP server with PID $SERVER_PID"

# Wait for server to be ready (max 10 seconds)
echo "Waiting for server to become ready..."
for i in {1..10}; do
  # Check if process is still running
  if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "ERROR: Server process $SERVER_PID has died"
    echo "Server log contents:"
    cat /tmp/gh-aw/safe-inputs/logs/server.log
    exit 1
  fi
  
  # Check if server is responding
  if curl -s -f "http://localhost:$GH_AW_SAFE_INPUTS_PORT/health" > /dev/null 2>&1; then
    echo "Safe Inputs MCP server is ready (attempt $i/10)"
    
    # Print the startup log for debugging
    echo "::notice::Safe Inputs MCP Server Startup Log"
    echo "::group::Server Log Contents"
    cat /tmp/gh-aw/safe-inputs/logs/server.log
    echo "::endgroup::"
    
    break
  fi
  
  if [ "$i" -eq 10 ]; then
    echo "ERROR: Safe Inputs MCP server failed to start after 10 seconds"
    echo "Process status: $(pgrep -f 'mcp-server.cjs' || echo 'not running')"
    echo "Server log contents:"
    cat /tmp/gh-aw/safe-inputs/logs/server.log
    echo "Checking port availability:"
    netstat -tuln | grep "$GH_AW_SAFE_INPUTS_PORT" || echo "Port $GH_AW_SAFE_INPUTS_PORT not listening"
    exit 1
  fi
  
  echo "Waiting for server... (attempt $i/10)"
  sleep 1
done

# Output the configuration for the MCP client
{
  echo "port=$GH_AW_SAFE_INPUTS_PORT"
  echo "api_key=$GH_AW_SAFE_INPUTS_API_KEY"
} >> "$GITHUB_OUTPUT"
