// @ts-check
"use strict";

const crypto = require("node:crypto");
const { TextDecoder } = require("node:util");
const { compactTransactions, deriveWorkId, parseTransactionLog, replayTransactions, serializeTransactions } = require("./dispatch_work_coordinator.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");

const COORDINATOR_FILE = "dispatch-work-coordinator.jsonl";
const MAX_ATTEMPTS = 5;
const TERMINAL_STATES = new Set(["completed", "cancelled"]);

function getStatus(error) {
  return error && typeof error === "object" ? (error.status ?? error.response?.status) : undefined;
}

function isNotFound(error) {
  return getStatus(error) === 404;
}

function isConflict(error) {
  const status = getStatus(error);
  const message = error instanceof Error ? error.message : String(error);
  return status === 409 || (status === 422 && /reference already exists|reference update failed|non-fast-forward/i.test(message));
}

function coordinatorBranchName(identity) {
  if (typeof identity !== "string" || !identity.trim() || Buffer.byteLength(identity) > 1024) {
    throw new TypeError("Invalid coordinator identity");
  }
  return `gh-aw/dispatch-work/${crypto.createHash("sha256").update(identity).digest("hex").slice(0, 32)}`;
}

function resolveCoordinatorIdentity(workflowRef, coordinatorId) {
  if (coordinatorId !== undefined && coordinatorId !== "") {
    if (typeof coordinatorId !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(coordinatorId)) {
      throw new TypeError("Invalid coordinator ID");
    }
    return coordinatorId;
  }
  if (typeof workflowRef !== "string" || !workflowRef.trim()) throw new TypeError("Invalid coordinator workflow identity");
  return workflowRef.split("@", 1)[0];
}

function decodeContent(content) {
  if (!content || content.type !== "file" || content.encoding !== "base64" || typeof content.content !== "string") {
    throw new TypeError("Coordinator branch has an invalid canonical file");
  }
  const encoded = content.content.replace(/\n/g, "");
  if (encoded && !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)) {
    throw new TypeError("Coordinator branch has invalid file encoding");
  }
  return new TextDecoder("utf-8", { fatal: true }).decode(Buffer.from(encoded, "base64"));
}

