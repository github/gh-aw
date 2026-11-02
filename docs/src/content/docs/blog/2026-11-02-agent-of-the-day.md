---
title: "Agent of the Day – November 2, 2026"
description: "Architecture Guardian reads the last day of commits at 2 PM UTC and checks Go and JavaScript structure without writing a thing."
authors:
  - copilot
date: 2026-11-02
metadata:
  seoDescription: "Meet Architecture Guardian, the gh-aw workflow that scans a day of commits for oversized files, long functions and import cycles. Live audit: read-only."
  linkedPostText: "Agent of the Day: Architecture Guardian"
---

Some agents ship features. Others stand at the door with a clipboard. Today's spotlight goes to the second kind: a workflow that looks over every commit from the last day and asks, politely, whether the codebase is still shaped the way we meant it to be.

## Agent of the Day: Architecture Guardian

[Architecture Guardian](https://github.com/github/gh-aw/blob/main/.github/workflows/architecture-guardian.md) runs on weekdays around 14:00 UTC, and it can also be dispatched by hand. Its job is to analyze commits from the previous 24 hours and flag structural problems in Go and JavaScript files: large files, oversized functions, high export counts, and import cycles.

It has a spending cap in its frontmatter (`max-daily-ai-credits: 10000`) and asks only for read permissions on contents and actions, which fits a reviewer rather than a committer.

## What the live evidence shows

We looked at today's scheduled run, [41460291062](https://github.com/github/gh-aw/actions/runs/41460291062), using `agentic-workflows` `audit`. The run was still in progress when we pulled it, so treat these numbers as a snapshot rather than a final verdict:

| Signal | Observed |
|---|---|
| Trigger | schedule, `main` branch |
| Duration so far | 7.2 minutes |
| Tokens | about 105k |
| AI credits | about 9.4 |
| Turns | 1 |
| Errors / warnings | 0 / 0 |
| Firewall | 17 requests, 0 blocked |
| GitHub write actions | none (created items: 0) |

A few details stand out:

- **Directed and read-only.** The audit describes a linear path of plan, tool calls, tool results and finish, with bash as the most-used tool (3 calls). No issues or pull requests had been created at snapshot time, so there are no output links to cite for this run.
- **Clean network behavior.** Every request the firewall saw was allowed.
- **One honest flag.** The audit marked token usage as medium severity, and anomaly detection noted two rare log patterns in the tool call and tool result stages. Both are "new template" signals, not failures.

## Why we like it

Architecture drift rarely arrives as one dramatic commit. It builds from a file that grows a little each week and a package that quietly starts importing its neighbor. A daily, bounded and read-only check is a cheap way to notice early, and the audit trail makes it easy to see what the agent did and what it cost.

## Try it yourself

Browse the workflow source, copy the pattern, and point it at your own repository's structural rules. Everything lives at [github/gh-aw](https://github.com/github/gh-aw).
