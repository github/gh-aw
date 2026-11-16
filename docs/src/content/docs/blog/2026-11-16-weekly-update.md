---
title: "Weekly Update – November 16, 2026"
description: "v1.2.2 and v1.2.3 bring cost-aware model routing, declarative connectors, and obsolete-work cancellation, plus a round of security hardening."
authors:
  - copilot
date: 2026-11-16
metadata:
  seoDescription: "gh-aw v1.2.2 and v1.2.3: cost-aware model routing, declarative connectors, cancel-work --obsolete, security hardening, and Agent of the Week: Sub-Issue Closer."
---

Two releases shipped in [github/gh-aw](https://github.com/github/gh-aw) this week, and a steady stream of pull requests landed alongside them. The theme is cost and control: model routing now favors cheaper models for eligible workflows, and several breaking changes need a look before you recompile.

## Release: v1.2.3

[v1.2.3](https://github.com/github/gh-aw/releases/tag/v1.2.3) (November 13, marked pre-release) centers on model routing, safer connectors and merges, and a Go 1.27.4 baseline. Review these breaking changes before you upgrade:

- **Default model routing changed.** Eligible workflows that set neither a model nor routing now use `goal: cost`, `mode: auto` on GitHub Copilot. To keep the previous selection, set `engine.model-routing: false`. See the [model routing reference](https://github.github.com/gh-aw/reference/model-routing/).
- **Declarative connectors replace native Jira, Linear, and Azure DevOps integrations.** Import `.github/workflows/connectors/<provider>.md`, choose read profiles and write effects, then recompile. See the [connectors reference](https://github.github.com/gh-aw/reference/connectors/).
- **`merge-pull-request` blocks PRs that change protected files.** Merge those manually, add `protected-files.exclude` entries, or set `protected-files: allowed`.
- **`pull_request_target` admits same-repository PRs only by default.** List exact `owner/repo` entries under `forks` to admit forks.
- **AWF and Copilot CLI downloads need a checksum pin.** Unpinned and `latest` downloads now fail closed.

New in this release, you can set `model: router`, `router/economy`, `router/balanced`, or `router/robust` directly, and `create-security-advisory` creates draft advisories that are never published automatically.

## Release: v1.2.2

[v1.2.2](https://github.com/github/gh-aw/releases/tag/v1.2.2) (November 10) focuses on work-queue handling, firewall egress, and session normalization.

- **Cancel obsolete work.** `cancel-work --obsolete` adds a read-only dry-run preview ([#76180](https://github.com/github/gh-aw/pull/76180)), and queue maintenance now cancels obsolete worker backlog ([#76126](https://github.com/github/gh-aw/pull/76126)).
- **Cheaper defaults.** PR Sous Chef and other workflows now use economy model routing instead of pinned models ([#76325](https://github.com/github/gh-aw/pull/76325), [#75471](https://github.com/github/gh-aw/pull/75471)).
- **Fewer wasted runs.** Skip-if-decision gates now cover AWF engine invocations ([#76008](https://github.com/github/gh-aw/pull/76008)) and ten more Copilot workflows ([#76099](https://github.com/github/gh-aw/pull/76099)).
- **Name-based artifact upserts** in `update-issue` ([#75988](https://github.com/github/gh-aw/pull/75988)).
- **Updated dependencies.** MCP Gateway v0.5.6 and firewall v0.29.11 ([#76311](https://github.com/github/gh-aw/pull/76311)). Terminal safe-output signals are now required ([#76194](https://github.com/github/gh-aw/pull/76194)).

## Notable Pull Requests

- [#78137](https://github.com/github/gh-aw/pull/78137): **Exponential backoff for threat-detect downloads.** Release-host 504s used to exhaust six fixed-delay retries in about 50 seconds, which stopped the detector from running at all.
- [#78015](https://github.com/github/gh-aw/pull/78015): **Zip option injection fixed.** A staged filename starting with a dash, such as `-T`, could be read as a `zip` option. Those names are now rejected.
- [#77989](https://github.com/github/gh-aw/pull/77989): **Firewall v0.29.14 and MCP gateway v0.5.10** become the default pins.
- [#77561](https://github.com/github/gh-aw/pull/77561): **Opt-in trigger reaction removal.** Set `on.reaction.remove-on-complete` to clear trigger reactions when a run finishes. It defaults to `false`.
- [#77866](https://github.com/github/gh-aw/pull/77866): **Workflow source in `gh aw list --json`.** Tooling can now tell which workflows are managed from an upstream bundle.

## 🤖 Agent of the Week: Sub-Issue Closer

Sub-Issue Closer is the scheduled workflow that closes a parent issue once all of its sub-issues are complete. It runs daily on the `pi` engine with `copilot/gpt-5`.

Over its last three scheduled runs ([November 13](https://github.com/github/gh-aw/actions/runs/43378428738), [November 14](https://github.com/github/gh-aw/actions/runs/43565944172), and [November 15](https://github.com/github/gh-aw/actions/runs/43685146428)), it closed 30 parent issues and commented on each one. All of them were smoke-test tracking issues, such as "Smoke Copilot ARM64 - Issue Group." Each run took 7 to 8.5 minutes, and all three succeeded.

Its closing comments are a handy audit trail, so if you rely on it, skim them now and then to confirm each parent really was finished.

💡 **Usage tip**: Use a closer like this for work you track as sub-issues, and review the comments it leaves so you can trust what it closed.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/sub-issue-closer.md)

## Try It Out

Review the breaking changes in [v1.2.3](https://github.com/github/gh-aw/releases/tag/v1.2.3), recompile your workflows, and tell us what you think. Feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
