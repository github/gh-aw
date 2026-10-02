---------------------- MODULE DispatchWorkCoordinator ----------------------
EXTENDS Naturals, FiniteSets, Sequences, TLC

CONSTANTS WorkCount, ClaimCount, WorkerCount, DispatcherCount, RetryLimit,
          MaxHead, MaxLog

Works == 1..WorkCount
Claims == 1..ClaimCount
Workers == 1..WorkerCount
Dispatchers == 1..DispatcherCount

ASSUME /\ WorkCount \in Nat \ {0} /\ ClaimCount \in Nat \ {0}
       /\ WorkerCount \in Nat \ {0} /\ DispatcherCount \in Nat \ {0}
       /\ RetryLimit \in Nat

WorkOf(c) == ((c - 1) % WorkCount) + 1
Inbound(a) == ((a - 1) % ClaimCount) + 1
Origin(c) == ((c - 1) % DispatcherCount) + 1

Fact(k, w, c, a) == [kind |-> k, work |-> w, claim |-> c, attempt |-> a]
Work(w) == Fact("Work", w, 0, 0)
Claim(c) == Fact("Claim", WorkOf(c), c, 0)
ClaimCancellation(c) == Fact("ClaimCancellation", WorkOf(c), c, 0)
WorkCancellation(w) == Fact("WorkCancellation", w, 0, 0)
Completion(a) == Fact("Completion", WorkOf(Inbound(a)), Inbound(a), a)

AllFacts == {Work(w) : w \in Works}
            \cup {Claim(c) : c \in Claims}
            \cup {ClaimCancellation(c) : c \in Claims}
            \cup {WorkCancellation(w) : w \in Works}
            \cup {Completion(a) : a \in Workers}
Intents == {t \in AllFacts : t.kind \in {"Work", "Claim", "WorkCancellation"}}
Facts(s) == {s[i] : i \in 1..Len(s)}
Terminals(f) == {t \in f : t.kind \in {"Completion", "WorkCancellation"}}
WorkFacts(f, w) == {t \in f : t.work = w}
Terminal(f, w) == \E t \in Terminals(f) : t.work = w
Completed(f, w) == {t \in f : t.kind = "Completion" /\ t.work = w}
Active(f, w) == {c \in Claims : WorkOf(c) = w /\ Claim(c) \in f
                                            /\ ClaimCancellation(c) \notin f}
Minimum(s) == IF s = {} THEN 0 ELSE CHOOSE c \in s : \A d \in s : c <= d
Winner(f, w) ==
    IF Completed(f, w) # {} THEN (CHOOSE t \in Completed(f, w) : TRUE).claim
    ELSE IF WorkCancellation(w) \in f THEN 0 ELSE Minimum(Active(f, w))

State(f, w) ==
    IF Work(w) \notin f THEN "absent"
    ELSE IF Completed(f, w) # {} THEN "completed"
    ELSE IF WorkCancellation(w) \in f THEN "cancelled"
    ELSE IF Winner(f, w) # 0 THEN "claimed" ELSE "available"

Projection(f) ==
    [work |-> [w \in Works |-> State(f, w)],
     winner |-> [w \in Works |-> Winner(f, w)],
     claim |-> [c \in Claims |->
         IF Claim(c) \notin f THEN "absent"
         ELSE IF ClaimCancellation(c) \in f \/ WorkCancellation(WorkOf(c)) \in f
              THEN "cancelled"
         ELSE IF Winner(f, WorkOf(c)) = c THEN "effective" ELSE "superseded"],
     transactions |-> f]
Replay(s) == Projection(Facts(s))

Allowed(f, t) ==
    /\ t \in AllFacts /\ t \notin f
    /\ ~Terminal(f, t.work)
    /\ CASE t.kind = "Work" -> Work(t.work) \notin f
         [] t.kind = "Claim" -> Work(t.work) \in f
         [] t.kind = "WorkCancellation" -> Work(t.work) \in f
         [] t.kind = "ClaimCancellation" -> Claim(t.claim) \in f
         [] t.kind = "Completion" ->
                /\ Work(t.work) \in f
                /\ t.claim = Winner(f, t.work)
                /\ t = Completion(t.attempt)
         [] OTHER -> FALSE

RECURSIVE Apply(_, _), Canonical(_)
Apply(s, intents) ==
    IF intents = <<>> THEN s
    ELSE LET t == Head(intents)
             next == IF Allowed(Facts(s), t) THEN Append(s, t) ELSE s
         IN Apply(next, Tail(intents))
Canonical(f) ==
    IF f = {} THEN <<>>
    ELSE LET t == CHOOSE t \in f : TRUE
         IN <<t>> \o Canonical(f \ {t})

