---
description: Complete reference for gh aw CLI commands and their MCP tool equivalents for restricted environments
---

# gh aw CLI Commands Reference

## CLI vs MCP Tool — When to Use Each

| Environment | Use |
|---|---|
| **Terminal with usable `gh` access** | `gh aw <command>` CLI |
| **Client exposing authorized gh-aw MCP tools** | Discover their schemas and use the supported operation |
| **GitHub Actions workflow step** | `gh aw <command>` after installing `github/gh-aw/actions/setup-cli` |
| **No CLI or authorized MCP access** | Cached evidence or static source; report unavailable collection |

> [!NOTE]
> **agentic-workflows MCP tool availability**
>
> Workflow configuration can expose gh-aw MCP tools through `agentic-workflows:`.
> Copilot client configuration and grants vary: do not assume MCP availability
> from the client name or treat it as a substitute for missing authentication.
> During debugging, use already-authorized tools only; adding configuration or
> starting a server requires the shared review gates.
>
> In a GitHub Actions workflow step, install the CLI first:
> ```yaml
> - uses: github/gh-aw/actions/setup-cli@<version>
> - run: gh aw compile
> ```

---

## Command Reference

### `gh aw skill`

Print the bundled `/agentic-workflows` `SKILL.md` content to standard output without adding files to the repository:

```bash
gh aw skill
```

Works outside a Git repository with no authentication or network access. Uses the file list bundled with the installed CLI version.

**MCP equivalent**: Not available.

---
### `gh aw init`

Initialize a repository for agentic workflows.

```bash
gh aw init                              # Initialize with defaults (non-interactive)
gh aw init --engine claude              # Skip Copilot-specific artifacts
gh aw init --no-mcp                     # Skip MCP server integration (Copilot engine)
gh aw init --no-agent                   # Skip custom agent creation (Copilot engine)
```

Creates `.github/skills/agentic-workflows/SKILL.md`. With the Copilot engine (default), also creates the custom agent (`.github/agents/agentic-workflows.md`) and enables MCP server integration — use `--no-mcp`/`--no-agent` to skip either. Non-Copilot engines skip both Copilot-specific artifacts automatically.

**MCP equivalent**: Not available — run from a local terminal or use the `upgrade` tool for updates.

---
### `gh aw compile`

Compile workflow `.md` files into GitHub Actions `.lock.yml` files.

```bash
gh aw compile                     # Compile all workflows
gh aw compile <workflow-name>     # Compile a specific workflow
gh aw compile <workflow-name> --dry-run  # Emit a diagnostic lock; does not execute it
gh aw compile <workflow-name> --dry-run --environment gh-aw-debug  # Optional all-job environment override
gh aw compile <workflow-name> --dry-run --zizmor --actionlint --poutine  # Optional Docker-based scanners
gh aw compile --strict            # Compile with strict mode validation
gh aw compile --validate          # Enable workflow, image, and action SHA validation
gh aw compile --fail-fast         # Stop at first error
gh aw compile --purge             # Remove orphaned .lock.yml files
gh aw compile --approve           # Approve new secrets / action changes
gh aw compile --no-lint           # Disable default built-in AST lint (rejected with --dry-run)
```

**MCP equivalent**: `compile` tool

For debugging, follow the [compilation guide](debug-compilation.md) for required
checks, optional Docker scanners, warning severity, and diagnostic preservation.
Low-severity/informational notices are not blanket failures; validation errors,
unclassified compiler warnings, model-inventory failures, built-in AST lint findings,
and scanner execution/parsing failures remain fatal to the dry-run gate.
`validate` uses `--no-emit` and skips zizmor, actionlint and poutine; a successful
`validate` does not establish scanner coverage. MCP `validate_images: true` opts
into `--validate-images` (works with `dry_run: true`) and fails when Docker is
unavailable. Built-in Actions/AW AST lint runs by default, including under
`--no-emit`/`validate`, and fails on findings; `--no-lint` (MCP `skip_lint: true`)
opts out but is rejected with `--dry-run`. `add`/`update`/`validate` lint too
without exposing `--no-lint`; for JS driver/grader checks use
`gh aw lint --javascript-module <path>`.

