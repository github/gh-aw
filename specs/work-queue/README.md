---
title: Work Queue protocol model
description: Bounded TLA+ model of work queue protocol v4 and how to check it with TLC.
---

# Work Queue protocol model

[`WorkQueueV4.tla`](WorkQueueV4.tla) models work queue protocol v4: the
time-free protocol replayed by `actions/setup/js/work_queue_replay.cjs` and
`pkg/workqueue`. [`WorkQueueV4.cfg`](WorkQueueV4.cfg) is its only TLC
configuration. Earlier models of protocol v3 (installed Policy, epochs,
delivery barriers, expiry) were removed with that protocol; they remain in Git
history.

The design rationale is recorded in
[ADR-64955](../../docs/adr/64955-git-backed-work-queue-coordination.md).

## What the model covers

Each action is one ledger commit. Commits are serialized by compare-and-swap
on `refs/heads/work-queue`, so the interleaving of actions is the log order.
No action reads a clock: there are no deadlines, grace periods, expiry,
backoff or observation ages, and the commit `at` field is not modelled.

| Variable | Values |
|---|---|
| `work[w]` | `none → available → claimed → completed`, or back to `available`, or `cancelled`; `attempts`, FIFO `pos`, current `claim` |
| `attempt[d]` | `unused → reserved → started → bound → completed \| cancelled` (`reserved`, `started` and `bound` mean the Claim is open and its Dispatch is not closed; `completed` and `cancelled` mean the Claim is settled and the Dispatch closed); the Work; the bound run attempt; the run attempt that completed it |
| `run[d]` | `none → queued → activation → agent → finishing → completed`; run attempt (re-runs keep the run id) |
| `dispatcher[d]` | `none → launching → running → completed`: the dispatcher run attempt that reserved the Dispatch (`reserved_by`) has its launch still ahead, has decided it (recorded, released or commit lost), or has completed (or is gone); tracked while the Dispatch is `reserved`, since nothing reads it afterwards |

Replay accepts exactly one Claim per Dispatch, so a Claim and the Dispatch
that launched it form one attempt identified by its Dispatch slot. A worker
run is started by the `workflow_dispatch` of one Dispatch and carries only
that `dispatch_id`, so a run is also identified by its Dispatch slot. The run
stays a separate variable: GitHub can queue, re-run or complete it
independently of what the ledger recorded. So does the dispatcher run attempt
recorded as `reserved_by` (from the actor of the `dispatch_next` commit). A
re-run of the dispatcher is a new attempt with new request and dispatch IDs,
so only the reserving attempt can launch a Dispatch. One attempt can reserve
several Dispatches; one `dispatcher[d]` per slot lets them end independently,
which includes ending together.

| Action | Protocol step |
|---|---|
| `Submit` | `submit` commit with a `Work` operation |
| `Plan` | `dispatch_next`: one `Claim` in a new `reserved` Dispatch for the FIFO head while non-closed pool Dispatches are below `Concurrency`; the reserving dispatcher attempt is `launching` |
| `LaunchOK` | the reserving dispatcher attempt, still in progress, made a successful `workflow_dispatch` and read back and verified the created run; `launch` commit `[Dispatch{started, run_id, revision}]` naming that run, which may be lost (Dispatch stays `reserved`) |
| `LaunchFail` | the reserving attempt's `workflow_dispatch` failed or returned no verifiable run; `launch` commit `[Release{launch_failed}]` (+ attempts rule) or lost; an ambiguous failure may still queue a run, which can never bind |
| `DispatcherEnd` | the reserving dispatcher attempt completes, is cancelled or crashes, before or after its launch, while its Dispatch is `reserved` |
| `RunStart`, `RunComplete` | GitHub run environment; a run can stop at any point |
| `Rerun` | GitHub re-run: same run id, next run attempt, up to `MaxRunAttempts` |
| `Bind` | activation job `bind` commit `[Bound]` only for the run the launch recorded (`started`) or a later attempt of the bound run; a run without a recorded launch (`reserved`: `LAUNCH_UNVERIFIED` at once, no waiting) or open Claim gets no Claims and the agent is skipped |
| `Complete`, `Fail` | finish step `[Completion]`, `[Completion, Work]` admitting one previously absent continuation successor, or `[ClaimCancellation{worker_failed}]` (+ attempts rule); rejected unless the Claim is open and the Dispatch is bound to this run attempt |
| `Terminate` | conclusion job `[Release{worker_terminated}]` when the Dispatch still has an open Claim and was launched as or bound to this run; never for a `reserved` Dispatch |
| `MaintainRelease` | maintenance `[Release{run_terminal}]` (`run_missing` if GitHub no longer has it) for a `started` or `bound` Dispatch whose recorded run completed, or for a `reserved` Dispatch whose reserving dispatcher attempt completed |