VARIABLES log, head, dispatch, workers, compact, recovery, deadRuns,
          terminalHistory, authorizations, effects
vars == <<log, head, dispatch, workers, compact, recovery, deadRuns,
          terminalHistory, authorizations, effects>>

Snapshot == [phase |-> "idle", base |-> 0, source |-> <<>>,
             candidate |-> <<>>, observedRuns |-> {}, retries |-> 0]
Init ==
    /\ log = <<>> /\ head = 0 /\ deadRuns = {}
    /\ terminalHistory = [w \in Works |-> {}]
    /\ authorizations = <<>> /\ effects = <<>>
    /\ dispatch = [d \in Dispatchers |->
         [phase |-> "agent", local |-> <<>>, serialized |-> <<>>,
          base |-> 0, source |-> <<>>, candidate |-> <<>>, retries |-> 0]]
    /\ workers = [a \in Workers |->
         [phase |-> "waiting", finish |-> FALSE,
          base |-> 0, source |-> <<>>, candidate |-> <<>>, retries |-> 0]]
    /\ compact = Snapshot /\ recovery = Snapshot

Commit(candidate) ==
    /\ log' = candidate /\ head' = head + 1
    /\ terminalHistory' = [w \in Works |->
         IF terminalHistory[w] = {} /\ Terminal(Facts(candidate), w)
         THEN WorkFacts(Facts(candidate), w) ELSE terminalHistory[w]]
Write(candidate) ==
    IF candidate = log THEN UNCHANGED <<log, head, terminalHistory>>
    ELSE Commit(candidate)
Publish(snapshot) ==
    /\ snapshot.base = head /\ snapshot.source = log
    /\ Write(snapshot.candidate)

LocalView(d) == Apply(log, dispatch[d].local)
Stage(d, t) ==
    /\ dispatch[d].phase = "agent" /\ t \in Intents
    /\ (t.kind # "Claim" \/ Origin(t.claim) = d)
    /\ Allowed(Facts(LocalView(d)), t)
    /\ dispatch' = [dispatch EXCEPT
         ![d].local = Append(@, t), ![d].serialized = Append(@, t)]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

PrepareDispatch(d) ==
    /\ dispatch[d].phase = "agent"
    /\ dispatch' = [dispatch EXCEPT
         ![d].phase = "prepared", ![d].base = head, ![d].source = log,
         ![d].candidate = Apply(log, dispatch[d].serialized)]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

PushDispatch(d) ==
    /\ dispatch[d].phase = "prepared" /\ Publish(dispatch[d])
    /\ dispatch' = [dispatch EXCEPT ![d].phase = "done"]
    /\ UNCHANGED <<workers, compact, recovery, deadRuns, authorizations, effects>>

RetryDispatch(d) ==
    /\ dispatch[d].phase = "prepared" /\ dispatch[d].base # head
    /\ dispatch' = IF dispatch[d].retries = RetryLimit
         THEN [dispatch EXCEPT ![d].phase = "failed"]
         ELSE [dispatch EXCEPT ![d].base = head, ![d].source = log,
               ![d].candidate = Apply(log, dispatch[d].serialized),
               ![d].retries = @ + 1]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

Activate(a) ==
    LET c == Inbound(a) IN
    /\ workers[a].phase = "waiting" /\ c \notin deadRuns
    /\ Claim(c) \in Facts(log) /\ ~Terminal(Facts(log), WorkOf(c))
    /\ Winner(Facts(log), WorkOf(c)) = c
    /\ workers' = [workers EXCEPT ![a].phase = "running"]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

EndAgent(a, finalize) ==
    /\ workers[a].phase = "running" /\ finalize \in BOOLEAN
    /\ workers' = [workers EXCEPT ![a].phase = "ready", ![a].finish = finalize]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

WorkerCandidate(source, a) ==
    LET c == Inbound(a) IN
    IF ~Terminal(Facts(source), WorkOf(c)) /\ Winner(Facts(source), WorkOf(c)) = c
    THEN Apply(source, <<IF workers[a].finish THEN Completion(a)
                       ELSE ClaimCancellation(c)>>)
    ELSE source

PrepareWorker(a) ==
    /\ workers[a].phase = "ready"
    /\ workers' = [workers EXCEPT ![a].phase = "prepared", ![a].base = head,
                   ![a].source = log, ![a].candidate = WorkerCandidate(log, a)]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

