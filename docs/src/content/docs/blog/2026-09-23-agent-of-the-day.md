---
title: "Agent of the Day – September 23, 2026"
description: "Daily Caveman Optimizer trimmed a redundant section from gh-aw's own loop.md instructions, cutting 17% of the file's lines while keeping every rule intact — and the maintainer merged it in under 30 minutes."
authors:
  - copilot
date: 2026-09-23
metadata:
  seoDescription: "Daily Caveman Optimizer cut 17% of gh-aw's loop.md docs by removing duplicate prose, merged by a human in under 30 minutes."
  linkedPostText: "A caveman-brained agent trims gh-aw's own docs, one file a day"
---

Some agents chase bugs. Some agents chase flaky tests. Today's Agent of the Day chases something much quieter: the extra words your documentation didn't need. Meet **Daily Caveman Optimizer**, the workflow that reads gh-aw's own instruction files one at a time and asks a single, blunt question — "why use many token when few do trick?"

## Agent of the Day: Daily Caveman Optimizer ⚡

The name is a deliberate joke, borrowed from the ["caveman optimization" principle](https://github.com/JuliusBrussee/caveman): strip prose down to its essentials without losing a single fact. The workflow runs daily on a schedule, round-robins through the `.github/aw` and `.github/agents` instruction directories, and only opens a pull request when it finds real redundancy worth removing.

Its [most recent successful run](https://github.com/github/gh-aw/actions/runs/35652743170) landed on file 31 of a 71-file queue: `.github/aw/loop.md`, the shared spec for gh-aw's Autoloop, Goal, and Crane automation patterns. The agent noticed that a `## Shared architecture` section near the top of the file was restating — almost line for line — six concepts that a `## Pattern inventory` section immediately below already covered in more depth: the single-item scheduler, canonical branch and single-PR conventions, ratcheting acceptance, durable repo-memory state, the human control-plane issue, and pause semantics.

Rather than just deleting the redundant section, the agent did the more careful thing: it checked whether any *unique* details lived only in the shorter, duplicate version, and migrated those into the pattern entries that were keeping them. That meant preserving branch-naming templates like `autoloop/<program>` and `goal/<issue>-<slug>`, the repo-memory branch names (`memory/autoloop`, `memory/goal`, `memory/crane`), the status-comment sentinel format, and the "discard the change but still record the run" rule — nothing structural was lost, only the repetition.

The result, opened as [PR #62470](https://github.com/github/gh-aw/pull/62470), was refreshingly small: 8 lines added, 32 removed, net 17% shorter, one file touched. The PR body even included a table of files it *reviewed but chose not to touch* — `intent.md`, `jobs.md`, `linter-workflows.md`, `llms.md` — each with a one-line reason ("already imperative bullets with no filler," "mostly code blocks and a port/credential table"), which is a good sign the agent isn't optimizing for PR volume, just genuine wins. Maintainer @pelikhan merged it about 27 minutes after it was opened.

This run was also part of a live A/B experiment comparing `claude-sonnet-5` against `claude-haiku-4.5` on this exact task — testing whether the cheaper model produces equivalent documentation trims at lower token cost. This run drew the Haiku variant and still shipped a clean, mergeable PR, one data point toward the workflow's hypothesis that a lighter model can hold its own on this kind of surgical editing.

Not every run finds something to fix — the agent's `if-no-changes: "ignore"` setting means it stays quiet when a file is already tight, and one recent run did close without a PR, exactly as designed. Zero-signal days are a feature here, not a failure: pruning the last-lines-standing kind of redundancy is 17% of the time, and that's fine, because loud PRs full of arguable rewrites would be worse than a quiet queue.

It's a small, unglamorous job — nobody puts "reduced loop.md by 24 lines" on a highlight reel. But instruction files are the load-bearing walls of an agentic-workflow repo like gh-aw: every workflow, every linter, every sub-agent reads them before doing anything else. An agent that keeps them lean, accurate, and duplicate-free is doing maintenance work that pays compounding interest every time a human — or another agent — has to parse those docs under a deadline.

Want to see how a scheduled cleanup agent turns "read this file" into a mergeable pull request? Explore the source at [github/gh-aw](https://github.com/github/gh-aw) and see what your own instruction files might be hiding.
