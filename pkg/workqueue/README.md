# workqueue Package

> Versioned Git-backed work-queue ledger, replay, scheduling, and lifecycle contracts.

## Overview

The `workqueue` package implements protocol version 4 of gh-aw's native work queue in Go. `work-queue.jsonl` on the `work-queue` branch is the only authority: an append-only JSONL log with one `QueueCommit` per line. Every write is a Git commit whose parent is the observed head, published with a non-force ref update (compare-and-swap); a lost race re-reads the ledger and re-plans. Issues can be used as a projection, but are not queue storage.

The package parses and validates commits, replays them into a `Projection`, plans `dispatch_next` selections, and publishes requests through `Branch`. Scheduling settings come from the `work_queue` section of `aw.json`, compiled into the `GH_AW_WORK_QUEUE_POLICY` environment (`PolicyEnv`); they are not stored in the ledger.

For the full queue behavior and operator configuration, see the [work-queue reference](../../docs/src/content/docs/reference/work-queue.md), the [specification](../../docs/src/content/docs/specs/work-queue-specification.md), and [protocol models](../../specs/work-queue/README.md).

## Public API

### Core Types

| Type | Kind | Description |
|------|------|-------------|
| `Actor` | struct | Role, repository, and optional workflow/run identity of a request, taken from the job's own context. |
| `Request` | struct | Request id, closed kind, and canonical parameters. |
| `QueueCommit` | struct | One ledger line: version, id, previous id, request, actor, informational `At`, and 1–256 operations. |
| `Operation` | alias | Canonical JSON of one closed ledger operation (`json.RawMessage`). |
| `Resource` | struct | GitHub Issue or pull request reference used by dependencies, Observations, and Issue links. |
| `Dependency` | struct | Work predecessor or external Issue/pull-request edge. |
| `Replacement` | struct | Optional `replacement_of` reference on a Work definition. |
| `WorkDefinition` | struct | Work operation: identity, pool, priority, fairness key, worker profile, payload, and dependencies. |
| `ObservationOperation`, `ClaimOperation`, `DispatchOperation`, `BoundOperation`, `CompletionOperation`, `ClaimCancellationOperation`, `WorkCancellationOperation`, `ReleaseOperation`, `CheckpointOperation`, `IssueLinkOperation`, `IssueCommentOperation` | struct | Remaining closed operation variants. |
| `WorkIDsParameters`, `DispatchParameters`, `DispatchProfile`, `DispatchIDParameters`, `ReleaseParameters`, `CancelWorkParameters`, `CheckpointParameters`, `IssueLinkParameters` | struct | Closed request parameters per request kind. |
| `Position` | struct | FIFO position: commit ordinal and operation index. |
| `Projection` | struct | State derived by replaying the ledger: Work, Claims, Dispatches, Observations, committed requests, and `Stats`. |
| `WorkState` | struct | Work definition plus state, position, attempts, Completion effects, and cancellation data. |
| `ClaimState` | struct | One Claim: ids, handle, state, terminal commit, cancellation reason, and effects. |
| `DispatchState` | struct | One dispatch: pool, profile, workflow, state, run binding, and Claims. |
| `RunBinding` | struct | Run id and attempt bound to a dispatch. |
| `Stats` | struct | Aggregate counts of Work by state, Claims, Dispatches, transactions, and graph nodes. |
| `Decision` | struct | Planned `dispatch_next` Claims, dispatch ids, and plan reason. |
| `Assignment`, `AssignmentClaim`, `ResultReference` | struct | Worker view of one dispatch, its Claims, and predecessor effects. |
| `Branch` | struct | Git-backed ledger transport for one repository and branch. |
| `Snapshot` | struct | One read of the queue branch: head, tree, raw data, and replayed state. |
| `Publication` | struct | Outcome of one request publication, including `Committed` and `Recovered`. |
| `OperationsFor` | func type | Plans a request's operations against the current projection; re-run after each lost compare-and-swap. |
| `CheckpointResult` | struct | Outcome of `Branch.Checkpoint`. |
| `PolicyEnv`, `IssuesEnv`, `PoolEnv`, `ProfileEnv` | struct | Compiled `GH_AW_WORK_QUEUE_POLICY` environment. |
| `Settings`, `SettingsRetry`, `PoolSettings`, `IssuesSettings` | struct | Parsed `aw.json` `work_queue` settings. |
| `ProtocolError` | struct | Error carrying a stable protocol error code. |

### Constants and Variables

