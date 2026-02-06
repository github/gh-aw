---
title: Feature Synchronization
description: Synchronize features from a main repository to sub-repositories or downstream services with automated pull requests.
sidebar:
  badge: { text: 'Multi-Repo', variant: 'note' }
---

Feature synchronization workflows propagate changes from a main repository to related sub-repositories, ensuring downstream projects stay current with upstream improvements while maintaining proper change tracking through pull requests.

## When to Use

Use feature sync when maintaining related projects in separate repositories (monorepo alternative), propagating library updates to dependent projects, updating platform-specific repos after core changes, or keeping downstream forks synchronized with upstream.

## How It Works

The workflow monitors specific paths in the main repository and creates pull requests in target repositories when changes occur, adapting the changes for each target's structure while maintaining full audit trails.

## Basic Feature Sync

Synchronize changes from shared directory to downstream repository:

```aw wrap
---
on:
  push:
    branches: [main]
    paths:
      - 'shared/**'
permissions:
  contents: read
  actions: read
tools:
  github:
    toolsets: [repos]
  edit:
  bash:
    - "git:*"
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-pull-request:
    target-repo: "myorg/downstream-service"
    title-prefix: "[sync] "
    labels: [auto-sync, upstream-update]
    reviewers: [team-lead]
    draft: true
---

# Sync Shared Components to Downstream Service

When shared components change, synchronize them to `myorg/downstream-service`. Review the git diff, read current versions from the target repo, adapt paths if needed, and create a PR with descriptive commit messages linking to original commits. Include structural adaptations and migration notes for breaking changes.
```

## Multi-Target Sync

Synchronize to multiple repositories simultaneously:

```aw wrap
---
on:
  push:
    branches: [main]
    paths:
      - 'core/**'
permissions:
  contents: read
  actions: read
tools:
  github:
    toolsets: [repos]
  edit:
  bash:
    - "git:*"
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-pull-request:
    max: 3
    title-prefix: "[core-sync] "
    labels: [automated-sync]
    draft: true
---

# Sync Core Library to All Services

When core library files change, create PRs in dependent services (`myorg/api-service`, `myorg/web-frontend`, `myorg/mobile-backend`). For each target, check if they use the changed modules, adapt imports/paths, and create a PR with compatibility notes and links to source commits.
```

## Release-Based Sync

Synchronize when new releases are published:

```aw wrap
---
on:
  release:
    types: [published]
permissions:
  contents: read
  actions: read
tools:
  github:
    toolsets: [repos]
  edit:
  bash:
    - "git:*"
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-pull-request:
    target-repo: "myorg/production-service"
    title-prefix: "[upgrade] "
    labels: [version-upgrade, auto-generated]
    reviewers: [release-manager]
    draft: false
---

# Upgrade Production Service to New Release

When a new release is published (version ${{ github.event.release.tag_name }}), create an upgrade PR that updates version references, applies API changes from release notes, updates configuration for breaking changes, and includes a migration guide with testing recommendations.
```

## Selective File Sync

Synchronize only specific file types or patterns:

```aw wrap
---
on:
  push:
    branches: [main]
    paths:
      - 'types/**/*.ts'
      - 'interfaces/**/*.ts'
permissions:
  contents: read
  actions: read
tools:
  github:
    toolsets: [repos]
  edit:
  bash:
    - "git:*"
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-pull-request:
    target-repo: "myorg/client-sdk"
    title-prefix: "[types] "
    labels: [type-definitions]
    draft: true
---

# Sync TypeScript Type Definitions

Synchronize TypeScript type definitions to client SDK. Identify changed `.ts` files in `types/` and `interfaces/`, update them in `myorg/client-sdk` while preserving client-specific extensions, validate no breaking changes, and document any compatibility concerns.
```

## Bidirectional Sync with Conflict Detection

Handle bidirectional synchronization with conflict awareness:

```aw wrap
---
on:
  push:
    branches: [main]
    paths:
      - 'shared-config/**'
permissions:
  contents: read
  actions: read
tools:
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - "git:*"
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-pull-request:
    target-repo: "myorg/sister-project"
    title-prefix: "[config-sync] "
    labels: [config-update, needs-review]
    draft: true
---

# Bidirectional Config Sync

Synchronize shared configuration between this project and `myorg/sister-project`, which may be modified independently. Compare timestamps and change history; if conflicts are detected, create a PR marked for manual review with conflict notes. If no conflict, apply changes automatically and record sync timestamp.
```

## Feature Branch Sync

Synchronize feature branches between repositories:

```aw wrap
---
on:
  pull_request:
    types: [opened, synchronize]
    branches:
      - 'feature/**'
permissions:
  contents: read
  pull-requests: read
  actions: read
tools:
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - "git:*"
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-pull-request:
    target-repo: "myorg/integration-tests"
    title-prefix: "[feature-test] "
    labels: [feature-branch, auto-sync]
    draft: true
---

# Sync Feature Branch for Integration Testing

When feature branch ${{ github.event.pull_request.head.ref }} (PR #${{ github.event.pull_request.number }}) is updated, create a matching branch in the integration test repo, sync relevant changes, update test configurations, and create a PR linking to the source with test scenarios and integration points.
```

## Scheduled Sync Check

Regularly check for sync drift and create catch-up PRs:

```aw wrap
---
on: weekly on monday
permissions:
  contents: read
  actions: read
tools:
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - "git:*"
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-pull-request:
    target-repo: "myorg/downstream-fork"
    title-prefix: "[weekly-sync] "
    labels: [scheduled-sync]
    draft: true
---

# Weekly Sync Check

Check for accumulated changes needing synchronization to downstream fork. Find the last sync PR, identify all commits since then, categorize changes (features, fixes, docs), and create a comprehensive PR grouping commits by category with breaking changes highlighted and migration guidance.
```

## Authentication Setup

Cross-repo sync workflows require authentication via PAT or GitHub App.

### PAT Configuration

Create a PAT with `repo`, `contents: write`, and `pull-requests: write` permissions, then store it as a repository secret:

```bash
gh aw secrets set CROSS_REPO_PAT --value "ghp_your_token_here"
```

### GitHub App Configuration

For enhanced security, use GitHub App installation tokens:

```yaml wrap
safe-outputs:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    repositories: ["downstream-service", "integration-tests"]
  create-pull-request:
    target-repo: "myorg/downstream-service"
```

## Best Practices

**Change Detection**: Use path filters to avoid unnecessary runs, check for meaningful changes (not just whitespace), group related changes into single PRs, and track sync history to avoid duplicates.

**PR Management**: Create draft PRs for syncs requiring review, use consistent labels, add appropriate reviewers, and include comprehensive descriptions linking to source commits.

**Error Handling**: Handle merge conflicts with clear documentation, validate target repo structure before creating PRs, provide rollback instructions, and monitor for failed syncs.

**Testing**: Test sync workflows on public repos first, verify path mappings, check for breaking changes, and validate in staging before production.

## Related Documentation

- [MultiRepoOps Design Pattern](/gh-aw/patterns/multirepoops/) - Complete multi-repo overview
- [Cross-Repo Issue Tracking](/gh-aw/examples/multi-repo/issue-tracking/) - Issue management patterns
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Pull request configuration
- [GitHub Tools](/gh-aw/reference/tools/#github-tools-github) - Repository access tools
