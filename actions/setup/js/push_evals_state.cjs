// @ts-check
/// <reference types="@actions/github-script" />

/**
 * push_evals_state — guards the evals state-branch push.
 *
 * The evals state branch holds answer history only. When the downloaded evals
 * artifact carries the run-level not-evaluated marker (evals_status.json), the
 * judge produced no answers, so nothing is pushed: no answer rows, no empty
 * file, and no new state branch. Otherwise the generic state writer
 * (push_experiment_state.cjs) pushes evals.jsonl unchanged.
 *
 * Environment variables:
 *   GH_AW_STATE_DIR - Directory containing the downloaded evals artifact
 *   (all other variables are consumed by push_experiment_state.cjs)
 */

"use strict";

const path = require("path");
const { EVALS_STATUS_FILENAME } = require("./evals_constants.cjs");
const { readEvalsNotEvaluatedStatus } = require("./evals_status.cjs");

/**
 * @param {{pushState?: () => Promise<void>}} [options]
 * @returns {Promise<void>}
 */
async function main({ pushState } = {}) {
  const stateDir = process.env.GH_AW_STATE_DIR || "";
  const status = stateDir ? readEvalsNotEvaluatedStatus(path.join(stateDir, EVALS_STATUS_FILENAME)) : null;
  if (status) {
    core.info(`Evals were not evaluated for this run (${status.reason_code || "no reason code"}); skipping the evals results push`);
    return;
  }
  const push = pushState ?? require("./push_experiment_state.cjs").main;
  await push();
}

module.exports = { main };
