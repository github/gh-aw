---
title: Pi reference
description: Pi engine configuration, supported tools, runtime behavior, and limitations.
---

For first-time setup, see [Using Pi](/gh-aw/engines/pi/). Shared options are in [AI engines](/gh-aw/reference/engines/).

## Providers and authentication

| Model prefix | Credential |
| --- | --- |
| `copilot/` or no prefix | `copilot-requests: write` or `COPILOT_GITHUB_TOKEN` |
| `anthropic/` | `ANTHROPIC_API_KEY` |
| `openai/` or `codex/` | `CODEX_API_KEY` or `OPENAI_API_KEY` |
| `google/` | `GEMINI_API_KEY` |

The default Pi version is `1.3.0`. Copilot defaults to `copilot/auto` with the gateway; native execution without the gateway retains `copilot/gpt-5.4`. `auto` delegates selection to the gateway and need not appear in its concrete-model inventory. OpenAI/Codex routes use the Responses API, not Chat Completions.

Threat detection defaults to Copilot regardless of Pi's provider. Supply its separate Copilot credential or [configure another detection engine](/gh-aw/reference/threat-detection/).

## Capabilities and limitations

Pi supports native MCP tools and resources, codemode, tool search, provider-prefixed models, and workflow-installed Pi packages. Native MCP connects only to the compiler-managed gateway; provider and GitHub credentials remain outside the sandbox.

Safe-output instructions use native MCP by default. Workflows with `tools.bash: false` must explicitly set `tools.cli-proxy: false` and use native tools. Explicit `tools.cli-proxy: true` selects CLI-only instructions for mounted safe-output tools, just as it does for other MCP-capable engines. GitHub `gh-proxy` mode changes GitHub reads only; it does not disable native safe-output tools.

Codemode batches tool calls and reduces results before they enter model context. Its classifier and image-generation APIs are available when the configured provider route supports those model types. Custom providers still need a compatible API-proxy target and credentials; enabling codemode does not grant additional credentials or network access.

For Copilot models using the Responses API, gh-aw disables grammar-based custom tools (`model.compat.supportsOpenAIGrammarTools: false`) so Pi sends codemode as a JSON-schema function tool. Codemode remains enabled; the Copilot compatibility adapter does not accept its custom-tool format. Other provider routes retain their configured compatibility settings.

| Option | Pi behavior |
|---|---|
| `max-turns`, `max-ai-credits` | Enforced by the AWF inference proxy. |
| `max-tool-calls` | Pre-dispatch limit covering local, MCP, and nested codemode calls. |
| `max-tool-denials` | Stops inference after repeated policy denials. |
| `tools.bash` | Supports disabling bash, executable-name rules, exact multiword commands, and explicit argument wildcards (`:*` or ` *`). Restricted commands reject shell comments, dynamic expansions, redirections, grouping, and background execution. |
| `tools.edit: false` | Disables native `edit` and `write` tools. |
| `engine.bare: true` | Disables automatic context, skill, prompt-template, extension, and theme discovery; retains explicit workflow infrastructure extensions and workflow-declared or imported inline skills. |
| `engine.extensions` | Installs npm, pinned git, or local Pi packages into the managed runtime directory. |
| `engine.driver: pi_agent_core_driver.cjs` | Uses the full coding-agent SDK session layer, including tools, resources, compaction, and retries. |
| `engine.driver: pi_rpc_driver.cjs` | Runs a headless RPC subprocess through Pi's RPC client. |

In bare mode, ambient repository and user skills are not discovered. Inline skills (`## skill:` blocks) that activation extracts from the workflow, its imports, or runtime imports remain available: before Pi starts, gh-aw reads the list of skills activation extracted and passes one explicit `--skill /tmp/gh-aw/pi-agent-dir/skills/<name>/SKILL.md` per skill to the CLI, sub-agents, and the SDK and RPC drivers. Workflows without inline skills get no `--skill` arguments.

