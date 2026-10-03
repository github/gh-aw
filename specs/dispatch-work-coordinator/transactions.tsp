import "@typespec/json-schema";

using TypeSpec.JsonSchema;

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