| Name | Value | Description |
|------|-------|-------------|
| `FileName` | `work-queue.jsonl` | Ledger file name. |
| `DefaultBranch` | `work-queue` | Default queue branch. |
| `Version`, `PolicyEnvVersion` | `4` | Ledger and policy-environment protocol versions. |
| `PayloadBytes` | 16 KiB | Maximum Work payload. |
| `OperationsPerCommit` | 256 | Maximum operations per commit. |
| `GraphNodes` | 4096 | Maximum replayed graph vertices. |
| `Predecessors` | 64 | Maximum dependencies per Work. |
| `AssignmentBytes` | 48 KiB | Maximum assignment size. |
| `EffectsBytes` | 4096 | Maximum canonical Completion effects. |
| `LedgerBytes` | 64 MiB | Maximum ledger size. |
| `CheckpointAfterLines` | 512 | Maintenance checkpoint threshold. |
| `MaxPendingWork` | 4096 | Maximum available plus claimed Work per pool. |
| `CASAttempts` | 8 | Maximum compare-and-swap publication attempts. |
| `Roles` | variable | Closed actor roles. |
| `ClaimCancellationReasons`, `WorkCancellationReasons`, `ReleaseReasons` | variable | Closed reason sets. |

### Ledger Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `Parse` | `func Parse(data []byte) ([]QueueCommit, error)` | Decodes ledger lines. |
| `Serialize` | `func Serialize(commits []QueueCommit) ([]byte, error)` | Encodes commits as canonical JSONL. |
| `Replay` | `func Replay(commits []QueueCommit) (Projection, error)` | Validates and replays commits. |
| `ReplayLog` | `func ReplayLog(data []byte) (Projection, error)` | Parses and replays a ledger file. |
| `ReplayRaw` | `func ReplayRaw(records []json.RawMessage) (Projection, error)` | Replays raw commit records. |
| `NewProjection` | `func NewProjection() Projection` | Returns an empty projection. |
| `AppendCommit` | `func AppendCommit(state Projection, commit QueueCommit) (Projection, error)` | Applies one commit to a cloned projection. |
| `ValidateCommit` | `func ValidateCommit(commit QueueCommit) error` | Validates commit shape and identity. |
| `LedgerProtocol` | `func LedgerProtocol(data []byte) int` | Reports the head line's protocol version. |
| `SerializeProjection` | `func SerializeProjection(state Projection) ([]byte, error)` | Serializes a projection for a Checkpoint. |
| `CheckpointCommit` | `func CheckpointCommit(state Projection, actor Actor, at int64) (QueueCommit, error)` | Builds a root Checkpoint commit. |
| `Projection.Clone` | `func (state Projection) Clone() Projection` | Deep-copies a projection. |
| `Projection.CommittedRequest` | `func (state Projection) CommittedRequest(requestID string) (*QueueCommit, bool)` | Finds the commit for a request id. |
| `Projection.OpenClaim` | `func (state Projection) OpenClaim(dispatch *DispatchState) *ClaimState` | Returns a dispatch's sole Claim while open, or nil once settled. |
| `Projection.DispatchOrder` | `func (state Projection) DispatchOrder() []string` | Lists dispatch ids in commit order. |
| `Projection.DependencyStatus` | `func (state Projection) DependencyStatus(work *WorkState) (bool, string)` | Reports whether a Work's dependencies are satisfied. |

### Request and Identity Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewRequest` | `func NewRequest(kind string, actor Actor, parameters any) (Request, error)` | Builds a request with its canonical id. |
| `RequestID` | `func RequestID(actor Actor, kind string, parameters any) (string, error)` | Computes `request:<sha256>`. |
| `CommitID` | `func CommitID(previous *string, requestID string) string` | Computes `q_<sha256>`. |
| `ProposedCommitID` | `func ProposedCommitID(state Projection, request Request) string` | Commit id for a request at the current tip. |
| `BuildCommit` | `func BuildCommit(state Projection, actor Actor, request Request, operations []Operation, at int64) QueueCommit` | Builds the next commit. |
| `Op` | `func Op(value any) (Operation, error)` | Canonically encodes an operation. |
| `ValidateActor` | `func ValidateActor(actor Actor) error` | Validates actor fields. |
| `Canonical`, `CanonicalValue` | `func Canonical(data []byte) ([]byte, error)`; `func CanonicalValue(value any) ([]byte, error)` | Canonical JSON encoding. |
| `ErrorCode` | `func ErrorCode(err error) string` | Extracts a protocol error code. |

