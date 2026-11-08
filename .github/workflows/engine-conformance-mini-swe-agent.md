---
name: Engine Conformance mini-SWE-agent
description: Run the shared configuration conformance suite with mini-SWE-agent.
on:
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
strict: true
inlined-imports: true
timeout-minutes: 10
max-turns: 30
max-ai-credits: 50
features:
  gh-aw-detection: false
model: copilot/claude-haiku-5.5
engine:
  id: mini-swe-agent
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-mini-swe-agent
tools:
  work-queue:
    worker: true
    require-assignment: true
imports:
  - shared/mini-swe-agent.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: mini-swe-agent
  - shared/engine-conformance-worker.md
---

Execute the imported engine configuration conformance suite once.
