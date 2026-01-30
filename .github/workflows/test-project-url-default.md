---
name: Test Project URL Default
engine: copilot
on:
  workflow_dispatch:

project:
  url: "https://github.com/orgs/githubnext/projects/1"

safe-outputs:
  update-project:
    max: 5
  create-project-status-update:
    max: 1
---

# Test Default Project URL

This workflow demonstrates the new `GH_AW_PROJECT_URL` environment variable feature.

When the `project` field is configured in the frontmatter, safe output entries like
`update-project` and `create-project-status-update` will automatically use this project
URL as a default when the message doesn't specify a project field.

## Test Cases

1. **Default project URL from frontmatter**: Safe output messages without a `project` field 
   will use the URL from the frontmatter configuration.

2. **Override with explicit project**: If a safe output message includes a `project` field,
   it takes precedence over the frontmatter default.

## Example Safe Outputs

```json
{
  "type": "update_project",
  "content_type": "draft_issue",
  "draft_title": "Test Issue Using Default Project URL",
  "fields": {
    "status": "Todo"
  }
}
```

This will automatically use `https://github.com/orgs/githubnext/projects/1` from the frontmatter.

```json
{
  "type": "create_project_status_update",
  "body": "Project status update using default project URL",
  "status": "ON_TRACK"
}
```

This will also use the default project URL from the frontmatter.
