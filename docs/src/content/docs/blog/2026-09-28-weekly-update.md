---
title: "Weekly Update – September 28, 2026"
description: "v0.89.22 removes deprecated sandbox runtimes, tightens firewall enforcement, and adds grouped audit findings."
authors:
  - copilot
date: 2026-09-28
metadata:
  seoDescription: "gh-aw v0.89.22 removes legacy sandboxes, ships hosted-web network policies, grouped audit findings, and CI shard rebalancing."
---

It was a big week for [github/gh-aw](https://github.com/github/gh-aw): three releases shipped, a legacy sandbox runtime was retired, and the firewall got noticeably stricter about what agents can reach. Here's the rundown.

## Release: v0.89.22

[v0.89.22](https://github.com/github/gh-aw/releases/tag/v0.89.22) landed on September 27th as the week's headline release, building on [v0.89.21](https://github.com/github/gh-aw/releases/tag/v0.89.21) and [v0.89.20](https://github.com/github/gh-aw/releases/tag/v0.89.20) from earlier in the week.

### ⚠️ Breaking Change

- **Docker sbx and gVisor sandbox runtimes removed** ([#63034](https://github.com/github/gh-aw/pull/63034)) — isolated agent execution now runs exclusively on Cloud Hypervisor. If your workflow frontmatter still sets `sandbox.agent.runtime: docker-sbx` or `gvisor`, switch it to `cloud-hypervisor` before you next recompile.

### ✨ What's New

- **Hosted-web domain policies in frontmatter** ([#63212](https://github.com/github/gh-aw/pull/63212)) — the new `network.hosted-web` key lets you allow or block domains that provider-hosted Claude and Codex web tools reach outside AWF's network boundary.
- **Firewall enforcement got sharper** — Copilot and web tools now get enforced firewall compatibility checks ([#63474](https://github.com/github/gh-aw/pull/63474)), scoped precisely to the web tools a workflow actually enables ([#63632](https://github.com/github/gh-aw/pull/63632)), while false blocked-domain warnings for otherwise-successful requests were squashed ([#63246](https://github.com/github/gh-aw/pull/63246)).
- **Grouped audit findings mode** ([#63032](https://github.com/github/gh-aw/pull/63032)) — `gh aw audit --group` now aggregates findings by run and code with occurrence counts, in pretty, markdown, or JSON output.
- **More audit visibility** — an opt-out for automatic audit baseline downloads ([#63012](https://github.com/github/gh-aw/pull/63012)) and MCP payload size reporting in audit ([#63687](https://github.com/github/gh-aw/pull/63687)) give workflow authors finer control over what audit runs measure and fetch.
- **Large Copilot prompts streamed through stdin** ([#62766](https://github.com/github/gh-aw/pull/62766), thanks @davidslater!) — prompts over 100 KiB are now delivered via stdin instead of being truncated, preserving full context for big agentic workflows.
- **Repo-memory backend for daily AIC guardrail** ([#62958](https://github.com/github/gh-aw/pull/62958)) — memory persistence now supports repository-backed storage for cost accounting.

### 🐛 Notable Fixes

- Fixed dangling `safe-outputs-app-token` references when safe outputs are staged ([#63490](https://github.com/github/gh-aw/pull/63490)).
- Fixed repo memory retry head refresh authentication ([#63497](https://github.com/github/gh-aw/pull/63497)) and prevented cache-memory validation marker `EACCES` failures ([#63498](https://github.com/github/gh-aw/pull/63498)).
- Pinned `pull_request` activation checkout to the base SHA for improved supply-chain safety ([#63499](https://github.com/github/gh-aw/pull/63499)).
- Fixed slash-command PR prefetch caching and error handling ([#63678](https://github.com/github/gh-aw/pull/63678)).

## Notable Pull Requests

- [Add friction-cost attribution to audit and usage artifacts](https://github.com/github/gh-aw/pull/63662) — audit output now attributes friction costs directly, making it easier to spot where workflows are burning extra cycles.
- [Track skill usage in audit artifacts](https://github.com/github/gh-aw/pull/63688) — you can now see which skills workflows actually invoke, straight from the audit trail.
- [Surface AWF steering counters in compact audit usage data](https://github.com/github/gh-aw/pull/63664) — steering activity is now visible in the compact usage summary, not just the full report.

## 🤖 Agent of the Week: ci-coach

Meet **ci-coach**, the [CI Optimization Coach](https://github.com/github/gh-aw/blob/main/.github/workflows/ci-coach.md) that runs daily to hunt down slow, imbalanced, or wasteful spots in gh-aw's CI pipeline and proposes fixes as pull requests.

This week ci-coach had its hands full with `cgo.yml`'s notoriously lopsided unit-test shard. On September 24th it noticed the alphabetic `A-C` shard was taking ~106 seconds — more than double the other four shards' 40-48 seconds — and traced nearly 20% of that gap to a single test full of pointless `time.Sleep`-backed cooldowns. It opened a fix ([#63187](https://github.com/github/gh-aw/pull/63187)) that reconfigured the mocked rate limit so the test skips its legacy 500ms sleep loop entirely. The very next day, still not satisfied, it went after the *same* shard again — this time proposing a full matrix rebalance that carves out a dedicated "Linters" shard, projected to cut the `A-C` shard's time from ~93s down to ~29s.

Two days, two PRs, one stubbornly slow test shard — ci-coach is basically the CI equivalent of someone who keeps rearranging the furniture until the room finally feels right.

💡 **Usage tip**: Run this class of workflow on a schedule against your CI config files — it's great at spotting shard imbalances and dead-weight sleeps that are easy to miss by eye but add up fast across hundreds of runs.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/ci-coach.md)

## Try It Out

Update to [v0.89.22](https://github.com/github/gh-aw/releases/tag/v0.89.22) today, double-check your sandbox runtime settings if you were on `docker-sbx` or `gvisor`, and give the new `gh aw audit --group` mode a spin. As always, questions and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
