--------------------------- MODULE QueueEvolution ---------------------------
EXTENDS Naturals, FiniteSets, TLC

\* Bounded deployment/configuration refinement; native evidence, contract
\* verification and the protected deployment source are trusted abstractions.
\* Each Work gets at most one single-Claim dispatch. QueueLifecycle covers
\* uncertain launches, cancellation, shared assignments and delivery failure.
CONSTANTS WorkSet, EvolutionMode, LowerCapacity, Broken
ASSUME /\ WorkSet \subseteq {"a", "b", "p", "x", "u"} /\ WorkSet # {}
       /\ EvolutionMode \in {"compatible", "incompatible", "unverified"}
       /\ LowerCapacity \in BOOLEAN

VARIABLES deployment, policyRevision, limit, head, candidate, grants,
          dispatches, nativePhase, bindings, workState, receipts, results,
          debt, acknowledged, restarted, halted, staleDiscarded,
          oldDeliveryRecovered

vars == <<deployment, policyRevision, limit, head, candidate, grants,
          dispatches, nativePhase, bindings, workState, receipts, results,
          debt, acknowledged, restarted, halted, staleDiscarded,
          oldDeliveryRecovered>>
None == "none"
NoCandidate == [work |-> None, ref |-> None, policy |-> 0, tip |-> 0]
EmptyExecution ==
  [workflow |-> None, ref |-> None, contract |-> 0, scope |-> 0,
   policy |-> 0, source_version |-> 0]
Refs == {"r1", "r2", "other", "untrusted"}
Order(w) == CASE w = "a" -> 1 [] w = "b" -> 2 [] w = "p" -> 3
                 [] w = "x" -> 4 [] OTHER -> 5
Workflow(w) == IF w = "u" THEN "other" ELSE "worker"
Contract(w) == IF w = "x" THEN 2 ELSE 1
Ceiling(w) == 1
Pinned(w) == w = "p"
ResolvedRef(w) ==
  IF Workflow(w) = "other" THEN "other"
  ELSE IF Pinned(w) THEN "r1"
  ELSE IF deployment = 1 THEN "r1" ELSE "r2"
SupportedContract(ref) ==
  IF ref = "r2" /\ EvolutionMode = "incompatible" THEN 2 ELSE 1
Verified(ref) ==
  ref # "untrusted"
  /\ ~(ref = "r2" /\ EvolutionMode = "unverified")
Compatible(w) ==
  Verified(ResolvedRef(w)) /\ SupportedContract(ResolvedRef(w)) = Contract(w)
BlockedReason(w) ==
  IF ~Verified(ResolvedRef(w)) THEN "deployment_unverified"
  ELSE IF ~Compatible(w) THEN "execution_contract_incompatible"
  ELSE None
Eligible == {w \in WorkSet : workState[w] = "queued" /\ Compatible(w)}
Winners ==
  {w \in Eligible : \A v \in Eligible :
       Workflow(v) = Workflow(w) => Order(w) <= Order(v)}
