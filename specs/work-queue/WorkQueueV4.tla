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
(*   Attempt   unused -> reserved -> started -> bound                      *)
(*               -> completed | cancelled                                  *)
(*   Run       queued -> in_progress -> completed (rerun: next attempt)    *)
(*   Reserver  launching -> running -> completed (dispatcher run attempt) *)
(*                                                                         *)
(* Replay accepts exactly one Claim per Dispatch, so a Claim and the      *)
(* Dispatch that launched it are one attempt, identified by its Dispatch  *)
(* slot: reserved|started|bound means the Claim is open and the Dispatch  *)
(* is not closed; completed|cancelled means the Claim is settled and the  *)
(* Dispatch is closed. A worker run is started by the workflow_dispatch   *)
(* of exactly one Dispatch and carries that dispatch_id in its input, so  *)
(* a run is also identified by its Dispatch slot; GitHub re-runs keep the *)
(* run id and increment the run attempt. Each Dispatch records the        *)
(* dispatcher run attempt that reserved it (reserved_by); only that       *)
(* attempt can record its launch, so maintenance judges a reserved        *)
(* Dispatch by that attempt's status alone. Nothing reads a clock: there  *)
(* are no deadlines, grace periods, expiry, backoff or observation ages.   *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Works,          \* Work identities (model values)
    NoWork,         \* "no Work" marker for unused attempt slots
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

VARIABLES work, attempt, run, dispatcher
vars == <<work, attempt, run, dispatcher>>

WorkStates == {"none", "available", "claimed", "completed", "cancelled"}
\* Phase of one attempt (one Claim in its own Dispatch):
\*   unused                     dispatch_id not minted yet
\*   reserved|started|bound     Claim open; Dispatch reserved, launch recorded,
\*                              or bound to a run attempt
\*   completed|cancelled        Claim settled; Dispatch closed
AttemptPhases == {"unused", "reserved", "started", "bound", "completed", "cancelled"}
OpenPhases == {"reserved", "started", "bound"}
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
\* The dispatcher run attempt that reserved the Dispatch (reserved_by: the
\* run id and run attempt of its dispatch_next commit's actor). A re-run of
\* the dispatcher is a new attempt with new request and dispatch ids, so it
\* never launches this Dispatch:
\*   none                       no dispatcher reserved the slot yet
\*   launching                  in progress; the launch of this Dispatch (POST
\*                              and launch commit) is still ahead
\*   running                    in progress; the launch is decided: recorded,
\*                              released (launch_failed) or its commit lost
\*   completed                  status completed, or the attempt is gone (404)
DispatcherPhases == {"none", "launching", "running", "completed"}

TypeOK ==
    /\ work \in [Works -> [state : WorkStates,
                           attempts : 0..MaxAttempts,
                           pos : 0..Cardinality(Works),
                           claim : D \cup {NoDispatch}]]
    \* bound: the run attempt recorded by bind; by: the run attempt that
    \* recorded Completion.
    /\ attempt \in [D -> [work : Works \cup {NoWork},
                          phase : AttemptPhases,
                          bound : 0..MaxRunAttempts,
                          by : 0..MaxRunAttempts]]
    /\ run \in [D -> [phase : RunPhases, attempt : 0..MaxRunAttempts]]
    /\ dispatcher \in [D -> DispatcherPhases]

Init ==
    /\ work = [w \in Works |-> [state |-> "none", attempts |-> 0, pos |-> 0, claim |-> NoDispatch]]
    /\ attempt = [d \in D |-> [work |-> NoWork, phase |-> "unused", bound |-> 0, by |-> 0]]
    /\ run = [d \in D |-> [phase |-> "none", attempt |-> 0]]
    /\ dispatcher = [d \in D |-> "none"]

-----------------------------------------------------------------------------
(* Helpers *)

OpenDispatches == {d \in D : attempt[d].phase \in OpenPhases}
HasOpenClaim(d) == attempt[d].phase \in OpenPhases
Available == {w \in Works : work[w].state = "available"}
\* Priority then FIFO: all modelled Work shares one priority, so the head is
\* the available Work with the oldest admission position.
Head == CHOOSE w \in Available : \A x \in Available : work[w].pos <= work[x].pos
\* A worker run may settle Claims only of the Dispatch bound to its own attempt.
BoundToRun(d) == attempt[d].phase = "bound" /\ attempt[d].bound = run[d].attempt

(* Cancel the open Claim of attempt d and close its Dispatch (Release /
   ClaimCancellation). The publisher appends WorkCancellation{attempts_exhausted}
   in the same commit when attempts + 1 >= max_attempts. A closed Dispatch is
   never launched; the recorded bound run attempt is kept. *)
CancelClaim(d) ==
    LET w == attempt[d].work
        exhausted == work[w].attempts + 1 >= MaxAttempts
    IN  /\ attempt' = [attempt EXCEPT ![d].phase = "cancelled"]
        /\ work' = [work EXCEPT ![w].attempts = @ + 1,
                                ![w].claim = NoDispatch,
                                ![w].state = IF exhausted THEN "cancelled" ELSE "available"]

-----------------------------------------------------------------------------
(* Producer *)

\* submit: admit new Work at the next log position.
Submit(w) ==
    /\ work[w].state = "none"
    /\ work' = [work EXCEPT ![w].state = "available",
                            ![w].pos = Cardinality({x \in Works : work[x].state # "none"}) + 1]
    /\ UNCHANGED <<attempt, run, dispatcher>>

(* Dispatcher *)

\* dispatch_next: one Claim in a fresh reserved Dispatch for the FIFO head,
\* while non-closed pool Dispatches stay below the compiled concurrency. The
\* Dispatch records the dispatcher attempt that reserved it, which then has
\* to launch it. One attempt may reserve several Dispatches; modelling one
\* attempt per slot lets them end independently, which includes ending
\* together.
Plan ==
    /\ Available # {}
    /\ Cardinality(OpenDispatches) < Concurrency
    /\ \E d \in D :
        /\ attempt[d].phase = "unused"
        /\ \A e \in D : attempt[e].phase = "unused" => d <= e
        /\ LET w == Head IN
           /\ attempt' = [attempt EXCEPT ![d] = [work |-> w, phase |-> "reserved", bound |-> 0, by |-> 0]]
           /\ work' = [work EXCEPT ![w].state = "claimed", ![w].claim = d]
        /\ dispatcher' = [dispatcher EXCEPT ![d] = "launching"]
    /\ UNCHANGED run

\* The reserving dispatcher attempt, still in progress, POSTed
\* workflow_dispatch and read back the created run: GitHub queues run
\* attempt 1. The launch commit [Dispatch{started, run_id, revision}] names
\* exactly that run (the run of slot d), applies only to a still reserved
\* Dispatch and may be lost (CAS exhausted, job killed); the Dispatch then
\* stays reserved and no run can bind it. A POST whose run cannot be
\* identified or verified is LaunchFail.
LaunchOK(d) ==
    /\ attempt[d].phase = "reserved"
    /\ dispatcher[d] = "launching"
    /\ run' = [run EXCEPT ![d] = [phase |-> "queued", attempt |-> 1]]
    /\ dispatcher' = [dispatcher EXCEPT ![d] = "running"]
    /\ \/ attempt' = [attempt EXCEPT ![d].phase = "started"]
       \/ UNCHANGED attempt
    /\ UNCHANGED work

\* The reserving dispatcher attempt's workflow_dispatch POST failed, or
\* returned no verifiable run: launch commit [Release{launch_failed}]
\* (+ attempts rule), or the commit is lost and the Dispatch stays reserved.
\* An ambiguous failure (transport error, 5xx) or an unverified launch may
\* still have queued a run; it can never bind.
LaunchFail(d) ==
    /\ attempt[d].phase = "reserved"
    /\ dispatcher[d] = "launching"
    /\ dispatcher' = [dispatcher EXCEPT ![d] = "running"]
    /\ \/ CancelClaim(d)
       \/ UNCHANGED <<work, attempt>>
    /\ \/ UNCHANGED run
       \/ run' = [run EXCEPT ![d] = [phase |-> "queued", attempt |-> 1]]

(* GitHub run environment *)

\* The reserving dispatcher attempt completes, is cancelled or crashes at any
\* point, before or after its launch. Only a reserved Dispatch is judged by
\* that attempt; once the Dispatch left reserved nothing reads dispatcher[d],
\* so its later end, which would change nothing else, is not modelled.
DispatcherEnd(d) ==
    /\ attempt[d].phase = "reserved"
    /\ dispatcher[d] \in {"launching", "running"}
    /\ dispatcher' = [dispatcher EXCEPT ![d] = "completed"]
    /\ UNCHANGED <<work, attempt, run>>

RunStart(d) ==
    /\ run[d].phase = "queued"
    /\ run' = [run EXCEPT ![d].phase = "activation"]
    /\ UNCHANGED <<work, attempt, dispatcher>>

\* A run may stop at any point without its conclusion job (cancellation,
\* runner loss, crash).
RunComplete(d) ==
    /\ run[d].phase \in InProgress
    /\ run' = [run EXCEPT ![d].phase = "completed"]
    /\ UNCHANGED <<work, attempt, dispatcher>>

\* A maintainer re-runs a completed run: same run id, next run attempt.
Rerun(d) ==
    /\ run[d].phase = "completed"
    /\ run[d].attempt < MaxRunAttempts
    /\ run' = [run EXCEPT ![d] = [phase |-> "queued", attempt |-> @.attempt + 1]]
    /\ UNCHANGED <<work, attempt, dispatcher>>

(* Worker run *)

\* Activation job: bind commit [Bound{dispatch_id, run_id, run_attempt}].
\* Allowed only once the dispatcher recorded the launch of this run (started)
\* or for its later attempts (bound); both phases have an open Claim, and the
\* same attempt is an idempotent no-op. A run without a recorded launch
\* (reserved: LAUNCH_UNVERIFIED at once, with no waiting), or without an open
\* Claim, receives no Claim: agent and safe_outputs jobs are skipped and only
\* the conclusion job remains. If the launch commit lands after such an
\* activation, the conclusion job finds a started Dispatch naming this run
\* and releases it (Terminate): the same ledger steps as LaunchOK followed by
\* a run that stops before Bind, so the launch is modelled as one step.
Bind(d) ==
    /\ run[d].phase = "activation"
    /\ IF /\ attempt[d].phase \in {"started", "bound"}
          /\ attempt[d].bound <= run[d].attempt
       THEN /\ attempt' = [attempt EXCEPT ![d].phase = "bound", ![d].bound = run[d].attempt]
            /\ run' = [run EXCEPT ![d].phase = "agent"]
       ELSE /\ run' = [run EXCEPT ![d].phase = "finishing"]
            /\ UNCHANGED attempt
    /\ UNCHANGED <<work, dispatcher>>

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
    /\ IF BoundToRun(d)
       THEN LET w == attempt[d].work
                completed == [work EXCEPT ![w].state = "completed"]
            IN
            /\ attempt' = [attempt EXCEPT ![d].phase = "completed", ![d].by = run[d].attempt]
            /\ \/ work' = completed
               \/ \E c \in Works \ {w} :
                    /\ work[c].state = "none"
                    /\ work' = [completed EXCEPT ![c].state = "available",
                                                 ![c].pos = Cardinality({x \in Works : work[x].state # "none"}) + 1]
       ELSE UNCHANGED <<work, attempt>>
    /\ UNCHANGED dispatcher

\* Finish step, outcome failed: ClaimCancellation{worker_failed} (+ attempts rule).
Fail(d) ==
    /\ run[d].phase = "agent"
    /\ run' = [run EXCEPT ![d].phase = "finishing"]
    /\ IF BoundToRun(d)
       THEN CancelClaim(d)
       ELSE UNCHANGED <<work, attempt>>
    /\ UNCHANGED dispatcher

\* Conclusion job "Terminate work queue worker" (if: always(); it also runs
\* when activation or the agent failed), after which the run completes:
\* Release{worker_terminated} when the Dispatch still has an open Claim and
\* records this run: launched as this run (started) or bound to it. A
\* reserved Dispatch records no launched run, so no worker run releases it;
\* maintenance does once its dispatcher attempt completed.
Terminate(d) ==
    /\ run[d].phase \in InProgress
    /\ run' = [run EXCEPT ![d].phase = "completed"]
    /\ IF attempt[d].phase \in {"started", "bound"}
       THEN CancelClaim(d)
       ELSE UNCHANGED <<work, attempt>>
    /\ UNCHANGED dispatcher

(* Maintenance (reconciler) *)

\* started/bound: judged by the recorded run id. Once that run is completed
\* -> Release{run_terminal} (a deleted run, 404, is run_missing); a queued or
\* in-progress run keeps the Dispatch open.
\* reserved: judged only by the dispatcher attempt that reserved it. While
\* that attempt is in progress it may still record the launch, so the
\* Dispatch stays open; once it completed -> Release{run_terminal} (404:
\* run_missing). Only that attempt can record a launch, so no run can bind
\* the Dispatch any more; a run it POSTed meets a closed Dispatch at bind.
MaintainRelease(d) ==
    /\ \/ attempt[d].phase \in {"started", "bound"} /\ run[d].phase = "completed"
       \/ attempt[d].phase = "reserved" /\ dispatcher[d] = "completed"
    /\ CancelClaim(d)
    /\ UNCHANGED <<run, dispatcher>>

-----------------------------------------------------------------------------

Next ==
    \/ \E w \in Works : Submit(w)
    \/ Plan
    \/ \E d \in D :
        \/ LaunchOK(d) \/ LaunchFail(d) \/ DispatcherEnd(d)
        \/ RunStart(d) \/ RunComplete(d) \/ Rerun(d)
        \/ Bind(d) \/ Complete(d) \/ Fail(d) \/ Terminate(d)
        \/ MaintainRelease(d)

\* Scheduled dispatcher planning, scheduled maintenance and GitHub run
\* progress are weakly fair; GitHub ends every dispatcher run attempt (job
\* timeout at the latest), so DispatcherEnd is weakly fair too. Launch,
\* worker steps and re-runs are not: a dispatcher or worker may crash at any
\* point, and maintenance recovers its Claim.
Fairness ==
    /\ WF_vars(Plan)
    /\ \A d \in D : WF_vars(MaintainRelease(d))
    /\ \A d \in D : WF_vars(RunStart(d)) /\ WF_vars(RunComplete(d))
    /\ \A d \in D : WF_vars(DispatcherEnd(d))

Spec == Init /\ [][Next]_vars /\ Fairness

-----------------------------------------------------------------------------
(* Safety *)

\* At most one open Claim per Work, and claimed Work is owned by exactly the
\* open Claim it records.
OneOpenClaimPerWork ==
    /\ \A w \in Works : Cardinality({d \in D : attempt[d].work = w /\ HasOpenClaim(d)}) <= 1
    /\ \A w \in Works :
        (work[w].state = "claimed") <=>
            (work[w].claim # NoDispatch /\ HasOpenClaim(work[w].claim) /\ attempt[work[w].claim].work = w)
    /\ \A d \in D : HasOpenClaim(d) => work[attempt[d].work].claim = d

\* A Completion is recorded only by the run attempt its Dispatch is bound to
\* (the independent by == bound witness), and completed Work has exactly one
\* completed attempt.
BoundRunOwnsCompletion ==
    /\ \A d \in D : attempt[d].phase = "completed" =>
        /\ attempt[d].by \in 1..run[d].attempt
        /\ attempt[d].bound = attempt[d].by
        /\ work[attempt[d].work].state = "completed"
    /\ \A w \in Works : work[w].state = "completed" =>
        Cardinality({d \in D : attempt[d].work = w /\ attempt[d].phase = "completed"}) = 1

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

\* A reserved Dispatch is released only by the dispatcher attempt that
\* reserved it, in the step that decides its launch (Release{launch_failed}),
\* or after that attempt completed: maintenance never releases a Dispatch
\* whose launch may still be recorded, and no worker run releases a Dispatch
\* that records no launch.
ReservedReleaseAwaitsDispatcher ==
    [][\A d \in D :
        (attempt[d].phase = "reserved" /\ attempt'[d].phase = "cancelled")
            => \/ dispatcher[d] = "completed"
               \/ dispatcher[d] = "launching" /\ dispatcher'[d] = "running"]_vars

(* Liveness *)

\* Every submitted Work is eventually completed or cancelled, without clocks.
EventualSettlement ==
    \A w \in Works : (work[w].state # "none") ~> (work[w].state \in {"completed", "cancelled"})

=============================================================================
