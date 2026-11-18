------------------------- MODULE ActivationAdmission -------------------------
EXTENDS Naturals, TLC

CONSTANTS Fault, OutagePolicy, OTelEnabled
VARIABLE s

MaxAttempts == 3
PolicyCap == 2
TraceIDs == {"trace-existing", "trace-generated", "trace-foreign"}
Contexts ==
    [endpoint : {"absent", "valid", "invalid"},
     identity : {"valid", "invalid"},
     triggeringActor : {"unused", "verified", "unverified"},
     localCap : 0..3,
     incomingTrace : {"absent", "valid"},
     detectionAllowance : BOOLEAN,
     evalsAllowance : BOOLEAN,
     dailyResult : {"allow", "block"},
     hiddenSpend : {"low", "high"}]
Decisions ==
    [invocation : {"current", "foreign"}, cap : 1..3,
     detectionCap : 0..2, evalsCap : 0..2]
NoDecision == [invocation |-> "none", cap |-> 0, detectionCap |-> 0, evalsCap |-> 0]
EmptyReply ==
    [kind |-> "none", invocation |-> "none", cap |-> 0,
     detectionCap |-> 0, evalsCap |-> 0, detail |-> "empty"]
MinimumCap(local, server) ==
    IF local = 0 \/ server < local THEN server ELSE local
CallerTrace(context) ==
    IF context.incomingTrace = "valid" THEN "trace-existing" ELSE "trace-generated"
Authorized ==
    /\ (s.context.identity = "valid" \/ Fault = "identity-bypass")
    /\ (s.context.triggeringActor # "unverified" \/ Fault = "trigger-bypass")
FreshDecision ==
    [invocation |-> IF Fault = "cross-invocation" THEN "foreign" ELSE "current",
     cap |-> IF Fault = "spend-ceiling" /\ s.context.hiddenSpend = "high"
             THEN 1 ELSE PolicyCap,
     detectionCap |-> IF s.context.detectionAllowance THEN PolicyCap ELSE 0,
     evalsCap |-> IF s.context.evalsAllowance THEN PolicyCap ELSE 0]
Decision ==
    IF s.cached = NoDecision THEN FreshDecision
    ELSE IF Fault = "retry-result"
         THEN [s.cached EXCEPT !.cap = IF @ = 1 THEN 2 ELSE 1]
         ELSE s.cached

Init ==
    \E context \in Contexts :
        s = [context |-> context, phase |-> "configure", activation |-> "pending",
             admissionStepOutcome |-> "pending",
             oidcJobs |-> {"activation", "conclusion"},
             admission |-> "none", attempts |-> 0, credential |-> "none",
             cached |-> NoDecision, firstDecision |-> NoDecision, reply |-> EmptyReply,
             firstReplica |-> 0, replica |-> 0, recovered |-> FALSE,
             denialAt |-> 0, malformed |-> FALSE, fatal |-> FALSE,
             timedOut |-> FALSE, dailyChecks |-> 0, dailyValidation |-> "pending",
             runtimeCap |-> context.localCap, approvedCap |-> 0,
             runtimeDetectionCap |-> context.localCap, runtimeEvalsCap |-> context.localCap,
             approvedDetectionCap |-> 0, approvedEvalsCap |-> 0,
             runtimeAdmission |-> FALSE, started |-> FALSE,
             privilegesActor |-> "original", financialEffects |-> 0,
             traceId |-> "none", traceSent |-> FALSE, traceExported |-> FALSE,
             exportedTrace |-> "none",
             outcome |-> "none", lastEvent |-> "Init"]

Configure ==
    /\ s.phase = "configure"
    /\ s' = [s EXCEPT
        !.oidcJobs = IF Fault = "shared-agent-oidc" THEN {"activation", "conclusion", "agent"} ELSE @,
        !.phase = IF s.context.endpoint = "valid"
                     \/ (s.context.endpoint = "absent" /\ Fault = "endpoint-bypass")
                  THEN "request"
                  ELSE IF s.context.endpoint = "absent" THEN "daily" ELSE "finalize",
        !.activation = IF s.context.endpoint = "absent" /\ Fault # "endpoint-bypass"
                       THEN "pending"
                       ELSE IF s.context.endpoint = "invalid" THEN "failed" ELSE @,
        !.admission = IF s.context.endpoint = "absent" /\ Fault # "endpoint-bypass"
                      THEN "skipped"
                      ELSE IF s.context.endpoint = "invalid" THEN "blocked" ELSE @,
        !.admissionStepOutcome = IF s.context.endpoint = "absent" /\ Fault # "endpoint-bypass"
                              THEN "skipped"
                              ELSE IF s.context.endpoint = "invalid" THEN "failure" ELSE @,
        !.lastEvent = "Configure"]

