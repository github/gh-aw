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
    /\ CASE t.kind = "Work" -> Work(t.work) \notin f
         [] t.kind = "Claim" -> Work(t.work) \in f /\ ~Terminal(f, t.work)
         [] t.kind = "WorkCancellation" ->
                Work(t.work) \in f /\ ~Terminal(f, t.work)
         [] t.kind = "ClaimCancellation" ->
                Claim(t.claim) \in f /\ ~Terminal(f, t.work)
         [] t.kind = "Completion" ->
                /\ Work(t.work) \in f /\ ~Terminal(f, t.work)
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

Snapshot == [phase |-> "idle", base |-> 0, candidate |-> <<>>, retries |-> 0]
Init ==
    /\ log = <<>> /\ head = 0 /\ deadRuns = {}
    /\ terminalHistory = {} /\ authorizations = <<>> /\ effects = <<>>
    /\ dispatch = [d \in Dispatchers |->
         [phase |-> "agent", local |-> <<>>, serialized |-> <<>>,
          base |-> 0, candidate |-> <<>>, retries |-> 0]]
    /\ workers = [a \in Workers |->
         [phase |-> "waiting", finish |-> FALSE,
          base |-> 0, candidate |-> <<>>, retries |-> 0]]
    /\ compact = Snapshot /\ recovery = Snapshot

Commit(candidate) ==
    /\ log' = candidate /\ head' = head + 1
    /\ terminalHistory' = terminalHistory \cup Terminals(Facts(candidate))
Write(candidate) ==
    IF candidate = log THEN UNCHANGED <<log, head, terminalHistory>>
    ELSE Commit(candidate)

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
         ![d].phase = "prepared", ![d].base = head,
         ![d].candidate = Apply(log, dispatch[d].serialized)]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

PushDispatch(d) ==
    /\ dispatch[d].phase = "prepared" /\ dispatch[d].base = head
    /\ Write(dispatch[d].candidate)
    /\ dispatch' = [dispatch EXCEPT ![d].phase = "done"]
    /\ UNCHANGED <<workers, compact, recovery, deadRuns, authorizations, effects>>

RetryDispatch(d) ==
    /\ dispatch[d].phase = "prepared" /\ dispatch[d].base # head
    /\ dispatch' = IF dispatch[d].retries = RetryLimit
         THEN [dispatch EXCEPT ![d].phase = "failed"]
         ELSE [dispatch EXCEPT ![d].base = head,
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

WorkerCandidate(a) ==
    LET c == Inbound(a) IN
    IF ~Terminal(Facts(log), WorkOf(c)) /\ Winner(Facts(log), WorkOf(c)) = c
    THEN Apply(log, <<IF workers[a].finish THEN Completion(a)
                       ELSE ClaimCancellation(c)>>)
    ELSE log

PrepareWorker(a) ==
    /\ workers[a].phase = "ready"
    /\ workers' = [workers EXCEPT ![a].phase = "prepared", ![a].base = head,
                   ![a].candidate = WorkerCandidate(a)]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

PushWorker(a) ==
    /\ workers[a].phase = "prepared" /\ workers[a].base = head
    /\ Write(workers[a].candidate)
    /\ workers' = [workers EXCEPT ![a].phase =
         IF Completion(a) \in Facts(workers[a].candidate) \ Facts(log)
         THEN "committed" ELSE "stopped"]
    /\ UNCHANGED <<dispatch, compact, recovery, deadRuns, authorizations, effects>>

RetryWorker(a) ==
    /\ workers[a].phase = "prepared" /\ workers[a].base # head
    /\ workers' = IF workers[a].retries = RetryLimit
         THEN [workers EXCEPT ![a].phase = "failed"]
         ELSE [workers EXCEPT ![a].base = head,
               ![a].candidate = WorkerCandidate(a), ![a].retries = @ + 1]
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

Orphans == {ClaimCancellation(c) : c \in
    {c \in deadRuns : Claim(c) \in Facts(log) /\ ~Terminal(Facts(log), WorkOf(c))
                     /\ ClaimCancellation(c) \notin Facts(log)}}
MaintenanceCandidate(kind) ==
    IF kind = "compact" THEN Canonical(Facts(log))
    ELSE Apply(log, Canonical(Orphans))
Maintenance(kind) == IF kind = "compact" THEN compact ELSE recovery

SetMaintenance(kind, value) ==
    /\ compact' = IF kind = "compact" THEN value ELSE compact
    /\ recovery' = IF kind = "recovery" THEN value ELSE recovery

PrepareMaintenance(kind) ==
    /\ Maintenance(kind).phase \in {"idle", "done"}
    /\ MaintenanceCandidate(kind) # log
    /\ SetMaintenance(kind, [phase |-> "prepared", base |-> head,
                             candidate |-> MaintenanceCandidate(kind), retries |-> 0])
    /\ UNCHANGED <<log, head, dispatch, workers, deadRuns,
                   terminalHistory, authorizations, effects>>

