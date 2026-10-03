---
title: Using OpenAI Codex with GitHub Agentic Workflows
description: Select and authenticate OpenAI Codex as the AI engine for GitHub Agentic Workflows, understand its capabilities and limitations, and start from an example.
---

[OpenAI Codex](https://openai.com/codex/) is OpenAI's coding-focused agent runtime for repository work. GitHub Agentic Workflows runs Codex through GitHub Actions from a Markdown workflow and adds GitHub triggers, sandbox controls, and safe outputs for event-driven, reviewable automation.

## Selecting Codex + OpenAI as the AI engine

To select Codex as the AI engine, with inference hosted and billed through the OpenAI API, add this to the workflow frontmatter:

```yaml
engine: codex
model: openai/gpt-6.1-sol
```

To authenticate, provide a [`CODEX_API_KEY`](/gh-aw/reference/auth/#openai_api_key) or [`OPENAI_API_KEY`](/gh-aw/reference/auth/#openai_api_key) as a GitHub Actions repository secret.

ChatGPT subscription login is not configured by this integration.

Recompile the workflow with `gh aw compile` and commit the changes to your repository. The workflow will now run with Codex as the AI engine.

## Selecting Codex + GitHub as the AI engine

To select Codex as the AI engine, with inference hosted and billed through a GitHub Copilot subscription, add a `copilot/` model declaration. This configures Codex's BYOK provider to use GitHub Copilot inference. Select a Codex-compatible model available to your Copilot account. For example:

```yaml
engine:
  id: codex
  model: copilot/gpt-6.1-sol
```

GPT-6.1 Sol, GPT-6 Sol, GPT-6 Luna, GPT-6 Astra, and GPT-5.6 Sol, Terra, and Luna support Codex through the Responses API without a `-codex` suffix. Check [Codex model support](https://developers.openai.com/codex/models), [API pricing](https://developers.openai.com/api/docs/pricing), and [API deprecations](https://developers.openai.com/api/docs/deprecations) before pinning a model; a cached pricing entry alone does not establish availability.

To authenticate:
- For organization-billed usage, grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission).
- Otherwise, provide a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) secret containing a fine-grained PAT with Copilot Requests access.

GitHub inference requires the AWF sandbox. Do not set `sandbox.agent: false` for this provider. For a model supplied through an expression or repository variable, set `engine.model-provider: github` explicitly; runtime model prefixes do not change the selected credentials.

Recompile the workflow with `gh aw compile` and commit the changes to your repository. The workflow will now run with Codex as the AI engine.

## Example: scheduled repository report

```aw wrap title=".github/workflows/daily-status.md"
---
on:
  schedule: daily

permissions:
  contents: read
  issues: read
  pull-requests: read

engine: codex

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

Codex enables its shared native search/browsing tool when either `tools.web-search` or `tools.web-fetch` is enabled. Search and page fetching cannot be disabled independently; use `network.hosted-web` to restrict hosted retrieval. Codex can disable shell execution completely but cannot enforce a nonempty per-command `tools.bash` allowlist.

Codex does not support bare mode, Copilot-style `max-continuations`, or native `engine.agent` selection. `engine.harness.use` can select a replacement harness that has been provisioned in the setup-action directory before execution. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

When a workflow does not declare any `plugins`, GitHub Agentic Workflows writes [`features.plugins=false`](https://developers.openai.com/codex/config-reference/#features) to Codex's generated `config.toml`. This prevents Codex from contacting the ChatGPT plugin catalog or synchronizing the curated plugin repository at startup. Codex does not provide a narrower setting that disables only startup synchronization, so workflows that declare Agent Plugins keep the plugin subsystem and its startup checks enabled. Directly configured MCP servers are unaffected.

Plugins are registered after configuration generation in the same `CODEX_HOME` used for execution. Inline skills use `.codex/skills/<name>/SKILL.md`.

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

## GitHub Agentic Workflows vs. running Codex directly in Actions

Running coding agent CLIs such as `codex` directly in GitHub Actions without an adequate security architecture is not recommended. GitHub Agentic Workflows gives an appropriate security architecture and workflow portability across AI engines.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
