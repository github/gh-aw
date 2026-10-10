-------------------------- MODULE QueueBootstrap --------------------------
EXTENDS Naturals, Sequences, TLC

\* Bounded first-submit refinement, separate from scheduling and Claim models.
\* One immutable submission carries a compiler-approved AW Policy and valid Work.
\* "producer"/"dispatcher" are operation roles, not queue enrollment. AW host
\* authorization is an independently supplied premise, never proven here.
\* Native ref reads, repository identity/isEmpty and worker-route verification
\* are trusted observations, not agent claims. Git objects and credentials are
\* abstracted; this does not prove GitHub API behavior or deployment security.
CONSTANTS InitiallyEmpty, SameBranch, Broken
ASSUME /\ InitiallyEmpty \in BOOLEAN /\ SameBranch \in BOOLEAN
       /\ Broken \in {"none", "conflict-absence", "invalid-preparation",
                      "same-branch", "unverified-route", "split-policy",
                      "host-authorization", "producer-enrollment"}

VARIABLES request, hostAuthorized, policy, defaultExists, workerAvailable, phase, checked, verified,
          prepared, lostResponse, deferred, retried, ledger, publications,
          violation
vars == <<request, hostAuthorized, policy, defaultExists, workerAvailable, phase, checked, verified,
          prepared, lostResponse, deferred, retried, ledger, publications,
          violation>>
Requests == {"submit", "invalid", "read", "dispatch", "worker"}
Phases == {"read", "observed", "checked", "confirm", "route", "publish",
           "deferred", "done", "rejected"}
Observations == {"not-found", "empty-confirmed", "conflict",
                 "foreign-empty", "unavailable"}
AWPolicy == [authorization |-> "aw", producers |-> {},
             concurrency |-> 16, pending |-> 4096, maxClaims |-> 1,
             deliveryAttempts |-> 3, retryMilliseconds |-> 30000]

Init ==
    /\ request \in Requests
    /\ hostAuthorized \in BOOLEAN /\ policy = <<>>
    /\ defaultExists = ~InitiallyEmpty
    /\ workerAvailable \in BOOLEAN
    /\ phase = "read" /\ checked = FALSE /\ verified = FALSE
    /\ prepared = FALSE /\ lostResponse = FALSE
    /\ deferred = FALSE /\ retried = FALSE
    /\ ledger = <<>> /\ publications = 0 /\ violation = "none"

ReadRef(observation) ==
    /\ phase = "read"
    /\ observation \in Observations
    /\ (observation = "not-found" => defaultExists)
    /\ (observation = "empty-confirmed" => ~defaultExists)
    /\ LET absent == observation \in {"not-found", "empty-confirmed"}
           accepted == absent \/ Broken = "conflict-absence"
       IN /\ phase' = IF accepted THEN "observed" ELSE "rejected"
          /\ violation' = IF accepted /\ ~absent
                          THEN "unconfirmed-absence" ELSE violation
    /\ UNCHANGED <<request, hostAuthorized, policy, defaultExists, workerAvailable, checked, verified,
                   prepared, lostResponse, deferred, retried, ledger, publications>>

ValidateCandidate ==
    /\ phase = "observed"
    \* A valid host-authorized submission needs no producer map entry.
    /\ checked' = (request = "submit" /\
                   (hostAuthorized \/ Broken = "host-authorization"))
    /\ phase' = IF checked' /\ Broken # "producer-enrollment" THEN "checked"
                ELSE IF request \in {"read", "dispatch"} THEN "done"
                ELSE "rejected"
    /\ violation' = IF checked' /\ Broken = "producer-enrollment"
                    THEN "enrollment-gate" ELSE violation
    /\ UNCHANGED <<request, hostAuthorized, policy, defaultExists, workerAvailable, verified, prepared,
                   lostResponse, deferred, retried, ledger, publications>>

InvalidPreparation ==
    /\ Broken = "invalid-preparation" /\ phase = "observed"
    /\ request # "submit" /\ ~defaultExists
    /\ defaultExists' = TRUE /\ prepared' = TRUE /\ phase' = "rejected"
    /\ UNCHANGED <<request, hostAuthorized, policy, workerAvailable, checked, verified, lostResponse,
                   deferred, retried, ledger, publications, violation>>

PrepareDefault(outcome) ==
    /\ phase = "checked" /\ checked
    /\ outcome \in {"acknowledged", "lost", "failed"}
    /\ IF defaultExists
       THEN /\ phase' = "route"
            /\ UNCHANGED <<defaultExists, prepared, lostResponse>>
       ELSE IF SameBranch /\ Broken # "same-branch"
       THEN /\ phase' = "rejected"
            /\ UNCHANGED <<defaultExists, prepared, lostResponse>>
       ELSE IF outcome = "failed"
       THEN /\ phase' = "rejected"
            /\ UNCHANGED <<defaultExists, prepared, lostResponse>>
       ELSE IF outcome = "lost"
       THEN /\ defaultExists' \in BOOLEAN
            /\ prepared' = defaultExists' /\ lostResponse' = TRUE
            /\ phase' = "confirm"
       ELSE /\ defaultExists' = TRUE /\ prepared' = TRUE
            /\ lostResponse' = FALSE /\ phase' = "route"
    /\ UNCHANGED <<request, hostAuthorized, policy, workerAvailable, checked, verified, deferred,
                   retried, ledger, publications, violation>>

