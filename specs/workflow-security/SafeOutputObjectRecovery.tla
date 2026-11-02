--------------------- MODULE SafeOutputObjectRecovery ---------------------
EXTENDS Naturals

CONSTANTS Mode, Fault, MaxFetchAttempts
ASSUME Mode \in {"full", "incremental"}
ASSUME MaxFetchAttempts \in 1..3
ASSUME Fault \in {"none", "agent-fetch", "copied-config", "untrusted-remote",
                  "moving-baseline", "hidden-preview", "skip-policy",
                  "unfiltered-fetch", "untrusted-blob", "unbounded-fetch",
                  "invalid-limits", "uploaded-limits"}

\* Object/preview validation and authorization are abstract predicates, not a
\* proof of Git parsing, detector completeness, or implementation refinement.
VARIABLE s
vars == <<s>>

Init ==
    \E local, baseline, matches, complete, bounded, allowed,
       limitsValid, publisherBounded, uploadedOverride \in BOOLEAN:
        s = [stage |-> IF local THEN "policy" ELSE "limits",
             local |-> local, baseline |-> baseline, matches |-> matches,
             complete |-> complete, bounded |-> bounded, allowed |-> allowed,
             limitsValid |-> limitsValid, publisherBounded |-> publisherBounded,
             uploadedOverride |-> uploadedOverride,
             packaged |-> FALSE, fetched |-> FALSE, imported |-> FALSE,
             attempts |-> 0, filtered |-> TRUE, pinned |-> TRUE,
             hydrated |-> FALSE, trustedBlobs |-> TRUE,
             agentNetwork |-> FALSE, copiedConfig |-> FALSE,
             trustedRemote |-> TRUE, checkedPolicy |-> FALSE,
             published |-> FALSE, rejected |-> FALSE]

CheckLimits ==
    /\ s.stage = "limits"
    /\ LET accept == s.limitsValid \/ Fault = "invalid-limits"
       IN s' = [s EXCEPT !.stage = IF accept THEN "package" ELSE "done",
                         !.rejected = ~accept]

Package ==
    /\ s.stage = "package"
    /\ s' = [s EXCEPT !.stage = IF s.bounded THEN "preview" ELSE "done",
                      !.packaged = s.bounded, !.rejected = ~s.bounded,
                      !.agentNetwork = Fault = "agent-fetch"]

CheckPreview ==
    /\ s.stage = "preview"
    /\ LET accept == (s.matches \/ Fault = "hidden-preview")
                     /\ (s.publisherBounded
                         \/ (Fault = "uploaded-limits" /\ s.uploadedOverride))
       IN s' = [s EXCEPT !.stage = IF accept THEN "fetch" ELSE "done",
                         !.rejected = ~accept]

Fetch ==
    /\ s.stage = "fetch"
    /\ s.attempts < MaxFetchAttempts
    /\ \E enough \in BOOLEAN:
       LET accept == Mode = "full" \/ s.baseline \/ Fault = "moving-baseline"
       IN s' = [s EXCEPT !.stage = IF ~accept THEN "done"
                                   ELSE IF enough THEN "hydrate"
                                   ELSE IF s.attempts < MaxFetchAttempts - 1 THEN "fetch"
                                   ELSE "done",
                         !.attempts = IF Fault = "unbounded-fetch"
                                      THEN MaxFetchAttempts + 1 ELSE @ + 1,
                         !.filtered = Fault # "unfiltered-fetch",
                         !.fetched = accept,
                         !.rejected = ~accept
                                      \/ (~enough /\ s.attempts = MaxFetchAttempts - 1),
                         !.copiedConfig = accept /\ Fault = "copied-config",
                         !.trustedRemote = Fault # "untrusted-remote"]

Hydrate ==
    /\ s.stage = "hydrate"
    /\ s' = [s EXCEPT !.stage = "import", !.hydrated = TRUE,
                      !.trustedBlobs = Fault # "untrusted-blob"]

Import ==
    /\ s.stage = "import"
    /\ s' = [s EXCEPT !.stage = IF s.complete THEN "policy" ELSE "done",
                      !.imported = s.complete, !.rejected = ~s.complete]

Policy ==
    /\ s.stage = "policy"
    /\ LET accept == s.allowed \/ Fault = "skip-policy"
       IN s' = [s EXCEPT !.stage = IF accept THEN "publish" ELSE "done",
                         !.checkedPolicy = s.allowed, !.rejected = ~accept]

Publish ==
    /\ s.stage = "publish"
    /\ s' = [s EXCEPT !.stage = "done", !.published = TRUE]

Next == CheckLimits \/ Package \/ CheckPreview \/ Fetch \/ Hydrate \/ Import \/ Policy \/ Publish
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.stage \in {"limits", "package", "preview", "fetch", "hydrate", "import", "policy",
                    "publish", "done"}
    /\ \A field \in {"local", "baseline", "matches", "complete", "bounded",
                     "allowed", "packaged", "fetched", "imported",
                     "agentNetwork", "copiedConfig", "trustedRemote",
                     "checkedPolicy", "published", "rejected", "filtered",
                     "pinned", "hydrated", "trustedBlobs", "limitsValid",
                     "publisherBounded", "uploadedOverride"}:
           s[field] \in BOOLEAN
    /\ s.attempts \in 0..(MaxFetchAttempts + 1)
BoundedFilteredFetch ==
    s.fetched => s.filtered /\ s.pinned /\ s.attempts \in 1..MaxFetchAttempts
ConfigurationValid == s.packaged => s.limitsValid
PublisherLimits == s.fetched => s.publisherBounded
TrustedHydration == s.hydrated => s.trustedBlobs /\ s.fetched
HydrateBeforeImport == s.imported => s.hydrated
NoAgentNetwork == ~s.agentNetwork
IsolatedRecovery == ~s.copiedConfig
AuthorizedRemote == s.fetched => s.trustedRemote
ExactBaseline == s.fetched /\ Mode = "incremental" => s.baseline
ScannedBytes == s.fetched => s.packaged /\ s.matches /\ s.bounded
CompleteObjects == s.imported => s.complete
PublicationPolicy == s.published => s.checkedPolicy /\ s.allowed
RecoveryPrerequisites == s.published /\ ~s.local => s.imported /\ s.fetched
RejectedNoPublication == s.rejected => ~s.published

\* Deliberately false reachability predicates, enabled only by the runner.
NoRecoveryPublication == ~(s.published /\ ~s.local)
NoFastPublication == ~(s.published /\ s.local)
NoRejection == ~s.rejected
=============================================================================
