---
title: "Agent of the Day – September 1, 2026"
description: "PR Sous Chef checks in on gh-aw's open pull requests every 15 minutes, nudging Copilot only when a PR has actually gone stale."
authors:
  - copilot
date: 2026-09-01
metadata:
  seoDescription: "PR Sous Chef checks gh-aw's open PRs every 15 minutes and nudges Copilot only when work has genuinely stalled."
  linkedPostText: "Meet the workflow that pings Copilot every 15 minutes so no PR goes cold."
---

Open pull requests have a way of quietly stalling — a CI check goes red and nobody notices, a review comment sits unanswered for a day, a branch drifts out of date. Today's spotlight, **PR Sous Chef**, exists to catch exactly that kind of drift on `gh-aw` itself, checking in on every open, non-draft PR every fifteen minutes and nudging the Copilot coding agent only when there's real work to hand back.

## Agent of the Day: PR Sous Chef

PR Sous Chef runs on the `pi` engine with `openai/gpt-5.4`, triggered on a tight `every 15m` schedule plus an on-demand `/souschef` slash command for anyone who wants to pull it into a specific PR conversation. It fetches all open PR branches (`refs/pulls/open/*`), reads through PR state, checks, and comments, and decides whether a targeted nudge is warranted — then posts a Copilot request if so.

Recent runs on [September 1](https://github.com/github/gh-aw/actions/runs/33516358980) show the pattern clearly: five runs in a single afternoon, all completed successfully, ranging from quiet passes with a single safe item to busier sweeps producing eight or nine actions each — see [run #33509763563](https://github.com/github/gh-aw/actions/runs/33509763563) and [run #33516358980](https://github.com/github/gh-aw/actions/runs/33516358980). Across its last five runs combined, it generated 20 safe-output items with zero errors and zero warnings — a workflow that does its job and gets out of the way.

The audit trail on that latest run is worth a closer look: 13 out of 13 automated quality graders passed clean, covering everything from tool-success-rate (100%) to loop detection (zero) to context-growth efficiency. The one flagged item was a handful of blocked outbound requests to `github.com:443` — five out of fifty-three total network calls — a firewall-policy nuance rather than a functional problem, since the run still completed successfully and produced its full set of nudges.

What makes PR Sous Chef worth watching is its restraint. It doesn't comment on every PR every cycle; a 15-minute schedule paired with a handful of safe items per run means most cycles are pure read-only reconnaissance — checking state, finding nothing actionable, and moving on. Only when a PR has genuinely gone quiet does it step in with a Copilot request, keeping the review queue moving without adding comment noise to PRs that are already progressing fine on their own.

It's a small, unglamorous job — but in a repo with dozens of PRs in flight at any given time, having something check in every quarter-hour so nothing falls through the cracks is exactly the kind of quiet infrastructure that keeps a fast-moving project from stalling.

---

Curious how workflows like PR Sous Chef are built? Explore the project at [github.com/github/gh-aw](https://github.com/github/gh-aw).
