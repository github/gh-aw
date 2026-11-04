# workqueue Package

> Versioned Git-backed work-queue ledger, replay, scheduling, and lifecycle contracts.

## Overview

The `workqueue` package defines the version-3 transaction log used by gh-aw's native work queue. `work-queue.jsonl` on the `work-queue` branch is authoritative; Issues can be used as a projection, but are not queue storage. The package models submitted Work, immutable Policy, assignments and Claims, worker deployments, evidence, and lifecycle operations.

The core ledger API parses and validates commits, replays them into a `Projection`, and plans candidate transactions and scheduling decisions. Host and delivery types provide the boundaries for repository transport, dispatch, and independently verified output. Repository scheduling preferences can be parsed separately from authorization and approved worker routes.

For the full queue behavior and operator configuration, see the [work-queue reference](../../docs/src/content/docs/reference/work-queue.md) and [protocol models](../../specs/work-queue/README.md).

## Public API

### Core Types

| Type | Kind | Description |
|------|------|-------------|
| `Actor` | struct | Principal and repository identity attached to a ledger request. |
| `Request` | struct | Idempotent request envelope with parameters and fingerprint. |
| `QueueCommit` | struct | Versioned transaction-log record with actor, policy epoch, operations, and predecessor. |
| `Operation` | alias | Raw JSON representation of a closed ledger operation variant. |
| `Policy` | struct | Queue authorization, scheduling, pool, and resource limits. |
| `WorkDefinition` | struct | Immutable task definition, payload, dependencies, scheduling attributes, and optional resource bindings. |
| `Projection` | struct | State derived by replaying the ledger, including Work, Claims, Dispatches, Policy, and scheduling data. |
| `WorkState` | struct | Current lifecycle state and immutable definition for one Work item. |
| `ClaimState` | struct | Lifecycle and terminal evidence for one attempt. |
| `DispatchState` | struct | Assignment and worker-run state for a dispatch. |
| `Assignment` | struct | Worker input containing one dispatch and its Claims. |
| `Evidence` | struct | Recorded evidence used to validate lifecycle and delivery transitions. |
| `Selection` | struct | Scheduler selection or the reason no Work was selected. |
| `Decision` | struct | Planned transaction operations, assignments, and next selection. |
| `Settings` | struct | Parsed repository scheduling preferences; it does not grant authorization or routes. |
| `Branch` | struct | Git-backed queue ledger transport. |
| `NativeDeliveryHost` | struct | Native host for verifying and publishing queue delivery outputs. |

Other exported types represent typed operation parameters, dependencies and external resources, policies and worker profiles, queue limits, explanations, statistics, delivery verification, and settings overrides.

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `Parse` | `func Parse(data []byte) ([]QueueCommit, error)` | Parses a JSONL ledger into commits. |
| `Serialize` | `func Serialize(commits []QueueCommit) ([]byte, error)` | Encodes commits as the canonical ledger representation. |
| `ValidateCommit` | `func ValidateCommit(commit QueueCommit) error` | Validates one commit against the queue contract. |
| `Replay` | `func Replay(commits []QueueCommit) (Projection, error)` | Validates and replays commits into queue state. |
| `ReplayForHost` | `func ReplayForHost(commits []QueueCommit, host string) (Projection, error)` | Replays commits while validating resource references against an explicit GitHub host. |
| `BuildCandidate` | `func BuildCandidate(commits []QueueCommit, actor Actor, request Request, at int64) ([]QueueCommit, *QueueCommit, Decision, error)` | Builds a candidate ledger update and scheduling decision for a request. |
| `PlanNext` | `func PlanNext(state Projection, pool string, at int64) (Selection, error)` | Plans the next eligible Work without changing scheduler state. |
| `PlanDispatch` | `func PlanDispatch(state Projection, params DispatchParameters, requestID, commitID string, at int64) (Decision, error)` | Plans scheduler-selected assignments within the supplied dispatch bounds. |
| `ValidatePolicy` | `func ValidatePolicy(policy Policy) error` | Validates policy structure and semantic limits. |
| `ParseSettings` | `func ParseSettings(data []byte) (Settings, error)` | Parses the `work_queue` settings object. |
| `ParseRepositorySettings` | `func ParseRepositorySettings(data []byte) (Settings, error)` | Extracts and parses `work_queue` from repository configuration JSON. |
| `Canonical` | `func Canonical(data []byte) ([]byte, error)` | Encodes JSON using the queue's integer-only canonical profile. |
| `NewRequest` | `func NewRequest(id, kind string, actor Actor, parameters any) (Request, error)` | Constructs a request with its canonical fingerprint. |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `FileName` | untyped string | `"work-queue.jsonl"` | Ledger filename. |
| `DefaultBranch` | untyped string | `"work-queue"` | Default ledger branch. |
| `Version` | untyped integer | `3` | Current queue-ledger version. |
| `MaxTimestamp` | `int64` | `9007199254740991` | Largest timestamp representable as a safe JSON integer. |
| `MaxEnqueued` | untyped integer | `MaxTimestamp` | Maximum accepted enqueue timestamp. |
| `MaxClaimsPerDispatch` | untyped integer | `16` | Maximum Claims in a dispatch. |

## Usage Example

```go
data, err := os.ReadFile(workqueue.FileName)
if err != nil {
	panic(err)
}

commits, err := workqueue.Parse(data)
if err != nil {
	panic(err)
}

state, err := workqueue.Replay(commits)
if err != nil {
	panic(err)
}

fmt.Println(state.Repository, state.Stats.Work)
```

## Design Decisions

The ledger is the durable source of truth; `Projection` is derived state, not an independently authoritative store. Queue writes are modeled as validated transactions, and replay rechecks commit and lifecycle invariants instead of trusting a supplied snapshot.

Scheduling produces a plan without mutating the projection. A Claim authorizes one attempt, while verified delivery establishes a Result; completion, cancellation, and reservation release are distinct lifecycle facts. Repository settings describe scheduling preferences and are not an authorization source.

## Dependencies

Core serialization and ledger logic use the Go standard library. Repository and host integrations use `go-gh`, `pkg/console`, `pkg/envutil`, `pkg/githubapi`, `pkg/logger`, and `pkg/parser`. JSON Schema validation uses `github.com/santhosh-tekuri/jsonschema/v6`.

## Concurrency

`Projection` and its nested maps are mutable caller-visible state. Callers should not mutate a projection concurrently with replay or planning operations; this package does not provide a package-wide synchronization guarantee.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
