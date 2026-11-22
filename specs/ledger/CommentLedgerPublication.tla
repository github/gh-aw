----------------------- MODULE CommentLedgerPublication -----------------------
EXTENDS Naturals, Sequences

Batches == 1..2
BatchShards(b) == {<<b, 1>>, <<b, 2>>, <<b, 3>>}
MaxAttempts == 2

VARIABLES comments, mode, batch, phase, attempts, outcome, lost
vars == <<comments, mode, batch, phase, attempts, outcome, lost>>
Published(b) == \E i \in 1..Len(comments): comments[i].batch = b

Init ==
    /\ comments = <<>>
    /\ mode \in {"live", "staged", "invalid-history"}
    /\ batch = 1
    /\ phase = "idle"
    /\ attempts = [b \in Batches |-> 0]
    /\ outcome = "none"
    /\ lost = {}

Begin(b) ==
    /\ phase = "idle"
    /\ attempts[b] < MaxAttempts
    /\ batch' = b
    /\ phase' = "scanning"
    /\ attempts' = [attempts EXCEPT ![b] = @ + 1]
    /\ outcome' = "none"
    /\ UNCHANGED <<comments, mode, lost>>

Finish(result) ==
    /\ phase' = "idle"
    /\ outcome' = result
    /\ UNCHANGED <<mode, batch, attempts>>

Skip ==
    /\ phase = "scanning"
    /\ mode # "live"
    /\ Finish(IF mode = "staged" THEN "staged" ELSE "failed")
    /\ UNCHANGED <<comments, lost>>

Reconcile ==
    /\ phase = "scanning"
    /\ mode = "live"
    /\ Published(batch)
    /\ Finish("already-present")
    /\ UNCHANGED <<comments, lost>>

Prepare ==
    /\ phase = "scanning"
    /\ mode = "live"
    /\ ~Published(batch)
    /\ phase' = "posting"
    /\ UNCHANGED <<comments, mode, batch, attempts, outcome, lost>>

Post(accepted, responseReceived) ==
    /\ phase = "posting"
    /\ comments' = IF accepted
                   THEN Append(comments, [batch |-> batch, shards |-> BatchShards(batch)])
                   ELSE comments
    /\ lost' = IF accepted /\ ~responseReceived THEN lost \cup {batch} ELSE lost
    /\ Finish(IF accepted /\ responseReceived THEN "persisted" ELSE "failed")

Next ==
    \/ \E b \in Batches: Begin(b)
    \/ Skip
    \/ Reconcile
    \/ Prepare
    \/ Post(FALSE, FALSE)
    \/ Post(TRUE, FALSE)
    \/ Post(TRUE, TRUE)

Spec == Init /\ [][Next]_vars

AtomicBatch ==
    \A i \in 1..Len(comments):
        /\ comments[i].batch \in Batches
        /\ comments[i].shards = BatchShards(comments[i].batch)

NoDuplicatePublication ==
    \A i, j \in 1..Len(comments): i # j => comments[i].batch # comments[j].batch

SuccessRequiresDurability ==
    outcome \in {"persisted", "already-present"} => Published(batch)

BlockedModesDoNotPublish == mode # "live" => comments = <<>>

NoLostResponseRecovery ==
    ~(batch \in lost /\ outcome = "already-present")

=============================================================================
