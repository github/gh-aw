---
title: AI Engines
description: Select an AI engine, compare supported features, and configure shared engine options.
sidebar:
  order: 600
---

An engine runs a workflow's Markdown instructions with its configured tools, sandbox, and safe outputs. This page covers selection and shared configuration. Use the individual references for engine-specific settings.

<span id="which-engine-should-i-choose"></span>

## Built-in AI engines

| Engine | Selection | Get started | Technical reference |
| --- | --- | --- | --- |
| GitHub Copilot (default) | `engine: copilot` or omit `engine` | [Copilot setup](/gh-aw/engines/copilot/) | [Copilot reference](/gh-aw/reference/engines/copilot/) |
| Claude Code | `engine: claude` | [Claude setup](/gh-aw/engines/claude/) | [Claude reference](/gh-aw/reference/engines/claude/) |
| OpenAI Codex | `engine: codex` | [Codex setup](/gh-aw/engines/codex/) | [Codex reference](/gh-aw/reference/engines/codex/) |
| Gemini CLI (deprecated) | `engine: gemini` | [Gemini setup](/gh-aw/engines/gemini/) | [Gemini reference](/gh-aw/reference/engines/gemini/) |
| Pi | `engine: pi` | [Pi setup](/gh-aw/engines/pi/) | [Pi reference](/gh-aw/reference/engines/pi/) |
| Agy (experimental) | `engine: agy` | [Agy setup](/gh-aw/engines/agy/) | [Agy reference](/gh-aw/reference/engines/agy/) |

For imported engine definitions and third-party samples, see [Custom AI engines](/gh-aw/reference/third-party-agent/). Switching engines may require different credentials, models, tools, and network rules.

## Data handling before setup

The engine, inference provider, and enabled tools determine where repository context is processed. Review provider terms, retention, accessible repository content, outbound destinations, and artifact access before enabling a workflow. Credentials and identity mechanisms are documented in [Authentication](/gh-aw/reference/auth/).

## Engine Feature Comparison

| Feature | Copilot | Claude | Codex | Gemini | Pi | Agy |
| --- | :---: | :---: | :---: | :---: | :---: | :---: |
| Top-level `max-turns` | Yes | Yes | Yes | Yes | Yes | No |
| `max-continuations` | Yes | No | No | No | No | No |
| `tools.web-fetch` | SDK only | Yes | Yes | Yes | Yes | Cannot disable |
| Native `tools.web-search` | No | Yes | Yes | No | No | Cannot disable |
| Native `engine.agent` selection | Yes | No | No | No | No | No |
| `engine.api-target` | Yes | Yes | Yes | Yes | Yes | No |
| `engine.bare` | Yes | Yes | No | No | Yes | No |
| Custom `engine.harness` | Yes | Yes | Yes | No | No | No |
| Per-command bash allowlist | Yes | Yes | No | Yes | Yes | No |
| Native MCP | Yes | Yes | Yes | Yes | Yes | Production-conformance gated |
| Agent Plugins (`plugins`) | Yes | Yes | Yes | No | No | No |

Codex can disable shell execution but cannot enforce a command allowlist. Agy rejects unsupported tool restrictions rather than ignoring them. See individual references for provider-specific web access, bare-mode behavior, and dynamic workflows.

