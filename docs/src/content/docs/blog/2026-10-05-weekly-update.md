---
title: "Weekly Update – October 5, 2026"
description: "Work queues and built-in ledgers land in v0.90.3, alongside safer safe outputs and broader Copilot model support."
authors:
  - copilot
date: 2026-10-05
metadata:
  seoDescription: "gh-aw v0.90.0–v0.90.3: work queues, built-in ledgers, hardened safe outputs, Copilot model routing, and a redesigned docs site."
---

Three releases shipped in [github/gh-aw](https://github.com/github/gh-aw) this week, headlined by work queues and built-in ledgers in [v0.90.3](https://github.com/github/gh-aw/releases/tag/v0.90.3). Here's what's new.

## Release Highlights

### [v0.90.3](https://github.com/github/gh-aw/releases/tag/v0.90.3)

This release introduces **work queues and ledgers**, tightens safe-output guardrails, and broadens Copilot model support.

- **Work queue & ledgers**: built-in log, set, map, table, and counter ledgers, plus Git-backed `work-queue` operator commands. See the [ledger compaction docs](https://github.github.com/gh-aw/experimental/ledger-compaction/). Related PRs: [#65494](https://github.com/github/gh-aw/pull/65494) adds selectable issue-backed queue storage, and [#65443](https://github.com/github/gh-aw/pull/65443) dispatches queued work to trusted worker workflows.
- **Copilot engine**: AWF task-level model routing, `none` and `max` reasoning efforts, GPT-6.1 Sol support, Claude 5.5 and GPT-6 pricing, and a run-wide tool-call budget for Copilot SDK agents.
- **Safe outputs**: `add-labels` supports separate call and per-call label limits, and several hardening fixes landed. [#65612](https://github.com/github/gh-aw/pull/65612) adds attributed issue creation for custom safe-output jobs.
- **Docs**: a redesigned site with a new landing page, warm palette, and Mona Sans.

Heads-up on behavior changes: GitHub App tokens now require explicit scopes at compile time, Claude defaults to strict MCP configuration, and `add-comment` fails when the triggering target can't be resolved.

### [v0.90.1](https://github.com/github/gh-aw/releases/tag/v0.90.1)

Standalone safe-output-backed ledgers arrived ([#64354](https://github.com/github/gh-aw/pull/64354)), along with richer audit artifacts covering ledger transactions and threat-detection outcomes ([#64509](https://github.com/github/gh-aw/pull/64509), [#64506](https://github.com/github/gh-aw/pull/64506)), and compile-time self-hosted runner enforcement ([#64173](https://github.com/github/gh-aw/pull/64173)).

### [v0.90.0](https://github.com/github/gh-aw/releases/tag/v0.90.0)

Step summaries now render friction cost, agents may emit two `noop` calls by default ([#64031](https://github.com/github/gh-aw/pull/64031)), and five new trajectory graders were added.

## Other Notable Pull Requests

- [#65423](https://github.com/github/gh-aw/pull/65423) adds a TLA+ workflow security model and fail-closed credential cleanup.
- [#65564](https://github.com/github/gh-aw/pull/65564) emits incomplete outcomes with diagnostics when an agent produces no safe outputs.
- [#65650](https://github.com/github/gh-aw/pull/65650) makes PR Sous Chef poll faster and start agents only for actionable work.

## 🤖 Agent of the Week: daily-arxiv-researcher

A daily scout that reads the latest arXiv papers and looks for ideas that could improve GitHub Agentic Workflows.

Its recent runs have all succeeded in about five to six minutes. The October 2 and 3 runs used the Copilot CLI engine, and the October 4 run was the first on the Codex engine with web search and fetch tools, after a [recent change](https://github.com/github/gh-aw/pull/65383) moved it there. That run used roughly 84K tokens, versus 7K and 13K for the earlier runs.

The new engine is evidently more curious: it read about six times as many tokens as its predecessor on the same morning schedule.

💡 **Usage tip**: Pair a research workflow with a dedup cache, as this one does with `cache-memory`, so it never reports the same paper twice.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-arxiv-researcher.md)

## Try It Out

Update to [v0.90.3](https://github.com/github/gh-aw/releases/tag/v0.90.3) and give work queues a spin. Feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
