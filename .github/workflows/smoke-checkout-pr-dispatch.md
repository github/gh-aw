---
private: true
emoji: "🧪"
name: Smoke Checkout PR Dispatch
description: Integration test validating that workflow_dispatch events with aw_context.item_type == 'pull_request' correctly check out the PR branch
on:
  schedule: every 2 days
  slash_command:
    name: smoke-checkout-pr-dispatch
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke-checkout-pr-dispatch"]
  status-comment: true
permissions:
  contents: read
  pull-requests: read
engine: copilot
strict: true
network:
  allowed:
    - defaults
imports:
  - shared/otlp.md
  - shared/reporting.md
tools:
  bash:
    - "git status"
    - "git log *"
    - "git branch *"
    - "git remote *"
    - "echo *"
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 1
  messages:
    footer: "> 🧪 *checkout_pr_branch dispatch smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🧪 [{workflow_name}]({run_url}) is validating workflow_dispatch PR branch checkout..."
    run-success: "✅ [{workflow_name}]({run_url}) successfully validated workflow_dispatch PR branch checkout."
    run-failure: "[{workflow_name}]({run_url}) failed to validate workflow_dispatch PR branch checkout: {status}"
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: workflow_dispatch + aw_context PR Branch Checkout

This workflow validates the `checkout_pr_branch.cjs` behaviour when triggered via `workflow_dispatch`
with an `aw_context` input whose `item_type` is `"pull_request"`. The setup step should detect the
context, fetch `refs/pull/<N>/head`, and check out the PR branch before the agent runs.

It also exercises the same checkout path when triggered directly by a pull-request event or
`slash_command`, ensuring both code paths are exercised.

## Context

- Event: `${{ github.event_name }}`
- `aw_context` input (if present): `${{ github.event.inputs.aw_context }}`

## Test Requirements

Run each check and mark as ✅ pass or ❌ fail:

1. **Git status**: Run `git status` and confirm the workspace is in a clean, initialised state.
2. **Branch check**: Run `git branch --show-current` and record the checked-out branch name.
3. **Remote check**: Run `git remote -v` and confirm a remote named `origin` is present.
4. **Log check**: Run `git log --oneline -3` and confirm at least one commit is visible.
5. **Not default branch**: When triggered via `workflow_dispatch` with a PR `aw_context`, or via
   a PR event, the current branch **must not** be `main` — a different branch name confirms the
   PR checkout ran successfully.

## Output

Add a comment summarising the checkout validation results:

- Event name and, if `${{ github.event_name }}` is `workflow_dispatch`, the `item_number` extracted
  from `aw_context` (parse `${{ github.event.inputs.aw_context }}` to find it)
- Current branch name (from `git branch --show-current`)
- Last 3 commits (from `git log --oneline -3`)
- Whether the workspace is on a branch other than `main`
- Overall status: PASS or FAIL with a brief explanation