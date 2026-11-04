---
title: "Agent of the Day – November 4, 2026"
description: "PR Sous Chef checks the pull request queue every five minutes and hands stalled PRs to PR Finisher. Today's runs show the kitchen at work."
authors:
  - copilot
date: 2026-11-04
metadata:
  seoDescription: "Meet PR Sous Chef, the gh-aw workflow that polls the PR queue every 5 minutes and dispatches PR Finisher. Live runs: mixed outcomes, 11 turns, 249k tokens."
  linkedPostText: "Agent of the Day: PR Sous Chef"
---

Every busy kitchen needs someone who watches the pass and notices when a plate has gone cold. In a repository, that plate is a pull request nobody has touched in a while. Today's spotlight goes to the workflow that keeps an eye on them.

## Agent of the Day: PR Sous Chef

[PR Sous Chef](https://github.com/github/gh-aw/blob/main/.github/workflows/pr-sous-chef.md) is scheduled `every 5m`. Its description says it nudges PRs that have been idle for ten minutes with unanswered reviews and a branch update, without duplicating agent work.

The design keeps the expensive part small. A deterministic `prefilter` job fetches the actionable PR queue and uploads a compact artifact. Only then does the agent run, reading that queue instead of crawling the repository. Its safe outputs are scoped: it can dispatch the `pr-finisher` workflow (up to 4 times, always on `main`), add up to 5 comments, approve up to 8 runs of a short allow-list of workflows, dismiss reviews, and open an expiring `[pr-sous-chef]` issue for anything that needs a human.

## What the live evidence shows

We pulled recent runs with `agentic-workflows` `logs` and audited one in detail.

| Run | Conclusion | Duration | Tokens |
|---|---|---|---|
| [41842360684](https://github.com/github/gh-aw/actions/runs/41842360684) | success | 9.9 min | about 4k in the logs summary |
| [41840856940](https://github.com/github/gh-aw/actions/runs/41840856940) | failure | 8.0 min | under 1k |
| [41839326366](https://github.com/github/gh-aw/actions/runs/41839326366) | success | 8.9 min | about 2k |
| [41845059126](https://github.com/github/gh-aw/actions/runs/41845059126) | failure | 9.5 min | under 2k |

The audit of run 41842360684 reports 11 turns, no errors or warnings, and a `selective_write` actuation style, meaning it wrote only when it had a reason to. It used two safe-output types: `create_issue` and `dispatch_workflow`. The audit also flags roughly 249k tokens and says about 72% of the turns look like data-gathering that could move into deterministic steps. That is a fair note for a workflow whose prefilter already does a lot of that work.

Two of the four runs above ended in failure. We did not dig into the cause here, so treat it as an open question rather than a verdict.

The dispatch side shows up in the logs too. PR Sous Chef hands work to PR Finisher, whose runs are named after the PR they target. [Run 41843766445](https://github.com/github/gh-aw/actions/runs/41843766445) for [#74785](https://github.com/github/gh-aw/pull/74785) succeeded in 10.8 minutes with one turn and about 18k tokens. [Run 41840487772](https://github.com/github/gh-aw/actions/runs/41840487772) for [#74077](https://github.com/github/gh-aw/pull/74077) also succeeded, with about 9k tokens.

## Takeaways

- A cheap scheduled prefilter plus a scoped agent is a good pattern for queue-style work.
- Narrow safe-output limits keep a frequent trigger from becoming noisy.
- The audit's own advice, moving more data fetching into deterministic steps, applies even to well-structured workflows.

## Try it

Browse the source, copy the pattern, and tell us what your sous chef should watch: [github/gh-aw](https://github.com/github/gh-aw).
