---
private: true
emoji: "🧪"
name: Smoke Workflow Call with Inputs
description: Reusable workflow with inputs - used to test that multiple callers don't clash on artifact names
on:
  schedule: every 2 days
  workflow_call:
    inputs:
      task-description:
        description: 'Short description of what this invocation should do'
        required: false
        default: 'generic task'
        type: string
      task-count:
        description: 'Optional task count used to verify zero-valued numeric defaults'
        required: false
        default: 0
        type: number
  workflow_dispatch:
    inputs:
      task-description:
        description: 'Short description of what this invocation should do'
        required: false
        default: 'generic task'
        type: string
      task-count:
        description: 'Optional task count used to verify zero-valued numeric defaults'
        required: false
        default: 0
        type: number
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: true
network:
  allowed:
    - defaults
imports:
  - shared/otlp.md
tools:
  cli-proxy: true
  bash:
    - "echo *"
    - "date"
safe-outputs:
  allowed-domains: [default-safe-outputs]
  noop:
timeout-minutes: 5
features:
  gh-aw-detection: false
sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
---

# Smoke Test: Workflow Call with Inputs

This reusable workflow is designed to be called multiple times from the same parent workflow.
It validates that artifact names do not clash when multiple callers invoke this workflow concurrently
or sequentially in the same GitHub Actions workflow run.

## Task

Task description: "${{ inputs.task-description }}"
Task count: "${{ inputs.task-count }}"

Execute `echo "Running task: ${{ inputs.task-description }}"` and then call the noop safe-output with
a message that includes the task description so the invocation is identifiable in the logs.

```json
{"noop": {"message": "Completed task: ${{ inputs.task-description }}"}}
```