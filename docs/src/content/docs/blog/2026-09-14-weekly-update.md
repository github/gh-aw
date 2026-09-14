---
title: "Weekly Update – September 14, 2026"
description: "v0.89.0 through v0.89.12 shipped a wave of releases hardening logs observability, model routing, and CI security across gh-aw."
authors:
  - copilot
date: 2026-09-14
metadata:
  seoDescription: "gh-aw weekly update: rapid v0.89.x releases, MCP tool call tracing in logs, credential-persistence hardening, and Agent of the Week: cli-version-checker."
---

It was a fast-moving week for [github/gh-aw](https://github.com/github/gh-aw)! The team shipped seventeen releases — from [v0.88.5](https://github.com/github/gh-aw/releases/tag/v0.88.5) all the way to [v0.89.12](https://github.com/github/gh-aw/releases/tag/v0.89.12) — packed with `gh aw logs` observability upgrades, model-routing fixes, and a steady drumbeat of CI and credential-security hardening.

## Release: v0.89.0

[v0.89.0](https://github.com/github/gh-aw/releases/tag/v0.89.0) was the headline release of the week, focused on hardening agentic engine model selection, improving `gh aw logs` observability, and tightening safe-output guardrails.

### What's New

- **Identifiable MCP tool calls in logs** ([#59579](https://github.com/github/gh-aw/pull/59579)): `gh aw logs --json` now records the timestamp, server name, and tool name for every MCP call, making it far easier to trace which server and tool produced a given usage entry.
- **`--ignore-workflow-runs` for `gh aw logs`** ([#59697](https://github.com/github/gh-aw/pull/59697)): exclude specific runs (by numeric ID or `slug/ID`) from log collection without shrinking your requested result count.
- **Refreshed cached logs JSON** ([#59690](https://github.com/github/gh-aw/pull/59690)): `gh aw logs --cached-json` now replaces the cache file with up-to-date results after each successful collection instead of leaving it stale.
- **GPT-6 Astra model support** ([#59711](https://github.com/github/gh-aw/pull/59711)): `gpt-6-astra` is now recognized in model alias resolution and pricing catalogs for GitHub Copilot and OpenAI.

### Bug Fixes & Improvements

- Fixed threat detection reporting `config_error` for workflows using custom engines ([#59636](https://github.com/github/gh-aw/pull/59636)), so custom-engine workflows now get proper threat analysis instead of silently skipping it.
- Fixed a Copilot SDK model inventory collection break caused by an incompatible CLI platform package after a dependency bump ([#59703](https://github.com/github/gh-aw/pull/59703)).
- Bundled MCP gateway upgraded to v0.4.20 ([#59602](https://github.com/github/gh-aw/pull/59602)), including a safe-outputs sink-visibility exemption fix.

## Release: v0.89.12

The week closed out with a small but important security release, [v0.89.12](https://github.com/github/gh-aw/releases/tag/v0.89.12):

- **Reduced credential blast radius in the slash-command router** ([#60685](https://github.com/github/gh-aw/pull/60685)): the generated central slash-command router workflow now checks out the repository with `persist-credentials: false`, so `GITHUB_TOKEN` is no longer persisted in local git config for the lifetime of the routing job.
- **Fixed the "Integration: CMD Tests" CI job** ([#60683](https://github.com/github/gh-aw/pull/60683)), restoring a green CI signal.

## Notable Pull Requests

Between the two headline releases, dozens of PRs kept the fleet humming:

- **[Support wildcard cached logs files](https://github.com/github/gh-aw/pull/60702)**: makes `gh aw logs` caching more flexible when matching multiple log targets.
- **[Preserve JSONL rows during repo-memory merge conflicts](https://github.com/github/gh-aw/pull/60663)**: hardens the repo-memory sync path so concurrent workflow writes don't clobber each other's history.
- **[Add package-aware targets to `gh aw update`](https://github.com/github/gh-aw/pull/60452)**: lets `gh aw update` understand `aw.json`-based packages when refreshing workflows.
- **[Disable persisted credentials in auto-upgrade checkout](https://github.com/github/gh-aw/pull/60650)** and **[Disable credential persistence on the agentic_commands router checkout](https://github.com/github/gh-aw/pull/60685)**: two more steps in a week-long push to shrink `GITHUB_TOKEN` exposure across generated automation checkouts.
- **[Pin GitHub Actions to commit SHAs](https://github.com/github/gh-aw/pull/60061)**: supply-chain hardening for the Actions used across the fleet, closing off tag-mutation risk.

## 🤖 Agent of the Week: CLI Version Checker

Meet [`cli-version-checker`](https://github.com/github/gh-aw/blob/main/.github/workflows/cli-version-checker.md) — the fleet's diligent version scout, running daily to watch for new releases of Claude Code, GitHub Copilot CLI, OpenAI Codex, the GitHub MCP Server, Playwright CLI, MCP Gateway, Pi, threat-detect, and a stack of container-scanning tools like `actionlint`, `syft`, `grype`, and `zizmor`.

Over its last three scheduled runs this agent had a genuinely mixed week: one clean 6.6-minute pass that filed its usual "[ca]"-prefixed update issue, one run that failed after just 49 seconds, and one earlier run that took 5.6 minutes before also hitting trouble. All told it burned through roughly 40K tokens and made 26 GitHub API calls chasing down version numbers across nine different tools and eight container images — a lot of bookkeeping for one little agent.

Its self-imposed 2-day issue expiry is a nice touch: if nobody acts on a version bump quickly, the checker doesn't let stale "you should upgrade" nags pile up in the issue tracker forever.

💡 **Usage tip**: For any "check external state and file an issue" workflow, pair a short `expires` window on the safe-output with a `cookie` label — it keeps the backlog honest and makes triage-by-label trivial.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/cli-version-checker.md)

## Try It Out

Check out the [latest releases](https://github.com/github/gh-aw/releases) of `gh-aw` and give the new `gh aw logs` observability features a spin. As always, feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
