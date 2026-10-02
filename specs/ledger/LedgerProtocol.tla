--------------------------- MODULE LedgerProtocol ---------------------------
EXTENDS Naturals, Integers, Sequences, FiniteSets, TLC

\* A bounded abstraction of the trusted jobs, their transaction artifact, and
\* the six built-in reducers. Values stand for canonical JSON equivalence
\* classes; IDs stand for deterministic record IDs (not cryptographic hashes).
CONSTANTS Kind, Keys, Values, MaxRecords, None
ASSUME /\ Kind \in {"raw", "log", "set", "map", "table", "counter", "notes"}
       /\ Keys # {} /\ Values # {}
       /\ None \notin Values
       /\ MaxRecords \in Nat \ {0}

Ops == CASE Kind = "raw"     -> {"record"}
         [] Kind = "log"     -> {"append"}
         [] Kind = "set"     -> {"add", "remove"}
         [] Kind = "map"     -> {"put", "delete"}
         [] Kind = "table"   -> {"insert", "update", "upsert", "delete"}
         [] Kind = "counter" -> {"increment", "decrement"}
         [] OTHER            -> {"note", "vote"}

Ids == {ToString(i) : i \in 1..MaxRecords}
Id(n) == ToString(n)
Targets == Keys \cup Ids
InputKeys == IF Kind = "notes" THEN Targets ELSE Keys

Empty == [log |-> <<>>,
          members |-> {},
          cells |-> [k \in Keys |-> None],
          counts |-> [k \in Keys |-> 0],
          notes |-> {},
          votes |-> <<>>]

