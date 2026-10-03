---
name: Engine Conformance Goose
description: Dispatch-only worker for the Goose configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-goose
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
  id: goose
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-goose
imports:
  - shared/goose.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: goose
---

Execute the imported engine configuration conformance suite once.
