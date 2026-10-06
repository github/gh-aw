// @ts-check
"use strict";

const { TextDecoder } = require("node:util");
const { createHash } = require("node:crypto");
const { canonical, digest, identity, integer, queueError } = require("./work_queue_codec.cjs");
const { actorFromContext, defaultPolicy, validatePolicy, validateRequestRole, validateTrustedContext } = require("./work_queue_policy.cjs");
const { assignmentsForRequest } = require("./work_queue_scheduler.cjs");
const {
  appendCommit,
  generateRequestOperations,
  newRequest,
  newState,
  parseTransactionLog,
  proposedCommitId,
  replayTransactions,
  serializeTransactionLog,
  validateRequest,
  validateRequestContext,
  validateWorkerContinuation,
} = require("./work_queue_replay.cjs");

const WORK_QUEUE_BRANCH = "work-queue";
const WORK_QUEUE_LOG_PATH = "work-queue.jsonl";
const LEGACY_BRANCHES = ["dispatch-coordinator", "gh-aw-work-queue"];
const LEGACY_LOGS = ["dispatch-work-coordinator.jsonl"];
const DEFAULT_MAX_RETRIES = 5;

function storageSupported(storage = process.env.GH_AW_WORK_QUEUE_STORAGE || "git") {
  if (storage !== "git") throw queueError("unsupported_backend", "only the mandatory fair Git queue backend is supported");
}

function validateBranch(branch) {
  identity(branch, "queue branch");
  if (LEGACY_BRANCHES.includes(branch)) throw queueError("unsupported_protocol", "legacy queue branches are unsupported; explicit new queue initialization is required");
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
  return status === 409 || (status === 422 && /already exists|not a fast.forward|reference update failed/i.test(error.message || ""));
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
  if (response.data.permissions?.pull === false) throw queueError("repository_unavailable", "caller lacks queue repository read visibility");
  return response.data;
}

async function initializationPolicy({ githubClient, owner, repo, actor, policyProposal }) {
  if (policyProposal !== undefined) return validatePolicy(structuredClone(policyProposal));
  const repository = await verifyRepository(githubClient, owner, repo);
  if (typeof repository.default_branch !== "string") throw queueError("policy_missing", "default Policy requires the verified repository default revision or a trusted compiled proposal");
  validateBranch(repository.default_branch);
  const ref = await readRef(githubClient, owner, repo, repository.default_branch);
  if (typeof ref !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(ref)) throw queueError("policy_missing", "default Policy requires an immutable verified repository default revision");
  return defaultPolicy({ repository: actor.repository, principal: actor.principal, ref });
}

