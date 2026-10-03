---
private: true
name: Smoke Codex Auto
description: Smoke test for Codex inference using the Copilot auto model.
intent: Detect whether Codex can complete inference using copilot/auto.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine: codex
model: copilot/auto
checkout: false
tools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
---

# Codex Auto Smoke Test

Call the `noop` safe-output tool once with message `CODEX_COPILOT_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
