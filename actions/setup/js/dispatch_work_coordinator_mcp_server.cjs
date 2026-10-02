// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { replayTransactions, parseTransactionLog } = require("./dispatch_work_coordinator_replay.cjs");

const DEFAULT_SNAPSHOT_PATH = "/tmp/gh-aw/dispatch-work-coordinator.snapshot.json";
const DEFAULT_FINISH_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "dispatch-coordinator", "dispatch-work-coordinator.finish.jsonl");

function loadDispatchCoordinatorSnapshot(snapshotPath = process.env.GH_AW_DISPATCH_COORDINATOR_SNAPSHOT || DEFAULT_SNAPSHOT_PATH) {
  let snapshot;
  try {
    snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
  } catch (error) {
    throw new Error(`Failed to load dispatch coordinator snapshot ${snapshotPath}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  if (
    !snapshot ||
    typeof snapshot !== "object" ||
    snapshot.version !== 2 ||
    (snapshot.sha !== null && (typeof snapshot.sha !== "string" || snapshot.sha.length === 0)) ||
    typeof snapshot.transactionLog !== "string" ||
    (snapshot.worker !== null && (!snapshot.worker || typeof snapshot.worker !== "object" || typeof snapshot.worker.work_id !== "string" || typeof snapshot.worker.claim_id !== "string"))
  ) {
    throw new TypeError("dispatch coordinator snapshot has an invalid shape");
  }

  const transactions = parseTransactionLog(snapshot.transactionLog);
  return Object.freeze({
    sha: snapshot.sha,
    worker: snapshot.worker,
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

function createDispatchCoordinatorFinishTool(options = {}) {
  const outputPath = options.finishIntentPath || process.env.GH_AW_DISPATCH_COORDINATOR_FINISH_INTENT || DEFAULT_FINISH_INTENT_PATH;
  return {
    name: "dispatch_claim_finish",
    description: "Record the finish intent for the trusted inbound dispatch work claim. The claim identity is supplied by workflow context and cannot be selected or changed here.",
    inputSchema: {
      type: "object",
      properties: {
        outcome: {
          type: "string",
          enum: ["completed", "cancelled"],
          description: "Complete the claim (default) or cancel it so another claim may proceed.",
        },
      },
      additionalProperties: false,
    },
    handler: args => {
      const outcome = args.outcome === undefined ? "completed" : args.outcome;
      if (!["completed", "cancelled"].includes(outcome)) {
        throw new TypeError("outcome must be completed or cancelled");
      }
      fs.mkdirSync(path.dirname(outputPath), { recursive: true });
      fs.appendFileSync(outputPath, `${JSON.stringify({ outcome })}\n`, { encoding: "utf8", mode: 0o600 });
      return { recorded: true, outcome };
    },
  };
}

function startDispatchCoordinatorServer(options = {}) {
  const snapshot = loadDispatchCoordinatorSnapshot(options.snapshotPath);
  const server = createServer({ name: "work-queue", version: "1.0.0" }, { logDir: options.logDir || process.env.GH_AW_MCP_LOG_DIR });
  registerTool(server, createDispatchCoordinatorStateTool(snapshot));
  registerTool(server, createDispatchCoordinatorFinishTool(options));
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
  DEFAULT_FINISH_INTENT_PATH,
  createDispatchCoordinatorStateTool,
  createDispatchCoordinatorFinishTool,
  loadDispatchCoordinatorSnapshot,
  readDispatchCoordinatorState,
  startDispatchCoordinatorServer,
};