Send ==
    /\ s.phase = "request"
    /\ s' = [s EXCEPT
        !.phase = "response", !.attempts = @ + 1,
        !.credential = IF Fault = "credential-delegation" THEN "github-api" ELSE "oidc",
        !.traceId = CallerTrace(s.context),
        !.traceSent = Fault # "traceparent-omitted",
        !.lastEvent = "SendPOST"]

FatalClientFailure ==
    /\ s.phase = "request"
    /\ LET failOpen == Fault = "fatal-as-outage" /\ OutagePolicy = "ignore"
       IN s' = [s EXCEPT
            !.phase = IF failOpen THEN "daily" ELSE "finalize",
            !.activation = IF failOpen THEN "pending" ELSE "failed",
            !.admission = IF failOpen THEN "outage" ELSE "blocked",
            !.admissionStepOutcome = IF failOpen THEN "success" ELSE "failure",
            !.fatal = TRUE, !.lastEvent = "FatalClientFailure"]

ReturnAllow ==
    /\ s.phase = "response"
    /\ Authorized
    /\ \E replica \in {1, 2}, delivered \in BOOLEAN :
        LET decision == Decision
        IN s' = [s EXCEPT
            !.phase = "evaluate",
            !.cached = decision,
            !.firstDecision = IF @ = NoDecision THEN decision ELSE @,
            !.firstReplica = IF @ = 0 THEN replica ELSE @,
            !.replica = replica,
            !.recovered = s.cached # NoDecision /\ delivered /\ replica # s.firstReplica,
            !.reply = IF delivered
                      THEN [kind |-> "allow", invocation |-> decision.invocation,
                            cap |-> decision.cap, detectionCap |-> decision.detectionCap,
                            evalsCap |-> decision.evalsCap, detail |-> "empty"]
                      ELSE [EmptyReply EXCEPT !.kind = "transient"],
            !.financialEffects = IF Fault = "admission-charge" THEN @ + 1 ELSE @,
            !.lastEvent = IF delivered THEN "Return200" ELSE "Lose200Response"]

ReturnDeny ==
    /\ s.phase = "response"
    /\ s' = [s EXCEPT
        !.phase = "evaluate", !.denialAt = s.attempts,
        !.reply = [EmptyReply EXCEPT !.kind = "deny",
                   !.detail = IF Fault = "denial-leak" THEN s.context.hiddenSpend ELSE @],
        !.lastEvent = "Return403"]

ReturnMalformed ==
    /\ s.phase = "response"
    /\ s' = [s EXCEPT
        !.phase = "evaluate", !.malformed = TRUE,
        !.reply = [EmptyReply EXCEPT !.kind = "malformed"],
        !.lastEvent = "ReturnInvalidProtocol"]

ReturnTransient ==
    /\ s.phase = "response"
    /\ s' = [s EXCEPT
        !.phase = "evaluate",
        !.reply = [EmptyReply EXCEPT !.kind = "transient"],
        !.lastEvent = "ReturnTransient"]

EvaluateAllow ==
    /\ s.phase = "evaluate" /\ s.reply.kind = "allow"
    /\ s' = [s EXCEPT
        !.phase = IF Fault = "daily-after-admission" THEN "daily" ELSE "activate",
        !.admissionStepOutcome = "success", !.admission = "admitted",
        !.approvedCap = IF Fault = "cap-not-validated" THEN 0 ELSE s.reply.cap,
        !.approvedDetectionCap = IF Fault = "optional-cap-ignored" THEN 0 ELSE s.reply.detectionCap,
        !.approvedEvalsCap = s.reply.evalsCap,
        !.runtimeCap = IF Fault = "agent-cap-mutation"
                      THEN s.context.localCap ELSE s.reply.cap,
        !.runtimeDetectionCap = IF s.reply.detectionCap > 0 THEN s.reply.detectionCap ELSE s.context.localCap,
        !.runtimeEvalsCap = IF s.reply.evalsCap > 0 THEN s.reply.evalsCap ELSE s.context.localCap,
        !.traceId = IF Fault = "server-trace-override" THEN "trace-foreign" ELSE @,
        !.dailyValidation = IF Fault # "daily-after-admission" THEN "bypassed" ELSE @,
        !.lastEvent = "Validate200"]

