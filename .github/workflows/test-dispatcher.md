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

**Your task**: Call the `dispatch_workflow` tool to trigger the `test-workflow` workflow.

**Important**: You MUST use the safe output tool - do NOT write to files or attempt other methods.

## Instructions

1. **Call the safe output tool**: Use `dispatch_workflow` to trigger the test-workflow
2. **Workflow name**: Specify `test-workflow` as the workflow to dispatch
3. **Inputs (optional)**: You can provide test parameters if needed, but they are optional

## Example

The agent should call the `dispatch_workflow` tool like this:

```json
{
  "type": "dispatch_workflow",
  "workflow_name": "test-workflow",
  "inputs": {
    "test_param": "example value"
  }
}
```
