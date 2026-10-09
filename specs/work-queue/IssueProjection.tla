--------------------------- MODULE IssueProjection ---------------------------
EXTENDS Naturals, TLC

\* Bounded authority and recovery model for the Git-backed Issue projector.
\* One Work, one Issue, one original Claim, and one projector identity are
\* modeled. Git replay, API payloads, transport behavior, and coordination
\* implementation are abstracted; this is not a runtime-refinement proof.
CONSTANTS TargetMode, TargetGrant, InitialClosePolicy, IsPullRequest, Broken

VARIABLES projectorInstalled, policyClose, admitted, admissionAuthorized,
          admissionClose, originalClaim, principalMatches, issueExists,
          createdByProjector, receipt, bound, journal, coordination,
          createAttempts, resultVerified, summaryProjected, summaryAuthorized,
          issueClosed, closureAuthorized, nativeWrites, unauthorizedWrites,
          humanText

vars == <<projectorInstalled, policyClose, admitted, admissionAuthorized,
          admissionClose, originalClaim, principalMatches, issueExists,
          createdByProjector, receipt, bound, journal, coordination,
          createAttempts, resultVerified, summaryProjected, summaryAuthorized,
          issueClosed, closureAuthorized, nativeWrites, unauthorizedWrites,
          humanText>>

HookAuthorized == projectorInstalled /\ originalClaim /\ principalMatches
AdmissionTargetAllowed == TargetMode = "created" \/ TargetGrant
CanCreate == admitted /\ admissionAuthorized /\ HookAuthorized
             /\ TargetMode = "created" /\ journal = "intent"
             /\ createAttempts = 0
CanBindExisting == admitted /\ admissionAuthorized /\ HookAuthorized
                   /\ TargetMode = "existing" /\ TargetGrant /\ issueExists
CanProject == admitted /\ admissionAuthorized /\ HookAuthorized /\ bound /\ issueExists
              /\ (TargetMode = "existing" => TargetGrant)
              /\ (TargetMode = "created" => createdByProjector /\ receipt)
CanClose == CanProject /\ resultVerified /\ ~IsPullRequest
            /\ admissionClose /\ ~issueClosed

Init ==
  /\ projectorInstalled = TRUE
  /\ policyClose = InitialClosePolicy
  /\ admitted = FALSE /\ admissionAuthorized = FALSE /\ admissionClose = FALSE
  /\ originalClaim = TRUE /\ principalMatches = TRUE
  /\ issueExists = (TargetMode = "existing")
  /\ createdByProjector = FALSE /\ receipt = FALSE /\ bound = FALSE
  /\ journal = "none" /\ coordination = "free" /\ createAttempts = 0
  /\ resultVerified = FALSE /\ summaryProjected = FALSE
  /\ summaryAuthorized = FALSE /\ issueClosed = FALSE /\ closureAuthorized = FALSE
  /\ nativeWrites = 0 /\ unauthorizedWrites = 0
  /\ humanText = "human content"

Admit ==
  /\ ~admitted /\ projectorInstalled /\ AdmissionTargetAllowed
  /\ admitted' = TRUE /\ admissionAuthorized' = TRUE
  /\ admissionClose' = policyClose
  /\ UNCHANGED <<projectorInstalled, policyClose, originalClaim,
                 principalMatches, issueExists, createdByProjector, receipt,
                 bound, journal, coordination, createAttempts, resultVerified,
                 summaryProjected, summaryAuthorized, issueClosed,
                 closureAuthorized, nativeWrites, unauthorizedWrites, humanText>>

ExpandPolicy ==
  /\ admitted /\ ~admissionClose /\ ~policyClose
  /\ policyClose' = TRUE
  /\ UNCHANGED <<projectorInstalled, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, bound, journal, coordination,
                 createAttempts, resultVerified, summaryProjected,
                 summaryAuthorized, issueClosed, closureAuthorized,
                 nativeWrites, unauthorizedWrites, humanText>>

LoseProjectorAuthority ==
  /\ projectorInstalled
  /\ projectorInstalled' = FALSE
  /\ UNCHANGED <<policyClose, admitted, admissionAuthorized, admissionClose,
                 originalClaim, principalMatches, issueExists, createdByProjector,
                 receipt, bound, journal, coordination, createAttempts,
                 resultVerified, summaryProjected, summaryAuthorized, issueClosed,
                 closureAuthorized, nativeWrites, unauthorizedWrites, humanText>>

