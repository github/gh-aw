------------------------ MODULE ModelAliasDiscovery ------------------------
EXTENDS Naturals

VARIABLE s

Requests == {"exact", "auto", "alias", "native-alias"}
AliasRequests == {"alias", "native-alias"}
CatalogViews == {"live-exact", "candidate", "no-candidate", "unavailable"}
Failures == {"model_unavailable", "catalog_unavailable"}
Selections == {"configured", "resolved"}
Terminal == {"launched", "failed"}

Init ==
    \E request \in Requests, awf \in BOOLEAN :
        s = [request |-> request, awf |-> awf, phase |-> "compile",
             attempts |-> 0, pending |-> FALSE, view |-> "none",
             firstFailure |-> "none", outcome |-> "none"]

\* The configured identifier and any workflow overlay travel together.
PassToHarness ==
    /\ s.phase = "compile"
    /\ s' = [s EXCEPT !.phase = "discover"]

RequestCatalog ==
    /\ s.phase \in {"discover", "refresh"}
    /\ s.awf
    /\ ~s.pending
    /\ s' = [s EXCEPT !.attempts = @ + 1, !.pending = TRUE]

ReceiveCatalog(view) ==
    /\ s.pending
    /\ s' = [s EXCEPT !.pending = FALSE, !.view = view, !.phase = "decide"]

UseNativePolicy ==
    /\ s.phase = "discover"
    /\ ~s.awf
    /\ s' = [s EXCEPT !.phase = "decide"]

Selected(outcome) ==
    s' = [s EXCEPT !.phase = "ready", !.outcome = outcome]

Stop(reason) ==
    s' = [s EXCEPT !.phase = "failed", !.outcome = reason]

Reject(reason) ==
    IF s.awf /\ s.attempts = 1
    THEN s' = [s EXCEPT !.phase = "refresh", !.firstFailure = reason]
    ELSE Stop(IF s.firstFailure = "model_unavailable"
              THEN s.firstFailure ELSE reason)

Decide ==
    /\ s.phase = "decide"
    /\ CASE s.request \notin AliasRequests -> Selected("configured")
         [] ~s.awf ->
             IF s.request = "native-alias" THEN Selected("configured")
             ELSE Stop("catalog_unavailable")
         [] s.view = "live-exact" -> Selected("configured")
         [] s.view = "candidate" -> Selected("resolved")
         [] s.view = "no-candidate" -> Reject("model_unavailable")
         [] OTHER -> Reject("catalog_unavailable")

Launch ==
    /\ s.phase = "ready"
    /\ s' = [s EXCEPT !.phase = "launched"]

Advance ==
    \/ PassToHarness
    \/ RequestCatalog
    \/ \E view \in CatalogViews : ReceiveCatalog(view)
    \/ UseNativePolicy
    \/ Decide
    \/ Launch

Next == Advance \/ (s.phase \in Terminal /\ UNCHANGED s)
Spec == Init /\ [][Next]_s /\ WF_s(Advance)

TypeOK ==
    s \in [request : Requests, awf : BOOLEAN,
           phase : {"compile", "discover", "refresh", "decide", "ready"} \cup Terminal,
           attempts : 0..2, pending : BOOLEAN, view : CatalogViews \cup {"none"},
           firstFailure : Failures \cup {"none"}, outcome : Selections \cup Failures \cup {"none"}]

BoundedDiscovery ==
    /\ ~s.awf => s.attempts = 0
    /\ s.attempts = 2 => s.firstFailure \in Failures
    /\ s.pending => s.phase \in {"discover", "refresh"}

EngineLaunchRequiresSelection ==
    /\ s.phase = "launched" => s.outcome \in Selections
    /\ s.outcome \in Failures => s.phase = "failed"
    /\ s.outcome = "resolved" =>
        s.awf /\ s.request \in AliasRequests /\ s.view = "candidate"

ExactWins ==
    (s.phase = "launched" /\ s.view = "live-exact") => s.outcome = "configured"

UnresolvedAliasDoesNotLaunch ==
    (s.phase = "launched" /\ s.request \in AliasRequests) =>
        IF s.awf THEN s.view \in {"live-exact", "candidate"}
        ELSE s.request = "native-alias"

FailedDiscoveryUsesBudget ==
    (s.phase = "failed" /\ s.awf) => s.attempts = 2

FailurePrecedence ==
    (s.phase = "failed" /\ s.firstFailure = "model_unavailable") =>
        s.outcome = "model_unavailable"

FinalCatalogFailureIsClassified ==
    (s.phase = "failed" /\ s.awf /\ s.firstFailure # "model_unavailable") =>
        s.outcome = (IF s.view = "no-candidate" THEN "model_unavailable"
                     ELSE "catalog_unavailable")

Terminates == <>(s.phase \in Terminal)
=============================================================================
