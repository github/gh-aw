---
private: true
name: Smoke Codex Bare Auto
description: Smoke test for Codex router fallback using a router-unknown Copilot model.
intent: Detect whether Codex falls back cleanly to an eligible router-unknown Copilot model with no_route and zero classifier requests.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: codex
  model-provider: github
model: router
model-routing:
  allowed-models: [gpt-5.3-codex]
checkout: false
tools:
  github: false
evals:
  - id: codex_auto_marker
    question: Does the output contain exactly one noop safe-output request with the exact message CODEX_AUTO_OK?
  - id: inference_only_scope
    question: Does the output show no repository inspection, command execution, or additional task beyond the required noop?
safe-outputs:
  noop:
  threat-detection: false
timeout-minutes: 5
post-steps:
  - name: Assert router fallback evidence
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
          allowedModels: ["gpt-5.3-codex"],
          mainEndpoint: "/responses",
          expectedSelectionSource: "fallback",
          expectedFallbackReason: "no_route",
          expectedClassifierRequests: 0,
          expectModelInventory: true,
          copilotInventory: true,
          resolutionRoles: [],
        });
---

# Codex Bare Auto Smoke Test

Call the `noop` safe-output tool once with message `CODEX_AUTO_OK`.
Do not inspect the repository, run commands, or perform any other task.
