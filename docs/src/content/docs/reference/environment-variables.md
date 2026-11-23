---
title: Environment Variables
description: Reference for all environment variables in GitHub Agentic Workflows — CLI configuration, model overrides, guard policy fallbacks, and workflow-level scope precedence
sidebar:
  order: 650
---

Environment variables in GitHub Agentic Workflows can be set at several scopes. More specific scopes override more general ones, following GitHub Actions conventions, with added AWF-specific contexts.

## Environment Variable Scopes

GitHub Agentic Workflows supports environment variables in 13 distinct contexts:

| Scope | Syntax | Context | Typical Use |
| ------- | -------- | --------- | ------------- |
| **Workflow-level** | `env:` | All jobs | Shared configuration |
| **Job-level** | `jobs.<job_id>.env` | All steps in job | Job-specific config |
| **Step-level** | `steps[*].env` | Single step | Step-specific config |
| **Engine** | `engine.env` | AI engine | Engine secrets, timeouts |
| **Container** | `container.env` | Container runtime | Container settings |
| **Services** | `services.<id>.env` | Service containers | Database credentials |
| **Sandbox Agent** | `sandbox.agent.env` | Sandbox runtime | Sandbox configuration |
| **Sandbox MCP** | `sandbox.mcp.env` | Model Context Protocol (MCP) gateway | MCP gateway configuration |
| **MCP Tools** | `tools.<name>.env` | MCP server process | MCP server secrets |
| **MCP Scripts** | `mcp-scripts.<name>.env` | MCP script execution | Tool-specific tokens |
| **Safe Outputs Global** | `safe-outputs.env` | All safe-output jobs | Shared safe-output config |
| **Safe Outputs Job** | `safe-outputs.jobs.<name>.env` | Specific safe-output job | Job-specific config |
| **GitHub Actions Step** | `githubActionsStep.env` | Pre-defined steps | Step configuration |

In strict mode, credential expressions in workflow-level `env` are rejected. This covers `secrets` references, `github.token` (dot or bracket notation), whole-context `github` references, and wildcard projections such as `github.*` or `toJSON(github.*)`. Non-strict mode emits a warning instead. The same checks apply to non-exempt keys in `engine.env`. Supported credential keys in `engine.env` remain allowed; strict mode requires AWF v0.25.3 or newer to exclude them from the agent sandbox.

Invalid expressions in `env` or `engine.env` fail compilation in either mode. Actions bracket indexes use single-quoted strings, for example `github['repository']`; double-quoted indexes are invalid.

### Example Configurations

**Workflow-level shared configuration:**

```yaml wrap
---
env:
  NODE_ENV: production
  API_ENDPOINT: https://api.example.com
---
```

**AWF-specific contexts:**

```yaml wrap
---
# Engine configuration
engine:
  id: copilot
  env:
    OPENAI_API_KEY: ${{ secrets.CUSTOM_KEY }}

# MCP server with secrets
tools:
  database:
    command: npx
    args: ["-y", "mcp-server-postgres"]
    env:
      DATABASE_URL: ${{ secrets.DATABASE_URL }}

# Safe outputs with custom PAT
safe-outputs:
  create-issue:
  env:
    GITHUB_TOKEN: ${{ secrets.CUSTOM_PAT }}
---
```

## Agent Step Summary (`GITHUB_STEP_SUMMARY`)

Agents can write markdown to `$GITHUB_STEP_SUMMARY` to publish a summary in the GitHub Actions run view.

Inside the AWF sandbox, `$GITHUB_STEP_SUMMARY` points to `/tmp/gh-aw/agent-step-summary.md`. After the agent finishes, the framework appends that file to the real step summary, applying secret redaction first.

> [!NOTE]
> The first 2000 characters of the summary are appended. If the content is longer, a `[truncated: ...]` notice is included. Write your most important content first.

Example:

```bash
echo "## Analysis complete" >> "$GITHUB_STEP_SUMMARY"
echo "Found 3 issues across 12 files." >> "$GITHUB_STEP_SUMMARY"
```

The output appears in the **Summary** tab of the GitHub Actions workflow run.

## System-Injected Runtime Variables

The compiler injects these variables into every engine execution step, both the main agent run and the threat-detection run. Agents can use them to detect their execution context. They are read-only from the agent's perspective.

