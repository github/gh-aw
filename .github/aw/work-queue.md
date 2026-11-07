---
description: Git-backed work queue agent instructions.
---

# Work Queue

`tools.work-queue` schedules immutable DAGs and Claim-scoped effects.
`work-queue.jsonl` is authoritative; V3 omits storage selectors. Issues/PRs are dependencies.
For issue-backed WorkQueueOps: [WorkQueueOps](../../docs/src/content/docs/patterns/workqueue-ops.md).

## Select the role

- **Observer:** read/explain only; never submit, dispatch or finish Work.
- **Producer/dispatcher:** submit trusted plans/request bounded pool work;
  queue authority grants no arbitrary resource writes.
- **Worker:** require compiler-supplied version-3 `claims`; never override
  reserved `work_queue_assignment` or caller context.

Trust runtime roles, not snapshots: AW authenticates native run/workflow/revision;
reruns cannot inherit attempt 1. Absent branches are empty;
reject malformed/unsupported logs.

## Configure deployment

Declare workers:

```yaml
tools:
  work-queue:
    require-assignment: true
    worker: true
```

Dispatchers require `tools.work-queue: true`, worker allowlist and bounded
`dispatch-workflow.max`. AW routes/credentials bind Claims to immutable runs;
ignore moved `target-ref`.

`aw.json` `work_queue` configures concurrency, pending limits, retries, pools,
fairness and Issues. Defaults: weighted-priority, concurrency 16, pending 4,096,
singleton assignments, three attempts, 30-second backoff. Submission initializes
Policy/Work; only trusted submit/dispatch install pools/profiles. New workers require
default-revision AW source/contract verification and active registration.
`PoolInstall.config_ref` records provenance, not authority.
Preserve installed profiles/limits/epoch.
Standalone seeding is unsupported; AW/Actions authorize submissions.
Economic/installed-authority changes require quiescence and
`policy --from-config --epoch EPOCH`; config edits never rewrite active state.
Compatible deployments need no drain. Never expose queue-write credentials to
agents/snapshot MCP. Repository access rules are GitHub's responsibility.
See [deployment](../../docs/src/content/docs/patterns/workqueue-ops.md#deploy-a-native-queue).

Protected synchronization selects the exact immutable bridge approved in
`.github/aw/work-queue-contract-routes.json` for differently admitted available or
claimed Work. It verifies source, stamp, registration and reader digests before
publishing registration, routes and availability atomically. No pause, ordering or
manual settlement is needed. Pins and Claims stay frozen; unavailable routes pause
affected Work without fallback. Upgrade closed-schema readers before adopting
`Deployment.contract_routes`; observer reads never install routes. All available
defaults, runnable pins and unreleased assignments must load approved readers.
Incompatible immutable pins or assignments block publication; never rewrite or
replay effects.

## Mirror admitted Work with Issues

Set `aw.json work_queue.issues: true` to mirror trusted admissions and original
Claims to Issues. Immutable `backing_issue` binds one Work per Issue; human edits
never establish Results. Only admission-time `completion_policy` permits closure.
Keep uncertain writes pending; never recreate or globally repair. Use exact-run
sync or `work-queue sync-issues --request-id ID`; see the
[backing Issue reference](../../docs/src/content/docs/reference/work-queue.md#backing-issues).

## Plan and dispatch

- Read immutable snapshots; stale predictions cannot override Policy.
  Submit bounded, secret-free plans within scheduling limits.
- Request a pool prefix with `work_queue_dispatch_next` (`pool`, `max_claims`,
  `max_dispatches`). Never choose winners/targets or use scalar dispatch.
- Trusted processing refreshes the ledger and commits a compatible fair prefix.
  CAS losers discard tentative choices/charges and recompute.

ESLint/daily-report/conformance agents only explain staged plans.
Trusted controls validate batches; empty dispatcher intents fail.
Unassigned dispatchers cannot emit `noop` or `report_incomplete`.

## Execute original Claims

- Process only the assignment's original Claims and stored plans. Scope every
  safe output and finish intent to one original Claim.
- Omit `claim_handle` only for an originally single-Claim assignment.
  Multi-Claim assignments require it on every message even after others
  close. Reject malformed, null, foreign or conflicting selectors.
- Finish members with `work_queue_claim_finish`, using
  `outcome: "completed"` or `"cancelled"`. Missing finish authorizes no effects;
  valid siblings settle independently.
- Retain Work with `outcome: "completed", keep_open: true` (default: false;
  invalid with cancellation). Preserve both on retries. Verified delivery permits
  another fair Claim, not successor release/backing Issue closure.
  Pending/failed delivery blocks Claims; never rerun uncertain effects.
- Default to one Claim; batch compatible Work with reusable setup. Native
  batch conclusions are not per-Work Results.
- Treat finish as an intent: trusted processing publishes Completion before
  scoped effects and Result only after independently verified delivery.
- Beyond built-in safe outputs, declare `safe-outputs.claim-adapters`
  ([reference](safe-outputs-runtime.md)); each adapter binds one effect-type to
  an independently verified result, never job success alone.

`<mcp-clis>` takes one JSON argument. `uninitialized` plus null SHA is empty.
Submit before dispatch.
Policyless ledgers fail; workers require Policy; `status: "staged"` is no grant.

## Dependencies and recovery

Require fresh Issue completed-state/PR merge evidence. Predecessors consume no
Claims; successors wait for predecessor Results.

Lost responses, dispatcher cancellation/deadlines never prove nonlaunch/termination.
Retain reservations until exact evidence. Never rerun uncertain effects after
Completion; bounded verification yields Result/DeliveryFailure. Compatible
deployments preserve obligations, debt, Results, frozen assignments and explicit
`execution_ref` pins. Unavailable/incompatible workers pause affected Work only.
Quiesce economic Policy changes. Reject unsupported protocols/scalar assignments.
Retries/checkpoints preserve submission identity; never delete queues to retry admission.

## Load details only when needed

- Commands/TUI/diagnostics: [queue reference](../../docs/src/content/docs/reference/work-queue.md).
  Cancellation ends Work, not workers; reconcile. `stats --json`
  reads state; `dispatch-next` only grants reservations.
- Destructive reset: `gh aw work-queue clear --reason CODE [--dry-run | --hard]`
  requires repo maintain/admin, forbids CI and confirms the snapshot preview.
  `--hard` forgets Work/Claim identities, retry history and fairness debt. Pause
  admissions/grants and clear Claims/pending deliveries first. See
  [hard reset](../../docs/src/content/docs/reference/work-queue.md#hard-reset).
- Daily report rotation and Claims:
  [portfolio example](../../docs/src/content/docs/gallery/daily-report-portfolio.md)
  and [shared worker instructions](../workflows/shared/daily-report-worker.md).
- Contracts: [specification](../../docs/src/content/docs/specs/work-queue-specification.md#91-implementation-coverage-and-remaining-requirements).
- Models/fixtures/bounded verification: [formal reference](../../specs/work-queue/README.md).

Tests cannot verify live Results/deployments.
