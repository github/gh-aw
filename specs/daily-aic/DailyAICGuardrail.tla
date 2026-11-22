--------------------------- MODULE DailyAICGuardrail ---------------------------
EXTENDS Integers, FiniteSets

Runs == {1, 2}
Modes == {"enforce", "fail_open", "disabled", "bypass"}
LedgerStates == {"not_selected", "mature", "cold", "absent", "untrusted", "clone_error"}
SnapshotStates == {"miss", "recorded", "estimated", "mismatched", "untrusted"}
ReadStages == {"restore", "history", "resolve"}
Stages == ReadStages \cup {"select", "wait", "ledger", "done"}
Decisions == {"pending", "under_budget", "exceeded", "disabled", "skipped",
              "structural_error", "transient_error", "quota_blocked"}
Unknown == {"structural_error", "transient_error", "quota_blocked"}
Certified == {"under_budget", "exceeded"}
Limit == 4
Maximum == 2
NoValue == -1
None == [source |-> "none", attempt |-> 0, credits |-> NoValue]
EmptyAccounting == [r \in Runs |-> None]

VARIABLES config, s
vars == <<config, s>>

Init ==
    /\ config \in [mode : Modes, ledger : LedgerStates, insecure : BOOLEAN,
                   firstAttempt : BOOLEAN, canEstimate : BOOLEAN]
    /\ s = [stage |-> "select", backend |-> "none", snapshot |-> "miss",
            history |-> "workflow", pages |-> 0, members |-> {}, observed |-> EmptyAccounting,
            retries |-> 0, deferred |-> {}, waitingFor |-> "none",
            decision |-> "pending", total |-> NoValue, estimated |-> NoValue, start |-> FALSE]

\* A scan deferral changes the environment: run 1 leaves the window and run 2
\* completes a new attempt. The page bound is two instead of the runtime's 25.
Generation == IF "scan" \in s.deferred THEN 2 ELSE 1
Identity(r) == IF r = 2 THEN Generation ELSE 1
CurrentRun == 3
ForeignRun == 4
ExpiredRun == 5
Page(n) ==
    IF n = 1
    THEN (IF Generation = 1 THEN {1, CurrentRun} ELSE {ForeignRun})
    ELSE (IF Generation = 1 THEN {1, 2, ExpiredRun} ELSE {2, ExpiredRun})
ExpectedMembers == IF Generation = 1 THEN Runs ELSE {2}
Measured(r) == r

Decide(result, value, estimatedValue) ==
    s' = [s EXCEPT !.stage = "done", !.decision = result,
          !.total = value, !.estimated = estimatedValue,
          !.start = result \in {"under_budget", "disabled", "skipped"}
                    \/ (result \in Unknown /\ config.mode = "fail_open")]

Publish(value, estimatedValue) ==
    Decide(IF value < Limit THEN "under_budget" ELSE "exceeded", value, estimatedValue)

Stop(result) == Decide(result, NoValue, NoValue)

UseLedger ==
    config.ledger = "mature"
    \/ (config.insecure /\ config.ledger \in {"cold", "absent", "untrusted"})

Select ==
    /\ s.stage = "select"
    /\ CASE config.mode = "disabled" -> Stop("disabled")
         [] config.mode = "bypass" -> Stop("skipped")
         [] UseLedger -> s' = [s EXCEPT !.stage = "ledger", !.backend = "ledger"]
         [] config.ledger \in {"not_selected", "cold", "absent"} ->
                s' = [s EXCEPT !.stage = "restore", !.backend = "artifact"]
         [] OTHER -> Stop("structural_error")

ReadLedger ==
    /\ s.stage = "ledger"
    \* These totals can come from a permissive read; zero need not prove coverage.
    /\ \E credits \in {0, Limit}: Publish(credits, 0)

Restore ==
    /\ s.stage = "restore"
    /\ \E snapshot \in SnapshotStates:
        s' = [s EXCEPT !.snapshot = snapshot, !.stage = "history", !.retries = 0]

ReadPage ==
    /\ s.stage = "history"
    /\ LET n == s.pages + 1 IN
        s' = [s EXCEPT !.pages = n, !.members = @ \cup (Page(n) \cap Runs),
              !.stage = IF n = 2 THEN "resolve" ELSE "history", !.retries = 0]

UseRepositoryHistory ==
    /\ s.stage = "history"
    /\ s.history = "workflow"
    /\ s.pages = 0
    /\ s' = [s EXCEPT !.history = "repository", !.retries = 0]

Account(r, source, credits) ==
    s' = [s EXCEPT !.observed[r] = [source |-> source, attempt |-> Identity(r), credits |-> credits],
          !.retries = 0]

Unresolved(r) ==
    IF config.canEstimate THEN Account(r, "estimated", Maximum) ELSE Stop("transient_error")

CanReuse(r) == s.snapshot = "recorded" /\ Identity(r) = 1
PendingRuns == {r \in s.members : s.observed[r] = None}
PendingRun == IF 1 \in PendingRuns THEN 1 ELSE 2

Resolve ==
    /\ s.stage = "resolve"
    /\ PendingRuns # {}
    /\ LET r == PendingRun IN
       IF CanReuse(r)
       THEN Account(r, "cached", Measured(r))
       ELSE Account(r, "recorded", Measured(r)) \/ Unresolved(r)

