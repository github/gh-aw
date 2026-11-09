----------------------- MODULE ObsoleteWorkRetirement -----------------------
EXTENDS Naturals, Sequences, FiniteSets, TLC

\* Focused retirement refinement, not a Git/JSON codec or authentication proof.
\* Active revisions are already approved; original pins and admission contracts
\* are immutable. "expired" represents derived work_expired metadata, not an
\* explicit cancellation operation. Legacy/checked represent absent/version:1.
CONSTANTS BatchLimit, Broken
ASSUME /\ BatchLimit \in 1..2
       /\ Broken \in {"none", "selection-expiry", "stale-publication", "pin",
                      "owned", "barrier", "unavailable", "legacy-recheck",
                      "checkpoint-version", "terminal-version", "debt",
                      "reservation", "result", "maintenance-stop"}
Works == {"expired", "old", "pinned", "owned", "barrier", "other"}
\* Trusted UTF-8 order is a premise; native comparator/codec tests remain separate.
Order == <<"barrier", "expired", "old", "other", "owned", "pinned">>
None == "none"
Forms == {"legacy", "checked"}
Deadline(w) == IF w = "expired" THEN 1 ELSE 3
Pinned(w) == w = "pinned"
SeqSet(s) == {s[i] : i \in 1..Len(s)}
Effective(q, at) == IF q.time > at THEN q.time ELSE at
Due(q, w, at) == q.work[w] = "available" /\ at >= Deadline(w)
                /\ q.barrier[w] \notin {"pending", "failed"}
Expire(q, at) ==
  LET time == Effective(q, at)
      due == {w \in Works : Due(q, w, time)}
  IN [q EXCEPT !.time = time,
       !.work = [w \in Works |-> IF w \in due THEN "cancelled" ELSE q.work[w]],
       !.receipt = [w \in Works |-> IF w \in due THEN "expired" ELSE q.receipt[w]],
       !.original = [w \in Works |-> IF w \in due THEN "expired" ELSE q.original[w]]]

VARIABLE state
vars == <<state>>
Mismatch(w) == w # "other" /\ state.contract = "new"
Obsolete(q, w) ==
  /\ q.work[w] = "available" /\ ~Pinned(w) /\ w \notin q.claims
  /\ q.barrier[w] = None
  /\ state.present /\ state.available /\ Mismatch(w)
MayRetire(q, w) ==
  /\ q.work[w] = "available" \/ (Broken = "owned" /\ q.work[w] = "claimed")
  /\ ~Pinned(w) \/ Broken = "pin"
  /\ w \notin q.claims \/ Broken = "owned"
  /\ q.barrier[w] = None \/ Broken = "barrier"
  /\ (state.present /\ state.available) \/ Broken = "unavailable"
  /\ Mismatch(w)
Prefix(q, checked) ==
  LET eligible == SelectSeq(Order, LAMBDA w :
                    IF checked THEN Obsolete(q, w) ELSE MayRetire(q, w))
  IN SubSeq(eligible, 1, IF Len(eligible) > BatchLimit
                        THEN BatchLimit ELSE Len(eligible))
