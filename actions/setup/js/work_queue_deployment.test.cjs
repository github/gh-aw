// @ts-check
import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import { randomUUID } from "node:crypto";
import { canonical } from "./work_queue_codec.cjs";
import { futurePolicy, executionProfile, serializeDeployments } from "./work_queue_deployment.cjs";
import { compactTransactions, replayTransactions } from "./work_queue_replay.cjs";
import { assignmentOnly, planNext, planDispatch } from "./work_queue_scheduler.cjs";
import { normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { launchAssignment, acceptedSubmissionParameters } from "./work_queue_dispatch.cjs";
import { synchronizeDeployments } from "./work_queue_deployment_control.cjs";
import { createWorkQueueSubmitTool, readWorkQueueState } from "./work_queue_mcp_server.cjs";
import { readStagedIntents } from "./work_queue_intents.cjs";
import { queueFixture, REF, WORKFLOW, REPOSITORY, DISPATCHER } from "./work_queue_lifecycle.test_helpers.cjs";

const CONTRACT = "a".repeat(64);
const NEW_REF = "b".repeat(40);
const WORKER_SOURCE = "---\ntools:\n  work-queue:\n    worker: true\n---\nDo approved work.\n";
const workerLock = (contract = CONTRACT) => `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${contract}"\n`;
function nativeFile(path, contents) {
  return { data: { type: "file", path, encoding: "base64", content: Buffer.from(contents).toString("base64") } };
}
function fixture() {
  const f = queueFixture({
    count: 4,
    batch: 1,
    granted: false,
    workerPrincipal: "22",
    configurePolicy: policy => {
      policy.authorization = "aw";
      policy.producers = {};
      const profile = policy.pools.default.profiles.default;
      delete profile.principal;
      profile.logical_contract = CONTRACT;
      policy.pools.default.profiles.other = { ...profile, workflow: ".github/workflows/other.lock.yml" };
    },
    configureWork: (work, index) => {
      if (index === 1) work.execution_ref = REF;
      if (index === 3) work.worker_profile = "other";
    },
  });
  f.githubClient.rest.repos.get = vi.fn(async () => ({ status: 200, data: { full_name: REPOSITORY, id: 7, default_branch: "main" } }));
  f.githubClient.rest.git = { getRef: vi.fn(async () => ({ data: { ref: "refs/heads/main", object: { sha: NEW_REF } } })) };
  return f;
}
function deploy(f, name = "default", ref = NEW_REF, contract = CONTRACT, available = true) {
  const worker = f.state.deployments.get("default").get(name);
  const profile = { ...worker.revisions[worker.current_ref].profile, ref, logical_contract: contract };
  return f.append(
    "deployment",
    { operations: [{ kind: "Deployment", pool: "default", worker_profile: name, expected_ref: worker.current_ref, expected_contract: worker.current_contract, profile, available, reason: "compiler_deployment" }] },
    f.dispatcher
  );
}

describe("prospective drain-free worker evolution", () => {
  it("propagates metadata authentication, rate-limit, service and transport failures instead of publishing unavailable defaults", async () => {
    for (const endpoint of ["content", "registration"])
      for (const status of [401, 403, 429, 500, undefined]) {
        const f = fixture();
        const proposal = futurePolicy(f.state);
        proposal.pools.default.profiles.default.ref = NEW_REF;
        const failure = Object.assign(new Error("metadata read denied"), status === undefined ? {} : { status });
        const workflow = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${CONTRACT}"\n`;
        f.githubClient.rest.repos.getContent = vi.fn(async ({ path }) => {
          if (endpoint === "content") throw failure;
          return nativeFile(path, path.endsWith(".md") ? WORKER_SOURCE : workflow);
        });
        f.githubClient.rest.actions.getWorkflow = vi.fn(async () => {
          throw failure;
        });
        const options = {
          githubClient: f.githubClient,
          context: f.dispatcherContext,
          policyProposal: proposal,
          config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
          readWorkQueueLog: f.readWorkQueueLog,
          publishWorkQueueRequest: f.publishWorkQueueRequest,
        };
        const before = canonical(f.transactions);
        const debt = structuredClone(f.state.clocks);
        await expect(synchronizeDeployments(options, { ...f.dispatcher, authenticated: true, roles: ["dispatcher"] })).rejects.toBe(failure);
        expect(canonical(f.transactions)).toBe(before);
        expect(f.state.clocks).toEqual(debt);
        expect(f.state.deployments.get("default").get("default").current_ref).toBe(REF);
      }
  });
  it("preserves legacy exact-ref pins without inferring compatibility with unregistered implementations", () => {
    const f = queueFixture({ count: 1, granted: false });
    const parameters = normalizeSubmitParameters({ nodes: [{ graph_id: "legacy-pin", payload: { task: "pinned" }, execution_ref: REF }] }, f.state.policy, 5000, f.state);
    f.append("submit", parameters, { role: "producer", principal: "11", repository: REPOSITORY });
    expect(f.state.deployments?.size ?? 0).toBe(0);
    expect(executionProfile(f.state, f.state.works.get(parameters.nodes[0].work_id)).profile.ref).toBe(REF);
    expect(() => normalizeSubmitParameters({ nodes: [{ graph_id: "foreign-pin", payload: { task: "foreign" }, execution_ref: NEW_REF }] }, f.state.policy, 5000, f.state)).toThrow(/registered/);
  });
  it("stages explicit reproducibility pins without exposing deployment or caller-authority changes on the agent wire", () => {
    const f = fixture();
    const intentPath = `.work-queue-pin-${randomUUID()}.jsonl`;
    try {
      const tool = createWorkQueueSubmitTool({ role: "dispatcher", worker: null, projection: f.state }, { intentPath });
      const node = { payload: { task: "pinned" }, execution_ref: REF, logical_contract: CONTRACT };
      tool.handler({ nodes: [node] });
      expect(readStagedIntents(intentPath)[0].parameters.nodes[0]).toEqual(node);
      expect(() => tool.handler({ nodes: [{ ...node, deployment: { available: true } }] })).toThrow();
      expect(() => tool.handler({ nodes: [{ ...node, execution_ref: "main" }] })).toThrow(/immutable/);
    } finally {
      fs.rmSync(intentPath, { force: true });
    }
  });
  it("preserves outstanding assignments/debt and separates explicit pins from compatible current implementations", () => {
    const f = fixture();
    f.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 48000 }, f.dispatcher);
    const before = f.state;
    deploy(f);
    const after = f.state;
    const snapshot = readWorkQueueState({ projection: after, captured_at: 5000, sha: "activation-head", role: "dispatcher" });
    const pinnedView = snapshot.works.find(work => work.execution_ref === REF);
    expect(pinnedView).toMatchObject({ admission_contract: CONTRACT, execution_ref: REF, deployment: { current_ref: NEW_REF, current_contract: CONTRACT, available: true } });
    expect(pinnedView).not.toHaveProperty("payload");
    for (const key of ["works", "claims", "dispatches"]) expect(canonical(Object.fromEntries(after[key]))).toBe(canonical(Object.fromEntries(before[key])));
    expect(after.policy).toEqual(before.policy);
    expect(after.policy_epoch).toBe(before.policy_epoch);
    expect(after.clocks).toEqual(before.clocks);
    expect(executionProfile(after, after.works.get(before.available[1])).profile.ref).toBe(NEW_REF);
    const pinned = [...after.works.values()].find(work => work.execution_ref);
    expect(executionProfile(after, pinned).profile.ref).toBe(REF);
    const decision = f.append("dispatch_next", { pool: "default", max_claims: 2, max_dispatches: 2, max_bytes: 48000, worker_profiles: ["default"] }, f.dispatcher);
    expect(decision.assignments).toHaveLength(2);
    const refs = decision.assignments.map(assignment => f.state.dispatches.get(assignment.dispatch_id).profile.ref);
    expect(refs).toEqual([REF, NEW_REF]);
    const one = compactTransactions(f.transactions, "c".repeat(40), f.administrator, f.at);
    const two = compactTransactions(one, "d".repeat(40), f.administrator, f.at);
    const restored = replayTransactions(two);
    expect(serializeDeployments(restored)).toEqual(serializeDeployments(f.state));
    expect(restored.works).toEqual(f.state.works);
    expect(restored.dispatches).toEqual(f.state.dispatches);
    expect(restored.clocks).toEqual(f.state.clocks);
    const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 48000 };
    const identity = { requestId: "after-checkpoint", commitId: "after-checkpoint", at: f.at };
    expect({ ...planDispatch(restored, parameters, identity), tip: "" }).toEqual({ ...planDispatch(f.state, parameters, identity), tip: "" });
  });

  it("pauses incompatible and unavailable targets locally, admits future contracts, and charges only eligible authorized reservations", () => {
    const f = fixture();
    const before = f.state;
    deploy(f, "default", NEW_REF, "b".repeat(64));
    expect(f.state.works).toEqual(before.works);
    expect(executionProfile(f.state, f.state.works.get(before.available[0])).reason).toBe("worker_incompatible");
    const parameters = normalizeSubmitParameters({ nodes: [{ graph_id: "future", payload: { task: "future" } }] }, futurePolicy(f.state), 5000, f.state);
    expect(parameters.nodes[0].logical_contract).toBe("b".repeat(64));
    f.append("submit", parameters, { role: "producer", principal: "11", repository: REPOSITORY });
    deploy(f, "default", NEW_REF, "b".repeat(64), false);
    expect(executionProfile(f.state, f.state.works.get(parameters.nodes[0].work_id)).reason).toBe("worker_unavailable");
    const grant = f.append("dispatch_next", { pool: "default", max_claims: 4, max_dispatches: 4, max_bytes: 48000, worker_profiles: ["other"] }, f.dispatcher);
    expect(grant.assignments).toHaveLength(1);
    expect(f.state.stats.claims).toBe(1);
    expect(grant.assignments[0].worker_profile).toBe("other");
    expect(planNext(f.state, "default", 5000).work_id).toBe([...f.state.works.values()].find(work => work.execution_ref).work_id);
    const empty = f.append("dispatch_next", { pool: "default", max_claims: 4, max_dispatches: 4, max_bytes: 48000, worker_profiles: [] }, f.dispatcher);
    expect(empty.operations).toEqual([]);
    expect(f.state.stats.claims).toBe(1);
  });

  it("does not expand scopes, reset scheduling economics, forge revisions, or accept stale CAS", () => {
    const f = fixture();
    const worker = f.state.deployments.get("default").get("default");
    const profile = worker.revisions[worker.current_ref].profile;
    const operation = { kind: "Deployment", pool: "default", worker_profile: "default", expected_ref: REF, expected_contract: CONTRACT, profile: { ...profile, ref: NEW_REF }, available: true, reason: "compiler_deployment" };
    for (const change of [{ effect_scope: "foreign/repo" }, { max_claims: 2 }, { share_keys: true }]) {
      expect(() => f.append("deployment", { operations: [{ ...operation, profile: { ...operation.profile, ...change } }] }, f.dispatcher)).toThrow(/deployment_invalid/);
    }
    deploy(f);
    expect(() => f.append("deployment", { operations: [operation] }, f.dispatcher)).toThrow(/deployment_conflict/);
    expect(() => deploy(f, "default", NEW_REF, "b".repeat(64))).toThrow(/deployment_invalid/);
    expect(() => f.append("policy", { operations: [{ kind: "Policy", epoch: "economic-update", policy: { ...f.state.policy, class_weights: [1, 1, 1, 1, 1] } }] })).toThrow(/policy_not_quiescent/);
  });

  it("updates historical pin availability without moving the current route and permits repeated availability transitions", async () => {
    const f = fixture();
    deploy(f);
    const current = f.state.deployments.get("default").get("default");
    const historical = current.revisions[REF].profile;
    f.append(
      "deployment",
      {
        operations: [
          {
            kind: "Deployment",
            pool: "default",
            worker_profile: "default",
            expected_ref: NEW_REF,
            expected_contract: CONTRACT,
            profile: historical,
            available: false,
            activate: false,
            reason: "historical_unavailable",
          },
        ],
      },
      f.dispatcher
    );
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(NEW_REF);
    expect(
      executionProfile(
        f.state,
        [...f.state.works.values()].find(work => work.execution_ref)
      ).reason
    ).toBe("worker_unavailable");
    const workflow = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${CONTRACT}"\n`;
    let available = false;
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path }) => {
      if (!available) throw Object.assign(new Error("temporarily unavailable"), { status: 404 });
      return nativeFile(path, path.endsWith(".md") ? WORKER_SOURCE : workflow);
    });
    f.githubClient.rest.actions.getWorkflow = vi.fn(async () => ({ data: { path: WORKFLOW, state: "active" } }));
    const options = {
      githubClient: f.githubClient,
      context: f.dispatcherContext,
      policyProposal: futurePolicy(f.state),
      config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
      readWorkQueueLog: f.readWorkQueueLog,
      publishWorkQueueRequest: f.publishWorkQueueRequest,
    };
    const trusted = { ...f.dispatcher, authenticated: true, roles: ["dispatcher"] };
    for (const value of [false, true, false, true]) {
      available = value;
      await synchronizeDeployments(options, trusted);
      const worker = f.state.deployments.get("default").get("default");
      expect(worker.current_ref).toBe(NEW_REF);
      expect(worker.revisions[NEW_REF].available).toBe(value);
      expect(worker.revisions[REF].available).toBe(value);
    }
    const before = canonical(f.transactions);
    await synchronizeDeployments(options, trusted);
    expect(canonical(f.transactions)).toBe(before);
  });

  it("recovers original accepted submissions after incompatible updates and repeated checkpoints without route or identity drift", () => {
    const f = fixture();
    const prior = f.state.requests.get("test-request-2");
    const original = {
      nodes: prior.request.parameters.nodes.map(node => ({
        payload: node.payload,
        graph_id: node.graph_id,
        node_key: node.node_key,
        worker_profile: node.worker_profile,
        ...(node.execution_ref ? { execution_ref: node.execution_ref } : {}),
      })),
    };
    deploy(f, "default", NEW_REF, "b".repeat(64));
    const context = { ...prior.actor, authenticated: true, roles: [prior.actor.role], created_at: 1000 };
    const state = replayTransactions(compactTransactions(compactTransactions(f.transactions, "c".repeat(40), f.administrator, f.at), "d".repeat(40), f.administrator, f.at));
    const accepted = state.requests.get(prior.request.id);
    expect(acceptedSubmissionParameters(state, context, original, accepted)).toEqual(prior.request.parameters);
    const before = canonical(f.transactions);
    expect(f.append("submit", prior.request.parameters, prior.actor, prior.request.id).idempotent).toBe(true);
    expect(canonical(f.transactions)).toBe(before);
  });

  it("pauses missing or inactive registrations before fair charging without blocking an unrelated compatible worker", async () => {
    for (const missing of [false, true]) {
      const f = fixture();
      const proposal = futurePolicy(f.state);
      proposal.pools.default.profiles.default.ref = NEW_REF;
      f.githubClient.rest.repos.getContent = vi.fn(async ({ path }) => {
        const workflow = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${CONTRACT}"\n`;
        return nativeFile(path, path.endsWith(".md") ? WORKER_SOURCE : workflow);
      });
      f.githubClient.rest.actions.getWorkflow = vi.fn(async ({ workflow_id }) => {
        if (workflow_id === "worker.lock.yml" && missing) throw Object.assign(new Error("unregistered worker"), { status: 404 });
        return { data: { path: `.github/workflows/${workflow_id}`, state: workflow_id === "worker.lock.yml" ? "disabled_manually" : "active" } };
      });
      const before = f.state;
      await synchronizeDeployments(
        {
          githubClient: f.githubClient,
          context: f.dispatcherContext,
          policyProposal: proposal,
          config: { work_queue_workflows: ["worker", "other"], aw_context_workflows: ["worker", "other"] },
          readWorkQueueLog: f.readWorkQueueLog,
          publishWorkQueueRequest: f.publishWorkQueueRequest,
        },
        { ...f.dispatcher, authenticated: true, roles: ["dispatcher"] }
      );
      expect(f.state.works).toEqual(before.works);
      expect(f.state.clocks).toEqual(before.clocks);
      expect(f.state.stats.claims).toBe(0);
      const decision = f.append("dispatch_next", { pool: "default", max_claims: 4, max_dispatches: 4, max_bytes: 48000, worker_profiles: ["default", "other"] }, f.dispatcher);
      expect(decision.assignments).toHaveLength(1);
      expect(decision.assignments[0].worker_profile).toBe("other");
      expect(f.state.stats.claims).toBe(1);
    }
  });

  it("missing default sources pause only the installed target without replacing its identity or revoking frozen launch/reconciliation", async () => {
    const f = fixture();
    f.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 48000 }, f.dispatcher);
    const assignment = assignmentOnly([...f.state.dispatches.values()][0]);
    const proposal = futurePolicy(f.state);
    proposal.pools.default.profiles.default.ref = NEW_REF;
    proposal.pools.default.profiles.other.ref = "e".repeat(40);
    const getContent = vi.fn().mockRejectedValue(Object.assign(new Error("worker unavailable"), { status: 404 }));
    f.githubClient.rest.repos.getContent = getContent;
    f.githubClient.rest.actions.getWorkflow = vi.fn();
    const options = {
      githubClient: f.githubClient,
      context: f.dispatcherContext,
      workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
      policyProposal: proposal,
      config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
      readWorkQueueLog: f.readWorkQueueLog,
      publishWorkQueueRequest: f.publishWorkQueueRequest,
    };
    const trusted = { ...f.dispatcher, authenticated: true, roles: ["dispatcher"] };
    await synchronizeDeployments(options, trusted);
    expect(f.state.deployments.get("default").get("default").revisions[REF].available).toBe(false);
    expect(f.state.deployments.get("default").get("default").revisions[NEW_REF]).toBeUndefined();
    expect(f.state.deployments.get("default").get("other").current_ref).toBe(REF);
    expect(f.state.dispatches.get(assignment.dispatch_id).profile.ref).toBe(REF);
    const post = vi.fn().mockResolvedValue({ status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } });
    f.githubClient.rest.actions.createWorkflowDispatch = post;
    const getRun = f.githubClient.rest.actions.getWorkflowRun;
    f.githubClient.rest.actions.getWorkflowRun = async parameters => {
      const response = await getRun(parameters);
      if (parameters.run_id === "42") response.data.display_title = `gh-aw work-queue ${assignment.dispatch_id}`;
      return response;
    };
    const launch = await launchAssignment({ ...options, config: {}, dispatchClient: f.githubClient, validateDispatchCredential: f.validateDispatchCredential, now: f.at, sleepFn: async () => {} }, assignment);
    expect(launch.state).toBe("bound");
    expect(post.mock.calls[0][0].ref).toBe(REF);
    expect(getContent).toHaveBeenCalledTimes(1);
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(REF);
    expect(f.state.deployments.get("default").get("default").revisions[REF].available).toBe(false);
    expect(f.state.dispatches.get(assignment.dispatch_id).run.principal).toBe("22");
  });
});
