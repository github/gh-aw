---
title: Getting started
description: Quick start guide for creating workflows with project tracking
---

This guide shows how to create a workflow with project tracking enabled.

## Prerequisites

- Repository with GitHub Agentic Workflows installed
- GitHub Actions enabled
- A GitHub Projects board (or create one during setup)

## Create a workflow with project tracking

1. **Create a new workflow file** at `.github/workflows/dependency-scanner.md`:

```yaml wrap
---
on:
  schedule:
    - cron: "0 0 * * 1"  # Weekly on Monday
project: https://github.com/orgs/myorg/projects/1
safe-outputs:
  create-issue:
    max: 10
---

# Dependency Scanner

Scan for outdated dependencies and create tracking issues.

## Task

1. Check for outdated npm packages
2. Create an issue for each outdated package
3. The issue will be automatically added to the project board
```

2. **Set up authentication** for project access:

```bash
gh aw secrets set GH_AW_PROJECT_GITHUB_TOKEN --value "YOUR_PROJECT_TOKEN"
```

See [GitHub Projects V2 Tokens](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2) for token setup.

3. **Compile the workflow**:

```bash
gh aw compile
```

4. **Commit and push**:

```bash
git add .github/workflows/dependency-scanner.md
git add .github/workflows/dependency-scanner.lock.yml
git commit -m "Add dependency scanner workflow"
git push
```

## How it works

When the workflow runs:

1. The AI agent analyzes your repository for outdated dependencies
2. Creates issues for packages that need updating
3. Each issue is automatically added to your GitHub Project
4. The project board updates with the new items

## Coordinating multiple workflows

You can create additional workflows that share the same project:

```yaml wrap
# .github/workflows/dependency-updater.md
---
on:
  workflow_dispatch:
project: https://github.com/orgs/myorg/projects/1
safe-outputs:
  create-pull-request:
    max: 5
---

# Dependency Updater

Create PRs to update dependencies based on project issues.
```

Both workflows will track their items in the same project board.

## Best practices

- **Start small** - Begin with one workflow and add more as needed
- **Set conservative limits** - Use `max-updates: 10` in project config to start
- **Test manually** - Use `workflow_dispatch` trigger to test before scheduling
- **Monitor progress** - Check your project board to see tracked items

## Next steps

- [Project Tracking Example](/gh-aw/examples/project-tracking/) - Complete configuration reference
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Available project operations
- [Trigger Events](/gh-aw/reference/triggers/) - Workflow trigger options
