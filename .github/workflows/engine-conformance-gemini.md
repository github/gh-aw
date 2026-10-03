---
name: Engine Conformance Gemini
description: Dispatch-only worker for the Gemini CLI configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-gemini
  cancel-in-progress: false
permissions:
  contents: read
strict: true
inlined-imports: true
timeout-minutes: 10
max-turns: 30
max-ai-credits: 5
features:
  gh-aw-detection: false
engine:
  id: gemini
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-gemini
imports:
  - uses: shared/engine-conformance.md
    with:
      engine-id: gemini
---

Execute the imported engine configuration conformance suite once.
