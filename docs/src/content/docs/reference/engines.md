---
title: AI Engines
description: Select an AI engine, compare supported features, and configure shared engine options.
sidebar:
  order: 600
---

An engine runs a workflow's Markdown instructions with its configured tools, sandbox, and safe outputs. This page covers selection and shared settings; engine-specific settings are in the individual references.

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

The engine, inference provider, and enabled tools determine where repository context is processed. Before enabling a workflow, review provider terms, retention, accessible content, outbound destinations, and artifact access. Credentials are covered in [Authentication](/gh-aw/reference/auth/).

## Engine Feature Comparison

| Feature | Copilot | Claude | Codex | Gemini | Pi | Agy |
| --- | :---: | :---: | :---: | :---: | :---: | :---: |
| Top-level `max-turns` | Yes | Yes | Yes | Yes | Yes | No |
| `max-continuations` | Yes | No | No | No | No | No |
| `tools.web-fetch` | SDK only | Anthropic only | Yes | Yes | Yes | Cannot disable |
| Native `tools.web-search` | No | Anthropic only | Yes | No | No | Cannot disable |
| Native `engine.agent` selection | Yes | No | No | No | No | No |
| `engine.api-target` | Yes | Yes | Yes | Yes | Yes | No |
| `engine.bare` | Yes | Yes | No | No | Yes | No |
| Custom `engine.harness` | Yes | Yes | Yes | No | No | No |
| Per-command bash allowlist | Yes | Yes | No | Yes | Yes | No |
| Native MCP | Yes | Yes | Yes | Yes | Yes | Production-conformance gated |
| Agent Plugins (`plugins`) | Yes | Yes | Yes | No | No | No |
| Artifact-backed `engine.durable` | Session (CLI/SDK) | Session | Session | Session | Session (CLI/SDK/RPC) | Session |

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
| `cooldown` | Minimum release age in nonnegative integer days; defaults to `3`. Explicit `0` is rejected in strict mode. |
| `env` | Engine-only runtime values; use top-level `env` for all steps. [Scope and precedence](/gh-aw/reference/environment-variables/) apply. |
| `args` | Ordered native CLI arguments placed before the prompt. Agy rejects it. |
| `command` | Replacement executable that skips the engine installer, not AWF installation. Provision it in setup steps; Agy checks the verified version, and Copilot checksum pins cannot be combined with it. |
| `api-target` | Endpoint hostname, where supported; see below. |
| `harness` | Replacement wrapper or retry settings for Copilot, Claude, and Codex. |
| `durable` | Artifact-backed session continuation with optional `retention-days` (1–90, default `7`). |

Engine-specific options such as `agent`, `copilot-sdk`, `driver`, `dynamic-workflows`, `permission-mode`, `config`, and `extensions` are documented in each engine reference. Agy rejects most overrides.

### Durable runs

> [!WARNING]
> Durable mode is experimental for all engines, including Pi. Enabling
> `engine.durable` emits a compile-time warning. Configuration and behavior may
> change without deprecation.

All built-in engines support artifact-backed continuation:

```aw
engine:
  id: copilot
  durable:
    retention-days: 7
```

The compiler restores an explicit parent from `aw_context`, or otherwise the last successful run of the same workflow on the same branch. Reruns prefer a checkpoint from an earlier attempt of the same run. Retention follows repository and organization policy. Durable mode needs no memory tool and never dispatches another run.

| Engine | Persisted state | Continuation |
| --- | --- | --- |
| Copilot CLI and SDK | Native session-state files and exact session ID | CLI `--session-id`; SDK `resumeSession` |
| Claude | Project transcripts, task state, and exact session ID | CLI `--resume <id>` |
| Codex | Native and archived rollout sessions and exact thread ID | CLI `exec resume <id>` |
| Gemini | Native temporary session storage and project registry | Prompt points to restored session files |
| Agy | Native conversation databases | Prompt points to restored session files |
| Pi CLI, SDK and RPC | Native JSONL conversation session | CLI/RPC `--session`; SDK `SessionManager.open` |

Copilot, Claude, Codex, and Pi resume the exact recorded session with the current prompt, credentials, models, and tool policy. Gemini and Agy receive restoration metadata instead of a forced continuation argument. Restored history counts as metadata unless facts are verified against trusted sources, and restored text is never treated as an instruction.

