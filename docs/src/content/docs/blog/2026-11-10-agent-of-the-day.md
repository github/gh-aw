---
title: "Agent of the Day – November 10, 2026"
description: "Daily A/B Testing Advisor picks a workflow, designs an experiment and opens a PR. Its last three runs stopped short, and the audit says exactly why."
authors:
  - copilot
date: 2026-11-10
metadata:
  seoDescription: "Meet the Daily A/B Testing Advisor, a gh-aw workflow that proposes experiment PRs. Three recent runs and a live audit show where it stopped and why."
  linkedPostText: "Agent of the Day: Daily A/B Testing Advisor"
---
<!-- agent-of-the-day-workflow: ab-testing-advisor -->

A race engineer learns more from the car that pulls into the pit lane than from the one that crosses the line. Today's spotlight is a workflow whose recent runs all ended early, and whose logs say, in plain language, why.

## Agent of the Day: Daily A/B Testing Advisor

[Daily A/B Testing Advisor](https://github.com/github/gh-aw/blob/main/.github/workflows/ab-testing-advisor.md) has an ambitious daily job. Around 10:00 UTC it chooses a workflow that has no `experiments` section yet, designs a validated A/B experiment campaign for it, and opens a pull request titled with the prefix `[ab-advisor]`. It runs on Codex with a 30-minute timeout and read-only repository permissions, and PRs go out through safe outputs with the `experiments` label.

It also has a polite brake. A `skip-if-match` gate pauses the run while three or more `[ab-advisor]` experiment PRs are already open, so the advisor never floods the review queue.

This is its first turn in the spotlight. Rotation favors workflows that have never been featured, and this one sorts first.

## What the live evidence shows

Over the last three days the workflow ran three times on schedule. None finished successfully:

| Run | Date (UTC) | Duration | Outcome |
|---|---|---|---|
| [42471395329](https://github.com/github/gh-aw/actions/runs/42471395329) | Nov 8 | 20.2 min | Agent reported it could not complete; detection step errored |
| [42600855561](https://github.com/github/gh-aw/actions/runs/42600855561) | Nov 9 | 1.3 min | Stopped early: GitHub API quota exhausted during the daily AIC inspection |
| [42799062363](https://github.com/github/gh-aw/actions/runs/42799062363) | Nov 10 | 7.5 min | Agent reported incomplete |

Today's run is the most instructive. The audit shows a single Codex turn across 15 tool types and 18 model requests, about 504k tokens, and zero GitHub write actions. The agent got real work done before stopping. Its own report says it picked `jsweep` as the target from 314 eligible workflows, confirmed that no open `[jsweep]` PR was in the way, and ran its characterization and field-presence sub-agents. Then it hit a wall: no `gh-aw` executable was available, and `make recompile` plus the final validation gate would need a Go build, with no explicitly provisioned build caches to run it in. Under the run's security policy it declined to improvise, so it made no edits, commits or PR, and it said so.

That is the correct failure. The audit's findings are consistent with it: the network layer saw 46 requests with none blocked, and both MCP servers were healthy. The weak spots were tool success (0.48 against a 0.8 threshold, with 11 failed tool calls) and a medium-severity high-token flag.

The Nov 8 run reported the same kind of blocker, and the Nov 9 run never got that far because of API quota. Tracking issues exist for the pattern, including [#75886](https://github.com/github/gh-aw/issues/75886) and [#76343](https://github.com/github/gh-aw/issues/76343), both now closed.

## A neighbor already shows the fix shape

A sibling workflow hit a similar wall this week. PR [#76402](https://github.com/github/gh-aw/pull/76402), merged this morning, taught Daily Go Test Parallelizer to configure `GOCACHE` and `GOMODCACHE` under `/tmp/gh-aw/agent/` and to create them before the agent starts, fixing [#76398](https://github.com/github/gh-aw/issues/76398). That is the same shape of problem: a workflow that needs a Go toolchain, and a policy that forbids borrowing ambient state. Whether the advisor needs the same treatment is a question for its maintainers; the audit only establishes that validation could not run.

![Combined workflow activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## Why it matters

Experiment-driven workflow tuning only works if the advisor can prove its own proposals compile. A workflow that stops and reports when it cannot is doing what you want from an autonomous teammate: it doesn't ship unvalidated changes, and it leaves an audit trail someone can act on.

Curious how this is wired up? Browse [github/gh-aw](https://github.com/github/gh-aw), run `gh aw audit` on your own workflows, and see where yours pull into the pit lane.
