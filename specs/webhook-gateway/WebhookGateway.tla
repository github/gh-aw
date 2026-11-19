------------------------ MODULE WebhookGateway ------------------------
EXTENDS Integers, TLC

CONSTANT Fault

Profiles == {"valid", "unsigned", "foreign-source", "nonmatch", "raw-payload",
             "broad-token", "foreign-app", "invalid-oidc", "stale-hash",
             "copied-hash", "foreign-response", "invalid-envelope",
             "incomplete-discovery"}
Workers == {1, 2}
Phases == {"idle", "queued", "sending", "nonaccepted", "uncertain", "accepted"}
RunStages == {"none", "provenance", "admission", "response", "activation", "agent"}
Bypass(name) == Fault = name

VARIABLE s
vars == <<s>>

Init ==
    s \in [profile : Profiles,
           approved : {TRUE}, generation : {0},
           table : BOOLEAN, tableGeneration : {0}, candidate : {-1},
           phase : {"idle"}, receipt : {FALSE},
           duplicateSeen : {FALSE}, conflictSeen : {FALSE},
           receiptChanged : {FALSE}, crashed : {FALSE},
           owner : {0}, fence : {0}, tickets : [{1, 2} -> {0}],
           attempts : {0}, accepted : {0},
           sent : {FALSE}, sendAuthorized : {TRUE},
           sendScoped : {TRUE}, sendFenced : {TRUE},
           run : {"none"}, principal : {TRUE},
           grantCurrent : {TRUE}]
    /\ s.table = (s.profile # "incomplete-discovery")

BeginScan ==
    /\ s.candidate = -1
    /\ s' = [s EXCEPT !.candidate = s.generation]

Publish ==
    /\ s.candidate # -1
    /\ (s.profile # "incomplete-discovery" \/ Bypass("incomplete-scan"))
    /\ ((s.approved /\ s.candidate = s.generation)
        \/ Bypass("stale-publication"))
    /\ s' = [s EXCEPT !.table = TRUE, !.tableGeneration = s.candidate,
                     !.candidate = -1]

Revoke ==
    /\ s.approved
    /\ s' = [s EXCEPT !.approved = FALSE, !.generation = 1, !.table = FALSE]

Receive ==
    /\ s.phase = "idle"
    /\ s.profile # "foreign-app"
    /\ s.approved /\ s.table
    /\ (s.profile # "unsigned" \/ Bypass("source-auth"))
    /\ (s.profile # "foreign-source" \/ Bypass("source-binding"))
    /\ (s.profile # "nonmatch" \/ Bypass("predicates"))
    /\ (s.profile # "raw-payload" \/ Bypass("projection"))
    /\ s' = [s EXCEPT !.receipt = TRUE, !.phase = "queued"]

Duplicate ==
    /\ s.receipt /\ ~s.duplicateSeen
    /\ s' = [s EXCEPT !.duplicateSeen = TRUE]

ConflictingDuplicate ==
    /\ s.receipt /\ ~s.conflictSeen
    /\ s' = [s EXCEPT !.conflictSeen = TRUE,
                     !.receiptChanged = Bypass("conflicting-replay")]

Claim(w) ==
    /\ s.phase = "queued" /\ s.fence < 2 /\ s.owner # w
    /\ s' = [s EXCEPT !.owner = w, !.fence = @ + 1,
                     !.tickets[w] = s.fence + 1]

\* The local authorization point precedes the external GitHub side effect.
Transmit(w) ==
    /\ s.phase = "queued" /\ s.attempts < 2 /\ s.tickets[w] > 0
    /\ ((s.owner = w /\ s.tickets[w] = s.fence) \/ Bypass("worker-fence"))
    /\ ((s.approved /\ s.table /\ s.tableGeneration = s.generation)
        \/ Bypass("dispatch-recheck"))
    /\ (s.profile # "broad-token" \/ Bypass("token-scope"))
    /\ s' = [s EXCEPT !.phase = "sending", !.attempts = @ + 1,
                     !.sent = TRUE,
                     !.sendAuthorized =
                         s.approved /\ s.table /\ s.tableGeneration = s.generation,
                     !.sendScoped = s.profile # "broad-token",
                     !.sendFenced = s.owner = w /\ s.tickets[w] = s.fence]

Accepted ==
    /\ s.phase = "sending"
    /\ s' = [s EXCEPT !.phase = "accepted", !.accepted = @ + 1]

KnownNonacceptance ==
    /\ s.phase = "sending"
    /\ s' = [s EXCEPT !.phase = "nonaccepted"]

LostResponse ==
    /\ s.phase = "sending"
    /\ \E accepted \in {0, 1}:
        s' = [s EXCEPT !.phase = "uncertain", !.accepted = @ + accepted]

RetryKnown ==
    /\ s.phase = "nonaccepted" /\ s.attempts < 2
    /\ s' = [s EXCEPT !.phase = "queued"]

RetryUncertain ==
    /\ Bypass("retry-uncertain")
    /\ s.phase = "uncertain" /\ s.attempts < 2
    /\ s' = [s EXCEPT !.phase = "queued"]

\* Persisted attempts survive restart; an in-flight call may have been accepted.
Crash ==
    /\ ~s.crashed
    /\ \E accepted \in {0, 1}:
        /\ (s.phase # "sending" => accepted = 0)
        /\ s' = [s EXCEPT !.crashed = TRUE,
                         !.accepted = @ + accepted,
                         !.phase = IF s.phase = "sending" THEN "uncertain"
                                   ELSE IF s.phase = "accepted"
                                           /\ Bypass("forget-receipt")
                                        THEN "queued" ELSE s.phase]

StartRun ==
    /\ s.run = "none" /\ s.accepted > 0
    /\ s' = [s EXCEPT !.run = "provenance"]

\* An unrelated principal can start an Actions run without using this receiver.
ExternalDispatch ==
    /\ s.profile = "foreign-app" /\ s.run = "none"
    /\ s' = [s EXCEPT !.run = "provenance", !.principal = FALSE]

Provenance ==
    /\ s.run = "provenance"
    /\ (s.principal \/ Bypass("app-provenance"))
    /\ s' = [s EXCEPT !.run = "admission"]

ServerAdmission ==
    /\ s.run = "admission"
    /\ s.approved /\ s.receipt
    /\ (s.profile # "invalid-oidc" \/ Bypass("oidc"))
    /\ (s.profile # "stale-hash" \/ Bypass("hash"))
    /\ (s.profile # "copied-hash" \/ Bypass("artifact"))
    /\ s' = [s EXCEPT !.run = "response", !.grantCurrent = s.approved]

ClientContinuation ==
    /\ s.run = "response"
    /\ (s.profile # "foreign-response" \/ Bypass("response"))
    /\ s' = [s EXCEPT !.run = "activation"]

Activate ==
    /\ s.run = "activation"
    /\ (s.profile # "invalid-envelope" \/ Bypass("envelope"))
    /\ s' = [s EXCEPT !.run = "agent"]

Next ==
    BeginScan \/ Publish \/ Revoke \/ Receive \/ Duplicate
    \/ ConflictingDuplicate \/ (\E w \in Workers: Claim(w) \/ Transmit(w))
    \/ Accepted \/ KnownNonacceptance \/ LostResponse \/ RetryKnown
    \/ RetryUncertain \/ Crash \/ StartRun \/ ExternalDispatch
    \/ Provenance \/ ServerAdmission \/ ClientContinuation \/ Activate

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.profile \in Profiles
    /\ s.approved \in BOOLEAN /\ s.generation \in 0..1
    /\ s.table \in BOOLEAN /\ s.tableGeneration \in 0..1
    /\ s.candidate \in -1..1
    /\ s.phase \in Phases /\ s.receipt \in BOOLEAN
    /\ s.duplicateSeen \in BOOLEAN /\ s.conflictSeen \in BOOLEAN
    /\ s.receiptChanged \in BOOLEAN /\ s.crashed \in BOOLEAN
    /\ s.owner \in {0, 1, 2} /\ s.fence \in 0..2
    /\ s.tickets \in [Workers -> 0..2]
    /\ s.attempts \in 0..2 /\ s.accepted \in 0..2
    /\ s.sent \in BOOLEAN /\ s.sendAuthorized \in BOOLEAN
    /\ s.sendScoped \in BOOLEAN /\ s.sendFenced \in BOOLEAN
    /\ s.run \in RunStages /\ s.principal \in BOOLEAN
    /\ s.grantCurrent \in BOOLEAN

SourceAuthenticated == s.receipt => s.profile # "unsigned"
SourceEnrolled == s.receipt => s.profile # "foreign-source"
PredicatesMatched == s.receipt => s.profile # "nonmatch"
MinimalDispatch == s.sent => s.profile # "raw-payload"
CompleteDiscovery == s.table => s.profile # "incomplete-discovery"
FencedPublication ==
    s.table => s.approved /\ s.tableGeneration = s.generation
CurrentDispatchAuthorization == s.sendAuthorized
ScopedDispatchToken == s.sendScoped
FencedWorker == s.sendFenced
ImmutableReceipt == ~s.receiptChanged
AtMostOneAcceptance == s.accepted <= 1
AppProvenance ==
    s.run \in {"admission", "response", "activation", "agent"} => s.principal
SignedAdmission ==
    s.run \in {"response", "activation", "agent"} =>
        s.profile # "invalid-oidc" /\ s.grantCurrent /\ s.receipt
HashBoundAdmission ==
    s.run \in {"response", "activation", "agent"} => s.profile # "stale-hash"
ArtifactBoundAdmission ==
    s.run \in {"response", "activation", "agent"} => s.profile # "copied-hash"
CorrelatedContinuation ==
    s.run \in {"activation", "agent"} => s.profile # "foreign-response"
ValidatedActivation == s.run = "agent" => s.profile # "invalid-envelope"

NoAgent == s.run # "agent"
NoUncertainty == s.phase # "uncertain"
NoKnownRetry == ~(s.attempts = 2 /\ s.accepted = 1)
NoDuplicate == ~s.duplicateSeen
NoConflict == ~s.conflictSeen
NoCrash == ~s.crashed
=============================================================================
