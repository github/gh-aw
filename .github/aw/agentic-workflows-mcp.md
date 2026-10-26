---
description: agentic-workflows MCP server tool reference for workflows that call status, logs, audit, or compile
---

# agentic-workflows MCP Server Tools

**⚠️ CRITICAL**: `status`, `logs`, `audit`, and `compile` are MCP server tools — NOT shell commands. Do NOT run `gh aw` directly. If the MCP server fails, give up.

## Tools

### `status`

Verify MCP server configuration and list all workflows.

No required parameters.

### `logs`

Download workflow run logs to private server storage at `/tmp/gh-aw/aw-mcp/logs/`. The normal response exposes a JSON summary through `file_path` under `/tmp/gh-aw/logs-cache/`; read only that explicitly returned file if session policy permits. Do not enumerate private raw artifacts or change their permissions. If the summary is inaccessible, report incomplete coverage rather than assuming the download or filters were validated.

| Parameter | Description |
|---|---|
| `workflow_name` | Filter to a specific workflow (leave empty for all) |
| `count` | Number of runs (default: 100) |
| `start_date` | Filter runs after this date — `YYYY-MM-DD` or relative like `-1d`, `-7d`, `-30d` |
| `end_date` | Filter runs before this date |
| `engine` | Filter by AI engine: `copilot`, `claude`, `codex` |
| `branch` | Filter by branch name |
| `firewall` / `no_firewall` | Filter by firewall status |
| `filtered_integrity` | Only runs with DIFC integrity-filtered events in gateway logs |
| `after_run_id` / `before_run_id` | Paginate by run database ID |
| `artifacts` | Artifact sets to download; defaults to compact `info,usage`, not full raw logs |
| `max_tokens` | Deprecated and ignored; output is written to the returned file |

If a gateway timeout or token budget guardrail cuts the call short, the response sets `partial: true` and includes `after_run_id`/`before_run_id` — reissue `logs` with that parameter to fetch the rest.

### `audit`

Inspect one or more runs in detail (missing tools, safe outputs, metrics). With multiple runs, compares each to the first (base).

| Parameter | Description |
|---|---|
| `run_ids_or_urls` | Preferred. One or more run IDs or run/job URLs (including step anchors). Single item: detailed audit; multiple: diff mode. |
| `run_id` / `run_id_or_url` | Aliases for a single run; `run_id_or_url` is deprecated in favor of `run_ids_or_urls`. |

### `compile`

Recompile workflow `.md` files into `.lock.yml` files.

Disabled by default, even with `dry_run: true`. Trusted workflow authors must explicitly configure `tools.agentic-workflows.allow-write-tools: true`; the compiler passes `--allow-write-tools` at server startup and retains actor validation. This also enables `add`, `update`, and `fix`, so prompts should constrain use to the intended operations. MCP callers cannot enable this capability through call arguments.

`dry_run: true` still emits local lock files with compiler-managed mutations suppressed; it does not execute workflows or make arbitrary custom code safe. Use `strict: true` for validation and report actual errors, warnings, and missing checks.

**MCP equivalent of**: `gh aw compile`

### Timeouts and partial results

`compile` and `audit` reserve 5 seconds before the caller's deadline (55s budget when no deadline is given). A timeout response is a tool error with `partial: true`, `complete: false`, and an `error` naming the timed-out stage. `compile` timeouts include `partial_results` with workflows completed before the stall; `audit` timeouts include any decoded report under `partial_results`. Retry the same call to reuse downloaded artifacts, or narrow `artifacts`.