Durable mode reuses potentially attacker-influenced history across runs. The untrusted-data guidance is advisory, not enforced isolation, and does not eliminate cross-run prompt injection.

Only session files are restored, not workspace edits, credentials, pending task phases, or exactly-once tool execution. Restored Copilot events are excluded from current-run capture and model verification. With no prior run or checkpoint, the first run starts a new session. Missing explicit-parent artifacts, corrupt sessions, and authorization failures remain errors; they never silently start an unrelated conversation.

The unified session records `durable.restore` (`restored`, `fresh`, or `failed`) and, for Copilot, Claude, Codex, and Pi, `durable.session` (`resumed` or `fresh`). These records hold metadata only, and restoration alone does not prove native continuation for Gemini, Agy, or imported engines. Recording is best effort; missing records mean evidence is unavailable, not a fresh start or a failed restore. `gh aw audit RUN --artifacts info,usage --json` and `gh aw health WORKFLOW --json --logs-dir DIRECTORY` report this evidence; see the [audit reference](/gh-aw/reference/audit/). Spans and durable session logs carry the checkpoint parent as `gh-aw.durable.parent_run_id`; fresh runs omit it.

Built-in durable mode rejects replacement commands, custom drivers, and harnesses. Pi's SDK and RPC drivers remain supported. Ordinary CLI arguments work, but session selectors and persistence-disabling flags cannot override compiler-managed continuation. Copilot cannot override `COPILOT_HOME`. Pi has [additional restrictions](/gh-aw/reference/engines/pi/#durable-runs), and [imported engines](/gh-aw/reference/third-party-agent/#durable-execution) must declare their own durable files. Threat detection uses a separate fresh session.

Checkpoints are published after secret redaction and integrity validation. Cancellation or hard runner loss can prevent publication. Artifacts contain restorable session content that may expose sensitive history to anyone with artifact access, and redaction is heuristic. Enable durable mode only where that suits the repository's artifact access and retention policies.

### Pinning a Specific Engine Version

The compiler pins a CLI release. Override it with `engine.version`, then recompile:

```yaml
engine:
  id: copilot
  version: "1.0.101"
```

<span id="parameterized-versions"></span>

