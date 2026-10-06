// @ts-check
import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import {
  launchAssignment,
  dispatchQueueIntent,
  processWorkQueueIntents,
  configuredDispatchBudget,
  inheritWorkerSubmission,
  intentContext,
  resolveAdmissionResources,
  refreshDependencies,
  assertSafeReplacements,
} from "./work_queue_dispatch.cjs";
import { cancelBeforeLaunch, reconcileDispatch } from "./work_queue_reconciler.cjs";
import { assignmentOnly } from "./work_queue_scheduler.cjs";
import { nodeId } from "./work_queue_graph.cjs";
import { validateStoredAssignment } from "./work_queue_binding.cjs";
import { requestForIntent } from "./work_queue_intents.cjs";
import { digest } from "./work_queue_codec.cjs";
import { queueFixture, DISPATCHER, REF, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";

function setup(settings = {}) {
  const fixture = queueFixture(settings);
  const post = vi.fn().mockResolvedValue({ status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } });
  fixture.githubClient.rest.actions.createWorkflowDispatch = post;
  fixture.githubClient.rest.actions.listWorkflowRuns = vi.fn().mockResolvedValue({ status: 200, data: { workflow_runs: [] } });
  const options = {
    assignment: fixture.assignment,
    githubClient: fixture.githubClient,
    dispatchClient: fixture.githubClient,
    context: fixture.dispatcherContext,
    workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
    readWorkQueueLog: fixture.readWorkQueueLog,
    publishWorkQueueRequest: fixture.publishWorkQueueRequest,
    config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], max: 3 },
    now: fixture.at,
    sleepFn: async () => {},
  };
  return { fixture, post, options };
}

