---
private: true
emoji: "💻"
name: Dev
description: Scratchpad smoke test for Codex with Copilot auto model routing
on:
  workflow_dispatch:
concurrency:
  job-discriminator: ${{ github.run_id }}
permissions:
  copilot-requests: write
  contents: read
engine: codex
model: copilot/auto
timeout-minutes: 5
strict: true
network: defaults
imports:
  - shared/otlp.md
tools:
  cli-proxy: true
safe-outputs:
  noop:
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Codex Auto Smoke Test

Verify that Codex can complete a tool-using turn through `copilot/auto`.

1. Run `echo CODEX_AUTO_TRACE_OK` exactly once and verify its output is exactly
   `CODEX_AUTO_TRACE_OK`.
2. Call the `noop` safe-output tool exactly once with the message
   `CODEX_AUTO_TRACE_OK: echo verified`.
3. Finish with exactly `CODEX_AUTO_TRACE_OK`.

Do not read repository files, access GitHub, or run unrelated commands. If either
tool call fails, report the error instead of claiming success.
