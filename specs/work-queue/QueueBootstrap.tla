-------------------------- MODULE QueueBootstrap --------------------------
EXTENDS Naturals, Sequences, TLC

\* Bounded first-submit refinement, separate from scheduling and Claim models.
\* One immutable producer request carries an approved Policy and valid Work.
\* Native ref reads, repository identity/isEmpty and worker-route verification
\* are trusted observations, not agent claims. Git objects and credentials are
\* abstracted; this does not prove GitHub API behavior or deployment security.
CONSTANTS InitiallyEmpty, SameBranch, Broken
ASSUME /\ InitiallyEmpty \in BOOLEAN /\ SameBranch \in BOOLEAN
       /\ Broken \in {"none", "conflict-absence", "invalid-preparation",
                      "same-branch", "unverified-route", "split-policy"}

VARIABLES request, defaultExists, workerAvailable, phase, checked, verified,
          prepared, lostResponse, deferred, retried, ledger, publications,
          violation
vars == <<request, defaultExists, workerAvailable, phase, checked, verified,
          prepared, lostResponse, deferred, retried, ledger, publications,
          violation>>
Requests == {"submit", "invalid", "read", "dispatch", "worker"}
Phases == {"read", "observed", "checked", "confirm", "route", "publish",
           "deferred", "done", "rejected"}
Observations == {"not-found", "empty-confirmed", "conflict",
                 "foreign-empty", "unavailable"}

Init ==
    /\ request \in Requests
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
    /\ UNCHANGED <<request, defaultExists, workerAvailable, checked, verified,
                   prepared, lostResponse, deferred, retried, ledger, publications>>

ValidateCandidate ==
    /\ phase = "observed"
    \* "invalid" includes malformed Work, unapproved Policy and unauthorized actors.
    /\ checked' = (request = "submit")
    /\ phase' = IF request = "submit" THEN "checked"
                ELSE IF request \in {"read", "dispatch"} THEN "done"
                ELSE "rejected"
    /\ UNCHANGED <<request, defaultExists, workerAvailable, verified, prepared,
                   lostResponse, deferred, retried, ledger, publications, violation>>

InvalidPreparation ==
    /\ Broken = "invalid-preparation" /\ phase = "observed"
    /\ request # "submit" /\ ~defaultExists
    /\ defaultExists' = TRUE /\ prepared' = TRUE /\ phase' = "rejected"
    /\ UNCHANGED <<request, workerAvailable, checked, verified, lostResponse,
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
       ELSE /\ defaultExists' = TRUE /\ prepared' = TRUE
            /\ lostResponse' = (outcome = "lost")
            /\ phase' = IF outcome = "lost" THEN "confirm" ELSE "route"
    /\ UNCHANGED <<request, workerAvailable, checked, verified, deferred,
                   retried, ledger, publications, violation>>

ConfirmDefault ==
    \* A lost write response is recovered only by a readable default branch.
    /\ phase = "confirm" /\ defaultExists
    /\ phase' = "route"
    /\ UNCHANGED <<request, defaultExists, workerAvailable, checked, verified,
                   prepared, lostResponse, deferred, retried, ledger,
                   publications, violation>>

DeployWorker ==
    /\ defaultExists /\ ~workerAvailable
    /\ workerAvailable' = TRUE
    /\ UNCHANGED <<request, defaultExists, phase, checked, verified, prepared,
                   lostResponse, deferred, retried, ledger, publications, violation>>

VerifyRoute ==
    /\ phase = "route" /\ defaultExists /\ checked
    /\ verified' = workerAvailable
    /\ phase' = IF workerAvailable \/ Broken = "unverified-route"
                THEN "publish" ELSE "deferred"
    /\ deferred' = (deferred \/ ~workerAvailable)
    /\ UNCHANGED <<request, defaultExists, workerAvailable, checked, prepared,
                   lostResponse, retried, ledger, publications, violation>>

RetrySubmission ==
    /\ phase = "deferred" /\ workerAvailable /\ ~retried
    \* The same immutable request is re-read and revalidated, not rewritten.
    /\ phase' = "read" /\ checked' = FALSE /\ verified' = FALSE
    /\ retried' = TRUE
    /\ UNCHANGED <<request, defaultExists, workerAvailable, prepared,
                   lostResponse, deferred, ledger, publications, violation>>

Publish ==
    /\ phase = "publish" /\ defaultExists /\ checked
    /\ verified \/ Broken = "unverified-route"
    /\ ledger = <<>>
    /\ ledger' = IF Broken = "split-policy" THEN <<"Policy">>
                 ELSE <<"Policy", "Work">>
    /\ publications' = publications + 1 /\ phase' = "done"
    /\ UNCHANGED <<request, defaultExists, workerAvailable, checked, verified,
                   prepared, lostResponse, deferred, retried, violation>>

RecoverPublished ==
    \* A repeat of the same submission returns its receipt without another commit.
    /\ phase = "done" /\ ledger # <<>> /\ ~retried
    /\ retried' = TRUE
    /\ UNCHANGED <<request, defaultExists, workerAvailable, phase, checked,
                   verified, prepared, lostResponse, deferred, ledger,
                   publications, violation>>

Next == ValidateCandidate \/ InvalidPreparation \/ ConfirmDefault
        \/ DeployWorker \/ VerifyRoute \/ RetrySubmission \/ Publish
        \/ RecoverPublished
        \/ (\E observation \in Observations : ReadRef(observation))
        \/ (\E outcome \in {"acknowledged", "lost", "failed"} : PrepareDefault(outcome))
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ request \in Requests /\ phase \in Phases
    /\ defaultExists \in BOOLEAN /\ workerAvailable \in BOOLEAN
    /\ checked \in BOOLEAN /\ verified \in BOOLEAN /\ prepared \in BOOLEAN
    /\ lostResponse \in BOOLEAN /\ deferred \in BOOLEAN /\ retried \in BOOLEAN
    /\ ledger \in {<<>>, <<"Policy">>, <<"Policy", "Work">>}
    /\ publications \in 0..1
    /\ violation \in {"none", "unconfirmed-absence"}
AbsenceAuthority == violation # "unconfirmed-absence"
PreparationAuthority == prepared => request = "submit"
SeparateQueueBranch == prepared => ~SameBranch
AtomicGenesis == ledger = <<>> \/ ledger = <<"Policy", "Work">>
PublicationAuthority == ledger # <<>> => checked /\ verified /\ defaultExists
RequestOnce == publications = IF ledger = <<>> THEN 0 ELSE 1
PreparationNotPublication == phase \in {"confirm", "route", "deferred"} => ledger = <<>>
Safety == TypeOK /\ AbsenceAuthority /\ PreparationAuthority /\ SeparateQueueBranch
          /\ AtomicGenesis /\ PublicationAuthority /\ RequestOnce
          /\ PreparationNotPublication

\* Deliberately false invariants find guarded reachability witnesses, not liveness.
NoDeferredPreparation == ~(prepared /\ phase = "deferred" /\ ledger = <<>>)
NoDeploymentRetry == ~(prepared /\ deferred /\ retried /\ ledger = <<"Policy", "Work">>)
NoLostResponseRecovery == ~(prepared /\ lostResponse /\ ledger = <<"Policy", "Work">>)
=============================================================================
