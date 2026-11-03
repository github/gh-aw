------------------- MODULE IssueRelationshipAuthorization -------------------
EXTENDS Naturals, Sequences

CONSTANT Fault
ASSUME Fault \in {"none", "resolved-policy-bypass", "early-mentions",
                  "parent-override", "staged-write"}

VARIABLE s
vars == <<s>>
Repos == {"home/repo", "allowed/repo", "denied/repo", "Allowed/Repo"}
Policies == {"default", "fixed", "exact", "org-glob", "all", "target-all"}
DefaultRepo(policy) == IF policy = "fixed" THEN "allowed/repo" ELSE "home/repo"
Allowed(repo, policy) ==
    \/ repo = DefaultRepo(policy)
    \/ policy \in {"all", "target-all"}
    \/ policy \in {"exact", "org-glob"} /\ repo = "allowed/repo"

\* Finite repo classes abstract exact/case-sensitive glob matching, not parsing.
Init ==
    \E kind \in {"link", "blocker"}, policy \in Policies,
       target \in {"*", "fixed", "triggering"}, staged \in BOOLEAN,
       ref \in {"numeric", "qualified", "url", "temporary"},
       refRepo \in Repos, parentRepo \in Repos, late \in BOOLEAN:
      /\ (kind = "link" => ref \in {"numeric", "temporary"})
      /\ (late => ref = "temporary")
      /\ s = [stage |-> "resolve", kind |-> kind, policy |-> policy,
              target |-> target, staged |-> staged, ref |-> ref,
              refRepo |-> refRepo, parentRepo |-> parentRepo, late |-> late,
              ready |-> ~late, retried |-> FALSE, resolved |-> FALSE,
              effective |-> {}, parent |-> "", sub |-> "",
              authorized |-> FALSE, rejected |-> FALSE, calls |-> <<>>,
              lastEvent |-> "Init"]

PairValid == s.kind = "blocker" \/ s.parent = s.sub
EffectiveAllowed == \A repo \in s.effective: Allowed(repo, s.policy)

Resolve ==
    /\ s.stage = "resolve"
    /\ IF ~s.ready
       THEN LET preview == s.staged /\ s.kind = "blocker"
            IN s' = [s EXCEPT !.stage = IF preview THEN "done" ELSE "deferred",
                             !.lastEvent = IF preview THEN "UnresolvedPreview" ELSE "Defer"]
       ELSE LET default == DefaultRepo(s.policy)
                sub == IF s.ref = "numeric" THEN default ELSE s.refRepo
                parent == IF s.target = "*" \/ Fault = "parent-override"
                          THEN IF s.ref = "temporary" THEN s.parentRepo ELSE default
                          ELSE default
                repos == IF s.kind = "link" THEN {parent, sub} ELSE {default, sub}
            IN s' = [s EXCEPT !.stage = "validate", !.resolved = TRUE,
                             !.effective = repos, !.parent = parent, !.sub = sub,
                             !.lastEvent = "Resolve"]

Retry ==
    /\ s.stage = "deferred" /\ ~s.retried
    /\ s' = [s EXCEPT !.stage = "resolve", !.ready = TRUE,
                      !.retried = TRUE, !.lastEvent = "Retry"]

Call(action, repo) ==
    [action |-> action, repo |-> repo, preflight |-> s.authorized]

Validate ==
    /\ s.stage = "validate"
    /\ LET allowed == EffectiveAllowed
           accept == PairValid /\ (allowed \/ Fault = "resolved-policy-bypass")
       IN s' = [s EXCEPT
           !.stage = IF accept THEN "execute" ELSE "done",
           !.authorized = accept, !.rejected = ~accept,
           !.calls = IF Fault = "early-mentions" /\ s.kind = "blocker"
                     THEN Append(@, Call("mentions", DefaultRepo(s.policy))) ELSE @,
           !.lastEvent = IF accept THEN "Authorize" ELSE "Reject"]

Execute ==
    /\ s.stage = "execute"
    /\ LET writes == ~s.staged \/ Fault = "staged-write"
           repo == IF s.kind = "link" THEN s.parent ELSE DefaultRepo(s.policy)
           read == Call("read", s.sub)
           mutation == Call("write", repo)
       IN s' = [s EXCEPT
           !.stage = "done",
           !.calls = IF writes THEN Append(Append(@, read), mutation) ELSE @,
           !.lastEvent = IF writes THEN "Execute" ELSE "Preview"]

Next == Resolve \/ Retry \/ Validate \/ Execute
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.stage \in {"resolve", "deferred", "validate", "execute", "done"}
    /\ s.kind \in {"link", "blocker"} /\ s.policy \in Policies
    /\ s.target \in {"*", "fixed", "triggering"} /\ s.staged \in BOOLEAN
    /\ s.ref \in {"numeric", "qualified", "url", "temporary"}
    /\ s.refRepo \in Repos /\ s.parentRepo \in Repos
    /\ s.late \in BOOLEAN /\ s.ready \in BOOLEAN /\ s.retried \in BOOLEAN
    /\ s.resolved \in BOOLEAN /\ s.authorized \in BOOLEAN /\ s.rejected \in BOOLEAN
    /\ s.effective \subseteq Repos
    /\ s.parent \in Repos \cup {""} /\ s.sub \in Repos \cup {""}
    /\ s.calls \in Seq([action: {"read", "write", "mentions"},
                        repo: Repos, preflight: BOOLEAN])

RepositoryContainment ==
    \A i \in 1..Len(s.calls): Allowed(s.calls[i].repo, s.policy)
PreflightBeforeCalls == \A i \in 1..Len(s.calls): s.calls[i].preflight
RejectedNoCalls == s.rejected => s.calls = <<>>
SameRepositoryLink == s.kind = "link" /\ s.calls # <<>> => s.parent = s.sub
TrustedParent ==
    s.kind = "link" /\ s.resolved /\ s.target # "*" =>
        s.parent = DefaultRepo(s.policy)
StagedNoWrites ==
    s.staged => \A i \in 1..Len(s.calls): s.calls[i].action # "write"
DeferredNoCalls == s.stage = "deferred" => s.calls = <<>>

\* False reachability predicates demonstrate non-vacuous secure executions.
NoSuccessfulWrite == \A i \in 1..Len(s.calls): s.calls[i].action # "write"
NoRejectedReference == ~s.rejected
NoDeferredRetry == ~s.retried
=============================================================================
