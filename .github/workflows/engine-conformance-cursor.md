---
name: Engine Conformance Cursor
description: Run the shared configuration conformance suite with Cursor Agent.
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
model: cursor/auto
engine:
  id: cursor
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-cursor
imports:
  - shared/cursor.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: cursor
---

Execute the imported engine configuration conformance suite once.
