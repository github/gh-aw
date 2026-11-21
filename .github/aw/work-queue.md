---
description: Git-backed work queue agent instructions.
---

# Work Queue

`tools.work-queue` schedules Work DAGs and Claim-scoped effects (protocol v4).
`work-queue.jsonl` on branch `work-queue` is authoritative; there is no storage selector.
Issues/PRs are dependencies or mirrors, never storage.
For issue-backed WorkQueueOps and the minimal deployment: [deployment](../../docs/src/content/docs/patterns/workqueue-ops.md).

## Select the role

- **Observer:** read and explain only; never submit, dispatch or finish Work.
- **Producer/dispatcher:** submit bounded, secret-free plans; request pool work
  with `work_queue_dispatch_next`; no arbitrary resource writes.
- **Worker:** process only the Claims of its `work_queue_assignment`
  (`{"version":4,"dispatch_id":…}`); never override that input or caller context.

Absent branches are empty; reject malformed/unsupported logs.
A v3 head is converted by the next dispatcher/producer/maintenance run;
workers and observers fail with `unsupported_protocol` until then.

## Configure deployment

```yaml
tools:
  work-queue:
    require-assignment: true
    worker: true
```

- Dispatchers: `tools.work-queue: true`, worker allowlist in
  `safe-outputs.dispatch-workflow.workflows`, bounded `dispatch-workflow.max`.
- `aw.json` `work_queue`: `concurrency` (default 16), `pending_limit` (4,096),
  `retry.max_attempts` (3), `pools.<name>` (`concurrency`, `per_account_limit`,
  `retry`), `issues`. Unknown keys fail compilation.
- The compiler emits pool limits as `GH_AW_WORK_QUEUE_POLICY` on every run.
  Config edits apply on the next run after recompilation; nothing is installed
  in the ledger and no drain is needed.
- Every pool routes to every `worker: true` workflow; profile = workflow ID.
- Standalone seeding is unsupported: the first submission creates the queue.
- Never expose queue-write credentials to agents or snapshot MCP.
  Repository access rules are GitHub's responsibility: branch write access is
  the only queue authority.

## Mirror Work with Issues

`aw.json` `work_queue.issues: true` (or `{label, title_prefix}`) mirrors Work:
create/adopt one labelled Issue per pending Work (immutable `IssueLink`),
comment when a Claim binds, comment and close on completion/cancellation, close
labelled Issues whose Work is absent. Projection is best-effort (≤50 Work/run),
never fails a job, and is idempotent via comment markers.
Closing a backing Issue cancels its `available` Work (`related_issue_closed`)
through generated `.github/workflows/agentic-workqueue.yml`.
See [backing Issue reference](../../docs/src/content/docs/reference/work-queue.md#backing-issues).

## Plan and dispatch

- Read immutable snapshots; predictions are hints, never grants.
- Request work with `work_queue_dispatch_next` (`pool`, `max_claims`,
  `max_dispatches`). Never choose Work, targets, or call `dispatch_workflow`.
- The scheduler takes eligible Work by priority (1 first), then FIFO, while
  non-closed pool Dispatches < `concurrency` and the fairness key is under
  `per_account_limit`. One Claim per Dispatch.
- Trusted controls launch each Dispatch once. Success → `started`; failure →
  `launch_failed` release, Work back in the pool (step still succeeds).

ESLint/daily-report/conformance agents only explain staged plans.
Empty dispatcher intents fail the controls step; only `blocked` receipts fail it otherwise.
`status: "staged"` grants nothing.

## Execute Claims

- Scope every safe output and finish intent to one Claim `claim_handle`;
  only a one-Claim assignment may omit it. Reject foreign or conflicting handles.
- Finish each Claim with `work_queue_claim_finish`, `outcome: "completed"` or
  `"failed"`. A missing outcome counts as failed.
- Effects apply only for `completed` Claims whose Dispatch is bound to this run
  and whose targets match the profile `effect_scope`; else `E004`.
- Trusted steps apply effects, then publish Completion (or `worker_failed`).
  Effects are at-least-once: a crash between them reruns the Work. Write
  idempotent workers (check before acting).
- Beyond built-ins, declare `safe-outputs.claim-adapters`
  ([reference](safe-outputs-runtime.md)).

## Dependencies and recovery

- Work edges wait for predecessor Completion; Issue edges for `completed`/`closed`,
  PR edges for `merged` observations refreshed by the dispatcher.
- No clocks: no expiry, backoff or deadlines. Recovery uses run state only.
- Worker conclusion releases its open Claims (`worker_terminated`).
  Maintenance releases Dispatches whose bound run completed (`run_terminal`) or
  whose run is missing (`run_missing`), and checkpoints logs over 512 lines.
- Each cancelled Claim adds one attempt; at `max_attempts` the Work is
  cancelled `attempts_exhausted`. Do not raise limits to hide failing Work.

## Load details only when needed

- Commands, diagnostics, receipts:
  [queue reference](../../docs/src/content/docs/reference/work-queue.md).
  `gh aw work-queue --repo OWNER/REPO stats --json`; `inspect --work-id ID`;
  `release --dispatch-id ID`; `cancel-work --work-id ID`.
- Destructive: `gh aw work-queue clear [--dry-run]` releases all open Dispatches
  and cancels available Work; interactive confirmation, refuses CI.
- Daily-report rotation/Claims:
  [portfolio example](../../docs/src/content/docs/gallery/daily-report-portfolio.md)
  and [worker instructions](../workflows/shared/daily-report-worker.md).
- Ledger contract: [specification](../../docs/src/content/docs/specs/work-queue-specification.md).
- [Formal model](../../specs/work-queue/README.md).
