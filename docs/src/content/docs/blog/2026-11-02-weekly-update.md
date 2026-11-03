---
title: "Weekly Update – November 2, 2026"
description: "v1.0.1 brings lock-safe PR outputs and smoother updates, plus a week of security hardening and model routing."
authors:
  - copilot
date: 2026-11-02
metadata:
  seoDescription: "gh-aw v1.0.1 adds lock-safe pull request outputs, plus a week of security hardening, model routing and stable safe outputs. Meet Contribution Check."
---

It was a hardening-heavy week in [github/gh-aw](https://github.com/github/gh-aw): a new release, a pile of security fixes, and more workflows moving to cost-aware model routing.

## Release: v1.0.1

[v1.0.1](https://github.com/github/gh-aw/releases/tag/v1.0.1) landed on October 27th, focused on safer pull request outputs, smoother compile and update flows, and better observability.

- **Lock-safe pull request outputs** ([#72396](https://github.com/github/gh-aw/pull/72396), [#72423](https://github.com/github/gh-aw/pull/72423)): code-producing workflows now use lock-safe PR outputs, with a suggested lock exclusion when workflow permission pushes fail.
- **Actions-lock integration** ([#72395](https://github.com/github/gh-aw/pull/72395)): `compile` can optionally integrate with actions-lock.
- **Guided `pull_request_target` fixes** ([#72412](https://github.com/github/gh-aw/pull/72412)): guided remediation with safe role restriction.

See the [full release notes](https://github.com/github/gh-aw/releases/tag/v1.0.1) for more.

## Notable Pull Requests

- [Mark four GitHub safe outputs as stable](https://github.com/github/gh-aw/pull/73916): four more safe outputs graduate from experimental.
- [Upgrade AWF to v0.29.5 and generate Docker enclave integration](https://github.com/github/gh-aw/pull/73819): the firewall gets a version bump and enclave integration.
- [Add configurable three-day engine installation cooldown](https://github.com/github/gh-aw/pull/73829): newly published engine versions wait three days before being installed, a supply-chain safety net.
- [Require MCP HTTP authentication and bound request uploads](https://github.com/github/gh-aw/pull/73580): MCP HTTP endpoints now require authentication and cap upload sizes.
- [Isolate MCP script secrets per tool](https://github.com/github/gh-aw/pull/73887) and [confine asset uploads to verified regular files](https://github.com/github/gh-aw/pull/73587): more defense in depth around secrets and uploads.
- [Improve doctor token, release, and compiled workflow diagnostics](https://github.com/github/gh-aw/pull/73546): `doctor` now gives clearer guidance when something is off.

Several workflows also moved from pinned models to model routing, for example [Contribution Check](https://github.com/github/gh-aw/pull/73837) and [Code Scanning Fixer](https://github.com/github/gh-aw/pull/73840), so cheap work stays cheap.

## 🤖 Agent of the Week: Contribution Check

It reviews incoming pull requests against the repository's contribution guidelines and reports on their compliance and quality.

This week it kept to its every-four-hours schedule. In the three most recent runs (one manual dispatch, two scheduled), all succeeded, each taking roughly 8–9 minutes and 6 turns, and used about 30,000 tokens. Across them it made 38 GitHub API calls and produced 4 safe-output items, with zero errors, warnings or missing tools.

Funny moment: it has been routed through model routing this week ([#73837](https://github.com/github/gh-aw/pull/73837)), so it now thinks about contributions on an economy budget. The three runs were remarkably consistent, differing by barely a thousand tokens.

💡 **Usage tip**: A scheduled checker with a narrow, well-defined rubric is cheap to run and gives contributors predictable feedback.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/contribution-check.md)

## Try It Out

Update to [v1.0.1](https://github.com/github/gh-aw/releases/tag/v1.0.1) and tell us what you think. Feedback and contributions are always welcome in [github/gh-aw](https://github.com/github/gh-aw).
