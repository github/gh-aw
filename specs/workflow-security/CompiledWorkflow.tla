------------------------- MODULE CompiledWorkflow -------------------------
EXTENDS Naturals, FiniteSets, Sequences, TLC

CONSTANTS Fault, Profile, Effect, DetectionPolicy, DetectionEnabled, CheckoutMode, ArtifactMode
ASSUME Fault \in {"none", "agent-write", "persist-credentials",
                 "artifact-origin", "skip-detection", "cross-repo-token",
                 "app-scope", "secret-channel", "untrusted-execution",
                 "checkout-widen", "expired-token", "network-bypass",
                 "untrusted-config", "output-limit", "private-sink",
                 "target-authorization", "retained-push-token", "ambient-agent-fetch",
                 "cleanup-fail-open", "artifact-invocation", "policy-downgrade",
                 "failure-token-retention"}
ASSUME Profile \in {"sparse", "full"}
ASSUME Effect \in {"issue", "pull-request"}
ASSUME DetectionPolicy \in {"required", "disabled", "conditional"}
ASSUME DetectionEnabled \in BOOLEAN
ASSUME CheckoutMode \in {"transient", "force-clean"}
ASSUME ArtifactMode \in {"plain", "reusable"}
DetectionRuns == DetectionPolicy = "required" \/
                 (DetectionPolicy = "conditional" /\ DetectionEnabled)
ActualDetectionRuns == DetectionRuns /\ Fault # "policy-downgrade"

Jobs == {"activation", "agent", "detection", "safe_outputs"}
Repos == {"main", "private_dependency"}
Channels == {"artifact", "output", "log"}
States == {"queued", "running", "success", "failure", "skipped"}
Permissions == {"contents-read", "contents-write", "issues-write", "inference",
                "administration"}
Secrets == {"app-key", "checkout-token", "write-token", "engine-token"}
Tokens == {"checkout-main", "checkout-dependency", "app-write", "engine"}
Artifacts == {"context", "request", "verdict"}
None == "none"
Run == "current-run"
Invocation == "current-invocation"
ArtifactPrefix == IF ArtifactMode = "reusable" THEN "invocation-prefix-" ELSE ""
Dependencies(j) ==
    CASE j = "activation" -> {}
      [] j = "agent" -> {"activation"}
      [] j = "detection" -> {"agent"}
      [] OTHER -> IF ActualDetectionRuns THEN {"agent", "detection"} ELSE {"agent"}
StepCount(j) ==
    CASE j = "activation" -> 1
      [] j = "agent" -> 3
      [] j = "detection" -> 2
      [] OTHER -> 5
EffectPermission == IF Effect = "issue" THEN "issues-write" ELSE "contents-write"
Grants(j) ==
    CASE j = "activation" -> {"contents-read"}
      [] j = "agent" -> {"contents-read", "inference"}
      [] j = "detection" -> {"contents-read"}
      [] OTHER -> {"contents-read", EffectPermission}
TokenRepo(t) ==
    CASE t = "checkout-dependency" -> "private_dependency"
      [] t = "engine" -> None
      [] OTHER -> "main"
TokenPermissions(t) ==
    CASE t = "app-write" -> {"contents-read", EffectPermission}
      [] t = "engine" -> {"inference"}
      [] OTHER -> {"contents-read"}
TokenFor(r) == IF r = "main" THEN "checkout-main" ELSE "checkout-dependency"

EmptyArtifact == [exists |-> FALSE, producer |-> None, run |-> None,
                  invocation |-> None, prefix |-> None,
                  secret |-> FALSE, private |-> FALSE, trusted |-> FALSE]
EmptyCheckout == [exists |-> FALSE, credentials |-> None,
                  sparse |-> FALSE, shallow |-> FALSE,
                  blobs |-> {}, refs |-> {}]
Artifact(j, trusted) == [exists |-> TRUE, producer |-> j, run |-> Run,
                        invocation |-> Invocation, prefix |-> ArtifactPrefix,
                        secret |-> FALSE, private |-> FALSE, trusted |-> trusted]
Transfer(j, channel, secret) ==
    [job |-> j, channel |-> channel, secret |-> secret]
Operation(j, op, r, t) == [job |-> j, op |-> op, repo |-> r, token |-> t]
Write(j, r, t, validated, detected, execution, private) ==
    [job |-> j, repo |-> r, token |-> t, validated |-> validated,
     detected |-> detected, execution |-> execution, private |-> private]