Assigned == {w \in WorkSet : grants[w] # EmptyExecution}
Outstanding == {w \in Assigned : nativePhase[w] # "released"}
Authority(w) ==
  w \in Assigned /\ bindings[w] = dispatches[w].ref
  /\ nativePhase[w] \in {"running", "terminated"}
  /\ (Broken # "retire-claim" \/ dispatches[w].policy = policyRevision)
Execution(w, ref, revision) ==
  [workflow |-> Workflow(w), ref |-> ref, contract |-> Contract(w),
   scope |-> IF Broken = "expand-scope" THEN 2 ELSE Ceiling(w),
   policy |-> revision, source_version |-> deployment]

Init ==
  /\ deployment = 1 /\ policyRevision = 1 /\ limit = 2 /\ head = 0
  /\ candidate = NoCandidate
  /\ grants = [w \in WorkSet |-> EmptyExecution]
  /\ dispatches = [w \in WorkSet |-> EmptyExecution]
  /\ nativePhase = [w \in WorkSet |-> "idle"]
  /\ bindings = [w \in WorkSet |-> None]
  /\ workState = [w \in WorkSet |-> "queued"]
  /\ receipts = [w \in WorkSet |-> None] /\ results = {}
  /\ debt = 0 /\ acknowledged = {}
  /\ restarted = FALSE /\ halted = FALSE /\ staleDiscarded = FALSE
  /\ oldDeliveryRecovered = FALSE

MoveDeployment ==
  /\ deployment = 1
  /\ Broken # "drain-only" \/ Outstanding = {}
  /\ deployment' = 2 /\ head' = head + 1
  /\ dispatches' =
       IF Broken = "rewrite-reservation"
       THEN [w \in WorkSet |->
              IF w \in Assigned /\ Workflow(w) = "worker"
              THEN [dispatches[w] EXCEPT !.ref = "r2"] ELSE dispatches[w]]
       ELSE dispatches
  /\ halted' = IF Broken = "global-block" /\ EvolutionMode # "compatible"
               THEN TRUE ELSE halted
  /\ results' = IF Broken = "invalidate-results" THEN {} ELSE results
  /\ UNCHANGED <<policyRevision, limit, candidate, grants, nativePhase,
                 bindings, workState, receipts, debt, acknowledged,
                 restarted, staleDiscarded, oldDeliveryRecovered>>

UpdatePolicy ==
  /\ policyRevision = 1
  /\ policyRevision' = 2 /\ head' = head + 1
  /\ limit' = IF LowerCapacity THEN 1 ELSE limit
  /\ debt' = IF Broken = "reset-debt" THEN 0 ELSE debt
  /\ nativePhase' =
       IF Broken = "evict-reservation" /\ LowerCapacity
       THEN [w \in WorkSet |-> IF w \in Outstanding THEN "released"
                               ELSE nativePhase[w]]
       ELSE nativePhase
  /\ UNCHANGED <<deployment, candidate, grants, dispatches, bindings,
                 workState, receipts, results, acknowledged, restarted,
                 halted, staleDiscarded, oldDeliveryRecovered>>

Propose(w) ==
  /\ candidate = NoCandidate /\ w \in Winners /\ ~halted
  /\ Cardinality(Outstanding) < limit
  /\ candidate' = [work |-> w, ref |-> ResolvedRef(w),
                   policy |-> policyRevision, tip |-> head]
  /\ UNCHANGED <<deployment, policyRevision, limit, head, grants, dispatches,
                 nativePhase, bindings, workState, receipts, results, debt,
                 acknowledged, restarted, halted, staleDiscarded,
                 oldDeliveryRecovered>>

DiscardStale ==
  /\ candidate # NoCandidate /\ candidate.tip # head
  /\ candidate' = NoCandidate
  /\ staleDiscarded' =
       (staleDiscarded \/ (candidate.ref = "r1" /\ deployment = 2))
  /\ UNCHANGED <<deployment, policyRevision, limit, head, grants, dispatches,
                 nativePhase, bindings, workState, receipts, results, debt,
                 acknowledged, restarted, halted, oldDeliveryRecovered>>

Reserve ==
  /\ candidate # NoCandidate /\ ~halted
  /\ candidate.tip = head \/ Broken = "stale-cas"
  /\ LET w == candidate.work IN
       /\ w \in Winners /\ Cardinality(Outstanding) < limit
       /\ grants' = [grants EXCEPT
             ![w] = Execution(w, ResolvedRef(w), policyRevision)]
       /\ dispatches' = [dispatches EXCEPT
             ![w] = Execution(w, IF Broken = "untrusted-source" THEN "untrusted"
                                  ELSE candidate.ref, candidate.policy)]
       /\ nativePhase' = [nativePhase EXCEPT ![w] = "reserved"]
       /\ workState' = [workState EXCEPT ![w] = "claimed"]
  /\ head' = head + 1 /\ candidate' = NoCandidate /\ debt' = debt + 1
  /\ UNCHANGED <<deployment, policyRevision, limit, bindings, receipts,
                 results, acknowledged, restarted, halted, staleDiscarded,
                 oldDeliveryRecovered>>

Launch(w) ==
  /\ nativePhase[w] = "reserved"
  /\ bindings' = [bindings EXCEPT
       ![w] = IF Broken = "moving-launch" THEN ResolvedRef(w)
              ELSE dispatches[w].ref]
  /\ nativePhase' = [nativePhase EXCEPT ![w] = "running"]
  /\ head' = head + 1
  /\ UNCHANGED <<deployment, policyRevision, limit, candidate, grants,
                 dispatches, workState, receipts, results, debt, acknowledged,
                 restarted, halted, staleDiscarded, oldDeliveryRecovered>>

