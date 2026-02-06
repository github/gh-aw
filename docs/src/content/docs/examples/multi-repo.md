---
title: Multi-Repository Examples
description: Complete examples for managing workflows across multiple GitHub repositories, including feature synchronization, cross-repo tracking, and organization-wide updates.
---

Multi-repository operations enable coordinating work across multiple GitHub repositories while maintaining security and proper access controls. These examples demonstrate common patterns for cross-repo workflows.

## Featured Examples

### [Feature Synchronization](/gh-aw/examples/multi-repo/feature-sync/)

Automates code synchronization from main repositories to sub-repositories or downstream services through pull requests with change detection, path filters, and bidirectional sync support. Use for monorepo alternatives, shared component libraries, multi-platform deployments, or fork maintenance.

### [Cross-Repository Issue Tracking](/gh-aw/examples/multi-repo/issue-tracking/)

Centralizes issue tracking by automatically creating tracking issues in a central repository with status synchronization and multi-component coordination. Use for component-based architecture visibility, multi-team coordination, cross-project initiatives, or upstream dependency tracking.

## Getting Started

All multi-repo workflows require proper authentication:

### Personal Access Token Setup

```bash
# Create PAT with required permissions
gh auth token

# Store as repository or organization secret
gh aw secrets set CROSS_REPO_PAT --value "ghp_your_token_here"
```

The PAT needs permissions **only on target repositories** (not the source repository where the workflow runs): `repo` for private repos, `contents: write` for commits, `issues: write` for issues, and `pull-requests: write` for PRs.

> [!TIP]
> **Security Best Practice**: If you only need to read from one repo and write to another, scope your PAT to have read access on the source and write access only on target repositories. Use separate tokens for different operations when possible.

### GitHub App Configuration

For enhanced security, use GitHub Apps for automatic token minting and revocation:

```yaml wrap
safe-outputs:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: "my-org"
    repositories: ["repo1", "repo2", "repo3"]
  create-issue:
    target-repo: "my-org/repo1"
```

GitHub App tokens are minted on-demand, automatically revoked after job completion, and provide better security than long-lived PATs. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#github-app-token-app) for complete configuration.

## Common Patterns

### Hub-and-Spoke Architecture

Central repository aggregates information from multiple component repositories:

```text
Component Repo A ──┐
Component Repo B ──┼──> Central Tracker
Component Repo C ──┘
```

### Upstream-to-Downstream Sync

Main repository propagates changes to downstream repositories:

```text
Main Repo ──> Sub-Repo Alpha
          ──> Sub-Repo Beta
          ──> Sub-Repo Gamma
```

### Organization-Wide Coordination

Single workflow creates issues across multiple repositories:

```text
Control Workflow ──> Repo 1 (tracking issue)
                 ──> Repo 2 (tracking issue)
                 ──> Repo 3 (tracking issue)
                 ──> ... (up to max limit)
```

## Cross-Repository Safe Outputs

Most safe output types support the `target-repo` parameter for cross-repository operations. **Without `target-repo`, these safe outputs operate on the repository where the workflow is running.**

| Safe Output | Cross-Repo Support | Example Use Case |
|-------------|-------------------|------------------|
| `create-issue` | ✅ | Create tracking issues in central repo |
| `add-comment` | ✅ | Comment on issues in other repos |
| `update-issue` | ✅ | Update issue status across repos |
| `add-labels` | ✅ | Label issues in target repos |
| `create-pull-request` | ✅ | Create PRs in downstream repos |
| `create-discussion` | ✅ | Create discussions in any repo |
| `create-agent-session` | ✅ | Create tasks in target repos |
| `update-release` | ✅ | Update release notes across repos |

**Configuration Example:**

```yaml wrap
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-issue:
    target-repo: "org/tracking-repo"  # Cross-repo: creates in tracking-repo
    title-prefix: "[component] "
  add-comment:
    # No target-repo: operates on current repository
```

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for complete configuration options.

## GitHub API Tools for Multi-Repo Access

Enable GitHub toolsets to allow agents to query multiple repositories:

```yaml wrap
tools:
  github:
    toolsets: [repos, issues, pull_requests, actions]
```

Agents can access **repos** (read files, search code, list commits, get releases), **issues** (list and search across repositories), **pull_requests** (list and search PRs), and **actions** (workflow runs and artifacts).

## Best Practices

Use GitHub Apps for automatic token revocation, scope PATs minimally, rotate tokens regularly, and store them as GitHub secrets. Set appropriate `max` limits on safe outputs, use meaningful title prefixes and consistent labels, and include clear documentation in created items. Validate repository access before operations, handle rate limits appropriately, and monitor workflow execution. Test with public repositories first, pilot with small subsets, verify configurations, and monitor costs.

## Advanced Topics

### Private Repository Access

When working with private repositories, ensure the PAT owner has repository access, install GitHub Apps in target organizations, configure repository lists explicitly, and test permissions before full rollout.

### Deterministic Workflows

For direct repository access, use custom engine with `actions/checkout`:

```yaml wrap
engine:
  id: custom
  steps:
    - name: Checkout main repo
      uses: actions/checkout@v5
      with:
        path: main-repo
    
    - name: Checkout secondary repo
      uses: actions/checkout@v5
      with:
        repository: org/secondary-repo
        token: ${{ secrets.CROSS_REPO_PAT }}
        path: secondary-repo
```

### Organization-Level Operations

For organization-wide workflows, use organization-level secrets, configure GitHub Apps at organization level, plan phased rollouts, and provide clear communication.

## Complete Guide

For comprehensive documentation on the MultiRepoOps design pattern, see:

[MultiRepoOps Design Pattern](/gh-aw/patterns/multirepoops/)

## Related Documentation

- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Configuration options
- [GitHub Tools](/gh-aw/reference/tools/#github-tools-github) - API access configuration
- [Security Best Practices](/gh-aw/introduction/architecture/) - Authentication and security
- [Packaging & Distribution](/gh-aw/guides/packaging-imports/) - Sharing workflows
