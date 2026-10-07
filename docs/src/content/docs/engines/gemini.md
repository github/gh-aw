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

The generated `.gemini/settings.json` selects API-key authentication explicitly so
the sandbox's inference proxy URL does not select Gemini's unsupported gateway
auth mode. Google Workload Identity Federation selects Vertex AI authentication
instead. Neither setting stores credentials in the project configuration.
Configured MCP servers are also included in Gemini's tool allowlist; their
gateway-side tool restrictions still apply.

In the AWF sandbox, `GEMINI_CLI_HOME` points to `/tmp/gh-aw/gemini-home`
so Gemini can write its project registry and runtime state without modifying
the protected `~/.gemini` directory. Project settings and system settings
remain at their existing paths; the shell's `HOME` is unchanged.

## Using Copilot-hosted Gemini models

Use a `copilot/gemini*` model with the AWF sandbox enabled:

```yaml
permissions:
  contents: read
  copilot-requests: write
engine:
  id: gemini
  model: copilot/gemini-3.8-flash
```

`copilot-requests: write` uses `${{ github.token }}` for inference and does not
require a PAT or `COPILOT_GITHUB_TOKEN` secret. Without that permission, provide
`COPILOT_GITHUB_TOKEN` instead. A Google API key is not required for this route.

A loopback bridge translates Gemini text, images, and function calls to Copilot
Chat Completions through AWF's credential-isolated proxy. Completions are buffered
per turn before being returned in Gemini's streaming format; utility model calls
use the same selected Copilot model. Google-hosted tools, custom safety settings,
cached-content requests, and the token-counting endpoint are not supported by
this route. Native `web_fetch` can use Gemini CLI's direct-fetch fallback.
Gemini's `topK` and thinking configuration use the Copilot model's defaults; the
bridge reports this when those options are present.

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
