---
private: true
emoji: "🧭"
description: Smoke Claude Code model routing on native Anthropic inference with deterministic runner and proxy evidence checks
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
name: Smoke Claude Routed Anthropic
concurrency:
  group: "smoke-claude-routed-anthropic-${{ github.event_name == 'pull_request' && format('pr-{0}', github.event.pull_request.number) || github.run_id }}"
  cancel-in-progress: true
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: claude
  bare: true
  model-routing:
    goal: cost
    mode: economy
    # Native Anthropic routing keeps ANTHROPIC_API_KEY inference. claude-sonnet-5
    # has maintained AWF routing metadata (Messages, low..max effort); Haiku has
    # none on the native path, so it is not a routing candidate here.
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
    close-older-key: "smoke-claude-routed-anthropic"
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
          routingMode: "economy",
          nativeProvider: "anthropic",
          expectRequestedEffortMatchesSelection: true,
          expectModelInventory: true,
          resolutionRoles: [],
        });
---

# Smoke Test: Claude Code Model Routing on Anthropic

This run checks native Anthropic model routing, not the task. Keep it trivial and short.

## Task

Run `echo 42` once and confirm the output is `42`. Do not make any other tool calls except the issue below.

## Output

Create an issue titled **"Smoke Test: Claude Routed Anthropic - ${{ github.run_id }}"** with:
- ✅ or ❌ for the task above
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report. It fails unless the proxy selected and served the native `anthropic` provider with the exact wire model and the selected effort.
