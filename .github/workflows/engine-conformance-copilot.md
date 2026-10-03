---
name: Engine Conformance Copilot
description: Dispatch-only worker for the Copilot CLI configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-copilot
  cancel-in-progress: false
permissions:
  contents: read
  copilot-requests: write
strict: true
inlined-imports: true
timeout-minutes: 10
max-turns: 30
max-ai-credits: 5
features:
  gh-aw-detection: false
engine:
  id: copilot
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-copilot
imports:
  - uses: shared/engine-conformance.md
    with:
      engine-id: copilot
---

Execute the imported engine configuration conformance suite once.
