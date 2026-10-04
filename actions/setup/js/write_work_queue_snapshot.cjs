// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { readWorkQueueLog } = require("./work_queue_store.cjs");
const { replayTransactions, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { readInboundAwContext, readWorkQueueAssignment } = require("./aw_context.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";

function resolveWorkerAssignment(payload, transactions) {
  const assignment = readWorkQueueAssignment(readInboundAwContext(payload));
  if (!assignment) return null;

  const projection = replayTransactions(transactions);
  const validate = worker => {
    const claim = projection.transactions.find(transaction => transaction.kind === "Claim" && transaction.claim === worker.claim_id);
    if (!claim || claim.work !== worker.work_id || !Object.hasOwn(projection.work, worker.work_id) || projection.claim[worker.claim_id] !== "effective" || ["completed", "cancelled"].includes(projection.work[worker.work_id])) {
      throw new Error("work queue assignment is not currently effective");
    }
    return { work_id: worker.work_id, claim_id: worker.claim_id };
  };
  return Array.isArray(assignment) ? assignment.map(validate) : validate(assignment);
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