\* The abstract table row is (primary key, one non-key field). Update changes
\* only that field; upsert replaces the row. All values satisfy the schema.
Allowed(s, e) ==
  CASE Kind = "table" ->
         (e.op # "insert" \/ s.cells[e.key] = None)
         /\ (e.op # "update" \/ s.cells[e.key] # None)
    [] Kind = "counter" ->
         (e.op # "increment" \/ s.counts[e.key] < MaxRecords)
         /\ (e.op # "decrement" \/ s.counts[e.key] > -MaxRecords)
    [] Kind = "notes" ->
         e.op # "vote" \/ e.key \in s.notes
    [] OTHER -> TRUE

Step(s, e) ==
  CASE Kind \in {"raw", "log"} ->
         [s EXCEPT !.log = Append(@, e.value)]
    [] Kind = "set" ->
         [s EXCEPT !.members =
           IF e.op = "add" THEN @ \cup {e.value} ELSE @ \ {e.value}]
    [] Kind \in {"map", "table"} ->
         [s EXCEPT !.cells[e.key] =
           IF e.op = "delete" THEN None ELSE e.value]
    [] Kind = "counter" ->
         [s EXCEPT !.counts[e.key] =
           @ + (IF e.op = "increment" THEN 1 ELSE -1)]
    [] OTHER ->
         IF e.op = "note"
         THEN [s EXCEPT !.notes = @ \cup {e.id}]
         ELSE [s EXCEPT !.votes = Append(@, <<e.id, e.key, e.value>>)]

\* Independent replay of the immutable canonical record stream.
RECURSIVE Replay(_)
Replay(h) ==
  IF Len(h) = 0 THEN Empty
  ELSE Step(Replay(SubSeq(h, 1, Len(h) - 1)), h[Len(h)])

VARIABLES phase, request, artifact, history, projection, snapshot,
          shard, planned, compacted
vars == <<phase, request, artifact, history, projection, snapshot,
          shard, planned, compacted>>

Init ==
  /\ phase = "idle"
  /\ request = None /\ artifact = None
  /\ history = <<>> /\ projection = Empty /\ snapshot = Empty
  /\ shard = {} /\ planned = {} /\ compacted = FALSE

\* Agent execution can only queue a request; its SQLite snapshot is unchanged.
Queue(op, key, value) ==
  /\ phase = "idle" /\ Len(history) < MaxRecords
  /\ op \in Ops /\ key \in InputKeys
  /\ value \in Values
  /\ request' = [op |-> op, key |-> key, value |-> value,
                 id |-> Id(Len(history) + 1)]
  /\ phase' = "queued"
  /\ UNCHANGED <<artifact, history, projection, snapshot, shard, planned, compacted>>

\* safe_outputs validates intent and publishes a versioned transaction artifact.
Validate ==
  /\ phase = "queued"
  /\ Allowed(projection, request)
  /\ artifact' = request
  /\ phase' = "validated"
  /\ UNCHANGED <<request, history, projection, snapshot, shard, planned, compacted>>

Reject ==
  /\ phase = "queued" /\ ~Allowed(projection, request)
  /\ phase' = "idle" /\ request' = None
  /\ UNCHANGED <<artifact, history, projection, snapshot, shard, planned, compacted>>

\* push_ledger_changes independently checks the artifact against the latest
\* history. This atomic action abstracts its expected-head Git push.
Persist ==
  /\ phase = "validated"
  /\ artifact = request
  /\ artifact.id = Id(Len(history) + 1)
  /\ Allowed(Replay(history), artifact)
  /\ history' = Append(history, artifact)
  /\ projection' = Step(projection, artifact)
  /\ shard' = shard \cup {artifact.id}
  /\ phase' = "committed"
  /\ UNCHANGED <<request, artifact, snapshot, planned, compacted>>

\* A retried artifact is idempotent; only a successful push changes durability.
Finish ==
  /\ phase = "committed"
  /\ phase' = "idle" /\ request' = None /\ artifact' = None
  /\ UNCHANGED <<history, projection, snapshot, shard, planned, compacted>>

\* Next agent job rebuilds its read-only projection from the canonical branch.
Refresh ==
  /\ phase = "idle"
  /\ snapshot' = Replay(history)
  /\ UNCHANGED <<phase, request, artifact, history, projection, shard, planned, compacted>>

\* Maintenance plans are untrusted; apply must preserve the record union.
Plan ==
  /\ phase = "idle" /\ planned = {} /\ shard # {}
  /\ planned' = shard
  /\ UNCHANGED <<phase, request, artifact, history, projection, snapshot, shard, compacted>>

Compact ==
  /\ phase = "idle" /\ planned # {} /\ planned = shard
  /\ compacted' = TRUE /\ planned' = {}
  /\ UNCHANGED <<phase, request, artifact, history, projection, snapshot, shard>>

StalePlan ==
  /\ phase = "idle" /\ planned # {} /\ planned # shard
  /\ planned' = {}
  /\ UNCHANGED <<phase, request, artifact, history, projection, snapshot, shard, compacted>>

Next ==
  \/ \E op \in Ops, key \in InputKeys, value \in Values :
       Queue(op, key, value)
  \/ Validate \/ Reject \/ Persist \/ Finish \/ Refresh
  \/ Plan \/ Compact \/ StalePlan

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ phase \in {"idle", "queued", "validated", "committed"}
  /\ history \in Seq([op : Ops, key : Targets,
                       value : Values, id : Ids])
  /\ shard \subseteq Ids

ReplayAgreement == projection = Replay(history)
NoEarlyDurability ==
  (phase \in {"queued", "validated"}) =>
    (request.id \notin shard /\ request.id = Id(Len(history) + 1))
ImmutableHistory == shard = {Id(i) : i \in 1..Len(history)}
ReadOnlySnapshot ==
  \E n \in 0..Len(history) : snapshot = Replay(SubSeq(history, 1, n))
NotesReferentialIntegrity ==
  Kind # "notes" \/
    \A i \in 1..Len(projection.votes) :
      projection.votes[i][2] \in projection.notes
CounterBound ==
  Kind # "counter" \/
    \A k \in Keys :
      -MaxRecords <= projection.counts[k] /\ projection.counts[k] <= MaxRecords
=============================================================================
