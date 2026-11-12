---
description: Agentic workflow specific frontmatter fields for GitHub Agentic Workflows.
---

# Agentic Workflow Frontmatter Fields

### Agentic Workflow Specific Fields

- **`description:`** - Human-readable workflow description (string)
- **`emoji:`** - Optional single emoji used to represent the workflow visually; recommended for quicker recognition in workflow lists and status output (string)
- **`source:`** - Workflow origin tracking in format `owner/repo/path@ref` (string). Explicit refs are preserved, so updates follow the requested branch or compatible releases.
- **`source-sha:`** - Installed 40-character source commit SHA, managed by `gh aw add`/`gh aw update` as the three-way merge base; it does not change the tracking ref. Do not hand-edit (string)
- **`labels:`** - Array of labels to categorize and organize workflows (array)
  - Labels filter workflows in status/list commands
  - Example: `labels: [automation, security, daily]`
- **`metadata:`** - Custom key-value pairs compatible with custom agent spec (object)
  - Key names limited to 64 characters
  - Values limited to 1024 characters
  - Example: `metadata: { team: "platform", priority: "high" }`
- **`github-token:`** - GitHub token override (must use `${{ secrets.* }}` syntax). Not a top-level field: set it under `on:` (trigger checks), `tools.github`, or `safe-outputs`.
- **`on.roles:`** - Repository access roles that can trigger workflow (array or `"all"`). Default `[admin, maintainer, write]`; available roles: `admin`, `maintainer`, `maintain`, `write`, `triage`, `read`, `all`.
- **`on.bots:`** - Bot identifiers allowed to trigger workflow regardless of role permissions (array; e.g. `[dependabot[bot], renovate[bot], github-actions[bot]]`). The bot must be active (installed) on the repository to trigger.
- **`strict:`** - Enable enhanced validation for production workflows (boolean, defaults to `true`; strongly recommended)
  - Prefer `strict: true`; `strict: false` is dangerous, should be extremely rare, and must be carefully security reviewed before use
