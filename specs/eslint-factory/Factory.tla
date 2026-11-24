------------------------------ MODULE Factory ------------------------------
EXTENDS Integers, FiniteSets, Sequences, TLC

CONSTANTS Worker, Installed, NoWrites, OutputMin, OutputMax, Fault, Witness
ASSUME /\ Worker \in {"miner", "refiner", "monster", "mixed"}
       /\ Installed \in BOOLEAN /\ NoWrites \in BOOLEAN
       /\ OutputMin \in 0..OutputMax /\ OutputMax \in 1..3

Works == {1, 2}
Profile(k) == IF Worker = "mixed"
              THEN IF k = 1 THEN "miner" ELSE "monster"
              ELSE Worker

VARIABLES phase, admitted, queueBranch, original, birth, requested, proposed,
          scoped, applied, completedAtWrite, verified, scan, lintClean, quality
vars == <<phase, admitted, queueBranch, original, birth, requested, proposed,
          scoped, applied, completedAtWrite, verified, scan, lintClean, quality>>

Init ==
  /\ phase = <<"absent", "absent">>
  /\ admitted = {} /\ queueBranch = FALSE
  /\ original = {} /\ birth = {} /\ requested = FALSE
  /\ proposed = <<-1, -1>> /\ scoped = <<FALSE, FALSE>>
  /\ applied = <<-1, -1>> /\ completedAtWrite = <<FALSE, FALSE>>
  /\ verified = <<FALSE, FALSE>>
  /\ scan = "unrun" /\ lintClean = FALSE /\ quality = "unchecked"

\* A missing ref is an empty queue. The producer's first checked safe-output
\* submission atomically installs the compiled Policy and Work while creating it.
Admit(k) ==
  /\ (Installed \/ Fault = "policy") /\ phase[k] = "absent"
  /\ phase' = [phase EXCEPT ![k] = "queued"]
  /\ admitted' = admitted \cup {k} /\ queueBranch' = TRUE
  /\ UNCHANGED <<original, birth, requested, proposed, scoped, applied,
                 completedAtWrite, verified, scan, lintClean, quality>>

Eligible == {k \in Works : phase[k] = "queued"}
PrefixHead == IF 1 \in Eligible THEN 1 ELSE 2
\* v4: a Dispatch carries exactly one Claim. The "group" fault puts both
\* eligible Works into this activation's Dispatch.
AssignmentProjection == IF Eligible = {} THEN {}
          ELSE IF Fault = "group" /\ Eligible = Works
               THEN Works ELSE {PrefixHead}

\* Two equal-priority, dependency-ready Works; queue arbitration is abstracted.
\* One protected prefix request; project only its first compatible assignment.
\* Other profile groups/native dispatches are outside this worker activation.
\* DispatchReservationRecovery in ../work-queue separately models the full
\* three-profile cohort's never-started reservations after dispatcher exit.
Dispatch ==
  /\ ~requested /\ queueBranch /\ (Installed \/ Fault = "policy")
  /\ original' = AssignmentProjection /\ birth' = AssignmentProjection /\ requested' = TRUE
  /\ phase' = [k \in Works |-> IF k \in AssignmentProjection THEN "claimed" ELSE phase[k]]
  /\ UNCHANGED <<admitted, queueBranch, proposed, scoped, applied, completedAtWrite, verified,
                 scan, lintClean, quality>>

Precheck(s) ==
  /\ \E k \in original : Profile(k) = "monster"
  /\ scan = "unrun"
  /\ s \in {"clean", "warnings", "errors", "installFailed", "buildFailed", "toolFailed"}
  /\ scan' = s
  /\ lintClean' = (s = "clean" \/ (s = "warnings" /\ Fault # "warning"))
  /\ UNCHANGED <<phase, admitted, queueBranch, original, birth, requested, proposed, scoped, applied,
                 completedAtWrite, verified, quality>>

\* Build/test/low-false-positive bars are agent obligations, not authority.
Quality(q) ==
  /\ original # {} /\ quality = "unchecked" /\ q \in {"passed", "failed"}
  /\ quality' = q
  /\ UNCHANGED <<phase, admitted, queueBranch, original, birth, requested, proposed, scoped, applied,
                 completedAtWrite, verified, scan, lintClean>>

\* The selector is omitted (it resolves to the one member), the member's own
\* handle, or a foreign handle; the scope boundary rejects a foreign one.
\* Omission is unambiguous only for a singleton original assignment.
Stage(k, n, selector) ==
  /\ k \in original /\ phase[k] \in {"claimed", "completed"}
  /\ proposed[k] = -1 /\ n \in 0..OutputMax
  /\ (~NoWrites \/ n = 0)
  /\ selector \in {"omitted", "own", "foreign"}
  /\ selector # "omitted" \/ Cardinality(original) = 1
  /\ selector # "foreign" \/ Fault = "scope"
  /\ proposed' = [proposed EXCEPT ![k] = n]
  /\ scoped' = [scoped EXCEPT ![k] = selector # "foreign"]
  /\ UNCHANGED <<phase, admitted, queueBranch, original, birth, requested, applied,
                 completedAtWrite, verified, scan, lintClean, quality>>

Complete(k) ==
  /\ k \in original /\ phase[k] = "claimed"
  /\ phase' = [phase EXCEPT ![k] = "completed"]
  /\ UNCHANGED <<admitted, queueBranch, original, birth, requested, proposed, scoped, applied,
                 completedAtWrite, verified, scan, lintClean, quality>>

