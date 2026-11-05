----------------------------- MODULE Counter -----------------------------
EXTENDS Naturals
CONSTANT Limit
VARIABLE n
vars == <<n>>
Init == n = 0
Advance == /\ n < Limit
           /\ n' = n + 1
Next == Advance
Spec == Init /\ [][Next]_vars /\ WF_vars(Advance)
Bounded == n \in 0..Limit
Progress == <>(n = Limit)
NeverAtLimit == n < Limit
=============================================================================