async function readSnapshot({ githubClient, owner, repo, branchName, workSchema }) {
  let ref;
  try {
    ({ data: ref } = await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${branchName}` }));
  } catch (error) {
    if (isNotFound(error)) return { headSha: null, transactions: [], projection: replayTransactions([]) };
    throw error;
  }
  const headSha = ref?.object?.sha;
  if (typeof headSha !== "string" || !headSha) throw new TypeError("Coordinator branch has an invalid HEAD");

  const { data: commit } = await githubClient.rest.git.getCommit({ owner, repo, commit_sha: headSha });
  const treeSha = commit?.tree?.sha;
  if (typeof treeSha !== "string" || !treeSha) throw new TypeError("Coordinator branch has an invalid commit tree");
  const { data: tree } = await githubClient.rest.git.getTree({ owner, repo, tree_sha: treeSha, recursive: "1" });
  const entries = tree?.tree;
  if (!Array.isArray(entries) || tree.truncated === true || entries.length !== 1 || entries[0]?.path !== COORDINATOR_FILE || entries[0]?.type !== "blob" || entries[0]?.mode !== "100644" || typeof entries[0]?.sha !== "string") {
    throw new TypeError("Coordinator branch must contain only the canonical transaction log");
  }
  const { data: content } = await githubClient.rest.git.getBlob({ owner, repo, file_sha: entries[0].sha });
  const rawContent = decodeContent(content);
  const transactions = parseTransactionLog(rawContent);
  for (const transaction of transactions) {
    if (transaction.type === "Work" && validateValueAgainstSchema(transaction.work, workSchema)) {
      throw new TypeError("Coordinator branch contains Work that violates its configured schema");
    }
  }
  return { headSha, rawContent, transactions, projection: replayTransactions(transactions) };
}

async function writeSnapshot({ githubClient, owner, repo, branchName, headSha, transactions }) {
  const content = serializeTransactions(transactions);
  const { data: blob } = await githubClient.rest.git.createBlob({
    owner,
    repo,
    content,
    encoding: "utf-8",
  });
  const { data: tree } = await githubClient.rest.git.createTree({
    owner,
    repo,
    tree: [{ path: COORDINATOR_FILE, mode: "100644", type: "blob", sha: blob.sha }],
  });
  const commitOptions = {
    owner,
    repo,
    message: "Update Dispatch Work Coordinator",
    tree: tree.sha,
    parents: headSha ? [headSha] : [],
  };
  const { data: commit } = await githubClient.rest.git.createCommit(commitOptions);
  if (headSha) {
    await githubClient.rest.git.updateRef({
      owner,
      repo,
      ref: `heads/${branchName}`,
      sha: commit.sha,
      force: false,
    });
  } else {
    await githubClient.rest.git.createRef({
      owner,
      repo,
      ref: `refs/heads/${branchName}`,
      sha: commit.sha,
    });
  }
}

class DispatchWorkCoordinator {
  constructor({ githubClient, owner, repo, identity, runId, workflowId, workSchema, maxAttempts = MAX_ATTEMPTS, sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds)), random = Math.random }) {
    if (!githubClient?.rest?.git || !githubClient?.rest?.repos) throw new TypeError("Coordinator requires a GitHub client");
    if (typeof owner !== "string" || !owner || typeof repo !== "string" || !repo) throw new TypeError("Coordinator requires a repository");
    if (!Number.isSafeInteger(maxAttempts) || maxAttempts < 1 || maxAttempts > MAX_ATTEMPTS) throw new TypeError("Invalid coordinator retry limit");
    this.githubClient = githubClient;
    this.owner = owner;
    this.repo = repo;
    this.branchName = coordinatorBranchName(identity);
    this.runId = runId;
    this.workflowId = workflowId;
    this.workSchema = workSchema;
    this.maxAttempts = maxAttempts;
    this.sleep = sleep;
    this.random = random;
    if (typeof runId !== "string" || !runId || typeof workflowId !== "string" || !workflowId) {
      throw new TypeError("Coordinator requires trusted workflow-run provenance");
    }
    if (!workSchema || typeof workSchema !== "object" || Array.isArray(workSchema)) throw new TypeError("Coordinator requires a Work schema");
  }

  async read() {
    return readSnapshot({
      githubClient: this.githubClient,
      owner: this.owner,
      repo: this.repo,
      branchName: this.branchName,
      workSchema: this.workSchema,
    });
  }

  async mutate(buildTransactions) {
    for (let attempt = 0; attempt < this.maxAttempts; attempt++) {
      const snapshot = await this.read();
      const nextTransactions = buildTransactions(snapshot.transactions, snapshot.projection);
      if (!Array.isArray(nextTransactions)) throw new TypeError("Coordinator mutation did not return transactions");
      replayTransactions(nextTransactions);
      if (serializeTransactions(nextTransactions) === serializeTransactions(snapshot.transactions)) return snapshot;

      try {
        await writeSnapshot({
          githubClient: this.githubClient,
          owner: this.owner,
          repo: this.repo,
          branchName: this.branchName,
          headSha: snapshot.headSha,
          transactions: nextTransactions,
        });
        return await this.read();
      } catch (error) {
        if (!isConflict(error)) throw error;
        if (attempt + 1 === this.maxAttempts) throw new Error("Coordinator update failed after bounded concurrency retries", { cause: error });
        const exponentialDelay = Math.min(1000, 50 * 2 ** attempt);
        await this.sleep(exponentialDelay + Math.floor(this.random() * exponentialDelay));
      }
    }
    throw new Error("Coordinator update failed after bounded concurrency retries");
  }

  async compact() {
    for (let attempt = 0; attempt < this.maxAttempts; attempt++) {
      const snapshot = await this.read();
      if (!snapshot.headSha) return snapshot.projection;
      const compacted = compactTransactions(snapshot.transactions);
      if (snapshot.rawContent === serializeTransactions(compacted)) return snapshot.projection;

      try {
        await writeSnapshot({
          githubClient: this.githubClient,
          owner: this.owner,
          repo: this.repo,
          branchName: this.branchName,
          headSha: snapshot.headSha,
          transactions: compacted,
        });
        return (await this.read()).projection;
      } catch (error) {
        if (!isConflict(error)) throw error;
        if (attempt + 1 === this.maxAttempts) throw new Error("Coordinator compaction failed after bounded concurrency retries", { cause: error });
        const exponentialDelay = Math.min(1000, 50 * 2 ** attempt);
        await this.sleep(exponentialDelay + Math.floor(this.random() * exponentialDelay));
      }
    }
    throw new Error("Coordinator compaction failed after bounded concurrency retries");
  }

  async recoverOrphanClaims(getWorkflowRun) {
    if (typeof getWorkflowRun !== "function") throw new TypeError("Orphan recovery requires a workflow-run lookup");
    const snapshot = await this.read();
    const candidates = snapshot.projection.works.filter(work => work.state === "claimed").flatMap(work => work.claims.filter(claim => claim.claim_id === work.effective_claim_id && claim.state === "effective"));
    const recovered = [];
    for (const claim of candidates) {
      let response;
      try {
        response = await getWorkflowRun(claim.run_id);
      } catch (error) {
        const status = getStatus(error);
        if (status === 404) continue;
        throw error;
      }
      const run = response?.data ?? response;
      const workflowPrefix = `${this.owner}/${this.repo}/`;
      const claimWorkflowPath = claim.workflow_id.startsWith(workflowPrefix) ? claim.workflow_id.slice(workflowPrefix.length).split("@", 1)[0] : "";
      if (!run || String(run.id) !== claim.run_id || run.status !== "completed" || typeof run.path !== "string" || claimWorkflowPath !== run.path.split("@", 1)[0]) {
        continue;
      }
      const work = await this.cancelClaim(claim.claim_id);
      const cancelledClaim = work?.claims.find(item => item.claim_id === claim.claim_id);
      if (cancelledClaim?.state === "cancelled") recovered.push(claim.claim_id);
    }
    return recovered;
  }

  async submit(work) {
    if (validateValueAgainstSchema(work, this.workSchema)) throw new TypeError("Work payload does not match its configured schema");
    const workId = deriveWorkId(work);
    const snapshot = await this.mutate((transactions, projection) => {
      if (projection.works.some(item => item.work_id === workId)) return transactions;
      return [...transactions, { type: "Work", work_id: workId, work }];
    });
    return { work_id: workId, ...snapshot.projection.works.find(item => item.work_id === workId) };
  }

  async get(workId) {
    const snapshot = await this.read();
    return snapshot.projection.works.find(item => item.work_id === workId) || null;
  }

  async list(filter = {}) {
    const snapshot = await this.read();
    const allowedStates = new Set(["available", "claimed", "completed", "cancelled"]);
    if (filter.state !== undefined && !allowedStates.has(filter.state)) throw new TypeError("Invalid coordinator Work state filter");
    return snapshot.projection.works.filter(item => filter.state === undefined || item.state === filter.state);
  }

  async status() {
    return (await this.read()).projection;
  }

  async claim(workId) {
    const claimId = crypto.randomUUID();
    const assignment = { type: "Claim", claim_id: claimId, work_id: workId, run_id: this.runId, workflow_id: this.workflowId };
    const snapshot = await this.mutate((transactions, projection) => {
      const work = projection.works.find(item => item.work_id === workId);
      if (!work) throw new TypeError("Unknown Work item");
      if (TERMINAL_STATES.has(work.state)) throw new TypeError("Work is terminal and cannot be claimed");
      if (work.state !== "available") throw new TypeError("Work is already claimed and cannot be claimed again");
      return [...transactions, assignment];
    });
    const work = snapshot.projection.works.find(item => item.work_id === workId);
    const claimResult = work?.claims.find(item => item.claim_id === claimId);
    if (!work || !claimResult) throw new Error("Coordinator could not verify the persisted Claim");
    return { work_id: workId, claim_id: claimId, claim_state: claimResult.state, assigned: claimResult.state === "effective", work: claimResult.state === "effective" ? work.work : undefined };
  }

  async claimNext(filter = {}) {
    if (filter.state !== undefined && filter.state !== "available") throw new TypeError("claim_next only accepts available Work");
    let assignment;
    const snapshot = await this.mutate((transactions, projection) => {
      const eligible = projection.works.find(item => item.state === "available");
      if (!eligible) {
        assignment = undefined;
        return transactions;
      }
      assignment = {
        type: "Claim",
        claim_id: crypto.randomUUID(),
        work_id: eligible.work_id,
        run_id: this.runId,
        workflow_id: this.workflowId,
      };
      return [...transactions, assignment];
    });
    if (!assignment) return null;
    const work = snapshot.projection.works.find(item => item.work_id === assignment.work_id);
    const claimResult = work?.claims.find(item => item.claim_id === assignment.claim_id);
    if (!work || !claimResult) throw new Error("Coordinator could not verify the persisted Claim");
    return { work_id: work.work_id, claim_id: assignment.claim_id, claim_state: claimResult.state, assigned: claimResult.state === "effective", work: claimResult.state === "effective" ? work.work : undefined };
  }

  async cancel(workId) {
    const snapshot = await this.mutate((transactions, projection) => {
      const work = projection.works.find(item => item.work_id === workId);
      if (!work) throw new TypeError("Unknown Work item");
      if (work.state === "completed") throw new TypeError("Completed Work cannot be cancelled");
      if (work.state === "cancelled") return transactions;
      return [...transactions, { type: "WorkCancellation", work_id: workId }];
    });
    return snapshot.projection.works.find(item => item.work_id === workId) || null;
  }

  async finishClaim(claimId, outcome) {
    const completion = { type: "Completion", claim_id: claimId };
    if (outcome !== undefined) completion.outcome = outcome;
    const snapshot = await this.mutate((transactions, projection) => {
      const claim = projection.works.flatMap(work => work.claims).find(item => item.claim_id === claimId);
      const work = claim && projection.works.find(item => item.claims.some(item => item.claim_id === claimId));
      if (!claim || !work) throw new TypeError("Unknown Claim");
      if (work.state !== "claimed" || work.effective_claim_id !== claimId || claim.state !== "effective") {
        throw new TypeError("Claim is not currently effective");
      }
      return [...transactions, completion];
    });
    const work = snapshot.projection.works.find(item => item.claims.some(item => item.claim_id === claimId));
    if (!work || work.state !== "completed" || work.effective_claim_id !== claimId) {
      throw new Error("Coordinator could not verify the persisted Completion");
    }
    return work;
  }

  async cancelClaim(claimId) {
    const snapshot = await this.mutate((transactions, projection) => {
      const work = projection.works.find(item => item.claims.some(claim => claim.claim_id === claimId));
      const claim = work?.claims.find(item => item.claim_id === claimId);
      if (!claim || !work) throw new TypeError("Unknown Claim");
      if (claim.state !== "effective" || work.state !== "claimed" || work.effective_claim_id !== claimId) return transactions;
      return [...transactions, { type: "ClaimCancellation", claim_id: claimId }];
    });
    return snapshot.projection.works.find(item => item.claims.some(claim => claim.claim_id === claimId)) || null;
  }
}

module.exports = {
  COORDINATOR_FILE,
  DispatchWorkCoordinator,
  coordinatorBranchName,
  isConflict,
  resolveCoordinatorIdentity,
  readSnapshot,
  writeSnapshot,
};
