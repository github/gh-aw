# lifecycle Package

> Defines provider-independent lifecycle records and adoption rules for safe-output effects.

## Overview

The `lifecycle` package models verified delivery separately from later adoption and survival observations. A `Record` identifies one safe-output effect, retains the evidence needed to revisit it, and merges replayed records without allowing older observations to replace newer or more authoritative evidence.

## Public API

### Types

| Type | Kind | Description |
|------|------|-------------|
| `Identity` | struct | Identifies the host, source repository and workflow, run and attempt, invocation, evidence item, output type, and optional target resource. |
| `Delivery` | struct | Records delivery state and signal, event and observation clocks, and evidence source. |
| `Actor` | struct | Identifies the login and type of the account that decided an adoption outcome. |
| `Adoption` | struct | Records adoption state and signal, decision and observation clocks, source, rule version, acceptance kind, deciding actor, human verification, coverage, and any error. |
| `Survival` | struct | Records whether an adopted effect remains observed or was reversed, with observation time, coverage, and any error. |
| `Record` | struct | Combines an ID and `Identity` with `Delivery`, `Adoption`, `Survival`, optional reversal details, and optional retained `Execution` evidence. |
| `Execution` | struct | Retains a resource number, labels before and added, metadata, and before/after state for rechecking an effect after artifacts expire. |
| `Observation` | struct | Supplies an adoption state and signal, optional decision clock, observation clock, acceptance kind, deciding actor, human-verification status, or an error. |
| `Rule` | struct | Describes an output type's effect class, delivery fallback and value eligibility, survival requirement, parent resource, and human-decision, rejection, and reversal signals. |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `Delivered` | `func Delivered(identity Identity, at, observedAt time.Time, source string) (Record, error)` | Validates execution identity and event clocks, derives a stable record ID from the identity, and initializes delivery and rule-based adoption state. A target URL, when present, must be an HTTPS resource on the execution host without user information or a query. |
| `Observe` | `func Observe(record Record, observation Observation) (Record, error)` | Applies a provider adoption observation. Accepted decisions require a valid value-eligible output rule and verified human acceptance, an automation decision with its actor, or an allowed delivery fallback. Automation decisions cannot claim human verification. |
| `Merge` | `func Merge(previous, incoming Record) (Record, error)` | Validates and combines records for the same effect, preserving execution evidence and preventing replayed delivery from replacing later or more authoritative observations. |
| `IsHuman` | `func IsHuman(actorType, login string, publishers []string) bool` | Reports whether an actor is a non-bot GitHub user who is not in the supplied publisher list. |
| `RuleFor` | `func RuleFor(outputType string) (Rule, bool)` | Returns a copy of the registered rule for an output type and whether that type is registered. |
| `Adoption.Equal` | `func (a Adoption) Equal(b Adoption) bool` | Compares adoption records after normalizing their timestamps to UTC. |
| `Survival.Equal` | `func (a Survival) Equal(b Survival) bool` | Compares survival records after normalizing observation timestamps to UTC. |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `RuleVersion` | untyped `string` | `"gh-aw-adoption-v1"` | Version identifier for the registered adoption rules. |

Rules classify supported output types as proposals, publications, mutations, delegations, or infrastructure. Proposal adoption requires an explicit decision; eligible publication, mutation, and delegation rules may accept delivery as a fallback. Infrastructure rules are not value-eligible.

## Usage Example

```go
import (
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
)

func recordDelivery() (lifecycle.Record, error) {
	now := time.Now().UTC()
	return lifecycle.Delivered(lifecycle.Identity{
		Host: "github.com", Repository: "org/source", Workflow: "worker",
		RunID: 42, RunAttempt: 1, InvocationID: "worker:1",
		EvidenceID: "result:1", Type: "create_issue",
	}, now, now, "safe-output-manifest")
}
```

## Design Decisions

- Delivery is based on execution identity and evidence, not a request or job conclusion.
- Adoption and survival are separate observations so delivery is not treated as proof of human acceptance or continued value.
- Rule slices returned by `RuleFor` are cloned so callers cannot mutate the package's registered rule data.

## Dependencies

The package has no internal dependencies. It uses Go standard-library packages for JSON encoding, hashing, URL validation, timestamps, and slice copying.

## Thread Safety

Lifecycle operations use caller-owned records. The package's rule registry is initialized once and is not exposed for mutation; `RuleFor` returns cloned signal slices.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
