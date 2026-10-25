---
title: GitHub Integrity Filtering
description: How integrity filtering restricts agent access to GitHub content based on author trust and merge status, and how filtered events appear in logs.
sidebar:
  order: 680
---

Integrity filtering (`tools.github.min-integrity`) controls which GitHub content an agent can access during a workflow run. Rather than filtering by permissions, it filters by **trust**: the author association of an issue, pull request, or comment, and whether that content has been merged into the main branch.

## Migrate from lockdown mode

GitHub lockdown mode has been superseded by integrity filtering. Replace `lockdown: true` with `min-integrity: approved`:

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

Replace `lockdown: false` with `min-integrity: none`:

```yaml wrap
# Before (deprecated)
tools:
  github:
    lockdown: false

# After
tools:
  github:
    min-integrity: none
```

## How It Works

The MCP gateway intercepts tool calls to GitHub and applies integrity checks to each piece of content returned. If an item's integrity level is below the configured minimum, the gateway removes it before the AI engine sees it. This happens transparently — the agent receives a reduced result set, and filtered items are logged as `DIFC_FILTERED` events for later inspection.

Starting with [MCP gateway v0.5.0](https://github.com/github/gh-aw-mcpg/releases/tag/v0.5.0), proxy responses that cannot be filtered item by item, such as artifact downloads, raw content, and redirects, require resource-level authorization even in filter mode. These requests can be denied when the configured access policy does not authorize the resource.

## Configuration

Set `min-integrity` under `tools.github` in your workflow frontmatter:

```aw wrap
tools:
  github:
    min-integrity: approved
```

`min-integrity` can be specified alone. When `allowed-repos` is omitted, it defaults to `"all"`. If `allowed-repos` is also specified, both fields must be present.

```aw wrap
tools:
  github:
    allowed-repos: "myorg/*"
    min-integrity: approved
```

## Configuration Reference

All integrity-filtering inputs are specified under `tools.github` in your workflow frontmatter. The table below summarizes every available field:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `min-integrity` | string | Yes (when any guard policy field is used) | `approved` for public repos; none for private | Minimum integrity level: `merged`, `approved`, `unapproved`, or `none` |
| `allowed-repos` | string or array | No | `"all"` | Repository scope: `"all"`, `"public"`, or an array of patterns like `["myorg/*", "partner/repo"]` |
| `blocked-users` | array or expression | No | `[]` | GitHub usernames whose content is unconditionally denied |
| `trusted-users` | array or expression | No | `[]` | GitHub usernames elevated to `approved` integrity regardless of author association |
| `approval-labels` | array or expression | No | `[]` | GitHub label names that promote items to `approved` integrity |
| `refusal-labels` | array or expression | No | `[]` | GitHub label names that downgrade items to `none` integrity, overriding any promotion from `trusted-users` or `approval-labels` |
| `integrity-proxy` | boolean | No | `true` | Whether to run the DIFC proxy for pre-agent `gh` CLI calls. Set to `false` to disable |
| `endorsement-reactions` | array | No | `["THUMBS_UP", "HEART"]` (when `integrity-reactions` enabled) | Reaction types that promote item integrity to `approved`. Requires `features.integrity-reactions: true` |
| `disapproval-reactions` | array | No | `["THUMBS_DOWN", "CONFUSED"]` (when `integrity-reactions` enabled) | Reaction types that demote item integrity. Requires `features.integrity-reactions: true` |
| `endorser-min-integrity` | string | No | `approved` (when `integrity-reactions` enabled) | Minimum integrity of the reactor for an endorsement or disapproval to take effect. Requires `features.integrity-reactions: true` |
| `disapproval-integrity` | string | No | `none` (when `integrity-reactions` enabled) | Integrity level assigned when a qualifying disapproval reaction is added. Requires `features.integrity-reactions: true` |

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

The four configurable levels (`merged`, `approved`, `unapproved`, `none`) are cumulative and ordered from most restrictive to least. Setting `min-integrity: approved` means only items at `approved` level **or higher** (`merged`) reach the agent. Items at `unapproved` or `none` are filtered out.

`blocked` is not a configurable `min-integrity` value — it is assigned automatically to items from users in `blocked-users` and is always denied regardless of the threshold, even when `min-integrity: none`. See [Blocking specific users](#blocking-specific-users).

## Scoping to Repositories

`allowed-repos` defines which repositories the guard policy applies to. It accepts three forms:

- **`"all"`** — All repositories the token can access (default when omitted).
- **`"public"`** — Only public repositories.
- **An array of patterns** — Specific repositories or owner wildcards.

```aw wrap
tools:
  github:
    allowed-repos:
      - "myorg/*"
      - "partner/shared-repo"
    min-integrity: approved
```

Repository patterns must be lowercase and follow one of these formats:

| Pattern | Meaning |
|---------|---------|
| `owner/*` | All repositories under `owner` |
| `owner/prefix*` | Repositories under `owner` whose name starts with `prefix` |
| `owner/repo` | A single specific repository |

## Adjusting Integrity Per-Item

Beyond setting a minimum level, you can override integrity for specific authors or labels.

### Blocking specific users

`blocked-users` unconditionally blocks content from listed GitHub usernames, regardless of `min-integrity`, `trusted-users`, or any labels. Blocked items receive an effective integrity of `blocked` (below `none`) and are always denied.

```aw wrap
tools:
  github:
    min-integrity: none
    blocked-users:
      - "spam-bot"
      - "compromised-account"
```

Use this to suppress content from known-bad accounts — automated bots, compromised users, or external contributors pending security review.

### Trusting specific users

`trusted-users` elevates content from listed GitHub usernames to `approved` integrity, regardless of their author association. This is useful for contractors, partner developers, or external contributors who should be treated as trusted even though GitHub classifies them as `CONTRIBUTOR` or `FIRST_TIME_CONTRIBUTOR`.

Bot logins such as `github-actions[bot]` are also valid. Use the full login, including the `[bot]` suffix. Trust is configured per author and does not extend to other authors' comments on a trusted bot's issue or pull request. Listing a bot does not require lowering `min-integrity` to `none`.

> [!NOTE]
> MCP gateway v0.4.37 fixes author-trust handling for wrapped `issue_read` responses. Earlier versions could filter trusted authors' issue bodies as `DIFC_FILTERED` at `min-integrity: approved`. Workflows compiled with an earlier gateway version must be recompiled with an updated CLI to use the fix; see the [runtime fix and regression coverage](https://github.com/github/gh-aw-mcpg/pull/15292).

```aw wrap
tools:
  github:
    min-integrity: approved
    trusted-users:
      - "contractor-1"
      - "partner-dev"
      - "github-actions[bot]"
```

Trust elevation only raises integrity — it never lowers it. A user already at `merged` stays at `merged`. `blocked-users` always takes precedence: if a user appears in both `blocked-users` and `trusted-users`, they are blocked.

`trusted-users` requires `min-integrity` to be set.

### Promoting items via labels

`approval-labels` promotes items bearing any listed GitHub label to `approved` integrity, enabling human-review workflows where a trusted reviewer labels content to signal it is safe for the agent.

```aw wrap
tools:
  github:
    min-integrity: approved
    approval-labels:
      - "human-reviewed"
      - "safe-for-agent"
```

This is useful when a workflow's `min-integrity` would normally filter out external contributions, but a maintainer can label specific items to let them through.

Promotion only raises integrity — it never lowers it. An item already at `merged` stays at `merged`. Blocked-user exclusion always takes precedence: a blocked user's items remain blocked even if they carry an approval label.

### Refusing items via labels

`refusal-labels` is the inverse of `approval-labels`. Items bearing any listed GitHub label are downgraded to `none` integrity, regardless of the author's association or any promotion from `trusted-users` or `approval-labels`.

```aw wrap
tools:
  github:
    min-integrity: approved
    refusal-labels:
      - "needs-security-review"
      - "do-not-automate"
```

This is useful when a workflow's `min-integrity` would normally allow certain content, but a maintainer can label specific items to suppress them from the agent — for example, issues flagged as security-sensitive or pull requests pending a manual compliance check.

Refusal always overrides promotion: if an item carries both an `approval-labels` label and a `refusal-labels` label, the item's effective integrity is set to `none`. Blocked-user exclusion still takes precedence: a blocked user's items remain blocked regardless of any labels.

### Promoting and demoting items via reactions

`features.integrity-reactions: true` allows maintainers to adjust item integrity using GitHub reactions, without adding labels or modifying issue state. Available from gh-aw v0.68.2.

```aw wrap
features:
  integrity-reactions: true
tools:
  github:
    min-integrity: approved
```

When enabled, the compiler automatically enables the CLI proxy (required to identify reaction authors) and injects default reaction configuration. When an account at or above `endorser-min-integrity` adds an endorsement reaction to an issue or comment, the item's integrity is promoted to `approved`. A disapproval reaction from such an account sets the item's integrity to `disapproval-integrity`.

The defaults are `endorsement-reactions: [THUMBS_UP, HEART]`, `disapproval-reactions: [THUMBS_DOWN, CONFUSED]`, `endorser-min-integrity: approved`, and `disapproval-integrity: none`. To override them, set the reaction fields explicitly under `tools.github`:

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

Valid reaction values: `THUMBS_UP`, `THUMBS_DOWN`, `HEART`, `HOORAY`, `CONFUSED`, `ROCKET`, `EYES`, `LAUGH`. The reaction fields only take effect when `features.integrity-reactions: true` is also set.

### Using GitHub Actions expressions

`blocked-users`, `trusted-users`, `approval-labels`, and `refusal-labels` can each accept a GitHub Actions expression instead of a literal array. The expression is evaluated at runtime and should resolve to a comma- or newline-separated list of values.

```aw wrap
tools:
  github:
    min-integrity: approved
    blocked-users: ${{ vars.BLOCKED_USERS }}
    trusted-users: ${{ vars.TRUSTED_USERS }}
    approval-labels: ${{ vars.APPROVAL_LABELS }}
    refusal-labels: ${{ vars.REFUSAL_LABELS }}
```

This is useful for managing lists centrally via GitHub repository or organization variables rather than duplicating them across workflows.

### Effective integrity computation

The gateway derives each item's effective integrity from the base level (author association, merge status, repo visibility), then applies the first matching rule below. The `min-integrity` threshold check runs against the result.

1. Author in `blocked-users` → `blocked` (always denied).
2. Item has a `refusal-labels` label → `none` (overrides any promotion).
3. Author in `trusted-users` → max(base, `approved`).
4. Item has an `approval-labels` label → max(base, `approved`).
5. Otherwise → base.

## Centralized Management via GitHub Variables

Each per-item list (`blocked-users`, `trusted-users`, `approval-labels`, `refusal-labels`) can also be extended centrally using GitHub repository or organization variables. The runtime automatically unions the per-workflow values with the corresponding variable:

| Workflow field | GitHub variable |
|---------------|----------------|
| `blocked-users` | `GH_AW_GITHUB_BLOCKED_USERS` |
| `trusted-users` | `GH_AW_GITHUB_TRUSTED_USERS` |
| `approval-labels` | `GH_AW_GITHUB_APPROVAL_LABELS` |
| `refusal-labels` | `GH_AW_GITHUB_REFUSAL_LABELS` |

For example, if a workflow declares `blocked-users: ["spam-bot"]` and the organization variable `GH_AW_GITHUB_BLOCKED_USERS` is set to `compromised-acct,old-bot`, the effective blocked-users list at runtime is `["spam-bot", "compromised-acct", "old-bot"]`.

Variables are split on commas and newlines, trimmed, and deduplicated. Set these as repository variables (under **Settings → Secrets and variables → Actions → Variables**) or as organization-level variables to apply them across all workflows.

This mechanism allows a security team to maintain a shared blocked-users list, approval-labels policy, or refusal-labels policy without modifying individual workflow files.

## Default Behavior

For **public repositories**, if no `min-integrity` is configured, the runtime automatically applies `min-integrity: approved`. This protects public workflows even when additional authentication has not been set up.

For **private and internal repositories**, no guard policy is applied automatically. Content from all users is accessible by default.

## Pre-Agent Integrity Proxy

When a guard policy is configured (`min-integrity` is set), the compiler injects a DIFC proxy that filters `gh` CLI calls in pre-agent setup steps. This ensures that custom steps running before the agent see the same integrity-filtered API responses that the agent itself operates under.

The proxy:

- Routes `gh` CLI calls through integrity filtering using the same MCP gateway container.
- Applies the static guard policy fields (`min-integrity` and `allowed-repos`) that are available at compile time.
- Does **not** apply `blocked-users`, `trusted-users`, `approval-labels`, or `refusal-labels` (those are resolved at runtime after the proxy starts).
- Is automatically started before custom steps and stopped before the MCP gateway starts to avoid double-filtering.

### Disabling the proxy

The proxy is enabled by default whenever a guard policy is configured. To disable it, set `integrity-proxy: false`:

```aw wrap
tools:
  github:
    min-integrity: approved
    integrity-proxy: false
```

This is an opt-out escape hatch for workflows where pre-agent steps should not be filtered — for example, when custom steps need unfiltered API access for setup purposes.

Disabling the proxy only affects pre-agent `gh` CLI calls. The agent itself always operates under the configured guard policy via the MCP gateway.

## Choosing a Level

The right level depends on who you want the agent to see content from:

- **Workflows that automate code review or apply changes**: `merged` or `approved` — only act on trusted content.
- **Workflows that respond to maintainers and trusted contributors**: `approved` — a common, safe default for most workflows.
- **Community triage or planning workflows**: `unapproved` — allow contributor input while excluding anonymous or first-time interactions.
- **Public-data workflows or spam detection**: `none` — see all activity, but ensure the workflow's outputs are not directly applied without review.

> [!NOTE]
> Setting `min-integrity: none` on a public repository disables the automatic protection. Only use it when the workflow is designed to handle untrusted input.

## Examples

**Production-only content (strictest):**

```aw wrap
tools:
  github:
    allowed-repos: "all"
    min-integrity: merged
```

**Community triage workflow:**

```aw wrap
tools:
  github:
    min-integrity: unapproved
```

**Combined policy — blocking, trusting, labeling, and refusing:**

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

See [Adjusting Integrity Per-Item](#adjusting-integrity-per-item) above for individual snippets covering each field.

## In Logs and Reports

When an item is filtered by the integrity check, the MCP gateway records a `DIFC_FILTERED` event in the run's `gateway.jsonl` log. Each event includes:

- **Server**: the MCP server that returned the filtered content
- **Tool**: the tool call that produced it (e.g., `list_issues`, `get_pull_request`)
- **User**: the login of the content's author
- **Reason**: a description such as `"Resource has lower integrity than agent requires."`
- **Integrity tags**: the tags assigned to the item that caused it to be filtered
- **Author association**: the GitHub author association (`CONTRIBUTOR`, `FIRST_TIMER`, etc.)

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

This is useful when investigating whether your `min-integrity` configuration is filtering expected content or when tuning the level after observing real traffic patterns.

## Learn More

- [GitHub Tools Reference](/gh-aw/reference/github-tools/) — Full `tools.github` configuration
- [MCP Gateway](/gh-aw/reference/mcp-gateway/) — Gateway architecture and log format
- [CLI Reference: logs](/gh-aw/setup/cli/#logs) — Downloading and analyzing workflow run logs
