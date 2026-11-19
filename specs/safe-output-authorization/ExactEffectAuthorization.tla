--------------------- MODULE ExactEffectAuthorization ---------------------
EXTENDS Naturals, FiniteSets

\* EXPERIMENTAL. Informative, bounded model of resolved-effect safe-output
\* authorization (Safe Outputs Specification, "Safe-output invocation
\* authorization"). It is not a conformance artifact and may change without
\* deprecation.
\*
\* Two logical records with identical inputs run concurrently with a hostile
\* environment that can change observed preconditions, move the atomically
\* guarded ref, expire the decision, tamper with SDK middleware, lose
\* responses and redeliver a record. Record "a" uses a trusted adapter route;
\* record "c" is custom code that must stay invocation-bound.
\*
\* Each adapter plan has two ordered effects: effect 1 creates or updates a
\* target under an atomic lease; effect 2 addresses the resource created by
\* effect 1 through a typed receipt (a derived slot).
\*
\* Mutation removes one protection as a negative control. Mutations are
\* deliberately insecure and are never runtime options.
CONSTANTS Mutation, Staged, RequireResolved

Mutations == {"none", "downgrade", "forged-promotion", "no-wire-check",
              "no-freshness", "retry", "no-claim", "unbound-derive",
              "no-revalidate", "partial-success", "no-lease"}
ASSUME Mutation \in Mutations /\ Staged \in BOOLEAN /\ RequireResolved \in BOOLEAN

Records == {"a", "c"}
Route(r) == IF r = "a" THEN "adapter" ELSE "custom"
Effects == {1, 2}
Ids == {"n1", "n2"}
Payloads == {"planned", "tampered"} \cup Ids \cup {"message"}
Phases == {"init", "planned", "requested", "running", "done", "staged"}
Statuses == {"pending", "confirmed", "rejected", "indeterminate", "blocked"}

VARIABLES phase, binding, requests, claimed, allows, decision, expired,
          delivered, eff, sendCount, receipt, sends, drift, head,
          revalidated, checked, reported

vars == <<phase, binding, requests, claimed, allows, decision, expired,
          delivered, eff, sendCount, receipt, sends, drift, head,
          revalidated, checked, reported>>

TypeOK ==
  /\ phase \in [Records -> Phases]
  /\ binding \in [Records -> {"none", "invocation", "resolved_effect"}]
  /\ requests \subseteq [rec : Records, binding : {"invocation", "resolved_effect"}, n : 1..3]
  /\ claimed \subseteq Records
  /\ allows \in [Records -> 0..3]
  /\ decision \in [Records -> {"none", "allow", "deny"}]
  /\ expired \in [Records -> BOOLEAN]
  /\ delivered \in [Records -> 1..2]
  /\ eff \in [Records -> [Effects -> Statuses]]
  /\ sendCount \in [Records -> [Effects -> 0..2]]
  /\ receipt \in [Records -> Ids \cup {"none"}]
  /\ drift \in [Records -> BOOLEAN]
  /\ head \in [Records -> {"h0", "h1"}]
  /\ revalidated \in [Records -> BOOLEAN]
  /\ checked \in [Records -> BOOLEAN]
  /\ reported \in [Records -> {"none", "success", "failure"}]

Init ==
  /\ phase = [r \in Records |-> "init"]
  /\ binding = [r \in Records |-> "none"]
  /\ requests = {}
  /\ claimed = {}
  /\ allows = [r \in Records |-> 0]
  /\ decision = [r \in Records |-> "none"]
  /\ expired = [r \in Records |-> FALSE]
  /\ delivered = [r \in Records |-> 1]
  /\ eff = [r \in Records |-> [i \in Effects |-> "pending"]]
  /\ sendCount = [r \in Records |-> [i \in Effects |-> 0]]
  /\ receipt = [r \in Records |-> "none"]
  /\ sends = {}
  /\ drift = [r \in Records |-> FALSE]
  /\ head = [r \in Records |-> "h0"]
  /\ revalidated = [r \in Records |-> FALSE]
  /\ checked = [r \in Records |-> FALSE]
  /\ reported = [r \in Records |-> "none"]

RequestCount(r) == Cardinality({q \in requests : q.rec = r})

\* Staged previews branch before any authority interaction. Only a trusted
\* adapter capability selects resolved_effect; custom code cannot assert it.
Prepare(r) ==
  /\ phase[r] = "init"
  /\ IF Staged
       THEN /\ phase' = [phase EXCEPT ![r] = "staged"]
            /\ UNCHANGED binding
       ELSE /\ phase' = [phase EXCEPT ![r] = "planned"]
            /\ \E b \in {"invocation", "resolved_effect"} :
                 /\ IF Route(r) = "adapter" THEN b = "resolved_effect"
                    ELSE b = "invocation" \/ Mutation = "forged-promotion"
                 /\ binding' = [binding EXCEPT ![r] = b]
  /\ UNCHANGED <<requests, claimed, allows, decision, expired, delivered, eff,
                 sendCount, receipt, sends, drift, head, revalidated, checked, reported>>

\* Exactly one authorization request for the frozen plan.
Request(r) ==
  /\ phase[r] = "planned"
  /\ RequestCount(r) < 3
  /\ requests' = requests \cup {[rec |-> r, binding |-> binding[r], n |-> RequestCount(r) + 1]}
  /\ phase' = [phase EXCEPT ![r] = "requested"]
  /\ UNCHANGED <<binding, claimed, allows, decision, expired, delivered, eff,
                 sendCount, receipt, sends, drift, head, revalidated, checked, reported>>

\* The authority atomically claims the logical invocation. It may require
\* resolved_effect and may deny for any policy reason.
Decide(r) ==
  /\ phase[r] = "requested"
  /\ \E d \in {"allow", "deny"} :
       /\ d = "allow" =>
            /\ (r \notin claimed \/ Mutation = "no-claim")
            /\ (RequireResolved => binding[r] = "resolved_effect")
       /\ decision' = [decision EXCEPT ![r] = d]
       /\ allows' = [allows EXCEPT ![r] = IF d = "allow" THEN @ + 1 ELSE @]
       /\ IF d = "allow"
            THEN phase' = [phase EXCEPT ![r] = "running"] /\ UNCHANGED reported
            \* A denied resolved request is never retried at a weaker binding.
            ELSE IF Mutation = "downgrade" /\ binding[r] = "resolved_effect"
                   THEN /\ phase' = [phase EXCEPT ![r] = "planned"]
                        /\ UNCHANGED reported
                   ELSE /\ phase' = [phase EXCEPT ![r] = "done"]
                        /\ reported' = [reported EXCEPT ![r] = "failure"]
  /\ claimed' = claimed \cup {r}
  /\ binding' = IF Mutation = "downgrade" THEN [binding EXCEPT ![r] = "invocation"] ELSE binding
  /\ UNCHANGED <<requests, expired, delivered, eff, sendCount, receipt, sends,
                 drift, head, revalidated, checked>>

\* Hostile environment.
Drift(r) == /\ ~drift[r] /\ drift' = [drift EXCEPT ![r] = TRUE]
            /\ UNCHANGED <<phase, binding, requests, claimed, allows, decision, expired,
                           delivered, eff, sendCount, receipt, sends, head, revalidated, checked, reported>>
MoveHead(r) == /\ head[r] = "h0" /\ head' = [head EXCEPT ![r] = "h1"]
               /\ UNCHANGED <<phase, binding, requests, claimed, allows, decision, expired,
                              delivered, eff, sendCount, receipt, sends, drift, revalidated, checked, reported>>
Expire(r) == /\ decision[r] = "allow" /\ ~expired[r] /\ expired' = [expired EXCEPT ![r] = TRUE]
             /\ UNCHANGED <<phase, binding, requests, claimed, allows, decision,
                            delivered, eff, sendCount, receipt, sends, drift, head, revalidated, checked, reported>>

Next1(r) == IF eff[r][1] = "pending" THEN 1 ELSE 2
Applied(r) == {i \in Effects : eff[r][i] = "confirmed"}
Stopped(r) == \E i \in Effects : eff[r][i] \in {"rejected", "indeterminate", "blocked"}

\* Observed preconditions are rechecked before every send.
Revalidate(r) ==
  /\ phase[r] = "running" /\ ~revalidated[r] /\ ~Stopped(r)
  /\ Applied(r) # Effects
  /\ IF drift[r] /\ Mutation # "no-revalidate"
       THEN /\ eff' = [eff EXCEPT ![r][Next1(r)] = "blocked"]
            /\ UNCHANGED revalidated
       ELSE /\ revalidated' = [revalidated EXCEPT ![r] = TRUE]
            /\ UNCHANGED eff
  /\ checked' = [checked EXCEPT ![r] = ~drift[r]]
  /\ UNCHANGED <<phase, binding, requests, claimed, allows, decision, expired,
                 delivered, sendCount, receipt, sends, drift, head, reported>>

\* The adapter-owned transport verifies the final wire request, checks
\* freshness, sends once, and refuses SDK retries. The target enforces only the
\* atomic lease on effect 1.
Send(r) ==
  LET i == Next1(r) IN
  /\ phase[r] = "running" /\ revalidated[r] /\ ~Stopped(r)
  /\ eff[r][i] = "pending"
  /\ i = 2 => eff[r][1] = "confirmed"
  /\ \E wire \in Payloads :
       LET planned == IF i = 1 THEN "planned" ELSE receipt[r]
           sourced == IF i = 2 /\ Mutation = "unbound-derive" THEN "message" ELSE planned
           verified == Mutation # "no-wire-check" => wire = sourced
           fresh == ~expired[r] \/ Mutation = "no-freshness"
       IN
       /\ wire \in {sourced, "tampered"}
       /\ IF ~verified \/ ~fresh
            THEN /\ eff' = [eff EXCEPT ![r][i] = "blocked"]
                 /\ UNCHANGED <<sendCount, receipt, sends>>
            ELSE /\ sendCount' = [sendCount EXCEPT ![r][i] = @ + 1]
                 /\ \E outcome \in {"confirmed", "rejected", "indeterminate"} :
                      LET leased == i = 1 /\ Mutation # "no-lease"
                          appliedOk == leased => head[r] = "h0"
                          result == IF ~appliedOk THEN "rejected" ELSE outcome
                      IN
                      /\ eff' = [eff EXCEPT ![r][i] = result]
                      /\ sends' = sends \cup {[rec |-> r, i |-> i, wire |-> wire, planned |-> planned,
                                               fresh |-> ~expired[r], allowed |-> decision[r] = "allow",
                                               drifted |-> drift[r], checked |-> checked[r], head |-> head[r], leased |-> leased,
                                               applied |-> result \in {"confirmed", "indeterminate"}]}
                      /\ \E id \in Ids : receipt' = IF i = 1 /\ result = "confirmed" THEN [receipt EXCEPT ![r] = id] ELSE receipt
  /\ revalidated' = [revalidated EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<phase, binding, requests, claimed, allows, decision, expired,
                 delivered, drift, head, checked, reported>>

