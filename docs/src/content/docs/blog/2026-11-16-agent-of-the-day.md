---
title: "Agent of the Day – November 16, 2026"
description: "Agent Job Health Monitor posts one discussion per scheduled run. Its last three runs all succeeded, yet they used very different amounts of work."
authors:
  - copilot
date: 2026-11-16
metadata:
  seoDescription: "Meet Agent Job Health Monitor, a daily gh-aw workflow that posts a discussion each run. Three live runs show how far the same job's cost can swing."
  linkedPostText: "Agent of the Day: Agent Job Health Monitor"
---

<!-- agent-of-the-day-workflow: agent-job-health -->

Some workflows exist to watch other workflows. Today's spotlight is one of them. It checks the health of agent jobs on a daily schedule and reports what it finds in a GitHub Discussion. Its recent runs all finished cleanly, which makes them a good place to look at a quieter question: how much work does a steady monitor really do?

## Agent of the Day: Agent Job Health Monitor

[Agent Job Health Monitor](https://github.com/github/gh-aw/blob/main/.github/workflows/agent-job-health.md) runs on a daily schedule. Each run produces one discussion, and every run in this window succeeded and created exactly one.

This is the first time this workflow has been featured. The rotation moves through active workflows from the least recently featured onward. The workflow ahead of it, `ace-editor`, had no runs in the last three days, so it could not be judged on live evidence and the rotation moved on to this one.

## What the last three runs show

| Run | Date (UTC) | Duration | Turns | Audit tokens | Discussion |
|---|---|---|---|---|---|
| [43525284919](https://github.com/github/gh-aw/actions/runs/43525284919) | Nov 13 | 21.6m | 60 | 3.25M | [#77583](https://github.com/github/gh-aw/discussions/77583) |
| [43651937051](https://github.com/github/gh-aw/actions/runs/43651937051) | Nov 14 | 26.5m | 3 | 547k | [#77876](https://github.com/github/gh-aw/discussions/77876) |
| [43773614833](https://github.com/github/gh-aw/actions/runs/43773614833) | Nov 15 | 24.1m | 2 | 339k | [#78176](https://github.com/github/gh-aw/discussions/78176) |

Token figures come from the audit output. The log summary counts tokens differently, so compare the audit numbers with each other rather than against other tools.

The November 13 run is the outlier. It took 60 turns and about 3.25 million tokens, roughly ten times the November 15 run's total. The audit classed it as a resource-heavy run for an issue-response task and recommended trimming turns, tools, or write actions. The later runs took two or three turns, yet they still ran for 24 to 27 minutes, so turn count does not explain duration on its own.

Every audit in the window carries the same two assessments: "resource heavy for domain" and "poor agentic control." These are judgments, not failures. All three runs completed, reported no errors, and made two write actions each.

## Signals worth a closer look

The November 15 audit includes several checks that deserve attention:

- **Loops grader:** the loops check failed, reporting 275 against a threshold of 3. The run recorded zero retries, so this is a signal to investigate, not proof of a retry storm.
- **Context growth:** the context grew about 29 times over the run. That growth is one reason the token total is high for a run of only two turns.
- **Cache window:** the audit warns that gaps between turns may exceed the five-minute prompt-cache lifetime. If they do, the context is re-read at full cost.
- **Network activity:** the firewall logged 155 requests and blocked none.

The chart below is the repository's general activity view, shown for context. It is not a chart of this workflow's runs.

![Agentic workflows activity](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## What to take from it

A monitor that succeeds every day can still be expensive. The clearest lever in this data is the turn count. The November 13 run shows how a long run multiplies cost without changing its outcome. The open question is whether that run was an outlier or an early sign of a pattern that the next few weeks of runs will show.

## Try it

Read the [workflow source](https://github.com/github/gh-aw/blob/main/.github/workflows/agent-job-health.md) to see how it checks agent jobs and reports results. To build something similar, start at [github/gh-aw](https://github.com/github/gh-aw).
