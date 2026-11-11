---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-security-advisory:
    max: 1
timeout-minutes: 5
---

# Test Copilot Create Security Advisory

Create one test repository security advisory with a short title, a low severity, and a brief description.

Output as JSONL format using the `create_security_advisory` safe output tool.
