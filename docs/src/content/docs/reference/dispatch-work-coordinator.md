---
title: Dispatch Work Coordinator
description: Configure and use a Git-backed Work queue with replayed Claims and safe-output completion.
---

The Dispatch Work Coordinator provides a durable queue for workflows that submit and process Work across multiple runs. Configure it under `tools`; the required `schema` child describes each Work payload and leaves room for future coordinator options.

## Configuration

```aw wrap
permissions:
  contents: write

tools:
  dispatch-work-coordinator:
    id: issue-workers
    schema:
      type: object
      properties:
        title:
          type: string
        priority:
          type: integer
      required: [title]
      additionalProperties: false
```

The root schema type must be `object`. The compiler validates supported JSON Schema keywords and rejects schemas containing GitHub Actions expressions. The same schema validates Work when it is submitted and whenever coordinator state is replayed.

The optional `id` lets dispatcher and worker workflows share one coordinator branch; every workflow using the same ID must use the same schema. Without an ID, the workflow identity selects the branch. `auto-claim` defaults to `true`; set it to `false` on a dispatcher that submits Work but should not automatically claim it.

The coordinator uses a dedicated branch derived from its configured ID or workflow identity. That branch contains one authoritative file, `dispatch-work-coordinator.jsonl`, with immutable `Work`, `Claim`, `ClaimCancellation`, `Completion`, and `WorkCancellation` transactions. Each operation fetches the current branch and derives its projection through deterministic replay; a runner's cached projection is never authoritative.

## MCP tools

The coordinator exposes these tools to the agent:

| Tool | Purpose |
| --- | --- |
| `dispatch_work_submit(work)` | Add Work idempotently using its canonical payload identity. |
| `dispatch_work_get(work_id)` | Read one item's projected state. |
| `dispatch_work_list(filter?)` | List projected Work, optionally filtered by state. |
| `dispatch_work_status()` | Read coordinator-level counts and states. |
| `dispatch_work_claim(work_id)` | Claim available Work using trusted workflow-run provenance. |
| `dispatch_work_claim_next(filter?)` | Claim the first available Work in deterministic order. |
| `dispatch_work_cancel(work_id)` | Cancel Work that has not completed. |

Projected Work states are `available`, `claimed`, `completed`, and `cancelled`. Multiple Claims may be recorded, but replay selects one effective Claim. Only that Claim may complete the Work.

## Completion and side effects

Workers receive the assigned Work and Claim through trusted `aw_context`. The worker does not provide Claim identity when finishing; it emits the separate safe output `dispatch_claim_finish(outcome?)`. Safe outputs fetch and replay the coordinator branch, persist a Completion for the currently effective Claim, and confirm that Completion before applying other external effects.

Execution is at least once: concurrent workers can pass activation before a later Claim becomes effective. Only the effective Claim can authorize Completion and external side effects. A losing or unverified Claim fails closed, and staged or threat-detection-blocked runs do not persist coordinator mutations.

## Maintenance

When a workflow configures the coordinator, generated Agentic Maintenance includes one `dispatch_work_coordinator_compaction` job. On its maintenance schedule, the job checks unresolved effective Claims against their owning workflow runs and cancels Claims only when the run is confirmed terminal. It then compacts the coordinator log by removing redundant duplicate transactions and verifying that replay produces the same projection before updating the branch.

Compaction rewrites the same canonical file and uses bounded optimistic-concurrency retries. A conflict triggers a fresh fetch, replay, and compaction attempt; stale compacted output is never merged into a newer log. Disabling maintenance in `.github/workflows/aw.json` also disables orphan-Claim recovery and compaction.