### Graph and Scheduling Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewWork` | `func NewWork(graphID, nodeKey, pool, profile, fairnessKey string, priority int, payload []byte, dependsOn []Dependency) (WorkDefinition, error)` | Builds and validates a Work definition. |
| `ValidateWork` | `func ValidateWork(work WorkDefinition) error` | Validates a Work definition. |
| `NodeID` | `func NodeID(graphID, nodeKey string) string` | Computes a Work id. |
| `IndependentGraphID` | `func IndependentGraphID(payload []byte) (string, error)` | Default graph id from the canonical payload. |
| `GateKey` | `func GateKey(resource Resource, condition string) string` | Observation key for an external edge. |
| `FIFOCompare` | `func FIFOCompare(a, b Position) int` | Orders Work by FIFO position. |
| `ClaimIdentity` | `func ClaimIdentity(requestID, workID string) (claimID, dispatchID, handle string)` | Derives Claim, dispatch, and handle ids. |
| `PlanDispatch` | `func PlanDispatch(state Projection, parameters DispatchParameters, requestID string) (Decision, error)` | Plans a priority-FIFO `dispatch_next` selection. |
| `Projection.EligibleWork` | `func (state Projection) EligibleWork(pool string, profiles map[string]DispatchProfile) []*WorkState` | Lists eligible Work in scheduling order. |
| `Projection.PoolOccupancy` | `func (state Projection) PoolOccupancy(pool string) (int, map[string]int)` | Counts occupied slots (open Claims) and open Claims per fairness key. |
| `AssignmentForDispatch` | `func AssignmentForDispatch(state Projection, dispatchID string) (Assignment, error)` | Builds a worker assignment. |

### Transport and Settings Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `Branch.Read` | `func (b Branch) Read(ctx context.Context) (Snapshot, error)` | Reads and replays the queue branch. |
| `Branch.Publish` | `func (b Branch) Publish(ctx context.Context, actor Actor, kind string, parameters any, operationsFor OperationsFor) (Publication, error)` | Publishes one request with compare-and-swap retries. |
| `Branch.Checkpoint` | `func (b Branch) Checkpoint(ctx context.Context, actor Actor, minimumLines int) (CheckpointResult, error)` | Replaces the log with one Checkpoint commit. |
| `ParseSettings` | `func ParseSettings(data []byte) (Settings, error)` | Parses a `work_queue` settings object. |
| `ParseRepositorySettings` | `func ParseRepositorySettings(data []byte) (Settings, error)` | Parses `work_queue` from an `aw.json` document. |
| `BuildPolicyEnv` | `func BuildPolicyEnv(settings Settings, repository string, workflows []string) (PolicyEnv, error)` | Compiles settings and worker workflows into the policy environment. |
| `ParsePolicyEnv` | `func ParsePolicyEnv(data []byte) (PolicyEnv, error)` | Parses a policy environment. |
| `ValidatePolicyEnv` | `func ValidatePolicyEnv(env PolicyEnv) error` | Validates a policy environment. |
| `PolicyEnv.MarshalJSON` | `func (env PolicyEnv) MarshalJSON() ([]byte, error)` | Canonical policy-environment encoding. |

## Usage Example

```go
branch := workqueue.Branch{Remote: "owner/repo", Name: workqueue.DefaultBranch}
snapshot, err := branch.Read(ctx)
if err != nil {
	panic(err)
}
fmt.Println(snapshot.Head, snapshot.State.Stats.Work, snapshot.State.Stats.Available)

data, err := os.ReadFile(workqueue.FileName)
if err != nil {
	panic(err)
}
state, err := workqueue.ReplayLog(data)
if err != nil {
	panic(err)
}
fmt.Println(state.Repository, state.Stats.Work)
```

## Design Decisions

The ledger is the durable source of truth; `Projection` is derived state. Replay revalidates every commit, role, and transition instead of trusting a supplied snapshot, and never reads the informational `At` field. Request ids are content hashes, so retrying the same intent is idempotent and a request found on re-read is reported as `Recovered`.

Scheduling is priority then FIFO, bounded by pool concurrency and an optional per-account limit. `PlanDispatch` produces a plan without mutating the projection; replay does not re-plan selection. A Claim owns one attempt; a Completion records the applied effects, and a cancelled Claim returns its Work to the pool. Configuration lives in the compiled policy environment, so changing `aw.json` needs no ledger change. The protocol has no clocks: no deadlines, expiry, or backoff.

## Dependencies

Core serialization and ledger logic use the Go standard library. Git transport uses the `git` executable and `github.com/cli/go-gh/v2/pkg/auth` for credentials. Logging and environment helpers use `pkg/logger` and `pkg/envutil`.

## Concurrency

`Projection` and its nested maps are mutable caller-visible state. Callers should not mutate a projection concurrently with replay or planning operations; this package does not provide a package-wide synchronization guarantee. Concurrent writers coordinate only through compare-and-swap ref updates on the queue branch.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
