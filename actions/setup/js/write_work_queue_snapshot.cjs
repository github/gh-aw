// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const fs = require("fs");
const path = require("path");
const { serializeTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { MAX_SNAPSHOT_PARSE_BYTES, canonical } = require("./work_queue_codec.cjs");
const { readInboundWorkQueueAssignment, resolveWorkQueueRuntime } = require("./aw_context.cjs");
const { authenticatePublisher, validateNativeRun } = require("./work_queue_native.cjs");
const { bindWorkerAssignment, loadQueue, validateStoredAssignment, expectedWorkerRun } = require("./work_queue_binding.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

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
  if (runtime.role === "observer" && options.initializationContext !== undefined) throw new Error(`${SAFE_OUTPUT_E001}: work_queue_observer_read_only`);
  const readConfiguration = runtime.role === "observer" ? { ...configuration, policyProposal: undefined } : configuration;
  let latest = await loadQueue(readConfiguration);
  if (!latest.projection.policy && !(runtime.role !== "worker" && latest.sha === null && latest.transactions.length === 0)) throw new Error("work_queue_policy_missing");
  let worker = runtime.assignment ? validateStoredAssignment(latest.projection, runtime.assignment).assignment : null;
  let trustedContext;
  const staged = process.env.GH_AW_WORK_QUEUE_ISSUES !== undefined && isStagedMode(configuration);
  if (worker && !staged) {
    const admitted = await bindWorkerAssignment({ ...configuration, assignment: worker });
    worker = admitted.assignment;
    trustedContext = admitted.trustedContext;
    latest = await loadQueue(readConfiguration);
  } else {
    trustedContext = await authenticatePublisher({ ...configuration, role: worker ? "worker" : runtime.role === "observer" ? "producer" : "dispatcher", ...(worker ? { dispatch_id: worker.dispatch_id } : {}) });
    if (worker) validateNativeRun(trustedContext.native_run, expectedWorkerRun(worker, latest.projection.dispatches.get(worker.dispatch_id).profile, configuration.context, trustedContext.repository));
    latest = await loadQueue(readConfiguration);
  }
  if (process.env.GH_AW_WORK_QUEUE_ISSUES !== undefined) {
    await require("./work_queue_issues.cjs").main({ ...configuration, trustedContext });
    latest = await loadQueue(readConfiguration);
  }
  if (!latest.projection.policy && !(runtime.role !== "worker" && latest.sha === null && latest.transactions.length === 0)) throw new Error("work_queue_policy_missing");
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
  const encoded = `${JSON.stringify(snapshot)}\n`;
  if (Buffer.byteLength(encoded, "utf8") > MAX_SNAPSHOT_PARSE_BYTES) throw new TypeError("work queue snapshot exceeds its bounded input limit");
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, encoded, { mode: 0o444 });
  fs.chmodSync(outputPath, 0o444);
  configuration.core.setOutput?.("work_queue_origin", canonical(snapshot.origin));
  configuration.core.info(
    `Work queue activation: ${worker ? `${worker.claims.length} immutable Claims authenticated${staged ? " for staged preview (not bound)" : " and bound"}` : latest.sha === null ? "queue uninitialized; the first trusted producer submission can provision the protected branch and install its compiled Policy" : runtime.role === "observer" ? "read-only observer" : "unassigned queue-control context"}; snapshot captured`
  );
  return snapshot;
}

module.exports = { main, resolveWorkerAssignment };
