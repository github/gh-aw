---
title: "Agent of the Day – September 11, 2026"
description: "Meet jsweep, gh-aw's daily agent that types and modernizes one JavaScript helper file at a time — and knows when to walk away empty-handed."
authors:
  - copilot
date: 2026-09-11
metadata:
  seoDescription: "jsweep is gh-aw's daily agent that removes @ts-nocheck and modernizes one JavaScript helper file a day, judging each file before touching it."
  linkedPostText: "The daily agent that turns gh-aw's untyped JS into typed, modern code"
---

Most codebases have a folder like `actions/setup/js/` — a drawer of small CommonJS helpers, glue scripts, and github-script snippets that accumulated over time, half of them still marked `@ts-nocheck` because nobody had a spare afternoon to fix the types. gh-aw runs a daily agent whose entire job is to whittle that pile down, one file at a time, forever.

## Agent of the Day: jsweep, the JavaScript Unbloater

**jsweep** ([workflow source](https://github.com/github/gh-aw/blob/main/.github/workflows/jsweep.md)) is deliberately narrow in scope: clean exactly one `.cjs` file per day from `actions/setup/js/`, prioritizing anything still hiding behind `@ts-nocheck`. It runs on a daily schedule with a `copilot` engine, a TypeScript language server wired in via LSP, and persistent cache-memory so it never repeats a file it already handled — tracked in a `jsweep-state.json` round-robin ledger.

What makes jsweep interesting isn't the cleanup itself — modernizing `var` into `const`, replacing manual loops with `map`/`reduce`, trimming try/catch blocks that don't actually handle anything — it's the discipline built into the process. Before touching a single line, jsweep dispatches a `file-triage` sub-agent that reads only the first 80 lines of the candidate file and returns a compact verdict: is this `github-script` or plain Node.js context, does it have `@ts-nocheck`, does a matching test file exist, and — critically — is this actually worth cleaning up. If the sub-agent says `noop`, jsweep stops immediately rather than burning context reading a file that doesn't need it.

### What the logs show

Today's run ([Action run #34559616639](https://github.com/github/gh-aw/actions/runs/34559616639)) finished in under 8 minutes, used 336K tokens, and completed the whole loop in just **2 turns** — a sign the triage sub-agent did its job well and jsweep made a fast, clean decision rather than wandering through the file tree. Zero errors, zero warnings, and by design no pull request was opened this time: no PR search turns up a `[jsweep]`-titled change for today's run, which is the expected outcome whenever the triage step decides a file isn't worth the diff.

That restraint is the point. Looking back over jsweep's history, its merged output speaks for itself — recent examples include [#54427: Clean run_validate_workflows.cjs](https://github.com/github/gh-aw/pull/54427), which extracted duplicated "truncate then sanitize" logic into a single reusable helper, and [#52227: Clean validate_memory_files.cjs](https://github.com/github/gh-aw/pull/52227). Each PR is small, scoped, draft-by-default, and tagged `unbloat` + `automation` so reviewers know exactly what kind of change to expect before they open the diff.

### Built for low-stakes, high-frequency change

jsweep's `safe-outputs` configuration reinforces the same philosophy as its prompt: `create-pull-request` with `if-no-changes: ignore`, a 2-day expiry, and a forced `draft: true`. There's no pressure to ship a PR every run — if there's nothing worth changing, the workflow simply reports nothing and waits for tomorrow. That's a deliberate contrast to agents that feel obligated to produce *something* on every invocation; jsweep is comfortable coming up empty.

### Why this matters for gh-aw

Type-safety debt is exactly the kind of maintenance work that never wins a sprint-planning argument against a shiny new feature, yet it compounds daily. By capping itself at one file per day and gating every action behind a fast triage decision, jsweep turns "clean up the JS helpers" from a permanently-deferred chore into a background process that just runs — no code review fatigue, no giant refactor PR, just steady, reviewable progress.

### Try it yourself

The full workflow definition, including its cache-memory state machine and triage sub-agent prompt, lives at [`.github/workflows/jsweep.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/jsweep.md) in [github/gh-aw](https://github.com/github/gh-aw). Curious how to build your own daily, self-limiting cleanup agent? Explore the full catalog of agentic workflows at **[github.com/github/gh-aw](https://github.com/github/gh-aw)**.
