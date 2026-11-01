"use strict";

const EVALS_OUTPUT_PATH = "/tmp/gh-aw/evals.jsonl";
// Run-level marker written instead of evals.jsonl when the evals judge did not
// produce a usable response (it did not start, failed, or emitted no answer).
const EVALS_STATUS_FILENAME = "evals_status.json";
const EVALS_STATUS_PATH = `/tmp/gh-aw/${EVALS_STATUS_FILENAME}`;
const EVALS_EXECUTION_EVIDENCE_PATH = "/tmp/gh-aw/evals/execution.json";

module.exports = { EVALS_OUTPUT_PATH, EVALS_STATUS_FILENAME, EVALS_STATUS_PATH, EVALS_EXECUTION_EVIDENCE_PATH };
