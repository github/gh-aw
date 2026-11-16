---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  runs-on: ubuntu-latest
  noop:
---

# Test Copilot Safe Outputs Runner

Complete the task and emit one JSONL result using the `noop` safe-output tool.
