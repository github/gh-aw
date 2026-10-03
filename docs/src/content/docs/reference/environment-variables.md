---
title: Environment Variables
description: Reference for all environment variables in GitHub Agentic Workflows — CLI configuration, model overrides, guard policy fallbacks, and workflow-level scope precedence
sidebar:
  order: 650
---

Environment variables in GitHub Agentic Workflows can be defined at multiple scopes, each serving a specific purpose in the workflow lifecycle. Variables defined at more specific scopes override those at more general scopes, following GitHub Actions conventions while adding AWF-specific contexts.

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

### Example Configurations

**Workflow-level shared configuration:**

```yaml wrap
---
env:
  NODE_ENV: production
  API_ENDPOINT: https://api.example.com
---
```

**Job-specific overrides:**

```yaml wrap
---
jobs:
  validation:
    env:
      VALIDATION_MODE: strict
    steps:
      - run: npm run build
        env:
          BUILD_ENV: production  # Overrides job and workflow levels
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

Agents can write markdown content to the `$GITHUB_STEP_SUMMARY` environment variable to publish a formatted summary visible in the GitHub Actions run view.

Inside the AWF sandbox, `$GITHUB_STEP_SUMMARY` is redirected to a file at `/tmp/gh-aw/agent-step-summary.md`. After agent execution completes, the framework automatically appends the contents of that file to the real GitHub step summary. Secret redaction runs before the content is published.

> [!NOTE]
> The first 2000 characters of the summary are appended. If the content is longer, a `[truncated: ...]` notice is included. Write your most important content first.

Example: an agent writing a brief analysis result to the step summary:

```bash
echo "## Analysis complete" >> "$GITHUB_STEP_SUMMARY"
echo "Found 3 issues across 12 files." >> "$GITHUB_STEP_SUMMARY"
```

The output appears in the **Summary** tab of the GitHub Actions workflow run.

## System-Injected Runtime Variables

GitHub Agentic Workflows automatically injects the following environment variables into every agentic engine execution step (both the main agent run and the threat detection run). These variables are read-only from the agent's perspective and are useful for writing workflows or agents that need to detect their execution context.

| Variable | Value | Description |
| ---------- | ------- | ------------- |
| `GITHUB_AW` | `"true"` | Present in every gh-aw engine execution step. Agents can check for this variable to confirm they are running inside a GitHub Agentic Workflow. |
| `GH_AW_PHASE` | `"agent"` or `"detection"` | Identifies which execution phase is active. `"agent"` for the main run; `"detection"` for the threat-detection safety check run that precedes the main run. |
| `GH_AW_VERSION` | e.g. `"0.40.1"` | The gh-aw compiler version that generated the workflow. Useful for conditional logic that depends on a minimum feature version. |

These variables appear alongside other `GH_AW_*` context variables in the compiled workflow:

```yaml
env:
  GITHUB_AW: "true"
  GH_AW_PHASE: agent        # or "detection"
  GH_AW_VERSION: "0.40.1"
  GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt
