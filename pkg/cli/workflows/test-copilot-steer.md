---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: write
engine: copilot
safe-outputs:
  steer: true
timeout-minutes: 5
---

# Test Steer Safe Output

Test the steer safe output functionality.

Create a steering issue so the agent can be steered from issue comments.

Output as JSONL format.
