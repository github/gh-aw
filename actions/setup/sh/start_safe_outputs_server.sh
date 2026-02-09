#!/usr/bin/env bash
# Start Safe Outputs MCP HTTP Server
# This script starts the safe-outputs MCP server and waits for it to become ready

set -e

cd /opt/gh-aw/safeoutputs || exit 1

# Verify required files exist
echo "Verifying safe-outputs setup..."

# Check core files (mcp-server.cjs and tools.json are required)
if [ ! -f mcp-server.cjs ]; then
  echo "ERROR: mcp-server.cjs not found in /opt/gh-aw/safeoutputs"
  ls -la /opt/gh-aw/safeoutputs/
  exit 1
fi
if [ ! -f tools.json ]; then
  echo "ERROR: tools.json not found in /opt/gh-aw/safeoutputs"
  ls -la /opt/gh-aw/safeoutputs/
  exit 1
fi

# config.json is optional - the server will create a default config if missing
if [ ! -f config.json ]; then
  echo "Note: config.json not found, server will use default configuration"
fi

# Check required dependency files for the MCP server
# These files are required by safe_outputs_mcp_server_http.cjs and its dependencies
REQUIRED_DEPS=(
  "safe_outputs_mcp_server_http.cjs"
  "mcp_http_transport.cjs"
  "mcp_logger.cjs"
  "safe_outputs_bootstrap.cjs"
  "error_helpers.cjs"
  "safe_outputs_append.cjs"
  "safe_outputs_handlers.cjs"
  "safe_outputs_tools_loader.cjs"
  "safe_outputs_config.cjs"
)

MISSING_FILES=()
for dep in "${REQUIRED_DEPS[@]}"; do
  if [ ! -f "$dep" ]; then
    MISSING_FILES+=("$dep")
  fi
done

if [ ${#MISSING_FILES[@]} -gt 0 ]; then
  echo "ERROR: Missing required dependency files in /opt/gh-aw/safeoutputs/"
  for file in "${MISSING_FILES[@]}"; do
    echo "  - $file"
  done
  echo
  echo "Current directory contents:"
  ls -la /opt/gh-aw/safeoutputs/
  echo
  echo "These files should have been copied by the Setup Scripts action."
  echo "This usually indicates a problem with the actions/setup step."
  exit 1
fi

echo "Configuration files verified"
echo "All ${#REQUIRED_DEPS[@]} required dependency files present"

# Log environment configuration
echo "Server configuration:"
echo "  Port: $GH_AW_SAFE_OUTPUTS_PORT"
echo "  API Key: ${GH_AW_SAFE_OUTPUTS_API_KEY:0:8}..."
echo "  Working directory: $(pwd)"

# Ensure logs directory exists
mkdir -p /tmp/gh-aw/mcp-logs/safeoutputs

# Create initial server.log file for artifact upload
{
  echo "Safe Outputs MCP Server Log"
  echo "Start time: $(date)"
  echo "==========================================="
  echo ""
} > /tmp/gh-aw/mcp-logs/safeoutputs/server.log

# Start the HTTP server in the background with DEBUG enabled
echo "Starting safe-outputs MCP HTTP server..."
DEBUG="*" node mcp-server.cjs >> /tmp/gh-aw/mcp-logs/safeoutputs/server.log 2>&1 &
SERVER_PID=$!
echo "Started safe-outputs MCP server with PID $SERVER_PID"

# Wait for server to be ready (max 60 seconds)
echo "Waiting for server to become ready..."
for i in {1..60}; do
  # Check if process is still running
  if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "ERROR: Server process $SERVER_PID has died"
    echo "Server log contents:"
    cat /tmp/gh-aw/mcp-logs/safeoutputs/server.log
    exit 1
  fi
  
  # Check if server is responding
  if curl -s -f "http://localhost:$GH_AW_SAFE_OUTPUTS_PORT/health" > /dev/null 2>&1; then
    echo "Safe Outputs MCP server is ready (attempt $i/60)"
    
    # Print the startup log for debugging
    echo "::notice::Safe Outputs MCP Server Startup Log"
    echo "::group::Server Log Contents"
    cat /tmp/gh-aw/mcp-logs/safeoutputs/server.log
    echo "::endgroup::"
    
    break
  fi
  
  if [ "$i" -eq 60 ]; then
    echo "ERROR: Safe Outputs MCP server failed to start after 60 seconds"
    echo "Process status: $(pgrep -f 'mcp-server.cjs' || echo 'not running')"
    echo "Server log contents:"
    cat /tmp/gh-aw/mcp-logs/safeoutputs/server.log
    echo "Checking port availability:"
    netstat -tuln | grep "$GH_AW_SAFE_OUTPUTS_PORT" || echo "Port $GH_AW_SAFE_OUTPUTS_PORT not listening"
    exit 1
  fi
  
  echo "Waiting for server... (attempt $i/60)"
  sleep 1
done

# Output the configuration for the MCP client
{
  echo "port=$GH_AW_SAFE_OUTPUTS_PORT"
  echo "api_key=$GH_AW_SAFE_OUTPUTS_API_KEY"
} >> "$GITHUB_OUTPUT"
