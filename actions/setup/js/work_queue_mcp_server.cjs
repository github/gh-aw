// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { replayTransactions, parseTransactionLog } = require("./work_queue_replay.cjs");

const DEFAULT_SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";
const DEFAULT_FINISH_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "work-queue", "work-queue.finish.jsonl");

function loadWorkQueueSnapshot(snapshotPath = process.env.GH_AW_WORK_QUEUE_SNAPSHOT || DEFAULT_SNAPSHOT_PATH) {
  let snapshot;
  try {
    snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
  } catch (error) {
    throw new Error(`Failed to load work queue snapshot ${snapshotPath}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  if (
    !snapshot ||
    typeof snapshot !== "object" ||
    snapshot.version !== 2 ||
    (snapshot.sha !== null && (typeof snapshot.sha !== "string" || snapshot.sha.length === 0)) ||
    typeof snapshot.transactionLog !== "string" ||
    (snapshot.worker !== null && (!snapshot.worker || typeof snapshot.worker !== "object" || typeof snapshot.worker.work_id !== "string" || typeof snapshot.worker.claim_id !== "string"))
  ) {
    throw new TypeError("work queue snapshot has an invalid shape");
  }

  const transactions = parseTransactionLog(snapshot.transactionLog);
  return Object.freeze({
    sha: snapshot.sha,
    worker: snapshot.worker,
    projection: replayTransactions(transactions),
  });
}

function validateSort(sort) {
  if (sort === undefined) return;
  if (!Array.isArray(sort) || sort.length === 0 || sort.length > 4) {
    throw new TypeError("sort must contain between one and four sort operators");
  }
  for (const term of sort) {
    if (!term || typeof term !== "object" || Array.isArray(term) || Object.keys(term).some(key => !["key", "direction"].includes(key)) || !["id", "enqueued", "id_length"].includes(term.key) || !["asc", "desc"].includes(term.direction)) {
      throw new TypeError("each sort operator must have a supported key and an asc or desc direction");
    }
  }
}

function readWorkQueueState(snapshot, args = {}) {
  if (args.work !== undefined && (typeof args.work !== "string" || args.work.length === 0)) {
    throw new TypeError("work must be a non-empty string when provided");
  }
  validateSort(args.sort);

  const enqueuedByWork = new Map();
  for (const transaction of snapshot.projection.transactions) {
    if (transaction.kind === "Work" && !enqueuedByWork.has(transaction.work)) {
      enqueuedByWork.set(transaction.work, transaction.enqueued ?? 0);
    }
  }
  const available = [...snapshot.projection.available];
  if (args.sort) {
    const defaultOrder = new Map(available.map((id, index) => [id, index]));
    available.sort((left, right) => {
      for (const { key, direction } of args.sort) {
        const a = key === "id" ? left : key === "enqueued" ? (enqueuedByWork.get(left) ?? 0) : [...left].length;
        const b = key === "id" ? right : key === "enqueued" ? (enqueuedByWork.get(right) ?? 0) : [...right].length;
        const comparison = typeof a === "string" ? Buffer.compare(Buffer.from(a, "utf8"), Buffer.from(b, "utf8")) : a < b ? -1 : a > b ? 1 : 0;
        if (comparison) return direction === "asc" ? comparison : -comparison;
      }
      return (defaultOrder.get(left) ?? 0) - (defaultOrder.get(right) ?? 0);
    });
  }
  const workIds = args.work === undefined ? [...available, ...Object.keys(snapshot.projection.work).filter(work => !snapshot.projection.available.includes(work))] : [args.work];
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
      enqueued: enqueuedByWork.get(work) ?? 0,
      winner: snapshot.projection.winner[work],
      claims,
    };
  });

  return { snapshot_sha: snapshot.sha, next_work: available[0] ?? null, works };
}

function createWorkQueueStateTool(snapshot) {
  return {
    name: "work_queue_read",
    description:
      "Read the immutable work queue snapshot captured during workflow activation. Available Work is oldest-first by default; optional sort operators reorder available Work and next_work. This view may be stale during agent execution; safe-output processing rechecks authority without enforcing the requested order.",
    inputSchema: {
      type: "object",
      properties: {
        work: { type: "string", minLength: 1, description: "Optional Work identifier to read." },
        sort: {
          type: "array",
          minItems: 1,
          maxItems: 4,
          description: 'Optional ordered sort keys (1–4), e.g. [{"key":"enqueued","direction":"desc"}]. Keys are id, enqueued, and id_length. Ties retain oldest-first order.',
          items: {
            type: "object",
            properties: {
              key: { type: "string", enum: ["id", "enqueued", "id_length"] },
              direction: { type: "string", enum: ["asc", "desc"] },
            },
            required: ["key", "direction"],
            additionalProperties: false,
          },
        },
      },
      additionalProperties: false,
    },
    handler: args => {
      console.error("[work-queue] Reading queue snapshot");
      return { content: [{ type: "text", text: JSON.stringify(readWorkQueueState(snapshot, args)) }] };
    },
  };
}

function createWorkQueueFinishTool(options = {}) {
  const outputPath = options.finishIntentPath || process.env.GH_AW_WORK_QUEUE_FINISH_INTENT || DEFAULT_FINISH_INTENT_PATH;
  return {
    name: "work_queue_claim_finish",
    description: "Record the finish intent for the trusted inbound work queue claim. The claim identity is supplied by workflow context and cannot be selected or changed here.",
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
      try {
        fs.mkdirSync(path.dirname(outputPath), { recursive: true });
        fs.appendFileSync(outputPath, `${JSON.stringify({ outcome })}\n`, { encoding: "utf8" });
        // The MCP container and runner artifact collector can run as different users.
        fs.chmodSync(outputPath, 0o644);
      } catch (error) {
        throw new Error("Failed to record work queue finish intent", { cause: error });
      }
      console.error(`[work-queue] Recorded ${outcome} finish intent`);
      return { content: [{ type: "text", text: JSON.stringify({ recorded: true, outcome }) }] };
    },
  };
}

function startWorkQueueServer(options = {}) {
  const snapshot = loadWorkQueueSnapshot(options.snapshotPath);
  console.error(`[work-queue] Loaded queue snapshot with ${snapshot.projection.transactions.length} transactions; worker ${snapshot.worker ? "assigned" : "absent"}`);
  const server = createServer({ name: "work-queue", version: "1.0.0" }, { logDir: options.logDir || process.env.GH_AW_MCP_LOG_DIR });
  registerTool(server, createWorkQueueStateTool(snapshot));
  registerTool(server, createWorkQueueFinishTool(options));
  start(server);
}

if (require.main === module) {
  try {
    startWorkQueueServer();
  } catch (error) {
    console.error(`Error starting work queue MCP server: ${error instanceof Error ? error.message : String(error)}`);
    process.exit(1);
  }
}

module.exports = {
  DEFAULT_SNAPSHOT_PATH,
  DEFAULT_FINISH_INTENT_PATH,
  createWorkQueueStateTool,
  createWorkQueueFinishTool,
  loadWorkQueueSnapshot,
  readWorkQueueState,
  startWorkQueueServer,
};
