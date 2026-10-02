// @ts-check
"use strict";

const fs = require("fs");
const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { replayTransactions, parseTransactionLog } = require("./dispatch_work_coordinator_replay.cjs");

const DEFAULT_SNAPSHOT_PATH = "/tmp/gh-aw/dispatch-work-coordinator.snapshot.json";

function loadDispatchCoordinatorSnapshot(snapshotPath = process.env.GH_AW_DISPATCH_COORDINATOR_SNAPSHOT || DEFAULT_SNAPSHOT_PATH) {
  let snapshot;
  try {
    snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
  } catch (error) {
    throw new Error(`Failed to load dispatch coordinator snapshot ${snapshotPath}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  if (!snapshot || typeof snapshot !== "object" || snapshot.version !== 1 || (snapshot.sha !== null && (typeof snapshot.sha !== "string" || snapshot.sha.length === 0)) || typeof snapshot.transactionLog !== "string") {
    throw new TypeError("dispatch coordinator snapshot has an invalid shape");
  }

  const transactions = parseTransactionLog(snapshot.transactionLog);
  return Object.freeze({
    sha: snapshot.sha,
    projection: replayTransactions(transactions),
  });
}

function readDispatchCoordinatorState(snapshot, args = {}) {
  if (args.work !== undefined && (typeof args.work !== "string" || args.work.length === 0)) {
    throw new TypeError("work must be a non-empty string when provided");
  }

  const workIds = args.work === undefined ? Object.keys(snapshot.projection.work) : [args.work];
  const works = workIds.map(work => {
    if (!Object.hasOwn(snapshot.projection.work, work)) {
      return { id: work, state: "absent", winner: null, claims: [] };
    }
    const claims = snapshot.projection.transactions
      .filter(transaction => transaction.kind === "Claim" && transaction.work === work)
      .map(transaction => ({
        id: transaction.claim,
        state: Object.hasOwn(snapshot.projection.claim, transaction.claim) ? snapshot.projection.claim[transaction.claim] : "absent",
      }));
    return {
      id: work,
      state: snapshot.projection.work[work],
      winner: snapshot.projection.winner[work],
      claims,
    };
  });

  return { snapshot_sha: snapshot.sha, works };
}

function createDispatchCoordinatorStateTool(snapshot) {
  return {
    name: "dispatch_work_coordinator_read",
    description: "Read the immutable dispatch coordinator snapshot captured during workflow activation. This view may be stale during agent execution; safe-output processing rechecks authority before publishing changes.",
    inputSchema: {
      type: "object",
      properties: {
        work: { type: "string", minLength: 1, description: "Optional Work identifier to read." },
      },
      additionalProperties: false,
    },
    handler: args => readDispatchCoordinatorState(snapshot, args),
  };
}

function startDispatchCoordinatorServer(options = {}) {
  const snapshot = loadDispatchCoordinatorSnapshot(options.snapshotPath);
  const server = createServer({ name: "dispatch-work-coordinator", version: "1.0.0" }, { logDir: options.logDir || process.env.GH_AW_MCP_LOG_DIR });
  registerTool(server, createDispatchCoordinatorStateTool(snapshot));
  start(server);
}

if (require.main === module) {
  try {
    startDispatchCoordinatorServer();
  } catch (error) {
    console.error(`Error starting dispatch coordinator MCP server: ${error instanceof Error ? error.message : String(error)}`);
    process.exit(1);
  }
}

module.exports = {
  DEFAULT_SNAPSHOT_PATH,
  createDispatchCoordinatorStateTool,
  loadDispatchCoordinatorSnapshot,
  readDispatchCoordinatorState,
  startDispatchCoordinatorServer,
};
