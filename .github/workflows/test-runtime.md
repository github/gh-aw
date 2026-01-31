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
