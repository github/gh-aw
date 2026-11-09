--------------------------- MODULE QueueLifecycle ---------------------------
EXTENDS Naturals, Sequences, FiniteSets, TLC

\* A separate bounded launch-boundary refinement, not a wire/host proof.
\* TrustedTerminal, TrustedActivation and TrustedReceipt abstract independently
\* checked evidence, NOT agent assertions, deadlines, or empty discovery.
\* Profile/ref/principal/resource compatibility is an immutable profile class.
\* The independent packing oracle is the finite FIFO fair order a,b,c with
\* profiles X,Y,X. A one-group budget must stop at b, not skip to c.
\* Epochs repeat only after full drain; fairness arithmetic is QueueService's
\* responsibility. No credentials, Git CAS, arbitrary JSON or host are modeled.
\* Reserved Release abstracts an already-authorized definitive prelaunch
\* decision. DispatchReservationRecovery refines terminal originating-attempt
\* evidence, bounded sweeps and the stale-recovery/start-marker race.
\* accepted is a volatile receipt observation, NOT a durable Dispatch state.
\* Receipt loss and exhausted bounded discovery never establish nonlaunch.
CONSTANTS GroupBudget, EpochBound, Broken
VARIABLES epoch, admissionPaused, grantsPaused, admitted, assignment,
          markers, sends, phase, binding, terminal, released,
          status, effects, results, failures, replacements, violation,
          verificationAttempts, accepted

vars == <<epoch, admissionPaused, grantsPaused, admitted, assignment,
          markers, sends, phase, binding, terminal, released,
          status, effects, results, failures, replacements, violation,
          verificationAttempts, accepted>>
Roots == {"a", "b", "c"}
Groups == {"X", "Y"}
Claims == Roots \cup {"join", "blocked"}
None == "none"
EmptyAssignment == [g \in Groups |-> <<>>]
Packed == IF GroupBudget = 1
          THEN [g \in Groups |-> IF g = "X" THEN <<"a">> ELSE <<>>]
          ELSE [g \in Groups |-> IF g = "X" THEN <<"a", "c">> ELSE <<"b">>]
Members(g) == {assignment[g][i] : i \in 1..Len(assignment[g])}
Assigned == UNION {Members(g) : g \in Groups}
Group(c) == IF c = "b" THEN "Y" ELSE "X"
TrustedActivation(g) == sends[g] > 0 /\ g \notin released
UnboundPhases == {"uncertain", "unresolved"}
TrustedTerminal(g) == sends[g] > 0 /\ phase[g] \in {"bound", "uncertain", "unresolved"}
TrustedReceipt(c) == c \in effects
LaunchStatus(g) == IF phase[g] = "uncertain" /\ g \in accepted
                   THEN "activation_pending" ELSE phase[g]
Authorized(c) == c \in Assigned /\ binding[Group(c)] = 1
                 /\ Group(c) \notin released /\ phase[Group(c)] = "bound"

Init ==
  /\ epoch = 1 /\ admissionPaused = FALSE /\ grantsPaused = FALSE
  /\ admitted = FALSE /\ assignment = EmptyAssignment
  /\ markers = {} /\ sends = [g \in Groups |-> 0]
  /\ phase = [g \in Groups |-> "idle"] /\ binding = [g \in Groups |-> 0]
  /\ terminal = {} /\ released = {}
  /\ status = [c \in Claims |-> "waiting"]
  /\ effects = {} /\ results = {} /\ failures = {} /\ replacements = {}
  /\ violation = None
  /\ verificationAttempts = [c \in Roots |-> 0]
  /\ accepted = {}

Control ==
  /\ \/ admissionPaused' = ~admissionPaused /\ grantsPaused' = grantsPaused
     \/ grantsPaused' = ~grantsPaused /\ admissionPaused' = admissionPaused
  /\ UNCHANGED <<accepted, epoch, admitted, assignment, markers, sends, phase, binding,
                 terminal, released, status, effects, results, failures,
                 replacements, violation, verificationAttempts>>

Admit ==
  /\ ~admitted /\ ~admissionPaused
  /\ admitted' = TRUE
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, assignment, markers,
                 sends, phase, binding, terminal, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

