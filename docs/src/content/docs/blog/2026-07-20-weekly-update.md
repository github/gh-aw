---
title: "Weekly Update – July 20, 2026"
description: "This week: v0.82.13 with smarter ESLint detection and issue intent metadata, a firewall bump to v0.27.37, rootless ARC installation, and a wave of workshop UX improvements."
authors:
  - copilot
date: 2026-07-20
metadata:
  seoDescription: "gh-aw week of July 20: v0.82.13, firewall v0.27.37, rootless ARC runners, workshop redesign, and new Go linters."
---

Another action-packed week in [github/gh-aw](https://github.com/github/gh-aw)! Between a fresh release, a firewall bump, improved rootless runner support, and a thoroughly redesigned workshop experience, there was plenty to keep the bots busy.

## Release: [v0.82.13](https://github.com/github/gh-aw/releases/tag/v0.82.13)

[v0.82.13](https://github.com/github/gh-aw/releases/tag/v0.82.13) landed on July 18th with smarter tooling, better defaults, and one breaking change to be aware of.

### ⚠️ Breaking Change

- **`gh aw add` now rejects packages with `aw.yml` config** ([#46273](https://github.com/github/gh-aw/pull/46273)): If you maintain packages that include an `aw.yml` configuration file, update them before upgrading — the CLI will now refuse to install them outright.

### ✨ What's New

- **Auto-configure `COPILOT_PROVIDER_WIRE_API` from the model catalog** ([#46156](https://github.com/github/gh-aw/pull/46156)): The CLI now resolves the provider wire API endpoint automatically, so you don't have to set it by hand.
- **Default-on issue intent metadata** ([#46207](https://github.com/github/gh-aw/pull/46207)): `set_issue_type`, `set_issue_field`, and `add_labels` now emit intent metadata by default — richer audit trails with zero extra config.
- **`NO_COLOR` support** ([#46197](https://github.com/github/gh-aw/pull/46197)): The CLI now honours the `NO_COLOR` environment variable for cleaner output in CI and accessibility-focused terminals.
- **Stronger ESLint alias detection** ([#46365](https://github.com/github/gh-aw/pull/46365)): The `no-core-setoutput` and `exportvariable` rules now catch aliased and destructured `@actions/core` bindings, closing a common bypass pattern.

## Notable Pull Requests

- **[Firewall bump to v0.27.37](https://github.com/github/gh-aw/pull/46637)**: The default `gh-aw-firewall` was updated from v0.27.35 to v0.27.37, bringing `ANTHROPIC_AUTH_TOKEN` credential isolation, `~/.local/bin` added to sandbox PATH for rootless Copilot installs, and runner doctor catalog updates.

- **[Rootless flag for ARC/DinD runners](https://github.com/github/gh-aw/pull/46047)**: `install_copilot_cli.sh` now accepts a `--rootless` flag for ARC and Docker-in-Docker runner environments — a welcome fix for teams running Copilot on custom runners.

> [!NOTE]
> ARC/DinD and rootless installation describe component capabilities, not certification of a Kubernetes ARC chart or official end-to-end GHES/GHEC availability. See the current [deployment and runner applicability summary](/gh-aw/reference/enterprise-configuration/#deployment-and-runner-applicability).

- **[New `timenowsub` linter](https://github.com/github/gh-aw/pull/46633)**: The linter-miner contributed another Go linter that flags `time.Now().Sub(t)` and auto-rewrites it to the idiomatic `time.Since(t)`. Small but satisfying.

- **[Workshop redesign](https://github.com/github/gh-aw/pull/46616)**: The workshop has been moved to `/workshop/`, simplified to match docs styling ([#46593](https://github.com/github/gh-aw/pull/46593)), and now shows step counts on entry and scenario cards ([#46622](https://github.com/github/gh-aw/pull/46622)) — making it much easier to gauge how much is left before you start.

- **[MCP toolsets sync](https://github.com/github/gh-aw/pull/46604)**: GitHub MCP toolset mappings were synced with the upstream `github-mcp-server` main branch, keeping tool definitions up to date.

## 🤖 Agent of the Week: Avenger

The CI guardian who never sleeps — Avenger runs every hour, checks whether CI is passing, and if it's not, merges `main`, runs `recompile`/`fmt`/`lint`/`test`, and opens a PR with any fixable issues.

This week, Avenger ran multiple times and achieved `success` across the board, quietly keeping the codebase tidy during the busy firewall bump and workshop refactor merge storm. Each run it faithfully pulled in the latest `main`, ran the full quality gauntlet, and — finding nothing broken — went back to sleep without making a fuss.

The highlight of Avenger's week was its run right after the [v0.27.37 firewall bump](https://github.com/github/gh-aw/pull/46637) landed, where it dutifully checked that all 258 recompiled `.lock.yml` files hadn't introduced any CI regressions. They hadn't. Avenger nodded once and clocked out.

💡 **Usage tip**: Avenger shines in repos where automated PRs (dependency bumps, codegen, lock file updates) can quietly break CI — it catches those regressions within the hour so humans don't have to.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/avenger.md)

## Try It Out

Grab [v0.82.13](https://github.com/github/gh-aw/releases/tag/v0.82.13) and take the redesigned workshop for a spin. As always, feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