Aggregate ==
    /\ s.stage = "resolve"
    /\ PendingRuns = {}
    /\ LET value(r) == IF r \in s.members THEN s.observed[r].credits ELSE 0
           estimate(r) == IF s.observed[r].source = "estimated" THEN value(r) ELSE 0
       IN Publish(value(1) + value(2), estimate(1) + estimate(2))

Reading == s.stage \in {"restore", "history"}
           \/ (s.stage = "resolve" /\ PendingRuns # {} /\ ~CanReuse(PendingRun))

TransientRead ==
    /\ Reading
    /\ IF s.retries < 2
       THEN s' = [s EXCEPT !.retries = @ + 1]
       ELSE IF s.stage = "resolve"
            THEN Unresolved(PendingRun)
            ELSE Stop("transient_error")

StructuralReadError == Reading /\ Stop("structural_error")

QuotaStop ==
    /\ Reading
    /\ LET phase == IF s.stage = "restore" THEN "restore" ELSE "scan"
       IN \/ Stop("quota_blocked")
          \/ /\ config.firstAttempt
             /\ phase \notin s.deferred
             \* This branch abstracts an eligible deadline within the wait budget.
             /\ s' = [s EXCEPT !.stage = "wait", !.waitingFor = phase, !.deferred = @ \cup {phase},
                       !.history = "workflow", !.pages = 0, !.members = {},
                       !.observed = EmptyAccounting, !.retries = 0]

AfterWait ==
    /\ s.stage = "wait"
    /\ \/ Stop("quota_blocked")
       \/ s' = [s EXCEPT !.stage = IF s.waitingFor = "restore" THEN "restore" ELSE "history",
                 !.waitingFor = "none"]

Next ==
    /\ UNCHANGED config
    /\ \/ Select \/ ReadLedger \/ Restore \/ ReadPage \/ UseRepositoryHistory
       \/ Resolve \/ Aggregate \/ TransientRead \/ StructuralReadError \/ QuotaStop \/ AfterWait

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

TypeOK ==
    /\ s.stage \in Stages
    /\ s.backend \in {"none", "artifact", "ledger"}
    /\ s.snapshot \in SnapshotStates
    /\ s.history \in {"workflow", "repository"}
    /\ s.pages \in 0..2
    /\ s.members \subseteq Runs
    /\ s.observed \in [Runs -> [source : {"none", "cached", "recorded", "estimated"},
                                 attempt : 0..2, credits : NoValue..Maximum]]
    /\ s.retries \in 0..2
    /\ s.deferred \subseteq {"restore", "scan"}
    /\ s.waitingFor \in {"none", "restore", "scan"}
    /\ s.decision \in Decisions
    /\ s.total \in NoValue..(2 * Maximum)
    /\ s.estimated \in NoValue..(2 * Maximum)
    /\ s.start \in BOOLEAN

VerifiedArtifactTotal ==
    (s.backend = "artifact" /\ s.decision \in Certified) =>
      /\ s.pages = 2
      /\ s.members = ExpectedMembers
      /\ \A r \in ExpectedMembers :
           /\ s.observed[r].attempt = Identity(r)
           /\ s.observed[r].source \in {"cached", "recorded", "estimated"}
      /\ LET estimates == {r \in ExpectedMembers : s.observed[r].source = "estimated"}
             measured == ExpectedMembers \ estimates
         IN /\ s.estimated = Cardinality(estimates) * Maximum
            /\ s.total = s.estimated + (IF 1 \in measured THEN 1 ELSE 0)
                                     + (IF 2 \in measured THEN 2 ELSE 0)

ObservationProvenance ==
    \A r \in Runs :
      /\ (s.observed[r] # None => r \in s.members /\ s.pages = 2)
      /\ (s.observed[r].source = "cached" =>
            s.snapshot = "recorded" /\ s.observed[r].attempt = 1 /\ Identity(r) = 1)
      /\ (s.observed[r].source = "estimated" => config.canEstimate /\ s.observed[r].credits = Maximum)

Admission ==
    /\ (s.decision \in Certified =>
          /\ s.total >= 0 /\ s.estimated >= 0
          /\ (s.decision = "under_budget") = (s.total < Limit))
    /\ (s.decision \notin Certified => s.total = NoValue /\ s.estimated = NoValue)
    /\ (s.start => s.decision \in {"under_budget", "disabled", "skipped"}
                  \/ (s.decision \in Unknown /\ config.mode = "fail_open"))
    /\ (config.mode \in {"disabled", "bypass"} => s.backend = "none" /\ s.pages = 0)

QuotaRecovery ==
    /\ (s.deferred # {} => config.firstAttempt)
    /\ (s.stage = "wait" =>
          /\ s.waitingFor \in s.deferred
          /\ s.pages = 0 /\ s.members = {} /\ s.observed = EmptyAccounting
          /\ s.decision = "pending" /\ ~s.start)

LedgerPolicy ==
    /\ (s.backend = "ledger" =>
          /\ config.ledger = "mature"
             \/ (config.insecure /\ config.ledger \in {"cold", "absent", "untrusted"})
          /\ s.pages = 0 /\ s.observed = EmptyAccounting
          /\ (s.decision \in Certified => s.estimated = 0))
    /\ (s.backend = "artifact" =>
          config.ledger = "not_selected" \/ (~config.insecure /\ config.ledger \in {"cold", "absent"}))

Terminates == <>(s.stage = "done")
=============================================================================