UseForeignContext ==
  /\ originalClaim /\ principalMatches
  /\ \/ originalClaim' = FALSE /\ UNCHANGED principalMatches
     \/ principalMatches' = FALSE /\ UNCHANGED originalClaim
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, issueExists, createdByProjector, receipt,
                 bound, journal, coordination, createAttempts, resultVerified,
                 summaryProjected, summaryAuthorized, issueClosed,
                 closureAuthorized, nativeWrites, unauthorizedWrites, humanText>>

PrepareCreation ==
  /\ admitted /\ admissionAuthorized /\ HookAuthorized
  /\ TargetMode = "created" /\ journal = "none"
  /\ journal' = "intent" /\ coordination' = "held"
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, bound, createAttempts,
                 resultVerified, summaryProjected, summaryAuthorized,
                 issueClosed, closureAuthorized, nativeWrites,
                 unauthorizedWrites, humanText>>

CreateWithReceipt ==
  /\ CanCreate /\ coordination = "held"
  /\ issueExists' = TRUE /\ createdByProjector' = TRUE /\ receipt' = TRUE
  /\ journal' = "receipted" /\ createAttempts' = createAttempts + 1
  /\ nativeWrites' = nativeWrites + 1
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, bound,
                 coordination, resultVerified, summaryProjected,
                 summaryAuthorized, issueClosed, closureAuthorized,
                 unauthorizedWrites, humanText>>

CreateWithLostReceipt ==
  /\ CanCreate /\ coordination = "held"
  /\ issueExists' = TRUE /\ createdByProjector' = TRUE /\ receipt' = FALSE
  /\ journal' = "uncertain" /\ createAttempts' = createAttempts + 1
  /\ nativeWrites' = nativeWrites + 1
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, bound,
                 coordination, resultVerified, summaryProjected,
                 summaryAuthorized, issueClosed, closureAuthorized,
                 unauthorizedWrites, humanText>>

BindCreatedIssue ==
  /\ admitted /\ admissionAuthorized /\ HookAuthorized
  /\ TargetMode = "created" /\ createdByProjector /\ receipt
  /\ journal = "receipted" /\ ~bound
  /\ bound' = TRUE
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, journal, coordination,
                 createAttempts, resultVerified, summaryProjected,
                 summaryAuthorized, issueClosed, closureAuthorized,
                 nativeWrites, unauthorizedWrites, humanText>>

BindExistingIssue ==
  /\ CanBindExisting /\ ~bound
  /\ bound' = TRUE
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, journal, coordination,
                 createAttempts, resultVerified, summaryProjected,
                 summaryAuthorized, issueClosed, closureAuthorized,
                 nativeWrites, unauthorizedWrites, humanText>>

ProjectSummary ==
  /\ CanProject /\ ~summaryProjected /\ issueExists
  /\ summaryProjected' = TRUE /\ summaryAuthorized' = TRUE
  /\ nativeWrites' = nativeWrites + 1
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, bound, journal, coordination,
                 createAttempts, resultVerified, issueClosed, closureAuthorized,
                 unauthorizedWrites, humanText>>

VerifyResult ==
  /\ admitted /\ ~resultVerified
  /\ resultVerified' = TRUE
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, bound, journal, coordination,
                 createAttempts, summaryProjected, summaryAuthorized, issueClosed,
                 closureAuthorized, nativeWrites, unauthorizedWrites, humanText>>

CloseIssue ==
  /\ CanClose
  /\ issueClosed' = TRUE /\ closureAuthorized' = TRUE
  /\ nativeWrites' = nativeWrites + 1
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, bound, journal, coordination,
                 createAttempts, resultVerified, summaryProjected,
                 summaryAuthorized, unauthorizedWrites, humanText>>

UnsafeAdmission ==
  /\ Broken = "unowned-admission" /\ ~admitted
  /\ TargetMode = "existing" /\ ~TargetGrant
  /\ admitted' = TRUE /\ admissionAuthorized' = TRUE
  /\ admissionClose' = policyClose
  /\ UNCHANGED <<projectorInstalled, policyClose, originalClaim,
                 principalMatches, issueExists, createdByProjector, receipt,
                 bound, journal, coordination, createAttempts, resultVerified,
                 summaryProjected, summaryAuthorized, issueClosed,
                 closureAuthorized, nativeWrites, unauthorizedWrites, humanText>>

