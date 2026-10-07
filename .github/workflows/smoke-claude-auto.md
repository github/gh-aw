---
private: true
name: Smoke Claude Auto
description: Smoke test for Claude inference using the bare auto model on GitHub.
intent: Detect whether Claude can select a compatible Copilot model and emit a safe output using auto.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: claude
  model-provider: github
model: auto
checkout: false
tools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
---

# Claude Auto Smoke Test

Call the `noop` safe-output tool once with message `CLAUDE_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
