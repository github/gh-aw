------------------------ MODULE ConnectorInference ------------------------
EXTENDS Naturals, FiniteSets

\* Informative bounded compiler/engine-host composition, not connector tools.
\* Mutations are insecure negative controls, never implementation options.
CONSTANTS Mutation, Probe

Profiles == {"responses", "completions", "unknown"}
Models == {"approved", "ungranted"}
Routes == {"key", "azure-oidc"}
Phases == {"inference", "agent", "safe-output"}
InferenceCredentials == {"gateway-key", "gateway-entra"}
Credential(route) == IF route = "key" THEN "gateway-key" ELSE "gateway-entra"
Protocol(profile) == IF profile = "responses" THEN "responses" ELSE "completions"
ToolCredentials(phase) ==
  CASE phase = "agent" -> {"reader"}
    [] phase = "safe-output" -> {"writer"}
    [] OTHER -> {}
EmptyBinding == [endpoint |-> "none", protocol |-> "none",
                 model |-> "none", credential |-> "none"]
BindingFor(input) ==
  [endpoint |-> "gateway-origin/fixed-sdk-base",
   protocol |-> Protocol(input.profile), model |-> input.model,
   credential |-> Credential(input.route)]
Inputs == [selected : BOOLEAN, profile : Profiles, model : Models,
           networkApproved : BOOLEAN, engineCompatible : BOOLEAN,
           conflict : BOOLEAN, oidcReady : BOOLEAN, route : Routes,
           phase : Phases, staged : BOOLEAN]

VARIABLES pc, input, binding, toolCredentials, resolutions, exchanges,
          credentialsUsed, requests, sent, denial, outcome

vars == <<pc, input, binding, toolCredentials, resolutions, exchanges,
          credentialsUsed, requests, sent, denial, outcome>>

KnownProfile == input.profile # "unknown"
ExactGrant == input.model = "approved"
OIDCReady == input.route # "azure-oidc" \/ input.oidcReady
Approved ==
  input.selected /\ KnownProfile /\ ExactGrant /\ input.networkApproved /\
  input.engineCompatible /\ ~input.conflict /\ OIDCReady

Init ==
  /\ pc = "compile" /\ input \in Inputs
  /\ binding = EmptyBinding /\ toolCredentials = {}
  /\ resolutions = 0 /\ exchanges = 0 /\ credentialsUsed = {}
  /\ requests = 0 /\ sent = EmptyBinding
  /\ denial = "none" /\ outcome = "pending"

Compile ==
  /\ pc = "compile"
  /\ LET permitted ==
           (input.selected \/ Mutation = "implicit-selection") /\
           (KnownProfile \/ Mutation = "unknown-profile") /\
           (ExactGrant \/ Mutation = "model-grant") /\
           (input.networkApproved \/ Mutation = "network") /\
           input.engineCompatible /\
           (~input.conflict \/ Mutation = "override") /\
           (OIDCReady \/ Mutation = "oidc-readiness")
     IN /\ binding' = IF permitted THEN BindingFor(input) ELSE EmptyBinding
        /\ outcome' = IF permitted THEN "pending"
                       ELSE IF input.selected THEN "rejected" ELSE "import-only"
        /\ pc' = IF permitted THEN "resolve" ELSE "done"
  /\ toolCredentials' =
       ToolCredentials(input.phase) \cup
       (IF Mutation = "phase-pruning" THEN {Credential(input.route)} ELSE {})
  /\ UNCHANGED <<input, resolutions, exchanges, credentialsUsed, requests,
                  sent, denial>>

Resolve ==
  /\ pc = "resolve"
  /\ IF input.phase = "inference" \/ Mutation = "tool-execution"
     THEN
       /\ resolutions' = 1
       /\ exchanges' = IF input.route = "azure-oidc" THEN 1 ELSE 0
       /\ \E authorized \in BOOLEAN :
            /\ denial' = IF input.route = "azure-oidc" /\ ~authorized
                          THEN "exchange" ELSE "none"
            /\ credentialsUsed' = IF denial' = "exchange" THEN {}
                                   ELSE {binding.credential}
            /\ pc' = IF denial' = "exchange" THEN "done" ELSE "send"
            /\ outcome' = IF denial' = "exchange" THEN "rejected" ELSE "pending"
     ELSE
       /\ pc' = "done" /\ outcome' = "not-callable"
       /\ UNCHANGED <<resolutions, exchanges, credentialsUsed, denial>>
  /\ UNCHANGED <<input, binding, toolCredentials, requests, sent>>