EvaluateRejection ==
    /\ s.phase = "evaluate" /\ s.reply.kind \in {"deny", "malformed"}
    /\ LET failOpen ==
               OutagePolicy = "ignore"
               /\ ((s.reply.kind = "deny" /\ Fault = "deny-as-outage")
                   \/ (s.reply.kind = "malformed" /\ Fault = "malformed-as-outage"))
       IN s' = [s EXCEPT
            !.phase = IF failOpen THEN "daily" ELSE "finalize",
            !.activation = IF failOpen THEN "pending" ELSE "failed",
            !.admission = IF failOpen THEN "outage" ELSE "blocked",
            !.admissionStepOutcome = IF failOpen THEN "success" ELSE "failure",
            !.lastEvent = "BlockRejection"]

EvaluateTransient ==
    /\ s.phase = "evaluate" /\ s.reply.kind = "transient"
    /\ LET retry == s.attempts < MaxAttempts + (IF Fault = "retry-overflow" THEN 1 ELSE 0)
           failOpen == OutagePolicy = "ignore" \/ Fault = "ignore-block"
       IN s' = [s EXCEPT
            !.phase = IF retry THEN "request" ELSE IF failOpen THEN "daily" ELSE "finalize",
            !.activation = IF retry THEN @ ELSE IF failOpen THEN "pending" ELSE "failed",
            !.admission = IF retry THEN @ ELSE "outage",
            !.admissionStepOutcome = IF retry THEN @ ELSE IF failOpen THEN "success" ELSE "failure",
            !.lastEvent = IF retry THEN "Retry" ELSE "ExhaustOutage"]

DeadlineExpired ==
    /\ (s.phase \in {"request", "response"}
        \/ (s.phase = "evaluate" /\ s.reply.kind = "transient"))
    /\ LET failOpen == OutagePolicy = "ignore"
       IN s' = [s EXCEPT
            !.timedOut = TRUE,
            !.phase = IF Fault = "deadline-retry" THEN "request"
                      ELSE IF failOpen THEN "daily" ELSE "finalize",
            !.activation = IF failOpen THEN "pending" ELSE "failed",
            !.admissionStepOutcome = IF failOpen THEN "success" ELSE "failure",
            !.admission = "outage", !.lastEvent = "DeadlineExpired"]

AdmissionStepFailure ==
    /\ s.phase = "activate" /\ s.admissionStepOutcome = "success"
    /\ s' = [s EXCEPT !.admissionStepOutcome = "failure", !.lastEvent = "AdmissionStepFailure"]

Activate ==
    /\ s.phase = "activate"
    /\ LET allowed == s.admissionStepOutcome \in {"success", "skipped"}
                      \/ Fault = "admission-step-bypass"
       IN s' = [s EXCEPT
            !.phase = IF allowed THEN "ready" ELSE "finalize",
            !.activation = IF allowed THEN "success" ELSE "failed",
            !.lastEvent = "Activate"]

ValidateDailyFallback ==
    /\ s.phase = "daily"
    /\ LET allowed == (s.context.dailyResult = "allow" \/ Fault = "daily-fallback-bypass")
                      /\ s.admissionStepOutcome \in {"success", "skipped"}
       IN s' = [s EXCEPT
            !.phase = IF allowed THEN "ready" ELSE "finalize",
            !.activation = IF allowed THEN "success" ELSE "failed",
            !.dailyChecks = 1, !.dailyValidation = IF allowed THEN "passed" ELSE "failed",
            !.lastEvent = "ValidateDailyFallback"]

StartAgent ==
    /\ ((s.phase = "ready" /\ s.activation = "success")
        \/ (Fault = "activation-bypass" /\ s.phase = "finalize" /\ s.activation = "failed"))
    /\ s' = [s EXCEPT
        !.phase = "running", !.started = TRUE,
        !.runtimeAdmission = Fault = "admission-context-forwarded",
        !.privilegesActor = IF Fault = "actor-substitution" THEN "triggering" ELSE @,
        !.lastEvent = "StartAgent"]

FinishAgent ==
    /\ s.phase = "running"
    /\ \E outcome \in {"success", "failure", "cancelled"} :
        s' = [s EXCEPT !.phase = "finalize", !.outcome = outcome, !.lastEvent = "FinishAgent"]

FinishTelemetry ==
    /\ s.phase = "finalize"
    /\ LET exporting == (OTelEnabled \/ Fault = "force-export") /\ s.attempts > 0
       IN s' = [s EXCEPT
            !.phase = "done", !.traceExported = exporting,
            !.exportedTrace = IF ~exporting THEN "none"
                              ELSE IF Fault = "correlation-drop" THEN "trace-foreign"
                              ELSE s.traceId,
            !.lastEvent = "FinishTelemetry"]

