---
name: Engine Conformance Gemini
description: Run the shared configuration conformance suite with Gemini CLI.
on:
  workflow_dispatch:
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
