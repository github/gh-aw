// @ts-check
"use strict";

const { applyTransactions, parseTransactionLog, serializeTransactionLog } = require("./work_queue_replay.cjs");

const WORK_QUEUE_BRANCH = "work-queue";
const WORK_QUEUE_LOG_PATH = "work-queue.jsonl";
const LEGACY_QUEUE_BRANCH = "dispatch-coordinator";
const LEGACY_QUEUE_LOG_PATH = "dispatch-work-coordinator.jsonl";
const DEFAULT_MAX_RETRIES = 5;

/**
 * @typedef {
 *   | {version: number, kind: "Work", work: string, claim: null, attempt: null}
 *   | {version: number, kind: "Claim", work: string, claim: string, attempt: null}
 *   | {version: number, kind: "ClaimCancellation", work: string, claim: string, attempt: null}
 *   | {version: number, kind: "Completion", work: string, claim: string, attempt: string}
 *   | {version: number, kind: "WorkCancellation", work: string, claim: null, attempt: null}
 * } WorkQueueTransaction
 */

/**
 * @param {unknown} error
 * @returns {number | undefined}
 */
function httpStatus(error) {
  return error && typeof error === "object" && "status" in error && typeof error.status === "number" ? error.status : undefined;
}

/**
 * @param {unknown} error
 * @returns {boolean}
 */
function isMissing(error) {
  return httpStatus(error) === 404;
}

/**
 * @param {unknown} error
 * @returns {boolean}
 */
function isRefConflict(error) {
  const status = httpStatus(error);
  if (status === 409) return true;
  if (status !== 422) return false;
  const message = error instanceof Error ? error.message : String(error);
  return /already exists|not a fast.forward|reference update failed/i.test(message);
}

/**
 * Read the queue branch and its canonical transaction log.
 * @param {{githubClient: any, owner: string, repo: string, core?: {info: (message: string) => void}}} options
 */
async function readWorkQueueLogRaw({ githubClient, owner, repo, core: coreApi = typeof core === "undefined" ? undefined : core }) {
  coreApi?.info("Work queue: reading queue branch");
  const [currentHead, legacyHead] = await Promise.all([readQueueBranch(githubClient, owner, repo, WORK_QUEUE_BRANCH), readQueueBranch(githubClient, owner, repo, LEGACY_QUEUE_BRANCH)]);
  if (currentHead && legacyHead) {
    throw new Error("Both current and legacy work queue branches exist; reconcile storage before proceeding");
  }
  const head = currentHead || legacyHead;
  const branch = legacyHead ? LEGACY_QUEUE_BRANCH : WORK_QUEUE_BRANCH;
  let logPath = legacyHead ? LEGACY_QUEUE_LOG_PATH : WORK_QUEUE_LOG_PATH;
  if (!head) {
    coreApi?.info("Work queue: queue branch does not exist");
    return { sha: null, transactions: [], branch, logPath };
  }
  if (legacyHead) coreApi?.info("Work queue: retaining legacy storage until an explicit migration");

  try {
    const commit = await githubClient.rest.git.getCommit({ owner, repo, commit_sha: head });
    const tree = await githubClient.rest.git.getTree({ owner, repo, tree_sha: commit.data.tree.sha });
    const entries = tree.data.tree.filter(item => item.path === WORK_QUEUE_LOG_PATH || item.path === LEGACY_QUEUE_LOG_PATH);
    if (entries.length > 1) throw new Error("Both current and legacy work queue logs exist; reconcile storage before proceeding");
    const entry = entries[0];
    if (!entry) {
      coreApi?.info("Work queue: queue branch has no transaction log");
      return { sha: head, transactions: [], branch, logPath };
    }
    logPath = entry.path;
    if (entry.type !== "blob" || typeof entry.sha !== "string") throw new TypeError("Work queue log is not a regular file");

    const blob = await githubClient.rest.git.getBlob({ owner, repo, file_sha: entry.sha });
    if (blob.data.encoding !== "base64" || typeof blob.data.content !== "string") {
      throw new TypeError("Work queue log has an unsupported blob encoding");
    }
    const contents = Buffer.from(blob.data.content, "base64").toString("utf8");
    const transactions = parseTransactionLog(contents);
    coreApi?.info(`Work queue: read ${transactions.length} queue transactions`);
    return { sha: head, transactions, branch, logPath, needsUpgrade: contents !== serializeTransactionLog(transactions) };
  } catch (error) {
    throw new Error("Failed to read work queue log", { cause: error });
  }
}