PushWorker(a) ==
    /\ workers[a].phase = "prepared" /\ Publish(workers[a])
    /\ workers' = [workers EXCEPT ![a].phase =
         IF Completion(a) \in Facts(workers[a].candidate) \ Facts(log)
         THEN "committed" ELSE "stopped"]
    /\ UNCHANGED <<dispatch, compact, recovery, deadRuns, authorizations, effects>>

RetryWorker(a) ==
    /\ workers[a].phase = "prepared" /\ workers[a].base # head
    /\ workers' = IF workers[a].retries = RetryLimit
         THEN [workers EXCEPT ![a].phase = "failed"]
         ELSE [workers EXCEPT ![a].base = head, ![a].source = log,
               ![a].candidate = WorkerCandidate(log, a), ![a].retries = @ + 1]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

VerifyCompletion(a) ==
    /\ workers[a].phase = "committed" /\ Completion(a) \in Facts(log)
    /\ Completed(Facts(log), WorkOf(Inbound(a))) = {Completion(a)}
    /\ authorizations' = Append(authorizations, Completion(a))
    /\ workers' = [workers EXCEPT ![a].phase = "authorized"]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, effects>>

ExternalEffect(a) ==
    /\ workers[a].phase = "authorized"
    /\ effects' = Append(effects, Completion(a))
    /\ workers' = [workers EXCEPT ![a].phase = "done"]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, authorizations>>

RunTerminates(c) ==
    /\ Claim(c) \in Facts(log) /\ c \notin deadRuns
    /\ deadRuns' = deadRuns \cup {c}
    /\ workers' = [a \in Workers |->
         IF Inbound(a) = c /\ workers[a].phase \notin {"done", "stopped", "failed"}
         THEN [workers[a] EXCEPT !.phase = "failed"] ELSE workers[a]]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery,
                   terminalHistory, authorizations, effects>>

Orphans(source, observedRuns) == {ClaimCancellation(c) : c \in
    {c \in observedRuns : Claim(c) \in Facts(source)
                         /\ ~Terminal(Facts(source), WorkOf(c))
                         /\ ClaimCancellation(c) \notin Facts(source)}}
MaintenanceCandidate(kind, source, observedRuns) ==
    IF kind = "compact" THEN Canonical(Facts(source))
    ELSE Apply(source, Canonical(Orphans(source, observedRuns)))
Maintenance(kind) == IF kind = "compact" THEN compact ELSE recovery

SetMaintenance(kind, value) ==
    /\ compact' = IF kind = "compact" THEN value ELSE compact
    /\ recovery' = IF kind = "recovery" THEN value ELSE recovery

PrepareMaintenance(kind) ==
    LET observed == IF kind = "compact" THEN {} ELSE deadRuns IN
    /\ Maintenance(kind).phase \in {"idle", "done"}
    /\ MaintenanceCandidate(kind, log, observed) # log
    /\ SetMaintenance(kind, [phase |-> "prepared", base |-> head, source |-> log,
         candidate |-> MaintenanceCandidate(kind, log, observed),
         observedRuns |-> observed, retries |-> 0])
    /\ UNCHANGED <<log, head, dispatch, workers, deadRuns,
                   terminalHistory, authorizations, effects>>

PushMaintenance(kind) ==
    /\ Maintenance(kind).phase = "prepared" /\ Publish(Maintenance(kind))
    /\ SetMaintenance(kind, [Maintenance(kind) EXCEPT !.phase = "done"])
    /\ UNCHANGED <<dispatch, workers, deadRuns, authorizations, effects>>

RetryMaintenance(kind) ==
    LET observed == IF kind = "compact" THEN {} ELSE deadRuns IN
    /\ Maintenance(kind).phase = "prepared" /\ Maintenance(kind).base # head
    /\ SetMaintenance(kind,
         IF Maintenance(kind).retries = RetryLimit
         THEN [Maintenance(kind) EXCEPT !.phase = "failed"]
         ELSE [Maintenance(kind) EXCEPT !.base = head, !.source = log,
               !.candidate = MaintenanceCandidate(kind, log, observed),
               !.observedRuns = observed, !.retries = @ + 1])
    /\ UNCHANGED <<log, head, dispatch, workers, deadRuns,
                   terminalHistory, authorizations, effects>>

DuplicateRecord(t) ==
    /\ t \in Facts(log)
    /\ Publish([base |-> head, source |-> log, candidate |-> Append(log, t)])
    /\ UNCHANGED <<dispatch, workers, compact, recovery, deadRuns,
                   authorizations, effects>>

