---
private: true
emoji: "🧪"
description: Deterministic Pi smoke test adapted from the Copilot inline sub-agent workflow
on:
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
name: Smoke Pi Sub Agents
strict: true
model: copilot/gpt-5.3-codex
engine:
  id: pi
  bare: true
max-tool-calls: 20
max-tool-denials: 3
tools:
  bash: false
  cli-proxy: false
  edit: false
safe-outputs:
  staged: true
  create-issue:
    labels: [automation, testing]
timeout-minutes: 5
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Pi Inline Sub-Agents

This is the Pi variant of `smoke-copilot-sub-agents.md`. Keep output short.

## Tasks

1. Use the managed `subagent` tool exactly once per declared agent, sequentially:
   - `{"agent":"haiku-whoami","task":"who am i?"}`
   - `{"agent":"mini-whoami","task":"who am i?"}`
   - `{"agent":"nano-whoami","task":"who am i?"}`
2. Check the exact responses: `claude-haiku-4.5`, `gpt-5-mini`, and `gpt-5-nano`.
3. Mark a missing tool, failed delegation, or unexpected response as FAIL. Do not
   answer on behalf of a sub-agent or substitute a different agent.
4. Do not make other tool calls except the staged `create_issue` report below.

## Output

Create a staged issue titled **"Smoke Test: Pi Sub Agents - ${{ github.run_id }}"**:
- One line per sub-agent: expected response, actual response, PASS or FAIL.
- Overall PASS only if all three delegations succeed and all responses match.
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

These fixed responses test delegation, not a model's ability to identify itself.
The dispatch records and inference model usage provide model-selection evidence.

## agent: `haiku-whoami`
---
description: Returns the Haiku model identity for smoke testing
model: claude-haiku-4.5
---
When asked `who am i?`, reply with exactly:

`claude-haiku-4.5`

No extra words, punctuation, or formatting.

## agent: `mini-whoami`
---
description: Returns the GPT-5 mini model identity for smoke testing
model: gpt-5-mini
---
When asked `who am i?`, reply with exactly:

`gpt-5-mini`

No extra words, punctuation, or formatting.

## agent: `nano-whoami`
---
description: Returns the GPT-5 nano model identity for smoke testing
model: gpt-5-nano
---
When asked `who am i?`, reply with exactly:

`gpt-5-nano`

No extra words, punctuation, or formatting.