\* Negative control: an SDK retry replays an indeterminate request.
Retry(r) ==
  /\ Mutation = "retry"
  /\ \E i \in Effects :
       /\ eff[r][i] = "indeterminate" /\ sendCount[r][i] = 1
       /\ sendCount' = [sendCount EXCEPT ![r][i] = 2]
       /\ eff' = [eff EXCEPT ![r][i] = "confirmed"]
  /\ UNCHANGED <<phase, binding, requests, claimed, allows, decision, expired,
                 delivered, receipt, sends, drift, head, revalidated, checked, reported>>

Finish(r) ==
  /\ phase[r] = "running"
  /\ Applied(r) = Effects \/ Stopped(r)
  /\ phase' = [phase EXCEPT ![r] = "done"]
  /\ reported' = [reported EXCEPT ![r] =
        IF Applied(r) = Effects \/ (Mutation = "partial-success" /\ Applied(r) # {}) THEN "success" ELSE "failure"]
  /\ UNCHANGED <<binding, requests, claimed, allows, decision, expired, delivered,
                 eff, sendCount, receipt, sends, drift, head, revalidated, checked>>

\* Redelivery of the same record keeps its logical invocation identity and
\* the target state produced by the earlier delivery.
Redeliver(r) ==
  /\ phase[r] = "done" /\ delivered[r] = 1
  /\ delivered' = [delivered EXCEPT ![r] = 2]
  /\ phase' = [phase EXCEPT ![r] = "init"]
  /\ decision' = [decision EXCEPT ![r] = "none"]
  /\ revalidated' = [revalidated EXCEPT ![r] = FALSE]
  /\ expired' = [expired EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<binding, requests, claimed, allows, eff, sendCount, receipt, sends, drift, head, checked, reported>>

Next ==
  \E r \in Records :
    \/ Prepare(r) \/ Request(r) \/ Decide(r) \/ Revalidate(r) \/ Send(r)
    \/ Retry(r) \/ Finish(r) \/ Redeliver(r)
    \/ Drift(r) \/ MoveHead(r) \/ Expire(r)

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
\* Safety properties.

\* At most one consuming request and one allow per logical invocation, even
\* across redelivery; a later request is a refused duplicate.
SingleDecision == \A r \in Records : allows[r] <= 1

\* Adapter routes never request, or retry at, invocation binding.
NoDowngrade == \A q \in requests : Route(q.rec) = "adapter" => q.binding = "resolved_effect"

\* Custom code is never relabeled as an exact effect.
HonestBinding == \A q \in requests : Route(q.rec) = "custom" => q.binding = "invocation"

\* Every transmitted request is exactly the approved plan entry; derived
\* values come only from the receipt of the earlier effect.
SentEqualsPlan == \A s \in sends : s.wire = s.planned /\ (s.i = 2 => s.planned \in Ids)

\* Sends require a live, unexpired permission and happen at most once each.
FreshPermission == \A s \in sends : s.allowed /\ s.fresh
SendOnce == \A r \in Records, i \in Effects : sendCount[r][i] <= 1

\* The leased effect is never applied over a moved ref.
AtomicLease == \A s \in sends : (s.i = 1 /\ s.applied) => s.head = "h0"

\* Every send follows a revalidation that observed the preconditions intact.
RevalidatedSend == \A s \in sends : s.checked

\* Partial or uncertain execution is never reported as success.
HonestOutcome == \A r \in Records : reported[r] = "success" => Applied(r) = Effects

\* Staged previews make no authority request and no effect.
StagedSilent == Staged => (requests = {} /\ sends = {})

Safety == SingleDecision /\ NoDowngrade /\ HonestBinding /\ SentEqualsPlan /\ FreshPermission
          /\ SendOnce /\ AtomicLease /\ RevalidatedSend /\ HonestOutcome /\ StagedSilent

-----------------------------------------------------------------------------
\* Witnesses: each must be violated, proving the scenario is reachable.
NoCompletePlan == \A r \in Records : ~(Route(r) = "adapter" /\ reported[r] = "success")
NoPartialFailure == \A r \in Records : ~(reported[r] = "failure" /\ eff[r][1] = "confirmed" /\ eff[r][2] # "confirmed")
\* Observed preconditions are not atomic: drift after the last revalidation
\* but before the send is a documented residual race.
NoResidualRace == \A s \in sends : ~s.drifted
=============================================================================
