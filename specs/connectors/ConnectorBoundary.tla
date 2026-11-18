------------------------ MODULE ConnectorBoundary ------------------------
EXTENDS Naturals, FiniteSets

\* Informative, bounded composition of compiler, trusted host and transport.
\* Mutations are deliberately insecure negative controls, never runtime options.
CONSTANTS Mutation, Probe

Capabilities == {"read", "other-read", "write"}
Phases == {"agent", "safe-output"}
IDs == {"approved", "other", "ungranted"}
Phase(c) == IF c = "write" THEN "safe-output" ELSE "agent"
Credential(c) == CASE c = "read" -> "reader"
                  [] c = "other-read" -> "other-reader"
                  [] OTHER -> "writer"
Endpoint(c) == IF c = "other-read" THEN "other-origin" ELSE "api-origin"
Resource(c) == c
Grant(c) == CASE c = "read" -> {"approved", "other"}
             [] c = "other-read" -> {"other"}
             [] OTHER -> {"approved"}
EmptyPlan == [caps |-> {}, credentials |-> {}, endpoints |-> {}, resources |-> {}]
PlanFor(cs) == [caps |-> cs,
               credentials |-> {Credential(c) : c \in cs},
               endpoints |-> {Endpoint(c) : c \in cs},
               resources |-> {Resource(c) : c \in cs}]

VARIABLES pc, selected, phase, inv, plan, secretReads, requests,
          credentialsUsed, sentEndpoint, outcome, authDenied, returnedID,
          evidenceOK

vars == <<pc, selected, phase, inv, plan, secretReads, requests,
          credentialsUsed, sentEndpoint, outcome, authDenied, returnedID,
          evidenceOK>>

SelectedPhase == {c \in selected : Phase(c) = phase}
Staging == inv.cap = "write" /\ inv.staged
SelectedCall == inv.cap \in plan.caps /\ Phase(inv.cap) = phase
ExactGrant == inv.target \in Grant(inv.cap)
HostAuthorization == phase # "safe-output" \/ inv.writeAuthorized

Init ==
  /\ pc = "compile"
  /\ selected \in SUBSET Capabilities
  /\ phase \in Phases
  /\ inv \in [cap : Capabilities, target : IDs, typed : BOOLEAN,
              networkApproved : BOOLEAN, writeAuthorized : BOOLEAN,
              staged : BOOLEAN]
  /\ plan = EmptyPlan
  /\ secretReads = 0 /\ requests = 0 /\ credentialsUsed = {}
  /\ sentEndpoint = "none" /\ outcome = "pending"
  /\ authDenied = FALSE /\ returnedID = "none" /\ evidenceOK = FALSE

Compile ==
  /\ pc = "compile"
  /\ plan' = PlanFor(IF Mutation = "phase-pruning" THEN Capabilities
                    ELSE SelectedPhase)
  /\ pc' = "validate"
  /\ UNCHANGED <<selected, phase, inv, secretReads, requests, credentialsUsed,
                  sentEndpoint, outcome, authDenied, returnedID, evidenceOK>>

Validate ==
  /\ pc = "validate"
  /\ LET permitted == SelectedCall /\ inv.typed
                      /\ (ExactGrant \/ Mutation = "grant")
                      /\ (Staging \/
                          ((HostAuthorization \/ Mutation = "write-authorization")
                           /\ (inv.networkApproved \/ Mutation = "network")))
     IN /\ pc' = IF permitted THEN (IF Staging THEN "stage" ELSE "resolve")
                 ELSE "done"
        /\ outcome' = IF permitted THEN "pending" ELSE "rejected"
  /\ UNCHANGED <<selected, phase, inv, plan, secretReads, requests,
                  credentialsUsed, sentEndpoint, authDenied, returnedID,
                  evidenceOK>>

Stage ==
  /\ pc = "stage"
  /\ secretReads' = IF Mutation = "staged-secret" THEN 1 ELSE 0
  /\ requests' = IF Mutation = "staged-network" THEN 1 ELSE 0
  /\ outcome' = "staged" /\ pc' = "done"
  /\ UNCHANGED <<selected, phase, inv, plan, credentialsUsed, sentEndpoint,
                  authDenied, returnedID, evidenceOK>>

Resolve ==
  /\ pc = "resolve"
  /\ secretReads' = secretReads + 1
  /\ credentialsUsed' = {IF Mutation = "writer-isolation" THEN "writer"
                         ELSE Credential(inv.cap)}
  /\ pc' = "send"
  /\ UNCHANGED <<selected, phase, inv, plan, requests, sentEndpoint,
                  outcome, authDenied, returnedID, evidenceOK>>

