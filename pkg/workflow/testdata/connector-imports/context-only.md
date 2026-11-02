---
imports:
  - uses: connector.md
    with:
      connection: pkg/workflow/testdata/connector-imports/connection.yaml
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
context:
  tracker:
    issue-details: {issue: ISSUE-1}
---

Summarize the untrusted ISSUE-1 context. No connector tools or writes are enabled.
