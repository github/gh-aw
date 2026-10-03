---
name: Engine Conformance Aider
description: Run the shared configuration conformance suite with Aider.
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
  id: aider
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-aider
imports:
  - shared/aider.md
  - uses: shared/engine-conformance.md
    with:
      engine-id: aider
tools:
  github:
    mode: gh-proxy
    toolsets: [repos]
---

Execute the imported engine configuration conformance suite once.

Aider is single-turn: implement the imported probes in a small JavaScript file
at `/tmp/gh-aw/agent/engine-conformance/run-probes.cjs` using SEARCH/REPLACE blocks,
then execute it with one `node` command in a bash block. The program must read
the fixture, run the supplied shell probe, invoke
`mcpscripts` with `spawnSync` and an argument array, and write `result.json` with
the actual returned values. Preserve nonzero child exit codes. Do not guess
nonces or replace the supplied probe. Emit the final staged noop in a bash block.
