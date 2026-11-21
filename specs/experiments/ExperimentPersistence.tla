----------------------- MODULE ExperimentPersistence -----------------------
EXTENDS FiniteSets, Naturals, Sequences

CONSTANTS Runs, MaxAttempts, HistoryLimit
VARIABLES mode, active, jobs, repo, cache

Empty == [counts |-> [v \in 1..2 |-> 0], history |-> <<>>]
InitialJob == [phase |-> "start", restored |-> Empty, variant |-> 0,
               local |-> Empty, artifact |-> Empty, tries |-> 0, committed |-> FALSE]
Vars == <<mode, active, jobs, repo, cache>>

Init ==
    /\ mode \in {"repo", "cache"}
    /\ active \in SUBSET Runs
    /\ jobs = [r \in Runs |-> InitialJob]
    /\ repo = Empty
    /\ cache = {Empty}

Restore(r) ==
    /\ jobs[r].phase = "start"
    /\ \E snapshot \in IF mode = "repo" THEN {repo} ELSE cache :
        jobs' = [jobs EXCEPT ![r].phase = "restored", ![r].restored = snapshot]
    /\ UNCHANGED <<mode, active, repo, cache>>

Select(r) ==
    LET s == jobs[r].restored
        recent == IF Len(s.history) = HistoryLimit THEN Tail(s.history) ELSE s.history
        eligible == IF r \notin active THEN {1}
                     ELSE {v \in 1..2 : s.counts[v] <= s.counts[3 - v]}
    IN
    /\ jobs[r].phase = "restored"
    /\ \E v \in eligible :
        /\ jobs' = [jobs EXCEPT ![r].phase = "selected", ![r].variant = v,
                        ![r].local = IF r \notin active THEN s ELSE
                            [counts |-> [s.counts EXCEPT ![v] = @ + 1],
                             history |-> Append(recent, r)]]
    /\ UNCHANGED <<mode, active, repo, cache>>

SaveCache(r) ==
    /\ mode = "cache"
    /\ jobs[r].phase = "selected"
    /\ jobs' = [jobs EXCEPT ![r].phase = "saved"]
    /\ cache' = IF r \in active THEN cache \cup {jobs[r].local} ELSE cache
    /\ UNCHANGED <<mode, active, repo>>

Publish(r) ==
    /\ jobs[r].phase = IF mode = "repo" THEN "selected" ELSE "saved"
    /\ jobs' = [jobs EXCEPT ![r].phase = "published", ![r].artifact = jobs[r].local]
    /\ UNCHANGED <<mode, active, repo, cache>>

Commit(r) ==
    /\ mode = "repo"
    /\ jobs[r].phase = "published"
    /\ r \in active
    /\ jobs' = [jobs EXCEPT ![r].phase = "committed", ![r].tries = @ + 1,
                            ![r].committed = TRUE]
    /\ repo' = jobs[r].artifact
    /\ UNCHANGED <<mode, active, cache>>

PushFailure(r) ==
    /\ mode = "repo"
    /\ jobs[r].phase = "published"
    /\ r \in active
    /\ jobs' = [jobs EXCEPT ![r].tries = @ + 1,
                            ![r].phase = IF jobs[r].tries + 1 = MaxAttempts
                                        THEN "failed" ELSE @]
    /\ UNCHANGED <<mode, active, repo, cache>>

Conclude(r) ==
    /\ jobs[r].phase =
        IF mode = "repo" /\ r \in active THEN "committed" ELSE "published"
    /\ jobs' = [jobs EXCEPT ![r].phase = "concluded"]
    /\ UNCHANGED <<mode, active, repo, cache>>

Next == \E r \in Runs :
    Restore(r) \/ Select(r) \/ SaveCache(r) \/ Publish(r) \/
    Commit(r) \/ PushFailure(r) \/ Conclude(r)
Spec == Init /\ [][Next]_Vars /\ WF_Vars(Next)

HistoryOK(s) ==
    /\ s.counts \in [1..2 -> 0..Cardinality(Runs)]
    /\ Len(s.history) <= HistoryLimit
    /\ \A i \in 1..Len(s.history) : s.history[i] \in active

ValidSnapshots ==
    /\ HistoryOK(repo)
    /\ \A s \in cache : HistoryOK(s)
    /\ \A r \in Runs :
        /\ HistoryOK(jobs[r].restored)
        /\ HistoryOK(jobs[r].local)
        /\ HistoryOK(jobs[r].artifact)

PublicationAndConclusion ==
    \A r \in Runs :
        LET j == jobs[r] IN
        /\ j.tries \in 0..MaxAttempts
        /\ j.phase \in {"published", "committed", "failed", "concluded"} =>
            j.artifact = j.local
        /\ mode = "cache" => j.tries = 0 /\ ~j.committed
        /\ j.phase = "concluded" /\ mode = "repo" /\ r \in active => j.committed
        /\ j.phase = "failed" => j.tries = MaxAttempts
        /\ mode = "cache" /\ r \in active /\
            j.phase \in {"published", "concluded"} => j.artifact \in cache

SelectionUsesRestoredSnapshot ==
    \A r \in Runs :
        LET j == jobs[r] IN
        j.variant # 0 =>
            IF r \in active THEN
                /\ j.variant \in 1..2
                /\ j.restored.counts[j.variant] <= j.restored.counts[3 - j.variant]
                /\ j.local.counts[j.variant] = j.restored.counts[j.variant] + 1
                /\ j.local.counts[3 - j.variant] = j.restored.counts[3 - j.variant]
                /\ Len(j.local.history) > 0
                /\ j.local.history[Len(j.local.history)] = r
            ELSE /\ j.variant = 1
                 /\ j.local = j.restored

BackendsStaySeparate ==
    /\ mode = "repo" => cache = {Empty}
    /\ mode = "cache" => repo = Empty

RunFinishes == \A r \in Runs : <>(jobs[r].phase \in {"concluded", "failed"})
=============================================================================