ConfirmDefault(observation) ==
    \* A lost write response is recovered only by a readable default branch.
    /\ phase = "confirm"
    /\ observation \in {"found", "not-found", "unavailable"}
    /\ (observation = "found" => defaultExists)
    /\ (observation = "not-found" => ~defaultExists)
    /\ phase' = IF observation = "found" THEN "route" ELSE "rejected"
    /\ UNCHANGED <<request, hostAuthorized, policy, defaultExists, workerAvailable, checked, verified,
                   prepared, lostResponse, deferred, retried, ledger,
                   publications, violation>>

DeployWorker ==
    /\ defaultExists /\ ~workerAvailable
    /\ workerAvailable' = TRUE
    /\ UNCHANGED <<request, hostAuthorized, policy, defaultExists, phase, checked, verified, prepared,
                   lostResponse, deferred, retried, ledger, publications, violation>>

VerifyRoute ==
    /\ phase = "route" /\ defaultExists /\ checked
    /\ verified' = workerAvailable
    \* "deferred" is a failed invocation (policy_missing), not a waiting runtime.
    /\ phase' = IF workerAvailable \/ Broken = "unverified-route"
                THEN "publish" ELSE "deferred"
    /\ deferred' = (deferred \/ ~workerAvailable)
    /\ UNCHANGED <<request, hostAuthorized, policy, defaultExists, workerAvailable, checked, prepared,
                   lostResponse, retried, ledger, publications, violation>>

RetrySubmission ==
    /\ phase = "deferred" /\ workerAvailable /\ ~retried
    \* An external new invocation resubmits the same immutable request.
    \* There is no automatic worker-deployment polling or retry loop.
    /\ phase' = "read" /\ checked' = FALSE /\ verified' = FALSE
    /\ retried' = TRUE
    /\ UNCHANGED <<request, hostAuthorized, policy, defaultExists, workerAvailable, prepared,
                   lostResponse, deferred, ledger, publications, violation>>

Publish ==
    /\ phase = "publish" /\ defaultExists /\ checked
    /\ verified \/ Broken = "unverified-route"
    /\ ledger = <<>>
    /\ ledger' = IF Broken = "split-policy" THEN <<"Policy">>
                 ELSE <<"Policy", "Work">>
    /\ policy' = <<AWPolicy>>
    /\ publications' = publications + 1 /\ phase' = "done"
    /\ UNCHANGED <<request, hostAuthorized, defaultExists, workerAvailable, checked, verified,
                   prepared, lostResponse, deferred, retried, violation>>

RecoverPublished ==
    \* A repeat of the same submission returns its receipt without another commit.
    /\ phase = "done" /\ ledger # <<>> /\ ~retried
    /\ retried' = TRUE
    /\ UNCHANGED <<request, hostAuthorized, policy, defaultExists, workerAvailable, phase, checked,
                   verified, prepared, lostResponse, deferred, ledger,
                   publications, violation>>

Next == ValidateCandidate \/ InvalidPreparation
        \/ DeployWorker \/ VerifyRoute \/ RetrySubmission \/ Publish
        \/ RecoverPublished
        \/ (\E observation \in Observations : ReadRef(observation))
        \/ (\E observation \in {"found", "not-found", "unavailable"} :
                ConfirmDefault(observation))
        \/ (\E outcome \in {"acknowledged", "lost", "failed"} : PrepareDefault(outcome))
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ request \in Requests /\ phase \in Phases
    /\ hostAuthorized \in BOOLEAN /\ policy \in {<<>>, <<AWPolicy>>}
    /\ defaultExists \in BOOLEAN /\ workerAvailable \in BOOLEAN
    /\ checked \in BOOLEAN /\ verified \in BOOLEAN /\ prepared \in BOOLEAN
    /\ lostResponse \in BOOLEAN /\ deferred \in BOOLEAN /\ retried \in BOOLEAN
    /\ ledger \in {<<>>, <<"Policy">>, <<"Policy", "Work">>}
    /\ publications \in 0..1
    /\ violation \in {"none", "unconfirmed-absence", "enrollment-gate"}
AbsenceAuthority == violation # "unconfirmed-absence"
PreparationAuthority == prepared => request = "submit"
SeparateQueueBranch == prepared => ~SameBranch
AtomicGenesis == ledger = <<>> \/ ledger = <<"Policy", "Work">>
PublicationAuthority == ledger # <<>> => checked /\ verified /\ defaultExists
RequestOnce == publications = IF ledger = <<>> THEN 0 ELSE 1
PreparationNotPublication == phase \in {"confirm", "route", "deferred"} => ledger = <<>>
TrustedAWAuthority == (checked => hostAuthorized) /\ (prepared => hostAuthorized)
NoEnrollmentGate == violation # "enrollment-gate"
DefaultOnlyPolicy == policy # <<>> => policy = <<AWPolicy>>
Safety == TypeOK /\ AbsenceAuthority /\ PreparationAuthority /\ SeparateQueueBranch
          /\ AtomicGenesis /\ PublicationAuthority /\ RequestOnce
          /\ PreparationNotPublication /\ TrustedAWAuthority /\ NoEnrollmentGate
          /\ DefaultOnlyPolicy

\* Deliberately false invariants find guarded reachability witnesses, not liveness.
NoDeferredPreparation == ~(prepared /\ phase = "deferred" /\ ledger = <<>>)
NoDeploymentRetry == ~(prepared /\ deferred /\ retried /\ ledger = <<"Policy", "Work">>)
NoLostResponseRecovery == ~(prepared /\ lostResponse /\ ledger = <<"Policy", "Work">>)
NoLostResponseFailure == ~(lostResponse /\ ~defaultExists /\ phase = "rejected"
                          /\ ledger = <<>>)
NoTrustedAWBootstrap == ~(hostAuthorized /\ ledger = <<"Policy", "Work">>
                         /\ policy[1].producers = {})
=============================================================================
