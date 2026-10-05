---------------------- MODULE FairWorkQueue ----------------------
EXTENDS Naturals, Integers, FiniteSets, Sequences, TLC

CONSTANTS WorkCount, DispatcherCount, MaxBatch, ClaimLimit, RunLimit,
          KeyLimit, RetryLimit, MaxLog, Mode
ASSUME /\ WorkCount \in Nat \ {0} /\ DispatcherCount \in Nat \ {0}
       /\ MaxBatch \in Nat \ {0} /\ ClaimLimit \in Nat \ {0}
       /\ RunLimit \in Nat \ {0} /\ KeyLimit \in Nat \ {0}
       /\ RetryLimit \in Nat /\ MaxLog \in Nat \ {0}
       /\ Mode \in {"default", "weighted", "strict"}
Works == 1..WorkCount
Groups == 1..DispatcherCount
Claims == 1..(WorkCount * DispatcherCount)
Class(w) == IF Mode = "default" THEN 1 ELSE IF w = 1 THEN 1 ELSE 2
Key(w) == IF Mode = "default" THEN 1 ELSE 1 + ((w - 1) % 2)
WorkOf(c) == 1 + ((c - 1) % WorkCount)
GroupOf(c) == 1 + ((c - 1) \div WorkCount)
ClaimOf(g, w) == (g - 1) * WorkCount + w
Stride(k) == IF k = 1 THEN 1 ELSE 2
Min(a) == CHOOSE x \in a : \A y \in a : x <= y
Max(a, b) == IF a >= b THEN a ELSE b
Last(s) == s[Len(s)]
Op(k, g, c, v) == [kind |-> k, group |-> g, claim |-> c, value |-> v]
Members(s, g) == {s.members[g][i] : i \in 1..Len(s.members[g])}
OpenClaims(s) == {c \in Claims : s.cs[c] = "open"}
OpenRuns(s) == {g \in Groups : s.run[g] \in {"reserved", "started", "bound", "terminal"}}
KeyOpen(s, k) == {c \in OpenClaims(s) : Key(WorkOf(c)) = k}
Eligible(s) ==
    {w \in Works : s.ws[w] = "available"
        /\ Cardinality(OpenClaims(s)) < ClaimLimit
        /\ Cardinality(KeyOpen(s, Key(w))) < KeyLimit}
Classes(s) == {Class(w) : w \in Eligible(s)}
Keys(s, p) == {Key(w) : w \in {v \in Eligible(s) : Class(v) = p}}
Normalize(s) ==
    [s EXCEPT
      !.cp = [p \in 1..2 |-> IF p \in Classes(s) \ s.ca
                   THEN Max(s.cp[p], s.cv + Stride(p)) ELSE s.cp[p]],
      !.kp = [p \in 1..2 |-> [k \in 1..2 |->
                   IF k \in Keys(s, p) \ s.ka[p]
                   THEN Max(s.kp[p][k], s.kv[p] + Stride(k)) ELSE s.kp[p][k]]],
      !.ca = Classes(s), !.ka = [p \in 1..2 |-> Keys(s, p)]]
NextWork(s) ==
    IF Eligible(s) = {} THEN 0
    ELSE LET n == Normalize(s)
             p == IF Mode = "strict" THEN Min(Classes(s))
                  ELSE CHOOSE p \in Classes(s) :
                      \A q \in Classes(s) :
                        n.cp[p] < n.cp[q] \/ (n.cp[p] = n.cp[q] /\ p <= q)
             k == CHOOSE k \in Keys(s, p) :
                      \A j \in Keys(s, p) :
                        n.kp[p][k] < n.kp[p][j]
                        \/ (n.kp[p][k] = n.kp[p][j] /\ k <= j)
         IN CHOOSE w \in Eligible(s) :
              /\ Class(w) = p /\ Key(w) = k
              /\ \A v \in Eligible(s) :
                   (Class(v) = p /\ Key(v) = k) => s.pos[w] <= s.pos[v]

Empty ==
    [ws |-> [w \in Works |-> "absent"], pos |-> [w \in Works |-> 0],
     owner |-> [w \in Works |-> 0], cs |-> [c \in Claims |-> "absent"],
     members |-> [g \in Groups |-> <<>>], run |-> [g \in Groups |-> "absent"],
     cp |-> [p \in 1..2 |-> 0], kp |-> [p \in 1..2 |-> [k \in 1..2 |-> 0]],
     cv |-> 0, kv |-> [p \in 1..2 |-> 0], ca |-> {},
     ka |-> [p \in 1..2 |-> {}], charges |-> [k \in 1..2 |-> 0]]
