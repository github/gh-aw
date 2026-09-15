---
title: "Agent of the Day – May 28, 2026"
description: "How the Dead Code Removal Agent quietly hit run #100 — finding and deleting a redundant Go wrapper function, running full verification, and opening a clean PR."
authors:
  - copilot
date: 2026-05-28
metadata:
  seoDescription: "How GitHub's Dead Code Removal Agent uses Copilot CLI and GitHub Actions to find and remove unused Go code, culminating in its run #100 milestone."
  linkedPostText: "Dead Code Removal Agent hits 100 automated runs"
---

> [!NOTE]

Every codebase accumulates sediment. A helper function that made sense six months ago. A wrapper that lost its reason to exist after a refactor. Nobody deletes it on purpose — it just lingers. In Go, that lingering costs you: extra surface area to maintain, test coverage for code that does nothing new, and cognitive overhead for every engineer who reads the file.

The **Dead Code Removal Agent** is a scheduled GitHub Actions workflow that runs daily on the `gh-aw` repository. Its job is simple: find unused code, verify nothing breaks, and open a pull request. No human intervention required until review time.

## Agent of the Day

### Run #100 — A Quiet Milestone

On May 27, 2026, the agent completed [run #100](https://github.com/github/gh-aw/actions/runs/26520529392). Not a fanfare moment — just another daily run doing exactly what it was built to do. It finished in **11.4 minutes** across **5 turns**, consumed **14.6M effective tokens**, and used **12 GitHub Actions minutes**.

The target this time was `NewValidationErrorWithLocation` in `pkg/workflow/workflow_errors.go`. The function was a constructor wrapper around `WorkflowValidationError` — originally a convenience, but over time it became redundant as callers could initialize the struct directly. The agent identified it, confirmed it had no remaining callers, and started working.

The tool call sequence tells the story cleanly: one `Install`, eight `Check` passes, five `Read`s, three `View`s, four `Edit`s, a `Find`, a `Verify`, a `Format`, two `Run`s, two `Create`s, an `Update`, and a `Vet`. That's methodical, not mechanical. The agent didn't just delete the function — it removed the corresponding `TestNewValidationErrorWithLocation` test from `pkg/workflow/error_helpers_test.go` and updated `compiler_error_formatting_test.go` to use direct `WorkflowValidationError` struct initialization instead.

Verification was thorough. Before touching the PR, the agent ran `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...`, and `make fmt`. Everything passed. The resulting PR — **"chore: remove dead functions — 1 function removed"** on branch `chore/remove-dead-code-20260527` — arrived clean, with no lint issues and a test suite that still compiles.

### What Five Runs Look Like

Zoom out a week and the picture gets more interesting. Across five runs in the last seven days, the agent logged:

- **35.5 minutes** total duration
- **38.9M effective tokens**
- **38 GitHub Actions minutes**
- **21 turns** across all five runs
- **5 out of 5** high-confidence episodes

Run classification across that window: two normal runs, one risky, one failure, one in-progress. The failure and the risky classification matter as much as the successes. The agent doesn't always find something safe to remove, and when it can't complete cleanly, it doesn't force a PR. That restraint is a feature, not a gap.

### Why Automation Fits This Problem

Dead code removal is well-suited to an agent for a specific reason: the feedback loop is entirely mechanical. Does it build? Does `go vet` pass? Does the test suite still run? Those questions have definitive answers. The agent never has to speculate about intent — it just has to be rigorous about verification, which it is.

The harder editorial question — *should* this code be removed — is answered by the PR review. The agent does the investigation and the grunt work. Engineers do the judgment call. That division feels right.

There's also something useful about the daily cadence. A function doesn't become dead overnight. But catching it the morning after the last caller disappears, rather than six months later during a refactor, is the difference between a one-line deletion and an archaeology project.

## Get Involved

If you're curious about how the Dead Code Removal Agent is built, or if you want to run something similar against your own Go codebase, the workflow lives at [github/gh-aw](https://github.com/github/gh-aw). The patterns here — schedule-triggered agents, structured verification steps, PR-as-output — are composable. Start there.

Run #100 was just another Tuesday. That's the point.
