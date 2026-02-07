---
description: Daily test of GitHub remote MCP authentication with GitHub Actions token
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  discussions: read
engine:
  id: copilot
  model: gpt-5.1-codex-mini
tools:
  github:
    mode: remote
    toolsets: [repos, issues, discussions]
    allowed: [get_repository, list_issues, issue_read]
safe-outputs:
  create-discussion:
    title-prefix: "[auth-test] "
    category: "audits"
    max: 1
    close-older-discussions: true
timeout-minutes: 5
strict: true
imports:
  - shared/mood.md
---

# GitHub Remote MCP Authentication Test

You are an automated testing agent that verifies GitHub remote MCP server authentication with the GitHub Actions token.

## Your Task

Test that the GitHub remote MCP server can authenticate and access GitHub API with the GitHub Actions token.

### Test Procedure

1. **Verify Tool Availability**: FIRST, check that GitHub MCP tools are accessible
   - Try to use the `get_repository` tool to get basic info about ${{ github.repository }}
   - This is a simple, read-only operation that should work if MCP tools are properly loaded
   - **If this fails with errors like "tool not found", "unknown tool", or "capability not available":**
     - The MCP toolsets are NOT loaded in the runner
     - Report this using the `missing_tool` safe output with:
       - Tool: "GitHub MCP tools (list_issues, get_repository)"
       - Reason: "MCP toolsets unavailable in runner - tools not loaded"
       - Alternatives: "Check MCP configuration, verify remote mode is accessible, or use local mode fallback"
     - **Do NOT proceed to step 2** - the test has failed due to missing tools

2. **List Open Issues**: If `get_repository` succeeded, now test with `list_issues`
   - Use the GitHub MCP server to list 3 open issues in the repository ${{ github.repository }}
   - Use the `list_issues` tool
   - Filter for `state: OPEN`
   - Limit to 3 results
   - Extract issue numbers and titles

3. **Verify Authentication**: 
   - If the MCP tools successfully return data, authentication is working correctly
   - If the MCP tools fail with authentication errors (401, 403, "unauthorized", or "invalid session"), authentication has failed
   - **IMPORTANT**: Do NOT fall back to using `gh api` directly - this test must use the MCP server
   - Distinguish between "tool not available" errors (missing tools) vs "authentication failed" errors (token issues)

### Success Case

If the test succeeds (issues are retrieved successfully):
- Output a brief success message with:
  - ✅ Authentication test passed
  - Number of issues retrieved
  - Sample issue numbers and titles
- **Do NOT create a discussion** - the test passed

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
  - Agent cannot use `list_issues`, `get_repository`, or other GitHub MCP tools
  - Workflow cannot complete its authentication test
  - This is a configuration/infrastructure issue, not an authentication issue
  
  ### Expected Configuration
  ```yaml
  tools:
    github:
      mode: remote
      toolsets: [repos, issues, discussions]
      allowed: [get_repository, list_issues, issue_read]
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
- **Check for MCP tools FIRST**: Start with a simple `get_repository` call to verify tools are loaded
- **Include error details**: If authentication fails, include the exact error message from the MCP tool
- **Provide actionable remediation**: Include specific steps to resolve the detected issue type
- **Auto-cleanup**: Old test discussions will be automatically closed by the close-older-discussions setting

## Expected Output

**On Success**:
```
✅ GitHub Remote MCP Authentication Test PASSED

Successfully retrieved 3 open issues:
- #123: Issue title 1
- #124: Issue title 2
- #125: Issue title 3

Authentication with GitHub Actions token is working correctly.
```

**On Failure**:
Create a discussion with the error details as described above.
