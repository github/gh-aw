---
title: GH-AW as an MCP Server
description: Use the gh-aw MCP server to expose CLI tools to AI agents via Model Context Protocol, enabling secure workflow management.
sidebar:
  order: 400
---

The `gh aw mcp-server` command exposes GitHub Agentic Workflows CLI commands as MCP tools, allowing chat systems and workflows to manage agentic workflows programmatically.

Tool options are selected through named MCP parameters, not CLI flags embedded in workflow names, workflow specifications, or run IDs/URLs. Except for the `logs` tool's `workflow_name`, positional values beginning with `-` return an invalid-parameters error before the command executes. The `logs` tool passes workflow names unchanged after a CLI `--` boundary, so names such as `-nightly` remain valid and cannot override options. For a local workflow file whose name begins with `-`, use a relative path such as `./-workflow.md`.

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

The `compile` tool uses a dedicated read-only mode with `--no-emit --no-check-update` by default: it validates workflows without modifying files, including lock files, diagnostic YAML, action pins, import caches, temporary includes, or `.gitattributes`. Remote imports and body-level includes, including nested includes, must already be cached; missing entries produce a validation error instead of downloading files. Container validation only inspects local Docker images and reports missing images without pulling them. Validation uses the newly generated YAML, not stale lock-file contents. Fixes, dry-run compilation, and external scanners require write tools.

This stronger MCP mode is separate from ordinary CLI `compile --no-emit` and `validate`, which can still populate import caches. Read-only compilation is not an offline guarantee: resolving an import's branch or tag to its current SHA can require network access.

Workflow-writing tools (`add`, `update`, and `fix`) are disabled by default. The operator can enable them and allow compilation to write files at server startup for trusted clients and workflow sources:

```bash wrap
gh aw mcp-server --allow-write-tools
```

> [!WARNING]
> This opt-in permits workflow modifications and compiler scanner execution. MCP callers cannot enable it through tool arguments. Repository write access alone does not enable these tools, and disabling actor validation does not bypass this gate.

The `mcp-inspect` tool only lists configuration. It never starts workflow-declared commands or containers, connects to configured HTTP endpoints, or checks secrets, even when `--allow-write-tools` is enabled. Live discovery remains available through `gh aw mcp inspect <workflow>` invoked directly by the operator after reviewing and trusting the workflow's commands, packages, container images, and endpoints.

Use `--port` to run over HTTP/SSE:

```bash wrap
gh aw mcp-server --port 8080
```

Use `--validate-actor` to require repository permission checks before exposing log and audit capabilities:

```bash wrap
gh aw mcp-server --validate-actor
```

When validation is enabled, `logs`, `audit`, `audit-diff`, and enabled workflow-writing tools require write, maintain, or admin access. The server reads `GITHUB_ACTOR` and `GITHUB_REPOSITORY`, caches permission results in memory for 1 hour, and never falls back to open access if `GITHUB_ACTOR` is missing. Both the startup capability gate and actor checks apply to stdio and HTTP/SSE transports.

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
      "tools": ["compile", "audit", "logs", "inspect", "status", "audit-diff", "work-queue", "health"]
    }
  }
}
```

Reload VS Code after making changes.

## Configuring with Docker

If `gh` is not installed locally, use the `ghcr.io/github/gh-aw` Docker image. The image ships with the GitHub CLI, gh-aw, and native ShellCheck pre-installed and uses `mcp-server` as the default command. Strict dry-run compilation keeps ShellCheck validation enabled without requiring Docker-in-Docker; missing required scanners remain errors.

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
| `health` | Read workflow health, success rates, execution trends, and revision-aware recovery evidence. | `workflow_name`, `days`, `start_date`, `end_date`, `threshold`, `repo`, `logs_dir` | JSON summary for all workflows or a detailed report for one workflow. |
| `compile` | Compile Markdown workflows to GitHub Actions YAML with optional static analysis. | `workflows`, `strict`, `fix`, `zizmor`, `poutine`, `actionlint`, `grant`, `jq` | JSON array with `workflow`, `valid`, `errors`, `warnings`, `compiled_file`. |
| `logs` | Download and analyze workflow logs with timeout and token guardrails. | `workflow_name`, `count`, `start_date`, `end_date`, `engine`, `firewall`, `no_firewall`, `branch`, `after_run_id`, `before_run_id`, `artifacts`, `timeout`, `max_tokens`, `jq` | JSON envelope with the collected report's `file_path` and collection state; continuation parameters are included when available. |
| `audit` | Audit one or more workflow runs; with multiple runs, compare each run to the first. | `run_ids_or_urls` (preferred), `run_id`, deprecated `run_id_or_url`, plus `artifacts`, `experiment`, `variant`, `jq` | Single-run JSON audit or multi-run diff JSON. |
| `checks` | Normalize CI check state for a pull request. | `pr_number`, `repo` | JSON with `state`, `required_state`, `pr_number`, `head_sha`, `check_runs`, `statuses`, `total_count`. |
| `mcp-inspect` | List MCP server configurations without executing or connecting to servers. | `workflow_file`; `server` and `tool` are unsupported and return an error. | Formatted configuration listing. |
| `work-queue` | Read the Git-backed work queue without publishing or changing queue state. | `operation`, `repo`, `branch`, and operation-specific selectors or filters | The selected CLI operation's JSON output. |
| `add` | Add workflows from remote repositories to `.github/workflows`. | `workflows`, `number`, `name` | Added workflow files. |
| `update` | Update sourced workflows and check for gh-aw updates. | `workflows`, `major`, `force` | Updated workflow files and version checks. |
| `fix` | Apply automatic codemod-style fixes. | `workflows`, `write`, `list_codemods` | Dry-run or written fixes. |

Workflow and run positional parameters must not start with `-`; use the tool's named parameters for options. For a local workflow filename beginning with `-`, supply a path prefixed with `./`. The `add` tool's `name` is a filename without an extension, not a path: `/`, `\`, and `..` are rejected. Workflow destinations must remain within the selected workflow directory, including when existing files are symlinks.

The read-only `health` tool is available without `--allow-write-tools` or actor validation. It uses the server's GitHub credentials to read a bounded current-revision sample. `days` accepts 7, 30, or 90 (default 7) and is mutually exclusive with `start_date`. Date parameters accept UTC dates, RFC3339 timestamps, or relative times such as `-24h`. `threshold` defaults to 80 percent. Failure clusters and sub-agent failures use the existing `logs_dir` cache only; the tool does not download artifacts or modify cached logs.

> [!NOTE]
> The `actionlint`, `zizmor`, and `poutine` scanners used by `compile` prefer compatible local binaries on `PATH` (actionlint ≥ 1.7.12, zizmor ≥ 1.31.1, poutine ≥ 1.1.6), falling back to Docker otherwise. Docker-based scanners, including `grant`, pull images on first use. If you see a "Docker images are being downloaded" message, wait 15–30 seconds and retry.

For `audit`, each run identifier may be a numeric run ID, a run URL, a job URL, or a job URL with a step anchor such as `https://github.com/owner/repo/actions/runs/123/job/456#step:7:1`.

