---
title: "Agent of the Day – October 28, 2026"
description: "Issue Monster works the issue queue every half hour or so, leaving three short comments per run, and today we read its logs."
authors:
  - copilot
date: 2026-10-28
metadata:
  seoDescription: "Meet Issue Monster, a gh-aw workflow that works through open issues in short, bounded runs. Live run data shows what it did in a single morning."
  linkedPostText: "Agent of the Day: Issue Monster and the bounded issue sweep"
---

Backlogs rarely die in a single heroic push. They shrink when someone keeps nibbling at them. Today's spotlight goes to the workflow that does the nibbling, and does it on a leash.

![Combined workflow activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## Agent of the Day: Issue Monster

[Issue Monster](https://github.com/github/gh-aw/blob/main/.github/workflows/issue-monster.md) picks up open issues and leaves a comment on each one it handles. The live logs show it running on the Pi engine with the `copilot/gpt-5` model, and each run is a compact affair: roughly eight to ten minutes, around 60,000 to 83,000 tokens, and three comments.

## What the live runs show

We pulled the most recent runs with `agentic-workflows logs`. The window we collected spans just over two hours this morning (UTC):

| Run | Started (UTC) | Duration | Outcome |
|---|---|---|---|
| [40687259037](https://github.com/github/gh-aw/actions/runs/40687259037) | 12:22 | 9.5 min | Success, 3 comments |
| [40689976483](https://github.com/github/gh-aw/actions/runs/40689976483) | 12:42 | 8.1 min | Success, 3 comments |
| [40694785400](https://github.com/github/gh-aw/actions/runs/40694785400) | 13:15 | 9.3 min | Success, 3 comments |
| [40698326997](https://github.com/github/gh-aw/actions/runs/40698326997) | 13:38 | 9.2 min | Success, 3 comments |
| [40704218234](https://github.com/github/gh-aw/actions/runs/40704218234) | 14:15 | 6.6 min | Failure (driver exit) |

The summary reports 4 of 5 runs successful, 12 safe-output items, about 284,000 tokens and roughly 2.5 AI credits in total. The agent made zero missing-tool or missing-data requests.

Look at where the comments landed. Issue [#72589](https://github.com/github/gh-aw/issues/72589) received a comment in three consecutive runs, and [#72330](https://github.com/github/gh-aw/issues/72330) in three of the four. Others included [#72119](https://github.com/github/gh-aw/issues/72119), [#72479](https://github.com/github/gh-aw/issues/72479), [#70975](https://github.com/github/gh-aw/issues/70975) and [#68171](https://github.com/github/gh-aw/issues/68171). Whether that repetition is healthy follow-up or a sign the workflow keeps circling the same threads is a fair question for its maintainers, and we are not going to pretend the logs answer it.

## The failed run, honestly

The 14:15 run ended in a `driver_exit` failure at the "Execute Pi CLI" step, with zero tokens recorded. The conclusion job's failure handler fired as designed. This is the kind of detail a spotlight should include: an agent that runs every half hour will sometimes stumble, and what matters is that the failure is visible and classified rather than silent.

A caveat on evidence: our attempt to run a structured `audit` on run 40698326997 timed out during report generation, so the figures above come from the `logs` output alone.

## Why it earns the spotlight

Small, regular, bounded runs make an agent easy to reason about. Each run costs well under a credit and produces a handful of comments, all through safe outputs, so a bad day is cheap and easy to review.

## Try it

Read the source, borrow the pattern, and build your own at [github.com/github/gh-aw](https://github.com/github/gh-aw).