---
### `gh aw run`

> [!IMPORTANT]
> **When dispatch is permitted and human-validated, prefer `gh aw run` over `gh workflow run <file>.lock.yml`** — it handles workflow resolution, input validation, and run-tracking. This preference grants no authorization.

Trigger a workflow on demand using `workflow_dispatch`.

For troubleshooting, first follow the shared [strategy](debug-agentic-workflow.md): respect explicit no-dispatch contexts, and handle `403 Forbidden` from a permitted run attempt by stopping live debugging and returning to diagnosis/patching. Do not retry unchanged or bypass the denial. `--dry-run` is a compilation profile, not run access or authorization. Revalidate each source/lock revision; a valid [session authorization](debug-security-review.md#session-authorization) can cover in-scope iterations without repeated human approval.

Follow the [live-outcome table](debug-live-testing.md#live-debug-loop) for unknown dispatch results, moved refs, run-SHA mismatches and audit/log denial. `--repeat` requires an approved matching run-count budget; examples below do not grant approval. Codespaces may still return SAML-related 403 responses — check the active token source and SSO authorization using the shared credential triage (environment tokens override stored CLI credentials); do not automatically replace tokens or retry a denied run.

```bash
gh aw run                           # Interactive mode — pick workflow and fill inputs
gh aw run <workflow-name>           # Run by short name
gh aw run <workflow-name>.md        # Alternative: explicit .md extension
gh aw run <workflow-name> --ref REVIEWED_REF      # Exact reviewed remote ref
gh aw run <workflow-name> --repeat 3              # Run 4 times total (1 + 3 repeats)
gh aw run <workflow-name> --raw-field key=value   # Pass a specific input
```

**MCP equivalent**: Not available. Copilot cloud agents must not substitute an
API/MCP dispatch or request manual dispatch as a workaround.

---
### `gh aw logs`

Download and analyze workflow execution logs.

```bash
gh aw logs                          # Logs for all agentic workflows
gh aw logs <workflow-name>          # Logs for a specific workflow
gh aw logs <workflow-name> --json   # JSON output for programmatic use
gh aw logs --engine copilot         # Filter by engine
gh aw logs -c 10                    # Last 10 runs
gh aw logs --start-date -1w         # Last week's runs
gh aw logs --start-date 2024-01-01 --end-date 2024-01-31
gh aw logs -o ./workflow-logs       # Save to directory
gh aw logs --repo owner/repo        # Query logs in another repository
gh aw logs --ignore-workflow-runs 123,456  # Exclude specific run IDs from results
```

**MCP equivalent**: `logs` tool

For failure triage methodology, see [analyzing-runs.md](analyzing-runs.md#select).

---
### `gh aw audit`

Investigate a specific workflow run in detail (missing tools, safe outputs, metrics).

```bash
gh aw audit <run-id>                # Audit a single run
gh aw audit <run-id> --json         # JSON output
gh aw audit <base-id> <compare-id>  # Diff two runs (regression detection)
gh aw audit <id1> <id2> <id3> --json  # Multi-run diff
gh aw audit <id1> <id2> --group     # Grouped findings: one entry per [run, code] with occurrence counts
gh aw audit <id1> <id2> <id3> --group --json  # Per-run finding codes for recurrence checks
gh aw audit <run-id> --search "ERR_AUTH_401 api.example.com" --json  # BM25 evidence with automatic repetition grouping
gh aw audit <run-id> --search "github.issue_read timeout" --search-limit 3
```

For audit triage methodology, see [analyzing-runs.md](analyzing-runs.md#audit).
`--search` is single-run CLI retrieval over the redacted unified session, not
finding generation. Results include bounded excerpts, source references,
neighboring events, and occurrence counts. Drain3 activates automatically only
when repetitive top matches crowd the limit and grouping saves excerpt text;
no configuration is needed. It never changes BM25 ranking, and groups keep
structured success/failure outcomes separate. Cluster/template/anomaly fields
are omitted for ungrouped results. Do not compare cluster IDs across runs or treat
zero matches as safety evidence. It cannot be combined with `--group` or job/step URLs.

**MCP equivalent**: `audit` tool (single run) / `audit-diff` tool (multi-run comparison)
for ordinary audits; BM25 `--search` is CLI-only.

---
### `gh aw status`

Show the status of all agentic workflows in the repository.

```bash
gh aw status
gh aw status --repo owner/repo    # Query status in another repository
```

**MCP equivalent**: `status` tool

---
### `gh aw checks`

Show check run results for a workflow run.

```bash
gh aw checks <run-id>
```

**MCP equivalent**: `checks` tool

---
### `gh aw health`

Display workflow health metrics, success rates, and revision-aware failure clusters.

```bash
gh aw health                                 # Current status of all workflows
gh aw health <workflow-name>                 # Detailed metrics for a specific workflow
gh aw health --days 30 --threshold 90        # Bound lookback; warn below 90% success
gh aw health --json                          # JSON output for programmatic use
gh aw health --start-date -24h --logs-dir ./workflow-logs  # Reuse cached logs, no downloads
```

Reports a bounded current-revision sample: compares `gh-aw-metadata` hashes across
`workflows[].revisions` for `recovery_observed`, and surfaces `failure_clusters`/
`failure_investigation` alongside success/failure counts and trend. See [analyzing-runs.md](analyzing-runs.md) for interpreting the JSON output.

**MCP equivalent**: Not available — run from a local terminal.

---
### `gh aw experiments`

Inspect experiment state tracked in `experiments/*` branches. Default behavior matches `experiments list`; use `experiments analyze` for per-workflow statistics. All subcommands accept `--repo/-r` and `--json/-j`.

```bash
gh aw experiments                             # List experiment workflow branches
gh aw experiments list --json                 # List all experiments as JSON
gh aw experiments analyze <workflow>          # Analyze one experiment workflow
gh aw experiments analyze <workflow> --repo owner/repo  # Analyze in another repository
```

**MCP equivalent**: Not available — run from a local terminal.

---
### `gh aw fix`

Apply automatic codemods to fix deprecated fields in workflow files.

```bash
gh aw fix                   # Preview changes (dry run)
gh aw fix --write           # Apply changes
```

**MCP equivalent**: `fix` tool

---
### `gh aw format`

Apply all available codemods and normalize workflow frontmatter (two-space indentation, deterministic field ordering, comments and Markdown body preserved).

```bash
gh aw format                        # Format all workflows in .github/workflows
gh aw format <workflow-name>        # Format a specific workflow
gh aw format --dir custom/workflows # Format workflows in a custom directory
```

**MCP equivalent**: Not available — run from a local terminal.

---
### `gh aw upgrade`

Upgrade the repository's agentic workflows configuration to the latest gh-aw version.

```bash
gh aw upgrade               # Upgrade agent files + codemods + compile
gh aw upgrade -v            # Verbose output
gh aw upgrade --no-fix      # Skip codemods and compilation
gh aw upgrade --create-pull-request         # Open a PR with the upgrade changes (alias: --pr)
gh aw upgrade --org my-org                  # Preview upgrade PRs across an organization
gh aw upgrade --org my-org --repos '*-service'  # Limit org mode to matching repos
gh aw upgrade --org my-org --create-issue   # Open issues in org repos with agentic workflows (requires --org)
```

**MCP equivalent**: `upgrade` tool

---
### `gh aw add`

Add a new shared workflow component as an import.

```bash
gh aw add <workflow-url>
```

**MCP equivalent**: `add` tool

---
### `gh aw update`

Update imported shared workflow components.

```bash
gh aw update                                # Update all workflows from source
gh aw update <workflow-name>                # Update a specific workflow
gh aw update <package-url-or-name>          # Update all workflows installed from a package
gh aw update --major                        # Allow major version updates
gh aw update --create-pull-request          # Update and open a PR (alias: --pr)
gh aw update --repo owner/repo              # Update workflows in another repository (isolated shallow checkout)
gh aw update --cool-down 3d                 # Custom cooldown before applying pending releases
```

**MCP equivalent**: `update` tool

---
### `gh aw deploy`

Deploy workflows to a target repository (chains update, add, compile --purge, opens a PR). `--repo` is required.

```bash
gh aw deploy <workflow>... --repo owner/repo            # Deploy listed workflows
gh aw deploy githubnext/agentics/ci-doctor --repo o/r   # Deploy a shared workflow
gh aw deploy ./local-workflow.md --repo owner/repo      # Deploy a local workflow
gh aw deploy <workflow> --repo owner/repo --force       # Overwrite without confirmation
```

**MCP equivalent**: Not available — run from a local terminal or invoke the CLI inside a workflow step with `github/gh-aw/actions/setup-cli`.

---
### `gh aw env`

Manage compiler default variables (`GH_AW_DEFAULT_*`) as repo/org/enterprise GitHub Actions variables. YAML file uses lowercase `default_*` keys. `null` deletes the variable; any non-null string sets it (`""` = set-to-empty, not delete).

```bash
gh aw env get [file]                          # Download defaults to file.yml (default name)
gh aw env get --scope org --org myorg         # Org-scope export
gh aw env update file.yml --scope repo        # Apply with interactive confirmation
gh aw env update file.yml --scope ent --enterprise myent --yes  # Skip confirmation
gh aw env update file.yml --scope repo --dry-run  # Preview without applying
```

Example file:

```yaml
default_max_ai_credits: "1000"
default_max_turn_cache_misses: "5"
default_detection_max_ai_credits: "400"
default_max_turns: "12"
default_model_copilot: "gpt-5-mini"
default_model_codex: null   # delete this variable
```

Recognized keys include `default_max_ai_credits`, `default_max_turn_cache_misses`, `default_detection_max_ai_credits`, `default_max_daily_ai_credits`, `default_timeout_minutes`, `default_agent_job_timeout_minutes`, `default_detection_job_timeout_minutes`, `default_max_turns`, `default_detection_model`, `default_utc`, `default_model_copilot`, `default_model_claude`, `default_model_codex`. The compiler resolves model selection as `GH_AW_MODEL_*` → `GH_AW_DEFAULT_MODEL_*` → built-in engine fallback.

**MCP equivalent**: Not available — run from a local terminal.

---
### `gh aw mcp inspect`

Inspect and analyze MCP server configurations in workflows.

```bash
gh aw mcp inspect <workflow-name>
gh aw mcp inspect <workflow-name> --inspector   # Launch web-based inspector UI
gh aw mcp list                                   # List workflows with MCP servers
```

**MCP equivalent**: `mcp-inspect` tool

---
### `gh aw json-schema`

Generate the JSON Schema for `audit`, `logs` JSON output, or cached logs JSONL items.

```bash
gh aw json-schema audit
gh aw json-schema logs
gh aw json-schema logs-jsonl
```

**MCP equivalent**: Not available — run from a local terminal.

---
## MCP Tool ↔ CLI Quick Reference

| CLI command | MCP tool |
|---|---|
| `gh aw status` | `status` |
| `gh aw compile` | `compile` |
| `gh aw run` | Not provided; no API/MCP/manual-dispatch substitution |
| `gh aw logs` | `logs` |
| `gh aw audit` | `audit` |
| `gh aw audit <id1> <id2>` | `audit-diff` |
| `gh aw checks` | `checks` |
| `gh aw health` | *(local only)* |
| `gh aw experiments` | *(local only)* |
| `gh aw mcp inspect` | `mcp-inspect` |
| `gh aw add` | `add` |
| `gh aw update` | `update` |
| `gh aw fix` | `fix` |
| `gh aw format` | *(local only)* |
| `gh aw upgrade` | `upgrade` |
| `gh aw deploy` | *(local only)* |
| `gh aw env` | *(local only)* |
| `gh aw init` | *(local only)* |
| `gh aw json-schema` | *(local only)* |
