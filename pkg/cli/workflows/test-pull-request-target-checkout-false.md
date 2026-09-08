---
strict: false
on:
  pull_request_target:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: read
engine: copilot
checkout: false
imports:
  - ./shared/keep-it-short.md
tools:
  github:
    toolsets: [pull_requests]
---

# Test pull_request_target with checkout disabled and imports

Validate that pull_request_target with `checkout: false` compiles successfully
even when shared workflow imports are present.

In strict mode this should emit a dangerous-trigger warning but succeed.