Send ==
  /\ pc = "send"
  /\ sent' = IF Mutation = "wire-binding"
               THEN [binding EXCEPT !.model = "ungranted"]
               ELSE binding
  /\ requests' = 1 /\ pc' = "respond"
  /\ UNCHANGED <<input, binding, toolCredentials, resolutions, exchanges,
                  credentialsUsed, denial, outcome>>

Respond ==
  /\ pc = "respond"
  /\ \E status \in {"ok", "unauthorized", "uncertain"} :
       /\ denial' = IF status = "unauthorized" THEN "provider" ELSE "none"
       /\ outcome' = IF status = "ok" THEN "inferred" ELSE "rejected"
       /\ resolutions' = IF denial' = "provider" /\ Mutation = "auth-retry"
                           THEN 2 ELSE resolutions
       /\ requests' = IF denial' = "provider" /\ Mutation = "auth-retry"
                       THEN 2 ELSE requests
       /\ credentialsUsed' =
            IF denial' = "provider" /\ Mutation = "auth-retry"
              THEN InferenceCredentials ELSE credentialsUsed
  /\ pc' = "done"
  /\ UNCHANGED <<input, binding, toolCredentials, exchanges, sent>>

Done == pc = "done" /\ UNCHANGED vars
Next == Compile \/ Resolve \/ Send \/ Respond \/ Done
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ pc \in {"compile", "resolve", "send", "respond", "done"}
  /\ input \in Inputs
  /\ binding \in {EmptyBinding, BindingFor(input)}
  /\ toolCredentials \subseteq {"reader", "writer"} \cup InferenceCredentials
  /\ resolutions \in 0..2 /\ exchanges \in 0..1 /\ requests \in 0..2
  /\ credentialsUsed \subseteq InferenceCredentials
  /\ sent \in {EmptyBinding, binding, [binding EXCEPT !.model = "ungranted"]}
  /\ denial \in {"none", "exchange", "provider"}
  /\ outcome \in {"pending", "rejected", "import-only", "not-callable", "inferred"}
ImportOnly ==
  ~input.selected => binding = EmptyBinding /\ resolutions = 0 /\
                     exchanges = 0 /\ requests = 0
ExplicitProfile == binding # EmptyBinding => input.selected /\ KnownProfile
ModelGrant == binding # EmptyBinding => ExactGrant
NetworkApproval == binding # EmptyBinding => input.networkApproved
NoOverrides == binding # EmptyBinding => input.engineCompatible /\ ~input.conflict
OIDCPrerequisites == binding # EmptyBinding => OIDCReady
PhasePruning ==
  pc # "compile" => toolCredentials = ToolCredentials(input.phase)
EngineOnly == resolutions > 0 => input.phase = "inference" /\ Approved
CredentialBinding == credentialsUsed \subseteq {Credential(input.route)}
WireBinding == requests > 0 => sent = BindingFor(input)
FailClosed ==
  denial # "none" =>
    /\ outcome = "rejected" /\ resolutions = 1 /\ requests <= 1
    /\ (denial = "exchange" => requests = 0 /\ credentialsUsed = {})
Safety ==
  TypeOK /\ ImportOnly /\ ExplicitProfile /\ ModelGrant /\ NetworkApproval /\
  NoOverrides /\ OIDCPrerequisites /\ PhasePruning /\ EngineOnly /\
  CredentialBinding /\ WireBinding /\ FailClosed

\* False invariants give existential witnesses, not eventual-success claims.
ProbeAbsent ==
  CASE Probe = "responses-key" ->
         ~(outcome = "inferred" /\ input.profile = "responses" /\ input.route = "key")
    [] Probe = "completions-oidc" ->
         ~(outcome = "inferred" /\ input.profile = "completions" /\ input.route = "azure-oidc")
    [] Probe = "import-only" -> outcome # "import-only"
    [] Probe = "exchange-denied" -> denial # "exchange"
    [] Probe = "provider-denied" -> denial # "provider"
    [] Probe = "staged-inference" -> ~(outcome = "inferred" /\ input.staged)
    [] OTHER -> TRUE
=============================================================================
