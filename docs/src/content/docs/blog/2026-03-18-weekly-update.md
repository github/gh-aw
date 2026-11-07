---
title: "Weekly Update – March 18, 2026"
description: "Seven releases in seven days: guard policy overhaul, new triggers, GHE improvements, and a healthy dose of quality-of-life fixes."
authors:
  - copilot
date: 2026-03-18
---

It's been a busy week in [github/gh-aw](https://github.com/github/gh-aw) — seven releases shipped between March 13 and March 17, covering everything from a security model overhaul to a new label-based trigger and a long-overdue terminal resize fix. Let's dig in.

## Releases This Week

### [v0.61.0](https://github.com/github/gh-aw/releases/tag/v0.61.0) — March 17

The freshest release focuses on reliability and developer experience:

- **Automatic debug logging** ([#21406](https://github.com/github/gh-aw/pull/21406)): Set `ACTIONS_RUNNER_DEBUG=true` on your runner and full debug logging activates automatically — no more manually adding `DEBUG=*` to every troubleshooting run.
- **Cross-repo project item updates** ([#21404](https://github.com/github/gh-aw/pull/21404)): `update_project` now accepts a `target_repo` parameter, so org-level project boards can update fields on items from any repository.
- **GHE Cloud data residency support** ([#21408](https://github.com/github/gh-aw/pull/21408)): Compiled workflows now auto-inject a `GH_HOST` step, fixing `gh` CLI failures on `*.ghe.com` instances.
- **CI build artifacts** ([#21440](https://github.com/github/gh-aw/pull/21440)): The `build` CI job now uploads the compiled `gh-aw` binary as a downloadable artifact — handy for testing PRs without a local build.

### [v0.60.0](https://github.com/github/gh-aw/releases/tag/v0.60.0) — March 17

This release rewires the security model. **Breaking change**: automatic `lockdown=true` is gone. Instead, the runtime now auto-configures guard policies on the GitHub MCP server — `min_integrity=approved` for public repos, `min_integrity=none` for private/internal. Remove any explicit `lockdown: false` from your frontmatter; it's no longer needed.

Other highlights:

- **GHES domain auto-allowlisting** ([#21301](https://github.com/github/gh-aw/pull/21301)): When `engine.api-target` points to a GHES instance, the compiler automatically adds GHES API hostnames to the firewall. No more silent blocks after every recompile.

> [!NOTE]
> GHES routing and host auto-detection are component compatibility paths, not confirmation of official end-to-end Agentic Workflows availability on GHES or a particular release. See the current [deployment and runner applicability summary](/gh-aw/reference/enterprise-configuration/#deployment-and-runner-applicability).
- **`github-app:` auth in APM dependencies** ([#21286](https://github.com/github/gh-aw/pull/21286)): APM `dependencies:` can now use `github-app:` auth for cross-org private package access.

### [v0.59.0](https://github.com/github/gh-aw/releases/tag/v0.59.0) — March 16

A feature-packed release with two breaking changes (field renames in `safe-outputs.allowed-domains`) and several new capabilities:

- **Label Command Trigger** ([#21118](https://github.com/github/gh-aw/pull/21118)): Activate a workflow by adding a label to an issue, PR, or discussion. The label is automatically removed so it can be reapplied to re-trigger.
- **`gh aw domains` command** ([#21086](https://github.com/github/gh-aw/pull/21086)): Inspect the effective network domain configuration for all your workflows, with per-domain ecosystem annotations.
- **Pre-activation step injection** — New `on.steps` and `on.permissions` frontmatter fields let you inject custom steps and permissions into the activation job for advanced scenarios.

### Earlier in the Week

- [v0.58.3](https://github.com/github/gh-aw/releases/tag/v0.58.3) (March 15): MCP write-sink guard policy for non-GitHub MCP servers, Copilot pre-flight diagnostic for GHES, and a richer run details step summary.
- [v0.58.2](https://github.com/github/gh-aw/releases/tag/v0.58.2) (March 14): GHES auto-detection in `audit` and `add-wizard`, `excluded-files` support for `create-pull-request`, and clearer `run` command errors.
- [v0.58.1](https://github.com/github/gh-aw/releases/tag/v0.58.1) / [v0.58.0](https://github.com/github/gh-aw/releases/tag/v0.58.0) (March 13): `call-workflow` safe output for chaining workflows, `checkout: false` for agent jobs, custom OpenAI/Anthropic API endpoints, and 92 merged PRs in v0.58.0 alone.

## Notable Pull Requests

- **[Top-level `github-app` fallback](https://github.com/github/gh-aw/pull/21510)** ([#21510](https://github.com/github/gh-aw/pull/21510)): Define your GitHub App config once at the top level and let it propagate to safe-outputs, checkout, MCP, APM, and activation — instead of repeating it in every section.
- **[GitHub App-only permission scopes](https://github.com/github/gh-aw/pull/21511)** ([#21511](https://github.com/github/gh-aw/pull/21511)): 31 new `PermissionScope` constants cover repository, org, and user-level GitHub App permissions (e.g., `administration`, `members`, `environments`).
- **[Custom Huh theme](https://github.com/github/gh-aw/pull/21557)** ([#21557](https://github.com/github/gh-aw/pull/21557)): All 11 interactive CLI forms now use a Dracula-inspired theme consistent with the rest of the CLI's visual identity.
- **[Weekly blog post writer workflow](https://github.com/github/gh-aw/pull/21575)** ([#21575](https://github.com/github/gh-aw/pull/21575)): Yes, the workflow that wrote this post was itself merged this week. Meta!
- **[CI job timeout limits](https://github.com/github/gh-aw/pull/21601)** ([#21601](https://github.com/github/gh-aw/pull/21601)): All 25 CI jobs that relied on GitHub's 6-hour default now have explicit timeouts, preventing a stuck test from silently burning runner compute.

## 🤖 Agent of the Week: auto-triage-issues

The first-ever Agent of the Week goes to the workflow that handles the unglamorous but essential job of keeping the issue tracker from becoming a swamp.

`auto-triage-issues` runs on a schedule and fires on every new issue, reading each one and deciding how to categorize it. This week it ran five times — three successful runs and two that were triggered by push events to a feature branch (which apparently fire the workflow but don't give it much to work with). On its scheduled run this morning, it found zero open issues in the repository, so it created a tidy summary discussion to announce the clean state, as instructed. On an earlier issues-triggered run, it attempted to triage issue [#21572](https://github.com/github/gh-aw/pull/21572) but hit empty results from GitHub MCP tools on all three read attempts — so it gracefully called `missing_data` and moved on rather than hallucinating a label.

Across its recent runs it made 131 `search_repositories` calls. We're not sure why it finds repository searches so compelling, but clearly it's very thorough about knowing its neighborhood before making any decisions.

💡 **Usage tip**: Pair `auto-triage-issues` with a notify workflow on specific labels (e.g., `security` or `needs-repro`) so the right people get pinged automatically without anyone having to watch the inbox.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/auto-triage-issues.md)

## Try It Out

Update to [v0.61.0](https://github.com/github/gh-aw/releases/tag/v0.61.0) to get all the improvements from this packed week. If you run workflows on GHES or in GHE Cloud, the new auto-detection and `GH_HOST` injection features are especially worth trying. As always, contributions and feedback are welcome in [github/gh-aw](https://github.com/github/gh-aw).
