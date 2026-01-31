---
on:
  issues:
    types: [labeled]
  workflow_dispatch:
engine: copilot
permissions:
  contents: read
  issues: read
safe-outputs:
  dispatch-workflow:
    workflows: [add-name, add-emojis]
    max: 1
  add-comment:
    max: 1
---

# Test Runtime Workflow

Only act if the label that was just added matches one of:

- `ai:test-runtime-workflow` - run ALL workflows

## Instructions

This workflow demonstrates the `dispatch-workflow` safe output capability. You can trigger other workflows by outputting a `dispatch_workflow` request.

### Example: Dispatch a workflow

To dispatch the `worker-workflow` with input parameters, output a JSON entry like this:

```json
{
  "type": "dispatch_workflow",
  "workflow_name": "worker-workflow",
  "inputs": {
    "campaign_id": "bootstrap-123",
    "payload": "{\"target\": \"repositories\"}"
  }
}
```

The available workflows you can dispatch are: `add-name`, `add-emojis`.
