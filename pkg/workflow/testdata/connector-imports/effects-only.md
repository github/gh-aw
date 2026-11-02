---
imports:
  - uses: connector.md
    with:
      connection: pkg/workflow/testdata/connector-imports/connection.yaml
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  tracker-add-comment:
---

Request a tracker comment on ISSUE-1. No connector tools or context are enabled.
