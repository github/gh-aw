---
title: "Weekly Update – September 7, 2026"
description: "v0.88.4 hardens the agentic firewall and CI reliability, plus new trusted enclave and DIFC policy support."
authors:
  - copilot
date: 2026-09-07
metadata:
  seoDescription: "gh-aw weekly update: v0.88.4 firewall hardening, trusted enclave sensitivity, DIFC policies, and Agent of the Week: dead-code-remover."
---

Another busy week for [github/gh-aw](https://github.com/github/gh-aw)! The team shipped a new release focused on hardening the agentic firewall and CI reliability, while dozens of pull requests tightened up sandboxing, model configuration, and safe-output handling across the fleet of agentic workflows.

## Release: v0.88.4

[v0.88.4](https://github.com/github/gh-aw/releases/tag/v0.88.4) landed this week, focused on hardening the agentic firewall/network layer, improving CI reliability, and expanding project tooling support.

### What's New

- **Trusted enclave sensitivity support** ([#58328](https://github.com/github/gh-aw/pull/58328)): adds finer-grained sensitivity controls for trusted enclave workflows.
- **DIFC policy generation for GitHub App workflows** ([#58302](https://github.com/github/gh-aw/pull/58302)): automatically generates data-flow integrity/confidentiality policies for workflows authenticated via a GitHub App.
- **`aw.json` project support in the `add` command** ([#58267](https://github.com/github/gh-aw/pull/58267)): makes it easier to add workflows into existing `aw.json`-based projects.
- **Daily Linear and Jira smoke issues workflow** ([#58320](https://github.com/github/gh-aw/pull/58320)): adds scheduled smoke-testing coverage for Linear and Jira integrations.

### Bug Fixes & Improvements

- Fixed root-relative workflow paths in imported local manifests ([#58317](https://github.com/github/gh-aw/pull/58317)).
- Fixed Pi Anthropic routing through the firewall ([#58313](https://github.com/github/gh-aw/pull/58313)).
- Disabled OTLP export when authorization secrets are empty, avoiding noisy failed exports ([#58312](https://github.com/github/gh-aw/pull/58312)).
- Preserved `setup-ruby` PATH precedence inside the Agentic Workflow Firewall ([#58311](https://github.com/github/gh-aw/pull/58311)).

## Notable Pull Requests

Beyond the release, the past week's merge queue was dominated by fleet-wide reliability work:

- **[Migrate agentic workflows to Cloud Hypervisor](https://github.com/github/gh-aw/pull/58726)**: a major infrastructure shift moving the workflow fleet onto Cloud Hypervisor-based microVMs for better isolation.
- **[Wire dynamic enclave delegation controller into the workflow runtime](https://github.com/github/gh-aw/pull/59046)**: lays groundwork for more flexible trusted-enclave delegation.
- **[Add compiler support for dynamic repository enclave policies](https://github.com/github/gh-aw/pull/58880)**: lets repository-level policies drive enclave behavior dynamically.
- **[Reclassify policy-driven safe-output declines as skipped, not hard failures](https://github.com/github/gh-aw/pull/58767)**: makes workflow runs easier to triage when a safe output is intentionally blocked by policy rather than broken.
- Several workflows got model-configuration fixes to stay on Codex-compatible models, including the [Daily Go Test Parallelizer](https://github.com/github/gh-aw/pull/58826), [Auto-Triage Issues](https://github.com/github/gh-aw/pull/58860), and [Linter Miner](https://github.com/github/gh-aw/pull/58864) — a good reminder to keep an eye on model deprecations across your own workflow fleet.

## 🤖 Agent of the Week: dead-code-remover

The tidiest member of the team — it scans the Go codebase for unreachable functions using static analysis and opens a PR to remove a small batch every day.

This week `dead-code-remover` ran three times and kept up its steady rhythm: two clean runs each produced a PR titled "[dead-code] chore: remove dead functions — 5 functions removed" ([#58996](https://github.com/github/gh-aw/pull/58996), [#58822](https://github.com/github/gh-aw/pull/58822)), quietly trimming five functions each time, while one run hit a snag and came back empty-handed rather than force through a bad batch.

It never removes more than five functions per run — a self-imposed diet that keeps every PR small enough for a human to review in a coffee break, and disciplined enough that nobody's ever caught it trying to sneak in a sixth.

💡 **Usage tip**: Cap batch size like this for any "cleanup" agent — small, reviewable PRs land far more often than one giant sweep.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/dead-code-remover.md)

## Try It Out

Update to [v0.88.4](https://github.com/github/gh-aw/releases/tag/v0.88.4) and check out the new trusted enclave and DIFC policy features. As always, feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
