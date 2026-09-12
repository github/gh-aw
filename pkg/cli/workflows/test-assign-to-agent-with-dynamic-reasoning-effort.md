---
name: Test Assign to Agent with Dynamic Reasoning Effort
on:
  workflow_dispatch:
    inputs:
      reasoning_effort:
        description: Reasoning effort to use
        required: true
        type: string
permissions:
  contents: read
  issues: read
engine: copilot
safe-outputs:
  assign-to-agent:
    name: copilot
    model: o3
    reasoning-effort: ${{ inputs.reasoning_effort }}
strict: false
---

# Test Dynamic Assign to Agent Reasoning Effort

Assign issue #1 to Copilot using the configured reasoning effort.
