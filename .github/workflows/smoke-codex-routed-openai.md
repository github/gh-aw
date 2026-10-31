---
private: true
emoji: "🧭"
description: Smoke Codex model routing on native OpenAI inference with deterministic runner and proxy evidence checks
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
name: Smoke Codex Routed OpenAI
concurrency:
  group: "smoke-codex-routed-openai-${{ github.event_name == 'pull_request' && format('pr-{0}', github.event.pull_request.number) || github.run_id }}"
  cancel-in-progress: true
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: codex
  model-routing:
    goal: cost
    mode: economy
    # Native OpenAI routing keeps CODEX_API_KEY / OPENAI_API_KEY inference.
    # gpt-5.4 has maintained AWF routing metadata (Responses, none..xhigh effort).
    allowed-models: [gpt-5.4]
imports:
  - shared/smoke-test-brevity.md
  - shared/reporting.md
checkout: false
tools:
  github: false
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-codex-routed-openai"
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
          engine: "codex",
          executionOutcome: process.env.SMOKE_EXECUTION,
          allowedModels: ["gpt-5.4"],
          mainEndpoint: "/responses",
          routingMode: "economy",
          nativeProvider: "openai",
          expectRequestedEffortMatchesSelection: true,
          expectModelInventory: true,
          resolutionRoles: [],
        });
---

# Smoke Test: Codex Model Routing on OpenAI

This run checks native OpenAI model routing, not the task. Keep it trivial and short.

## Task

Do not inspect the repository or run commands.

## Output

Create an issue titled **"Smoke Test: Codex Routed OpenAI - ${{ github.run_id }}"** with:
- ✅ for the task above
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report. It fails unless the proxy selected and served the native `openai` provider with the exact wire model and the selected effort.
