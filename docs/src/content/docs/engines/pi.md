---
title: Using Pi with GitHub Agentic Workflows
description: Select and authenticate Pi as the AI engine for GitHub Agentic Workflows, understand its capabilities and limitations, and start from an example.
---

[Pi](https://pi.dev/) is a provider-agnostic coding agent for repository analysis and code changes. GitHub Agentic Workflows runs Pi through GitHub Actions from a Markdown workflow and adds GitHub triggers, sandbox controls, and safe outputs for event-driven, reviewable automation.

Pi requires `tools.github.mode: gh-proxy` and `tools.cli-proxy: true`. The compiler rejects Pi workflows that omit either requirement.

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
  cli-proxy: true
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

Pi supports top-level `max-turns`, provider-prefixed models, and `engine.extensions`. Pi already runs in bare mode, so `engine.bare: true` is accepted but has no effect. The built-in `tools.playwright` integration works through `playwright-cli`; omit its `mode` field because CLI is the only built-in mode. Pi does not provide native MCP server integration, native `tools.web-search`, per-command bash allowlisting, `max-continuations`, native `engine.agent` selection, or custom `engine.harness` scripts. MCP-backed tools must be exposed through the required CLI proxy.

See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison) and [Pi extensions reference](/gh-aw/reference/engines/#pi-extensions-extensions).

## GitHub Agentic Workflows vs. running Pi directly in Actions

Running coding agent CLIs such as Pi directly in GitHub Actions without an adequate security architecture is not recommended. GitHub Agentic Workflows provides sandboxing, credential isolation, scoped permissions, safe outputs, and workflow portability across AI engines.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
