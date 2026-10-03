---------------------- MODULE DispatchWorkCoordinator ----------------------
EXTENDS Naturals, FiniteSets, Sequences, TLC

CONSTANTS WorkCount, ClaimCount, WorkerCount, DispatcherCount, RetryLimit,
          MaxHead, MaxLog, QueueEnabled, SeedQueue

Works == 1..WorkCount
Claims == 1..ClaimCount
Workers == 1..WorkerCount
Dispatchers == 1..DispatcherCount

ASSUME /\ WorkCount \in Nat \ {0} /\ ClaimCount \in Nat \ {0}
       /\ WorkerCount \in Nat \ {0} /\ DispatcherCount \in Nat \ {0}
       /\ RetryLimit \in Nat
       /\ QueueEnabled \in BOOLEAN /\ SeedQueue \in BOOLEAN

WorkOf(c) == ((c - 1) % WorkCount) + 1
Inbound(a) == ((a - 1) % ClaimCount) + 1
Origin(c) == ((c - 1) % DispatcherCount) + 1

ProtocolVersion == 3
Payload(w) == [priority |-> w % 2, cost |-> WorkCount - w + 1,
               group |-> ((w - 1) % 2) + 1]
Fact(k, w, c, a, q) ==
    [version |-> ProtocolVersion, kind |-> k, work_id |-> w,
     claim_id |-> c, attempt_id |-> a, sequence |-> q,
     work |-> IF k = "Work" THEN Payload(w) ELSE 0,
     run_id |-> IF k = "Claim" THEN Origin(c) ELSE 0]
Work(w, q) == Fact("Work", w, 0, 0, q)
WorkIntent(w) == Work(w, 0)
Claim(c) == Fact("Claim", WorkOf(c), c, 0, 0)
ClaimCancellation(c) == Fact("ClaimCancellation", WorkOf(c), c, 0, 0)
WorkCancellation(w) == Fact("WorkCancellation", w, 0, 0, 0)
Completion(a) == Fact("Completion", WorkOf(Inbound(a)), Inbound(a), a, 0)

AllFacts == {Work(w, q) : w \in Works, q \in Works}
            \cup {Claim(c) : c \in Claims}
            \cup {ClaimCancellation(c) : c \in Claims}
            \cup {WorkCancellation(w) : w \in Works}
            \cup {Completion(a) : a \in Workers}
Intents == {WorkIntent(w) : w \in Works}
           \cup {Claim(c) : c \in Claims}
           \cup {WorkCancellation(w) : w \in Works}
Facts(s) == {s[i] : i \in 1..Len(s)}
Terminals(f) == {t \in f : t.kind \in {"Completion", "WorkCancellation"}}
WorkFacts(f, w) == {t \in f : t.work_id = w}
Submitted(f) == {t \in f : t.kind = "Work"}
WorkExists(f, w) == \E t \in Submitted(f) : t.work_id = w
Enqueued(f, w) ==
    IF WorkExists(f, w)
    THEN (CHOOSE t \in Submitted(f) : t.work_id = w).sequence ELSE 0
Maximum(s) == IF s = {} THEN 0 ELSE CHOOSE q \in s : \A r \in s : q >= r
NextSequence(f) == Maximum({t.sequence : t \in Submitted(f)}) + 1
Materialize(f, t) ==
    IF t.kind = "Work" THEN Work(t.work_id, NextSequence(f)) ELSE t
Terminal(f, w) == \E t \in Terminals(f) : t.work_id = w
Completed(f, w) == {t \in f : t.kind = "Completion" /\ t.work_id = w}
Active(f, w) == {c \in Claims : WorkOf(c) = w /\ Claim(c) \in f
                                            /\ ClaimCancellation(c) \notin f}
Minimum(s) == IF s = {} THEN 0 ELSE CHOOSE c \in s : \A d \in s : c <= d
Winner(f, w) ==
    IF Completed(f, w) # {} THEN (CHOOSE t \in Completed(f, w) : TRUE).claim_id
    ELSE IF WorkCancellation(w) \in f THEN 0 ELSE Minimum(Active(f, w))

State(f, w) ==
    IF ~WorkExists(f, w) THEN "absent"
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