The attempts rule: when a Claim is cancelled, the publisher appends
`WorkCancellation{attempts_exhausted}` in the same commit if
`attempts + 1 >= MaxAttempts`.

Maintenance judges a `reserved` Dispatch only by its reserving dispatcher
attempt and never lists or correlates worker runs. While that attempt is in
progress it may still record the launch, so the Dispatch stays open. Once it
completed, no launch can be recorded any more: a run it POSTed meets a closed
Dispatch at bind and gets no Claim. If a worker run reaches activation before
the launch commit lands, it fails at once and its conclusion job releases the
then `started` Dispatch, consuming one attempt. Those are the ledger steps of
`LaunchOK` followed by a run that stops before `Bind`, so the model keeps the
launch one step.

## Properties

| Property | Kind | Statement |
|---|---|---|
| `TypeOK` | invariant | variables stay in their domains |
| `OneOpenClaimPerWork` | invariant | at most one open Claim per Work; `claimed` Work is owned by exactly the open Claim it records |
| `BoundRunOwnsCompletion` | invariant | a completed Claim was completed by the run attempt its Dispatch is bound to; completed Work has exactly one completed Claim |
| `CapacityBound` | invariant | non-closed Dispatches never exceed `Concurrency` |
| `AttemptsBound` | invariant | `attempts <= MaxAttempts`; `available`/`claimed` Work has attempts left |
| `AttemptsMonotone` | action property | `attempts` never decreases |
| `NoReopenCompleted` | action property | `completed` and `cancelled` Work never change state |
| `ReservedReleaseAwaitsDispatcher` | action property | a `reserved` Dispatch is released only by its reserving dispatcher attempt in its launch step (`launch_failed`) or after that attempt completed; never by maintenance or a worker run while the attempt is in progress |
| `EventualSettlement` | liveness | every submitted Work is eventually `completed` or `cancelled` |

`EventualSettlement` assumes weak fairness of `Plan` (the scheduled
dispatcher), `MaintainRelease` (scheduled maintenance), GitHub run progress
(`RunStart`, `RunComplete`) and `DispatcherEnd` (GitHub ends every run
attempt, at the latest at its job timeout). Launch, worker steps and re-runs
are not fair: a dispatcher or worker may crash at any point, and maintenance
recovers its Claim. Every cancelled Claim consumes one attempt, so settlement
needs no clock.

## Bounds

`WorkQueueV4.cfg` checks one pool with 2 Work, `MaxAttempts = 2`,
`Concurrency = 2` (the module accepts 1..2), `MaxRunAttempts = 2` and
`DispatchSlots = 4`. Each Work needs at most `MaxAttempts` Dispatches, so the
module assumes `DispatchSlots >= Cardinality(Works) * MaxAttempts`. Settled
queues stop, so the configuration sets `CHECK_DEADLOCK FALSE`.

## Run the model

Install the pinned TLC jar (TLA+ Tools v1.7.4, checksum-verified) and run TLC
from the repository root:

```bash
bash .github/scripts/install_tla_runtime.sh "$PWD/.queue-validation-cache/tla-tools" --jar-only
java -XX:+UseParallelGC -Xmx4g -cp .queue-validation-cache/tla-tools/tla2tools.jar tlc2.TLC \
  -workers 8 -fp 0 -config specs/work-queue/WorkQueueV4.cfg \
  -metadir .queue-validation-cache/tlc-WorkQueueV4 specs/work-queue/WorkQueueV4.tla
```

A pass ends with `Model checking completed. No error has been found.`

## Daily evidence collection

[`daily-work-queue-formal-verification.md`](../../.github/workflows/daily-work-queue-formal-verification.md)
runs `WorkQueueV4` daily (and on manual dispatch) through
`.github/scripts/work-queue-formal-check.cjs`. The collector records the model
and configuration snapshots with their SHA-256 hashes, the TLC command and
checksum, the raw log, the verdict (`passed`, `violation`, `timed_out`,
`tool_error`, `setup_incomplete`) and state counts in the
`work-queue-formal-WorkQueueV4-<run-id>-<attempt>` artifact. A read-only agent
writes a `work-queue-formal-handoff-<run-id>-<attempt>` artifact summarizing
that evidence; it does not rerun TLC.

## Limits

The model is a bounded check, not an unbounded proof, and it does not prove
that the JavaScript or Go replay refines it; the shared fixtures under
`fixtures/` cover JS/Go replay parity. It abstracts DAG dependencies,
observations, priorities other than FIFO order, per-account limits, the v3
converter, checkpoints and the Issue projection. Effects are applied before the
`finish` commit, so a run that dies in between can repeat effects on the next
Claim (at-least-once); the model tracks Claim ownership, not external effects.
