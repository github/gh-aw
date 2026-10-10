---
private: true
emoji: "🧭"
description: Smoke Copilot SDK model routing with a cross-family declared sub-agent and deterministic runner and proxy evidence checks
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
name: Smoke Copilot SDK Routed
concurrency:
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: copilot
  copilot-sdk: true
  bare: true
  model-routing:
    goal: cost
    mode: economy
    allowed-models: [gpt-5.6-luna]
imports:
  - shared/reporting.md
max-tool-denials: 3
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-sdk-routed"
    labels: [automation, testing]
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
post-steps:
  - name: Assert model-routing and sub-agent evidence
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
          mainEndpoint: "/responses",
          subAgents: [{ name: "haiku-whoami", model: "claude-haiku-4.5", endpoint: "/chat/completions" }],
          requireDeclaredAgentNames: true,
        });
---

# Smoke Test: Copilot SDK Model Routing With a Sub-Agent

This run checks model routing and sub-agent delegation, not the task. Keep output short.

## Tasks

1. Call the `haiku-whoami` agent exactly once and ask it exactly: `who am i?`
2. Do not answer on its behalf, and do not call any other agent.
3. Do not make other tool calls except the issue below.

## Output

Create an issue titled **"Smoke Test: Copilot SDK Routed - ${{ github.run_id }}"** with:
- The `haiku-whoami` response, or the error if delegation failed.
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing and sub-agent verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report.

## agent: `haiku-whoami`
---
description: Returns a fixed reply for model-routing smoke testing
model: claude-haiku-4.5
---
When asked `who am i?`, reply with exactly:

`haiku-whoami`

No extra words, punctuation, or formatting.
