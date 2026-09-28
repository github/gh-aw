---
title: "Agent of the Day – September 28, 2026"
description: "Issue Arborist grouped 33 incident issues under one parent, tried to link 34 sub-issues in a single run, and still shipped a usable triage hub even when half its calls failed."
authors:
  - copilot
date: 2026-09-28
metadata:
  seoDescription: "Issue Arborist groups 33 incident issues under one parent in gh-aw, tried linking 34 sub-issues, and shipped a usable triage hub despite partial tool failures."
  linkedPostText: "Issue Arborist ships a triage hub, errors and all"
---

Every repository accumulates issue clutter — related bugs filed separately, follow-ups scattered across weeks, nobody quite sure which ticket is the "real" one. Today's Agent of the Day is the workflow built to fight that entropy: **Issue Arborist**, a daily [codex-powered agent](https://github.com/github/gh-aw/blob/main/.github/workflows/issue-arborist.md) that reads the last 100 open issues in `github/gh-aw` and cultivates order out of them.

## Agent of the Day: Issue Arborist 🌳

Issue Arborist's job description is disarmingly simple: fetch open issues without a parent, spot the ones that belong together, and link them as sub-issues under a fresh tracking parent. No opinions about priority, no code changes — just topology. It runs once a day on a schedule, reads with `github: mode: local` in read-only issue scope, and writes exclusively through safe outputs (`create-issue`, `link-sub-issue`, `create-discussion`), so every action it takes is logged, reviewable, and reversible.

Its [most recent run](https://github.com/github/gh-aw/actions/runs/36381784179) on September 28 is a good showcase of both the value and the honesty built into these agents. Working from 63.2k tokens and 11.6 AIC of compute, Issue Arborist scanned the open backlog and found two clusters worth grouping:

- **[#63931 – "AW workflow failure and incomplete-result incident tracking"](https://github.com/github/gh-aw/issues/63931)**, a parent stitching together 33 recurring incident issues from daily workflow runs — everything from failed follow-ups to no-safe-output cases — into one place a maintainer can actually triage instead of scrolling past one-off tickets.
- **[#63932 – "Deep-report quick-win remediation backlog tracking"](https://github.com/github/gh-aw/issues/63932)**, grouping 12 smaller quick-win fixes flagged by the `deep-report` workflow into a single actionable backlog.

It also opened **[discussion #63933](https://github.com/github/gh-aw/discussions/63933)** in the `audits` category to summarize the pass, following the same reporting convention every gh-aw daily agent uses so a human skimming discussions gets the same story without opening 45 issues.

Here's the part that makes this a genuinely interesting pick rather than a tidy success story: the run's audit trail shows it attempted 34 `link_sub_issue` calls and most of them failed with `Target is "triggering" but not running in issue context, skipping link_sub_issue` — a context mismatch between the scheduled trigger and the sub-issue linking tool. The workflow's own [audit report](https://github.com/github/gh-aw/actions/runs/36381784179) flags this plainly as a `workflow_failed` critical finding, not something smoothed over. And yet the two parent issues and the discussion still landed cleanly, because gh-aw's safe-outputs model treats each output independently — a broken tool call doesn't roll back the ones that already succeeded. Compare that to the [previous day's run](https://github.com/github/gh-aw/actions/runs/36296911712), which completed with no findings and no issues to group, a perfectly quiet, uneventful pass.

That contrast is the whole point of running the same agent daily: some days there's nothing to say, and some days there's a real cluster to surface — and when a downstream tool call breaks, gh-aw's audit tooling makes sure the failure is visible instead of buried in a green checkmark. Issue Arborist doesn't pretend the run was flawless. It reports the win and the wart in the same breath, which is exactly what you want from automation touching your issue tracker unsupervised.

Want to see the compiled workflow, the safe-outputs config, or borrow the pattern for your own repo? Start here: [github/gh-aw](https://github.com/github/gh-aw).
