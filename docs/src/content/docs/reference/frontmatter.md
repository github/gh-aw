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

This page summarizes the available frontmatter fields for GitHub Agentic Workflows.

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

### Invocation Skip Questions

> [!WARNING]
> `skip-if-decision` is experimental. Nonempty lists emit a compile-time
> warning; configuration and behavior may change without deprecation.

An optional top-level `skip-if-decision` list of up to 64 questions that AWF evaluates before each otherwise eligible agent invocation, including retries, resumes, and continuations. It applies to all built-in and declarative engines. Compilation rejects nonempty lists combined with declarative `protected-session-capture`, which preserves recorder isolation.

```aw wrap
skip-if-decision:
  - Is there no actionable work remaining in @context/task.md?
  - Has @reports/progress.md established that further execution is unnecessary?
```

Each question returns `deny` (skip condition established), `allow` (execution recommended), or `unknown` (abstain). Any `deny` skips the invocation; otherwise the deterministic harness path proceeds. An absent or empty list preserves existing behavior. Decisions cannot override retry limits, budgets, deadlines, or nonrecoverable failures.

All questions share one Decisions request through the AWF API proxy, using `gpt-6-luna-decisions` on a configured Copilot provider or `gpt-6-luna` when only OpenAI is configured, with no direct-provider fallback. Unavailable models, timeouts, malformed responses, and file-read failures abstain without retrying.

Questions and referenced files are screened for credential patterns and configured secret values; a match withholds that question as `unknown` with `sensitive_content_blocked`. Screening is heuristic, so do not reference confidential files merely because they sit in an allowed path. File evidence remains untrusted data.

`@file` references resolve relative to `engine.cwd` (or the workspace) and are re-read before each invocation. Only regular files under the workspace or `/tmp/gh-aw/agent` are eligible; symlinks, hardlinks, sensitive paths, and oversized context are rejected. Imports are capped at 512 KiB per question and 400,000 UTF-8 bytes per request; over-budget or unreadable questions become `unknown`. Paths may use letters, digits, `_`, `.`, `/`, and `-`; Actions expressions are not supported.

Outcomes appear in the harness retry artifact, as `harness.decision` events, and under `retries.decisions` in `gh aw audit --json`, which counts avoided invocations separately from retries. Events never contain file contents or raw provider diagnostics. A denied invocation records a noop if no terminal safe-output exists. The gate does not apply to threat detection or evals.

### Labels (`labels:`)

Optional array of strings for categorizing workflows by purpose, team, or functionality. Labels appear in `gh aw status` output as `[automation ci diagnostics]` (or a JSON array in `--json` mode) and can be filtered with `gh aw status --label automation`.

Each label name is limited to 50 characters. The compiler rejects longer names, including static labels configured for safe outputs.

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

Keys are 1–64 characters and values are strings up to 1024 characters. `metadata.docs`, if present, must be an absolute HTTPS URL; it is kept in lock-file metadata and never fetched. Reuse a value in OTLP attributes with a whole-value `${{ github.aw.metadata.<key> }}` reference, resolved at compile time. Metadata is not exported automatically.

### Trigger Events (`on:`)

The `on:` section uses standard GitHub Actions trigger syntax, plus controls for reactions, status comments, deadlines, approval, fork handling, actor filtering, and more. See [Trigger Events](/gh-aw/reference/triggers/).

### Conditional Execution (`if:`)

Standard GitHub Actions `if:` syntax:

```yaml wrap
if: github.event_name == 'push'
```

### Token Broker (`token-broker:`)

> [!WARNING]
> Token broker authentication is experimental. Configuration and behavior may change without deprecation. Compiling a workflow that enables a broker capability emits a warning.

An optional object that independently enables OIDC-authenticated Copilot credential exchange.

```aw
token-broker:
  copilot-task: true
```

