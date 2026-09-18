---
title: "Agent of the Day – June 1, 2026"
description: "Architecture Guardian: a scheduled agentic workflow that detects code structure violations before they become load-bearing."
authors:
  - copilot
date: 2026-06-01
metadata:
  seoDescription: "Architecture Guardian runs daily gh-aw checks for large files, oversized functions, and import cycles to keep Go and JavaScript code structurally healthy."
  linkedPostText: "Catch architectural drift automatically with Architecture Guardian"
---

> [!NOTE]

## 🏗️ Agent of the Day: Architecture Guardian

Architectural drift is quiet and cumulative. A file grows past 600 lines. A function absorbs one more responsibility. An import cycle sneaks in between two packages that "just need to share a little logic." None of it trips a CI gate, no test turns red, and six months later a new engineer opens that directory and wonders how it got this bad. The Architecture Guardian workflow exists precisely to interrupt that pattern before it becomes load-bearing.

### What It Does

The Architecture Guardian runs on a weekday schedule, firing each afternoon around 14:00 UTC. It pulls the last 24 hours of commits, walks every changed Go and JavaScript file, and applies a tiered set of structural checks:

- **File size**: files over 500 lines generate a warning; over 1,000 lines, a blocker.
- **Function length**: any function exceeding 80 lines is flagged.
- **Export count**: more than 10 exports from a single file draws scrutiny.
- **Import cycles**: the full dependency graph of changed packages is traced for cycles.

When violations surface, the workflow doesn't just log and move on. It opens a GitHub issue labeled `architecture`, `automated-analysis`, and `cookie`, assigned directly to Copilot for triage. The issue is the artifact — something a team can discuss, link to a PR, close when remediated.

The engine is GitHub Copilot, running as an agentic workflow defined in [`architecture-guardian.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/architecture-guardian.md). No bash scripts wrapping static analysis tools, no bespoke CI job to maintain. The analysis logic, thresholds, and issue-creation behavior all live in a single, readable workflow spec.

### The June 1 Run

[Run 26766995181](https://github.com/github/gh-aw/actions/runs/26766995181) completed on June 1, 2026 at 16:18 UTC, five minutes and forty seconds after it started. The agent worked through three turns with `claude-sonnet-4.6` via GitHub Copilot, made 10 GitHub API calls, and consumed 125,356 tokens — a number that looks large until you factor in the effective token count of 1,206,982 once prompt caching is included. Caching is doing real work here.

The verdict: no violations. Every changed file over the past 24 hours fell within the configured thresholds. The agent's own summary put it plainly — *"0 files analyzed, no import cycles detected."* Nothing to open, nothing to assign.

That outcome is worth pausing on. A clean run isn't a null result; it's confirmation. The codebase was touched, the guardian looked, and the boundaries held. Knowing that with specificity — on a schedule, with a receipt — is materially different from assuming it because nothing has caught fire yet.

### Why the Thresholds Matter

The 500-line warning and 1,000-line blocker aren't arbitrary. Files in that range have a documented tendency to accumulate mixed responsibilities: they're long because they're doing too many things, not because the domain is genuinely complex. The 80-line function limit enforces a similar discipline. It's not a style preference; it's a forcing function for decomposition.

Export counts above 10 are a softer signal — a package with 15 exports might be perfectly well-structured — but they surface files worth a second look. Import cycles are harder: they indicate a structural coupling that can't be resolved without a real refactor, and they compound over time.

The Architecture Guardian makes these checks automatic and visible without requiring anyone to remember to run a linter or build a policy around code review checklists. The standards are encoded in the workflow. The workflow runs whether or not anyone's thinking about it.

### Grounded Takeaways

A few things worth noting if you're thinking about adapting this pattern for your own team:

**Scheduling matters.** A daily check at 14:00 UTC catches violations before they're a day old. Violations that linger for a week become rationalizations.

**Issue creation is the accountability loop.** Logging a warning to stdout is easy to ignore. An open issue is harder to lose, links to the violating commit, and can be closed with a reference to the fixing PR. That chain is the point.

**Clean runs are data.** The June 1 run found nothing. That's not a failure of the workflow — it's the workflow confirming steady-state health. Over time, a history of clean runs punctuated by occasional issues tells you something real about your team's structural discipline.

**Token efficiency scales.** 1.2 million effective tokens for a daily architectural scan, amortized across a codebase's active lifetime, is not expensive. The cost of a missed import cycle or a 2,000-line God file is.

---

The Architecture Guardian is one of the workflows available in [github/gh-aw](https://github.com/github/gh-aw). If your team is dealing with structural drift — or wants to make sure it never starts — the repository has the workflow definitions, the engine configuration, and the patterns to adapt it to your thresholds and language stack.
