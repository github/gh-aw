// @ts-check
"use strict";

const { TextDecoder } = require("node:util");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("store");
const { canonical, identity, integer, queueError } = require("./work_queue_codec.cjs");
const { LEDGER_BYTES } = require("./work_queue_limits.cjs");
const { validateActor } = require("./work_queue_policy.cjs");
const { writeWorkQueueUpdateSummary } = require("./work_queue_summary_renderer.cjs");
const { queueLedgerClient } = require("./work_queue_git_transport.cjs");
const { observeQueueOperation, publicationAttempt, publicationRecovery } = require("./work_queue_telemetry.cjs");
const { appendCommit, buildCommit, checkpointCommit, committedRequest, newRequest, newState, replayTransactionLog } = require("./work_queue_replay.cjs");

const WORK_QUEUE_BRANCH = "work-queue";
const WORK_QUEUE_LOG_PATH = "work-queue.jsonl";
// Each failed compare-and-swap re-reads the ledger before retrying; the re-read is the backoff.
const CAS_ATTEMPTS = 8;

/**
 * @typedef {{githubClient: any, owner: string, repo: string, branch?: string, core?: any}} QueueReadOptions
 * @typedef {{sha: string | null, treeSha: string | null, contents: string, protocol: number | null, transactions: Array<any>, state: any, branch: string, logPath: string}} QueueLog
 */

