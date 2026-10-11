---
private: true
emoji: "🧭"
description: Smoke Copilot CLI model routing with deterministic runner and proxy evidence checks
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
name: Smoke Copilot Routed
concurrency:
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: copilot
  bare: true
  model-routing:
    goal: cost
    mode: economy
    allowed-models: [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]
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
    close-older-key: "smoke-copilot-routed"
    labels: [automation, testing]
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
post-steps:
  - name: Assert model-routing evidence
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
          allowedModels: ["gpt-5.4-mini", "gpt-5.6-luna", "claude-haiku-4.5"],
        });
---

# Smoke Test: Copilot CLI Model Routing

This run checks model routing, not the task. Keep it trivial and short.

## Task

Run `echo 42` once and confirm the output is `42`. Do not make any other tool calls except the issue below.

## Output

Create an issue titled **"Smoke Test: Copilot Routed - ${{ github.run_id }}"** with:
- ✅ or ❌ for the task above
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report.
