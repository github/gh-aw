// @ts-check

/**
 * evals_status — shared reader for the BinEval run-level not-evaluated marker.
 *
 * run_evals.cjs writes evals_status.json (and no evals.jsonl) when the evals judge
 * did not start, failed, was skipped, or produced no assistant response. Consumers
 * use this helper to recognize the marker without treating it as answer records.
 */

"use strict";

const fs = require("fs");
const { EVALS_STATUS_PATH } = require("./evals_constants.cjs");

const EVALS_NOT_EVALUATED = "not_evaluated";

/** Machine-readable reasons for a not-evaluated evals run. */
const EVALS_REASON_CODES = Object.freeze({
  JUDGE_NOT_STARTED: "judge_not_started",
  JUDGE_FAILED: "judge_failed",
  NO_ASSISTANT_RESPONSE: "no_assistant_response",
  EXECUTION_EVIDENCE_UNAVAILABLE: "execution_evidence_unavailable",
});

/**
 * @typedef {object} EvalsNotEvaluatedStatus
 * @property {"not_evaluated"} status
 * @property {string} reason_code
 * @property {string} reason
 * @property {string} [engine]
 * @property {string} [model]
 * @property {string} [run_id]
 * @property {string} [run_attempt]
 * @property {string} [timestamp]
 * @property {string} [execution_outcome]
 * @property {string} [execution_state]
 */

/**
 * Reads a not-evaluated marker. Returns null when the file is absent, malformed,
 * or does not describe a not-evaluated run.
 * @param {string} [filePath]
 * @returns {EvalsNotEvaluatedStatus | null}
 */
function readEvalsNotEvaluatedStatus(filePath = EVALS_STATUS_PATH) {
  if (!fs.existsSync(filePath)) {
    return null;
  }
  let record;
  try {
    record = JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch {
    return null;
  }
  if (!record || typeof record !== "object" || Array.isArray(record) || record.status !== EVALS_NOT_EVALUATED) {
    return null;
  }
  /** @type {EvalsNotEvaluatedStatus} */
  const status = {
    status: EVALS_NOT_EVALUATED,
    reason_code: typeof record.reason_code === "string" ? record.reason_code : "",
    reason: typeof record.reason === "string" ? record.reason : "",
  };
  for (const key of ["engine", "model", "run_id", "run_attempt", "timestamp", "execution_outcome", "execution_state"]) {
    if (typeof record[key] === "string" && record[key]) {
      status[key] = record[key];
    }
  }
  return status;
}

module.exports = { EVALS_NOT_EVALUATED, EVALS_REASON_CODES, readEvalsNotEvaluatedStatus };
