---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
imports:
  - uses: "@builtin:connectors/linear.md"
    with:
      issues: [22222222-2222-4222-8222-222222222222]
safe-outputs:
  linear-update-issue:
timeout-minutes: 5
---

# Test Linear Update Issue Safe Output

Test the linear-update-issue safe output functionality.

Update the title and body of an existing Linear issue.

Use `connector_6_linear_update_issue` with the approved issue UUID, title and
body. Replace the specimen UUID before live use.
