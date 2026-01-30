---
title: Project Tracking
description: Automatically track issues and pull requests in GitHub Projects boards
sidebar:
  badge: { text: 'Project', variant: 'tip' }
---

The `project` frontmatter field enables automatic tracking of workflow-created items in GitHub Projects boards. When configured, workflows automatically get project management capabilities including item addition, field updates, and status reporting.

## Quick Start

Add the `project` field to your workflow frontmatter to enable project tracking:

```yaml
---
on:
  issues:
    types: [opened]
project: https://github.com/orgs/github/projects/123
safe-outputs:
  create-issue:
    max: 3
---
```

This automatically enables:
- **update-project** - Add items to projects, update fields (status, priority, etc.)
- **create-project-status-update** - Post status updates to project boards

## Configuration Options

### Simple Format (String)

Use a GitHub Project URL directly:

```yaml
project: https://github.com/orgs/github/projects/123
```

### Full Configuration (Object)

Customize behavior with additional options:

```yaml
project:
  url: https://github.com/orgs/github/projects/123
  scope:
    - owner/repo1
    - owner/repo2
    - org:myorg
  max-updates: 50
  max-status-updates: 2
  github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
  do-not-downgrade-done-items: true
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | (required) | GitHub Project URL (user or organization) |
| `scope` | array | current repo | Repositories/organizations this workflow can operate on (e.g., `owner/repo`, `org:name`) |
| `max-updates` | integer | 100 | Maximum project updates per workflow run |
| `max-status-updates` | integer | 1 | Maximum status updates per workflow run |
| `github-token` | string | `GITHUB_TOKEN` | Custom token with Projects permissions |
| `do-not-downgrade-done-items` | boolean | false | Prevent moving completed items backward |

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
project:
  url: https://github.com/orgs/myorg/projects/1
  max-updates: 10
  github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
safe-outputs:
  add-comment:
    max: 1
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
project:
  url: https://github.com/orgs/myorg/projects/2
  max-updates: 5
  do-not-downgrade-done-items: true
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

## Automatic Safe Outputs

When you configure the `project` field, the compiler automatically adds these safe-output operations if not already configured:

### update-project

Manages project items (add, update fields, views):

```yaml
# Automatically configured with project field
update-project:
  max: 100  # Default from project.max-updates
  github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

Operations:
- `create` - Create a new project
- `add` - Add items to project
- `update` - Update project fields (status, priority, custom fields)
- `create_fields` - Create custom fields
- `create_views` - Create project views

### create-project-status-update

Posts status updates to project boards:

```yaml
# Automatically configured with project field
create-project-status-update:
  max: 1  # Default from project.max-status-updates
  github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

Use for campaign progress reports, milestone summaries, or workflow health indicators.

## Overriding Auto-Configuration

If you need custom configuration, define safe-outputs explicitly. Your configuration takes precedence:

```yaml
project:
  url: https://github.com/orgs/github/projects/123
safe-outputs:
  update-project:
    max: 25  # Custom max overrides project.max-updates
    views:
      - name: "Triage View"
        layout: board
        filter: "status:Needs Triage"
  create-project-status-update:
    max: 3  # Custom max overrides project.max-status-updates
```

## Relationship with Campaigns

The `project` field brings project tracking capabilities from [campaign coordinators](/gh-aw/guides/campaigns/) to regular agentic workflows:

**Campaign coordinators** (campaign.md files):
- Use `project-url` in campaign spec
- Automatically coordinate multiple workflows
- Track campaign-wide progress

**Agentic workflows** (regular .md files):
- Use `project` in frontmatter
- Focus on single workflow operations
- Track workflow-specific items

Both use the same underlying safe-output operations (`update-project`, `create-project-status-update`).

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
- [Campaigns](/gh-aw/guides/campaigns/) - Campaign coordination documentation