Cancel(k) ==
  /\ k \in original /\ phase[k] \in {"claimed", "completed"}
  /\ phase' = [phase EXCEPT ![k] = "cancelled"]
  /\ UNCHANGED <<admitted, queueBranch, original, birth, requested, proposed, scoped, applied,
                 completedAtWrite, verified, scan, lintClean, quality>>

\* Resource/principal/run binding is abstracted as trusted successful authority.
\* Writes cannot precede Completion; cancellation and Result close the channel.
Effect(k) ==
  /\ k \in original /\ proposed[k] >= 0 /\ applied[k] = -1
  /\ phase[k] = "completed" \/ (Fault = "completion" /\ phase[k] = "claimed")
  /\ applied' = [applied EXCEPT ![k] = proposed[k]]
  /\ completedAtWrite' = [completedAtWrite EXCEPT ![k] = phase[k] = "completed"]
  /\ phase' = [j \in Works |-> IF Fault = "dag" /\ phase[j] = "absent"
                               THEN "queued" ELSE phase[j]]
  /\ UNCHANGED <<admitted, queueBranch, original, birth, requested, proposed, scoped,
                 verified, scan, lintClean, quality>>

\* "verified" stands for whole-contract independent readback, including memory
\* and controls where required. Failed/unavailable readback leaves it FALSE.
Readback(k) ==
  /\ k \in original /\ phase[k] = "completed" /\ ~verified[k]
  /\ applied[k] = proposed[k] /\ proposed[k] >= 0
  /\ proposed[k] >= OutputMin \/ NoWrites \/ Fault = "cardinality"
  /\ verified' = [verified EXCEPT ![k] = TRUE]
  /\ UNCHANGED <<phase, admitted, queueBranch, original, birth, requested, proposed, scoped, applied,
                 completedAtWrite, scan, lintClean, quality>>

Result(k) ==
  /\ k \in original /\ phase[k] = "completed"
  /\ verified[k] \/ Fault = "result"
  /\ phase' = [phase EXCEPT ![k] = "result"]
  /\ UNCHANGED <<admitted, queueBranch, original, birth, requested, proposed, scoped, applied,
                 completedAtWrite, verified, scan, lintClean, quality>>

\* A settled Dispatch's current assignment is empty; authority must keep using
\* the original membership instead of re-reading it.
Shrink ==
  /\ Fault = "membership" /\ original # {}
  /\ \E k \in original : phase[k] = "cancelled"
  /\ original' = {k \in original : phase[k] # "cancelled"}
  /\ UNCHANGED <<phase, admitted, queueBranch, birth, requested, proposed, scoped, applied,
                 completedAtWrite, verified, scan, lintClean, quality>>

Next ==
  \/ \E k \in Works : Admit(k) \/ Complete(k) \/ Cancel(k) \/ Effect(k) \/ Readback(k) \/ Result(k)
  \/ Dispatch \/ Shrink
  \/ \E s \in {"clean", "warnings", "errors", "installFailed", "buildFailed", "toolFailed"} : Precheck(s)
  \/ \E q \in {"passed", "failed"} : Quality(q)
  \/ \E k \in Works, n \in 0..OutputMax, selector \in {"omitted", "own", "foreign"} : Stage(k, n, selector)
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ phase \in [Works -> {"absent", "queued", "claimed", "completed", "cancelled", "result"}]
  /\ admitted \subseteq Works /\ queueBranch \in BOOLEAN
  /\ original \subseteq Works /\ birth \subseteq Works /\ requested \in BOOLEAN
  /\ proposed \in [Works -> (-1..OutputMax)] /\ applied \in [Works -> (-1..OutputMax)]
  /\ scoped \in [Works -> BOOLEAN] /\ completedAtWrite \in [Works -> BOOLEAN]
  /\ verified \in [Works -> BOOLEAN]
  /\ scan \in {"unrun", "clean", "warnings", "errors", "installFailed", "buildFailed", "toolFailed"}
  /\ lintClean \in BOOLEAN /\ quality \in {"unchecked", "passed", "failed"}
ImmutableMembership == original = birth
SingleClaimDispatch == Cardinality(birth) <= 1
PolicyRequired == original # {} => Installed
BranchProvisioning == (admitted # {}) <=> queueBranch
ScopedIntents == \A k \in Works : proposed[k] >= 0 => scoped[k]
EffectRequiresCompletion == \A k \in Works : applied[k] > 0 => completedAtWrite[k]
ResultRequiresReadback == \A k \in Works : phase[k] = "result" => verified[k]
ResultCardinality == \A k \in Works : phase[k] = "result" =>
                     (NoWrites \/ proposed[k] \in OutputMin..OutputMax)
WarningExitZero == scan # "unrun" => (lintClean = (scan \in {"clean", "warnings"}))
NoAutomaticDAG == admitted = {k \in Works : phase[k] # "absent"}
BoundedAssignment == Cardinality(birth) <= 3

WitnessReached ==
  CASE Witness = "warning-clean" -> scan = "warnings" /\ lintClean
    [] Witness = "completion-only" -> \E k \in birth : phase[k] = "completed" /\ ~verified[k]
    [] Witness = "three-outputs" -> \E k \in birth : applied[k] = 3
    [] Witness = "quality-not-gate" -> quality = "failed" /\ (\E k \in birth : phase[k] = "result")
    [] Witness = "tool-failure" -> scan = "toolFailed" /\ ~lintClean
    [] Witness = "result" -> \E k \in birth : phase[k] = "result" /\ verified[k]
    [] Witness = "no-write" -> NoWrites /\ (\E k \in birth : phase[k] = "result" /\ applied[k] = 0)
    [] OTHER -> FALSE
NeverWitness == ~WitnessReached
=============================================================================