DefaultPolicy == [filter |-> Works, sort |-> <<>>, group |-> FALSE, max_active |-> 1,
                  queued |-> TRUE]
OperatorPolicy == [DefaultPolicy EXCEPT !.queued = FALSE]
PriorityPolicy == [DefaultPolicy EXCEPT !.sort = <<"priority-desc">>]
ObjectivePolicy == [DefaultPolicy EXCEPT !.sort = <<"cost-asc", "priority-desc">>]
GroupedPolicy == [PriorityPolicy EXCEPT !.group = TRUE]
FilteredPolicy == [GroupedPolicy EXCEPT !.filter = {w \in Works : w <= 2}]
WideGroupPolicy == [GroupedPolicy EXCEPT !.max_active = 2]
Policies == IF WorkCount = 1 THEN {DefaultPolicy}
            ELSE {DefaultPolicy, PriorityPolicy, ObjectivePolicy,
                  GroupedPolicy, FilteredPolicy, WideGroupPolicy}
ActiveGroup(f, w) ==
    {other \in Works : State(f, other) = "claimed"
                      /\ Payload(other).group = Payload(w).group}
Eligible(f, p) ==
    {w \in p.filter : State(f, w) = "available"
          /\ (~p.group \/ Cardinality(ActiveGroup(f, w)) < p.max_active)}
FieldValue(field, w) ==
    IF field = "priority-desc" THEN Payload(w).priority ELSE Payload(w).cost
RECURSIVE ObjectivesEqual(_, _, _), ObjectivesBefore(_, _, _)
ObjectivesEqual(fields, a, b) ==
    IF fields = <<>> THEN TRUE
    ELSE FieldValue(Head(fields), a) = FieldValue(Head(fields), b)
         /\ ObjectivesEqual(Tail(fields), a, b)
ObjectivesBefore(fields, a, b) ==
    IF fields = <<>> THEN FALSE
    ELSE IF FieldValue(Head(fields), a) = FieldValue(Head(fields), b)
         THEN ObjectivesBefore(Tail(fields), a, b)
         ELSE IF Head(fields) = "priority-desc"
              THEN FieldValue(Head(fields), a) > FieldValue(Head(fields), b)
              ELSE FieldValue(Head(fields), a) < FieldValue(Head(fields), b)
Before(f, p, a, b) ==
    IF ObjectivesEqual(p.sort, a, b)
    THEN Enqueued(f, a) < Enqueued(f, b)
         \/ (Enqueued(f, a) = Enqueued(f, b) /\ a < b)
    ELSE ObjectivesBefore(p.sort, a, b)
NextWork(f, p) ==
    IF Eligible(f, p) = {} THEN 0
    ELSE CHOOSE w \in Eligible(f, p) :
         \A other \in Eligible(f, p) : w = other \/ Before(f, p, w, other)

\* Version 2 has the same facts; the codemod changes only the version field.
Version2(t) == [t EXCEPT !.version = 2]
UpgradeMessage(t) ==
    IF t \in AllFacts THEN t
    ELSE IF t.version = 2 /\ [t EXCEPT !.version = ProtocolVersion] \in AllFacts
         THEN [t EXCEPT !.version = ProtocolVersion] ELSE [t EXCEPT !.version = 0]
UpgradeLog(s) ==
    IF \A i \in 1..Len(s) : UpgradeMessage(s[i]) \in AllFacts
    THEN [accepted |-> TRUE, transactions |-> [i \in 1..Len(s) |-> UpgradeMessage(s[i])]]
    ELSE [accepted |-> FALSE, transactions |-> <<>>]
VersionUpgradeEquivalence ==
    \A t \in AllFacts :
         /\ UpgradeMessage(Version2(t)) = t
         /\ UpgradeLog(<<t, [t EXCEPT !.version = ProtocolVersion + 1]>>)
               = [accepted |-> FALSE, transactions |-> <<>>]

Allowed(f, t) ==
    /\ t \in AllFacts /\ t \notin f
    /\ ~Terminal(f, t.work_id)
    /\ CASE t.kind = "Work" ->
               ~WorkExists(f, t.work_id) /\ t.sequence = NextSequence(f)
         [] t.kind = "Claim" -> WorkExists(f, t.work_id)
         [] t.kind = "WorkCancellation" -> WorkExists(f, t.work_id)
         [] t.kind = "ClaimCancellation" -> Claim(t.claim_id) \in f
         [] t.kind = "Completion" ->
                /\ WorkExists(f, t.work_id)
                /\ t.claim_id = Winner(f, t.work_id)
                /\ t = Completion(t.attempt_id)
         [] OTHER -> FALSE

