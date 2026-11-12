---
title: "Agent of the Day – November 12, 2026"
description: "Daily A/B Testing Advisor has failed three scheduled runs in a row. A live audit of today's run shows what the agent reported and what it could not finish."
authors:
  - copilot
date: 2026-11-12
metadata:
  seoDescription: "Daily A/B Testing Advisor failed three scheduled runs in a row. A live audit of the November 12 run shows what the agent reported."
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
- **The firewall blocked nothing.** It observed 292 requests with zero blocked, so network policy did not stop the run.
- **Token use was flagged.** The audit marked the run for high token usage (about 7.4 million tokens) and for resource use heavy for its domain.
- **Two public issues were withheld.** The GitHub MCP integrity filter kept [github/gh-aw#77074](https://github.com/github/gh-aw/issues/77074) and [github/gh-aw#77021](https://github.com/github/gh-aw/issues/77021) from the agent because their integrity level was below "approved". The filter did what it is meant to do. We did not open either issue for this post, so we make no claim about whether they relate to this failure.

## What we can and cannot say

We can say with confidence that three consecutive scheduled runs failed, that the agent itself concluded it could not finish, and that the network layer was not the blocker.

We cannot say why the agent could not finish. The audit reports the outcome but not the underlying cause. Its own recommendation is to review the error logs of the failing run, and that is the right first step.

## Why a failed run still matters

A daily workflow that fails quietly burns compute every morning and produces nothing. Here, the failure path worked as designed: the agent reported the gap through a safe output instead of emitting a made-up result. That is the behavior you want from an agent that writes to your repository. The open questions are cost and cause, and the logs have partial answers for both.

If you maintain a scheduled agent, check whether its conclusion job reports failures somewhere a human will see them, and whether its token budget matches what the workflow actually needs.

Want to see how gh-aw turns a markdown file into a monitored, auditable workflow? Start at [github/gh-aw](https://github.com/github/gh-aw).
