------------------------- MODULE ExperimentsBroker -------------------------
EXTENDS Naturals, FiniteSets

\* Bounded server contract, not an implementation of a deployed broker.
\* Token validation abstracts signature, issuer, expiration, approved caller
\* and reusable revisions, runner enrollment, and trusted job-role evidence.
CONSTANTS Tenants, Attempts, Variants, Observations, Bug
ASSUME /\ Tenants # {} /\ Attempts # {} /\ Variants # {}
       /\ Observations # {}
       /\ "none" \notin Variants \cup Observations
       /\ Bug \in {"none", "authorization", "reassign", "overwrite", "dry-run"}

\* Workflow and run are fixed; tenant and attempt vary independently.
Keys == [tenant : Tenants, attempt : Attempts]
Tokens == [key : Keys, valid : BOOLEAN, audience : {"broker", "other"}]

VARIABLES assignments, reports, counts, assignmentHistory, reportHistory,
          unauthorizedWrite, dryRunWrite
vars == <<assignments, reports, counts, assignmentHistory, reportHistory,
          unauthorizedWrite, dryRunWrite>>

Authorized(token, key) ==
    /\ token.valid /\ token.audience = "broker" /\ token.key = key

Allowed(token, key, dryRun) ==
    /\ (Authorized(token, key) \/ Bug = "authorization")
    /\ (~dryRun \/ Bug = "dry-run")

Init ==
    /\ assignments = [k \in Keys |-> "none"]
    /\ reports = [k \in Keys |-> "none"]
    /\ counts = [tenant \in Tenants |-> [v \in Variants |-> 0]]
    /\ assignmentHistory = {} /\ reportHistory = {}
    /\ unauthorizedWrite = FALSE /\ dryRunWrite = FALSE

Assign(token, key, variant, dryRun) ==
    /\ Allowed(token, key, dryRun)
    /\ assignments[key] = "none" \/ Bug = "reassign"
    /\ assignments' = [assignments EXCEPT ![key] = variant]
    /\ counts' = [counts EXCEPT ![key.tenant][variant] = @ + 1]
    /\ assignmentHistory' =
           assignmentHistory \cup {[key |-> key, variant |-> variant]}
    /\ unauthorizedWrite' = (unauthorizedWrite \/ ~Authorized(token, key))
    /\ dryRunWrite' = (dryRunWrite \/ dryRun)
    /\ UNCHANGED <<reports, reportHistory>>

Report(token, key, observation, dryRun) ==
    /\ Allowed(token, key, dryRun)
    /\ assignments[key] # "none"
    /\ reports[key] = "none" \/ Bug = "overwrite"
    /\ reports' = [reports EXCEPT ![key] = observation]
    /\ reportHistory' =
           reportHistory \cup {[key |-> key, observation |-> observation]}
    /\ unauthorizedWrite' = (unauthorizedWrite \/ ~Authorized(token, key))
    /\ dryRunWrite' = (dryRunWrite \/ dryRun)
    /\ UNCHANGED <<assignments, counts, assignmentHistory>>

\* Lost responses, identical retries, conflicting retries, rejected tokens,
\* outages and cancellation may stutter. A retry never reallocates a key.
Next ==
    \/ \E token \in Tokens, key \in Keys, v \in Variants, dry \in BOOLEAN :
           Assign(token, key, v, dry)
    \/ \E token \in Tokens, key \in Keys, o \in Observations, dry \in BOOLEAN :
           Report(token, key, o, dry)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ assignments \in [Keys -> Variants \cup {"none"}]
    /\ reports \in [Keys -> Observations \cup {"none"}]
    /\ counts \in [Tenants -> [Variants -> Nat]]
    /\ assignmentHistory \subseteq [key : Keys, variant : Variants]
    /\ reportHistory \subseteq [key : Keys, observation : Observations]
    /\ unauthorizedWrite \in BOOLEAN /\ dryRunWrite \in BOOLEAN

IdentityBound == ~unauthorizedWrite
DryRunIsLocal == ~dryRunWrite
AssignmentImmutable ==
    \A key \in Keys :
        Cardinality({h \in assignmentHistory : h.key = key}) <= 1
ReportImmutable ==
    \A key \in Keys :
        Cardinality({h \in reportHistory : h.key = key}) <= 1
ReportRequiresAssignment ==
    \A key \in Keys : reports[key] # "none" => assignments[key] # "none"
CountsExactlyOnce ==
    \A tenant \in Tenants, v \in Variants :
        counts[tenant][v] =
            Cardinality({k \in Keys : k.tenant = tenant /\ assignments[k] = v})

=============================================================================
