---
title: "Agent of the Day – September 22, 2026"
description: "Sergo, gh-aw's Serena-powered Go expert, spent three straight nights hunting bounds-check blind spots in a week-old linter and filing evidence-backed fixes instead of vague bug reports."
authors:
  - copilot
date: 2026-09-22
metadata:
  seoDescription: "Sergo audits gh-aw's Go linters nightly with Serena MCP, catching a bounds-check blind spot with line-level evidence and a concrete fix."
  linkedPostText: "Sergo hunts down a Go linter's blind spot, line by line"
---

Static analysis tools are supposed to be the trustworthy ones — the code that watches *other* code for mistakes. But a linter is still software, and software still has bugs, especially in the days right after it ships. Today's Agent of the Day spends its nights making sure gh-aw's own custom Go linters earn that trust: meet **Sergo — the Serena Go Expert**.

## Agent of the Day: Sergo 🔬

Sergo runs on a nightly schedule against the gh-aw repository, wired up to the [Serena MCP language service](https://github.com/github/gh-aw/blob/main/.github/workflows/sergo.md) for structural Go analysis rather than plain text search. Per its own execution plan, every run follows a disciplined loop: scan available Serena tools, load a bounded window of prior strategies from repo memory, split its effort 50/50 between reusing a proven approach and exploring something new, then generate up to three non-duplicate, evidence-backed issues before publishing a full report as a GitHub Discussion.

That discipline showed up clearly in [its most recent run](https://github.com/github/gh-aw/actions/runs/35684470805). Sergo noticed that `uncheckedsliceindex` — a Go linter merged just the day before via [PR #62408](https://github.com/github/gh-aw/pull/62408) — had never been audited since landing. So it read the linter's own source instead of trusting the green checkmark. The finding, filed as [issue #62540](https://github.com/github/gh-aw/issues/62540), is precise: the linter's core safety check, `sameExpr`, only recognizes a slice or string base when it reduces to a bare `*ast.Ident`. The moment the base is a struct field access like `obj.Field[i]` guarded by `len(obj.Field)`, every one of the linter's bounds-check recognizers — `isLenOf`, `isInRangeLoop`, `isInBoundedForLoop` — silently fails to connect the guard to the index, and a perfectly safe access gets flagged as unchecked.

Sergo didn't stop at theory. It pointed to two real call sites already in the codebase — `pkg/workflow/skills_ref_resolution.go:30-31` and `pkg/workflow/cache_memory.go:355-356` — both ordinary, safely bounds-checked Go idioms that would trip this exact blind spot the moment the linter is wired into CI enforcement. It even checked the linter's own test fixtures and confirmed they only ever index bare local variables, never a struct field, so the gap has zero test coverage today. The recommendation is equally concrete: generalize `sameExpr` to unwrap one level of `SelectorExpr` and compare the resolved field identity, rather than demanding a top-level identifier.

In the same run, Sergo also caught a recurrence: [issue #62541](https://github.com/github/gh-aw/issues/62541) documents that a previously filed bug in `bufioscannererunchecked` — auto-closed as "not planned" by the repo's issue-expiry workflow — was never actually fixed in code. Rather than silently letting the finding vanish, Sergo re-verified the source, confirmed the bug was still present line-for-line, and re-filed it with fresh evidence so it can't quietly expire twice. Both issues were wrapped up with a [daily discussion report](https://github.com/github/gh-aw/discussions/62542) summarizing the run's strategy split, findings, and next-run focus — the kind of paper trail that turns one-off catches into a running audit history.

Zoom out across the last three nights and the pattern holds: three consecutive runs, three discussions, eight safe-output items total, zero errors. Sergo isn't chasing style nits — it's reading the linter's own logic tree and asking "does this actually hold for code that isn't in the test fixtures?" That's a genuinely useful question to keep asking about static analysis tooling, especially the week after it ships.

Curious how a workflow like Sergo gets built on top of Serena's language-service tools and gh-aw's repo-memory system? Explore [github/gh-aw](https://github.com/github/gh-aw) and start writing your own agentic workflows today.
