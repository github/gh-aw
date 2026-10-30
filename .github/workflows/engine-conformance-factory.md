---
name: Engine Conformance Factory
description: Run the shared configuration conformance suite with Factory Droid.
on:
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
strict: true
inlined-imports: true
timeout-minutes: 10
max-ai-credits: 5
features:
  gh-aw-detection: false
model: copilot/gpt-5.6-luna
engine:
  id: factory
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-factory
tools:
  work-queue:
    worker: true
    require-assignment: true
imports:
  - shared/factory.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: factory
  - shared/engine-conformance-worker.md
---

Execute the imported engine configuration conformance suite once.
