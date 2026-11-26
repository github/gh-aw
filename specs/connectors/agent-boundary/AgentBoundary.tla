--------------------------- MODULE AgentBoundary ---------------------------
EXTENDS FiniteSets

\* Bounded abstraction of an untrusted agent and trusted connector host.
\* Mutations model isolation failures, not supported configuration options.
CONSTANTS Mutation, Probe

Readers == {"rest", "remote"}
Operations == Readers \cup {"unselected", "writer", "arbitrary"}
Paths == {"mcp", "socket", "oidc", "runner"}

VARIABLES selected, path, operation, valid, state, agentToken, executed
vars == <<selected, path, operation, valid, state, agentToken, executed>>

Init ==
  /\ selected \in SUBSET Readers
  /\ path \in Paths
  /\ operation \in Operations
  /\ valid \in BOOLEAN
  /\ state = "request"
  /\ agentToken = FALSE
  /\ executed = FALSE

Attempt ==
  /\ state = "request"
  /\ agentToken' = (path \in {"socket", "oidc", "runner"} /\
                      Mutation = path)
  /\ executed' =
       IF path = "mcp"
       THEN (operation \in selected \/ Mutation = "selection") /\ valid
       ELSE path \in {"socket", "oidc", "runner"} /\ Mutation = path
  /\ state' = "done"
  /\ UNCHANGED <<selected, path, operation, valid>>

Done == state = "done" /\ UNCHANGED vars
Next == Attempt \/ Done
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ selected \subseteq Readers /\ path \in Paths /\ operation \in Operations
  /\ valid \in BOOLEAN /\ state \in {"request", "done"}
  /\ agentToken \in BOOLEAN /\ executed \in BOOLEAN

TokenIsolation == ~agentToken
SelectedMCPOnly == executed => path = "mcp" /\ operation \in selected /\ valid
Safety == TypeOK /\ TokenIsolation /\ SelectedMCPOnly

\* A false invariant demonstrates an authorized selected read is reachable.
ProbeAbsent ==
  IF Probe = "read" THEN ~(executed /\ path = "mcp" /\ operation = "rest")
  ELSE TRUE
=============================================================================