Grant ==
  /\ admitted /\ assignment = EmptyAssignment /\ ~grantsPaused
  /\ assignment' = IF Broken = "skip-prefix" /\ GroupBudget = 1
                   THEN [g \in Groups |-> IF g = "X" THEN <<"a", "c">> ELSE <<>>]
                   ELSE Packed
  /\ phase' = [g \in Groups |-> IF Len(assignment'[g]) > 0 THEN "reserved" ELSE "idle"]
  /\ status' = [c \in Claims |-> IF c \in UNION { {assignment'[g][i] :
                      i \in 1..Len(assignment'[g])} : g \in Groups}
                                  THEN "open" ELSE "waiting"]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, markers,
                 sends, binding, terminal, released, effects, results,
                 failures, replacements, violation, verificationAttempts>>

Mark(g) ==
  /\ phase[g] = "reserved" /\ ~grantsPaused /\ g \notin released
  /\ markers' = markers \cup {g} /\ phase' = [phase EXCEPT ![g] = "marked"]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 sends, binding, terminal, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

Send(g) ==
  /\ g \notin released
  /\ (phase[g] = "marked"
      \/ (Broken = "second-sender" /\ phase[g] \in UnboundPhases /\ sends[g] = 1))
  /\ sends' = [sends EXCEPT ![g] = @ + 1]
  /\ phase' = [phase EXCEPT ![g] = "uncertain"]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, binding, terminal, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

ObserveReceipt(g) ==
  /\ sends[g] = 1 /\ g \notin accepted
  /\ accepted' = accepted \cup {g}
  /\ binding' = IF Broken = "receipt-binding" /\ phase[g] \in UnboundPhases
                THEN [binding EXCEPT ![g] = 1] ELSE binding
  /\ UNCHANGED <<epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, terminal, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

LoseReceipt(g) ==
  /\ g \in accepted /\ accepted' = accepted \ {g}
  /\ UNCHANGED <<epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released, status,
                 effects, results, failures, replacements, violation, verificationAttempts>>

\* Exhaust abstracts failed bounded discovery or the installed polling budget.
Exhaust(g, outcome) ==
  /\ phase[g] \in UnboundPhases /\ binding[g] = 0 /\ g \notin released
  /\ outcome \in {"unresolved", "conflict"}
  /\ phase' = [phase EXCEPT ![g] = outcome]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, binding, terminal, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

Activate(g) ==
  /\ TrustedActivation(g) /\ phase[g] \in UnboundPhases
  /\ binding' = [binding EXCEPT ![g] = 1]
  /\ phase' = [phase EXCEPT ![g] = "bound"]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, terminal, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

ConflictingActivation(g) ==
  /\ TrustedActivation(g) /\ binding[g] = 1 /\ phase[g] = "bound"
  /\ phase' = [phase EXCEPT ![g] = "conflict"]
  \* One formerly bound run's termination cannot discharge a newly conflicting
  \* launch. This bounded model conservatively leaves that conflict unresolved.
  /\ binding' = IF Broken = "conflicting-binding"
               THEN [binding EXCEPT ![g] = 2] ELSE binding
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, terminal, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

UnauthenticatedActivation(g) ==
  /\ Broken = "unauthenticated-activation" /\ phase[g] \in UnboundPhases
  /\ phase' = [phase EXCEPT ![g] = "bound"] /\ binding' = [binding EXCEPT ![g] = 1]
  /\ violation' = "unauthenticated"
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, terminal, released, status, effects,
                 results, failures, replacements, verificationAttempts>>

Finish(c, outcome) ==
  /\ Authorized(c) /\ status[c] = "open"
  /\ status' = [status EXCEPT ![c] = outcome]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released,
                 effects, results, failures, replacements, violation, verificationAttempts>>

Effect(c, attempt) ==
  /\ c \in Assigned /\ status[c] = "completed" /\ Authorized(c)
  /\ c \notin effects
  /\ (attempt = 1 \/ (Broken = "rerun-effects" /\ attempt = 2))
  /\ effects' = effects \cup {c}
  /\ violation' = IF attempt = 2 THEN "rerun" ELSE violation
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released,
                 status, results, failures, replacements, verificationAttempts>>