ApplyOp(s, t, position) ==
    LET c == t.claim
        g == t.group
        w == WorkOf(c)
    IN CASE t.kind = "Work" ->
                [s EXCEPT !.ws[c] = "available", !.pos[c] = position]
       [] t.kind = "Claim" ->
            LET n == Normalize(s) p == Class(w) k == Key(w)
            IN [n EXCEPT !.ws[w] = "claimed", !.owner[w] = c,
                 !.cs[c] = "open", !.members[g] = Append(@, c),
                 !.run[g] = "reserved", !.cv = n.cp[p],
                 !.cp[p] = @ + Stride(p), !.kv[p] = n.kp[p][k],
                 !.kp[p][k] = @ + Stride(k), !.charges[k] = @ + 1]
       [] t.kind = "Close" ->
            [s EXCEPT !.cs[c] = t.value,
                 !.ws[w] = IF t.value = "completed" THEN "completed" ELSE "available",
                 !.owner[w] = IF t.value = "completed" THEN c ELSE 0]
       [] t.kind = "Started" -> [s EXCEPT !.run[g] = "started"]
       [] t.kind = "Bound" -> [s EXCEPT !.run[g] = "bound"]
       [] t.kind = "Terminated" -> [s EXCEPT !.run[g] = "terminal"]
       [] t.kind = "Release" -> [s EXCEPT !.run[g] = "released"]
       [] OTHER -> s
RECURSIVE ApplyOps(_, _, _), Replay(_), Plan(_, _, _), ValidOps(_, _)
ApplyOps(s, ops, position) ==
    IF ops = <<>> THEN s
    ELSE ApplyOps(ApplyOp(s, Head(ops), position), Tail(ops), position + 1)
Replay(history) ==
    IF history = <<>> THEN Empty
    ELSE ApplyOps(Replay(SubSeq(history, 1, Len(history) - 1)),
                  history[Len(history)].ops, Len(history) * (WorkCount + 1))
Plan(s, g, left) ==
    IF left = 0 \/ NextWork(s) = 0
       \/ (s.run[g] = "absent" /\ Cardinality(OpenRuns(s)) >= RunLimit)
    THEN <<>>
    ELSE LET t == Op("Claim", g, ClaimOf(g, NextWork(s)), 0)
         IN <<t>> \o Plan(ApplyOp(s, t, 0), g, left - 1)
ValidOps(s, ops) ==
    IF ops = <<>> THEN TRUE
    ELSE LET t == Head(ops)
         IN /\ (t.kind = "Claim" =>
                    /\ WorkOf(t.claim) = NextWork(s)
                    /\ GroupOf(t.claim) = t.group
                    /\ s.cs[t.claim] = "absent")
            /\ ValidOps(ApplyOp(s, t, 0), Tail(ops))

\* Policy is a fixed epoch. Close abstracts Completion or ClaimCancellation.
\* One group abstracts one approved worker profile and its actual bound run.
\* Claims are individually charged; a group's native run slot survives closure.
VARIABLES log, head, pending, worker, intents, handled, authorized, effects, completed
vars == <<log, head, pending, worker, intents, handled, authorized, effects, completed>>
State == Replay(log)
Record(source, id, request, ops) ==
    Append(source, [id |-> id, previous |-> IF source = <<>> THEN 0 ELSE Last(source).id,
                    request |-> request, ops |-> ops])
Init ==
    /\ log = Record(<<>>, 1, 0, [w \in Works |-> Op("Work", 0, w, 0)])
    /\ head = 1
    /\ pending = [g \in Groups |-> [phase |-> "idle", base |-> 0,
                                   source |-> <<>>, ops |-> <<>>, retries |-> 0]]
    /\ worker = [g \in Groups |-> "waiting"]
    /\ intents = [c \in Claims |-> "none"]
    /\ handled = {} /\ authorized = {} /\ effects = <<>> /\ completed = {}
AppendOps(ops) == /\ log' = Record(log, head + 1, 0, ops) /\ head' = head + 1
Prepare(g) ==
    /\ pending[g].phase = "idle" /\ Plan(State, g, MaxBatch) # <<>>
    /\ pending' = [pending EXCEPT ![g].phase = "prepared", ![g].base = head,
                                 ![g].source = log, ![g].ops = Plan(State, g, MaxBatch)]
    /\ UNCHANGED <<log, head, worker, intents, handled, authorized, effects, completed>>
