---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  request-continuation:
---

# Test Copilot Request Continuation

Record that useful work remains beyond the current run budget with a concise continuation message.

Output results in JSONL format using the `request_continuation` safe-output tool.
