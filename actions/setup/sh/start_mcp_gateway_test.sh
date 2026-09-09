#!/bin/bash
set +o histexpand

# Test script for start_mcp_gateway.sh
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_PATH="$SCRIPT_DIR/start_mcp_gateway.sh"

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

# Test 2: Required environment variables validation
test_env_var_validation() {
  echo ""
  echo "Test 2: Required environment variables validation"
  
  # Test missing MCP_GATEWAY_PORT
  if ! MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm --network host test-image" bash "$SCRIPT_PATH" 2>/dev/null; then
    print_result "Script rejects missing MCP_GATEWAY_PORT" "PASS"
  else
    print_result "Script should reject missing MCP_GATEWAY_PORT" "FAIL"
  fi
  
  # Test missing MCP_GATEWAY_DOMAIN
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm --network host test-image" bash "$SCRIPT_PATH" 2>/dev/null; then
    print_result "Script rejects missing MCP_GATEWAY_DOMAIN" "PASS"
  else
    print_result "Script should reject missing MCP_GATEWAY_DOMAIN" "FAIL"
  fi
  
  # Test missing MCP_GATEWAY_AGENT_ID
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm --network host test-image" bash "$SCRIPT_PATH" 2>/dev/null; then
    print_result "Script rejects missing MCP_GATEWAY_AGENT_ID" "PASS"
  else
    print_result "Script should reject missing MCP_GATEWAY_AGENT_ID" "FAIL"
  fi
  
  # Test missing MCP_GATEWAY_DOCKER_COMMAND
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" bash "$SCRIPT_PATH" 2>/dev/null; then
    print_result "Script rejects missing MCP_GATEWAY_DOCKER_COMMAND" "PASS"
  else
    print_result "Script should reject missing MCP_GATEWAY_DOCKER_COMMAND" "FAIL"
  fi
}

