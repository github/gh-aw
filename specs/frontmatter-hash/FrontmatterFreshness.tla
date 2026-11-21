----------------------- MODULE FrontmatterFreshness ------------------------

VARIABLE s

Digests == {"original", "changed"}
Reads == {"original", "changed", "no-digest", "unreadable"}
Terminal == {"started", "conclusion"}

Init ==
    s = [phase |-> "compile", stored |-> "none", source |-> "original",
         primary |-> "none", reader |-> "none", observed |-> "none",
         verdict |-> "none", failedOutput |-> FALSE]

Compile ==
    /\ s.phase = "compile"
    /\ s' = [s EXCEPT !.phase = "pre-run", !.stored = s.source]

BeginActivation(source) ==
    /\ s.phase = "pre-run"
    /\ s' = [s EXCEPT !.phase = "api", !.source = source]

\* A successful read recomputes the digest of the run's source snapshot.
AvailableReads == {s.source, "no-digest", "unreadable"}

ReadAPI(result) ==
    /\ s.phase = "api"
    /\ s' = [s EXCEPT !.primary = result,
                     !.phase = IF result = "unreadable" THEN "local" ELSE "compare",
                     !.reader = "api", !.observed = result]

ReadLocal(result) ==
    /\ s.phase = "local"
    /\ s' = [s EXCEPT !.phase = "compare", !.reader = "local", !.observed = result]

Compare ==
    /\ s.phase = "compare"
    /\ LET verdict == CASE s.observed \notin Digests -> "unverifiable"
                        [] s.observed = s.stored -> "fresh"
                        [] OTHER -> "stale"
       IN s' = [s EXCEPT !.verdict = verdict,
                        !.phase = IF verdict = "fresh" THEN "verified" ELSE "failed",
                        !.failedOutput = (verdict # "fresh")]

StartWorkflow ==
    /\ s.phase = "verified"
    /\ s' = [s EXCEPT !.phase = "started"]

RouteFailure ==
    /\ s.phase = "failed"
    /\ s.failedOutput
    /\ s' = [s EXCEPT !.phase = "conclusion"]

Advance ==
    \/ Compile
    \/ \E source \in Digests : BeginActivation(source)
    \/ \E result \in AvailableReads : ReadAPI(result)
    \/ \E result \in AvailableReads : ReadLocal(result)
    \/ Compare
    \/ StartWorkflow
    \/ RouteFailure

Next == Advance \/ (s.phase \in Terminal /\ UNCHANGED s)
Spec == Init /\ [][Next]_s /\ WF_s(Advance)

TypeOK ==
    s \in [phase : {"compile", "pre-run", "api", "local", "compare", "verified", "failed"}
                   \cup Terminal,
           stored : Digests \cup {"none"}, source : Digests,
           primary : Reads \cup {"none"}, reader : {"none", "api", "local"},
           observed : Reads \cup {"none"},
           verdict : {"none", "fresh", "stale", "unverifiable"}, failedOutput : BOOLEAN]

FallbackOnlyOnReadFailure ==
    (s.phase = "local" \/ s.reader = "local") => s.primary = "unreadable"

StartRequiresVerifiedMatch ==
    s.phase = "started" =>
        /\ s.verdict = "fresh"
        /\ s.stored = s.source
        /\ s.observed = s.stored
        /\ ~s.failedOutput

MismatchFailsActivation ==
    (s.verdict # "none" /\ s.observed \in Digests /\ s.observed # s.stored) =>
        s.verdict = "stale" /\ s.phase \in {"failed", "conclusion"}

NoDigestFailsActivation ==
    (s.verdict # "none" /\ s.observed \notin Digests) =>
        s.verdict = "unverifiable" /\ s.phase \in {"failed", "conclusion"}

FailureSignal ==
    s.failedOutput <=> s.phase \in {"failed", "conclusion"}

Terminates == <>(s.phase \in Terminal)
VerifiedMatchStarts == (s.phase = "verified") ~> (s.phase = "started")
FailureReachesConclusion == (s.phase = "failed") ~> (s.phase = "conclusion")
=============================================================================
