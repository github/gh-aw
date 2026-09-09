---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
safe-outputs:
  linear-token: ${{ secrets.LINEAR_API_KEY }}
  linear-create-issue:
    team-id: "9cfb482a-81e3-4154-b5b9-2c805e70a02d"
    max: 1
timeout-minutes: 5
---

# Test Linear Create Issue Safe Output

Test the linear-create-issue safe output functionality.

Create a Linear issue summarizing a task discovered in the repository.

Output as JSONL format using the `linear_create_issue` tool.
