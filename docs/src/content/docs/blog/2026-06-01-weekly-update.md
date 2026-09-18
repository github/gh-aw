---
title: "Weekly Update – June 1, 2026"
description: "v0.77.4 delivers Anthropic WIF auth, a new copilot-sdk engine, per-workflow token guardrails, and much more."
authors:
  - copilot
date: 2026-06-01
---

> [!NOTE]

It's been a busy week in [github/gh-aw](https://github.com/github/gh-aw)! Five releases landed between May 28 and May 31, capped off by [v0.77.4](https://github.com/github/gh-aw/releases/tag/v0.77.4) — one of the biggest releases in recent memory. Here's everything that shipped.

## Release: v0.77.4

[v0.77.4](https://github.com/github/gh-aw/releases/tag/v0.77.4) published on May 31st and packs in a ton of new capability.

### ✨ What's New

- **Anthropic WIF Authentication** ([#35939](https://github.com/github/gh-aw/pull/35939)): Claude-engine workflows can now authenticate via Workload Identity Federation. No more long-lived API key secrets stored in your repo — WIF handles it securely.

- **`copilot-sdk` Engine** ([#35936](https://github.com/github/gh-aw/pull/35936)): A new `engine: copilot-sdk` frontmatter option gives workflows direct access to the Copilot SDK runtime, opening up new integration patterns.

- **`aw.yml` Manifest: Includes, Skills & Agents** ([#35778](https://github.com/github/gh-aw/pull/35778)): Your repository manifest now supports `includes`, `skills`, and `agents` keys so you can compose and share workflow components across repos.

- **Per-Workflow 24-Hour AI-Credits Guardrail** ([#36042](https://github.com/github/gh-aw/pull/36042)): A configurable token guardrail prevents runaway agent costs with enterprise-grade defaults and handy `AIC` shorthand support.

- **`search_commits` in GitHub MCP Search Toolset** ([#36115](https://github.com/github/gh-aw/pull/36115)): Agents can now search commits directly via the GitHub MCP search toolset.

- **New Skills: `copilot-review` and `go-codemod`** ([#36111](https://github.com/github/gh-aw/pull/36111), [#36034](https://github.com/github/gh-aw/pull/36034)): Two new skills help agents plan and address PR review feedback, and implement Go codemods for the `gh aw fix` command.

### 🐛 Notable Fixes

- **Prefer toolcache Copilot CLI** ([#35992](https://github.com/github/gh-aw/pull/35992)): Workflows now use the Actions toolcache copy of the Copilot CLI before downloading a release — faster setup for everyone.
- **Reusable workflow timeout** ([#36107](https://github.com/github/gh-aw/pull/36107)): `timeout-minutes` is now correctly passed through reusable workflow callers.
- **Threat-detection hardening** ([#36113](https://github.com/github/gh-aw/pull/36113)): Missing prompt artifacts no longer block safe-output execution.
- **`on.needs` YAML strip** ([#35965](https://github.com/github/gh-aw/pull/35965)): Processed `on.needs` keys are stripped from emitted YAML, preventing invalid workflow syntax.

## Release: v0.77.3

[v0.77.3](https://github.com/github/gh-aw/releases/tag/v0.77.3) on May 29th brought sandbox improvements and better initialization:

- **`authHeader` in sandbox agent targets** ([#35694](https://github.com/github/gh-aw/pull/35694)): You can now specify custom authentication headers directly in `sandbox.agent.targets` frontmatter.
- **`gh aw init` creates the Agentic Workflows custom agent** ([#35773](https://github.com/github/gh-aw/pull/35773)): Running `gh aw init` now scaffolds a GitHub Copilot custom agent for Agentic Workflows right out of the box.
- **Stricter schema validation for `workflow_call`/`workflow_dispatch`** ([#35788](https://github.com/github/gh-aw/pull/35788)): Unknown input keys are now rejected at compile time.

## Notable Merged PRs This Week

- **[Add project UTC offset support for rendered timestamps](https://github.com/github/gh-aw/pull/36142)** — Timestamps and expiration messages now render correctly for teams in non-UTC time zones.
- **[Optimize `api-consumption-report` with inline small-model sub-agents](https://github.com/github/gh-aw/pull/36137)** — The API consumption report workflow is now faster and more efficient thanks to inline sub-agents.
- **[Add structured diagnostics to the daily workflow AIC guardrail](https://github.com/github/gh-aw/pull/36164)** — The AI Credits guardrail now emits structured logs with a stable `[daily-workflow-aic]` prefix, making debugging much easier.
- **[Enable `close_discussion` safe output in Daily Regulatory workflow](https://github.com/github/gh-aw/pull/36155)** — The regulatory compliance workflow can now close discussions as part of its cycle.

## 🤖 Agent of the Week: api-consumption-report

The bean counter who never sleeps — tracks every GitHub API call your workflows make and publishes a detailed report so you know exactly where your rate-limit quota is going.

This week `api-consumption-report` analyzed 95 workflow runs across the repository (58 successes, 37 failures — it doesn't sugarcoat the numbers), tallied up 10,619 GitHub REST API calls in a single day, and generated a full trend chart showing that API usage spiked to ~80K calls on May 20th before settling back down. It also uploaded five charts as release assets — a trend line, a heatmap, a per-workflow breakdown, a "burners" donut chart, and a workflow-level trend — then published the whole package as a GitHub Discussion for everyone to browse.

Hilariously, in one of its recent runs it completed in under 2 minutes with zero token usage and exactly one GitHub API call. Turns out that was the run where the cache hadn't warmed yet — it took a look around, shrugged, and went home early.

💡 **Usage tip**: Schedule this workflow weekly to catch runaway API consumption before you hit rate limits — the per-workflow breakdown makes it easy to spot which agent is hogging the quota.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/api-consumption-report.md)

## Try It Out

Upgrade to [v0.77.4](https://github.com/github/gh-aw/releases/tag/v0.77.4) today and explore the new `copilot-sdk` engine and WIF authentication for Claude. As always, feedback and contributions are welcome at [github/gh-aw](https://github.com/github/gh-aw).
