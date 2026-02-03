---
title: ProjectOps
description: Automate GitHub Projects board management with AI-powered workflows (triage, routing, and field updates)
sidebar:
  badge: { text: 'Event-triggered', variant: 'success' }
---

ProjectOps automates [GitHub Projects](https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects) management using AI-powered workflows.

When a new issue or pull request arrives, the agent analyzes it and determines where it belongs, what status to set, which fields to update (priority, effort, etc.), and whether to create or update project structures.

Safe outputs handle all project operations in separate, scoped jobs with minimal permissions—the agent job never sees the Projects token, ensuring secure automation.

## Prerequisites

1. **Create a Project**: Before you wire up a workflow, you must first create the Project in the GitHub UI (user or organization level). Keep the Project URL handy (you'll need to reference it in your workflow instructions).

2. **Create a token**: The kind of token you need depends on whether the Project you created is **user-owned** or **organization-owned**.

#### User-owned Projects (v2)

Use a **classic PAT** with scopes:
- `project` (required for user Projects)
- `repo` (required if accessing private repositories)

#### Organization-owned Projects (v2)

Use a **fine-grained** PAT with scopes:
- Repository access: Select specific repos that will use the workflow
- Repository permissions:
  - Contents: Read
  - Issues: Read (if workflow is triggered by issues)
  - Pull requests: Read (if workflow is triggered by pull requests)
- Organization permissions:
  - Projects: Read & Write (required for updating projects)

### 3) Store the token as a secret

After creating your token, add it to your repository:

```bash
gh aw secrets set GH_AW_PROJECT_GITHUB_TOKEN --value "YOUR_PROJECT_TOKEN"
```

See the [GitHub Projects v2 token reference](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2) for complete details.

## Example: Smart Issue Triage

This example demonstrates intelligent issue routing to project boards with AI-powered content analysis:

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
tools:
  github:
    toolsets: [default, projects]
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
safe-outputs:
  update-project:
    max: 1
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
  add-comment:
    max: 1
---

# Smart Issue Triage with Project Tracking

When a new issue is created, analyze it and add to the appropriate project board.

Examine the issue title and description to determine its type:
- Bug reports → Add to "Bug Triage" project, status: "Needs Triage", priority: based on severity
- Feature requests → Add to "Feature Roadmap" project, status: "Proposed"
- Documentation issues → Add to "Docs Improvements" project, status: "Todo"
- Performance issues → Add to "Performance Optimization" project, priority: "High"

After adding to project board, comment on the issue confirming where it was added.
```

This workflow creates an intelligent triage system that automatically organizes new issues onto appropriate project boards with relevant status and priority fields.

## Available Safe Outputs

ProjectOps workflows leverage these safe outputs for project management operations:

### Core Operations

- **[`create-project`](/gh-aw/reference/safe-outputs/#project-creation-create-project)** - Create new GitHub Projects V2 boards with custom configuration
- **[`update-project`](/gh-aw/reference/safe-outputs/#project-board-updates-update-project)** - Add issues/PRs to projects, update fields (status, priority, custom fields), and manage project views
- **[`create-project-status-update`](/gh-aw/reference/safe-outputs/#project-status-updates-create-project-status-update)** - Post status updates to project boards with progress summaries and health indicators

Each safe output operates in a separate job with minimal, scoped permissions. See the [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for complete configuration options and examples.

## Key Capabilities

**Project Creation and Management**
- Create new Projects V2 boards programmatically
- Add issues and pull requests to projects with duplicate prevention
- Update project status with automated progress summaries

**Field Management**
- Set status, priority, effort, and sprint fields
- Update custom date fields (start date, end date) for timeline tracking
- Support for TEXT, DATE, NUMBER, ITERATION, and SINGLE_SELECT field types
- Automatic field option creation for single-select fields

**View Configuration**
- Automatically create project views (table, board, roadmap)
- Configure view filters and visible fields
- Support for swimlane grouping by custom fields

**Orchestration & Monitoring**
- Use Projects as a shared dashboard across runs/workflows
- Post status updates with health indicators
- Coordinate work across repositories and workflows (optionally with an orchestrator/worker setup)

See the [Safe Outputs reference](/gh-aw/reference/safe-outputs/#project-board-updates-update-project) for project field and view configuration.

## When to Use ProjectOps

ProjectOps complements [GitHub's built-in Projects automation](https://docs.github.com/en/issues/planning-and-tracking-with-projects/automating-your-project/using-the-built-in-automations) with AI-powered intelligence:

- **Content-based routing** - Analyze issue content to determine which project board and what priority (native automation only supports label/status triggers)
- **Multi-issue coordination** - Add related issues/PRs to projects and apply consistent tracking labels
- **Dynamic field assignment** - Set priority, effort, and custom fields based on AI analysis
- **Automated project creation** - Create new project boards programmatically based on initiative needs
- **Status tracking** - Generate automated progress summaries with health indicators

## Best Practices

**Create projects programmatically** when launching an initiative to ensure consistent structure and field configuration. Use `create-project` with an optional first issue to initialize tracking.

**Use descriptive project names** that clearly indicate purpose and scope. Prefer "Performance Optimization Q1 2026" over "Project 1".

**Prefer Projects fields over ad-hoc labels** for grouping related work across issues and PRs.

**Set meaningful field values** like status, priority, and effort to enable effective filtering and sorting on boards.

**Create custom views automatically** using the `views` configuration in frontmatter for consistent board setup.

**Post regular status updates** using `create-project-status-update` to keep stakeholders informed of progress and health.

**Combine with issue creation** for initiative workflows that generate multiple tracked tasks automatically.

**Update status progressively** as work moves through stages (Todo → In Progress → In Review → Done).

**Archive completed initiatives** rather than deleting them to preserve historical context and learnings.

## Common Challenges

**Permission Errors**: Project operations require `projects: write` permission via a PAT. Default `GITHUB_TOKEN` lacks Projects v2 access.

**Field Name Mismatches**: Custom field names are case-sensitive. Use exact field names as defined in project settings. Field names are automatically normalized (e.g., `story_points` matches `Story Points`).

**Token Scope**: Default `GITHUB_TOKEN` cannot access Projects. Store a PAT with Projects permissions in `GH_AW_PROJECT_GITHUB_TOKEN` secret.

**Project URL Format**: Use full project URLs (e.g., `https://github.com/orgs/myorg/projects/42`), not project numbers alone.

**Field Type Detection**: Ensure field types match expected formats (dates as `YYYY-MM-DD`, numbers as integers, single-select as exact option values).

## Additional Resources

- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Complete safe output configuration and API details
- [Projects & Monitoring](/gh-aw/guides/monitoring/) - Design pattern guide
- [Orchestration](/gh-aw/guides/orchestration/) - Design pattern guide
- [Trigger Events](/gh-aw/reference/triggers/) - Event trigger configuration options
- [IssueOps Guide](/gh-aw/examples/issue-pr-events/issueops/) - Related issue automation patterns
- [Token Reference](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2) - GitHub Projects token setup
