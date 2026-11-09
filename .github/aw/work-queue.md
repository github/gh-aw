---
description: Git-backed work queue agent instructions.
---

# Work Queue

`tools.work-queue` schedules DAGs and Claim-scoped effects.
`work-queue.jsonl` is authoritative; V3 omits storage selectors. Issues/PRs are dependencies.
For issue-backed WorkQueueOps: [deployment](../../docs/src/content/docs/patterns/workqueue-ops.md).

## Select the role

- **Observer:** explain only; never submit, dispatch or finish Work.
- **Producer/dispatcher:** submit trusted plans/request bounded pool work;
  no arbitrary resource writes.
- **Worker:** require compiler-supplied version-3 `claims`; never override
  reserved `work_queue_assignment` or caller context.

Trust roles, not snapshots: AW authenticates run/workflow/revision;
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

`aw.json` `work_queue`: concurrency, pending limits, retries, pools, fairness,
Issues. Defaults: weighted-priority, concurrency 16, pending 4,096,
singleton assignments, three attempts, 30-second backoff.
Submission initializes Policy/Work. Only trusted submit/dispatch install pools/profiles;
workers, observers, previews and accepted-request recovery never install.
New workers require default-revision AW source/contract verification and active registration.
`PoolInstall.config_ref` records provenance, not authority.
Preserve installed profiles/limits/epoch.
Standalone seeding is unsupported; AW/Actions authorize submissions.
Quiesce economic/authority changes; apply: `policy --from-config --epoch EPOCH`.
Config edits never rewrite active state.
Never expose queue-write credentials to agents or snapshot MCP. Repository access rules are GitHub's responsibility.
See [deployment](../../docs/src/content/docs/patterns/workqueue-ops.md#deploy-a-native-queue).

## Mirror admitted Work with Issues

`aw.json` `work_queue.issues: true` mirrors admitted Work: purple `work` /
`work:<status>` labels. `label` customizes both prefixes. Defaults: `title_prefix` =
`[work queue]`, `orphaned_after_days` = 30. Titles/summaries: 12-character Work IDs;
bodies: full IDs.
Only protected AW hooks project admissions/Claims. Immutable `backing_issue`
binds one Work/Issue. Agents never write mirrors; human edits never establish
Results. Pre-existing Issues require exact installed `backing_issues` grants.
Only admission-time `completion_policy` closes verified non-PR results.
Maintenance reconciles at most 256 same-repository mirrors/run from the checked
ledger; flag aged orphan Issues for review without closing. Keep uncertain
writes pending; never recreate mirrors or repair untracked Issues. Recovery:
`work-queue sync-issues --request-id ID`.
Upgrade closed-schema readers. See
[backing Issue reference](../../docs/src/content/docs/reference/work-queue.md#backing-issues).
Mirroring generates `.github/workflows/agentic-workqueue.yml` for Issue lifecycle
events; disabling removes it.

## Plan and dispatch

- Read immutable snapshots; stale predictions cannot override Policy.
  Submit bounded, secret-free plans within scheduling limits.
- Request prefixes: `work_queue_dispatch_next` (`pool`, `max_claims`,
  `max_dispatches`). Never choose winners/targets or use scalar dispatch.
- Trusted processing refreshes the ledger and commits a compatible fair prefix.
  CAS losers discard tentative choices/charges and recompute.

ESLint/daily-report/conformance agents only explain staged plans.
Trusted controls validate batches; empty dispatcher intents fail.
Unassigned dispatchers cannot emit `noop` or `report_incomplete`.

## Execute original Claims

- Process only assigned original Claims/stored plans. Scope every safe output
  and finish intent to one original Claim.
- Omit `claim_handle` only for an originally single-Claim assignment.
  Multi-Claim assignments require it on every message even after siblings
  close. Reject malformed, null, foreign or conflicting selectors.
- Finish members with `work_queue_claim_finish`, using
  `outcome: "completed"` or `"cancelled"`. Missing finish authorizes no effects;
  valid siblings settle independently.
- Retain Work: `outcome: "completed", keep_open: true` (default: false;
  invalid with cancellation). Preserve both on retries. Verified delivery permits
  another fair Claim, never successor release/backing Issue closure.
  Pending/failed delivery blocks Claims; never rerun uncertain effects.
- Default: one Claim; batch compatible Work with reusable setup. Native
  batch conclusions are not per-Work Results.
- Finish is an intent: trusted processing publishes Completion before
  scoped effects and Result only after independently verified delivery.
- Beyond built-ins, declare `safe-outputs.claim-adapters`
  ([reference](safe-outputs-runtime.md)); each adapter binds one effect-type to
  independently verified results, never job success alone.

`<mcp-clis>` takes one JSON argument. `uninitialized` plus null SHA is empty.
Policyless ledgers fail; workers require Policy; `status: "staged"` is no grant.

## Dependencies and recovery

Require fresh Issue completed-state/PR merge evidence. Predecessors consume no
Claims; successors await predecessor Results.

Lost responses, dispatcher cancellation/deadlines never prove nonlaunch/termination.
Retain reservations until exact evidence. Never rerun uncertain effects after
Completion; bounded verification yields Result/DeliveryFailure. Compatible
deployments preserve obligations, debt, Results, frozen assignments and explicit
`execution_ref` pins. Unavailable workers pause affected Work only. Scheduled
maintenance terminally cancels unclaimed, unpinned Work with admission/approved-active-worker
contract mismatch: `WorkCancellation`, operation `version: 1`, reason
`worker_contract_obsolete`. Current-time expiration excludes elapsed Work;
publication rechecks eligibility. Preserve delivery-barrier Work. Version-3
history lacking operation version retains reason-only semantics, including
compatible/pinned cancellations.
Quiesce economic Policy changes. Reject unsupported protocols/scalar assignments.
Retries/checkpoints preserve submission identity; never delete queues to retry admission.

## Load details only when needed

- Commands, TUI, diagnostics:
  [queue reference](../../docs/src/content/docs/reference/work-queue.md).
  Cancellation terminates Work, not workers; reconcile separately.
  `gh aw work-queue --repo OWNER/REPO stats --json`. `dispatch-next` grants
  reservations, never launches; use authorized dispatchers.
- Destructive: `gh aw work-queue clear --reason CODE [--dry-run | --hard]`
  requires repo maintain/admin, forbids CI, requires interactive confirmation
  of exact snapshot previews; no bypass. `--hard` replaces the ledger,
  forgetting Work/Claim identities, retries and fairness debt. Pause admission/grants
  first, without open Claims/pending deliveries.
  See [hard reset](../../docs/src/content/docs/reference/work-queue.md#hard-reset).
- Daily-report rotation/Claims:
  [portfolio example](../../docs/src/content/docs/gallery/daily-report-portfolio.md)
  and [worker instructions](../workflows/shared/daily-report-worker.md).
- Contracts: [specification](../../docs/src/content/docs/specs/work-queue-specification.md#91-implementation-coverage-and-remaining-requirements).
- Models/fixtures/bounded verification: [formal reference](../../specs/work-queue/README.md).
