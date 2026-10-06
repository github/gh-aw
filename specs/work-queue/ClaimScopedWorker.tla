---------------------- MODULE ClaimScopedWorker ----------------------
EXTENDS Naturals, Integers, FiniteSets, Sequences, TLC

CONSTANT ClaimCount
ASSUME ClaimCount \in {1, 3}
Assigned == 1..ClaimCount
RawHandles == 0..(ClaimCount + 1)
Successors == {ClaimCount + 1, ClaimCount + 2}
Predecessors(w) ==
    IF w = ClaimCount + 1 THEN {1, ClaimCount}
    ELSE {IF ClaimCount = 1 THEN 1 ELSE 2}
ScopeFor(assignment, raw) ==
    IF raw = 0 /\ Cardinality(assignment) = 1
    THEN CHOOSE c \in assignment : TRUE
    ELSE IF raw \in assignment THEN raw ELSE 0
Fact(kind, c) == [kind |-> kind, claim |-> c]

\* Zero represents an omitted selector, -1 no staged output.
\* The assignment and trusted run binding are fixed, not agent-selected.
\* Each root requires one scoped handler batch; delivery is independently verified.
VARIABLES phase, intents, outputs, log, effects, verified, admitted, rejected
vars == <<phase, intents, outputs, log, effects, verified, admitted, rejected>>
Completed == {log[i].claim : i \in {j \in 1..Len(log) : log[j].kind = "Completion"}}
Cancelled == {log[i].claim : i \in {j \in 1..Len(log) : log[j].kind = "ClaimCancellation"}}
Results == {log[i].claim : i \in {j \in 1..Len(log) : log[j].kind = "Result"}}
Closed == Completed \cup Cancelled
EffectClaims == {e.claim : e \in effects}
Ready == {w \in Successors : Predecessors(w) \subseteq Results}
Init ==
    /\ phase = "agent"
    /\ intents = [c \in Assigned |-> "none"]
    /\ outputs = [c \in Assigned |-> -1]
    /\ log = <<>> /\ effects = {} /\ verified = {}
    /\ admitted = {} /\ rejected = {}
StageOutput(raw) ==
    /\ phase = "agent" /\ raw \in RawHandles
    /\ LET c == ScopeFor(Assigned, raw)
       IN IF c = 0
          THEN /\ rejected' = rejected \cup {raw}
               /\ UNCHANGED outputs
          ELSE /\ outputs[c] = -1
               /\ outputs' = [outputs EXCEPT ![c] = raw]
               /\ UNCHANGED rejected
    /\ UNCHANGED <<phase, intents, log, effects, verified, admitted>>
FinishIntent(raw, outcome) ==
    /\ phase = "agent" /\ raw \in RawHandles
    /\ outcome \in {"completed", "cancelled"}
    /\ LET c == ScopeFor(Assigned, raw)
       IN IF c = 0
          THEN /\ rejected' = rejected \cup {raw}
               /\ UNCHANGED intents
          ELSE /\ intents[c] = "none"
               /\ intents' = [intents EXCEPT ![c] = outcome]
               /\ UNCHANGED rejected
    /\ UNCHANGED <<phase, outputs, log, effects, verified, admitted>>
BeginFinalize ==
    /\ phase = "agent" /\ phase' = "finalizing"
    /\ UNCHANGED <<intents, outputs, log, effects, verified, admitted, rejected>>
Finalize(c) ==
    /\ phase = "finalizing" /\ c \in Assigned \ Closed
    /\ log' = Append(log, Fact(IF intents[c] = "completed"
                              THEN "Completion" ELSE "ClaimCancellation", c))
    /\ UNCHANGED <<phase, intents, outputs, effects, verified, admitted, rejected>>
Effect(c) ==
    /\ phase = "finalizing" /\ c \in Completed \ EffectClaims
    /\ outputs[c] # -1 /\ ScopeFor(Assigned, outputs[c]) = c
    /\ effects' = effects \cup {[claim |-> c, raw_handle |-> outputs[c]]}
    /\ UNCHANGED <<phase, intents, outputs, log, verified, admitted, rejected>>
VerifyDelivery(c) ==
    /\ c \in (Completed \cap EffectClaims) \ verified
    /\ verified' = verified \cup {c}
    /\ UNCHANGED <<phase, intents, outputs, log, effects, admitted, rejected>>
PublishResult(c) ==
    /\ c \in (Completed \cap verified) \ Results
    /\ log' = Append(log, Fact("Result", c))
    /\ UNCHANGED <<phase, intents, outputs, effects, verified, admitted, rejected>>
Terminate ==
    /\ phase = "finalizing" /\ Closed = Assigned
    /\ phase' = "terminal"
    /\ UNCHANGED <<intents, outputs, log, effects, verified, admitted, rejected>>
Crash ==
    /\ phase \in {"agent", "finalizing"} /\ phase' = "terminal"
    /\ UNCHANGED <<intents, outputs, log, effects, verified, admitted, rejected>>
Recover(c) ==
    /\ phase = "terminal" /\ c \in Assigned \ Closed
    /\ log' = Append(log, Fact("ClaimCancellation", c))
    /\ UNCHANGED <<phase, intents, outputs, effects, verified, admitted, rejected>>
