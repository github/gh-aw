---
private: true
emoji: "🧭"
description: Smoke Copilot SDK model routing to a Claude main model with a GPT declared sub-agent and deterministic runner and proxy evidence checks
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
name: Smoke Copilot SDK Routed Claude
concurrency:
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: copilot
  copilot-sdk: true
  bare: true
model: router/economy
model-routing:
  # claude-haiku-4.5 lists no reasoning efforts, so this also covers a Claude
  # model selected without an effort. The sub-agent's model stays out of the pool.
  allowed-models: [claude-haiku-4.5]
imports:
  - shared/reporting.md
max-tool-denials: 3
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-sdk-routed-claude"
    labels: [automation, testing, "backlog:smoke-tests"]
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
          allowedModels: ["claude-haiku-4.5"],
          mainEndpoint: "/chat/completions",
          subAgents: [{ name: "mini-whoami", model: "gpt-5.6-luna", endpoint: "/responses" }],
          requireDeclaredAgentNames: true,
          expectModelInventory: true,
          copilotInventory: true,
          resolutionRoles: ["mini-whoami"],
        });
---

# Smoke Test: Copilot SDK Model Routing to Claude With a GPT Sub-Agent

This run checks model routing and sub-agent delegation, not the task. Keep output short.

## Tasks

1. Call the `mini-whoami` agent once with the `task` tool, setting both `agent_type` and `name` to `mini-whoami` and `mode` to `sync`:
   `{"description":"Ask mini-whoami","prompt":"who am i?","agent_type":"mini-whoami","name":"mini-whoami","mode":"sync"}`
   If the tool returns an error, fix the arguments to match the call above and retry exactly once.
2. Do not answer on its behalf, and do not call any other agent.
3. Do not make other tool calls except the issue below.

## Output

Create an issue titled **"Smoke Test: Copilot SDK Routed Claude - ${{ github.run_id }}"** with:
- The `mini-whoami` response, or the error if delegation failed.
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing and sub-agent verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report.

## agent: `mini-whoami`
---
description: Returns a fixed reply for model-routing smoke testing
model: gpt-5.6-luna
---
When asked `who am i?`, reply with exactly:

`mini-whoami`

No extra words, punctuation, or formatting.
