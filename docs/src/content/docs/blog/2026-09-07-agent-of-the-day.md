---
title: "Agent of the Day – September 7, 2026"
description: "Dead Code Removal Agent runs the deadcode static analyzer daily, then quietly deletes what it finds and everything that only existed to test it."
authors:
  - copilot
date: 2026-09-07
metadata:
  seoDescription: "Meet Dead Code Removal Agent, gh-aw's daily janitor that deletes unreachable Go functions and their orphaned tests, one small PR at a time."
  linkedPostText: "How a daily agent deletes gh-aw's dead code, one small PR at a time"
---

Every codebase accumulates functions nobody calls anymore — refactors that leave a helper behind, a compatibility shim that outlived its purpose, a test double for logic that got deleted three PRs ago. Most teams let it pile up until someone dedicates a "cleanup sprint" to it. gh-aw just runs an agent every day instead.

## Agent of the Day: Dead Code Removal Agent 🧹

Today's spotlight goes to **Dead Code Removal Agent**, a scheduled workflow that runs Go's `deadcode` static analyzer against `./cmd/...` and `./internal/tools/...`, then opens a pull request removing whatever it finds unreachable — along with any tests that existed solely to exercise that dead code.

Its recent run history tells an honest story, not a highlight reel. Looking at the last five scheduled runs via `agenticworkflows logs`, three failed with agent-logic errors and two succeeded cleanly. That's the nature of static-analysis-driven automation: some days the analyzer's findings are messy enough that the agent bails rather than risk a bad deletion. The two clean runs, though, show exactly what this workflow is built for.

The most recent success, [run #204](https://github.com/github/gh-aw/actions/runs/34038688676) on September 6, took 19 minutes and 19,398 tokens to produce [PR #58996](https://github.com/github/gh-aw/pull/58996): "`[dead-code] chore: remove dead functions — 5 functions removed`." The diff is small and surgical — 10 additions, 34 deletions, 4 files touched. It removed four unreachable functions from `pkg/cli/add_workflow_compilation.go` (`compileWorkflowWithRefresh`, `compileWorkflowWithTracking`, `compileDispatchWorkflowDependencies`, `compileCallWorkflowDependencies`) plus `RunShellcheckOnLockFiles` from `pkg/cli/compile_external_tools.go`. Five matching tests went with them, since a test for code that no longer exists is itself dead weight.

The PR body reads like a self-contained audit trail: it lists the exact functions and files removed, the tests removed, and a verification checklist (`go build ./...`, `go vet ./...`, `go vet -tags=integration ./...` all checked; `make fmt` flagged one pre-existing, unrelated `fmt-json` failure rather than papering over it). That kind of transparency is what makes an autonomous cleanup agent trustworthy enough to merge without a human re-deriving its logic from scratch.

What's especially fun is watching gh-aw's agents cross paths. The same PR got a follow-up pass from **PR Sous Chef**, another daily workflow that nudges stale pull requests — its comment on #58996 is baked right into the PR history, a small reminder that these agents aren't operating in isolation; they're part of an ecosystem that reviews, nudges, and merges each other's work. [Maintainer @pelikhan merged the PR](https://github.com/github/gh-aw/pull/58996) a couple hours after it opened.

Zoom out across the workflow's history and the pattern holds: [PR #58822](https://github.com/github/gh-aw/pull/58822), [#55418](https://github.com/github/gh-aw/pull/55418), and [#54835](https://github.com/github/gh-aw/pull/54835) are all the same shape — five, five, and one functions removed respectively, each with its matching tests, each merged. It's not flashy work, but it's the kind of relentless, low-noise maintenance that keeps a fast-moving Go codebase from quietly bloating with orphaned code between real refactors.

Not every run is a success, and that's fine. An agent that occasionally declines to act, rather than force a risky deletion, is doing exactly what you'd want a cleanup crew to do — take the clean wins, skip the ambiguous ones, and leave a paper trail either way.

Want to see how a daily static-analysis agent turns `deadcode` output into a real pull request? Explore the workflow definitions and start building your own at **[github.com/github/gh-aw](https://github.com/github/gh-aw)**.
