---
title: "Agent of the Day – November 17, 2026"
description: "Agent Persona Explorer writes one persona issue a day. Its three live runs show how much the same daily job can vary in turns and tokens."
authors:
  - copilot
date: 2026-11-17
metadata:
  seoDescription: "Meet Agent Persona Explorer, a daily gh-aw workflow that files one persona report per run. Three live runs show how its turns and token use vary."
  linkedPostText: "Agent of the Day: Agent Persona Explorer"
---

<!-- agent-of-the-day-workflow: agent-persona-explorer -->

Every day, a workflow in this repository generates software personas and checks how a custom agent responds to common automation tasks. It writes its findings into a GitHub issue. Over three recent runs, the work it did varied a lot, and so did the cost. Today's spotlight is Agent Persona Explorer.

## Agent of the Day: Agent Persona Explorer

[Agent Persona Explorer](https://github.com/github/gh-aw/blob/main/.github/workflows/agent-persona-explorer.md) runs once a day. Each run creates one issue titled "Agent Persona Exploration" with a Persona Overview and Key Findings section. Its frontmatter also runs an A/B experiment, `sub_agent_strategy_prefix_v1`, which compares a per-scenario sub-agent strategy with a batched one.

This is the first time it has been featured. The rotation moves through active workflows from the least recently featured onward. The workflow ahead of it, `ace-editor`, was skipped. Its two most recent recorded runs, from July 31 and July 13, failed before executing, with zero turns and no outputs, so there was no live evidence to judge.

## Three runs, three different shapes

| Run | Date (UTC) | Duration | Turns | Audit tokens | Experiment variant | Issue |
|---|---|---|---|---|---|---|
| [43610365440](https://github.com/github/gh-aw/actions/runs/43610365440) | Nov 14 | 14.6m | 23 | 1.44M | per_scenario | [#77806](https://github.com/github/gh-aw/issues/77806) |
| [43730042500](https://github.com/github/gh-aw/actions/runs/43730042500) | Nov 15 | 13.9m | 20 | 1.11M | batch | [#78082](https://github.com/github/gh-aw/issues/78082) |
| [43898094066](https://github.com/github/gh-aw/actions/runs/43898094066) | Nov 16 | 12.7m | 9 | 690k | per_scenario | [#78410](https://github.com/github/gh-aw/issues/78410) |

All three runs succeeded and each created one issue. The log summary totals 37.1 AI credits across them. Its token counter disagrees with the audit's: the log reports zero tokens for the November 14 run, while the audit records its highest count. Compare the audit figures with each other, not with the log summary.

Token use tracks turns closely. Each run spent roughly 56,000 to 77,000 tokens per turn, so the number of turns is the main lever on cost. The November 14 run took 23 turns and the November 16 run took 9, a difference of more than twofold in tokens.

## What the audits flag

All three audits carry the same two findings: "High Token Usage" and "Declared Sub-agent Model Not Observed." The sub-agent model the workflow declares never appeared in the observed run data, so the audit cannot confirm which model did the sub-agent work. Each run is also tagged "Resource Heavy For Domain." The firewall blocked no requests in any of the three runs.

Each audit also carries a "Partially Reducible To Deterministic" finding. It estimates that 91% of the November 14 turns, 90% of the November 15 turns, and 77% of the November 16 turns were data gathering that a script could do instead. That is the most concrete lead in this data: most turns go to retrieval, not judgment.

The chart below shows the repository's general agentic activity. It is not a chart of this workflow's runs, so read it as context only.

![Agentic workflows activity](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## What to take from it

Across these three runs, the workflow took between 9 and 23 turns. The lever is how many of those turns go to gathering data before any judgment happens. Whether that gathering can move into a script is the question the audit raises. The next few runs will show whether the November 14 spike was a one-off or a pattern.

## Try it

Read the [workflow source](https://github.com/github/gh-aw/blob/main/.github/workflows/agent-persona-explorer.md) to see how the personas are chosen and how each run reports back. To build something similar, start at [github/gh-aw](https://github.com/github/gh-aw).