RECURSIVE Apply(_, _), Canonical(_)
Apply(s, intents) ==
    IF intents = <<>> THEN s
    ELSE LET t == Materialize(Facts(s), Head(intents))
             next == IF Allowed(Facts(s), t) THEN Append(s, t) ELSE s
         IN Apply(next, Tail(intents))
Canonical(f) ==
    IF f = {} THEN <<>>
    ELSE LET t == CHOOSE t \in f : TRUE
         IN <<t>> \o Canonical(f \ {t})

QueueRequestValid(source, t, policies) ==
    IF t.kind # "Claim" THEN TRUE
    ELSE IF ~policies[t.claim_id].queued THEN TRUE
    ELSE IF Claim(t.claim_id) \in Facts(source)
         THEN State(Facts(source), t.work_id) = "claimed"
              /\ Winner(Facts(source), t.work_id) = t.claim_id
         ELSE NextWork(Facts(source), policies[t.claim_id]) = t.work_id
RECURSIVE Reconcile(_, _, _)
Reconcile(source, intents, policies) ==
    IF intents = <<>> THEN [valid |-> TRUE, candidate |-> source]
    ELSE IF ~QueueRequestValid(source, Head(intents), policies)
         THEN [valid |-> FALSE, candidate |-> source]
         ELSE LET result == Reconcile(Apply(source, <<Head(intents)>>),
                                      Tail(intents), policies)
              IN IF result.valid THEN result
                 ELSE [valid |-> FALSE, candidate |-> source]

VARIABLES log, head, dispatch, workers, compact, recovery, deadRuns,
          terminalHistory, authorizations, effects
vars == <<log, head, dispatch, workers, compact, recovery, deadRuns,
          terminalHistory, authorizations, effects>>

Snapshot == [phase |-> "idle", base |-> 0, source |-> <<>>,
             candidate |-> <<>>, observedRuns |-> {}, retries |-> 0]
EmptyActivationSnapshot ==
    [ready |-> FALSE, head |-> 0, admitted |-> FALSE]
QueueOrders == {order \in [1..WorkCount -> Works] :
                     {order[i] : i \in 1..WorkCount} = Works}
Init ==
    /\ IF SeedQueue
       THEN \E order \in QueueOrders : log = [i \in 1..WorkCount |-> Work(order[i], i)]
       ELSE log = <<>>
    /\ head = IF SeedQueue THEN 1 ELSE 0
    /\ deadRuns = {}
    /\ terminalHistory = [w \in Works |-> {}]
    /\ authorizations = <<>> /\ effects = <<>>
    /\ dispatch = [d \in Dispatchers |->
         [phase |-> "agent", local |-> <<>>, serialized |-> <<>>,
          base |-> 0, source |-> <<>>, candidate |-> <<>>, retries |-> 0,
          activationReady |-> FALSE, activationHead |-> 0, activationSource |-> <<>>,
          policies |-> [c \in Claims |-> OperatorPolicy],
          staged |-> [c \in Claims |-> <<>>]]]
    /\ workers = [a \in Workers |->
         [phase |-> "waiting", finish |-> FALSE,
          base |-> 0, source |-> <<>>, candidate |-> <<>>, retries |-> 0,
          activation |-> EmptyActivationSnapshot]]
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
QueueView(d) == Apply(dispatch[d].activationSource, dispatch[d].local)
CaptureDispatcherSnapshot(d) ==
    /\ QueueEnabled /\ dispatch[d].phase = "agent" /\ ~dispatch[d].activationReady
    /\ dispatch' = [dispatch EXCEPT ![d].activationReady = TRUE,
          ![d].activationHead = head, ![d].activationSource = log]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>
