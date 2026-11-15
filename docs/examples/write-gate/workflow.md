---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
imports:
  - .github/workflows/shared/control-write-gate.md
safe-outputs:
  create-issue:
  create-pull-request:
    allowed-files: ["docs/**", "src/**"]
---

Propose one documentation improvement. Do not change workflow policy.
