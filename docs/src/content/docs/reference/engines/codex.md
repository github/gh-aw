---
title: OpenAI Codex reference
description: OpenAI Codex engine configuration, supported tools, runtime behavior, and limitations.
---

For first-time setup, see [Using OpenAI Codex](/gh-aw/engines/codex/). Shared options are in [AI engines](/gh-aw/reference/engines/).

## Selection and authentication

Use `engine: codex` with `CODEX_API_KEY` or `OPENAI_API_KEY`. ChatGPT subscription login is unsupported. The default CLI pin is `0.165.0`; override it with `engine.version`.

Codex has no default model. Set `model` explicitly or configure the repository's model-selection variables; see [model selection](/gh-aw/reference/model-tables/).

## GitHub Copilot inference

To select Codex as the AI engine, with inference hosted and billed through a GitHub Copilot subscription, add a `copilot/` model declaration. This configures Codex's BYOK provider to use GitHub Copilot inference. Select a Codex-compatible model available to your Copilot account. For example:

```yaml
engine:
  id: codex
  model: copilot/gpt-6.1-sol
```

GPT-6.1 Sol, GPT-6 Sol, GPT-6 Luna, GPT-6 Astra, and GPT-5.6 Sol, Terra, and Luna support Codex through the Responses API without a `-codex` suffix. Check [Codex model support](https://developers.openai.com/codex/models), [API pricing](https://developers.openai.com/api/docs/pricing), and [API deprecations](https://developers.openai.com/api/docs/deprecations) before pinning a model; a cached pricing entry alone does not establish availability.

Both `copilot/auto` and `auto` with `engine.model-provider: github` delegate model selection to the Copilot gateway through the Responses API. The bare spelling does not change Codex's default OpenAI provider; select GitHub explicitly to use Copilot inference.

To authenticate:
- For organization-billed usage, grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission).
- Otherwise, provide a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) secret containing a fine-grained PAT with Copilot Requests access.

GitHub inference requires the AWF sandbox. Do not set `sandbox.agent: false` for this provider. For a model supplied through an expression or repository variable, set `engine.model-provider: github` explicitly; runtime model prefixes do not change the selected credentials.

GitHub inference uses direct function-backed `exec_command` for native shell execution unless `tools.bash: false`. JavaScript code mode is disabled because the compatibility adapter does not support its custom `exec` tool. gh-aw generates a direct-tool catalog from the installed binary's bundled metadata; MCP and `apply_patch` remain available. The binary must support `debug models --bundled`. `engine.config` cannot re-enable code mode or replace the managed catalog. OpenAI inference is unchanged.

## Capabilities and limitations

Codex enables its shared native search/browsing tool when either `tools.web-search` or `tools.web-fetch` is enabled. Search and page fetching cannot be disabled independently; use `network.hosted-web` to restrict hosted retrieval. Codex can disable shell execution completely, but not restrict it to a per-command `tools.bash` allowlist.

With OpenAI inference through the AWF sandbox, enabling `network.hosted-web` also enables Codex's standalone `web.run` search tool for Responses Lite models, which do not receive the hosted `web_search` tool. Without hosted web, this provider flag is not set. Codex standalone search does not support `network.hosted-web.max-uses`; omit that limit when using Codex with OpenAI inference.

`network.hosted-web` is not functional with Codex using GitHub Copilot inference. The compiler rejects this combination; use OpenAI inference to enable Codex standalone web search.

Codex does not support bare mode, Copilot-style `max-continuations`, or native `engine.agent` selection. `engine.harness.use` can select a replacement harness that has been provisioned in the setup-action directory before execution. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

