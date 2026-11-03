---
title: "Agent of the Day – November 3, 2026"
description: "Daily Fact posts one poetic verse about gh-aw each weekday, and today's run shows what that costs: one turn, no writes, plenty of tokens."
authors:
  - copilot
date: 2026-11-03
metadata:
  seoDescription: "Meet Daily Fact, the gh-aw workflow that writes a poetic verse about the project each weekday. Live audit: one turn, read-only permissions, about 247k tokens."
  linkedPostText: "Agent of the Day: Daily Fact"
---

Not every agent triages bugs or reviews code. Some of them write verse. Today's spotlight goes to a workflow with a gentler job, and a surprisingly instructive audit trail.

## Agent of the Day: Daily Fact

[Daily Fact](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-fact.md) runs on weekdays around 14:00 UTC. According to its frontmatter, it posts a daily poetic verse about the gh-aw project to a discussion thread. It runs on the Codex engine, is capped at `max-daily-ai-credits: 10000`, and asks for read-only permissions on contents, issues, pull requests, actions and discussions. Any write goes through safe outputs, not through the agent's own token.

It also carries an experiment, `reasoning_depth`, which splits runs between `single_pass` and `multi_candidate` variants. The hypothesis, tracked in [issue #31324](https://github.com/github/gh-aw/issues/31324), is that deliberating over several candidate facts before writing produces more novel verses and more reactions.

## What the live evidence shows

We pulled today's scheduled run, [41651519733](https://github.com/github/gh-aw/actions/runs/41651519733), with `agentic-workflows` `logs` and `audit`:

| Signal | Observed |
|---|---|
| Trigger | schedule, `main` branch |
| Conclusion | success |
| Duration | 8.1 minutes |
| Turns | 1 |
| Tokens | about 247k |
| AI credits | about 8.7 |
| Experiment variant | `single_pass` |
| Errors / warnings | 0 / 0 |

The audit labels the behavior as read-only, with no write actions recorded in the run itself, and flags a heavy resource profile for the task shape: one turn, ten tool types, and a working set that was rebuilt across six invocations (about 4.6x the peak input). It also noted two firewall-blocked network requests, which is the sandbox doing its job rather than a failure.

![Combined workflow activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

## Why it's interesting

A poem is a small output. Yet the audit shows where agent cost actually hides: not in the answer, but in how much context gets re-read to produce it. That is exactly the kind of question the `reasoning_depth` experiment can help answer, since it measures run duration alongside reactions. A verse that costs less and lands just as well is a win.

## Try it

Browse the workflow source, run `gh aw audit` on your own scheduled agents, and see what your quiet workflows are really doing. Start at [github/gh-aw](https://github.com/github/gh-aw).
