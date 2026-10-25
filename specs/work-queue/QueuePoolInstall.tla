-------------------------- MODULE QueuePoolInstall --------------------------
EXTENDS Naturals, TLC

\* Bounded explicit additive PoolInstall for an existing AW-managed queue.
\* Host authorization, proposal validity and verified worker-route properties
\* are explicit trusted inputs; Git publication/CAS behavior is abstracted.
CONSTANTS ExistingPool, NewPool, CallerRole, HostAuthorized,
          ProposalPreservesPolicy, NewPoolValid, RoutesValid,
          RoutesImmutable, RepositoryScoped, RoutesApproved,
          RepositoryAllowlistPreserved, DefaultRevisionApproved, ExtendExistingPool

Roles == {"administrator", "producer", "dispatcher"}
Phases == {"requested", "route-check", "ready", "done", "rejected"}

VARIABLES installedPools, acceptedWork, policyEpoch, phase,
          routesVerified, publications, registeredProfiles, existingDeployment
vars == <<installedPools, acceptedWork, policyEpoch, phase,
          routesVerified, publications, registeredProfiles, existingDeployment>>

Init ==
    /\ ExistingPool # NewPool
    /\ CallerRole \in Roles
    /\ HostAuthorized \in BOOLEAN
    /\ ProposalPreservesPolicy \in BOOLEAN
    /\ NewPoolValid \in BOOLEAN
    /\ RoutesValid \in BOOLEAN
    /\ RoutesImmutable \in BOOLEAN
    /\ RepositoryScoped \in BOOLEAN
    /\ RoutesApproved \in BOOLEAN
    /\ RepositoryAllowlistPreserved \in BOOLEAN
    /\ DefaultRevisionApproved \in BOOLEAN
    /\ ExtendExistingPool \in BOOLEAN
    /\ installedPools = {ExistingPool}
    /\ acceptedWork = {"accepted"}
    /\ policyEpoch = 1
    /\ phase = "requested"
    /\ routesVerified = FALSE
    /\ publications = 0
    /\ registeredProfiles = {"existing-worker"}
    /\ existingDeployment = "frozen"

ValidateProposal ==
    /\ phase = "requested"
    /\ phase' = IF ProposalPreservesPolicy /\ NewPoolValid /\ RepositoryAllowlistPreserved
                 THEN "route-check" ELSE "rejected"
    /\ UNCHANGED <<installedPools, acceptedWork, policyEpoch,
                   routesVerified, publications, registeredProfiles, existingDeployment>>

VerifyRoutes ==
    /\ phase = "route-check"
    /\ routesVerified' = RoutesValid /\ RoutesImmutable /\ RepositoryScoped
                          /\ (RoutesApproved \/ DefaultRevisionApproved)
    /\ phase' = IF routesVerified' THEN "ready" ELSE "rejected"
    /\ UNCHANGED <<installedPools, acceptedWork, policyEpoch, publications,
                   registeredProfiles, existingDeployment>>

InstallPool ==
    /\ phase = "ready"
    /\ HostAuthorized
    /\ CallerRole \in Roles
    /\ routesVerified
    \* Replay independently checks authority, even for a direct ledger request.
    /\ (RoutesApproved \/ DefaultRevisionApproved)
    /\ ProposalPreservesPolicy /\ NewPoolValid /\ RepositoryAllowlistPreserved
    /\ publications = 0
    /\ installedPools' = IF ExtendExistingPool THEN installedPools
                         ELSE installedPools \cup {NewPool}
    /\ registeredProfiles' = registeredProfiles \cup {"new-worker"}
    /\ acceptedWork' = acceptedWork
    /\ policyEpoch' = policyEpoch
    /\ phase' = "done"
    /\ publications' = publications + 1
    /\ UNCHANGED <<routesVerified, existingDeployment>>

Next == ValidateProposal \/ VerifyRoutes \/ InstallPool
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ installedPools \subseteq {ExistingPool, NewPool}
    /\ acceptedWork = {"accepted"}
    /\ policyEpoch \in Nat
    /\ phase \in Phases
    /\ routesVerified \in BOOLEAN
    /\ publications \in 0..1
    /\ registeredProfiles \subseteq {"existing-worker", "new-worker"}
    /\ existingDeployment = "frozen"

ExistingPoolPreserved == ExistingPool \in installedPools
AcceptedWorkPreserved == acceptedWork = {"accepted"}
PolicyEpochPreserved == policyEpoch = 1
SingleAdditiveInstall ==
    /\ ("new-worker" \in registeredProfiles) = (publications = 1)
    /\ IF ExtendExistingPool THEN installedPools = {ExistingPool}
       ELSE (NewPool \in installedPools) = (publications = 1)
    /\ publications \in 0..1
PublicationAuthority ==
    publications = 0 \/
        /\ HostAuthorized
        /\ CallerRole \in Roles
        /\ routesVerified
        /\ ProposalPreservesPolicy
        /\ NewPoolValid
        /\ RoutesValid
        /\ RoutesImmutable
        /\ RepositoryScoped
        /\ (RoutesApproved \/ DefaultRevisionApproved)
        /\ RepositoryAllowlistPreserved
Safety ==
    /\ TypeOK
    /\ ExistingPoolPreserved
    /\ AcceptedWorkPreserved
    /\ PolicyEpochPreserved
    /\ SingleAdditiveInstall
    /\ PublicationAuthority
    /\ "existing-worker" \in registeredProfiles
    /\ existingDeployment = "frozen"

\* A deliberately false invariant demonstrates the producer path is reachable.
NoProducerInstall ==
    ~(CallerRole = "producer" /\ NewPool \in installedPools)
NoNewWorkerInstall == "new-worker" \notin registeredProfiles
=============================================================================