Next ==
    \/ Configure \/ Send \/ FatalClientFailure
    \/ ReturnAllow \/ ReturnDeny \/ ReturnMalformed \/ ReturnTransient
    \/ EvaluateAllow \/ EvaluateRejection \/ EvaluateTransient
    \/ DeadlineExpired \/ AdmissionStepFailure \/ Activate \/ ValidateDailyFallback
    \/ StartAgent \/ FinishAgent \/ FinishTelemetry

Spec == Init /\ [][Next]_s /\ WF_s(Next)

TypeOK ==
    /\ OutagePolicy \in {"ignore", "block"} /\ OTelEnabled \in BOOLEAN
    /\ s.context \in Contexts
    /\ s.phase \in {"configure", "request", "response", "evaluate", "activate", "daily", "ready",
                   "running", "finalize", "done"}
    /\ s.activation \in {"pending", "success", "failed"}
    /\ s.admissionStepOutcome \in {"pending", "success", "failure", "skipped"}
    /\ s.oidcJobs \subseteq {"activation", "conclusion", "agent"}
    /\ s.admission \in {"none", "skipped", "admitted", "blocked", "outage"}
    /\ s.attempts \in 0..4 /\ s.denialAt \in 0..4
    /\ s.credential \in {"none", "oidc", "github-api"}
    /\ s.cached \in Decisions \cup {NoDecision} /\ s.firstDecision \in Decisions \cup {NoDecision}
    /\ s.reply \in [kind : {"none", "allow", "deny", "malformed", "transient"},
                    invocation : {"none", "current", "foreign"}, cap : 0..3,
                    detectionCap : 0..2, evalsCap : 0..2,
                    detail : {"empty", "low", "high"}]
    /\ s.firstReplica \in 0..2 /\ s.replica \in 0..2
    /\ s.runtimeCap \in 0..3 /\ s.approvedCap \in 0..3 /\ s.financialEffects \in 0..4
    /\ s.runtimeDetectionCap \in 0..3 /\ s.runtimeEvalsCap \in 0..3
    /\ s.approvedDetectionCap \in 0..2 /\ s.approvedEvalsCap \in 0..2
    /\ s.dailyChecks \in 0..1
    /\ s.dailyValidation \in {"pending", "bypassed", "passed", "failed"}
    /\ s.traceId \in TraceIDs \cup {"none"}
    /\ s.exportedTrace \in TraceIDs \cup {"none"}
    /\ s.privilegesActor \in {"original", "triggering"}
    /\ s.outcome \in {"none", "success", "failure", "cancelled"}
    /\ \A value \in {s.recovered, s.malformed, s.fatal, s.runtimeAdmission,
                    s.started, s.traceExported, s.traceSent, s.timedOut} : value \in BOOLEAN

ActivationGate == s.started => s.activation = "success"
AdmissionOIDCScope == s.oidcJobs = {"activation", "conclusion"}
AdmissionStepGate == s.activation = "success" => s.admissionStepOutcome \in {"success", "skipped"}
NoEndpointNoRequest == s.context.endpoint = "absent" => s.attempts = 0 /\ s.credential = "none"
InvalidConfigurationBlocks == s.context.endpoint = "invalid" => s.activation # "success" /\ s.attempts = 0
OIDCOnly == s.credential \in {"none", "oidc"}
IdentityVerified == s.firstDecision # NoDecision => s.context.identity = "valid"
TriggeringActorVerified == s.firstDecision # NoDecision => s.context.triggeringActor # "unverified"
OriginalActorPrivileges == s.privilegesActor = "original"
InvocationBound ==
    IF s.cached = NoDecision THEN TRUE
    ELSE s.cached.invocation = "current"
StableRetryDecision == s.cached = s.firstDecision
RetryBound == s.attempts <= MaxAttempts
DenialBlocks == s.denialAt > 0 => s.activation # "success" /\ s.attempts = s.denialAt
MalformedBlocks == s.malformed => s.activation # "success"
FatalErrorsBlock == s.fatal => s.activation = "failed"
OutagePolicyHonored == s.admission = "outage" /\ OutagePolicy = "block" => s.activation = "failed"
DeadlineHonored == s.timedOut => s.phase \notin {"request", "response", "evaluate"}
DailyAdmissionPrecedence ==
    s.admission = "admitted" => s.dailyChecks = 0 /\ s.dailyValidation = "bypassed"
DailyFallbackEnforced ==
    s.started /\ s.admission \in {"skipped", "outage"} =>
        s.context.dailyResult = "allow" /\ s.dailyChecks = 1 /\ s.dailyValidation = "passed"
