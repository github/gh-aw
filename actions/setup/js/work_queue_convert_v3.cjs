// @ts-check
"use strict";

const { canonical, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { LEDGER_BYTES, OPERATIONS_PER_COMMIT } = require("./work_queue_limits.cjs");
const { validateActor } = require("./work_queue_policy.cjs");
const { validateWork } = require("./work_queue_graph.cjs");
const { appendCommit, buildCommit, newRequest, newState } = require("./work_queue_replay.cjs");
const { CAS_ATTEMPTS, WORK_QUEUE_BRANCH, ambiguousWrite, readWorkQueueLog, refConflict, writeQueueContent } = require("./work_queue_store.cjs");

/** A v3 log the converter cannot replay completely aborts the conversion; the ref is not updated. */
function abort(message) {
  return queueError("ledger_invalid", `v3 conversion aborted: ${message}`);
}

/**
 * @typedef {{work: any, state: string, barrier: string, order: [number, number]}} V3Entry
 */

/**
 * Replay the Work ownership of a whole v3 log: from its Checkpoint when line 1 is one,
 * otherwise from the v3 genesis. Every line must parse and extend the previous line's
 * commit, and every Work event must name known Work; otherwise the conversion aborts
 * rather than writing a v4 log that silently omits pending Work. Only this module reads v3 fields.
 * @param {string[]} lines
 * @param {number} at conversion time; v3 Work whose deadline passed counts as expired
 * @returns {Map<string, V3Entry>}
 */
function replayV3Work(lines, at) {
  /** @type {Map<string, V3Entry>} */
  const tracked = new Map();
  if (!lines.length) throw abort("the v3 log is empty");
  let ordinal = 0;
  let previous = null;
  let watermark = at;
  let start = 0;
  const first = parseV3Commit(lines[0], 1);
  if (first.operations?.[0]?.kind === "Checkpoint") {
    if (first.previous !== null || first.operations.length !== 1) throw abort("a v3 Checkpoint must be the sole operation of the root commit");
    const snapshot = first.operations[0].state;
    const works = snapshot?.works;
    if (!works || typeof works !== "object" || Array.isArray(works)) throw queueError("checkpoint_invalid", "v3 checkpoint has no Work map");
    for (const work of Object.values(works)) {
      if (typeof work?.work_id !== "string" || typeof work.state !== "string") throw queueError("checkpoint_invalid", "v3 checkpoint Work record is inconsistent");
      tracked.set(work.work_id, { work, state: work.state, barrier: work.barrier ?? "none", order: [work.position?.commit ?? 0, work.position?.operation ?? 0] });
    }
    ordinal = Math.max(0, ...[...tracked.values()].map(entry => entry.order[0])) + 1;
    if (Number.isSafeInteger(snapshot.last_at)) watermark = Math.max(watermark, snapshot.last_at);
    previous = first.id;
    start = 1;
  }
  for (let index = start; index < lines.length; index++) {
    const commit = index === 0 ? first : parseV3Commit(lines[index], index + 1);
    if (commit.previous !== previous) throw abort(index === 0 ? "line 1 is neither the v3 genesis nor a v3 Checkpoint" : `line ${index + 1} does not extend the previous v3 commit`);
    if (!Array.isArray(commit.operations)) throw abort(`line ${index + 1} has no operations`);
    if (Number.isSafeInteger(commit.at)) watermark = Math.max(watermark, commit.at);
    for (const [position, operation] of commit.operations.entries()) applyV3Operation(tracked, operation, [ordinal, position], index + 1);
    previous = commit.id;
    ordinal++;
  }
  // v3 replay cancels available Work at its deadline (work_queue_expiration.cjs in v3).
  for (const entry of tracked.values()) {
    if (entry.state === "available" && entry.work.expires_at !== undefined && watermark >= entry.work.expires_at && !["pending", "failed"].includes(entry.barrier)) entry.state = "cancelled";
  }
  return tracked;
}

/** @param {string} line @param {number} number */
function parseV3Commit(line, number) {
  let commit;
  try {
    commit = parseStrictJSON(line, { maxBytes: LEDGER_BYTES });
  } catch {
    throw abort(`line ${number} is unreadable`);
  }
  if (commit?.version !== 3 || typeof commit.id !== "string") throw abort(`line ${number} is not a v3 commit`);
  return commit;
}

/**
 * The v3 lifecycle effect of one operation on its Work's state and delivery barrier.
 * @param {Map<string, V3Entry>} tracked @param {any} operation @param {[number, number]} order @param {number} line
 */
function applyV3Operation(tracked, operation, order, line) {
  const kind = operation?.kind;
  if (kind === "Checkpoint") throw abort(`line ${line} carries a Checkpoint after the log root`);
  if (kind === "Work") {
    if (typeof operation.work_id !== "string") throw abort(`line ${line} has Work without an ID`);
    if (!tracked.has(operation.work_id)) tracked.set(operation.work_id, { work: operation, state: "available", barrier: "none", order });
    return;
  }
  if (!["Claim", "ClaimCancellation", "Completion", "Result", "DeliveryFailure", "WorkCancellation", "WorkPriority", "IssueLink"].includes(kind)) return;
  const entry = tracked.get(operation.work_id);
  if (!entry) throw abort(`line ${line} names unknown Work ${operation.work_id}`);
  switch (kind) {
    case "Claim":
      entry.state = "claimed";
      break;
    case "ClaimCancellation":
      if (entry.state === "claimed") entry.state = "available";
      break;
    case "Completion":
      entry.state = operation.keep_open === true ? "available" : "completed";
      entry.barrier = "pending";
      break;
    case "Result":
      // A keep-open Work's verified Result reopens its delivery barrier for the next round.
      entry.barrier = entry.state === "available" ? "none" : "verified";
      break;
    case "DeliveryFailure":
      entry.barrier = "failed";
      break;
    case "WorkCancellation":
      entry.state = "cancelled";
      break;
    case "WorkPriority":
      entry.work = { ...entry.work, effective_priority: operation.priority };
      break;
    case "IssueLink":
      if (!entry.work.backing_issue) entry.work = { ...entry.work, issue_link: operation.resource };
      break;
  }
}

/**
 * Why a v3 predecessor does not satisfy its dependents, or undefined when v3 would
 * let them run: it completed and its Result was verified.
 * @param {V3Entry | undefined} entry
 */
function unresolvedPredecessor(entry) {
  if (!entry) return "is not in the v3 ledger";
  if (entry.state === "completed") {
    if (entry.barrier === "verified") return undefined;
    return entry.barrier === "failed" ? "failed delivery in v3" : "completed in v3 without a verified Result";
  }
  if (entry.state === "cancelled") return "was cancelled in v3";
  if (entry.state === "claimed") return "was still claimed by a v3 run";
  return "is not carried";
}

/**
 * Map pending v3 Work to v4 Work operations: drop v3-only fields, carry the linked Issue,
 * and keep only Work whose pool and worker profile the compiled policy still routes.
 * A Work edge to a predecessor that completed with a verified v3 Result is satisfied and
 * dropped from `depends_on` (the v3 Result is not forwarded as a v4 result reference);
 * edges to carried Work stay. Any other predecessor can never complete in v4, so its
 * dependents, and transitively theirs, are skipped with a warning each.
 * @param {Map<string, V3Entry>} tracked
 * @param {import("./work_queue_policy.cjs").PolicyEnv} policy
 * @param {(message: string) => void} warn
 */
function v4WorkOperations(tracked, policy, warn) {
  const pending = [...tracked.values()].filter(entry => entry.state === "available").sort((a, b) => a.order[0] - b.order[0] || a.order[1] - b.order[1]);
  for (const entry of tracked.values()) {
    if (entry.state === "claimed") warn(`v3 Work ${entry.work.work_id} skipped: it is still claimed by a v3 run that cannot finish into the v4 log`);
  }
  const carried = new Map();
  for (const { work } of pending) {
    if (!policy.pools[work.pool]?.profiles?.[work.worker_profile]) {
      warn(`v3 Work ${work.work_id} skipped: pool ${work.pool} / profile ${work.worker_profile} is not configured`);
      continue;
    }
    if (work.expires_at !== undefined) warn(`v3 Work ${work.work_id} carried without its deadline: v4 Work does not expire`);
    const backing = work.backing_issue ?? work.issue_link;
    const operation = {
      kind: "Work",
      work_id: work.work_id,
      graph_id: work.graph_id,
      node_key: work.node_key,
      pool: work.pool,
      priority: work.effective_priority ?? work.priority,
      fairness_key: work.fairness_key,
      worker_profile: work.worker_profile,
      payload: work.payload,
      depends_on: work.depends_on ?? [],
      ...(work.subject === undefined ? {} : { subject: work.subject }),
      ...(backing === undefined ? {} : { backing_issue: backing }),
      ...(work.replacement_of?.work_id === undefined ? {} : { replacement_of: { work_id: work.replacement_of.work_id } }),
    };
    try {
      carried.set(operation.work_id, validateWork(operation));
    } catch (error) {
      warn(`v3 Work ${operation.work_id} skipped: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  let changed = true;
  while (changed) {
    changed = false;
    for (const [id, operation] of carried) {
      for (const edge of operation.depends_on) {
        if (edge.kind !== "work" || carried.has(edge.work_id)) continue;
        const why = unresolvedPredecessor(tracked.get(edge.work_id));
        if (why === undefined) continue;
        warn(`v3 Work ${id} skipped: predecessor ${edge.work_id} ${why}`);
        carried.delete(id);
        changed = true;
        break;
      }
    }
  }
  const operations = [];
  for (const [id, operation] of carried) {
    const satisfied = operation.depends_on.filter(edge => edge.kind === "work" && !carried.has(edge.work_id));
    for (const edge of satisfied) warn(`v3 Work ${id}: predecessor ${edge.work_id} completed with a verified v3 Result; the edge is satisfied and the Result is not forwarded`);
    operations.push(satisfied.length ? { ...operation, depends_on: operation.depends_on.filter(edge => !satisfied.includes(edge)) } : operation);
  }
  return operations;
}

/**
 * Build the replacement v4 log content (possibly empty, when no v3 Work is pending) for
 * a v3 log. Throws ledger_invalid, before any write, when the v3 log cannot be replayed completely.
 * @param {string} contents
 * @param {import("./work_queue_policy.cjs").QueueActor} actor administrator actor
 * @param {import("./work_queue_policy.cjs").PolicyEnv} policy
 * @param {(message: string) => void} warn
 * @param {number} at
 */
function convertV3Contents(contents, actor, policy, warn, at) {
  const lines = contents.split("\n").filter(line => line.length);
  const operations = v4WorkOperations(replayV3Work(lines, at), policy, warn);
  let state = newState();
  const commits = [];
  for (let offset = 0; offset < operations.length; offset += OPERATIONS_PER_COMMIT) {
    const chunk = operations.slice(offset, offset + OPERATIONS_PER_COMMIT);
    const request = newRequest("migrate", actor, { work_ids: chunk.map(operation => operation.work_id) });
    const commit = buildCommit(state, actor, request, chunk, at);
    state = appendCommit(state, commit);
    commits.push(commit);
  }
  return { content: commits.map(commit => canonical(commit) + "\n").join(""), commits, state };
}

/**
 * One-shot conversion of a v3 queue head into a v4 log. The new content is written as a
 * commit whose Git parent is the v3 head, so the archived v3 log stays reachable.
 * @param {{githubClient: any, owner: string, repo: string, branch?: string, actor: import("./work_queue_policy.cjs").QueueActor, policy: import("./work_queue_policy.cjs").PolicyEnv, core?: {warning?: (message: string) => void, info?: (message: string) => void}, now?: () => number}} options
 * @returns {Promise<{converted: boolean, carried?: number, sha?: string}>}
 */
async function convertV3Ledger({ githubClient, owner, repo, branch = WORK_QUEUE_BRANCH, actor, policy, core: coreApi = undefined, now = Date.now }) {
  const administrator = validateActor({ ...actor, role: "administrator" });
  const warn = message => (coreApi?.warning ?? console.warn)(message);
  for (let attempt = 1; attempt <= CAS_ATTEMPTS; attempt++) {
    const current = await readWorkQueueLog({ githubClient, owner, repo, branch, core: coreApi, allowLegacy: true });
    if (current.protocol !== 3) return { converted: false };
    const { content, commits } = convertV3Contents(current.contents, administrator, policy, warn, now());
    try {
      const sha = await writeQueueContent({ githubClient, owner, repo, branch, parent: current.sha, treeSha: null, content, message: "work queue: convert v3 ledger to v4" });
      const carried = commits.reduce((total, commit) => total + commit.operations.length, 0);
      coreApi?.info?.(`Work queue: converted the v3 ledger; carried ${carried} available Work`);
      return { converted: true, carried, sha };
    } catch (error) {
      if (!refConflict(error) && !ambiguousWrite(error)) throw error;
    }
  }
  throw queueError("cas_exhausted", "work queue head changed during v3 conversion");
}

module.exports = { convertV3Contents, convertV3Ledger };
