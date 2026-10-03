---
title: Using Pi with GitHub Agentic Workflows
description: Select and authenticate Pi as the AI engine for GitHub Agentic Workflows, understand its capabilities and limitations, and start from an example.
---

[Pi](https://pi.dev/) is a provider-agnostic coding agent for repository analysis and code changes. GitHub Agentic Workflows runs Pi through GitHub Actions from a Markdown workflow and adds GitHub triggers, sandbox controls, and safe outputs for event-driven, reviewable automation.

Pi v1.0.0 supports native MCP through the gh-aw policy gateway. GitHub CLI proxy mode and `tools.cli-proxy: true` remain optional transport choices. The default model is `copilot/gpt-5.4` when no model is configured.

## Selecting Pi + GitHub as the AI engine

To select Pi as the AI engine, with inference hosted and billed through a GitHub Copilot subscription, use a `copilot/` model. A model without a provider prefix also uses the Copilot backend.

```yaml
engine:
  id: pi
  model: copilot/gpt-5.4
```

To authenticate:

- For organization-billed usage, grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission).
- Otherwise, provide a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) secret containing a fine-grained PAT with Copilot Requests access.

## Selecting Pi + Anthropic as the AI engine

To run Pi with inference hosted and billed through Anthropic, use an `anthropic/` model:

```yaml
engine:
  id: pi
  model: anthropic/claude-sonnet-4.6
```

To authenticate, provide [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key) as a GitHub Actions repository secret.

## Selecting Pi + OpenAI as the AI engine

To run Pi with inference hosted and billed through OpenAI, use an `openai/` or `codex/` model:

```yaml
engine:
  id: pi
  model: openai/gpt-5.4
```

To authenticate, provide a [`CODEX_API_KEY`](/gh-aw/reference/auth/#openai_api_key) or [`OPENAI_API_KEY`](/gh-aw/reference/auth/#openai_api_key) as a GitHub Actions repository secret.

Pi routes `openai/` and `codex/` models through OpenAI's [Responses API](https://developers.openai.com/api/docs/guides/responses-vs-chat-completions) rather than Chat Completions, since OpenAI rejects function tool calls on Chat Completions whenever reasoning is enabled. This matches Pi's own OpenAI model catalog and requires no additional configuration. The Copilot and Anthropic backends are unaffected and keep using their existing wire protocols.

## Authenticating threat detection

By default, threat detection for Pi workflows runs on the GitHub Copilot CLI, regardless of Pi's model provider. Grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission) or provide a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) secret for detection. This credential is separate from the OpenAI or Anthropic key used by the Pi agent.

For any provider, recompile the workflow with `gh aw compile` and commit the changes to the repository.

## Example: scheduled repository report

```aw wrap title=".github/workflows/daily-status.md"
---
on:
  schedule: daily

permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write

engine:
  id: pi
  model: copilot/gpt-5.4

tools:
  github:
    mode: gh-proxy
    toolsets: [default]

safe-outputs:
  create-issue:
    title-prefix: "[status] "
    labels: [report]
    close-older-issues: true
---

# Daily Repository Status

Analyze the repository and create a concise daily status report covering:
- Open issues and their priority
- Recent PR activity
- Upcoming work items
```

## Capabilities and limitations

Pi supports native MCP tools and resources, codemode, tool search, provider-prefixed models, and workflow-installed Pi packages. Native MCP connects only to the compiler-managed gateway; provider and GitHub credentials remain outside the sandbox.

Codemode batches tool calls and reduces results before they enter model context. Its classifier and image-generation APIs are available when the configured provider route supports those model types. Custom providers still need a compatible API-proxy target and credentials; enabling codemode does not grant additional credentials or network access.

| Option | Pi behavior |
|---|---|
| `max-turns`, `max-ai-credits` | Enforced by the AWF inference proxy. |
| `max-tool-calls` | Pre-dispatch limit covering local, MCP, and nested codemode calls. |
| `max-tool-denials` | Stops inference after repeated policy denials. |
| `tools.bash` | Supports disabling bash, executable-name rules, exact multiword commands, and explicit argument wildcards (`:*` or ` *`). Restricted commands reject shell comments, dynamic expansions, redirections, grouping, and background execution. |
| `tools.edit: false` | Disables native `edit` and `write` tools. |
| `engine.bare: true` | Disables automatic context, skill, prompt-template, extension, and theme discovery; retains explicit workflow infrastructure extensions. |
| `engine.extensions` | Installs npm, pinned git, or local Pi packages into the managed runtime directory. |
| `engine.driver: pi_agent_core_driver.cjs` | Uses the full coding-agent SDK session layer, including tools, resources, compaction, and retries. |
| `engine.driver: pi_rpc_driver.cjs` | Runs a headless RPC subprocess through Pi's RPC client. |

Project-local executable resources remain untrusted by default. Workflow-installed packages and activation-installed skills use the managed agent directory. Model catalog metadata preserves thinking, vision, token limits, pricing, and cache lifetimes through gateway routing. Workflow system instructions remain separate from user instructions so compaction does not summarize them away.

Custom executables without the npm SDK must supply complete model metadata (`reasoning`, `input`, `contextWindow`, and `maxTokens`) in `engine.config.model`.

The built-in `tools.playwright` integration uses `playwright-cli`; omit its `mode` field. Native `tools.web-search`, `max-continuations`, native `engine.agent` selection, and custom `engine.harness` scripts remain unsupported. Custom drivers must implement their own policies and cannot opt into the built-in aggregate tool budgets.

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
| `mcp` | Default `exposure` (`deferred`, `direct`, `codemode`, or `hidden`) and per-server `toolExposure` patterns. Does not add servers or credentials. |
| `session` | `enabled`, optional `id`, `resume` or `fork`, and `export`. |

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

See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison) and [Pi extensions reference](/gh-aw/reference/engines/#pi-extensions-extensions).

## GitHub Agentic Workflows vs. running Pi directly in Actions

Running coding agent CLIs such as Pi directly in GitHub Actions without an adequate security architecture is not recommended. GitHub Agentic Workflows provides sandboxing, credential isolation, scoped permissions, safe outputs, and workflow portability across AI engines.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
