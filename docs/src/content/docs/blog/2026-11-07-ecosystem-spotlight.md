---
title: "Ecosystem Spotlight: witt3rd/review-council"
description: "Five specialist gh-aw reviewers, packaged as reusable workflows that any repository can call with a thin, pinned caller."
authors:
  - copilot
date: 2026-11-07
metadata:
  seoDescription: "How witt3rd/review-council ships five focused AI PR reviewers as reusable gh-aw workflows with pinned callers, base-commit rules and one required status each."
  linkedPostText: "Ecosystem Spotlight: a reusable AI review council on gh-aw"
---

One AI reviewer that comments on everything tends to produce vague, overlapping feedback. [witt3rd/review-council](https://github.com/witt3rd/review-council) takes a different approach: five narrowly scoped reviewers, each a gh-aw agentic workflow compiled into a reusable workflow. A repository calls the reviewers it wants from a short caller file and gets one required commit status per reviewer on the pull request head. It is aimed at teams that want consistent, enforceable AI review across many repositories without copying prompts around.

## Why it's awesome

**Sharp scopes, one verdict each.** The council has a Steward (written law and PR claims), an Architect (layers and contracts), an Inspector (bugs), a Warden (abuse) and an Editor (prose, for documents repositories). Each workflow states what is *not* its job. The [Warden](https://github.com/witt3rd/review-council/blob/main/.github/workflows/warden.md), for example, requires every finding to name a concrete path: who sends what, and what they get that they should not. "No path, no finding" and no defence-in-depth requests "just in case" keeps the output actionable.

**A thin, pinned caller.** [caller/review-council.yml](https://github.com/witt3rd/review-council/blob/main/caller/review-council.yml) uses YAML anchors to share permissions, inputs and secrets across one job per reviewer. Each `uses:` is pinned to a release commit SHA with the tag as a comment, and a `pins.yml` workflow reports which callers lag behind the newest release.

**The consumer's law is read from the base commit.** According to [AGENTS.md](https://github.com/witt3rd/review-council/blob/main/AGENTS.md), the council reads the repository's `principles.md` and `AGENTS.md` from the PR's *base* commit, so a PR cannot rewrite the rules it is judged by. The council holds no key and no consumer's law: each caller supplies its own `OPENROUTER_API_KEY`.

**Defensive by default.** Reviewers only run on non-draft, same-repo pull requests, because fork PRs get no secrets. Permissions are read-only for contents and pull requests. Turns and minutes are bounded (`max-turns: 100`, `timeout-minutes: 40`), and a newer push cancels the review of the older head.

**Evaluated releases.** Releases are tagged only when an eval run on golden pull requests passes. The fixtures carry planted defects and even a prompt injection on purpose. The changelog records eval scores and spend; AGENTS.md cites roughly $0.80 for five reviewers on a small PR.

## How it works

Each reviewer is a `.md` source under `.github/workflows/` with an `on: workflow_call` trigger, compiled by a pinned gh-aw version into a `.lock.yml` that callers reference. The shared contract (review format, rounds, memory and the verdict rule) lives once in `.github/workflows/shared/`, so the five frontmatters are identical apart from the description, name and a `reviewer` input default.

Two details show real gh-aw experience. Every lock uses `inlined-imports: true`, because when called from another repository nothing from the council repository is on disk. And because `github.workflow` is the caller's name inside a called workflow, status names and concurrency IDs use the reviewer's fixed name, and the verdict is set on `pull_request.head.sha`. CI recompiles the locks and requires no diff.

## Try it

The adoption steps in AGENTS.md are short:

1. Copy `caller/review-council.yml` to `.github/workflows/review-council.yml` and keep the reviewers you want, pinned to a release SHA.
2. Copy `caller/principles.md` to `.github/review-council/principles.md` and write your own principles and checks.
3. Add the `OPENROUTER_API_KEY` repository secret.
4. Require each `<Reviewer> verdict` status in your branch ruleset.

Review the prompts and locks before adopting them, as you would any third-party workflow, and note that model spend bills to your repository.

## Learn more

Explore the [review-council repository](https://github.com/witt3rd/review-council), especially its contract and eval setup, if you are designing reusable agentic workflows. To build your own, start with [github/gh-aw](https://github.com/github/gh-aw).