CeilingValidated ==
    s.admission = "admitted" =>
        s.approvedCap = s.reply.cap /\ s.approvedCap > 0
        /\ s.approvedDetectionCap = s.reply.detectionCap
        /\ s.approvedEvalsCap = s.reply.evalsCap
RuntimeBudgetsResolved ==
    /\ ~s.runtimeAdmission
    /\ s.runtimeCap = IF s.admission = "admitted" THEN s.approvedCap ELSE s.context.localCap
    /\ s.runtimeDetectionCap =
        IF s.admission = "admitted" /\ s.approvedDetectionCap > 0 THEN s.approvedDetectionCap ELSE s.context.localCap
    /\ s.runtimeEvalsCap =
        IF s.admission = "admitted" /\ s.approvedEvalsCap > 0 THEN s.approvedEvalsCap ELSE s.context.localCap
NoFabricatedAdmission ==
    s.admission \in {"skipped", "outage"} =>
        s.approvedCap = 0 /\ s.approvedDetectionCap = 0 /\ s.approvedEvalsCap = 0
PrivateDenial == s.reply.kind = "deny" => s.reply.detail = "empty"
SpendPrivateCeiling ==
    IF s.firstDecision = NoDecision THEN TRUE
    ELSE /\ s.firstDecision.cap = PolicyCap
         /\ s.firstDecision.detectionCap = IF s.context.detectionAllowance THEN PolicyCap ELSE 0
         /\ s.firstDecision.evalsCap = IF s.context.evalsAllowance THEN PolicyCap ELSE 0
NoFinancialEffects == s.financialEffects = 0
TraceparentRequired == s.attempts > 0 => s.traceSent
CallerTracePreserved == s.attempts > 0 => s.traceId = CallerTrace(s.context)
CorrelationPreserved == s.traceExported => s.exportedTrace = s.traceId
OptionalExport == ~OTelEnabled => ~s.traceExported
Termination == <>(s.phase = "done")

NoAdmittedStart == ~(s.started /\ s.admission = "admitted")
NoSkippedStart == ~(s.started /\ s.admission = "skipped")
NoOutageStart == ~(s.started /\ s.admission = "outage")
NoRetryRecovery == ~(s.started /\ s.admission = "admitted" /\ s.recovered /\ s.attempts > 1)
NoVerifiedRerun == ~(s.started /\ s.admission = "admitted" /\ s.context.triggeringActor = "verified")
NoUnexportedAdmission ==
    ~(s.phase = "done" /\ s.started /\ s.admission = "admitted"
      /\ ~OTelEnabled /\ s.traceSent /\ ~s.traceExported)
NoExistingTraceAdmission ==
    ~(s.started /\ s.admission = "admitted" /\ s.traceId = "trace-existing")
NoGeneratedTraceAdmission ==
    ~(s.started /\ s.admission = "admitted" /\ s.traceId = "trace-generated")
NoPrimaryOnlyAllowance ==
    ~(s.started /\ s.admission = "admitted"
      /\ s.approvedDetectionCap = 0 /\ s.approvedEvalsCap = 0)
NoDetectionOnlyAllowance ==
    ~(s.started /\ s.admission = "admitted"
      /\ s.approvedDetectionCap > 0 /\ s.approvedEvalsCap = 0)
NoEvalsOnlyAllowance ==
    ~(s.started /\ s.admission = "admitted"
      /\ s.approvedDetectionCap = 0 /\ s.approvedEvalsCap > 0)
NoCombinedAllowances ==
    ~(s.started /\ s.admission = "admitted"
      /\ s.approvedDetectionCap > 0 /\ s.approvedEvalsCap > 0)
NoCompletedDenial == ~(s.phase = "done" /\ s.denialAt > 0 /\ ~s.started)
NoAdmittedDailyBypass ==
    ~(s.started /\ s.admission = "admitted" /\ s.context.dailyResult = "block" /\ s.dailyChecks = 0)
NoSkippedDailyBlock ==
    ~(s.phase = "done" /\ s.admission = "skipped" /\ s.dailyValidation = "failed" /\ ~s.started)
NoOutageDailyBlock ==
    ~(s.phase = "done" /\ s.admission = "outage" /\ s.dailyValidation = "failed" /\ ~s.started)
NoEarlyTimeout ==
    ~(s.started /\ s.admission = "outage" /\ s.timedOut /\ s.attempts < MaxAttempts)
NoFailedAdmissionStepBlock ==
    ~(s.phase = "done" /\ s.admission = "admitted" /\ s.admissionStepOutcome = "failure" /\ ~s.started)
=============================================================================
