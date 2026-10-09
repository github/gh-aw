---
description: Agent instructions for Git-backed work queue producers, dispatchers, workers and observers.
---

# Work Queue

Choose the queue pattern: Git storage for fair scheduling and Claim authority, or
[WorkQueueOps](../../docs/src/content/docs/patterns/workqueue-ops.md) for
issue-backed WorkQueueOps: lists, sub-issues, Discussions or cache backlogs.

Use `tools.work-queue` for fair scheduling, immutable Work DAGs and Claim-scoped
effects. The causal `work-queue.jsonl` log is authoritative. Weights distribute
Claim opportunities, not CPU or successful completions. Protocol v3 always uses
Git; Issues/PRs are dependencies, not storage. Never set `storage`. WorkQueueOps
is a separate pattern.

## Select the role

- **Observer:** read/explain only; use ordinary configured report or `noop`
  authorization. Do not submit, dispatch or finish Work.
- **Producer/dispatcher:** stage entitled task plans and bounded pool requests.
  Queue-control authority does not permit arbitrary resource-writing outputs.
- **Worker:** require a compiler-supplied version-3 `claims` array. Never
  construct/override reserved `work_queue_assignment` or caller context.

Trust the compiler/runtime role, never snapshot metadata or assignment alone;
don't downgrade declared workers. Activation authenticates run/workflow/revision,
and reruns cannot inherit attempt 1 authority. Report absent queues as
uninitialized; reject empty, malformed or unsupported logs.

## Configure deployment

Declare a worker with:

```yaml
tools:
  work-queue:
    require-assignment: true
    worker: true
```

Dispatchers need `tools.work-queue: true`, an explicit
`safe-outputs.dispatch-workflow.workflows` allowlist and a bounded `max`.
The allowlist is compiler approval, not authority: installed Policy binds each
profile's exact workflow path, immutable SHA, authenticated principal, trust
domain and effect scope. Queue dispatch ignores moving `target-ref`.

Require administrator-installed Policy and protected queue-branch writers;
frontmatter provisions neither, and administrator status grants no producer
entitlement. Never give agents or snapshot MCP queue-write credentials. Read the
[deployment guide](../../docs/src/content/docs/guides/deploy-work-queue.md) only
for installation; writer-restriction automation remains deferred.

## Mirror admitted Work with Issues

`tools.work-queue.issues: true` mirrors admitted Work with the `work` label.
An object may set `label` and a pre-provisioned native organization
`status-field`. Require installed projector authority: only protected hooks
project their own admissions/original Claims. Immutable `backing_issue` binds
one Work per Issue. Agents never write mirrors; human edits never establish
Result. Pre-existing Issues need exact installed `backing_issues` grants.
Only trusted admission-time `completion_policy` permits closure, never payload.
Uncertain writes stay pending, never recreated or globally repaired.
Upgrade all closed-schema readers first. See the
[backing Issue reference](../../docs/src/content/docs/reference/work-queue.md#backing-issues).

## Plan and dispatch

- Use `work_queue_read` / `work_queue_explain` for the immutable activation
  snapshot. Predictions may be stale; sorting cannot override Policy.
- Stage bounded, secret-free task/graph plans with `work_queue_submit`, within
  installed producer pools, priorities and accounting keys.
- Request a pool prefix using `work_queue_dispatch_next` with `pool`,
  `max_claims` and `max_dispatches`. Never select winning Work or a target, or
  use legacy `dispatch_workflow` with `work_queue: {work_id: ...}`.
- Let trusted processing refresh the ledger, enforce Policy/DAG/scope and commit
  a compatible fair prefix atomically. A CAS loser discards tentative choices
  and charges before recomputing. Ordinary non-queue dispatch is separate.

## Execute original Claims

- Process only the assignment's original Claims and stored plans. Scope every
  safe output and finish intent to one original Claim.
- Omit `claim_handle` only for an originally single-Claim assignment.
  Multi-Claim assignments require it on every message even after other members
  close. Reject malformed, null, foreign or conflicting selectors.
- Finish each member with `work_queue_claim_finish`, using
  `outcome: "completed"` or `"cancelled"`. Missing finish authorizes no effects;
  valid siblings settle independently.
- Default to one Claim. Enable batching only for compatible Work with substantial
  reusable setup; never treat the batch's native conclusion as every Work's Result.
- Treat finish as an intent: trusted processing publishes Completion before
  scoped effects and Result only after independently verified delivery.

Use `<mcp-clis>` advertised MCP subcommands with one JSON argument (not
structured-tool `command`/`description`). Dispatcher: `uninitialized` plus
null `snapshot_sha` means absent; stop with `noop`, not submit/dispatch.
Existing policyless ledgers are deployment failures; workers require Policy.
`status: "staged"` is no grant or launch.

## Dependencies and recovery

Require fresh completed-state evidence for Issue predecessors and actual merge
evidence for PR predecessors; these nodes consume no Claims. Work successors
wait for their own predecessor Results.

Never infer nonlaunch/termination from lost responses, dispatcher cancellation
or elapsed deadlines. Retain reservations until exact evidence exists. Do not
rerun effects after Completion with uncertain delivery; bounded verification
yields Result or DeliveryFailure. Use pause/drain for incompatible revisions;
quiesce before Policy changes. Reject old protocols, scalar assignments and
automatic upgrades; preserve old evidence before explicit redeployment.

## Load details only when needed

- Operator commands, TUI and diagnostic artifacts:
  [queue reference](../../docs/src/content/docs/reference/work-queue.md).
  Cancellation is terminal for Work, not a worker stop; reconcile separately.
  Build with `go build -o ./gh-aw ./cmd/gh-aw`; use
  `./gh-aw work-queue --repo OWNER/REPO stats --json`. `dispatch-next` grants
  reservations, never launches; use the authorized dispatcher.
- Daily report rotation, dedicated Policy and Claim examples:
  [portfolio walkthrough](../../docs/src/content/docs/patterns/daily-report-portfolio.md)
  and [shared worker instructions](../workflows/shared/daily-report-worker.md).
- Normative contracts and unfinished implementation boundaries:
  [specification](../../docs/src/content/docs/specs/work-queue-specification.md#91-implementation-coverage-and-remaining-requirements).
- Executable models, fixtures and bounded verification:
  [formal reference](../../specs/work-queue/README.md).

Do not treat audit exports, agent assertions or functional tests as proof of
verified Result or deployment-security completion.