| Variable | Value | Description |
| ---------- | ------- | ------------- |
| `GITHUB_AW` | `"true"` | Set in every engine execution step. Check it to confirm the process runs inside a GitHub Agentic Workflow. |
| `GH_AW_PHASE` | `"agent"` or `"detection"` | Current phase: `"agent"` for the main run, `"detection"` for the threat-detection check that runs first. |
| `GH_AW_VERSION` | e.g. `"0.40.1"` | Compiler version that generated the workflow. Useful for minimum-version checks. |

> [!NOTE]
> These variables are injected by the compiler and cannot be overridden by user-defined `env:` blocks in the workflow frontmatter.

## Harness Settings and Runtime Tuning Variables

The built-in Copilot, Claude, and Codex harnesses expose a small set of runtime controls. Prefer the structured `engine.harness` frontmatter fields. Set the environment variables directly only when a value comes from a GitHub Actions expression or must be shared across a workflow.

### Shared harness retry settings

| Variable | Frontmatter field | Default | Units / range | Description |
| --- | --- | --- | --- | --- |
| `GH_AW_HARNESS_MAX_RETRIES` | `engine.harness.max-retries` | `3` | retry attempts after the initial run; minimum `0`, maximum `100` | Maximum retries. `0` disables retries. Invalid values use the default; values above `100` are clamped. |
| `GH_AW_HARNESS_INITIAL_DELAY_MS` | `engine.harness.initial-delay-ms` | `5000` | milliseconds; minimum `1` | Delay before the first retry. Invalid values use the default. |
| `GH_AW_HARNESS_BACKOFF_MULTIPLIER` | `engine.harness.backoff-multiplier` | `2` | decimal multiplier; minimum `1` | Multiplier applied after each retry. Invalid values use the default. |
| `GH_AW_HARNESS_MAX_DELAY_MS` | `engine.harness.max-delay-ms` | `60000` | milliseconds; minimum `1` | Maximum exponential backoff delay; does not cap provider `Retry-After` waits, which have a separate 60-second fallback limit if the timeout is unset or invalid. Invalid values use the default. Values below `GH_AW_HARNESS_INITIAL_DELAY_MS` are clamped up to it. |

### Shared post-result watchdog

`GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS` sets the post-result stdio inactivity watchdog for the built-in Copilot, Claude, and Codex harnesses, in **milliseconds**. The default is `120000` (2 minutes), the minimum is `50`, and the maximum is `600000` (10 minutes). Unset, non-numeric, zero, and negative values use the default; positive values outside the range are clamped.

The watchdog stays dormant until the agent emits a terminal safe output. `noop` and ordinary task outputs (comments, labels, pushes, pull request creation) are terminal. Diagnostic outputs (`report_missing_tool`, `report_missing_data`, `report_incomplete`) are not terminal and do not arm it.

Once armed, any stdout or stderr activity resets the clock. A quiet process doing useful CPU or I/O work, such as a silent monorepo scan or build, can therefore be terminated. If it fires after a terminal output already exists, the harness may still count the run as successful.

To allow quiet work after an intermediate safe output, set the variable in workflow-level `env:`. The agent can read workflow-level `env`, so do not put secrets there:

```yaml wrap
---
env:
  GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "600000"
---
```

In frontmatter, the same timeout is `engine.harness.watchdog-timeout: 600` (seconds).

### Shared stall watchdog

`GH_AW_HARNESS_STALL_WARNING_MS` sets the driver-level stall watchdog shared by all built-in harnesses, in **milliseconds**. The default is `300000` (5 minutes), the minimum is `1000`, and the maximum is `3600000` (1 hour). Unset and non-numeric values use the default; out-of-range positive values are clamped; zero or negative values disable warnings.

Unlike the post-result watchdog, the stall watchdog never terminates the agent. When the agent CLI produces no stdout or stderr for the interval, the harness logs a warning in the `Execute ... CLI` step, for example:

```text wrap
[copilot-harness] attempt 1: stall watchdog: no output from '/usr/bin/copilot' for 5m 0s (elapsed=5m 1s pid=1234 warnings=1) - the step may be hung; GitHub Actions will cancel this step in about 14m 59s (timeout-minutes=20)
```

