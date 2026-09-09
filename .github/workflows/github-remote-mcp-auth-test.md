---
private: true
emoji: "🧪"
description: Daily test of GitHub remote MCP authentication with GitHub Actions token
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  discussions: read


  copilot-requests: write
model: copilot/gpt-5.3-codex
engine:
  id: codex
  model-provider: github
tools:
  cli-proxy: true
  github:
    mode: remote
    toolsets: [repos, issues, discussions]
    allowed: [list_issues, issue_read]
timeout-minutes: 5
strict: true
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[auth-test] "
      expires: 1d


  - shared/otlp.md
jobs:
  raw_mcp_canary:
    needs: agent
    if: always() && !cancelled()
    runs-on: ubuntu-latest
    permissions:
      contents: read
      issues: read
      discussions: read
    steps:
      - name: Verify raw GitHub remote MCP handshake
        env:
          GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail

          if [ -z "${GITHUB_MCP_SERVER_TOKEN:-}" ]; then
            echo "No GitHub MCP token is available."
            exit 1
          fi
          echo "::add-mask::${GITHUB_MCP_SERVER_TOKEN}"

          server_url="https://api.githubcopilot.com/mcp/"
          initialize_headers_file="$(mktemp)"
          response_headers_file="$(mktemp)"
          body_file="$(mktemp)"
          json_file="$(mktemp)"
          error_file="$(mktemp)"
          trap 'rm -f "$initialize_headers_file" "$response_headers_file" "$body_file" "$json_file" "$error_file"' EXIT

          {
            echo "## Raw GitHub remote MCP canary"
            echo
          } >> "$GITHUB_STEP_SUMMARY"

          mcp_post() {
            local payload="$1"
            local response_headers_file="$2"
            shift 2
            local http_code
            # runner-guard:ignore RGS-012 -- pinned request to the official GitHub remote MCP endpoint (api.githubcopilot.com); verifies the token authenticates and receives the response, no data is exfiltrated to an attacker-controlled host.
            if ! http_code="$(curl -sS -D "$response_headers_file" -o "$body_file" -w "%{http_code}" --max-time 20 \
              -X POST "$server_url" \
              --oauth2-bearer "$GITHUB_MCP_SERVER_TOKEN" \
              -H "Content-Type: application/json" \
              -H "Accept: application/json, text/event-stream" \
              -H "X-MCP-Readonly: true" \
              -H "X-MCP-Toolsets: repos,issues,discussions" \
              "$@" \
              -d "$payload" 2>"$error_file")"; then
              http_code="000"
            fi
            printf '%s' "$http_code"
          }

          read_json_response() {
            if jq -e . "$body_file" >/dev/null 2>&1; then
              cp "$body_file" "$json_file"
            else
              awk '/^data:/ { sub(/^data: ?/, ""); print }' "$body_file" | tail -n 1 > "$json_file"
            fi
          }

          log_info() {
            echo "::notice title=Raw GitHub remote MCP canary::$1"
          }

          assert_success_response() {
            local label="$1"
            local http_code="$2"
            if [ "$http_code" != "200" ]; then
              echo "$label failed with HTTP $http_code."
              echo "- ❌ $label: HTTP $http_code" >> "$GITHUB_STEP_SUMMARY"
              if [ -s "$body_file" ]; then
                echo "response body: $(head -c 500 "$body_file")"
              fi
              if [ -s "$error_file" ]; then
                echo "curl error: $(head -c 200 "$error_file")"
              fi
              exit 1
            fi
            echo "$label responded with HTTP $http_code."

            read_json_response
            if ! jq -e . "$json_file" >/dev/null 2>&1; then
              echo "$label did not return a JSON response."
              echo "- ❌ $label: invalid JSON response" >> "$GITHUB_STEP_SUMMARY"
              exit 1
            fi
            if jq -e '.error' "$json_file" >/dev/null 2>&1; then
              error_message="$(jq -r '.error.message // "unknown JSON-RPC error"' "$json_file")"
              echo "$label returned JSON-RPC error: $error_message"
              echo "- ❌ $label: JSON-RPC error \`$error_message\`" >> "$GITHUB_STEP_SUMMARY"
              exit 1
            fi
          }

          initialize_code="$(mcp_post '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"clientInfo":{"name":"gh-aw-raw-mcp-canary","version":"1.0.0"},"protocolVersion":"2024-11-05"}}' "$initialize_headers_file")"
          assert_success_response "MCP initialize" "$initialize_code"
          protocol_version="$(jq -r '.result.protocolVersion // "unknown"' "$json_file")"
          server_info="$(jq -c '.result.serverInfo // {}' "$json_file")"
          server_capabilities="$(jq -c '.result.capabilities // {}' "$json_file")"
          log_info "initialize: protocol $protocol_version, server $server_info"
          echo "server capabilities: $server_capabilities"
          echo "- ✅ MCP initialize: protocol \`$protocol_version\`, server \`$server_info\`" >> "$GITHUB_STEP_SUMMARY"

          session_id="$(awk 'BEGIN{IGNORECASE=1} /^Mcp-Session-Id:/ { gsub(/\r/, "", $2); print $2; exit }' "$initialize_headers_file")"
          session_args=()
          if [ -n "$session_id" ]; then
            echo "::add-mask::$session_id"
            session_args=(-H "Mcp-Session-Id: $session_id")
            log_info "initialize: received an Mcp-Session-Id header"
          else
            log_info "initialize: no Mcp-Session-Id header returned"
          fi

          initialized_code="$(mcp_post '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' "$response_headers_file" "${session_args[@]}")"
          if [[ ! "$initialized_code" =~ ^20[024]$ ]]; then
            echo "MCP notifications/initialized failed with HTTP $initialized_code."
            echo "- ❌ MCP notifications/initialized: HTTP $initialized_code" >> "$GITHUB_STEP_SUMMARY"
            exit 1
          fi
          log_info "notifications/initialized: HTTP $initialized_code"
          echo "- ✅ MCP notifications/initialized: HTTP $initialized_code" >> "$GITHUB_STEP_SUMMARY"

          ping_code="$(mcp_post '{"jsonrpc":"2.0","id":2,"method":"ping"}' "$response_headers_file" "${session_args[@]}")"
          assert_success_response "MCP ping" "$ping_code"
          log_info "ping: succeeded"
          echo "- ✅ MCP ping" >> "$GITHUB_STEP_SUMMARY"

          tools_code="$(mcp_post '{"jsonrpc":"2.0","id":3,"method":"tools/list"}' "$response_headers_file" "${session_args[@]}")"
          assert_success_response "MCP tools/list" "$tools_code"

          tool_count="$(jq -r 'if (.result.tools | type) == "array" then (.result.tools | length) else 0 end' "$json_file")"
          tool_names="$(jq -r '[.result.tools[]?.name] | sort | join(", ")' "$json_file")"
          next_cursor="$(jq -r '.result.nextCursor // ""' "$json_file")"
          log_info "tools/list returned $tool_count tools"
          echo "tools: $tool_names"
          if [ -n "$next_cursor" ]; then
            log_info "tools/list is paginated: more tools are available beyond this page"
          fi
          {
            echo "<details><summary>MCP tools/list catalog ($tool_count tools)</summary>"
            echo
            jq -r '.result.tools[]? | "- `\(.name)`: \(.description // "" | split("\n")[0])"' "$json_file"
            echo
            echo "</details>"
            echo
          } >> "$GITHUB_STEP_SUMMARY"

          if [ "$tool_count" -eq 0 ]; then
            echo "MCP tools/list did not return any tools."
            echo "- ❌ MCP tools/list: empty tool catalog" >> "$GITHUB_STEP_SUMMARY"
            exit 1
          fi
          if ! jq -e '.result.tools[] | select(.name == "list_issues")' "$json_file" >/dev/null; then
            echo 'MCP tools/list did not return the list_issues tool.'
            echo "available tools: $tool_names"
            echo "- ❌ MCP tools/list: \`list_issues\` is unavailable (available tools: $tool_names)" >> "$GITHUB_STEP_SUMMARY"
            exit 1
          fi
          log_info "tools/list: \`list_issues\` is available"
          echo "- ✅ MCP tools/list: $tool_count tools, including \`list_issues\`" >> "$GITHUB_STEP_SUMMARY"

          repository_owner="${GITHUB_REPOSITORY%%/*}"
          repository_name="${GITHUB_REPOSITORY#*/}"
          call_payload="$(jq -nc \
            --arg owner "$repository_owner" \
            --arg repo "$repository_name" \
            '{jsonrpc:"2.0",id:4,method:"tools/call",params:{name:"list_issues",arguments:{owner:$owner,repo:$repo,state:"OPEN",perPage:1}}}')"
          call_code="$(mcp_post "$call_payload" "$response_headers_file" "${session_args[@]}")"
          assert_success_response "MCP list_issues" "$call_code"
          if jq -e '.result.isError == true' "$json_file" >/dev/null; then
            tool_error="$(jq -r '([.result.content[]?.text] | join(" "))[0:200]' "$json_file")"
            echo "MCP list_issues returned a tool error: $tool_error"
            echo "- ❌ MCP list_issues: tool error \`$tool_error\`" >> "$GITHUB_STEP_SUMMARY"
            exit 1
          fi
          log_info "list_issues: retrieved open issues from $GITHUB_REPOSITORY"
          echo "- ✅ MCP list_issues: retrieved open issues from \`$GITHUB_REPOSITORY\`" >> "$GITHUB_STEP_SUMMARY"
          echo "Raw GitHub remote MCP handshake succeeded with $tool_count tools available."
