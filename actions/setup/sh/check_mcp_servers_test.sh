#!/bin/bash
set +o histexpand

# Test script for check_mcp_servers.sh
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_PATH="$SCRIPT_DIR/check_mcp_servers.sh"

# Color codes for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Print test result
print_result() {
  local test_name="$1"
  local result="$2"
  
  TESTS_RUN=$((TESTS_RUN + 1))
  
  if [ "$result" = "PASS" ]; then
    echo -e "${GREEN}✓ PASS${NC}: $test_name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo -e "${RED}✗ FAIL${NC}: $test_name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

start_mock_mcp_server() {
  local port_file="$1"
  local log_file="$2"

  node - "$port_file" >"$log_file" 2>&1 <<'NODE' &
const fs = require("fs");
const http = require("http");

const portFile = process.argv[2];

const send = (res, code, payload, sessionId) => {
  const body = JSON.stringify(payload);
  res.writeHead(code, {
    "Content-Type": "application/json",
    "Content-Length": Buffer.byteLength(body),
    ...(sessionId ? { "Mcp-Session-Id": sessionId } : {}),
  });
  res.end(body);
};

const server = http.createServer((req, res) => {
  let raw = "";
  req.on("data", chunk => {
    raw += chunk;
  });
  req.on("end", () => {
    let data = {};
    try {
      data = JSON.parse(raw || "{}");
    } catch {
      data = {};
    }

    const method = data.method;
    const reqId = data.id ?? 1;

    if (req.url.endsWith("/github")) {
      if (method === "initialize") {
        send(res, 200, { jsonrpc: "2.0", id: reqId, result: { protocolVersion: "2024-11-05", capabilities: {}, serverInfo: { name: "github", version: "1.0.0" } } }, "s1");
      } else if (method === "tools/list") {
        send(res, 200, { jsonrpc: "2.0", id: reqId, result: { tools: [] } });
      } else {
        send(res, 200, { jsonrpc: "2.0", id: reqId, result: {} });
      }
      return;
    }

    if (req.url.endsWith("/datadog")) {
      if (method === "initialize") {
        send(res, 403, { errors: ["Forbidden"] });
      } else {
        send(res, 200, { jsonrpc: "2.0", id: reqId, result: {} });
      }
      return;
    }

    send(res, 404, { error: "not found" });
  });
});

server.listen(0, "127.0.0.1", () => {
  fs.writeFileSync(portFile, String(server.address().port), "utf8");
});
NODE
  echo "$!"
}

wait_for_port_file() {
  local port_file="$1"
  local i=0
  while [ ! -s "$port_file" ] && [ $i -lt 50 ]; do
    sleep 0.1
    i=$((i + 1))
  done
}

start_and_validate_mock_server() {
  local port_file="$1"
  local log_file="$2"
  local server_pid

  server_pid=$(start_mock_mcp_server "$port_file" "$log_file")
  if [ -z "$server_pid" ] || ! kill -0 "$server_pid" 2>/dev/null; then
    return 1
  fi

  wait_for_port_file "$port_file"

  if [ ! -s "$port_file" ]; then
    if kill -0 "$server_pid" 2>/dev/null; then
      kill "$server_pid" 2>/dev/null || true
    fi
    return 1
  fi

  echo "$server_pid"
}

create_failing_gateway_client() {
  local fake_bin="$1"
  mkdir -p "$fake_bin"
  cat > "$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CURL_MARKER"
printf '%s\n%s\n' '{"error":"backend_unavailable","retryable":true}' '503'
EOF
  cat > "$fake_bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$fake_bin/curl" "$fake_bin/sleep"
}

# Test 1: Script syntax is valid
test_script_syntax() {
  echo ""
  echo "Test 1: Verify script syntax"
  
  if bash -n "$SCRIPT_PATH" 2>/dev/null; then
    print_result "Script syntax is valid" "PASS"
  else
    print_result "Script has syntax errors" "FAIL"
  fi
}

# Test 2: Script requires 3 arguments
test_argument_validation() {
  echo ""
  echo "Test 2: Argument validation"
  
  # Test with no arguments
  if ! bash "$SCRIPT_PATH" 2>/dev/null; then
    print_result "Script rejects no arguments" "PASS"
  else
    print_result "Script should reject no arguments" "FAIL"
  fi
  
  # Test with 1 argument
  if ! bash "$SCRIPT_PATH" "config.json" 2>/dev/null; then
    print_result "Script rejects 1 argument" "PASS"
  else
    print_result "Script should reject 1 argument" "FAIL"
  fi
  
  # Test with 2 arguments
  if ! bash "$SCRIPT_PATH" "config.json" "http://localhost:8080" 2>/dev/null; then
    print_result "Script rejects 2 arguments" "PASS"
  else
    print_result "Script should reject 2 arguments" "FAIL"
  fi
}

# Test 3: Config file not found
test_config_not_found() {
  echo ""
  echo "Test 3: Config file not found"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local nonexistent_config="$tmpdir/nonexistent.json"
  
  if ! bash "$SCRIPT_PATH" "$nonexistent_config" "http://localhost:8080" "test-key" 2>/dev/null; then
    print_result "Script rejects non-existent config file" "PASS"
  else
    print_result "Script should reject non-existent config file" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 4: Invalid JSON configuration
test_invalid_json_config() {
  echo ""
  echo "Test 4: Invalid JSON configuration"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  
  # Create invalid JSON
  echo "{ invalid json" > "$config_file"
  
  if ! bash "$SCRIPT_PATH" "$config_file" "http://localhost:8080" "test-key" 2>/dev/null; then
    print_result "Script rejects invalid JSON" "PASS"
  else
    print_result "Script should reject invalid JSON" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 5: Empty mcpServers object
test_empty_servers() {
  echo ""
  echo "Test 5: Empty mcpServers object"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  
  # Create config with empty mcpServers
  cat > "$config_file" <<'EOF'
{
  "mcpServers": {},
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF
  
  # Should exit 0 but indicate no servers
  if bash "$SCRIPT_PATH" "$config_file" "http://localhost:8080" "test-key" >/dev/null 2>&1; then
    print_result "Script handles empty mcpServers gracefully" "PASS"
  else
    print_result "Script should handle empty mcpServers gracefully" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 6: Configuration with null mcpServers
test_null_servers() {
  echo ""
  echo "Test 6: Null mcpServers"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  
  # Create config with null mcpServers
  cat > "$config_file" <<'EOF'
{
  "mcpServers": null,
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF
  
  # Should exit 0 but indicate no servers
  if bash "$SCRIPT_PATH" "$config_file" "http://localhost:8080" "test-key" >/dev/null 2>&1; then
    print_result "Script handles null mcpServers gracefully" "PASS"
  else
    print_result "Script should handle null mcpServers gracefully" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 7: Valid configuration with HTTP server
test_valid_http_server() {
  echo ""
  echo "Test 7: Valid configuration with HTTP server"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  
  # Create valid config with HTTP server
  cat > "$config_file" <<'EOF'
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://localhost:8080/mcp/github",
      "headers": {
        "Authorization": "Bearer test-token"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF
  
  # Script should fail because no servers can be connected (no gateway running)
  if ! bash "$SCRIPT_PATH" "$config_file" "http://localhost:8080" "test-key" >/dev/null 2>&1; then
    print_result "Script fails when no servers can connect" "PASS"
  else
    print_result "Script should fail when no servers can connect" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 8: Server without URL (stdio server)
test_server_without_url() {
  echo ""
  echo "Test 8: Server without URL (stdio server)"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  
  # Create config with stdio server (no URL)
  cat > "$config_file" <<'EOF'
{
  "mcpServers": {
    "safeinputs": {
      "type": "stdio",
      "command": "gh",
      "args": ["aw", "mcp-server", "--mode", "mcp-scripts"]
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF
  
  # Should fail because only stdio servers (which are skipped)
  if ! bash "$SCRIPT_PATH" "$config_file" "http://localhost:8080" "test-key" >/dev/null 2>&1; then
    print_result "Script fails when only stdio servers (no HTTP servers)" "PASS"
  else
    print_result "Script should fail when only stdio servers" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 9: Multiple servers with mixed types
test_mixed_servers() {
  echo ""
  echo "Test 9: Multiple servers with mixed types"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  
  # Create config with multiple servers
  cat > "$config_file" <<'EOF'
{
  "mcpServers": {
    "safeinputs": {
      "type": "stdio",
      "command": "gh",
      "args": ["aw", "mcp-server", "--mode", "mcp-scripts"]
    },
    "github": {
      "type": "http",
      "url": "http://localhost:8080/mcp/github",
      "headers": {
        "Authorization": "Bearer github-token"
      }
    },
    "playwright": {
      "type": "http",
      "url": "http://localhost:8080/mcp/playwright"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF
  
  # Should fail because HTTP servers cannot connect (no gateway running)
  if ! bash "$SCRIPT_PATH" "$config_file" "http://localhost:8080" "test-key" >/dev/null 2>&1; then
    print_result "Script fails when HTTP servers cannot connect" "PASS"
  else
    print_result "Script should fail when HTTP servers cannot connect" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 10: Key validation functions exist
test_validation_functions_exist() {
  echo ""
  echo "Test 10: Verify key validation logic exists"
  
  # Check for configuration file validation
  if grep -q "Gateway configuration file not found" "$SCRIPT_PATH"; then
    print_result "Config file validation exists" "PASS"
  else
    print_result "Config file validation missing" "FAIL"
  fi
  
  # Check for mcpServers parsing
  if grep -q "Failed to parse mcpServers" "$SCRIPT_PATH"; then
    print_result "mcpServers parsing validation exists" "PASS"
  else
    print_result "mcpServers parsing validation missing" "FAIL"
  fi
  
  # Check for tools/list request (used instead of ping to verify backend connectivity)
  if grep -q 'method.*tools/list' "$SCRIPT_PATH"; then
    print_result "tools/list request logic exists" "PASS"
  else
    print_result "tools/list request logic missing" "FAIL"
  fi
  
  # Check for MCP ping as first step (per MCP protocol)
  if grep -q 'method.*ping' "$SCRIPT_PATH"; then
    print_result "MCP ping request logic exists" "PASS"
  else
    print_result "MCP ping request logic missing" "FAIL"
  fi
  
  # Check for MCP initialize before tools/list (per MCP protocol)
  if grep -q 'method.*initialize' "$SCRIPT_PATH"; then
    print_result "MCP initialize request logic exists" "PASS"
  else
    print_result "MCP initialize request logic missing" "FAIL"
  fi
  
  # Check for Mcp-Session-Id handling
  if grep -q 'Mcp-Session-Id' "$SCRIPT_PATH"; then
    print_result "Mcp-Session-Id session tracking exists" "PASS"
  else
    print_result "Mcp-Session-Id session tracking missing" "FAIL"
  fi
  
  # Check for gateway config authentication logic
  if grep -q "Authorization" "$SCRIPT_PATH"; then
    print_result "Gateway config authentication logic exists" "PASS"
  else
    print_result "Gateway config authentication logic missing" "FAIL"
  fi
}

# Test 11: Optional failing server should not fail startup when another server is healthy
test_optional_server_failure_degrades_to_warning() {
  echo ""
  echo "Test 11: Optional server failure degrades to warning"

  local tmpdir
  tmpdir=$(mktemp -d)
  local port_file="$tmpdir/port"
  local config_file="$tmpdir/config.json"

  local server_pid
  if ! server_pid=$(start_and_validate_mock_server "$port_file" "$tmpdir/mock.log"); then
    print_result "Mock MCP server failed to start (check $tmpdir/mock.log)" "FAIL"
    return
  fi

  local port
  port=$(cat "$port_file")

  cat > "$config_file" <<EOF
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/github"
    },
    "datadog": {
      "type": "http",
      "required": false,
      "url": "http://127.0.0.1:${port}/mcp/datadog"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF

  if bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:${port}" "test-key" >/dev/null 2>&1; then
    print_result "Optional failing server does not fail startup" "PASS"
  else
    print_result "Optional failing server should not fail startup" "FAIL"
  fi

  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
  rm -rf "$tmpdir"
}

# Test 12: Server with no required field should fail startup by default (required is the default)
test_required_server_failure_is_fatal() {
  echo ""
  echo "Test 12: Server failure is fatal by default (required is the default)"

  local tmpdir
  tmpdir=$(mktemp -d)
  local port_file="$tmpdir/port"
  local config_file="$tmpdir/config.json"

  local server_pid
  if ! server_pid=$(start_and_validate_mock_server "$port_file" "$tmpdir/mock.log"); then
    print_result "Mock MCP server failed to start (check $tmpdir/mock.log)" "FAIL"
    return
  fi

  local port
  port=$(cat "$port_file")

  cat > "$config_file" <<EOF
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/github"
    },
    "datadog": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/datadog"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF

  if ! bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:${port}" "test-key" >/dev/null 2>&1; then
    print_result "Failing server without required field defaults to fatal" "PASS"
  else
    print_result "Failing server without required field should default to fatal" "FAIL"
  fi

  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
  rm -rf "$tmpdir"
}

# Test 13: All optional servers fail => startup should fail (no successful connections)
test_all_optional_servers_fail_is_error() {
  echo ""
  echo "Test 13: All optional servers fail => startup fails with clear error"

  local tmpdir
  tmpdir=$(mktemp -d)
  local port_file="$tmpdir/port"
  local config_file="$tmpdir/config.json"

  local server_pid
  if ! server_pid=$(start_and_validate_mock_server "$port_file" "$tmpdir/mock.log"); then
    print_result "Mock MCP server failed to start (check $tmpdir/mock.log)" "FAIL"
    return
  fi

  local port
  port=$(cat "$port_file")

  # Only the datadog server (returns 403 on initialize), marked optional
  cat > "$config_file" <<EOF
{
  "mcpServers": {
    "datadog": {
      "type": "http",
      "required": false,
      "url": "http://127.0.0.1:${port}/mcp/datadog"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF

  local output_file="$tmpdir/output.txt"
  local run_result=0
  bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:${port}" "test-key" >"$output_file" 2>&1 || run_result=$?

  if [ $run_result -ne 0 ]; then
    print_result "All optional servers failing causes startup to fail" "PASS"
  else
    print_result "All optional servers failing should cause startup to fail" "FAIL"
  fi

  # Check that the error message is about all optional servers failing, not about missing config
  if grep -q "optional server" "$output_file"; then
    print_result "Error message correctly references optional server failure" "PASS"
  else
    print_result "Error message should reference optional server failure (not 'no HTTP servers configured')" "FAIL"
  fi

  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
  rm -rf "$tmpdir"
}

# Test 14: Optional server declared via GH_AW_MCP_OPTIONAL_SERVERS should not fail startup
test_optional_server_from_env_degrades_to_warning() {
  echo ""
  echo "Test 14: Optional server from GH_AW_MCP_OPTIONAL_SERVERS degrades to warning"

  local tmpdir
  tmpdir=$(mktemp -d)
  local port_file="$tmpdir/port"
  local config_file="$tmpdir/config.json"

  local server_pid
  if ! server_pid=$(start_and_validate_mock_server "$port_file" "$tmpdir/mock.log"); then
    print_result "Mock MCP server failed to start (check $tmpdir/mock.log)" "FAIL"
    return
  fi

  local port
  port=$(cat "$port_file")

  # The gateway output does not echo the "required" flag, so criticality is
  # forwarded through GH_AW_MCP_OPTIONAL_SERVERS instead.
  cat > "$config_file" <<EOF
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/github"
    },
    "datadog": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/datadog"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF

  local output_file="$tmpdir/output.txt"
  local run_result=0
  GH_AW_MCP_OPTIONAL_SERVERS="grafana,datadog" bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:${port}" "test-key" >"$output_file" 2>&1 || run_result=$?

  if [ $run_result -eq 0 ]; then
    print_result "Optional server from environment does not fail startup" "PASS"
  else
    print_result "Optional server from environment should not fail startup" "FAIL"
  fi

  if grep -q "non-critical MCP server unavailable, continuing without it" "$output_file"; then
    print_result "Non-critical failure logs an actionable message" "PASS"
  else
    print_result "Non-critical failure should log an actionable message" "FAIL"
  fi

  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
  rm -rf "$tmpdir"
}

# Test 15: Servers not listed in GH_AW_MCP_OPTIONAL_SERVERS stay required
test_env_optional_list_does_not_affect_other_servers() {
  echo ""
  echo "Test 15: GH_AW_MCP_OPTIONAL_SERVERS only applies to listed servers"

  local tmpdir
  tmpdir=$(mktemp -d)
  local port_file="$tmpdir/port"
  local config_file="$tmpdir/config.json"

  local server_pid
  if ! server_pid=$(start_and_validate_mock_server "$port_file" "$tmpdir/mock.log"); then
    print_result "Mock MCP server failed to start (check $tmpdir/mock.log)" "FAIL"
    return
  fi

  local port
  port=$(cat "$port_file")

  cat > "$config_file" <<EOF
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/github"
    },
    "datadog": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/datadog"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "test-key"
  }
}
EOF

  if ! GH_AW_MCP_OPTIONAL_SERVERS="grafana" bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:${port}" "test-key" >/dev/null 2>&1; then
    print_result "Unlisted failing server remains fatal" "PASS"
  else
    print_result "Unlisted failing server should remain fatal" "FAIL"
  fi

  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
  rm -rf "$tmpdir"
}

# Test 16: Deferred enclave server succeeds without an eager probe
test_deferred_enclave_server_is_not_probed() {
  echo ""
  echo "Test 16: Deferred enclave server succeeds without an eager probe"

  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  local marker="$tmpdir/curl-invocations"
  local fake_bin="$tmpdir/bin"
  create_failing_gateway_client "$fake_bin"

  cat > "$config_file" <<'EOF'
{
  "mcpServers": {
    "awf-enclave": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp/awf-enclave"
    }
  }
}
EOF

  local output_file="$tmpdir/output.txt"
  local run_result=0
  PATH="$fake_bin:$PATH" CURL_MARKER="$marker" GH_AW_MCP_DEFERRED_SERVERS="awf-enclave" \
    bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:8080" "test-key" >"$output_file" 2>&1 || run_result=$?

  if [ $run_result -eq 0 ]; then
    print_result "Deferred-only enclave configuration succeeds" "PASS"
  else
    print_result "Deferred-only enclave configuration should succeed" "FAIL"
  fi

  if [ ! -e "$marker" ] && grep -q "awf-enclave: deferred until its AWF-owned backend starts" "$output_file"; then
    print_result "Deferred enclave server is not probed" "PASS"
  else
    print_result "Deferred enclave server should not be probed" "FAIL"
  fi

  rm -rf "$tmpdir"
}

# Test 17: Deferred enclave server does not affect healthy required servers
test_deferred_enclave_with_healthy_required_server() {
  echo ""
  echo "Test 17: Deferred enclave server and healthy required server succeed"

  local tmpdir
  tmpdir=$(mktemp -d)
  local port_file="$tmpdir/port"
  local config_file="$tmpdir/config.json"

  local server_pid
  if ! server_pid=$(start_and_validate_mock_server "$port_file" "$tmpdir/mock.log"); then
    print_result "Mock MCP server failed to start (check $tmpdir/mock.log)" "FAIL"
    return
  fi

  local port
  port=$(cat "$port_file")

  cat > "$config_file" <<EOF
{
  "mcpServers": {
    "awf-enclave": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/awf-enclave"
    },
    "github": {
      "type": "http",
      "url": "http://127.0.0.1:${port}/mcp/github"
    }
  }
}
EOF

  if GH_AW_MCP_DEFERRED_SERVERS="awf-enclave" \
    bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:${port}" "test-key" >/dev/null 2>&1; then
    print_result "Healthy required server passes alongside deferred enclave server" "PASS"
  else
    print_result "Deferred enclave server should not affect healthy required server" "FAIL"
  fi

  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
  rm -rf "$tmpdir"
}

# Test 18: The enclave server remains required without compiler classification
test_unclassified_enclave_server_is_fatal() {
  echo ""
  echo "Test 18: Unclassified enclave server failure remains fatal"

  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  local marker="$tmpdir/curl-invocations"
  local fake_bin="$tmpdir/bin"
  create_failing_gateway_client "$fake_bin"

  cat > "$config_file" <<'EOF'
{
  "mcpServers": {
    "awf-enclave": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp/awf-enclave"
    }
  }
}
EOF

  local output_file="$tmpdir/output.txt"
  local run_result=0
  PATH="$fake_bin:$PATH" CURL_MARKER="$marker" \
    bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:8080" "test-key" >"$output_file" 2>&1 || run_result=$?

  if [ $run_result -ne 0 ] && [ -e "$marker" ] && grep -q "failed to connect (required)" "$output_file"; then
    print_result "Unclassified enclave server remains required and fatal" "PASS"
  else
    print_result "Unclassified enclave server failure should remain fatal" "FAIL"
  fi

  rm -rf "$tmpdir"
}

# Test 19: Deferred names outside the compiler-owned allowlist remain required
test_arbitrary_deferred_server_name_is_fatal() {
  echo ""
  echo "Test 19: Arbitrary deferred server name remains required"

  local tmpdir
  tmpdir=$(mktemp -d)
  local config_file="$tmpdir/config.json"
  local marker="$tmpdir/curl-invocations"
  local fake_bin="$tmpdir/bin"
  create_failing_gateway_client "$fake_bin"

  cat > "$config_file" <<'EOF'
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp/github"
    }
  }
}
EOF

  local run_result=0
  PATH="$fake_bin:$PATH" CURL_MARKER="$marker" GH_AW_MCP_DEFERRED_SERVERS="github" \
    bash "$SCRIPT_PATH" "$config_file" "http://127.0.0.1:8080" "test-key" >/dev/null 2>&1 || run_result=$?

  if [ $run_result -ne 0 ] && [ -e "$marker" ]; then
    print_result "Arbitrary deferred server name remains required and probed" "PASS"
  else
    print_result "Only the compiler-owned awf-enclave server may be deferred" "FAIL"
  fi

  rm -rf "$tmpdir"
}

# Run all tests
echo "=== Testing check_mcp_servers.sh ==="
echo "Script: $SCRIPT_PATH"

test_script_syntax
test_argument_validation
test_config_not_found
test_invalid_json_config
test_empty_servers
test_null_servers
test_valid_http_server
test_server_without_url
test_mixed_servers
test_validation_functions_exist
test_optional_server_failure_degrades_to_warning
test_required_server_failure_is_fatal
test_all_optional_servers_fail_is_error
test_optional_server_from_env_degrades_to_warning
test_env_optional_list_does_not_affect_other_servers
test_deferred_enclave_server_is_not_probed
test_deferred_enclave_with_healthy_required_server
test_unclassified_enclave_server_is_fatal
test_arbitrary_deferred_server_name_is_fatal

# Print summary
echo ""
echo "=== Test Summary ==="
echo "Tests run: $TESTS_RUN"
echo -e "${GREEN}Tests passed: $TESTS_PASSED${NC}"
if [ $TESTS_FAILED -gt 0 ]; then
  echo -e "${RED}Tests failed: $TESTS_FAILED${NC}"
  exit 1
else
  echo -e "${GREEN}All tests passed!${NC}"
  exit 0
fi