Push(g) ==
    /\ pending[g].phase = "prepared" /\ pending[g].base = head /\ pending[g].source = log
    /\ pending[g].ops # <<>>
    /\ log' = Record(log, head + 1, g, pending[g].ops) /\ head' = head + 1
    /\ pending' = [pending EXCEPT ![g].phase = "done"]
    /\ UNCHANGED <<worker, intents, handled, authorized, effects, completed>>
Retry(g) ==
    /\ pending[g].phase = "prepared" /\ pending[g].base # head
    /\ pending' = IF pending[g].retries = RetryLimit
        THEN [pending EXCEPT ![g].phase = "failed"]
        ELSE [pending EXCEPT ![g].phase = IF Plan(State, g, MaxBatch) = <<>> THEN "done" ELSE "prepared",
              ![g].base = head, ![g].source = log,
              ![g].ops = Plan(State, g, MaxBatch), ![g].retries = @ + 1]
    /\ UNCHANGED <<log, head, worker, intents, handled, authorized, effects, completed>>
Launch(g) ==
    /\ State.run[g] = "reserved" /\ AppendOps(<<Op("Started", g, 0, 0)>>)
    /\ UNCHANGED <<pending, worker, intents, handled, authorized, effects, completed>>
Bind(g) ==
    /\ State.run[g] = "started" /\ AppendOps(<<Op("Bound", g, 0, 0)>>)
    /\ worker' = [worker EXCEPT ![g] = "agent"]
    /\ UNCHANGED <<pending, intents, handled, authorized, effects, completed>>
FinishIntent(g, c, outcome) ==
    /\ worker[g] = "agent" /\ c \in Members(State, g) /\ intents[c] = "none"
    /\ intents' = [intents EXCEPT ![c] = outcome]
    /\ UNCHANGED <<log, head, pending, worker, handled, authorized, effects, completed>>
BeginFinalize(g) ==
    /\ worker[g] = "agent" /\ worker' = [worker EXCEPT ![g] = "finalizing"]
    /\ UNCHANGED <<log, head, pending, intents, handled, authorized, effects, completed>>
Finalize(g, c) ==
    /\ worker[g] = "finalizing" /\ State.run[g] = "bound"
    /\ c \in Members(State, g) /\ c \notin handled /\ State.cs[c] = "open"
    /\ State.owner[WorkOf(c)] = c
    /\ LET outcome == IF intents[c] = "completed" THEN "completed" ELSE "cancelled"
       IN /\ AppendOps(<<Op("Close", g, c, outcome)>>)
          /\ completed' = IF outcome = "completed" THEN completed \cup {c} ELSE completed
          /\ authorized' = IF outcome = "completed" THEN authorized \cup {c} ELSE authorized
    /\ handled' = handled \cup {c}
    /\ UNCHANGED <<pending, worker, intents, effects>>
Effect(g, c) ==
    /\ worker[g] = "finalizing" /\ c \in Members(State, g) /\ c \in authorized
    /\ \A i \in 1..Len(effects) : effects[i][2] # c
    /\ effects' = Append(effects, <<g, c>>)
    /\ UNCHANGED <<log, head, pending, worker, intents, handled, authorized, completed>>
Terminate(g) ==
    /\ worker[g] = "finalizing" /\ Members(State, g) \subseteq handled
    /\ AppendOps(<<Op("Terminated", g, 0, 0)>>)
    /\ worker' = [worker EXCEPT ![g] = "terminal"]
    /\ UNCHANGED <<pending, intents, handled, authorized, effects, completed>>
Crash(g) ==
    /\ worker[g] \in {"agent", "finalizing"}
    /\ AppendOps(<<Op("Terminated", g, 0, 0)>>)
    /\ worker' = [worker EXCEPT ![g] = "terminal"]
    /\ UNCHANGED <<pending, intents, handled, authorized, effects, completed>>
Recover(g, c) ==
    /\ State.run[g] = "terminal" /\ c \in Members(State, g) /\ State.cs[c] = "open"
    /\ AppendOps(<<Op("Close", g, c, "cancelled")>>)
    /\ handled' = handled \cup {c}
    /\ UNCHANGED <<pending, worker, intents, authorized, effects, completed>>
Release(g) ==
    /\ State.run[g] = "terminal" /\ Members(State, g) \cap OpenClaims(State) = {}
    /\ AppendOps(<<Op("Release", g, 0, 0)>>)
    /\ UNCHANGED <<pending, worker, intents, handled, authorized, effects, completed>>