VARIABLE s
vars == <<s>>
DependencySatisfied(j, d) ==
    IF d = "agent" /\ j \in {"detection", "safe_outputs"}
    THEN s.status[d] \in {"success", "failure"}
    ELSE s.status[d] = "success"

Init ==
    s = [status |-> [j \in Jobs |-> IF j = "detection" /\ ~ActualDetectionRuns
                                  THEN "skipped" ELSE "queued"],
         step |-> [j \in Jobs |-> 1],
         started |-> {},
         grants |-> [j \in Jobs |-> Grants(j)],
         live |-> {}, revoked |-> {},
         agentSecrets |-> {},
         checkouts |-> [r \in Repos |-> EmptyCheckout],
         cleanup |-> [r \in Repos |-> "not-required"],
         agentBegan |-> FALSE,
         privilegedCheckout |-> EmptyCheckout,
         artifacts |-> [a \in Artifacts |-> EmptyArtifact],
         transfers |-> {}, operations |-> {}, writes |-> {},
         errors |-> {}, gitDone |-> FALSE, networkDone |-> FALSE,
         egress |-> {}, effects |-> 0,
         requestValid |-> FALSE, target |-> None,
         detection |-> "pending", approved |-> FALSE,
         appScope |-> {}, appRepos |-> {},
         lastEvent |-> "Init"]

At(j, n) == s.status[j] = "running" /\ s.step[j] = n
GoodOrigin(a, j) ==
    s.artifacts[a].exists /\ s.artifacts[a].producer = j
    /\ s.artifacts[a].run = Run
    /\ s.artifacts[a].invocation = Invocation
    /\ s.artifacts[a].prefix = ArtifactPrefix
    /\ (j = "agent" \/ s.artifacts[a].trusted)

Start(j) ==
    /\ s.status[j] = "queued"
    /\ \A d \in Dependencies(j): DependencySatisfied(j, d)
    /\ s' = [s EXCEPT
        !.status[j] = "running",
        !.started = @ \cup {j},
        !.grants[j] = IF j = "agent" /\ Fault = "agent-write"
                     THEN @ \cup {"contents-write"} ELSE @,
        !.lastEvent = "Start:" \o j]

Skip(j) ==
    /\ s.status[j] = "queued"
    /\ \E d \in Dependencies(j):
        s.status[d] \in {"failure", "skipped"} /\ ~DependencySatisfied(j, d)
    /\ s' = [s EXCEPT !.status[j] = "skipped", !.lastEvent = "Skip:" \o j]

Finish(j) ==
    /\ At(j, StepCount(j) + 1)
    /\ s' = [s EXCEPT !.status[j] = "success", !.lastEvent = "Finish:" \o j]

Activate ==
    /\ At("activation", 1)
    /\ s' = [s EXCEPT
        !.artifacts["context"] = Artifact("activation", TRUE),
        !.artifacts["context"].trusted = Fault # "untrusted-config",
        !.transfers = @ \cup {Transfer("activation", "artifact", FALSE)},
        !.step["activation"] = 2, !.lastEvent = "Activate"]

Checkout(r) ==
    /\ At("agent", 1)
    /\ ~s.checkouts[r].exists
    /\ LET limited == Profile = "sparse" /\ r = "main"
           t == IF Fault = "cross-repo-token" THEN "checkout-main"
                ELSE TokenFor(r)
       IN s' = [s EXCEPT
           !.checkouts[r] =
               [exists |-> TRUE,
                credentials |-> IF Fault = "persist-credentials" \/ CheckoutMode = "force-clean"
                                THEN t ELSE None,
                sparse |-> limited, shallow |-> limited,
                blobs |-> IF limited THEN {"README.md"}
                          ELSE {"README.md", "src/code.go"},
                refs |-> IF limited THEN {"HEAD"} ELSE {"HEAD", "base"}],
           !.cleanup[r] = IF CheckoutMode = "force-clean" THEN "pending" ELSE "not-required",
           !.operations = @ \cup {Operation("agent", "checkout", r, t)},
           !.lastEvent = "Checkout:" \o r]

