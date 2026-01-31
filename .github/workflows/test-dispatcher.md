---
on: issues
engine: copilot
permissions:
  contents: read
  issues: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - test-workflow
    max: 1
---

# Test Dispatcher Workflow

This workflow demonstrates the dispatch-workflow safe output capability.
The agent can trigger the test-workflow using the test_workflow tool.