Stage(d, t) ==
    /\ dispatch[d].phase = "agent" /\ t \in Intents
    /\ (~SeedQueue \/ t.kind = "Claim")
    /\ (t.kind # "Claim" \/ Origin(t.claim_id) = d)
    /\ Allowed(Facts(LocalView(d)), Materialize(Facts(LocalView(d)), t))
    /\ dispatch' = [dispatch EXCEPT
         ![d].local = Append(@, t), ![d].serialized = Append(@, t)]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

StageNext(d, p, c) ==
    /\ QueueEnabled /\ dispatch[d].phase = "agent" /\ dispatch[d].activationReady
    /\ p \in Policies /\ c \in Claims /\ Origin(c) = d
    /\ NextWork(Facts(QueueView(d)), p) = WorkOf(c)
    /\ Allowed(Facts(QueueView(d)), Claim(c))
    /\ dispatch' = [dispatch EXCEPT
          ![d].local = Append(@, Claim(c)), ![d].serialized = Append(@, Claim(c)),
          ![d].policies[c] = p, ![d].staged[c] = QueueView(d)]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

PrepareDispatch(d) ==
    LET result == Reconcile(log, dispatch[d].serialized, dispatch[d].policies) IN
    /\ dispatch[d].phase = "agent"
    /\ dispatch' = [dispatch EXCEPT
         ![d].phase = IF result.valid THEN "prepared" ELSE "failed",
         ![d].base = head, ![d].source = log, ![d].candidate = result.candidate]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

PushDispatch(d) ==
    /\ dispatch[d].phase = "prepared" /\ Publish(dispatch[d])
    /\ dispatch' = [dispatch EXCEPT ![d].phase = "done"]
    /\ UNCHANGED <<workers, compact, recovery, deadRuns, authorizations, effects>>

RetryDispatch(d) ==
    LET result == Reconcile(log, dispatch[d].serialized, dispatch[d].policies) IN
    /\ dispatch[d].phase = "prepared" /\ dispatch[d].base # head
    /\ dispatch' = IF dispatch[d].retries = RetryLimit \/ ~result.valid
         THEN [dispatch EXCEPT ![d].phase = "failed"]
         ELSE [dispatch EXCEPT ![d].base = head, ![d].source = log,
               ![d].candidate = result.candidate,
               ![d].retries = @ + 1]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

ActivationArtifact(a) ==
    LET c == Inbound(a)
        w == WorkOf(c)
        projection == Projection(Facts(log))
        workState == projection.work[w]
        winner == projection.winner[w]
        claimState == projection.claim[c]
    IN [ready |-> TRUE, head |-> head,
        admitted |-> claimState = "effective"
                      /\ workState \notin {"completed", "cancelled"}
                      /\ winner = c]

