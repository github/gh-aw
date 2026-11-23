// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactionLog } = require("./work_queue_replay.cjs");
const { main, dispatchQueueIntent, launchDispatch, refreshObservations } = require("./work_queue_dispatch.cjs");
const { continuationChild } = require("./work_queue_graph.cjs");

const DISPATCHER_REF = "owner/repo/.github/workflows/dispatcher.lock.yml@refs/heads/main";
const WORKER_REF = "owner/repo/.github/workflows/worker.lock.yml@refs/heads/main";

function policy(overrides = {}) {
  return {
    version: 4,
    pending_limit: 30,
    issues: false,
    pools: { default: { concurrency: 3, per_account_limit: 0, max_attempts: 3, default_profile: "worker", profiles: { worker: { workflow: h.WORKER_WORKFLOW, effect_scope: "owner/repo" } } } },
    ...overrides,
  };
}

function fakeCore() {
  return { info: vi.fn(), warning: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn(async () => undefined) } };
}

/** The dispatcher's compiled dispatch-workflow config: it may launch only `worker`. */
const CONFIG = { workflows: ["worker"], work_queue_workflows: ["worker"], workflow_files: { worker: ".lock.yml" } };

/** A dispatch-token client over the in-memory Actions API; `createWorkflowDispatch` may be replaced. */
function dispatchClient(createWorkflowDispatch = undefined, actions = h.fakeActions()) {
  return { rest: { actions: { ...actions.actions, ...(createWorkflowDispatch ? { createWorkflowDispatch } : {}) } } };
}

const dispatcherContext = { repo: { owner: "owner", repo: "repo" }, runId: 100, runAttempt: 1, payload: {}, ref: "refs/heads/main" };

let directory;
let intentPath;
const savedEnv = {};

beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "wq-dispatch-"));
  intentPath = path.join(directory, "intents.jsonl");
  for (const key of ["GITHUB_WORKFLOW_REF", "GITHUB_REF", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_SAFE_OUTPUTS_STAGED", "GH_AW_WORK_QUEUE_POLICY"]) savedEnv[key] = process.env[key];
  process.env.GITHUB_WORKFLOW_REF = DISPATCHER_REF;
  delete process.env.GITHUB_REF;
  delete process.env.GH_AW_WORK_QUEUE_ROLE;
  delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  delete process.env.GH_AW_WORK_QUEUE_POLICY;
});

afterEach(() => {
  fs.rmSync(directory, { recursive: true, force: true });
  for (const [key, value] of Object.entries(savedEnv)) {
    if (value === undefined) delete process.env[key];
    else process.env[key] = value;
  }
});

function stage(...intents) {
  fs.writeFileSync(intentPath, intents.map(intent => JSON.stringify({ version: 4, ...intent }) + "\n").join(""));
}

function stateOf(fake) {
  return replayTransactionLog(fake.contents());
}

