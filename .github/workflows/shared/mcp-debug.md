---
mcp-servers:
  gh-aw:
    type: http
    url: http://localhost:8765
    allowed:
      - mcp-inspect
safe-outputs:
  jobs:
    report-diagnostics-to-pull-request:
      description: "Post MCP diagnostic findings as a comment on the pull request associated with the triggering branch"
      runs-on: ubuntu-latest
      output: "Diagnostic report posted to pull request successfully!"
      inputs:
        message:
          description: "The diagnostic message to post as a PR comment"
          required: true
          type: string
      permissions:
        contents: read
        pull-requests: write
      steps:
        - name: Checkout repository
          uses: actions/checkout@v7.0.1
        - name: Post diagnostic report to pull request
          uses: actions/github-script@v9.0.0
          with:
            script: |
              const fs = require('fs');
              const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === 'true';
              const outputContent = process.env.GH_AW_AGENT_OUTPUT;
              
              // Read and parse agent output
              if (!outputContent) {
                core.info('No GH_AW_AGENT_OUTPUT environment variable found');
                return;
              }
              
              let agentOutputData;
              try {
                const fileContent = fs.readFileSync(outputContent, 'utf8');
                agentOutputData = JSON.parse(fileContent);
              } catch (error) {
                core.setFailed(`Error reading or parsing agent output: ${error instanceof Error ? error.message : String(error)}`);
                return;
              }
              
              if (!agentOutputData.items || !Array.isArray(agentOutputData.items)) {
                core.info('No valid items found in agent output');
                return;
              }
              
              // Filter for report_diagnostics_to_pull_request items
              const diagnosticItems = agentOutputData.items.filter(item => item.type === 'report_diagnostics_to_pull_request');
              
              if (diagnosticItems.length === 0) {
                core.info('No report_diagnostics_to_pull_request items found in agent output');
                return;
              }
              
              core.info(`Found ${diagnosticItems.length} report_diagnostics_to_pull_request item(s)`);
              
              // Get the current branch
              const ref = context.ref;
              const branch = ref.replace('refs/heads/', '');
              core.info(`Current branch: ${branch}`);
              
              // Find pull requests associated with this branch
              let pullRequests;
              try {
                const { data } = await github.rest.pulls.list({
                  owner: context.repo.owner,
                  repo: context.repo.repo,
                  head: `${context.repo.owner}:${branch}`,
                  state: 'open'
                });
                pullRequests = data;
              } catch (error) {
                core.setFailed(`Failed to list pull requests: ${error instanceof Error ? error.message : String(error)}`);
                return;
              }
              
              if (pullRequests.length === 0) {
                core.warning(`No open pull requests found for branch: ${branch}`);
                core.info('Diagnostic report cannot be posted without an associated pull request');
                return;
              }
              
              const pullRequest = pullRequests[0];
              const prNumber = pullRequest.number;
              core.info(`Found pull request #${prNumber} for branch ${branch}`);
              
              // Process each diagnostic item
              for (let i = 0; i < diagnosticItems.length; i++) {
                const item = diagnosticItems[i];
                const message = item.message;
                
                if (!message) {
                  core.warning(`Item ${i + 1}: Missing message field, skipping`);
                  continue;
                }
                
                if (isStaged) {
                  let summaryContent = "## 🎭 Staged Mode: Diagnostic Report Preview\n\n";
                  summaryContent += "The following diagnostic report would be posted to the pull request if staged mode was disabled:\n\n";
                  summaryContent += `**Pull Request:** #${prNumber}\n`;
                  summaryContent += `**Branch:** ${branch}\n\n`;
                  summaryContent += `**Diagnostic Message:**\n\n${message}\n\n`;
                  await core.summary.addRaw(summaryContent).write();
                  core.info("📝 Diagnostic report preview written to step summary");
                  continue;
                }
                
                core.info(`Posting diagnostic report ${i + 1}/${diagnosticItems.length} to PR #${prNumber}`);
                
                try {
                  const { data: comment } = await github.rest.issues.createComment({
                    owner: context.repo.owner,
                    repo: context.repo.repo,
                    issue_number: prNumber,
                    body: message
                  });
                  
                  core.info(`✅ Diagnostic report ${i + 1} posted successfully`);
                  core.info(`Comment ID: ${comment.id}`);
                  core.info(`Comment URL: ${comment.html_url}`);
                } catch (error) {
                  core.setFailed(`Failed to post diagnostic report ${i + 1}: ${error instanceof Error ? error.message : String(error)}`);
                  return;
                }
              }
steps:
  - name: Setup Go
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
  - name: Install dependencies
    run: make deps-dev
  - name: Install binary as 'gh-aw'
    run: make build
  - name: Start MCP server
    run: |
      set -e
      ./gh-aw mcp-server --cmd ./gh-aw --port 8765 &
      MCP_PID=$!
      
      # Wait a moment for server to start
      sleep 2
      
      # Check if server is still running
      if ! kill -0 $MCP_PID 2>/dev/null; then
        echo "MCP server failed to start"
        exit 1
      fi
      
      echo "MCP server started successfully with PID $MCP_PID"
    env:
      GH_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
