---
private: true
name: Dev mini-SWE-agent Smoke Test
description: Minimal mini-SWE-agent inference, shell, and noop test on GitHub.
on:
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: mini-swe-agent
  model-provider: github
model: copilot/claude-haiku-5.5
imports:
  - shared/mini-swe-agent.md
checkout: false
network:
  allowed: []
tools:
  github:
    mode: gh-proxy
    toolsets: [repos]
  bash: ["*"]
safe-outputs:
  create-issue: false
  noop:
    report-as-issue: false
  report-incomplete: false
  report-missing-tool: false
  report-missing-data: false
  report-failure-as-issue: false
  threat-detection: false
timeout-minutes: 5
max-turns: 5
max-ai-credits: 5
sandbox:
  agent:
    id: awf
---

# mini-SWE-agent Smoke Test

Run `printf 'MINI_SWE_AGENT_OK\n'`.
Call `safeoutputs noop` once with message `MINI_SWE_AGENT_OK`.
Finally run `echo COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT` as a separate action.
Do not inspect the repository, edit files, or perform any other task.
