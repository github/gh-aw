-------------------------- MODULE WorkerEvolution --------------------------
EXTENDS Naturals, FiniteSets, TLC

\* Bounded refinement of contract-marked AW Deployment records, not a host,
\* compiler hash, Git CAS, packing or scheduling-fairness implementation proof.
\* Registered revision profiles are immutable; availability may change.
\* Work admission_contract and optional execution_ref never change. Dispatch
\* profile and the actual credential principal freeze independently of routes.
CONSTANTS UpdateBound, ProfilePrincipal, Broken
ASSUME /\ UpdateBound \in 1..3 /\ ProfilePrincipal \in BOOLEAN
       /\ Broken \in {"none", "stale-cas", "revision-rewrite", "pin",
                      "incompatible", "unavailable", "global-pause",
                      "debt", "result", "reservation", "dispatch",
                      "credential", "economics"}

VARIABLES current, registered, available, contracts, updates, prepared,
          expectedRef, expectedContract, status, dispatchRef, originalRef,
          dispatchContract, credential, originalCredential, reservations,
          expectedReservations, debt, charged, results, expectedResults,
          terminal, policyConcurrency, violation
vars == <<current, registered, available, contracts, updates, prepared,
          expectedRef, expectedContract, status, dispatchRef, originalRef,
          dispatchContract, credential, originalCredential, reservations,
          expectedReservations, debt, charged, results, expectedResults,
          terminal, policyConcurrency, violation>>
None == "none"
Works == {"unpinned", "pinned", "other"}
Refs == {"r0", "r1", "r2", "y0"}
WorkerRefs == {"r0", "r1", "r2"}
Principals == {"p1", "p2"}
Contract(r) == IF r = "r2" THEN "c1" ELSE "c0"
AdmissionContract(w) == "c0"
Pin(w) == IF w = "pinned" THEN "r0" ELSE None
Route(w) == IF w = "other" THEN "y0"
            ELSE IF Pin(w) # None /\ Broken # "pin" THEN Pin(w)
            ELSE current
LocalRouteReadiness(w) ==
  LET r == Route(w)
  IN IF r \notin registered \/ ~available[r] THEN "worker_unavailable"
     ELSE IF contracts[r] # AdmissionContract(w) THEN "worker_incompatible"
     ELSE "ready"
Readiness(w) == IF Broken = "global-pause" /\ w = "other" /\
                  LocalRouteReadiness("unpinned") # "ready"
               THEN "worker_unavailable" ELSE LocalRouteReadiness(w)
Eligible(w) == status[w] = "pending" /\ Readiness(w) = "ready"
TrustedPrincipal(p) == p \in Principals /\ (~ProfilePrincipal \/ p = "p1")

Init ==
  /\ current = "r0" /\ registered = {"r0", "y0"}
  /\ available = [r \in Refs |-> TRUE]
  /\ contracts = [r \in Refs |-> Contract(r)]
  /\ updates = 0 /\ prepared = FALSE
  /\ expectedRef = None /\ expectedContract = None
  /\ status = [w \in Works |-> "pending"]
  /\ dispatchRef = [w \in Works |-> None] /\ originalRef = dispatchRef
  /\ dispatchContract = [w \in Works |-> None]
  /\ credential = [w \in Works |-> None] /\ originalCredential = credential
  /\ reservations = {} /\ expectedReservations = {}
  /\ debt = [w \in Works |-> 0] /\ charged = {}
  /\ results = [w \in Works |-> None] /\ expectedResults = results
  /\ terminal = {} /\ policyConcurrency = 16 /\ violation = None

PrepareDeployment ==
  /\ ~prepared /\ updates < UpdateBound
  /\ prepared' = TRUE
  /\ expectedRef' = current /\ expectedContract' = contracts[current]
  /\ UNCHANGED <<current, registered, available, contracts, updates, status,
                 dispatchRef, originalRef, dispatchContract, credential,
                 originalCredential, reservations, expectedReservations,
                 debt, charged, results, expectedResults, terminal,
                 policyConcurrency, violation>>

