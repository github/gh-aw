-------------------------- MODULE CopilotSDKDriver --------------------------
EXTENDS Naturals

CONSTANT DenialLimit
ASSUME DenialLimit \in Nat \ {0}

VARIABLES validConfig, phase, opened, cleanupAttempted, dispatched,
          denials, mainRejected, outcome

vars == <<validConfig, phase, opened, cleanupAttempted, dispatched,
          denials, mainRejected, outcome>>
Resources == {"client", "session", "stream"}
Outcomes == {"ok", "error", "timeout", "rejection", "tool-denial"}

Init ==
  /\ validConfig \in BOOLEAN
  /\ phase = "configure"
  /\ opened = {}
  /\ cleanupAttempted = {}
  /\ dispatched = FALSE
  /\ denials = 0
  /\ mainRejected = FALSE
  /\ outcome = "pending"

Acquire(from, to, resource) ==
  /\ phase = from
  /\ validConfig
  /\ phase' = to
  /\ opened' = opened \cup {resource}
  /\ UNCHANGED <<validConfig, cleanupAttempted, dispatched,
                 denials, mainRejected, outcome>>

Dispatch ==
  /\ phase = "handlers"
  /\ phase' = "waiting"
  /\ dispatched' = TRUE
  /\ UNCHANGED <<validConfig, opened, cleanupAttempted,
                 denials, mainRejected, outcome>>

\* Inputs stand for classified callbacks, not parsed text or commands.
Observe(kind) ==
  /\ phase = "waiting"
  /\ denials < DenialLimit
  /\ ~(kind = "main-refusal" /\ mainRejected)
  /\ denials' = IF kind = "deny" THEN denials + 1 ELSE denials
  /\ mainRejected' = (mainRejected \/ kind = "main-refusal")
  /\ UNCHANGED <<validConfig, phase, opened, cleanupAttempted,
                 dispatched, outcome>>

Result(value) ==
  /\ phase' = "cleanup"
  /\ outcome' = value
  /\ UNCHANGED <<validConfig, opened, cleanupAttempted, dispatched,
                 denials, mainRejected>>

Finish(sdkResult) ==
  /\ phase = "waiting"
  /\ Result(IF mainRejected THEN "rejection"
            ELSE IF denials = DenialLimit THEN "tool-denial"
            ELSE IF sdkResult # "ok" /\ denials > 0 THEN "tool-denial"
            ELSE sdkResult)

Cleanup(resource) ==
  /\ phase = "cleanup"
  /\ resource \in opened \ cleanupAttempted
  /\ cleanupAttempted' = cleanupAttempted \cup {resource}
  /\ UNCHANGED <<validConfig, phase, opened, dispatched,
                 denials, mainRejected, outcome>>

Done ==
  /\ phase = "cleanup"
  /\ cleanupAttempted = opened
  /\ phase' = "done"
  /\ UNCHANGED <<validConfig, opened, cleanupAttempted, dispatched,
                 denials, mainRejected, outcome>>

Next ==
  \/ Acquire("configure", "connected", "client")
  \/ Acquire("connected", "session", "session")
  \/ Acquire("session", "handlers", "stream")
  \/ Dispatch
  \/ \E k \in {"deny", "allow", "main-refusal", "nested-refusal", "diagnostic"}:
       Observe(k)
  \/ \E r \in {"ok", "error", "timeout"}: Finish(r)
  \/ /\ phase \in {"configure", "connected", "session", "handlers"}
     /\ Result("error")
  \/ \E r \in Resources: Cleanup(r)
  \/ Done
  \/ phase = "done" /\ UNCHANGED vars

Spec == Init /\ [][Next]_vars

LifecycleOrder ==
  /\ (~validConfig => opened = {})
  /\ ("stream" \in opened => "session" \in opened)
  /\ ("session" \in opened => "client" \in opened)
  /\ (dispatched => opened = Resources)
  /\ (outcome = "ok" => dispatched)

FailureEvidence ==
  /\ denials \in 0..DenialLimit
  /\ outcome \in Outcomes \cup {"pending"}
  /\ (outcome = "tool-denial" => denials > 0)
  /\ (outcome = "rejection" => mainRejected)
  /\ (outcome = "ok" => ~mainRejected /\ denials < DenialLimit)
  /\ (outcome # "pending" /\ denials = DenialLimit =>
        outcome \in {"tool-denial", "rejection"})

CleanupCoverage ==
  /\ cleanupAttempted \subseteq opened
  /\ (phase = "done" => cleanupAttempted = opened /\ outcome \in Outcomes)
=============================================================================