Retire(q, batch) ==
  LET selected == SeqSet(batch)
  IN [q EXCEPT
       !.work = [w \in Works |-> IF w \in selected THEN "cancelled" ELSE q.work[w]],
       !.receipt = [w \in Works |-> IF w \in selected THEN "checked" ELSE q.receipt[w]],
       !.original = [w \in Works |-> IF w \in selected /\ q.original[w] = None
                                    THEN "checked" ELSE q.original[w]],
       !.charges = IF Broken = "debt" /\ selected # {}
                   THEN [w \in Works |-> 0] ELSE q.charges,
       !.reservations = IF Broken = "reservation" /\ selected # {}
                        THEN {} ELSE q.reservations,
       !.results = IF Broken = "result" /\ selected # {} THEN {} ELSE q.results]

Init ==
  LET q == [work |-> [w \in Works |-> IF w = "owned" THEN "claimed" ELSE "available"],
            barrier |-> [w \in Works |-> IF w = "barrier" THEN "failed" ELSE None],
            claims |-> {"owned"}, reservations |-> {"owned"},
            charges |-> [w \in Works |-> IF w = "owned" THEN 1 ELSE 0],
            results |-> {"historical"},
            receipt |-> [w \in Works |-> None],
            original |-> [w \in Works |-> None], time |-> 0]
  IN state = [queue |-> q, contract |-> "new", available |-> TRUE,
              present |-> TRUE, updated |-> FALSE, now |-> 0,
              batch |-> <<>>, expectedBatch |-> <<>>, phase |-> "idle",
              failed |-> FALSE, recovered |-> FALSE, compacted |-> FALSE,
              selectedExpired |-> FALSE, unsafe |-> {},
              legacyLoaded |-> FALSE, legacyRejected |-> FALSE,
              snapshot |-> q, snapshotExpected |-> q, snapshotTaken |-> FALSE,
              expectedCharges |-> q.charges, expectedReservations |-> q.reservations]

Tick ==
  /\ state.now < 2
  /\ state' = [state EXCEPT !.now = @ + 1]

Deploy(contract, available, present) ==
  /\ ~state.updated /\ contract \in {"old", "new"}
  /\ available \in BOOLEAN /\ present \in BOOLEAN
  /\ state' = [state EXCEPT !.contract = contract, !.available = available,
                             !.present = present, !.updated = TRUE]

ClaimOld ==
  /\ state.queue.work["old"] = "available" /\ state.queue.barrier["old"] = None
  /\ state.contract = "old" /\ state.present /\ state.available
  /\ state' = [state EXCEPT !.queue.work["old"] = "claimed",
       !.queue.claims = @ \cup {"old"}, !.queue.reservations = @ \cup {"old"},
       !.queue.charges["old"] = 1, !.expectedCharges["old"] = 1,
       !.expectedReservations = @ \cup {"old"}]

HoldDelivery ==
  \* Abstract retained completion with a closed historical Claim.
  /\ state.queue.work["old"] = "available" /\ state.queue.barrier["old"] = None
  /\ state' = [state EXCEPT !.queue.barrier["old"] = "pending"]

Prepare ==
  /\ state.phase = "idle"
  /\ LET fresh == Expire(state.queue, state.now)
         view == IF Broken = "selection-expiry" THEN state.queue ELSE fresh
         batch == Prefix(view, FALSE)
     IN state' = [state EXCEPT !.batch = batch, !.expectedBatch = Prefix(fresh, TRUE),
       !.phase = "selected", !.selectedExpired =
         \E w \in SeqSet(batch) : Due(state.queue, w, Effective(state.queue, state.now))]

Publish(infrastructureFailure) ==
  /\ state.phase = "selected" /\ infrastructureFailure \in BOOLEAN
  /\ LET fresh == Expire(state.queue, state.now)
         valid == (\A w \in SeqSet(state.batch) : MayRetire(fresh, w))
                  \/ Broken = "stale-publication"
         failed == infrastructureFailure \/ ~valid
     IN state' = [state EXCEPT !.phase = "retired", !.failed = failed,
       !.queue = IF failed THEN state.queue ELSE Retire(fresh, state.batch),
       !.unsafe = IF failed THEN @ ELSE
                    @ \cup {w \in SeqSet(state.batch) : ~Obsolete(fresh, w)}]

ReplayLegacy(w) ==
  /\ w \in {"pinned", "other"} /\ ~state.legacyLoaded
  /\ state.queue.work[w] = "available" /\ state.queue.barrier[w] = None
  /\ LET rejected == Broken = "legacy-recheck" /\ ~Obsolete(state.queue, w)
     IN state' = [state EXCEPT !.legacyLoaded = TRUE, !.legacyRejected = rejected,
       !.queue.work[w] = IF rejected THEN @ ELSE "cancelled",
       !.queue.receipt[w] = IF rejected THEN @ ELSE "legacy",
       !.queue.original[w] = IF rejected THEN @ ELSE "legacy"]

Rewrite(w, form) ==
  /\ w \in Works /\ form \in Forms
  /\ state.queue.work[w] = "cancelled" /\ state.queue.receipt[w] \in Forms
  /\ form # state.queue.receipt[w] /\ Broken = "terminal-version"
  /\ state' = [state EXCEPT !.queue.receipt[w] = form]

