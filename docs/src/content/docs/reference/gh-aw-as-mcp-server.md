---
title: GH-AW as an MCP Server
description: Use the gh-aw MCP server to expose CLI tools to AI agents via Model Context Protocol, enabling secure workflow management.
sidebar:
  order: 400
---

The `gh aw mcp-server` command exposes GitHub Agentic Workflows CLI commands as MCP tools, allowing chat systems and workflows to manage agentic workflows programmatically.

Start the server:

```bash wrap
gh aw mcp-server
```

Or configure for any Model Context Protocol (MCP) host:

```yaml wrap
command: gh
args: [aw, mcp-server]
```

## Configuration Options

Use `--port` to run over HTTP/SSE:

```bash wrap
gh aw mcp-server --port 8080
```

Use `--validate-actor` to require repository permission checks before exposing log and audit capabilities:

```bash wrap
gh aw mcp-server --validate-actor
```

When validation is enabled, `logs`, `audit`, and `audit-diff` require write, maintain, or admin access. The server reads `GITHUB_ACTOR` and `GITHUB_REPOSITORY`, caches permission results in memory for 1 hour, and never falls back to open access if `GITHUB_ACTOR` is missing.

> [!WARNING]
> With `--validate-actor`, privileged tools remain mounted, but `logs`, `audit`, and `audit-diff` return permission-denied errors until `GITHUB_ACTOR` is provided.

> [!NOTE]
> The permission cache is in-memory only. Restarting `gh aw mcp-server` clears it immediately, and there is no manual cache-flush command.

## Configuring with GitHub Copilot Agent

Configure GitHub Copilot Agent to use gh-aw MCP server:

```bash wrap
gh aw init
```

This creates `.github/workflows/copilot-setup-steps.yml` that sets up Go, GitHub CLI, and gh-aw extension before agent sessions start, making workflow management tools available to the agent. MCP server integration is enabled by default. Use `gh aw init --no-mcp` to skip MCP configuration.

## Configuring with Copilot CLI

To add the MCP server in the interactive Copilot CLI session, start `copilot` and run:

```text
/mcp add github-agentic-workflows gh aw mcp-server
```

## Configuring with VS Code

Run `gh aw init` to configure VS Code Copilot Chat:

```bash wrap
gh aw init
```

This creates `.github/mcp.json` and `.github/workflows/copilot-setup-steps.yml`. MCP server integration is enabled by default; use `gh aw init --no-mcp` to skip it.

Alternatively, create `.github/mcp.json` manually:

```json wrap
{
  "mcpServers": {
    "github-agentic-workflows": {
      "type": "local",
      "command": "gh",
      "args": ["aw", "mcp-server"],
      "tools": ["compile", "audit", "logs", "inspect", "status", "audit-diff"]
    }
  }
}
```

Reload VS Code after making changes.

## Configuring with Docker

If `gh` is not installed locally, use the `ghcr.io/github/gh-aw` Docker image. The image ships with the GitHub CLI and gh-aw pre-installed and uses `mcp-server` as the default command.

```json wrap
{
  "command": "docker",
  "args": [
    "run", "--rm", "-i",
    "-e", "GITHUB_TOKEN",
    "-e", "GITHUB_ACTOR",
    "ghcr.io/github/gh-aw:latest",
    "mcp-server"
  ]
}
```

Pass your GitHub token via the `GITHUB_TOKEN` environment variable. Add `--validate-actor` to the `args` array to enforce permission checks based on `GITHUB_ACTOR`.

## Available Tools

The MCP server exposes these workflow-management tools:

| Tool | Purpose | Key options | Returns |
| --- | --- | --- | --- |
| `status` | Show workflow and compiled-file status. | `pattern`, `jq` | JSON array with `workflow`, `agent`, `compiled`, `status`, `time_remaining`. |
| `compile` | Compile Markdown workflows to GitHub Actions YAML with optional static analysis. | `workflows`, `strict`, `fix`, `zizmor`, `poutine`, `actionlint`, `grant`, `jq` | JSON array with `workflow`, `valid`, `errors`, `warnings`, `compiled_file`. |
| `logs` | Download and analyze workflow logs with timeout and token guardrails. | `workflow_name`, `count`, `start_date`, `end_date`, `engine`, `firewall`, `no_firewall`, `branch`, `after_run_id`, `before_run_id`, `artifacts`, `timeout`, `max_tokens`, `jq` | JSON run data and metrics; the response sets `partial: true` and includes continuation parameters when results are incomplete. |
| `audit` | Audit one or more workflow runs; with multiple runs, compare each run to the first. | `run_ids_or_urls` (preferred), `run_id`, deprecated `run_id_or_url`, plus `artifacts`, `experiment`, `variant`, `jq` | Single-run JSON audit or multi-run diff JSON. |
| `checks` | Normalize CI check state for a pull request. | `pr_number`, `repo` | JSON with `state`, `required_state`, `pr_number`, `head_sha`, `check_runs`, `statuses`, `total_count`. |
| `mcp-inspect` | List MCP servers in workflows and inspect their tools, resources, and roots. | `workflow_file`, `server`, `tool` | Formatted text output. |
| `add` | Add workflows from remote repositories to `.github/workflows`. | `workflows`, `number`, `name` | Added workflow files. |
| `update` | Update sourced workflows and check for gh-aw updates. | `workflows`, `major`, `force` | Updated workflow files and version checks. |
| `fix` | Apply automatic codemod-style fixes. | `workflows`, `write`, `list_codemods` | Dry-run or written fixes. |

> [!NOTE]
> The `actionlint`, `zizmor`, `poutine`, and `grant` scanners used by `compile` pull Docker images on first use. If you see a "Docker images are being downloaded" message, wait 15–30 seconds and retry.

For `audit`, each run identifier may be a numeric run ID, a run URL, a job URL, or a job URL with a step anchor such as `https://github.com/owner/repo/actions/runs/123/job/456#step:7:1`.

For `checks`, normalized states are `success`, `failed`, `pending`, `no_checks`, and `policy_blocked`. Use `required_state` as the authoritative CI verdict when optional third-party deployments are present.

Available `fix` codemods include `timeout-minutes-migration`, `network-firewall-migration`, `mcp-scripts-mode-removal`, and `steps-run-secrets-to-env`.

## Using GH-AW as an MCP from an Agentic Workflow

Use the GH-AW MCP server from within a workflow to enable self-management (status checks, compilation, log analysis):

```yaml wrap
---
permissions:
  actions: read  # Required for agentic-workflows tool
tools:
  agentic-workflows:
---

Check workflow status, download logs, and audit failures.
```