# Test 3: Configuration file not found
test_config_not_found() {
  echo ""
  echo "Test 3: Configuration file not found"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_home="$tmpdir/home"
  mkdir -p "$fake_home/.copilot"
  
  # Create a modified script that uses our fake home
  local test_script="$tmpdir/test_script.sh"
  sed "s|/home/runner|$fake_home|g" "$SCRIPT_PATH" > "$test_script"
  
  # Test without config file
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm --network host test-image" bash "$test_script" 2>/dev/null; then
    print_result "Script rejects non-existent config file" "PASS"
  else
    print_result "Script should reject non-existent config file" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 4: Configuration file is invalid JSON
test_invalid_json_config() {
  echo ""
  echo "Test 4: Configuration file is invalid JSON"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_home="$tmpdir/home"
  mkdir -p "$fake_home/.copilot"
  
  # Create invalid JSON config
  echo "{ invalid json" > "$fake_home/.copilot/mcp-config.json"
  
  # Create a modified script that uses our fake home
  local test_script="$tmpdir/test_script.sh"
  sed "s|/home/runner|$fake_home|g" "$SCRIPT_PATH" > "$test_script"
  
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm --network host test-image" bash "$test_script" 2>/dev/null; then
    print_result "Script rejects invalid JSON config" "PASS"
  else
    print_result "Script should reject invalid JSON config" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 5: Container missing 'docker run' prefix
test_container_missing_docker_run() {
  echo ""
  echo "Test 5: Container missing 'docker run' prefix"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_home="$tmpdir/home"
  mkdir -p "$fake_home/.copilot"
  
  # Create valid JSON config with required gateway section
  echo '{"mcpServers":{},"gateway":{"port":8080,"domain":"localhost","agentId":"test-key"}}' > "$fake_home/.copilot/mcp-config.json"
  
  # Create a modified script that uses our fake home
  local test_script="$tmpdir/test_script.sh"
  sed "s|/home/runner|$fake_home|g" "$SCRIPT_PATH" > "$test_script"
  
  # Test with container that doesn't start with "docker run"
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="test-image" bash "$test_script" 2>/dev/null; then
    print_result "Script rejects container without 'docker run'" "PASS"
  else
    print_result "Script should reject container without 'docker run'" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 6: Container missing required -i flag
test_container_missing_i_flag() {
  echo ""
  echo "Test 6: Container missing required -i flag"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_home="$tmpdir/home"
  mkdir -p "$fake_home/.copilot"
  
  # Create valid JSON config with required gateway section
  echo '{"mcpServers":{},"gateway":{"port":8080,"domain":"localhost","agentId":"test-key"}}' > "$fake_home/.copilot/mcp-config.json"
  
  # Create a modified script that uses our fake home
  local test_script="$tmpdir/test_script.sh"
  sed "s|/home/runner|$fake_home|g" "$SCRIPT_PATH" > "$test_script"
  
  # Test with container missing -i flag
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="docker run --rm --network host test-image" bash "$test_script" 2>/dev/null; then
    print_result "Script rejects container without -i flag" "PASS"
  else
    print_result "Script should reject container without -i flag" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 7: Container missing required --rm flag
test_container_missing_rm_flag() {
  echo ""
  echo "Test 7: Container missing required --rm flag"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_home="$tmpdir/home"
  mkdir -p "$fake_home/.copilot"
  
  # Create valid JSON config with required gateway section
  echo '{"mcpServers":{},"gateway":{"port":8080,"domain":"localhost","agentId":"test-key"}}' > "$fake_home/.copilot/mcp-config.json"
  
  # Create a modified script that uses our fake home
  local test_script="$tmpdir/test_script.sh"
  sed "s|/home/runner|$fake_home|g" "$SCRIPT_PATH" > "$test_script"
  
  # Test with container missing --rm flag
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="docker run -i --network host test-image" bash "$test_script" 2>/dev/null; then
    print_result "Script rejects container without --rm flag" "PASS"
  else
    print_result "Script should reject container without --rm flag" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 8: Container missing required --network host flag
test_container_missing_network_flag() {
  echo ""
  echo "Test 8: Container missing required --network host flag"
  
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_home="$tmpdir/home"
  mkdir -p "$fake_home/.copilot"
  
  # Create valid JSON config with required gateway section
  echo '{"mcpServers":{},"gateway":{"port":8080,"domain":"localhost","agentId":"test-key"}}' > "$fake_home/.copilot/mcp-config.json"
  
  # Create a modified script that uses our fake home
  local test_script="$tmpdir/test_script.sh"
  sed "s|/home/runner|$fake_home|g" "$SCRIPT_PATH" > "$test_script"
  
  # Test with container missing --network host flag
  if ! MCP_GATEWAY_PORT="8080" MCP_GATEWAY_DOMAIN="localhost" MCP_GATEWAY_AGENT_ID="test-key" MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm test-image" bash "$test_script" 2>/dev/null; then
    print_result "Script rejects container without --network host flag" "PASS"
  else
    print_result "Script should reject container without --network host flag" "FAIL"
  fi
  
  rm -rf "$tmpdir"
}

# Test 9: Validation functions exist
test_validation_functions_exist() {
  echo ""
  echo "Test 9: Verify validation logic exists"
  
  # Check for stdin configuration handling
  if grep -q "MCP configuration received" "$SCRIPT_PATH"; then
    print_result "Configuration input handling exists" "PASS"
  else
    print_result "Configuration input handling missing" "FAIL"
  fi

  if grep -q 'gateway-api-key=${MCP_GATEWAY_AGENT_ID}' "$SCRIPT_PATH"; then
    print_result "Gateway API key output handoff exists" "PASS"
  else
    print_result "Gateway API key output handoff missing" "FAIL"
  fi
  
  # Check for JSON validation
  if grep -q "not valid JSON" "$SCRIPT_PATH"; then
    print_result "JSON validation exists" "PASS"
  else
    print_result "JSON validation missing" "FAIL"
  fi
  
  # Check for container syntax validation
  if grep -q "incorrect syntax" "$SCRIPT_PATH"; then
    print_result "Container syntax validation exists" "PASS"
  else
    print_result "Container syntax validation missing" "FAIL"
  fi
  
  # Check for -i flag validation
  if grep -q "must include -i flag" "$SCRIPT_PATH"; then
    print_result "-i flag validation exists" "PASS"
  else
    print_result "-i flag validation missing" "FAIL"
  fi
  
  # Check for --rm flag validation
  if grep -q "must include --rm flag" "$SCRIPT_PATH"; then
    print_result "--rm flag validation exists" "PASS"
  else
    print_result "--rm flag validation missing" "FAIL"
  fi
  
  # Check for --network validation
  if grep -q "must include --network flag" "$SCRIPT_PATH"; then
    print_result "--network flag validation exists" "PASS"
  else
    print_result "--network flag validation missing" "FAIL"
  fi

  # Check for health check retry/backoff logic
  if grep -q "RETRY_COUNT -eq 1" "$SCRIPT_PATH" &&
    grep -q "RETRY_COUNT -eq 2" "$SCRIPT_PATH" &&
    grep -q "elif \[ \$RETRY_COUNT -eq 2 \]" "$SCRIPT_PATH" &&
    grep -q "else" "$SCRIPT_PATH" &&
    grep -q "RETRY_DELAY=\"0.25\"" "$SCRIPT_PATH" &&
    grep -q "RETRY_DELAY=\"0.5\"" "$SCRIPT_PATH" &&
    grep -q "RETRY_DELAY=\"1\"" "$SCRIPT_PATH" &&
    grep -q "attempt 3+ -> 1s" "$SCRIPT_PATH" &&
    grep -q "sleep \"\$RETRY_DELAY\"" "$SCRIPT_PATH"; then
    print_result "Health check exponential backoff configuration exists" "PASS"
  else
    print_result "Health check exponential backoff configuration missing" "FAIL"
  fi

  # Check for stale container cleanup before gateway start
  if grep -q "awmg-mcpg" "$SCRIPT_PATH" && grep -q "docker rm -f awmg-mcpg" "$SCRIPT_PATH"; then
    print_result "Stale awmg-mcpg container cleanup exists" "PASS"
  else
    print_result "Stale awmg-mcpg container cleanup missing" "FAIL"
  fi
}

test_gateway_startup_diagnostics() {
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_bin="$tmpdir/bin"
  mkdir -p "$fake_bin"
  cat > "$fake_bin/docker" << 'EOF'
#!/usr/bin/env bash
  echo "simulated gateway startup failure: API_TOKEN=redact-me {\"agentId\":\"redact-me\"}" >&2
exit 42
EOF
  chmod +x "$fake_bin/docker"

  rm -rf /tmp/gh-aw/mcp-config /tmp/gh-aw/mcp-gateway-started
  local output
  set +e
  output=$(printf '%s\n' '{"mcpServers":{},"gateway":{"port":8080,"domain":"localhost","agentId":"redact-me"}}' | \
    PATH="$fake_bin:$PATH" \
    MCP_GATEWAY_PORT="8080" \
    MCP_GATEWAY_DOMAIN="localhost" \
    MCP_GATEWAY_AGENT_ID="redact-me" \
    MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm --network host test-image" \
    bash "$SCRIPT_PATH" 2>&1)
  local exit_code=$?
  set -e

  if [[ "$exit_code" -ne 0 ]] &&
    grep -q "Gateway startup diagnostics:" <<< "$output" &&
    grep -q "::group::Gateway stderr" <<< "$output" &&
    grep -q "::endgroup::" <<< "$output" &&
    grep -q "simulated gateway startup failure" <<< "$output" &&
    grep -q "API_TOKEN=\[REDACTED\]" <<< "$output" &&
    grep -q "\"agentId\":\"\[REDACTED\]\"" <<< "$output" &&
    ! grep -q "redact-me" <<< "$output" &&
    [[ ! -e /tmp/gh-aw/mcp-config/gateway-stderr.log ]] &&
    ! compgen -G "/tmp/gh-aw-mcp-gateway-stderr.*" > /dev/null; then
    print_result "Gateway startup failure logs redacted stderr without artifacts" "PASS"
  else
    print_result "Gateway startup failure logs redacted stderr without artifacts" "FAIL"
  fi
  rm -rf "$tmpdir" /tmp/gh-aw/mcp-config /tmp/gh-aw/mcp-gateway-started
}

test_gateway_agent_identifier_validation() {
  local tmpdir
  tmpdir=$(mktemp -d)
  local fake_bin="$tmpdir/bin"
  mkdir -p "$fake_bin"
  cat > "$fake_bin/docker" << 'EOF'
#!/usr/bin/env bash
exit 42
EOF
  chmod +x "$fake_bin/docker"

  assert_agent_identifier_config() {
    local test_name="$1"
    local gateway_config="$2"
    local expected_message="$3"
    local output
    set +e
    output=$(printf '%s\n' "{\"mcpServers\":{},\"gateway\":{\"port\":8080,\"domain\":\"localhost\",${gateway_config}}}" | \
      PATH="$fake_bin:$PATH" \
      MCP_GATEWAY_PORT="8080" \
      MCP_GATEWAY_DOMAIN="localhost" \
      MCP_GATEWAY_AGENT_ID="test-key" \
      MCP_GATEWAY_DOCKER_COMMAND="docker run -i --rm --network host test-image" \
      bash "$SCRIPT_PATH" 2>&1)
    set -e

    if grep -Fq "$expected_message" <<< "$output"; then
      print_result "$test_name" "PASS"
    else
      print_result "$test_name" "FAIL"
    fi
  }

  assert_agent_identifier_config "Accepts a singular agent ID" '"agentId":"agent-1"' "Configuration validated successfully"
  assert_agent_identifier_config "Accepts one plural agent ID" '"agentIds":["agent-1"]' "Configuration validated successfully"
  assert_agent_identifier_config "Accepts multiple plural agent IDs" '"agentIds":["agent-1","agent-2"]' "Configuration validated successfully"
  assert_agent_identifier_config "Rejects an empty plural agent ID list" '"agentIds":[]' "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"
  assert_agent_identifier_config "Rejects a non-array plural agent ID" '"agentIds":"agent-1"' "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"
  assert_agent_identifier_config "Rejects an empty ID in the plural list" '"agentIds":["agent-1",""]' "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"
  assert_agent_identifier_config "Rejects a non-string ID in the plural list" '"agentIds":["agent-1",2]' "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"
  assert_agent_identifier_config "Rejects both identifier forms" '"agentId":"agent-1","agentIds":["agent-1"]' "ERROR: Gateway configuration must specify exactly one of 'agentId' or 'agentIds'"
  assert_agent_identifier_config "Rejects neither identifier form" '"other":"value"' "ERROR: Gateway configuration must specify exactly one of 'agentId' or 'agentIds'"
  assert_agent_identifier_config "Rejects an empty singular agent ID" '"agentId":""' "ERROR: Gateway 'agentId' must be a non-empty string"
  assert_agent_identifier_config "Rejects a malformed singular agent ID" '"agentId":["agent-1"]' "ERROR: Gateway 'agentId' must be a non-empty string"

  rm -rf "$tmpdir" /tmp/gh-aw/mcp-config /tmp/gh-aw/mcp-gateway-started
}

# Run all tests
echo "=== Testing start_mcp_gateway.sh ==="
echo "Script: $SCRIPT_PATH"

test_script_syntax
test_env_var_validation
test_config_not_found
test_invalid_json_config
test_container_missing_docker_run
test_container_missing_i_flag
test_container_missing_rm_flag
test_container_missing_network_flag
test_validation_functions_exist
test_gateway_startup_diagnostics
test_gateway_agent_identifier_validation

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
