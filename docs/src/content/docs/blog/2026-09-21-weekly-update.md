---
title: "Weekly Update – September 21, 2026"
description: "v0.89.17 lands faster logs auditing, refreshed model catalog, and a batch of AIC accounting fixes."
authors:
  - copilot
date: 2026-09-21
metadata:
  seoDescription: "gh-aw v0.89.17 ships faster logs auditing, model catalog updates, and reliability fixes for AWF, safe-outputs, and AIC accounting."
---

Another busy week for [github/gh-aw](https://github.com/github/gh-aw)! We shipped a fresh release packed with reliability fixes, a smarter logs pipeline, and an updated model catalog — plus our usual dose of documentation polish.

## Release: v0.89.17

[v0.89.17](https://github.com/github/gh-aw/releases/tag/v0.89.17) landed on September 19th, focused on hardening the AIC accounting pipeline, AWF/firewall integration, and safe-outputs handling.

### What's New

- **Faster, smarter logs auditing** — cached workflow runs are no longer redownloaded during logs audits ([#61871](https://github.com/github/gh-aw/pull/61871)), multi-target logs queries are now distributed fairly across targets ([#61027](https://github.com/github/gh-aw/pull/61027)), and per-run download duration/size is now tracked in an end-of-run stats summary ([#60951](https://github.com/github/gh-aw/pull/60951)).
- **Updated model catalog** — added `gemini-3.8-flash` and `claude-fable-5.1` aliases and corrected pricing for `gpt-6-astra`/`gpt-5.6-sol` ([#61234](https://github.com/github/gh-aw/pull/61234)).
- **MCP Gateway and firewall bumped** — MCP Gateway updated to v0.4.25 ([#61661](https://github.com/github/gh-aw/pull/61661)) and `gh-aw-firewall` (AWF) updated to v0.28.20 ([#61527](https://github.com/github/gh-aw/pull/61527)) and v0.28.17 ([#60945](https://github.com/github/gh-aw/pull/60945)).
- **Better automatic grading** — native Copilot tool calls are now included in the automatic grader trace payload for more accurate evaluation ([#61426](https://github.com/github/gh-aw/pull/61426)).

### Bug Fixes & Improvements

- Fixed Code Scanning Fixer timeouts and tool denials ([#61605](https://github.com/github/gh-aw/pull/61605)).
- Fixed slash command activation failing on CRLF line endings ([#61602](https://github.com/github/gh-aw/pull/61602)).
- The `safeoutputs` CLI transport now fails loudly instead of silently failing open, surfacing real errors sooner ([#61427](https://github.com/github/gh-aw/pull/61427)).
- Imported engine config (including auth) is now preserved when a workflow sets a top-level `model` ([#61424](https://github.com/github/gh-aw/pull/61424)).
- Fixed several gaps in daily AIC (AI Credits) accounting, including legacy runs ([#61313](https://github.com/github/gh-aw/pull/61313)), pre-harness failures ([#61232](https://github.com/github/gh-aw/pull/61232)), and unassigned jobs ([#61222](https://github.com/github/gh-aw/pull/61222)).

## Notable Pull Requests

- [Avoid redownloading cached runs during logs audit](https://github.com/github/gh-aw/pull/61871) — a nice efficiency win that speeds up repeated `gh aw logs` invocations by skipping runs already cached locally.
- [Fix Copilot SDK multiword shell prefix matching and denial-guard hang](https://github.com/github/gh-aw/pull/61430) — resolves a subtle hang in the tool-denial guard that could stall workflows using multiword shell allowlist prefixes.
- [Rewrite `experiments.<name>` in engine.model to valid job-scoped expressions](https://github.com/github/gh-aw/pull/61599) — prevents invalid compiled workflow YAML when experiments reference model names.

## 🤖 Agent of the Week: deployment-incident-monitor

Meet [`deployment-incident-monitor`](https://github.com/github/gh-aw/blob/main/.github/workflows/deployment-incident-monitor.md), the on-call responder of the gh-aw fleet — it watches every `deployment_status` event and automatically files a deduplicated incident issue with root-cause analysis whenever something breaks.

This was its busiest week yet, firing **19 times** — more runs than any other workflow in the repo. Most of the time it stayed quiet (no news is good news for deployments), but on September 19th it caught a real one: the `Smoke Copilot - AOAI (Entra)` workflow started failing with a `400` error because Azure OpenAI required organization verification for reasoning summaries. The agent didn't just flag the failure — it traced it back to the exact commit, confirmed the change wasn't the culprit, and filed [issue #61892](https://github.com/github/gh-aw/issues/61892) with a full evidence trail linking the failing run and deployment.

Its incident history reads like a highlight reel of infra archaeology — Go toolchain mismatches, GitHub Pages queue timeouts, and cancelled `sync_actions` approvals — and thanks to `close-older-issues`, it keeps the incident tracker tidy instead of piling up duplicates.

💡 **Usage tip**: Pair `deployment_status`-triggered monitors like this one with `skip-if-match` on your incident label so repeated failures from the same root cause collapse into a single, evolving issue instead of flooding your tracker.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/deployment-incident-monitor.md)

## Try It Out

Update to [v0.89.17](https://github.com/github/gh-aw/releases/tag/v0.89.17) today, and check out the full changelog and prior releases on the [releases page](https://github.com/github/gh-aw/releases). As always, feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
