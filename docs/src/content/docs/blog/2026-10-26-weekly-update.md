---
title: "Weekly Update – October 26, 2026"
description: "Five releases (v0.93.0–v0.93.5) focused on hardening: sharper linters, safer credential handling, and better diagnostics."
authors:
  - copilot
date: 2026-10-26
metadata:
  seoDescription: "gh-aw v0.93.0 through v0.93.5 harden linters, credential handling, and diagnostics. Plus our Agent of the Week: the Daily Documentation Healer."
---

It was a hardening week in [github/gh-aw](https://github.com/github/gh-aw): six releases from [v0.93.0](https://github.com/github/gh-aw/releases/tag/v0.93.0) through [v0.93.5](https://github.com/github/gh-aw/releases/tag/v0.93.5) brought stronger static analysis, safer credential handling, and clearer failure diagnostics.

## Release Highlights

The latest, [v0.93.5](https://github.com/github/gh-aw/releases/tag/v0.93.5), landed on October 25th. Earlier releases this week were [v0.93.4](https://github.com/github/gh-aw/releases/tag/v0.93.4), [v0.93.3](https://github.com/github/gh-aw/releases/tag/v0.93.3), [v0.93.2](https://github.com/github/gh-aw/releases/tag/v0.93.2), [v0.93.1](https://github.com/github/gh-aw/releases/tag/v0.93.1) and [v0.93.0](https://github.com/github/gh-aw/releases/tag/v0.93.0).

- **Network credentials in safe-output integration jobs** ([#71833](https://github.com/github/gh-aw/pull/71833)): safe-output integration jobs can now use network credentials when they need them.
- **Engine driver crashes are classified** ([#71883](https://github.com/github/gh-aw/pull/71883)): failures now come with secret-safe stderr diagnostics, so triage is faster.
- **Pi CLI 1.4.0 is the new default** ([#71899](https://github.com/github/gh-aw/pull/71899)).
- **Sharper custom linters**: a new `readerstringconv` analyzer ([#71891](https://github.com/github/gh-aw/pull/71891)), detection of parenthesized `Flush` discards ([#71896](https://github.com/github/gh-aw/pull/71896)), and missing recovery in goroutines launched through local closures ([#71897](https://github.com/github/gh-aw/pull/71897)).
- **Fewer surprises at compile time**: indexing boundaries are guarded and cron-step overflow is prevented ([#71889](https://github.com/github/gh-aw/pull/71889)), and generated conclusion references and fetch warnings are fixed ([#71898](https://github.com/github/gh-aw/pull/71898)).

## Notable Pull Requests

- [Prevent skipped threat detection from reporting inherited agent tokens](https://github.com/github/gh-aw/pull/71933) keeps token reporting honest when detection is skipped.
- [Return stage-aware partial results for MCP compile and audit timeouts](https://github.com/github/gh-aw/pull/71886) means a timeout no longer throws away the work already done.
- [Stop treating gh-aw-detection as experimental](https://github.com/github/gh-aw/pull/71848) graduates threat detection from its experimental label.
- [Preflight agent assignment credentials before processing issues](https://github.com/github/gh-aw/pull/71611) fails early instead of halfway through.

## 🤖 Agent of the Week: Daily Documentation Healer

The self-healing companion to the Daily Documentation Updater: it hunts for documentation gaps the updater missed and proposes fixes.

In its last three runs it ran at roughly 23:40 UTC each night, taking 7–8 minutes per run. The most recent run on October 25th succeeded, while the two before it failed in the conclusion job's "Handle agent failure" step. The healer is also running a model-size experiment, alternating between Claude Sonnet 5 and Claude Haiku 4.5 to see whether the cheaper model heals docs just as well.

The healer is a documentation doctor that is itself part of a clinical trial, with a pre-registered hypothesis and everything.

💡 **Usage tip**: Pair a generator workflow with a second "healer" pass that audits its output, so gaps get caught the next day.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-doc-healer.md)

## Try It Out

Update to [v0.93.5](https://github.com/github/gh-aw/releases/tag/v0.93.5) and tell us what you think. Feedback and contributions are always welcome in [github/gh-aw](https://github.com/github/gh-aw).
