------------------------------ MODULE WriteGate ------------------------------
EXTENDS Naturals, FiniteSets, Sequences

CONSTANT Fault
ASSUME Fault \in {"none", "gate-bypass", "partial-widen", "manifest-mutation",
                  "stale-replay", "conclusion-recovery"}

\* Symbolic digests represent identity equality, not a proof of SHA-256 or JSON.
VARIABLE s
vars == <<s>>
Items == {1, 2}
Decisions == {"pending", "allow", "partial", "deny", "gate_error"}

Init ==
    s = [stage |-> "prepare", valid |-> FALSE, completed |-> FALSE,
         decision |-> "pending", selected |-> {}, digest |-> 0, seen |-> 0,
         attempt |-> 1, gateAttempt |-> 1, changed |-> FALSE, replayed |-> FALSE,
         writes |-> {}, reportedAllowed |-> FALSE, lastEvent |-> "Init"]

Prepare(valid) ==
    /\ s.stage = "prepare"
    /\ s' = [s EXCEPT !.valid = valid,
                      !.stage = IF valid THEN "gate" ELSE "conclusion",
                      !.decision = IF valid THEN "pending" ELSE "gate_error",
                      !.lastEvent = IF valid THEN "Manifest" ELSE "ManifestError"]

Decide(decision, selected) ==
    /\ s.stage = "gate"
    /\ decision \in Decisions \ {"pending"}
    /\ selected \subseteq Items
    /\ (decision = "allow" => selected = Items)
    /\ (decision = "partial" => selected \in {{1}, {}})
    /\ (decision \in {"deny", "gate_error"} => selected = {})
    /\ s' = [s EXCEPT !.completed = decision # "gate_error",
                      !.decision = decision, !.selected = selected,
                      !.seen = s.digest, !.gateAttempt = s.attempt,
                      !.stage = "apply", !.lastEvent = "Gate:" \o decision]

Mutate ==
    /\ s.stage = "apply" /\ ~s.changed
    /\ s' = [s EXCEPT !.digest = 1, !.changed = TRUE,
                      !.lastEvent = "MutateManifest"]

Replay ==
    /\ s.stage = "apply" /\ ~s.replayed
    /\ s' = [s EXCEPT !.attempt = 2, !.replayed = TRUE,
                      !.lastEvent = "ReplayPriorDecision"]

Allowed ==
    /\ s.valid /\ s.completed /\ s.decision \in {"allow", "partial"}
    /\ (s.digest = s.seen \/ Fault = "manifest-mutation")
    /\ (s.attempt = s.gateAttempt \/ Fault = "stale-replay")

Publish(item) ==
    /\ s.stage = "apply" /\ item \in Items
    /\ ~\E w \in s.writes: w.item = item
    /\ (Allowed \/ (Fault = "gate-bypass" /\ ~s.completed))
    /\ (item \in s.selected \/ Fault = "partial-widen"
        \/ (Fault = "gate-bypass" /\ ~s.completed))
    /\ s' = [s EXCEPT !.writes = @ \cup {
           [item |-> item, completed |-> s.completed, decision |-> s.decision,
            selected |-> s.selected, digest |-> s.digest, seen |-> s.seen,
            attempt |-> s.attempt, gateAttempt |-> s.gateAttempt,
            origin |-> "publisher"]},
                      !.lastEvent = IF item = 1 THEN "Publish:1" ELSE "Publish:2"]

Finish ==
    /\ s.stage = "apply"
    /\ s' = [s EXCEPT !.stage = "conclusion", !.lastEvent = "Finish"]

Conclusion ==
    /\ s.stage = "conclusion"
    /\ s' = [s EXCEPT !.stage = "done",
           !.reportedAllowed = s.valid /\ s.completed
                               /\ s.decision \in {"allow", "partial"}
                               /\ s.digest = s.seen /\ s.attempt = s.gateAttempt,
           !.writes = IF Fault = "conclusion-recovery" /\ ~Allowed
                      THEN @ \cup {
                          [item |-> 1, completed |-> s.completed,
                           decision |-> s.decision, selected |-> s.selected,
                           digest |-> s.digest, seen |-> s.seen,
                           attempt |-> s.attempt, gateAttempt |-> s.gateAttempt,
                           origin |-> "conclusion"]}
                      ELSE @,
           !.lastEvent = "Conclusion"]

Next ==
    \/ \E valid \in BOOLEAN: Prepare(valid)
    \/ \E decision \in Decisions \ {"pending"}, selected \in SUBSET Items:
           Decide(decision, selected)
    \/ Mutate \/ Replay
    \/ \E item \in Items: Publish(item)
    \/ Finish \/ Conclusion

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.stage \in {"prepare", "gate", "apply", "conclusion", "done"}
    /\ s.valid \in BOOLEAN /\ s.completed \in BOOLEAN
    /\ s.decision \in Decisions /\ s.selected \subseteq Items
    /\ s.digest \in 0..1 /\ s.seen \in 0..1
    /\ s.attempt \in 1..2 /\ s.gateAttempt \in 1..2
    /\ s.changed \in BOOLEAN /\ s.replayed \in BOOLEAN
    /\ s.reportedAllowed \in BOOLEAN
    /\ s.writes \subseteq [item: Items, completed: BOOLEAN, decision: Decisions,
           selected: SUBSET Items, digest: 0..1, seen: 0..1,
           attempt: 1..2, gateAttempt: 1..2, origin: {"publisher", "conclusion"}]

CompletedGateBeforeWrite ==
    \A w \in s.writes: w.completed /\ w.decision \in {"allow", "partial"}
ItemNarrowing == \A w \in s.writes: w.item \in w.selected
ManifestBinding == \A w \in s.writes: w.digest = w.seen
InvocationBinding == \A w \in s.writes: w.attempt = w.gateAttempt
NoConclusionRecovery == \A w \in s.writes: w.origin = "publisher"
ConclusionChecksBoth ==
    s.reportedAllowed =>
        /\ s.completed /\ s.decision \in {"allow", "partial"}
        /\ s.digest = s.seen /\ s.attempt = s.gateAttempt

\* False reachability predicates exercise live allow, partial, and hold paths.
NoSuccessfulWrite == s.writes = {}
NoPartialWrite == ~(\E w \in s.writes: w.decision = "partial")
NoHeldRun == ~(s.stage = "done" /\ s.decision = "partial" /\ s.selected = {} /\ s.writes = {})
=============================================================================
