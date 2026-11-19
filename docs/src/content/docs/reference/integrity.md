---
title: GitHub Integrity Filtering
description: How integrity filtering restricts agent access to GitHub content based on author trust and merge status, and how filtered events appear in logs.
sidebar:
  order: 680
---

Integrity filtering (`tools.github.min-integrity`) controls which GitHub content an agent can access during a workflow run. Rather than filtering by permissions, it filters by **trust**: the author association of an issue, pull request, or comment, and whether that content has been merged into the main branch.

## Migrate from lockdown mode

GitHub lockdown mode has been superseded by integrity filtering. Replace `lockdown: true` with `min-integrity: approved`, and `lockdown: false` with `min-integrity: none`:

```yaml wrap
# Before (deprecated)
tools:
  github:
    lockdown: true

# After
tools:
  github:
    min-integrity: approved
```

## How It Works

The MCP gateway intercepts tool calls to GitHub and checks the integrity of each item returned. Items below the configured minimum are removed before the AI engine sees them, so the agent receives a reduced result set. Each filtered item is logged as a `DIFC_FILTERED` event for later inspection.

Some proxy responses cannot be filtered item by item. Starting with [MCP gateway v0.5.0](https://github.com/github/gh-aw-mcpg/releases/tag/v0.5.0), artifact downloads, raw content, and redirects require resource-level authorization even in filter mode, and can be denied when the allow-only policy does not authorize the resource. Starting with [v0.5.1](https://github.com/github/gh-aw-mcpg/releases/tag/v0.5.1), Actions job logs, security advisory contents, and file contents fetched at explicit SHAs or non-default refs have baseline `none` integrity. Repository visibility and trusted PR authors do not promote them, so `min-integrity: approved` can deny them. [v0.5.2](https://github.com/github/gh-aw-mcpg/releases/tag/v0.5.2) filters nested review-thread comments individually in filter mode, preserving thread and pagination metadata. In strict mode, any denied comment rejects the entire response.

## Configuration

Set `min-integrity` under `tools.github` in your workflow frontmatter:

```aw wrap
tools:
  github:
    min-integrity: approved
```

`min-integrity` can be specified alone. When `allowed-repos` is omitted, it defaults to `"all"`. If `allowed-repos` is also specified, both fields must be present.

## Configuration Reference

The table lists every field under `tools.github`. The four reaction-related fields (`endorsement-reactions`, `disapproval-reactions`, `endorser-min-integrity`, and `disapproval-integrity`) take effect only when `features.integrity-reactions: true` is set.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `min-integrity` | string | Yes (when any guard policy field is used) | `approved` for public repos; none for private | Minimum integrity level: `merged`, `approved`, `unapproved`, or `none` |
| `allowed-repos` | string or array | No | `"all"` | Repository scope: `"all"`, `"public"`, or an array of patterns like `["myorg/*", "partner/repo"]` |
| `blocked-users` | array or expression | No | `[]` | GitHub usernames whose content is unconditionally denied |
| `trusted-users` | array or expression | No | `[]` | GitHub usernames elevated to `approved` integrity regardless of author association |
| `approval-labels` | array or expression | No | `[]` | GitHub label names that promote items to `approved` integrity |
| `refusal-labels` | array or expression | No | `[]` | GitHub label names that downgrade items to `none` integrity, overriding any promotion from `trusted-users` or `approval-labels` |
| `integrity-proxy` | boolean | No | `true` | Whether to run the DIFC proxy for pre-agent `gh` CLI calls. Set to `false` to disable |
| `endorsement-reactions` | array | No | `["THUMBS_UP", "HEART"]` (when `integrity-reactions` enabled) | Reaction types that promote item integrity to `approved` |
| `disapproval-reactions` | array | No | `["THUMBS_DOWN", "CONFUSED"]` (when `integrity-reactions` enabled) | Reaction types that demote item integrity |
| `endorser-min-integrity` | string | No | `approved` (when `integrity-reactions` enabled) | Minimum integrity of the reactor for an endorsement or disapproval to take effect |
| `disapproval-integrity` | string | No | `none` (when `integrity-reactions` enabled) | Integrity level assigned when a qualifying disapproval reaction is added |

## Integrity Levels

The full integrity hierarchy, from highest to lowest:

```text
merged > approved > unapproved > none > blocked
```

| Level | What qualifies at this level |
|-------|------------------------------|
| `merged` | Pull requests that have been merged, and commits reachable from the default branch (any author) |
| `approved` | Objects authored by `OWNER`, `MEMBER`, or `COLLABORATOR`; non-fork PRs on public repos; all items in private repos; trusted platform bots (e.g., dependabot); users listed in `trusted-users` |
| `unapproved` | Objects authored by `CONTRIBUTOR` or `FIRST_TIME_CONTRIBUTOR` |
| `none` | All objects, including `FIRST_TIMER` and users with no association (`NONE`) |
| `blocked` | Items authored by users in `blocked-users` — always denied, cannot be promoted |

The four configurable levels are cumulative. `min-integrity: approved` lets through items at `approved` or `merged` and filters out `unapproved` and `none`.

`blocked` is not a configurable value. It is assigned automatically to items from users in `blocked-users` and is always denied, even with `min-integrity: none`. See [Blocking and trusting users](#blocking-and-trusting-users).

## Scoping to Repositories

`allowed-repos` defines which repositories the guard policy applies to. It accepts `"all"` (the default when omitted; all repositories the token can access), `"public"` (public repositories only), or an array of patterns. Patterns must be lowercase:

| Pattern | Meaning |
|---------|---------|
| `owner/*` | All repositories under `owner` |
| `owner/prefix*` | Repositories under `owner` whose name starts with `prefix` |
| `owner/repo` | A single specific repository |

```aw wrap
tools:
  github:
    allowed-repos:
      - "myorg/*"
      - "partner/shared-repo"
    min-integrity: approved
```

## Adjusting Integrity Per-Item

Beyond setting a minimum level, you can override integrity for specific authors or labels.

### Blocking and trusting users

`blocked-users` unconditionally denies content from listed GitHub usernames, regardless of `min-integrity`, `trusted-users`, or labels. Blocked items receive an effective integrity of `blocked` (below `none`). Use it for automated bots, compromised accounts, or external contributors pending security review.

`trusted-users` elevates content from listed usernames to `approved`, even when GitHub classifies them as `CONTRIBUTOR` or `FIRST_TIME_CONTRIBUTOR`. Use it for contractors, partner developers, and similar trusted externals. Bot logins such as `github-actions[bot]` are valid; use the full login, including the `[bot]` suffix. Trust applies per author and does not extend to other authors' comments on a trusted bot's issue or pull request. Listing a bot does not require lowering `min-integrity` to `none`. `trusted-users` requires `min-integrity` to be set.

> [!NOTE]
> MCP gateway v0.4.37 fixes author-trust handling for wrapped `issue_read` responses. Earlier versions could filter trusted authors' issue bodies as `DIFC_FILTERED` at `min-integrity: approved`. Workflows compiled with an earlier gateway version must be recompiled with an updated CLI to use the fix; see the [runtime fix and regression coverage](https://github.com/github/gh-aw-mcpg/pull/15292).

### Promoting and refusing items via labels

`approval-labels` promotes items bearing any listed label to `approved` integrity. A maintainer can label specific items to let them past a stricter `min-integrity`, which suits human-review workflows.

`refusal-labels` is the inverse. Items bearing any listed label drop to `none`, regardless of author association or any promotion from `trusted-users` or `approval-labels`. Use it to hold back content such as security-sensitive issues or pull requests awaiting a manual compliance check.

### Promoting and demoting items via reactions

`features.integrity-reactions: true` lets maintainers adjust item integrity with GitHub reactions, without adding labels or changing issue state. Available from gh-aw v0.68.2.

```aw wrap
features:
  integrity-reactions: true
tools:
  github:
    min-integrity: approved
```

When enabled, the compiler automatically enables the CLI proxy (required to identify reaction authors) and injects default reaction configuration. An endorsement reaction from an account at or above `endorser-min-integrity` promotes the item to `approved`. A disapproval reaction from such an account sets the item to `disapproval-integrity`.

The defaults are `endorsement-reactions: [THUMBS_UP, HEART]`, `disapproval-reactions: [THUMBS_DOWN, CONFUSED]`, `endorser-min-integrity: approved`, and `disapproval-integrity: none`. To override them, set the fields explicitly under `tools.github`:

```aw wrap
tools:
  github:
    endorsement-reactions:
      - "THUMBS_UP"
      - "HEART"
    disapproval-reactions:
      - "THUMBS_DOWN"
    endorser-min-integrity: merged
    disapproval-integrity: unapproved
```

Valid reaction values: `THUMBS_UP`, `THUMBS_DOWN`, `HEART`, `HOORAY`, `CONFUSED`, `ROCKET`, `EYES`, `LAUGH`.

### Shared lists from GitHub variables

`blocked-users`, `trusted-users`, `approval-labels`, and `refusal-labels` can each take a GitHub Actions expression instead of a literal array. The expression is evaluated at runtime and should resolve to a comma- or newline-separated list:

```aw wrap
tools:
  github:
    min-integrity: approved
    blocked-users: ${{ vars.BLOCKED_USERS }}
    trusted-users: ${{ vars.TRUSTED_USERS }}
    approval-labels: ${{ vars.APPROVAL_LABELS }}
    refusal-labels: ${{ vars.REFUSAL_LABELS }}
```

The runtime also unions each per-workflow list with a matching GitHub variable. A security team can maintain shared policy this way without editing individual workflow files:

| Workflow field | GitHub variable |
|---------------|----------------|
| `blocked-users` | `GH_AW_GITHUB_BLOCKED_USERS` |
| `trusted-users` | `GH_AW_GITHUB_TRUSTED_USERS` |
| `approval-labels` | `GH_AW_GITHUB_APPROVAL_LABELS` |
| `refusal-labels` | `GH_AW_GITHUB_REFUSAL_LABELS` |

Variables are split on commas and newlines, trimmed, and deduplicated. Set them as repository variables (**Settings → Secrets and variables → Actions → Variables**) or as organization variables. For example, if a workflow declares `blocked-users: ["spam-bot"]` and `GH_AW_GITHUB_BLOCKED_USERS` is `compromised-acct,old-bot`, the effective blocked list is `["spam-bot", "compromised-acct", "old-bot"]`.

### Effective integrity computation

The gateway derives each item's base integrity from author association, merge status, and repo visibility. It then applies the first matching rule below, and the `min-integrity` threshold check runs on the result:

1. Author in `blocked-users` → `blocked` (always denied).
2. Item has a `refusal-labels` label → `none` (overrides any promotion).
3. Author in `trusted-users` → max(base, `approved`).
4. Item has an `approval-labels` label → max(base, `approved`).
5. Otherwise → base.

Trust and label promotions only raise integrity, never lower it. Blocking always wins: a blocked user stays blocked even if also trusted or labeled.

### Combined example

This policy uses all four per-item fields together:

```aw wrap
tools:
  github:
    allowed-repos: "all"
    min-integrity: approved
    blocked-users:
      - "known-spam-bot"
    trusted-users:
      - "contractor-1"
    approval-labels:
      - "agent-approved"
    refusal-labels:
      - "needs-security-review"
```

## Default Behavior

For **public repositories**, if no `min-integrity` is configured, the runtime applies `min-integrity: approved` automatically. This protects public workflows even when additional authentication has not been set up.

For **private and internal repositories**, no guard policy is applied automatically, and content from all users is accessible by default.

## Choosing a Level

| Workflow type | Suggested level |
|---------------|-----------------|
| Automated code review or changes applied to code | `merged` or `approved`: act only on trusted content |
| Responds to maintainers and trusted contributors | `approved`: a common, safe default |
| Community triage or planning | `unapproved`: allow contributor input, while excluding anonymous and `FIRST_TIMER` interactions |
| Public-data or spam detection | `none`: see all activity, but do not apply outputs without review |

> [!NOTE]
> Setting `min-integrity: none` on a public repository disables the automatic protection. Only use it when the workflow is designed to handle untrusted input.

## Pre-Agent Integrity Proxy

When a guard policy is configured, the compiler injects a DIFC proxy that filters `gh` CLI calls in pre-agent setup steps. Those steps then see the same integrity-filtered responses as the agent. The proxy routes `gh` calls through the same MCP gateway container and applies only the compile-time fields (`min-integrity` and `allowed-repos`). It does not apply `blocked-users`, `trusted-users`, `approval-labels`, or `refusal-labels`, which are resolved at runtime after the proxy starts. The proxy starts before custom steps and stops before the MCP gateway starts, to avoid double-filtering.

The proxy is enabled by default whenever a guard policy is configured. To disable it, set `integrity-proxy: false`:

```aw wrap
tools:
  github:
    min-integrity: approved
    integrity-proxy: false
```

Use this opt-out when pre-agent steps need unfiltered API access for setup. Disabling the proxy affects only pre-agent `gh` CLI calls. The agent always operates under the configured guard policy through the MCP gateway.

## In Logs and Reports

When an item is filtered, the MCP gateway records a `DIFC_FILTERED` event in the run's `gateway.jsonl` log. Each event includes the server and tool that returned the content, the author's login, a reason (for example, `"Resource has lower integrity than agent requires."`), the integrity tags on the item, and the author association (such as `CONTRIBUTOR` or `FIRST_TIMER`).

When gateway metrics are displayed, filtered events appear in a **DIFC Filtered Events** table alongside the standard server usage table:

```text
┌────────────────────────────────────────────────────────────────────────────────────┐
│ DIFC Filtered Events                                                               │
├────────────────┬───────────────┬───────────────┬──────────────────────────────────-┤
│ Server         │ Tool          │ User          │ Reason                            │
├────────────────┼───────────────┼───────────────┼───────────────────────────────────┤
│ github         │ list_issues   │ new-user      │ Resource has lower integrity than │
│                │               │               │ agent requires.                   │
└────────────────┴───────────────┴───────────────┴───────────────────────────────────┘
```

The `Total DIFC Filtered` count in the summary line shows how many items were suppressed during the run.

### Filtering Logs by Integrity Events

To download only runs that had integrity-filtered content, use the `--filtered-integrity` flag with the `logs` command:

```bash
gh aw logs --filtered-integrity
```

This helps when checking whether `min-integrity` filters the content you expect, or when tuning the level after observing real traffic.

## Learn More

- [GitHub Tools Reference](/gh-aw/reference/github-tools/) — Full `tools.github` configuration
- [MCP Gateway](/gh-aw/reference/mcp-gateway/) — Gateway architecture and log format
- [CLI Reference: logs](/gh-aw/setup/cli/#logs) — Downloading and analyzing workflow run logs
