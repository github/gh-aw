---
title: Enterprise Configuration
description: Configure GitHub Agentic Workflows for GitHub Enterprise Server (GHES) and GitHub Enterprise Cloud (GHEC), including GHES compatibility mode and CLI setup.
sidebar:
  order: 51
---

# Enterprise Configuration

This page covers configuration options specific to GitHub Enterprise Server (GHES) and GitHub Enterprise Cloud (GHEC) deployments.

For human sponsorship, independent code review, and evidence requirements, see
[Human Review and Compliance Patterns](/gh-aw/reference/compliance-patterns/).

## GitHub Enterprise Server (GHES) Compatibility

### GHES Compatibility Mode

GHES instances running versions that predate `@actions/artifact` v2.0.0 support cannot use `actions/upload-artifact@v4+` or `actions/download-artifact@v4+`. Attempting to run compiled workflows on these instances produces a `GHESNotSupportedError`.

gh-aw includes a GHES compatibility mode toggle (`aw.json` `ghes` or `gh aw compile --ghes`) for GHES releases that require the v3 artifact backend. Compatibility mode emits `upload-artifact@v3.2.2` and `download-artifact@v3.1.0`; default GitHub.com compilation continues to use the latest artifact actions.

Compatibility mode also omits compiler-generated workflow and job `cache-mode` fields, including dry-run fields. The [GitHub announcement](https://github.blog/changelog/2026-09-10-control-github-actions-cache-access-with-cache-mode/) establishes availability on GitHub.com, not a supported GHES release. Enable compatibility mode when the target server does not support this syntax, then recompile the workflows. This is the existing combined GHES switch, not an independent cache-only option or automatic server-version negotiation.

> [!WARNING]
> Omitting `cache-mode` removes the scoped-token cache restriction. Detection-enabled cache-memory restores, publisher event/validation gates, and event-conditioned dependency-cache actions remain. Dry-run still uses restore-only compiler-managed cache actions and omits the cache-memory publisher; it cannot constrain arbitrary cache writers through the scoped token on unsupported servers. See [cache access boundaries](/gh-aw/reference/cache-memory/#cache-access-boundaries).

This compatibility path supports GHES 3.21.x and earlier when the workflow runs on Actions Runner 2.327.1 or later, which is required by the Node.js 24 runtime used by these pinned artifact actions. For later GHES releases, keep compatibility mode enabled until your instance supports the v4 artifact backend.

#### Enable via `aw.json` (recommended)

Set `ghes: true` in `.github/workflows/aw.json` to apply GHES compatibility to every workflow compiled in the repository:

```json
{
  "ghes": true
}
```

#### Auto-detection with `gh aw init`

Running `gh aw init` inside a GHES repository automatically detects the deployment and writes `ghes: true` to `.github/workflows/aw.json`. No manual configuration is required.

#### Enable via CLI flag

Pass `--ghes` to `gh aw compile` for a one-off compilation without modifying `aw.json`:

```bash
gh aw compile --ghes my-workflow.md
```

> [!NOTE]
> The `--ghes` flag only affects the current compilation. Use `aw.json` to apply GHES compatibility permanently across all workflows in the repository.

## GitHub Enterprise Server CLI Setup

For `gh` CLI configuration, host authentication, and `GH_HOST` setup on GHES, see [GitHub Enterprise Server Support](/gh-aw/setup/cli/#github-enterprise-server-support) in the CLI reference.

## Centralized Activation Admission

Admission jobs are omitted by default. Enable both `admission` and `conclusion_admission` by setting `on.admission: true` in workflow frontmatter, or `"admission": true` in `.github/workflows/aw.json` for all workflows. Either setting enables the jobs; `on.admission: false` does not override a repository opt-in. Recompile after changing either setting.

```aw
on:
  workflow_dispatch:
  admission: true
```

Configure an OIDC-authenticated admission authority using organization or repository Actions variables: `GH_AW_DEFAULT_ADMISSION_URL`, `GH_AW_DEFAULT_ADMISSION_AUDIENCE`, and `GH_AW_DEFAULT_ADMISSION_ON_OUTAGE`. For opted-in workflows, the endpoint enables admission at runtime; an empty endpoint skips both jobs. Variables alone do not generate admission jobs. For enterprise-wide rollout, provision organization variables in each enrolled organization. Deployments must explicitly configure their issuer, accepted claims, and trusted workflow identities on the authority.

Approved admission supersedes daily-AIC validation and supplies the primary and optional component allowances used by the existing runtime credit limits. Omitted optional allowances retain their configured component limits. Disabled admission and ignored transient outages retain daily validation and existing limits. The default outage policy is `ignore`; select and protect `block` when availability must not bypass admission. Variables remain cooperative defaults, not mandatory organization policy. Reusable workflow callers must grant `id-token: write` to the caller job; see [enterprise environment controls](/gh-aw/reference/compiler-enterprise-environment-controls/) for the permission envelope and rollout requirements. See also the [admission service specification](/gh-aw/specs/admission-service-specification/).

The same configuration enables an isolated conclusion-admission job to post to `/api/conclusion` on the authority's origin. The ordinary conclusion job first publishes the full unified-session artifact, finishes any upload retry, and passes only bounded compact metadata and artifact IDs to the callback job. The callback obtains its own fresh OIDC token for the same audience; unrelated conclusion and safe-output steps do not receive this permission. The compact profile excludes agent conversation and tool content and is bounded to 128 records/16 KiB JSONL; the whole request is capped at 64 KiB. The authority can resolve the unchanged full session through authenticated, invocation-verified artifact reads. Failed artifact publication does not suppress compact delivery, but the request explicitly carries no IDs if neither upload produced one. Endpoint enrollment covers both compact delivery and protected full-session retrieval. Delivery failures warn without changing completed workflow results, even with outage policy `block`; the authority remains responsible for secure, deduplicated processing and retention.

## Copilot Engine on GHES

For Copilot-specific prerequisites, licensing requirements, and firewall configuration on GHES, see [Copilot Engine Prerequisites on GHES](/gh-aw/troubleshooting/common-issues/#copilot-engine-prerequisites-on-ghes).
