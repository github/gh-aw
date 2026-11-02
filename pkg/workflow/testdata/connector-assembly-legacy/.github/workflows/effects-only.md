---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
network:
  allowed: [defaults, api.example.com]
connectors:
  issues:
    definition: .github/aw/connectors/issues.yaml
    connection: .github/aw/connections/issues.yaml
    safe-outputs: [add-comment]
---

# Request one bounded comment

Request connector_6_issues_add_comment with issue
ISSUE-1 and text "Assembly example completed." Do not call the service directly.
No connector read tool or context recipe is selected.
