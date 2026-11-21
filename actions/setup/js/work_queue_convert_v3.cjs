// @ts-check
"use strict";

const { canonical, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { LEDGER_BYTES, OPERATIONS_PER_COMMIT } = require("./work_queue_limits.cjs");
const { validateActor } = require("./work_queue_policy.cjs");
const { validateWork } = require("./work_queue_graph.cjs");
const { appendCommit, buildCommit, newRequest, newState } = require("./work_queue_replay.cjs");
const { CAS_ATTEMPTS, WORK_QUEUE_BRANCH, ambiguousWrite, readWorkQueueLog, refConflict, writeQueueContent } = require("./work_queue_store.cjs");

/**
 * Pending Work of a v3 log: the Checkpoint's available Work, adjusted by the
 * ownership events of any later v3 lines. Only this module reads v3 fields.
 * @param {string[]} lines
 * @param {(message: string) => void} warn
 */
function pendingV3Work(lines, warn) {
  const first = parseStrictJSON(lines[0], { maxBytes: LEDGER_BYTES });
  if (first?.version !== 3 || first.operations?.[0]?.kind !== "Checkpoint") {
    warn("v3 ledger without checkpoint; pending Work not carried");
    return [];
  }
  const works = first.operations[0].state?.works;
  if (!works || typeof works !== "object" || Array.isArray(works)) throw queueError("checkpoint_invalid", "v3 checkpoint has no Work map");
  /** @type {Map<string, {work: any, state: string, order: [number, number]}>} */
  const tracked = new Map();
  for (const work of Object.values(works)) {
    tracked.set(work.work_id, { work, state: work.state, order: [work.position?.commit ?? 0, work.position?.operation ?? 0] });
  }
  let ordinal = Math.max(0, ...[...tracked.values()].map(entry => entry.order[0])) + 1;
  for (const [index, line] of lines.slice(1).entries()) {
    let commit;
    try {
      commit = parseStrictJSON(line, { maxBytes: LEDGER_BYTES });
    } catch {
      warn(`v3 ledger line ${index + 2} is unreadable; later ownership changes not applied`);
      break;
    }
    for (const [position, operation] of (commit?.operations ?? []).entries()) {
      const entry = tracked.get(operation?.work_id);
      switch (operation?.kind) {
        case "Work":
          if (!entry) tracked.set(operation.work_id, { work: operation, state: "available", order: [ordinal, position] });
          break;
        case "Claim":
          if (entry) entry.state = "claimed";
          break;
        case "ClaimCancellation":
          if (entry && entry.state === "claimed") entry.state = "available";
          break;
        case "Completion":
          if (entry) entry.state = operation.keep_open === true ? "available" : "completed";
          break;
        case "WorkCancellation":
          if (entry) entry.state = "cancelled";
          break;
      }
    }
    ordinal++;
  }
  return [...tracked.values()].filter(entry => entry.state === "available").sort((a, b) => a.order[0] - b.order[0] || a.order[1] - b.order[1]);
}

/**
 * Map v3 Work to v4 Work operations: drop v3-only fields, carry the linked Issue,
 * and keep only Work whose pool and worker profile the compiled policy still routes.
 * @param {Array<{work: any}>} entries
 * @param {import("./work_queue_policy.cjs").PolicyEnv} policy
 * @param {(message: string) => void} warn
 */
function v4WorkOperations(entries, policy, warn) {
  const carried = new Map();
  for (const { work } of entries) {
    if (!policy.pools[work.pool]?.profiles?.[work.worker_profile]) {
      warn(`v3 Work ${work.work_id} skipped: pool ${work.pool} / profile ${work.worker_profile} is not configured`);
      continue;
    }
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
    carried.set(operation.work_id, operation);
  }
  // A Work edge to an uncarried predecessor can never be satisfied: drop the dependent too.
  let changed = true;
  while (changed) {
    changed = false;
    for (const [id, operation] of carried) {
      const missing = operation.depends_on.find(edge => edge.kind === "work" && !carried.has(edge.work_id));
      if (missing) {
        warn(`v3 Work ${id} skipped: predecessor ${missing.work_id} is not carried`);
        carried.delete(id);
        changed = true;
      }
    }
  }
  const operations = [];
  for (const [id, operation] of carried) {
    try {
      operations.push(validateWork(operation));
    } catch (error) {
      warn(`v3 Work ${id} skipped: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return operations;
}

/**
 * Build the replacement v4 log content (possibly empty) for a v3 log.
 * @param {string} contents
 * @param {import("./work_queue_policy.cjs").QueueActor} actor administrator actor
 * @param {import("./work_queue_policy.cjs").PolicyEnv} policy
 * @param {(message: string) => void} warn
 * @param {number} at
 */
function convertV3Contents(contents, actor, policy, warn, at) {
  const lines = contents.split("\n").filter(line => line.length);
  const operations = v4WorkOperations(pendingV3Work(lines, warn), policy, warn);
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
