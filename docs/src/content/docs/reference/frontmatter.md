---
title: Frontmatter
description: Complete guide to all available frontmatter configuration options for GitHub Agentic Workflows, including triggers, permissions, AI engines, and workflow settings.
sidebar:
  order: 200
---

The [frontmatter](/gh-aw/reference/glossary/#frontmatter) (YAML configuration section between `---` markers) of GitHub Agentic Workflows includes the triggers, permissions, AI [engines](/gh-aw/reference/glossary/#engine) (which AI model/provider to use), and workflow settings. For example:

```yaml wrap
---
on:
  issues:
    types: [opened]

tools:
  edit:
  bash: ["gh issue comment"]
---
...markdown instructions...
```

## Frontmatter Elements

Below is a comprehensive reference to all available frontmatter fields for GitHub Agentic Workflows.

### Description (`description:`)

Provides a human-readable description of the workflow rendered as a comment in the generated lock file.

```yaml wrap
description: "Workflow that analyzes pull requests and provides feedback"
```

### Intent (`intent:`)

Describes the durable outcome the workflow exists to achieve, rendered as a comment in the generated lock file. While `description` explains *what* the workflow does, `intent` explains *why* it exists and should stay implementation-independent, so it remains valid when the implementation changes.

```yaml wrap
intent: "Reduce maintainer attention spent identifying recurring CI regressions."
description: "Analyzes failed CI runs and opens incidents for novel regressions."
```

### Emoji (`emoji:`)

An optional emoji to represent the workflow visually, for example in listings and UI surfaces.

```yaml wrap
emoji: "🤖"
```

### Labels (`labels:`)

Optional array of strings for categorizing workflows by purpose, team, or functionality. Labels appear in `gh aw status` output as `[automation ci diagnostics]` (or a JSON array in `--json` mode) and can be filtered with `gh aw status --label automation`.

```yaml wrap
labels: ["automation", "ci", "diagnostics"]
```

### Metadata (`metadata:`)

Optional key-value pairs for storing custom metadata compatible with the [GitHub Copilot custom agent spec](https://docs.github.com/en/copilot/reference/custom-agents-configuration).

```yaml wrap
metadata:
  author: John Doe
  version: 1.0.0
  category: automation
  docs: https://docs.example.com/automation/repository-health
```

Keys must be 1–64 characters; values are string-only, up to 1024 characters.
`metadata.docs`, when present, must be an absolute HTTPS URL. The compiler preserves
it in generated lock-file metadata without fetching it or changing workflow execution.

### Trigger Events (`on:`)

The `on:` section uses standard GitHub Actions syntax to define workflow triggers, with additional fields for security and approval controls:

- Standard GitHub Actions triggers (push, pull_request, issues, schedule, etc.)
- `reaction:` - Add emoji reactions to triggering items
- `status-comment:` - Post a started/completed comment with a workflow run link (automatically enabled for `slash_command` and `label_command` triggers; must be explicitly set to `true` for other trigger types). Accepts a boolean or an object with optional `issues`, `pull-requests`, and `discussions` toggle fields to selectively disable status comments for specific target types.
- `stop-after:` - Automatically disable triggers after a deadline
- `manual-approval:` - Require manual approval using environment protection rules
- `forks:` - Configure fork filtering for pull_request triggers
- `skip-roles:` - Skip workflow execution for specific repository roles
- `skip-bots:` - Skip workflow execution for specific GitHub actors
- `skip-author-associations:` - Skip execution for configured event + `author_association` combinations
- `roles:` - Restrict which repository roles can trigger the workflow (default: `[admin, maintainer, write]`)
- `bots:` - Allow specific bot accounts to trigger the workflow
- `skip-if-match:` - Skip execution when a search query has matches (supports `scope: none`; use top-level `on.github-token` / `on.github-app` for custom auth)
- `skip-if-no-match:` - Skip execution when a search query has no matches (supports `scope: none`; use top-level `on.github-token` / `on.github-app` for custom auth)
- `steps:` - Inject custom deterministic steps into the pre-activation job (saves one workflow job vs. multi-job pattern)
- `restore-memory:` - Opt in to restoring memory stores before `on.steps` in pre-activation (default: `false`)
- `permissions:` - Grant additional GitHub token scopes to the pre-activation job (for use with `on.steps:` API calls)
- `needs:` - Add custom job dependencies that both `pre_activation` and `activation` must wait for
- `github-token:` - Custom token for activation job reactions, status comments, and skip-if search queries
- `github-app:` - GitHub App for minting a short-lived token used by the activation job and all skip-if search steps

See [Trigger Events](/gh-aw/reference/triggers/) for complete documentation.

### Conditional Execution (`if:`)

Standard GitHub Actions `if:` syntax:

```yaml wrap
if: github.event_name == 'push'
```

### Imports (`imports:`)

Share and reuse workflow components across multiple workflows. The `imports:` field in frontmatter (or `{{#import ...}}` in markdown) composes shared tools, steps, MCP servers, and prompts from other workflow files.

```yaml wrap
imports:
  - shared/common-tools.md
  - shared/mcp/tavily.md
```

See [Imports](/gh-aw/reference/imports/) for complete documentation on syntax, shared components, APM package dependencies, and composition patterns.

### Import Schema (`import-schema:`)

Declares typed inputs accepted by a shared workflow when another workflow imports it with `uses:`/`with:` (or `inputs:`). The compiler validates required inputs, rejects unknown inputs, applies defaults, and exposes values through `${{ github.aw.import-inputs.<name> }}` expressions.

```yaml wrap
import-schema:
  branch:
    type: string
    required: true
    description: "Branch to analyze"
  mode:
    type: choice
    options: [quick, full]
    default: quick
```

Supported scalar types are `string`, `number`, `boolean`, `choice`, and `array`. Object inputs support one level of declared `properties`, referenced as `${{ github.aw.import-inputs.<name>.<property> }}`. See [Imports](/gh-aw/reference/imports/) for import input examples.

### Custom Steps and Jobs (`pre-steps:`, `steps:`, `pre-agent-steps:`, `post-steps:`, `jobs:`)

Add deterministic steps before or after agentic execution, or define full custom GitHub Actions jobs that run before the agent. See [Custom Steps and Jobs](/gh-aw/reference/steps-jobs/) for complete documentation.

The `jobs:` map can also target compiler-generated built-in jobs such as `agent`, `activation`, and `safe_outputs` for additive customization. In particular, `jobs.agent.needs` and `jobs.agent.if` let you gate the generated agent job on a custom setup job while preserving compiler-managed dependencies.

### Cache Configuration (`cache:`)

Cache configuration using standard GitHub Actions `actions/cache` syntax:

Single cache:

```yaml wrap
cache:
  key: node-modules-${{ hashFiles('package-lock.json') }}
  path: node_modules
  restore-keys: |
    node-modules-
```

For secure Go-specific cache guidance, see [FAQ: How should I configure Go caches safely in agentic workflows?](/gh-aw/reference/faq/#how-should-i-configure-go-caches-safely-in-agentic-workflows).

### Repository Checkout (`checkout:`)

Configure how `actions/checkout` is invoked in the agent job. Override default checkout settings or check out multiple repositories for cross-repository workflows.

Set `checkout: false` to disable the default repository checkout entirely — useful for workflows that access repositories through MCP servers or other mechanisms that do not require a local clone:

```yaml wrap
checkout: false
```

See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for complete documentation on checkout configuration options (including `fetch:`, `checkout: false`), merging behavior, and cross-repo examples.

### Ambient Folders (`ambient-folders:`)

Top-level, workspace-relative folders to bundle into the activation artifact and restore into the checkout before the agent runs. Useful for activation steps that generate reusable prompt, skill, or agent context (e.g. a `.squad/` team state directory).

```yaml wrap
ambient-folders:
  - .squad
  - .github/agents
```

### Permissions (`permissions:`)

The `permissions:` section uses syntax similar to standard GitHub Actions permissions to configure the GitHub API scopes available to the workflow. Scopes support their permitted `read`, `write`, and `none` levels, including write-only `copilot-requests` and `id-token` scopes. GitHub App-only scopes, such as `secret-scanning-alerts` and `organization-*`, require an appropriate GitHub App token. See [GitHub Tools Read Permissions](/gh-aw/reference/permissions/).

```yaml wrap
permissions:
  contents: write
  copilot-requests: write
  id-token: write
  secret-scanning-alerts: read
```

### GitHub App (`github-app:`)

Configures GitHub App credentials used as a workflow-wide fallback for minting installation access tokens. The fallback applies to the activation job (`on.github-app`), `safe-outputs.github-app`, each `checkout` entry, and `tools.github.github-app`.

```yaml wrap
github-app:
  client-id: ${{ vars.APP_ID }}
  private-key: ${{ secrets.APP_PRIVATE_KEY }}
```

Requires both `client-id` (or the deprecated `app-id` alias) and `private-key`. Optional fields include `owner` and `repositories` to scope the installation, and `ignore-if-missing: true` to skip token minting when the credentials resolve to empty strings at runtime.

Precedence per section is: a section-specific `github-app`, then a section-specific `github-token`, then this top-level value. A section that already sets its own `github-token` keeps that token and does not receive the fallback. When the top-level `github-app` is defined in a shared workflow, importing workflows inherit it unless they declare their own.

Per-skill credentials under `skills[].github-app` are independent and are not covered by this fallback. Note also that `permissions:` inside a `github-app` block only takes effect for `tools.github.github-app` and `safe-outputs.github-app`; it is ignored for `on.github-app` and for this top-level fallback.

### AI Engine (`engine:`)

Specifies which AI engine interprets the markdown section. See [AI Engines](/gh-aw/reference/engines/) for details.

```yaml wrap
engine: copilot
```

Harness retry and post-result watchdog settings live under `engine.harness` for built-in Copilot, Claude, and Codex harnesses:

```yaml wrap
engine:
  id: copilot
  harness:
    # Allow quiet monorepo scans and builds after an intermediate safe output.
    watchdog-timeout: 600  # seconds
```

See [Harness Retry and Post-result Watchdog Policy](/gh-aw/reference/engines/#harness-retry-and-post-result-watchdog-policy) for defaults, units, and the equivalent `GH_AW_HARNESS_*` environment variables.

### Engine Driver (`engine.driver:`)

Overrides the built-in engine runtime driver for engines that support driver mode. For Copilot, setting `engine.driver` also enables SDK mode. `engine.driver` accepts either a string path/command or an inline source object with exactly one of `node:`, `python:`, `go:`, or `java:`. See [AI Engines](/gh-aw/reference/engines/#copilot-sdk-support) for driver requirements and supported formats.

```yaml wrap
engine:
  id: copilot
  driver:
    node: |
      console.error("hello from inline copilot sdk driver")
```

### Engine Extensions (`engine.extensions:`)

Lists engine-specific plugins to install before the agent runs. Only the Pi engine reads this field; each entry is an npm package name passed to `pi install <extension>`. Other engines ignore it. See [Pi Extensions](/gh-aw/reference/engines/#pi-extensions-extensions) for details.

```yaml wrap
engine:
  id: pi
  extensions:
    - "@pi/web-search"
    - "@pi/file-browser"
```

### Network Permissions (`network:`)

Controls network access using ecosystem identifiers and domain allowlists. See [Network Permissions](/gh-aw/reference/network/) for full documentation.

```yaml wrap
network:
  allowed:
    - defaults              # Basic infrastructure
    - python               # Python/PyPI ecosystem
    - "api.example.com"    # Custom domain
```

### Tools (`tools:`)

Specifies which GitHub API calls, bash commands, browser automation, and MCP servers are available to the AI agent.

```yaml wrap
tools:
  edit:
  bash: ["gh issue comment"]
  github:
    toolsets: [default]
```

See [Tools](/gh-aw/reference/tools/) for complete documentation on built-in tools, GitHub toolsets, and MCP server configuration.

### Frontmatter Skills (`skills:`)

Installs Copilot skills in the activation job before the agent runs.

Supported entry formats:

- String form (shared authentication):
  - `skills/name` or `.github/skills/name` (local development path; installed with `--from-local`)
  - `owner/repo@<ref>`
  - `owner/repo/skill/path@<ref>`
- Object form (per-skill authentication):
  - `skill` (required)
  - `github-token` (optional)
  - `github-app` (optional)

`github-token` and `github-app` are mutually exclusive for each object entry.
`github-token` must be an expression such as `${{ secrets.NAME }}` or
`${{ needs.auth.outputs.token }}`.
`<ref>` may be a branch, tag, or 40-character lowercase commit SHA. Non-SHA
refs are resolved and rewritten to the matching commit SHA at compile time.
If resolution fails (for example, due to missing network access or authentication),
the compiler keeps the original unpinned ref and emits a warning. Omitting the ref
(`owner/repo@`) installs from the repository's default branch on every run
and is not pinned; the compiler emits a warning recommending an explicit ref.

```yaml wrap
skills:
  # Shared auth via workflow-level activation token; sha-pinned automatically at compile time
  - mattpocock/skills/tdd@main

  # Per-skill PAT (or fallback) for private skill repositories
  - skill: mattpocock/skills/diagnosing-bugs@801dca688564c529fa84f247f64472520d9ebe28
    github-token: ${{ secrets.MATT_SKILLS_PAT || secrets.GITHUB_TOKEN }}

  # Per-skill GitHub App credentials
  - skill: mattpocock/skills/domain-modeling@801dca688564c529fa84f247f64472520d9ebe28
    github-app:
      client-id: ${{ vars.MATT_SKILLS_APP_CLIENT_ID }}
      private-key: ${{ secrets.MATT_SKILLS_APP_PRIVATE_KEY }}
```

See [Glossary: Frontmatter Skills](/gh-aw/reference/glossary/#frontmatter-skills-skills)
for terminology, and
[`mattpocock-skills-reviewer.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/mattpocock-skills-reviewer.md)
for a full workflow example using `skills:`.

### Agent Plugins (`plugins:`)

:::caution[Experimental]
Agent Plugins support is experimental and may change. Compiling a workflow that uses `plugins:` emits a warning.
:::

Installs [Agent Plugins](https://agent-plugins.org) through the selected agentic engine. Each entry identifies a GitHub repository and, optionally, the path to a plugin within that repository:

```yaml wrap
engine: copilot
plugins:
  - octo-org/agent-plugin@v1
  - octo-org/agent-plugins/plugins/example@main
```

Entries use `owner/repository[/path]@ref` syntax. The ref is required and may be a branch, tag, or full 40-character lowercase commit SHA. During compilation, gh-aw resolves every branch or tag to a commit SHA. Compilation fails if a reference cannot be resolved, so generated workflows never install a plugin from a moving ref.

The agent job checks out each pinned plugin immediately after installing the engine, then makes it available the way the engine expects: GitHub Copilot CLI runs `copilot plugin install`, Claude Code loads each plugin directory through `--plugin-dir`, and OpenAI Codex CLI registers a local, single-plugin marketplace for each checkout (reading the plugin's declared name from its `plugin.json` manifest) and runs `codex plugin marketplace add` followed by `codex plugin add`. Imported engine definitions declare their own handling with a `behaviors.plugins` block, so the shared Cursor and Kiro engines stage plugins in the folder their CLI scans. Using `plugins:` with an engine that has no Agent Plugins support is a compile-time error.

Shared agentic workflows may also declare plugins. When the same plugin path is declared more than once, identical refs are deduplicated and compatible semantic versions are merged to the highest version. Incompatible major versions or conflicting non-semver refs fail compilation.

Supported entry formats:

- String form (shared authentication):
  - `owner/repo@<ref>`
  - `owner/repo/plugins/path@<ref>`
- Object form (per-plugin authentication):
  - `plugin` (required)
  - `github-token` (optional)
  - `github-app` (optional)

`github-token` and `github-app` are mutually exclusive for each object entry. By default (string form, or object form without either field), the checkout step uses the workflow's default `github.token`, which cannot read private repositories. Set a per-plugin `github-token` or `github-app` to install a plugin from a private repository:

```yaml wrap
engine: copilot
plugins:
  # Public plugin, shared auth via the workflow's default token
  - octo-org/agent-plugin@v1

  # Per-plugin PAT for a private plugin repository
  - plugin: octo-org/private-plugin@6f2a1b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90
    github-token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}

  # Per-plugin GitHub App credentials
  - plugin: octo-org/private-marketplace/plugins/example@6f2a1b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90
    github-app:
      client-id: ${{ vars.PLUGIN_APP_CLIENT_ID }}
      private-key: ${{ secrets.PLUGIN_APP_PRIVATE_KEY }}
```

Because plugin support is implemented per agentic engine, per-plugin credentials only take effect for the checkout step; whether an engine can install a plugin at all still depends on that engine's own Agent Plugins support.

### MCP Scripts (`mcp-scripts:`)

Enables defining custom MCP tools inline using JavaScript or shell scripts. See [MCP Scripts](/gh-aw/reference/mcp-scripts/) for complete documentation on creating custom tools with controlled secret access.

### Safe Outputs (`safe-outputs:`)

Enables automatic issue creation, comment posting, and other safe outputs. See [Safe Outputs Processing](/gh-aw/reference/safe-outputs/).

`safe-outputs:` also accepts workflow-level control fields that apply to safe-output processing as a whole, not just individual handlers. For example, `report-failed-jobs: false` disables the automatic failed-job reporting issue created by the framework:

```yaml wrap
safe-outputs:
  create-issue:
    max: 1
  report-failed-jobs: false
```

When omitted, `report-failed-jobs` defaults to `true`.

Custom safe-output jobs are defined under `safe-outputs.jobs:`. They run after the agent completes and can expose a custom safe-output tool to the agent. The optional `output` field is the success message returned to the agent:

```yaml wrap
safe-outputs:
  jobs:
    notify:
      description: "Send a notification"
      runs-on: ubuntu-latest
      output: "Notification sent"
      steps:
        - run: ./scripts/send-notification.sh
```

See [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/) for job inputs, secrets, and third-party integrations.

### Threat Detection Suppression (`threat-detection-suppress:`)

Suppresses specific threat-detection diagnostic rules (`CTR-###` identifiers) that would otherwise block safe-output processing, with a required, auditable justification. Each entry must include a `rule` matching `CTR-###`, a non-empty `reason`, and an optional `expires` date in `YYYY-MM-DD` format; once `expires` has passed (UTC), the suppression is no longer active and the rule is enforced again.

```yaml wrap
threat-detection-suppress:
  - rule: CTR-012
    reason: "False positive on generated changelog entries; tracked in issue #123"
    expires: "2026-12-31"
```

Compilation fails if any entry has an invalid `rule`, an empty `reason`, or a malformed `expires` date. See [Threat Detection](/gh-aw/reference/threat-detection/) for the full list of detection rules.

### Run Configuration (`run-name:`, `runs-on:`, `runs-on-slim:`, `timeout-minutes:`)

Standard GitHub Actions properties:

```yaml wrap
run-name: "Custom workflow run name"  # Defaults to workflow name
runs-on: ubuntu-latest               # Defaults to ubuntu-latest (main job only)
runs-on-slim: ubuntu-slim            # Defaults to ubuntu-slim (framework jobs only)
timeout-minutes: 30                  # Agentic step timeout, defaults to 20 minutes
```

`runs-on` applies to the main agent job only. `runs-on-slim` applies to all framework/generated jobs (activation, safe-outputs, unlock, etc.), accepts the same string, array, or runner-group object forms as `runs-on`, and defaults to `ubuntu-slim`. `safe-outputs.runs-on` and `safe-outputs.threat-detection.runs-on` also accept the same runner forms and take precedence where applicable.

`timeout-minutes` accepts an integer or a GitHub Actions expression string (e.g. `${{ inputs.timeout }}`), letting a reusable `workflow_call` workflow parameterize its own timeout from caller inputs. It bounds the `agentic_execution` step and defaults to `${{ vars.GH_AW_DEFAULT_TIMEOUT_MINUTES }}` before a 20-minute fallback. The generated jobs are bounded separately: `jobs.agent.timeout-minutes` (default `${{ vars.GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES }}` or 60 minutes) and `jobs.detection.timeout-minutes` (default `${{ vars.GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES }}` or 10 minutes), each covering every step of its job. It applies to the workflow being compiled, **not** to plain caller jobs that invoke a reusable workflow with job-level `uses:` — GitHub rejects `timeout-minutes` there.

**Supported runners for `runs-on:`**

| Runner | Status |
|--------|--------|
| `ubuntu-latest` | ✅ Default. Recommended for most workflows. |
| `ubuntu-24.04` / `ubuntu-22.04` | ✅ Supported. |
| `ubuntu-24.04-arm` | ✅ Supported. Linux ARM64 runner. |
| `macos-*` | ❌ Not supported. Docker is unavailable on macOS runners (no nested virtualization). See [FAQ](/gh-aw/reference/faq/). |
| `windows-*` | ❌ Not supported. AWF requires Linux. |

### Workflow Concurrency Control (`concurrency:`)

Automatically generates concurrency policies for the agent job. See [Concurrency Control](/gh-aw/reference/concurrency/).

### Environment Variables (`env:`)

Standard GitHub Actions `env:` syntax for workflow-level environment variables:

```yaml wrap
env:
  CUSTOM_VAR: "value"
```

Environment variables can be defined at multiple scopes (workflow, job, step, engine, safe-outputs, etc.) with clear precedence rules. See [Environment Variables](/gh-aw/reference/environment-variables/) for complete documentation on all 13 env scopes and precedence order.

> [!WARNING]
> Do not use `${{ secrets.* }}` expressions in the workflow-level `env:` section. Environment variables defined here are passed directly to the agent container, which means secret values would be visible to the AI model. In strict mode, this is a compilation error. In non-strict mode, it emits a warning.
>
> Use engine-specific secret configuration instead of the `env:` section to pass secrets securely.

### Excluded Environment Variables (`excluded-env:`)

Lists environment variable names that must be excluded from the AWF agent container even when the compiler cannot infer that they contain sensitive values. Names are deduplicated and merged with automatically excluded variables detected from `secrets.*` and `needs.*.outputs.*` references.

```yaml wrap
excluded-env:
  - MY_DISPATCH_TOKEN
  - GH_TOKEN
```

### Turn Limit (`max-turns:`)

Caps the number of chat iterations (model responses and tool calls) the AWF proxy allows for a single workflow run, across all supported engines. Defaults to `500` when omitted. Accepts an integer or a GitHub Actions expression that resolves to an integer at runtime.

```yaml wrap
max-turns: 20
```

The top-level `max-runs:` field is a **deprecated** alias for `max-turns:` and is only accepted as a fallback for backward compatibility. Migrate existing workflows with `gh aw fix`. See [Cost Management](/gh-aw/reference/cost-management/#cap-turns-per-run) for more details.

### Turn Cache Miss Limit (`max-turn-cache-misses:`)

Sets the maximum consecutive AWF cache misses allowed before the API proxy blocks further requests. The value maps to `apiProxy.maxCacheMisses`, must be a positive integer, and defaults to `5` when neither frontmatter nor the `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES` environment override provides a value.

```yaml wrap
max-turn-cache-misses: 5
```

### AI Credits Guardrail (`max-ai-credits:`)

Sets the AWF AI Credits budget used for cost enforcement. It is enabled by default and defaults to `1000` (`1k`) when omitted. Steering (budget-warning messages at 80%, 90%, 95%, and 99% of the budget) is enabled by default. Use plain integers or `K`/`M` suffixes such as `100000K` or `100M`. Set to a negative value to disable both budget enforcement and steering.

```yaml wrap
max-ai-credits: 500
```

```yaml wrap
# Disable budget enforcement and steering
max-ai-credits: -1
```

### Daily Per-Workflow AI Credits Guardrail (`max-daily-ai-credits:`)

Sets a 24-hour AI Credits cap for a single workflow, aggregated across recent runs of the same workflow in the repository. When the activation job detects that the previous 24 hours already exceed this threshold, it warns, creates an issue, skips the agent job, and lets the conclusion job report the specialized failure context. Use plain integers or `K`/`M` suffixes such as `100000K` or `100M`.

This guardrail is disabled by default when omitted, and `-1` explicitly disables it. This guardrail is skipped for `workflow_call`, `repository_dispatch`, and `workflow_dispatch` runs that carry internal `aw_context` dispatch metadata.

```yaml wrap
max-daily-ai-credits: 10000
```

Use the object form to supply a dedicated GitHub App token for the guardrail's API calls:

```yaml wrap
max-daily-ai-credits:
  value: 10000
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
```

```yaml wrap
# Disable the guardrail explicitly
max-daily-ai-credits: -1
```

### Per-User Rate Limiting (`user-rate-limit:`)

Limits how frequently a single user can trigger the workflow. When the limit is exceeded, the pre-activation job cancels the run before the agent executes. Rate limiting applies to programmatically triggered events (such as `workflow_dispatch`, `issue_comment`, and `pull_request_review`); when `events` is omitted, the applicable events are inferred from the `on:` section, falling back to all supported programmatic events if no supported triggers are found.

```yaml wrap
user-rate-limit:
  max-runs-per-window: 5                      # Required: maximum runs per user per window (1-10)
  window: 60                                  # Optional: window in minutes (default: 60, max: 180)
  events: [workflow_dispatch, issue_comment]  # Optional: events to rate limit (inferred from `on:` when omitted; fallback to all supported programmatic events)
  ignored-roles: [admin, maintain]            # Optional: exempt roles (default: [admin, maintain, write])
```

`max-runs-per-window` also supports a GitHub Actions expression (for example `${{ inputs.max-runs-per-window }}`) that resolves to an integer at runtime.

Users with any of the `ignored-roles` are not rate limited. The default exemptions are `admin`, `maintain`, and `write`; set `ignored-roles: []` to rate limit every user, including administrators.

Legacy frontmatter that used a top-level `rate-limit:` section, or `max:`/`max-runs:` instead of `max-runs-per-window:`, must be migrated with `gh aw fix`.

See [Rate Limiting and Controls](/gh-aw/reference/rate-limiting-controls/) for more details.

### Secrets (`secrets:`)

Defines secret values passed to workflow execution. Secrets are typically used to provide sensitive configuration to MCP servers or workflow components. Values must be GitHub Actions expressions that reference secrets (e.g., `${{ secrets.API_KEY }}`).

```yaml wrap
secrets:
  API_TOKEN: ${{ secrets.API_TOKEN }}
  DATABASE_URL: ${{ secrets.DB_URL }}
```

Secrets can also include descriptions for documentation:

```yaml wrap
secrets:
  API_TOKEN:
    value: ${{ secrets.API_TOKEN }}
    description: "API token for external service"
  DATABASE_URL:
    value: ${{ secrets.DB_URL }}
    description: "Production database connection string"
```

Always reference secrets through `${{ secrets.NAME }}` expressions, never plaintext; prefer environment-specific secrets (via the `environment:` field) and limit access to the components that need them.

**Note:** For passing secrets to reusable workflows, use the `jobs.<job_id>.secrets` field instead. The top-level `secrets:` field is for workflow-level secret configuration.

### Environment Protection (`environment:`)

Specifies the environment for deployment protection rules and environment-specific secrets. Standard GitHub Actions syntax.

```yaml wrap
environment: production
```

See [GitHub Actions environment docs](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment).

### Container Configuration (`container:`)

Specifies a container to run job steps in.

```yaml wrap
container: node:18
```

See [GitHub Actions container docs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-where-workflows-run/run-jobs-in-a-container).

### Service Containers (`services:`)

Defines service containers that run alongside your job (databases, caches, etc.).

```yaml wrap
services:
  postgres:
    image: postgres:13
    env:
      POSTGRES_PASSWORD: postgres
    ports:
      - 5432:5432
```

> [!NOTE]
> The AWF agent runs inside an isolated Docker container. Service containers expose ports on the runner host, not within the agent's network namespace. To connect to a service from the agent, use `host.docker.internal` as the hostname instead of `localhost`. For example, a Postgres service configured with port `5432:5432` is accessible at `host.docker.internal:5432`.

See [GitHub Actions service docs](https://docs.github.com/en/actions/using-containerized-services).

### Observability (`observability:`)

Use `observability.otlp` to export distributed traces from workflow runs to an OpenTelemetry Protocol (OTLP) compatible backend.

```yaml wrap
observability:
  otlp:
    endpoint: ${{ secrets.OTLP_ENDPOINT }}
    headers:
      Authorization: ${{ secrets.OTLP_TOKEN }}
      X-Tenant: my-org
```

`endpoint` accepts a string, a `{url, headers}` object, or an array of endpoint objects for fan-out; `headers` accepts a map or comma-separated `key=value` string; `if-missing` supports `error` (default), `warn`, and `ignore`; `attributes` is an optional map of custom span attributes (values support GitHub Actions expressions); and `resource-attributes` appends custom OTel resource attributes to the built-in gh-aw/GitHub set. Use static strings or GitHub Actions expressions for `resource-attributes`, but do not use `secrets.*` or `vars.*` values because resource attributes are exported to external observability backends and are not treated as secret values. See the [OpenTelemetry guide](/gh-aw/reference/open-telemetry/) for setup and the [OpenTelemetry attribute reference](/gh-aw/reference/open-telemetry-attributes/) for emitted fields.

### Resources (`resources:`)

Declares additional workflow or action files to fetch alongside this workflow when running `gh aw add`. Use this field when the workflow depends on companion workflows or custom actions stored in the same directory.

```yaml wrap
resources:
  - triage-issue.md          # companion workflow
  - label-issue.md           # companion workflow
  - shared/helper-action.yml # supporting GitHub Action
```

Entries are relative paths from the workflow's location in the source repository (GitHub Actions expression syntax `${{` is not allowed). When `gh aw add` installs this workflow, each listed file is downloaded alongside it so dependencies are available after installation. `gh aw add` also automatically fetches workflows referenced in the [`dispatch-workflow`](/gh-aw/reference/safe-outputs/#workflow-dispatch-dispatch-workflow) safe output, even when not listed here.

### Runtimes (`runtimes:`)

Override default runtime versions for languages and tools used in workflows. The compiler detects which runtimes are needed from tool configurations (e.g. `bash: ["node"]`) and workflow steps, then installs the specified versions. Pin versions for reproducibility, opt into preview releases, or point at custom setup actions (forks, enterprise mirrors).

Each runtime takes a required `version` string, plus optional `action-repo` and `action-version` to override the default setup action:

| Runtime | Default Version | Default Setup Action |
|---------|----------------|---------------------|
| `node` | 24 | `actions/setup-node@v7` |
| `python` | 3.12 | `actions/setup-python@v5` |
| `go` | 1.25 | `actions/setup-go@v5` |
| `uv` | latest | `astral-sh/setup-uv@v5` |
| `bun` | 1.1 | `oven-sh/setup-bun@v2` |
| `deno` | 2.x | `denoland/setup-deno@v2` |
| `ruby` | 3.3 | `ruby/setup-ruby@v1` |
| `java` | 21 | `actions/setup-java@v4` |
| `dotnet` | 8.0 | `actions/setup-dotnet@v4` |
| `elixir` | 1.17 | `erlef/setup-beam@v1` |
| `haskell` | 9.10 | `haskell-actions/setup@v2` |

Override one or more runtimes, optionally with a custom setup action:

```yaml wrap
runtimes:
  node:
    version: "20"
  python:
    version: "3.12"
    action-repo: "actions/setup-python"
    action-version: "v5"
```

Omitted runtimes use the defaults above. Runtimes from imported shared workflows are merged with your workflow's configuration.

### `run-install-scripts`

Controls whether npm pre/post-install scripts are allowed during package installation. Configure this under `runtimes.node.run-install-scripts`. The default is `false`.

```yaml wrap
runtimes:
  node:
    run-install-scripts: true
```

Enabling this increases supply chain risk because install hooks from dependencies can execute arbitrary code. In strict mode, `run-install-scripts: true` is rejected.

### Source Tracking (`source:`)

Tracks workflow origin in format `owner/repo/path@ref`. Automatically populated when using `gh aw add` to install workflows from external repositories. Optional for manually created workflows.

```yaml wrap
source: "githubnext/agentics/workflows/ci-doctor.md@v1.0.0"
```

### Redirect (`redirect:`)

Specifies a new canonical location, using the same `owner/repo/path@ref` format as `source:`, when a workflow has been moved or renamed. `gh aw add`, `gh aw add-wizard`, and `gh aw update` follow redirect chains transitively (up to a depth limit) to the resolved location, rewrite the local `source` field accordingly, and report redirect loops as errors.

```yaml wrap
redirect: "githubnext/agentics/workflows/new-workflow-name.md@main"
```

`gh aw compile` only treats a file as a redirect-only placeholder when `redirect:` is present and `on:` is absent. In that placeholder case, compilation is skipped successfully and an informational message tells the user to run `gh aw update` to resolve the full workflow. A workflow that contains both `redirect:` and `on:` is compiled as a normal workflow; the redirect metadata remains available to `gh aw add` and `gh aw update`.

Use `gh aw update --no-redirect` to fail the update instead of following the redirect — useful for auditing or controlling exactly when redirects are applied.

> [!NOTE]
> The `redirect` field is set by workflow *authors* to signal that a workflow has moved. It is not typically set by end-users. If you see a redirect when running `gh aw update`, it means the upstream workflow has been relocated.

### Tracker ID (`tracker-id:`)

Tags every asset (issues, pull requests, discussions, comments) the workflow creates with a hidden HTML comment — `<!-- gh-aw-tracker-id: … -->` — enabling GitHub search to find all items associated with this workflow.

```yaml wrap
tracker-id: code-simplifier
```

Accepts 8–128 alphanumeric characters, hyphens, and underscores. Most workflows use their filename as the tracker ID.

Search for all assets created by a specific workflow:

```
repo:owner/repo "gh-aw-tracker-id: code-simplifier" in:body
```

See [Footers](/gh-aw/reference/footers/) for marker details and footer visibility control.

### Private Workflows (`private:`)

Mark a workflow as private to prevent it from being installed into other repositories via `gh aw add`.

```yaml wrap
private: true
```

Adding the workflow from another repository then fails with `workflow 'owner/repo/internal-tooling' is private and cannot be added to other repositories`. Use this for internal tooling, sensitive automation, or repository-specific workflows not intended for reuse.

This only blocks installation via `gh aw add`; the visibility of the workflow file itself is controlled by your repository's access settings.

### `check-for-updates`

Controls whether the compile-agentic version update check runs in the activation job.

```yaml wrap
check-for-updates: true
```

When `true` (default), the activation job verifies the compiled version is not blocked and meets the minimum supported version. Set to `false` to disable this check (not allowed in strict mode).

### Feature Flags (`features:`)

Enable experimental or optional compiler and runtime behaviors as key-value pairs. See [Feature Flags](/gh-aw/reference/feature-flags/) for complete documentation.

### Strict Mode (`strict:`)

Enables enhanced security validation for production workflows (default: `true`).

```yaml wrap
strict: false  # Disable enhanced security validation for development/testing
```

Workflows compiled with `strict: false` cannot run on public repositories. The workflow fails at runtime with an error message prompting recompilation with strict mode.

To prevent workflows from opting out, set `"strict": true` at the top level of `.github/workflows/aw.json`. This enforces strict mode for every `gh aw compile` invocation in the repository. The repository setting only accepts `true`; `"strict": false` is invalid.

See [Network Permissions - Strict Mode Validation](/gh-aw/reference/network/#strict-mode-validation) for details on network validation and [CLI Commands](/gh-aw/setup/cli/#compile) for compilation options.

## Learn More

See also: [Trigger Events](/gh-aw/reference/triggers/), [AI Engines](/gh-aw/reference/engines/), [CLI Commands](/gh-aw/setup/cli/), [Workflow Structure](/gh-aw/reference/workflow-structure/), [Network Permissions](/gh-aw/reference/network/), [Feature Flags](/gh-aw/reference/feature-flags/), [Custom Steps and Jobs](/gh-aw/reference/steps-jobs/), [OpenTelemetry Guide](/gh-aw/reference/open-telemetry/), [Command Triggers](/gh-aw/reference/command-triggers/), [MCPs](/gh-aw/guides/mcps/), [Tools](/gh-aw/reference/tools/), [Imports](/gh-aw/reference/imports/)
