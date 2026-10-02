// @ts-check
"use strict";

const { applyTransactions, parseTransactionLog, serializeTransactionLog } = require("./dispatch_work_coordinator_replay.cjs");

const COORDINATOR_BRANCH = "gh-aw-dispatch-work-coordinator";
const COORDINATOR_LOG_PATH = "dispatch-work-coordinator.jsonl";
const DEFAULT_MAX_RETRIES = 5;

/**
 * @typedef {import("./dispatch_work_coordinator_replay.cjs").DispatchWorkTransaction} CoordinatorTransaction
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
 * @param {{githubClient: any, owner: string, repo: string, core?: {info: (message: string) => void}}} options
 */
async function readCoordinatorLogRaw({ githubClient, owner, repo, core: coreApi = typeof core === "undefined" ? undefined : core }) {
  coreApi?.info("Dispatch coordinator: reading queue branch");
  let head;
  let legacyBranch = false;
  try {
    const response = await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${COORDINATOR_BRANCH}` });
    head = response.data.object.sha;
  } catch (error) {
    if (isMissing(error)) {
      try {
        const legacy = await githubClient.rest.git.getRef({ owner, repo, ref: "heads/dispatch-coordinator" });
        head = legacy.data.object.sha;
        legacyBranch = true;
      } catch (legacyError) {
        if (!isMissing(legacyError)) throw new Error("Failed to read legacy dispatch coordinator branch", { cause: legacyError });
        try {
          await githubClient.rest.repos.get({ owner, repo });
        } catch (repositoryError) {
          throw new Error("Failed to read dispatch coordinator repository", { cause: repositoryError });
        }
        coreApi?.info("Dispatch coordinator: queue branch does not exist");
        return { sha: null, transactions: [] };
      }
    } else {
      throw new Error("Failed to read dispatch coordinator branch", { cause: error });
    }
  }

  if (typeof head !== "string" || !head) throw new TypeError("Dispatch coordinator branch returned an invalid commit");

  try {
    const commit = await githubClient.rest.git.getCommit({ owner, repo, commit_sha: head });
    const tree = await githubClient.rest.git.getTree({ owner, repo, tree_sha: commit.data.tree.sha });
    if (tree.data.truncated) throw new TypeError("Dispatch coordinator tree is truncated");
    const entry = tree.data.tree.find(item => item.path === COORDINATOR_LOG_PATH);
    if (!entry) {
      coreApi?.info("Dispatch coordinator: queue branch has no transaction log");
      throw new TypeError("Dispatch coordinator branch has no transaction log");
    }
    if (entry.type !== "blob" || !["100644", "100755"].includes(entry.mode) || typeof entry.sha !== "string") throw new TypeError("Dispatch coordinator log is not a regular file");

    const blob = await githubClient.rest.git.getBlob({ owner, repo, file_sha: entry.sha });
    if (blob.data.encoding !== "base64" || typeof blob.data.content !== "string") {
      throw new TypeError("Dispatch coordinator log has an unsupported blob encoding");
    }
    const contents = Buffer.from(blob.data.content, "base64").toString("utf8");
    const transactions = parseTransactionLog(contents);
    coreApi?.info(`Dispatch coordinator: read ${transactions.length} queue transactions`);
    return { sha: head, transactions, legacyBranch, needsUpgrade: legacyBranch || contents !== serializeTransactionLog(transactions) };
  } catch (error) {
    throw new Error("Failed to read dispatch coordinator log", { cause: error });
  }
}

/**
 * Activation is read-only; trusted write-capable readers publish upgrades.
 * @param {{githubClient: any, owner: string, repo: string, publishUpgrades?: boolean, core?: {info: (message: string) => void}}} options
 */
async function readCoordinatorLog({ githubClient, owner, repo, publishUpgrades = true, core: coreApi = typeof core === "undefined" ? undefined : core }) {
  const current = await readCoordinatorLogRaw({ githubClient, owner, repo, core: coreApi });
  if (!current.needsUpgrade || !publishUpgrades) return current;
  const upgraded = await applyAndPublishCoordinatorTransactions({ githubClient, owner, repo, intents: [], core: coreApi });
  return { sha: upgraded.sha, transactions: upgraded.transactions };
}

/**
 * Apply intents against the latest branch head and publish with fast-forward-only
 * ref updates. A stale publication is replayed against the refreshed log.
 * @param {{
 *   githubClient: any,
 *   owner: string,
 *   repo: string,
 *   intents: CoordinatorTransaction[],
 *   deriveIntents?: (transactions: CoordinatorTransaction[]) => CoordinatorTransaction[],
 *   maxRetries?: number,
 *   sleepFn?: (delay: number) => Promise<void>,
 *   core?: {info: (message: string) => void}
 * }} options
 */
async function applyAndPublishCoordinatorTransactions({
  githubClient,
  owner,
  repo,
  intents,
  deriveIntents,
  maxRetries = DEFAULT_MAX_RETRIES,
  sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay)),
  core: coreApi = typeof core === "undefined" ? undefined : core,
}) {
  if (!Number.isSafeInteger(maxRetries) || maxRetries < 0 || maxRetries > 10) {
    throw new RangeError("Dispatch coordinator maxRetries must be an integer between 0 and 10");
  }

  let lastConflict;
  coreApi?.info(`Dispatch coordinator: publishing ${intents.length} queue intents`);
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    coreApi?.info(`Dispatch coordinator: queue publication attempt ${attempt + 1} of ${maxRetries + 1}`);
    const current = await readCoordinatorLogRaw({ githubClient, owner, repo, core: coreApi });
    const applied = applyTransactions(current.transactions, deriveIntents ? deriveIntents(current.transactions) : intents);
    coreApi?.info(`Dispatch coordinator: ${applied.transactions.length - current.transactions.length} new transactions, ${applied.rejected.length} rejected intents`);
    if (!current.needsUpgrade && applied.transactions.length === current.transactions.length) {
      coreApi?.info("Dispatch coordinator: queue unchanged; publication skipped");
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
        tree: [{ path: COORDINATOR_LOG_PATH, mode: "100644", type: "blob", sha: blob.data.sha }],
      });
      const commit = await githubClient.rest.git.createCommit({
        owner,
        repo,
        message: "Update dispatch coordinator transaction log",
        tree: tree.data.sha,
        parents: current.sha ? [current.sha] : [],
      });

      if (current.sha && !current.legacyBranch) {
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
      coreApi?.info(`Dispatch coordinator: queue published ${applied.transactions.length} transactions`);
      return { ...applied, transactions: parseTransactionLog(contents), sha: commit.data.sha, persisted: true };
    } catch (error) {
      if (!isRefConflict(error)) throw new Error("Failed to publish dispatch coordinator log", { cause: error });
      lastConflict = error;
      coreApi?.info(`Dispatch coordinator: queue ref conflict; ${attempt < maxRetries ? "retrying" : "retries exhausted"}`);
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