---

# MCP Server Debugging Assistant

You are an expert MCP (Model Context Protocol) server debugger. Your role is to diagnose and troubleshoot MCP server configuration and runtime issues.

## Your Tools

**mcp-inspect**: Use this tool to inspect MCP server configurations and diagnose connectivity issues. It can list workflows with MCP servers, inspect specific servers, show available tools and their status, display tool schemas and parameters, and verify HTTP endpoint connectivity.

## MCP Server Logs

When debugging, always check MCP server logs located at `/tmp/gh-aw/mcp-logs/<server-name>/`:
- `server.log` - Main server startup and runtime logs
- `curl-test.log` - HTTP endpoint connectivity tests (for HTTP servers)

These logs contain crucial diagnostic information about server startup failures, dependency issues, port binding problems, configuration errors, and runtime exceptions.

## Debugging Workflow

When you encounter an MCP server issue, follow this systematic approach:

### 1. Identify the Problem
Document what operation failed, which MCP server is affected, what error messages you received, and what you were trying to accomplish.

### 2. Read the Logs
Check the server logs to understand what went wrong:
```bash
cat /tmp/gh-aw/mcp-logs/<server-name>/server.log
cat /tmp/gh-aw/mcp-logs/<server-name>/curl-test.log  # For HTTP servers
```

Look for Python/Node.js import errors, port binding errors, configuration validation errors, and stack traces.

### 3. Use mcp-inspect
For HTTP-based MCP servers, use the mcp-inspect tool to diagnose connectivity and tool availability:
- Inspect all MCP servers: `mcp-inspect` with `workflow_file` parameter
- Inspect specific server: `mcp-inspect` with `workflow_file` and `server` parameters
- Get tool details: `mcp-inspect` with `workflow_file`, `server`, and `tool` parameters

### 4. Analyze Root Cause
Determine:
- What specifically failed?
- Which tools or operations are affected?
- Can you proceed without this server?
- What changes are needed to resolve the issue?

## Common Issues and Solutions

**Server Failed to Start:**
- Check server.log for startup errors
- Look for missing dependencies (ModuleNotFoundError, ImportError)
- Verify port is not in use: `netstat -tln | grep <port>`
- Check Python/Node version compatibility

**HTTP Endpoint Not Responding:**
- Check if server process is running: `ps aux | grep <server-name>`
- Review curl-test.log for connection details
- Verify server is listening on the correct port
- Check HOST and PORT environment variables

**Tools Not Available:**
- Use mcp-inspect to list available tools
- Compare with workflow's allowed list
- Check tool registration in server code
- Verify server initialized successfully

## Reporting Your Findings

**CRITICAL**: You MUST use the `report_diagnostics_to_pull_request` safe-output to report your diagnostic findings. Do not just describe the issue - create a structured diagnostic report using the safe-output.

Your diagnostic report must include:
- **Issue Description**: What specifically failed
- **Root Cause Analysis**: Why it failed based on log evidence
- **Evidence**: Specific error messages and log excerpts
- **Impact Assessment**: Which tools/operations are affected
- **Recommended Fix**: Detailed steps to resolve the issue
- **Workarounds**: Alternative approaches if applicable

**Output Format:**
Use the `report_diagnostics_to_pull_request` safe-output type with this structure:

```json
{
  "items": [
    {
      "type": "report_diagnostics_to_pull_request",
      "message": "## MCP Diagnostic Report\n\n**Server**: <server-name>\n\n**Issue**: <brief description>\n\n**Root Cause**: <analysis>\n\n**Evidence**:\n```\n<log excerpts>\n```\n\n**Impact**: <affected tools/operations>\n\n**Recommended Fix**:\n1. <step 1>\n2. <step 2>\n\n**Workaround**: <if applicable>"
    }
  ]
}
```

The diagnostic report will be automatically posted as a comment on the pull request associated with the current branch.

## Example Diagnostic Session

```
# Server failed to start - investigate
Read /tmp/gh-aw/mcp-logs/<server-name>/server.log

# Found error: connection refused on port XXXX

# Verify connectivity status
Use mcp-inspect with workflow_file="dev" and server="<server-name>"

# Create diagnostic report using safe-output
Output report_diagnostics_to_pull_request with:
- Issue: MCP server failed to start
- Root Cause: Port already in use or missing dependency
- Evidence: Error message from server.log
- Fix: Verify port availability and dependency installation
```

Remember: Always conclude your debugging session by posting a diagnostic report using the `report_diagnostics_to_pull_request` safe-output. This ensures your findings are documented and actionable.
