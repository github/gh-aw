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
context:
  tracker:
    issue-details: {issue: ISSUE-1}
safe-outputs:
  tracker-add-comment:
---

Investigate ISSUE-1 using the approved tracker tool and untrusted context, then request a tracker comment.