CleanCheckout(r, success) ==
    /\ At("agent", 1) /\ s.cleanup[r] = "pending"
    /\ s' = [s EXCEPT
        !.cleanup[r] = IF success THEN "passed" ELSE "failed",
        !.checkouts[r].credentials = IF success THEN None ELSE @,
        !.status["agent"] = IF success \/ Fault = "cleanup-fail-open"
                           THEN @ ELSE "failure",
        !.errors = IF success THEN @ ELSE @ \cup {"credential-cleanup-failed"},
        !.lastEvent = IF success THEN "CleanCheckout:" \o r
                      ELSE "CleanupFailure:" \o r]

CheckoutComplete ==
    /\ At("agent", 1)
    /\ \A r \in Repos: s.checkouts[r].exists
    /\ \A r \in Repos:
        s.checkouts[r].credentials = None \/ Fault = "persist-credentials" \/
        (Fault = "cleanup-fail-open" /\ s.cleanup[r] = "failed")
    /\ GoodOrigin("context", "activation")
    /\ s' = [s EXCEPT
        !.live = @ \cup {"engine"},
        !.agentBegan = TRUE,
        !.agentSecrets = @ \cup {"engine-token"},
        !.step["agent"] = 2, !.lastEvent = "CheckoutComplete"]

GitOps == {"diff", "show-base", "read-blob", "fetch", "push", "gh-read"}
Remote(op) == op \in {"checkout", "fetch", "push", "gh-read"}
Required(op) == IF op = "push" THEN "contents-write" ELSE "contents-read"
LocalAvailable(op, r) ==
    CASE op = "diff" -> TRUE
      [] op = "show-base" -> "base" \in s.checkouts[r].refs
      [] op = "read-blob" -> "src/code.go" \in s.checkouts[r].blobs
      [] OTHER -> FALSE

GitOperation(op, r) ==
    /\ At("agent", 2)
    /\ ~s.gitDone
    /\ LET available == LocalAvailable(op, r)
           \* gh is REST with command-env auth; it does not inherit git config.
           t == IF op = "gh-read" /\ r = "main" THEN "checkout-main"
                ELSE IF Fault = "ambient-agent-fetch" /\ op = "fetch"
                     THEN TokenFor(r)
                ELSE None
           widening == Fault = "checkout-widen" /\ op = "read-blob"
                       /\ ~available
       IN s' = [s EXCEPT
           !.gitDone = TRUE,
           !.operations = IF available \/ t # None
                          THEN @ \cup {Operation("agent", op, r, t)}
                          ELSE IF widening
                               THEN @ \cup {Operation("agent", "fetch", r, None)}
                               ELSE @,
           !.errors = IF ~available /\ t = None /\ ~widening
                      THEN @ \cup {"git-unavailable:" \o op \o ":" \o r}
                      ELSE @,
           !.lastEvent = "Git:" \o op \o ":" \o r]

ToolCall(destination) ==
    /\ At("agent", 2) /\ ~s.networkDone
    /\ s' = [s EXCEPT
        !.networkDone = TRUE,
        !.egress = IF destination \in {"inference", "github"} \/ Fault = "network-bypass"
                   THEN @ \cup {[destination |-> destination,
                                 credential |-> IF destination = "inference"
                                                THEN "engine-token" ELSE None]}
                   ELSE @,
        !.errors = IF destination = "blocked" /\ Fault # "network-bypass"
                   THEN @ \cup {"network-denied"} ELSE @,
        !.lastEvent = "ToolCall:" \o destination]

AgentRequest(valid, target, channel, private) ==
    /\ At("agent", 2) /\ s.gitDone /\ s.networkDone
    /\ s' = [s EXCEPT
        !.requestValid = valid, !.target = target,
        !.artifacts["request"] = Artifact("agent", FALSE),
        !.artifacts["request"].private = private,
        !.transfers = @ \cup {Transfer("agent", channel, FALSE)},
        !.step["agent"] = 3, !.lastEvent = "AgentRequest"]

Publish ==
    /\ At("agent", 3)
    /\ s' = [s EXCEPT
        !.transfers = @ \cup {Transfer("agent", "artifact", FALSE),
                             Transfer("agent", "output", FALSE),
                             Transfer("agent", "log", FALSE)},
        !.live = @ \ {"engine"}, !.revoked = @ \cup {"engine"},
        !.step["agent"] = 4, !.lastEvent = "Publish"]

TamperOrigin ==
    /\ Fault \in {"artifact-origin", "artifact-invocation"}
    /\ At("detection", 1)
    /\ s.artifacts["request"].run = Run /\ s.artifacts["request"].invocation = Invocation
    /\ s' = [s EXCEPT
        !.artifacts["request"].run = IF Fault = "artifact-origin" THEN "other-run" ELSE @,
        !.artifacts["request"].invocation =
            IF Fault = "artifact-invocation" THEN "other-invocation" ELSE @,
        !.lastEvent = "TamperOrigin"]