Result(c) ==
  /\ status[c] = "completed" /\ TrustedReceipt(c)
  /\ c \notin results /\ (c \notin failures \/ Broken = "result-after-failure")
  /\ results' = results \cup {c}
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released,
                 status, effects, failures, replacements, violation, verificationAttempts>>

TerminalEvidence(g) ==
  /\ TrustedTerminal(g) /\ g \notin terminal
  /\ terminal' = terminal \cup {g}
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, released, status, effects,
                 results, failures, replacements, violation, verificationAttempts>>

VerifyDelivery(c) ==
  /\ c \in Assigned /\ status[c] = "completed" /\ Group(c) \in terminal
  /\ c \notin results \cup failures /\ verificationAttempts[c] < 2
  /\ verificationAttempts' = [verificationAttempts EXCEPT ![c] = @ + 1]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released, status,
                 effects, results, failures, replacements, violation>>

DeliveryFailure(c) ==
  /\ c \in Assigned /\ status[c] = "completed"
  /\ Group(c) \in terminal /\ c \notin results /\ c \notin failures
  /\ (verificationAttempts[c] = 2 \/ Broken = "early-delivery-failure")
  \* The finite verifier budget is explicit; receipt truth remains abstract.
  /\ failures' = failures \cup {c}
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released,
                 status, effects, results, replacements, violation, verificationAttempts>>

Release(g) ==
  /\ g \notin released /\ Len(assignment[g]) > 0
  /\ ((g \in terminal /\ phase[g] # "conflict") \/ phase[g] = "reserved"
      \/ (Broken = "ambiguous-release" /\ phase[g] \in UnboundPhases)
      \/ (Broken = "conflict-release" /\ g \in terminal /\ phase[g] = "conflict"))
  \* "reserved" means no start marker exists: definitive nonlaunch.
  /\ violation' = IF g \notin terminal /\ g \in markers
                  THEN "unproved-release" ELSE violation
  /\ released' = released \cup {g}
  /\ status' = [c \in Claims |-> IF c \in Members(g) /\ status[c] = "open"
                                 THEN "cancelled" ELSE status[c]]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, effects, results,
                 failures, replacements, verificationAttempts>>

Replace(c, disposition) ==
  /\ c \in failures /\ c \notin replacements
  /\ (disposition \in {"none", "remediated"}
      \/ (Broken = "unsafe-replacement" /\ disposition = "unknown"))
  /\ replacements' = replacements \cup {c}
  /\ violation' = IF disposition = "unknown" THEN "unsafe-replacement" ELSE violation
  \* A replacement is a NEW immutable node; old descendants stay unchanged.
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released, status,
                 effects, results, failures, verificationAttempts>>

Successor(c) ==
  /\ c \in {"join", "blocked"} /\ status[c] = "waiting"
  /\ IF c = "join" THEN {"a", "c"} \subseteq results ELSE "b" \in results
  /\ status' = [status EXCEPT ![c] = "ready"]
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released, effects,
                 results, failures, replacements, violation, verificationAttempts>>

Preempt ==
  /\ Broken = "pause-preempts" /\ grantsPaused /\ "a" \in Assigned
  /\ status["a"] = "open"
  /\ status' = [status EXCEPT !["a"] = "cancelled"]
  /\ violation' = "preempted"
  /\ UNCHANGED <<accepted, epoch, admissionPaused, grantsPaused, admitted, assignment,
                 markers, sends, phase, binding, terminal, released,
                 effects, results, failures, replacements, verificationAttempts>>

Repeat ==
  /\ epoch < EpochBound /\ Assigned # {}
  /\ \A g \in Groups : Len(assignment[g]) > 0 => g \in released
  /\ \A c \in Assigned : status[c] \in {"completed", "cancelled"}
  /\ \A c \in Assigned : status[c] = "completed" => c \in results \cup failures
  /\ epoch' = epoch + 1
  /\ admitted' = FALSE /\ assignment' = EmptyAssignment
  /\ markers' = {} /\ sends' = [g \in Groups |-> 0]
  /\ phase' = [g \in Groups |-> "idle"] /\ binding' = [g \in Groups |-> 0]
  /\ terminal' = {} /\ released' = {}
  /\ status' = [c \in Claims |-> "waiting"] /\ effects' = {}
  /\ results' = {} /\ failures' = {} /\ replacements' = {}
  /\ verificationAttempts' = [c \in Roots |-> 0]
  /\ accepted' = {}
  /\ UNCHANGED <<admissionPaused, grantsPaused, violation>>

