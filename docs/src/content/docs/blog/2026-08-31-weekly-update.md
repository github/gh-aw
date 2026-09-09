---
title: "Weekly Update – August 31, 2026"
description: "Three v0.87.x pre-releases landed this week alongside intent-driven workflow design guidance, typed cooldown gating, and a wave of trajectory grader implementations."
authors:
  - copilot
date: 2026-08-31
metadata:
  seoDescription: "gh-aw weekly update: v0.87.9 pre-releases, intent-driven workflow design, on.cooldown gating, and new trajectory graders."
---

Another packed week for [github/gh-aw](https://github.com/github/gh-aw)! We shipped three pre-releases (`v0.87.5`, `v0.87.8`, and `v0.87.9`) and merged well over a hundred pull requests spanning new workflow scheduling controls, compiler hardening, and a steady stream of trajectory grader implementations. Here's what stood out.

## Release Highlights

The [v0.87.9](https://github.com/github/gh-aw/releases/tag/v0.87.9) line of pre-releases built on [v0.87.8](https://github.com/github/gh-aw/releases/tag/v0.87.8) and [v0.87.5](https://github.com/github/gh-aw/releases/tag/v0.87.5), focused on new workflow gating primitives, engine reliability, and internal grader tooling.

- **`on.cooldown` workflow gating** ([#56998](https://github.com/github/gh-aw/pull/56998)): workflows can now declare a cooldown window so back-to-back triggers don't pile up on the same target.
- **Typed `on.stop-after` field** ([#56983](https://github.com/github/gh-aw/pull/56983)): `stop-after` now accepts GitHub Actions expressions in addition to static values, giving authors more flexible run-limiting logic.
- **Codex harness tool-schema diagnostics** ([#57256](https://github.com/github/gh-aw/pull/57256)): unsupported-model tool-schema failures now surface with a dedicated, readable error message instead of a cryptic provider error.
- **Bump default MCP Gateway to v0.4.14** ([#57188](https://github.com/github/gh-aw/pull/57188)) and **Bump Agentic Workflow Firewall to v0.28.10** ([#56914](https://github.com/github/gh-aw/pull/56914)): the usual steady drumbeat of dependency upgrades keeping the sandboxing and networking layers current.

## Notable Pull Requests

- [Document intent-driven workflow design](https://github.com/github/gh-aw/pull/57005): new guidance explains how to write workflow prompts around clear intent rather than rigid step-by-step instructions, complementing the earlier [intent-driven workflow design guidance](https://github.com/github/gh-aw/pull/56611) and the optional `intent` frontmatter field ([#56599](https://github.com/github/gh-aw/pull/56599)).
- [Add the Feature Farmer workflow pattern](https://github.com/github/gh-aw/pull/56614): a new documented pattern for workflows that continuously grow small, incremental features — later put into practice by [converting the trajectory grader workflow to the "all-you-can-eat" pattern](https://github.com/github/gh-aw/pull/56988).
- [Prevent large MCP query payloads from exceeding argument limits](https://github.com/github/gh-aw/pull/57253): a reliability fix so oversized MCP requests fail gracefully instead of silently breaking tool calls.
- [Support authenticated Agent Plugin installation from private repositories](https://github.com/github/gh-aw/pull/56505): opens up plugin installation for teams running private forks or internal plugin repos.
- [Add Bash support for Windows runners](https://github.com/github/gh-aw/pull/56508): another step toward first-class Windows runner support for agentic workflows.

Under the hood, the team also kept implementing new entries in the trajectory grader library — including [event-entropy-rate](https://github.com/github/gh-aw/pull/56464), [lempel-ziv-trajectory-complexity](https://github.com/github/gh-aw/pull/56972), [policy-near-miss](https://github.com/github/gh-aw/pull/56996), [exploration-error](https://github.com/github/gh-aw/pull/57087), and [exploitation-error](https://github.com/github/gh-aw/pull/57152) — building out a richer picture of how agents behave across runs.

## 🤖 Agent of the Week: AI Moderator

**AI Moderator** is the quiet gatekeeper that watches newly opened issues, comments, and pull requests for spam, AI-generated noise, and link spam, then quietly labels or hides what it finds.

This week it stayed busy on the front lines — triggered repeatedly across incoming issues and PRs, including runs tied to the Codex harness fix ([#57256](https://github.com/github/gh-aw/pull/57256)) and a caveman instruction-verbosity pass. It runs read-only by design (no write-capable safe outputs get exercised unless it actually flags something), which is exactly the kind of low-risk, always-on moderation you want watching your front door.

Its recent runs did turn up as reliability "failures" in our observability logs — a reminder that even the calmest bouncer occasionally needs a coffee break, or in this case, a closer look at run classification before we assume the worst.

💡 **Usage tip**: Because it runs read-only with `threat-detection: false` and tight per-window rate limits, `ai-moderator` is a solid template for any workflow that needs to watch high-volume public triggers (like `issues: opened` or `pull_request: opened` from forks) without risking runaway write actions.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/ai-moderator.md)

## Try It Out

Update to [v0.87.9](https://github.com/github/gh-aw/releases/tag/v0.87.9) and give `on.cooldown` or the new intent-driven design guidance a try. As always, feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