PushMaintenance(kind) ==
    /\ Maintenance(kind).phase = "prepared" /\ Maintenance(kind).base = head
    /\ Write(Maintenance(kind).candidate)
    /\ SetMaintenance(kind, [Maintenance(kind) EXCEPT !.phase = "done"])
    /\ UNCHANGED <<dispatch, workers, deadRuns, authorizations, effects>>

RetryMaintenance(kind) ==
    /\ Maintenance(kind).phase = "prepared" /\ Maintenance(kind).base # head
    /\ SetMaintenance(kind,
         IF Maintenance(kind).retries = RetryLimit
         THEN [Maintenance(kind) EXCEPT !.phase = "failed"]
         ELSE [Maintenance(kind) EXCEPT !.base = head,
               !.candidate = MaintenanceCandidate(kind), !.retries = @ + 1])
    /\ UNCHANGED <<log, head, dispatch, workers, deadRuns,
                   terminalHistory, authorizations, effects>>

DuplicateRecord(t) ==
    /\ t \in Facts(log) /\ Commit(Append(log, t))
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
    /\ terminalHistory \subseteq Terminals(AllFacts)
    /\ authorizations \in Seq({Completion(a) : a \in Workers})
    /\ effects \in Seq({Completion(a) : a \in Workers})
    /\ dispatch \in [Dispatchers ->
         [phase : {"agent", "prepared", "done", "failed"},
          local : Seq(Intents), serialized : Seq(Intents), base : Nat,
          candidate : Seq(AllFacts), retries : 0..RetryLimit]]
    /\ workers \in [Workers ->
         [phase : {"waiting", "running", "ready", "prepared", "committed",
                   "authorized", "done", "stopped", "failed"},
          finish : BOOLEAN, base : Nat, candidate : Seq(AllFacts), retries : 0..RetryLimit]]
    /\ compact \in [phase : {"idle", "prepared", "done", "failed"}, base : Nat,
                   candidate : Seq(AllFacts), retries : 0..RetryLimit]
    /\ recovery \in [phase : {"idle", "prepared", "done", "failed"}, base : Nat,
                    candidate : Seq(AllFacts), retries : 0..RetryLimit]

PreparedExtension(record) ==
    record.phase = "prepared" /\ record.base = head =>
         /\ Facts(log) \subseteq Facts(record.candidate)
         /\ ValidFacts(Facts(record.candidate))
SnapshotValidity ==
    /\ \A d \in Dispatchers : PreparedExtension(dispatch[d])
    /\ \A a \in Workers : PreparedExtension(workers[a])
    /\ PreparedExtension(recovery)
    /\ (compact.phase = "prepared" /\ compact.base = head =>
         Facts(compact.candidate) = Facts(log))
SnapshotVersions ==
    /\ \A d \in Dispatchers : dispatch[d].base <= head
    /\ \A a \in Workers : workers[a].base <= head
    /\ compact.base <= head /\ recovery.base <= head
Serialization == \A d \in Dispatchers : dispatch[d].local = dispatch[d].serialized
TerminalPersistence == terminalHistory \subseteq Facts(log)
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
    /\ TerminalPersistence /\ SingleEffectiveClaim /\ AuthorizationSoundness
    /\ EffectSoundness /\ WorkerOrigin /\ FinishRequired
    /\ LifecycleAccounting /\ SingleAuthorization /\ SingleEffect

Bound == head <= MaxHead /\ Len(log) <= MaxLog

\* Negative controls are deliberately excluded from Next.
UnsafeReauthorize(a) ==
    /\ workers[a].phase \in {"ready", "done", "stopped"} /\ workers[a].finish
    /\ \E t \in Completed(Facts(log), WorkOf(Inbound(a))) : t.claim = Inbound(a)
    /\ authorizations' = Append(authorizations, Completion(a))
    /\ workers' = [workers EXCEPT ![a].phase = "authorized"]
    /\ UNCHANGED <<log, head, dispatch, compact, recovery, deadRuns,
                   terminalHistory, effects>>
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

BrokenAuthorizationSpec ==
    Init /\ [][Next \/ (\E a \in Workers : UnsafeReauthorize(a))]_vars
BrokenCASSpec ==
    Init /\ [][Next \/ (\E d \in Dispatchers : UnsafeStalePush(d))]_vars
BrokenTerminalSpec ==
    Init /\ [][Next \/ (\E c \in Claims : UnsafeLateClaim(c))]_vars

THEOREM ReplayDeterminism ==
    \A a, b \in Seq(AllFacts) : Facts(a) = Facts(b) => Replay(a) = Replay(b)
THEOREM CompactionEquivalence ==
    \A s \in Seq(AllFacts) : Replay(s) = Replay(Canonical(Facts(s)))
THEOREM ProtocolSafety == Spec => []Safety
=============================================================================
