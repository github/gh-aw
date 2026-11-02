------------------------ MODULE SafeOutputGeneration ------------------------
EXTENDS Naturals, Sequences

CONSTANTS Fault, Transport, Mode, AllowFullHistoryFetch
ASSUME Transport \in {"patch", "bundle"}
ASSUME Mode \in {"full", "incremental"}
ASSUME AllowFullHistoryFetch \in BOOLEAN
ASSUME Fault \in {"none", "explicit-fetch", "lazy-fetch", "config-override",
                  "caller-override", "token-forwarding", "implicit-recovery",
                  "incremental-fallback", "generation-fallback",
                  "config-mutation", "environment-mutation", "hidden-missing"}

\* Generation in an agent-writable checkout, not privileged bundle application.
\* Local inputs abstract existing commit refs and required object availability.
\* Rejection may queue offline object packaging, modeled separately by
\* SafeOutputObjectRecovery; it never grants this generator network authority.
VARIABLE s
vars == <<s>>
Stages == {"resolve", "objects", "generate", "done"}

Init ==
    \E baseline, fallback, objects, ambientToken, explicitToken \in BOOLEAN:
        s = [stage |-> "resolve", baseline |-> baseline, fallback |-> fallback,
             objects |-> objects, ambientToken |-> ambientToken,
             explicitToken |-> explicitToken, hostileConfig |-> FALSE,
             hostileCaller |-> FALSE, transportAttempted |-> FALSE,
             transportEnabled |-> FALSE, helperExecuted |-> FALSE,
             headerInjected |-> FALSE, tokenForwarded |-> FALSE,
             configChanged |-> FALSE, environmentChanged |-> FALSE,
             widened |-> FALSE, errorGuidance |-> FALSE,
             rejected |-> FALSE, artifact |-> FALSE, base |-> "none",
             lastEvent |-> "Init"]

AgentChangesConfig ==
    /\ s.stage # "done" /\ ~s.hostileConfig
    /\ s' = [s EXCEPT !.hostileConfig = TRUE,
                      !.lastEvent = "AgentChangesConfig"]

CallerRelaxesGuard ==
    /\ s.stage # "done" /\ ~s.hostileCaller
    /\ s' = [s EXCEPT !.hostileCaller = TRUE,
                      !.lastEvent = "CallerRelaxesGuard"]

Resolve ==
    /\ s.stage = "resolve"
    /\ LET local == s.baseline \/ (Mode = "full" /\ s.fallback)
           widen == ~s.baseline /\ Mode = "incremental" /\ s.fallback
                    /\ Fault = "incremental-fallback"
           fetch == ~local /\ (Fault = "explicit-fetch" \/
                      (Fault = "implicit-recovery" /\ AllowFullHistoryFetch))
           accept == local \/ widen
       IN s' = [s EXCEPT
           !.stage = IF accept THEN "objects" ELSE "done",
           !.base = IF s.baseline THEN "baseline"
                    ELSE IF accept THEN "fallback" ELSE "none",
           !.widened = widen, !.transportAttempted = fetch,
           !.transportEnabled = fetch,
           !.helperExecuted = fetch /\ s.hostileConfig,
           !.tokenForwarded = Fault = "token-forwarding" /\ s.explicitToken,
           !.headerInjected = Fault = "token-forwarding" /\
                              (s.ambientToken \/ s.explicitToken),
           !.configChanged = Fault = "config-mutation",
           !.environmentChanged = Fault = "environment-mutation",
           !.rejected = ~accept,
           !.errorGuidance = ~accept /\ Fault # "hidden-missing",
           !.lastEvent = "Resolve"]

CheckObjects ==
    /\ s.stage = "objects"
    /\ LET enabled == ~s.objects /\ (Fault = "lazy-fetch" \/
                        (Fault = "config-override" /\ s.hostileConfig) \/
                        (Fault = "caller-override" /\ s.hostileCaller))
       IN s' = [s EXCEPT
           !.stage = IF s.objects THEN "generate" ELSE "done",
           !.transportAttempted = @ \/ enabled,
           !.transportEnabled = @ \/ enabled,
           !.helperExecuted = @ \/ (enabled /\ s.hostileConfig),
           !.rejected = ~s.objects,
           !.errorGuidance = ~s.objects /\ Fault # "hidden-missing",
           !.lastEvent = "CheckObjects"]

Generate(success) ==
    /\ s.stage = "generate"
    /\ LET substitute == ~success /\ Fault = "generation-fallback"
       IN s' = [s EXCEPT !.stage = "done", !.artifact = success \/ substitute,
                        !.widened = @ \/ substitute,
                        !.rejected = ~success /\ ~substitute,
                        !.errorGuidance = ~success /\ ~substitute,
                        !.lastEvent = IF success THEN "Generate" ELSE "GenerationFailure"]

Next ==
    \/ AgentChangesConfig \/ CallerRelaxesGuard \/ Resolve \/ CheckObjects
    \/ \E success \in BOOLEAN: Generate(success)
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.stage \in Stages /\ s.base \in {"none", "baseline", "fallback"}
    /\ \A field \in {"baseline", "fallback", "objects", "ambientToken",
                     "explicitToken", "hostileConfig", "hostileCaller",
                     "transportAttempted", "transportEnabled", "helperExecuted",
                     "headerInjected", "tokenForwarded", "configChanged",
                     "environmentChanged", "widened", "errorGuidance",
                     "rejected", "artifact"}: s[field] \in BOOLEAN

LocalGeneration == ~s.transportEnabled /\ ~s.helperExecuted
NoRecoveryAuthority == ~s.transportAttempted
NoGenerationAuthentication == ~s.headerInjected /\ ~s.tokenForwarded
IncrementalBaseline == Mode = "incremental" /\ s.base # "none" => s.base = "baseline"
StableGenerationRange == ~s.widened
ConfigurationPreserved == ~s.configChanged
EnvironmentPreserved == ~s.environmentChanged
MissingDataGuidance == s.rejected => s.errorGuidance
ArtifactPrerequisites ==
    s.artifact => s.objects /\ (s.baseline \/ (Mode = "full" /\ s.fallback))
RejectedNoArtifact == s.rejected => ~s.artifact

\* False predicates used only as guarded reachability witnesses.
NoLocalArtifact == ~s.artifact
NoFullFallback == ~(s.artifact /\ s.base = "fallback")
NoMissingBaselineRejection == ~(s.rejected /\ ~s.baseline /\ ~s.fallback)
NoMissingObjectRejection == ~(s.rejected /\ s.baseline /\ ~s.objects)
NoHostileConfigSuccess == ~(s.artifact /\ s.hostileConfig /\ s.hostileCaller)
NoGenerationFailure == ~(s.rejected /\ s.lastEvent = "GenerationFailure")
=============================================================================
