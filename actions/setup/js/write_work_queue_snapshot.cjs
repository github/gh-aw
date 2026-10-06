// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { serializeTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { readInboundWorkQueueAssignment, resolveWorkQueueRuntime } = require("./aw_context.cjs");
const { authenticatePublisher } = require("./work_queue_native.cjs");
const { bindWorkerAssignment, loadQueue, validateStoredAssignment } = require("./work_queue_binding.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";

function resolveWorkerAssignment(payload, transactions) {
  const assignment = readInboundWorkQueueAssignment(payload);
  if (!assignment) return null;
  return validateStoredAssignment(replayTransactions(transactions), assignment).assignment;
}

async function main(options = {}) {
  const configuration = {
    ...options,
    githubClient: options.githubClient || github,
    context: options.context || context,
    core: options.core || core,
  };
  const outputPath = options.snapshotPath || process.env.GH_AW_WORK_QUEUE_SNAPSHOT || SNAPSHOT_PATH;
  const runtime = resolveWorkQueueRuntime(configuration.context.payload, { role: options.role, requireAssignment: options.requireAssignment });
  if (runtime.role === "observer" && configuration.initializationContext !== undefined) throw new Error("work_queue_observer_read_only");
  const readConfiguration = runtime.role === "observer" ? { ...configuration, policyProposal: undefined } : configuration;
  let latest = await loadQueue(readConfiguration);
  if (!latest.projection.policy && !(runtime.role === "observer" && latest.sha === null && latest.transactions.length === 0)) throw new Error("work_queue_policy_missing");
  let worker = runtime.assignment ? validateStoredAssignment(latest.projection, runtime.assignment).assignment : null;
  let trustedContext;
  if (worker) {
    const admitted = await bindWorkerAssignment({ ...configuration, assignment: worker });
    worker = admitted.assignment;
    trustedContext = admitted.trustedContext;
    latest = await loadQueue(readConfiguration);
  } else {
    trustedContext = await authenticatePublisher({ ...configuration, role: runtime.role === "observer" ? "producer" : "dispatcher" });
    latest = await loadQueue(readConfiguration);
  }
  const snapshot = {
    version: 3,
    sha: latest.sha,
    transactionLog: latest.transactions.length ? serializeTransactionLog(latest.transactions) : "",
    captured_at: options.now ?? Date.now(),
    origin: actorFromContext(trustedContext),
    role: runtime.role,
    worker,
    ...(options.visibleWorkIds === undefined ? {} : { visible_work_ids: options.visibleWorkIds }),
  };
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, `${JSON.stringify(snapshot)}\n`, { mode: 0o444 });
  fs.chmodSync(outputPath, 0o444);
  configuration.core.setOutput?.("work_queue_origin", canonical(snapshot.origin));
  configuration.core.info(`Work queue activation: ${worker ? `${worker.claims.length} immutable Claims authenticated and bound` : runtime.role === "observer" ? "read-only observer" : "unassigned queue-control context"}; snapshot captured`);
  return snapshot;
}

module.exports = { main, resolveWorkerAssignment };
