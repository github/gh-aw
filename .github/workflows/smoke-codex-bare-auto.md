---
private: true
name: Smoke Codex Bare Auto
description: Smoke test for Codex inference using the bare auto model on GitHub.
intent: Detect whether Codex can complete inference and emit a safe output using auto.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: codex
  model-provider: github
model: auto
checkout: false
tools:
  github: false
evals:
  - id: codex_auto_marker
    question: Does the output contain exactly one noop safe-output request with the exact message CODEX_AUTO_OK?
  - id: inference_only_scope
    question: Does the output show no repository inspection, command execution, or additional task beyond the required noop?
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
---

# Codex Bare Auto Smoke Test

Call the `noop` safe-output tool once with message `CODEX_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
