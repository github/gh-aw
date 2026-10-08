---
title: GitHub Tools Read Permissions
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

This separation provides an audit trail, limits blast radius if an agent misbehaves, supports compliance approval gates, and defends against prompt injection. Safe outputs add one extra job but provide critical safety guarantees.

## Permission Scopes

Key read permission scopes include:

- `contents` (code access)
- `issues` (issue management)
- `pull-requests` (PR management)
- `discussions` (discussions and comments)
- `actions` (workflow control)
- `checks` (checks and statuses)
- `deployments` (deployment management)
- `packages` (package management)
- `pages` (GitHub Pages management)
- `statuses` (commit status management)
- `attestations` (artifact attestations)

See [GitHub's permissions reference](https://docs.github.com/en/actions/using-jobs/assigning-permissions-to-jobs) for the complete list.

**Shorthand Options:**

- **`read-all`**: Read access to all scopes (useful for inspection workflows)
- **`{}`** or **`none`**: No permissions (for computation-only workflows)

### GitHub App-Only Permissions

Certain permission scopes require [additional authentication](/gh-aw/reference/github-tools/#additional-authentication-for-github-tools). These include:

> **Note:** All scopes in this section must always be declared as `read`.

**Repository-level** (repository configuration and access):
`administration`, `environments`, `git-signing`, `workflows`, `repository-hooks`,
`single-file`, `codespaces`, `repository-custom-properties`, `secret-scanning-alerts`

**Organization-level** (organization membership and settings):
`organization-projects`, `members`, `organization-administration`, `team-discussions`,
`organization-hooks`, `organization-members`, `organization-packages`,
`organization-self-hosted-runners`, `organization-custom-org-roles`,
`organization-custom-properties`, `organization-custom-repository-roles`,
`organization-announcement-banners`, `organization-events`, `organization-plan`,
`organization-user-blocking`, `organization-personal-access-token-requests`,
`organization-personal-access-tokens`, `organization-copilot`, `organization-codespaces`

**User-level** (user account access):
`email-addresses`, `codespaces-lifecycle-admin`, `codespaces-metadata`

### Special Permission: `id-token`

The `id-token` permission controls access to GitHub's OIDC token service for [OpenID Connect (OIDC) authentication](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect) with cloud providers (AWS, GCP, Azure).

The only valid values are `write` and `none`. `id-token: read` is not a valid permission and will be rejected at compile time.

Unlike other write permissions, `id-token: write` does not grant any ability to modify repository content. It only allows the workflow to request a short-lived OIDC token from GitHub's token service for authentication with external cloud providers.

```yaml wrap
# Example: Deploy to AWS using OIDC authentication
permissions:
  id-token: write      # Allowed for OIDC authentication
  contents: read       # Read repository code
```

This permission does not require safe-outputs.

The compiler emits an informational reminder to validate OIDC audiences and
cloud-provider trust policies. The reminder does not count as a warning or
block warnings-as-errors validation, including `gh aw compile --dry-run`.

### Special Permission: `copilot-requests: write`

The `copilot-requests: write` permission enables Copilot inference using the built-in GitHub Actions token. This is the recommended way to authenticate Copilot for workflows running in organizations with a Copilot subscription.

```yaml wrap
permissions:
  contents: read
  copilot-requests: write
```

When `copilot-requests: write` is set, gh-aw uses the GitHub Actions token for all Copilot inference calls. `COPILOT_GITHUB_TOKEN` and `GH_AW_GITHUB_TOKEN` are **ignored** for inference — you do not need to configure either secret. Billing is handled centrally through your organization's Copilot plan.

The only valid value is `write`. See [Authentication → `copilot-requests: write` permission](/gh-aw/reference/auth/#copilot-requests-write-permission) for setup details and prerequisites.

## Learn More

- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Secure write operations with content sanitization
- [Security Guide](/gh-aw/introduction/architecture/) - Security best practices and permission strategies
- [Tools](/gh-aw/reference/tools/) - GitHub API tools and their permission requirements
- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete frontmatter configuration reference
