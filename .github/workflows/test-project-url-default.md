---
name: Test Project URL Default
engine: copilot
on:
  workflow_dispatch:

safe-outputs:
  update-project:
    max: 5
    project: "https://github.com/orgs/<ORG>/projects/<NUMBER>"
  create-project-status-update:
    max: 1
    project: "https://github.com/orgs/<ORG>/projects/<NUMBER>"
---

# Test Default Project URL

This workflow demonstrates the `project:` field within safe-outputs configuration.

When the `project` field is configured in safe-outputs like `update-project` or 
`create-project-status-update`, the safe output handler will use this URL as the 
default project when processing messages.

## Test Cases

1. **Default project URL from safe-outputs config**: Safe output messages without a 
   `project` field will use the URL from the safe-outputs configuration.

2. **Override with explicit project**: If a safe output message includes a `project` 
   field, it takes precedence over the configured default.

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

This will automatically use `https://github.com/orgs/<ORG>/projects/<NUMBER>` from the 
safe-outputs configuration.

Important: this is a placeholder. Replace it with a real GitHub Projects v2 URL before 
running the workflow.

```json
{
  "type": "create_project_status_update",
  "body": "Project status update using default project URL",
  "status": "ON_TRACK"
}
```

This will also use the default project URL from the frontmatter.
