---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  environment: safe-output-test
  noop:
---

# Test Copilot Safe Output Environment

Complete the task and emit one JSONL result using the `noop` safe-output tool.
