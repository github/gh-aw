---
private: true
emoji: "🧭"
description: Smoke AWF AI-credit steering notices on a routed Copilot CLI run with deterministic runner and proxy evidence checks
on:
  schedule: every 2 days
  workflow_dispatch:
  label_command:
    name: smoke-routing
    events: [pull_request]
  github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
permissions:
  contents: read
  copilot-requests: write
name: Smoke Copilot Routed Steering
concurrency:
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: copilot
  bare: true
  # Remove the built-in task tool so no sub-agent can receive a steering notice.
  args: ["--excluded-tools=task"]
  model-routing:
    goal: cost
    mode: economy
    allowed-models: [gpt-5.6-luna]
# Keep in sync with steering.maxAiCredits in the post-step.
max-ai-credits: 7
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
tools:
  bash:
    - "echo"
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-routed-steering"
    labels: [automation, testing]
# Long enough that no timeout notice comes due before the AI-credit notices.
timeout-minutes: 30
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
post-steps:
  - name: Assert model-routing and steering evidence
    if: always()
    uses: actions/github-script@v9.0.0
    env:
      SMOKE_EXECUTION: ${{ steps.agentic_execution.outcome }}
    with:
      script: |
        const path = require("node:path");
        const { main } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/smoke_model_routing_assertions.cjs"));
        await main({
          core,
          engine: "copilot",
          executionOutcome: process.env.SMOKE_EXECUTION,
          allowedModels: ["gpt-5.6-luna"],
          steering: { maxAiCredits: 7 },
          expectModelInventory: true,
          copilotInventory: true,
          resolutionRoles: [],
        });
---

# Smoke Test: Copilot CLI AI-Credit Steering

This run checks that you react to an AI-credit steering notice, not the task. The run has a small AI-credit budget and the task below asks for more steps than the budget can pay for.

## Rules

- Do all the work yourself. Do not use the task tool, sub-agents or background agents.
- Make exactly one tool call per turn and wait for its result before the next one. Do not batch or parallelize tool calls.
- Do not write any text between steps.

## Task

Run `echo 1`, then `echo 2`, then `echo 3`, and so on up to `echo 200`: one `echo N` bash call per turn, in order.

## Stop on the steering notice

As soon as any message you receive contains text starting with `[AWF AI CREDIT WARNING]`, stop the task immediately. Do not run any further `echo` commands. Then:

1. Create an issue titled **"Smoke Test: Copilot Routed Steering - ${{ github.run_id }}"** with:
   - The amounts stated in the warning you saw (for example `247 of 300 AI credits are used`)
   - The last `N` you echoed
   - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
2. Finish with a one-line reply. Make no other tool calls.

If you reach `echo 200` without seeing the warning, create the same issue stating that no warning was seen, and finish.

The verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report.
