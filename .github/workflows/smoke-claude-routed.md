---
private: true
emoji: "🧭"
description: Smoke Claude Code model routing with deterministic runner and proxy evidence checks
on:
  schedule: every 2 days
  workflow_dispatch:
  label_command:
    names: ["smoke-routing", "smoke"]
    events: [pull_request]
    strategy: decentralized
  bots: [Copilot]
  status-comment: false
  github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
permissions:
  contents: read
  copilot-requests: write
name: Smoke Claude Routed
concurrency:
  group: "smoke-claude-routed-${{ github.event_name == 'pull_request' && format('pr-{0}', github.event.pull_request.number) || github.run_id }}"
  cancel-in-progress: true
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: claude
  model-provider: github
  bare: true
model: router/economy
model-routing:
  # claude-haiku-4.5 is excluded: it supports no reasoning effort, and Claude
  # Code on GitHub inference sends one (see smoke-claude-on-copilot's 400).
  allowed-models: [claude-sonnet-5]
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
tools:
  bash:
    - "echo:*"
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-claude-routed"
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
          engine: "claude",
          executionOutcome: process.env.SMOKE_EXECUTION,
          allowedModels: ["claude-sonnet-5"],
          mainEndpoint: "/v1/messages",
          expectModelInventory: true,
          copilotInventory: true,
          resolutionRoles: [],
        });
---

# Smoke Test: Claude Code Model Routing

This run checks model routing, not the task. Keep it trivial and short.

## Task

Run `echo 42` once and confirm the output is `42`. Do not make any other tool calls except the issue below.

## Output

Create an issue titled **"Smoke Test: Claude Routed - ${{ github.run_id }}"** with:
- ✅ or ❌ for the task above
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report.
