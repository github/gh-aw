---
title: Using Gemini CLI with GitHub Agentic Workflows
description: Select and authenticate Google Gemini CLI as the AI engine for GitHub Agentic Workflows, understand its capabilities and limitations, and start from an example.
---

[Google Gemini CLI](https://geminicli.com/) is a coding agent from Google. GitHub Agentic Workflows runs Gemini CLI in GitHub Actions, adding GitHub event triggers, sandbox controls, and safe outputs for constrained, reviewable automation.

## Selecting Gemini CLI as the AI engine

To select Gemini CLI as the AI engine, with inference hosted and billed through a Google subscription, add this to the workflow frontmatter:

```yaml
engine: gemini
```

To authenticate, either:

1. Provide [`GEMINI_API_KEY`](/gh-aw/reference/auth/#gemini_api_key) as a GitHub Actions repository secret, or

2. configure keyless [Google Workload Identity Federation](/gh-aw/reference/auth/#google-workload-identity-federation-wif).

## Example: scheduled repository report

```aw wrap title=".github/workflows/daily-status.md"
---
on:
  schedule: daily

permissions:
  contents: read
  issues: read
  pull-requests: read

engine: gemini

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

Gemini supports top-level `max-turns`, custom API targets, and per-command bash allowlisting. Gemini does not provide native `tools.web-search`; configure an MCP search integration when needed. It also does not support bare mode, `max-continuations`, native `engine.agent` selection, or custom `engine.harness` scripts. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

## GitHub Agentic Workflows vs. running Gemini directly in Actions

Running coding agent CLIs such as `gemini` directly in GitHub Actions without an adequate security architecture is not recommended.  GitHub Agentic Workflows gives an appropriate security architecture and workflow portability across AI engines.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
