---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
safe-outputs:
  linear-token: ${{ secrets.LINEAR_API_KEY }}
  linear-add-comment:
    target: "ENG-123"
    max: 1
timeout-minutes: 5
---

# Test Linear Add Comment Safe Output

Test the linear-add-comment safe output functionality.

Add a comment to an existing Linear issue with findings from the repository.

Output as JSONL format using the `linear_add_comment` tool.
