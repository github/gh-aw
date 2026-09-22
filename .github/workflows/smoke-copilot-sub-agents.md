---
private: true
emoji: "🧪"
description: Smoke Copilot SDK inline sub-agents
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
name: Smoke Copilot Sub Agents
strict: true
model: gpt-5.3-codex
engine:
  id: copilot
  copilot-sdk: true
  bare: true
max-tool-denials: 3
experiments:
  sub_agent_strategy:
    variants: [inline_strict, delegated_sequential, single_agent_control]
    description: "Measure whether inline sub-agent orchestration is the best reliability/cost tradeoff for a model-identity smoke test."
    hypothesis: "H0: no change in pass_rate between inline_strict and delegated_sequential. H1: delegated_sequential improves pass_rate by >= 0.15 absolute versus inline_strict. Note: single_agent_control is a synthetic negative baseline (always FAIL by design) excluded from H1 comparisons."
    metric: pass_rate
    secondary_metrics: [run_duration_seconds, output_validity_rate]
    guardrail_metrics:
      - name: empty_output_rate
        direction: min
        threshold: 0.01
      - name: false_pass_rate
        direction: min
        threshold: 0.05
    min_samples: 30
    weight: [34, 33, 33]
    start_date: "2026-07-23"
    end_date: "2026-10-23"
    issue: 47551
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-sub-agents"
    labels: [automation, testing]
timeout-minutes: 10
features:
  gh-aw-detection: false
imports:
  - shared/reporting.md
evals:
  - id: sub_agent_strategy_goal_met
    question: Does the agent output show that the objective for experiment sub_agent_strategy was successfully completed?

sandbox:
  agent:
    id: awf
---

# Smoke Test: Copilot SDK Inline Sub-Agents

**IMPORTANT: Keep all outputs extremely short.**

## Tasks

{{#if experiments.sub_agent_strategy == 'single_agent_control' }}
1. Do not call any sub-agent.
2. Produce the issue in the same format, but mark each agent as `not_invoked`.
3. Set overall status to FAIL.
4. Do not use unnecessary tool calls.
{{/if}}
{{#if experiments.sub_agent_strategy == 'delegated_sequential' }}
1. Call `haiku-whoami`, `mini-whoami`, and `nano-whoami` exactly once each.
2. Ask each sub-agent exactly this question: `who am i?`
3. Check the exact responses:
   - `haiku-whoami` → `claude-haiku-4.5`
   - `mini-whoami` → `gpt-5-mini`
   - `nano-whoami` → `gpt-5-nano`
4. Execute and validate each sub-agent one at a time before moving to the next.
5. Do not use any other agent or any unnecessary tool calls.
{{/if}}
{{#if experiments.sub_agent_strategy == 'inline_strict' }}
1. Call `haiku-whoami`, `mini-whoami`, and `nano-whoami` exactly once each.
2. Ask each sub-agent exactly this question: `who am i?`
3. Check the exact responses:
   - `haiku-whoami` → `claude-haiku-4.5`
   - `mini-whoami` → `gpt-5-mini`
   - `nano-whoami` → `gpt-5-nano`
4. Validate all three results and keep the current inline orchestration behavior.
5. Do not use any other agent or any unnecessary tool calls.
{{/if}}

## Output

Always create an issue titled **"Smoke Test: Copilot Sub Agents - ${{ github.run_id }}"** with:
- One line per sub-agent showing expected value, actual value, and ✅/❌
- Overall status: PASS only if all three exact matches succeed, otherwise FAIL
- Experiment variant: `{{ experiments.sub_agent_strategy }}`
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

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