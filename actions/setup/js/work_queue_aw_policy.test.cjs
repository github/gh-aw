// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import { randomUUID } from "node:crypto";
import { canonical } from "./work_queue_codec.cjs";
import { nodeId, workDefinition } from "./work_queue_graph.cjs";
import { defaultPolicy, validatePolicy, validateSubmissionEntitlement, dispatchPrincipal } from "./work_queue_policy.cjs";
import { createDispatchCredentialValidator, isDispatchCredentialProof } from "./work_queue_dispatch_credential.cjs";
import { bindWorkerAssignment, loadQueue } from "./work_queue_binding.cjs";
import { launchAssignment, intentContext, dispatchQueueIntent, processWorkQueueIntents, approvedSubmissionParameters } from "./work_queue_dispatch.cjs";
import { cancelBeforeLaunch, reconcileDispatch } from "./work_queue_reconciler.cjs";
import { compactTransactions, replayTransactions } from "./work_queue_replay.cjs";
import { applySettings, parseSettings } from "./work_queue_settings.cjs";
import { queueFixture, DISPATCHER, WORKFLOW, REF, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";

function awPolicy(policy) {
  policy.authorization = "aw";
  policy.producers = {};
  delete policy.pools.default.profiles.default.principal;
}

function setupAW(settings = {}) {
  const fixture = queueFixture({
    count: 1,
    workerPrincipal: "22",
    ...settings,
    configurePolicy: policy => {
      awPolicy(policy);
      settings.configurePolicy?.(policy);
    },
  });
  const post = vi.fn().mockResolvedValue({
    status: 200,
    data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" },
  });
  fixture.githubClient.rest.actions.createWorkflowDispatch = post;
  fixture.githubClient.rest.actions.listWorkflowRuns = vi.fn().mockResolvedValue({ status: 200, data: { workflow_runs: [] } });
  fixture.githubClient.rest.repos.getContent = vi.fn().mockResolvedValue({
    data: {
      type: "file",
      path: WORKFLOW,
      encoding: "base64",
      content: Buffer.from("on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n").toString("base64"),
    },
  });
  fixture.githubClient.rest.actions.getWorkflow = vi.fn().mockResolvedValue({ data: { path: WORKFLOW, state: "active" } });
  const options = {
    assignment: fixture.assignment,
    githubClient: fixture.githubClient,
    dispatchClient: fixture.githubClient,
    validateDispatchCredential: fixture.validateDispatchCredential,
    context: fixture.dispatcherContext,
    workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
    readWorkQueueLog: fixture.readWorkQueueLog,
    publishWorkQueueRequest: fixture.publishWorkQueueRequest,
    config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
    now: fixture.at,
    sleepFn: async () => {},
  };
  return { fixture, options, post };
}

describe("AW-owned authorization with queue-owned scheduling and delivery", () => {
  const intentFiles = [];
  afterEach(() => {
    for (const file of intentFiles.splice(0)) fs.rmSync(file, { force: true });
  });
  function writeIntents(intents) {
    const file = `.gh-aw-submission-${randomUUID()}.jsonl`;
    intentFiles.push(file);
    fs.writeFileSync(file, intents.map(intent => canonical({ version: 3, ...intent })).join("\n") + "\n");
    return file;
  }
  function sharedSubmissionFixture() {
    const result = setupAW({
      granted: false,
      workDefaults: { pool: "seed" },
      configurePolicy: policy => {
        const pool = policy.pools.default;
        policy.pools.seed = structuredClone(pool);
        const worker = pool.profiles.default;
        pool.profiles = {
          "artifacts-summary": { ...worker, workflow: ".github/workflows/artifacts-summary.lock.yml" },
          workerB: { ...worker },
        };
        pool.default_profile = "artifacts-summary";
      },
    });
    const getWorkflowRun = result.fixture.githubClient.rest.actions.getWorkflowRun;
    result.fixture.githubClient.rest.actions.getWorkflowRun = async parameters => {
      const response = await getWorkflowRun(parameters);
      if (parameters.run_id === "42") response.data.display_title = `gh-aw work-queue ${[...result.fixture.state.dispatches.values()][0].dispatch_id}`;
      return response;
    };
    return result;
  }

  it("admits an omitted profile using the caller's one approved worker instead of an unrelated global default, then launches it", async () => {
    const { fixture, options, post } = sharedSubmissionFixture();
    const submit = { kind: "submit", intent_id: "own-default", parameters: { nodes: [{ payload: { task: "own approved worker" } }] } };
    const intentPath = writeIntents([submit, { kind: "dispatch_next", intent_id: "own-dispatch", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } }]);
    const result = await processWorkQueueIntents({ ...options, intentPath, maxDispatches: 1 });
    expect(result.receipts.map(receipt => receipt.status)).toEqual(["durable", "durable"]);
    expect(result.receipts[1].launches).toEqual([expect.objectContaining({ state: "bound", run_id: "42" })]);
    const work = [...fixture.state.works.values()].find(work => work.payload.task === "own approved worker");
    expect(work.worker_profile).toBe("workerB");
    expect(fixture.state.policy.pools.default.default_profile).toBe("artifacts-summary");
    expect(post).toHaveBeenCalledOnce();
    expect(post.mock.calls[0][0]).toMatchObject({ workflow_id: "worker.lock.yml", ref: REF });
    expect([...fixture.state.dispatches.values()][0].run.principal).toBe("22");
    fs.writeFileSync(intentPath, canonical({ version: 3, ...submit }) + "\n");
    const before = canonical(fixture.transactions);
    expect((await processWorkQueueIntents({ ...options, config: {}, intentPath })).receipts[0].status).toBe("durable");
    expect(canonical(fixture.transactions)).toBe(before);
    expect(fixture.state.works.get(work.work_id).worker_profile).toBe("workerB");
  });

  it("rejects an explicit unrelated installed profile before admission or native launch", async () => {
    const { fixture, options, post } = sharedSubmissionFixture();
    const intentPath = writeIntents([{ kind: "submit", intent_id: "foreign-explicit", parameters: { nodes: [{ payload: { task: "unrelated" }, worker_profile: "artifacts-summary" }] } }]);
    const before = canonical(fixture.transactions);
    const publish = vi.fn(fixture.publishWorkQueueRequest);
    expect((await processWorkQueueIntents({ ...options, intentPath, publishWorkQueueRequest: publish })).receipts[0].status).toBe("blocked");
    expect(publish).not.toHaveBeenCalled();
    expect(canonical(fixture.transactions)).toBe(before);
    expect(post).not.toHaveBeenCalled();
  });

  it("chooses deterministically among caller-approved profiles and rejects missing approval in either protected list", () => {
    const { fixture, options } = sharedSubmissionFixture();
    const state = fixture.state;
    state.policy.pools.default.profiles["a-worker"] = { ...state.policy.pools.default.profiles.workerB };
    const parameters = { nodes: [{ payload: { task: "default" } }] };
    expect(approvedSubmissionParameters(state, fixture.dispatcher, parameters, options.config).nodes[0].worker_profile).toBe("a-worker");
    for (const config of [{ work_queue_workflows: ["worker"] }, { aw_context_workflows: ["worker"] }, {}]) {
      expect(() => approvedSubmissionParameters(state, fixture.dispatcher, parameters, config)).toThrow(/profile_not_approved/);
    }
  });

  it("retains an AW worker's original completed-Claim profile for child defaults without granting unrelated routes", async () => {
    const { fixture, options } = setupAW({
      configurePolicy: policy => {
        policy.pools.default.profiles.daily = { ...policy.pools.default.profiles.default, workflow: ".github/workflows/daily.lock.yml" };
        policy.pools.default.default_profile = "daily";
      },
    });
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "bound" });
    const member = fixture.assignment.claims[0];
    const origin = { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle };
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, origin);
    const trusted = await intentContext({ ...options, role: "worker", context: fixture.workerContext, workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}` }, { claim_handle: member.handle });
    expect(approvedSubmissionParameters(fixture.state, trusted, { nodes: [{ payload: { task: "child" } }] }, {}).nodes[0].worker_profile).toBe("default");
    expect(() => approvedSubmissionParameters(fixture.state, trusted, { nodes: [{ payload: { task: "foreign child" }, worker_profile: "daily" }] }, {})).toThrow(/worker_not_compiler_approved/);
  });

  it("keeps absent authorization historical and removes author-facing principal/producer configuration only for AW policies", () => {
    const policy = defaultPolicy({ repository: REPOSITORY, principal: "11", ref: REF });
    awPolicy(policy);
    expect(validatePolicy(policy)).toBe(policy);
    expect(canonical(policy)).toContain('"authorization":"aw"');
    expect(policy.producers).toEqual({});
    expect(policy.pools.default.profiles.default).not.toHaveProperty("principal");
    expect(() => validatePolicy({ ...policy, authorization: "" })).toThrow();
    expect(() => validatePolicy({ ...policy, authorization: "other" })).toThrow();
    const legacy = structuredClone(policy);
    delete legacy.authorization;
    expect(() => validatePolicy(legacy)).toThrow();
    expect(() => validatePolicy({ ...policy, producers: { "11": { pools: ["default"], priorities: [3], fairness_keys: [""] } } })).toThrow();
    for (const field of ["credential_scope", "trust_domain", "effect_scope"]) {
      const invalid = structuredClone(policy);
      invalid.pools.default.profiles.default[field] = "custom";
      expect(() => validatePolicy(invalid)).toThrow();
    }
  });

  it("admits distinct trusted AW producers without registration but retains scheduling, immutable Work, role and lineage checks", () => {
    const { fixture } = setupAW();
    const root = workDefinition(fixture.state.works.get(fixture.assignment.claims[0].work_id));
    for (const principal of ["33", "9007199254740993"]) {
      const node = { ...root, work_id: nodeId(root.graph_id, principal), node_key: principal };
      fixture.append("submit", { nodes: [node] }, { role: "producer", principal, repository: REPOSITORY }, `producer-${principal}`);
    }
    expect(fixture.state.works.size).toBe(3);
    expect(fixture.state.policy.producers).toEqual({});
    for (const update of [{ pool: "foreign" }, { priority: 6 }, { fairness_key: "unregistered" }])
      expect(() => validateSubmissionEntitlement(fixture.state, { ...root, ...update }, { role: "producer", principal: "33", repository: REPOSITORY })).toThrow();
    expect(() => validateSubmissionEntitlement(fixture.state, root, { role: "reconciler", principal: "33", repository: REPOSITORY })).toThrow();
    expect(() => validateSubmissionEntitlement(fixture.state, root, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id })).toThrow();
    fixture.append("cancel_work", { operations: [{ kind: "WorkCancellation", work_id: root.work_id, reason: "aw_authorized" }] }, { role: "producer", principal: "44", repository: REPOSITORY });
    expect(fixture.state.works.get(root.work_id).state).toBe("cancelled");
  });

  it("binds a no-principal profile to the selected credential, not the dispatcher or actor hint, and retains it across checkpoints", async () => {
    const { fixture, options, post } = setupAW();
    const frozen = canonical(fixture.state.dispatches.get(fixture.assignment.dispatch_id).profile);
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "bound", run_id: "42" });
    expect(post).toHaveBeenCalledOnce();
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch.credential_principal).toBe("22");
    expect(dispatch.run.principal).toBe("22");
    expect(dispatch.sender.principal).toBe("11");
    expect(canonical(dispatch.profile)).toBe(frozen);
    expect(dispatchPrincipal(dispatch)).toBe("22");
    const workerOptions = { ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`, role: "worker", requireAssignment: true };
    expect((await bindWorkerAssignment(workerOptions)).binding.principal).toBe("22");
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    expect((await intentContext(workerOptions, { claim_handle: member.handle })).principal).toBe("22");
    const state = fixture.state;
    const checkpoint = compactTransactions(fixture.transactions, "c".repeat(40), fixture.administrator, fixture.at);
    const restored = replayTransactions(checkpoint);
    expect(canonical([...restored.dispatches])).toBe(canonical([...state.dispatches]));
    expect(restored.policy.authorization).toBe("aw");
    expect(restored.policy.producers).toEqual({});
    fixture.append(
      "result",
      {
        operations: [
          {
            kind: "Result",
            work_id: member.work_id,
            claim_id: member.claim_id,
            completion_id: state.works.get(member.work_id).completion_id,
            descriptor: {},
            evidence: { kind: "delivery", source: "verified_receipts", repository: REPOSITORY, workflow: WORKFLOW, ref: REF, principal: "22", checked_at: fixture.at, run_id: "42", run_attempt: 1, receipt: "verified-none" },
          },
        ],
      },
      { ...fixture.dispatcher, role: "reconciler" }
    );
    expect(fixture.state.works.get(member.work_id).barrier).toBe("verified");
  });

  it("requires a durable selected principal before activation and refuses foreign native principal binding", async () => {
    const { fixture, options } = setupAW();
    const start = { kind: "Dispatch", dispatch_id: fixture.assignment.dispatch_id, state: "started", sender: fixture.dispatcher };
    expect(() => fixture.append("dispatch", { operations: [start] }, fixture.dispatcher)).toThrow(/credential principal/);
    fixture.append("dispatch", { operations: [{ ...start, credential_principal: "22" }] }, fixture.dispatcher);
    const forged = { ...fixture.nativeRun(), actor: { id: "11" }, triggering_actor: { id: "11" } };
    fixture.githubClient.rest.actions.getWorkflowRun = async () => ({ status: 200, data: forged });
    await expect(bindWorkerAssignment({ ...options, context: { ...fixture.workerContext, actorId: "11" }, workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}` })).rejects.toThrow(/principal/);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).not.toHaveProperty("run");
  });

  it("checks returned and discovered native principals against the frozen dispatch credential proof", async () => {
    const { fixture, options, post } = setupAW();
    fixture.githubClient.rest.actions.getWorkflowRun = async ({ run_id }) => ({
      status: 200,
      data: run_id === "42" ? { ...fixture.nativeRun(), actor: { id: "33" }, triggering_actor: { id: "33" } } : fixture.nativeRun(fixture.dispatcherContext),
    });
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "launch_unresolved" });
    expect(post).toHaveBeenCalledOnce();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).credential_principal).toBe("22");
    fixture.githubClient.rest.actions.listWorkflowRuns = async () => ({ status: 200, data: { workflow_runs: [fixture.nativeRun()] } });
    expect(await reconcileDispatch(options)).toMatchObject({ state: "run_binding_conflict", released: false });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).not.toHaveProperty("run");
  });

  it("revalidates AW-approved exact workflow/SHA routes before launch without resetting an installed Policy from new compile settings", async () => {
    const { fixture, options, post } = setupAW();
    const changed = structuredClone(fixture.policy);
    changed.pools.default.native_limit = 1;
    const loaded = await loadQueue({ ...options, policyProposal: changed });
    expect(loaded.projection.policy.pools.default.native_limit).toBe(16);
    fixture.githubClient.rest.actions.getWorkflow = async () => ({ data: { path: WORKFLOW, state: "disabled_manually" } });
    await expect(launchAssignment({ ...options, policyProposal: changed }, fixture.assignment)).rejects.toThrow(/active/);
    expect(post).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).state).toBe("reserved");
    expect(fixture.githubClient.rest.repos.getContent).toHaveBeenCalledWith({ owner: "owner", repo: "repo", path: WORKFLOW, ref: REF });
  });

  it("releases a never-started AW reservation with authenticated prelaunch evidence without inventing a worker identity", async () => {
    const { fixture, options, post } = setupAW();
    expect(await cancelBeforeLaunch(options)).toMatchObject({ released: true });
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch.profile).not.toHaveProperty("principal");
    expect(dispatch).not.toHaveProperty("credential_principal");
    expect(post).not.toHaveBeenCalled();
  });

  it("keeps no-principal credential proofs client-bound and rejects stale or forged proofs", async () => {
    const { fixture } = setupAW();
    const profile = fixture.state.dispatches.get(fixture.assignment.dispatch_id).profile;
    const client = fixture.githubClient;
    const validator = createDispatchCredentialValidator(client, { kind: "authenticated" });
    const proof = await validator({ profile });
    expect(proof.principal).toBe("22");
    expect(isDispatchCredentialProof(proof, client, profile)).toBe(true);
    expect(isDispatchCredentialProof({ principal: "22", kind: "authenticated" }, client, profile)).toBe(false);
    expect(isDispatchCredentialProof(proof, { ...client }, profile)).toBe(false);
    expect(isDispatchCredentialProof(proof, client, { ...profile, ref: "b".repeat(40) })).toBe(false);
  });

  it("derives the AW bot principal only when the compiler-selected token is bound to the actual dispatch client", async () => {
    const { fixture } = setupAW();
    const profile = fixture.state.dispatches.get(fixture.assignment.dispatch_id).profile;
    const token = "test-only-selected-token";
    const client = {
      auth: vi.fn().mockResolvedValue({ type: "token", token }),
      rest: { users: { getByUsername: vi.fn().mockResolvedValue({ status: 200, data: { id: "22", type: "Bot", login: "github-actions[bot]" } }) } },
    };
    const validator = createDispatchCredentialValidator(client, { kind: "github_token" }, token);
    const proof = await validator({ profile });
    expect(proof.principal).toBe("22");
    expect(client.auth).toHaveBeenCalledWith({ type: "token" });
    expect(client.rest.users.getByUsername).toHaveBeenCalledWith(expect.objectContaining({ username: "github-actions[bot]" }));
    expect(isDispatchCredentialProof(proof, client, profile)).toBe(true);
    client.auth.mockResolvedValue({ type: "token", token: "different-client-token" });
    await expect(validator({ profile })).rejects.toThrow(/client_mismatch/);
  });

  it("launches only selected caller-approved routes in a Policy shared by independent pools", async () => {
    const { fixture, options, post } = setupAW({
      granted: false,
      configurePolicy: policy => {
        const daily = { ...policy.pools.default.profiles.default, workflow: ".github/workflows/daily.lock.yml" };
        policy.pools.default.profiles.daily = daily;
        policy.pools.daily = { ...structuredClone(policy.pools.default), default_profile: "daily" };
      },
    });
    const result = await dispatchQueueIntent({
      ...options,
      remainingDispatches: 1,
      runDispatchBudget: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "shared-pool-grant", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result.dispatches).toBe(1);
    expect(post).toHaveBeenCalledOnce();
    expect(post.mock.calls[0][0].workflow_id).toBe("worker.lock.yml");
    expect(fixture.state.policy.pools.daily.profiles.daily.workflow).toBe(".github/workflows/daily.lock.yml");
    expect(options.config.work_queue_workflows).toEqual(["worker"]);
  });

  it("refuses an entire mixed-route scheduler prefix before publishing any Claims", async () => {
    const { fixture, options, post } = setupAW({
      granted: false,
      configurePolicy: policy => {
        policy.pools.default.profiles.daily = { ...policy.pools.default.profiles.default, workflow: ".github/workflows/daily.lock.yml" };
      },
    });
    const root = workDefinition([...fixture.state.works.values()][0]);
    const daily = { ...root, node_key: "daily", work_id: nodeId(root.graph_id, "daily"), worker_profile: "daily" };
    fixture.append("submit", { nodes: [daily] }, { role: "producer", principal: "33", repository: REPOSITORY });
    const before = canonical(fixture.transactions);
    await expect(
      dispatchQueueIntent({
        ...options,
        remainingDispatches: 2,
        runDispatchBudget: 2,
        message: { type: "work_queue_dispatch_next", intent_id: "mixed-route-grant", pool: "default", max_claims: 2, max_dispatches: 2 },
      })
    ).rejects.toThrow(/worker_not_compiler_approved/);
    expect(canonical(fixture.transactions)).toBe(before);
    expect(fixture.state.claims.size).toBe(0);
    expect(fixture.state.dispatches.size).toBe(0);
    expect(post).not.toHaveBeenCalled();
  });

  it("applies aw.json global scheduling across shared pools without widening caller worker approvals", async () => {
    const { fixture, options, post } = setupAW({
      granted: false,
      configurePolicy: policy => {
        policy.pools.default.profiles.daily = { ...policy.pools.default.profiles.default, workflow: ".github/workflows/daily.lock.yml" };
        Object.assign(policy, applySettings(policy, parseSettings({ concurrency: 4, pending_limit: 32, retry: { max_attempts: 5, backoff_seconds: 60 }, pools: { daily: { concurrency: 2 } } })));
      },
    });
    expect(fixture.state.policy.authorization).toBe("aw");
    expect(fixture.state.policy.producers).toEqual({});
    expect(fixture.state.policy.limits.pending_nodes).toBe(32);
    for (const [name, limit] of [
      ["default", 4],
      ["daily", 2],
    ]) {
      const pool = fixture.state.policy.pools[name];
      expect(pool.native_limit).toBe(limit);
      expect(pool.retry).toEqual({ max_attempts: 5, backoff_ms: 60000 });
      for (const profile of Object.values(pool.profiles)) {
        expect(profile).not.toHaveProperty("principal");
        expect(profile).toMatchObject({ credential_scope: "repository", effect_scope: REPOSITORY, trust_domain: "default" });
      }
    }
    const getWorkflowRun = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => {
      const response = await getWorkflowRun(parameters);
      if (parameters.run_id === "42") response.data.display_title = `gh-aw work-queue ${[...fixture.state.dispatches.values()][0].dispatch_id}`;
      return response;
    };
    expect(
      await dispatchQueueIntent({
        ...options,
        remainingDispatches: 1,
        runDispatchBudget: 1,
        message: { type: "work_queue_dispatch_next", intent_id: "global-default", pool: "default", max_claims: 1, max_dispatches: 1 },
      })
    ).toMatchObject({ dispatches: 1 });
    expect(post).toHaveBeenCalledOnce();
    expect([...fixture.state.dispatches.values()][0].run.principal).toBe("22");
    const root = workDefinition([...fixture.state.works.values()][0]);
    fixture.append(
      "submit",
      { nodes: [{ ...root, graph_id: "daily-graph", pool: "daily", worker_profile: "daily", node_key: "daily", work_id: nodeId("daily-graph", "daily") }] },
      { role: "producer", principal: "33", repository: REPOSITORY }
    );
    const before = canonical(fixture.transactions);
    await expect(
      dispatchQueueIntent({
        ...options,
        remainingDispatches: 1,
        runDispatchBudget: 2,
        message: { type: "work_queue_dispatch_next", intent_id: "global-daily", pool: "daily", max_claims: 1, max_dispatches: 1 },
      })
    ).rejects.toThrow(/worker_not_compiler_approved/);
    expect(canonical(fixture.transactions)).toBe(before);
    expect(fixture.state.claims.size).toBe(1);
    expect(post).toHaveBeenCalledOnce();
    expect(options.config).toEqual({ work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] });
  });

  it("does not treat the global installed Policy as authority for an unapproved selected launch", async () => {
    const { fixture, options, post } = setupAW({
      configurePolicy: policy => {
        policy.pools.default.profiles.default.workflow = ".github/workflows/daily.lock.yml";
      },
    });
    await expect(launchAssignment(options, fixture.assignment)).rejects.toThrow(/worker_not_compiler_approved/);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).state).toBe("reserved");
    expect(post).not.toHaveBeenCalled();
    expect(fixture.githubClient.rest.repos.getContent).not.toHaveBeenCalled();
  });
});
