import "@typespec/json-schema";

using TypeSpec.JsonSchema;

@doc("Operator work-queue records used by gh aw work-queue on gh-aw-work-queue.")
@jsonSchema
model WorkTransaction {
  version?: 3;
  kind: "Work";
  work_id: string;
  work: Record<unknown>;
  @minValue(1)
  @maxValue(9007199254740991)
  sequence?: int64;
}

@jsonSchema
model ClaimTransaction {
  version?: 3;
  kind: "Claim";
  work_id: string;
  claim_id: string;
  run_id: string;
}

@jsonSchema
model ClaimCancellationTransaction {
  version?: 3;
  kind: "ClaimCancellation";
  work_id: string;
  claim_id: string;
}

@jsonSchema
model WorkCancellationTransaction {
  version?: 3;
  kind: "WorkCancellation";
  work_id: string;
}

@jsonSchema
model CompletionTransaction {
  version?: 3;
  kind: "Completion";
  work_id: string;
  claim_id: string;
  attempt_id: string;
  outcome?: string;
}

@jsonSchema
union Transaction {
  WorkTransaction,
  ClaimTransaction,
  ClaimCancellationTransaction,
  WorkCancellationTransaction,
  CompletionTransaction,
}

@minLength(1)
scalar WorkQueueIdentity extends string;

@jsonSchema
model WorkQueueWorkTransaction {
  version: 1;
  kind: "Work";
  work: WorkQueueIdentity;
  claim: null;
  attempt: null;
}

@jsonSchema
model WorkQueueClaimTransaction {
  version: 1;
  kind: "Claim";
  work: WorkQueueIdentity;
  claim: WorkQueueIdentity;
  attempt: null;
}

@jsonSchema
model WorkQueueClaimCancellationTransaction {
  version: 1;
  kind: "ClaimCancellation";
  work: WorkQueueIdentity;
  claim: WorkQueueIdentity;
  attempt: null;
}

@jsonSchema
model WorkQueueWorkCancellationTransaction {
  version: 1;
  kind: "WorkCancellation";
  work: WorkQueueIdentity;
  claim: null;
  attempt: null;
}

@jsonSchema
model WorkQueueCompletionTransaction {
  version: 1;
  kind: "Completion";
  work: WorkQueueIdentity;
  claim: WorkQueueIdentity;
  attempt: WorkQueueIdentity;
}

@doc("Canonical version-1 workflow work-queue records stored on work-queue.")
@jsonSchema
union WorkQueueTransaction {
  WorkQueueWorkTransaction,
  WorkQueueClaimTransaction,
  WorkQueueClaimCancellationTransaction,
  WorkQueueWorkCancellationTransaction,
  WorkQueueCompletionTransaction,
}

@doc("Trusted inbound assignment supplied as aw_context.work_queue.")
@jsonSchema
model WorkQueueAssignment {
  work_id: WorkQueueIdentity;
  claim_id: WorkQueueIdentity;
  work: Record<unknown>;
}

@doc("Agent intent recorded by work_queue_claim_finish, without authority fields.")
@jsonSchema
model WorkQueueFinishIntent {
  outcome: "completed" | "cancelled";
}
