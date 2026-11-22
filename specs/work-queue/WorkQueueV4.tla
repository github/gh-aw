---------------------------- MODULE WorkQueueV4 ----------------------------
(***************************************************************************)
(* Bounded model of work queue protocol v4 (time-free).                    *)
(*                                                                         *)
(* One pool, one worker profile. Every action is one ledger commit, so the  *)
(* serialized append-only log (compare-and-swap on refs/heads/work-queue)  *)
(* is the interleaving itself. The model mirrors the replay transitions of *)
(* actions/setup/js/work_queue_replay.cjs and pkg/workqueue:               *)
(*                                                                         *)
(*   Work      available -> claimed -> completed | available | cancelled  *)
(*             (submit, or a finish admitting a continuation successor)   *)
(*   Claim     open -> completed | cancelled                               *)
(*   Dispatch  reserved -> started -> bound -> closed                      *)
(*   Run       queued -> in_progress -> completed (rerun: next attempt)    *)
(*                                                                         *)
(* The scheduler emits one Claim per Dispatch, so a Claim is identified by *)
(* its Dispatch slot. A worker run is started by the workflow_dispatch of  *)
(* exactly one Dispatch and carries that dispatch_id in its input, so a    *)
(* run is also identified by its Dispatch slot; GitHub re-runs keep the    *)
(* run id and increment the run attempt. Nothing reads a clock: there are  *)
(* no deadlines, expiry, backoff or observation ages.                      *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Works,          \* Work identities (model values)
    NoWork,         \* "no Work" marker for unused Claim slots
    DispatchSlots,  \* number of dispatch_id values the dispatcher can mint
    Concurrency,    \* pool concurrency from the compiled policy env
    MaxAttempts,    \* pool max_attempts from the compiled policy env
    MaxRunAttempts  \* GitHub re-runs bound (run_attempt 1..MaxRunAttempts)

ASSUME /\ NoWork \notin Works
       /\ Concurrency \in 1..2
       /\ MaxAttempts \in Nat \ {0}
       /\ MaxRunAttempts \in Nat \ {0}
       \* Every Claim either completes its Work or consumes one attempt, so
       \* each Work needs at most MaxAttempts Dispatches.
       /\ DispatchSlots \in Nat
       /\ DispatchSlots >= Cardinality(Works) * MaxAttempts

D == 1..DispatchSlots
NoDispatch == 0

VARIABLES work, claim, dispatch, run
vars == <<work, claim, dispatch, run>>

WorkStates == {"none", "available", "claimed", "completed", "cancelled"}
ClaimStates == {"none", "open", "completed", "cancelled"}
DispatchStates == {"unused", "reserved", "started", "bound", "closed"}
\* GitHub run status and worker job progress of the current run attempt:
\*   none                       no run exists
\*   queued                     status queued
\*   activation|agent|finishing status in_progress: activation job (snapshot +
\*                              bind), agent and safe-output jobs, then the
\*                              finish commit is done and the conclusion job
\*                              ("Terminate work queue worker") is next
\*   completed                  status completed
RunPhases == {"none", "queued", "activation", "agent", "finishing", "completed"}
InProgress == {"activation", "agent", "finishing"}

TypeOK ==
    /\ work \in [Works -> [state : WorkStates,
                           attempts : 0..MaxAttempts,
                           pos : 0..Cardinality(Works),
                           claim : D \cup {NoDispatch}]]
    /\ claim \in [D -> [work : Works \cup {NoWork},
                        state : ClaimStates,
                        by : 0..MaxRunAttempts]]
    /\ dispatch \in [D -> [state : DispatchStates,
                           launched : BOOLEAN,
                           bound : 0..MaxRunAttempts]]
    /\ run \in [D -> [phase : RunPhases, attempt : 0..MaxRunAttempts]]

Init ==
    /\ work = [w \in Works |-> [state |-> "none", attempts |-> 0, pos |-> 0, claim |-> NoDispatch]]
    /\ claim = [d \in D |-> [work |-> NoWork, state |-> "none", by |-> 0]]
    /\ dispatch = [d \in D |-> [state |-> "unused", launched |-> FALSE, bound |-> 0]]
    /\ run = [d \in D |-> [phase |-> "none", attempt |-> 0]]

-----------------------------------------------------------------------------
(* Helpers *)

OpenDispatches == {d \in D : dispatch[d].state \in {"reserved", "started", "bound"}}
HasOpenClaim(d) == claim[d].state = "open"
Available == {w \in Works : work[w].state = "available"}
\* Priority then FIFO: all modelled Work shares one priority, so the head is
\* the available Work with the oldest admission position.
Head == CHOOSE w \in Available : \A x \in Available : work[w].pos <= work[x].pos
\* A worker run may settle Claims only of the Dispatch bound to its own attempt.
BoundToRun(d) == dispatch[d].state = "bound" /\ dispatch[d].bound = run[d].attempt

(* Cancel the open Claim of dispatch d and close d (Release / ClaimCancellation).
   The publisher appends WorkCancellation{attempts_exhausted} in the same commit
   when attempts + 1 >= max_attempts. A closed Dispatch is never launched. *)
CancelClaim(d) ==
    LET w == claim[d].work
        exhausted == work[w].attempts + 1 >= MaxAttempts
    IN  /\ claim' = [claim EXCEPT ![d].state = "cancelled"]
        /\ work' = [work EXCEPT ![w].attempts = @ + 1,
                                ![w].claim = NoDispatch,
                                ![w].state = IF exhausted THEN "cancelled" ELSE "available"]
        /\ dispatch' = [dispatch EXCEPT ![d].state = "closed", ![d].launched = TRUE]

