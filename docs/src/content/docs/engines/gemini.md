---
title: Using Gemini CLI with GitHub Agentic Workflows
description: Maintain workflows using the deprecated Google Gemini CLI engine and evaluate migration to the experimental Agy engine.
---

[Google Gemini CLI](https://geminicli.com/) is a coding agent from Google. GitHub Agentic Workflows runs Gemini CLI in GitHub Actions, adding GitHub event triggers, sandbox controls, and safe outputs for constrained, reviewable automation.

> [!WARNING]
> The `gemini` engine is deprecated in favor of the experimental [`agy` engine](/gh-aw/engines/agy/).
> Existing Gemini workflows remain supported, with no automatic migration or removal date.
> For API-key workflows, review [Agy's migration guidance](/gh-aw/engines/agy/#evaluating-a-gemini-api-key-workflow)
> before changing engines. Keep `engine: gemini` for Google WIF or features Agy does not support,
> including native bash restrictions.

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

## Session artifacts

Gemini's native `agent-stdio.log` maps to the standard events in
`agent-session.jsonl`; conclusion publishes their essential fields in
`usage/aw_session.jsonl`. Messages retain exact text and streaming whitespace,
and tool events retain native IDs, structured arguments/results, and observed
outcomes. Understood provider errors use `session.error`, with non-warning
diagnostics also available in `session.result.errors`; system status uses
`session.info`, not an assistant answer. Identified message snapshots retain
their native history on core events without extra engine-specific wrapper events
or duplicate text. Existing native extension events remain supported.

Terminal accounting retains `sourceType: "result"` and reported zero values.
Gemini's `cached` tokens are already included in `input_tokens`; model subtotals
are not added again. Tool-call counts are not turns, and reasoning-token counts
are not inferred from a difference between total and input/output tokens.
Compatible reasoning and structured refusal observations use
`assistant.reasoning` and `assistant.refusal` when exposed; their absence from a
native stream does not imply an empty answer or a successful session.
See the [unified session specification](/gh-aw/specs/unified-agent-session-specification/#74-gemini)
for the mapping and existing-run fixture provenance.

## GitHub Agentic Workflows vs. running Gemini directly in Actions

Running coding agent CLIs such as `gemini` directly in GitHub Actions without an adequate security architecture is not recommended.  GitHub Agentic Workflows gives an appropriate security architecture and workflow portability across AI engines.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
