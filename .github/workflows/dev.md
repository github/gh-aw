---
private: true
name: Dev Auto Model Smoke Test
description: Minimal inference and safe-output test for Pi, Codex, and Claude using auto on GitHub.
intent: Detect whether an engine can complete GitHub inference and emit a safe output using auto.
on:
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: pi
  model-provider: github
model: auto
checkout: false
tools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
evals:
  - id: auto_inference_completed
    question: Did the agent call noop exactly once with message AUTO_MODEL_OK without performing any other task?
---

# Auto Model Smoke Test

Call the `noop` safe-output tool exactly once with message `AUTO_MODEL_OK`.
Do not inspect the repository, run commands, or perform any other task.
