// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactionLog } = require("./work_queue_replay.cjs");
const { main, dispatchQueueIntent, refreshObservations } = require("./work_queue_dispatch.cjs");
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

function dispatchClient(createWorkflowDispatch = vi.fn(async () => ({ status: 204 }))) {
  return { rest: { actions: { createWorkflowDispatch } } };
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
    const post = vi.fn(async () => ({ status: 204 }));
    const core = fakeCore();
    stage({ intent_id: "dispatch", kind: "dispatch_next", parameters: { pool: "default", max_claims: 4, max_dispatches: 4 } });
    const result = await main({ githubClient: fake.githubClient, dispatchClient: dispatchClient(post), context: dispatcherContext, core, policy: policy(), intentPath, maxDispatches: 4 });

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
    expect([...state.dispatches.values()].map(dispatch => dispatch.state)).toEqual(["started", "started", "started"]);
    expect(works.map(node => state.works.get(node.work_id).state)).toEqual(["claimed", "claimed", "claimed", "available"]);
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
      config: { "target-ref": "refs/heads/release" },
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
