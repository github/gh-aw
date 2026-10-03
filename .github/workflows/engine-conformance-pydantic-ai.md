---
name: Engine Conformance Pydantic AI
description: Run the shared configuration conformance suite with Pydantic AI.
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
  id: pydantic-ai
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-pydantic-ai
imports:
  - pydantic/pydantic-ai/src/pydantic_ai_harness/gh-aw/pydantic.md@319d72ccf220427e59b9f4e61f17d7be52c90d68
  - uses: shared/engine-conformance.md
    with:
      engine-id: pydantic-ai
---

Execute the imported engine configuration conformance suite once.