Complete(w) ==
  /\ nativePhase[w] = "running" /\ workState[w] = "claimed" /\ Authority(w)
  /\ workState' = [workState EXCEPT ![w] = "completed"] /\ head' = head + 1
  /\ UNCHANGED <<deployment, policyRevision, limit, candidate, grants,
                 dispatches, nativePhase, bindings, receipts, results, debt,
                 acknowledged, restarted, halted, staleDiscarded,
                 oldDeliveryRecovered>>

Receipt(w) ==
  /\ workState[w] = "completed" /\ Authority(w) /\ receipts[w] = None
  /\ receipts' = [receipts EXCEPT ![w] = bindings[w]] /\ head' = head + 1
  /\ UNCHANGED <<deployment, policyRevision, limit, candidate, grants,
                 dispatches, nativePhase, bindings, workState, results, debt,
                 acknowledged, restarted, halted, staleDiscarded,
                 oldDeliveryRecovered>>

VerifyResult(w) ==
  /\ workState[w] = "completed" /\ w \notin results
  /\ receipts[w] # None /\ receipts[w] = dispatches[w].ref
  /\ Broken # "latest-delivery" \/ receipts[w] = ResolvedRef(w)
  /\ results' = results \cup {w} /\ head' = head + 1
  /\ oldDeliveryRecovered' =
       (oldDeliveryRecovered \/
         (w = "a" /\ policyRevision = 2 /\ deployment = 2 /\ restarted
          /\ dispatches[w].ref = "r1"))
  /\ UNCHANGED <<deployment, policyRevision, limit, candidate, grants,
                 dispatches, nativePhase, bindings, workState, receipts, debt,
                 acknowledged, restarted, halted, staleDiscarded>>

Terminate(w) ==
  /\ nativePhase[w] = "running" /\ workState[w] = "completed"
  /\ nativePhase' = [nativePhase EXCEPT ![w] = "terminated"]
  /\ head' = head + 1
  /\ UNCHANGED <<deployment, policyRevision, limit, candidate, grants,
                 dispatches, bindings, workState, receipts, results, debt,
                 acknowledged, restarted, halted, staleDiscarded,
                 oldDeliveryRecovered>>

Release(w) ==
  /\ nativePhase[w] = "terminated"
  /\ nativePhase' = [nativePhase EXCEPT ![w] = "released"]
  /\ head' = head + 1
  /\ UNCHANGED <<deployment, policyRevision, limit, candidate, grants,
                 dispatches, bindings, workState, receipts, results, debt,
                 acknowledged, restarted, halted, staleDiscarded,
                 oldDeliveryRecovered>>

Acknowledge(w) ==
  /\ w \in Assigned \ acknowledged
  \* Recovering an ambiguous publication uses the already committed request.
  /\ acknowledged' = acknowledged \cup {w}
  /\ debt' = debt + IF Broken = "double-charge" THEN 1 ELSE 0
  /\ UNCHANGED <<deployment, policyRevision, limit, head, candidate, grants,
                 dispatches, nativePhase, bindings, workState, receipts,
                 results, restarted, halted, staleDiscarded,
                 oldDeliveryRecovered>>

Restart ==
  /\ ~restarted /\ Assigned # {}
  \* Abstract normalization/checkpoint round-trip, not a JSON codec proof.
  /\ dispatches' = grants
  /\ bindings' =
       IF Broken = "checkpoint-forgets-binding"
       THEN [w \in WorkSet |-> None] ELSE bindings
  /\ candidate' = NoCandidate /\ restarted' = TRUE
  /\ UNCHANGED <<deployment, policyRevision, limit, head, grants, nativePhase,
                 workState, receipts, results, debt, acknowledged, halted,
                 staleDiscarded, oldDeliveryRecovered>>