Consume ==
    /\ At("detection", 1)
    /\ IF GoodOrigin("request", "agent") \/
          (Fault \in {"artifact-origin", "artifact-invocation"} /\ s.artifacts["request"].exists)
       THEN s' = [s EXCEPT
           !.step["detection"] = 2, !.lastEvent = "Consume"]
       ELSE s' = [s EXCEPT
           !.status["detection"] = "failure",
           !.errors = @ \cup {"artifact-origin"}, !.lastEvent = "RejectOrigin"]

Detect(result) ==
    /\ At("detection", 2)
    /\ s' = [s EXCEPT
        !.detection = result,
        !.artifacts["verdict"] = Artifact("detection", TRUE),
        !.step["detection"] = 3, !.lastEvent = "Detect:" \o result]

Validate ==
    /\ At("safe_outputs", 1)
    /\ LET accepted == s.requestValid
                       /\ (s.target = "main" \/ Fault = "target-authorization")
                       /\ GoodOrigin("request", "agent")
                       /\ (~ActualDetectionRuns \/ GoodOrigin("verdict", "detection"))
                       /\ (~ActualDetectionRuns \/ s.detection = "pass" \/ Fault = "skip-detection")
                       /\ (~s.artifacts["request"].private \/ Fault = "private-sink")
       IN s' = [s EXCEPT
           !.approved = accepted,
           !.errors = IF accepted THEN @ ELSE @ \cup {"request-rejected"},
           !.step["safe_outputs"] = 2, !.lastEvent = "Validate"]

MintAppToken ==
    /\ At("safe_outputs", 2)
    /\ s' = [s EXCEPT
        !.live = IF s.approved THEN @ \cup {"app-write"} ELSE @,
        !.appScope = IF s.approved
                     THEN IF Fault = "app-scope"
                          THEN {EffectPermission, "administration"}
                          ELSE {EffectPermission}
                     ELSE {},
        !.appRepos = IF s.approved THEN {"main"} ELSE {},
        !.step["safe_outputs"] = 3, !.lastEvent = "MintAppToken"]

PrepareEffect ==
    /\ At("safe_outputs", 3)
    /\ s' = [s EXCEPT
        !.privilegedCheckout =
            IF s.approved /\ Effect = "pull-request"
            THEN [exists |-> TRUE, credentials |-> "app-write",
                  sparse |-> FALSE, shallow |-> FALSE,
                  blobs |-> {"README.md", "src/code.go"}, refs |-> {"HEAD", "base"}]
            ELSE @,
        !.operations = IF s.approved /\ Effect = "pull-request"
                       THEN @ \cup {Operation("safe_outputs", "checkout", "main", "app-write")}
                       ELSE @,
        !.step["safe_outputs"] = 4, !.lastEvent = "PrepareEffect:" \o Effect]

Execute ==
    /\ At("safe_outputs", 4)
    /\ s' = [s EXCEPT
        !.writes = IF s.approved
                   THEN @ \cup {Write("safe_outputs", s.target, "app-write",
                                      s.requestValid, s.detection = "pass",
                                      IF Fault = "untrusted-execution"
                                      THEN "agent-script" ELSE "trusted-handler",
                                      s.artifacts["request"].private)}
                   ELSE @,
        !.operations = IF s.approved /\ Effect = "pull-request"
                       THEN @ \cup {Operation("safe_outputs", "push", "main", "app-write")}
                       ELSE @,
        !.effects = IF s.approved THEN @ + 1 ELSE @,
        !.step["safe_outputs"] = 5, !.lastEvent = "Execute"]

Revoke ==
    /\ At("safe_outputs", 5)
    /\ s' = [s EXCEPT
        !.live = @ \ {"app-write"}, !.revoked = @ \cup {"app-write"},
        !.privilegedCheckout.credentials = IF Fault = "retained-push-token" THEN @ ELSE None,
        !.step["safe_outputs"] = 6, !.lastEvent = "Revoke"]

Leak(channel) ==
    /\ Fault = "secret-channel"
    /\ At("safe_outputs", 4) /\ "app-write" \in s.live
    /\ Transfer("safe_outputs", channel, TRUE) \notin s.transfers
    /\ s' = [s EXCEPT
        !.transfers = @ \cup {Transfer("safe_outputs", channel, TRUE)},
        !.agentSecrets = @ \cup {"write-token"}, !.lastEvent = "Leak:" \o channel]