describe("dispatcher controls", () => {
  it("claims and launches one worker per Dispatch up to the pool concurrency", async () => {
    const works = ["a", "b", "c", "d"].map(key => h.work(key));
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], works)));
    const actions = h.fakeActions();
    const post = vi.spyOn(actions.actions, "createWorkflowDispatch");
    const core = fakeCore();
    stage({ intent_id: "dispatch", kind: "dispatch_next", parameters: { pool: "default", max_claims: 4, max_dispatches: 4 } });
    const result = await main({ githubClient: fake.githubClient, dispatchClient: dispatchClient(undefined, actions), context: dispatcherContext, core, policy: policy(), config: CONFIG, intentPath, maxDispatches: 4 });

    expect(result.success).toBe(true);
    expect(result.receipts).toHaveLength(1);
    expect(result.receipts[0].dispatches).toBe(3);
    expect(result.receipts[0].launches.map(launch => launch.state)).toEqual(["started", "started", "started"]);
    expect(result.remaining_dispatches).toBe(1);
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(JSON.parse(core.setOutput.mock.calls.find(([name]) => name === "work_queue_requests")[1])).toMatchObject({ version: 4, success: true, status: "committed" });
    expect(post).toHaveBeenCalledTimes(3);
    for (const [parameters] of post.mock.calls) {
      expect(parameters).toMatchObject({ owner: "owner", repo: "repo", workflow_id: "worker.lock.yml", ref: "refs/heads/main" });
      expect(Object.keys(JSON.parse(parameters.inputs.work_queue_assignment))).toEqual(["version", "dispatch_id"]);
    }
    const state = stateOf(fake);
    // Each Dispatch records the exact run its POST created and that run's revision.
    expect([...state.dispatches.values()].map(dispatch => [dispatch.state, dispatch.launch])).toEqual(["200", "201", "202"].map(run_id => ["started", { run_id, revision: h.WORKER_REVISION }]));
    expect(works.map(node => state.works.get(node.work_id).state)).toEqual(["claimed", "claimed", "claimed", "available"]);
  });

  it("grants Claims only for profiles in the compiled dispatch-workflow allowlist", async () => {
    const pools = {
      default: {
        concurrency: 3,
        per_account_limit: 0,
        max_attempts: 3,
        default_profile: "worker",
        profiles: { worker: { workflow: h.WORKER_WORKFLOW, effect_scope: "owner/repo" }, other: { workflow: ".github/workflows/other.lock.yml", effect_scope: "owner/repo" } },
      },
    };
    const foreign = h.work("foreign", { worker_profile: "other", priority: 1 });
    const own = h.work("own");
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [foreign, own])));
    const actions = h.fakeActions();
    const post = vi.spyOn(actions.actions, "createWorkflowDispatch");
    const receipt = await dispatchQueueIntent({
      githubClient: fake.githubClient,
      dispatchClient: dispatchClient(undefined, actions),
      context: dispatcherContext,
      core: fakeCore(),
      policy: policy({ pools }),
      config: CONFIG,
      remainingDispatches: 2,
      message: { intent_id: "dispatch", pool: "default", max_claims: 2, max_dispatches: 2 },
    });

    expect(receipt).toMatchObject({ status: "committed", dispatches: 1, launches: [{ state: "started" }] });
    expect(post.mock.calls.map(([parameters]) => parameters.workflow_id)).toEqual(["worker.lock.yml"]);
    const state = stateOf(fake);
    // The higher-priority Work of the non-allowlisted profile is never claimed.
    expect(state.works.get(foreign.work_id)).toMatchObject({ state: "available", attempts: 0 });
    expect(state.works.get(own.work_id).state).toBe("claimed");
    const request = state.transactions.find(commit => commit.request.kind === "dispatch_next").request;
    expect(Object.keys(request.parameters.profiles)).toEqual(["worker"]);
  });

  it("blocks a dispatch whose pool has no allowlisted profile without claiming or launching", async () => {
    const node = h.work("a");
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [node])));
    const post = vi.fn();
    const core = fakeCore();
    stage({ intent_id: "dispatch", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } });
    const before = fake.contents();
    const config = { workflows: ["other"], work_queue_workflows: ["other"], workflow_files: { other: ".lock.yml" } };
    const result = await main({ githubClient: fake.githubClient, dispatchClient: dispatchClient(post), context: dispatcherContext, core, policy: policy(), config, intentPath });

    expect(result.receipts).toEqual([{ intent_id: "dispatch", status: "blocked", reason: "work_queue_worker_not_allowlisted" }]);
    expect(post).not.toHaveBeenCalled();
    expect(fake.contents()).toBe(before);
  });

  it("re-checks the resolved workflow before launch and releases a Dispatch outside the allowlist", async () => {
    const node = h.work("a");
    const planned = h.dispatchNext(h.submit([], [node]), { max_claims: 1, max_dispatches: 1 });
    const fake = h.fakeQueueGitHub(h.ledgerText(planned.log));
    const dispatch = stateOf(fake).dispatches.get(planned.decision.dispatches[0]);
    const post = vi.fn();
    const options = { githubClient: fake.githubClient, dispatchClient: dispatchClient(post), context: dispatcherContext, core: fakeCore(), config: CONFIG };
    const receipt = await launchDispatch(options, policy(), dispatch, new Set([".github/workflows/other.lock.yml"]));

    expect(receipt).toEqual({ dispatch_id: dispatch.dispatch_id, state: "launch_failed", reason: "not_allowlisted" });
    expect(post).not.toHaveBeenCalled();
    expect(stateOf(fake).dispatches.get(dispatch.dispatch_id)).toMatchObject({ state: "closed", launch: null });
    expect(stateOf(fake).works.get(node.work_id).state).toBe("available");
  });

  it.each([
    ["no run details", async () => ({ status: 204 }), undefined],
    ["a run of another workflow", undefined, { path: ".github/workflows/other.lock.yml" }],
    ["a run that was not workflow_dispatch", undefined, { event: "push" }],
  ])("records no launch a worker could bind when the POST returns %s", async (_case, post, runOverrides) => {
    const node = h.work("a");
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [node])));
    const actions = h.fakeActions();
    if (runOverrides) {
      const create = actions.actions.createWorkflowDispatch;
      actions.actions.createWorkflowDispatch = async input => {
        const response = await create(input);
        const id = String(response.data.workflow_run_id);
        actions.runs.set(id, h.workflowRun(id, runOverrides));
        return response;
      };
    }
    const receipt = await dispatchQueueIntent({
      githubClient: fake.githubClient,
      dispatchClient: dispatchClient(post, actions),
      context: dispatcherContext,
      core: fakeCore(),
      policy: policy(),
      config: CONFIG,
      remainingDispatches: 1,
      message: { intent_id: "dispatch", pool: "default", max_claims: 1, max_dispatches: 1 },
    });

    expect(receipt.launches).toEqual([{ dispatch_id: expect.stringMatching(/^d_/), state: "launch_failed", reason: "unverified" }]);
    const state = stateOf(fake);
    expect(state.dispatches.get(receipt.launches[0].dispatch_id)).toMatchObject({ state: "closed", launch: null });
    expect(state.works.get(node.work_id)).toMatchObject({ state: "available", attempts: 1 });
  });

  it("releases a Dispatch whose launch POST fails and returns its Work to the pool", async () => {
    const node = h.work("a");
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [node])));
    const post = vi.fn(async () => {
      throw Object.assign(new Error("Unexpected inputs provided"), { status: 422 });
    });
    const receipt = await dispatchQueueIntent({
      githubClient: fake.githubClient,
      dispatchClient: dispatchClient(post),
      context: dispatcherContext,
      core: fakeCore(),
      policy: policy(),
      config: { ...CONFIG, "target-ref": "refs/heads/release" },
      remainingDispatches: 1,
      message: { intent_id: "dispatch", pool: "default", max_claims: 1, max_dispatches: 1 },
    });

    expect(post.mock.calls[0][0].ref).toBe("refs/heads/release");
    expect(receipt.status).toBe("committed");
    expect(receipt.launches).toEqual([{ dispatch_id: expect.stringMatching(/^d_/), state: "launch_failed", reason: "http_4xx" }]);
    const state = stateOf(fake);
    expect(state.works.get(node.work_id)).toMatchObject({ state: "available", attempts: 1 });
    expect(state.dispatches.get(receipt.launches[0].dispatch_id).state).toBe("closed");
    expect(state.claims.get(state.dispatches.get(receipt.launches[0].dispatch_id).claims[0]).cancellation_reason).toBe("launch_failed");
  });

  it("records a ready Observation for an external edge and claims the unblocked Work", async () => {
    const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "55", number: "5" };
    const node = h.work("gated", { depends_on: [{ kind: "issue", resource, condition: "completed" }] });
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [node])));
    const reader = {
      rest: {
        repos: { get: async () => ({ status: 200, data: { full_name: "owner/repo", id: 1 } }) },
        issues: { get: async () => ({ status: 200, data: { id: 55, number: 5, state: "closed", state_reason: "completed" } }) },
      },
    };
    const receipt = await dispatchQueueIntent({
      githubClient: fake.githubClient,
      dispatchClient: dispatchClient(),
      context: dispatcherContext,
      core: fakeCore(),
      policy: policy(),
      config: CONFIG,
      dependencyResolver: { scopes: [{ host: "github.com", repository: "owner/repo" }], getClient: async () => reader },
      remainingDispatches: 1,
      message: { intent_id: "dispatch", pool: "default", max_claims: 1, max_dispatches: 1 },
    });

    expect(receipt.dispatches).toBe(1);
    const state = stateOf(fake);
    expect([...state.observations.values()]).toEqual([expect.objectContaining({ kind: "Observation", state: "ready", state_reason: "completed" })]);
    expect(state.works.get(node.work_id).state).toBe("claimed");
  });

  it("commits submitted Work and blocks a submission over the pool's pending_limit", async () => {
    const fake = h.fakeQueueGitHub();
    const core = fakeCore();
    stage({ intent_id: "submit", kind: "submit", parameters: { nodes: [{ node_key: "one", graph_id: "g", payload: { n: 1 } }] } });
    const committed = await main({ githubClient: fake.githubClient, context: { ...dispatcherContext, payload: {} }, core, policy: policy(), intentPath, role: "dispatcher" });
    expect(committed.receipts).toEqual([expect.objectContaining({ intent_id: "submit", status: "committed", commit_id: expect.stringMatching(/^q_/) })]);
    const [created] = stateOf(fake).works.values();
    expect(created).toMatchObject({ graph_id: "g", node_key: "one", pool: "default", priority: 3, fairness_key: "", worker_profile: "worker", state: "available" });

    stage({ intent_id: "more", kind: "submit", parameters: { nodes: [{ node_key: "two", graph_id: "g", payload: { n: 2 } }] } });
    const blocked = await main({ githubClient: fake.githubClient, context: dispatcherContext, core, policy: policy({ pending_limit: 1 }), intentPath, role: "dispatcher" });
    expect(blocked).toMatchObject({ success: false, status: "blocked", receipts: [{ intent_id: "more", status: "blocked", reason: "work_queue_pending_limit" }] });
    expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("work_queue_pending_limit"));
    expect(stateOf(fake).works.size).toBe(1);
  });

  it("defaults a worker's child Work to its bound Claim's graph, pool and profile", async () => {
    const parent = h.work("parent", { priority: 2, fairness_key: "team" });
    const planned = h.dispatchNext(h.submit([], [parent]));
    const dispatchId = planned.decision.dispatches[0];
    const log = h.bind(h.launch(planned.log, dispatchId), dispatchId, h.workerActor("200", 1));
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    process.env.GITHUB_WORKFLOW_REF = WORKER_REF;
    stage({ intent_id: "child", kind: "submit", parameters: { nodes: [{ node_key: "child", payload: { step: 2 } }] } });
    const context = { repo: { owner: "owner", repo: "repo" }, runId: 200, runAttempt: 1, payload: { inputs: { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: dispatchId }) } } };
    const result = await main({ githubClient: fake.githubClient, context, core: fakeCore(), policy: policy(), intentPath, role: "worker" });

    expect(result.success).toBe(true);
    const state = stateOf(fake);
    const child = [...state.works.values()].find(work => work.node_key === "child");
    expect(child).toMatchObject({ graph_id: parent.graph_id, pool: "default", priority: 2, fairness_key: "team", worker_profile: "worker", state: "available" });
    expect(state.workCreators.get(child.work_id).role).toBe("worker");
  });

  it("fails a dispatcher that staged no control intents", async () => {
    const core = fakeCore();
    const result = await main({ githubClient: h.fakeQueueGitHub().githubClient, context: dispatcherContext, core, policy: policy(), intentPath });
    expect(result.receipts).toEqual([{ status: "blocked", reason: "work_queue_dispatcher_intents_missing" }]);
    expect(core.setFailed).toHaveBeenCalledWith("Work queue dispatcher produced no control intents; inspect the preparation step and tool-denial logs.");
  });

  it("budgets the closer's permission read when refreshing a continuation approval gate", async () => {
    const approval = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "55", number: "5" };
    const parent = h.work("parent");
    const planned = h.dispatchNext(h.submit([], [parent]));
    const dispatchId = planned.decision.dispatches[0];
    let log = h.bind(h.launch(planned.log, dispatchId), dispatchId, h.workerActor("200", 1));
    const claim = [...replayTransactionLog(h.ledgerText(log)).claims.values()][0];
    const child = continuationChild(replayTransactionLog(h.ledgerText(log)).works.get(parent.work_id), { route: "issue", message: "next", remaining: 0 }, approval);
    log = h.commit(log, h.workerActor("200", 1), "finish", { dispatch_id: dispatchId }, [{ kind: "Completion", work_id: parent.work_id, claim_id: claim.claim_id, dispatch_id: dispatchId, claim_handle: claim.handle, effects: [] }, child]);
    const reader = {
      rest: {
        repos: {
          get: vi.fn(async () => ({ status: 200, data: { full_name: "owner/repo", id: 1 } })),
          getCollaboratorPermissionLevel: vi.fn(async () => ({ status: 200, data: { permission: "admin", role_name: "admin", user: { login: "octo", id: 7 } } })),
        },
        issues: { get: vi.fn(async () => ({ status: 200, data: { id: 55, number: 5, state: "closed", state_reason: "completed", closed_by: { login: "octo", id: 7, type: "User" } } })) },
      },
    };
    const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, concurrency: 3, per_account_limit: 0, profiles: { worker: { workflow: h.WORKER_WORKFLOW } } };
    const observations = await refreshObservations(
      { dependencyResolver: { scopes: [{ host: "github.com", repository: "owner/repo" }], getClient: async () => reader }, core: fakeCore() },
      replayTransactionLog(h.ledgerText(log)),
      parameters
    );
    expect(observations).toEqual([expect.objectContaining({ resource: approval, condition: "approved", state: "ready", approved_by: "7" })]);
    expect(reader.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledTimes(1);
  });
});
