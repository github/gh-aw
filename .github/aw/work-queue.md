---
description: Git-backed work queue agent instructions.
---

# Work Queue

`tools.work-queue` schedules DAGs and Claim-scoped effects.
`work-queue.jsonl` is authoritative; V3 omits storage selectors. Issues/PRs are dependencies.
For issue-backed WorkQueueOps: [WorkQueueOps](../../docs/src/content/docs/patterns/workqueue-ops.md).

## Select the role

- **Observer:** explain only; never submit, dispatch or finish Work.
- **Producer/dispatcher:** submit trusted, bounded pool plans; queue authority
  grants no arbitrary writes.
- **Worker:** require compiler-supplied version-3 `claims`; never override
  reserved `work_queue_assignment` or caller context.

AW authenticates run/workflow/revision;
reruns cannot inherit attempt 1. Absent branches are empty;
reject malformed/unsupported logs.

## Configure deployment

```yaml
tools:
  work-queue:
    require-assignment: true
    worker: true
```

Dispatchers require `tools.work-queue: true`, a worker allowlist, bounded
`dispatch-workflow.max`. Approved routes/credentials bind Claims to immutable
native runs; ignore moved `target-ref`.

`aw.json` `work_queue` configures concurrency, pending limits, retries, pools,
fairness and Issues. Defaults: weighted-priority, concurrency 16,
pending 4,096, singleton assignments, three attempts, 30-second backoff.
Submission initializes Policy/Work. Only trusted submit/dispatch install
pools/profiles; other roles cannot. New workers require default-revision AW
source/contract checks and active registration.
`PoolInstall.config_ref` records provenance, not authority.
Preserve installed profiles/limits/epoch.
Standalone seeding is unsupported; AW/Actions authorize submissions.
Quiesce economic/authority changes; apply with
`policy --from-config --epoch EPOCH`. Config edits never rewrite active state.
Never expose queue-write credentials to agents or snapshot MCP. Repository access rules are GitHub's responsibility.
See [deployment](../../docs/src/content/docs/patterns/workqueue-ops.md#deploy-a-native-queue).

## Mirror admitted Work with Issues

`aw.json` `work_queue.issues: true` mirrors admitted Work with purple `work` /
`work:<status>` labels. `label` customizes both prefixes; `title_prefix` defaults
to `[work queue]`; `orphaned_after_days` defaults to 30. Titles/summaries show
12-character Work IDs; bodies use full IDs.
Protected AW hooks alone project admissions/Claims. Immutable `backing_issue`
binds one Work per Issue. Agents never write mirrors; human edits never establish
Results. Pre-existing Issues require exact installed `backing_issues` grants.
Only admission-time `completion_policy` closes verified non-PR results.
Maintenance reconciles at most 256 same-repository mirrors/run from the checked
ledger and flags aged orphan Issues for review, leaving them open. Keep uncertain
writes pending; never recreate mirrors or repair untracked Issues. Recovery:
`work-queue sync-issues --request-id ID`.
Upgrade closed-schema readers. See
[backing Issue reference](../../docs/src/content/docs/reference/work-queue.md#backing-issues).
Mirroring generates `.github/workflows/agentic-workqueue.yml` for Issue lifecycle
events; disabling removes it.

## Plan and dispatch

- Read immutable snapshots; predictions cannot override Policy. Submit bounded,
  secret-free plans within limits.
- Request a pool prefix with `work_queue_dispatch_next` (`pool`, `max_claims`,
  `max_dispatches`). Never choose winners/targets or use scalar dispatch.
- Trusted processing refreshes the ledger and commits a fair compatible prefix.
  CAS losers discard tentative choices/charges and recompute.

ESLint/daily-report/conformance agents only explain staged plans.
Trusted controls validate batches; empty dispatcher intents fail.
Unassigned dispatchers cannot emit `noop` or `report_incomplete`.

## Execute Claims

- Process only assigned original Claims and stored plans. Scope each safe output
  and finish intent to one original Claim.
- Omit `claim_handle` only for an originally single-Claim assignment.
  Multi-Claim assignments require it on every message, even after others close.
  Reject malformed, null, foreign or conflicting selectors.
- Finish members with `work_queue_claim_finish`, using
  `outcome: "completed"` or `"cancelled"`. Missing finish authorizes no effects;
  valid siblings settle independently.
- Retain Work with `outcome: "completed", keep_open: true` (default: false;
  invalid with cancellation). Preserve both on retries. Verified delivery permits
  another fair Claim, not successor release/backing Issue closure.
  Pending/failed delivery blocks Claims; never rerun uncertain effects.
- Default to one Claim; batch compatible Work with reusable setup. Native
  batch conclusions are not per-Work Results.
- Treat finish as intent: trusted processing publishes Completion before scoped
  effects and Result only after independently verified delivery.
- Beyond built-in safe outputs, declare `safe-outputs.claim-adapters`
  ([reference](safe-outputs-runtime.md)); each adapter binds one effect-type to
  an independently verified result, never job success alone.

`<mcp-clis>` takes one JSON argument. `uninitialized` plus null SHA is empty.
Policyless ledgers fail; workers require Policy; `status: "staged"` grants nothing.

## Dependencies and recovery

Require fresh Issue completed-state/PR merge evidence. Predecessors consume no
Claims; successors wait for predecessor Results.

Lost responses, cancellation or deadlines never prove nonlaunch/termination.
Retain reservations until exact evidence; never rerun uncertain effects after
Completion. Bounded verification yields Result/DeliveryFailure. Compatible
deployments preserve obligations, debt, Results, frozen assignments and explicit
`execution_ref` pins. Unavailable workers pause affected Work only. Scheduled
maintenance terminally cancels unclaimed, unpinned Work when its admission
contract differs from the approved active worker contract
(`worker_contract_obsolete`). Preserve Work with delivery barriers.
Quiesce economic Policy changes; reject unsupported protocols/scalar assignments.
Retries/checkpoints preserve submission identity; never delete queues to retry admission.

## Details

- Operator commands, TUI, diagnostics:
  [queue reference](../../docs/src/content/docs/reference/work-queue.md).
  Cancellation terminates Work, not workers; reconcile separately.
  `gh aw work-queue --repo OWNER/REPO stats --json`. `dispatch-next` grants
  reservations, never launches; use authorized dispatcher.
- Destructive: `gh aw work-queue clear --reason CODE [--dry-run | --hard]`
  requires repo maintain/admin, forbids CI, and requires interactive confirmation
  of the exact snapshot preview; no bypass. `--hard` replaces the ledger,
  forgetting Work/Claim identities, retry history and fairness debt. Admission/grants
  must already be paused, without open Claims/pending deliveries.
  See [hard reset](../../docs/src/content/docs/reference/work-queue.md#hard-reset).
- Daily report rotation/Claims:
  [portfolio example](../../docs/src/content/docs/gallery/daily-report-portfolio.md)
  and [shared worker instructions](../workflows/shared/daily-report-worker.md).
- Contracts: [specification](../../docs/src/content/docs/specs/work-queue-specification.md#91-implementation-coverage-and-remaining-requirements).
- Models/fixtures: [formal reference](../../specs/work-queue/README.md).