async function readQueueBranch(githubClient, owner, repo, branch) {
  try {
    const response = await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${branch}` });
    const head = response.data.object.sha;
    if (typeof head !== "string" || !head) throw new TypeError("Work queue branch returned an invalid commit");
    return head;
  } catch (error) {
    if (isMissing(error)) return null;
    throw new Error("Failed to read work queue branch", { cause: error });
  }
}

/**
 * Activation is read-only; trusted write-capable readers publish upgrades.
 * @param {{githubClient: any, owner: string, repo: string, publishUpgrades?: boolean, core?: {info: (message: string) => void}}} options
 */
async function readWorkQueueLog({ githubClient, owner, repo, publishUpgrades = true, core: coreApi = typeof core === "undefined" ? undefined : core }) {
  const current = await readWorkQueueLogRaw({ githubClient, owner, repo, core: coreApi });
  if (!current.needsUpgrade || !publishUpgrades) return current;
  const upgraded = await applyAndPublishWorkQueueTransactions({ githubClient, owner, repo, intents: [], core: coreApi });
  return { sha: upgraded.sha, transactions: upgraded.transactions };
}

/**
 * Apply intents against the latest branch head and publish with fast-forward-only
 * ref updates. A stale publication is replayed against the refreshed log.
 * @param {{
 *   githubClient: any,
 *   owner: string,
 *   repo: string,
 *   intents: WorkQueueTransaction[],
 *   maxRetries?: number,
 *   sleepFn?: (delay: number) => Promise<void>,
 *   core?: {info: (message: string) => void}
 * }} options
 */
async function applyAndPublishWorkQueueTransactions({
  githubClient,
  owner,
  repo,
  intents,
  maxRetries = DEFAULT_MAX_RETRIES,
  sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay)),
  core: coreApi = typeof core === "undefined" ? undefined : core,
}) {
  if (!Number.isSafeInteger(maxRetries) || maxRetries < 0 || maxRetries > 10) {
    throw new RangeError("Work queue maxRetries must be an integer between 0 and 10");
  }

  let lastConflict;
  coreApi?.info(`Work queue: publishing ${intents.length} queue intents`);
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    coreApi?.info(`Work queue: queue publication attempt ${attempt + 1} of ${maxRetries + 1}`);
    const current = await readWorkQueueLogRaw({ githubClient, owner, repo, core: coreApi });
    const applied = applyTransactions(current.transactions, intents);
    coreApi?.info(`Work queue: ${applied.transactions.length - current.transactions.length} new transactions, ${applied.rejected.length} rejected intents`);
    if (!current.needsUpgrade && applied.transactions.length === current.transactions.length) {
      coreApi?.info("Work queue: queue unchanged; publication skipped");
      return { ...applied, sha: current.sha, persisted: false };
    }

    try {
      const contents = serializeTransactionLog(applied.transactions);
      const blob = await githubClient.rest.git.createBlob({
        owner,
        repo,
        content: contents,
        encoding: "utf-8",
      });
      const tree = await githubClient.rest.git.createTree({
        owner,
        repo,
        ...(current.sha ? { base_tree: (await githubClient.rest.git.getCommit({ owner, repo, commit_sha: current.sha })).data.tree.sha } : {}),
        tree: [{ path: current.logPath, mode: "100644", type: "blob", sha: blob.data.sha }],
      });
      const commit = await githubClient.rest.git.createCommit({
        owner,
        repo,
        message: "Update work queue transaction log",
        tree: tree.data.sha,
        parents: current.sha ? [current.sha] : [],
      });

      if (current.sha) {
        await githubClient.rest.git.updateRef({
          owner,
          repo,
          ref: `heads/${current.branch}`,
          sha: commit.data.sha,
          force: false,
        });
      } else {
        await githubClient.rest.git.createRef({
          owner,
          repo,
          ref: `refs/heads/${current.branch}`,
          sha: commit.data.sha,
        });
      }
      coreApi?.info(`Work queue: queue published ${applied.transactions.length} transactions`);
      return { ...applied, transactions: parseTransactionLog(contents), sha: commit.data.sha, persisted: true };
    } catch (error) {
      if (!isRefConflict(error)) throw new Error("Failed to publish work queue log", { cause: error });
      lastConflict = error;
      coreApi?.info(`Work queue: queue ref conflict; ${attempt < maxRetries ? "retrying" : "retries exhausted"}`);
      if (attempt < maxRetries) await sleepFn(Math.min(1000, 50 * 2 ** attempt));
    }
  }

  throw new Error("Work queue branch kept changing; publication retries were exhausted", { cause: lastConflict });
}

module.exports = {
  WORK_QUEUE_BRANCH,
  WORK_QUEUE_LOG_PATH,
  applyAndPublishWorkQueueTransactions,
  readWorkQueueLog,
};
