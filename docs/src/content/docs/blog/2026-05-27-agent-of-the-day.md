---
title: "Agent of the Day – May 27, 2026"
description: "The Agent Performance Analyzer scored 236 workflows this week — ecosystem health jumped 20 points to 90/100 and a persistent 5-day CLI failure was auto-filed as a GitHub issue."
authors:
  - copilot
date: 2026-05-27
metadata:
  seoDescription: "How gh-aw's Agent Performance Analyzer scores 236 agentic workflows daily — ecosystem health hit 90/100, a 20-point weekly gain, with auto-filed GitHub issues."
  linkedPostText: "Agent Performance Analyzer hits 90/100 ecosystem health"
---

> [!NOTE]

Every day, 236 agentic workflows run inside the `gh-aw` repository. Most complete quietly. A few fail in patterns worth tracking. And once a week, one workflow reads the entire fleet, scores it, and writes up what it found. That workflow is the **Agent Performance Analyzer**, and its run on May 27, 2026 produced the clearest signal in months.

## Agent of the Day: Agent Performance Analyzer — Meta-Orchestrator

The `agent-performance-analyzer` is not a workflow that builds features or merges PRs. Its job is to watch everything else. On a daily schedule, it fans out across the full fleet of 236 workflows, scores each agent group across three dimensions — quality (0–100), effectiveness (0–100), and ecosystem health (0–100) — and surfaces what the aggregate data says about systemic health. Think of it as a standing post-incident review that runs without anyone needing to call one.

[Run #26515287616](https://github.com/github/gh-aw/actions/runs/26515287616), logged on May 27, ran for 10.7 minutes and processed 12.2 million effective tokens. Those numbers matter because they reflect how much context the analyzer actually reads — audit logs, PR outcomes, failure histories, discussion threads — before rendering a score. This is not a lightweight health check.

The headline number from this week's pass: ecosystem health hit **90/100**, up 20 points from the prior week. That is the largest single-week jump in the recorded history of this metric. It is also a number that demands interpretation, not celebration. A 20-point move in one week usually means either the fleet genuinely improved, or something was suppressing the score before and is now resolved. The weekly Discussion [#35220](https://github.com/github/gh-aw/discussions/35220) breaks down the contributing factors — most of the lift came from `copilot-swe-agent` merge rate recovery, which landed at 67% week-over-week, up 6 percentage points, with 6 merges on May 27 alone. Merge rate as a proxy for workflow effectiveness is imperfect, but 67% across a fleet this size is a meaningful signal.

The top performers bear out that story. **Lint Monster** scored 90/100 on quality and 85/100 on effectiveness — consistent, expected, unglamorous. **copilot-swe-agent** followed at 88/100 quality and 84/100 effectiveness. **spec-enforcer/extractor** went 3-for-3 on merges this week, a 100% merge rate on a small but non-trivial sample. These are the parts of the fleet holding their line.

Quality, though, is flat. 74/100 for the fourth consecutive week. A plateau at week four is no longer noise. The analyzer flagged this directly: without intervention, the quality score will not self-correct. The fleet is not degrading, but it is not improving either, and in a system that runs daily, stasis accumulates.

## What the Analyzer Filed This Week

The more operationally significant output from this run was not the Discussion — it was [issue #35219](https://github.com/github/gh-aw/issues/35219). The analyzer detected a Copilot CLI execution failure pattern affecting the Daily News and Daily Issues Report workflows across five or more consecutive days at a 100% failure rate. A workflow failing once is noise. Failing every day for a week is infrastructure. The issue was filed automatically based on threshold logic baked into the analyzer's scoring criteria. No human had to notice the pattern.

Three other systemic issues surfaced in [Discussion #35220](https://github.com/github/gh-aw/discussions/35220). A `safe-outputs` permission regression is blocking three or more agent groups and has been classified P1. A CGO/CJS build regression running at 37% failure rate has now exceeded 90 days without resolution — that is a P0 by any reasonable SLO definition. And 87 of the fleet's 236 workflows show no recent runs at all, which makes them deprecation candidates pending owner review. The firewall processed 113 requests during this period and blocked 30 of them — a 27% block rate — which is consistent with prior weeks but warrants monitoring if the trend climbs.

The value of a meta-orchestrator is not that it prevents incidents. It is that it shortens the time between an incident beginning and someone with context knowing about it. Five consecutive days of 100% failure on two named workflows, with an auto-filed issue linking directly to the evidence, is a materially better outcome than a developer noticing something is off on day seven.

---

The work of keeping 236 workflows healthy is mostly invisible until something breaks. The Agent Performance Analyzer makes that work legible — in scores, in filed issues, in a weekly Discussion that records what the fleet looked like at a point in time. If you want to follow along, the full weekly report is in [Discussion #35220](https://github.com/github/gh-aw/discussions/35220), and the project lives at [github/gh-aw](https://github.com/github/gh-aw).
