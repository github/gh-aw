---
name: Engine Conformance Claude
description: Dispatch-only worker for the Claude Code configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-claude
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
  id: claude
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-claude
imports:
  - uses: shared/engine-conformance.md
    with:
      engine-id: claude
---

Execute the imported engine configuration conformance suite once.
