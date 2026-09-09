---
description: Overview and practical guidance for configuring and using GitHub MCP server toolsets in agentic workflows.
---

# GitHub MCP Server Instructions

**Source**: [github/github-mcp-server](https://github.com/github/github-mcp-server/tree/main/pkg/github)
**Mapping File**: [pkg/workflow/data/github_toolsets_permissions.json](https://github.com/github/gh-aw/blob/main/pkg/workflow/data/github_toolsets_permissions.json)
**Last Updated**: 2026-08-16

## Overview

The GitHub MCP server provides tools to interact with GitHub APIs through the Model Context Protocol (MCP). It operates in two modes:

- **Remote mode**: Connects to GitHub's hosted MCP endpoint (`https://api.githubcopilot.com/mcp/`)
- **Local mode**: Runs `gh mcp` (GitHub CLI) as a local subprocess

### Authentication

**Remote mode**: Uses a Bearer token in the Authorization header:
```
Authorization: Bearer <github-token>
```

**Read-only mode**: Add the `X-MCP-Readonly: true` header to restrict to read operations only:
```
X-MCP-Readonly: true
```

**Local mode**: Uses the GitHub CLI's existing authentication (`gh auth login`).

## Configuration

### In Agentic Workflows

```yaml
tools:
  github:
    toolsets: [default]     # or specific toolsets
    # Optional: GitHub App authentication
    github-app:
      client-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
```

> ⚠️ **Do NOT use `mode: remote`** in GitHub Actions workflows. Remote mode does not work with the GitHub Actions token (`GITHUB_TOKEN`) — it requires a special PAT or GitHub App token with MCP access. The default `mode: local` (Docker-based) works with `GITHUB_TOKEN` and should always be used.
>
> GitHub App authentication only scopes the token. It does not replace DIFC guard policy labels. When safe outputs are enabled, compile the workflow and confirm the lock file includes both `mcp_servers.github.guard-policies.allow-only` and `mcp_servers.safeoutputs.guard-policies.write-sink`.

### Toolset Options

- `[default]` — Recommended defaults: `context`, `repos`, `issues`, `pull_requests`
- `[all]` — Enable all toolsets
- Specific toolsets: `[repos, issues, pull_requests, discussions]`
- Extend defaults: `[default, discussions, actions]`

## Recommended Default Toolsets

The following toolsets are recommended as defaults for typical agentic workflows:

When the GitHub tool is configured, gh-aw injects a separate `<github-context>` prompt block with workflow identity metadata. That injected block is independent of toolset selection, so enable `context` for its team-awareness tools, not to obtain workflow identity.

| Toolset | Rationale |
|---------|-----------|
| `context` | Team-awareness helpers (`get_teams`, `get_team_members`) — enable when workflows need org or team membership lookups |
| `repos` | Core repository operations (read files, list commits/branches) — most workflows need file access |
| `issues` | Issue management (read, comment, create) — common in CI/CD and automation workflows |
| `pull_requests` | PR operations (read, create, review) — critical for code review and merge automation |

**Enable explicitly when needed** (not in defaults):

| Toolset | When to Enable |
|---------|---------------|
| `actions` | Workflow introspection, triggering runs |
| `code_quality` | Code quality finding lookups |
| `code_security` | Code scanning alert management |
| `copilot` | Copilot assignment, PR creation, and review requests |
| `copilot_issue_intents` | Intent-aware Copilot issue assignment |
| `copilot_spaces` | GitHub Copilot Spaces (remote mode only) |
| `dependabot` | Dependency vulnerability management |
| `discussions` | Community discussion workflows |
| `gists` | Gist creation and management |
| `git` | Git API operations (tree, refs) |
| `github_support_docs_search` | GitHub support documentation search (remote mode only) |
| `labels` | Label management automation |
| `notifications` | Notification processing agents |
| `orgs` | Organization search operations |
| `projects` | GitHub Projects automation (requires PAT) |
| `secret_protection` | Secret scanning alert management |
| `security_advisories` | Advisory database queries |
| `stargazers` | Star/unstar repository operations |
| `users` | User search operations |

## Tools by Toolset

See [github-mcp-server-tools.md](github-mcp-server-tools.md) for the full per-toolset tool reference (parameters, known quirks like the `search_repositories` `repo:` limitation).

---

## Pagination

MCP tool responses have a **25,000 token limit**; always pass an explicit `perPage`. See [github-mcp-server-pagination.md](github-mcp-server-pagination.md) for per-tool `perPage` defaults, the pagination loop pattern, known tool quirks (`list_label`, `list_workflows`, `search_repositories` with `repo:`), and oversized-response recovery.

---

## Best Practices

### Toolset Selection

1. **Start with defaults** (`context`, `repos`, `issues`, `pull_requests`) for most workflows
2. **Add toolsets incrementally** based on actual needs rather than enabling `all`
3. **Security toolsets** (`code_security`, `dependabot`, `secret_protection`, `security_advisories`) require `security-events` permission
4. **Write operations** require appropriate GitHub token permissions (see `write_permissions` in the JSON mapping)
5. **Projects toolset** requires a PAT (Personal Access Token) — `GITHUB_TOKEN` lacks the required `project` scope

### Permission Requirements

Most toolsets work with the default `GITHUB_TOKEN` in GitHub Actions. Exceptions:

- `projects` — Requires a PAT with `project` scope
- `security_advisories` (write) — Requires `security-events: write` permission
- `actions` (write for `actions_run_trigger`) — Requires `actions: write` permission
