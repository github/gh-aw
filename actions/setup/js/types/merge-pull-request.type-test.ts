import type { HandlerResult, MergePullRequestIndeterminateResult } from "./handler-factory";

const indeterminate: MergePullRequestIndeterminateResult = { success: false, merge_outcome: "indeterminate", merged: null };
const handlerResult: HandlerResult = indeterminate;
void handlerResult;

// @ts-expect-error An indeterminate merge cannot claim that the mutation did not happen.
const notMerged: MergePullRequestIndeterminateResult = { success: false, merge_outcome: "indeterminate", merged: false };
void notMerged;

// @ts-expect-error Indeterminate results must carry the explicit outcome marker.
const missingOutcome: MergePullRequestIndeterminateResult = { success: false, merged: null };
void missingOutcome;