Deploy(ref, isAvailable, concurrent) ==
  /\ ref \in WorkerRefs /\ isAvailable \in BOOLEAN
  /\ updates < UpdateBound
  /\ IF concurrent THEN TRUE
     ELSE prepared /\ (Broken = "stale-cas" \/
                       (current = expectedRef /\
                        contracts[current] = expectedContract))
  \* "concurrent" is another checked publisher's current-HEAD Deployment.
  \* It can advance the route after the modeled writer prepared its CAS.
  /\ current' = ref /\ registered' = registered \cup {ref}
  /\ available' = [available EXCEPT ![ref] = isAvailable]
  /\ updates' = updates + 1
  /\ prepared' = IF concurrent THEN prepared ELSE FALSE
  /\ violation' = IF ~concurrent /\ (current # expectedRef \/
                                    contracts[current] # expectedContract)
                    THEN "stale-cas"
                 ELSE violation
  /\ contracts' = IF Broken = "revision-rewrite"
                  THEN [contracts EXCEPT !["r0"] = "c1"] ELSE contracts
  /\ dispatchRef' = IF Broken = "dispatch"
                    THEN [w \in Works |-> IF w # "other" /\
                          dispatchRef[w] # None THEN ref ELSE dispatchRef[w]]
                    ELSE dispatchRef
  /\ credential' = IF Broken = "credential" /\ ~ProfilePrincipal
                   THEN [w \in Works |-> IF credential[w] = "p1"
                         THEN "p2" ELSE credential[w]] ELSE credential
  /\ debt' = IF Broken = "debt" /\ status["unpinned"] = "pending"
             THEN [debt EXCEPT !["unpinned"] = 1] ELSE debt
  /\ results' = IF Broken = "result"
                THEN [w \in Works |-> IF results[w] # None
                      THEN ref ELSE results[w]] ELSE results
  /\ reservations' = IF Broken = "reservation" THEN {} ELSE reservations
  /\ policyConcurrency' = IF Broken = "economics" THEN 17 ELSE policyConcurrency
  /\ UNCHANGED <<expectedRef, expectedContract, status, originalRef,
                 dispatchContract, originalCredential, expectedReservations,
                 charged, expectedResults, terminal>>

RefreshDeployment ==
  /\ prepared /\ updates < UpdateBound
  /\ current # expectedRef \/ contracts[current] # expectedContract
  /\ expectedRef' = current /\ expectedContract' = contracts[current]
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 status, dispatchRef, originalRef, dispatchContract,
                 credential, originalCredential, reservations,
                 expectedReservations, debt, charged, results,
                 expectedResults, terminal, policyConcurrency, violation>>

Grant(w) ==
  /\ status[w] = "pending"
  /\ \/ Readiness(w) = "ready"
     \/ Broken = "incompatible" /\ Readiness(w) = "worker_incompatible"
     \/ Broken = "unavailable" /\ Readiness(w) = "worker_unavailable"
  /\ status' = [status EXCEPT ![w] = "reserved"]
  /\ dispatchRef' = [dispatchRef EXCEPT ![w] = Route(w)]
  /\ originalRef' = [originalRef EXCEPT ![w] =
                     IF w = "pinned" THEN "r0" ELSE Route(w)]
  /\ dispatchContract' = [dispatchContract EXCEPT ![w] = contracts[Route(w)]]
  /\ reservations' = reservations \cup {w}
  /\ expectedReservations' = expectedReservations \cup {w}
  /\ debt' = [debt EXCEPT ![w] = @ + 1] /\ charged' = charged \cup {w}
  /\ violation' = IF Readiness(w) # "ready" THEN Readiness(w) ELSE violation
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 expectedRef, expectedContract, credential, originalCredential,
                 results, expectedResults, terminal, policyConcurrency>>

Start(w, principal) ==
  \* AW validates the actual credential identity at native dispatch, outside
  \* the queue. The optional profile principal adds a matching constraint.
  /\ status[w] = "reserved" /\ TrustedPrincipal(principal)
  /\ status' = [status EXCEPT ![w] = "running"]
  /\ credential' = [credential EXCEPT ![w] = principal]
  /\ originalCredential' = [originalCredential EXCEPT ![w] = principal]
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 expectedRef, expectedContract, dispatchRef, originalRef,
                 dispatchContract, reservations, expectedReservations, debt,
                 charged, results, expectedResults, terminal,
                 policyConcurrency, violation>>

Finish(w, observedRef, principal) ==
  \* Completion authenticates against the frozen Dispatch, never current route.
  /\ status[w] = "running" /\ observedRef = dispatchRef[w]
  /\ principal = credential[w]
  /\ status' = [status EXCEPT ![w] = "completed"]
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 expectedRef, expectedContract, dispatchRef, originalRef,
                 dispatchContract, credential, originalCredential,
                 reservations, expectedReservations, debt, charged, results,
                 expectedResults, terminal, policyConcurrency, violation>>

Deliver(w) ==
  \* Trusted effect receipt/readback abstracts verification, not worker claims.
  /\ status[w] = "completed" /\ results[w] = None
  /\ results' = [results EXCEPT ![w] = dispatchRef[w]]
  /\ expectedResults' = [expectedResults EXCEPT ![w] = originalRef[w]]
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 expectedRef, expectedContract, status, dispatchRef, originalRef,
                 dispatchContract, credential, originalCredential,
                 reservations, expectedReservations, debt, charged, terminal,
                 policyConcurrency, violation>>

