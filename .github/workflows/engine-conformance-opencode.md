---
name: Engine Conformance OpenCode
description: Dispatch-only worker for the OpenCode configuration conformance suite.
on:
  workflow_dispatch:
concurrency:
  group: engine-conformance-opencode
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
  id: opencode
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-opencode
imports:
  - shared/opencode.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: opencode
---

Execute the imported engine configuration conformance suite once.
