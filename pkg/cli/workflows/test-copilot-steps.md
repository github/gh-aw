---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  steps:
    - name: Prepare safe output test
      run: echo prepared
  noop:
---

# Test Copilot Safe Output Steps

Complete the task and emit one JSONL result using the `noop` safe-output tool.
