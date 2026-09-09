---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
safe-outputs:
  linear-token: ${{ secrets.LINEAR_API_KEY }}
  linear-update-issue:
    target: "ENG-123"
    title: true
    body: true
    max: 1
timeout-minutes: 5
---

# Test Linear Update Issue Safe Output

Test the linear-update-issue safe output functionality.

Update the title and body of an existing Linear issue.

Output as JSONL format using the `linear_update_issue` tool.
