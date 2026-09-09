---
private: true
emoji: "🧪"
description: "Tests that max-ai-credits: 1 is enforced by the AWF firewall and that the per-run budget guardrail cuts off the agent. Concludes success when the credit limit is reached."
features:
  gh-aw-detection: true
on:
  schedule: daily around 10:30
  workflow_dispatch:
max-ai-credits: 1
max-daily-ai-credits: -1
permissions:
  contents: read
  issues: read
  copilot-requests: write
engine:
  id: codex
model: copilot/gpt-5.3-codex
strict: true
sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
timeout-minutes: 5
network: {}
safe-outputs:
  noop:
  create-issue:
    expires: 24h
    close-older-issues: true
    close-older-key: "daily-max-ai-credits-test"
    labels: [automation, testing]
    max: 1
  report-failure-as-issue:
    - "!ai_credits_rate_limit_error"
    - "!max_ai_credits_exceeded"
  messages:
    run-started: "🧪 [{workflow_name}]({run_url}) — per-run AI credit limit test running (limit: 1 AI credit/run)."
    run-success: "🧪 [{workflow_name}]({run_url}) — expected: the per-run AI credit limit of 1 was reached and the AWF firewall cut off the agent."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status} — completed without hitting the per-run limit of 1 AI credit — verify that max-ai-credits enforcement is working."
imports:
  - shared/reporting.md
---

### Daily Max AI Credits Test

> 🧪 **This workflow tests the per-run AI credits guardrail.** It verifies that
> `max-ai-credits: 1` is enforced by the AWF firewall and that the agent is
> cut off when the per-run budget is exhausted. The run **concludes success**
> when the credit limit is reached as expected.

#### What This Tests

1. The AWF firewall enforces the `max-ai-credits` per-run budget.
2. Once the agent consumes more than 1 AI credit in a single run, the firewall cuts off the LLM API.
3. The prompt forces multiple turns and multiple large-file reads so the run reliably burns credits.
4. The run is expected to be cut off by the per-run budget before all turns complete.

#### Task

Use **at least four separate assistant turns**. Do not combine all work into one response.

Turn 1 (Job 1): Read a large file: `pkg/parser/schemas/main_workflow_schema.json`.

Turn 2 (Job 2): Read a large file: `.github/workflows/daily-max-ai-credits-test.lock.yml`.

Turn 3 (Job 3): Read a large file: `.github/workflows/daily-credit-limit-test.lock.yml`.

Turn 4: Call `noop` with the message: "Completed max-ai-credits multi-turn guardrail test."

After each job, briefly summarize what was read, then continue to the next turn.

Since the per-run budget is `max-ai-credits: 1`, the AWF firewall should cut off the agent
before all turns complete. That is the expected and correct outcome — the run will conclude success.

If the workflow somehow completes without hitting the per-run limit, call `noop` with the message:
"Per-run credit limit not exceeded — verify that max-ai-credits: 1 is enforced by the AWF firewall."