When a workflow does not declare any `plugins`, GitHub Agentic Workflows writes [`features.plugins=false`](https://developers.openai.com/codex/config-reference/#features) to Codex's generated `config.toml`. This prevents Codex from contacting the ChatGPT plugin catalog or synchronizing the curated plugin repository at startup. Codex does not provide a narrower setting that disables only startup synchronization, so workflows that declare Agent Plugins keep the plugin subsystem and its startup checks enabled. Directly configured MCP servers are unaffected.

Plugins are registered after configuration generation in the same `CODEX_HOME` used for execution. Inline skills use `.codex/skills/<name>/SKILL.md`.

The harness resolves [model aliases](/gh-aw/reference/engines/#model-aliases) before Codex starts, against the configured provider's live models: `copilot/gpt-5.6` runs on the newest live `gpt-5.6*` model, and workflow-defined aliases under `models:` work the same way. Exact ids such as `copilot/gpt-5.6-sol` reach Codex unchanged. `copilot/auto` stays the Copilot gateway sentinel.

## Custom Codex configuration

`engine.config` accepts TOML that is parsed and structurally merged with the generated configuration. Root settings remain at the root, and table settings preserve unrelated defaults:

```yaml wrap
engine:
  id: codex
  config: |
    model_reasoning_effort = "high"
```

Generated configuration disables the default metrics exporter and preserves native MCP timeouts and client headers. MCP connections and the AWF provider's connection settings remain gateway-managed; configure upstream servers through `mcp-servers` and inference endpoints through `engine.api-target` or `engine.env.OPENAI_BASE_URL`.

Use `${ENV_VAR}` inside TOML strings to reference an environment value configured through `engine.env`. GitHub Actions expressions are not accepted directly inside `engine.config`. The native integration supports OpenAI Responses-compatible inference and GitHub inference; an Anthropic backend requires a Responses-compatible bridge configured as an OpenAI endpoint.

For shell environment filtering, use either `[shell_environment_policy.filters]` or the legacy `include_only`/`exclude` arrays. Selecting one representation replaces inherited settings from the other; declaring both in the same custom configuration is a compile-time error.

## Context-rebuild circuit breaker

The Codex harness enables a context-rebuild circuit breaker by default to limit excessive repeated input processing, such as runaway context-rebuilding loops. It stops a run when **both** `rebuild_factor >= 25` and `cumulative_input_tokens >= 1,000,000`. The factor is cumulative input tokens divided by the largest single request's input tokens, calculated from token-usage records. It is not a count of actual context resets or compactions: a long run making steady progress can also cross the threshold. After this attempt emits `noop`, any non-diagnostic task output, or a `report_missing_data` or `report_incomplete` diagnostic, the breaker allows Codex to finish; `report_missing_tool` does not exempt the run. A separate post-result watchdog handles slow shutdown.

Configure these harness controls through `engine.env`, not through Codex's TOML configuration:

| Environment variable | Default | Behavior |
| --- | --- | --- |
| `GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER` | Enabled | Set to `false`, `0`, `off`, or `no` to disable (case-insensitive, surrounding whitespace ignored). |
| `GH_AW_CODEX_MAX_REBUILD_FACTOR` | `25` | Factor threshold; accepts finite numbers greater than or equal to `1`. |
| `GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS` | `1000000` | Cumulative input floor; accepts finite numbers that round down to at least `1` token. |
| `GH_AW_CODEX_REBUILD_GUARD_POLL_MS` | `15000` | Check interval in milliseconds; positive values round down and are clamped to a minimum of `1000`. |
| `GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS` | `5000` | Grace period after SIGTERM before SIGKILL, in milliseconds; positive values round down and are clamped to a minimum of `250`. |

Missing or invalid numeric values use the defaults. A guard stop is not retried. It emits `report_incomplete` with `reason: infrastructure_error`; its `details` start with `Codex run stopped by a runtime guard.` and include the measured factor, cumulative input, and thresholds for this breaker. Dashboards can use these details to distinguish a guard cutoff from a process crash.

### Tuning for long runs that make progress

Use successful runs of the same workload to establish a baseline and set the factor limit above their observed range with headroom. For workloads completing around factor `30`, `50` is a reasonable starting point, not a universal safe limit:

```yaml wrap
engine:
  id: codex
  env:
    GH_AW_CODEX_MAX_REBUILD_FACTOR: "50"
```

Recompile and commit the generated workflow after changing `engine.env`. To disable this breaker instead, set `GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER: "false"`. Raising or disabling the limit permits more input processing and potentially higher cost; retain a workflow timeout and an appropriate [`max-ai-credits` budget](/gh-aw/reference/frontmatter/).

Inspect per-request `input_tokens` in the `agent` artifact's `token-usage.jsonl` alongside tool activity in `agent-stdio.log` or `agent-session.jsonl`. Steadily growing or plateauing input with new edits, test results, or completed subtasks is consistent with useful progress. Repeated equivalent tool calls, repeated failed approaches, or repeated context drops and regrowth without new results warrant investigation. Input growth between consecutive requests alone cannot prove a loop or progress; the breaker does not evaluate these signals, and a high factor alone does not establish a runaway loop.

## Session artifacts

The `agent` artifact retains native `agent-stdio.log` and normalized `agent-session.jsonl`. The conclusion job projects essential fields into `usage/aw_session.jsonl`, following the [unified session contract](/gh-aw/specs/unified-agent-session-specification/).

| Codex observation | Session mapping |
| --- | --- |
| Thread startup and recognized legacy headers | `session.init`, with observed model, working directory, reasoning effort, and available inventories. |
| Agent text, reasoning, and user text | `assistant.message`, `assistant.reasoning`, and `user.message`, with exact content. Explicit provider refusal signals use `assistant.refusal`; natural-language disclaimers do not. |
| Commands and MCP calls | Independent `tool.execution_start` and `tool.execution_complete` events, preserving native IDs, arguments, outputs, and explicit failures, including declined commands. |
| File changes | An `apply_patch` tool completion with observed changes and status. A change summary does not supply executed patch arguments or a missing start. |
| Web searches and collaboration calls | Standard tool events with observed queries/actions or receiver IDs/prompts, and available search results or agent states. Completing `spawn_agent` does not mean the child finished. |
| Completed turns and session errors | `session.result`, with cumulative per-turn usage or separate error observations. A completed CLI turn does not establish task success. |

Tool IDs remain scoped to their observed thread for correlation across retries. Unfinished tools remain dangling; orphan completions do not acquire invented inputs. Known message updates are snapshots, not text deltas: only the latest available content is exposed as a core message, while superseded snapshots remain native evidence. Todo-list snapshots and unknown native extensions remain available without fabricating tool executions.

Reported totals and cache aliases are counted once. Later authoritative accounting snapshots replace corresponding totals rather than adding to them; unavailable counts, duration, and cost remain absent. Legacy message sections retain JSON-looking answers as text instead of interpreting them as execution or usage records.

Regression fixtures include sanitized excerpts from [Smoke Codex](https://github.com/github/gh-aw/actions/runs/36909965579), [Daily Documentation Updater](https://github.com/github/gh-aw/actions/runs/36850958249), and an [unsupported-model failure](https://github.com/github/gh-aw/actions/runs/37100404405). Advanced tool, refusal, overflow, and mixed-format cases are synthetic protocol-shaped regressions, not claims that these features appeared in the sampled runs.
