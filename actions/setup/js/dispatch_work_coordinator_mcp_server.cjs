// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { randomUUID } = require("crypto");
const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { replayTransactions, parseTransactionLog } = require("./dispatch_work_coordinator_replay.cjs");
const { SELECTION_SCHEMA, selectNext, validateSelection } = require("./dispatch_work_coordinator_selection.cjs");
const { CURRENT_VERSION } = require("./dispatch_work_coordinator_codemods.cjs");
const { readClaimIntents } = require("./publish_dispatch_work_claims.cjs");

const DEFAULT_SNAPSHOT_PATH = "/tmp/gh-aw/dispatch-work-coordinator.snapshot.json";
const DEFAULT_FINISH_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "dispatch-coordinator", "dispatch-work-coordinator.finish.jsonl");
const DEFAULT_CLAIM_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "dispatch-coordinator", "dispatch-work-coordinator.claims.jsonl");

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

function createDispatchCoordinatorClaimNextTool(snapshot, options = {}) {
  const outputPath = options.claimIntentPath || process.env.GH_AW_DISPATCH_COORDINATOR_CLAIM_INTENT || DEFAULT_CLAIM_INTENT_PATH;
  const pending = readClaimIntents(outputPath).map(intent => ({ version: CURRENT_VERSION, kind: "Claim", work_id: intent.work_id, claim_id: intent.claim_id, run_id: "pending" }));
  return {
    name: "dispatch_claim_next",
    description:
      "Stage a claim for the next available Work. Defaults to FIFO. Use a declarative payload-field selection to filter, sort, and limit active work per group. The result is pending, not authority: trusted safe outputs recheck selection and publish it or fail closed if stale. Pass claim_id as dispatch-workflow inputs.work_queue_claim_id.",
    inputSchema: {
      type: "object",
      properties: { selection: SELECTION_SCHEMA },
      additionalProperties: false,
    },
    handler: args => {
      if (!args || typeof args !== "object" || Array.isArray(args) || Object.keys(args).some(key => key !== "selection")) throw new TypeError("claimNext accepts only selection");
      const selection = structuredClone(validateSelection(args.selection === undefined ? {} : args.selection));
      if (pending.length >= 100) throw new RangeError("at most 100 pending claims may be staged");
      const work = selectNext([...snapshot.projection.transactions, ...pending], selection);
      if (!work) return { snapshot_sha: snapshot.sha, work: null, pending: false };
      const claim = { version: CURRENT_VERSION, kind: "Claim", work_id: work.work_id, claim_id: randomUUID(), run_id: "pending" };
      appendCoordinatorIntent(outputPath, { work_id: claim.work_id, claim_id: claim.claim_id, selection });
      pending.push(claim);
      console.error("[dispatch-work-coordinator] Recorded pending next-work claim");
      return { snapshot_sha: snapshot.sha, work_id: work.work_id, claim_id: claim.claim_id, work: work.work, pending: true };
    },
  };
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
      .filter(transaction => transaction.kind === "Claim" && transaction.work_id === work)
      .map(transaction => ({
        id: transaction.claim_id,
        state: Object.hasOwn(snapshot.projection.claim, transaction.claim_id) ? snapshot.projection.claim[transaction.claim_id] : "absent",
      }));
    const workTransaction = snapshot.projection.transactions.find(transaction => transaction.kind === "Work" && transaction.work_id === work);
    if (!workTransaction) throw new Error("dispatch coordinator projection is missing Work");
    return {
      id: work,
      work: workTransaction.work,
      sequence: workTransaction.sequence,
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
    handler: args => {
      console.error("[dispatch-work-coordinator] Reading queue snapshot");
      return readDispatchCoordinatorState(snapshot, args);
    },
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
      appendCoordinatorIntent(outputPath, { outcome });
      console.error(`[dispatch-work-coordinator] Recorded ${outcome} finish intent`);
      return { recorded: true, outcome };
    },
  };
}

function appendCoordinatorIntent(outputPath, intent) {
  try {
    fs.mkdirSync(path.dirname(outputPath), { recursive: true });
    fs.appendFileSync(outputPath, `${JSON.stringify(intent)}\n`, { encoding: "utf8", mode: 0o600 });
  } catch (error) {
    throw new Error("Failed to record dispatch coordinator intent", { cause: error });
  }
}

function startDispatchCoordinatorServer(options = {}) {
  const snapshot = loadDispatchCoordinatorSnapshot(options.snapshotPath);
  console.error(`[dispatch-work-coordinator] Loaded queue snapshot with ${snapshot.projection.transactions.length} transactions; worker ${snapshot.worker ? "assigned" : "absent"}`);
  const server = createServer({ name: "work-queue", version: "1.0.0" }, { logDir: options.logDir || process.env.GH_AW_MCP_LOG_DIR });
  registerTool(server, createDispatchCoordinatorStateTool(snapshot));
  registerTool(server, createDispatchCoordinatorClaimNextTool(snapshot, options));
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
  DEFAULT_CLAIM_INTENT_PATH,
  createDispatchCoordinatorClaimNextTool,
  createDispatchCoordinatorStateTool,
  createDispatchCoordinatorFinishTool,
  loadDispatchCoordinatorSnapshot,
  readDispatchCoordinatorState,
  startDispatchCoordinatorServer,
};
