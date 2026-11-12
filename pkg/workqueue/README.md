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
| `SettingsRetry` | struct | Retry limits and backoff preferences for settings and pools. |
| `PoolSettings` | struct | Per-pool concurrency, account limit, and retry preferences. |
| `Branch` | struct | Git-backed queue ledger transport. |
| `NativeDeliveryHost` | struct | Native host for verifying and publishing queue delivery outputs. |
| `LedgerSession` | struct | Scoped set of ledger operations sharing one isolated Git object store. |

Other exported types represent typed operation parameters, dependencies and external resources, policies and worker profiles, queue limits, explanations, statistics, delivery verification, and settings overrides.

### Inspection and Delivery Types

| Type | Description |
|------|-------------|
| `TraceOptions`, `TraceEvent`, `TraceEvidence`, `TracePage` | Bounded ledger provenance, including lifecycle events and evidence metadata without work payloads or request parameters. |
| `NativeDeliveryScope` | Binds approved host callbacks to an immutable assignment and authenticated Actions attempt. |
| `NativeDeliveryOutput`, `NativeDeliveryInventory` | Closed effect-channel records supplied by an approved process callback; neither caller data nor receipts can create verifier authority. |
| `NativeOutputVerifier` | Host-registered callback for independent readback of a native delivery output. |

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
| `ObsoleteWorkIDs` | `func ObsoleteWorkIDs(state Projection, at int64, limit int) ([]string, error)` | Selects a bounded, byte-ordered batch of unclaimed available Work whose worker revision is incompatible; publication must still validate each cancellation. |
| `PlanDispatch` | `func PlanDispatch(state Projection, params DispatchParameters, requestID, commitID string, at int64) (Decision, error)` | Plans scheduler-selected assignments within the supplied dispatch bounds. |
| `ValidatePolicy` | `func ValidatePolicy(policy Policy) error` | Validates policy structure and semantic limits. |
| `ParseSettings` | `func ParseSettings(data []byte) (Settings, error)` | Parses the `work_queue` settings object. |
| `ParseRepositorySettings` | `func ParseRepositorySettings(data []byte) (Settings, error)` | Extracts and parses `work_queue` from repository configuration JSON. |
| `Canonical` | `func Canonical(data []byte) ([]byte, error)` | Encodes JSON using the queue's integer-only canonical profile. |
| `NewRequest` | `func NewRequest(id, kind string, actor Actor, parameters any) (Request, error)` | Constructs a request with its canonical fingerprint. |
| `AuthorizeEffect` | `func AuthorizeEffect(state Projection, actor Actor, target EffectResource) error` | Checks whether a completed worker Claim authorizes the requested effect. |
| `CanonicalValue` | `func CanonicalValue(value any) ([]byte, error)` | Encodes typed JSON without repairing strings or losing numeric values. |
| `CompactCheckpoint` | `func CompactCheckpoint(commits []QueueCommit, priorGitSHA string, actor Actor, at int64) ([]QueueCommit, error)` | Builds a minimal state checkpoint from validated history, bound to the Git commit containing the prior ledger. |
| `DefaultPolicy` | `func DefaultPolicy(principal, repository string) Policy` | Creates the default weighted-priority policy for a principal and repository. |
| `DispatchPrincipal` | `func DispatchPrincipal(dispatch *DispatchState) string` | Returns the principal frozen at dispatch launch from the selected AW credential. |
| `ExplainBeforeClaim` | `func ExplainBeforeClaim(commits []QueueCommit, claimID string) (ClaimExplanation, error)` | Validates ledger authority and explains the observations and Claims preceding a Claim. |
| `ExplainRequest` | `func ExplainRequest(commits []QueueCommit, requestID string) (RequestExplanation, error)` | Replays the ledger and explains the commit associated with a request ID. |
| `ExplainWork` | `func ExplainWork(state Projection, id string, at int64) (WorkExplanation, error)` | Explains a Work item's readiness and current state at the supplied time. |
| `Fingerprint` | `func Fingerprint(actor Actor, kind string, parameters json.RawMessage) (string, error)` | Computes the canonical fingerprint for a request. |
| `FuturePolicy` | `func FuturePolicy(state Projection) Policy` | Exposes current routes to trusted admission builders; it does not replace installed policy or affect existing assignments. |
| `Genesis` | `func Genesis(actor Actor, policy Policy, requestID, epoch string, at int64) (QueueCommit, error)` | Creates the initial policy commit for a ledger. |
| `IndependentGraphID` | `func IndependentGraphID(payload []byte) (string, error)` | Computes a graph ID from the canonical payload. |
| `NewChildWork` | `func NewChildWork(state Projection, actor Actor, payload []byte, nodeKey string, at int64) (WorkDefinition, error)` | Constructs child Work after validating actor authority and the parent projection. |
| `NewNativeDeliveryHost` | `func NewNativeDeliveryHost(options NativeDeliveryHostOptions) *NativeDeliveryHost` | Creates a native delivery host from the supplied options. |
| `NewWork` | `func NewWork(payload []byte, graphID, nodeKey, pool string, policy Policy, at int64) (WorkDefinition, error)` | Constructs a Work definition from its payload, graph, node, pool, and policy. |
| `NodeID` | `func NodeID(graphID, nodeKey string) string` | Derives a stable node ID from a graph ID and node key. |
| `NormalizeClaimHandle` | `func NormalizeClaimHandle(assignment Assignment, selector *string) (string, error)` | Resolves an optional Claim selector to the canonical Claim handle in an assignment. |
| `NormalizeFinishIntent` | `func NormalizeFinishIntent(assignment Assignment, data []byte) (FinishParameters, error)` | Validates finish-intent JSON while preserving the distinction between absent and null fields. |
| `ObservationRefreshBudget` | `func ObservationRefreshBudget(state Projection, requested int) (int, error)` | Bounds an observation refresh while reserving a Claim slot and excluding optional recovery reads. |
| `Op` | `func Op(value any) (Operation, error)` | Encodes a typed operation using the queue's canonical JSON representation. |
| `ParseAssignment` | `func ParseAssignment(data []byte) (Assignment, error)` | Validates and decodes a worker assignment. |
| `ProposedCommitID` | `func ProposedCommitID(state Projection, request Request) string` | Derives the proposed commit ID from the current tip and request ID. |
| `RecoveryHeadroom` | `func RecoveryHeadroom(state Projection) int64` | Reports the outstanding closure reservation in a replayed projection. |
| `ReplayAt` | `func ReplayAt(commits []QueueCommit, at int64) (Projection, error)` | Replays historical decisions at committed time and adds a disposable current-time view. |
| `ReplayAtForHost` | `func ReplayAtForHost(commits []QueueCommit, at int64, host string) (Projection, error)` | Adds a disposable current-time view using the selected resource host. |
| `TraceQueue` | `func TraceQueue(commits []QueueCommit, options TraceOptions, at int64) (TracePage, error)` | Returns bounded ledger provenance without payloads, descriptors, receipts, or request parameters. |
| `ValidReason` | `func ValidReason(reason string) bool` | Reports whether a reason is a bounded, sanitized queue reason code. |
| `ValidateAssignment` | `func ValidateAssignment(state Projection, assignment Assignment) error` | Verifies that an assignment matches the dispatch recorded in the projection. |

### Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Assignment.ToReceipt` | `func (assignment Assignment) ToReceipt() AssignmentReceipt` | Projects assignment identity without work payloads or result references; callers populate lifecycle facts separately from a validated projection. |
| `AssignmentClaim.ToReceipt` | `func (claim AssignmentClaim) ToReceipt() ClaimReceipt` | Projects Claim identity without work payloads or result references. |
| `Branch.Authenticate` | `func (b Branch) Authenticate(ctx context.Context, role string) (Actor, error)` | Derives operator identity from credentials rather than caller-supplied actor fields or flags. |
| `Branch.AuthenticateClear` | `func (b Branch) AuthenticateClear(ctx context.Context) (Actor, error)` | Authorizes repository maintainers and administrators for clear operations. |
| `Branch.CancelReserved` | `func (b Branch) CancelReserved(ctx context.Context, dispatchID, requestID string) (Reconciliation, error)` | Cancels a reservation before launch; a competing start marker wins the compare-and-swap. |
| `Branch.Compact` | `func (b Branch) Compact(ctx context.Context) (Compaction, error)` | Replaces validated ledger history with a canonical checkpoint bound to the prior Git commit. |
| `Branch.DeploymentOperationsFromConfig` | `func (b Branch) DeploymentOperationsFromConfig(ctx context.Context, state Projection, poolName, workerName string) ([]Operation, error)` | Proposes route-only changes for existing AW workers; unavailable registrations pause only affected targets. |
| `Branch.DispatcherFromConfig` | `func (b Branch) DispatcherFromConfig(ctx context.Context, name string) (DispatcherRoute, error)` | Resolves a dispatcher at the verified default revision without granting or launching Claims. |
| `Branch.HardClear` | `func (b Branch) HardClear(ctx context.Context, requestID, reason string, dryRun bool) (HardClearResult, error)` | Previews or resets only a settled, paused queue while retaining the prior ledger in Git history. |
| `Branch.HardClearFromSnapshot` | `func (b Branch) HardClearFromSnapshot(ctx context.Context, requestID, reason, priorGitSHA string) (HardClearResult, error)` | Resets only the exact Git snapshot previewed by the operator. |
| `Branch.InspectEvidence` | `func (b Branch) InspectEvidence(ctx context.Context, dispatchID string) (Reconciliation, error)` | Reads evidence for the specified dispatch and returns its reconciliation state. |
| `Branch.OpenLedgerSession` | `func (b Branch) OpenLedgerSession(ctx context.Context) (*LedgerSession, error)` | Opens a scoped session with an isolated Git object store. |
| `Branch.PolicyFromConfig` | `func (b Branch) PolicyFromConfig(ctx context.Context) (Policy, error)` | Resolves scheduling policy and worker approval from the target repository's verified immutable default ref. |
| `Branch.PrepareDispatcherActivation` | `func (b Branch) PrepareDispatcherActivation(ctx context.Context, route DispatcherRoute) (string, error)` | Binds a verified dispatcher route to a named snapshot ref without moving or deleting tags. |
| `Branch.Publish` | `func (b Branch) Publish(ctx context.Context, actor Actor, request Request) (Publication, error)` | Publishes authenticated intent, rebuilding selections and charges after compare-and-swap retries. |
| `Branch.PublishFromSnapshot` | `func (b Branch) PublishFromSnapshot(ctx context.Context, actor Actor, request Request, selected []QueueCommit) (Publication, error)` | Publishes intent selected from a snapshot while rejecting replacement ledgers. |
| `Branch.Read` | `func (b Branch) Read(ctx context.Context) ([]QueueCommit, error)` | Refreshes the queue head and reads its validated ledger. |
| `Branch.Reconcile` | `func (b Branch) Reconcile(ctx context.Context, dispatchID, requestID string) (Reconciliation, error)` | Inspects the bound native attempt; missing or unbound runs retain reservations. |
| `Branch.RecoverDelivery` | `func (b Branch) RecoverDelivery(ctx context.Context, workID, requestID string) (DeliveryRecovery, error)` | Recovers a delivery barrier independently of native capacity; missing proof remains unknown. |
| `Branch.SynchronizeIssueTransition` | `func (b Branch) SynchronizeIssueTransition(ctx context.Context, requestID string) (result IssueSynchronization, err error)` | Synchronizes one checked terminal request as an explicit maintain/admin action. |
| `Branch.SynchronizePublishedIssueTransition` | `func (b Branch) SynchronizePublishedIssueTransition(ctx context.Context, publication *Publication) (IssueSynchronization, error)` | Synchronizes the exact checked publication using its publisher's existing writer authority. |
| `DispatchParameters.MarshalJSON` | `func (parameters DispatchParameters) MarshalJSON() ([]byte, error)` | Serializes dispatch bounds while preserving an explicitly empty worker-profile list. |
| `LedgerSession.Close` | `func (s *LedgerSession) Close()` | Closes the session and removes its temporary Git object store. |
| `LedgerSession.Compact` | `func (s *LedgerSession) Compact(ctx context.Context) (Compaction, error)` | Compacts the current queue using the session's object store. |
| `LedgerSession.Publish` | `func (s *LedgerSession) Publish(ctx context.Context, actor Actor, request Request) (Publication, error)` | Publishes a checked request using the session's object store. |
| `LedgerSession.Read` | `func (s *LedgerSession) Read(ctx context.Context) ([]QueueCommit, error)` | Refreshes the queue head and reads its validated ledger. |
| `NativeDeliveryHost.Verifier` | `func (host *NativeDeliveryHost) Verifier(branch Branch) DeliveryVerifier` | Returns the native verifier used for recovery and fresh ledger prefixes. |
| `ProtocolError.Error` | `func (e *ProtocolError) Error() string` | Formats the stable error code and rejection message as `<code>: <message>`. |
| `Settings.Apply` | `func (settings Settings) Apply(policy Policy) (Policy, error)` | Applies scheduling preferences to host-approved policy routes without adding profile authority. |
| `WorkState.SchedulingPriority` | `func (work *WorkState) SchedulingPriority() int` | Computes scheduling priority without changing the admitted definition or historical Claims. |

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