Next == MoveDeployment \/ UpdatePolicy \/ DiscardStale \/ Reserve \/ Restart
        \/ (\E w \in WorkSet : Propose(w) \/ Launch(w) \/ Complete(w)
             \/ Receipt(w) \/ VerifyResult(w) \/ Terminate(w) \/ Release(w)
             \/ Acknowledge(w))
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ deployment \in 1..2 /\ policyRevision \in 1..2 /\ limit \in 1..2
  /\ head \in Nat /\ debt \in Nat
  /\ (candidate = NoCandidate \/
       candidate \in [work : WorkSet, ref : Refs, policy : 1..2, tip : Nat])
  /\ grants \in [WorkSet -> {EmptyExecution} \cup
       [workflow : {"worker", "other"}, ref : Refs, contract : 1..2,
        scope : 1..2, policy : 1..2, source_version : 1..2]]
  /\ dispatches \in [WorkSet -> {EmptyExecution} \cup
       [workflow : {"worker", "other"}, ref : Refs, contract : 1..2,
        scope : 1..2, policy : 1..2, source_version : 1..2]]
  /\ nativePhase \in [WorkSet ->
       {"idle", "reserved", "running", "terminated", "released"}]
  /\ bindings \in [WorkSet -> Refs \cup {None}]
  /\ workState \in [WorkSet -> {"queued", "claimed", "completed"}]
  /\ receipts \in [WorkSet -> Refs \cup {None}]
  /\ results \subseteq WorkSet /\ acknowledged \subseteq Assigned
  /\ restarted \in BOOLEAN /\ halted \in BOOLEAN /\ staleDiscarded \in BOOLEAN
  /\ oldDeliveryRecovered \in BOOLEAN
FrozenExecution == \A w \in Assigned : dispatches[w] = grants[w]
DeploymentAuthority == \A w \in Assigned :
  Verified(dispatches[w].ref) /\ dispatches[w].ref # "untrusted"
  /\ dispatches[w].scope <= Ceiling(w)
  /\ dispatches[w].workflow = Workflow(w)
  /\ dispatches[w].contract = Contract(w)
BindingAuthority == \A w \in Assigned :
  nativePhase[w] \in {"running", "terminated", "released"} =>
       bindings[w] = grants[w].ref
RetainedAuthority == \A w \in Assigned :
  nativePhase[w] \in {"running", "terminated"} => Authority(w)
Accounting == debt = Cardinality(Assigned)
LocalBlocking == ~halted
ResultSoundness == \A w \in results :
  workState[w] = "completed" /\ receipts[w] = grants[w].ref
ReleaseEvidence == \A w \in Assigned :
  nativePhase[w] = "released" => workState[w] = "completed"
DeliveryAvailable == \A w \in Assigned :
  workState[w] = "completed" /\ receipts[w] # None /\ w \notin results
       => ENABLED VerifyResult(w)
DeploymentUpdateAvailable == deployment = 1 => ENABLED MoveDeployment
Safety == TypeOK /\ FrozenExecution /\ DeploymentAuthority /\ BindingAuthority
          /\ RetainedAuthority /\ Accounting /\ LocalBlocking
          /\ ResultSoundness /\ ReleaseEvidence /\ DeliveryAvailable
          /\ DeploymentUpdateAvailable

CapacityAdmission ==
  [][(\A w \in WorkSet :
      grants[w] = EmptyExecution /\ grants'[w] # EmptyExecution =>
        Cardinality(Outstanding) < limit)]_vars
NoDebtReset ==
  [][(policyRevision' # policyRevision \/ deployment' # deployment)
       => debt' = debt]_vars
NativeReleaseAuthority ==
  [][(\A w \in WorkSet :
     nativePhase[w] # "released" /\ nativePhase'[w] = "released" =>
       nativePhase[w] = "terminated")]_vars
ResultPersistence == [][results \subseteq results']_vars
EvolutionSafety ==
  CapacityAdmission /\ NoDebtReset /\ NativeReleaseAuthority /\ ResultPersistence

NoRollingOverlap ==
  ~("a" \in WorkSet /\ "b" \in WorkSet
    /\ nativePhase["a"] = "running" /\ nativePhase["b"] = "running"
    /\ dispatches["a"].ref = "r1" /\ dispatches["b"].ref = "r2"
    /\ policyRevision = 2)
NoPinnedEvolution ==
  ~("p" \in WorkSet /\ deployment = 2 /\ "p" \in results
    /\ dispatches["p"].ref = "r1" /\ dispatches["p"].source_version = 2)
NoLocalProgress ==
  ~("b" \in WorkSet /\ "u" \in WorkSet /\ deployment = 2
    /\ workState["b"] = "queued" /\ BlockedReason("b") # None
    /\ "u" \in results)
NoRetainedDelivery == ~oldDeliveryRecovered
NoStaleRecovery ==
  ~(staleDiscarded /\ "b" \in WorkSet /\ "b" \in Assigned
    /\ dispatches["b"].ref = "r2")
NoOverLimitRetention ==
  ~(LowerCapacity /\ policyRevision = 2
    /\ Cardinality(Outstanding) > limit /\ debt = Cardinality(Assigned))
=============================================================================
