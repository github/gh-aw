// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { readCoordinatorLog } = require("./dispatch_work_coordinator_store.cjs");
const { serializeTransactionLog } = require("./dispatch_work_coordinator_replay.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/dispatch-work-coordinator.snapshot.json";

async function main(options = {}) {
  const githubClient = options.githubClient || github;
  const repositoryContext = options.context || context;
  const outputPath = options.snapshotPath || SNAPSHOT_PATH;
  const logger = options.core || core;
  const { sha, transactions } = await readCoordinatorLog({
    githubClient,
    owner: repositoryContext.repo.owner,
    repo: repositoryContext.repo.repo,
  });
  const snapshot = {
    version: 1,
    sha,
    transactionLog: serializeTransactionLog(transactions),
  };
  try {
    fs.mkdirSync(path.dirname(outputPath), { recursive: true });
    fs.writeFileSync(outputPath, `${JSON.stringify(snapshot)}\n`, { mode: 0o444 });
  } catch (error) {
    throw new Error(`Failed to write dispatch coordinator snapshot ${outputPath}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  logger.info(`Captured dispatch coordinator snapshot (${transactions.length} transactions)`);
}

module.exports = { main };