function validateBranch(branch) {
  identity(branch, "queue branch");
  if (
    branch === "@" ||
    branch.startsWith("-") ||
    branch.startsWith("/") ||
    branch.endsWith("/") ||
    branch.includes("//") ||
    branch.includes("..") ||
    /[\s~^:?*[\\]/.test(branch) ||
    branch.includes("@{") ||
    branch.split("/").some(part => part.startsWith(".") || part.endsWith(".") || part.endsWith(".lock"))
  )
    throw queueError("branch_invalid", "invalid queue branch");
}

function httpStatus(error) {
  return error && typeof error === "object" && typeof error.status === "number" ? error.status : undefined;
}

function refConflict(error) {
  const status = httpStatus(error);
  return (
    status === 409 ||
    (status === 422 && /already exists|not a fast.forward|reference update failed/i.test(error.message || "")) ||
    (error?.errors?.some(item => /expectedHeadOid|head.*(?:changed|match)|expected.*head/i.test(item.message || "")) ?? false)
  );
}

function ambiguousWrite(error) {
  const status = httpStatus(error);
  return !status || status >= 500 || status === 408;
}

async function verifyRepository(githubClient, owner, repo) {
  let response;
  try {
    response = await githubClient.rest.repos.get({ owner, repo });
  } catch (error) {
    throw queueError("repository_unavailable", `cannot establish queue repository visibility (${httpStatus(error) ?? "transport"})`);
  }
  if (!response?.data || typeof response.data.full_name !== "string" || response.data.full_name.toLowerCase() !== `${owner}/${repo}`.toLowerCase()) throw queueError("repository_unavailable", "repository identity does not match the queue");
  return response.data;
}

async function readRef(githubClient, owner, repo, branch) {
  try {
    const response = await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${branch}` });
    const sha = response?.data?.object?.sha;
    if (typeof sha !== "string" || !sha) throw queueError("ledger_invalid", "queue branch returned no commit identity");
    return sha;
  } catch (error) {
    if (httpStatus(error) === 404) return null;
    if (httpStatus(error) === 409) {
      // GitHub returns 409 rather than 404 for refs in a branchless repository.
      const response = await githubClient.graphql("query WorkQueueEmptyRepository($owner:String!,$repo:String!) { repository(owner:$owner,name:$repo) { nameWithOwner isEmpty } }", { owner, repo });
      if (response?.repository?.nameWithOwner?.toLowerCase() === `${owner}/${repo}`.toLowerCase() && response.repository.isEmpty === true) return null;
    }
    throw error;
  }
}

/** @returns {Promise<{treeSha: string, contents: string}>} */
async function readQueueContentsAtSHA({ githubClient, owner, repo }, sha) {
  const commit = await githubClient.rest.git.getCommit({ owner, repo, commit_sha: sha });
  const treeSha = commit?.data?.tree?.sha;
  if (typeof treeSha !== "string" || !treeSha) throw queueError("ledger_invalid", "queue commit has no tree");
  const response = await githubClient.rest.git.getTree({ owner, repo, tree_sha: treeSha });
  if (response?.data?.truncated === true || !Array.isArray(response?.data?.tree)) throw queueError("ledger_invalid", "queue tree is missing or truncated");
  const logs = response.data.tree.filter(entry => entry.path === WORK_QUEUE_LOG_PATH);
  if (logs.length !== 1 || logs[0].type !== "blob" || typeof logs[0].sha !== "string" || logs[0].mode !== "100644") throw queueError("ledger_invalid", "existing queue must contain exactly one regular log");
  const blob = await githubClient.rest.git.getBlob({ owner, repo, file_sha: logs[0].sha });
  if (blob?.data?.encoding !== "base64" || typeof blob.data.content !== "string") throw queueError("ledger_invalid", "queue blob encoding is unsupported");
  const encoded = blob.data.content.replace(/\s/g, "");
  if (encoded.length > Math.ceil(LEDGER_BYTES / 3) * 4) throw queueError("resource_limit", "queue blob exceeds the ledger bound");
  const bytes = Buffer.from(encoded, "base64");
  if (bytes.toString("base64") !== encoded) throw queueError("ledger_invalid", "malformed queue blob base64");
  if (bytes.length > LEDGER_BYTES || (typeof blob.data.size === "number" && blob.data.size !== bytes.length)) throw queueError("ledger_invalid", "queue blob is oversized or truncated");
  let contents;
  try {
    contents = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    throw queueError("ledger_invalid", "queue blob contains malformed UTF-8");
  }
  return { treeSha, contents };
}

/**
 * Protocol version of a canonical log. Canonical JSON sorts `version` last, so the
 * first line ends with its version; anything else is left to the strict parser.
 * @param {string} contents
 * @returns {number | null}
 */
function ledgerProtocol(contents) {
  if (!contents) return null;
  const end = contents.indexOf("\n");
  const line = end === -1 ? contents : contents.slice(0, end);
  const match = /,"version":([0-9]+)}\r?$/.exec(line.slice(-32));
  return match ? Number(match[1]) : null;
}

/**
 * Read and replay the ledger. A v3 head is returned unreplayed only when `allowLegacy`
 * (the converter); every other reader gets unsupported_protocol.
 * @param {QueueReadOptions & {allowLegacy?: boolean}} options
 * @returns {Promise<QueueLog>}
 */
async function readWorkQueueLog(options) {
  return observeQueueOperation("read", options, () => readWorkQueueLogImpl(options));
}

/** @param {QueueReadOptions & {allowLegacy?: boolean}} options */
async function readWorkQueueLogImpl({ githubClient, owner, repo, branch = WORK_QUEUE_BRANCH, core: coreApi = undefined, allowLegacy = false }) {
  validateBranch(branch);
  githubClient = queueLedgerClient(githubClient, owner, repo, branch);
  // Also refreshes the Git transport's ref advertisement before reading the head.
  const repository = await verifyRepository(githubClient, owner, repo);
  const sha = await readRef(githubClient, owner, repo, branch);
  if (!sha) {
    if (repository.size !== 0 && (typeof repository.default_branch !== "string" || !repository.default_branch || !(await readRef(githubClient, owner, repo, repository.default_branch))))
      throw queueError("repository_unavailable", "cannot establish contents access before treating the queue as absent");
    return { sha: null, treeSha: null, contents: "", protocol: null, transactions: [], state: newState(), branch, logPath: WORK_QUEUE_LOG_PATH };
  }
  const { treeSha, contents } = await readQueueContentsAtSHA({ githubClient, owner, repo }, sha);
  // A conversion that carried no Work leaves an empty v4 log.
  if (!contents) return { sha, treeSha, contents, protocol: 4, transactions: [], state: newState(), branch, logPath: WORK_QUEUE_LOG_PATH };
  const protocol = ledgerProtocol(contents);
  if (protocol !== null && protocol < 4) {
    if (allowLegacy) return { sha, treeSha, contents, protocol, transactions: [], state: null, branch, logPath: WORK_QUEUE_LOG_PATH };
    throw queueError("unsupported_protocol", "work queue protocol v4 required; run the v3 converter");
  }
  const state = replayTransactionLog(contents);
  coreApi?.info?.(`Work queue: replayed ${state.transactions.length} commits`);
  log.debug("ledger.read.complete", { bytes: Buffer.byteLength(contents, "utf8"), transactions: state.transactions.length, works: state.works.size });
  return { sha, treeSha, contents, protocol: 4, transactions: state.transactions, state, branch, logPath: WORK_QUEUE_LOG_PATH };
}

async function prepareEmptyRepository(githubClient, owner, repo, branch) {
  const repository = await verifyRepository(githubClient, owner, repo);
  if (repository.size !== 0 || typeof repository.default_branch !== "string") return;
  validateBranch(repository.default_branch);
  if (repository.default_branch === branch) throw queueError("branch_invalid", "queue branch cannot be the default branch of an empty repository");
  if (await readRef(githubClient, owner, repo, repository.default_branch)) return;
  try {
    await githubClient.rest.repos.createOrUpdateFileContents({
      owner,
      repo,
      path: ".gh-aw/work-queue-bootstrap",
      message: "Initialize repository for work queue",
      content: Buffer.from("The work-queue branch holds the work queue ledger.\n").toString("base64"),
    });
  } catch (error) {
    if (!(await readRef(githubClient, owner, repo, repository.default_branch))) throw error;
  }
}

/**
 * Write `content` as the whole log in one commit whose parent is the observed head
 * (compare-and-swap via a non-forced ref update).
 * @param {{githubClient: any, owner: string, repo: string, branch: string, parent: string | null, treeSha: string | null, content: string, message: string}} options
 * @returns {Promise<string>}
 */
async function writeQueueContent({ githubClient, owner, repo, branch, parent, treeSha, content, message }) {
  if (Buffer.byteLength(content, "utf8") > LEDGER_BYTES) throw queueError("ledger_limit", "the ledger would exceed its byte bound; a checkpoint is required");
  githubClient = queueLedgerClient(githubClient, owner, repo, branch);
  if (!parent) await prepareEmptyRepository(githubClient, owner, repo, branch);
  const blob = await githubClient.rest.git.createBlob({ owner, repo, content, encoding: "utf-8" });
  const tree = await githubClient.rest.git.createTree({ owner, repo, ...(treeSha ? { base_tree: treeSha } : {}), tree: [{ path: WORK_QUEUE_LOG_PATH, mode: "100644", type: "blob", sha: blob.data.sha }] });
  const commit = await githubClient.rest.git.createCommit({ owner, repo, message, tree: tree.data.sha, parents: parent ? [parent] : [] });
  if (parent) await githubClient.rest.git.updateRef({ owner, repo, ref: `heads/${branch}`, sha: commit.data.sha, force: false });
  else await githubClient.rest.git.createRef({ owner, repo, ref: `refs/heads/${branch}`, sha: commit.data.sha });
  return commit.data.sha;
}

/**
 * @typedef {{committed: boolean, recovered: boolean, commit: any, state: any, sha: string | null, request: {id: string, kind: string, parameters: any}}} PublishResult
 */

/**
 * Publish one request. `operationsFor(state, request)` is re-evaluated against every
 * re-read head, so a lost compare-and-swap never commits a stale decision. An empty
 * operation list commits nothing.
 * @param {QueueReadOptions & {actor: import("./work_queue_policy.cjs").QueueActor, request: {kind: string, parameters: Record<string, any>}, operationsFor: (state: any, request: {id: string, kind: string, parameters: any}) => Array<any> | Promise<Array<any>>, maxAttempts?: number, now?: () => number, message?: string}} options
 * @returns {Promise<PublishResult>}
 */
async function publishWorkQueueRequest(options) {
  return observeQueueOperation("publish", options, () => publishWorkQueueRequestImpl(options));
}

/** @param {Parameters<typeof publishWorkQueueRequest>[0]} options */
async function publishWorkQueueRequestImpl({ githubClient, owner, repo, branch = WORK_QUEUE_BRANCH, actor, request, operationsFor, core: coreApi = undefined, maxAttempts = CAS_ATTEMPTS, now = Date.now, message = undefined }) {
  validateBranch(branch);
  integer(maxAttempts, 1, CAS_ATTEMPTS, "publication attempts");
  validateActor(actor);
  if (actor.repository.toLowerCase() !== `${owner}/${repo}`.toLowerCase()) throw queueError("actor_unauthorized", "actor belongs to another repository");
  if (typeof operationsFor !== "function") throw queueError("request_invalid", "publication requires an operationsFor(state) planner");
  const stable = newRequest(request.kind, actor, request.parameters);
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    publicationAttempt();
    const current = await readWorkQueueLog({ githubClient, owner, repo, branch, core: coreApi });
    const prior = committedRequest(current.state, stable.id);
    if (prior !== undefined) {
      if (attempt > 1) publicationRecovery("ambiguous");
      return { committed: true, recovered: true, commit: prior, state: current.state, sha: current.sha, request: stable };
    }
    const operations = await operationsFor(current.state, stable);
    if (!Array.isArray(operations)) throw queueError("request_invalid", "operationsFor must return an operation array");
    if (!operations.length) return { committed: false, recovered: false, commit: null, state: current.state, sha: current.sha, request: stable };
    const commit = buildCommit(current.state, actor, stable, operations, now());
    const state = appendCommit(current.state, commit);
    try {
      const sha = await writeQueueContent({
        githubClient,
        owner,
        repo,
        branch,
        parent: current.sha,
        treeSha: current.treeSha,
        content: current.contents + canonical(commit) + "\n",
        message: message ?? `work queue: ${stable.kind}`,
      });
      coreApi?.info?.(`Work queue: committed ${stable.kind} (${operations.length} operations)`);
      await writeWorkQueueUpdateSummary(coreApi, state, commit);
      return { committed: true, recovered: false, commit, state, sha, request: stable };
    } catch (error) {
      if (!refConflict(error) && !ambiguousWrite(error)) throw error;
      publicationRecovery(refConflict(error) ? "conflict" : "ambiguous");
      log.debug("request.publish.retry", { attempt, conflict: refConflict(error) });
    }
  }
  const latest = await readWorkQueueLog({ githubClient, owner, repo, branch, core: coreApi });
  const prior = committedRequest(latest.state, stable.id);
  if (prior !== undefined) return { committed: true, recovered: true, commit: prior, state: latest.state, sha: latest.sha, request: stable };
  throw queueError("cas_exhausted", `work queue head changed on all ${maxAttempts} publication attempts`);
}

/**
 * Replace the log with one Checkpoint commit (Git history keeps the prior content).
 * @param {QueueReadOptions & {actor: import("./work_queue_policy.cjs").QueueActor, minimumLines?: number, now?: () => number}} options
 * @returns {Promise<{checkpointed: boolean, sha: string | null, state: any}>}
 */
async function publishWorkQueueCheckpoint({ githubClient, owner, repo, branch = WORK_QUEUE_BRANCH, actor, minimumLines = 0, core: coreApi = undefined, now = Date.now }) {
  validateBranch(branch);
  validateActor(actor);
  for (let attempt = 1; attempt <= CAS_ATTEMPTS; attempt++) {
    const current = await readWorkQueueLog({ githubClient, owner, repo, branch, core: coreApi });
    if (!current.sha || current.state.transactions.length <= Math.max(1, minimumLines)) return { checkpointed: false, sha: current.sha, state: current.state };
    const checkpoint = checkpointCommit(current.state, actor, now());
    try {
      const sha = await writeQueueContent({ githubClient, owner, repo, branch, parent: current.sha, treeSha: current.treeSha, content: canonical(checkpoint) + "\n", message: "work queue: checkpoint" });
      coreApi?.info?.(`Work queue: checkpointed ${current.state.transactions.length} lines`);
      return { checkpointed: true, sha, state: current.state };
    } catch (error) {
      if (!refConflict(error) && !ambiguousWrite(error)) throw error;
    }
  }
  throw queueError("cas_exhausted", `work queue head changed on all ${CAS_ATTEMPTS} checkpoint attempts`);
}

module.exports = {
  CAS_ATTEMPTS,
  WORK_QUEUE_BRANCH,
  WORK_QUEUE_LOG_PATH,
  ambiguousWrite,
  ledgerProtocol,
  publishWorkQueueCheckpoint,
  publishWorkQueueRequest,
  readQueueContentsAtSHA,
  readWorkQueueLog,
  refConflict,
  validateBranch,
  verifyRepository,
  writeQueueContent,
};
