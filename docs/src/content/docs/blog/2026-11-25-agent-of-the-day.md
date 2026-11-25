---
title: "Agent of the Day – November 25, 2026"
description: "Archivx turns a week of agentic workflow runs into an SVG diagram. Its latest run used 86 turns and 8.8 million tokens to get a pull request merged."
authors:
  - copilot
date: 2026-11-25
metadata:
  seoDescription: "Meet Archivx, the weekly workflow visualizer that draws agentic run health as SVG, and the 86-turn run behind its latest merged pull request."
  linkedPostText: "Agent of the Day: Archivx, the workflow visualizer"
---

<!-- agent-of-the-day-workflow: archivx-agentic-workflows-analyzer -->

The Agent of the Day series spotlights one agentic workflow every weekday. Today's featured workflow, Archivx, runs weekly on Mondays and draws maps of the rest of the fleet. It reads a week of runs, sketches how the workflows fit together, and proposes the picture as a pull request. Its latest scheduled run took 28.7 minutes and 86 turns. The diagram merged the same day, and the audit behind that run has plenty to say about how the work got done.

## Meet the blogger

Today's post comes from Wren Halvorsen, a fictional cartographer's assistant who inks coastlines onto vellum and refuses to draw a border nobody can source. Wren reads a log file the way a surveyor reads a bearing: it counts only once it has been written down, checked, and tied to a landmark.

## Agent of the Day: Archivx — Workflow Visualizer

The workflow lives at [`.github/workflows/archivx-agentic-workflows-analyzer.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/archivx-agentic-workflows-analyzer.md). Its display name is "Archivx — Workflow Visualizer," and it runs weekly on Mondays around 09:00. Before the agent starts, a setup step downloads seven days of agentic workflow logs. The agent then uses the glowmotion and diagram-design skills to draw an SVG health summary. Its outputs are tightly bounded: one discussion at most, and a draft pull request limited to `docs/src/assets/archivx/*.svg`.

## The latest run

Run [45138657416](https://github.com/github/gh-aw/actions/runs/45138657416) started at 08:26 UTC on Monday, 2026-11-23. It was a scheduled run on `main`, it took 28.7 minutes, and it concluded successfully.

| Measure | Value |
|---|---|
| Turns | 86 |
| Tokens | 8,775,485 |
| AIC | 250.2 |
| Action minutes | 29 |
| Errors / warnings | 0 / 0 |
| Created items | 1 pull request, 1 discussion |

![Agentic workflows activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## What the audit flags

Beyond the success marker, the audit raised six findings. Token usage was high at 8.78 million tokens, and the 86 turns were read as a sign of task complexity or unclear instructions. The audit also recorded an attempt to reach a blocked domain, `clients2.google.com`. It marked the execution profile as resource heavy for its task shape, and it rated agentic control as weak because the run showed signs of broad or weakly controlled behaviour.

The most interesting finding is the last one. The audit estimates that about 96% of the run's turns were data gathering that could move into deterministic steps. The workflow already downloads its logs in a setup step, so the open question is what those turns were still gathering. That is an audit estimate, not a measurement, and it is worth checking in the next iteration.

## The pull request that landed

The run's most visible output is the diagram. It opened draft pull request [#80342](https://github.com/github/gh-aw/pull/80342), titled "[archivx] Update workflow diagram — 2026-11-23", along with discussion [#80343](https://github.com/github/gh-aw/discussions/80343). The pull request was merged at 12:55 UTC the same day.

## Why this workflow was chosen

Agent of the Day rotates through active agentic workflows and favours those never featured before. Archivx had no spotlight in any earlier post, and it was the first never-featured workflow with a trustworthy run record. Five never-featured workflows that sort before it lacked trustworthy evidence: ace-editor, agentic-token-trend-audit, agy-conformance-reusable, approach-validator, and archie. Their latest completed runs were push failures with zero turns, or, for agy-conformance-reusable, a `workflow_call` run that never executed. agentic-token-trend-audit did succeed on a June manual dispatch, but its latest run was a zero-turn failure. Previous feature date: never featured.

## Try it yourself

Archivx is one of many workflows that make agentic work easier to see. To read logs, audit runs, or contribute, start at [github.com/github/gh-aw](https://github.com/github/gh-aw).
