---
strict: false
on:
  pull_request_target:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: read
engine: copilot
imports:
  - ./shared/keep-it-short.md
tools:
  github:
    toolsets: [pull_requests]
---

# Test pull_request_target with checkout enabled and imports

Validate that pull_request_target without `checkout: false` emits a warning
even when shared workflow imports are present.