Checkpoint ==
  /\ ~state.snapshotTaken
  /\ \E w \in Works : state.queue.receipt[w] \in Forms
  /\ state' = [state EXCEPT !.snapshotTaken = TRUE, !.snapshotExpected = state.queue,
       !.snapshot = IF Broken = "checkpoint-version"
         THEN [state.queue EXCEPT !.receipt =
           [w \in Works |-> IF state.queue.receipt[w] = "checked" THEN "legacy"
                            ELSE IF state.queue.receipt[w] = "legacy" THEN "checked"
                            ELSE state.queue.receipt[w]]]
         ELSE state.queue]

RecoverCore ==
  /\ state.phase = "retired"
  /\ LET stopped == Broken = "maintenance-stop" /\ state.failed
     IN state' = [state EXCEPT !.phase = IF stopped THEN "done" ELSE "recovered",
                                !.recovered = ~stopped]
Compact ==
  /\ state.phase = "recovered"
  /\ state' = [state EXCEPT !.phase = "done", !.compacted = TRUE]

Next == Tick \/ ClaimOld \/ HoldDelivery \/ Prepare \/ Checkpoint \/ RecoverCore \/ Compact
        \/ (\E contract \in {"old", "new"}, available \in BOOLEAN, present \in BOOLEAN :
              Deploy(contract, available, present))
        \/ (\E failure \in BOOLEAN : Publish(failure))
        \/ (\E w \in Works : ReplayLegacy(w))
        \/ (\E w \in Works, form \in Forms : Rewrite(w, form))
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ state.now \in 0..2 /\ state.queue.time \in 0..2
  /\ state.contract \in {"old", "new"}
  /\ state.available \in BOOLEAN /\ state.present \in BOOLEAN /\ state.updated \in BOOLEAN
  /\ state.queue.work \in [Works -> {"available", "claimed", "cancelled"}]
  /\ state.queue.barrier \in [Works -> {None, "pending", "failed"}]
  /\ state.queue.receipt \in [Works -> Forms \cup {None, "expired"}]
  /\ state.queue.original \in [Works -> Forms \cup {None, "expired"}]
  /\ state.queue.claims \subseteq Works /\ state.queue.reservations \subseteq Works
  /\ state.queue.charges \in [Works -> 0..1]
  /\ state.queue.results = {"historical"}
  /\ state.batch \in Seq(Works) /\ Len(state.batch) <= BatchLimit
  /\ state.phase \in {"idle", "selected", "retired", "recovered", "done"}
  /\ state.failed \in BOOLEAN /\ state.recovered \in BOOLEAN /\ state.compacted \in BOOLEAN
  /\ state.selectedExpired \in BOOLEAN /\ state.unsafe \subseteq Works
  /\ state.legacyLoaded \in BOOLEAN /\ state.legacyRejected \in BOOLEAN
  /\ state.snapshotTaken \in BOOLEAN
SelectionExpiration == ~state.selectedExpired
DeterministicPrefix == state.batch = state.expectedBatch
CancellationAuthority == state.unsafe = {}
LegacyReplayCompatibility == ~state.legacyRejected
TerminalMetadata == state.queue.receipt = state.queue.original
CheckpointMetadata == state.snapshot = state.snapshotExpected
ProtectedState == state.queue.charges = state.expectedCharges
                  /\ state.queue.reservations = state.expectedReservations
                  /\ state.queue.results = {"historical"}
RecoveryIsolation == state.phase = "done" => state.recovered
Safety == TypeOK /\ SelectionExpiration /\ DeterministicPrefix
          /\ CancellationAuthority /\ LegacyReplayCompatibility /\ TerminalMetadata
          /\ CheckpointMetadata /\ ProtectedState /\ RecoveryIsolation

\* Guarded reachability controls, not liveness or exactly-once effect proofs.
NoCheckedRetirement == ~(state.queue.receipt["old"] = "checked")
NoLegacyCheckpoint == ~(state.snapshotTaken /\
                        state.snapshot.receipt["pinned"] = "legacy")
NoRetirementRecovery == ~(state.failed /\ state.phase = "done"
                          /\ state.recovered /\ state.compacted)
NoElapsedPrefix == ~(state.now >= 1 /\ state.phase = "done"
                     /\ state.queue.receipt["expired"] = "expired"
                     /\ state.queue.receipt["old"] = "checked")
=============================================================================