UseExpired ==
    /\ Fault = "expired-token"
    /\ s.status["safe_outputs"] = "success" /\ s.approved
    /\ s' = [s EXCEPT !.live = @ \cup {"app-write"}, !.lastEvent = "UseExpired"]

ExceedLimit ==
    /\ Fault = "output-limit"
    /\ s.status["safe_outputs"] = "success" /\ s.approved /\ s.effects = 1
    /\ s' = [s EXCEPT !.effects = 2, !.lastEvent = "ExceedLimit"]

Fail(j) ==
    /\ s.status[j] = "running"
    /\ LET owned == IF j = "agent" THEN {"engine"}
                    ELSE IF j = "safe_outputs" THEN {"app-write"} ELSE {}
       IN s' = [s EXCEPT
           !.status[j] = "failure",
           !.live = IF Fault = "failure-token-retention" /\ j = "safe_outputs"
                   THEN @ ELSE @ \ owned,
           !.privilegedCheckout.credentials =
               IF j = "safe_outputs" THEN None ELSE @,
           !.revoked = IF Fault = "failure-token-retention" /\ j = "safe_outputs"
                      THEN @ ELSE @ \cup owned,
           !.lastEvent = "Fail:" \o j]

Next ==
    \/ \E j \in Jobs: Start(j) \/ Skip(j) \/ Finish(j) \/ Fail(j)
    \/ Activate
    \/ \E r \in Repos: Checkout(r)
    \/ \E r \in Repos, success \in BOOLEAN: CleanCheckout(r, success)
    \/ CheckoutComplete
    \/ \E op \in GitOps, r \in Repos: GitOperation(op, r)
    \/ \E destination \in {"inference", "github", "blocked"}: ToolCall(destination)
    \/ \E valid \in BOOLEAN, target \in Repos, channel \in Channels, private \in BOOLEAN:
           AgentRequest(valid, target, channel, private)
    \/ Publish \/ TamperOrigin \/ Consume
    \/ \E result \in {"pass", "deny"}: Detect(result)
    \/ Validate \/ MintAppToken \/ PrepareEffect \/ Execute \/ Revoke
    \/ \E channel \in Channels: Leak(channel)
    \/ UseExpired \/ ExceedLimit

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.status \in [Jobs -> States]
    /\ s.step \in [Jobs -> 1..6]
    /\ s.started \subseteq Jobs
    /\ s.grants \in [Jobs -> SUBSET Permissions]
    /\ s.live \subseteq Tokens /\ s.revoked \subseteq Tokens
    /\ s.agentSecrets \subseteq Secrets
    /\ s.checkouts \in [Repos ->
        [exists: BOOLEAN, credentials: Tokens \cup {None},
         sparse: BOOLEAN, shallow: BOOLEAN,
         blobs: SUBSET {"README.md", "src/code.go"}, refs: SUBSET {"HEAD", "base"}]]
    /\ s.cleanup \in [Repos -> {"not-required", "pending", "passed", "failed"}]
    /\ s.agentBegan \in BOOLEAN
    /\ s.privilegedCheckout \in
        [exists: BOOLEAN, credentials: Tokens \cup {None},
         sparse: BOOLEAN, shallow: BOOLEAN,
         blobs: SUBSET {"README.md", "src/code.go"}, refs: SUBSET {"HEAD", "base"}]
    /\ s.artifacts \in [Artifacts ->
        [exists: BOOLEAN, producer: Jobs \cup {None},
         run: {Run, None, "other-run"},
         invocation: {Invocation, None, "other-invocation"},
         prefix: {None, "", "invocation-prefix-"}, secret: BOOLEAN, private: BOOLEAN,
         trusted: BOOLEAN]]
    /\ s.transfers \subseteq [job: Jobs, channel: Channels, secret: BOOLEAN]
    /\ s.operations \subseteq [job: Jobs, op: GitOps \cup {"checkout"},
                                repo: Repos, token: Tokens \cup {None}]
    /\ s.writes \subseteq [job: Jobs, repo: Repos, token: Tokens,
        validated: BOOLEAN, detected: BOOLEAN,
        execution: {"trusted-handler", "agent-script"}, private: BOOLEAN]
    /\ s.errors \subseteq {"artifact-origin", "request-rejected", "network-denied",
                           "credential-cleanup-failed"} \cup
        {"git-unavailable:" \o op \o ":" \o r : op \in GitOps, r \in Repos}
    /\ s.gitDone \in BOOLEAN /\ s.networkDone \in BOOLEAN
    /\ s.requestValid \in BOOLEAN /\ s.effects \in 0..2
    /\ s.egress \subseteq [destination: {"inference", "github", "blocked"},
                          credential: {None, "engine-token"}]
    /\ s.target \in Repos \cup {None}
    /\ s.detection \in {"pending", "pass", "deny"} /\ s.approved \in BOOLEAN
    /\ s.appScope \subseteq Permissions /\ s.appRepos \subseteq Repos