describe("native queue launch fencing and conservative recovery", () => {
  const edge = { kind: "issue", condition: "completed", resource: { kind: "issue", host: "github.com", repository: "foreign/design", repository_id: "20", resource_id: "30", number: "7" } };
  const externalSettings = { granted: false, count: 1, workDefaults: { depends_on: [edge] }, configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design") };
  function resolverFor(fixture, state = "closed") {
    const client = {
      rest: {
        repos: { get: vi.fn().mockResolvedValue({ status: 200, data: { id: 20, full_name: "foreign/design" } }) },
        issues: { get: vi.fn().mockResolvedValue({ status: 200, data: { id: 30, number: 7, state, state_reason: state === "closed" ? "completed" : null } }) },
      },
    };
    return { client, scopes: [{ host: "github.com", repository: "foreign/design", access_generation: fixture.state.credential_generation }], getClient: vi.fn().mockResolvedValue(client) };
  }

  it("publishes freshly resolved dependency evidence and selected Claims in one checked request", async () => {
    const { fixture, options } = setup(externalSettings);
    const resolver = resolverFor(fixture);
    const result = await dispatchQueueIntent({
      ...options,
      dependencyResolver: resolver,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "atomic-observation", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ status: "durable", dispatches: 1 });
    const grant = fixture.transactions.find(commit => commit.request.kind === "dispatch_next");
    expect(grant.operations.map(operation => operation.kind)).toEqual(["Observation", "Claim"]);
    expect(grant.operations[0]).toMatchObject({ state: "ready", credential_generation: fixture.state.credential_generation });
    expect(grant.operations[0].observed_at).toBeLessThanOrEqual(grant.at);
    expect(fixture.transactions.some(commit => commit.request.kind === "observe")).toBe(false);
    expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(1);
  });

  it("does not persist observations, request IDs or reservations when fresh dependency reads yield no grant", async () => {
    const { fixture, options, post } = setup(externalSettings);
    const before = fixture.transactions;
    const resolver = resolverFor(fixture, "open");
    const result = await dispatchQueueIntent({
      ...options,
      dependencyResolver: resolver,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "no-grant-observation", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ status: "no_grant", dispatches: 0 });
    expect(fixture.transactions).toEqual(before);
    expect(fixture.state.observations.size).toBe(0);
    expect(fixture.state.dispatches.size).toBe(0);
    expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(1);
    expect(post).not.toHaveBeenCalled();
  });

  it("rechecks access generation and rejects unscoped workers before foreign credential access", async () => {
    const { fixture, options } = setup(externalSettings);
    const resolver = resolverFor(fixture);
    const configured = { ...options, dependencyResolver: resolver };
    const initial = await refreshDependencies(configured, fixture.state, "default", fixture.dispatcher);
    expect(initial[0].state).toBe("ready");
    const rotated = { ...fixture.state, credential_generation: "rotated-access" };
    const changed = await refreshDependencies(configured, rotated, "default", fixture.dispatcher);
    expect(changed[0]).toMatchObject({ state: "unknown", credential_generation: "rotated-access", read_status: "external_access_generation_mismatch" });
    await expect(refreshDependencies(configured, fixture.state, "default", fixture.workerActor)).rejects.toThrow(/actor_untrusted/);
    expect(resolver.getClient).toHaveBeenCalledTimes(1);
    expect(fixture.state.observations.size).toBe(0);
  });

  it("atomically refreshes worker dependencies only under authenticated completed original Claim scope", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 1, configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design") });
    const parent = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle });
    const template = fixture.transactions[1].operations[0];
    fixture.append("submit", { nodes: [{ ...template, work_id: nodeId("foreign-frontier", "child"), graph_id: "foreign-frontier", node_key: "child", depends_on: [edge] }] }, { role: "producer", principal: "11", repository: REPOSITORY });
    const worker = { ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}` };
    const trusted = await intentContext(worker, { claim_handle: parent.handle });
    const resolver = resolverFor(fixture);
    for (const override of [{ ref: "b".repeat(40) }, { event: "push" }]) {
      await expect(refreshDependencies({ ...options, dependencyResolver: resolver }, fixture.state, "default", { ...trusted, ...override })).rejects.toThrow(/run_binding_conflict/);
    }
    expect(resolver.getClient).not.toHaveBeenCalled();
    const request = requestForIntent(trusted, "worker-observation", "dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: fixture.policy.limits.assignment_bytes });
    const result = await fixture.publishWorkQueueRequest({
      context: trusted,
      request,
      refreshObservations: (state, stable) => refreshDependencies({ ...options, dependencyResolver: resolver, now: fixture.at }, state, stable.parameters.pool, trusted),
    });
    expect(result.commit.actor).toMatchObject({ role: "worker", dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle });
    expect(result.commit.operations.map(operation => operation.kind)).toEqual(["Observation", "Claim"]);
    expect(result.commit.operations[0].state).toBe("ready");
    expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(1);
    const reads = resolver.getClient.mock.calls.length;
    const corrupted = structuredClone(fixture.state);
    corrupted.works.get(parent.work_id).barrier = "failed";
    await expect(refreshDependencies({ ...options, dependencyResolver: resolver }, corrupted, "default", trusted)).rejects.toThrow(/claim_effects_unauthorized/);
    expect(resolver.getClient).toHaveBeenCalledTimes(reads);
    expect(post).not.toHaveBeenCalled();
  });

  it("bounds refresh to 128 gates without letting a large unknown frontier block independent ready Work", async () => {
    const { fixture, options } = setup(externalSettings);
    const template = fixture.transactions[1].operations[0];
    const nodes = Array.from({ length: 130 }, (_, index) => ({
      ...template,
      work_id: nodeId("g2", `n${index}`),
      graph_id: "g2",
      node_key: `n${index}`,
      depends_on: index === 129 ? [] : [{ ...edge, resource: { ...edge.resource, resource_id: String(100 + index), number: String(100 + index) } }],
    }));
    fixture.append("submit", { nodes }, { role: "producer", principal: "11", repository: REPOSITORY });
    const getClient = vi.fn();
    const result = await dispatchQueueIntent({
      ...options,
      dependencyResolver: { scopes: [], getClient },
      now: fixture.at,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "bounded-unknown-frontier", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ status: "durable", dispatches: 1 });
    const grant = fixture.transactions.find(commit => commit.request.kind === "dispatch_next");
    expect(grant.operations.filter(operation => operation.kind === "Observation")).toHaveLength(128);
    expect(grant.operations.filter(operation => operation.kind === "Claim").map(operation => operation.work_id)).toEqual([nodes[129].work_id]);
    expect(grant.operations.filter(operation => operation.kind === "Observation").every(operation => operation.state === "unknown")).toBe(true);
    expect(getClient).not.toHaveBeenCalled();
  });

  it("rejects explicit observer queue controls and native launch before authentication or ledger reads", async () => {
    const { fixture, options, post } = setup();
    const read = vi.fn(options.readWorkQueueLog);
    const configured = { ...options, role: "observer", readWorkQueueLog: read };
    await expect(launchAssignment(configured, fixture.assignment)).rejects.toThrow(/observer_read_only/);
    await expect(intentContext(configured, { kind: "submit", intent_id: "forged", parameters: { nodes: [] } })).rejects.toThrow(/observer_read_only/);
    await expect(dispatchQueueIntent({ ...configured, staged: true, message: { type: "work_queue_dispatch_next", intent_id: "forged", pool: "default", max_claims: 1, max_dispatches: 1 } })).rejects.toThrow(/observer_read_only/);
    expect(read).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
  });

  it("rejects declared worker queue control without an original immutable assignment", async () => {
    const { options, post } = setup({ granted: false });
    await expect(intentContext({ ...options, role: "worker" }, { kind: "submit", intent_id: "missing", parameters: { nodes: [] } })).rejects.toThrow(/assignment_required/);
    expect(post).not.toHaveBeenCalled();
  });

  it("does not let agent-supplied inspection or disposition strings remediate partial or unknown failed effects", () => {
    const failed = { barrier: "failed", disposition: "unknown" };
    const state = { works: new Map([["failed", failed]]) };
    const parameters = { nodes: [{ replacement_of: { work_id: "failed", disposition: "inspection", evidence: "agent-assertion" } }] };
    expect(() => assertSafeReplacements(state, parameters)).toThrow("work_queue_trusted_remediation_required");
    failed.disposition = "partial";
    parameters.nodes[0].replacement_of.disposition = "idempotent";
    expect(() => assertSafeReplacements(state, parameters)).toThrow("work_queue_trusted_remediation_required");
    failed.disposition = "none";
    expect(() => assertSafeReplacements(state, parameters)).not.toThrow();
  });

  it("uses trusted resolver bindings for renamed admission coordinates without weakening immutable IDs", async () => {
    const { fixture, options } = setup();
    const scope = { host: "github.com", repository: "foreign/old", access_generation: fixture.state.credential_generation };
    const client = {
      rest: {
        repos: { get: vi.fn().mockResolvedValue({ status: 200, data: { id: 20, full_name: "foreign/new" } }) },
        issues: { get: vi.fn().mockResolvedValue({ status: 200, data: { id: 30, number: 7, state: "open" } }) },
      },
    };
    const configured = { ...options, dependencyResolver: { scopes: [scope, { ...scope, repository: "foreign/new" }], getClient: async () => client } };
    const parameters = { nodes: [{ depends_on: [{ kind: "issue", condition: "completed", resource: { kind: "issue", host: "github.com", repository: "foreign/old", number: "7" } }] }] };
    const admitted = await resolveAdmissionResources(configured, fixture.state, parameters, fixture.dispatcher);
    expect(admitted.nodes[0].depends_on[0]).toMatchObject({ condition: "completed", resource: { repository: "foreign/new", repository_id: "20", resource_id: "30", number: "7" } });
    const mismatch = { nodes: [{ depends_on: [{ kind: "issue", condition: "completed", resource: { kind: "issue", host: "github.com", repository: "foreign/old", number: "7", resource_id: "31" } }] }] };
    await expect(resolveAdmissionResources(configured, fixture.state, mismatch, fixture.dispatcher)).rejects.toThrow("external_admission_unverified");
  });

  it("selects one CAS sender before one pinned POST and binds the actual returned worker", async () => {
    const { fixture, post, options } = setup();
    post.mockImplementation(async parameters => {
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).state).toBe("started");
      expect(parameters.ref).toBe(REF);
      expect(parameters.headers).toEqual({ "X-GitHub-Api-Version": "2026-03-10" });
      expect(parameters.request.retries).toBe(0);
      expect(JSON.parse(parameters.inputs.work_queue_assignment)).toEqual(fixture.assignment);
      expect(parameters.inputs).not.toHaveProperty("task");
      return { status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } };
    });
    const result = await launchAssignment(options, fixture.assignment);
    expect(result).toMatchObject({ state: "bound", run_id: "42", dispatched: true });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run.run_id).toBe("42");
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run.run_id).not.toBe("15");
    await launchAssignment(options, fixture.assignment);
    expect(post).toHaveBeenCalledTimes(1);
    expect(fixture.state.claims.size).toBe(3);
  });

  it("returns detached deeply frozen original assignment authority rather than internal Dispatch metadata", () => {
    const { fixture } = setup();
    const state = fixture.state;
    const dispatch = state.dispatches.get(fixture.assignment.dispatch_id);
    const { assignment } = validateStoredAssignment(state, fixture.assignment);
    expect(assignment).toEqual(fixture.assignment);
    expect(assignment.claims).not.toBe(dispatch.claims);
    expect(assignment.claims[0].work).not.toBe(dispatch.claims[0].work);
    for (const value of [assignment, assignment.claims, assignment.claims[0], assignment.claims[0].work, assignment.claims[0].work.effect_contract, assignment.claims[0].result_refs]) expect(Object.isFrozen(value)).toBe(true);
    expect(() => (assignment.claims[0].work.plan = "changed authority")).toThrow(TypeError);
    expect(() => assignment.claims.pop()).toThrow(TypeError);
    expect(dispatch.claims).toHaveLength(3);
    expect(dispatch.claims[0].work.plan).toBe("stored task 1");
    expect(assignment).not.toHaveProperty("sender");
    expect(assignment).not.toHaveProperty("state");
    expect(assignment).not.toHaveProperty("run");
  });

  it("derives native groups from the checked publication state rather than adapter assignment metadata", async () => {
    const { fixture, options, post } = setup({ granted: false, count: 1 });
    const publish = options.publishWorkQueueRequest;
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => {
      const response = await get(args);
      const dispatch = [...fixture.state.dispatches.values()][0];
      return args.run_id === "42" ? { ...response, data: { ...response.data, display_title: `gh-aw work-queue ${dispatch.dispatch_id}` } } : response;
    };
    const result = await dispatchQueueIntent({
      ...options,
      assignment: null,
      publishWorkQueueRequest: async args => {
        const published = await publish(args);
        return args.request.kind === "dispatch_next" ? { ...published, assignments: [{ dispatch_id: "forged-adapter-metadata" }] } : published;
      },
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "checked-groups", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    const dispatch = [...fixture.state.dispatches.values()][0];
    expect(result).toMatchObject({ success: true, status: "durable", dispatches: 1 });
    expect(result.launches[0]).toMatchObject({ dispatch_id: dispatch.dispatch_id, state: "bound", run_id: "42" });
    expect(JSON.parse(post.mock.calls[0][0].inputs.work_queue_assignment)).toMatchObject({ dispatch_id: dispatch.dispatch_id, claims: [{ work: { plan: "stored task 1" } }] });
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("separates a later-attempt dispatcher origin from the approved native launch principal", async () => {
    const { fixture, post, options } = setup({ count: 1, workerPrincipal: "22", dispatcherRunAttempt: 2 });
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "bound", run_id: "42", dispatched: true });
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch.sender).toMatchObject({ principal: "11", run_id: "15", run_attempt: 2 });
    expect(dispatch.run).toMatchObject({ principal: "22", run_id: "42", run_attempt: 1 });
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("does not grant another fair prefix when a later publisher retries the same protected original intent", async () => {
    const { fixture, post, options } = setup({ granted: false, count: 2 });
    const control = {
      ...options,
      assignment: null,
      intentOrigin: fixture.dispatcher,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "same-serialized-intent", pool: "default", max_claims: 1, max_dispatches: 1 },
    };
    const first = await dispatchQueueIntent(control);
    expect(fixture.state.claims.size).toBe(1);
    fixture.dispatcherContext.runAttempt = 2;
    const recovered = await dispatchQueueIntent(control);
    expect(recovered.request_id).toBe(first.request_id);
    expect(fixture.state.claims.size).toBe(1);
    expect(post).toHaveBeenCalledTimes(1);
    await expect(dispatchQueueIntent({ ...control, intentOrigin: undefined })).rejects.toThrow(/origin_required/);
  });

  it("keeps the logical original grant actor separate from the current selected native sender", async () => {
    const { fixture, post, options } = setup({ granted: false, count: 1 });
    fixture.dispatcherContext.runAttempt = 2;
    await dispatchQueueIntent({
      ...options,
      assignment: null,
      intentOrigin: fixture.dispatcher,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "original-intent", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    const grant = fixture.transactions.find(commit => commit.request.kind === "dispatch_next");
    expect(grant.actor).toMatchObject({ principal: "11", run_id: "15", run_attempt: 1 });
    const dispatch = [...fixture.state.dispatches.values()][0];
    expect(dispatch.sender).toMatchObject({ principal: "11", run_id: "15", run_attempt: 2 });
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("never re-POSTs a recovered start marker, even for its recorded sender", async () => {
    const { fixture, post, options } = setup();
    const publish = options.publishWorkQueueRequest;
    options.publishWorkQueueRequest = async args => {
      const result = await publish(args);
      return args.request.parameters.operations?.[0]?.state === "started" ? { ...result, persisted: false, recovered: true, idempotent: true, publishedNow: false, reused: true } : result;
    };
    expect((await launchAssignment(options, fixture.assignment)).state).toBe("launch_unresolved");
    expect(post).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });

  it("requires explicit fresh non-idempotent publication flags before any POST", async () => {
    for (const flags of [{ publishedNow: undefined }, { publishedNow: false }, { reused: undefined }, { reused: true }, { persisted: undefined }, { recovered: undefined }, { idempotent: undefined }, { idempotent: true }]) {
      const { fixture, post, options } = setup();
      const publish = options.publishWorkQueueRequest;
      options.publishWorkQueueRequest = async args => {
        const result = await publish(args);
        return args.request.parameters.operations?.[0]?.state === "started" ? { ...result, ...flags } : result;
      };
      expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "launch_unresolved", dispatched: false, released: false });
      expect(post).not.toHaveBeenCalled();
    }
  });

  it("lets concurrent publishers recover one marker without sending two native launches", async () => {
    const { fixture, post, options } = setup();
    await Promise.all([launchAssignment(options, fixture.assignment), launchAssignment(options, fixture.assignment)]);
    expect(post).toHaveBeenCalledTimes(1);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run.run_id).toBe("42");
  });

  it("rejects adapter overrides of the selected immutable worker destination before reserving a sender", async () => {
    for (const destination of [{ repository: "foreign/repo" }, { workflow: ".github/workflows/other.yml" }, { ref: "b".repeat(40) }]) {
      const { fixture, post, options } = setup();
      await expect(launchAssignment({ ...options, destination }, fixture.assignment)).rejects.toThrow();
      expect(post).not.toHaveBeenCalled();
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).state).toBe("reserved");
    }
  });

  it("retains all uncertain reservations for timeouts, missing response details and old HTTP contracts", async () => {
    for (const response of [new Error("timeout"), { status: 204, data: {} }, { status: 200, data: { workflow_run_id: "42" } }]) {
      const { fixture, post, options } = setup();
      if (response instanceof Error) post.mockRejectedValue(response);
      else post.mockResolvedValue(response);
      const result = await launchAssignment(options, fixture.assignment);
      expect(result.state).toBe("launch_unresolved");
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
      expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
      await launchAssignment(options, fixture.assignment);
      expect(post).toHaveBeenCalledTimes(1);
    }
  });

  it("does not assert native scope evidence when the actual returned worker principal fails validation", async () => {
    const { fixture, post, options } = setup({ count: 1 });
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => {
      const response = await get(args);
      return args.run_id === "42" ? { ...response, data: { ...response.data, actor: { id: "22" }, triggering_actor: { id: "22" } } } : response;
    };
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "launch_unresolved", run_id: "42" });
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch.released).toBe(false);
    expect(dispatch.run).toBeUndefined();
    const uncertain = fixture.transactions.flatMap(commit => commit.operations).find(operation => operation.kind === "Dispatch" && operation.state === "uncertain");
    expect(uncertain).toMatchObject({ reason: "binding_unconfirmed" });
    expect(uncertain).not.toHaveProperty("evidence");
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("releases positive API rejection once, never equating no discovery with nonlaunch", async () => {
    const rejected = setup();
    rejected.post.mockRejectedValue({ status: 422, response: { status: 422, headers: { "x-github-request-id": "trusted-provider-request" } } });
    expect((await launchAssignment(rejected.options, rejected.fixture.assignment)).released).toBe(true);
    expect([...rejected.fixture.state.claims.values()].every(claim => claim.state === "cancelled")).toBe(true);
    const before = rejected.fixture.transactions.length;
    await launchAssignment(rejected.options, rejected.fixture.assignment);
    expect(rejected.fixture.transactions).toHaveLength(before);
    const uncertain = setup({ started: true });
    expect((await reconcileDispatch(uncertain.options)).state).toBe("launch_unresolved");
    expect(uncertain.fixture.state.dispatches.get(uncertain.fixture.assignment.dispatch_id).released).toBe(false);
    expect(uncertain.post).not.toHaveBeenCalled();
  });

  it("preserves completed siblings and cancels only open members with exact terminal-run proof", async () => {
    const { fixture, options } = setup({ bound: true });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => ({ status: 200, data: { ...(await get(args)).data, ...(args.run_id === "42" ? { status: "completed", conclusion: "cancelled" } : {}) } });
    const result = await reconcileDispatch({ ...options, now: fixture.at });
    expect(result.released).toBe(true);
    expect(fixture.state.claims.get(fixture.assignment.claims[0].claim_id).state).toBe("completed");
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("pending");
    expect(fixture.state.claims.get(fixture.assignment.claims[1].claim_id).state).toBe("cancelled");
    expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Completion")).toHaveLength(1);
  });

  it("uses the independently frozen profile for historical evidence after a fully drained policy replacement", async () => {
    const { fixture, options } = setup({ bound: true, count: 1 });
    const assignment = fixture.assignment;
    const frozen = structuredClone(fixture.state.dispatches.get(assignment.dispatch_id).profile);
    fixture.append("finish", { dispatch_id: assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: assignment.dispatch_id, claim_handle: "h1" });
    const completion = fixture.state.works.get(assignment.claims[0].work_id).completion_id;
    fixture.append(
      "result",
      {
        operations: [
          {
            kind: "Result",
            work_id: assignment.claims[0].work_id,
            claim_id: assignment.claims[0].claim_id,
            completion_id: completion,
            descriptor: { ok: true },
            evidence: {
              kind: "delivery",
              source: "verified_receipts",
              repository: REPOSITORY,
              workflow: fixture.binding.workflow,
              ref: REF,
              principal: "11",
              checked_at: fixture.at,
              run_id: "42",
              run_attempt: 1,
              receipt: "positive-scoped-receipt",
            },
          },
        ],
      },
      { role: "reconciler", principal: "11", repository: REPOSITORY }
    );
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => ({ status: 200, data: { ...(await get(parameters)).data, ...(parameters.run_id === "42" ? { status: "completed", conclusion: "success" } : {}) } });
    expect((await reconcileDispatch({ ...options, now: fixture.at })).released).toBe(true);
    const replacement = structuredClone(fixture.policy);
    replacement.pools.default.profiles.default = { ...replacement.pools.default.profiles.default, workflow: ".github/workflows/new-worker.lock.yml", ref: "b".repeat(40), principal: "22" };
    fixture.append("policy", { operations: [{ kind: "Policy", epoch: "e2", policy: replacement }] });
    const state = fixture.state;
    delete state.transactions;
    expect(validateStoredAssignment(state, assignment, { allowReleased: true }).profile).toEqual(frozen);
    expect(validateStoredAssignment(state, assignment, { allowReleased: true }).assignment).toEqual(assignment);
    expect(() => validateStoredAssignment(state, assignment)).toThrow(/policy_mismatch/);
  });

  it("treats cancellation requests and dispatcher death as insufficient release evidence", async () => {
    const { fixture, options } = setup({ bound: true });
    fixture.githubClient.rest.actions.cancelWorkflowRun = vi.fn().mockResolvedValue({ status: 202 });
    expect((await reconcileDispatch({ ...options, requestCancellation: true })).state).toBe("cancellation_requested");
    expect(fixture.githubClient.rest.actions.cancelWorkflowRun.mock.calls[0][0].request).toEqual({ retries: 0, timeout: 15000 });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
  });

  it("releases only the original bound attempt after a later rerun without granting it Claim authority", async () => {
    const { fixture, options } = setup({ bound: true });
    const run = fixture.nativeRun(fixture.workerContext);
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => (args.run_id === "42" ? { status: 200, data: { ...run, run_attempt: 2 } } : get(args));
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...run, status: "completed", conclusion: "failure" } });
    expect((await reconcileDispatch(options)).released).toBe(true);
    expect(fixture.githubClient.rest.actions.getWorkflowRunAttempt.mock.calls[0][0].attempt_number).toBe(1);
  });

  it.each([false, true])("recovers an unbound original attempt after a rerun with returned hint=%s", async returnedHint => {
    const { fixture, options, post } = setup({ started: true });
    const original = { ...fixture.nativeRun(fixture.workerContext), status: "completed", conclusion: "failure" };
    const later = { ...original, run_attempt: 2, status: "in_progress", conclusion: null };
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => (args.run_id === "42" ? { status: 200, data: later } : get(args));
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [later] } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: original });
    expect((await reconcileDispatch({ ...options, ...(returnedHint ? { returnedRunId: "42" } : {}) })).released).toBe(true);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toMatchObject({ run_id: "42", run_attempt: 1 });
    expect(fixture.githubClient.rest.actions.getWorkflowRunAttempt.mock.calls.every(([args]) => args.run_id === "42" && args.attempt_number === 1 && args.headers["X-GitHub-Api-Version"] === "2026-03-10" && args.request.retries === 0)).toBe(
      true
    );
    expect([...fixture.state.claims.values()].every(claim => claim.state === "cancelled")).toBe(true);
    expect(post).not.toHaveBeenCalled();
  });

  it("retains an unbound rerun reservation when original-attempt evidence is unavailable", async () => {
    const { fixture, options, post } = setup({ started: true });
    const later = { ...fixture.nativeRun(fixture.workerContext), run_attempt: 2 };
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [later] } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockRejectedValue(Object.assign(new Error("original attempt unavailable"), { status: 503 }));
    expect(await reconcileDispatch({ ...options, returnedRunId: "42" })).toMatchObject({ state: "launch_unresolved", released: false });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
    expect(post).not.toHaveBeenCalled();
  });

  it("rejects later-attempt metadata returned by the original-attempt discovery endpoint", async () => {
    const { fixture, options, post } = setup({ started: true });
    const later = { ...fixture.nativeRun(fixture.workerContext), run_attempt: 2 };
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [later] } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: later });
    expect(await reconcileDispatch(options)).toMatchObject({ state: "run_binding_conflict", released: false });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
    expect(post).not.toHaveBeenCalled();
  });

  it("rejects multiple correlated native runs rather than granting conflicting effects", async () => {
    const { fixture, options } = setup({ started: true });
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [fixture.nativeRun(fixture.workerContext), { ...fixture.nativeRun(fixture.workerContext), id: "43" }] } });
    expect((await reconcileDispatch(options)).state).toBe("run_binding_conflict");
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });

  it("ignores sibling-prefix runs before discovery proof reads instead of poisoning an exact binding", async () => {
    const { fixture, options, post } = setup({ started: true });
    const getAttempt = vi.spyOn(fixture.githubClient.rest.actions, "getWorkflowRunAttempt");
    const own = fixture.nativeRun(fixture.workerContext);
    const sibling = { ...own, id: "43", run_attempt: 2, display_title: `gh-aw work-queue ${fixture.assignment.dispatch_id}0` };
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [sibling, own] } });
    expect(await reconcileDispatch(options)).toMatchObject({ state: "bound", run_id: "42", released: false });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toMatchObject({ run_id: "42", run_attempt: 1 });
    expect(getAttempt.mock.calls.every(([args]) => args.run_id === "42")).toBe(true);
    expect(post).not.toHaveBeenCalled();
  });

  it("retains an unbound reservation when discovery only returns a sibling-prefix run", async () => {
    const { fixture, options, post } = setup({ started: true });
    const getAttempt = vi.spyOn(fixture.githubClient.rest.actions, "getWorkflowRunAttempt");
    const sibling = { ...fixture.nativeRun(fixture.workerContext), id: "43", run_attempt: 2, display_title: `gh-aw work-queue ${fixture.assignment.dispatch_id}0` };
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [sibling] } });
    expect(await reconcileDispatch(options)).toMatchObject({ state: "launch_unresolved", released: false });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
    expect(getAttempt).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
  });

  it("preserves finite lifecycle headroom by recording unresolved only once before later exact recovery", async () => {
    const { fixture, options, post } = setup({
      started: true,
      configurePolicy: policy => {
        policy.pools.default.reconciliation.max_attempts = 1;
      },
    });
    const dispatchId = fixture.assignment.dispatch_id;
    fixture.append("dispatch", { operations: [{ kind: "Dispatch", dispatch_id: dispatchId, state: "uncertain", reason: "binding_unconfirmed" }] }, fixture.dispatcher);
    for (let invocation = 0; invocation < 3; invocation++) {
      expect(await reconcileDispatch({ ...options, now: fixture.at })).toMatchObject({ state: "launch_unresolved", released: false });
    }
    const unresolved = fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Dispatch" && operation.state === "unresolved");
    expect(unresolved).toHaveLength(1);
    const original = { ...fixture.nativeRun(fixture.workerContext), status: "completed", conclusion: "failure" };
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [original] } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: original });
    expect((await reconcileDispatch({ ...options, now: fixture.at })).released).toBe(true);
    expect(fixture.state.dispatches.get(dispatchId).run).toMatchObject({ run_id: "42", run_attempt: 1 });
    expect(fixture.state.dispatches.get(dispatchId).released).toBe(true);
    expect(post).not.toHaveBeenCalled();
  });

  it("allows definitive prelaunch cancellation only before the marker race is won", async () => {
    const reserved = setup();
    expect((await cancelBeforeLaunch(reserved.options)).released).toBe(true);
    const started = setup({ started: true });
    await expect(cancelBeforeLaunch(started.options)).rejects.toThrow(/may_have_started/);
    expect(started.fixture.state.dispatches.get(started.fixture.assignment.dispatch_id).released).toBe(false);
  });

  it("recovers an already released immutable assignment after a drained policy epoch changes", async () => {
    const { fixture, post, options } = setup({ count: 1 });
    await cancelBeforeLaunch(options);
    fixture.append("cancel_work", { operations: [{ kind: "WorkCancellation", work_id: fixture.assignment.claims[0].work_id, reason: "retired" }] });
    const policy = JSON.parse(JSON.stringify(fixture.policy));
    policy.pools.default.profiles.default.ref = "b".repeat(40);
    fixture.append("policy", { operations: [{ kind: "Policy", epoch: "e2", policy }] });
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "released", dispatched: false });
    expect(await reconcileDispatch(options)).toMatchObject({ state: "released" });
    expect(post).not.toHaveBeenCalled();
  });

  it("publishes fair-prefix Claims, rejects selectors and enforces an aggregate dispatch budget", async () => {
    const { fixture, post, options } = setup({ granted: false });
    post.mockResolvedValue({ status: 204, data: {} });
    const first = await dispatchQueueIntent({ ...options, assignment: null, remainingDispatches: 1, message: { type: "work_queue_dispatch_next", intent_id: "batch1", pool: "default", max_claims: 2, max_dispatches: 1 } });
    expect(first.dispatches).toBe(1);
    expect(fixture.state.claims.size).toBe(2);
    expect([...fixture.state.dispatches.values()][0].claims.map(claim => claim.work.plan)).toEqual(["stored task 1", "stored task 2"]);
    await expect(dispatchQueueIntent({ ...options, remainingDispatches: 0, message: { type: "work_queue_dispatch_next", intent_id: "batch2", pool: "default", max_claims: 1, max_dispatches: 1 } })).rejects.toThrow(/budget/);
    await expect(dispatchQueueIntent({ ...options, remainingDispatches: 1, message: { type: "work_queue_dispatch_next", intent_id: "forged", pool: "default", max_claims: 1, max_dispatches: 1, work_id: "preferred" } })).rejects.toThrow();
  });

  it("rechecks the authenticated origin's durable budget inside fresh candidate generation", async () => {
    const { fixture, post, options } = setup({ granted: false });
    const publish = options.publishWorkQueueRequest;
    const competing = async args => {
      fixture.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: fixture.policy.limits.assignment_bytes }, fixture.dispatcher, "competing-grant");
      return publish(args);
    };
    await expect(
      dispatchQueueIntent({
        ...options,
        assignment: null,
        config: { ...options.config, max: 1 },
        publishWorkQueueRequest: competing,
        remainingDispatches: 1,
        message: { type: "work_queue_dispatch_next", intent_id: "racing-grant", pool: "default", max_claims: 1, max_dispatches: 1 },
      })
    ).rejects.toThrow(/budget/);
    expect(post).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.size).toBe(1);
  });

  it("inherits omitted child accounting metadata from authoritative parent Work, not global defaults", () => {
    const { fixture } = setup({
      bound: true,
      workDefaults: { priority: 1, fairness_key: "tenant" },
      configurePolicy: policy => {
        policy.accounting_weights.tenant = 1;
        policy.producers["11"].fairness_keys.push("tenant");
      },
    });
    const actor = { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" };
    const requested = { nodes: [{ graph_id: "child", node_key: "n", payload: { plan: "follow-up" } }] };
    expect(inheritWorkerSubmission(fixture.state, actor, requested).nodes[0]).toMatchObject({ pool: "default", priority: 1, fairness_key: "tenant" });
    expect(requested.nodes[0]).not.toHaveProperty("priority");
    expect(inheritWorkerSubmission(fixture.state, actor, { nodes: [{ ...requested.nodes[0], priority: 5, fairness_key: "foreign" }] }).nodes[0]).toMatchObject({ priority: 5, fairness_key: "foreign" });
  });

  it("requires fresh active native binding and same-Claim Completion for queue controls", async () => {
    const { fixture, options } = setup({ bound: true, count: 2 });
    const worker = { ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}` };
    await expect(intentContext(worker, { claim_handle: "h1" })).rejects.toThrow(/claim_ineffective/);
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h2", outcome: "cancelled" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h2" });
    expect(await intentContext(worker, { claim_handle: "h1" })).toMatchObject({ role: "worker", authenticated: true, claim_handle: "h1" });
    await expect(intentContext(worker, { claim_handle: "h2" })).rejects.toThrow(/claim_ineffective/);
  });

  it("does not let an assigned worker widen its fixed dispatch pool", async () => {
    const { fixture, options, post } = setup({
      bound: true,
      count: 1,
      configurePolicy: policy => {
        policy.pools.other = structuredClone(policy.pools.default);
      },
    });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const before = fixture.transactions.length;
    await expect(
      dispatchQueueIntent({
        ...options,
        context: fixture.workerContext,
        workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`,
        remainingDispatches: 1,
        message: { type: "work_queue_dispatch_next", intent_id: "cross-pool", claim_handle: "h1", pool: "other", max_claims: 1, max_dispatches: 1 },
      })
    ).rejects.toThrow("work_queue_control_pool_not_authorized");
    expect(fixture.transactions).toHaveLength(before);
    expect(post).not.toHaveBeenCalled();
  });

  function releaseCompletedParent(fixture) {
    fixture.append(
      "release",
      {
        operations: [
          {
            kind: "Release",
            dispatch_id: fixture.assignment.dispatch_id,
            evidence: {
              kind: "terminal_run",
              source: "github_api",
              repository: REPOSITORY,
              workflow: fixture.binding.workflow,
              ref: REF,
              principal: fixture.binding.principal,
              checked_at: fixture.at,
              run_id: "42",
              run_attempt: 1,
              status: "completed",
              conclusion: "success",
            },
          },
        ],
      },
      { role: "reconciler", principal: "11", repository: REPOSITORY }
    );
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => ({ status: 200, data: { ...(await get(args)).data, ...(args.run_id === "42" ? { status: "completed", conclusion: "success" } : {}) } });
  }

  it("recovers an accepted worker dispatch after Release without new grants, POSTs or authority", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 1 });
    const actor = { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" };
    fixture.append("finish", { dispatch_id: actor.dispatch_id, claim_handle: actor.claim_handle, outcome: "completed" }, actor);
    const template = fixture.transactions[1].operations[0];
    fixture.append("submit", { nodes: [{ ...template, work_id: nodeId("ack-child", "n"), graph_id: "ack-child", node_key: "n" }] }, { role: "producer", principal: "11", repository: REPOSITORY });
    const worker = { ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}` };
    const trusted = await intentContext(worker, { claim_handle: "h1" });
    const message = { type: "work_queue_dispatch_next", intent_id: "accepted-worker-grant", claim_handle: "h1", pool: "default", max_claims: 1, max_dispatches: 1 };
    const request = requestForIntent(trusted, message.intent_id, "dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: fixture.policy.limits.assignment_bytes });
    fixture.append("dispatch_next", request.parameters, actor, request.id);
    releaseCompletedParent(fixture);
    const before = fixture.transactions;
    expect(await dispatchQueueIntent({ ...worker, remainingDispatches: 0, message })).toMatchObject({ status: "durable", recovered: true, acknowledgement_only: true, dispatches: 1, launches: [] });
    await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 0, message: { ...message, max_claims: 2 } })).rejects.toThrow(/request_reused/);
    await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { ...message, intent_id: "new-after-release" } })).rejects.toThrow(/claim_ineffective/);
    expect(fixture.transactions).toEqual(before);
    expect(post).not.toHaveBeenCalled();
  });

  it("rechecks worker Completion authority on a fresh publisher prefix before dependency reads", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 1, configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design") });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const template = fixture.transactions[1].operations[0];
    fixture.append("submit", { nodes: [{ ...template, work_id: nodeId("cas-child", "n"), graph_id: "cas-child", node_key: "n", depends_on: [edge] }] }, { role: "producer", principal: "11", repository: REPOSITORY });
    const resolver = resolverFor(fixture);
    const publish = vi.fn(async args => {
      releaseCompletedParent(fixture);
      return args.refreshObservations(fixture.state, args.request, args.actor);
    });
    await expect(
      dispatchQueueIntent({
        ...options,
        context: fixture.workerContext,
        workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`,
        dependencyResolver: resolver,
        publishWorkQueueRequest: publish,
        remainingDispatches: 1,
        message: { type: "work_queue_dispatch_next", intent_id: "released-during-cas", claim_handle: "h1", pool: "default", max_claims: 1, max_dispatches: 1 },
      })
    ).rejects.toThrow(/claim_ineffective/);
    expect(publish).toHaveBeenCalledTimes(1);
    expect(resolver.getClient).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
  });

  it("recovers accepted child submission identities after Release without foreign reads or changed intent semantics", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 1, configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design") });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const directory = fs.mkdtempSync(path.join(process.cwd(), ".queue-worker-ack-"));
    const filename = path.join(directory, "intents.jsonl");
    const intent = {
      version: 3,
      intent_id: "accepted-worker-submit",
      kind: "submit",
      claim_handle: "h1",
      parameters: { nodes: [{ graph_id: "ack-child", node_key: "n", payload: { plan: "original child" }, depends_on: [{ ...edge, resource: { kind: "issue", host: "github.com", repository: "foreign/design", number: "7" } }] }] },
    };
    const resolver = resolverFor(fixture);
    const worker = { ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`, intentPath: filename, dependencyResolver: resolver };
    try {
      fs.writeFileSync(filename, `${JSON.stringify(intent)}\n`);
      const original = await processWorkQueueIntents(worker);
      expect(original.receipts[0].status).toBe("durable");
      expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(1);
      releaseCompletedParent(fixture);
      const before = fixture.transactions;
      const recovered = await processWorkQueueIntents({ ...worker, dependencyResolver: undefined });
      expect(recovered.receipts[0]).toEqual(original.receipts[0]);
      fs.writeFileSync(filename, `${JSON.stringify({ ...intent, parameters: { nodes: [{ ...intent.parameters.nodes[0], payload: { plan: "changed child" } }] } })}\n`);
      expect((await processWorkQueueIntents(worker)).receipts[0].status).toBe("blocked");
      fs.writeFileSync(filename, `${JSON.stringify({ ...intent, intent_id: "new-after-release" })}\n`);
      expect((await processWorkQueueIntents(worker)).receipts[0].status).toBe("blocked");
      expect(fixture.transactions).toEqual(before);
      expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(1);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.unlinkSync(filename);
      fs.rmdirSync(directory);
    }
  });

  it("reuses default roots across authenticated native origins and preserves explicit distinct nodes", async () => {
    const { fixture, options, post } = setup({ granted: false, count: 1 });
    const directory = fs.mkdtempSync(path.join(process.cwd(), ".queue-independent-root-"));
    const filename = path.join(directory, "intents.jsonl");
    const payload = { plan: "same independent payload", effect_contract: { kind: "none" } };
    const intent = { version: 3, intent_id: "root-submit", kind: "submit", parameters: { nodes: [{ payload }] } };
    const secondContext = { ...fixture.dispatcherContext, runId: "16" };
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = args => (args.run_id === "16" ? Promise.resolve({ status: 200, data: { ...fixture.nativeRun(secondContext), created_at: "2026-10-06T00:00:00Z" } }) : get(args));
    try {
      fs.writeFileSync(filename, `${JSON.stringify(intent)}\n`);
      const first = await processWorkQueueIntents({ ...options, intentPath: filename });
      expect(first.receipts[0].status).toBe("durable");
      const rootId = nodeId(digest(payload), "root");
      const root = fixture.state.works.get(rootId);
      expect(root.enqueued).toBe(Date.parse("2026-10-05T00:00:00Z"));
      const before = fixture.transactions;
      const repeated = await processWorkQueueIntents({ ...options, context: secondContext, intentPath: filename });
      expect(repeated.receipts[0].status).toBe("durable");
      expect(repeated.receipts[0].request_id).not.toBe(first.receipts[0].request_id);
      expect(fixture.transactions).toEqual(before);
      expect(fixture.state.works.get(rootId)).toEqual(root);
      fs.writeFileSync(filename, `${JSON.stringify({ ...intent, intent_id: "distinct-node", parameters: { nodes: [{ node_key: "second", payload }] } })}\n`);
      expect((await processWorkQueueIntents({ ...options, context: secondContext, intentPath: filename })).receipts[0].status).toBe("durable");
      expect(fixture.state.works.get(nodeId(digest(payload), "second")).enqueued).toBe(Date.parse("2026-10-06T00:00:00Z"));
      fs.writeFileSync(filename, `${JSON.stringify(intent)}\n`);
      expect((await processWorkQueueIntents({ ...options, intentPath: filename })).receipts[0]).toEqual(first.receipts[0]);
      fs.writeFileSync(filename, `${JSON.stringify({ ...intent, intent_id: "changed-definition", parameters: { nodes: [{ graph_id: digest(payload), node_key: "root", payload: { plan: "changed" } }] } })}\n`);
      const committed = fixture.transactions;
      expect((await processWorkQueueIntents({ ...options, context: secondContext, intentPath: filename })).receipts[0].status).toBe("blocked");
      expect(fixture.transactions).toEqual(committed);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.unlinkSync(filename);
      fs.rmdirSync(directory);
    }
  });

  it("publishes valid sibling controls despite malformed JSON, invalid selector or foreign Claim intents", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 2 });
    for (const claim of fixture.assignment.claims)
      fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: claim.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: claim.handle });
    const directory = fs.mkdtempSync(path.join(process.cwd(), ".queue-worker-siblings-"));
    const filename = path.join(directory, "intents.jsonl");
    const intent = handle => ({ version: 3, intent_id: `child-${handle}`, kind: "submit", claim_handle: handle, parameters: { nodes: [{ graph_id: "siblings", node_key: handle, payload: { plan: `child for ${handle}` } }] } });
    try {
      fs.writeFileSync(filename, [JSON.stringify(intent("h1")), "{not JSON", JSON.stringify({ ...intent("bad"), claim_handle: null }), JSON.stringify(intent("foreign")), JSON.stringify(intent("h2"))].join("\n"));
      const result = await processWorkQueueIntents({ ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`, intentPath: filename });
      expect(result.receipts.filter(receipt => receipt.status === "durable").map(receipt => receipt.intent_id)).toEqual(["child-h1", "child-h2"]);
      expect(result.receipts.filter(receipt => receipt.status === "blocked")).toHaveLength(3);
      expect(fixture.state.works.has(nodeId("siblings", "h1"))).toBe(true);
      expect(fixture.state.works.has(nodeId("siblings", "h2"))).toBe(true);
      expect(fixture.state.works.has(nodeId("siblings", "foreign"))).toBe(false);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.unlinkSync(filename);
      fs.rmdirSync(directory);
    }
  });

  it("normalizes every worker preview selector without publishing Completion or queue controls", async () => {
    const { fixture, options, post } = setup({ count: 2 });
    const directory = fs.mkdtempSync(path.join(process.cwd(), ".queue-worker-preview-"));
    const filename = path.join(directory, "intents.jsonl");
    const base = { version: 3, kind: "submit", parameters: { nodes: [{ graph_id: "preview", node_key: "n", payload: { plan: "preview only" } }] } };
    const intents = [
      { ...base, intent_id: "missing" },
      { ...base, intent_id: "foreign", claim_handle: "h99" },
      { ...base, intent_id: "null", claim_handle: null },
      { ...base, intent_id: "valid", claim_handle: "h2" },
    ];
    const read = vi.fn(options.readWorkQueueLog);
    const publish = vi.fn(options.publishWorkQueueRequest);
    const worker = { ...options, staged: true, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`, intentPath: filename, readWorkQueueLog: read, publishWorkQueueRequest: publish };
    try {
      fs.writeFileSync(filename, intents.map(intent => JSON.stringify(intent)).join("\n"));
      const result = await processWorkQueueIntents(worker);
      expect(result.receipts.filter(receipt => receipt.status === "staged_preview").map(receipt => receipt.intent_id)).toEqual(["valid"]);
      expect(result.receipts.filter(receipt => receipt.status === "blocked")).toHaveLength(3);
      expect(read).not.toHaveBeenCalled();
      expect(publish).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
      const message = { type: "work_queue_dispatch_next", intent_id: "preview-dispatch", pool: "default", max_claims: 1, max_dispatches: 1 };
      await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message })).rejects.toMatchObject({ code: "claim_scope_required" });
      await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { ...message, claim_handle: "h99" } })).rejects.toThrow(/foreign/);
      await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { ...message, claim_handle: null } })).rejects.toThrow();
      expect(await dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { ...message, claim_handle: "h2" } })).toMatchObject({ staged: true, dispatches: 0 });
      expect(publish).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
      expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
    } finally {
      fs.unlinkSync(filename);
      fs.rmdirSync(directory);
    }
  });

  it("accepts only bounded canonical protected-env dispatch budgets with explicit config precedence", () => {
    const previous = process.env.GH_AW_WORK_QUEUE_MAX_DISPATCHES;
    try {
      process.env.GH_AW_WORK_QUEUE_MAX_DISPATCHES = "3";
      expect(configuredDispatchBudget({})).toBe(3);
      expect(configuredDispatchBudget({ config: { max: 2 } })).toBe(2);
      expect(configuredDispatchBudget({ maxDispatches: 0 })).toBe(0);
      for (const invalid of ["01", "-1", "1.5", "1e2", "4097", "bad"]) {
        process.env.GH_AW_WORK_QUEUE_MAX_DISPATCHES = invalid;
        expect(() => configuredDispatchBudget({})).toThrow();
      }
    } finally {
      if (previous === undefined) delete process.env.GH_AW_WORK_QUEUE_MAX_DISPATCHES;
      else process.env.GH_AW_WORK_QUEUE_MAX_DISPATCHES = previous;
    }
  });

  it("reconciles exact terminal reservations before granting the next native-capacity winner", async () => {
    const { fixture, post, options } = setup({
      granted: false,
      count: 4,
      configurePolicy: policy => {
        policy.pools.default.native_limit = 1;
      },
    });
    fixture.append("dispatch_next", { pool: "default", max_claims: 3, max_dispatches: 1, max_bytes: fixture.policy.limits.assignment_bytes }, { ...fixture.dispatcher, run_id: "14" }, "previous-dispatcher");
    const old = assignmentOnly([...fixture.state.dispatches.values()][0]);
    fixture.append("dispatch", { operations: [{ kind: "Dispatch", dispatch_id: old.dispatch_id, state: "started", sender: fixture.dispatcher }] }, fixture.dispatcher);
    const run = { ...fixture.nativeRun(), display_title: `gh-aw work-queue ${old.dispatch_id}`, status: "completed", conclusion: "failure" };
    fixture.append(
      "dispatch",
      {
        operations: [
          {
            kind: "Dispatch",
            dispatch_id: old.dispatch_id,
            state: "bound",
            run: fixture.binding,
            evidence: { kind: "reconciliation", source: "github_api", repository: REPOSITORY, workflow: run.path, ref: REF, principal: "11", checked_at: fixture.at, run_id: "42", run_attempt: 1 },
          },
        ],
      },
      fixture.dispatcher
    );
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: run });
    post.mockResolvedValue({ status: 204, data: {} });
    const result = await dispatchQueueIntent({
      ...options,
      assignment: null,
      now: fixture.at,
      config: { ...options.config, max: 1 },
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "next-native-winner", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result.dispatches).toBe(1);
    expect(fixture.state.dispatches.get(old.dispatch_id).released).toBe(true);
    expect([...fixture.state.dispatches.values()][1].claims[0].work.plan).toBe("stored task 4");
    expect(post).toHaveBeenCalledTimes(1);
  });
});
