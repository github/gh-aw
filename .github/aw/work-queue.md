---
description: Agent instructions for Git-backed work queue producers, dispatchers, workers and observers.
---

# Work Queue

Choose Git-backed `tools.work-queue` for fair scheduling, immutable Work DAGs
and Claim-scoped effects; `work-queue.jsonl` is authoritative. Protocol v3 has
no storage selector; Issues/PRs are dependencies, not storage. Weights distribute
Claim opportunities, not CPU or successful completions. For issue-backed WorkQueueOps
alternatives, see [WorkQueueOps](../../docs/src/content/docs/patterns/workqueue-ops.md).

## Select the role

- **Observer:** read/explain only; never submit, dispatch or finish Work.
- **Producer/dispatcher:** submit entitled plans and request bounded pool work;
  queue authority grants no arbitrary resource-writing outputs.
- **Worker:** require compiler-supplied version-3 `claims`; never override
  reserved `work_queue_assignment` or caller context.

Trust compiler/runtime role, not snapshot metadata; don't downgrade workers.
Activation authenticates run/workflow/revision; reruns cannot inherit attempt 1.
An absent branch is uninitialized/empty. Its first producer submit atomically
creates it with compiled Policy and Work; reads and dispatch alone never do.
Reject malformed or unsupported existing logs.

## Configure deployment

Declare a worker with:

```yaml
tools:
  work-queue:
    require-assignment: true
    worker: true
```

Dispatchers need `tools.work-queue: true`, a worker allowlist and bounded
`dispatch-workflow.max`. Compiler-approved Policy binds each profile's workflow,
immutable SHA, principal, trust domain and effect scope; moving `target-ref` is
ignored.

Configure producer entitlements; Agentic Workflows authenticates participants
at its trusted publication boundary. First submit automatically installs compiled
Policy and Work without administrator seeding, separate participant enrollment,
branch protection or ruleset checks. Later Policy changes are admin-only. Admin
status grants no producer entitlement. Never expose queue-write credentials to
agents/snapshot MCP. Branch protections are optional operator hardening, not a
bootstrap or dispatch gate. Do not provision, inspect or remove them as part of
queue use. See [deployment](../../docs/src/content/docs/guides/deploy-work-queue.md).

## Mirror admitted Work with Issues

`tools.work-queue.issues: true` mirrors admitted Work with purple `work` and
`work: <status>` labels. An object may customize the tracking label and status
label prefix with `label`. Require installed projector authority: only protected hooks
project their own admissions/original Claims. Immutable `backing_issue` binds
one Work per Issue. Agents never write mirrors; human edits never establish
Result. Pre-existing Issues need exact installed `backing_issues` grants.
Only trusted admission-time `completion_policy` permits closure, never payload.
Uncertain writes stay pending, never recreated or globally repaired.
Upgrade all closed-schema readers first. See the
[backing Issue reference](../../docs/src/content/docs/reference/work-queue.md#backing-issues).

## Plan and dispatch

- Read the immutable snapshot; predictions may be stale and cannot override
  Policy. Submit bounded, secret-free plans within producer entitlements.
- Request a pool prefix with `work_queue_dispatch_next` (`pool`, `max_claims`,
  `max_dispatches`). Never choose winners/targets or use scalar dispatch.
- Trusted processing refreshes the ledger and commits a compatible fair prefix.
  CAS losers discard tentative choices/charges and recompute.

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

Use advertised `<mcp-clis>` subcommands with one JSON argument, not
structured-tool `command`/`description`. `uninitialized` plus null SHA is empty;
first `work_queue_submit` atomically installs compiled Policy and Work. Submit
before dispatch. Existing policyless ledgers are failures; workers require
Policy. `status: "staged"` is no grant or launch.

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
