---
imports:
  - uses: connector.md
    with:
      connection: pkg/workflow/testdata/connector-imports/connection.yaml
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
tools:
  tracker: [get-issue]
---

Read ISSUE-1 using the approved tracker tool. No connector context or writes are enabled.
