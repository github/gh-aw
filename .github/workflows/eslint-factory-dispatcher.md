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

Read the queue with `work_queue_read` (use `work-queue work_queue_read '{}'` if the tool is advertised under `<mcp-clis>`). Dispatch at most three available items, selecting the oldest available item for each distinct worker:

- `eslint-miner:<identity>` → `eslint-miner`
- `eslint-refiner:<identity>` → `eslint-refiner`
- `eslint-monster:<identity>` → `eslint-monster`

Only use exact, nonempty identities with one of these prefixes. Call the matching typed tool (`eslint_miner`, `eslint_refiner`, or `eslint_monster`) with `work_queue: {"work_id": "<selected id>"}` at the top level. Do not call `dispatch_workflow`, provide a claim ID, or construct `aw_context`; trusted safe-output processing claims the work and supplies the assignment. Do not dispatch a work item twice in one run. If nothing eligible is available, call `noop`. Queue entries are provisioned by an authorized operator; neither this agent nor its workers can submit work through the read-only queue tools.
