---
private: true
emoji: "🧪"
description: "⚠️ INTENTIONALLY BROKEN — Tests that max-daily-ai-credits: 1 is enforced by the activation guardrail and that a limit-exceeded message is posted when the daily budget is consumed."
features:
  intentional-failure: true
  gh-aw-detection: true
on:
  schedule: every 12 hours
  workflow_dispatch:
max-daily-ai-credits: 1
permissions:
  copilot-requests: write
  contents: read
  issues: read
engine: pi
model: copilot/auto
strict: true
sandbox:
  agent:
    id: awf
timeout-minutes: 5
network: {}
safe-outputs:
  noop:
  create-issue:
    expires: 24h
    close-older-issues: true
    close-older-key: "daily-credit-limit-test"
    labels: [automation, testing]
    max: 1
  messages:
    run-started: "🧪 [{workflow_name}]({run_url}) — credit limit test running (intentionally broken, limit: 1 AI credit/day)."
    run-success: "⚠️ [{workflow_name}]({run_url}) completed without hitting the daily limit of 1 AI credit — verify that credit accounting is working."
    run-failure: "🚫 [{workflow_name}]({run_url}) {status} — expected: the daily AI credit limit of 1 was reached and this run was blocked."
imports:
  - shared/reporting.md
---

### Daily Credit Limit Test (Intentionally Broken)

> ⚠️ **This workflow is intentionally broken.** It exists solely to verify that
> `max-daily-ai-credits: 1` is enforced by the activation guardrail and that the
> system posts the appropriate limit-exceeded message when the budget is consumed.
> **Do not fix this workflow.**

#### What This Tests

1. The activation job checks whether the last 24 hours of runs consumed more than 1 AI credit.
2. If the limit is exceeded the agent job is skipped and a "Daily Workflow AIC Guardrail Exceeded"
   message is posted by the conclusion job.
3. Because any real model invocation exceeds 1 AI credit, this workflow will always trip the
   guardrail after the very first run.

#### Task (broken by design)

Call the tool `nonexistent_tool_that_does_not_exist` with the argument `"credit-limit-test"`.

This tool does not exist. The call will always fail. This is intentional.

If the daily credit limit of 1 has already been consumed by a prior run today, this workflow
will never reach this point — the activation guardrail will block it first and post the
limit-exceeded message. That is the expected and correct outcome.

If the agent somehow reaches this point (first run only), call `noop` with the message:
"First run: daily credit limit not yet exhausted. Subsequent runs will be blocked by the guardrail."