- **`model:`** - Default LLM model for the agentic engine (string). A nested `engine.model` takes precedence for that engine instance. Accepts full model IDs (e.g. `claude-3-5-sonnet-20241022`, `gpt-5.4`), aliases (e.g. `small`, `large`), and the router values `router`, `router/economy`, `router/balanced`, and `router/robust` (gh-aw per-task model routing; see [syntax-engine.md](syntax-engine.md) for requirements and details). **Unless the task needs a specific model, omit `model` only when the workflow is eligible for default routing (GitHub Copilot inference; Claude and Codex require `model-provider: github`); otherwise, use a router value where supported.** See the [default routing eligibility rules](https://github.github.com/gh-aw/reference/model-routing/#default-routing-for-workflows-without-a-model).
- **`max-turns:`** - AWF turn cap applied consistently across all agentic engines (integer or expression, e.g. `${{ inputs.max-turns }}`). The engine-level `engine.max-turns` is a deprecated alias kept for backward compatibility — prefer this top-level field.
- **`max-runs:`** - Deprecated legacy alias for the AWF invocation cap (`apiProxy.maxRuns`, defaults to `500` when omitted). Use `max-turns` instead; run `gh aw fix` to migrate.
- **`max-ai-credits:`** - Per-run AI Credits (AIC) budget enforced by the AWF firewall (integer or `K`/`M` short-form string like `100M`; default `1000`). Set a negative value to disable enforcement and token steering. See [token-optimization.md](token-optimization.md).
- **`max-turn-cache-misses:`** - Maximum consecutive AWF cache misses allowed before the API proxy blocks further requests (integer, default `5`). Maps to `apiProxy.maxCacheMisses`; precedence is frontmatter → `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES` env override → built-in default.
- **`max-tool-calls:`** - Run-wide cap on tool dispatches, including nested/subagent calls (positive integer or expression). Reserved before execution, so failed and denied calls count. Enforced only by the built-in Copilot SDK driver (CLI mode and custom drivers/harnesses reject it) and Pi drivers. Distinct from per-tool `tools.github.allowed[].max-calls` and from `max-tool-denials`.
- **`models:`** - Model policy and optional pricing (object). Experimental policy fields `allowed` / `blocked` (lists of model names or patterns) restrict which models the workflow may use; they map to AWF `apiProxy.allowedModels` / `disallowedModels` and merge as unions across imports. Environment-variable overrides are supported. The separate `providers` field supplies custom pricing, and `default-ai-credits-pricing` (`input`/`output` in $/1M tokens) is a fallback rate for models absent from the built-in table — required to avoid HTTP 400 rejections for self-hosted/BYOK models (set both to `0` for free/local models). See [token-optimization.md](token-optimization.md).

  ```yaml
  models:
    allowed: ["gpt-5", "claude-*"]
    blocked: ["*-preview"]
  ```
- **`max-daily-ai-credits:`** - Per-user 24-hour AI Credits (AIC) guardrail: activation blocks execution once the triggering user's aggregated AI Credits for this workflow over the last 24h exceed the threshold (integer or `K`/`M` short-form string, or `-1`). Disabled by default; omit the field to leave the guardrail off, or set an explicit threshold to enable it. See [token-optimization.md](token-optimization.md).
- **`user-rate-limit:`** - Rate limiting configuration to prevent users from triggering the workflow too frequently (object)
  - **`max-runs-per-window:`** - Maximum runs allowed per user per time window (required, integer 1-10)
  - **`window:`** - Time window in minutes (integer 1-180, default: 60)
  - **`events:`** - Event types to apply rate limiting to (array; if omitted, applies to all programmatic events)
    - Available: `workflow_dispatch`, `issue_comment`, `pull_request_review`, `pull_request_review_comment`, `issues`, `pull_request`, `discussion_comment`, `discussion`
  - **`ignored-roles:`** - Roles exempt from rate limiting (array of `admin`, `maintain`, `write`, `triage`, `read`; default: `[admin, maintain, write]`). Set to `[]` to apply to all users.
  - Example:

    ```yaml
    user-rate-limit:
      max-runs-per-window: 5
      window: 60
      ignored-roles: [admin, maintain]
    ```

- **`check-for-updates:`** - Whether the activation job checks that the compiled `gh-aw` version is still supported (boolean, default `true`). When `true`, blocked versions fail fast and below-recommended versions warn. Set `false` only for isolated environments (compiler then warns at compile time).

- **`features:`** - Feature flags for experimental or optional features (object)
  - Each flag is a key-value pair; boolean flags (`true`/`false`) or string values are accepted
  - Known feature flags:
    - `disable-xpia-prompt: true` - Disable the built-in cross-prompt injection attack (XPIA) system prompt
    - `action-tag: "v0"` - Pin compiled action references to a specific version of the `gh-aw-actions` repository. Accepts version tags (e.g., `"v0"`, `"v1"`, `"v1.0.0"`) or a full 40-character commit SHA. When set, overrides the compiler's default action mode and resolves all action references from the external `github/gh-aw-actions` repository at the specified tag.
    - `action-mode: "script"` - Control how the compiler generates action references: `"dev"` (local paths, default), `"release"` (SHA-pinned remote), `"action"` (gh-aw-actions repo), `"script"` (direct shell calls). Can also be overridden via `--action-mode` CLI flag.
    - `difc-proxy: true` - Enable DIFC (Data Integrity and Flow Control) proxy injection. When set alongside `tools.github.min-integrity`, injects proxy steps around the agent for full network-boundary integrity enforcement.
    - `cli-proxy: true` - Enable AWF CLI proxy sidecar for secure read-only `gh` CLI access without exposing `GITHUB_TOKEN` (requires AWF v0.26.0+). Prerequisite for `integrity-reactions`; the compiler enables it automatically when `integrity-reactions: true` is set.
    - `integrity-reactions: true` - Enable reaction-based integrity promotion/demotion. Maintainers can use 👍/❤️ reactions to promote content to `approved` and 👎/😕 to demote it to `none`. Compiler automatically enables `cli-proxy`. Requires `tools.github.min-integrity` to be set and MCPG >= v0.2.18. Defaults: endorsement reactions THUMBS_UP/HEART, disapproval reactions THUMBS_DOWN/CONFUSED, endorser-min-integrity: approved, disapproval-integrity: none.
    - `dangerously-disable-sandbox-agent: true` - Required when `sandbox.agent: false` is set. This opt-out is rejected in strict mode.
    - `dangerously-allow-pull-request-target-forks-all: true` - Lets strict mode accept `on.pull_request_target.forks: ["*"]`; the warning remains. Must be in the main workflow's top-level `features:` (imported features do not count). See [triggers.md](triggers.md).

- **`experiments:`** - A/B testing experiments for balanced variant selection (object)
  - Maps experiment names to variant lists (bare array) or full config objects
  - Bare array form: `prompt_style: [concise, detailed]` — round-robin balanced across runs
  - Object form for weighted/gated experiments:

    ```yaml
    experiments:
      prompt_style:
        variants: [concise, detailed, step_by_step]
        weight: [2, 1, 1]           # Optional: proportional weights (defaults to round-robin)
        start_date: "2026-05-01"    # Optional: ISO-8601; returns control variant before this date
        end_date: "2026-06-01"      # Optional: ISO-8601; returns control variant after this date
        description: "Verbosity test"  # Optional: experiment description
        metric: "token_count"       # Optional: primary metric name
        issue: "42"                 # Optional: linked tracking issue number
    ```

  - Selected variant available as `${{ experiments.<name> }}` and in `{{#if experiments.<name> }}` template blocks
  - See [A/B Testing Experiments](experiments.md) for full design guidance
- **`evals:`** - ⚠️ Experimental. BinEval binary (YES/NO) evaluation questions run after safe-outputs and before the conclusion job. Shorthand: a list of `{ id, question, model? }` objects. Extended form: object with `questions`, plus optional `model` (default alias/ID for all questions) and `runs-on`.

- **`imports:`** - Array of workflow specifications to import (array)
  - Format: `owner/repo/path@ref` or local paths like `shared/common.md`
  - Markdown files under `.github/agents/` are treated as custom agent files
  - Only one agent file is allowed per workflow
  - See [Imports Field](syntax-tools-imports.md#imports-field) section for detailed documentation
- **`inlined-imports:`** - Inline all imports at compile time (boolean, default: `false`)
  - When `true`, all imports (including those without inputs) are inlined in the generated `.lock.yml` instead of using runtime-import macros
  - The frontmatter hash covers the entire markdown body when enabled, so any content change invalidates the hash
  - **Required for repository rulesets**: Workflows used as required status checks in repository rulesets run without access to repository files at runtime. Set `inlined-imports: true` to bundle all imported content at compile time to avoid "Runtime import file not found" errors
  - **Constraint**: Cannot be combined with agent file imports (`.github/agents/` files). Remove any custom agent file imports before enabling
- **`import-schema:`** - Define typed input parameters for this shared workflow (object). Use when other workflows import this one via the `uses:`/`with:` syntax (see [Imports Field](syntax-tools-imports.md#imports-field)).
  - Parameters are accessible inside the shared workflow via `${{ github.aw.import-inputs.<name> }}` expressions
  - Object inputs (type: `object`) allow one-level deep sub-fields: `${{ github.aw.import-inputs.<name>.<subkey> }}`
  - Fields per parameter:
    - `type:` - Input type: `string`, `number`, `boolean`, `choice`, or `array`
    - `description:` - Human-readable parameter description
    - `required:` - Whether the input is required when imported (default: `false`)
    - `default:` - Default value when not provided
    - `options:` - Allowed values for `choice` type inputs
  - Example:

    ```yaml
    import-schema:
      environment:
        type: choice
        description: "Target environment"
        options: [dev, staging, prod]
        required: true
      max-issues:
        type: number
        default: 5
    ```

- **`mcp-servers:`** - MCP (Model Context Protocol) server definitions (object)
  - Defines custom MCP servers for additional tools beyond built-in ones

- **`private:`** - Mark this workflow as private, preventing it from being shared via `gh aw add` (boolean, default: `false`)
  - Example: `private: true`

- **`redirect:`** - Workflow relocation path for updates (string). When present, `gh aw update` follows this location and rewrites the `source:` field. Format: `owner/repo/path@ref` or full GitHub URL.
  - Example: `redirect: "org/agentics/workflows/my-workflow-v2.md@main"`

- **`resources:`** - Additional workflow or action files fetched alongside this workflow when running `gh aw add` (array). Entries are relative paths from the same directory to `.md` or `.yml`/`.yaml` files.
  - Example: `resources: [shared/tool-setup.md, shared/mcp/tavily.md]`

- **`threat-detection-suppress:`** - Auditable false-positive suppression annotations for compiler threat-detection rules (array of objects)
  - Each entry requires `rule:` (a `CTR-###` identifier) and `reason:` (non-empty string); optional `expires:` (ISO-8601 date)
  - Example: `threat-detection-suppress: [{ rule: CTR-025, reason: "reviewed false positive", expires: "2026-12-31" }]`

- **`ambient-folders:`** - Workspace-relative folders bundled into the activation artifact and restored before the agent runs (array of strings)
  - Useful for activation steps that generate reusable prompt, skill, or agent context ahead of the agent job
  - Merges with `ambient-folders` declared by imported workflows
  - Example: `ambient-folders: [".claude/skills", ".github/agents"]`

- **`tracker-id:`** - Optional identifier to tag all created assets (string)
  - Must be at least 8 characters and contain only alphanumeric characters, hyphens, and underscores
  - This identifier is inserted in the body/description of all created assets (issues, discussions, comments, pull requests)
  - Enables searching and retrieving assets associated with this workflow
  - Examples: `"workflow-2024-q1"`, `"team-alpha-bot"`, `"security_audit_v2"`

- **`secret-masking:`** - Configuration for secret redaction behavior in workflow outputs and artifacts (object)
  - `steps:` - Additional secret redaction steps to inject after the built-in secret redaction (array)
  - Use this to mask secrets in generated files using custom patterns
  - Example:

    ```yaml
    secret-masking:
      steps:
        - name: Redact custom secrets
          run: find /tmp/gh-aw -type f -exec sed -i 's/password123/REDACTED/g' {} +
    ```

- **`observability:`** - Workflow observability and telemetry configuration (object)
  - **`otlp:`** - Export OpenTelemetry spans to any OTLP-compatible backend (Honeycomb, Grafana Tempo, Sentry, etc.) (object)
    - `endpoint:` - OTLP collector URL string, a `{url, headers}` object, or an array of them for concurrent multi-endpoint export. Static hostnames are added to the AWF firewall allowlist automatically. Supports GitHub Actions expressions.
    - `github-app:` - Optional runtime auth configuration.
      - Preferred: provide GitHub App credentials (`app-id`/`client-id` + `private-key`) to mint a token with `actions/create-github-app-token` before `actions/setup`.
      - OIDC mode is used when `github-app` is configured without credentials (`app-id`/`client-id` + `private-key`).
      - OIDC mode requires `permissions.id-token: write` on the workflow/job.
    - `workload-identity:` - Exchange a GitHub Actions OIDC token for a cloud access token before OTLP export. Only `provider: google` is currently supported; requires `audience:` (Google Workload Identity Provider resource name) and accepts optional `service-account:` to impersonate after STS token exchange.
    - `headers:` - Map of HTTP header names to values (e.g. `Authorization: ${{ secrets.OTLP_AUTH }}`) for the string `endpoint:` form; object/array endpoints carry their own `headers:`. Injected as `OTEL_EXPORTER_OTLP_HEADERS`. The comma-separated `key=value` string form is deprecated.
    - `if-missing:` - `error` (default), `warn`, or `ignore` when endpoint/header values resolve empty at runtime (affects MCP gateway OTLP setup only).
    - `attributes:` - Optional map of custom span attributes; values are static strings or `{{ <span-attribute> }}` templates. Exported to backends and masked in runner logs.
    - `resource-attributes:` - Optional map of additional OTEL resource attributes appended to gh-aw/GitHub defaults. Values may be static strings or GitHub Actions expressions. Do not use `secrets.*` or `vars.*` here because resource attributes are exported to external observability backends and are not treated as secret values.
  - Runtime controls (Actions variables, not frontmatter; recompile once to add the lookup): `OTEL_EXPORTER_OTLP_PROTOCOL` selects `http/json` (default) or `http/protobuf` (gRPC unsupported; per-signal `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`/`OTEL_EXPORTER_OTLP_LOGS_PROTOCOL` override it); `GH_AW_OTLP_LOGS_ENABLED=true` opts in to compact unified-session OTLP logs posted to `<endpoint>/v1/logs`, capped by `GH_AW_OTLP_LOGS_MAX_BYTES` (default `65536`). Manage the protocol with `gh aw env` key `default_otlp_protocol`.
  - Example:

    ```yaml
    observability:
      otlp:
        endpoint:
          - url: ${{ secrets.GH_AW_OTEL_ENDPOINT }}
            headers:
              Authorization: ${{ secrets.GH_AW_OTEL_AUTHORIZATION }}
        github-app:
          app-id: ${{ vars.APP_ID }}
          private-key: ${{ secrets.APP_PRIVATE_KEY }}
    ```

    Every job emits setup and conclusion spans with rich attributes (`gh-aw.job.name`, `gh-aw.workflow.name`, `gh-aw.engine.id`, token usage). All jobs in a run share one trace ID. Dispatched child workflows inherit the parent's trace context via `aw_context`.

    To emit a custom span from a `github-script` step, resolve the helper through `RUNNER_TEMP` rather than a hardcoded `/tmp` path:

    ```javascript
    const path = require('path');
    const otlp = require(path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'otlp.cjs'));
    ```

- **`runtimes:`** - Runtime environment version overrides (object)
  - Allows customizing runtime versions (e.g., Node.js, Python) or defining new runtimes
  - Runtimes from imported shared workflows are also merged
  - Each runtime is identified by a runtime ID (e.g., 'node', 'python', 'go')
  - Runtime configuration properties:
    - `version:` - Runtime version as string or number (e.g., '22', '3.12', 'latest', 22, 3.12)
    - `action-repo:` - GitHub Actions repository for setup (e.g., 'actions/setup-node')
    - `action-version:` - Version of the setup action (e.g., 'v4', 'v5')
    - `if:` - Optional GitHub Actions condition to control when runtime setup runs (e.g., `"hashFiles('go.mod') != ''"`)
    - `cooldown:` - Enable a default 3-day dependency cooldown for installs on this runtime (boolean, default: `true`); set `false` to disable
  - Example:

    ```yaml
    runtimes:
      node:
        version: "22"
      python:
        version: "3.12"
        action-repo: "actions/setup-python"
        action-version: "v5"
      go:
        version: "1.22"
        if: "hashFiles('go.mod') != ''"   # Only install Go when go.mod exists
    ```

- **`runtimes.node.run-install-scripts:`** - Allow npm pre/post install scripts to execute during package installation for the Node.js runtime (boolean, default: `false`)
  - By default, generated Node.js runtime and engine npm installs disable scripts using `NPM_CONFIG_IGNORE_SCRIPTS: 'true'` for global installs or `--ignore-scripts` for local installs to prevent supply chain attacks via malicious install hooks
  - Set `run-install-scripts: true` under `runtimes.node` to allow scripts for Node.js installs
  - A supply chain security warning is emitted at compile time; in strict mode this is an error

- **`checkout:`** - Override how the repository is checked out in the agent job (object, array, or `false`). By default, the workflow automatically checks out the repository. Set to `false` to disable automatic checkout entirely, or set `permissions.contents: none` to skip only the automatic workflow-repository checkout while still running other explicitly configured `checkout:` entries. Supports single or multiple checkout entries, GitHub App/token auth, sparse/shallow/LFS/submodule/wiki options, and an opt-in `multi: true` allowlisted multi-repository mode. Full field reference and examples: [syntax-checkout.md](syntax-checkout.md).

- **`jobs:`** - Groups together all the jobs that run in the workflow (object)
  - Standard GitHub Actions jobs configuration
  - Each job can have: `name`, `runs-on`, `steps`, `needs`, `if`, `env`, `permissions`, `timeout-minutes`, etc.
  - For most agentic workflows, jobs are auto-generated; only specify this for advanced multi-job workflows
  - **Security Notice**: Custom jobs run OUTSIDE the firewall sandbox. Execute with standard GitHub Actions security but NO network egress controls. Use only for deterministic preprocessing, data fetching, or static analysis—not agentic compute or untrusted AI execution.
  - **Detection gate**: custom jobs that depend (directly or transitively) on `agent`/`detection` get a direct `detection` dependency and a non-overridable success guard; failed, cancelled, skipped, or warn-flagged detection blocks them even with `if: always()`. Custom jobs cannot run between `agent` and `detection`. For leaf status reporters only, set `detection-gate: status-only` (compiler-only key) to run anyway: they may read outputs of compiler-owned jobs (e.g. `safe_outputs`) and of pre-agent custom jobs listed directly in `needs` (pass via `env`, never interpolate into `run`/script code). The compiler rejects them if they download artifacts, call opaque reusable workflows, read other job outputs, or reference agent/safe-output files; no job may depend on them.
  - **`setup-steps:`** - Steps injected at the earliest point in a custom or built-in job, before framework GitHub App token minting and before checkout (array). Use this for OIDC login, secret fetch, and credential bootstrap that must happen before framework token/checkout steps. Imported `setup-steps` run before main workflow `setup-steps`.
  - **`pre-steps:`** - Steps injected after framework setup scaffolding and before the job's main `steps:` in a custom or built-in job (array). For built-in jobs, this is after the `id: setup` step (which includes framework token minting/checkout setup) and before the first checkout. Imported `pre-steps` run before main workflow `pre-steps`.
  - **`setup-steps` vs `pre-steps`** - Use `setup-steps` for work that must run before framework GitHub App token minting and checkout (e.g., OIDC/secret bootstrap). Use `pre-steps` for work that should run later, after setup scaffolding and before the job's main `steps:`.
  - **Migration note** - No migration is required. `setup-steps` is additive; existing workflows that only use `pre-steps` continue to behave as before.
  - Example:

    ```yaml
    jobs:
      custom-job:
        runs-on: ubuntu-latest
        setup-steps:
          - name: Bootstrap credentials
            run: echo "runs before framework token/checkout setup"
        pre-steps:
          - name: Pre-flight setup
            run: echo "runs before checkout"
        steps:
          - name: Custom step
            run: echo "Custom job"
    ```

  - `setup-steps`/`pre-steps` also apply to built-in jobs (e.g. `activation`): use `setup-steps` for OIDC/secret bootstrap that must run before framework token minting, then verify the result in `pre-steps`. Use `jobs.activation.steps` for activation work that must run after the activation checkout and before the activation artifact is staged.
  - **`needs`/`if` on built-in jobs** — targeting a compiler-generated job (`agent`, `activation`, `safe_outputs`, etc.) under `jobs:` also accepts additive `needs` and `if`: `jobs.agent.needs` merges with compiler-generated dependencies, and `jobs.agent.if` combines with compiler-generated conditions using `&&`. Use this to gate the agent job on a custom setup job's outcome.

- **`engine:`** - AI processor configuration (string or object: `id`, `model`, `permission-mode`, `agent`, `max-continuations`, `driver`, `copilot-sdk`, `auth`, and more). See [syntax-engine.md](syntax-engine.md) for the full field reference, per-engine support notes, and inline driver examples.

- **`network:`** - Network access control for AI engines (top-level field)
  - String format: `"defaults"` (curated allow-list of development domains)
  - Empty object format: `{}` (no network access)
  - Object format for custom permissions:

    ```yaml
    network:
      allowed:
        - "example.com"
        - "*.trusted-domain.com"
        - "https://api.secure.com"        # Optional: protocol-specific filtering
      blocked:
        - "blocked-domain.com"
        - "*.untrusted.com"
        - python                          # Block ecosystem identifiers
    ```

  - **Firewall (AWF) configuration** is set under `sandbox.agent`, not `network`. Use `sandbox.agent.version` to pin the AWF version (see below). The legacy `network.firewall` field is deprecated; run `gh aw fix` to migrate.

- **`sandbox:`** - Sandbox configuration for AI engines (string or object)
  - String format: `"default"` (default sandbox), `"awf"` (Agent Workflow Firewall)
  - Object format to pin an AWF version (strict mode requires explicit `id: awf`):

    ```yaml
    sandbox:
      agent:
        id: awf                     # Required in strict mode
        version: "v0.25.29"         # Optional: pin AWF version
        model-fallback: false       # Optional: disable model fallback (default true); set false for BYOK Azure OpenAI to prevent deployment-name rewriting
        token-steering: false       # Optional: disable API proxy token steering to preserve the configured provider and model
    ```

  - When `engine.env` sets `OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL` (custom provider endpoints, e.g. OpenRouter), `model-fallback` is disabled automatically so provider-specific model slugs pass through verbatim; set it explicitly to override.

  - To disable the agent firewall while keeping MCP gateway enabled, set `strict: false` and enable the dangerous sandbox opt-out:

    ```yaml
    features:
      dangerously-disable-sandbox-agent: true
    sandbox:
      agent: false
    strict: false
    ```

  - **`sandbox.agent.runtime`** (string) selects the sandbox security and topology profile: `docker` (default: rootless AWF with network isolation), `docker-sudo-iptables` (privileged AWF with legacy iptables networking and host/service access), or `cloud-hypervisor` (preview KVM runtime). Omitting the field is equivalent to `docker`. Cloud Hypervisor is incompatible with `runner.topology: arc-dind`.
  - **`sandbox.agent.allow-host-ports`** (array of integers) additional host TCP ports the agent may connect to. Requires `runtime: docker-sudo-iptables`. Ports published by `services:` are reached via `--allow-host-service-ports` instead; use this only for host daemons not declared there. There is no `sandbox.agent.legacy-security` field — that mode was replaced by `runtime: docker-sudo-iptables`.
  - **Strict mode**: `sandbox.agent` blocks without an explicit `id: awf` are rejected in strict mode. Any non-nil, non-disabled agent config without `id`/`type` defaults to AWF at runtime.

- **`enclaves:`** - AWF-owned private-repository executors exposed only through the compiler-launched MCP gateway (array). See [enclaves.md](enclaves.md) for the full schema and usage.

- **`tools:`** - Tool configuration for the coding agent (`github`, `agentic-workflows`, `edit`, `web-fetch`, `web-search`, `bash`, `playwright`, custom MCP server names, plus `timeout`/`startup-timeout`/`cli-proxy`). See [syntax-tools-imports.md](syntax-tools-imports.md#tool-configuration) for the full schema (GitHub `mode`/`toolsets`/integrity fields, bash allowlist decision rule, Playwright CLI mode).

- **`safe-outputs:`** - Safe output processing configuration. See [safe-outputs.md](safe-outputs.md) for complete documentation of all output types: `create-issue`, `create-discussion`, `add-comment`, `create-pull-request`, `push-to-pull-request-branch`, `close-issue`, `close-discussion`, `update-issue`, `update-pull-request`, `add-labels`, `remove-labels`, `replace-label`, `dispatch-workflow`, `call-workflow`, `create-code-scanning-alert`, `upload-asset`, `upload-artifact`, `assign-to-agent`, `assign-to-user`, `approve-workflow-run`, and more.

  **Key safe-outputs global fields** (detail in [safe-outputs-runtime.md](safe-outputs-runtime.md)): `github-token`, `github-app`, `staged` (preview mode, no API calls), `footer`, `threat-detection`, `runs-on` (default `ubuntu-slim`), `messages`, `env`, `max-patch-size` (KB, default `4096`).


- **`mcp-scripts:`** - Define custom lightweight MCP tools as JavaScript, shell, Python, or Go scripts (object)
  - Tools mounted in MCP server with access to specified secrets
  - Each tool requires `description` and one of: `script` (JavaScript), `run` (shell), `py` (Python), or `go` (Go)
  - Tool configuration properties:
    - `description:` - Tool description (required)
    - `inputs:` - Input parameters with type and description (object)
    - `script:` - JavaScript implementation (CommonJS format)
    - `run:` - Shell script implementation
    - `py:` - Python script implementation
    - `go:` - Go script implementation (executed via `go run`, receives inputs as JSON via stdin)
    - `env:` - Environment variables for secrets (supports `${{ secrets.* }}`)
    - `dependencies:` - Runtime packages installed before first invocation (list of strings). Manager inferred from script type: `script`→npm, `py`→pip, `go`→`go get`, `run`→apt. Must be exact-version-pinned (`name@1.2.3`, `name==1.2.3`, `module@v1.2.3`, `name=1.6`); floating refs are rejected.
    - `timeout:` - Execution timeout in seconds (default: 60)
  - Example:

    ```yaml
    mcp-scripts:
      search-issues:
        description: "Search GitHub issues using API"
        inputs:
          query: { type: string, description: "Search query", required: true }
        script: |
          const { Octokit } = require('@octokit/rest');
          const octokit = new Octokit({ auth: process.env.GH_TOKEN });
          const r = await octokit.search.issuesAndPullRequests({ q: inputs.query });
          return r.data.items;
        dependencies: ["@octokit/rest@21.0.2"]
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    ```

- **`slash_command:`** - Command trigger configuration for /mention workflows (under `on:`)
- **`cache:`** - Cache configuration for workflow dependencies (object or array)
- **`cache-memory:`** - Memory MCP server with persistent cache storage (boolean or object, under `tools:`)
- **`repo-memory:`** - Repository-specific memory storage (boolean, under `tools:`)
- **`comment-memory:`** - Managed issue/PR comment memory with file-based agent editing (boolean or object, under `tools:`)
