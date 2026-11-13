# connectors Package

> Bounded runtime for experimental AW connector capabilities.

## Overview

The `connectors` package validates connector specifications, lowers a binding into
a frozen execution `Plan`, and runs selected reads or safe-output requests through
a host-supplied runtime. The workflow compiler accepts declarations under
`connectors.<alias>`; importing or declaring a capability does not select or
authorize it.

The complete declaration format, execution boundaries, supported operations,
and security requirements are specified in [`CONTRACT.md`](CONTRACT.md). Use
that contract as the source of truth when adding or changing connector behavior.

## Public API

| Symbol | Description |
|--------|-------------|
| `ConnectorSpec`, `Connection`, `Binding` | Validated source documents and selected capability bindings. |
| `Service`, `Selection`, `NewService`, `LoadService`, `Select` | Construct or load a service and select capabilities into a plan. |
| `Plan`, `Lower`, `DecodePlan`, `Plan.ForPhase` | Produce, decode, or prune an immutable execution plan for a phase. |
| `Runtime`, `SecretResolver`, `Plan.Execute` | Execute a selected capability using host-resolved credentials and approved origins. |
| `SafeOutputRequest`, `Plan.PrepareSafeOutput`, `Plan.ExecuteSafeOutput` | Prepare credential-free write intent and validate it again in the safe-output phase. |
| `Plan.ProjectEvent`, `Plan.ActivateTriggers` | Project event evidence for inspection; trigger activation remains unavailable without an approved ingress. |

### Declaration and execution types

| Area | Exported symbols | Description |
|------|------------------|-------------|
| Requests | `Credential`, `Input`, `Value`, `BinaryRequest`, `Patch`, `Operation`, `RetryPolicy` | Describe phase-bound credentials, bounded inputs, typed request values, request bodies, and explicitly bounded retries. |
| Trusted writes | `TrustedInput`, `TrustedPreflight`, `InputLookup`, `CreatedResource`, `CreatedResourceHost` | Model authored safe-output inputs, preflight checks, canonical identity lookups, and invocation-local temporary resources. |
| Read and MCP capabilities | `ReadTool`, `HTTPMCP`, `MCPTool`, `Result`, `ToolCapability` | Describe REST-backed reads, remote streamable-HTTP MCP tools, and untrusted execution results. |
| Events | `Trigger`, `EventPredicate`, `EventFilter`, `EventVariant`, `EventProjection`, `EventEvaluationVersion` | Represent bounded matching and projection rules over authenticated evidence; variants cannot grant resource authority. |
| Connections and payloads | `APIVersion`, `SecretRef`, `Connection`, `Binding`, `InputSchema`, `Inputs`, `Secrets`, `Origins`, `ToolNames`, `SafeOutputNames`, `TriggerNames`, `UsesStagedFile` | Represent the versioned declaration contract, credential names, selected capabilities, and staged-file requirements in the frozen, phase-restricted plan. |

### Additional exported operations

`Decode`, `LoadSpec`, `LoadServiceSpec`, `Schema`,
`Plan.ValidateContext`, `ValidateEventPredicate`, and
`EvaluateEventPredicate` provide strict document loading, editor schema
generation, context checks, and event-rule validation.
`Plan.Encode`, `Plan.EncodeJSON`, and `DecodePlan` freeze or verify plan
snapshots; `MaxDocumentBytes` and `MaxEncodedPlanBytes` bound their inputs.
`Plan.PrepareSafeOutputIntent` and `Plan.ExecuteCreatedSafeOutput` handle
temporary references within the safe-output boundary. `Plan.CallMCP` and
`Plan.MCPServer` provide the constrained MCP adapter. `ErrIngressUnavailable`
makes the uninstalled trigger boundary explicit.

Connector plans do not fetch remote definitions or resolve credentials while
lowering. Execution bounds responses, refuses redirects, validates inputs and
resources, and labels remote results untrusted. Safe outputs require an explicit
write-enabled runtime and are validated again outside the agent credential
boundary.

## Tests

Run `go test ./pkg/connectors -count=1` to exercise the runtime package.
