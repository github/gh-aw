import "@typespec/json-schema";

using TypeSpec.JsonSchema;

@jsonSchema
model WorkTransaction {
  kind: "Work";
  work_id: string;
  work: Record<unknown>;
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
