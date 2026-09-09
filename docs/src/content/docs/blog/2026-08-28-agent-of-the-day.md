---
title: "Agent of the Day – August 28, 2026"
description: "Meet ESLint Refiner, the workflow that reviews gh-aw's own custom lint rules for correctness — and caught a wording bug quietly recurring across eleven files."
authors:
  - copilot
date: 2026-08-28
metadata:
  seoDescription: "ESLint Refiner reviews its own lint rules daily, catching an overclaim that recurred across 11 files and a misclassified literal bug, filing real issues."
  linkedPostText: "The linter that audits its own rulebook, one overclaim at a time."
---

Most teams write lint rules once and trust them forever. Nobody goes back to ask whether the rule's own error message is still telling the truth, or whether a fix applied to one rule ever made it to its dozen siblings. Today's spotlight, **ESLint Refiner**, exists precisely for that blind spot: a daily workflow that treats `gh-aw`'s custom ESLint rule set — `eslint-factory` — as a codebase worth auditing in its own right, not just a tool you point at other code.

## Agent of the Day: ESLint Refiner

`eslint-factory` is the internal library of custom lint rules that keep `actions/setup/js` scripts safe — things like "wrap this `fs.mkdtempSync` call in a try/catch" or "don't compare objects with `JSON.stringify` equality." Rules like these accumulate fast; a sibling workflow, `eslint-miner`, adds roughly one new rule a day. But nobody was reviewing whether the rules themselves stayed correct as the pile grew. ESLint Refiner picks two of the least-scrutinized rules each run, checks their logic against real call sites in the codebase, and only files an issue when it finds something grounded in an actual bug — not a hypothetical.

In its [August 27 run](https://github.com/github/gh-aw/actions/runs/33050923118), the agent reviewed `require-mkdtempsync-try-catch` and `require-decodeuricomponent-try-catch`, and turned up two real problems:

1. **A recurring overclaim.** Eleven rules — including both reviewed that day — say a call "will crash the action if unhandled." That's not quite true: every entrypoint in `actions/setup/js` already has a top-level try/catch that routes any uncaught throw into a controlled `core.setFailed`, so nothing actually crashes silently. The real cost of skipping the fix is losing a specific `{ cause }` and message, not crashing. This exact wording had already been fixed once, for `require-fetch-response-body-try-catch`, but the fix never propagated — and had since recurred in two brand-new rules. The agent filed [issue #56288](https://github.com/github/gh-aw/issues/56288) asking for a reword-all pass plus a guard so it can't quietly recur a third time.
2. **A misclassified literal.** `require-decodeuricomponent-try-catch` only recognized string literals as provably safe arguments, so calls like `decodeURIComponent(42)` or `decodeURIComponent(null)` got flagged even though a number, boolean, or `null` can never produce a decoding error. Zero live call sites hit this today, but it's a cheap, well-scoped fix worth closing before one does — filed as [issue #56289](https://github.com/github/gh-aw/issues/56289).

Rather than silently move on, the agent also published a same-day [discussion post](https://github.com/github/gh-aw/discussions/56290) summarizing exactly what it checked, what came back clean, and what's queued for tomorrow's review — including a note that its own repo-memory had gone stale for weeks even while it kept filing issues, which it then rebuilt from a ground-truth GitHub search.

The following day's run, on [August 28](https://github.com/github/gh-aw/actions/runs/33152652663), kept the streak going with another clean pass — no errors, three safe outputs produced, business as usual for a workflow that's quietly been doing this every day since it launched.

What makes ESLint Refiner worth spotlighting isn't flashy output — it's discipline. It doesn't flag speculative issues; it grounds every finding in live call sites before filing, and it explicitly tracks precedent (citing the earlier fix for the same defect class) so fixes actually propagate instead of getting re-invented rule by rule.

---

Curious how workflows like ESLint Refiner are built? Explore the project at [github.com/github/gh-aw](https://github.com/github/gh-aw).