async function readRef(githubClient, owner, repo, branch) {
  try {
    const response = await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${branch}` });
    const sha = response?.data?.object?.sha;
    if (typeof sha !== "string" || !sha) throw queueError("ledger_invalid", "queue branch returned no commit identity");
    return sha;
  } catch (error) {
    if (httpStatus(error) === 404) return null;
    throw error;
  }
}

async function readWorkQueueLog({ githubClient, owner, repo, branch = WORK_QUEUE_BRANCH, storage, core: coreApi }) {
  storageSupported(storage);
  validateBranch(branch);
  const repository = await verifyRepository(githubClient, owner, repo);
  const sha = await readRef(githubClient, owner, repo, branch);
  if (!sha) {
    if (repository.default_branch && repository.size !== 0 && !(await readRef(githubClient, owner, repo, repository.default_branch)))
      throw queueError("repository_unavailable", "cannot establish contents access before treating a queue ref as absent");
    // Detect but never adopt/copy/upgrade known unsupported storage.
    if (branch === WORK_QUEUE_BRANCH) {
      for (const legacy of LEGACY_BRANCHES) if (await readRef(githubClient, owner, repo, legacy)) throw queueError("unsupported_protocol", "a legacy queue exists; no implicit storage migration is permitted");
    }
    return { sha: null, treeSha: null, transactions: [], state: newState(), branch, logPath: WORK_QUEUE_LOG_PATH };
  }
  const commit = await githubClient.rest.git.getCommit({ owner, repo, commit_sha: sha });
  const treeSha = commit?.data?.tree?.sha;
  if (typeof treeSha !== "string" || !treeSha) throw queueError("ledger_invalid", "queue commit has no tree");
  const response = await githubClient.rest.git.getTree({ owner, repo, tree_sha: treeSha });
  if (response?.data?.truncated === true || !Array.isArray(response?.data?.tree)) throw queueError("ledger_invalid", "queue tree is missing or truncated");
  const entries = response.data.tree;
  if (entries.length > 16384) throw queueError("resource_limit", "queue tree entry limit exceeded");
  if (entries.some(entry => LEGACY_LOGS.includes(entry.path))) throw queueError("unsupported_protocol", "legacy transaction log cannot be adopted");
  const logs = entries.filter(entry => entry.path === WORK_QUEUE_LOG_PATH);
  if (logs.length !== 1 || logs[0].type !== "blob" || typeof logs[0].sha !== "string" || logs[0].mode !== "100644") throw queueError("ledger_invalid", "existing queue must contain exactly one regular canonical log");
  const blob = await githubClient.rest.git.getBlob({ owner, repo, file_sha: logs[0].sha });
  if (blob?.data?.encoding !== "base64" || typeof blob.data.content !== "string") throw queueError("ledger_invalid", "queue blob encoding is unsupported");
  const encoded = blob.data.content.replace(/\s/g, "");
  if (encoded.length > Math.ceil((80 * 1024 * 1024) / 3) * 4) throw queueError("resource_limit", "queue blob exceeds cold parser limit");
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)) throw queueError("ledger_invalid", "malformed queue blob base64");
  const bytes = Buffer.from(encoded, "base64");
  if (bytes.length > 80 * 1024 * 1024 || (typeof blob.data.size === "number" && blob.data.size !== bytes.length)) throw queueError("ledger_invalid", "queue blob is oversized or truncated");
  let contents;
  try {
    contents = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    throw queueError("ledger_invalid", "queue blob contains malformed UTF-8");
  }
  const transactions = parseTransactionLog(contents);
  if (!transactions.length) throw queueError("policy_missing", "existing queue has an empty/policyless log");
  if (transactions.some(transaction => transaction.actor.repository.toLowerCase() !== repository.full_name.toLowerCase())) throw queueError("actor_unauthorized", "queue log contains an actor from another repository");
  const state = replayTransactions(transactions);
  coreApi?.info(`Work queue: validated ${transactions.length} causal commits`);
  return { sha, treeSha, transactions, state, branch, logPath: WORK_QUEUE_LOG_PATH };
}

function stableRequestResult(current, request, actor) {
  const commit = current.state.requests.get(request.id);
  if (!commit) return null;
  if (commit.request.fingerprint !== request.fingerprint || canonical(commit.request.parameters) !== canonical(request.parameters) || canonical(commit.actor) !== canonical(actor) || commit.request.kind !== request.kind)
    throw queueError("request_reused", "stable request identity has different actor/kind/validated semantics");
  const assignments = assignmentsForRequest(current.state, request.id);
  return { ...current, commit, assignments, operations: commit.operations, publishedNow: false, reused: true, persisted: false, recovered: true, idempotent: true, rejected: [] };
}

async function writeCandidate({ githubClient, owner, repo, current, transactions }) {
  const content = serializeTransactionLog(transactions);
  const blob = await githubClient.rest.git.createBlob({ owner, repo, content, encoding: "utf-8" });
  const tree = await githubClient.rest.git.createTree({ owner, repo, ...(current.treeSha ? { base_tree: current.treeSha } : {}), tree: [{ path: WORK_QUEUE_LOG_PATH, mode: "100644", type: "blob", sha: blob.data.sha }] });
  const commit = await githubClient.rest.git.createCommit({ owner, repo, message: "Publish checked fair work queue request", tree: tree.data.sha, parents: current.sha ? [current.sha] : [] });
  if (current.sha) await githubClient.rest.git.updateRef({ owner, repo, ref: `heads/${current.branch}`, sha: commit.data.sha, force: false });
  else await githubClient.rest.git.createRef({ owner, repo, ref: `refs/heads/${current.branch}`, sha: commit.data.sha });
  return commit.data.sha;
}

async function publishWorkQueueRequest({
  githubClient,
  owner,
  repo,
  branch = WORK_QUEUE_BRANCH,
  storage,
  request,
  context,
  actor = actorFromContext(context),
  generateOperations = generateRequestOperations,
  refreshObservations,
  remediationVerifier,
  policyProposal,
  initializationContext,
  initializeOnly = false,
  maxRetries = DEFAULT_MAX_RETRIES,
  now = Date.now,
  commitId = proposedCommitId,
  sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay)),
  core: coreApi,
}) {
  storageSupported(storage);
  validateBranch(branch);
  integer(maxRetries, 0, 10, "publication retries");
  validateTrustedContext(context, actor);
  if (actor.repository.toLowerCase() !== `${owner}/${repo}`.toLowerCase()) throw queueError("actor_unauthorized", "authenticated context belongs to another repository");
  validateRequest(request, actor);
  validateRequestRole(actor, request.kind);
  if (refreshObservations && (typeof refreshObservations !== "function" || request.kind !== "dispatch_next")) throw queueError("request_invalid", "observation refresh is only supported for stable dispatch_next requests");
  if (remediationVerifier !== undefined && typeof remediationVerifier !== "function") throw queueError("request_invalid", "remediation verification requires a trusted host capability");
  // Capture stable semantic data once. A generator receives a copy, not authority.
  const stable = parseStrictRequest(request);
  const stableActor = structuredClone(actor);
  let observedPrefix = [];
  let lastConflict;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    let current = await readWorkQueueLog({ githubClient, owner, repo, branch, storage, core: coreApi });
    if (observedPrefix.some((commit, index) => !current.transactions[index] || canonical(current.transactions[index]) !== canonical(commit)))
      throw queueError("ledger_invalid", "refreshed queue history does not extend the previously validated prefix");
    observedPrefix = current.transactions;
    const recovered = stableRequestResult(current, stable, stableActor);
    if (recovered) return recovered;
    if (initializeOnly && current.sha) return { ...current, operations: [], assignments: [], publishedNow: false, reused: false, persisted: false, recovered: false, idempotent: false, rejected: [] };
    if (!current.sha) {
      if (stableActor.role === "worker") throw queueError("claim_scope_invalid", "a worker cannot have effective Claim authority in an uninitialized queue");
      if (stable.kind !== "policy") {
        // Genuine genesis only: separate explicit Policy request, never overlay an
        // installed epoch with a compiled proposal after a publication conflict.
        const genesisContext = initializationContext || (context.role === "administrator" ? context : null);
        if (!genesisContext) throw queueError("policy_missing", "genuine genesis requires an explicit approved administrator context");
        const genesisActor = actorFromContext(genesisContext);
        if (genesisActor.role !== "administrator") throw queueError("actor_unauthorized", "trusted Policy initialization requires administrator credentials");
        const installed = await initializationPolicy({ githubClient, owner, repo, actor: genesisActor, policyProposal });
        const seed = createHash("sha256").update(stable.id, "utf8").digest("hex");
        const policyOperation = { kind: "Policy", epoch: `epoch_${seed}`, policy: installed };
        const initRequest = newRequest(`init_${seed}`, "policy", genesisActor, { operations: [policyOperation] });
        await publishWorkQueueRequest({ githubClient, owner, repo, branch, storage, request: initRequest, actor: genesisActor, context: genesisContext, initializeOnly: true, maxRetries, now, commitId, sleepFn, core: coreApi });
        current = await readWorkQueueLog({ githubClient, owner, repo, branch, storage, core: coreApi });
        observedPrefix = current.transactions;
      }
    }
    validateRequestContext(current.state, stable, stableActor);
    if (stableActor.role === "worker" && ["submit", "dispatch_next", "observe"].includes(stable.kind)) validateWorkerContinuation(current.state, context);
    if (stable.kind === "submit")
      for (const node of stable.parameters.nodes) {
        const failed = node.replacement_of && current.state.works.get(node.replacement_of.work_id);
        if (!failed || failed.disposition === "none") continue;
        if (!remediationVerifier) throw queueError("remediation_verifier_required", "partial/unknown effects require trusted domain-specific remediation proof");
        let verified = false;
        try {
          verified = (await remediationVerifier(structuredClone(current.state), structuredClone(node), validateTrustedContext(context, stableActor))) === true;
        } catch {
          throw queueError("remediation_invalid", "trusted replacement proof verification failed");
        }
        if (!verified) throw queueError("remediation_invalid", "trusted replacement proof was not independently confirmed");
      }
    const refresh = refreshObservations ? await refreshObservations(structuredClone(current.state), structuredClone(stable), structuredClone(stableActor)) : [];
    const observations = Array.isArray(refresh) ? refresh : refresh?.operations;
    if (!Array.isArray(observations) || observations.some(operation => operation?.kind !== "Observation")) throw queueError("request_invalid", "observation refresh must return only typed Observation operations");
    const at = integer(now(), 0, Number.MAX_SAFE_INTEGER, "trusted publication time");
    const id = identity(commitId(structuredClone(current.state), structuredClone(stable)), "candidate commit ID");
    const generated = await generateOperations(structuredClone(current.state), structuredClone(stable), structuredClone(stableActor), at, id, structuredClone(observations));
    const decision = Array.isArray(generated) ? { operations: generated } : generated;
    if (!decision || !Array.isArray(decision.operations)) throw queueError("request_invalid", "candidate generator must return operations");
    if (!decision.operations.length) return { ...decision, ...current, operations: [], assignments: [], publishedNow: false, reused: false, persisted: false, recovered: false, idempotent: decision.idempotent === true, rejected: [] };
    if (observations.length && canonical(decision.operations.slice(0, observations.length)) !== canonical(observations)) throw queueError("request_invalid", "candidate must bind every refreshed observation before its Claim prefix");
    const policyOp = decision.operations.find(operation => operation.kind === "Policy");
    const candidate = { version: 3, id, previous: current.state.tip || null, request: stable, actor: stableActor, policy_epoch: policyOp?.epoch ?? current.state.policy_epoch, at, operations: decision.operations };
    const checked = appendCommit(current.transactions, candidate);
    try {
      const sha = await writeCandidate({ githubClient, owner, repo, current, transactions: checked.transactions });
      coreApi?.info(`Work queue: published checked request (${decision.operations.length} operations)`);
      return { ...decision, ...checked, assignments: assignmentsForRequest(checked.state, stable.id), sha, publishedNow: true, reused: false, persisted: true, recovered: false, rejected: [] };
    } catch (error) {
      if (!refConflict(error) && !ambiguousWrite(error)) throw new Error("Failed to publish checked work queue request", { cause: error });
      lastConflict = error;
      // A lost response is not permission to choose another request. Refresh
      // before another candidate, and at exhaustion before reporting uncertainty.
      const refreshed = await readWorkQueueLog({ githubClient, owner, repo, branch, storage, core: coreApi });
      if (observedPrefix.some((commit, index) => !refreshed.transactions[index] || canonical(refreshed.transactions[index]) !== canonical(commit))) throw queueError("ledger_invalid", "queue history was rewritten during publication");
      const committed = stableRequestResult(refreshed, stable, stableActor);
      if (committed) return committed;
      observedPrefix = refreshed.transactions;
      if (attempt < maxRetries) await sleepFn(Math.min(1000, 50 * 2 ** attempt));
    }
  }
  throw new Error("publication_unresolved: queue CAS retries exhausted without a committed stable request", { cause: lastConflict });
}

function parseStrictRequest(request) {
  // Canonical round-trip rejects undefined/prototypes before retry capture.
  const { parseStrictJSON } = require("./work_queue_codec.cjs");
  return parseStrictJSON(canonical(request));
}

function applyAndPublishWorkQueueTransactions(options) {
  if (!options?.request || !options.context || Object.hasOwn(options, "intents")) throw queueError("unsupported_protocol", "fixed standalone intents cannot publish Claims; use stable request + authenticated context");
  return publishWorkQueueRequest(options);
}

async function initializeWorkQueue(options) {
  const actor = actorFromContext(options.context);
  if (actor.role !== "administrator") throw queueError("actor_unauthorized", "trusted Policy initialization requires administrator credentials");
  const branch = options.branch || WORK_QUEUE_BRANCH;
  const policy = await initializationPolicy({ ...options, actor });
  const operation = { kind: "Policy", epoch: options.epoch || "initial", policy };
  const request = newRequest(`init:${digest({ actor, branch, operation })}`, "policy", actor, { operations: [operation] });
  return publishWorkQueueRequest({ ...options, actor, request, initializeOnly: true });
}

module.exports = { WORK_QUEUE_BRANCH, WORK_QUEUE_LOG_PATH, applyAndPublishWorkQueueTransactions, initializeWorkQueue, publishWorkQueueRequest, readWorkQueueLog, stableRequestResult, storageSupported, verifyRepository };
