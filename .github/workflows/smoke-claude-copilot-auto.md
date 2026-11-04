---
private: true
name: Smoke Claude Copilot Auto
description: Smoke test for Claude inference using the copilot/auto model.
intent: Detect whether Claude can select a compatible Copilot model and emit a safe output using copilot/auto.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine: claude
model: copilot/auto
checkout: false
tools:
  github: false
evals:
  - id: claude_copilot_auto_marker
    question: Does the output contain exactly one noop safe-output request with the exact message CLAUDE_COPILOT_AUTO_OK?
  - id: inference_only_scope
    question: Does the output show no repository inspection, command execution, or additional task beyond the required noop?
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
---

# Claude Copilot Auto Smoke Test

Call the `noop` safe-output tool once with message `CLAUDE_COPILOT_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
