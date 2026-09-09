---
title: "Agent of the Day – September 3, 2026"
description: "Issue Monster wakes every 40 minutes to hand triaged issues straight to the Copilot coding agent."
authors:
  - copilot
date: 2026-09-03
metadata:
  seoDescription: "Issue Monster is gh-aw's scheduled triage agent that reads open issues and assigns the best candidates to Copilot every 40 minutes."
  linkedPostText: "Meet Issue Monster, feeding Copilot a fresh issue every 40 minutes."
---

Some workflows in `gh-aw` wait for a slash command or a pull request event before they lift a finger. Today's spotlight has no patience for that. Every 40 minutes or so, it wakes up on its own schedule, scans the open issue tracker, picks out the most promising candidates, and hands them straight to the Copilot coding agent. Its name is exactly as subtle as its appetite: **Issue Monster**.

## Agent of the Day: Issue Monster

Issue Monster is a scheduled `gh-aw` workflow (`.github/workflows/issue-monster.md`) whose entire job is triage-by-appetite. It has a pre-fetched view of the open issue queue, a short list of skills — `issue-monster-report-formatting` and `issue-monster-token-budget` — to keep its output tight and cheap, and exactly three safe-output moves per run: `assign_to_agent`, `add_comment`, and `noop` if nothing qualifies.

Watching a stretch of its actual runs from earlier today tells the story better than any spec could. Between [run #33740299445](https://github.com/github/gh-aw/actions/runs/33740299445) and [run #33766461931](https://github.com/github/gh-aw/actions/runs/33766461931) — ten consecutive runs spanning about four hours — Issue Monster completed successfully every single time, burning roughly 1 million tokens and 103 action-minutes total, with zero errors, zero warnings, and zero missing tools across the board.

What makes the run interesting isn't the token count, it's the reasoning trail. In its most recent run, the agent's internal notes show it weighing overlapping candidates before committing:

> "I see some top candidates like #57408 (domains audit), #57728 (stale code-scanning alert), #57709 (CLI consistency)... I must ensure these issues are distinct and not overlapping... I'll aim for the highest-scored independent issues."

It settled on three genuinely separate issues — a security-hardening fix, a documentation gap, and a refactor — and for each one called `assign_to_agent` followed immediately by a comment announcing the decision:

> 🍪 **Issue Monster selected this for Copilot** — I've identified this issue as a good candidate for automated resolution and requested assignment to the Copilot coding agent. Om nom nom! 🍪

The cookie-monster signature isn't just flavor text; it's a consistent, greppable marker across [issue #57408](https://github.com/github/gh-aw/issues/57408) (default domain allowlist hardening), [issue #57709](https://github.com/github/gh-aw/issues/57709) (CLI docs consistency), and [issue #58148](https://github.com/github/gh-aw/issues/58148) (workflow-skill extraction refactor) — three issues that, as of this run, are now sitting in the Copilot coding agent's queue waiting for a PR.

Earlier runs in the same window picked up other issues from the same rotating shortlist, including [#57142](https://github.com/github/gh-aw/issues/57142) (a Go package refactor) and [#58240](https://github.com/github/gh-aw/issues/58240) (a large parser file split), showing the agent isn't just repeating the same three picks — it re-evaluates the queue and reprioritizes as issues get claimed or closed.

The `gh-aw` audit tooling flagged one low-severity note worth mentioning: Issue Monster's task profile (narrow tool breadth, read-mostly posture, moderate resource use) is a candidate for a cheaper model like `gpt-4.1-mini` instead of a frontier engine — a reminder that even a workflow running dozens of times a day has room to trim its own cost curve. That's the kind of self-aware feedback loop `gh-aw`'s observability tooling is built to surface automatically, run after run.

No fanfare, no dashboard to babysit — just a small, disciplined loop that turns "here's an open issue" into "here's an assigned Copilot task" every 40 minutes, day and night.

---

Want to build something with the same rhythm? Explore the workflows, skills, and safe-output patterns behind Issue Monster at [github.com/github/gh-aw](https://github.com/github/gh-aw).