The MCP `logs` response returns a `file_path` to the collected report. Truncated discovery sets `partial: true` and preserves either `continuation` or the per-target `continuations` array. Resume each target using its repository and all cursor parameters, including `end_date` and `before_run_id`. Episode evidence gaps also set `partial: true`, but may have no resumable cursor. For episode reports, `coverage_status` is `partial` or `unknown`, and `coverage_reasons` summarizes recorded gaps. An unknown status does not establish complete membership; the report retains per-episode coverage and `episode_retrieval` bounds, counters, stop reasons, and issues.

Cursor fields and optional episode coverage sections are decoded independently: malformed episode data does not discard valid cursors. Malformed report JSON exposes `coverage_status: "unknown"` with an `invalid_report_json` reason instead of an empty success state; malformed sections have section-specific `invalid_*` reasons. Any usable evidence of partial collection remains partial even when another section cannot be decoded. Parse uncertainty alone does not set `partial: true`, and neither unknown state nor the absence of a cursor proves completeness. The envelope does not change cached report bytes or access controls.

The MCP `compile` and `audit` tools reserve time to respond before the default 60-second tool deadline. Timeout responses are tool errors with `partial: true`, `complete: false`, and an `error` naming the timed-out `stage`, including `subprocess_slot` when waiting for capacity. Compile timeout envelopes include available structured `partial_results` and bounded per-call `diagnostics` instead of the usual validation array. Concurrent calls do not share diagnostic buffers.

Audit timeout envelopes include recovery `suggestions`. When available, `partial_results` contains single-run metadata and collected analysis results; it is not a complete audit report and is never stored as a full-audit cache entry. Retry the same audit to reuse downloaded artifacts, or narrow `artifacts`. CLI callers can set a separate duration with `gh aw audit <run-id> --json --timeout 50s`; timeout still exits unsuccessfully while returning available single-run data.

If `compile` fails before producing JSON, its fallback errors prioritize the actionable cause and remediation rather than debug stderr. Each diagnostic is limited to 4 KiB, and the fallback JSON is limited to 48 KiB. Omitted debug output or oversized diagnostics are marked explicitly. If per-workflow errors exceed that budget, the response contains one invalid `scope: "batch"` compile result with the affected workflow count. Structured compiler and scanner results are not subject to this fallback budget. The deprecated `compile` parameter `max_tokens` remains ignored.

For `checks`, normalized states are `success`, `failed`, `pending`, `no_checks`, and `policy_blocked`. Use `required_state` as the authoritative CI verdict when optional third-party deployments are present.

Available `fix` codemods include `timeout-minutes-migration`, `network-firewall-migration`, `mcp-scripts-mode-removal`, and `steps-run-secrets-to-env`.

### Read-only work queue

The `work-queue` tool requires `operation` and `repo` (`owner/repo`), matching the CLI's explicit repository selection. The optional `branch` defaults to `work-queue`. Git storage uses the server's existing credentials; private repositories require read access. Actor validation for log and audit tools does not gate this tool because its operations are read-only and repository access is already bounded by those Git credentials.

| Operation | Options | Returns |
| --- | --- | --- |
| `state` | `graph`, `pool`, `state`, `search`, `offset`, `limit` | Bounded metadata-only Work/Claim forest with counts, queue tip, and `next_offset` when more rows remain. |
| `inspect` | Exactly one of `work_id` or `claim_id` | Metadata, dependencies, assignment membership, and delivery barriers. |

Options belonging to another operation are rejected. For `state`, the `state` filter accepts `available`, `claimed`, `completed`, or `cancelled`. Pagination uses a nonnegative `offset` and a `limit` of 1–256 (default 80).

```json wrap
{
  "name": "work-queue",
  "arguments": {
    "operation": "state",
    "repo": "owner/repo",
    "state": "available",
    "limit": 20
  }
}
```

Only `state` and `inspect` are exposed through MCP. Other read-only operations (`replay`, `stats`, `explain`, `trace`, and `evidence`) remain CLI-only, as do all mutating operations.

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
