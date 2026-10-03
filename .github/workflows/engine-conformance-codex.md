---
name: Engine Conformance Codex
description: Dispatch-only worker for the Codex configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-codex
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
  id: codex
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-codex
imports:
  - uses: shared/engine-conformance.md
    with:
      engine-id: codex
---

Execute the imported engine configuration conformance suite once.
