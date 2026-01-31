---
on: 
  issues:
    types: [labeled]
engine: copilot
permissions:
  contents: read
  issues: read
safe-outputs:
  dispatch-workflow:
    workflows: [add-name, add-emojis]
    max: 2
  add-comment:
    max: 1
---

# Test Runtime Workflow

Test workflow for dispatch-workflow runtime.
