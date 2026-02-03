#!/usr/bin/env bash
# Start Serena MCP HTTP Server
# This script starts the Serena MCP server using uvx and waits for it to become ready

set -e

# Ensure logs directory exists
mkdir -p /tmp/gh-aw/serena/logs

echo "Starting Serena MCP server using uvx..."
echo "  Port: $GH_AW_SERENA_PORT"
echo "  Context: copilot"
echo "  Project: $GITHUB_WORKSPACE"

# Create initial server.log file
{
  echo "Serena MCP Server Log"
  echo "Start time: $(date)"
  echo "==========================================="
  echo ""
} > /tmp/gh-aw/serena/logs/server.log

# Start Serena with uvx in background with DEBUG enabled
nohup env DEBUG="*" uvx --from git+https://github.com/oraios/serena serena start-mcp-server \
  --transport streamable-http \
  --port "${GH_AW_SERENA_PORT}" \
  --context copilot \
  --project "${GITHUB_WORKSPACE}" \
  >> /tmp/gh-aw/serena/logs/server.log 2>&1 &

SERVER_PID=$!
echo "Started Serena MCP server with PID $SERVER_PID"

# Wait for server to be ready (max 30 seconds)
echo "Waiting for server to become ready..."
for i in {1..30}; do
  # Check if process is still running
  if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "ERROR: Server process $SERVER_PID has died"
    echo "Server log contents:"
    cat /tmp/gh-aw/serena/logs/server.log
    exit 1
  fi
  
  # Check if server is responding
  if curl -s -o /dev/null -w '%{http_code}' "http://localhost:${GH_AW_SERENA_PORT}/health" | grep -q "200"; then
    echo "Serena MCP server is ready (attempt $i/30)"
    
    # Print the startup log for debugging
    echo "::notice::Serena MCP Server Startup Log"
    echo "::group::Server Log Contents"
    cat /tmp/gh-aw/serena/logs/server.log
    echo "::endgroup::"
    
    break
  fi
  
  if [ "$i" -eq 30 ]; then
    echo "ERROR: Serena MCP server failed to start after 30 seconds"
    echo "Process status: $(pgrep -f 'serena' || echo 'not running')"
    echo "Server log contents:"
    cat /tmp/gh-aw/serena/logs/server.log
    echo "Checking port availability:"
    netstat -tuln | grep "$GH_AW_SERENA_PORT" || echo "Port $GH_AW_SERENA_PORT not listening"
    exit 1
  fi
  
  echo "Waiting for Serena MCP server... ($i/30)"
  sleep 1
done

echo "Serena MCP server started successfully on port ${GH_AW_SERENA_PORT}"
