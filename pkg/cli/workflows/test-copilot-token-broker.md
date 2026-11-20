---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
engine: copilot
token-broker:
  copilot-task: true
safe-outputs:
  token-broker: true
  assign-to-agent:
    max: 1
timeout-minutes: 5
---

# Test Copilot Token Broker

Test the `safe-outputs.token-broker` configuration with a Copilot task.

Assign issue #1 to the Copilot coding agent using the brokered task credential.
Output results in JSONL format using the `assign_to_agent` tool.
