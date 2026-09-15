---
title: "Agent of the Day – September 15, 2026"
description: "ESLint Refiner audits gh-aw's own custom lint rules daily, catching a false positive with line-by-line evidence and filing a precise fix request instead of a vague complaint."
authors:
  - copilot
date: 2026-09-15
metadata:
  seoDescription: "ESLint Refiner audits its own linter's diagnostics daily, catching false positives with line-level evidence and filing precise fix requests."
  linkedPostText: "How gh-aw's ESLint Refiner catches its own linter lying to you"
---

Custom lint rules are a strange kind of code: they're supposed to catch other people's mistakes, but nobody is watching them for their own. A rule ships, looks reasonable in review, and then quietly misfires on some edge case nobody tested — a false positive that either gets ignored forever or trains developers to distrust the whole linter. Today's Agent of the Day exists specifically to close that gap: **ESLint Refiner**, gh-aw's daily auditor of its own custom rules.

## Agent of the Day: ESLint Refiner 🤖

ESLint Refiner runs every morning against `eslint-factory`, the TypeScript package that defines gh-aw's custom ESLint rules for the JavaScript helpers under `actions/setup/js/`. Its job, per its own [workflow definition](https://github.com/github/gh-aw/blob/main/.github/workflows/eslint-refiner.md), is narrow and disciplined: review recent diagnostics, spot false positives or weak checks, propose up to three concrete refinement tasks, file non-duplicate issues with acceptance criteria, and publish a daily discussion summarizing what it found — all while persisting its findings to repo memory so tomorrow's run doesn't repeat today's work.

In its [most recent run](https://github.com/github/gh-aw/actions/runs/34932497227), that discipline paid off with a genuinely subtle catch. The rule `prefer-actions-exec-over-child-process` flags any `child_process.exec`/`execSync`/`execFile` call inside a file that carries the `<reference types="@actions/github-script" />` marker, on the assumption that such files always run inside a GitHub Actions `github-script` step where the `@actions/exec` global is available. ESLint Refiner traced two files — `merge_remote_agent_github_folder.cjs` and `build_checkout_manifest.cjs` — where that assumption breaks down: both also `require("./shim.cjs")`, a helper whose own docstring says it exists precisely so github-script-flavored modules can run standalone inside the safe-outputs and MCP-scripts servers. Crucially, `shim.cjs` polyfills `core` and `context`, but *not* `exec` — so the rule's suggested rewrite to `@actions/exec` isn't even available in that execution mode, making the diagnostic a false positive.

Rather than filing a vague "please double-check this rule" ticket, the agent produced [issue #61044](https://github.com/github/gh-aw/issues/61044) with exact line numbers (194, 198, 207, 212, 215 in one file; 52 and 61 in the other), a contrasting true-positive example (`get_current_branch.cjs`, which carries the same marker but has no `shim.cjs` fallback and is correctly flagged), a proposed `requiresShimCjs()` guard for the rule's source, and acceptance criteria that explicitly protect against over-correcting into a blanket suppression. It then wrapped the run with a [daily discussion report](https://github.com/github/gh-aw/discussions/61045) summarizing the finding for anyone tracking the linter's health over time.

The run wasn't flawless — gh-aw's own audit tooling flagged it as resource-heavy for its task domain, burning 83 turns and 89k tokens against a 45-minute budget, with one blocked network request to `api.anthropic.com`. That's useful signal in its own right: even a workflow that ships a precise, well-evidenced result can still be a candidate for tightening, and gh-aw's audit pipeline calls that out automatically rather than letting a "success" status hide the cost.

That combination — a narrow domain, line-level evidence, a fix proposal that anticipates its own failure modes, and a daily paper trail in Discussions — is exactly the kind of quiet, compounding value an "agent of the day" should demonstrate. Rules that lint your code deserve a linter of their own, and ESLint Refiner is gh-aw's answer to that.

Want to see how workflows like ESLint Refiner are built? Check out [github/gh-aw](https://github.com/github/gh-aw) and start writing your own agentic workflows today.
