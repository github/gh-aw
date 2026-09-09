---
title: Using OpenAI Codex with GitHub Agentic Workflows
description: Select and authenticate OpenAI Codex as the AI engine for GitHub Agentic Workflows, understand its capabilities and limitations, and start from an example.
---

[OpenAI Codex](https://openai.com/codex/) is OpenAI's coding-focused agent runtime for repository work. GitHub Agentic Workflows runs Codex through GitHub Actions from a Markdown workflow and adds GitHub triggers, sandbox controls, and safe outputs for event-driven, reviewable automation.

## Selecting Codex + OpenAI as the AI engine

To select Codex as the AI engine, with inference hosted and and billed through an OpenAI subscription, add this to the workflow frontmatter:

```yaml
engine: codex
```

To authenticate, provide a [`CODEX_API_KEY`](/gh-aw/reference/auth/#openai_api_key) or [`OPENAI_API_KEY`](/gh-aw/reference/auth/#openai_api_key) as a GitHub Actions repository secret.

Recompile the workflow with `gh aw compile` and commit the changes to your repository. The workflow will now run with Codex as the AI engine.

## Selecting Codex + GitHub as the AI engine

To select Codex as the AI engine, with inference hosted and billed through a GitHub Copilot subscription, add a `copilot/` model declaration. This configures Codex's BYOK provider to use GitHub Copilot inference. Select a Codex model because the Codex runtime relies on model capabilities that general-purpose models do not provide. For example:

```yaml
engine:
  id: codex
  model: copilot/gpt-5.3-codex
```
To authenticate:
- For organization-billed usage, grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission).
- Otherwise, provide a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) secret containing a fine-grained PAT with Copilot Requests access.

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

Codex supports native web search when `tools.web-search` is enabled and can disable shell execution completely. Codex cannot enforce a nonempty per-command `tools.bash` allowlist and does not support bare mode, `max-continuations`, native `engine.agent` selection, or custom `engine.harness` scripts. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

When a workflow does not declare any `plugins`, GitHub Agentic Workflows writes [`features.plugins=false`](https://developers.openai.com/codex/config-reference/#features) to Codex's generated `config.toml`. This prevents Codex from contacting the ChatGPT plugin catalog or synchronizing the curated plugin repository at startup. Codex does not provide a narrower setting that disables only startup synchronization, so workflows that declare Agent Plugins keep the plugin subsystem and its startup checks enabled. Directly configured MCP servers are unaffected.

## GitHub Agentic Workflows vs. running Codex directly in Actions

Running coding agent CLIs such as `codex` directly in GitHub Actions without an adequate security architecture is not recommended. GitHub Agentic Workflows gives an appropriate security architecture and workflow portability across AI engines.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
