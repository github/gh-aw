---
title: "Agent of the Day – August 31, 2026"
description: "Issue Arborist reads 100 open issues a day and links sub-issues into trees, only creating a new parent when a cluster truly needs one."
authors:
  - copilot
date: 2026-08-31
metadata:
  seoDescription: "Issue Arborist reads 100 open issues a day and links sub-issues into trees, only creating a new parent when a cluster truly needs one."
  linkedPostText: "The gardener that reads 100 issues a day and only prunes when it's sure."
---

Every open-source repo eventually drowns in the same problem: a hundred open issues, half of them clearly related, none of them linked. Someone has to notice that six issues are all milestones of the same game plan, or that a dozen bug reports trace back to one root cause — and then actually go build the sub-issue tree. Today's spotlight, **Issue Arborist**, is the daily workflow that does exactly that for `gh-aw` itself.

## Agent of the Day: Issue Arborist

Every morning, Issue Arborist pulls the last 100 open issues that don't already have a parent, reads through their titles, bodies, and labels, and decides what — if anything — deserves to be grouped. It's deliberately conservative: it only proposes a brand-new parent issue when a cluster is strong and orphaned, and it only links a sub-issue when the relationship is unambiguous.

In its [August 29 run](https://github.com/github/gh-aw/actions/runs/33235825296), that discipline produced a genuinely useful cleanup. The agent noticed seven Deep Report issues — schema drift fixes, duplicate type consolidation, messaging tweaks — that were all clearly part of the same maintenance effort but had no tracking issue. Rather than link them to something that didn't fit, it created a new parent, `[Parent] Deep Report cleanup and schema consolidation`, and attached all seven (#56706–#56712) underneath it. It then found two more clean clusters: four cache-miss issues that belonged under the existing `Daily Cache Strategy Analyzer` issue group (#56716), and six milestone issues for a fictional game project, "Neon Heist" (#56635), each one an obvious child of its own plan.

The very next day, on [August 30](https://github.com/github/gh-aw/actions/runs/33294394874), and again on [August 31](https://github.com/github/gh-aw/actions/runs/33360242345), it kept going — linking six more milestone issues for a different project, "Starling Drift" (#57144), and folding eight fresh `[deep-report]` findings into the existing issue-group parent (#56849), all without creating a single unnecessary new tracking issue.

What's most interesting is what the agent chose *not* to do. Each run ends with a same-day [discussion post](https://github.com/github/gh-aw/discussions/56833) explicitly listing the clusters it saw but declined to link — a group of near-duplicate "Implement skill-constraint-coverage" issues that might be duplicate attempts rather than a hierarchy, a set of repeated workflow-failure reports that could be separate incidents rather than one root cause, and several `[aw] Smoke ... failed` issues that looked plausible but weren't confident enough to map automatically. That restraint is the whole point: a triage bot that links everything it can find isn't useful, it's noise. Issue Arborist's value is in knowing when *not* to act.

Across its last three runs it made 129 `link_sub_issue` calls and opened one new parent issue — a quiet, steady gardening job that keeps `gh-aw`'s issue tracker legible without anyone having to ask it to.

---

Curious how workflows like Issue Arborist are built? Explore the project at [github.com/github/gh-aw](https://github.com/github/gh-aw).