`max-turns` defaults to `500` (legacy alias `max-runs`); `max-ai-credits` defaults to `1000`. These are top-level fields, not `engine` properties. Agy supports credit/cache-miss limits but not `max-turns`. `engine.max-turns` is a deprecated Claude-only iteration limit. [Agent Plugins](/gh-aw/reference/frontmatter/#agent-plugins-plugins) remain experimental.

## Extended Coding Agent Configuration

String form selects a built-in engine. Object form adds settings:

```yaml
engine:
  id: copilot
  model: opus
  env:
    DEBUG_MODE: "true"
```

| Field | Contract |
| --- | --- |
| `id` | Built-in ID, or the ID of an [imported definition](/gh-aw/reference/third-party-agent/). |
| `model` | Exact model ID, alias, or provider-prefixed selection; may also be top-level `model`. |
| `version` | Override the compiler-pinned CLI version. |
| `env` | Runtime environment values; [scope and precedence](/gh-aw/reference/environment-variables/) still apply. |
| `args` | Ordered arguments passed to the native CLI before its prompt argument. |
| `command` | Replacement executable; skips the engine installer, not AWF installation. |
| `api-target` | Endpoint hostname, where supported; see below. |
| `harness` | Replacement wrapper or retry settings for Copilot, Claude, and Codex. |

Engine-specific options such as `agent`, `copilot-sdk`, `driver`, `dynamic-workflows`, `permission-mode`, `config`, and `extensions` are documented in each engine reference. Agy rejects most overrides.

### Pinning a Specific Engine Version

Claude Code `2.1.313` is blocked for security reasons, including in threat-detection engine overrides. Remove that override to use the compiler-pinned version, or select an unaffected release, then recompile existing workflows.

The compiler pins a CLI release. Override it with `engine.version`, then recompile:

```yaml
engine:
  id: copilot
  version: "1.0.101"
```

<span id="parameterized-versions"></span>

Versions can use GitHub Actions expressions; the runtime passes them through environment variables rather than shell interpolation. Copilot expression values must resolve to trusted cached releases or built-in manifest pins. Other Copilot releases require a literal version and [checksum pin](/gh-aw/reference/engines/copilot/#copilot-release-checksum-pins). Agy accepts only its verified releases.

Claude version selectors are resolved before installation and rejected if they select the blocked release, before any package lifecycle scripts run.

### Engine Environment Variables

Use `engine.env` for engine-only runtime values. Use top-level `env` for all workflow steps; see [environment variable scope and precedence](/gh-aw/reference/environment-variables/).

### Enterprise API Endpoint (`api-target`)

`api-target` is a hostname, without protocol or path, and must be permitted by `network.allowed`:

```yaml
engine:
  id: copilot
  api-target: api.acme.ghe.com
network:
  allowed: [defaults, acme.ghe.com, api.acme.ghe.com]
```

For GHES Copilot use `api.enterprise.githubcopilot.com`. See [GHE Cloud troubleshooting](/gh-aw/troubleshooting/debug-ghe/) for data-residency setup. Agy does not support `api-target`.

#### Custom API Endpoints via Environment Variables

| Engine | Base URL variable |
| --- | --- |
| Copilot | `GITHUB_COPILOT_BASE_URL` |
| Claude | `ANTHROPIC_BASE_URL` |
| Codex | `OPENAI_BASE_URL` |
| Gemini | `GEMINI_API_BASE_URL` |

Set the URL through `engine.env` and allow its domain in `network.allowed`. AWF derives the proxy target from it. Copilot's `api-target` takes precedence over `GITHUB_COPILOT_BASE_URL`. Pi's additional provider routing is in its [reference](/gh-aw/reference/engines/pi/#additional-providers).

For `OPENAI_BASE_URL` and `ANTHROPIC_BASE_URL`, gh-aw disables automatic model fallback by default so provider-specific model IDs pass through unchanged. Explicit [`sandbox.agent.model-fallback`](/gh-aw/reference/sandbox/#model-fallback-sandboxagentmodel-fallback) overrides that default. If proxy-side model steering is incompatible with the endpoint, use [`sandbox.agent.token-steering: false`](/gh-aw/reference/sandbox/#token-steering-sandboxagenttoken-steering).

### Engine Command-Line Arguments

`engine.args` is an ordered array of native CLI arguments, inserted before the prompt argument. Consult the selected CLI's documentation for available flags; Agy rejects this setting.

### Custom Engine Command

`engine.command` replaces the executable and skips the engine installer, not AWF installation. Provision it in setup steps and ensure it satisfies the selected engine's version/configuration contract. Agy checks the verified executable version; Copilot checksum pins cannot be combined with `command`.

### Custom Harness Script (`harness`)

Copilot, Claude, and Codex accept a replacement harness provisioned in `${RUNNER_TEMP}/gh-aw/actions/`, for example through `pre-agent-steps`:

```yaml
engine:
  id: copilot
  harness:
    use: custom_copilot_harness.cjs
```

`use` must start with a letter, digit, or underscore, end in `.js`, `.cjs`, or `.mjs`, and contain no directory separators, `..`, or shell metacharacters. Selecting it does not copy a repository file. Node 24 is provisioned automatically. A replacement owns its retry, timeout, prompt-delivery, and tool-policy behavior.

### Harness Retry and Post-result Watchdog Policy

The built-in Copilot, Claude, and Codex harnesses accept:

| `engine.harness` field | Default | Meaning |
| --- | --- | --- |
| `max-retries` | `3` | Retries after the initial attempt; `0` disables. |
| `initial-delay-ms` | `5000` | First retry delay. |
| `backoff-multiplier` | `2` | Exponential delay multiplier. |
| `max-delay-ms` | `60000` | Delay cap. |
| `watchdog-timeout` | `120` | Post-result idle timeout; literal integers are seconds. |

All accept integers or Actions expressions. A watchdog expression must already be in milliseconds. Explicit `engine.env.GH_AW_HARNESS_*` values override these fields; see [runtime tuning variables](/gh-aw/reference/environment-variables/#harness-settings-and-runtime-tuning-variables).

The watchdog arms after a terminal safe output, not diagnostic reports such as `report_incomplete`. Any stdout/stderr activity resets it. It can terminate a quiet process still doing useful work and treat that termination as success if a terminal output exists. Claude counts only terminal outputs from the current attempt; its [resume rules](/gh-aw/reference/engines/claude/#harness-recovery) also apply.

Threat detection defaults to zero retries; configure its engine explicitly to change that. Model fallback below retries individual inference requests, not whole agent attempts.

### Model aliases

> [!NOTE]
> The Copilot model catalog offered to a workflow can differ by organization and lag the models available to that organization's Copilot seats. Availability depends on the agentic-workflows integration, not just organization policy; a model listed by this repository's daily inventory is not necessarily offered to another workflow. An exact Copilot ID missing from a complete live catalog means that model is **not offered to this workflow**. Prefer aliases such as `opus` and `sonnet` over exact Copilot IDs so resolution selects an available model. Run `gh aw audit <run-url> --models` for a detailed report of that run's model information, including its catalog, discovery status, supported endpoints, and recorded routing and usage. Missing or incomplete discovery does not confirm model absence.

Copilot, Claude, Codex, Pi, and Agy resolve aliases against the configured provider's live AWF `/reflect` catalog before execution:

- Built-in aliases such as `opus`/`small`, workflow-defined `models:`, and their globs select the newest live match.
- A `provider/` prefix restricts the lookup; otherwise the engine's configured provider applies.
- Exact live IDs take precedence over an alias of the same name. Unrecognized exact IDs are not rewritten by alias resolution. Pi checks exact IDs before launch; Copilot, Claude, and Codex fail on the first unavailable-model request.
- Parameters such as `?effort=` are retained. `auto` keeps its engine/provider-specific meaning.
- Missing alias matches fail with `model_unavailable`; incomplete discovery fails alias resolution with `catalog_unavailable`.

Gemini and declarative engines pass model names through unchanged and warn about gh-aw aliases. AWF can independently rewrite unrecognized models on non-Copilot providers; disable [`sandbox.agent.model-fallback`](/gh-aw/reference/sandbox/#model-fallback-sandboxagentmodel-fallback) when exact routing is required.

Resolution produces `model.resolution` evidence in `usage/aw_session.jsonl`. See [Model Aliases](/gh-aw/reference/model-tables/) for the catalog and [Model Routing](/gh-aw/reference/model-routing/) for experimental per-run model/effort selection on Copilot-backed Copilot, Claude, Codex, and Pi.

### Ordered model fallback (`fallback-models`)

```yaml
engine:
  id: copilot
  model: grok-4.7
  fallback-models: [gpt-5.6-luna, mai-code-1.1-flash]
```

All built-in engines accept ordered, concrete same-provider alternatives with AWF v0.28.31+ and a compatible API-proxy image. AWF retries the failed request without restarting the agent, preserving policy and budget checks. An explicit chain disables automatic middle-power substitution.

Fallback covers server errors, request timeouts, unsupported models, and model-availability HTTP 400 errors. Authentication failures, HTTP 429, policy/guardrail failures, cancellation, and job timeouts do not switch models. Exhausted chains fail the run.

Unqualified alternatives inherit the primary provider. Prefixes must match it; credentials follow that provider's normal configuration. Cross-provider chains, fallback aliases, missing compatible AWF, and combinations with `engine.model-routing` fail compilation. Custom harnesses/drivers can use AWF fallback without implementing switching.

`token-usage.jsonl` records the serving model and `model_fallback` evidence for post-run attribution. This does not change harness retries or prove that every configured alternative was exercised.

### Bare Mode (`bare`)

`engine.bare: true` disables ambient discovery in Copilot, Claude, and Pi; default false. Copilot suppresses custom instructions. Claude skips ambient discovery but keeps explicitly installed workflow skills/plugins, with a minimal native tool set. Pi retains managed infrastructure and explicitly loaded workflow skills. See individual references for delegation and dynamic-workflow restrictions.

<span id="job-level-timeout-timeout-minutes"></span>
<span id="per-tool-call-timeout-toolstimeout"></span>
<span id="per-engine-timeout-controls"></span>

## Timeout Configuration

| Setting | Default | Scope |
| --- | --- | --- |
| `timeout-minutes` | `20` | Entire agent job. |
| `tools.timeout` | `60` seconds for Claude/Codex | Supported tool operations; Codex native MCP calls, not shell commands. |
| `tools.startup-timeout` | `120` seconds for Codex | MCP server startup; explicit Codex server config takes precedence. |
| `max-turns` | `500` | AWF invocation cap, except unsupported Agy. |
| `max-continuations` | Unset | Copilot autopilot continuation budget. |

Claude receives top-level `max-turns` automatically; no duplicate `CLAUDE_CODE_MAX_TURNS` is needed. Copilot and Gemini retain native tool-limit behavior. See [tool timeouts](/gh-aw/reference/tools/#tool-timeout-configuration) and [long builds](/gh-aw/reference/sandbox/#long-build-times).

## Configuration conformance workflows

Repository `engine-conformance-<id>.md` workflows are manual configuration smoke tests. They check file/shell/MCP probes and typed results using recorded receipts, not an agent's success claim. Safe outputs are staged; missing credentials fail activation. Reports are retained for two days.

These tests do not establish security, model quality, SDK/driver coverage, or provider compliance with a requested model. Agy's [production gate](/gh-aw/reference/engines/agy/#troubleshooting-and-conformance) is separate from its native authentication probe.

<span id="experimental-agy-authentication-gate"></span>
<span id="learn-more"></span>

## Engine-specific sections

The following anchors retain links to sections moved from this page.

### Copilot Custom Configuration

See [Copilot custom agents](/gh-aw/reference/engines/copilot/#copilot-custom-configuration).

### Copilot release checksum pins

See [Copilot release integrity](/gh-aw/reference/engines/copilot/#copilot-release-checksum-pins).

<span id="azure-foundry-openai"></span>

### Copilot Bring Your Own Key (BYOK) Mode

See [Copilot BYOK](/gh-aw/reference/engines/copilot/#copilot-bring-your-own-key-byok-mode).

### Copilot SDK Support

<span id="sdk-driver-environment-variables"></span>

See [Copilot SDK configuration](/gh-aw/reference/engines/copilot/#copilot-sdk-support).

### Pi Extensions (`extensions`)

See [Pi packages](/gh-aw/reference/engines/pi/#pi-extensions-extensions).

### Claude Tool Enforcement Security Model

<span id="gateway-side-enforcement"></span>

See [Claude permissions and gateway enforcement](/gh-aw/reference/engines/claude/#claude-tool-enforcement-security-model).

### Shared imported engines

See [Custom AI engines](/gh-aw/reference/third-party-agent/#shared-imported-engines).

### Unsupported engine samples

See [third-party samples](/gh-aw/reference/third-party-agent/#unsupported-engine-samples).
