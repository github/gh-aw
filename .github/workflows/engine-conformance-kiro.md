---
name: Engine Conformance Kiro
description: Run the shared configuration conformance suite with Kiro CLI.
on:
  workflow_dispatch:
permissions:
  contents: read
strict: true
inlined-imports: true
timeout-minutes: 10
max-ai-credits: 5
features:
  gh-aw-detection: false
model: kiro/auto
engine:
  id: kiro
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-kiro
tools:
  work-queue:
    worker: true
    require-assignment: true
imports:
  - shared/kiro.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: kiro
  - shared/engine-conformance-worker.md
---

Execute the imported engine configuration conformance suite once.
