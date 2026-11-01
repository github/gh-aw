#!/usr/bin/env bash
set +o histexpand

# Start MCP Scripts HTTP Server
# This script starts the mcp-scripts MCP server and waits for it to become ready

set -e

cd ${RUNNER_TEMP}/gh-aw/mcp-scripts || exit 1

# Verify required files exist
echo "Verifying mcp-scripts setup..."

# Check core configuration files
if [ ! -f mcp-server.cjs ]; then
  echo "ERROR: mcp-server.cjs not found in ${RUNNER_TEMP}/gh-aw/mcp-scripts"
  ls -la ${RUNNER_TEMP}/gh-aw/mcp-scripts/
  exit 1
fi
if [ ! -f tools.json ]; then
  echo "ERROR: tools.json not found in ${RUNNER_TEMP}/gh-aw/mcp-scripts"
  ls -la ${RUNNER_TEMP}/gh-aw/mcp-scripts/
  exit 1
fi

# Check required dependency files for the MCP server
# These files are required by mcp_scripts_mcp_server_http.cjs and its dependencies
REQUIRED_DEPS=(
  "mcp_scripts_mcp_server_http.cjs"
  "mcp_http_transport.cjs"
  "schema-validation.cjs"
  "mcp_enhanced_errors.cjs"
  "mcp_logger.cjs"
  "mcp_scripts_bootstrap.cjs"
  "error_helpers.cjs"
  "mcp_server_core.cjs"
  "safe_output_names.cjs"
  "mcp_scripts_config_loader.cjs"
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
  echo "ERROR: Missing required dependency files in ${RUNNER_TEMP}/gh-aw/mcp-scripts/"
  for file in "${MISSING_FILES[@]}"; do
    echo "  - $file"
  done
  echo
  echo "Current directory contents:"
  ls -la ${RUNNER_TEMP}/gh-aw/mcp-scripts/
  echo
  echo "These files should have been copied by the Setup Scripts action."
  echo "This usually indicates a problem with the actions/setup step."
  exit 1
fi

echo "Configuration files verified"
echo "All ${#REQUIRED_DEPS[@]} required dependency files present"

# Log environment configuration
echo "Server configuration:"
echo "  Port: $GH_AW_MCP_SCRIPTS_PORT"
echo "  API Key: ${GH_AW_MCP_SCRIPTS_API_KEY:0:8}..."
echo "  Working directory: $(pwd)"

# Ensure logs directory exists
mkdir -p /tmp/gh-aw/mcp-scripts/logs

# Create initial server.log file for artifact upload
{
  echo "MCP Scripts Server Log"
  echo "Start time: $(date)"
  echo "==========================================="
  echo ""
} > /tmp/gh-aw/mcp-scripts/logs/server.log

# Start the HTTP server in the background
echo "Starting mcp-scripts MCP HTTP server..."
node mcp-server.cjs >> /tmp/gh-aw/mcp-scripts/logs/server.log 2>&1 &
SERVER_PID=$!
echo "Started mcp-scripts MCP server with PID $SERVER_PID"

# Wait for server to be ready (max 10 seconds)
echo "Waiting for server to become ready..."
for i in {1..10}; do
  # Check if process is still running
  if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "ERROR: Server process $SERVER_PID has died"
    echo "Server log contents:"
    cat /tmp/gh-aw/mcp-scripts/logs/server.log
    exit 1
  fi
  
  # Check if server is responding
  if curl -s -f "http://localhost:$GH_AW_MCP_SCRIPTS_PORT/health" > /dev/null 2>&1; then
    echo "MCP Scripts server is ready (attempt $i/10)"
    
    # Print the startup log for debugging
    echo "::group::Server Log Contents"
    cat /tmp/gh-aw/mcp-scripts/logs/server.log
    echo "::endgroup::"
    
    break
  fi
  
  if [ "$i" -eq 10 ]; then
    echo "ERROR: MCP Scripts server failed to start after 10 seconds"
    echo "Process status: $(pgrep -f 'mcp-server.cjs' || echo 'not running')"
    echo "Server log contents:"
    cat /tmp/gh-aw/mcp-scripts/logs/server.log
    echo "Checking port availability:"
    netstat -tuln | grep "$GH_AW_MCP_SCRIPTS_PORT" || echo "Port $GH_AW_MCP_SCRIPTS_PORT not listening"
    exit 1
  fi
  
  echo "Waiting for server... (attempt $i/10)"
  sleep 1
done

# Output the configuration for the MCP client
{
  echo "port=$GH_AW_MCP_SCRIPTS_PORT"
  echo "api_key=$GH_AW_MCP_SCRIPTS_API_KEY"
} >> "$GITHUB_OUTPUT"