Warnings repeat each interval while the silence continues. A `stall watchdog: output resumed after ...` line is logged when output returns, so a hung step can be diagnosed from its log alone. The final `process closed` line reports `stallWarnings=<count>` if any warning fired.

### Engine-specific harness settings

| Variable | Engine | Default | Units / range | Description |
| --- | --- | --- | --- | --- |
| `GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD` | Copilot | `10000` | tokens; minimum `0` | Token threshold for classifying a partial execution as `long_run_exit` rather than a generic partial. Invalid or negative values use the default. |
| `GH_AW_HARNESS_STARTUP_RETRIES` | All engine harnesses | `1` | retry attempts; range `0`-`2` | Extra fresh-run retries for startup failures, before session progress is recorded. Invalid values use the default; out-of-range integers are clamped. `GH_AW_CLAUDE_STARTUP_RETRIES` is a fallback when this variable is unset. |
| `GH_AW_CLAUDE_ALLOW_FRESH_RESTART` | Claude harness | `false` | exact string `true` enables | Lets replay-safe workflows restart from the original prompt when a partial run cannot resume. Filesystem changes and staged outputs are kept, so work may repeat. |
| `CLAUDE_CODE_MAX_RETRIES` | Claude CLI | `0` for primary/evals execution | retry attempts | Native CLI transient-error retries. gh-aw disables them by default because its harness owns the retry loop. Explicit `engine.env` values override this; external detection keeps the CLI default. |