-----------------------------------------------------------------------------
(* Producer *)

\* submit: admit new Work at the next log position.
Submit(w) ==
    /\ work[w].state = "none"
    /\ work' = [work EXCEPT ![w].state = "available",
                            ![w].pos = Cardinality({x \in Works : work[x].state # "none"}) + 1]
    /\ UNCHANGED <<claim, dispatch, run>>

(* Dispatcher *)

\* dispatch_next: one Claim in a fresh reserved Dispatch for the FIFO head,
\* while non-closed pool Dispatches stay below the compiled concurrency.
Plan ==
    /\ Available # {}
    /\ Cardinality(OpenDispatches) < Concurrency
    /\ \E d \in D :
        /\ dispatch[d].state = "unused"
        /\ \A e \in D : dispatch[e].state = "unused" => d <= e
        /\ LET w == Head IN
           /\ claim' = [claim EXCEPT ![d] = [work |-> w, state |-> "open", by |-> 0]]
           /\ work' = [work EXCEPT ![w].state = "claimed", ![w].claim = d]
           /\ dispatch' = [dispatch EXCEPT ![d] = [state |-> "reserved", launched |-> FALSE, bound |-> 0]]
    /\ UNCHANGED run

\* workflow_dispatch POST succeeded: GitHub queues run attempt 1. The launch
\* commit [Dispatch{started}] applies only to a still reserved Dispatch and may
\* be lost (CAS exhausted, job killed); the Dispatch then stays reserved.
LaunchOK(d) ==
    /\ dispatch[d].state = "reserved"
    /\ ~dispatch[d].launched
    /\ run' = [run EXCEPT ![d] = [phase |-> "queued", attempt |-> 1]]
    /\ \/ dispatch' = [dispatch EXCEPT ![d].state = "started", ![d].launched = TRUE]
       \/ dispatch' = [dispatch EXCEPT ![d].launched = TRUE]
    /\ UNCHANGED <<work, claim>>

\* workflow_dispatch POST failed: launch commit [Release{launch_failed}]
\* (+ attempts rule), or the commit is lost and the Dispatch stays reserved.
\* An ambiguous failure (transport error, 5xx) may still have queued a run.
LaunchFail(d) ==
    /\ dispatch[d].state = "reserved"
    /\ ~dispatch[d].launched
    /\ \/ CancelClaim(d)
       \/ /\ dispatch' = [dispatch EXCEPT ![d].launched = TRUE]
          /\ UNCHANGED <<work, claim>>
    /\ \/ UNCHANGED run
       \/ run' = [run EXCEPT ![d] = [phase |-> "queued", attempt |-> 1]]

(* GitHub run environment *)

RunStart(d) ==
    /\ run[d].phase = "queued"
    /\ run' = [run EXCEPT ![d].phase = "activation"]
    /\ UNCHANGED <<work, claim, dispatch>>

\* A run may stop at any point without its conclusion job (cancellation,
\* runner loss, crash).
RunComplete(d) ==
    /\ run[d].phase \in InProgress
    /\ run' = [run EXCEPT ![d].phase = "completed"]
    /\ UNCHANGED <<work, claim, dispatch>>

\* A maintainer re-runs a completed run: same run id, next run attempt.
Rerun(d) ==
    /\ run[d].phase = "completed"
    /\ run[d].attempt < MaxRunAttempts
    /\ run' = [run EXCEPT ![d] = [phase |-> "queued", attempt |-> @.attempt + 1]]
    /\ UNCHANGED <<work, claim, dispatch>>

(* Worker run *)

\* Activation job: bind commit [Bound{dispatch_id, run_id, run_attempt}].
\* Allowed from reserved/started/bound with an open Claim; a later attempt of
\* the same run rebinds; the same attempt is an idempotent no-op. Without an
\* open Claim the assignment is settled: agent and safe_outputs jobs are
\* skipped and only the conclusion job remains.
Bind(d) ==
    /\ run[d].phase = "activation"
    /\ IF /\ dispatch[d].state \in {"reserved", "started", "bound"}
          /\ HasOpenClaim(d)
          /\ dispatch[d].bound <= run[d].attempt
       THEN /\ dispatch' = [dispatch EXCEPT ![d].state = "bound", ![d].bound = run[d].attempt]
            /\ run' = [run EXCEPT ![d].phase = "agent"]
       ELSE /\ run' = [run EXCEPT ![d].phase = "finishing"]
            /\ UNCHANGED dispatch
    /\ UNCHANGED <<work, claim>>

\* Finish step, outcome completed: Completion is accepted only for an open
\* Claim whose Dispatch is bound to this run attempt (else claim_scope_invalid
\* / claim_ineffective and the ledger is unchanged). The same finish commit
\* may also admit the completed Work's continuation successor: one previously
\* absent Work, distinct from the completed one, available at the next log
\* position (request_continuation route workqueue/issue). Its external
\* approval gate is abstracted like every other gate.
Complete(d) ==
    /\ run[d].phase = "agent"
    /\ run' = [run EXCEPT ![d].phase = "finishing"]
    /\ IF HasOpenClaim(d) /\ BoundToRun(d)
       THEN LET w == claim[d].work
                completed == [work EXCEPT ![w].state = "completed"]
            IN
            /\ claim' = [claim EXCEPT ![d].state = "completed", ![d].by = run[d].attempt]
            /\ dispatch' = [dispatch EXCEPT ![d].state = "closed"]
            /\ \/ work' = completed
               \/ \E c \in Works \ {w} :
                    /\ work[c].state = "none"
                    /\ work' = [completed EXCEPT ![c].state = "available",
                                                 ![c].pos = Cardinality({x \in Works : work[x].state # "none"}) + 1]
       ELSE UNCHANGED <<work, claim, dispatch>>

\* Finish step, outcome failed: ClaimCancellation{worker_failed} (+ attempts rule).
Fail(d) ==
    /\ run[d].phase = "agent"
    /\ run' = [run EXCEPT ![d].phase = "finishing"]
    /\ IF HasOpenClaim(d) /\ BoundToRun(d)
       THEN CancelClaim(d)
       ELSE UNCHANGED <<work, claim, dispatch>>

\* Conclusion job "Terminate work queue worker" (if: always(); it also runs
\* when activation or the agent failed), after which the run completes:
\* Release{worker_terminated} when the Dispatch still has an open Claim and is
\* unbound or bound to this run.
Terminate(d) ==
    /\ run[d].phase \in InProgress
    /\ run' = [run EXCEPT ![d].phase = "completed"]
    /\ IF dispatch[d].state \in {"reserved", "started", "bound"} /\ HasOpenClaim(d)
       THEN CancelClaim(d)
       ELSE UNCHANGED <<work, claim, dispatch>>

(* Maintenance (reconciler) *)

\* bound: the bound run is completed -> Release{run_terminal}.
\* reserved/started: no queued/in_progress run lists the dispatch_id ->
\* Release{run_missing}. A just-queued run may be missing from the listing,
\* so a queued run does not protect an unbound Dispatch; the late run then
\* finds no open Claim at bind and settles.
MaintainRelease(d) ==
    /\ HasOpenClaim(d)
    /\ \/ dispatch[d].state = "bound" /\ run[d].phase = "completed"
       \/ dispatch[d].state \in {"reserved", "started"} /\ run[d].phase \notin InProgress
    /\ CancelClaim(d)
    /\ UNCHANGED run

-----------------------------------------------------------------------------

Next ==
    \/ \E w \in Works : Submit(w)
    \/ Plan
    \/ \E d \in D :
        \/ LaunchOK(d) \/ LaunchFail(d)
        \/ RunStart(d) \/ RunComplete(d) \/ Rerun(d)
        \/ Bind(d) \/ Complete(d) \/ Fail(d) \/ Terminate(d)
        \/ MaintainRelease(d)

\* Scheduled dispatcher planning, scheduled maintenance and GitHub run
\* progress are weakly fair. Launch, worker steps and re-runs are not: a
\* worker may crash at any point, and maintenance recovers its Claims.
Fairness ==
    /\ WF_vars(Plan)
    /\ \A d \in D : WF_vars(MaintainRelease(d))
    /\ \A d \in D : WF_vars(RunStart(d)) /\ WF_vars(RunComplete(d))

Spec == Init /\ [][Next]_vars /\ Fairness

-----------------------------------------------------------------------------
(* Safety *)

\* At most one open Claim per Work, and claimed Work is owned by exactly the
\* open Claim it records.
OneOpenClaimPerWork ==
    /\ \A w \in Works : Cardinality({d \in D : claim[d].work = w /\ claim[d].state = "open"}) <= 1
    /\ \A w \in Works :
        (work[w].state = "claimed") <=>
            (work[w].claim # NoDispatch /\ claim[work[w].claim].state = "open" /\ claim[work[w].claim].work = w)
    /\ \A d \in D : claim[d].state = "open" => work[claim[d].work].claim = d

\* A Completion is recorded only by the run attempt its Dispatch is bound to,
\* and completed Work has exactly one completed Claim.
BoundRunOwnsCompletion ==
    /\ \A d \in D : claim[d].state = "completed" =>
        /\ claim[d].by \in 1..run[d].attempt
        /\ dispatch[d].bound = claim[d].by
        /\ work[claim[d].work].state = "completed"
    /\ \A w \in Works : work[w].state = "completed" =>
        Cardinality({d \in D : claim[d].work = w /\ claim[d].state = "completed"}) = 1

\* Non-closed Dispatches of the pool never exceed the compiled concurrency.
CapacityBound == Cardinality(OpenDispatches) <= Concurrency

\* Attempts never exceed max_attempts; Work still competing for Claims has
\* attempts left.
AttemptsBound ==
    \A w \in Works :
        /\ work[w].attempts <= MaxAttempts
        /\ work[w].state \in {"available", "claimed"} => work[w].attempts < MaxAttempts

\* Attempts only grow.
AttemptsMonotone == [][\A w \in Works : work'[w].attempts >= work[w].attempts]_vars

\* Terminal Work (completed or cancelled) never changes state again.
NoReopenCompleted ==
    [][\A w \in Works : work[w].state \in {"completed", "cancelled"} => work'[w].state = work[w].state]_vars

(* Liveness *)

\* Every submitted Work is eventually completed or cancelled, without clocks.
EventualSettlement ==
    \A w \in Works : (work[w].state # "none") ~> (work[w].state \in {"completed", "cancelled"})

=============================================================================
