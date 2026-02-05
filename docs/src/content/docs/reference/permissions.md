---
title: Permissions
description: Configure GitHub Actions permissions for agentic workflows
sidebar:
  order: 500
---

The `permissions:` section controls what GitHub API operations your workflow can perform. GitHub Agentic Workflows uses read-only permissions by default for security, with write operations handled through [safe outputs](/gh-aw/reference/safe-outputs/).

```yaml wrap
permissions:
  contents: read
  actions: read
safe-outputs:
  create-issue:
  add-comment:
```

## Permission Model

### Security-First Design

Agentic workflows follow a principle of least privilege:

- **Read-only by default**: Main job runs with minimal read permissions only
- **Write through safe outputs**: Write operations happen in separate jobs with sanitized content
- **No direct write permissions**: Use safe-outputs instead of `write` permissions in the main job

This model prevents AI agents from accidentally or maliciously modifying repository content during execution.

### Why This Model?

AI agents require careful security controls:

- **Audit Trail**: Separating read (agent) from write (safe outputs) provides clear accountability for all changes
- **Blast Radius Containment**: If an agent misbehaves, it cannot modify code, merge PRs, or delete resources
- **Compliance**: Many organizations require approval workflows for automated changes - safe outputs provide the approval gate
- **Defense in Depth**: Even if prompt injection occurs, the agent cannot perform destructive actions

This model trades convenience for security. Safe outputs add one extra job but provide critical safety guarantees.

### Permission Scopes

Key permissions include `contents` (code access), `issues` (issue management), `pull-requests` (PR management), `discussions`, `actions` (workflow control), `checks`, `deployments`, `packages`, `pages`, and `statuses`. Each has read and write levels. See [GitHub's permissions reference](https://docs.github.com/en/actions/using-jobs/assigning-permissions-to-jobs) for the complete list.

#### Special Permission: `id-token`

The `id-token: write` permission is a special case that is explicitly allowed in workflows, including strict mode. This permission enables [OpenID Connect (OIDC) authentication](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect) for cloud provider authentication (AWS, GCP, Azure) without storing long-lived credentials.

Unlike other write permissions, `id-token: write` does not grant any ability to modify repository content. It only allows the workflow to request a short-lived OIDC token from GitHub's token service for authentication with external cloud providers.

```yaml wrap
# Example: Deploy to AWS using OIDC authentication
permissions:
  id-token: write      # Allowed for OIDC authentication
  contents: read       # Read repository code
```

This permission is safe to use and does not require safe-outputs, even in strict mode.

## Configuration

### Basic Configuration

Specify individual permission levels:

```yaml wrap
permissions:
  contents: read
  actions: read
safe-outputs:
  create-issue:
```

### Shorthand Options

- **`read-all`**: Read access to all scopes (useful for inspection workflows)
- **`{}`**: No permissions (for computation-only workflows)

> [!CAUTION]
> Avoid using `write-all` or direct write permissions in agentic workflows. Use [safe outputs](/gh-aw/reference/safe-outputs/) instead for secure write operations.

## Common Patterns

All workflows should use read-only permissions with safe outputs for write operations:

```yaml wrap
# IssueOps: Read code, comment via safe outputs
permissions:
  contents: read
  actions: read
safe-outputs:
  add-comment:
    max: 5

# PR Review: Read code, review via safe outputs
permissions:
  contents: read
  actions: read
safe-outputs:
  create-pr-review-comment:
    max: 10

# Scheduled: Analysis with issue creation via safe outputs
permissions:
  contents: read
  actions: read
safe-outputs:
  create-issue:
    max: 3

# Manual: Admin tasks with approval gate
permissions: read-all
manual-approval: production
```

## Safe Outputs

Write operations use safe outputs instead of direct API access. This provides content sanitization, rate limiting, audit trails, and security isolation by separating write permissions from AI execution. See [Safe Outputs](/gh-aw/reference/safe-outputs/) for details.

## Permission Validation

Run `gh aw compile workflow.md` to validate permissions. Common errors include undefined permissions, direct write permissions in the main job (use safe outputs instead), and insufficient permissions for declared tools. Use `--strict` mode to enforce read-only permissions and require explicit network configuration.

### Write Permission Policy

Write permissions are blocked by default to enforce the security-first design. Workflows with write permissions will fail compilation with an error:

```
Write permissions are not allowed.

Found write permissions:
  - contents: write

To fix this issue, change write permissions to read:
permissions:
  contents: read
```

**Exception:** The `id-token: write` permission is explicitly allowed as it is used for OIDC authentication with cloud providers and does not grant repository write access.

#### Migrating Existing Workflows

To migrate workflows with write permissions, use the automated codemod (recommended):
```bash
# Check what would be changed (dry-run)
gh aw fix workflow.md

# Apply the fix
gh aw fix workflow.md --write
```

This automatically converts all write permissions to read permissions.

> [!TIP]
> Use Safe Outputs Instead
> For workflows that need to make changes to your repository, use [safe outputs](/gh-aw/reference/safe-outputs/) instead of write permissions. Safe outputs provide a secure way to create issues, pull requests, and comments without granting direct write access to the AI agent.
   :::

#### Scope

This validation applies to:
- Top-level workflow `permissions:` configuration

This validation does **not** apply to:
- Custom jobs (defined in `jobs:` section)
- Safe outputs jobs (defined in `safe-outputs.job:` section)

Custom jobs and safe outputs jobs can have their own permission requirements based on their specific needs.

### Tool-Specific Requirements

Some tools require specific permissions to function:

- **`agentic-workflows`**: Requires `actions: read` to access workflow logs and run data
- **GitHub Model Context Protocol (MCP) toolsets**: See [Tools](/gh-aw/reference/tools/) for GitHub API permission requirements

The compiler validates these requirements and provides clear error messages when permissions are missing.

## Related Documentation

- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Secure write operations with content sanitization
- [Security Guide](/gh-aw/introduction/architecture/) - Security best practices and permission strategies
- [Tools](/gh-aw/reference/tools/) - GitHub API tools and their permission requirements
- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete frontmatter configuration reference
