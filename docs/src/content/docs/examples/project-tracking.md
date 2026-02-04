---
title: Project Tracking
description: Automatically track issues and pull requests in GitHub Projects boards using safe-outputs configuration
sidebar:
  badge: { text: 'Project', variant: 'tip' }
---

The `update-project` and `create-project-status-update` safe-output tools enable automatic tracking of workflow-created items in GitHub Projects boards. Configure these tools in the `safe-outputs` section of your workflow frontmatter to enable project management capabilities including item addition, field updates, and status reporting.

## Quick Start

Add project configuration to your workflow's `safe-outputs` section:

```yaml
---
on:
  issues:
    types: [opened]
safe-outputs:
  create-issue:
    max: 3
  update-project:
    project: https://github.com/orgs/github/projects/123
    max: 10
  create-project-status-update:
    project: https://github.com/orgs/github/projects/123
    max: 1
---
```

This enables:
- **update-project** - Add items to projects, update fields (status, priority, etc.)
- **create-project-status-update** - Post status updates to project boards

## Configuration

### Update Project Configuration

Configure `update-project` in the `safe-outputs` section:

```yaml
safe-outputs:
  update-project:
    project: https://github.com/orgs/github/projects/123  # Default project URL
    max: 20                                                # Max operations per run (default: 10)
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
    views:                                                 # Optional: auto-create views
      - name: "Sprint Board"
        layout: board
        filter: "is:issue is:open"
      - name: "Task Tracker"
        layout: table
```

### Project Status Update Configuration

Configure `create-project-status-update` in the `safe-outputs` section:

```yaml
safe-outputs:
  create-project-status-update:
    project: https://github.com/orgs/github/projects/123  # Default project URL
    max: 1                                                 # Max status updates per run (default: 1)
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `project` | string | (required) | GitHub Project URL for update-project or create-project-status-update |
| `max` | integer | 10 | Maximum operations per run (update-project) or 1 (create-project-status-update) |
| `github-token` | string | `GITHUB_TOKEN` | Custom token with Projects permissions |
| `views` | array | - | Optional auto-created views for update-project (with name, layout, filter) |

See [Safe Outputs: Project Board Updates](/gh-aw/reference/safe-outputs/#project-board-updates-update-project) for complete configuration details.

## Prerequisites

### 1. Create a GitHub Project

Create a Projects V2 board in the GitHub UI before configuring your workflow. You'll need the Project URL from the browser address bar.

### 2. Set Up Authentication

#### For User-Owned Projects

Use a **classic PAT** with scopes:
- `project` (required)
- `repo` (if accessing private repositories)

#### For Organization-Owned Projects

Use a **fine-grained PAT** with:
- Repository access: Select specific repos
- Repository permissions:
  - Contents: Read
  - Issues: Read (if workflow triggers on issues)
  - Pull requests: Read (if workflow triggers on pull requests)
- Organization permissions:
  - Projects: Read & Write

### 3. Store the Token

```bash
gh aw secrets set GH_AW_PROJECT_GITHUB_TOKEN --value "YOUR_PROJECT_TOKEN"
```

See the [GitHub Projects V2 token reference](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2) for complete details.

## Example: Issue Triage

Automatically add new issues to a project board with intelligent categorization:

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
  issues: read
tools:
  github:
    toolsets: [default, projects]
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
safe-outputs:
  add-comment:
    max: 1
  update-project:
    project: https://github.com/orgs/myorg/projects/1
    max: 10
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
---

# Smart Issue Triage

When a new issue is created, analyze it and add to the project board.

## Task

Examine the issue title and description to determine its type:
- **Bug reports** → Add to project, set status="Needs Triage", priority="High"
- **Feature requests** → Add to project, set status="Backlog", priority="Medium"
- **Documentation** → Add to project, set status="Todo", priority="Low"

After adding to the project board, comment on the issue confirming where it was added.
```

## Example: Pull Request Tracking

Track pull requests through the development workflow:

