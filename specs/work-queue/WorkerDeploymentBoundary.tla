--------------------- MODULE WorkerDeploymentBoundary ---------------------
EXTENDS WorkerEvolution

\* Supplemental refinement of activate:false and protected per-call approval.
\* The original 35 configurations remain unchanged. Caller profiles model a
\* protected dispatch request input, not queue enrollment or installed Policy.
\* Authorized deployment roles are trusted host inputs; workers never synchronize
\* proposals. Native credential/effect authority still uses frozen Dispatch.
CONSTANT BoundaryBroken
ASSUME BoundaryBroken \in {"none", "availability-cas", "availability-route",
                          "worker-deploy", "caller-approval", "approval-debt"}
VARIABLES callerProfiles, boundaryViolation
boundaryVars == <<current, registered, available, contracts, updates, prepared,
                  expectedRef, expectedContract, status, dispatchRef, originalRef,
                  dispatchContract, credential, originalCredential, reservations,
                  expectedReservations, debt, charged, results, expectedResults,
                  terminal, policyConcurrency, violation, callerProfiles,
                  boundaryViolation>>
Profiles == {"X", "Y"}
Roles == {"administrator", "producer", "dispatcher", "worker"}
\* The three authorized roles induce identical state transitions.
RoleRepresentatives == {"dispatcher", "worker"}
Profile(w) == IF w = "other" THEN "Y" ELSE "X"
CallerApproved(w) == Profile(w) \in callerProfiles
DeploymentRole(role) == role \in Roles \ {"worker"}

BoundaryInit ==
  /\ Init
  /\ callerProfiles = Profiles /\ boundaryViolation = None

ApprovedGrant(w) ==
  /\ CallerApproved(w) \/ BoundaryBroken = "caller-approval"
  /\ Grant(w)
  /\ boundaryViolation' = IF ~CallerApproved(w) THEN "caller-approval"
                          ELSE boundaryViolation
  /\ UNCHANGED callerProfiles

AuthorizedDeploy(ref, isAvailable, concurrent, role) ==
  /\ DeploymentRole(role) \/ BoundaryBroken = "worker-deploy"
  /\ Deploy(ref, isAvailable, concurrent)
  /\ boundaryViolation' = IF role = "worker" THEN "worker-deploy"
                          ELSE boundaryViolation
  /\ UNCHANGED callerProfiles

AvailabilityOnly(ref, isAvailable, concurrent, role) ==
  /\ role \in Roles /\ (DeploymentRole(role) \/ BoundaryBroken = "worker-deploy")
  /\ ref \in registered \cap WorkerRefs /\ isAvailable \in BOOLEAN
  /\ updates < UpdateBound
  /\ IF concurrent THEN TRUE
     ELSE prepared /\ (BoundaryBroken = "availability-cas" \/
                       (current = expectedRef /\
                        contracts[current] = expectedContract))
  \* activate:false can only edit an existing revision's availability. Its CAS
  \* still names the current route/contract, not the historical revision edited.
  /\ current' = IF BoundaryBroken = "availability-route" THEN ref ELSE current
  /\ available' = [available EXCEPT ![ref] = isAvailable]
  /\ updates' = updates + 1
  /\ prepared' = IF concurrent THEN prepared ELSE FALSE
  /\ boundaryViolation' =
       IF role = "worker" THEN "worker-deploy"
       ELSE IF ~concurrent /\ (current # expectedRef \/
                               contracts[current] # expectedContract)
            THEN "availability-cas"
       ELSE IF current' # current THEN "availability-route"
       ELSE boundaryViolation
  /\ UNCHANGED <<registered, contracts, expectedRef, expectedContract, status,
                 dispatchRef, originalRef, dispatchContract, credential,
                 originalCredential, reservations, expectedReservations,
                 debt, charged, results, expectedResults, terminal,
                 policyConcurrency, violation, callerProfiles>>

NextCaller(approved) ==
  \* Starting another NEW dispatch request may change protected local approval,
  \* but cannot reset queue clocks or mutate an outstanding assignment.
  /\ approved \in SUBSET Profiles /\ approved # callerProfiles
  /\ callerProfiles' = approved
  /\ debt' = IF BoundaryBroken = "approval-debt"
             THEN [w \in Works |-> 0] ELSE debt
  /\ UNCHANGED <<current, registered, available, contracts, updates, prepared,
                 expectedRef, expectedContract, status, dispatchRef,
                 originalRef, dispatchContract, credential, originalCredential,
                 reservations, expectedReservations, charged, results,
                 expectedResults, terminal, policyConcurrency, violation,
                 boundaryViolation>>

BoundaryNext ==
  \/ /\ PrepareDeployment \/ RefreshDeployment
     /\ UNCHANGED <<callerProfiles, boundaryViolation>>
  \/ \E w \in Works : ApprovedGrant(w)
  \/ \E ref \in WorkerRefs, isAvailable \in BOOLEAN,
        concurrent \in BOOLEAN, role \in RoleRepresentatives :
        AuthorizedDeploy(ref, isAvailable, concurrent, role)
        \/ AvailabilityOnly(ref, isAvailable, concurrent, role)
  \/ \E approved \in SUBSET Profiles : NextCaller(approved)
  \/ /\ (\E w \in Works : Deliver(w) \/ ObserveTerminal(w) \/ Release(w))
         \/ (\E w \in Works, p \in Principals : Start(w, p))
         \/ (\E w \in Works, r \in Refs, p \in Principals : Finish(w, r, p))
     /\ UNCHANGED <<callerProfiles, boundaryViolation>>
BoundarySpec == BoundaryInit /\ [][BoundaryNext]_boundaryVars

BoundaryType == callerProfiles \subseteq Profiles /\
                boundaryViolation \in {None, "worker-deploy", "caller-approval",
                                       "availability-cas", "availability-route"}
NoWorkerDeployment == boundaryViolation # "worker-deploy"
CallerApprovalAuthority == boundaryViolation # "caller-approval"
HistoricalAvailabilityCAS == boundaryViolation # "availability-cas"
HistoricalRoutePreservation == boundaryViolation # "availability-route"
BoundarySafety == Safety /\ BoundaryType /\ NoWorkerDeployment
                  /\ CallerApprovalAuthority /\ HistoricalAvailabilityCAS
                  /\ HistoricalRoutePreservation

\* These guarded witnesses remain reachability evidence, not liveness.
NoPinnedAvailabilityUpdate == ~(current = "r1" /\ ~available["r0"]
                               /\ status["pinned"] = "pending"
                               /\ LocalRouteReadiness("pinned") = "worker_unavailable"
                               /\ LocalRouteReadiness("unpinned") = "ready"
                               /\ debt["pinned"] = 0)
NoUnapprovedLocalPause == ~(callerProfiles = {"Y"} /\
                           status["unpinned"] = "pending" /\
                           ~CallerApproved("unpinned") /\ debt["unpinned"] = 0 /\
                           status["other"] = "running")
NoFrozenCallerCompletion == ~(callerProfiles = {"Y"} /\ current = "r2"
                             /\ dispatchRef["unpinned"] = "r0"
                             /\ status["unpinned"] = "completed"
                             /\ results["unpinned"] = "r0"
                             /\ "unpinned" \notin reservations)
=============================================================================