features:
  gh-aw-detection: true
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# GitHub Remote MCP Authentication Test

You are an automated testing agent that verifies GitHub remote MCP server authentication with the GitHub Actions token.

## Your Task

Test that the GitHub remote MCP server can authenticate and access GitHub API with the GitHub Actions token.

### Test Procedure

1. **Verify Tool Availability**: FIRST, check that the `list_issues` GitHub MCP tool is accessible
   - Try to use the `list_issues` tool to list open issues in ${{ github.repository }}
   - This is a simple, read-only operation that should work if MCP tools are properly loaded
   - **If this fails with errors like "tool not found", "unknown tool", or "capability not available":**
     - The MCP toolsets are NOT loaded in the runner
     - Report this using the `missing_tool` safe output with:
       - Tool: "GitHub MCP tool (list_issues)"
       - Reason: "MCP toolsets unavailable in runner - tools not loaded"
       - Alternatives: "Check MCP configuration, verify remote mode is accessible, or use local mode fallback"
     - The test has failed due to missing tools

2. **Verify Authentication**:
   - If the MCP tools successfully return data, authentication is working correctly
   - If the MCP tools fail with authentication errors (401, 403, "unauthorized", or "invalid session"), authentication has failed
   - **IMPORTANT**: Do NOT fall back to using `gh api` directly - this test must use the MCP server
   - Distinguish between "tool not available" errors (missing tools) vs "authentication failed" errors (token issues)