ObserveTerminal(w) ==
  /\ status[w] = "completed" /\ w \notin terminal
  /\ terminal' = terminal \cup {w}
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 expectedRef, expectedContract, status, dispatchRef, originalRef,
                 dispatchContract, credential, originalCredential,
                 reservations, expectedReservations, debt, charged, results,
                 expectedResults, policyConcurrency, violation>>

Release(w) ==
  /\ w \in reservations /\ w \in terminal /\ results[w] # None
  /\ reservations' = reservations \ {w}
  /\ expectedReservations' = expectedReservations \ {w}
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 expectedRef, expectedContract, status, dispatchRef, originalRef,
                 dispatchContract, credential, originalCredential, debt,
                 charged, results, expectedResults, terminal,
                 policyConcurrency, violation>>

Next == PrepareDeployment \/ RefreshDeployment
        \/ (\E ref \in WorkerRefs, isAvailable \in BOOLEAN, concurrent \in BOOLEAN :
              Deploy(ref, isAvailable, concurrent))
        \/ (\E w \in Works : Grant(w) \/ Deliver(w) \/ ObserveTerminal(w) \/ Release(w))
        \/ (\E w \in Works, p \in Principals : Start(w, p))
        \/ (\E w \in Works, r \in Refs, p \in Principals : Finish(w, r, p))
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ current \in WorkerRefs /\ registered \subseteq Refs
  /\ available \in [Refs -> BOOLEAN] /\ contracts \in [Refs -> {"c0", "c1"}]
  /\ updates \in 0..UpdateBound /\ prepared \in BOOLEAN
  /\ expectedRef \in WorkerRefs \cup {None}
  /\ expectedContract \in {"c0", "c1", None}
  /\ status \in [Works -> {"pending", "reserved", "running", "completed"}]
  /\ dispatchRef \in [Works -> Refs \cup {None}]
  /\ originalRef \in [Works -> Refs \cup {None}]
  /\ dispatchContract \in [Works -> {"c0", "c1", None}]
  /\ credential \in [Works -> Principals \cup {None}]
  /\ originalCredential \in [Works -> Principals \cup {None}]
  /\ reservations \subseteq Works /\ expectedReservations \subseteq Works
  /\ debt \in [Works -> 0..1] /\ charged \subseteq Works
  /\ results \in [Works -> Refs \cup {None}]
  /\ expectedResults \in [Works -> Refs \cup {None}]
  /\ terminal \subseteq Works /\ policyConcurrency \in {16, 17}
  /\ violation \in {None, "stale-cas",
                    "worker_incompatible", "worker_unavailable"}
DeploymentCAS == violation # "stale-cas"
RevisionImmutability == \A r \in registered : contracts[r] = Contract(r)
PinnedExecution == dispatchRef["pinned"] \in {None, "r0"}
LocalReadiness == \A w \in Works : status[w] = "pending" =>
                   Readiness(w) = LocalRouteReadiness(w)
ReadyGrants == violation \notin {"worker_incompatible", "worker_unavailable"}
FrozenDispatch == dispatchRef = originalRef
FrozenCredential == credential = originalCredential
ContractPreservation == \A w \in charged : dispatchContract[w] = AdmissionContract(w)
DebtPreservation == \A w \in Works : debt[w] = IF w \in charged THEN 1 ELSE 0
ResultPreservation == results = expectedResults
ReservationPreservation == reservations = expectedReservations
EconomicPolicy == policyConcurrency = 16
Safety == TypeOK /\ DeploymentCAS /\ RevisionImmutability /\ PinnedExecution
          /\ LocalReadiness /\ ReadyGrants /\ FrozenDispatch /\ FrozenCredential
          /\ ContractPreservation /\ DebtPreservation /\ ResultPreservation
          /\ ReservationPreservation /\ EconomicPolicy

\* False invariants are reachability controls, not eventual-service guarantees.
NoCompatibleReroute == ~(dispatchRef["unpinned"] = "r1")
NoPinnedOldRevision == ~(current = "r1" /\ dispatchRef["pinned"] = "r0")
NoLocalIncompatibility == ~(current = "r2" /\ status["unpinned"] = "pending"
                           /\ Readiness("unpinned") = "worker_incompatible"
                           /\ status["other"] = "running"
                           /\ debt["unpinned"] = 0)
NoLocalUnavailability == ~(~available[current] /\ status["unpinned"] = "pending"
                          /\ Readiness("unpinned") = "worker_unavailable"
                          /\ status["other"] = "running"
                          /\ debt["unpinned"] = 0)
NoFrozenCompletion == ~(current = "r2" /\ dispatchRef["unpinned"] = "r0"
                       /\ status["unpinned"] = "completed"
                       /\ results["unpinned"] = "r0"
                       /\ "unpinned" \notin reservations)
NoDeploymentRace == ~(prepared /\ current # expectedRef)
=============================================================================
