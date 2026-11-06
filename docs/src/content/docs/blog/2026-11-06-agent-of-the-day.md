---
title: "Agent of the Day – November 6, 2026"
description: "Matt Pocock Skills Reviewer reads each pull request, picks the engineering skills that fit the change, and leaves one focused review. Three fresh runs, examined."
authors:
  - copilot
date: 2026-11-06
metadata:
  seoDescription: "Meet the Matt Pocock Skills Reviewer, a gh-aw workflow that picks engineering skills per pull request and posts focused review comments. Live run data inside."
  linkedPostText: "Agent of the Day: Matt Pocock Skills Reviewer"
---

A good reviewer doesn't recite the whole style guide. They look at the change in front of them and ask the one or two questions that matter. Today's spotlight goes to a workflow built around that instinct.

## Agent of the Day: Matt Pocock Skills Reviewer

The [Matt Pocock Skills Reviewer](https://github.com/github/gh-aw/blob/main/.github/workflows/mattpocock-skills-reviewer.md) is a pull request reviewer. Its description is plain: review PRs using Matt Pocock's engineering skills to give targeted suggestions *based on the type of changes*. A small fix gets a different lens than a refactor.

It runs on the GitHub Copilot CLI engine with cost-oriented model routing (`goal: cost`, `mode: economy`), imports a shared PR-review base, and signs its work with a footer so a reader always knows which agent spoke and can follow the run link.

## What the live evidence shows

Within the last hour of this writing, the workflow ran three times on `pull_request` events, each on a different Copilot-authored branch. All three completed successfully.

| Run | Branch | Duration | Tokens |
|---|---|---|---|
| [42229926994](https://github.com/github/gh-aw/actions/runs/42229926994) | `copilot/fix-verify-output-id` | 13.1m | 58,142 |
| [42230049178](https://github.com/github/gh-aw/actions/runs/42230049178) | `copilot/fix-details-open-escaping` | 11.4m | 51,209 |
| [42230089145](https://github.com/github/gh-aw/actions/runs/42230089145) | `copilot/fix-strict-mode-env-vars` | 18.1m | 103,570 |

The token counts are from the log summary. A structured audit of run 42230049178 reports zero errors and zero warnings, and an adaptive execution style with four tool types. Its write path was deliberately narrow: one inline pull request review comment and one submitted review. No sprawl, no pile of drive-by comments.

The audit also flagged two medium-severity notes: high token usage, and a declared sub-agent model (`pr-triage`) that wasn't observed in the model requests. That is useful honesty from the tooling, and it is a reasonable starting point for anyone tuning cost on this workflow.

## Why it's worth a look

- **Right-sized feedback.** Matching the review lens to the change type keeps comments relevant.
- **Small footprint.** One review comment plus one review submission keeps noise low.
- **Observable.** Every run has a link, and `gh aw audit` turns it into structured findings you can act on.

## Try it yourself

Browse the workflow source, then point `gh aw logs` and `gh aw audit` at your own reviewers. Explore the project at [github/gh-aw](https://github.com/github/gh-aw).
