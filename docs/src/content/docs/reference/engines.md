---
title: AI Engines
description: Compare the built-in AI engines for GitHub Agentic Workflows, including selection, authentication, capabilities, limitations, and examples for Copilot, Claude Code, Codex, Gemini, and Pi.
sidebar:
  order: 600
---

GitHub Agentic Workflows uses an [AI engine](/gh-aw/reference/glossary/#engine) - usually a coding agent - to interpret a workflow's Markdown instructions. Set the engine in YAML frontmatter; GitHub Actions then runs that engine with the workflow's configured tools, permissions, sandbox, and outputs.

## Built-in AI engines

Set `engine:` in workflow frontmatter and configure the corresponding authentication method:

| AI engine | `engine:` value | Authentication | Setup and example |
|---|---|---|---|
| [GitHub Copilot CLI](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/use-copilot-cli) (default) | `copilot` | [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission) or [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) | [Using GitHub Copilot with GitHub Agentic Workflows](/gh-aw/engines/copilot/) |
| [Claude Code](https://www.anthropic.com/index/claude) | `claude` | [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key) or [Anthropic WIF](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif) | [Using Claude Code with GitHub Agentic Workflows](/gh-aw/engines/claude/) |
| [OpenAI Codex](https://openai.com/blog/openai-codex) | `codex` | `CODEX_API_KEY` or [`OPENAI_API_KEY`](/gh-aw/reference/auth/#openai_api_key) | [Using OpenAI Codex with GitHub Agentic Workflows](/gh-aw/engines/codex/) |
| [Google Gemini CLI](https://github.com/google-gemini/gemini-cli) | `gemini` | [`GEMINI_API_KEY`](/gh-aw/reference/auth/#gemini_api_key) or [Google WIF](/gh-aw/reference/auth/#google-workload-identity-federation-wif) | [Using Google Gemini with GitHub Agentic Workflows](/gh-aw/engines/gemini/) |
| [Pi](https://www.npmjs.com/package/@earendil-works/pi-coding-agent) | `pi` | Copilot authentication by default; Anthropic or OpenAI/Codex key for a provider-prefixed `model:` | [Using Pi with GitHub Agentic Workflows](/gh-aw/engines/pi/) |

Copilot CLI is the default, so `engine:` can be omitted when using Copilot. Copilot SDK mode is an execution mode of the Copilot engine, not a separate engine; enable it with `engine: copilot` and `copilot-sdk: true`. See [Copilot SDK support](#copilot-sdk-support).

## Unsupported engine samples

The OpenCode, Aider, Crush, Cursor, DeepSeek Harness, Kiro, and Pydantic AI integrations in this repository are **samples only**. They are not officially supported by gh-aw and have no compatibility or maintenance commitment.

| Sample engine | Sample definition |
|---------------|-------------------|
| [OpenCode](https://opencode.ai) | `.github/workflows/shared/opencode.md` |
| [Aider](https://aider.chat/docs/) | `.github/workflows/shared/aider.md` |
| [Crush](https://github.com/charmbracelet/crush) | `.github/workflows/shared/crush.md` |
| [Cursor](https://cursor.com/docs/cli) | `.github/workflows/shared/cursor.md` |
| [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) | `.github/workflows/shared/deepseek-harness.md` |
| [Kiro](https://kiro.dev/) | `.github/workflows/shared/kiro.md` |
| [Pydantic AI](https://ai.pydantic.dev/) | `.github/workflows/shared/pydantic.md` |

Engine owners should publish and maintain their own Markdown integration definition. Users should import the definition from that owner-maintained source, pinned to a tag or commit SHA. The in-repository files are examples for authors, not supported engine integrations.

## Which engine should I choose?

Choose the engine that matches the required capabilities, identity mechanism, and existing provider access. Copilot supports the broadest engine-specific feature set, including native agent selection, custom harnesses, and continuation mode. Claude Code and Codex provide native web search when enabled. Gemini supports Google WIF and per-command bash restrictions. Pi supports multiple providers but requires proxy-specific tool configuration.

Changing engines requires updating `engine:` and may also require different authentication, tools, model names, or network access. Review the setup guide and comparison before switching.

## Engine Feature Comparison

Not all features are available across all engines. The table below summarizes per-engine support for commonly used workflow options:

| Feature | Copilot | Claude | Codex | Gemini | Pi |
|---------|:-------:|:------:|:-----:|:------:|:--:|
| `max-turns` (top-level AWF invocation cap; `max-runs` deprecated) | ✅ | ✅ | ✅ | ✅ | ✅ |
| `engine.max-turns` (deprecated nested alias) | ❌ | ✅ | ❌ | ❌ | ❌ |
| `max-continuations` | ✅ | ❌ | ❌ | ❌ | ❌ |
| `tools.web-fetch` | ✅ | ✅ | ✅ | ✅ | ✅ |
| `tools.web-search` | via MCP | ✅ (native) | ✅ (native, opt-in) | via MCP | ❌ |
| `engine.agent` (native custom-agent selection) | ✅ | ❌ | ❌ | ❌ | ❌ |
| `engine.api-target` (custom endpoint) | ✅ | ✅ | ✅ | ✅ | ✅ |
| `engine.bare` (disable context loading) | ✅ | ✅ | ❌ | ❌ | ✅ (no-op; already bare) |
| `engine.harness` (custom harness script) | ✅ | ❌ | ❌ | ❌ | ❌ |
| Per-command `tools.bash` allowlist | ✅ | ✅ | ❌ (disable only) | ✅ | ❌ |
| Native MCP server integration | ✅ | ✅ | ✅ | ✅ | ❌ |
| Agent Plugins (`plugins`) | ✅ | ✅ | ✅ | ❌ | ❌ |

`max-turns` (default `500`, legacy alias `max-runs`) and `max-ai-credits` (default `1000`) are top-level frontmatter fields supported by all engines. `engine.max-turns` is a deprecated nested alias that still limits Claude iterations when present; `max-continuations` enables Copilot continuation mode. Claude and Codex have native web search support; Codex requires explicit `tools: web-search:` configuration. Copilot and Gemini can use a third-party MCP server for search. Top-level `plugins` is experimental, uses the [Agent Plugins](https://agent-plugins.org) format, and is supported by Copilot, Claude, Codex, and any imported engine definition that declares a `behaviors.plugins` block (such as the shared Cursor and Kiro engines). See [Using Web Search](/gh-aw/reference/web-search/) and [Agent Plugins](/gh-aw/reference/frontmatter/#agent-plugins-plugins).

## Shared imported engines

Most workflows use one of the supported engine IDs above. You can also define an engine in an imported shared workflow and reference it by `engine.id`. Third-party engine owners should publish and maintain these Markdown definition files in their own repositories.

Use an owner-maintained, pinned import:

```yaml wrap
engine:
  id: example-engine

imports:
  - owner/repository/.github/workflows/example-engine.md@v1.2.3
```

Do not treat imported definitions as supported unless their engine owner explicitly supports them. The OpenCode, Aider, Crush, Cursor, and Kiro files listed above remain samples; copy or adapt them only under the maintenance and support terms provided by their respective owners.

> [!NOTE]
> There is no flat `engine: custom` value. `engine:` in string form only accepts a built-in engine ID (`copilot`, `claude`, `codex`, `gemini`, `pi`); any other value fails compilation. Third-party and self-defined engines always use the nested object form with `engine.id` set to the ID declared by an imported engine definition, as shown above.

## Extended Coding Agent Configuration

Workflows can specify extended configuration for the coding agent:

```yaml wrap
engine:
  id: copilot
  version: latest                       # defaults to latest
  model: gpt-5                          # example override; omit to use engine default
  command: /usr/local/bin/copilot       # custom executable path
  args: ["--add-dir", "/workspace"]     # custom CLI arguments
  agent: agent-id                       # custom agent file identifier
  api-target: api.acme.ghe.com          # custom API endpoint hostname (GHEC/GHES)
```

### Pinning a Specific Engine Version

By default, workflows install the latest available version of each engine CLI. To pin to a specific version, set `version` to the desired release:

| Engine | `id` | Example `version` |
|--------|------|-------------------|
| GitHub Copilot CLI | `copilot` | `"0.0.422"` |
| Claude Code | `claude` | `"2.1.70"` |
| Codex | `codex` | `"0.111.0"` |
| Gemini CLI | `gemini` | `"0.31.0"` |
| Pi | `pi` | `"0.72.1"` |

```yaml wrap
engine:
  id: copilot
  version: "0.0.422"
```

Pinning is useful when you need reproducible builds or want to avoid breakage from a new CLI release while testing. Remember to update the pinned version periodically to pick up bug fixes and new features.

`version` also accepts a GitHub Actions expression string, enabling `workflow_call` reusable workflows to parameterize the engine version via caller inputs. Expressions are passed injection-safely through an environment variable rather than direct shell interpolation:

```yaml wrap
on:
  workflow_call:
    inputs:
      engine-version:
        type: string
        default: latest

---

engine:
  id: copilot
  version: ${{ inputs.engine-version }}
```

### Copilot Custom Configuration

Use `agent` to reference a custom agent file in `.github/agents/` (omit the `.agent.md` extension):

```yaml wrap
engine:
  id: copilot
  agent: technical-doc-writer  # .github/agents/technical-doc-writer.agent.md
```

See [Copilot Agent Files](/gh-aw/reference/copilot-custom-agents/) for details.

### Engine Environment Variables

All engines support custom environment variables through the `env` field:

```yaml wrap
engine:
  id: copilot
  env:
    DEBUG_MODE: "true"
    AWS_REGION: us-west-2
    CUSTOM_API_ENDPOINT: https://api.example.com
```

Environment variables can also be defined at workflow, job, step, and other scopes. See [Environment Variables](/gh-aw/reference/environment-variables/) for complete documentation on precedence and all 13 env scopes.

### Enterprise API Endpoint (`api-target`)

The `api-target` field specifies a custom API endpoint hostname for the agentic engine. Use this when running workflows against GitHub Enterprise Cloud (GHEC), GitHub Enterprise Server (GHES), or any custom AI endpoint.

For a complete setup and debugging walkthrough for GHE Cloud with data residency, see [Debugging GHE Cloud with Data Residency](/gh-aw/troubleshooting/debug-ghe/).

The value must be a hostname only — no protocol or path (e.g., `api.acme.ghe.com`, not `https://api.acme.ghe.com/v1`). The field works with any engine.

**Example** — specify a GHEC or GHES Copilot endpoint (use `api.enterprise.githubcopilot.com` for GHES):

```yaml wrap
engine:
  id: copilot
  api-target: api.acme.ghe.com
network:
  allowed:
    - defaults
    - acme.ghe.com
    - api.acme.ghe.com
```

The specified hostname must also be listed in `network.allowed` for the firewall to permit outbound requests.

#### Custom API Endpoints via Environment Variables

Set a base URL environment variable in `engine.env` to route API calls to an internal LLM router, Azure OpenAI deployment, or corporate proxy. AWF automatically extracts the hostname and applies it to the API proxy. The target domain must also appear in `network.allowed`.

| Engine | Environment variable |
|--------|---------------------|
| `codex` | `OPENAI_BASE_URL` |
| `claude` | `ANTHROPIC_BASE_URL` |
| `copilot` | `GITHUB_COPILOT_BASE_URL` |
| `gemini` | `GEMINI_API_BASE_URL` |

```yaml wrap
engine:
  id: codex
  model: gpt-4o
  env:
    OPENAI_BASE_URL: "https://llm-router.internal.example.com/v1"
    OPENAI_API_KEY: ${{ secrets.LLM_ROUTER_KEY }}

network:
  allowed:
    - github.com
    - llm-router.internal.example.com
```

`GITHUB_COPILOT_BASE_URL` is a fallback — if both it and `engine.api-target` are set, `engine.api-target` takes precedence.

When `OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL` is set, the configured model is passed through to the custom provider verbatim: gh-aw automatically emits `apiProxy.modelFallback.enabled: false` so the API proxy does not rewrite provider-specific model slugs (for example `anthropic/claude-sonnet-5` on OpenRouter) that are absent from the built-in model catalog, which otherwise causes HTTP 404 `model_not_found`. Set [`sandbox.agent.model-fallback`](/gh-aw/reference/sandbox/#model-fallback-sandboxagentmodel-fallback) explicitly to override this default.

```yaml wrap
engine:
  id: claude
  env:
    ANTHROPIC_BASE_URL: "https://openrouter.ai/api/v1"
    ANTHROPIC_API_KEY: ${{ secrets.OPENROUTER_KEY }}
model: anthropic/claude-sonnet-5

network:
  allowed:
    - defaults
    - openrouter.ai
```

If the custom provider also rejects requests because of proxy-side model steering, disable it with [`sandbox.agent.token-steering: false`](/gh-aw/reference/sandbox/#token-steering-sandboxagenttoken-steering).

### Copilot Bring Your Own Key (BYOK) Mode

The Copilot engine supports routing requests to an external LLM provider instead of GitHub's default routing. This is useful when you want to use a different model or provider (e.g., OpenAI, Anthropic, Azure OpenAI, or a local Ollama/vLLM instance) while still using the Copilot CLI tooling.

Set `COPILOT_PROVIDER_BASE_URL` in `engine.env` to activate BYOK mode. The credential variables `COPILOT_PROVIDER_BASE_URL`, `COPILOT_PROVIDER_API_KEY`, and `COPILOT_PROVIDER_BEARER_TOKEN` are explicitly allowed to carry `${{ secrets.* }}` references in `engine.env` under strict mode — they are not leaked to the agent container. Other `COPILOT_PROVIDER_*` variables hold non-sensitive configuration and can be set as plain strings. When `COPILOT_PROVIDER_BASE_URL` is a literal URL, gh-aw automatically adds its provider hostname to the AWF allow-list for both the main agent run and the threat-detection Copilot step, and the threat-detection step now derives its Copilot API target from that literal BYOK URL even when `engine.api-target` and `GITHUB_COPILOT_BASE_URL` are unset. When it is supplied via a secret or variable expression, add the provider hostname explicitly to `network.allowed` so the threat-detection step can reuse that concrete host safely.

| Variable | Required | Description |
|---|---|---|
| `COPILOT_PROVIDER_BASE_URL` | ✅ for BYOK | Base URL of the external provider (e.g. `https://api.openai.com/v1` or `https://RESOURCE.openai.azure.com/openai/v1` for Azure Foundry OpenAI) |
| `COPILOT_MODEL` | ✅ for BYOK | Model to use (e.g. `claude-sonnet-4`, `gpt-4o`); required by most providers |
| `COPILOT_PROVIDER_API_KEY` | Optional | API key for cloud providers (OpenAI, Anthropic, etc.); not needed for local providers |
| `COPILOT_PROVIDER_BEARER_TOKEN` | Optional | Bearer token alternative to `COPILOT_PROVIDER_API_KEY`; takes precedence when set |
| `COPILOT_PROVIDER_TYPE` | Optional | Provider format: `openai` (default), `azure`, or `anthropic` |
| `COPILOT_PROVIDER_WIRE_API` | Optional | Wire API variant: `completions` (default) or `responses` (for GPT-5 series) |
| `COPILOT_PROVIDER_MODEL_ID` | Optional | Model ID sent on the wire when it differs from `COPILOT_MODEL` (e.g. an Azure deployment name) |
| `COPILOT_PROVIDER_WIRE_MODEL` | Optional | Alternative to `COPILOT_PROVIDER_MODEL_ID` for overriding the wire model |
| `COPILOT_PROVIDER_MAX_PROMPT_TOKENS` | Optional | Override the maximum prompt token limit (otherwise resolved from model catalog) |
| `COPILOT_PROVIDER_MAX_OUTPUT_TOKENS` | Optional | Override the maximum output token limit |

**Example:**

```yaml wrap
engine:
  id: copilot
  env:
    COPILOT_PROVIDER_BASE_URL: ${{ secrets.PROVIDER_BASE_URL }}   # REQUIRED — activates BYOK
    COPILOT_MODEL: claude-sonnet-4                                # REQUIRED for most providers
    COPILOT_PROVIDER_API_KEY: ${{ secrets.PROVIDER_API_KEY }}     # OPTIONAL for local providers
    COPILOT_PROVIDER_TYPE: anthropic                              # OPTIONAL — default: openai

network:
  allowed:
    - defaults
    - your-provider-domain.example.com
```

> [!NOTE]
> Credentials are kept out of the agent container — only a dummy API key activating the AWF BYOK detection path is visible to the agent process; the real credential is isolated in the AWF API proxy sidecar. See [AWF sandbox architecture](/gh-aw/reference/sandbox/).

#### Azure Foundry OpenAI

Azure Foundry OpenAI supports the newer OpenAI v1 URL style. Set
`COPILOT_PROVIDER_BASE_URL` to the resource endpoint with the `/openai/v1`
path, then choose one authentication method:

```yaml wrap
engine:
  id: copilot
  model: o4-mini-aw
  env:
    COPILOT_PROVIDER_BASE_URL: https://RESOURCE.openai.azure.com/openai/v1
    COPILOT_PROVIDER_API_KEY: ${{ secrets.FOUNDRY_API_KEY }}
    COPILOT_PROVIDER_WIRE_API: responses

network:
  allowed:
    - defaults
    - RESOURCE.openai.azure.com
```

When the Azure deployment name differs from the Azure model ID, keep
`engine.model` on the model ID that Azure exposes from `/openai/v1/models` and
set `COPILOT_PROVIDER_MODEL_ID` to the deployment name that Azure expects on
the wire.

For Entra authentication, omit `COPILOT_PROVIDER_API_KEY` and configure
GitHub OIDC in `engine.auth`:

```yaml wrap
permissions:
  id-token: write

engine:
  id: copilot
  model: o4-mini-aw
  auth:
    type: github-oidc
  env:
    COPILOT_PROVIDER_BASE_URL: https://RESOURCE.openai.azure.com/openai/v1
    COPILOT_PROVIDER_WIRE_API: responses

network:
  allowed:
    - defaults
    - RESOURCE.openai.azure.com
```

See [How to use Azure OpenAI with Copilot BYOK](/gh-aw/reference/azure-openai-byok/)
for deployment-name mapping, `responses` API guidance for GPT-5 and o-series
models, and Azure-specific troubleshooting.

### Engine Command-Line Arguments

All engines support custom command-line arguments through the `args` field, injected before the prompt:

```yaml wrap
engine:
  id: copilot
  args: ["--add-dir", "/workspace", "--verbose"]
```

Arguments are added in order and placed before the `--prompt` flag. Consult the specific engine's CLI documentation for available flags.

### Custom Engine Command

Override the default engine executable using the `command` field. Useful for testing pre-release versions, custom builds, or non-standard installations. Engine installation steps are automatically skipped; when the firewall is enabled, gh-aw still installs its configured AWF binary.

```yaml wrap
engine:
  id: copilot
  command: /usr/local/bin/copilot-dev  # absolute path
  args: ["--verbose"]
```

### Custom Harness Script (`harness`)

The `harness` field lets you replace the built-in Node.js harness wrapper that the Copilot engine uses to launch the CLI. Use this when you need to customize startup behavior, inject pre/post hooks, or test an alternative harness implementation.

```yaml wrap
engine:
  id: copilot
  harness:
    use: custom_copilot_harness.cjs
```

The `use` value must be a bare filename — no directory separators, no `..`, and no shell metacharacters. It must end with `.js`, `.cjs`, or `.mjs`. When `harness.use` is set, AWF automatically ensures Node 24 is available in the runner environment.

> [!NOTE]
> `engine.harness` is currently only applied during Copilot engine execution. Setting it on other engines has no effect.

**Validation rules for `harness.use`:**

| Rule | Valid example | Invalid example |
|------|--------------|-----------------|
| Bare filename only | `my_harness.cjs` | `subdir/harness.cjs` |
| No path traversal | `harness.mjs` | `../harness.cjs` |
| Must start with `[A-Za-z0-9_]` | `harness.js` | `-harness.cjs` |
| Must end with `.js`, `.cjs`, or `.mjs` | `wrapper.cjs` | `harness.sh` |

### Harness Retry and Post-result Watchdog Policy

The built-in Copilot, Claude, and Codex harnesses default to **3 retries** after the initial run (4 total attempts), with exponential backoff starting at 5 s (capped at 60 s). Use sub-keys under `engine.harness` to widen the retry window without replacing the harness:

```yaml wrap
engine:
  id: copilot
  harness:
    max-retries: 6
    initial-delay-ms: 10000
    backoff-multiplier: 2
    max-delay-ms: 180000
    watchdog-timeout: 120
```

All five fields accept a literal integer or a GitHub Actions expression (e.g. `${{ vars.MY_RETRIES }}`).
For `watchdog-timeout`, the value is treated as seconds when it is a literal integer.
When an expression is used, it must already be in milliseconds (GitHub Actions expressions do not support arithmetic operators):

| Sub-key | Default | Description |
|---|---|---|
| `max-retries` | `3` | Maximum retry attempts after the initial run (0 = no retries) |
| `initial-delay-ms` | `5000` | Delay in ms before the first retry |
| `backoff-multiplier` | `2` | Multiplier applied to the delay after each retry |
| `max-delay-ms` | `60000` | Maximum delay cap in ms |
| `watchdog-timeout` | `120` | Post-result idle watchdog timeout in seconds before terminating a quiet process |

The post-result watchdog is dormant until the harness observes a terminal safe output. `noop` and ordinary task outputs such as comments, labels, pushes, and pull request creation are terminal; diagnostics such as `missing_tool`, `missing_data`, and `report_incomplete` are not. Once armed, any stdout or stderr activity resets the inactivity clock. A quiet child process can still be terminated while it is doing useful work, and the harness may treat that termination as successful when a terminal safe output already exists.

You can also set the underlying `GH_AW_HARNESS_*` env vars directly via `engine.env` when you need expression-level control, including `GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS` for the post-result watchdog and `GH_AW_HARNESS_STARTUP_RETRIES` for fresh startup retries. Explicit `engine.env` values take precedence over `engine.harness` sub-key values. See [Harness Settings and Runtime Tuning Variables](/gh-aw/reference/environment-variables/#harness-settings-and-runtime-tuning-variables) for supported env vars, units, and clamping behavior.

Threat detection runs default `max-retries` to **0** instead of inheriting the harness default of 3, since detection is a bounded scan of already-completed agent output rather than the primary task — a failed attempt should not silently retry the whole detection run and burn extra time and model spend. Set `engine.harness.max-retries` (or `safe-outputs.threat-detection.engine.harness.max-retries`) explicitly to opt back into retries for detection.

### Copilot SDK Support

Enable `engine.copilot-sdk: true` to run Copilot in SDK mode.
In this mode, the harness starts a local sidecar and runs the
SDK driver process instead of the default CLI-only flow.

Use top-level `max-tool-denials` to stop SDK inference when
tool requests are repeatedly denied. The default is `5`.
This field is only supported when `engine.id: copilot` and
`engine.copilot-sdk: true`.

Use `engine.driver` to replace the built-in
`copilot_sdk_driver.cjs` implementation. On the Copilot engine,
setting `engine.driver` also enables `engine.copilot-sdk: true`:

```yaml wrap
engine:
  id: copilot
  driver: .github/drivers/custom-copilot-driver.js
max-tool-denials: 8
```

`engine.driver` must be a **relative path from the workspace root**
(no absolute paths, `..`, backslashes, or shell metacharacters). It supports:

- script filenames ending with `.js`, `.cjs`, `.mjs`,
  `.py`, `.ts`, `.mts`, or `.rb`
- bare command names without an extension (resolved from
  `PATH`)
- inline source blocks using exactly one of `node:`,
  `python:`, `go:`, or `java:`

Inline driver sources are materialized into runtime files under
`${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/` before execution.
This lets the agent emit a complete Copilot SDK driver inline in
workflow frontmatter:

```yaml wrap
engine:
  id: copilot
  driver:
    python: |
      import sys
      print("hello from inline driver", file=sys.stderr)
```

See [Copilot SDK Driver Specification](/gh-aw/specs/copilot-sdk-driver-specification/)
for the full driver contract.

#### SDK driver environment variables

The specification defines the driver environment contract.
In SDK mode, gh-aw injects required runtime values:

- `GH_AW_PROMPT`
- `COPILOT_SDK_URI`
- `COPILOT_CONNECTION_TOKEN`

`COPILOT_MODEL` is required and must be set to the model to use
(e.g. `gpt-4o`, `claude-sonnet-4`). Drivers MUST fail fast when
it is not set.

For runtime controls, the driver should consume:

- `COPILOT_SDK_SEND_TIMEOUT_MS`
- `COPILOT_SDK_LOG_LEVEL`

In gh-aw, `COPILOT_SDK_SEND_TIMEOUT_MS` is usually injected
automatically from workflow `timeout-minutes` (via
`GH_AW_TIMEOUT_MINUTES`) with safety headroom. Override it in
`engine.env` only when you need a custom SDK send timeout.
`COPILOT_SDK_LOG_LEVEL` is a host-provided driver control and
should be honored when gh-aw passes it to the driver process.

Do not set `COPILOT_CONNECTION_TOKEN` manually. The harness
generates it per run and passes the same token to both the
sidecar and driver process.

```yaml wrap
engine:
  id: copilot
  driver: .github/drivers/my_driver.ts
  model: gpt-5
  env:
    COPILOT_SDK_SEND_TIMEOUT_MS: "900000"
    COPILOT_SDK_LOG_LEVEL: info
```

### Bare Mode (`bare`)

Set `engine.bare: true` with Copilot or Claude to disable automatic loading of context and custom instructions by the engine. Use this when the workflow prompt is fully self-contained and you want to prevent the engine from reading memory files, `AGENTS.md`, or built-in system prompts that would otherwise be loaded automatically. Pi also accepts `engine.bare: true`, but the setting is a no-op because Pi already runs in bare mode. Codex and Gemini do not support this field.

```yaml wrap
engine:
  id: claude
  bare: true
```

The underlying mechanism is engine-specific:

| Engine | Effect |
|--------|--------|
| Copilot | Passes `--no-custom-instructions` — suppresses `.github/AGENTS.md` and user-level custom instructions |
| Claude | Passes `--bare` — suppresses CLAUDE.md memory files |
| Pi | No effect — Pi already runs in bare mode |

Defaults to `false`.

### Pi Extensions (`extensions`)

The Pi engine supports loading additional plugins via `engine.extensions`. Each entry is an npm package name installed with `pi install <extension>` before the agent runs. Only the Pi engine reads this field; other engines ignore it. Pi extensions are distinct from the top-level Agent Plugins `plugins` field.

```yaml wrap
engine:
  id: pi
  extensions:
    - "@pi/web-search"
    - "@pi/file-browser"
```

Each listed extension produces one additional install step in the compiled workflow. If `engine.command` is set, the same executable is used to install the extensions.

## Timeout Configuration

Repositories with long build or test cycles require careful timeout tuning at multiple levels. This section documents the timeout knobs available for each engine.

### Job-Level Timeout (`timeout-minutes`)

`timeout-minutes` sets the maximum wall-clock time for the entire agent job. This is the primary knob for repositories with long build times. The default is 20 minutes.

```yaml wrap
timeout-minutes: 60   # allow up to 60 minutes for the agent job
```

See [Long Build Times](/gh-aw/reference/sandbox/#long-build-times) in the Sandbox reference for recommended values and concrete examples, including a 30-minute C++ workflow.

### Per-Tool-Call Timeout (`tools.timeout`)

`tools.timeout` limits how long any single tool invocation may run, in seconds. Useful when individual `bash` commands (builds, test suites) take longer than an engine's default:

```yaml wrap
tools:
  timeout: 300   # 5 minutes per tool call
```

Defaults: Claude `60s`, Codex `120s`. Other engines (Copilot, Gemini) are engine-managed and not enforced by gh-aw. See [Tool Timeout Configuration](/gh-aw/reference/tools/#tool-timeout-configuration) for full documentation including `tools.startup-timeout`.

### Per-Engine Timeout Controls

| Knob | Copilot | Claude | Codex/Gemini | Purpose |
|---|:---:|:---:|:---:|---|
| `timeout-minutes` | ✅ | ✅ | ✅ | Job-level wall clock |
| `tools.timeout` | ✅ | ✅ | ✅ | Per tool-call limit (seconds) |
| `tools.startup-timeout` | ✅ | ✅ | ✅ | MCP server startup limit |
| `max-turns` | ✅ | ✅ | ✅ | Top-level AWF invocation cap (enforced by the proxy) |
| `engine.max-turns` (deprecated) | ❌ | ✅ | ❌ | Claude-only nested iteration budget |
| `max-continuations` | ✅ | ❌ | ❌ | Autopilot run budget |

The top-level `max-turns` field applies to every engine because the proxy enforces the AWF invocation cap. In addition, Copilot uses `max-continuations` for autopilot runs, and Claude supports the deprecated nested `engine.max-turns` to cap its own iterations. Beyond `max-turns`, Codex and Gemini rely solely on `timeout-minutes` and `tools.timeout`.

```yaml wrap
# Claude — combine iteration cap with per-tool timeout
engine:
  id: claude
max-turns: 20
tools:
  timeout: 600
timeout-minutes: 60
```

When `max-turns` is set in frontmatter, gh-aw passes it to Claude automatically — no need to also set the `CLAUDE_CODE_MAX_TURNS` env var.

## Claude Tool Enforcement Security Model

Claude Code accepts a `--permission-mode` flag that determines whether the declared `tools:` allowlist is enforced. Set `engine.permission-mode` to one of `auto`, `acceptEdits`, `plan`, or `bypassPermissions`:

```yaml wrap
engine:
  id: claude
  permission-mode: auto
```

`engine.permission-mode` takes precedence over any `--permission-mode` flag supplied through `engine.args`. When unset, the default is `acceptEdits` (or `auto` when `tools.edit: false`). gh-aw **does not** derive `bypassPermissions` implicitly from unrestricted bash — set it explicitly.

| `engine.permission-mode` | Effective mode | `--allowed-tools` enforced? | Gateway `allowed:` enforced? |
|---|---|:---:|:---:|
| unset (default) | `acceptEdits` | ✅ Yes | ✅ Yes |
| unset, with `tools.edit: false` | `auto` | ✅ Yes | ✅ Yes |
| `auto` | `auto` | ✅ Yes | ✅ Yes |
| `acceptEdits` | `acceptEdits` | ✅ Yes | ✅ Yes |
| `plan` | `plan` | ✅ Yes | ✅ Yes |
| `bypassPermissions` | `bypassPermissions` | ❌ No | ✅ Yes |

### Gateway-side enforcement

The MCP gateway's `allowed:` filter is the sole effective tool boundary in `bypassPermissions` mode (and a second layer of enforcement otherwise). Always specify `allowed:` on each `mcp-servers:` entry to restrict which MCP tools are reachable:

```yaml wrap
mcp-servers:
  notion:
    container: "mcp/notion"
    allowed: ["search_pages", "get_page"]   # enforced at gateway level
```

> [!WARNING]
> Do not rely on `tools:` or `mcp-servers: allowed:` for security guarantees in `bypassPermissions` mode. The agent can already run arbitrary shell commands when unrestricted bash is granted, so `--allowed-tools` provides no meaningful additional boundary.

## Learn More

- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete configuration reference
- [Authentication](/gh-aw/reference/auth/) - Engine credentials and identity mechanisms
- [Tools](/gh-aw/reference/tools/) - Available tools and MCP servers
- [Security Guide](/gh-aw/introduction/architecture/) - Security considerations for AI engines
- [Gallery](/gh-aw/gallery/) - Agentic workflows organized by task
- [MCPs](/gh-aw/guides/mcps/) - Model Context Protocol setup and configuration
