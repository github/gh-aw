---
name: Engine Conformance DeepSeek Harness
description: Run the shared configuration conformance suite with DeepSeek Harness.
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
  id: deepseek-harness
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-deepseek-harness
imports:
  - shared/deepseek-harness.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: deepseek-harness
tools:
  github:
    mode: gh-proxy
    toolsets: [repos]
---

Execute the imported engine configuration conformance suite once.