Next == Control \/ Admit \/ Grant \/ Repeat \/ Preempt
        \/ (\E g \in Groups : Mark(g) \/ Send(g) \/ ObserveReceipt(g)
             \/ LoseReceipt(g) \/ Exhaust(g, "unresolved") \/ Exhaust(g, "conflict") \/ Activate(g)
             \/ ConflictingActivation(g) \/ UnauthenticatedActivation(g)
             \/ TerminalEvidence(g) \/ Release(g))
        \/ (\E c \in Roots : Finish(c, "completed") \/ Finish(c, "cancelled")
             \/ Effect(c, 1) \/ Effect(c, 2) \/ Result(c) \/ VerifyDelivery(c) \/ DeliveryFailure(c)
             \/ Replace(c, "none") \/ Replace(c, "remediated") \/ Replace(c, "unknown"))
        \/ (\E c \in {"join", "blocked"} : Successor(c))
Spec == Init /\ [][Next]_vars

TypeOK == epoch \in 1..EpochBound /\ accepted \subseteq Groups /\ results \subseteq Roots
          /\ failures \subseteq Roots /\ replacements \subseteq Roots
FairPrefixPacking == assignment = EmptyAssignment \/ assignment = Packed
OneSender == \A g \in Groups : sends[g] <= 1 /\ (sends[g] > 0 => g \in markers)
BindingAuthority == (\A g \in Groups : binding[g] \in {0, 1})
                    /\ violation # "unauthenticated"
AcceptanceNotAuthority == \A g \in Groups :
                           LaunchStatus(g) = "activation_pending" => binding[g] = 0
AttemptOneEffects == violation # "rerun"
ReservationAuthority == \A g \in released :
                           ((g \in terminal) \/ (g \notin markers /\ sends[g] = 0))
                           /\ phase[g] # "conflict"
ControlNotPreemption == violation # "preempted"
DeliveryExclusivity == results \cap failures = {}
DeliveryFailureBudget == \A c \in failures :
                            verificationAttempts[c] = 2 /\ Group(c) \in terminal
ResultSoundness == \A c \in results : status[c] = "completed" /\ c \in effects
ReplacementSafety == replacements \subseteq failures /\ violation # "unsafe-replacement"
DAGBarrier == (status["join"] = "ready" => {"a", "c"} \subseteq results)
              /\ (status["blocked"] = "ready" => "b" \in results)
NoMixedLifecycleWitness ==
  ~(status["a"] = "completed" /\ status["b"] = "cancelled"
    /\ status["c"] = "completed" /\ status["join"] = "ready"
    /\ status["blocked"] = "waiting" /\ admissionPaused /\ grantsPaused)
NoActivationRecoveryWitness ==
  ~(\E g \in Groups : phase[g] = "bound" /\ sends[g] = 1 /\ binding[g] = 1)
NoAcceptedPendingWitness ==
  ~(\E g \in Groups : LaunchStatus(g) = "activation_pending"
                     /\ binding[g] = 0 /\ g \notin released)
NoAcceptedBindingWitness ==
  ~(\E g \in Groups : g \in accepted /\ phase[g] = "bound" /\ binding[g] = 1)
NoUnresolvedReservationWitness ==
  ~(\E g \in Groups : phase[g] = "unresolved" /\ binding[g] = 0
                     /\ g \notin terminal /\ g \notin released)
NoConflictReservationWitness ==
  ~(\E g \in Groups : phase[g] = "conflict" /\ binding[g] = 1 /\ g \notin released)
NoReleasedPendingDelivery ==
  ~(\E c \in Assigned : Group(c) \in released /\ Group(c) \in terminal
                        /\ status[c] = "completed" /\ c \notin results \cup failures)
=============================================================================
