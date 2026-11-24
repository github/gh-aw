// @ts-check
"use strict";

const { queueControlErrorExplanation } = require("./work_queue_logging.cjs");

/**
 * @typedef {{dispatch_id: string, state: "started" | "launch_failed", reason?: string}} LaunchReceipt
 * @typedef {{intent_id?: string, request_id?: string, commit_id?: string, status: "committed" | "blocked", reason?: string, dispatches?: number, launches?: LaunchReceipt[]}} ControlReceipt
 * @typedef {{version: 4, success: boolean, status: "committed" | "blocked", receipts: ControlReceipt[], remaining_dispatches: number, summary: string}} ControlResult
 */

/**
 * A blocked receipt for an intent that committed nothing.
 * @param {string} reason
 * @param {string} [intentId]
 * @returns {ControlReceipt}
 */
function blockedReceipt(reason, intentId = undefined) {
  return /** @type {ControlReceipt} */ { ...(intentId === undefined ? {} : { intent_id: intentId }), status: "blocked", reason };
}

/**
 * The `work_queue_requests` output. Only blocked intents fail the step; a failed
 * launch returned its Work to the pool and is a successful control outcome.
 * @param {ControlReceipt[]} receipts
 * @param {number} remaining
 * @returns {ControlResult}
 */
function controlResult(receipts, remaining) {
  const blocked = receipts.filter(receipt => receipt.status === "blocked");
  const launches = receipts.flatMap(receipt => receipt.launches ?? []);
  const started = launches.filter(launch => launch.state === "started").length;
  const failed = launches.length - started;
  const summary = [
    "## Work queue controls",
    "",
    `Status: **${blocked.length ? "Blocked" : "Committed"}**`,
    "",
    `- Committed requests: ${receipts.length - blocked.length}`,
    `- Blocked intents: ${blocked.length}`,
    `- Started launches: ${started}`,
    `- Failed launches returned to the pool: ${failed}`,
    ...blocked.map(receipt => `- Blocked (\`${receipt.reason}\`): ${queueControlErrorExplanation(receipt.reason ?? "")}`),
    ...launches.filter(launch => launch.state === "launch_failed").map(launch => `- Launch of \`${launch.dispatch_id}\` failed (\`${launch.reason}\`); its Work is available again.`),
  ].join("\n");
  return { version: 4, success: blocked.length === 0, status: blocked.length ? "blocked" : "committed", receipts, remaining_dispatches: remaining, summary };
}

module.exports = { blockedReceipt, controlResult };
