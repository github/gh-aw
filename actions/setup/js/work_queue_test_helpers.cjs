"use strict";

const { appendCommit, buildCommit, cloneState, newRequest, replayTransactions } = require("./work_queue_replay.cjs");
const { gateKey, nodeId } = require("./work_queue_graph.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");
const { createHash } = require("node:crypto");
const { canonical } = require("./work_queue_codec.cjs");
const { registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
const { readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");

const repository = "owner/repo";
const WORKER_WORKFLOW = ".github/workflows/worker.lock.yml";
const administrator = { role: "administrator", repository };
const producer = { role: "producer", repository, workflow: ".github/workflows/producer.lock.yml", run_id: "10", run_attempt: 1 };
const dispatcher = { role: "dispatcher", repository, workflow: ".github/workflows/dispatcher.lock.yml", run_id: "100", run_attempt: 1 };
const reconciler = { role: "reconciler", repository, workflow: ".github/workflows/agentics-maintenance.yml", run_id: "300", run_attempt: 1 };
const projector = { role: "projector", repository, workflow: ".github/workflows/dispatcher.lock.yml", run_id: "100", run_attempt: 1 };
const PROFILES = { worker: { workflow: WORKER_WORKFLOW } };
/** The head_sha of every launched test worker run. */
const WORKER_REVISION = "c".repeat(40);

/** A worker actor for one run attempt. */
function workerActor(runId = "200", runAttempt = 1, workflow = WORKER_WORKFLOW) {
  return { role: "worker", repository, workflow, run_id: runId, run_attempt: runAttempt };
}

/** A v4 Work operation in graph "graph", pool "default", profile "worker". */
function work(key, overrides = {}) {
  const graphId = overrides.graph_id ?? "graph";
  return { kind: "Work", work_id: nodeId(graphId, key), graph_id: graphId, node_key: key, pool: "default", priority: 3, fairness_key: "", worker_profile: "worker", payload: { task: key }, depends_on: [], ...overrides };
}

/** Append a validated commit for `kind`/`parameters`/`operations` to a log. */
function commit(log, actor, kind, parameters, operations, at = 1) {
  const state = replayTransactions(log);
  const next = buildCommit(state, actor, newRequest(kind, actor, parameters), operations, at);
  appendCommit(state, next);
  return [...log, next];
}

function submit(log, works, actor = producer) {
  return commit(log, actor, "submit", { work_ids: works.map(node => node.work_id) }, works);
}

function dispatchParameters(overrides = {}) {
  return { pool: "default", max_claims: 16, max_dispatches: 16, concurrency: 16, per_account_limit: 0, profiles: PROFILES, ...overrides };
}

/** Plan (after applying `observations`) and commit one dispatch_next; returns the log and decision. */
function dispatchNext(log, overrides = {}, actor = dispatcher, observations = []) {
  const parameters = dispatchParameters(overrides);
  const request = newRequest("dispatch_next", actor, parameters);
  const state = replayTransactions(log);
  const observed = cloneState(state);
  for (const observation of observations) observed.observations.set(gateKey(observation.resource, observation.condition), observation);
  const decision = planDispatch(observed, parameters, request.id);
  const operations = [...observations, ...decision.operations];
  if (!operations.length) return { log, decision };
  const next = buildCommit(state, actor, request, operations, 1);
  appendCommit(state, next);
  return { log: [...log, next], decision };
}

/** Record a launch: `Dispatch{started}` naming the launched run (default: workerActor()'s run "200"), or a launch_failed Release. */
function launch(log, dispatchId, started = true, actor = dispatcher, runId = "200", revision = WORKER_REVISION) {
  return commit(
    log,
    actor,
    "launch",
    { dispatch_id: dispatchId },
    started ? [{ kind: "Dispatch", dispatch_id: dispatchId, state: "started", run_id: runId, revision }] : [{ kind: "Release", dispatch_id: dispatchId, reason: "launch_failed" }]
  );
}

function bind(log, dispatchId, actor = workerActor()) {
  return commit(log, actor, "bind", { dispatch_id: dispatchId }, [{ kind: "Bound", dispatch_id: dispatchId, run_id: actor.run_id, run_attempt: actor.run_attempt }]);
}

/** Finish every open Claim of a Dispatch: "completed" or "failed" per handle (default completed). */
function finish(log, dispatchId, actor = workerActor(), outcomes = {}, effects = []) {
  const state = replayTransactions(log);
  const dispatch = state.dispatches.get(dispatchId);
  const operations = [];
  for (const claim of dispatch.claims.map(id => state.claims.get(id)).filter(member => member.state === "open")) {
    if ((outcomes[claim.handle] ?? "completed") === "completed") operations.push({ kind: "Completion", work_id: claim.work_id, claim_id: claim.claim_id, dispatch_id: dispatchId, claim_handle: claim.handle, effects });
    else operations.push({ kind: "ClaimCancellation", work_id: claim.work_id, claim_id: claim.claim_id, reason: "worker_failed" });
  }
  return commit(log, actor, "finish", { dispatch_id: dispatchId }, operations);
}

function release(log, dispatchId, reason = "run_missing", actor = reconciler, extra = []) {
  return commit(log, actor, "release", { dispatch_id: dispatchId, reason }, [{ kind: "Release", dispatch_id: dispatchId, reason }, ...extra]);
}

/** Canonical JSONL text of a log. */
function ledgerText(log) {
  return log.map(entry => canonical(entry) + "\n").join("");
}

/** The Actions API view of a launched worker run: a workflow_dispatch run of the worker at WORKER_REVISION. */
function workflowRun(runId, overrides = {}) {
  return { id: Number(runId), path: WORKER_WORKFLOW, event: "workflow_dispatch", head_sha: WORKER_REVISION, run_attempt: 1, repository: { full_name: repository }, ...overrides };
}

/**
 * In-memory Actions API. `createWorkflowDispatch` creates run `nextRunId` (then +1) of the
 * posted workflow and returns it as `return_run_details` does; `getWorkflowRun` serves `runs`.
 */
function fakeActions({ runs = new Map(), nextRunId = 200, revision = WORKER_REVISION } = {}) {
  const posts = [];
  const actions = {
    createWorkflowDispatch: async input => {
      posts.push(input);
      const id = String(nextRunId++);
      runs.set(id, workflowRun(id, { path: `.github/workflows/${input.workflow_id}`, head_sha: revision }));
      return { status: 200, data: { workflow_run_id: Number(id) } };
    },
    getWorkflowRun: async ({ run_id }) => {
      const run = runs.get(String(run_id));
      if (!run) throw Object.assign(new Error("Not Found"), { status: 404 });
      return { status: 200, data: run };
    },
  };
  return { actions, runs, posts };
}

/**
 * In-memory GitHub Git-data API holding the queue branch. `install(text)` moves the
 * head (a concurrent writer); `state.beforeUpdate` runs before the next ref update;
 * `state.ambiguousOnce` loses one successful update response.
 * @param {string | undefined} [initialContents]
 */
function fakeQueueGitHub(initialContents = undefined, { branch = "work-queue", repository: fullName = repository } = {}) {
  let serial = 0;
  const blobs = new Map();
  const trees = new Map();
  const commits = new Map();
  const refs = new Map();
  /** @type {{beforeUpdate: null | (() => unknown), ambiguousOnce: boolean, updates: number, writes: number}} */
  const state = { beforeUpdate: null, ambiguousOnce: false, updates: 0, writes: 0 };
  const id = prefix => createHash("sha1").update(`${prefix}-${++serial}`).digest("hex");
  const missing = () => Object.assign(new Error("Not Found"), { status: 404 });
  function install(contents, parents = refs.has(branch) ? [refs.get(branch)] : []) {
    const blob = id("blob");
    const tree = id("tree");
    const commit = id("commit");
    blobs.set(blob, contents);
    trees.set(tree, blob);
    commits.set(commit, { tree, parents });
    refs.set(branch, commit);
    return commit;
  }
  if (initialContents !== undefined) install(initialContents);
  const contents = () => (refs.has(branch) ? blobs.get(trees.get(commits.get(refs.get(branch)).tree)) : undefined);
  const actionsApi = fakeActions();
  const githubClient = {
    graphql: async () => ({ repository: { nameWithOwner: fullName, isEmpty: false } }),
    rest: {
      repos: { get: async () => ({ data: { full_name: fullName, default_branch: "main", size: 1 } }) },
      actions: actionsApi.actions,
      git: {
        getRef: async ({ ref }) => {
          if (ref === "heads/main") return { data: { object: { sha: "a".repeat(40) } } };
          const sha = refs.get(ref.slice("heads/".length));
          if (!sha) throw missing();
          return { data: { object: { sha } } };
        },
        getCommit: async ({ commit_sha }) => {
          if (!commits.has(commit_sha)) throw missing();
          return { data: { sha: commit_sha, tree: { sha: commits.get(commit_sha).tree }, parents: commits.get(commit_sha).parents.map(sha => ({ sha })) } };
        },
        getTree: async ({ tree_sha }) => ({ data: { truncated: false, tree: [{ path: "work-queue.jsonl", type: "blob", mode: "100644", sha: trees.get(tree_sha) }] } }),
        getBlob: async ({ file_sha }) => {
          const text = blobs.get(file_sha);
          return { data: { encoding: "base64", content: Buffer.from(text, "utf8").toString("base64"), size: Buffer.byteLength(text, "utf8") } };
        },
        createBlob: async ({ content }) => {
          const sha = id("blob");
          blobs.set(sha, content);
          return { data: { sha } };
        },
        createTree: async ({ tree }) => {
          const sha = id("tree");
          trees.set(sha, tree[0].sha);
          return { data: { sha } };
        },
        createCommit: async ({ tree, parents }) => {
          const sha = id("commit");
          commits.set(sha, { tree, parents });
          return { data: { sha } };
        },
        createRef: async ({ ref, sha }) => {
          const name = ref.slice("refs/heads/".length);
          if (refs.has(name)) throw Object.assign(new Error("Reference already exists"), { status: 422 });
          refs.set(name, sha);
          state.writes++;
          return { data: {} };
        },
        updateRef: async ({ ref, sha, force }) => {
          state.updates++;
          if (force !== false) throw new Error("queue ref updates must not force");
          if (state.beforeUpdate) {
            const callback = state.beforeUpdate;
            state.beforeUpdate = null;
            await callback();
          }
          const name = ref.slice("heads/".length);
          if (!commits.get(sha).parents.includes(refs.get(name))) throw Object.assign(new Error("Update is not a fast forward"), { status: 422 });
          refs.set(name, sha);
          state.writes++;
          if (state.ambiguousOnce) {
            state.ambiguousOnce = false;
            throw Object.assign(new Error("Transport response lost"), { status: 502 });
          }
          return { data: {} };
        },
      },
    },
  };
  registerQueueLedgerClient(githubClient, githubClient);
  return { githubClient, state, refs, commits, contents, install, runs: actionsApi.runs, posts: actionsApi.posts };
}

/**
 * The compiled GH_AW_WORK_QUEUE_POLICY text for an in-memory policy. The compiled form lists
 * workers once, so every pool must share one profile set; the round trip rejects a fixture
 * the compiler could not emit.
 * @param {import("./work_queue_policy.cjs").PolicyEnv} policy
 */
function policyEnvText(policy) {
  const [first] = Object.values(policy.pools);
  const workers = Object.keys(first.profiles).sort();
  const pools = Object.fromEntries(Object.entries(policy.pools).map(([name, pool]) => [name, { concurrency: pool.concurrency, per_account_limit: pool.per_account_limit, max_attempts: pool.max_attempts }]));
  const text = JSON.stringify({ version: policy.version, pending_limit: policy.pending_limit, issues: policy.issues, effect_scope: first.profiles[workers[0]].effect_scope, workers, pools });
  if (canonical(readWorkQueuePolicyEnv({ GH_AW_WORK_QUEUE_POLICY: text })) !== canonical(policy)) throw new Error("policy fixture is not expressible as a compiled policy environment");
  return text;
}

module.exports = {
  PROFILES,
  WORKER_REVISION,
  WORKER_WORKFLOW,
  administrator,
  bind,
  commit,
  dispatchNext,
  dispatchParameters,
  dispatcher,
  fakeActions,
  fakeQueueGitHub,
  finish,
  launch,
  ledgerText,
  policyEnvText,
  producer,
  projector,
  reconciler,
  release,
  repository,
  submit,
  work,
  workerActor,
  workflowRun,
};
