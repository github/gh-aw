---
private: true
name: Smoke Pi Auto
description: Smoke test for Pi inference using the bare auto model.
intent: Detect whether Pi can complete inference and emit a safe output using auto.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine: pi
model: auto
checkout: false
tools:
  github: false
evals:
  - id: pi_auto_marker
    question: Does the output contain exactly one noop safe-output request with the exact message PI_AUTO_OK?
  - id: inference_only_scope
    question: Does the output show no repository inspection, command execution, or additional task beyond the required noop?
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
---

# Pi Auto Smoke Test

Call the `noop` safe-output tool once with message `PI_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
