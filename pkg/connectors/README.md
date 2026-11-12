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

Connector plans do not fetch remote definitions or resolve credentials while
lowering. Execution bounds responses, refuses redirects, validates inputs and
resources, and labels remote results untrusted. Safe outputs require an explicit
write-enabled runtime and are validated again outside the agent credential
boundary.

## Tests

Run `go test ./pkg/connectors -count=1` to exercise the runtime package.