Copilot SDK driver settings such as `COPILOT_SDK_SEND_TIMEOUT_MS` are documented in [Copilot SDK Support](/gh-aw/reference/engines/copilot/#copilot-sdk-support) and the [Copilot SDK Driver Specification](/gh-aw/specs/copilot-sdk-driver-specification/).

### Internal runtime variables

`GH_AW_TIMEOUT_MINUTES` is compiler-managed. It is derived from the workflow `timeout-minutes` frontmatter value so soft timeouts and SDK send timeouts stay below the Actions job timeout. Set `timeout-minutes` in frontmatter instead of this variable.

`GH_AW_CLAUDE_DISABLE_REPO_EDITS` is compiler-managed. It is set to `true` when `tools.edit: false`, overriding `engine.env` values. The built-in Claude harness then denies `Edit` within `GITHUB_WORKSPACE` and removes `Write`, `MultiEdit`, and `NotebookEdit` globally, even under a permission-mode override. Scoped memory edits remain available through `Edit`. Configure `tools.edit` rather than setting this variable. Custom harnesses must implement their own edit policy.

### Setup helper process timeouts

The JavaScript setup helpers time out child processes, archive operations, and setup-time network requests. Large repositories or artifacts may need bigger budgets. Set a positive integer in milliseconds; unset, zero, negative, or non-numeric values use the default. Values above Node's maximum timer delay (`2147483647` ms, about 24.8 days) are clamped to it.

| Variable | Default | Applies to |
| --- | --- | --- |
| `GH_AW_APPLY_SAMPLES_FETCH_TIMEOUT_MS` | `120000` | GitHub API fetches performed by `apply_samples.cjs`. |
| `GH_AW_APPLY_SAMPLES_GIT_TIMEOUT_MS` | `120000` | Git commands used while replaying sample patches. |
| `GH_AW_ARTIFACT_ARCHIVE_PROBE_TIMEOUT_MS` | `15000` | `zip -v` and `unzip -v` availability probes. |
| `GH_AW_ARTIFACT_ARCHIVE_TIMEOUT_MS` | `300000` | `zip` and `unzip` archive creation/extraction. |
| `GH_AW_ARTIFACT_FETCH_TIMEOUT_MS` | `120000` | Artifact service metadata and redirect requests. |
| `GH_AW_ARTIFACT_TRANSFER_TIMEOUT_MS` | `300000` | Artifact blob upload and download transfers. |
| `GH_AW_GIT_BRANCH_TIMEOUT_MS` | `15000` | Current branch detection via `git rev-parse`. |
| `GH_AW_IMPORT_GIT_TIMEOUT_MS` | `300000` | Sparse-checkout/import git commands for remote `.github` content. |
| `GH_AW_MCP_CONFIG_CONVERTER_TIMEOUT_MS` | `120000` | MCP configuration converter subprocess. |
| `GH_AW_MCP_CONTAINER_STATUS_TIMEOUT_MS` | `15000` | Docker/container status probes used in MCP gateway diagnostics. |
| `GH_AW_MCP_DOCKER_CLEANUP_TIMEOUT_MS` | `30000` | Stale MCP gateway container cleanup. |
| `GH_AW_MCP_SERVER_CHECK_TIMEOUT_MS` | `120000` | MCP server health-check script. |
| `GH_AW_ADOPTION_GH_TIMEOUT_MS` | `300000` | `gh` CLI calls made by outcome evaluation. |
| `GH_AW_SAFEOUTPUTS_CLI_TIMEOUT_MS` | `120000` | `safeoutputs` CLI invocations used for structured diagnostics. |

### MCP gateway readiness polling

The MCP gateway startup step polls `/health` with exponential backoff until the gateway is ready. Unset, zero, negative, or non-numeric values fall back to the default with a warning.

| Variable | Default | Applies to |
| --- | --- | --- |
| `GH_AW_MCP_GATEWAY_HEALTH_MAX_ATTEMPTS` | At least `150`, automatically sized up to `10000` | Total health-check attempts, including the first one. |
| `GH_AW_MCP_GATEWAY_HEALTH_INITIAL_DELAY_MS` | `250` | Delay the backoff starts from. |
| `GH_AW_MCP_GATEWAY_HEALTH_MAX_DELAY_MS` | `1000` | Cap applied to every retry delay. |
| `GH_AW_MCP_GATEWAY_HEALTH_BACKOFF_MULTIPLIER` | `2` | Multiplier applied to the delay before every retry. |
| `GH_AW_MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_MS` | `120000` | MCP backend startup timeout the retry budget must outlast. |

These values are cross-validated at startup:

- A backoff multiplier below `1` is raised to `1` (constant delay).
- A delay cap below the initial delay is raised to the initial delay.
- Unless a valid attempt count is set explicitly, the count grows so cumulative retry delay covers the backend startup timeout (including `tools.startup-timeout`) plus 25 seconds for cleanup and registration. Explicit and auto-sized counts are capped at `10000`; an insufficient budget emits a warning. Health and readiness probes share the startup deadline, so more attempts do not extend it.

## CLI Configuration Variables

These variables configure the `gh aw` CLI tool. Set them in your local shell environment or as repository/organization variables in GitHub Actions.

| Variable | Default | Description |
| --- | --- | --- |
| `DEBUG` | disabled | npm-style namespace debug logging. `DEBUG=*` enables all output; `DEBUG=cli:*,workflow:*` selects specific namespaces. Exclusions are supported: `DEBUG=*,-workflow:test`. Also activated when `ACTIONS_RUNNER_DEBUG=true` or `RUNNER_DEBUG=1`. |
| `DEBUG_COLORS` | `1` (enabled) | Set to `0` to disable ANSI colors in debug output. Colors are automatically disabled when output is not a TTY. |
| `ACCESSIBLE` | empty | Any non-empty value enables accessibility mode, which disables spinners and animations. Also enabled when `TERM=dumb` or `NO_COLOR` is set. |
| `NO_COLOR` | empty | Any non-empty value disables colored output and enables accessibility mode. Follows the [no-color.org](https://no-color.org/) standard. |
| `GH_AW_ACTION_MODE` | auto-detected | Overrides how JavaScript is embedded in compiled workflows. Valid values: `dev`, `release`, `script`, `action`. When unset, the CLI auto-detects the appropriate mode. |
| `GH_AW_FEATURES` | empty | Comma-separated list of experimental feature flags to enable globally. Values in workflow `features:` frontmatter take precedence over this variable. |
| `GH_AW_MAX_CONCURRENT_DOWNLOADS` | `10` | Maximum number of parallel log and artifact downloads for `gh aw logs`. Valid range: `1`–`100`. |
| `GH_AW_MCP_SERVER` | unset | Disables the automatic update check. Set automatically when `gh aw` runs as an MCP server subprocess. |

**Enabling debug logging:**

Compiled workflows leave MCP debug logging off for normal runs. GitHub Actions debug mode (`runner.debug == '1'`, including **Re-run jobs** with **Enable debug logging**) turns on verbose MCP logs automatically. An explicit workflow or job `DEBUG` value takes precedence and is forwarded to the gateway and built-in MCP servers.

The built-in JavaScript MCP loggers use server names as namespaces (`safeoutputs`, `mcpscripts`). Patterns are comma- or whitespace-separated, support `*`, and let `-` exclusions win regardless of order. For example, `DEBUG=*,-safeoutputs` removes safe-outputs diagnostics from stderr and `server.log`. Error summaries, required audit records, and structured safe-output files are unaffected; error stack traces need a matching namespace.

```bash
# All namespaces
DEBUG=* gh aw compile

# Without colors
DEBUG_COLORS=0 DEBUG=* gh aw compile
```

---

## Model Override Variables

Set these repository or organization variables to change the default agent and threat-detection model without editing frontmatter.

> [!NOTE]
> The `engine.model:` field in workflow frontmatter takes precedence over these variables.

### Compiler-managed default behavior

Model defaults resolve through two paths: `GH_AW_DEFAULT_DETECTION_MODEL` at compile time, and `GH_AW_DEFAULT_MODEL_COPILOT`, `GH_AW_DEFAULT_MODEL_CLAUDE`, and `GH_AW_DEFAULT_MODEL_CODEX` as runtime `vars.*` expressions. At compile time, gh-aw emits expressions such as:

```yaml
COPILOT_MODEL: ${{ vars.GH_AW_MODEL_AGENT_COPILOT || vars.GH_AW_DEFAULT_MODEL_COPILOT || '<engine default model>' }}
```

Use `gh aw env get` and `gh aw env update` to manage these `GH_AW_DEFAULT_*` variables in bulk at repo, org, or enterprise scope. Their `default_`-prefixed YAML keys include `default_max_ai_credits`, `default_max_turn_cache_misses`, `default_detection_max_ai_credits`, `default_max_daily_ai_credits`, and `default_model_copilot`.

OTLP metrics are on by default when configured. Set `default_otel_metrics: false` in the `gh aw env` defaults file to disable them at a scope; delete the variable to restore the default. A workflow-level `GH_AW_OTEL_METRICS` value takes precedence.

### Agent runs

| Variable | Engine |
| --- | --- |
| `GH_AW_MODEL_AGENT_COPILOT` | GitHub Copilot |
| `GH_AW_MODEL_AGENT_CLAUDE` | Anthropic Claude |
| `GH_AW_MODEL_AGENT_CODEX` | OpenAI Codex |
| `GH_AW_MODEL_AGENT_GEMINI` | Google Gemini |
| `GH_AW_MODEL_AGENT_CUSTOM` | Custom engine |

### Detection runs

| Variable | Engine |
| --- | --- |
| `GH_AW_MODEL_DETECTION_COPILOT` | GitHub Copilot |
| `GH_AW_MODEL_DETECTION_CLAUDE` | Anthropic Claude |
| `GH_AW_MODEL_DETECTION_CODEX` | OpenAI Codex |

For inline and external detection, an explicit `safe-outputs.threat-detection.engine.model` overrides the inherited workflow model. Otherwise, detection resolves in this order: compile-time `GH_AW_DEFAULT_DETECTION_MODEL`, then `vars.GH_AW_MODEL_DETECTION_<ENGINE>`, then `vars.GH_AW_DEFAULT_MODEL_<ENGINE>`, then the engine's built-in detection default. The detection engine can differ from the main engine, and these variables follow it.

Claude and Codex have no engine-specific detection default, so they use the firewall's `detection` alias. That alias is resolved at runtime and is not pinned. Copilot's built-in fallback is cost-optimized (currently a mini model). To pin a specific model, set an explicit detection model or a model override variable. This does not change the [threat detection failure policy](/gh-aw/reference/threat-detection/).

Set a model override as an organization variable:

```bash
gh variable set GH_AW_MODEL_AGENT_COPILOT --org my-org --body "gpt-5"
```

See [Engines](/gh-aw/reference/engines/) for available engine identifiers and model configuration options.

---

## Guard Policy Fallback Variables

These variables supply fallback values for `tools.github.*` guard policy fields that frontmatter does not set. Use organization or repository variables for a consistent policy across workflows.

> [!NOTE]
> Explicit `tools.github.*` values in workflow frontmatter always take precedence over these variables.

| Variable | Frontmatter field | Format | Description |
| --- | --- | --- | --- |
| `GH_AW_GITHUB_BLOCKED_USERS` | `tools.github.blocked-users` | Comma- or newline-separated usernames | GitHub usernames blocked from triggering agent runs |
| `GH_AW_GITHUB_APPROVAL_LABELS` | `tools.github.approval-labels` | Comma- or newline-separated label names | Labels that promote content to "approved" integrity for guard checks |
| `GH_AW_GITHUB_TRUSTED_USERS` | `tools.github.trusted-users` | Comma- or newline-separated usernames | GitHub usernames elevated to "approved" integrity, bypassing guard checks |

Set an org-wide blocked user list:

```bash
gh variable set GH_AW_GITHUB_BLOCKED_USERS --org my-org --body "bot-account1,bot-account2"
```

See [Tools Reference](/gh-aw/reference/tools/) for complete guard policy documentation.

---

## Runtime Policy Variables

These variables enforce policy at runtime, without recompiling workflows.

| Variable | Default | Description |
| --- | --- | --- |
| `GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST` | unset (allow) | Disables `safe-outputs.create-pull-request` at runtime when set to `"false"`. Any other value — including unset — leaves the tool enabled. Set to `"false"` to block PR creation without recompiling the workflow. |

---

## Precedence Rules

Environment variables follow a **most-specific-wins** model, as in GitHub Actions. A more specific scope fully overrides a less specific one with the same name.

### General Precedence (Highest to Lowest)

1. **Step-level** (`steps[*].env`, `githubActionsStep.env`)
2. **Job-level** (`jobs.<job_id>.env`)
3. **Workflow-level** (`env:`)

### Safe Outputs Precedence

1. **Job-specific** (`safe-outputs.jobs.<job_name>.env`)
2. **Global** (`safe-outputs.env`)
3. **Workflow-level** (`env:`)

### Context-Specific Scopes

These scopes are independent and operate in different contexts: `engine.env`, `container.env`, `services.<id>.env`, `sandbox.agent.env`, `sandbox.mcp.env`, `tools.<tool>.env`, `mcp-scripts.<tool>.env`.

### `sandbox.mcp.env` validation and transport

Variables under `sandbox.mcp.env` configure the MCP gateway process. They are not written into the startup script as raw `export` lines. gh-aw passes them through compiler-controlled step variables and rebuilds the gateway's `-e NAME=VALUE` arguments at runtime, which keeps values out of shell interpolation and avoids command-injection risks.

Names must match `^[A-Z_][A-Z0-9_]*$`. The `GH_AW_MCP_GATEWAY_` prefix is reserved for gh-aw transport metadata and cannot be used for custom variables.

```yaml wrap
sandbox:
  mcp:
    env:
      DEBUG: "1"
      LOG_LEVEL: trace
```

Use `sandbox.mcp.env` only for gateway-facing settings. For MCP server credentials or per-tool settings, prefer `tools.<name>.env` or `mcp-scripts.<name>.env`.

### Override Example

```yaml wrap
---
env:
  API_KEY: default-key
  DEBUG: "false"

jobs:
  test:
    env:
      API_KEY: test-key    # Overrides workflow-level
      EXTRA: "value"
    steps:
      - run: |
          # API_KEY = "test-key" (job-level override)
          # DEBUG = "false" (workflow-level inherited)
          # EXTRA = "value" (job-level)
---
```

## Learn More

- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - Complete frontmatter configuration
- [Governance Guide](/gh-aw/reference/governance/) - Rolling out defaults at enterprise, organization, and repository scope
- [Compiler Enterprise Environment Controls](/gh-aw/reference/compiler-enterprise-environment-controls/) - Enterprise guardrails for timeouts, turns, detection model, and AI credits
- [Cost Management](/gh-aw/reference/cost-management/) - Practical model and token guardrail rollout guidance
- [Tools](/gh-aw/reference/tools/) - MCP tool configuration and guard policies
- [GitHub Actions Environment Variables](https://docs.github.com/en/actions/learn-github-actions/variables) - GitHub Actions documentation
