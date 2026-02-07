---
on: issues
permissions:
  contents: read
  issues: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - test-workflow
    max: 1
imports:
  - shared/mood.md
---

# Test Dispatcher Workflow

This workflow demonstrates the dispatch-workflow safe output capability.

**Your task**: Call the MCP tool to trigger the `test-workflow` workflow.

**Important**: The MCP tool is automatically generated based on the target workflow's `workflow_dispatch` inputs.

## How It Works

When you configure `dispatch-workflow: [test-workflow]`, the compiler automatically:
1. Reads the `test-workflow.yml` file
2. Extracts the `workflow_dispatch.inputs` schema
3. Generates an MCP tool named `test_workflow` that you can call

The target workflow (`test-workflow.yml`) defines:
```yaml
on:
  workflow_dispatch:
    inputs:
      test_param:
        description: 'Question to the worker workflow'
        type: string
        required: true
```

So you'll have a `test_workflow` tool available with a required `test_param` input.

## Instructions

1. **Call the MCP tool**: Use the `test_workflow` tool (automatically generated from the workflow name)
2. **Provide inputs (required)**: The `test_param` input is required
3. **The tool handles everything**: The MCP tool will automatically dispatch the workflow with the correct inputs

## Example Tool Call

The agent should call the `test_workflow` MCP tool directly and send it a question to answer via the `test_param` input:

```javascript
// The MCP tool is named after the workflow (underscores replace hyphens)
test_workflow({
  test_param: "question to the worker workflow"
})
```

Or in the agent's output format:

```json
{
  "type": "dispatch_workflow",
  "workflow_name": "test-workflow",
  "inputs": {
    "test_param": "question to the worker workflow"
  }
}
```
