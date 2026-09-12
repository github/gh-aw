---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
engine: copilot
safe-outputs:
  assign-to-agent:
    max: 1
    model: o3
    reasoning-effort: high
---

# Test Copilot Assign To Agent

Test the `assign_to_agent` safe output type with the Copilot engine.

## Task

Assign issue #1 to the copilot agent in this repository.

Output results in JSONL format using the `assign_to_agent` tool.
