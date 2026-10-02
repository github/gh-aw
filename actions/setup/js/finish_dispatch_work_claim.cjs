// @ts-check
"use strict";

const fs = require("fs");
const { applyAndPublishCoordinatorTransactions, readCoordinatorLog } = require("./dispatch_work_coordinator_store.cjs");
const { CURRENT_VERSION } = require("./dispatch_work_coordinator_codemods.cjs");
const { replayTransactions } = require("./dispatch_work_coordinator_replay.cjs");
const { buildWorkflowCallId } = require("./aw_context.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/dispatch-work-coordinator.snapshot.json";
const FINISH_INTENT_PATH = "/tmp/gh-aw/dispatch-work-coordinator.finish.jsonl";
const SAFE_OUTPUTS_PATH = "/tmp/gh-aw/safeoutputs.jsonl";

function readWorkerSnapshot(snapshotPath = process.env.GH_AW_DISPATCH_COORDINATOR_SNAPSHOT || SNAPSHOT_PATH) {
  const snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
  if (
    !snapshot ||
    typeof snapshot !== "object" ||
    snapshot.version !== 2 ||
    (snapshot.worker !== null &&
      (!snapshot.worker || typeof snapshot.worker !== "object" || typeof snapshot.worker.work_id !== "string" || snapshot.worker.work_id.length === 0 || typeof snapshot.worker.claim_id !== "string" || snapshot.worker.claim_id.length === 0))
  ) {
    throw new TypeError("dispatch coordinator snapshot has an invalid worker assignment");
  }
  return snapshot.worker;
}

function readFinishIntent(finishIntentPath = process.env.GH_AW_DISPATCH_COORDINATOR_FINISH_INTENT || FINISH_INTENT_PATH) {
  if (!fs.existsSync(finishIntentPath)) return null;
  const outcomes = new Set();
  const contents = fs.readFileSync(finishIntentPath, "utf8");
  for (const [index, line] of contents.split("\n").entries()) {
    if (!line.trim()) continue;
    let intent;
    try {
      intent = JSON.parse(line);
    } catch {
      throw new TypeError(`dispatch claim finish intent line ${index + 1} is malformed`);
    }
    if (!intent || typeof intent !== "object" || Array.isArray(intent) || Object.keys(intent).length !== 1 || !Object.hasOwn(intent, "outcome") || !["completed", "cancelled"].includes(intent.outcome)) {
      throw new TypeError(`dispatch claim finish intent line ${index + 1} is invalid`);
    }
    outcomes.add(intent.outcome);
  }
  if (outcomes.size > 1) throw new TypeError("dispatch claim finish intents conflict");
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
  return `## Dispatch work reconciliation\n\n<details>\n<summary>Show claim reconciliation</summary>\n\n${description}\n\n</details>\n`;
}

async function reconcileWorkerClaim(options = {}) {
  const worker = options.worker === undefined ? readWorkerSnapshot(options.snapshotPath) : options.worker;
  const githubClient = options.githubClient || (typeof github === "undefined" ? undefined : github);
  const repositoryContext = options.context || (typeof context === "undefined" ? undefined : context);
  if (!repositoryContext?.repo) throw new Error("GitHub repository context is unavailable");
  const owner = repositoryContext.repo.owner;
  const repo = repositoryContext.repo.repo;
  const readLog = options.readCoordinatorLog || readCoordinatorLog;

  if (!worker) return { authorized: true, status: "unassigned" };

  const finishIntent = readFinishIntent(options.finishIntentPath);
  const initial = await readLog({ githubClient, owner, repo });
  const projection = replayTransactions(initial.transactions);
  const claim = initial.transactions.find(transaction => transaction.kind === "Claim" && transaction.claim === worker.claim_id);
  if (!claim || claim.work !== worker.work_id || !Object.hasOwn(projection.work, worker.work_id) || !Object.hasOwn(projection.claim, worker.claim_id)) {
    return { authorized: false, status: "superseded" };
  }

  const runId = String(repositoryContext.runId ?? process.env.GITHUB_RUN_ID ?? "").trim();
  const attempt = buildWorkflowCallId(runId, process.env.GITHUB_RUN_ATTEMPT || "1", process.env.GITHUB_WORKFLOW_REF || "");
  if (!attempt) throw new Error("current workflow attempt identity is unavailable");

  const existingCompletion = initial.transactions.find(transaction => transaction.kind === "Completion" && transaction.work === worker.work_id && transaction.claim === worker.claim_id && transaction.attempt === attempt);
  if (existingCompletion && projection.work[worker.work_id] === "completed" && projection.winner[worker.work_id] === worker.claim_id) {
    return { authorized: true, status: "completed" };
  }

  if (projection.claim[worker.claim_id] !== "effective") {
    if (projection.claim[worker.claim_id] === "cancelled") return { authorized: false, status: "cancelled" };
    return { authorized: false, status: ["completed", "cancelled"].includes(projection.work[worker.work_id]) ? "terminal" : "superseded" };
  }

  const cancel = finishIntent === null || finishIntent === "cancelled";
  const intent = cancel
    ? { version: CURRENT_VERSION, kind: "ClaimCancellation", work: worker.work_id, claim: worker.claim_id, attempt: null }
    : { version: CURRENT_VERSION, kind: "Completion", work: worker.work_id, claim: worker.claim_id, attempt };
  const publish = options.applyAndPublish || applyAndPublishCoordinatorTransactions;
  await publish({ githubClient, owner, repo, intents: [intent] });

  const latest = await readLog({ githubClient, owner, repo });
  const verified = replayTransactions(latest.transactions);
  if (
    !cancel &&
    latest.transactions.some(transaction => transaction.kind === "Completion" && transaction.work === worker.work_id && transaction.claim === worker.claim_id && transaction.attempt === attempt) &&
    verified.work[worker.work_id] === "completed" &&
    verified.winner[worker.work_id] === worker.claim_id
  ) {
    return { authorized: true, status: "completed" };
  }
  if (cancel && latest.transactions.some(transaction => transaction.kind === "ClaimCancellation" && transaction.work === worker.work_id && transaction.claim === worker.claim_id) && verified.claim[worker.claim_id] === "cancelled") {
    return { authorized: false, status: "cancelled" };
  }
  return { authorized: false, status: ["completed", "cancelled"].includes(verified.work[worker.work_id]) ? "terminal" : "superseded" };
}

async function main(options = {}) {
  const coreApi = options.core || core;
  try {
    const result = await reconcileWorkerClaim(options);
    coreApi.setOutput("authorized", String(result.authorized));
    coreApi.info(`Dispatch work claim reconciliation: ${result.status}`);
    await coreApi.summary.addRaw(renderSummary(result.status)).write();
    return result;
  } catch {
    coreApi.setOutput("authorized", "false");
    coreApi.info("Dispatch work claim reconciliation failed; ordinary safe outputs are blocked.");
    await coreApi.summary.addRaw(renderSummary("failed")).write();
    throw new Error("Dispatch work claim reconciliation failed; ordinary safe outputs are blocked");
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
