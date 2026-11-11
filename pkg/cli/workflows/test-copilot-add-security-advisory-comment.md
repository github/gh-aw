---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  add-security-advisory-comment:
    max: 1
timeout-minutes: 5
---

# Test Copilot Add Security Advisory Comment

Add one test comment to an existing repository security advisory with a short status update.

Output as JSONL format using the `add_security_advisory_comment` safe output tool.