JobIsolation ==
    /\ s.grants["agent"] \cap {"contents-write", "issues-write"} = {}
    /\ \A j \in s.started: \A d \in Dependencies(j): DependencySatisfied(j, d)
NoCredentialPersistence ==
    s.agentBegan => \A r \in Repos: s.checkouts[r].credentials = None
ArtifactProvenance ==
    /\ (s.step["detection"] > 1 => GoodOrigin("request", "agent"))
    /\ (s.approved => GoodOrigin("request", "agent"))
DetectionGate ==
    \A w \in s.writes: ~DetectionRuns \/ w.detected
ValidatedEffects ==
    \A w \in s.writes:
        w.job = "safe_outputs" /\ w.repo = "main" /\ w.validated
        /\ w.token = "app-write"
AppLeastPrivilege ==
    s.appScope \subseteq Grants("safe_outputs") /\ s.appRepos \subseteq {"main"}
PrivilegedCheckoutIsolation ==
    s.privilegedCheckout.credentials # None =>
        /\ s.status["safe_outputs"] = "running"
        /\ s.privilegedCheckout.credentials = "app-write"
        /\ "app-write" \in s.live
SecretConfinement ==
    /\ s.agentSecrets \subseteq {"engine-token"}
    /\ \A t \in s.transfers: ~t.secret
    /\ \A a \in Artifacts: ~s.artifacts[a].secret
TrustedExecution ==
    \A w \in s.writes: w.execution = "trusted-handler"
GitAuthorization ==
    \A op \in s.operations:
        Remote(op.op) =>
            /\ op.token # None
            /\ TokenRepo(op.token) = op.repo
            /\ Required(op.op) \in TokenPermissions(op.token)
NoImplicitFetch ==
    \A op \in s.operations: op.job = "agent" => op.op \notin {"fetch", "push"}
TokenLifetime ==
    /\ s.live \cap s.revoked = {}
    /\ (s.status["agent"] \in {"success", "failure"} => "engine" \notin s.live)
    /\ (s.status["safe_outputs"] \in {"success", "failure"} =>
        "app-write" \notin s.live)
NetworkPolicy ==
    \A e \in s.egress:
        e.destination \in {"inference", "github"}
        /\ (e.credential = "engine-token" => e.destination = "inference")
TrustedConfiguration ==
    s.artifacts["context"].exists => s.artifacts["context"].trusted
OutputLimit == s.effects <= 1
PrivateSinkPolicy == \A w \in s.writes: ~w.private

Safety == TypeOK /\ JobIsolation /\ NoCredentialPersistence
          /\ ArtifactProvenance /\ DetectionGate /\ ValidatedEffects
          /\ AppLeastPrivilege /\ PrivilegedCheckoutIsolation
          /\ SecretConfinement /\ TrustedExecution
          /\ GitAuthorization /\ NoImplicitFetch /\ TokenLifetime
          /\ NetworkPolicy /\ TrustedConfiguration /\ OutputLimit /\ PrivateSinkPolicy

\* Deliberately false reachability predicates, not security requirements.
NoSuccessfulWrite == s.writes = {}
NoDeniedRequest == "request-rejected" \notin s.errors
NoMissingGitData ==
    \A op \in GitOps, r \in Repos:
        "git-unavailable:" \o op \o ":" \o r \notin s.errors
NoCrossRepoCheckout == ~s.checkouts["private_dependency"].exists
NoFailedJob == \A j \in Jobs: s.status[j] # "failure"
NoCleanupFailure == "credential-cleanup-failed" \notin s.errors
NoTemporaryCredentials == \A r \in Repos: s.checkouts[r].credentials = None
=============================================================================