```aw wrap
---
on:
  pull_request:
    types: [opened, review_requested]
permissions:
  contents: read
  actions: read
  pull-requests: read
tools:
  github:
    toolsets: [default, projects]
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
safe-outputs:
  update-project:
    project: https://github.com/orgs/myorg/projects/2
    max: 5
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
---

# PR Project Tracker

Track pull requests in the development project board.

## Task

When a pull request is opened or reviews are requested:
1. Add the PR to the project board
2. Set status based on PR state:
   - Just opened → "In Progress"
   - Reviews requested → "In Review"
3. Set priority based on PR labels:
   - Has "urgent" label → "High"
   - Has "enhancement" label → "Medium"
   - Default → "Low"
```

## Safe Output Operations

Configure project operations in the `safe-outputs` section:

### update-project

Manages project items (add, update fields, views):

```yaml
safe-outputs:
  update-project:
    project: https://github.com/orgs/myorg/projects/1
    max: 100  # Maximum operations per run
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

Operations:
- `add` - Add items to project
- `update` - Update project fields (status, priority, custom fields)
- `create_fields` - Create custom fields
- `create_views` - Create project views

### create-project-status-update

Posts status updates to project boards:

```yaml
safe-outputs:
  create-project-status-update:
    project: https://github.com/orgs/myorg/projects/1
    max: 1  # Maximum status updates per run
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

Use for progress reports, milestone summaries, or workflow health indicators.

## How this fits

- **Projects & Monitoring:** Use `update-project` to track work items and `create-project-status-update` to publish run summaries.
- **Orchestration:** An orchestrator can dispatch workers and use the same project safe-outputs to keep a shared board updated.

See:
- [Projects & Monitoring](/gh-aw/guides/monitoring/)
- [Orchestration](/gh-aw/guides/orchestration/)

## Common Patterns

### Progressive Status Updates

Move items through workflow stages:

```aw
Analyze the issue and determine its current state:
- If new and unreviewed → status="Needs Triage"
- If reviewed and accepted → status="Todo"
- If work started → status="In Progress"
- If PR merged → status="Done"

Update the project item with the appropriate status.
```

### Priority Assignment

Set priority based on content analysis:

```aw
Examine the issue for urgency indicators:
- Contains "critical", "urgent", "blocker" → priority="High"
- Contains "important", "soon" → priority="Medium"
- Default → priority="Low"

Update the project item with the assigned priority.
```

### Field-Based Routing

Use custom fields for workflow routing:

```aw
Determine the team that should handle this issue:
- Security-related → team="Security"
- UI/UX changes → team="Design"
- API changes → team="Backend"
- Default → team="General"

Update the project item with the team field.
```

## Best Practices

1. **Use specific project URLs** - Reference the exact project board to avoid ambiguity
2. **Set reasonable limits** - Use `max-updates` to prevent runaway operations
3. **Secure tokens properly** - Store project tokens as repository/organization secrets
4. **Enable do-not-downgrade** - Prevent accidental status regression on completed items
5. **Test with dry runs** - Use `staged: true` in safe-outputs to preview changes
6. **Document field mappings** - Comment your workflow to explain project field choices

## Troubleshooting

### Items Not Added to Project

**Symptoms**: Workflow runs successfully but items don't appear in project board

**Solutions**:
- Verify project URL is correct (check browser address bar)
- Confirm token has Projects: Read & Write permissions
- Check that organization allows Projects access for the token
- Review workflow logs for safe_outputs job errors

### Permission Errors

**Symptoms**: Workflow fails with "Resource not accessible" or "Insufficient permissions"

**Solutions**:
- For organization projects: Use fine-grained PAT with organization Projects permission
- For user projects: Use classic PAT with `project` scope
- Ensure token is stored in correct secret name
- Verify repository settings allow Actions to access secrets

### Token Not Resolved

**Symptoms**: Workflow fails with "invalid token" or token appears as literal string

**Solutions**:
- Use GitHub expression syntax: `${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}`
- Don't quote the expression in YAML
- Ensure secret name matches exactly (case-sensitive)
- Check secret is set at repository or organization level

## See Also

- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Complete safe-outputs documentation
- [update-project](/gh-aw/reference/safe-outputs/#project-board-updates-update-project) - Detailed update-project configuration
- [create-project-status-update](/gh-aw/reference/safe-outputs/#project-status-updates-create-project-status-update) - Status update configuration
- [GitHub Projects V2 Tokens](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2) - Token setup guide
- [Projects & Monitoring](/gh-aw/guides/monitoring/) - Design pattern guide
- [Orchestration](/gh-aw/guides/orchestration/) - Design pattern guide
