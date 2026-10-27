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

Dispatchers require `tools.work-queue: true`, worker allowlist, bounded
`dispatch-workflow.max`. Approved AW routes/credentials bind Claims to immutable
native runs; ignore moved `target-ref`.

`aw.json` `work_queue`: concurrency, pending limits, retries, pools, fairness,
Issues. Defaults: weighted-priority, concurrency 16,
pending 4,096, singleton assignments, three attempts, 30-second backoff.
Submission initializes Policy/Work. Trusted submit/dispatch install pools/profiles;
workers, observers, previews and accepted-request recovery never install.
New workers require default-revision
AW source/contract verification and active registration.
`PoolInstall.config_ref` records provenance, not authority.
Preserve installed profiles/limits/epoch.
Standalone seeding is unsupported; AW/Actions authorize submissions.
Economic/installed-authority changes require quiescence and
`policy --from-config --epoch EPOCH`; config edits never rewrite active state.
Compatible deployments need no drain. Never expose queue-write credentials to
agents/snapshot MCP. Repository access rules are GitHub's responsibility.
See [deployment](../../docs/src/content/docs/patterns/workqueue-ops.md#deploy-a-native-queue).

## Mirror admitted Work with Issues

`aw.json` `work_queue.issues: true` mirrors admitted Work with purple `work` /
`work:<status>` labels. `label` customizes both prefixes; `title_prefix` defaults
to `[work queue]`. Titles/summaries show 12-character Work IDs; bodies use full IDs.
No per-workflow overrides. Only protected AW hooks project admissions/original Claims.
Immutable `backing_issue` binds one Work per Issue. Agents never write
mirrors; human edits never establish Result. Pre-existing Issues require exact
installed `backing_issues` grants. Only trusted admission-time `completion_policy`
permits closure, never payload. Keep uncertain writes pending; never recreate or
globally repair. Maintenance: exact-run sync; recovery: `work-queue sync-issues --request-id ID`.
Upgrade closed-schema readers. See
[backing Issue reference](../../docs/src/content/docs/reference/work-queue.md#backing-issues).
Mirroring generates `.github/workflows/agentic-workqueue.yml` for Issue lifecycle
events; disabling removes it.

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

- Operator commands, TUI and diagnostic artifacts:
  [queue reference](../../docs/src/content/docs/reference/work-queue.md).
  Cancellation terminates Work, not workers; reconcile separately.
  `gh aw work-queue --repo OWNER/REPO stats --json`. `dispatch-next` grants
  reservations, never launches; use the authorized dispatcher.
- Destructive: `gh aw work-queue clear --reason CODE [--dry-run | --hard]`
  requires repo maintain/admin, forbids CI, and requires interactive confirmation
  of the exact snapshot preview; no bypass. `--hard` replaces the ledger,
  forgetting Work/Claim identities, retry history and fairness debt. Admission/grants
  must already be paused, without open Claims/pending deliveries.
  See [hard reset](../../docs/src/content/docs/reference/work-queue.md#hard-reset).
- Daily report rotation and Claims:
  [portfolio example](../../docs/src/content/docs/gallery/daily-report-portfolio.md)
  and [shared worker instructions](../workflows/shared/daily-report-worker.md).
- Contracts: [specification](../../docs/src/content/docs/specs/work-queue-specification.md#91-implementation-coverage-and-remaining-requirements).
- Models/fixtures/bounded verification: [formal reference](../../specs/work-queue/README.md).

Tests prove neither Result nor live deployment.
