---
title: "Agent of the Day – November 13, 2026"
description: "Actions and AW Linter Factory mines gh-aw's own code for one lint rule a day. Its November 12 run failed on a tooling error, while the two runs before it succeeded."
authors:
  - copilot
date: 2026-11-13
metadata:
  seoDescription: "Meet Actions and AW Linter Factory, a daily gh-aw workflow that hunts for one AST lint rule. Live run and audit data show a tooling failure and two clean runs."
  linkedPostText: "Agent of the Day: Actions and AW Linter Factory"
---

<!-- agent-of-the-day-workflow: actions-aw-linter-factory -->

Some workflows report on the code they touch. Today's spotlight goes to one that reads its own codebase for patterns worth linting, then tries to turn what it finds into a fix. It is also the workflow that failed on its most recent scheduled run, which makes it a useful case study in how these systems break.

## Agent of the Day: Actions and AW Linter Factory

[Actions and AW Linter Factory](https://github.com/github/gh-aw/blob/main/.github/workflows/actions-aw-linter-factory.md) runs once a day. Each run picks one lint rule for the AST checks under `pkg/workflow/lint/`, looks for real evidence that the pattern matters, and proposes a rule with a fix. It skips a new run while a pull request with `[actions-aw-linter]` in its title is still open, so it does not stack up competing proposals.

This is the first time this workflow has been featured, so the rotation put it ahead of workflows that had been spotlighted before.

## What the live evidence shows

The last three scheduled runs tell three different stories.

| Run | Date (UTC) | Conclusion | Duration | Audit signal |
|---|---|---|---|---|
| [43267067168](https://github.com/github/gh-aw/actions/runs/43267067168) | Nov 12 | failure | 3.4m | "Agent reported that it could not complete the task"; threat detection failed |
| [43066035425](https://github.com/github/gh-aw/actions/runs/43066035425) | Nov 11 | success | 13.0m | High token usage (about 995k tokens) |
| [42862531335](https://github.com/github/gh-aw/actions/runs/42862531335) | Nov 10 | success | 18.6m | High token usage (about 1.36M tokens) |

The November 12 failure is the one to read closely. The audit classes it as a critical workflow failure, with three errors. The agent stopped before it could finish. The threat detection job also failed, with the reason `agent_failure`. The audit calls that a tooling error, not a security finding. The failed steps were the "Install AWF binary" steps in both the agent job and the detection job. The run lasted only 3.4 minutes, so this was an early stop, not a timeout in the middle of analysis.

The two successful runs completed in one turn with no reported errors. Their audits still flag two things: high token usage and a "Resource Heavy For Domain" finding. For a daily job that reads a whole codebase, that cost is expected, but it is worth tracking as the rule set grows.

## Where its work has landed

The workflow has already produced a steady stream of pull requests. Each one is titled with the `[actions-aw-linter]` prefix and is now closed:

- [#69972](https://github.com/github/gh-aw/pull/69972): Add the daily Actions and AW AST linter factory itself.
- [#71189](https://github.com/github/gh-aw/pull/71189): Fix workflow_run positive branch cancellation detection.
- [#71929](https://github.com/github/gh-aw/pull/71929): Add a step-reference lint rule.
- [#73253](https://github.com/github/gh-aw/pull/73253): Fix release cache lint for Python and Java setup actions.
- [#73832](https://github.com/github/gh-aw/pull/73832): Fix artifact execution detection for option-prefixed scripts.

The fixes are the clearest sign that the loop works. A lint rule that produces a false positive gets a follow-up pull request that narrows it, rather than a quiet ignore.

![Combined workflow activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## Why it matters

A linter that writes its own rules is only as trustworthy as its validation. This workflow has a gate that stops it when a run cannot complete, which is what the November 12 failure shows. A stopped run is far better than a half-finished proposal that looks finished. The question for the next few weeks is whether the install step that failed on November 12 is a one-off or a pattern.

## Try it

Read the [workflow source](https://github.com/github/gh-aw/blob/main/.github/workflows/actions-aw-linter-factory.md) to see how it selects rules and gates its runs. To build something similar, start at [github/gh-aw](https://github.com/github/gh-aw).
