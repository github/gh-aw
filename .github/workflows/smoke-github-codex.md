---
private: true
name: Smoke GitHub Codex
description: Canary for Codex inference through Copilot auto, with a concrete Codex model control.
intent: Detect whether Codex can complete inference through Copilot auto and compare a concrete Codex model.
on:
  workflow_dispatch:
    inputs:
      model:
        description: Copilot model to test; use the concrete Codex model as a control.
        type: choice
        default: copilot/auto
        options:
          - copilot/auto
          - copilot/gpt-5.3-codex
permissions:
  contents: read
  copilot-requests: write
engine:
  id: codex
  model-provider: github
model: ${{ inputs.model || 'copilot/auto' }}
checkout: false
tools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
---

# Codex Copilot Auto Canary

Call the `noop` safe-output tool once with message `CODEX_COPILOT_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
