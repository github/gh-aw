---
title: "Agent of the Day – October 26, 2026"
description: "Daily File Diet finds the Go file that quietly grew too big and files a tidy, actionable refactoring plan."
authors:
  - copilot
date: 2026-10-26
metadata:
  seoDescription: "Daily File Diet scans Go sources for oversized files and opens a refactoring issue. Live audit: one turn, one issue, zero blocked requests."
  linkedPostText: "Meet Daily File Diet, the agent that spots files outgrowing their jeans"
---

Every codebase has a file that started small and kept saying yes. Today's spotlight goes to the workflow whose whole job is to notice.

## Agent of the Day: Daily File Diet

[Daily File Diet](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-file-diet.md) runs on a weekday schedule (around 13:00 UTC) and can also be started manually. It imports a shared Go source analysis, looks for files past a size threshold, and turns its findings into a single issue labelled `refactoring` and `code-health`. Issues expire after two days, and the workflow skips a run if a `[file-diet]` issue is still open, so it never piles up duplicates.

Its permissions are read-only for contents, issues and pull requests. The only write path is the issue it files through safe outputs.

## What the logs show

The latest completed run, [run 40338270065](https://github.com/github/gh-aw/actions/runs/40338270065), finished successfully in about 9 minutes:

- **Outcome:** success in one agent turn, with no reported workflow errors.
- **Output:** one issue, [#72111](https://github.com/github/gh-aw/issues/72111), flagging `pkg/cli/token_usage_subagent_session.go` at 1,557 lines against an 800-line threshold.
- **Network:** the firewall recorded 20 requests, all allowed, none blocked.
- **Stability:** against an earlier successful baseline ([run 39956353556](https://github.com/github/gh-aw/actions/runs/39956353556)), turns and write-capable posture were unchanged.
- **Cost:** the audit flagged medium "high token usage" at roughly 227k tokens, which is a reasonable thing to watch for a workflow that reads large files.

## The issue it wrote

The report is more than "this file is big." It maps the file into concerns: event schema types, unified token usage reading, session parsing, Copilot and Pi observers, error helpers, and row aggregation. It notes test coverage (a 645-line test file, roughly a 0.41 ratio) and proposes a split into focused files. That is the difference between a nag and a starting point: a maintainer or a coding agent can pick it up and begin.

## Why it's worth a look

Size thresholds are blunt, but a tool that applies them daily, explains the seams, and cleans up after itself keeps technical debt visible without creating noise. You can reproduce this evidence with `gh aw logs` and `gh aw audit`.

## Try it

Browse the workflow sources, borrow the pattern for your own repository, and tell us what your agents find at [github/gh-aw](https://github.com/github/gh-aw).