```

> [!NOTE]
> These variables are injected by the compiler and cannot be overridden by user-defined `env:` blocks in the workflow frontmatter.

## Harness Settings and Runtime Tuning Variables

The built-in Copilot, Claude, and Codex harnesses expose a small set of supported runtime controls. Prefer the structured `engine.harness` frontmatter fields when possible; set the underlying environment variables only when you need a value that is supplied by a GitHub Actions expression or shared across a workflow.

### Shared harness retry settings

These settings apply to the built-in Copilot, Claude, and Codex harnesses.

| Variable | Frontmatter field | Default | Units / range | Description |
| --- | --- | --- | --- | --- |
| `GH_AW_HARNESS_MAX_RETRIES` | `engine.harness.max-retries` | `3` | retry attempts after the initial run; minimum `0`, maximum `100` | Maximum number of harness retries. `0` disables retries. Invalid values use the default; values above `100` are clamped to `100`. |
| `GH_AW_HARNESS_INITIAL_DELAY_MS` | `engine.harness.initial-delay-ms` | `5000` | milliseconds; minimum `1` | Delay before the first retry. Invalid values use the default. |
| `GH_AW_HARNESS_BACKOFF_MULTIPLIER` | `engine.harness.backoff-multiplier` | `2` | decimal multiplier; minimum `1` | Multiplier applied after each retry. Invalid values use the default. |
| `GH_AW_HARNESS_MAX_DELAY_MS` | `engine.harness.max-delay-ms` | `60000` | milliseconds; minimum `1` | Maximum retry delay. Invalid values use the default. If set below `GH_AW_HARNESS_INITIAL_DELAY_MS`, it is clamped up to the initial delay. |

### Shared post-result watchdog

`GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS` configures the post-result stdio inactivity watchdog used by the built-in Copilot, Claude, and Codex harnesses. It is measured in **milliseconds**. The default is `120000` ms (2 minutes), the minimum is `50` ms, and the maximum is `600000` ms (10 minutes). Unset, non-numeric, zero, and negative values use the default; positive values outside the supported range are clamped.

The watchdog is dormant until the agent emits a terminal safe output. `noop` and ordinary task outputs such as comments, labels, pushes, and pull request creation are terminal. Diagnostic safe outputs such as `missing_tool`, `missing_data`, and `report_incomplete` are not terminal and do not arm the watchdog by themselves.

After the watchdog arms, any stdout or stderr activity resets the inactivity clock. A quiet child process can therefore be terminated even while it is doing useful CPU or I/O work, such as a monorepo scan, build, or test command that produces no logs. If the watchdog fires after a terminal safe output already exists, the harness may still treat the run as successful because the requested safe output was already produced.

For literal frontmatter values, use `engine.harness.watchdog-timeout` in seconds. For raw environment variables, use milliseconds:

```yaml wrap
---
env:
  # Allow quiet monorepo scans and builds after an intermediate safe output.
  # Workflow-level env is visible to the agent; do not put secrets here.
  GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "600000"
---
```

```yaml wrap
---
engine:
  id: copilot
  harness:
    # Same timeout expressed as a literal frontmatter value in seconds.
    watchdog-timeout: 600
