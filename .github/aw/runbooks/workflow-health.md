# Workflow Health Monitoring Runbook

This runbook documents how to investigate and resolve workflow health issues in GitHub Agentic Workflows, based on learnings from operational incident response.

## When to Use This Runbook

Use this runbook when:
- Workflows are failing in scheduled runs
- Missing-tool errors appear in workflow logs
- Authentication or permission errors occur
- Safe-input or safe-output configurations fail

## Common Workflow Failure Patterns

### Missing Tool Configurations

**Symptoms**:
- Error messages containing "missing-tool" or "tool not found"
- Workflow fails when attempting to access GitHub APIs
- Agent cannot perform GitHub operations (read issues, create PRs, etc.)

**Common Causes**:
- GitHub MCP server not configured in workflow frontmatter
- Missing toolsets specification
- Incorrect toolset names

### Authentication and Permission Errors

**Symptoms**:
- HTTP 403 (Forbidden) errors
- "Resource not accessible" errors
- Token scope errors

**Common Causes**:
- Missing `permissions:` block in workflow frontmatter
- Insufficient token permissions for requested operations
- GITHUB_TOKEN not passed to custom actions

### Input/Secret Validation Failures

**Symptoms**:
- Safe-inputs action fails
- Environment variable not available
- Template expression evaluation errors

**Common Causes**:
- Safe-inputs action not configured
- Missing required secrets
- Incorrect secret references

## Investigation Steps

### Step 1: Analyze Workflow Logs

Use the `gh aw logs` command to download and analyze workflow logs:

```bash
# Download logs from last 24 hours
gh aw logs --start-date -1d -o /tmp/workflow-logs

# Download logs for a specific workflow run
gh aw logs --run-id <run-id> -o /tmp/workflow-logs

# Analyze logs for a specific workflow
gh aw logs --workflow <workflow-name> --start-date -7d
```

**What to look for**:
- Error messages in the "Run AI Agent" step
- Missing-tool errors
- HTTP error codes (401, 403, 404, 500)
- Stack traces or exception details

### Step 2: Identify Missing-Tool Errors

Missing-tool errors typically appear in this format:

```
Error: Tool 'github:read_issue' not found
Error: missing tool configuration for safeinputs-gh
```

To identify which tools are missing:

1. Check the workflow `.md` file for the `tools:` section
2. Compare with similar working workflows
3. Verify the tool is properly configured in frontmatter

### Step 3: Verify MCP Server Configurations

Check if the workflow has proper MCP server configuration:

```aw
---
tools:
  github:
    mode: remote          # or "local" for Docker-based
    toolsets: [default]   # Enables repos, issues, pull_requests
---
```

Use `gh aw mcp inspect <workflow-name>` to verify MCP server configuration:

```bash
# Inspect MCP servers for a workflow
gh aw mcp inspect <workflow-name>

# List all workflows with MCP servers
gh aw mcp list
```

### Step 4: Check Permissions Configuration

Verify the workflow has required permissions:

```aw
---
permissions:
  contents: read      # For reading repository files
  issues: write       # For creating/updating issues
  pull-requests: write # For creating/updating PRs
  actions: read       # For accessing workflow runs
---
```

Common permission requirements:
- **Reading issues**: `issues: read`
- **Creating issues**: `issues: write`
- **Reading PRs**: `pull-requests: read`
- **Creating PRs**: `pull-requests: write`
- **Reading workflow runs**: `actions: read`

## Resolution Procedures

### Adding GitHub MCP Server to Workflows

**Problem**: Workflow fails with missing GitHub tool errors.

**Solution**: Add GitHub MCP server configuration to the workflow frontmatter.

1. Open the workflow `.md` file
2. Add or update the `tools:` section:

```aw
---
tools:
  github:
    mode: remote
    toolsets: [default]
---
```

3. Compile the workflow:

```bash
gh aw compile <workflow-name>.md
```

4. Verify the configuration:

```bash
gh aw mcp inspect <workflow-name>
```

**Available toolsets**:
- `default`: repositories, issues, pull requests, and common operations
- `repos`: repository management tools
- `issues`: issue operations
- `pull_requests`: PR operations
- `actions`: GitHub Actions workflow tools

**Example**: Dev workflow with GitHub MCP server

```aw
---
description: Development workflow with GitHub integration
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [default]
---

# Development Agent

Analyze repository issues and provide insights.
```

### Configuring Safe-Inputs and Safe-Outputs

**Problem**: Workflow fails with missing safeinputs-gh or safe-output errors.

**Solution**: Configure safe-inputs and safe-outputs in the workflow.

#### Adding Safe-Inputs

Safe-inputs securely pass GitHub context to AI agents:

```aw
---
safe-inputs:
  issue:
    title: ${{ github.event.issue.title }}
    body: ${{ github.event.issue.body }}
    number: ${{ github.event.issue.number }}
---
```

The safe-inputs are automatically made available to the agent as environment variables.