Activate(a) ==
    LET c == Inbound(a)
        artifact == ActivationArtifact(a)
    IN
    /\ workers[a].phase = "waiting"
    /\ artifact.admitted
    /\ c \notin deadRuns
    /\ workers' = [workers EXCEPT ![a].phase = "running",
                   ![a].activation = artifact]
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
         \/ PushDispatch(d) \/ RetryDispatch(d) \/ CaptureDispatcherSnapshot(d)
         \/ (\E p \in Policies, c \in Claims : StageNext(d, p, c))
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
         /\ (t.kind # "Work" => WorkExists(f, t.work_id))
         /\ (t.kind \in {"ClaimCancellation", "Completion"} => Claim(t.claim_id) \in f)
    /\ \A w \in Works : Cardinality({t \in Terminals(f) : t.work_id = w}) <= 1
    /\ \A t \in f : t.kind = "Completion" =>
         /\ t = Completion(t.attempt_id) /\ t.claim_id = Minimum(Active(f, t.work_id))
    /\ \A w \in Works : Cardinality({t \in Submitted(f) : t.work_id = w}) <= 1
    /\ \A a, b \in Submitted(f) : a.sequence = b.sequence => a.work_id = b.work_id
    /\ {t.sequence : t \in Submitted(f)} = 1..Cardinality(Submitted(f))
ValidLog == ValidFacts(Facts(log))

TypeOK ==
    /\ log \in Seq(AllFacts) /\ head \in Nat /\ deadRuns \subseteq Claims
    /\ terminalHistory \in [Works -> SUBSET AllFacts]
    /\ authorizations \in Seq({Completion(a) : a \in Workers})
    /\ effects \in Seq({Completion(a) : a \in Workers})
    /\ dispatch \in [Dispatchers ->
         [phase : {"agent", "prepared", "done", "failed"},
          local : Seq(Intents), serialized : Seq(Intents), base : Nat,
          source : Seq(AllFacts), candidate : Seq(AllFacts), retries : 0..RetryLimit,
          activationReady : BOOLEAN, activationHead : Nat, activationSource : Seq(AllFacts),
          policies : [Claims -> Policies \cup {OperatorPolicy}],
          staged : [Claims -> Seq(AllFacts)]]]
    /\ workers \in [Workers ->
         [phase : {"waiting", "running", "ready", "prepared", "committed",
                   "authorized", "done", "stopped", "failed"},
          finish : BOOLEAN, base : Nat, source : Seq(AllFacts),
          candidate : Seq(AllFacts), retries : 0..RetryLimit,
          activation : [ready : BOOLEAN, head : Nat, admitted : BOOLEAN]]]
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
         dispatch[d].candidate =
              Reconcile(dispatch[d].source, dispatch[d].serialized, dispatch[d].policies).candidate
    /\ \A a \in Workers : workers[a].phase = "prepared" =>
         workers[a].candidate = WorkerCandidate(workers[a].source, a)
    /\ compact.phase = "prepared" =>
         compact.candidate = Canonical(Facts(compact.source))
    /\ recovery.phase = "prepared" =>
         recovery.candidate = MaintenanceCandidate("recovery",
             recovery.source, recovery.observedRuns)
SnapshotVersions ==
    /\ \A d \in Dispatchers : dispatch[d].base <= head
    /\ \A d \in Dispatchers : dispatch[d].activationHead <= head
    /\ \A a \in Workers : workers[a].base <= head
    /\ \A a \in Workers : workers[a].activation.ready =>
          workers[a].activation.head <= head
    /\ compact.base <= head /\ recovery.base <= head
ActivationSnapshotValidity ==
    \A a \in Workers : workers[a].activation.ready =>
         workers[a].activation.admitted
Serialization == \A d \in Dispatchers : dispatch[d].local = dispatch[d].serialized
QueueStagingSoundness ==
    \A d \in Dispatchers, c \in Claims : dispatch[d].policies[c].queued =>
         /\ dispatch[d].activationReady
         /\ NextWork(Facts(dispatch[d].staged[c]), dispatch[d].policies[c]) = WorkOf(c)
QueueSelectionSoundness ==
    \A d \in Dispatchers : dispatch[d].phase = "prepared" =>
         Reconcile(dispatch[d].source, dispatch[d].serialized, dispatch[d].policies).valid
FIFOSelection ==
    LET f == Facts(log)
        next == NextWork(f, DefaultPolicy)
    IN next # 0 => \A w \in Eligible(f, DefaultPolicy) : Enqueued(f, next) <= Enqueued(f, w)
GroupSelection ==
    \A p \in Policies :
         LET next == NextWork(Facts(log), p)
         IN p.group /\ next # 0 =>
              Cardinality(ActiveGroup(Facts(log), next)) < p.max_active
SelectionCompaction ==
    \A p \in Policies :
         NextWork(Facts(log), p) = NextWork(Facts(Canonical(Facts(log))), p)
CurrentProtocol == \A t \in Facts(log) : t.version = ProtocolVersion
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
         t.kind = "Completion" /\ t.attempt_id = a}) <= 1
SingleEffectiveClaim ==
    \A w \in Works : Cardinality({c \in Active(Facts(log), w) :
                                  c = Winner(Facts(log), w)}) <= 1
AuthorizationSoundness == Facts(authorizations) \subseteq
                           {t \in Facts(log) : t.kind = "Completion"}
EffectSoundness == Facts(effects) \subseteq Facts(authorizations)
CountAttempt(s, a) == Cardinality({i \in 1..Len(s) : s[i].attempt_id = a})
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
    \A t \in Facts(authorizations) : workers[t.attempt_id].finish
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
                                  authorizations[i].work_id = w}) <= 1
SingleEffect ==
    \A w \in Works : Cardinality({i \in 1..Len(effects) : effects[i].work_id = w}) <= 1
