// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { readCoordinatorLog } = require("./dispatch_work_coordinator_store.cjs");
const { canonicalJSON, replayTransactions, serializeTransactionLog } = require("./dispatch_work_coordinator_replay.cjs");
const { parseInboundAwContext } = require("./aw_context.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/dispatch-work-coordinator.snapshot.json";

/**
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function resolveWorkerAssignment(payload, transactions) {
  const awContext = parseInboundAwContext(payload?.inputs?.aw_context) || parseInboundAwContext(payload?.client_payload?.aw_context);
  const rawAssignment = awContext?.work_claim;
  if (rawAssignment == null) return null;
  if (!isRecord(rawAssignment)) {
    throw new TypeError("dispatch work coordinator assignment has an invalid shape");
  }
  const assignment = rawAssignment;
  if (
    typeof assignment.work_id !== "string" ||
    assignment.work_id.length === 0 ||
    typeof assignment.claim_id !== "string" ||
    assignment.claim_id.length === 0 ||
    !assignment.work ||
    typeof assignment.work !== "object" ||
    Array.isArray(assignment.work)
  ) {
    throw new TypeError("dispatch work coordinator assignment has an invalid shape");
  }

  const projection = replayTransactions(transactions);
  const claim = projection.transactions.find(transaction => transaction.kind === "Claim" && transaction.claim_id === assignment.claim_id);
  if (
    !claim ||
    claim.work_id !== assignment.work_id ||
    !Object.hasOwn(projection.work, assignment.work_id) ||
    projection.claim[assignment.claim_id] !== "effective" ||
    ["completed", "cancelled"].includes(projection.work[assignment.work_id])
  ) {
    throw new Error("dispatch work coordinator assignment is not currently effective");
  }

  const work = projection.transactions.find(transaction => transaction.kind === "Work" && transaction.work_id === assignment.work_id);
  if (!work || canonicalJSON(work.work) !== canonicalJSON(assignment.work)) {
    throw new Error("dispatch work coordinator assignment payload does not match queued Work");
  }
  return { work_id: assignment.work_id, claim_id: assignment.claim_id };
}

async function main(options = {}) {
  const githubClient = options.githubClient || github;
  const repositoryContext = options.context || context;
  const outputPath = options.snapshotPath || SNAPSHOT_PATH;
  const logger = options.core || core;
  const { sha, transactions } = await readCoordinatorLog({
    githubClient,
    owner: repositoryContext.repo.owner,
    repo: repositoryContext.repo.repo,
    publishUpgrades: false,
    core: logger,
  });
  const worker = resolveWorkerAssignment(repositoryContext.payload, transactions);
  logger.info(`Dispatch coordinator: worker assignment ${worker ? "admitted" : "absent"}`);
  const snapshot = {
    version: 2,
    sha,
    transactionLog: serializeTransactionLog(transactions),
    worker,
  };
  try {
    fs.mkdirSync(path.dirname(outputPath), { recursive: true });
    fs.writeFileSync(outputPath, `${JSON.stringify(snapshot)}\n`, { mode: 0o444 });
  } catch (error) {
    throw new Error(`Failed to write dispatch coordinator snapshot ${outputPath}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  logger.info(`Captured dispatch coordinator snapshot (${transactions.length} transactions${worker ? ", worker admitted" : ""})`);
}

module.exports = { main, resolveWorkerAssignment };
