import "@typespec/json-schema";

using TypeSpec.JsonSchema;

// Closed current-only contract. Actor/evidence are normalized by authenticated
// trusted publishers; their strings are provenance, never authentication.
@minLength(1)
@maxLength(256)
@extension("x-utf8-max-bytes", 256)
@extension("x-forbid-ascii-controls", true)
scalar Identity extends string;
@minLength(1)
@maxLength(128)
@pattern("^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
scalar ReasonCode extends string;
@pattern("^[1-9][0-9]*$")
@maxLength(256)
scalar Decimal extends string;
@pattern("^[a-f0-9]{40}$|^[a-f0-9]{64}$")
scalar Revision extends string;
@pattern("^[a-f0-9]{64}$")
scalar Digest extends string;
@minValue(0)
@maxValue(9007199254740991)
scalar Timestamp extends safeint;
@minValue(1)
@maxValue(1000)
scalar Weight extends safeint;
@minValue(1)
@maxValue(5)
scalar Priority extends safeint;
@minValue(1)
@maxValue(16)
scalar ClaimBound extends safeint;
@minValue(1)
@maxValue(4096)
scalar CountBound extends safeint;

@jsonSchema
model Actor {
  role: "administrator" | "producer" | "dispatcher" | "worker" | "reconciler";
  principal: Identity;
  repository: Identity;
  workflow?: Identity;
  run_id?: Decimal;
  // Logical source attempts may rerun; worker authority requires attempt 1.
  run_attempt?: CountBound;
  dispatch_id?: Identity;
  claim_handle?: Identity;
}

@jsonSchema
model Trace {
  trace_id?: Identity;
  span_id?: Identity;
  publisher_attempt?: CountBound;
}

@jsonSchema
model Resource {
  kind: "issue" | "pull_request";
  host: Identity;
  repository: Identity;
  repository_id: Decimal;
  resource_id: Decimal;
  number: Decimal;
}

@jsonSchema
model WorkEdge {
  kind: "work";
  work_id: Identity;
}
@jsonSchema
model IssueEdge {
  kind: "issue";
  resource: Resource;
  condition: "completed" | "closed";
}
@jsonSchema
model PullRequestEdge {
  kind: "pull_request";
  resource: Resource;
  condition: "merged";
}
@jsonSchema
union Dependency { WorkEdge, IssueEdge, PullRequestEdge }

@jsonSchema
model Replacement {
  work_id: Identity;
  disposition: "idempotent" | "compensated" | "inspection";
  evidence: Identity;
}

@jsonSchema
model WorkOperation {
  kind: "Work";
  work_id: Identity;
  graph_id: Identity;
  node_key: Identity;
  pool: Identity;
  priority: Priority;
  @maxLength(128)
  fairness_key: string;
  worker_profile: Identity;
  batch_trust_domain: Identity;
  // All nested values also obey the native canonical wire profile, including numeric token restrictions.
  payload: Record<unknown>;
  @maxItems(64)
  depends_on: Dependency[];
  enqueued: Timestamp;
  subject?: Resource;
  replacement_of?: Replacement;
}

@jsonSchema
model WorkerProfile {
  workflow: Identity;
  ref: Revision;
  principal: Identity;
  trust_domain: Identity;
  credential_scope: Identity;
  effect_scope: Identity;
  max_claims: ClaimBound;
  share_keys: boolean;
}
@jsonSchema
model RetryPolicy {
  max_attempts: CountBound;
  backoff_ms: Timestamp;
}
@jsonSchema
model ReconciliationPolicy {
  max_attempts: CountBound;
  deadline_ms: Timestamp;
}
@jsonSchema
model PoolPolicy {
  default_profile: Identity;
  @extension("x-key-min-utf8-bytes", 1)
  @extension("x-key-utf8-max-bytes", 256)
  @extension("x-forbid-key-ascii-controls", true)
  profiles: Record<WorkerProfile>;
  logical_limit: CountBound;
  native_limit: CountBound;
  per_account_limit?: CountBound;
  allowed_repositories: Identity[];
  max_observation_age_ms: Timestamp;
  retry: RetryPolicy;
  reconciliation: ReconciliationPolicy;
}
@jsonSchema
model ProducerRule {
  pools: Identity[];
  priorities: Priority[];
  fairness_keys: string[];
}
@jsonSchema
model Limits {
  ledger_bytes: Timestamp;
  recovery_bytes: Timestamp;
  payload_bytes: Timestamp;
  graph_nodes: CountBound;
  predecessors: CountBound;
  pending_nodes: CountBound;
  operations: CountBound;
  assignment_bytes: Timestamp;
  result_bytes: Timestamp;
  evidence_bytes: Timestamp;
  observation_writes: CountBound;
}
@jsonSchema
model QueuePolicy {
  mode: "weighted-priority" | "strict-priority";
  @minItems(5)
  @maxItems(5)
  class_weights: Weight[];
  accounting_weights: Record<Weight>;
  @extension("x-key-min-utf8-bytes", 1)
  @extension("x-key-utf8-max-bytes", 256)
  @extension("x-forbid-key-ascii-controls", true)
  producers: Record<ProducerRule>;
  @extension("x-key-min-utf8-bytes", 1)
  @extension("x-key-utf8-max-bytes", 256)
  @extension("x-forbid-key-ascii-controls", true)
  pools: Record<PoolPolicy>;
  limits: Limits;
}

@jsonSchema
model PolicyOperation {
  kind: "Policy";
  epoch: Identity;
  policy: QueuePolicy;
}
@jsonSchema
model ControlOperation {
  kind: "Control";
  control: "admission_paused" | "grants_paused" | "credential_generation";
  value: boolean | Identity;
  reason: ReasonCode;
}
@jsonSchema
model ClaimOperation {
  kind: "Claim";
  work_id: Identity;
  claim_id: Identity;
  dispatch_id: Identity;
  handle: Identity;
  @maxItems(64)
  @extension("uniqueItems", true)
  observations: Identity[];
}
@jsonSchema
model CompletionOperation {
  kind: "Completion";
  work_id: Identity;
  claim_id: Identity;
  dispatch_id: Identity;
  claim_handle: Identity;
  run_id: Decimal;
  run_attempt: 1;
}
@jsonSchema
model Evidence {
  kind: "prelaunch" | "nonlaunch" | "terminal_run" | "delivery" | "reconciliation";
  source: "trusted_publisher" | "github_api" | "trusted_activation" | "verified_receipts";
  repository: Identity;
  workflow: Identity;
  ref: Revision;
  principal: Identity;
  checked_at: Timestamp;
  run_id?: Decimal;
  run_attempt?: 1;
  status?: "completed";
  conclusion?: Identity;
  receipt?: Identity;
  attempts?: CountBound;
  effects?: "none" | "partial" | "unknown";
}
@jsonSchema
model RunBinding {
  run_id: Decimal;
  run_attempt: 1;
  repository: Identity;
  workflow: Identity;
  ref: Revision;
  principal: Identity;
  event: "workflow_dispatch";
}
@jsonSchema
model DispatchOperation {
  kind: "Dispatch";
  dispatch_id: Identity;
  state: "started" | "bound" | "rejected" | "uncertain" | "unresolved";
  sender?: Actor;
  run?: RunBinding;
  evidence?: Evidence;
  reason?: ReasonCode;
}
@jsonSchema
model ResultOperation {
  kind: "Result";
  work_id: Identity;
  claim_id: Identity;
  completion_id: Identity;
  descriptor: Record<unknown>;
  evidence: Evidence;
}
@jsonSchema
model DeliveryFailureOperation {
  kind: "DeliveryFailure";
  work_id: Identity;
  claim_id: Identity;
  completion_id: Identity;
  reason: ReasonCode;
  disposition: "none" | "partial" | "unknown";
  evidence: Evidence;
}
@jsonSchema
model ObservationOperation {
  kind: "Observation";
  observation_id: Identity;
  resource: Resource;
  condition: "completed" | "closed" | "merged";
  state: "ready" | "waiting" | "failed" | "unknown";
  observed_at: Timestamp;
  credential_generation: Identity;
  source_updated_at?: Timestamp;
  read_status: Identity;
  state_reason?: "completed" | "not_planned" | "reopened";
  resource_state?: "open" | "closed";
  merged?: boolean;
  merge_commit?: Identity;
}
@jsonSchema
model ClaimCancellationOperation {
  kind: "ClaimCancellation";
  work_id: Identity;
  claim_id: Identity;
  reason: ReasonCode;
  retry_not_before: Timestamp;
}
@jsonSchema
model WorkCancellationOperation {
  kind: "WorkCancellation";
  work_id: Identity;
  reason: ReasonCode;
}
@jsonSchema
model ReleaseOperation {
  kind: "Release";
  dispatch_id: Identity;
  evidence: Evidence;
}
@jsonSchema
union QueueOperation {
  PolicyOperation, ControlOperation, WorkOperation, ClaimOperation,
  CompletionOperation, ResultOperation, DeliveryFailureOperation,
  ObservationOperation, ClaimCancellationOperation, WorkCancellationOperation,
  DispatchOperation, ReleaseOperation
}

@jsonSchema
model SubmitParameters {
  @minItems(1)
  @maxItems(256)
  nodes: WorkOperation[];
}
@jsonSchema
model DispatchNextParameters {
  pool: Identity;
  max_claims: CountBound;
  max_dispatches: CountBound;
  max_bytes: Timestamp;
}
@jsonSchema
model FinishParameters {
  dispatch_id: Identity;
  claim_handle: Identity;
  outcome: "completed" | "cancelled";
}
@jsonSchema
model OperationsParameters {
  @minItems(1)
  @maxItems(256)
  operations: QueueOperation[];
}
@jsonSchema
union RequestParameters {
  SubmitParameters, DispatchNextParameters, FinishParameters, OperationsParameters
}
@jsonSchema
model QueueRequest {
  id: Identity;
  kind: "policy" | "control" | "submit" | "dispatch_next" | "finish" | "observe" | "dispatch" | "release" | "result" | "delivery_failure" | "cancel_work" | "cancel_claim";
  parameters: RequestParameters;
  fingerprint: Digest;
}

@jsonSchema
model QueueCommit {
  version: 3;
  id: Identity;
  previous: Identity | null;
  request: QueueRequest;
  actor: Actor;
  policy_epoch: Identity;
  at: Timestamp;
  @minItems(1)
  @maxItems(256)
  operations: QueueOperation[];
  trace?: Trace;
}

@jsonSchema
model ResultReference {
  work_id: Identity;
  result_commit_id: Identity;
  descriptor: Record<unknown>;
}
@jsonSchema
model AssignmentClaim {
  handle: Identity;
  claim_id: Identity;
  work_id: Identity;
  work: Record<unknown>;
  result_refs: ResultReference[];
}
@jsonSchema
model WorkQueueAssignment {
  version: 3;
  dispatch_id: Identity;
  request_id: Identity;
  commit_id: Identity;
  policy_epoch: Identity;
  pool: Identity;
  worker_profile: Identity;
  @minItems(1)
  @maxItems(16)
  claims: AssignmentClaim[];
}
@jsonSchema
model WorkQueueFinishIntent {
  claim_handle?: Identity;
  outcome: "completed" | "cancelled";
}
