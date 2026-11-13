import type { HandlerIndeterminateResult, HandlerResult, MergePullRequestIndeterminateResult } from "./handler-factory";

const uncertainOperation: HandlerIndeterminateResult = { success: false, operation_outcome: "indeterminate" };
void uncertainOperation;
const indeterminate: MergePullRequestIndeterminateResult = { success: false, operation_outcome: "indeterminate", merged: null };
const handlerResult: HandlerResult = indeterminate;
void handlerResult;

// @ts-expect-error An indeterminate merge cannot claim that the mutation did not happen.
const notMerged: MergePullRequestIndeterminateResult = { success: false, operation_outcome: "indeterminate", merged: false };
void notMerged;

// @ts-expect-error Indeterminate results must carry the explicit outcome marker.
const missingOutcome: MergePullRequestIndeterminateResult = { success: false, merged: null };
void missingOutcome;