Project-local executable resources remain untrusted by default. Workflow-installed packages and activation-installed skills use the managed agent directory. Model catalog metadata preserves thinking, vision, token limits, pricing, and cache lifetimes through gateway routing. Workflow system instructions remain separate from user instructions so compaction does not summarize them away.

When AWF `/reflect` completes model discovery and reports reasoning-effort support for the selected provider and model, Pi uses that capability information instead of model-name exceptions. If discovery is incomplete or reflection does not report reasoning capability data, Pi retains the model catalog or `engine.config.model` reasoning setting. Missing effort fields alone do not disable reasoning.

Custom executables without the npm SDK must supply complete model metadata (`reasoning`, `input`, `contextWindow`, and `maxTokens`) in `engine.config.model`.

The integration runs Pi headlessly; terminal UI controls, interactive authentication, and `/share` uploads are not workflow outputs. Native provider fixes, prompt-cache behavior, model switching, codemode worker limits, and session tool restoration come from the installed Pi release. The `kimi-coding` provider uses Chat Completions in v1.3.0; custom native base URLs must include `/v1`. Sandboxed custom routes still require a compatible AWF endpoint.

The built-in `tools.playwright` integration uses `playwright-cli`; omit its `mode` field. Native `tools.web-search`, `max-continuations`, native `engine.agent` selection, and custom `engine.harness` scripts remain unsupported. Custom drivers must implement their own policies and cannot opt into the built-in aggregate tool budgets.