### Success Case

If the test succeeds (issues are retrieved successfully):
- **Call `noop`** with the success message — do NOT create a discussion since the test passed:
  ```json
  {"noop": {"message": "Authentication test passed: successfully retrieved [N] open issues via GitHub remote MCP server"}}
  ```
- Include in the noop message:
  - ✅ Authentication test passed
  - Number of issues retrieved
  - Sample issue numbers and titles

### Failure Case

If the test fails, create a discussion using safe-outputs based on the failure type:

**For Missing Tools (tool not found/not loaded):**
- Use the `missing_tool` safe output first, then create a discussion
- **Title**: "GitHub Remote MCP Tools Not Available"
- **Body**:
  ```markdown
  ## ❌ MCP Tool Availability Test Failed
  
  The GitHub remote MCP toolsets are not available in the runner environment.
  
  ### Error Details
  [Include the specific error message - likely "tool not found" or "unknown tool"]
  
  ### Root Cause
  **MCP Tools Not Loaded**: The GitHub MCP toolsets (repos, issues, discussions) are not being loaded in the runner. This prevents the agent from accessing GitHub data through MCP.
  
  ### Impact
  - Agent cannot use `list_issues` or other GitHub MCP tools
  - Workflow cannot complete its authentication test
  - This is a configuration/infrastructure issue, not an authentication issue
  
  ### Expected Configuration
  ```yaml
  tools:
    github:
      mode: remote
      toolsets: [repos, issues, discussions]
      allowed: [list_issues, issue_read]
  ```
  
  ### Remediation Steps
  1. **Verify MCP server initialization**: Check if GitHub MCP server is starting properly
  2. **Check remote mode availability**: Verify https://api.githubcopilot.com/mcp/ is accessible
  3. **Review runner logs**: Look for MCP server startup errors or tool loading failures
  4. **Consider local mode fallback**: Add fallback configuration to use `mode: local` if remote fails
  5. **Test manually**: Run `gh aw mcp inspect github-remote-mcp-auth-test` locally to verify tool configuration
  
  ### Test Configuration
  - Repository: ${{ github.repository }}
  - Workflow: ${{ github.workflow }}
  - Run ID: ${{ github.run_id }}
  - Run URL: https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Time: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
  ```

