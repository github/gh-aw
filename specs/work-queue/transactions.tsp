import "@typespec/json-schema";

using TypeSpec.JsonSchema;

@minValue(0)
@maxValue(9007199254740991)
scalar WorkQueueEnqueueTime extends safeint;

@doc("Operator work-queue records used by gh aw work-queue on gh-aw-work-queue.")
@jsonSchema
model WorkTransaction {
  kind: "Work";
  work_id: string;
  work: Record<unknown>;
  enqueued?: WorkQueueEnqueueTime;
}

@jsonSchema
model ClaimTransaction {
  kind: "Claim";
  work_id: string;
  claim_id: string;
  run_id: string;
}

@jsonSchema
model ClaimCancellationTransaction {
  kind: "ClaimCancellation";
  work_id: string;
  claim_id: string;
}

@jsonSchema
model WorkCancellationTransaction {
  kind: "WorkCancellation";
  work_id: string;
}

@jsonSchema
model CompletionTransaction {
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
  version: 2;
  kind: "Work";
  work: WorkQueueIdentity;
  claim: null;
  attempt: null;
  enqueued?: WorkQueueEnqueueTime;
}

@jsonSchema
model WorkQueueClaimTransaction {
  version: 2;
  kind: "Claim";
  work: WorkQueueIdentity;
  claim: WorkQueueIdentity;
  attempt: null;
}

@jsonSchema
model WorkQueueClaimCancellationTransaction {
  version: 2;
  kind: "ClaimCancellation";
  work: WorkQueueIdentity;
  claim: WorkQueueIdentity;
  attempt: null;
}

@jsonSchema
model WorkQueueWorkCancellationTransaction {
  version: 2;
  kind: "WorkCancellation";
  work: WorkQueueIdentity;
  claim: null;
  attempt: null;
}

@jsonSchema
model WorkQueueCompletionTransaction {
  version: 2;
  kind: "Completion";
  work: WorkQueueIdentity;
  claim: WorkQueueIdentity;
  attempt: WorkQueueIdentity;
}

@doc("Canonical version-2 workflow work-queue records stored on work-queue.")
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
