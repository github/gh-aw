---
private: true
emoji: "🧪"
name: Smoke Workflow Call
description: Reusable workflow to validate checkout from fork works correctly in workflow_call context
on:
  schedule: every 2 days
  workflow_call:
    inputs:
      payload:
        type: string
        required: false
      task-description:
        description: Short description of the validation task to include in the output comment
        type: string
        required: false
        default: validate workflow_call checkout
  workflow_dispatch:
    inputs:
      task-description:
        description: Short description of the validation task to include in the output comment
        type: string
        required: false
        default: validate workflow_call checkout
permissions:
  contents: write
  pull-requests: read
engine: claude
strict: true
network:
  allowed:
    - defaults
imports:
  - shared/otlp.md
  - shared/reporting.md
tools:
  dispatch-work-coordinator:
    id: smoke-call-workflow
    schema:
      type: object
      properties:
        task_id:
          type: string
        task_description:
          type: string
      required: [task_id, task_description]
      additionalProperties: false
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
  dispatch-claim-finish:
    max: 1
  messages:
    append-only-comments: true
    footer: "> 🔁 *workflow_call smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🔁 [{workflow_name}]({run_url}) is validating workflow_call checkout..."
    run-success: "✅ [{workflow_name}]({run_url}) successfully validated workflow_call checkout."
    run-failure: "❌ [{workflow_name}]({run_url}) failed to validate workflow_call checkout. Check the logs."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Workflow Call Checkout Validation

This workflow is designed to be called via `workflow_call` from another workflow (e.g., `smoke-trigger`).
It validates that the PR branch checkout works correctly when invoked in a `workflow_call` context.

## Test Requirements

Use `aw_context.dispatch_work_coordinator.work.task_description` as the task when a trusted Dispatch Work Coordinator assignment is present. Do not derive the task from a caller-controlled workflow input in that case.

1. **Git Status**: Run `git status` to verify the workspace is properly initialized.
2. **Branch Check**: Run `git branch --show-current` to confirm which branch is checked out.
3. **Remote Check**: Run `git remote -v` to confirm the remote configuration.
4. **Log Check**: Run `git log --oneline -3` to verify the commit history is available.

## Output

Add a comment summarizing the checkout validation results:
- Task: the trusted Work task description, or "${{ inputs.task-description }}" when no coordinator assignment is present
- Current branch name
- Whether the workspace is clean or has changes
- Whether the checkout succeeded (based on git commands working without errors)
- Overall status: ✅ PASS or ❌ FAIL

When a trusted Dispatch Work Coordinator assignment is present, finish it exactly once with `dispatch_claim_finish` and an outcome containing the validation status. Do not include Work or Claim authority fields in the outcome.