// @ts-check
"use strict";

const { applyTransactions, parseTransactionLog, serializeTransactionLog } = require("./dispatch_work_coordinator_replay.cjs");

const COORDINATOR_BRANCH = "dispatch-coordinator";
const COORDINATOR_LOG_PATH = "dispatch-work-coordinator.jsonl";
const DEFAULT_MAX_RETRIES = 5;

/**
 * @typedef {
 *   | {kind: "Work", work: string, claim: null, attempt: null}
 *   | {kind: "Claim", work: string, claim: string, attempt: null}
 *   | {kind: "ClaimCancellation", work: string, claim: string, attempt: null}
 *   | {kind: "Completion", work: string, claim: string, attempt: string}
 *   | {kind: "WorkCancellation", work: string, claim: null, attempt: null}
 * } CoordinatorTransaction
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
 * Read the coordinator branch and its canonical transaction log.
 * @param {{githubClient: any, owner: string, repo: string}} options
 */
async function readCoordinatorLog({ githubClient, owner, repo }) {
  let head;
  try {
    const response = await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${COORDINATOR_BRANCH}` });
    head = response.data.object.sha;
  } catch (error) {
    if (isMissing(error)) return { sha: null, transactions: [] };
    throw new Error("Failed to read dispatch coordinator branch", { cause: error });
  }

  if (typeof head !== "string" || !head) throw new TypeError("Dispatch coordinator branch returned an invalid commit");

  try {
    const commit = await githubClient.rest.git.getCommit({ owner, repo, commit_sha: head });
    const tree = await githubClient.rest.git.getTree({ owner, repo, tree_sha: commit.data.tree.sha });
    const entry = tree.data.tree.find(item => item.path === COORDINATOR_LOG_PATH);
    if (!entry) return { sha: head, transactions: [] };
    if (entry.type !== "blob" || typeof entry.sha !== "string") throw new TypeError("Dispatch coordinator log is not a regular file");

    const blob = await githubClient.rest.git.getBlob({ owner, repo, file_sha: entry.sha });
    if (blob.data.encoding !== "base64" || typeof blob.data.content !== "string") {
      throw new TypeError("Dispatch coordinator log has an unsupported blob encoding");
    }
    const contents = Buffer.from(blob.data.content, "base64").toString("utf8");
    return { sha: head, transactions: parseTransactionLog(contents) };
  } catch (error) {
    throw new Error("Failed to read dispatch coordinator log", { cause: error });
  }
}

/**
 * Apply intents against the latest branch head and publish with fast-forward-only
 * ref updates. A stale publication is replayed against the refreshed log.
 * @param {{
 *   githubClient: any,
 *   owner: string,
 *   repo: string,
 *   intents: CoordinatorTransaction[],
 *   maxRetries?: number,
 *   sleepFn?: (delay: number) => Promise<void>
 * }} options
 */
async function applyAndPublishCoordinatorTransactions({ githubClient, owner, repo, intents, maxRetries = DEFAULT_MAX_RETRIES, sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay)) }) {
  if (!Number.isSafeInteger(maxRetries) || maxRetries < 0 || maxRetries > 10) {
    throw new RangeError("Dispatch coordinator maxRetries must be an integer between 0 and 10");
  }

  let lastConflict;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    const current = await readCoordinatorLog({ githubClient, owner, repo });
    const applied = applyTransactions(current.transactions, intents);
    if (applied.transactions.length === current.transactions.length) {
      return { ...applied, sha: current.sha, persisted: false };
    }

    try {
      const blob = await githubClient.rest.git.createBlob({
        owner,
        repo,
        content: serializeTransactionLog(applied.transactions),
        encoding: "utf-8",
      });
      const tree = await githubClient.rest.git.createTree({
        owner,
        repo,
        ...(current.sha ? { base_tree: (await githubClient.rest.git.getCommit({ owner, repo, commit_sha: current.sha })).data.tree.sha } : {}),
        tree: [{ path: COORDINATOR_LOG_PATH, mode: "100644", type: "blob", sha: blob.data.sha }],
      });
      const commit = await githubClient.rest.git.createCommit({
        owner,
        repo,
        message: "Update dispatch coordinator transaction log",
        tree: tree.data.sha,
        parents: current.sha ? [current.sha] : [],
      });

      if (current.sha) {
        await githubClient.rest.git.updateRef({
          owner,
          repo,
          ref: `heads/${COORDINATOR_BRANCH}`,
          sha: commit.data.sha,
          force: false,
        });
      } else {
        await githubClient.rest.git.createRef({
          owner,
          repo,
          ref: `refs/heads/${COORDINATOR_BRANCH}`,
          sha: commit.data.sha,
        });
      }
      return { ...applied, sha: commit.data.sha, persisted: true };
    } catch (error) {
      if (!isRefConflict(error)) throw new Error("Failed to publish dispatch coordinator log", { cause: error });
      lastConflict = error;
      if (attempt < maxRetries) await sleepFn(Math.min(1000, 50 * 2 ** attempt));
    }
  }

  throw new Error("Dispatch coordinator branch kept changing; publication retries were exhausted", { cause: lastConflict });
}

module.exports = {
  COORDINATOR_BRANCH,
  COORDINATOR_LOG_PATH,
  applyAndPublishCoordinatorTransactions,
  readCoordinatorLog,
};
