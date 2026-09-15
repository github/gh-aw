---
title: "Weekly Update – May 11, 2026"
description: "Four releases in one week: gh aw lint, inline sub-agents default-on, a new forecast command, and Claude /tmp access — plus the story of our tireless Auto-Triage Issues agent."
authors:
  - copilot
date: 2026-05-11
---

> [!NOTE]

It was a busy week in [github/gh-aw](https://github.com/github/gh-aw)! Four releases landed between May 4 and May 7, paired with a wave of pull requests that delivered new commands, security hardening, and developer-experience polish. Here's everything that shipped.

## Releases This Week

### [v0.72.1](https://github.com/github/gh-aw/releases/tag/v0.72.1) — May 7

The headline feature is a new `gh aw lint` command that runs [actionlint](https://github.com/rhysd/actionlint) directly against your existing `.lock.yml` files — no recompile required. It's a lightweight CI gate you can drop into any pipeline to catch syntax errors early. Pass `--shellcheck` or `--pyflakes` for deeper script analysis, or point it at specific files with `--dir`.

Other highlights:

- **Shared workflow `engine.mcp.tool-timeout` inheritance** ([#30634](https://github.com/github/gh-aw/issues/30634)): Shared workflows that wrap slow MCP servers can now declare timeout values once and have consumers inherit them automatically — no more duplicating `engine.mcp.tool-timeout` in every downstream workflow.
- **First-party coding-agent skill** ([#27259](https://github.com/github/gh-aw/issues/27259)): Copilot, Claude, and other coding agents now get structured guidance on creating, debugging, and updating agentic workflows via a router skill shipped with `gh aw`.
- **`&&` preserved in compiled expressions** ([#30695](https://github.com/github/gh-aw/issues/30695)): A sneaky Go HTML-escaping bug was silently turning `&&` into `\u0026\u0026` inside `.lock.yml` files, corrupting `${{ ... && ... }}` expressions. Fixed.

### [v0.72.0](https://github.com/github/gh-aw/releases/tag/v0.72.0) — May 6

Inline sub-agents are now **default-on** — the `features.inline-agents: true` flag is deprecated. Run `gh aw fix --write` to auto-remove it from existing workflows via the new `features-inline-agents-removal` codemod.

This release also fixed a community-reported `push_to_pull_request_branch` rerun failure: when an agent reran and its patch reintroduced a file already on the branch, `git am --3way` produced an unresolvable add/add conflict. The fix detects add/add-only conflicts and resolves them by taking the patch side automatically.

### [v0.71.6](https://github.com/github/gh-aw/releases/tag/v0.71.6) and [v0.71.5](https://github.com/github/gh-aw/releases/tag/v0.71.5) — May 5–6

These patch releases addressed Claude engine stability (no more mid-session crashes from "Fast mode unavailable"), fixed multi-line `engine.env` block-scalar values that compiled to broken YAML, added gateway RPC message rendering in step summaries, and switched inline sub-agent blocks to the `small` model alias by default to reduce cost and latency.

## Notable Pull Requests

Beyond the releases, several PRs merged this week are worth highlighting:

- **[`gh aw forecast` command (experimental)](https://github.com/github/gh-aw/pull/31377)** — A new command for projecting workflow AI Credit usage before you run it. Useful for budgeting and capacity planning.
- **[Grant Claude default `/tmp` read/write in sandboxed workflows](https://github.com/github/gh-aw/pull/31357)** — Claude-engine workflows can now read and write to `/tmp` by default in sandboxed environments, eliminating a common pain point when agents need temporary scratch space.
- **[Rename `rate-limit` → `user-rate-limit` and `max-runs` → `max-runs-per-window`](https://github.com/github/gh-aw/pull/31390)** — Clearer naming for rate-limiting configuration fields.
- **[OTel `gen_ai.response.finish_reasons`](https://github.com/github/gh-aw/pull/31332)** — Agent spans now emit finish reasons (e.g., `stop`, `length`, `tool_calls`) as an OpenTelemetry attribute, improving observability dashboards.
- **[Synthetic OTel exception events for silent failures](https://github.com/github/gh-aw/pull/31334)** — When a workflow fails but the agent produces no readable output, a synthetic exception event is now emitted so traces still surface the failure.

## 🤖 Agent of the Week: auto-triage-issues

The unsung inbox manager of the repository — reads every new issue the moment it's opened and figures out where it belongs.

This week `auto-triage-issues` ran three times in quick succession (May 9–10), successfully triaging two issues and stumbling on a third that triggered a failure — a small battle scar it wore with dignity. In its successful runs it stayed impressively lean: nine API requests, ~270 K input tokens pulled from cache, and a turnaround of under 40 seconds per issue. It never wastes a compute cycle it doesn't have to.

The run summary noted with mild concern that `auto-triage-issues` is so reliable and narrow in its tool usage that it might be "overkill for agentic" — meaning deterministic automation could theoretically do its job. The workflow appears to have taken this note personally and immediately triaged the next issue without comment.

💡 **Usage tip**: Pair `auto-triage-issues` with a `notify` or `discussion` workflow on high-priority labels so the right people are paged the moment a critical bug or security issue lands.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/auto-triage-issues.md)

## Try It Out

Update to [v0.72.1](https://github.com/github/gh-aw/releases/tag/v0.72.1) today — `gh extension upgrade gh-aw` — and try the new `gh aw lint` and experimental `gh aw forecast` commands. As always, feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
