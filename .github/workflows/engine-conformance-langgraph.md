---
name: Engine Conformance LangGraph
description: Run the shared configuration conformance suite with LangGraph.
on:
  workflow_dispatch:
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
model: copilot/claude-haiku-5.5
engine:
  id: langgraph
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-langgraph
tools:
  github:
    mode: gh-proxy
    toolsets: [repos]
  work-queue:
    worker: true
    require-assignment: true
imports:
  - shared/langgraph.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: langgraph
  - shared/engine-conformance-worker.md
---

Execute the imported engine configuration conformance suite once.