Send ==
  /\ pc = "send"
  /\ sentEndpoint' \in (IF Mutation = "endpoint" THEN
                          {Endpoint(inv.cap), "changed-origin-or-path"}
                        ELSE {Endpoint(inv.cap)})
  /\ requests' = requests + 1 /\ pc' = "respond"
  /\ UNCHANGED <<selected, phase, inv, plan, secretReads, credentialsUsed,
                  outcome, authDenied, returnedID, evidenceOK>>

Respond ==
  /\ pc = "respond"
  /\ \E status \in {"ok", "unauthorized", "redirect", "uncertain"},
        identity \in IDs, valid \in BOOLEAN :
       /\ returnedID' = identity /\ evidenceOK' = valid
       /\ authDenied' = (status = "unauthorized")
       /\ requests' = IF status = "unauthorized" /\ Mutation = "auth-retry"
                       THEN requests + 1 ELSE requests
       /\ outcome' = IF status = "ok" /\ valid /\
                          (identity = inv.target \/ Mutation = "response-identity")
                        THEN (IF inv.cap = "write" THEN "write" ELSE "read")
                      ELSE "rejected"
  /\ pc' = "done"
  /\ UNCHANGED <<selected, phase, inv, plan, secretReads, credentialsUsed,
                  sentEndpoint>>

\* Explicit terminal stuttering; no fairness or eventual-delivery assertion.
Done == pc = "done" /\ UNCHANGED vars
Next == Compile \/ Validate \/ Stage \/ Resolve \/ Send \/ Respond \/ Done
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ pc \in {"compile", "validate", "stage", "resolve", "send", "respond", "done"}
  /\ selected \subseteq Capabilities /\ phase \in Phases
  /\ inv \in [cap : Capabilities, target : IDs, typed : BOOLEAN,
              networkApproved : BOOLEAN, writeAuthorized : BOOLEAN,
              staged : BOOLEAN]
  /\ plan \in [caps : SUBSET Capabilities,
                credentials : SUBSET {"reader", "other-reader", "writer"},
                endpoints : SUBSET {"api-origin", "other-origin"},
                resources : SUBSET Capabilities]
  /\ secretReads \in 0..1 /\ requests \in 0..2
  /\ credentialsUsed \subseteq {"reader", "other-reader", "writer"}
  /\ sentEndpoint \in {"none", "api-origin", "other-origin", "changed-origin-or-path"}
  /\ outcome \in {"pending", "rejected", "staged", "read", "write"}
  /\ authDenied \in BOOLEAN /\ returnedID \in IDs \cup {"none"}
  /\ evidenceOK \in BOOLEAN

ExactPhasePlan == pc # "compile" => plan = PlanFor(SelectedPhase)
GrantBeforeSecret ==
  secretReads > 0 => SelectedCall /\ inv.typed /\ ExactGrant
WriterIsolation ==
  phase = "agent" => "writer" \notin plan.credentials \cup credentialsUsed
StagedIsolation == Staging => secretReads = 0 /\ requests = 0
HostWriteAuthorization ==
  secretReads > 0 /\ inv.cap = "write" => phase = "safe-output" /\ inv.writeAuthorized
NetworkBoundary ==
  /\ (secretReads > 0 /\ ~Staging => inv.networkApproved)
  /\ (requests > 0 /\ ~Staging => sentEndpoint = Endpoint(inv.cap))
ResponseIdentity ==
  outcome \in {"read", "write"} => evidenceOK /\ returnedID = inv.target
FailClosed == authDenied => outcome = "rejected" /\ requests = 1 /\ secretReads = 1
Safety == TypeOK /\ ExactPhasePlan /\ GrantBeforeSecret /\ WriterIsolation /\
          StagedIsolation /\ HostWriteAuthorization /\ NetworkBoundary /\
          ResponseIdentity /\ FailClosed

\* Deliberately false invariants: counterexamples establish existential witnesses,
\* not universal liveness. The identity witness uses another *granted* read ID.
ProbeAbsent ==
  CASE Probe = "read" -> outcome # "read"
    [] Probe = "write" -> outcome # "write"
    [] Probe = "staged" -> outcome # "staged"
    [] Probe = "authorization-denied" -> ~authDenied
    [] Probe = "wrong-granted-identity" ->
         ~(outcome = "rejected" /\ inv.cap = "read" /\
           inv.target = "approved" /\ returnedID = "other" /\ evidenceOK)
    [] OTHER -> TRUE
=============================================================================