Select `copilot-request`, `copilot-task`, or both. Brokered request authentication works in threat-detection and evals jobs; main agent inference remains blocked pending renewable AWF credential support. Exchanges include the selected LLM inference provider ID, and brokered jobs run an unconditional post-step that invalidates their credentials. `copilot-request` cannot be combined with `permissions.copilot-requests: write`. The endpoint comes from the `GH_AW_TOKEN_BROKER_URL` Actions secret at runtime. If configuration is missing or an exchange fails, the operation is blocked rather than falling back to existing credentials. See [enterprise token broker configuration](/gh-aw/reference/enterprise-configuration/#oidc-token-broker).

For GitHub API authentication, add `token-broker:` under `tools.github`, `safe-outputs`, or both. An empty value means `true`; explicit `false` disables the broker for that section.

```aw
tools:
  github:
    token-broker:
safe-outputs:
  token-broker:
  add-comment:
```

Each setting independently requests the `github-app` broker capability; no top-level `token-broker` is required. Don't combine a section's `token-broker` with its `github-app` or `github-token`. GitHub tool authentication needs an enabled agent sandbox with AWF environment-exclusion support, which keeps OIDC bootstrap credentials away from the agent. Deployment policy controls scope and lifetime, and GitHub tool credentials must be read-only. Each job gets a fixed token with no automatic renewal.

> [!CAUTION]
> Custom steps, jobs, and actions in a brokered job run with its full credential authority, including `pre-steps`, `steps`, `pre-agent-steps`, `post-steps`, `jobs`, and `safe-outputs.steps`. They can read the brokered token and use `id-token: write` to request their own credentials. Cleanup only limits use after the job ends, and `safe-outputs.jobs` do not inherit broker authentication. Pin third-party actions to full commit SHAs, never let untrusted input choose code or targets, and keep credential-free code in jobs without `id-token: write`. See [security considerations](/gh-aw/specs/token-broker-specification/#appendix-c-security-and-privacy-considerations-normative).

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

### Pre-start Docker Image Pull Policy (`docker-image-pull-policy:`)

By default, the generated agent and detection jobs pre-pull their Docker images (`docker-image-pull-policy: always`). Set `docker-image-pull-policy: never` to require those images to be present locally instead:

```yaml
docker-image-pull-policy: never
```

In `never` mode, each pre-start image must be a registry-qualified `repository[:tag]@sha256:<64 lowercase hex>` reference that Docker can inspect, with a matching `repository@sha256:...` entry in its `RepoDigests`. All images are validated **before** any local alias is tagged, and the helper never falls back to a pull. A versioned reference creates only its versioned alias (not `:latest`), so downstream consumers must use that alias. Missing metadata, unpinned images, conflicting aliases, or failed inspections stop the job.

Use `jobs.agent.pre-steps` and `jobs.detection.pre-steps` to restore verified image content **before** each download step. `docker save`/`load` may discard `RepoDigests`, which this mode will not fabricate. The setting checks only local Docker metadata; it does not verify image bytes, provenance, or scanner policy.

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

Configures how `actions/checkout` runs in the agent job, including multi-repository checkouts. Set `checkout: false` to skip the default checkout, for example when a workflow reaches repositories only through MCP servers:

```yaml wrap
checkout: false
```

See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for `fetch:`, merging behavior, and cross-repo examples.

### Ambient Folders (`ambient-folders:`)

Workspace-relative folders to bundle into the activation artifact and restore into the checkout before the agent runs. Useful for generated prompt, skill, or agent context, such as a `.squad/` team state directory.

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

Precedence per section: a section-specific `github-app`, then its `github-token`, then this top-level value. A section that sets its own `github-token` does not receive the fallback. Importing workflows inherit a shared workflow's top-level `github-app` unless they declare their own.

Per-skill credentials under `skills[].github-app` are independent of this fallback. `permissions:` inside a `github-app` block takes effect only for `tools.github.github-app` and `safe-outputs.github-app`; it is ignored for `on.github-app` and for this fallback.

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

### Program (`program:`)

With `engine: none`, runs a deterministic TypeScript or JavaScript program, relative to the workflow's directory, instead of an agent. See [Programs](/gh-aw/reference/programs/).

```yaml wrap
engine: none
program: dispatcher.ts
```

<span id="model-routing-model-router-enginemodel-routing"></span>

### Model Routing

Set the top-level `model` to `router` to let each task determine the routing level, or choose `router/economy`, `router/balanced`, or `router/robust` for a fixed level:

```aw wrap
model: router/economy
```

To narrow the main agent's choices without restricting sub-agents, add a [shortlist](/gh-aw/reference/model-routing/#a-shortlist-of-models) alongside a literal router value. To opt out of [default routing](/gh-aw/reference/model-routing/#default-routing-for-workflows-without-a-model) without pinning a model, omit `model` and set top-level `model-routing: false`.

To record outcomes and cost without changing the router's pick, set `learning: observe` under `model-routing` and enable `tools.repo-memory`; `learning: false` overrides an inherited setting. See [Adaptive Routing](/gh-aw/reference/adaptive-routing/).

```aw wrap
model: router
model-routing:
  learning: observe
tools:
  repo-memory: true
```

### Engine Driver (`engine.driver:`)

Overrides the built-in engine runtime driver for engines that support driver mode. For Copilot, it also enables SDK mode. Accepts a string path/command or an inline source object with exactly one of `node:`, `python:`, `go:`, or `java:`. See [AI Engines](/gh-aw/reference/engines/copilot/#copilot-sdk-support) for driver requirements and supported formats.

```yaml wrap
engine:
  id: copilot
  driver:
    node: |
      console.error("hello from inline copilot sdk driver")
```

### Engine Extensions (`engine.extensions:`)

Lists npm packages to install before the agent runs. Only the Pi engine reads this field, passing each entry to `pi install <extension>`; other engines ignore it. See [Pi Extensions](/gh-aw/reference/engines/pi/#pi-extensions-extensions).

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

### AWF Version (`sandbox.agent.version`)

Selects a pinned AWF release. Installation checks the release's checksum manifest against a trusted SHA256 pin, then verifies the downloaded bundle or binary; release attestations are not queried. GitHub Enterprise Cloud (`*.ghe.com`) installs need no extra github.com token. See [AWF checksum pins](/gh-aw/reference/sandbox/#release-integrity-sandboxagentchecksums-sha256) for custom versions.

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

- String form with shared authentication: `skills/name` or `.github/skills/name` for local development, `owner/repo@<ref>`, or `owner/repo/skill/path@<ref>`
- Object form with per-skill authentication: `skill` (required), plus optional `github-token` or `github-app`

`github-token` and `github-app` are mutually exclusive for each object entry. `github-token` must be an expression such as `${{ secrets.NAME }}` or `${{ needs.auth.outputs.token }}`.

Refs may be a branch, tag, or 40-character lowercase commit SHA. Non-SHA refs are rewritten to commit SHAs at compile time; if resolution fails, the original ref is kept with a warning. An omitted ref (`owner/repo@`) tracks the default branch on every run, so the compiler warns.

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

Entries use `owner/repository[/path]@ref`. The ref is required and may be a branch, tag, or full 40-character lowercase commit SHA. Compilation resolves branches and tags to commit SHAs and fails on unresolvable refs, so generated workflows never install from a moving ref.

The agent job checks out each pinned plugin after installing the engine, then registers it per engine: Copilot CLI runs `copilot plugin install`, Claude Code loads each directory with `--plugin-dir`, and Codex CLI registers a single-plugin local marketplace (named from `plugin.json`) and runs `codex plugin marketplace add` and `codex plugin add`. Engine definitions can declare their own handling with a `behaviors.plugins` block. The Kiro sample selects V2 and does not support V3-only Powers plugins. `plugins:` with an engine lacking Agent Plugins support is a compile-time error.

Shared workflows may also declare plugins. A repeated plugin path deduplicates identical refs and merges compatible semantic versions to the highest one; incompatible majors or conflicting non-semver refs fail compilation.

Supported entry formats:

- String form with shared authentication: `owner/repo@<ref>` or `owner/repo/plugins/path@<ref>`
- Object form with per-plugin authentication: `plugin` (required), plus optional `github-token` or `github-app`

`github-token` and `github-app` are mutually exclusive per object entry. By default, the checkout step uses the workflow's `github.token`, which cannot read private repositories. Set a per-plugin `github-token` or `github-app` to install from a private repository:

```yaml wrap
engine: copilot
plugins:
  # Public plugin, shared auth via the workflow's default token
  - octo-org/agent-plugin@v1

  # Per-plugin PAT for a private plugin repository
  - plugin: octo-org/private-plugin@6f2a1b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90
    github-token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}

  # Per-plugin token from a same-job pre-step output
  - plugin: octo-org/private-reporting-plugin@6f2a1b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90
    github-token: ${{ steps.plugin_credentials.outputs.github_token }}

  # Per-plugin token from a same-job environment variable
  - plugin: octo-org/private-triage-plugin@6f2a1b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90
    github-token: ${{ env.GH_TOKEN }}

  # Per-plugin GitHub App credentials
  - plugin: octo-org/private-marketplace/plugins/example@6f2a1b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90
    github-app:
      client-id: ${{ vars.PLUGIN_APP_CLIENT_ID }}
      private-key: ${{ secrets.PLUGIN_APP_PRIVATE_KEY }}
```

When a `pre-steps` entry fetches a credential at runtime, pass it through a same-job step output (preferred) or environment variable. Values exported to `$GITHUB_ENV` remain visible to later steps until cleared.

Per-plugin credentials apply only to the checkout step. Whether an engine can install plugins at all depends on its own Agent Plugins support.

### MCP Scripts (`mcp-scripts:`)

Enables defining custom MCP tools inline using JavaScript or shell scripts. See [MCP Scripts](/gh-aw/reference/mcp-scripts/) for complete documentation on creating custom tools with controlled secret access.

### Safe Outputs (`safe-outputs:`)

Enables automatic issue creation, comment posting, and other safe outputs. See [Safe Outputs Processing](/gh-aw/reference/safe-outputs/).

`safe-outputs:` also accepts workflow-wide control fields. For example, `report-failed-jobs: false` disables the framework's automatic failed-job reporting issue:

```yaml wrap
safe-outputs:
  create-issue:
    max: 1
  report-failed-jobs: false
```

`report-failed-jobs` also accepts a GitHub Actions expression that resolves to a boolean. When omitted, it defaults to `true`.

Failed custom prerequisites are reported even when they skip the agent, provided activation succeeded and the run was neither cancelled nor stopped by the AI Credits quota guardrail. Failed prerequisites still block the agent; skipped or cancelled ones are not reported.

Custom safe-output jobs under `safe-outputs.jobs:` run after the agent completes and can expose a custom safe-output tool to it. The optional `output` field is the success message returned to the agent:

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

Suppresses specific threat-detection rules (`CTR-###`) that would otherwise block safe-output processing. Each entry requires a `rule` matching `CTR-###`, a non-empty `reason`, and an optional `expires` date (`YYYY-MM-DD`, UTC); after expiry the rule is enforced again.

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

`runs-on` applies only to the main agent job. `runs-on-slim` applies to all framework and generated jobs (activation, safe-outputs, unlock, and so on), accepts the same string, array, or runner-group forms, and defaults to `ubuntu-slim`. `safe-outputs.runs-on` and `safe-outputs.threat-detection.runs-on` take precedence where applicable.

`ubuntu-slim` supports job timeouts of at most 15 minutes. A literal workflow timeout above 15 minutes moves a compiler-default framework runner to `ubuntu-latest`; an explicit `ubuntu-slim` runner with such a timeout is a compilation error. Safe-output jobs that create or push pull requests default to `ubuntu-latest`; API-only ones can still use `ubuntu-slim`.

`timeout-minutes` accepts an integer or an expression such as `${{ inputs.timeout }}`. It bounds the `agentic_execution` step, defaulting to `${{ vars.GH_AW_DEFAULT_TIMEOUT_MINUTES }}` with a 20-minute fallback. Generated jobs have separate limits: `jobs.agent.timeout-minutes` (default `${{ vars.GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES }}`, else 60 minutes) and `jobs.detection.timeout-minutes` (default `${{ vars.GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES }}`, else 10 minutes). These apply to the compiled workflow, **not** to plain caller jobs that invoke a reusable workflow with job-level `uses:`.

**Supported runners for `runs-on:`**

| Runner | Status |
|--------|--------|
| `ubuntu-latest` | ✅ Default. Recommended for most workflows. |
| `ubuntu-24.04` / `ubuntu-22.04` | ✅ Supported. |
| `ubuntu-24.04-arm` | ✅ Supported. Linux ARM64 runner. |
| `macos-*` | ❌ Not supported. Docker is unavailable on macOS runners (no nested virtualization). See [FAQ](/gh-aw/reference/faq/). |
| `windows-latest` | Framework Bash steps execute in Ubuntu WSL; see [Windows runner execution](#windows-runner-execution). |

#### Windows runner execution

For `runs-on: windows-latest`, the compiler runs POSIX steps in framework jobs (including the agent job) through PowerShell launchers that execute Bash in Ubuntu WSL. Native Windows actions stay on the host; custom jobs and other Windows labels do not get this adapter.

The runner must provide working WSL2. Setup installs or selects Ubuntu, Linux tools, Docker, and the Node.js version chosen by `actions/setup-node`, and fails if WSL2 or the runtime cannot initialize.

The adapter gives framework commands a Linux environment, not a native Windows agent. Linux remains the recommended runner, and this does not guarantee end-to-end availability for every Windows configuration.

### Workflow Concurrency Control (`concurrency:`)

Automatically generates concurrency policies for the agent job. See [Concurrency Control](/gh-aw/reference/concurrency/).

### Environment Variables (`env:`)

Standard GitHub Actions `env:` syntax for workflow-level environment variables:

```yaml wrap
env:
  CUSTOM_VAR: "value"
```

Environment variables can be set at multiple scopes (workflow, job, step, engine, safe-outputs, and others) with defined precedence. See [Environment Variables](/gh-aw/reference/environment-variables/) for all 13 scopes and the precedence order.

> [!WARNING]
> Do not use credential expressions such as `${{ secrets.* }}`, `${{ github.token }}`, or `${{ toJSON(github.*) }}` in the workflow-level `env:` section. Environment variables defined here are passed directly to the agent container, which means secret values would be visible to the AI model. In strict mode, this is a compilation error. In non-strict mode, it emits a warning.
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

Caps chat iterations (model responses and tool calls) the AWF proxy allows per workflow run, across all engines. Defaults to `500`. Accepts an integer or an expression that resolves to one at runtime.

```yaml wrap
max-turns: 20
```

`max-runs:` is a **deprecated** top-level alias for `max-turns:`, accepted only as a fallback. Migrate with `gh aw fix`. See [Cost Management](/gh-aw/reference/cost-management/#cap-turns-per-run) for more details.

### Turn Cache Miss Limit (`max-turn-cache-misses:`)

Sets the maximum consecutive AWF cache misses before the API proxy blocks further requests. The value maps to `apiProxy.maxCacheMisses` and must be a positive integer. It defaults to `5`, or to the `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES` environment override when set. The value also applies to the threat-detection job, which uses the same LLM provider.

```yaml wrap
max-turn-cache-misses: 5
```

### AI Credits Guardrail (`max-ai-credits:`)

Sets the AWF AI Credits budget for cost enforcement. It is enabled by default at `1000` (`1k`); plain integers and `K`/`M` suffixes such as `5K` are accepted. AWF warns the agent at 80%, 90%, 95%, and 99% of the budget. AWF clamps budgets above its hard cap, 10,000 AI credits by default (runner operators can change it with `AWF_AI_CREDITS_HARD_CAP`; gh-aw does not set it). A negative value disables enforcement. With AWF releases after v0.29.11, notices continue against the agent deadline and hard cap unless [`sandbox.agent.token-steering: false`](/gh-aw/reference/sandbox/#token-steering-sandboxagenttoken-steering) is set.

```yaml wrap
max-ai-credits: 500
```

### Daily Per-Workflow AI Credits Guardrail (`max-daily-ai-credits:`)

Sets a 24-hour AI Credits cap for one workflow, summed across its recent runs in the repository. If the previous 24 hours already exceed the cap, the activation job warns, creates an issue, skips the agent job, and lets the conclusion job report the failure. Plain integers and `K`/`M` suffixes (for example `100000K` or `100M`) are accepted.

The guardrail is disabled when omitted; `-1` disables it explicitly. It is skipped for `workflow_call`, `repository_dispatch`, and `workflow_dispatch` runs that carry internal `aw_context` dispatch metadata.

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

Transient server and network/timeout failures are retried up to three attempts with short backoff. If a historical run's accounting stays unavailable, a positive `max-ai-credits` supplies a conservative per-run estimate, reported separately from measured usage and reinspected by later scans. Permission failures, rate limiting, quota-reserve stops, and explicit server retry restrictions never use this fallback.

A fresh, complete run listing always determines which observations count toward the 24-hour total.

By default, the activation job fails if the complete 24-hour total cannot be determined, including when run history is unreadable and no positive per-run fallback exists. Set `continue-on-error: true` to report the unknown total as a warning and continue:

```yaml wrap
max-daily-ai-credits:
  value: 10000
  continue-on-error: true
```

### Per-User Rate Limiting (`user-rate-limit:`)

Limits how often one user can trigger the workflow. When the limit is exceeded, the pre-activation job cancels the run before the agent executes. Applies to programmatic events such as `workflow_dispatch`, `issue_comment`, and `pull_request_review`. If `events` is omitted, events are inferred from `on:`, falling back to all supported programmatic events when none match.

```yaml wrap
user-rate-limit:
  max-runs-per-window: 5                      # Required: maximum runs per user per window (1-10)
  window: 60                                  # Optional: window in minutes (default: 60, max: 180)
  events: [workflow_dispatch, issue_comment]  # Optional: events to rate limit (inferred from `on:` when omitted; fallback to all supported programmatic events)
  ignored-roles: [admin, maintain]            # Optional: exempt roles (default: [admin, maintain, write])
```

`max-runs-per-window` also supports a GitHub Actions expression (for example `${{ inputs.max-runs-per-window }}`) that resolves to an integer at runtime.

Users with any `ignored-roles` are exempt. The defaults are `admin`, `maintain`, and `write`; `ignored-roles: []` rate-limits everyone, including administrators.

Legacy frontmatter that used a top-level `rate-limit:` section, or `max:`/`max-runs:` instead of `max-runs-per-window:`, must be migrated with `gh aw fix`.

See [Rate Limiting and Controls](/gh-aw/reference/rate-limiting-controls/) for more details.

### Secrets (`secrets:`)

Defines secret values passed to workflow components, such as MCP servers. Values must be GitHub Actions expressions that reference secrets, such as `${{ secrets.API_KEY }}`.

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

Always reference secrets through `${{ secrets.NAME }}` expressions, never plaintext. Prefer environment-specific secrets (via `environment:`) and limit access to the components that need them.

**Note:** For reusable workflows, pass secrets with `jobs.<job_id>.secrets` instead; the top-level `secrets:` field configures workflow-level secrets.

### Environment Protection (`environment:`)

Specifies the environment for deployment protection rules and environment-specific secrets. Standard GitHub Actions syntax.

```yaml wrap
environment: production
```

The activation job inherits this environment and its protection rules. If [`on.manual-approval`](/gh-aw/reference/triggers/#manual-approval-gates-manual-approval) is configured, its environment name takes precedence, while `deployment` and `url` from an object environment are kept.

The object form accepts `name`, optional `url`, and optional `deployment` (a boolean or GitHub Actions expression, such as `${{ github.ref_name == 'main' }}`). For environments that only gate secrets, set `deployment: false` to retain environment secrets and protection rules—including branch policies, required reviewers, and wait timers—without creating deployment records:

```aw wrap
environment:
  name: protected-secrets
  deployment: false
```

Omitting `deployment` preserves GitHub Actions' default deployment behavior. The CLI `--environment` override replaces the entire environment block with a literal name, including any `deployment` setting.

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

`endpoint` accepts a string, a `{url, headers}` object, or an array of endpoint objects for fan-out. `headers` takes a map or a comma-separated `key=value` string. `if-missing` is `error` (default), `warn`, or `ignore`. `attributes` adds custom span attributes, and `resource-attributes` appends OTel resource attributes; these are exported to external backends and are not secret-safe, so use only static strings or expressions there, never `secrets.*` or `vars.*`. See the [OpenTelemetry guide](/gh-aw/reference/open-telemetry/) and [attribute reference](/gh-aw/reference/open-telemetry-attributes/).

### Resources (`resources:`)

Declares companion workflow or action files to fetch alongside this workflow when running `gh aw add`.

```yaml wrap
resources:
  - triage-issue.md          # companion workflow
  - label-issue.md           # companion workflow
  - shared/helper-action.yml # supporting GitHub Action
```

Paths are relative to the workflow's location in the source repository, and `${{` expressions are not allowed. `gh aw add` downloads each listed file alongside the workflow so dependencies are available after installation. It also fetches workflows referenced by the [`dispatch-workflow`](/gh-aw/reference/safe-outputs/#workflow-dispatch-dispatch-workflow) safe output, even when not listed here.

### Runtimes (`runtimes:`)

Override default runtime versions for languages and tools. The compiler detects runtimes needed by tool configurations (for example `bash: ["node"]`) and workflow steps, then installs them. Pin versions, opt into previews, or point at custom setup actions such as forks or enterprise mirrors.

Each runtime takes a required `version` plus optional `action-repo` and `action-version` to override the default setup action:

| Runtime | Default Version | Default Setup Action |
|---------|----------------|---------------------|
| `node` | 24 | `actions/setup-node@v7` |
| `python` | 3.12 | `actions/setup-python@v5` |
| `go` | Root `go.mod`, or 1.27.4 if absent | `actions/setup-go@v7` |
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
    version: "24.22.1"
  python:
    version: "3.12"
    action-repo: "actions/setup-python"
    action-version: "v5"
```

Omitted runtimes use the defaults above. Runtimes from imported shared workflows are merged with your workflow's configuration.

`runtimes.node.version` also sets Node.js for npm-based engine installation steps, including when the value is inherited from an import.

If Go is required and `runtimes.go.version` is omitted, setup uses `go-version-file: go.mod` when the repository has a root `go.mod` and the workflow checks out that repository at the workspace root without an explicit ref. Otherwise the default version applies. An explicit `runtimes.go.version` takes precedence.

### `run-install-scripts`

Controls whether npm pre/post-install scripts are allowed during package installation. Configure this under `runtimes.node.run-install-scripts`. The default is `false`.

The shared npm installer disables lifecycle scripts for global installs via step-level `NPM_CONFIG_IGNORE_SCRIPTS: 'true'`, and local installs keep `--ignore-scripts`. Version pins and release-age cooldowns are unchanged.

```yaml wrap
runtimes:
  node:
    run-install-scripts: true
```

Enabling this increases supply chain risk because install hooks from dependencies can execute arbitrary code. In strict mode, `run-install-scripts: true` is rejected.

### Source Tracking (`source:`)

Tracks workflow origin in format `owner/repo/path@ref`. Automatically populated when using `gh aw add` to install workflows from external repositories. Optional for manually created workflows.

Explicit refs are preserved, so updates follow the requested branch or compatible releases. The optional `source-sha` field records the installed commit SHA, which `gh aw add` and `gh aw update` use as the base for three-way merges.

```yaml wrap
source: "githubnext/agentics/workflows/ci-doctor.md@v1.0.0"
```

### Redirect (`redirect:`)

Specifies a new canonical location, using the same `owner/repo/path@ref` format as `source:`, when a workflow has been moved or renamed. `gh aw add`, `gh aw add-wizard`, and `gh aw update` follow redirect chains (up to a depth limit), rewrite the local `source` field, and report redirect loops as errors.

```yaml wrap
redirect: "githubnext/agentics/workflows/new-workflow-name.md@main"
```

`gh aw compile` skips placeholder files that have `redirect:` but no `on:`, printing a message to run `gh aw update`. Files with both keys compile normally, and their redirect metadata stays available to `gh aw add` and `gh aw update`.

`gh aw update --no-redirect` fails the update instead of following a redirect, which is useful for auditing.

> [!NOTE]
> Authors set `redirect` when a workflow moves; if `gh aw update` shows one, the upstream workflow has been relocated.

### Tracker ID (`tracker-id:`)

Tags every asset (issues, pull requests, discussions, comments) the workflow creates with a hidden HTML comment, `<!-- gh-aw-tracker-id: … -->`, so GitHub search can find them all.

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

Adding the workflow from another repository then fails with `workflow 'owner/repo/internal-tooling' is private and cannot be added to other repositories`. Use this for internal tooling, sensitive automation, or repository-specific workflows not intended for reuse. This blocks only `gh aw add`; file visibility still follows repository access settings.

### `check-for-updates`

Controls whether the compile-agentic version update check runs in the activation job.

```yaml wrap
check-for-updates: true
```

When `true` (default), the activation job verifies the compiled version is not blocked and meets the minimum supported version. Set to `false` to disable this check (not allowed in strict mode).

### Feature Flags (`features:`)

Enable experimental or optional compiler and runtime behaviors as key-value pairs. See [Feature Flags](/gh-aw/reference/feature-flags/) for complete documentation.

### Strict Mode (`strict:`)

Enables enhanced security validation for production workflows. Default: `true`.

For `pull_request_target` workflows, the `on.pull_request_target` options documented in [Explicit Trusted Checkout Policy](/gh-aw/reference/checkout/#explicit-trusted-checkout-policy) can declare fixed trusted checkout pairs and acknowledge the trigger-specific warning without disabling strict mode.

```yaml wrap
strict: false  # Disable enhanced security validation for development/testing
```

Workflows compiled with `strict: false` cannot run on public repositories. The workflow fails at runtime with an error message prompting recompilation with strict mode.

To stop workflows from opting out, set `"strict": true` at the top level of `.github/workflows/aw.json`, which enforces strict mode for every `gh aw compile` in the repository. The repository setting accepts only `true`.

See [Network Permissions - Strict Mode Validation](/gh-aw/reference/network/#strict-mode-validation) for details on network validation and [CLI Commands](/gh-aw/setup/cli/#compile) for compilation options.

## Learn More

See also: [Trigger Events](/gh-aw/reference/triggers/), [AI Engines](/gh-aw/reference/engines/), [CLI Commands](/gh-aw/setup/cli/), [Workflow Structure](/gh-aw/reference/workflow-structure/), [Network Permissions](/gh-aw/reference/network/), [Feature Flags](/gh-aw/reference/feature-flags/), [Custom Steps and Jobs](/gh-aw/reference/steps-jobs/), [OpenTelemetry Guide](/gh-aw/reference/open-telemetry/), [Command Triggers](/gh-aw/reference/command-triggers/), [MCPs](/gh-aw/guides/mcps/), [Tools](/gh-aw/reference/tools/), and [Imports](/gh-aw/reference/imports/).