Safety ==
    /\ TypeOK /\ ValidLog /\ SnapshotValidity /\ SnapshotVersions
    /\ ActivationSnapshotValidity /\ Serialization
    /\ CandidateDerivation /\ TerminalHistoryValid /\ TerminalPersistence /\ TerminalFreeze
    /\ QueueStagingSoundness /\ QueueSelectionSoundness /\ FIFOSelection /\ GroupSelection
    /\ SelectionCompaction /\ CurrentProtocol /\ VersionUpgradeEquivalence
    /\ SingleCompletionPerWorker /\ SingleEffectiveClaim /\ AuthorizationSoundness
    /\ EffectSoundness /\ WorkerOrigin /\ FinishRequired
    /\ LifecycleAccounting /\ SingleAuthorization /\ SingleEffect

\* Reachability witnesses: deliberately false for safe executions, not safety requirements.
NoCompetingClaims ==
    \A w \in Works : Cardinality({c \in Claims : Claim(c) \in Facts(log)
                                               /\ WorkOf(c) = w}) < 2
NoRecoveredOrphan ==
    ~(recovery.phase = "done" /\ \E c \in recovery.observedRuns :
          ClaimCancellation(c) \in Facts(recovery.candidate) \ Facts(recovery.source)
          /\ ClaimCancellation(c) \in Facts(log))
NoExternalEffect == effects = <<>>

Bound == head <= MaxHead /\ Len(log) <= MaxLog
         /\ \A d \in Dispatchers : Len(dispatch[d].local) <= MaxLog

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

UnsafeQueueSelection(d, c) ==
    /\ dispatch[d].phase = "agent" /\ dispatch[d].activationReady
    /\ c \in Claims /\ Origin(c) = d
    /\ State(Facts(QueueView(d)), WorkOf(c)) = "available"
    /\ NextWork(Facts(QueueView(d)), DefaultPolicy) # WorkOf(c)
    /\ Allowed(Facts(QueueView(d)), Claim(c))
    /\ dispatch' = [dispatch EXCEPT
          ![d].local = Append(@, Claim(c)), ![d].serialized = Append(@, Claim(c)),
          ![d].policies[c] = DefaultPolicy, ![d].staged[c] = QueueView(d)]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>
UnsafeQueuePrepare(d) ==
    /\ dispatch[d].phase = "agent"
    /\ ~Reconcile(log, dispatch[d].serialized, dispatch[d].policies).valid
    /\ dispatch' = [dispatch EXCEPT
          ![d].phase = "prepared", ![d].base = head, ![d].source = log,
          ![d].candidate = Apply(log, dispatch[d].serialized)]
    /\ UNCHANGED <<log, head, workers, compact, recovery, deadRuns,
                   terminalHistory, authorizations, effects>>

BrokenCASSpec ==
    Init /\ [][Next \/ (\E d \in Dispatchers : UnsafeStalePush(d))]_vars
BrokenTerminalSpec ==
    Init /\ [][Next \/ (\E c \in Claims : UnsafeLateClaim(c))]_vars
BrokenSelectionSpec ==
    Init /\ [][Next \/ (\E d \in Dispatchers, c \in Claims : UnsafeQueueSelection(d, c))]_vars
BrokenQueueSpec ==
    Init /\ [][Next \/ (\E d \in Dispatchers : UnsafeQueuePrepare(d))]_vars

THEOREM ReplayDeterminism ==
    \A a, b \in Seq(AllFacts) : Facts(a) = Facts(b) => Replay(a) = Replay(b)
THEOREM CompactionEquivalence ==
    \A s \in Seq(AllFacts) : Replay(s) = Replay(Canonical(Facts(s)))
THEOREM QueueCompactionEquivalence ==
    \A s \in Seq(AllFacts), p \in Policies :
         NextWork(Facts(s), p) = NextWork(Facts(Canonical(Facts(s))), p)
THEOREM ProtocolUpgradePreservesFacts ==
    \A s \in Seq(AllFacts) :
         Facts(UpgradeLog([i \in 1..Len(s) |-> Version2(s[i])]).transactions) = Facts(s)
THEOREM ProtocolSafety == Spec => []Safety
THEOREM WorkerCannotRestart == Spec => WorkerOneShot
=============================================================================
