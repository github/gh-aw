------------------------- MODULE QueueService -------------------------
EXTENDS Naturals, Integers, FiniteSets, TLC

CONSTANTS Mode, Dynamic, Packable, Weights
Keys == 1..3
ReferenceWeights == [k \in Keys |-> IF k = 1 THEN 5 ELSE IF k = 2 THEN 3 ELSE 2]
ASSUME /\ Mode \in {"weighted", "strict", "broken"}
       /\ Dynamic \in BOOLEAN /\ Packable \in BOOLEAN
       /\ Weights = ReferenceWeights
Scale == 30
Stride(k) == Scale \div Weights[k]
TotalWeight == 10
Max(a, b) == IF a >= b THEN a ELSE b
Abs(a) == IF a >= 0 THEN a ELSE -a
Min(s) == CHOOSE n \in s : \A m \in s : n <= m

VARIABLES passes, active, eligible, counts, phase, completedCycle, cycleCounts,
          served
vars == <<passes, active, eligible, counts, phase, completedCycle, cycleCounts,
          served>>

Init ==
    /\ passes = [k \in Keys |-> 0]
    /\ active = {}
    /\ eligible = Keys
    /\ counts = [k \in Keys |-> 0]
    /\ phase = 0
    /\ completedCycle = FALSE
    /\ cycleCounts = [k \in Keys |-> 0]
    /\ served = [k \in Keys |-> 0]

Prepared ==
    [k \in Keys |-> IF k \in eligible \ active
                    THEN Max(passes[k], Stride(k)) ELSE passes[k]]
Winner ==
    IF eligible = {} THEN 0
    ELSE IF Mode \in {"strict", "broken"} THEN Min(eligible)
    ELSE CHOOSE k \in eligible :
         \A j \in eligible :
             Prepared[k] < Prepared[j]
             \/ (Prepared[k] = Prepared[j] /\ k <= j)

Grant ==
    /\ eligible # {}
    /\ Packable
    /\ LET k == Winner
           nextCounts == [counts EXCEPT ![k] = @ + 1]
       IN /\ passes' = [j \in Keys |->
                 Max(0, Prepared[j]
                        + (IF j = k THEN Stride(j) ELSE 0) - Prepared[k])]
          /\ active' = eligible
          /\ served' = [served EXCEPT ![k] = 1 - @]
          /\ IF Dynamic
             THEN UNCHANGED <<phase, counts, completedCycle, cycleCounts>>
             ELSE IF phase = TotalWeight - 1
             THEN /\ phase' = 0
                  /\ counts' = [j \in Keys |-> 0]
                  /\ completedCycle' = TRUE
                  /\ cycleCounts' = nextCounts
             ELSE /\ phase' = phase + 1
                  /\ counts' = nextCounts
                  /\ UNCHANGED <<completedCycle, cycleCounts>>
    /\ UNCHANGED eligible

ChangeEligibility ==
    /\ Dynamic
    /\ eligible' \in SUBSET Keys
    /\ UNCHANGED <<passes, active, counts, phase, completedCycle, cycleCounts,
                   served>>

Next == Grant \/ ChangeEligibility
Spec == Init /\ [][Next]_vars /\ WF_vars(Grant)
UnfairSpec == Init /\ [][Next]_vars

TypeOK ==
    /\ passes \in [Keys -> Nat]
    /\ active \subseteq Keys /\ eligible \subseteq Keys
    /\ counts \in [Keys -> 0..TotalWeight]
    /\ phase \in 0..(TotalWeight - 1)
    /\ completedCycle \in BOOLEAN
    /\ cycleCounts \in [Keys -> 0..TotalWeight]
    /\ served \in [Keys -> {0, 1}]

\* Independent count predicates do not invoke Winner or compare two selectors.
ServiceDeviation ==
    Dynamic \/ Mode = "strict" \/
        \A k \in Keys :
            Abs(counts[k] * TotalWeight - phase * Weights[k])
                <= Cardinality(Keys) * TotalWeight
FixedCycleShare ==
    Dynamic \/ Mode = "strict" \/ ~completedCycle \/
        \A k \in Keys : cycleCounts[k] = Weights[k]

\* Two recurring bit values witness infinitely many grants, not a stale winner.
EventualService ==
    \A k \in Keys :
        (<>[](k \in eligible)) =>
            ([]<>(served[k] = 0) /\ []<>(served[k] = 1))
=======================================================================
