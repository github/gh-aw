// @ts-check

/**
 * @typedef {"delegated"|"cancelled"|"deferred"|"skipped"|"warning"|"success"|"failed"} SafeOutputOutcome
 */

/**
 * Read an outcome field from either the processing record or its nested handler result.
 *
 * @param {any} result
 * @param {string} field
 * @returns {any}
 */
function pickOutcomeField(result, field) {
  if (result && typeof result === "object" && result[field] !== undefined) {
    return result[field];
  }
  const handlerResult = result?.result;
  if (handlerResult && typeof handlerResult === "object" && handlerResult[field] !== undefined) {
    return handlerResult[field];
  }
  return undefined;
}

/**
 * Classify a processing result using the shared safe-output outcome precedence.
 *
 * @param {any} result
 * @returns {SafeOutputOutcome}
 */
function classifySafeOutputResult(result) {
  if (pickOutcomeField(result, "delegated") === true) return "delegated";
  if (pickOutcomeField(result, "cancelled")) return "cancelled";
  if (pickOutcomeField(result, "deferred")) return "deferred";
  // A skipped operation did not apply a mutation, even when it carries a warning.
  if (pickOutcomeField(result, "skipped")) return "skipped";
  if (pickOutcomeField(result, "warning")) return "warning";
  if (pickOutcomeField(result, "success")) return "success";
  return "failed";
}

/**
 * Determine whether a processing result is a non-skipped, non-deferred, non-cancelled failure.
 *
 * @param {any} result
 * @returns {boolean}
 */
function isFailedProcessingResult(result) {
  return classifySafeOutputResult(result) === "failed";
}

/**
 * Compute item-level safe-output status for logs, step summary, and GitHub Actions outputs.
 *
 * @param {Array<any>|null|undefined} results
 * @returns {{itemsSucceeded: number, itemsApplied: number, itemsSkipped: number, itemsWarnings: number, itemsCancelled: number, itemsDeferred: number, itemsFailed: number, status: "success" | "completed_with_skips" | "completed_with_warnings" | "cancelled" | "deferred" | "partial_success" | "failure"}}
 */
function computeSafeOutputsStatus(results) {
  const safeResults = Array.isArray(results) ? results : [];
  const outcomes = safeResults.map(classifySafeOutputResult).filter(outcome => outcome !== "delegated");
  const count = outcome => outcomes.filter(itemOutcome => itemOutcome === outcome).length;
  const itemsApplied = count("success");
  const itemsSkipped = count("skipped");
  const itemsWarnings = count("warning");
  const itemsCancelled = count("cancelled");
  const itemsDeferred = count("deferred");
  const itemsFailed = count("failed");
  const itemsSucceeded = itemsApplied;
  const status =
    itemsFailed > 0
      ? itemsApplied > 0 || itemsSkipped > 0 || itemsWarnings > 0 || itemsCancelled > 0 || itemsDeferred > 0
        ? "partial_success"
        : "failure"
      : itemsCancelled > 0
        ? "cancelled"
        : itemsDeferred > 0
          ? "deferred"
          : itemsSkipped > 0
            ? "completed_with_skips"
            : itemsWarnings > 0
              ? "completed_with_warnings"
              : "success";

  return { itemsSucceeded, itemsApplied, itemsSkipped, itemsWarnings, itemsCancelled, itemsDeferred, itemsFailed, status };
}

module.exports = {
  classifySafeOutputResult,
  computeSafeOutputsStatus,
  isFailedProcessingResult,
  pickOutcomeField,
};