---
```

### Shared stall watchdog

`GH_AW_HARNESS_STALL_WARNING_MS` configures the driver-level stall watchdog shared by all built-in harnesses. It is measured in **milliseconds**. The default is `300000` ms (5 minutes), the minimum is `1000` ms, and the maximum is `3600000` ms (1 hour). Unset and non-numeric values use the default; positive values outside the supported range are clamped; zero or negative values disable the warnings.

Unlike the post-result watchdog, the stall watchdog never terminates the agent process. Whenever the agent CLI produces no stdout or stderr output for the configured interval, the harness logs a warning in the `Execute ... CLI` step, for example:

```text wrap
[copilot-harness] attempt 1: stall watchdog: no output from '/usr/bin/copilot' for 5m 0s (elapsed=5m 1s pid=1234 warnings=1) - the step may be hung; GitHub Actions will cancel this step in about 14m 59s (timeout-minutes=20)
```

The warning repeats on each interval while the silence continues, and a `stall watchdog: output resumed after ...` line is logged once output comes back. This makes a hung step diagnosable from the step log alone, without cross-referencing job or step metadata. The final `process closed` line reports `stallWarnings=<count>` when any warning fired.

### Engine-specific harness settings

| Variable | Engine | Default | Units / range | Description |
| --- | --- | --- | --- | --- |
| `GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD` | Copilot | `10000` | tokens; minimum `0` | Token threshold used to classify long-running partial executions as `long_run_exit` instead of a generic partial execution. Invalid or negative values use the default. |
| `GH_AW_HARNESS_STARTUP_RETRIES` | All engine harnesses | `1` | retry attempts; range `0`-`2` | Additional fresh-run retry budget for startup failures before the harness records session progress. Invalid values use the default; out-of-range integers are clamped. `GH_AW_CLAUDE_STARTUP_RETRIES` is still accepted as a Claude-compatible fallback when the shared variable is unset. |
| `GH_AW_CLAUDE_ALLOW_FRESH_RESTART` | Claude harness | `false` | exact string `true` enables | Opts replay-safe workflows into restarting from the original prompt after partial execution cannot be resumed. Filesystem changes and staged outputs are retained, so work may be repeated. |

Copilot SDK driver settings such as `COPILOT_SDK_SEND_TIMEOUT_MS` are documented in [Copilot SDK Support](/gh-aw/reference/engines/#copilot-sdk-support) and the [Copilot SDK Driver Specification](/gh-aw/specs/copilot-sdk-driver-specification/).

### Internal runtime variables

`GH_AW_TIMEOUT_MINUTES` is compiler-managed. gh-aw derives it from the workflow `timeout-minutes` frontmatter value and passes it to harness and driver code so soft timeouts and SDK send timeouts stay below the GitHub Actions job timeout. Do not set `GH_AW_TIMEOUT_MINUTES` directly; set `timeout-minutes` in frontmatter instead.

`GH_AW_CLAUDE_DISABLE_REPO_EDITS` is also compiler-managed. gh-aw sets it when a workflow specifies `tools.edit: false`, instructing the Claude runtime to deny workspace writes even when a more permissive permission mode is selected. Do not set this variable directly; configure `tools.edit` in workflow frontmatter.

### Setup helper process timeouts

The JavaScript setup helpers bound child processes, archive operations, and setup-time network requests with positive millisecond timeouts. These defaults protect workflows from indefinitely hung setup commands, but very large repositories or artifact payloads can require larger budgets. Set the relevant variable to a positive integer number of milliseconds; unset, zero, negative, or non-numeric values use the default. Values above Node's maximum timer delay of `2147483647` ms (about 24.8 days) are clamped to that maximum.

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
| `GH_AW_OUTCOME_GH_TIMEOUT_MS` | `300000` | `gh` CLI calls made by outcome evaluation. |
| `GH_AW_SAFEOUTPUTS_CLI_TIMEOUT_MS` | `120000` | `safeoutputs` CLI invocations used for structured diagnostics. |

### MCP gateway readiness polling

The MCP gateway startup step polls the gateway `/health` endpoint with exponential backoff until the gateway reports ready. Each retry parameter can be overridden; unset, zero, negative, or non-numeric values fall back to the default with a warning.

| Variable | Default | Applies to |
| --- | --- | --- |
| `GH_AW_MCP_GATEWAY_HEALTH_MAX_ATTEMPTS` | `150` | Total health-check attempts, including the first one. |
| `GH_AW_MCP_GATEWAY_HEALTH_INITIAL_DELAY_MS` | `250` | Delay the backoff starts from. |
| `GH_AW_MCP_GATEWAY_HEALTH_MAX_DELAY_MS` | `1000` | Cap applied to every retry delay. |
| `GH_AW_MCP_GATEWAY_HEALTH_BACKOFF_MULTIPLIER` | `2` | Multiplier applied to the delay before every retry. |
| `GH_AW_MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_MS` | `120000` | MCP backend startup timeout the retry budget must outlast. |

These values are cross-validated at startup:

- A backoff multiplier below `1` is raised to `1` (constant delay).
- A delay cap below the initial delay is raised to the initial delay.
- The cumulative retry delay must cover the backend startup timeout plus 25 seconds for backend cleanup and final server registration; otherwise a warning is emitted that the health check may give up before the gateway is ready.

## CLI Configuration Variables

These variables configure the `gh aw` CLI tool. Set them in your local shell environment or as repository/organization variables in GitHub Actions.

| Variable | Default | Description |
| --- | --- | --- |
| `DEBUG` | disabled | npm-style namespace debug logging. `DEBUG=*` enables all output; `DEBUG=cli:*,workflow:*` selects specific namespaces. Exclusions are supported: `DEBUG=*,-workflow:test`. Also activated when `ACTIONS_RUNNER_DEBUG=true`. |
| `DEBUG_COLORS` | `1` (enabled) | Set to `0` to disable ANSI colors in debug output. Colors are automatically disabled when output is not a TTY. |
| `ACCESSIBLE` | empty | Any non-empty value enables accessibility mode, which disables spinners and animations. Also enabled when `TERM=dumb` or `NO_COLOR` is set. |
| `NO_COLOR` | empty | Any non-empty value disables colored output and enables accessibility mode. Follows the [no-color.org](https://no-color.org/) standard. |
| `GH_AW_ACTION_MODE` | auto-detected | Overrides how JavaScript is embedded in compiled workflows. Valid values: `dev`, `release`, `script`, `action`. When unset, the CLI auto-detects the appropriate mode. |
| `GH_AW_FEATURES` | empty | Comma-separated list of experimental feature flags to enable globally. Values in workflow `features:` frontmatter take precedence over this variable. |
| `GH_AW_MAX_CONCURRENT_DOWNLOADS` | `10` | Maximum number of parallel log and artifact downloads for `gh aw logs`. Valid range: `1`–`100`. |
| `GH_AW_MCP_SERVER` | unset | When set, disables the automatic update check. Set automatically when `gh aw` runs as an MCP server subprocess — no manual configuration needed. |

**Enabling debug logging:**

```bash
# All namespaces
DEBUG=* gh aw compile

