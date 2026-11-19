---
on:
  workflow_dispatch:
permissions:
  id-token: write
engine: copilot
safe-outputs:
  authorization:
    endpoint: https://authority.example/a
    audience: https://authority.example/gh-aw
  noop:
    max: 2
---

# Test Safe Output Authorization

Exercise the global safe-output authorization configuration. Call the `noop` safe-output tool with one JSONL-compatible message and return the tool result.
