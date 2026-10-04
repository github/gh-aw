// @ts-check
"use strict";

const fs = require("fs");
const { applyAndPublishWorkQueueTransactions, readWorkQueueLog } = require("./work_queue_store.cjs");
const { CURRENT_VERSION } = require("./work_queue_codemods.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { buildWorkflowCallId } = require("./aw_context.cjs");
const { AGENT_OUTPUT_FILENAME, TMP_GH_AW_PATH } = require("./constants.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";
const FINISH_INTENT_PATH = "/tmp/gh-aw/work-queue.finish.jsonl";
const SAFE_OUTPUTS_PATH = "/tmp/gh-aw/safeoutputs.jsonl";

function readWorkerSnapshot(snapshotPath = process.env.GH_AW_WORK_QUEUE_SNAPSHOT || SNAPSHOT_PATH) {
  const snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
  if (
    !snapshot ||
    typeof snapshot !== "object" ||
    snapshot.version !== 2 ||
    (snapshot.worker !== null && !(Array.isArray(snapshot.worker) ? snapshot.worker.length > 0 && snapshot.worker.every(validWorker) : validWorker(snapshot.worker)))
  ) {
    throw new TypeError("work queue snapshot has an invalid worker assignment");
  }
  return snapshot.worker;
}

function validWorker(worker) {
  return worker && typeof worker === "object" && typeof worker.work_id === "string" && worker.work_id.length > 0 && typeof worker.claim_id === "string" && worker.claim_id.length > 0;
}

function readFinishIntent(finishIntentPath = process.env.GH_AW_WORK_QUEUE_FINISH_INTENT || FINISH_INTENT_PATH, workers = null) {
  if (!fs.existsSync(finishIntentPath)) return null;
  const outcomes = new Set();
  const perClaim = new Map();
  const contents = fs.readFileSync(finishIntentPath, "utf8");
  for (const [index, line] of contents.split("\n").entries()) {
    if (!line.trim()) continue;
    let intent;
    try {
      intent = JSON.parse(line);
    } catch {
      throw new TypeError(`work queue claim finish intent line ${index + 1} is malformed`);
    }
    const multiple = Array.isArray(workers);
    if (
      !intent ||
      typeof intent !== "object" ||
      Array.isArray(intent) ||
      Object.keys(intent).length !== (multiple ? 2 : 1) ||
      !Object.hasOwn(intent, "outcome") ||
      !["completed", "cancelled"].includes(intent.outcome) ||
      (multiple && (typeof intent.claim_id !== "string" || !workers.some(worker => worker.claim_id === intent.claim_id)))
    ) {
      throw new TypeError(`work queue claim finish intent line ${index + 1} is invalid`);
    }
    if (multiple) {
      if (perClaim.has(intent.claim_id) && perClaim.get(intent.claim_id) !== intent.outcome) throw new TypeError("work queue claim finish intents conflict");
      perClaim.set(intent.claim_id, intent.outcome);
    } else {
      outcomes.add(intent.outcome);
    }
  }
  if (Array.isArray(workers)) return perClaim;
  if (outcomes.size > 1) throw new TypeError("work queue claim finish intents conflict");
  return outcomes.size === 0 ? null : [...outcomes][0];
}

function renderSummary(status) {
  const labels = {
    unassigned: "No worker claim was assigned; safe outputs may proceed.",
    completed: "The effective worker claim was durably completed and verified; safe outputs may proceed.",
    cancelled: "The worker claim was cancelled; ordinary safe outputs were skipped.",
    superseded: "The worker claim is no longer effective; ordinary safe outputs were skipped.",
    terminal: "The work is already terminal; ordinary safe outputs were skipped.",
    failed: "Claim reconciliation failed; ordinary safe outputs were blocked.",
  };
  const description = labels[status] || labels.failed;
  return `## Work queue reconciliation\n\n<details>\n<summary>Show claim reconciliation</summary>\n\n${description}\n\n</details>\n`;
}

async function reconcileSingleClaim(worker, finishIntent, options, initial, attempt, coreApi, githubClient, owner, repo, readLog) {
  const projection = replayTransactions(initial.transactions);
  const claim = initial.transactions.find(transaction => transaction.kind === "Claim" && transaction.claim === worker.claim_id);
  if (!claim || claim.work !== worker.work_id || !Object.hasOwn(projection.work, worker.work_id) || !Object.hasOwn(projection.claim, worker.claim_id)) {
    coreApi?.info("Work queue: worker claim is no longer present in the queue");
    return { authorized: false, status: "superseded" };
  }

  const existingCompletion = initial.transactions.find(transaction => transaction.kind === "Completion" && transaction.work === worker.work_id && transaction.claim === worker.claim_id && transaction.attempt === attempt);
  if (existingCompletion && projection.work[worker.work_id] === "completed" && projection.winner[worker.work_id] === worker.claim_id) {
    coreApi?.info("Work queue: worker completion already verified");
    return { authorized: true, status: "completed" };
  }

  if (projection.claim[worker.claim_id] !== "effective") {
    coreApi?.info(`Work queue: worker claim is ${projection.claim[worker.claim_id]}`);
    if (projection.claim[worker.claim_id] === "cancelled") return { authorized: false, status: "cancelled" };
    return { authorized: false, status: ["completed", "cancelled"].includes(projection.work[worker.work_id]) ? "terminal" : "superseded" };
  }

  const cancel = finishIntent === null || finishIntent === "cancelled";
  const intent = cancel
    ? { version: CURRENT_VERSION, kind: "ClaimCancellation", work: worker.work_id, claim: worker.claim_id, attempt: null }
    : { version: CURRENT_VERSION, kind: "Completion", work: worker.work_id, claim: worker.claim_id, attempt };
  const publish = options.applyAndPublish || applyAndPublishWorkQueueTransactions;
  coreApi?.info(`Work queue: publishing worker ${cancel ? "cancellation" : "completion"}`);
  await publish({ githubClient, owner, repo, intents: [intent], core: coreApi });

  const latest = await readLog({ githubClient, owner, repo, core: coreApi });
  const verified = replayTransactions(latest.transactions);
  coreApi?.info(`Work queue: verifying worker against ${latest.transactions.length} queue transactions`);
  if (
    !cancel &&
    latest.transactions.some(transaction => transaction.kind === "Completion" && transaction.work === worker.work_id && transaction.claim === worker.claim_id && transaction.attempt === attempt) &&
    verified.work[worker.work_id] === "completed" &&
    verified.winner[worker.work_id] === worker.claim_id
  ) {
    coreApi?.info("Work queue: worker completion verified");
    return { authorized: true, status: "completed" };
  }
  if (cancel && latest.transactions.some(transaction => transaction.kind === "ClaimCancellation" && transaction.work === worker.work_id && transaction.claim === worker.claim_id) && verified.claim[worker.claim_id] === "cancelled") {
    coreApi?.info("Work queue: worker cancellation verified");
    return { authorized: false, status: "cancelled" };
  }
  coreApi?.info("Work queue: worker reconciliation did not verify the intended queue state");
  return { authorized: false, status: ["completed", "cancelled"].includes(verified.work[worker.work_id]) ? "terminal" : "superseded" };
}

function filterClaimOutputs(completed, allCompleted, outputPath) {
  if (!fs.existsSync(outputPath)) return;
  const output = JSON.parse(fs.readFileSync(outputPath, "utf8"));
  if (!Array.isArray(output.items)) throw new TypeError("agent output has an invalid shape");
  output.items = output.items
    .filter(item => {
      if (item.claim_id === undefined) return allCompleted;
      return typeof item.claim_id === "string" && completed.has(item.claim_id);
    })
    .map(item => {
      const { claim_id, ...rest } = item;
      return rest;
    });
  fs.writeFileSync(outputPath, JSON.stringify(output));
}

async function reconcileWorkerClaim(options = {}) {
  const coreApi = options.core || (typeof core === "undefined" ? undefined : core);
  const worker = options.worker === undefined ? readWorkerSnapshot(options.snapshotPath) : options.worker;
  const githubClient = options.githubClient || (typeof github === "undefined" ? undefined : github);
  const repositoryContext = options.context || (typeof context === "undefined" ? undefined : context);
  if (!repositoryContext?.repo) throw new Error("GitHub repository context is unavailable");
  const owner = repositoryContext.repo.owner;
  const repo = repositoryContext.repo.repo;
  const readLog = options.readWorkQueueLog || readWorkQueueLog;

  if (!worker) {
    coreApi?.info("Work queue: no inbound worker claim; skipping queue reconciliation");
    filterClaimOutputs(new Set(), true, options.agentOutputPath || `${TMP_GH_AW_PATH}/${AGENT_OUTPUT_FILENAME}`);
    return { authorized: true, status: "unassigned" };
  }

  const multiple = Array.isArray(worker);
  const workers = multiple ? worker : [worker];
  if (!workers.length || workers.some(item => !validWorker(item)) || new Set(workers.map(item => item.claim_id)).size !== workers.length) throw new TypeError("work queue snapshot has an invalid worker assignment");
  const finishIntent = readFinishIntent(options.finishIntentPath, multiple ? workers : null);
  coreApi?.info(`Work queue: worker finish intent ${multiple ? "per-claim" : finishIntent || "absent"}`);
  const initial = await readLog({ githubClient, owner, repo, core: coreApi });
  coreApi?.info(`Work queue: rechecking worker against ${initial.transactions.length} queue transactions`);
  const runId = String(repositoryContext.runId ?? process.env.GITHUB_RUN_ID ?? "").trim();
  const attempt = buildWorkflowCallId(runId, process.env.GITHUB_RUN_ATTEMPT || "1", process.env.GITHUB_WORKFLOW_REF || "");
  if (!attempt) throw new Error("current workflow attempt identity is unavailable");

  if (!multiple) {
    const result = await reconcileSingleClaim(worker, finishIntent, options, initial, attempt, coreApi, githubClient, owner, repo, readLog);
    if (result.authorized) filterClaimOutputs(new Set([worker.claim_id]), true, options.agentOutputPath || `${TMP_GH_AW_PATH}/${AGENT_OUTPUT_FILENAME}`);
    return result;
  }
  // An omitted finish cancels the entire batch, including claims that have an explicit completion intent.
  const completeBatch = finishIntent !== null && workers.every(item => finishIntent.has(item.claim_id));
  const results = [];
  for (const item of workers) {
    const outcome = completeBatch ? finishIntent.get(item.claim_id) : "cancelled";
    results.push(await reconcileSingleClaim(item, outcome, options, await readLog({ githubClient, owner, repo, core: coreApi }), attempt, coreApi, githubClient, owner, repo, readLog));
  }
  const completed = new Set(workers.filter((_, index) => results[index].authorized).map(item => item.claim_id));
  const allCompleted = completed.size === workers.length;
  const authorized = completeBatch && completed.size > 0;
  if (authorized) filterClaimOutputs(completed, allCompleted, options.agentOutputPath || `${TMP_GH_AW_PATH}/${AGENT_OUTPUT_FILENAME}`);
  return { authorized, allAuthorized: completeBatch && allCompleted, status: !completeBatch ? "cancelled" : allCompleted ? "completed" : authorized ? "partial" : "cancelled" };
}

async function main(options = {}) {
  const coreApi = options.core || core;
  try {
    const result = await reconcileWorkerClaim(options);
    coreApi.setOutput("authorized", String(result.authorized));
    coreApi.setOutput("all_authorized", String(result.allAuthorized ?? result.authorized));
    coreApi.info(`Work queue claim reconciliation: ${result.status}`);
    await coreApi.summary.addRaw(renderSummary(result.status)).write();
    return result;
  } catch {
    coreApi.setOutput("authorized", "false");
    coreApi.info("Work queue claim reconciliation failed; ordinary safe outputs are blocked.");
    await coreApi.summary.addRaw(renderSummary("failed")).write();
    throw new Error("Work queue claim reconciliation failed; ordinary safe outputs are blocked");
  }
}

module.exports = {
  FINISH_INTENT_PATH,
  SAFE_OUTPUTS_PATH,
  SNAPSHOT_PATH,
  main,
  readFinishIntent,
  readWorkerSnapshot,
  reconcileWorkerClaim,
  renderSummary,
};
