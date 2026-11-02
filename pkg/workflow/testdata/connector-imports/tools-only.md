---
imports:
  - uses: connector.md
    with:
      issue: ISSUE-1
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
tools:
  tracker: [get-issue]
---

Read ISSUE-1 using the approved tracker tool. No connector context or writes are enabled.