Versions may use Actions expressions, passed through environment variables rather than shell interpolation. Copilot expressions must resolve to trusted cached releases or built-in manifest pins; other Copilot releases need a literal version and a [checksum pin](/gh-aw/reference/engines/copilot/#copilot-release-checksum-pins). Agy accepts only verified releases.

### Engine release-age cooldown

Compiler-managed engine installs require releases to be at least **3 days** old by default. Configure a different age with `engine.cooldown`:

```aw
engine:
  id: copilot
  cooldown: 7
```

The value must be a nonnegative integer number of days; booleans, strings, durations, and expressions are rejected. `cooldown: 0` disables the check only with `strict: false`. Omitting the field keeps the default.

Claude, Codex, Pi, Gemini, and behavior-defined npm installs pass the age to npm via `NPM_CONFIG_MIN_RELEASE_AGE`, including microVM staging. npm enforces it with its built-in `min-release-age` setting, so runtime overrides need npm **11.10.0 or later**. Copilot's installer checks GitHub release publication metadata before activating a cached CLI or downloading an archive; missing or invalid metadata fails the install. The cooldown does not replace checksum verification or pins.

The policy does not cover shell installers, `engine.command` executables, pre-agent steps, or extensions. Runtime dependency cooldowns use `runtimes.<id>.cooldown` (a boolean). Imported definitions that set `behaviors.installation.cooldown` are ignored; set `engine.cooldown` in the workflow instead.

### Enterprise API Endpoint (`api-target`)

`api-target` is a hostname, without protocol or path, and must be permitted by `network.allowed`:

```yaml
engine:
  id: copilot
  api-target: api.acme.ghe.com
network:
  allowed: [defaults, acme.ghe.com, api.acme.ghe.com]
```

For GHES Copilot, `api.enterprise.githubcopilot.com` is an endpoint-routing example only if the endpoint is available for your account and deployment; it is not a GHES support promise. Confirm licensing, policy, and firewall prerequisites in [Copilot Engine Prerequisites on GHES](/gh-aw/troubleshooting/common-issues/#copilot-engine-prerequisites-on-ghes). See [GHE Cloud troubleshooting](/gh-aw/troubleshooting/debug-ghe/) for data-residency setup. Agy does not support `api-target`.

#### Custom API Endpoints via Environment Variables

| Engine | Base URL variable |
| --- | --- |
| Copilot | `GITHUB_COPILOT_BASE_URL` |
| Claude | `ANTHROPIC_BASE_URL` |
| Codex | `OPENAI_BASE_URL` |
| Gemini | `GEMINI_API_BASE_URL` |

Set the URL through `engine.env` and allow its domain in `network.allowed`. AWF derives the proxy target from it. Copilot's `api-target` takes precedence over `GITHUB_COPILOT_BASE_URL`. Pi's additional provider routing is in its [reference](/gh-aw/reference/engines/pi/#additional-providers).

For `OPENAI_BASE_URL` and `ANTHROPIC_BASE_URL`, model fallback is off by default so provider model IDs pass through; override with [`sandbox.agent.model-fallback`](/gh-aw/reference/sandbox/#model-fallback-sandboxagentmodel-fallback). If proxy steering conflicts with the endpoint, set [`sandbox.agent.token-steering: false`](/gh-aw/reference/sandbox/#token-steering-sandboxagenttoken-steering).

### Custom Harness Script (`harness`)

Copilot, Claude, and Codex accept a replacement harness provisioned in `${RUNNER_TEMP}/gh-aw/actions/`, for example through `pre-agent-steps`:

```yaml
engine:
  id: copilot
  harness:
    use: custom_copilot_harness.cjs
```

`use` must start with a letter, digit, or underscore, end in `.js`, `.cjs`, or `.mjs`, and contain no directory separators, `..`, or shell metacharacters. Node 24 is provisioned automatically. A replacement owns its retry, timeout, prompt-delivery, and tool-policy behavior.

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

For GitHub-provided models, the Copilot, Claude, and Codex harnesses wait at least the duration of a valid HTTP 429 `Retry-After` header (seconds or HTTP-date) before a permitted retry. Claude and Codex use headers from structured terminal errors on stdout; other output does not provide retry timing. If the wait exceeds `max-delay-ms` or the remaining step timeout budget (including the harness's shutdown buffer), they stop immediately with failure class `rate_limit_wait_exceeded` instead of retrying early. For example, a 245-second wait requires `max-delay-ms: 245000` and sufficient time remaining. Other providers and missing or invalid headers keep the existing retry policy, and terminal guards still apply. This does not enable model fallback on HTTP 429. Engines without a retrying harness do not gain automatic retries.

The watchdog arms after a terminal safe output (not diagnostic reports such as `report_incomplete`) and resets on any stdout or stderr activity. It can end a quiet process that is still working and count that as success. Claude counts only current-attempt terminal outputs; its [resume rules](/gh-aw/reference/engines/claude/#harness-recovery) also apply.

Threat detection defaults to zero retries; configure its engine explicitly to change that. Model fallback below retries individual inference requests, not whole agent attempts.

### Model aliases

> [!NOTE]
> Copilot catalogs vary by organization and can lag seat offerings, so another workflow's model inventory does not prove availability here. An exact Copilot ID missing from a complete live catalog is **not offered to this workflow**; prefer `opus` or `sonnet`. Run `gh aw audit <run-url> --models` for run-specific catalog and routing details. Incomplete discovery does not confirm absence.

Copilot, Claude, Codex, Pi, and Agy resolve aliases against the configured provider's live AWF `/reflect` catalog before execution:

- Built-in aliases (such as `opus`/`small`), workflow-defined `models:`, and their globs select the newest live match.
- A `provider/` prefix restricts the lookup; otherwise the engine's configured provider applies.
- Exact live IDs take precedence over same-named aliases, and unrecognized IDs are not rewritten. Pi checks exact IDs before launch; Copilot, Claude, and Codex fail on the first unavailable-model request.
- `?effort=` parameters are retained; `auto` keeps its engine-specific meaning.
- Missing matches fail with `model_unavailable`; incomplete discovery fails with `catalog_unavailable`.

Gemini and declarative engines pass model names through unchanged and warn about gh-aw aliases. AWF can independently rewrite unrecognized models on non-Copilot providers; disable [`sandbox.agent.model-fallback`](/gh-aw/reference/sandbox/#model-fallback-sandboxagentmodel-fallback) when exact routing is required.

Resolution produces `model.resolution` evidence in `usage/aw_session.jsonl`. See [Model Aliases](/gh-aw/reference/model-tables/) for the catalog and [Model Routing](/gh-aw/reference/model-routing/) for experimental per-run model/effort selection on Copilot-backed Copilot, Claude, Codex, and Pi, chosen with `model: router` (or `router/economy`, `router/balanced`, `router/robust`). Router values are not model names: `router` and `router/*` are reserved and can't be used as aliases.

### Default model routing

On GitHub Copilot inference, `copilot`, `claude`, `codex`, and `pi` workflows that set no model or routing use `goal: cost` and `mode: auto`, the same as `model: router` (Claude and Codex also require `model-provider: github`). Any explicit `model` (including a router value), `engine.model`, `engine.model-routing`, `engine.fallback-models`, or model `engine.env` variable, including from imports, keeps existing behavior, as do unsupported or uncertain configurations. A nonempty `GH_AW_MODEL_AGENT_<ENGINE>` or `GH_AW_DEFAULT_MODEL_<ENGINE>` variable overrides the default at run time for Copilot, Claude, and Codex. Opt out with `engine.model-routing: false`; see [default routing](/gh-aw/reference/model-routing/#default-routing-for-workflows-without-a-model).

### Ordered model fallback (`fallback-models`)

```yaml
engine:
  id: copilot
  model: grok-4.7
  fallback-models: [gpt-5.6-luna, mai-code-1.1-flash]
```

All built-in engines accept ordered same-provider alternatives with AWF v0.28.31+ and a compatible API-proxy image. AWF retries the failed request without restarting the agent, keeping policy and budget checks. An explicit chain disables automatic middle-power substitution.

Fallback covers server errors, request timeouts, unsupported models, and model-availability HTTP 400 errors. Authentication failures, HTTP 429, policy/guardrail failures, cancellation, and job timeouts do not switch models. Exhausted chains fail the run.

Unqualified alternatives inherit the primary provider; prefixes must match it, and credentials follow that provider's configuration. Cross-provider chains, fallback aliases, missing compatible AWF, and combinations with `engine.model-routing` fail compilation. Custom harnesses and drivers can use AWF fallback without implementing switching.

`token-usage.jsonl` records the serving model and `model_fallback` evidence, without changing harness retries or proving every alternative ran.

### Bare Mode (`bare`)

`engine.bare: true` (default `false`) disables ambient discovery in Copilot, Claude, and Pi. Copilot suppresses custom instructions; Claude keeps explicitly installed skills and plugins with a minimal tool set; Pi keeps managed infrastructure and explicitly loaded skills. See the individual references for restrictions.

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

Repository `engine-conformance-<id>.md` workflows are manual smoke tests. They check file, shell, and MCP probes using recorded receipts, not an agent's success claim. Missing credentials fail activation. Reports are kept for two days.

These tests do not establish security, model quality, SDK/driver coverage, or provider compliance with a requested model. Agy's [production gate](/gh-aw/reference/engines/agy/#troubleshooting-and-conformance) is separate from its native authentication probe.

<span id="experimental-agy-authentication-gate"></span>
<span id="learn-more"></span>

## Engine-specific sections

The following anchors retain links to sections moved from this page.

- Copilot custom agents, release integrity, BYOK, and SDK: see the [Copilot reference](/gh-aw/reference/engines/copilot/#copilot-custom-configuration), including [checksum pins](/gh-aw/reference/engines/copilot/#copilot-release-checksum-pins), [BYOK](/gh-aw/reference/engines/copilot/#copilot-bring-your-own-key-byok-mode), and [SDK support](/gh-aw/reference/engines/copilot/#copilot-sdk-support).
- Pi `extensions`: [Pi packages](/gh-aw/reference/engines/pi/#pi-extensions-extensions).
- Claude tool enforcement: [Claude permissions and gateway enforcement](/gh-aw/reference/engines/claude/#claude-tool-enforcement-security-model).
- Shared imported engines and samples: [Custom AI engines](/gh-aw/reference/third-party-agent/#shared-imported-engines) and [third-party samples](/gh-aw/reference/third-party-agent/#unsupported-engine-samples), including the experimental Atomic and mini-SWE-agent declarative engines.
