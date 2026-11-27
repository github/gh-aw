---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  set-issue-status:
    max: 5
---

# Test Copilot Set Issue Status

Set the planning status of issue #1 to `In Progress`.

Output the result in JSONL format using the `set_issue_status` tool.