UnsafeProjectUnowned ==
  /\ Broken = "unowned-admission" /\ admitted /\ ~TargetGrant
  /\ projectorInstalled /\ originalClaim /\ principalMatches /\ issueExists
  /\ nativeWrites' = nativeWrites + 1
  /\ unauthorizedWrites' = unauthorizedWrites + 1
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, bound, journal, coordination,
                 createAttempts, resultVerified, summaryProjected,
                 summaryAuthorized, issueClosed, closureAuthorized, humanText>>

UnsafeCloseFromPayload ==
  /\ Broken = "payload-close" /\ admitted /\ bound /\ issueExists
  /\ resultVerified /\ ~issueClosed
  /\ issueClosed' = TRUE /\ closureAuthorized' = FALSE
  /\ nativeWrites' = nativeWrites + 1
  /\ unauthorizedWrites' = unauthorizedWrites + 1
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, issueExists,
                 createdByProjector, receipt, bound, journal, coordination,
                 createAttempts, resultVerified, summaryProjected,
                 summaryAuthorized, humanText>>

UnsafeRetryUncertainCreation ==
  /\ Broken = "retry-uncertain" /\ journal = "uncertain"
  /\ createAttempts' = createAttempts + 1
  /\ issueExists' = TRUE /\ createdByProjector' = TRUE
  /\ nativeWrites' = nativeWrites + 1 /\ unauthorizedWrites' = unauthorizedWrites + 1
  /\ UNCHANGED <<projectorInstalled, policyClose, admitted, admissionAuthorized,
                 admissionClose, originalClaim, principalMatches, receipt, bound,
                 journal, coordination, resultVerified, summaryProjected,
                 summaryAuthorized, issueClosed, closureAuthorized, humanText>>

Next == Admit \/ ExpandPolicy \/ LoseProjectorAuthority \/ UseForeignContext
        \/ PrepareCreation \/ CreateWithReceipt \/ CreateWithLostReceipt
        \/ BindCreatedIssue \/ BindExistingIssue \/ ProjectSummary \/ VerifyResult
        \/ CloseIssue \/ UnsafeAdmission \/ UnsafeProjectUnowned
        \/ UnsafeCloseFromPayload \/ UnsafeRetryUncertainCreation

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ TargetMode \in {"existing", "created"}
  /\ projectorInstalled \in BOOLEAN /\ policyClose \in BOOLEAN
  /\ admitted \in BOOLEAN /\ admissionAuthorized \in BOOLEAN
  /\ admissionClose \in BOOLEAN /\ originalClaim \in BOOLEAN
  /\ principalMatches \in BOOLEAN /\ issueExists \in BOOLEAN
  /\ createdByProjector \in BOOLEAN /\ receipt \in BOOLEAN /\ bound \in BOOLEAN
  /\ journal \in {"none", "intent", "receipted", "uncertain"}
  /\ coordination \in {"free", "held"}
  /\ createAttempts \in Nat /\ resultVerified \in BOOLEAN
  /\ summaryProjected \in BOOLEAN /\ summaryAuthorized \in BOOLEAN
  /\ issueClosed \in BOOLEAN /\ closureAuthorized \in BOOLEAN
  /\ nativeWrites \in Nat /\ unauthorizedWrites \in Nat
  /\ humanText = "human content"

AdmissionAuthority ==
  admitted => admissionAuthorized /\ (TargetMode = "created" \/ TargetGrant)

TargetBindingAuthority ==
  bound => admitted /\ admissionAuthorized
           /\ ((TargetMode = "existing" /\ TargetGrant)
               \/ (TargetMode = "created" /\ createdByProjector /\ receipt))

ProjectionAuthority ==
  unauthorizedWrites = 0
  /\ (summaryProjected => summaryAuthorized)

SingleCreation == createAttempts <= 1

UncertainCreationFence ==
  journal = "uncertain" =>
    coordination = "held" /\ createAttempts = 1
    /\ ~receipt /\ ~bound

ClosureAuthority ==
  issueClosed => closureAuthorized /\ resultVerified /\ ~IsPullRequest
                 /\ admissionClose /\ bound

=============================================================================
