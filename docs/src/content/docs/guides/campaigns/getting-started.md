---
title: Getting started
description: Quick start guide for creating campaign workflows
---

This guide shows how to create a campaign workflow that coordinates work across repositories.

## Prerequisites

- Repository with GitHub Agentic Workflows installed
- GitHub Actions enabled
- A GitHub Projects board (or create one during setup)

## Create a campaign workflow

1. **Create a new workflow file** at `.github/workflows/my-campaign.md`:

```yaml wrap
---
name: My Campaign
on:
  schedule: daily
  workflow_dispatch:

permissions:
  issues: read
  pull-requests: read

imports:
  - shared/campaign.md
---

# My Campaign

- Project URL: https://github.com/orgs/myorg/projects/1
- Campaign ID: my-campaign

Your campaign instructions here...
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
git add .github/workflows/my-campaign.md
git add .github/workflows/my-campaign.lock.yml
git commit -m "Add my campaign workflow"
git push
```

## How it works

The campaign workflow:

1. Imports standard orchestration rules from `shared/campaign.md`
2. Runs on schedule to discover work items
3. Processes items according to your instructions
4. Updates the GitHub Project board with progress
5. Reports status via project status updates

## Campaign orchestration

The `imports: [shared/campaign.md]` provides:

- **Safe-output defaults**: Pre-configured limits for project operations
- **Execution phases**: Discover → Decide → Write → Report
- **Best practices**: Deterministic execution, pagination budgets, cursor management
- **Project integration**: Standard field mappings and status updates

## Example: Dependabot Burner

See the [Dependabot Burner](https://github.com/githubnext/gh-aw/blob/main/.github/workflows/dependabot-burner.md) workflow for a complete example:

- Discovers open Dependabot PRs
- Creates bundle issues for upgrades
- Tracks everything in a GitHub Project
- Runs daily with smart conditional execution

## Best practices

- **Use imports** - Include `shared/campaign.md` for standard orchestration
- **Define campaign ID** - Include a clear Campaign ID in your workflow
- **Specify project URL** - Document the GitHub Projects board URL
- **Test manually** - Use `workflow_dispatch` trigger to test before scheduling
- **Monitor progress** - Check your project board to see tracked items

## Next Steps

- [Campaign Orchestration](/gh-aw/guides/campaigns/) - Overview and patterns
- [Project Tracking Example](/gh-aw/examples/project-tracking/) - Complete configuration reference
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Available project operations
- [Trigger Events](/gh-aw/reference/triggers/) - Workflow trigger options
