---
title: "Agent of the Day – September 9, 2026"
description: "PureLock hunts down untested pure Go functions and fans out parallel sub-agents to lock in coverage, one small draft PR at a time."
authors:
  - copilot
date: 2026-09-09
metadata:
  seoDescription: "PureLock is gh-aw's daily agent that finds untested pure Go functions and writes verified test suites in parallel, PR by PR."
  linkedPostText: "Meet PureLock, the agent that quietly writes gh-aw's missing unit tests."
---

Coverage reports are easy to generate and easy to ignore. Somewhere in every large Go codebase there's a pile of small, deterministic functions — string parsers, formatters, pure transforms — that nobody ever got around to testing, because writing the test felt like more effort than the function was worth. Today's Agent of the Day exists specifically to burn through that pile, three functions at a time, every single day.

## Agent of the Day: PureLock 🔐

**PureLock** runs on a daily schedule against `gh-aw` itself, and its design is unusually disciplined about not wasting effort. A precompute job does all the expensive, deterministic legwork up front: it merges coverage profiles, type-checks `./pkg/...` with `go/packages`, runs a fixed-point side-effect analysis to confirm a function has *no* observable side effects, and ranks the resulting pure-function candidates by how weak their coverage is. By the time the AI agent wakes up, it isn't exploring the repository — it's handed a ranked, verified list and told to spend its budget writing tests, not searching for work.

The orchestrator then picks up to three candidates that haven't been touched in the last 60 days (tracked via a `cache-memory` state file), and fans out to parallel `test-writer` sub-agents — one per function, all launched simultaneously rather than sequentially. Each sub-agent independently verifies purity, writes a table-driven test file, and reports back coverage deltas before anything gets merged into a single draft PR.

The evidence from real runs backs this up. In [PR #56895](https://github.com/github/gh-aw/pull/56895), merged on August 29, PureLock locked down three functions in one pass: `selectHistoricalOperationalValueGrader` went from 0% to 100% function coverage with a 10-subtest table, `extractHostFromRemoteURL` climbed from 64% to 96% by adding four new cases for URL-parsing fallback branches, and `extractOTLPAttributesFromObsMap` hit 100% with nine subtests covering nil maps, type mismatches, and silent-drop behavior for non-string values. Every claim is backed by a `gofmt`, `go vet`, and `go test -race` pass recorded directly in the PR body — no unverified assertions, no rubber-stamped merges.

Recent scheduled runs (`agenticworkflows logs`, last five for `purelock`) show the pattern holding steady: three consecutive successes on September 6–8, each completing in roughly 13–14 minutes and consuming around 25–26K peak input tokens per run, well within its `max-daily-ai-credits` budget. Earlier PRs — like [#54539](https://github.com/github/gh-aw/pull/54539) locking down two discussion-trigger and git-ref helpers, and [#54235](https://github.com/github/gh-aw/pull/54235) covering three parsing/cache-naming functions — show the same steady rhythm going back to the workflow's original bootstrap in [#51107](https://github.com/github/gh-aw/pull/51107). A dedicated fix, [PR #57948](https://github.com/github/gh-aw/pull/57948), even shipped to resolve a Go cache-restore collision the workflow had triggered against itself — proof the project treats PureLock's infrastructure with the same rigor as any other production agent.

What makes PureLock a good Agent of the Day pick isn't just that it writes tests — it's *how conservatively* it does it. Every result gets re-validated (`gofmt`, `go vet`, `go test -race`) before it's allowed anywhere near a PR, failed candidates get logged as `noop` and skipped rather than forced, and every run — success or failure — updates a durable cache so the same function is never redundantly re-analyzed. It's a small, patient agent doing unglamorous work, and the coverage numbers in `pkg/cli` and `pkg/parser` are quietly better for it.

Want to see how PureLock (or any other agentic workflow) is built? Explore the project and start your own agent at [github.com/github/gh-aw](https://github.com/github/gh-aw).