Next ==
    \/ \E g \in Groups : Prepare(g) \/ Push(g) \/ Retry(g) \/ Launch(g) \/ Bind(g)
                         \/ BeginFinalize(g) \/ Terminate(g) \/ Crash(g) \/ Release(g)
    \/ \E g \in Groups, c \in Claims :
         Finalize(g, c) \/ Effect(g, c) \/ Recover(g, c)
         \/ \E outcome \in {"completed", "cancelled"} : FinishIntent(g, c, outcome)
Spec == Init /\ [][Next]_vars
Bound == Len(log) <= MaxLog
CausalChain ==
    /\ head = Last(log).id
    /\ \A i \in 2..Len(log) : log[i].previous = log[i - 1].id
DecisionValidity ==
    \A i \in 1..Len(log) : ValidOps(Replay(SubSeq(log, 1, i - 1)), log[i].ops)
Capacity ==
    /\ Cardinality(OpenClaims(State)) <= ClaimLimit
    /\ Cardinality(OpenRuns(State)) <= RunLimit
    /\ \A k \in 1..2 : Cardinality(KeyOpen(State, k)) <= KeyLimit
ClaimChargeCount ==
    State.charges[1] + State.charges[2] =
       Cardinality({c \in Claims : State.cs[c] # "absent"})
RequestOnce ==
    \A g \in Groups : Cardinality({i \in 1..Len(log) : log[i].request = g}) <= 1
AssignmentIntegrity ==
    \A g \in Groups :
       /\ Len(State.members[g]) <= MaxBatch
       /\ Cardinality(Members(State, g)) = Len(State.members[g])
       /\ \A c \in Members(State, g) : GroupOf(c) = g /\ State.cs[c] # "absent"
NoDuplicateEffects ==
    Cardinality({effects[i][2] : i \in 1..Len(effects)}) = Len(effects)
ClaimClosureAuthority ==
    \A i \in 1..Len(log) : \A j \in 1..Len(log[i].ops) :
      LET t == log[i].ops[j]
          before == ApplyOps(Replay(SubSeq(log, 1, i - 1)),
                             SubSeq(log[i].ops, 1, j - 1), i * (WorkCount + 1))
      IN t.kind = "Close" =>
         /\ t.group = GroupOf(t.claim)
         /\ t.claim \in Members(before, t.group)
         /\ before.cs[t.claim] = "open" /\ before.owner[WorkOf(t.claim)] = t.claim
         /\ IF t.value = "completed" THEN before.run[t.group] = "bound"
            ELSE before.run[t.group] \in {"bound", "terminal"}
RunReleaseAuthority ==
    \A i \in 1..Len(log) : \A j \in 1..Len(log[i].ops) :
      LET t == log[i].ops[j]
          before == ApplyOps(Replay(SubSeq(log, 1, i - 1)),
                             SubSeq(log[i].ops, 1, j - 1), i * (WorkCount + 1))
      IN t.kind = "Release" =>
         /\ before.run[t.group] = "terminal"
         /\ Members(before, t.group) \cap OpenClaims(before) = {}
TypeOK ==
    /\ head \in Nat \ {0} /\ log # <<>>
    /\ State.ws \in [Works -> {"absent", "available", "claimed", "completed"}]
    /\ State.cs \in [Claims -> {"absent", "open", "completed", "cancelled"}]
    /\ State.run \in [Groups -> {"absent", "reserved", "started", "bound", "terminal", "released"}]
    /\ State.owner \in [Works -> Claims \cup {0}]
    /\ State.cp \in [1..2 -> Nat] /\ State.kp \in [1..2 -> [1..2 -> Nat]]
    /\ State.cv \in Nat /\ State.kv \in [1..2 -> Nat]
    /\ State.charges \in [1..2 -> Nat]
    /\ worker \in [Groups -> {"waiting", "agent", "finalizing", "terminal"}]
    /\ intents \in [Claims -> {"none", "completed", "cancelled"}]
    /\ handled \subseteq Claims /\ authorized \subseteq completed
    /\ completed \subseteq Claims
Ownership ==
    /\ \A w \in Works :
         Cardinality({c \in OpenClaims(State) : WorkOf(c) = w}) <= 1
    /\ \A c \in OpenClaims(State) :
         State.ws[WorkOf(c)] = "claimed" /\ State.owner[WorkOf(c)] = c
DefaultFIFO ==
    Mode = "default" =>
       \A i \in 1..Len(log) : \A j \in 1..Len(log[i].ops) :
         LET t == log[i].ops[j]
             before == ApplyOps(Replay(SubSeq(log, 1, i - 1)),
                                SubSeq(log[i].ops, 1, j - 1), i * (WorkCount + 1))
         IN t.kind = "Claim" =>
            \A w \in Eligible(before) : before.pos[WorkOf(t.claim)] <= before.pos[w]
StrictPriority ==
    Mode = "strict" =>
       \A i \in 1..Len(log) : \A j \in 1..Len(log[i].ops) :
         LET t == log[i].ops[j]
             before == ApplyOps(Replay(SubSeq(log, 1, i - 1)),
                                SubSeq(log[i].ops, 1, j - 1), i * (WorkCount + 1))
         IN t.kind = "Claim" =>
            \A w \in Eligible(before) : Class(WorkOf(t.claim)) <= Class(w)
EffectAuthorization ==
    \A i \in 1..Len(effects) :
       LET g == effects[i][1] c == effects[i][2]
       IN /\ g = GroupOf(c) /\ c \in authorized /\ State.cs[c] = "completed"
TerminalPersistence == \A c \in completed : State.cs[c] = "completed"
Safety == TypeOK /\ CausalChain /\ DecisionValidity /\ Capacity /\ ClaimChargeCount /\ RequestOnce
          /\ AssignmentIntegrity /\ NoDuplicateEffects
          /\ ClaimClosureAuthority
          /\ RunReleaseAuthority
          /\ EffectAuthorization /\ TerminalPersistence /\ Ownership
          /\ DefaultFIFO /\ StrictPriority
NoBatchedAssignment == \A g \in Groups : Len(State.members[g]) <= 1
NoPartialCompletion ==
    ~(\E g \in Groups : \E a, b \in Members(State, g) :
         State.cs[a] = "completed" /\ State.cs[b] = "open")
BrokenSelect(g) ==
    /\ pending[g].phase = "idle" /\ Eligible(State) # {}
    /\ LET w == CHOOSE w \in Eligible(State) : \A v \in Eligible(State) : w >= v
       IN pending' = [pending EXCEPT ![g].phase = "prepared", ![g].base = head,
              ![g].source = log, ![g].ops = <<Op("Claim", g, ClaimOf(g, w), 0)>>]
    /\ UNCHANGED <<log, head, worker, intents, handled, authorized, effects, completed>>
BrokenEffect(g, c) ==
    /\ worker[g] = "finalizing" /\ c \in Members(State, g) /\ c \notin authorized
    /\ Members(State, g) \cap authorized # {}
    /\ effects' = Append(effects, <<g, c>>)
    /\ UNCHANGED <<log, head, pending, worker, intents, handled, authorized, completed>>
BrokenHandle(g, c) ==
    /\ worker[g] = "finalizing" /\ c \notin Members(State, g) /\ State.cs[c] = "open"
    /\ AppendOps(<<Op("Close", g, c, "completed")>>)
    /\ completed' = completed \cup {c} /\ authorized' = authorized \cup {c}
    /\ handled' = handled \cup {c}
    /\ UNCHANGED <<pending, worker, intents, effects>>
BrokenPush(g) ==
    /\ pending[g].phase = "prepared" /\ pending[g].base # head
    /\ log' = Record(pending[g].source, head + 1, g, pending[g].ops)
    /\ head' = head + 1
    /\ pending' = [pending EXCEPT ![g].phase = "done"]
    /\ UNCHANGED <<worker, intents, handled, authorized, effects, completed>>
BrokenRelease(g) ==
    /\ State.run[g] = "bound" /\ Members(State, g) \cap completed # {}
    /\ Members(State, g) \cap OpenClaims(State) # {}
    /\ AppendOps(<<Op("Release", g, 0, 0)>>)
    /\ UNCHANGED <<pending, worker, intents, handled, authorized, effects, completed>>
BrokenSelectionSpec == Init /\ [][Next \/ \E g \in Groups : BrokenSelect(g)]_vars
BrokenEffectsSpec == Init /\ [][Next \/ \E g \in Groups, c \in Claims : BrokenEffect(g, c)]_vars
BrokenHandleSpec == Init /\ [][Next \/ \E g \in Groups, c \in Claims : BrokenHandle(g, c)]_vars
BrokenCASSpec == Init /\ [][Next \/ \E g \in Groups : BrokenPush(g)]_vars
BrokenReleaseSpec == Init /\ [][Next \/ \E g \in Groups : BrokenRelease(g)]_vars
=================================================================
