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
  linear-add-comment:
timeout-minutes: 5
---

# Test Linear Add Comment Safe Output

Test the linear-add-comment safe output functionality.

Add a comment to an existing Linear issue with findings from the repository.

Use `connector_6_linear_add_comment` with the approved issue UUID and body.
Replace the specimen UUID before live use.
