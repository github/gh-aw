---
private: true
name: Smoke GitHub Codex
description: Canary for Codex inference through Copilot.
intent: Detect whether Codex can complete inference through Copilot.
on:
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine: codex
model: copilot/gpt-5.3-codex
checkout: false
tools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
---

# Codex Copilot Canary

Call the `noop` safe-output tool once with message `CODEX_COPILOT_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