#### Adding Safe-Outputs

Safe-outputs enable AI agents to create GitHub resources:

```aw
---
safe-outputs:
  create-issue:
    labels: ["ai-generated"]
  create-pull-request:
    labels: ["ai-generated"]
  create-discussion:
    category: "general"
---
```

**Example**: Complete workflow with safe-inputs and safe-outputs

```aw
---
description: Issue triage workflow
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [default]
safe-inputs:
  issue:
    title: ${{ github.event.issue.title }}
    body: ${{ github.event.issue.body }}
    number: ${{ github.event.issue.number }}
safe-outputs:
  create-issue:
    labels: ["ai-generated", "triage"]
---

# Issue Triage Agent

Analyze the issue and determine appropriate labels and priority.
```

### Testing Workflow Fixes

After making changes, test the workflow:

1. **Compile the workflow**:

```bash
gh aw compile <workflow-name>.md
```

2. **Trigger manually** (if `workflow_dispatch` is enabled):

```bash
gh workflow run <workflow-name>.lock.yml
```

3. **Monitor the run**:

```bash
# Get the run ID
gh run list --workflow=<workflow-name>.lock.yml --limit 1

# Watch the run
gh run watch <run-id>

# Download logs if it fails
gh aw logs --run-id <run-id>
```

4. **Verify success**:
   - Check that no missing-tool errors occur
   - Verify the agent completes successfully
   - Confirm any created resources (issues, PRs, discussions)

## Case Study: DeepReport Incident Response

### Background

The DeepReport Intelligence Briefing (Discussion #7277) identified several workflow health issues:

1. **Weekly Issue Summary workflow** - Failed in recent runs
2. **Dev workflow** - Missing GitHub MCP read_issue capability (Run #20435819459)
3. **Daily Copilot PR Merged workflow** - Missing safeinputs-gh tool

### Investigation

**Weekly Issue Summary**:
- Analyzed workflow logs using `gh aw logs`
- Identified authentication errors in GitHub API calls
- Found missing permissions in workflow configuration

**Dev Workflow**:
- Error: "Tool 'github:read_issue' not found"
- Root cause: GitHub MCP server not configured
- The workflow attempted to read issue information without GitHub MCP toolset

**Daily Copilot PR Merged**:
- Error: "missing tool configuration for safeinputs-gh"
- Root cause: Safe-inputs action not set up in workflow
- PR merge data not being passed securely to agent

### Resolution

**Weekly Issue Summary**:
- Added missing `actions: read` permission
- Recompiled workflow with `gh aw compile`
- Verified success in next scheduled run

**Dev Workflow**:
Added GitHub MCP server configuration:

```aw
tools:
  github:
    mode: remote
    toolsets: [default]
```

**Daily Copilot PR Merged**:
Added safe-inputs configuration:

```aw
safe-inputs:
  pull_request:
    number: ${{ github.event.pull_request.number }}
    title: ${{ github.event.pull_request.title }}
```

### Lessons Learned

1. **MCP-first approach**: Always configure GitHub MCP server when workflows need GitHub API access
2. **Permission planning**: Define required permissions upfront based on workflow operations
3. **Safe-inputs for context**: Use safe-inputs to securely pass GitHub event context to agents
4. **Test after compilation**: Always test workflows manually after making configuration changes
5. **Monitor systematically**: Use `gh aw logs` for regular workflow health monitoring

## Quick Reference

### Essential Commands

```bash
# Download recent workflow logs
gh aw logs --start-date -1d -o /tmp/logs

# Inspect MCP configuration
gh aw mcp inspect <workflow-name>

# List all workflows with MCP servers
gh aw mcp list

# Compile workflow after changes
gh aw compile <workflow-name>.md

# Trigger workflow manually
gh workflow run <workflow-name>.lock.yml

# Watch workflow execution
gh run watch <run-id>
```

### Common Configuration Patterns

**Basic GitHub integration**:
```aw
---
permissions:
  contents: read
  issues: read
tools:
  github:
    mode: remote
    toolsets: [default]
---
```

**Issue-triggered workflow with safe-inputs**:
```aw
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
safe-inputs:
  issue:
    title: ${{ github.event.issue.title }}
    body: ${{ github.event.issue.body }}
tools:
  github:
    mode: remote
    toolsets: [default]
---
```

**Workflow with safe-outputs**:
```aw
---
permissions:
  contents: read
  issues: write
  discussions: write
safe-outputs:
  create-issue:
    labels: ["ai-generated"]
  create-discussion:
    category: "general"
tools:
  github:
    mode: remote
    toolsets: [default]
---
```

## Additional Resources

- [Getting Started with MCP](/docs/src/content/docs/guides/getting-started-mcp.md)
- [Security Guide](/docs/src/content/docs/introduction/architecture.md)
- [Error Reference](/docs/src/content/docs/troubleshooting/errors.md)
- [Common Issues](/docs/src/content/docs/troubleshooting/common-issues.md)
