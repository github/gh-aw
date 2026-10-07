---
name: ESLint Factory Dispatcher
description: Dispatches queued ESLint factory work to the corresponding worker
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine: copilot
tools:
  work-queue: true
  cli-proxy: true
safe-outputs:
  dispatch-workflow:
    workflows: [eslint-miner, eslint-refiner, eslint-monster]
    target-ref: ${{ github.event.repository.default_branch }}
    max: 3
  noop:
timeout-minutes: 15
strict: true
---

# ESLint Factory Dispatcher

Do not select Work IDs, workers, revisions, or targets from a queue snapshot. Request the trusted scheduler's fair prefix with `work_queue_dispatch_next`, for example `{"pool":"default","max_claims":3,"max_dispatches":3}`. Keep each request within the installed pool policy and this workflow's dispatch budget; issue at most one request for this pool in a run. The scheduler selects eligible Work and launches only the compatible worker profile at its installed immutable workflow revision. This workflow's `safe-outputs.dispatch-workflow.workflows` list is the compiler-approved worker-name allowlist; it does not replace the installed policy's profile, revision, or principal binding. Do not call ordinary `dispatch_workflow` or typed per-worker dispatch tools.

The authenticated operator provisions the queue Policy and authorized producers before this workflow runs. Do not create or select queue entries unless this workflow is separately authorized as a producer. If the request reports no eligible work, use `noop`; stale snapshot ordering is never authority to dispatch.
