---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  require-agent-success: true
  noop:
---

# Test Copilot Require Agent Success

Complete the task successfully, then emit one JSONL result using the `noop` safe-output tool.
