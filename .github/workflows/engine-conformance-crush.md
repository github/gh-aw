---
name: Engine Conformance Crush
description: Run the shared configuration conformance suite with Crush.
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
