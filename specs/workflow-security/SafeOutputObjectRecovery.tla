--------------------- MODULE SafeOutputObjectRecovery ---------------------
EXTENDS Naturals

CONSTANTS Mode, Fault
ASSUME Mode \in {"full", "incremental"}
ASSUME Fault \in {"none", "agent-fetch", "copied-config", "untrusted-remote",
                  "moving-baseline", "hidden-preview", "skip-policy"}

\* Object/preview validation and authorization are abstract predicates, not a
\* proof of Git parsing, detector completeness, or implementation refinement.
VARIABLE s
vars == <<s>>

Init ==
    \E local, baseline, matches, complete, bounded, allowed \in BOOLEAN:
        s = [stage |-> IF local THEN "policy" ELSE "package",
             local |-> local, baseline |-> baseline, matches |-> matches,
             complete |-> complete, bounded |-> bounded, allowed |-> allowed,
             packaged |-> FALSE, fetched |-> FALSE, imported |-> FALSE,
             agentNetwork |-> FALSE, copiedConfig |-> FALSE,
             trustedRemote |-> TRUE, checkedPolicy |-> FALSE,
             published |-> FALSE, rejected |-> FALSE]

Package ==
    /\ s.stage = "package"
    /\ s' = [s EXCEPT !.stage = IF s.bounded THEN "preview" ELSE "done",
                      !.packaged = s.bounded, !.rejected = ~s.bounded,
                      !.agentNetwork = Fault = "agent-fetch"]

CheckPreview ==
    /\ s.stage = "preview"
    /\ LET accept == s.matches \/ Fault = "hidden-preview"
       IN s' = [s EXCEPT !.stage = IF accept THEN "fetch" ELSE "done",
                         !.rejected = ~accept]

Fetch ==
    /\ s.stage = "fetch"
    /\ LET accept == Mode = "full" \/ s.baseline \/ Fault = "moving-baseline"
       IN s' = [s EXCEPT !.stage = IF accept THEN "import" ELSE "done",
                         !.fetched = accept, !.rejected = ~accept,
                         !.copiedConfig = accept /\ Fault = "copied-config",
                         !.trustedRemote = Fault # "untrusted-remote"]

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

Next == Package \/ CheckPreview \/ Fetch \/ Import \/ Policy \/ Publish
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.stage \in {"package", "preview", "fetch", "import", "policy",
                    "publish", "done"}
    /\ \A field \in {"local", "baseline", "matches", "complete", "bounded",
                     "allowed", "packaged", "fetched", "imported",
                     "agentNetwork", "copiedConfig", "trustedRemote",
                     "checkedPolicy", "published", "rejected"}:
           s[field] \in BOOLEAN
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