**For Authentication Errors (401, 403, unauthorized):**
- **Title**: "GitHub Remote MCP Authentication Test Failed"
- **Body**:
  ```markdown
  ## ❌ Authentication Test Failed
  
  The daily GitHub remote MCP authentication test has failed.
  
  ### Error Details
  [Include the specific error message from the MCP tool]
  
  ### Root Cause Analysis
  [Determine if the issue is:
  - Token authentication issue (401, 403 errors)
  - Invalid or expired token
  - Insufficient token permissions
  - MCP server connection failure (invalid session, 400 error)
  - Other issue]
  
  ### Expected Behavior
  The GitHub remote MCP server should authenticate with the GitHub Actions token and successfully list open issues using MCP tools.
  
  ### Actual Behavior
  [Describe what happened - authentication error, timeout, connection refused, etc.]
  
  ### Test Configuration
  - Repository: ${{ github.repository }}
  - Workflow: ${{ github.workflow }}
  - Run ID: ${{ github.run_id }}
  - Run URL: https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Time: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
  
  ### Next Steps
  1. Review workflow logs at the run URL above for detailed error information
  2. Check if GitHub remote MCP server (https://api.githubcopilot.com/mcp/) is available
  3. Verify token is compatible with GitHub Copilot MCP server and has required scopes
  4. Check token expiration and validity
  5. Review recent GitHub Copilot service status
  ```

## Guidelines

- **Be concise**: Keep output brief and focused
- **Test quickly**: This should complete in under 1 minute
- **Only create discussion on failure**: Don't create discussions when the test passes
- **Do NOT use gh api directly**: This test must verify MCP server authentication, not GitHub CLI
- **Distinguish failure types**: 
  - Missing tools = Configuration/infrastructure issue
  - Auth errors = Token/permissions issue
- **Use missing_tool safe output**: When tools aren't available, report it properly before creating a discussion
- **Check for MCP tools FIRST**: Start with a simple `list_issues` call to verify tools are loaded
- **Include error details**: If authentication fails, include the exact error message from the MCP tool
- **Provide actionable remediation**: Include specific steps to resolve the detected issue type
- **Auto-cleanup**: Old test discussions will be automatically closed by the close-older-discussions setting

## Expected Output

**On Success**:
Call `noop` with a message like:
```
Authentication test passed: successfully retrieved 3 open issues via GitHub remote MCP server (#123 Issue title 1, #124 Issue title 2, #125 Issue title 3)
```

**On Failure**:
Create a discussion with the error details as described above.