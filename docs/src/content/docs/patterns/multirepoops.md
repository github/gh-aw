---
title: MultiRepoOps
description: Coordinate agentic workflows across multiple GitHub repositories with automated issue tracking, feature synchronization, and organization-wide enforcement
sidebar:
  badge: { text: 'Advanced', variant: 'caution' }
---

MultiRepoOps extends operational automation patterns (IssueOps, ChatOps, etc.) across multiple GitHub repositories. Using cross-repository safe outputs and secure authentication, MultiRepoOps enables coordinating work between related projects-creating tracking issues in central repos, synchronizing features to sub-repositories, and enforcing organization-wide policies-all through AI-powered workflows.

## When to Use MultiRepoOps

- **Feature synchronization** - Propagate changes from main repositories to sub-repos or forks
- **Hub-and-spoke tracking** - Centralize issue tracking across component repositories
- **Organization-wide enforcement** - Roll out security patches, policy updates, or dependency changes across all repos
- **Monorepo alternatives** - Coordinate packages or services living in separate repositories
- **Upstream/downstream workflows** - Sync features between upstream dependencies and downstream consumers

## How It Works

MultiRepoOps workflows use the `target-repo` parameter on safe outputs to create issues, pull requests, and comments in external repositories. Combined with GitHub API toolsets for querying remote repos and proper authentication (PAT or GitHub App tokens), workflows can coordinate complex multi-repository operations automatically.

```aw wrap
---
on:
  issues:
    types: [opened, labeled]
permissions:
  contents: read
  actions: read
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-issue:
    target-repo: "org/tracking-repo"
    title-prefix: "[component-a] "
    labels: [tracking, multi-repo]
---

# Cross-Repo Issue Tracker

When issues are created in component repositories, automatically create tracking issues in the central coordination repo.

Analyze the issue and create a tracking issue that:
- Links back to the original component issue
- Summarizes the problem and impact
- Tags relevant teams across the organization
- Provides context for cross-component coordination
```

This workflow creates a hub-and-spoke architecture where component repositories automatically report issues to a central tracking repository.

## Authentication for Cross-Repo Access

Cross-repository operations require authentication beyond the default `GITHUB_TOKEN`, which is scoped to the current repository only.

### Personal Access Token (PAT)

Configure a Personal Access Token with access to target repositories:

```yaml wrap
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-issue:
    target-repo: "org/tracking-repo"
```

**Required Permissions:**

The PAT needs permissions **only on target repositories** where you want to create resources, not on the source repository where the workflow runs.

- Repository access to target repos (public or private)
- `contents: write`, `issues: write`, `pull-requests: write` (depending on operations)

> [!TIP]
> Security Best Practice
> If you only need to read from one repo and write to another, scope your PAT to have read access on the source and write access only on target repositories.

### GitHub App Installation Token

For enhanced security, use GitHub Apps with automatic token revocation:

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

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for complete authentication configuration.

## Common MultiRepoOps Patterns

### Hub-and-Spoke Issue Tracking

Central repository aggregates issues from multiple component repositories:

```text
Component Repo A ──┐
Component Repo B ──┼──> Central Tracker
Component Repo C ──┘
```

Each component workflow creates tracking issues in the central repo using `target-repo` parameter.

### Upstream-to-Downstream Sync

Main repository propagates changes to downstream repositories:

```text
Main Repo ──> Sub-Repo Alpha
          ──> Sub-Repo Beta
          ──> Sub-Repo Gamma
```

Use cross-repo pull requests with `create-pull-request` safe output and `target-repo` configuration.

### Organization-Wide Coordination

Single workflow creates issues across multiple repositories:

```text
Control Workflow ──> Repo 1 (tracking issue)
                 ──> Repo 2 (tracking issue)
                 ──> Repo 3 (tracking issue)
```

Agent generates multiple tracking issues with different `target-repo` values (up to configured `max` limit).

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

## Teaching Agents Multi-Repo Access

Enable GitHub toolsets to allow agents to query multiple repositories:

```yaml wrap
tools:
  github:
    toolsets: [repos, issues, pull_requests, actions]
```

**Available Operations:**
- **repos**: Read files, search code, list commits, get releases
- **issues**: List and search issues across repositories
- **pull_requests**: List and search PRs across repositories
- **actions**: Access workflow runs and artifacts

Agent instructions can reference remote repositories:

```markdown
Search for open issues in org/upstream-repo related to authentication.
Check the latest release notes from org/dependency-repo.
Compare code structure between this repo and org/reference-repo.
```

## Deterministic Multi-Repo Workflows

For direct repository access without agent involvement, use custom engine with multiple checkouts:

```aw wrap
---
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

    - name: Compare and sync
      run: |
        # Deterministic sync logic
        rsync -av main-repo/shared/ secondary-repo/shared/
        cd secondary-repo
        git add .
        git commit -m "Sync from main repo"
        git push
---

# Deterministic Feature Sync

Custom workflow that directly checks out multiple repos and synchronizes files.
```

## Example Workflows

Explore detailed MultiRepoOps examples:

- **[Feature Synchronization](/gh-aw/examples/multi-repo/feature-sync/)** - Sync code changes from main repo to sub-repositories
- **[Cross-Repo Issue Tracking](/gh-aw/examples/multi-repo/issue-tracking/)** - Hub-and-spoke tracking architecture

## Best Practices

### Authentication and Security

- Use GitHub Apps for automatic token revocation
- Scope PATs minimally to required repositories
- Rotate tokens regularly
- Store tokens as GitHub secrets (never in code)

### Workflow Design

- Set appropriate `max` limits on safe outputs
- Use meaningful title prefixes for tracking
- Apply consistent labels across repositories
- Include clear documentation in created items

### Error Handling

- Validate repository access before operations
- Handle rate limits appropriately
- Provide fallback for permission failures
- Monitor workflow execution across repositories

### Testing

- Test with public repositories first
- Pilot with small repository subset
- Verify path mappings and configurations
- Monitor costs and rate limits

## Advanced Topics

### Private Repository Access

When working with private repositories:
- Ensure PAT owner has repository access
- Install GitHub Apps in target organizations
- Configure repository lists explicitly
- Test permissions before full rollout

### Organization-Level Operations

For organization-wide workflows:
- Use organization-level secrets
- Configure GitHub Apps at organization level
- Plan phased rollouts
- Provide clear communication

## Related Patterns

- **[IssueOps](/gh-aw/patterns/issueops/)** - Single-repo issue automation
- **[ChatOps](/gh-aw/patterns/chatops/)** - Command-driven workflows
- **[Orchestration](/gh-aw/patterns/orchestration/)** - Multi-issue initiative coordination

## Related Documentation

- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Complete safe output configuration
- [GitHub Tools](/gh-aw/reference/tools/#github-tools-github) - GitHub API toolsets
- [Security Best Practices](/gh-aw/introduction/architecture/) - Authentication and token security
- [Packaging & Distribution](/gh-aw/guides/packaging-imports/) - Sharing workflows across repos
