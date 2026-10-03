---
name: Engine Conformance Crush
description: Dispatch-only worker for the Crush configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-crush
  cancel-in-progress: false
permissions:
  contents: read
  copilot-requests: write
strict: true
inlined-imports: true
timeout-minutes: 10
max-ai-credits: 5
features:
  gh-aw-detection: false
model: copilot/gpt-5.4
engine:
  id: crush
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-crush
imports:
  - shared/crush.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: crush
---

Execute the imported engine configuration conformance suite once.