Pi resolves [model aliases](/gh-aw/reference/engines/#model-aliases) for the main model and for sub-agents with the same rules as Copilot: `model: copilot/opus` runs on the newest live Opus. Exact ids are checked against AWF `/reflect` before Pi starts, and an unlisted id fails with `model_unavailable`, producing the same unavailable-model failure issue as Copilot. When AWF's model discovery is incomplete, exact ids pass through and aliases fail with `catalog_unavailable`; sub-agents no longer fall back to Pi's built-in model list.

## Durable runs

> [!WARNING]
> Pi Durable integration is experimental. Enabling `engine.pi.durable` or
> `engine.durable` emits a compile-time warning. Configuration and behavior may
> change without deprecation.

Pi Durable persists harness tasks, submissions, conversation history, tool-call
budgets, usage and trace propagation in a dedicated GitHub Actions artifact:

```aw wrap
on: workflow_dispatch
engine:
  pi:
    durable:
      retention-days: 7
```

`durable: {}` enables the default retention of seven days. `retention-days`
accepts an integer from 1 to 90, subject to repository and organization artifact
retention policy and gh-aw's global artifact retention overrides. Durable storage
does not use or require `tools.repo-memory` or `tools.cache-memory`.

The compiler installs and verifies pinned `@earendil-works/pi-durable`, `pi-ai`,
`chord` and `pi-telemetry` dependencies separately from the ordinary Pi CLI.
Unsupported engines and custom drivers fail compilation rather than falling back
to a non-durable invocation. Custom commands, CLI arguments, extensions, native
session settings and sub-agents cannot be combined with the durable harness.
The credential-isolated model and MCP gateways remain required.

Before execution, a trusted host step selects the parent run identified by
inbound `aw_context.run_id` (or `parent_run_id` when the context describes the
current run). Without an explicit parent, it selects the last successful run of the same workflow on the same
branch, considering the latest 100 successful matching runs. Artifacts are scoped
to the repository, workflow and branch. Fork-originated runs and other
in-progress runs are excluded. An explicit parent must belong to that same
workflow and branch; it may have failed or been cancelled if it published a
checkpoint. Missing or expired explicit-parent checkpoints fail rather than
silently switching lineage. Without an explicit parent, a missing or expired
artifact on the last successful run starts a fresh session.
When retrying a run, an unexpired checkpoint published by an earlier attempt of
that same run takes precedence over the original parent.

Pending submissions resume their original task rather than admitting the new
run's prompt. After a submission completes, a later run admits a new prompt into
the persisted conversation. Restoring a completed checkpoint from the same run
does not resubmit it.
The harness yields before its execution deadline with a pending checkpoint so a
successful workflow run can become the next run's parent. Interrupted external
mutations are not blindly replayed; durable execution does
not guarantee exactly-once external side effects.

Workflow-level concurrency serializes execution and artifact publication without
cancelling the previous writer. Explicit concurrency, reusable workflow calls and
work-queue workers are not supported. The compiler grants `actions: read` only
to the job that restores state; GitHub credentials are not passed to the harness.
The host validates checkpoint integrity after secret redaction, then publishes
the artifact without treating upload failures as successful persistence.

The harness emits Pi streaming events for the existing unified agentic session
parser and emits native durable spans through gh-aw's OTel exporter, retaining
trace propagation across checkpoints. Inference and tool policies are rebound
to the current run instead of restoring credentials from the artifact.

> [!IMPORTANT]
> Checkpoints preserve harness state, not the checkout or arbitrary workspace
> edits. A hard job timeout, cancellation, runner loss or artifact upload failure
> can prevent publication. Start a later run to continue from the last published
> checkpoint; durable mode does not dispatch continuation runs automatically.

## Sub-agents

Define [inline sub-agents](/gh-aw/reference/inline-sub-agents/) with a non-empty `description` and instruct the parent to call the `subagent` tool:

```aw wrap
Use subagent with {"agent":"reader","task":"Summarize README.md"}.

## agent: `reader`
---
description: Reads and summarizes files
model: small
tools: [read, grep, find, ls]
---
Read the requested files and report the facts.
```

The built-in CLI, SDK, and RPC modes load managed delegation, including with `engine.bare: true`. Each invocation starts an isolated, ephemeral child session with the agent's instructions and the workflow system prompt. The tool accepts one agent and task per call; it does not accept chain or parallel-task parameters, and children cannot recursively delegate.

An omitted model inherits the parent's configured model. Bare model IDs use the parent's provider; aliases such as `small` resolve against that provider's models in AWF `/reflect`, using the same rules as the main model. A declaration cannot switch to another provider or silently fall back to the parent model when alias resolution fails. The optional `?effort=` parameter supports `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`; Pi maps `none` to `off` and clamps thinking to the selected model's capabilities. The `max` effort requires installed Pi v1.3.0 or newer and is rejected during runtime setup on older installations. Other model parameters are rejected. An explicit `name` must match the agent marker.

Children retain the workflow's inference gateway, MCP configuration, steering, bash allowlist, and edit restrictions. Tool-call and denial budgets are shared with the parent. Children use compiler-managed execution arguments rather than inheriting `engine.args` overrides. Optional automatically discovered extensions are disabled in children; only the managed infrastructure extensions are loaded. Custom drivers do not support this delegation path.

Children return task results to the parent instead of emitting workflow-completion reports or `noop` safe outputs. Task-required safe-output actions remain available. Time-pressure steering applies to ongoing tool turns, not completed answers, so it does not reopen a finished child session.

Dispatch logs record both the requested alias and resolved model. `gh aw audit` reports `subagent_model_not_observed` when usage evidence exists but no agent inference matches a declared model. This means the agent may not have run or its model was not honored; model presence alone does not prove delegation. Detection requests are not treated as agent-model evidence.

## Additional providers

`google/` models use the Gemini gateway and `GEMINI_API_KEY`. Other API-compatible providers can use an explicit inference family and endpoint:

```yaml wrap
engine:
  id: pi
  model: openrouter/anthropic/claude-sonnet-4
  model-provider: openai
  env:
    OPENAI_BASE_URL: https://openrouter.ai/api/v1
    OPENAI_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
network:
  allowed: [defaults, openrouter.ai]
```

Compatible provider catalogs, including classifier and image models, retain their native identities but route through the same credential-isolated gateway. Requests must be supported by that gateway and upstream endpoint. Providers requiring different protocols or ambient cloud authentication can run natively only when the agent sandbox is explicitly disabled; this removes credential isolation and is not the recommended deployment mode. Unsupported sandboxed provider routes fail compilation instead of silently switching to Copilot.

## Pi runtime configuration

`engine.config` accepts JSON with four optional objects:

| Object | Purpose |
|---|---|
| `settings` | Pi settings, including `defaultThinkingLevel`, `codemode`, compaction, retries, and cache warming. Project trust remains disabled. |
| `model` | Model metadata overrides: API protocol, reasoning, input types, context/output limits, pricing, compatibility, and prompt-cache lifetimes. |
| `mcp` | Default `exposure` (`direct`, `deferred`, `codemode`, or `hidden`) and per-server `toolExposure` patterns. Defaults to `direct`, making configured MCP tools available without tool search. Does not add servers or credentials. |
| `session` | `enabled`, optional `id`, `resume` or `fork`, and `export`. |

The compiler supplies `mcp.exposure: "direct"` when omitted, including when other
configuration objects or per-tool overrides are present. Explicit exposure modes
and per-tool overrides are preserved.

```yaml wrap
engine:
  id: pi
  config: |
    {
      "settings": {"defaultThinkingLevel": "high", "codemode": {"mode": "on"}},
      "session": {"enabled": true, "export": true}
    }
max-tool-calls: 100
```

Sessions are ephemeral by default. Persistent JSONL sessions are written under `/tmp/gh-aw/agent/pi-sessions`; restore a previous session artifact there before using `resume` or `fork`. HTML exports are stored at `/tmp/gh-aw/pi-agent-dir/session.html`. Both are secret-redacted before artifact upload.

Pi's streaming event log is written to `/tmp/gh-aw/sandbox/agent/logs/pi-streaming.jsonl` for step summaries and session artifacts. With sandboxed `runner.topology: arc-dind`, the compiler uses `${RUNNER_TEMP}/gh-aw/sandbox/agent/logs/pi-streaming.jsonl` inside the writable agent mount; no extra mounts or pre-steps are required. With `sandbox.agent: false`, execution and log parsing retain the `/tmp/gh-aw/sandbox/agent/logs/pi-streaming.jsonl` path. Session reconstruction also accepts historical artifacts with `pi-streaming.jsonl` at the artifact root.

Settings are passed through to Pi, including `codemode.inlineBudget`, per-model thinking and compaction overrides, and v1.3.0's `sessionShareDestination`. The latter configures Pi's `/share` command; it does not publish workflow artifacts automatically. Installed `defaultTools` selections are merged with gh-aw's `+codemode` and `+tool_search` defaults; explicit `engine.config.settings.defaultTools` replaces that list. On resume, Pi restores tools activated in the saved session; explicit CLI `--tools` selections or exclusions still apply. See the [Pi settings reference](https://github.com/earendil-works/pi/blob/v1.3.0/packages/coding-agent/docs/settings.md).

See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison) and [Pi extensions reference](/gh-aw/reference/engines/pi/#pi-extensions-extensions).

## Pi Extensions (`extensions`)

The Pi engine supports packages via `engine.extensions`. Each npm, pinned git, or local package source is installed with `pi install <source>` into the same managed agent directory used at execution. Packages can contain extensions, skills, and prompt templates. Only Pi reads this field; other engines ignore it. Pi packages are distinct from the top-level Agent Plugins `plugins` field.

```yaml wrap
engine:
  id: pi
  extensions:
    - "npm:@example/pi-tools@1.0.0"
```

Each listed extension produces one additional install step in the compiled workflow. If `engine.command` is set, the same executable is used to install the extensions.

Pi v1 also enables codemode and tool search, supports native MCP tools and resources through the policy gateway, and provides built-in coding-agent SDK and RPC drivers. Use JSON in `engine.config` to configure settings, model metadata, MCP exposure, and persistent sessions. See [Pi runtime configuration](/gh-aw/reference/engines/pi/#pi-runtime-configuration).
