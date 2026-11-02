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
      teams: [33333333-3333-4333-8333-333333333333]
      projects: [44444444-4444-4444-8444-444444444444]
safe-outputs:
  linear-create-issue:
timeout-minutes: 5
---

# Test Linear Create Issue Safe Output

Test the linear-create-issue safe output functionality.

Create a Linear issue summarizing a task discovered in the repository.

Use `connector_6_linear_create_issue` with the approved team/project UUIDs,
title and body. Replace the specimen UUIDs before live use.
