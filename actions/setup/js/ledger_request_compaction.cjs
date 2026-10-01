// @ts-check
/// <reference types="@actions/github-script" />
// @safe-outputs-exempt SEC-005: requests are dispatched only to the Agentic Maintenance workflow in the current GitHub Actions repository context.
"use strict";

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

// ledger_request_compaction never compacts anything. It only asks Agentic Maintenance to
// consider compacting one configured ledger. Maintenance stays authoritative about whether the
// ledger exists, whether compaction is enabled, and whether the operation proceeds; the agent
// cannot supply compaction code or influence the trusted apply stage.

const { getErrorMessage } = require("./error_helpers.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "ledger_request_compaction";
const LEDGER_NAME = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const MAINTENANCE_WORKFLOW = /^[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.ya?ml$/;

/** @type {HandlerFactoryFunction} */
async function main(config = {}) {
  const ledgers = Array.isArray(config.ledgers) ? config.ledgers.filter(name => typeof name === "string" && LEDGER_NAME.test(name)) : [];
  if (!ledgers.length) throw new TypeError("ledger_request_compaction has no compaction-enabled ledgers");
  const workflow = typeof config.workflow === "string" && MAINTENANCE_WORKFLOW.test(config.workflow) ? config.workflow : "agentics-maintenance.yml";
  const maxCount = typeof config.max === "number" && Number.isSafeInteger(config.max) && config.max > 0 ? config.max : 1;
  const isStaged = isStagedMode(config);
  const requested = new Set();
  let processedCount = 0;

  return async function handleLedgerRequestCompaction(message) {
    const rawName = message && message.ledger !== undefined && message.ledger !== null ? message.ledger : ledgers.length === 1 ? ledgers[0] : "";
    if (typeof rawName !== "string" || !rawName.trim()) {
      return { success: false, error: `ledger is required when more than one ledger is configured (${ledgers.join(", ")})` };
    }
    const ledger = rawName.trim();
    if (!ledgers.includes(ledger)) {
      return { success: false, error: `Ledger "${ledger}" is not a compaction-enabled ledger of this workflow` };
    }
    if (requested.has(ledger)) {
      return { success: true, skipped: true, ledger, reason: "compaction already requested for this ledger in this run" };
    }
    if (processedCount >= maxCount) {
      core.warning(`Skipping ${HANDLER_TYPE}: max count of ${maxCount} reached`);
      return { success: false, error: `Max count of ${maxCount} reached` };
    }
    processedCount++;
    requested.add(ledger);

    const defaultBranch = (context.payload && context.payload.repository && context.payload.repository.default_branch) || "";
    let ref = defaultBranch ? `refs/heads/${defaultBranch}` : "";
    if (isStaged) {
      logStagedPreviewInfo(`Would request Agentic Maintenance compaction for ledger ${ledger} via ${workflow}`);
      return { success: true, staged: true, ledger };
    }
    try {
      if (!ref) {
        const { data } = await github.rest.repos.get({ owner: context.repo.owner, repo: context.repo.repo });
        ref = `refs/heads/${data.default_branch}`;
      }
      await github.rest.actions.createWorkflowDispatch({
        owner: context.repo.owner,
        repo: context.repo.repo,
        workflow_id: workflow,
        ref,
        inputs: { operation: "compact_ledger", ledger },
      });
      core.info(`Requested Agentic Maintenance compaction for ledger ${ledger}`);
      return { success: true, ledger, workflow };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.warning(`Failed to request compaction for ledger ${ledger}: ${errorMessage}`);
      return { success: false, error: `Failed to request compaction for ledger ${ledger}: ${errorMessage}` };
    }
  };
}

module.exports = { main };