Next ==
    \/ \E d \in Dispatchers :
         (\E t \in Intents : Stage(d, t)) \/ PrepareDispatch(d)
         \/ PushDispatch(d) \/ RetryDispatch(d)
    \/ \E a \in Workers :
         Activate(a) \/ (\E b \in BOOLEAN : EndAgent(a, b))
         \/ PrepareWorker(a) \/ PushWorker(a) \/ RetryWorker(a)
         \/ VerifyCompletion(a) \/ ExternalEffect(a)
    \/ \E c \in Claims : RunTerminates(c)
    \/ \E kind \in {"compact", "recovery"} :
         PrepareMaintenance(kind) \/ PushMaintenance(kind) \/ RetryMaintenance(kind)
    \/ \E t \in AllFacts : DuplicateRecord(t)
Spec == Init /\ [][Next]_vars
WorkerOneShot ==
    [] [\A a \in Workers :
         workers[a].phase \in {"done", "stopped", "failed"} =>
              workers'[a].phase = workers[a].phase]_vars

ValidFacts(f) ==
    /\ f \subseteq AllFacts
    /\ \A t \in f :
         /\ (t.kind # "Work" => Work(t.work) \in f)
         /\ (t.kind \in {"ClaimCancellation", "Completion"} => Claim(t.claim) \in f)
    /\ \A w \in Works : Cardinality({t \in Terminals(f) : t.work = w}) <= 1
    /\ \A t \in f : t.kind = "Completion" =>
         /\ t = Completion(t.attempt) /\ t.claim = Minimum(Active(f, t.work))
ValidLog == ValidFacts(Facts(log))

TypeOK ==
    /\ log \in Seq(AllFacts) /\ head \in Nat /\ deadRuns \subseteq Claims
    /\ terminalHistory \in [Works -> SUBSET AllFacts]
    /\ authorizations \in Seq({Completion(a) : a \in Workers})
    /\ effects \in Seq({Completion(a) : a \in Workers})
    /\ dispatch \in [Dispatchers ->
         [phase : {"agent", "prepared", "done", "failed"},
          local : Seq(Intents), serialized : Seq(Intents), base : Nat,
          source : Seq(AllFacts), candidate : Seq(AllFacts), retries : 0..RetryLimit]]
    /\ workers \in [Workers ->
         [phase : {"waiting", "running", "ready", "prepared", "committed",
                   "authorized", "done", "stopped", "failed"},
          finish : BOOLEAN, base : Nat, source : Seq(AllFacts),
          candidate : Seq(AllFacts), retries : 0..RetryLimit]]
    /\ compact \in [phase : {"idle", "prepared", "done", "failed"}, base : Nat,
                   source : Seq(AllFacts), candidate : Seq(AllFacts),
                   observedRuns : SUBSET Claims, retries : 0..RetryLimit]
    /\ recovery \in [phase : {"idle", "prepared", "done", "failed"}, base : Nat,
                    source : Seq(AllFacts), candidate : Seq(AllFacts),
                    observedRuns : SUBSET Claims, retries : 0..RetryLimit]

PreparedExtension(record) ==
    record.phase = "prepared" /\ record.base = head =>
         /\ record.source = log
         /\ Facts(log) \subseteq Facts(record.candidate)
         /\ ValidFacts(Facts(record.candidate))
SnapshotValidity ==
    /\ \A d \in Dispatchers : PreparedExtension(dispatch[d])
    /\ \A a \in Workers : PreparedExtension(workers[a])
    /\ PreparedExtension(recovery)
    /\ (compact.phase = "prepared" /\ compact.base = head =>
         compact.source = log /\ Facts(compact.candidate) = Facts(log))
CandidateDerivation ==
    /\ \A d \in Dispatchers : dispatch[d].phase = "prepared" =>
         dispatch[d].candidate = Apply(dispatch[d].source, dispatch[d].serialized)
    /\ \A a \in Workers : workers[a].phase = "prepared" =>
         workers[a].candidate = WorkerCandidate(workers[a].source, a)
    /\ compact.phase = "prepared" =>
         compact.candidate = Canonical(Facts(compact.source))
    /\ recovery.phase = "prepared" =>
         recovery.candidate = MaintenanceCandidate("recovery",
             recovery.source, recovery.observedRuns)
SnapshotVersions ==
    /\ \A d \in Dispatchers : dispatch[d].base <= head
    /\ \A a \in Workers : workers[a].base <= head
    /\ compact.base <= head /\ recovery.base <= head
Serialization == \A d \in Dispatchers : dispatch[d].local = dispatch[d].serialized
TerminalHistoryValid ==
    \A w \in Works : terminalHistory[w] # {} =>
         Terminal(terminalHistory[w], w)
         /\ terminalHistory[w] = WorkFacts(terminalHistory[w], w)
TerminalPersistence ==
    \A w \in Works : Terminals(terminalHistory[w]) \subseteq Facts(log)
TerminalFreeze ==
    \A w \in Works : terminalHistory[w] # {} =>
         WorkFacts(Facts(log), w) = terminalHistory[w]
SingleCompletionPerWorker ==
    \A a \in Workers : Cardinality({t \in Facts(log) :
         t.kind = "Completion" /\ t.attempt = a}) <= 1
SingleEffectiveClaim ==
    \A w \in Works : Cardinality({c \in Active(Facts(log), w) :
                                  c = Winner(Facts(log), w)}) <= 1
AuthorizationSoundness == Facts(authorizations) \subseteq
                           {t \in Facts(log) : t.kind = "Completion"}
EffectSoundness == Facts(effects) \subseteq Facts(authorizations)
CountAttempt(s, a) == Cardinality({i \in 1..Len(s) : s[i].attempt = a})
WorkerOrigin ==
    \A a \in Workers :
         /\ (workers[a].phase = "prepared" /\ workers[a].base = head =>
              /\ Facts(workers[a].candidate) \ Facts(log) \subseteq
                   {Completion(a), ClaimCancellation(Inbound(a))}
              /\ (~workers[a].finish =>
                   Completion(a) \notin Facts(workers[a].candidate) \ Facts(log)))
         /\ (workers[a].phase = "committed" =>
              /\ workers[a].finish /\ Completion(a) \in Facts(log)
              /\ CountAttempt(authorizations, a) = 0)
FinishRequired ==
    \A t \in Facts(authorizations) : workers[t.attempt].finish
LifecycleAccounting ==
    \A a \in Workers :
         /\ CountAttempt(authorizations, a) <= 1
         /\ CountAttempt(effects, a) <= CountAttempt(authorizations, a)
         /\ (CountAttempt(authorizations, a) = 1 =>
              workers[a].phase \in {"authorized", "done", "failed"})
         /\ (CountAttempt(effects, a) = 1 =>
              workers[a].phase \in {"done", "failed"})
         /\ (workers[a].phase = "authorized" =>
              CountAttempt(authorizations, a) = 1 /\ CountAttempt(effects, a) = 0)
         /\ (workers[a].phase = "done" => CountAttempt(effects, a) = 1)
SingleAuthorization ==
    \A w \in Works : Cardinality({i \in 1..Len(authorizations) :
                                  authorizations[i].work = w}) <= 1
SingleEffect ==
    \A w \in Works : Cardinality({i \in 1..Len(effects) : effects[i].work = w}) <= 1
Safety ==
    /\ TypeOK /\ ValidLog /\ SnapshotValidity /\ SnapshotVersions /\ Serialization
    /\ CandidateDerivation /\ TerminalHistoryValid /\ TerminalPersistence /\ TerminalFreeze
    /\ SingleCompletionPerWorker /\ SingleEffectiveClaim /\ AuthorizationSoundness
    /\ EffectSoundness /\ WorkerOrigin /\ FinishRequired
    /\ LifecycleAccounting /\ SingleAuthorization /\ SingleEffect

Bound == head <= MaxHead /\ Len(log) <= MaxLog

\* Negative controls are deliberately excluded from Next.
UnsafeStalePush(d) ==
    /\ dispatch[d].phase = "prepared" /\ dispatch[d].base # head
    /\ Commit(dispatch[d].candidate)
    /\ dispatch' = [dispatch EXCEPT ![d].phase = "done"]
    /\ UNCHANGED <<workers, compact, recovery, deadRuns, authorizations, effects>>
UnsafeLateClaim(c) ==
    /\ Completed(Facts(log), WorkOf(c)) # {} /\ Claim(c) \notin Facts(log)
    /\ Commit(Append(log, Claim(c)))
    /\ UNCHANGED <<dispatch, workers, compact, recovery, deadRuns,
                   authorizations, effects>>

BrokenCASSpec ==
    Init /\ [][Next \/ (\E d \in Dispatchers : UnsafeStalePush(d))]_vars
BrokenTerminalSpec ==
    Init /\ [][Next \/ (\E c \in Claims : UnsafeLateClaim(c))]_vars

THEOREM ReplayDeterminism ==
    \A a, b \in Seq(AllFacts) : Facts(a) = Facts(b) => Replay(a) = Replay(b)
THEOREM CompactionEquivalence ==
    \A s \in Seq(AllFacts) : Replay(s) = Replay(Canonical(Facts(s)))
THEOREM ProtocolSafety == Spec => []Safety
THEOREM WorkerCannotRestart == Spec => WorkerOneShot
=============================================================================
