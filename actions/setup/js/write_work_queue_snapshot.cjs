// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { readWorkQueueLog } = require("./work_queue_store.cjs");
const { replayTransactions, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { readInboundWorkQueueAssignment } = require("./aw_context.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";

function resolveWorkerAssignment(payload, transactions) {
  const assignment = readInboundWorkQueueAssignment(payload);
  if (!assignment) return null;

  const projection = replayTransactions(transactions);
  const claim = projection.transactions.find(transaction => transaction.kind === "Claim" && transaction.claim === assignment.claim_id);
  if (!claim || claim.work !== assignment.work_id || !Object.hasOwn(projection.work, assignment.work_id) || projection.claim[assignment.claim_id] !== "effective" || ["completed", "cancelled"].includes(projection.work[assignment.work_id])) {
    throw new Error("work queue assignment is not currently effective");
  }

  return { work_id: assignment.work_id, claim_id: assignment.claim_id };
}

async function main(options = {}) {
  const githubClient = options.githubClient || github;
  const repositoryContext = options.context || context;
  const outputPath = options.snapshotPath || SNAPSHOT_PATH;
  const logger = options.core || core;
  const { sha, transactions } = await readWorkQueueLog({
    githubClient,
    owner: repositoryContext.repo.owner,
    repo: repositoryContext.repo.repo,
    publishUpgrades: false,
    core: logger,
  });
  const worker = resolveWorkerAssignment(repositoryContext.payload, transactions);
  logger.info(`Work queue: worker assignment ${worker ? "admitted" : "absent"}`);
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
    throw new Error(`Failed to write work queue snapshot ${outputPath}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  logger.info(`Captured work queue snapshot (${transactions.length} transactions${worker ? ", worker admitted" : ""})`);
}

module.exports = { main, resolveWorkerAssignment };
