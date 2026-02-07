---
name: Test Project URL Explicit Requirement
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
imports:
  - shared/mood.md
---

# Test Explicit Project URL Requirement

This workflow tests that the `project` field is required in agent output messages.

The `project` field in safe-outputs configuration is for reference purposes. Agent 
output messages must explicitly include the `project` field with the target project URL.

## Test Cases

1. **Explicit project URL in message**: Safe output messages must include the `project` 
   field with the target project URL.

2. **Project field is always required**: The agent must always provide the `project` field
   in safe output messages. The configured value in frontmatter is for reference only.

## Example Safe Outputs

All safe output messages must explicitly include the `project` field:

```json
{
  "type": "update_project",
  "project": "https://github.com/orgs/<ORG>/projects/<NUMBER>",
  "content_type": "draft_issue",
  "draft_title": "Test Issue with Explicit Project URL",
  "fields": {
    "status": "Todo"
  }
}
```

The `project` field must be included in every message.

Important: Replace `<ORG>` and `<NUMBER>` with a real GitHub Projects v2 URL before 
running the workflow.

```json
{
  "type": "create_project_status_update",
  "project": "https://github.com/orgs/<ORG>/projects/<NUMBER>",
  "body": "Project status update with explicit project URL",
  "status": "ON_TRACK"
}
```

The agent must always provide the project URL explicitly in the output message.
