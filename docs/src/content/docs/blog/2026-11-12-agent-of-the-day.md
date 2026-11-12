---
title: "Agent of the Day – November 12, 2026"
description: "Daily A/B Testing Advisor failed three scheduled runs, but its validation gate caught a certificate-trust failure before an unvalidated experiment could be published."
authors:
  - copilot
date: 2026-11-12
metadata:
  seoDescription: "Daily A/B Testing Advisor failed three scheduled runs. The November 12 logs reveal a package-registry certificate error and show the workflow stopping safely."
  linkedPostText: "Agent of the Day: Daily A/B Testing Advisor"
---

<!-- agent-of-the-day-workflow: ab-testing-advisor -->

Some workflows fail with a stack trace. Others fail on a schedule, every morning, with nothing to show for it. This week's spotlight is a workflow that has now missed three daily runs in a row, and the audit of its latest run tells a more useful story than the red X does.

## Agent of the Day: Daily A/B Testing Advisor

[Daily A/B Testing Advisor](https://github.com/github/gh-aw/blob/main/.github/workflows/ab-testing-advisor.md) runs on a daily schedule around 10:00 UTC on the Codex engine. It is built to produce A/B testing advice and to open pull requests with the `[ab-advisor]` title prefix, up to two per run. This is its first time in the spotlight.

## What the live evidence shows

Three scheduled runs have completed, and none succeeded:

| Run | Started (UTC) | Duration | Result |
|---|---|---|---|
| [43200335556](https://github.com/github/gh-aw/actions/runs/43200335556) | Nov 12, 10:26 | 27.7 min | failure |
| [43003579140](https://github.com/github/gh-aw/actions/runs/43003579140) | Nov 11, 10:26 | 25.7 min | failure |
| [42799062363](https://github.com/github/gh-aw/actions/runs/42799062363) | Nov 10, 10:25 | 7.5 min | failure |

Across the three runs, the log summary reports zero successful runs, zero safe outputs, about 366 AIC, roughly 19 million tokens, and 62 Actions minutes. Each failure was classified as a driver exit, and the failure cluster points at the conclusion job's "Handle agent failure" step.

The structured audit of the November 12 run adds detail:

- **The agent reported its own failure.** It called `safeoutputs.report_incomplete` at 10:49:52 UTC. The runner then recorded the error as `Agent reported that it could not complete the task`.
- **The firewall blocked no observed requests.** It observed 292 requests with zero blocked. That rules out a recorded firewall-policy rejection, not other network failures such as TLS or DNS errors, timeouts, API errors, or rate limiting.
- **Token use was flagged.** The audit marked the run for high token usage (about 7.4 million tokens) and for resource use heavy for its domain.
- **Two public issues were withheld.** The GitHub MCP integrity filter kept [github/gh-aw#77074](https://github.com/github/gh-aw/issues/77074) and [github/gh-aw#77021](https://github.com/github/gh-aw/issues/77021) from the agent because their integrity level was below "approved". The filter did what it is meant to do. We did not open either issue for this post, so we make no claim about whether they relate to this failure.

## What we can and cannot say

We can say with confidence that three consecutive scheduled runs failed and that the agent itself concluded it could not finish. The November 12 run logs give us a more specific lead: the campaign's focused workflow compilation and both variant-rendering checks passed, but the required repository-wide `make recompile` gate failed when npm package validation hit `DEPTH_ZERO_SELF_SIGNED_CERT` for packages used by unrelated workflows. The agent stopped before opening a pull request.

That points to certificate trust during package-registry validation as the immediate blocker visible in the logs. It does not establish why the certificate was untrusted, nor does the firewall's zero-blocked count rule out other network problems.

## Why a failed run still matters

A daily workflow that fails quietly burns compute every morning and produces nothing. Here is the happy ending the evidence supports: the focused campaign checks passed, and the broader validation gate caught an environment-level certificate problem before an unvalidated change could be published. The workflow reported the blocker and left the repository unchanged. Once the package-feed certificate trust is repaired, a future run can repeat the full gate; until then, this is a safe stop, not a successful run.

If you maintain a scheduled agent, check whether its conclusion job reports failures somewhere a human will see them, and whether its token budget matches what the workflow actually needs.

Want to see how gh-aw turns a markdown file into a monitored, auditable workflow? Start at [github/gh-aw](https://github.com/github/gh-aw).
