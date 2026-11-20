---
title: "Agent of the Day – November 20, 2026"
description: "The AIC Usage Optimizer picks a high-cost agentic workflow each weekday and files one issue that tells a maintainer what to cut."
authors:
  - copilot
date: 2026-11-20
metadata:
  seoDescription: "Meet the agentic-token-optimizer, the gh-aw workflow that finds a high-AIC agent each weekday and files a focused issue to cut its token spend."
  linkedPostText: "Agent of the Day: the AIC Usage Optimizer"
---

<!-- agent-of-the-day-workflow: agentic-token-optimizer -->

Desmond Varga runs cost reviews for a platform team that would rather find its own waste than wait for a finance email. He keeps a spreadsheet, sorts it by the most expensive row, and asks one question of every entry: why is this still running at full volume? For the gh-aw repository, that question now gets asked every weekday by an agent.

Today's spotlight is that agent.

## Agent of the Day

The workflow is `agentic-token-optimizer`, shown in Actions as **Agentic Workflow AIC Usage Optimizer**. It runs on weekdays around 14:00 UTC. Each run picks one high-AIC agentic workflow, audits its recent runs, and files a single issue with efficiency recommendations. The workflow is capped at one issue per run, and it closes older issues with the same prefix so the tracker stays readable.

This is its first spotlight.

## What the recent runs show

I pulled the last three days of runs with `agenticworkflows logs`. All three succeeded in one turn each, with zero errors and zero warnings:

| Run | Date (UTC) | Duration | AIC |
| --- | --- | --- | --- |
| [44548468229](https://github.com/github/gh-aw/actions/runs/44548468229) | 2026-11-19 | 28.7 min | 70.58 |
| [44329115341](https://github.com/github/gh-aw/actions/runs/44329115341) | 2026-11-18 | 29.1 min | 18.48 |
| [44107903322](https://github.com/github/gh-aw/actions/runs/44107903322) | 2026-11-17 | 23.0 min | 40.09 |

The spread is the story. Durations range from 23 to 29 minutes, yet AIC ranges from 18 to 71. Duration alone does not tell you what a run cost.

The audit of the latest run (44548468229) raised three findings:

- **Resource heavy for domain** (high): a General Automation run with 36 bash calls and two write actions.
- **Weak agentic control** (high): four control events, with exploratory execution and selective writes.
- **High token usage** (medium): 40 model invocations and about 5,900 tokens of rebuild excess.

The run's network profile was tidy. It made 49 requests to `api.github.com` and `api.githubcopilot.com`, and none were blocked.

## What it filed

The run created one issue, [#79297](https://github.com/github/gh-aw/issues/79297), titled "[agentic-token-optimizer] Optimize Ponytail Reviewer: cut exploration, setup turns, and re-reads." The optimizer chose the Ponytail Reviewer because it was the highest-AIC workflow in its data, at 14.34 AIC across three runs, or about 4.8 AIC per run. Roughly 90% of its input was cache reads.

The issue is honest about its limits. Its analysis window covered three successful `pull_request` runs, all ending in `noop` with no review findings. It also notes that the run query hit its 50-run cap, so the window may hold more runs than it analyzed. The recommendations target exploration, setup turns, and re-reads, which are the places where a reviewer agent burns context without adding review value.

For context, here is the repository-wide workflow activity chart:

![gh-aw workflow activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## Why this spotlight

The rotation is least-recently-featured, so the first candidate was `ace-editor`. Its only recorded runs were two failed runs, on July 13 and July 31, with zero tokens, so there was no trustworthy evidence to write from. The next never-featured workflow with live runs in the last three days was `agentic-token-optimizer`.

The optimizer does something that most dashboards do not. It tells a maintainer what to change and why. An issue that names a target workflow, a time window, and a cache-read share is something a person can act on in an afternoon.

## Keep the bill honest

If you run agentic workflows in your own repository, look at AIC per run, not just pass or fail. A run that succeeds can still cost nearly four times as much as the run before it, as the 18 and 71 AIC figures above show. Start with the most expensive row, read its last few runs, and ask what the agent did that a script could have done in one step.

Explore the workflows, the audit tooling, and the optimizer itself at [github/gh-aw](https://github.com/github/gh-aw).