# Specific namespaces
DEBUG=cli:*,workflow:* gh aw compile

# Without colors
DEBUG_COLORS=0 DEBUG=* gh aw compile
```

---

## Model Override Variables

These variables override the default AI model used for agent runs and threat detection. Set them as GitHub Actions repository or organization variables to apply org-wide defaults without modifying workflow frontmatter.

> [!NOTE]
> The `engine.model:` field in workflow frontmatter takes precedence over these variables.

### Compiler-managed default behavior

Model defaults now use two different resolution paths:

- **Compiler process environment (compile time):**
  `GH_AW_DEFAULT_DETECTION_MODEL`
- **GitHub `vars.*` expressions (runtime in compiled workflow):**
  `GH_AW_DEFAULT_MODEL_COPILOT`,
  `GH_AW_DEFAULT_MODEL_CLAUDE`,
  `GH_AW_DEFAULT_MODEL_CODEX`

At compile time, gh-aw emits runtime model expressions like:

```yaml
COPILOT_MODEL: ${{ vars.GH_AW_MODEL_AGENT_COPILOT || vars.GH_AW_DEFAULT_MODEL_COPILOT || '<engine default model>' }}
```

Use `gh aw env get` / `gh aw env update` to batch-manage
these `GH_AW_DEFAULT_*` variables at repo, org, or enterprise scope with
`default_`-prefixed YAML keys such as `default_max_ai_credits`,
`default_max_turn_cache_misses`,
`default_detection_max_ai_credits`, `default_max_daily_ai_credits`, and `default_model_copilot`.

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
| `GH_AW_MODEL_DETECTION_GEMINI` | Google Gemini |

Set a model override as an organization variable:

```bash
gh variable set GH_AW_MODEL_AGENT_COPILOT --org my-org --body "gpt-5"
```

See [Engines](/gh-aw/reference/engines/) for available engine identifiers and model configuration options.

---

## Guard Policy Fallback Variables

These variables provide fallback values for guard policy fields when the corresponding `tools.github.*` configuration is absent from workflow frontmatter. Set them as GitHub Actions organization or repository variables to enforce a consistent policy across all workflows.

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

These variables enforce runtime policy decisions in compiled workflows without requiring recompilation.

| Variable | Default | Description |
| --- | --- | --- |
| `GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST` | unset (allow) | Disables `safe-outputs.create-pull-request` at runtime when set to `"false"`. Any other value — including unset — leaves the tool enabled. Set to `"false"` to block PR creation without recompiling the workflow. |

---

## Precedence Rules

Environment variables follow a **most-specific-wins** model, consistent with GitHub Actions. Variables at more specific scopes completely override variables with the same name at less specific scopes.

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

Variables under `sandbox.mcp.env` configure the MCP gateway process, but they are not injected into the startup shell script as raw `export NAME=VALUE` lines. Instead, gh-aw transports them through compiler-controlled step environment variables and reconstructs the final gateway container `-e NAME=VALUE` arguments at runtime. This keeps values out of shell interpolation paths and avoids command-injection hazards from special characters.

Names in `sandbox.mcp.env` must match `^[A-Z_][A-Z0-9_]*$`. The internal `GH_AW_MCP_GATEWAY_` namespace is reserved for gh-aw transport metadata and cannot be used for custom variables.

```yaml wrap
sandbox:
  mcp:
    env:
      DEBUG: "1"
      LOG_LEVEL: trace
```

Use `sandbox.mcp.env` for gateway-facing configuration only. For MCP server credentials or per-tool settings, prefer `tools.<name>.env` or `mcp-scripts.<name>.env`.

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
- [Governance Guide](/gh-aw/reference/governance/) - Roll out and manage defaults across enterprise, organization, and repository scopes
- [Compiler Enterprise Environment Controls](/gh-aw/reference/compiler-enterprise-environment-controls/) - Enterprise defaults for timeout, max-turns, detection model, model fallback, and max-ai-credits guardrails
- [Cost Management](/gh-aw/reference/cost-management/) - Practical model and token guardrail rollout guidance
- [Tools](/gh-aw/reference/tools/) - MCP tool configuration and guard policies
- [GitHub Actions Environment Variables](https://docs.github.com/en/actions/learn-github-actions/variables) - GitHub Actions documentation
