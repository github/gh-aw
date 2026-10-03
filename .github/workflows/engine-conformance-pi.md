---
name: Engine Conformance Pi
description: Dispatch-only worker for the Pi configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-pi
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
model: copilot/gpt-5.4
engine:
  id: pi
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-pi
imports:
  - uses: shared/engine-conformance.md
    with:
      engine-id: pi
---

Execute the imported engine configuration conformance suite once.
