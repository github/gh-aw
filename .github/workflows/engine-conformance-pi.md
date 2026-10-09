---
name: Engine Conformance Pi
description: Run the shared configuration conformance suite with Pi.
on:
  workflow_dispatch:
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
tools:
  work-queue:
    worker: true
    require-assignment: true
imports:
  - uses: shared/engine-conformance.md
    with:
      engine-id: pi
  - shared/engine-conformance-worker.md
---

Execute the imported engine configuration conformance suite once.
