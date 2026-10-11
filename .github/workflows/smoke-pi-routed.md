---
private: true
emoji: "🧭"
description: Smoke pi model routing with cross-family declared sub-agents and deterministic runner and proxy evidence checks
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
name: Smoke Pi Routed
concurrency:
  job-discriminator: ${{ github.run_id }}
strict: true
engine:
  id: pi
  bare: true
  model-routing:
    goal: cost
    mode: economy
    allowed-models: [gpt-5.6-luna]
imports:
  - shared/reporting.md
max-tool-calls: 20
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
    close-older-key: "smoke-pi-routed"
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
          engine: "pi",
          executionOutcome: process.env.SMOKE_EXECUTION,
          allowedModels: ["gpt-5.6-luna"],
          subAgents: [
            { name: "haiku-whoami", model: "claude-haiku-4.5", endpoint: "/v1/messages" },
            { name: "mini-whoami", model: "gpt-5.4-mini", endpoint: "/responses" },
          ],
        });
---

# Smoke Test: Pi Model Routing With Sub-Agents

This run checks model routing and sub-agent delegation, not the task. Keep output short.

## Tasks

1. Use the managed `subagent` tool exactly once per declared agent, sequentially:
   - `{"agent":"haiku-whoami","task":"who am i?"}`
   - `{"agent":"mini-whoami","task":"who am i?"}`
2. Do not answer on behalf of a sub-agent or substitute a different agent.
3. Do not make other tool calls except the issue below.

## Output

Create an issue titled **"Smoke Test: Pi Routed - ${{ github.run_id }}"** with:
- One line per sub-agent: its response, or the error if delegation failed.
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

The routing and sub-agent verdict comes from the post-step, which checks runner- and proxy-written evidence, not this report.

## agent: `haiku-whoami`
---
description: Returns a fixed reply for model-routing smoke testing
model: copilot/claude-haiku-4.5
---
When asked `who am i?`, reply with this single plain-text line, without Markdown
or backticks:

haiku-whoami

Do not call tools. No extra words, punctuation, or formatting.

## agent: `mini-whoami`
---
description: Returns a fixed reply for model-routing smoke testing
model: copilot/gpt-5.4-mini
---
When asked `who am i?`, reply with this single plain-text line, without Markdown
or backticks:

mini-whoami

Do not call tools. No extra words, punctuation, or formatting.
