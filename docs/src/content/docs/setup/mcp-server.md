---
title: MCP Server
description: Use the gh-aw MCP server to expose CLI tools to AI agents via Model Context Protocol, enabling secure workflow management.
sidebar:
  order: 400
---

The `gh aw mcp-server` command exposes CLI tools (status, compile, logs, audit, update, add, mcp-inspect) to AI agents through the Model Context Protocol.

Start the server:
```bash wrap
gh aw mcp-server
```

Or configure for any Model Context Protocol (MCP) host:
```yaml wrap
command: gh
args: [aw, mcp-server]
```

> [!TIP]
> Use in agentic workflows by adding `agentic-workflows:` to your workflow's `tools:` section. See [Using as Agentic Workflows Tool](#using-as-agentic-workflows-tool).

## Configuration Options

### HTTP Server Mode

Run with HTTP/SSE transport using `--port`:

```bash wrap
gh aw mcp-server --port 8080
```

### Actor Validation

Control access to logs and audit tools based on repository permissions using `--validate-actor`:

```bash wrap
gh aw mcp-server --validate-actor
```

When actor validation is enabled:
- Logs and audit tools require write, maintain, or admin repository access
- The server reads `GITHUB_ACTOR` and `GITHUB_REPOSITORY` environment variables to determine actor permissions
- Permission checks are performed at runtime using the GitHub API
- Results are cached for 1 hour to minimize API calls

When actor validation is disabled (default):
- All tools are available without permission checks
- Backward compatible with existing configurations

**Environment Variables:**
- `GITHUB_ACTOR`: GitHub username of the current actor (required when validation enabled)
- `GITHUB_REPOSITORY`: Repository in `owner/repo` format (optional, improves performance)

**Permission Requirements:**

Restricted tools (logs, audit) require:
- Minimum role: write, maintain, or admin
- Permission check via GitHub API: `GET /repos/{owner}/{repo}/collaborators/{username}/permission`

**Error Handling:**

When `GITHUB_ACTOR` is not set and validation is enabled:
```json
{
  "error": "GITHUB_ACTOR environment variable not set",
  "tool": "logs",
  "reason": "This tool requires at least write access to the repository. Set GITHUB_ACTOR environment variable to enable access."
}
```

When actor has insufficient permissions:
```json
{
  "error": "insufficient repository permissions",
  "actor": "username",
  "repository": "owner/repo",
  "role": "read",
  "required": "write, maintain, or admin",
  "reason": "Actor username has read access to owner/repo. This tool requires at least write access."
}
```

## Configuring with GitHub Copilot Agent

Configure GitHub Copilot Agent to use gh-aw MCP server:

```bash wrap
gh aw init
```

This creates `.github/workflows/copilot-setup-steps.yml` that sets up Go, GitHub CLI, and gh-aw extension before agent sessions start, making workflow management tools available to the agent. MCP server integration is enabled by default. Use `gh aw init --no-mcp` to skip MCP configuration.

## Configuring with Copilot CLI

To add the MCP server in the interactive Copilot CLI session, start `copilot` and run:

```
/mcp add github-agentic-workflows gh aw mcp-server
```

## Configuring with VS Code

Configure VS Code Copilot Chat to use gh-aw MCP server:

```bash wrap
gh aw init
```

This creates `.vscode/mcp.json` and `.github/workflows/copilot-setup-steps.yml`. MCP server integration is enabled by default. Use `gh aw init --no-mcp` to skip MCP configuration.

Alternatively, create `.vscode/mcp.json` manually:

```json wrap
{
  "servers": {
    "github-agentic-workflows": {
      "command": "gh",
      "args": ["aw", "mcp-server"],
      "cwd": "${workspaceFolder}"
    }
  }
}
```

Reload VS Code after making changes.

## Available Tools

The MCP server exposes the following tools for workflow management:

### status

Show status of agentic workflow files and workflows.

**Parameters:**
- `pattern` (optional): Filter workflows by name pattern
- `jq` (optional): Apply jq filter to JSON output

**Returns:** JSON array with workflow information including:
- `workflow`: Name of the workflow file
- `agent`: AI engine used (e.g., "copilot", "claude", "codex")
- `compiled`: Compilation status ("Yes", "No", or "N/A")
- `status`: GitHub workflow status ("active", "disabled", "Unknown")
- `time_remaining`: Time remaining until workflow deadline (if applicable)

### compile

Compile Markdown workflows to GitHub Actions YAML with optional static analysis.

> [!CAUTION]
> Any change to `.github/workflows/*.md` files **MUST** be compiled using this tool. The `.lock.yml` files are what GitHub Actions executes.

**Parameters:**
- `workflows` (optional): Array of workflow files to compile (empty for all)
- `strict` (optional): Enforce strict mode validation (default: true)
- `fix` (optional): Apply automatic codemod fixes before compiling
- `zizmor` (optional): Run zizmor security scanner
- `poutine` (optional): Run poutine security scanner
- `actionlint` (optional): Run actionlint linter
- `jq` (optional): Apply jq filter to JSON output

**Strict Mode:** Workflows use strict mode by default (unless `strict: false` in frontmatter). Enforces:
- Action pinning to commit SHAs
- Explicit network configuration
- Safe outputs for write operations
- Refuses write permissions and deprecated fields

**Returns:** JSON array with validation results:
- `workflow`: Name of the workflow file
- `valid`: Boolean indicating compilation success
- `errors`: Array of error objects with type, message, and line number
- `warnings`: Array of warning objects
- `compiled_file`: Path to generated `.lock.yml` file

### logs

Download and analyze workflow logs with timeout handling and size guardrails.

