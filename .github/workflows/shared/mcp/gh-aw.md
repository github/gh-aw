---
permissions:
  actions: read
mcp-servers:
  gh-aw:
    type: http
    url: http://localhost:8765
steps:
  - name: Setup Go
    uses: actions/setup-go@v6
    with:
      go-version-file: go.mod
      cache: true
  - name: Install dependencies
    run: make deps-dev
  - name: Install binary as 'gh-aw'
    run: |
      # Check if gh-aw extension is already installed
      if gh extension list | grep -q "github/gh-aw"; then
        echo "gh-aw extension already installed, skipping installation..."
      else
        # Check if a different extension provides the 'aw' command
        # gh extension list format: NAME  COMMAND  VERSION
        EXISTING_EXTENSION=$(gh extension list | awk '$2 == "aw" {print $1}' | head -n1)
        if [ -n "$EXISTING_EXTENSION" ]; then
          echo "Found conflicting extension providing 'aw' command: $EXISTING_EXTENSION"
          echo "Removing conflicting extension..."
          gh extension remove "$EXISTING_EXTENSION" || true
        fi
        
        # Install the extension
        echo "Installing gh-aw extension..."
        make install
      fi
      
      # Verify installation
      gh aw --version
    env:
      GH_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
  - name: Start MCP server
    run: |
      set -e
      ./gh-aw mcp-server --cmd ./gh-aw --port 8765 &
      MCP_PID=$!
      
      # Robust health check with TCP connection test
      echo "Waiting for MCP server to start (PID: $MCP_PID)..."
      for i in {1..15}; do
        # Check if process is still running
        if ! kill -0 $MCP_PID 2>/dev/null; then
          echo "Error: MCP server process died unexpectedly"
          exit 1
        fi
        
        # Try to connect to the server port
        if timeout 1 bash -c "echo > /dev/tcp/localhost/8765" 2>/dev/null; then
          echo "MCP server is accepting connections on port 8765"
          echo "MCP server started successfully with PID $MCP_PID"
          exit 0
        fi
        
        echo "Waiting for server to accept connections... (attempt $i/15)"
        sleep 1
      done
      
      echo "Error: MCP server failed to accept connections after 15 seconds"
      exit 1
    env:
      GH_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
---