Admit(w) ==
    /\ w \in Ready \ admitted /\ admitted' = admitted \cup {w}
    /\ UNCHANGED <<phase, intents, outputs, log, effects, verified, rejected>>
Next ==
    \/ \E raw \in RawHandles :
         StageOutput(raw) \/ \E outcome \in {"completed", "cancelled"} :
             FinishIntent(raw, outcome)
    \/ BeginFinalize \/ Terminate \/ Crash
    \/ \E c \in Assigned :
         Finalize(c) \/ Effect(c) \/ VerifyDelivery(c) \/ PublishResult(c) \/ Recover(c)
    \/ \E w \in Successors : Admit(w)
Spec == Init /\ [][Next]_vars
TypeOK ==
    /\ phase \in {"agent", "finalizing", "terminal"}
    /\ intents \in [Assigned -> {"none", "completed", "cancelled"}]
    /\ outputs \in [Assigned -> {-1} \cup RawHandles]
    /\ log \in Seq([kind : {"Completion", "ClaimCancellation", "Result"}, claim : Assigned])
    /\ effects \subseteq [claim : Assigned, raw_handle : RawHandles]
    /\ verified \subseteq Assigned /\ admitted \subseteq Successors
    /\ rejected \subseteq RawHandles
ScopeResolution ==
    \A assignment \in SUBSET (1..3) : \A raw \in 0..4 :
      LET c == ScopeFor(assignment, raw)
      IN /\ (c # 0 => c \in assignment)
         /\ (raw = 0 => (c # 0 <=> Cardinality(assignment) = 1))
         /\ (raw # 0 => c = IF raw \in assignment THEN raw ELSE 0)
OutputScope ==
    \A c \in Assigned : outputs[c] # -1 => ScopeFor(Assigned, outputs[c]) = c
EffectAuthorization ==
    /\ EffectClaims \subseteq Completed
    /\ \A e \in effects : ScopeFor(Assigned, e.raw_handle) = e.claim
    /\ Cardinality(effects) = Cardinality(EffectClaims)
ClosureIntegrity ==
    /\ Completed \cap Cancelled = {}
    /\ \A c \in Assigned :
         Cardinality({i \in 1..Len(log) :
             log[i].claim = c /\ log[i].kind # "Result"}) <= 1
ResultAuthority ==
    /\ verified \subseteq Completed \cap EffectClaims
    /\ Results \subseteq Completed \cap verified
    /\ \A c \in Assigned :
         Cardinality({i \in 1..Len(log) :
             log[i].claim = c /\ log[i].kind = "Result"}) <= 1
DAGAuthorization == admitted \subseteq Ready
Safety == TypeOK /\ ScopeResolution /\ OutputScope /\ EffectAuthorization
          /\ ClosureIntegrity /\ ResultAuthority /\ DAGAuthorization
NoAutomaticScope == ~(\E e \in effects : e.raw_handle = 0)
NoMixedDAGProgress ==
    ~(ClaimCount = 3 /\ phase = "terminal"
      /\ Completed = {1, 3} /\ Cancelled = {2}
      /\ intents[2] = "cancelled"
      /\ Results = {1, 3} /\ EffectClaims = {1, 3}
      /\ outputs[2] = 2 /\ admitted = {4})
BrokenScope(raw) ==
    /\ phase = "agent" /\ outputs[1] = -1
    /\ ScopeFor(Assigned, raw) = 0
    /\ outputs' = [outputs EXCEPT ![1] = raw]
    /\ UNCHANGED <<phase, intents, log, effects, verified, admitted, rejected>>
BrokenLastOpenScope ==
    /\ phase = "finalizing" /\ ClaimCount > 1
    /\ Cardinality(Assigned \ Closed) = 1
    /\ LET c == CHOOSE c \in Assigned \ Closed : TRUE
       IN /\ outputs[c] = -1 /\ outputs' = [outputs EXCEPT ![c] = 0]
    /\ UNCHANGED <<phase, intents, log, effects, verified, admitted, rejected>>
BrokenCancelledEffect(c) ==
    /\ c \in Cancelled \ EffectClaims /\ outputs[c] # -1
    /\ effects' = effects \cup {[claim |-> c, raw_handle |-> outputs[c]]}
    /\ UNCHANGED <<phase, intents, outputs, log, verified, admitted, rejected>>
BrokenDAGAdmission(w) ==
    /\ w \in Successors \ admitted /\ Predecessors(w) \subseteq Closed
    /\ ~(Predecessors(w) \subseteq Results)
    /\ admitted' = admitted \cup {w}
    /\ UNCHANGED <<phase, intents, outputs, log, effects, verified, rejected>>
BrokenMissingScopeSpec == Init /\ [][Next \/ BrokenScope(0)]_vars
BrokenForeignScopeSpec == Init /\ [][Next \/ BrokenScope(ClaimCount + 1)]_vars
BrokenLastOpenScopeSpec == Init /\ [][Next \/ BrokenLastOpenScope]_vars
BrokenCancelledOutputSpec ==
    Init /\ [][Next \/ \E c \in Assigned : BrokenCancelledEffect(c)]_vars
BrokenMixedDAGSpec ==
    Init /\ [][Next \/ \E w \in Successors : BrokenDAGAdmission(w)]_vars
=================================================================