**Parameters:**
- `workflow_name` (optional): Workflow name to download logs for (empty for all)
- `count` (optional): Number of workflow runs to download (default: 100)
- `start_date` (optional): Filter runs after this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)
- `end_date` (optional): Filter runs before this date
- `engine` (optional): Filter by agentic engine type (claude, codex, copilot)
- `firewall` (optional): Filter to only runs with firewall enabled
- `no_firewall` (optional): Filter to only runs without firewall
- `branch` (optional): Filter runs by branch name
- `after_run_id` (optional): Filter runs after this database ID
- `before_run_id` (optional): Filter runs before this database ID
- `timeout` (optional): Maximum time in seconds to download logs (default: 50)
- `max_tokens` (optional): Maximum output tokens before guardrail triggers (default: 12000)
- `jq` (optional): Apply jq filter to JSON output

**Features:**
- **Timeout and Continuation**: Uses 50-second timeout. Returns `continuation` field with `before_run_id` to resume fetching
- **Output Size Guardrail**: When output exceeds token limit, returns schema description and suggested jq filters
- **Large Output Handling**: Outputs exceeding 16,000 tokens written to `/tmp/gh-aw/safe-outputs/`

**Returns:** JSON with workflow run data and metrics, or continuation parameters if timeout occurred.

### audit

Investigate a workflow run, job, or specific step and generate a detailed report.

**Parameters:**
- `run_id_or_url` (required): One of:
  - Numeric run ID: `1234567890`
  - Run URL: `https://github.com/owner/repo/actions/runs/1234567890`
  - Job URL: `https://github.com/owner/repo/actions/runs/1234567890/job/9876543210`
  - Job URL with step: `https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1`
- `jq` (optional): Apply jq filter to JSON output

**Job URL Handling:**
- With step number: Extracts that specific step's output
- Without step: Finds and extracts first failing step's output
- Saves job logs and step-specific logs to output directory

**Returns:** JSON with comprehensive audit data:
- `overview`: Basic run information (run_id, workflow_name, status, conclusion, duration, url, logs_path)
- `metrics`: Execution metrics (token_usage, estimated_cost, turns, error_count, warning_count)
- `jobs`: List of job details (name, status, conclusion, duration)
- `downloaded_files`: List of artifact files with descriptions
- `missing_tools`: Tools requested but not available
- `mcp_failures`: MCP server failures
- `errors`: Error details with file, line, type, and message
- `warnings`: Warning details
- `tool_usage`: Tool usage statistics
- `firewall_analysis`: Network firewall analysis if available

### mcp-inspect

Inspect MCP servers in workflows and list available tools, resources, and roots.

**Parameters:**
- `workflow_file` (optional): Workflow file to inspect (empty to list all workflows with MCP servers)
- `server` (optional): Filter to specific MCP server
- `tool` (optional): Show detailed information about a specific tool (requires server parameter)

**Features:**
- Starts each configured MCP server and queries capabilities
- Supports stdio, Docker, and HTTP MCP servers
- Secret checking enabled by default to validate GitHub Actions secrets availability
- Silently skips secret checking if GitHub token unavailable or lacks permissions

**Returns:** Formatted text output showing:
- Available MCP servers in the workflow
- Tools, resources, and roots exposed by each server
- Secret availability status (if GitHub token available)
- Detailed tool information when tool parameter specified

### add

Add workflows from remote repositories to `.github/workflows`.

**Parameters:**
- `workflows` (required): Array of workflow specifications
  - Format: `owner/repo/workflow-name` or `owner/repo/workflow-name@version`
- `number` (optional): Create multiple numbered copies
- `name` (optional): Specify name for added workflow (without .md extension)

**Returns:** Formatted text output showing added workflows.

### update

Update workflows from their source repositories and check for gh-aw updates.

**Parameters:**
- `workflows` (optional): Array of workflow IDs to update (empty for all)
- `major` (optional): Allow major version updates when updating tagged releases
- `force` (optional): Force update even if no changes detected

**Update Logic:**
- If ref is a tag: Updates to latest release (use `major` flag for major version updates)
- If ref is a branch: Fetches latest commit from that branch
- Otherwise: Fetches latest commit from default branch

**The command:**
1. Checks if newer version of gh-aw is available
2. Updates workflows using `source` field in frontmatter
3. Compiles each workflow immediately after update

**Returns:** Formatted text output showing:
- Extension update status
- Updated workflows with new versions
- Compilation status for each workflow

### fix

Apply automatic codemod-style fixes to workflow files.

**Parameters:**
- `workflows` (optional): Array of workflow IDs to fix (empty for all)
- `write` (optional): Write changes to files (default is dry-run)
- `list_codemods` (optional): List all available codemods and exit

**Available Codemods:**
- `timeout-minutes-migration`: Replaces `timeout_minutes` with `timeout-minutes`
- `network-firewall-migration`: Removes deprecated `network.firewall` field
- `sandbox-agent-false-removal`: Removes `sandbox.agent: false` (firewall now mandatory)
- `safe-inputs-mode-removal`: Removes deprecated `safe-inputs.mode` field

**The command:**
1. Scans workflow files for deprecated fields
2. Applies relevant codemods to fix issues
3. Reports what was changed in each file
4. Writes updated files back to disk (with `write` flag)

**Returns:** Formatted text output showing:
- List of workflow files processed
- Which codemods were applied to each file
- Summary of fixes applied

## Using as Agentic Workflows Tool

Enable in workflow frontmatter:

```yaml wrap
---
permissions:
  actions: read  # Required for agentic-workflows tool
tools:
  agentic-workflows:
---

Check workflow status, download logs, and audit failures.
```

> [!CAUTION]
> Required Permission
> The `agentic-workflows` tool requires `actions: read` permission to access GitHub Actions workflow logs and run data.

