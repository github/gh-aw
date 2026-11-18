---
private: true
emoji: "🧭"
description: Smoke pi model routing to a Claude main model in robust mode with deterministic runner and proxy evidence checks
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
name: Smoke Pi Routed Claude
concurrency:
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: pi
  bare: true
model: router/robust
model-routing:
  # Claude-only pool: the main model's family, and so its endpoint
  # (/v1/messages), is fixed; the model's effort is not.
  allowed-models: [claude-sonnet-5]
imports:
  - shared/reporting.md
max-tool-calls: 10
max-tool-denials: 3
tools:
  bash: false
  cli-proxy: false
  edit: false
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-pi-routed-claude"
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
          engine: "pi",
          executionOutcome: process.env.SMOKE_EXECUTION,
          allowedModels: ["claude-sonnet-5"],
          mainEndpoint: "/v1/messages",
          routingMode: "robust",
          expectModelInventory: true,
          copilotInventory: true,
          resolutionRoles: [],
        });
---

# Smoke Test: Pi Model Routing to Claude

This run checks model routing, not the task. Keep it trivial and short.

## Task

Answer the question "what is 6 × 7?" in one word. Do not make any tool calls except the issue below.

## Output

Create an issue titled **"Smoke Test: Pi Routed Claude - ${{ github.run_id }}"** with:
- ✅ or ❌ for the task above
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report.
