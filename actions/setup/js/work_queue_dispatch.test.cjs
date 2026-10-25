// @ts-check
import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import {
  launchAssignment,
  dispatchQueueIntent,
  processWorkQueueIntents,
  configuredDispatchBudget,
  inheritWorkerSubmission,
  intentContext,
  resolveAdmissionResources,
  refreshDependencies,
  assertRemediationBackend,
  main as publishQueueControls,
} from "./work_queue_dispatch.cjs";
import { cancelBeforeLaunch, cancellationOperations, reconcileDispatch, reconcileQueue, reconciliationCandidates } from "./work_queue_reconciler.cjs";
import { appendCommit, compactTransactions, generateRequestOperations, newRequest, replayTransactions, serializeProjection } from "./work_queue_replay.cjs";
import { assignmentOnly } from "./work_queue_scheduler.cjs";
import { nodeId } from "./work_queue_graph.cjs";
import { validateStoredAssignment } from "./work_queue_binding.cjs";
import { installWorkQueuePools } from "./work_queue_pool_install.cjs";
import { synchronizeDeployments } from "./work_queue_deployment_control.cjs";
import { authenticatePublisher } from "./work_queue_native.cjs";
import { requestForIntent, stagePlanIntents } from "./work_queue_intents.cjs";
import { canonical, digest } from "./work_queue_codec.cjs";
import { queueFixture, noWriteClaimVerifier, DISPATCHER, REF, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";
import { reconcileWorkerClaim, finalizeWorkerResults } from "./finish_work_queue_claim.cjs";
import { fakeGitHub } from "./work_queue_store_checks.cjs";

function setup(settings = {}) {
  const fixture = queueFixture(settings);
  const post = vi.fn().mockResolvedValue({ status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } });
  fixture.githubClient.rest.actions.createWorkflowDispatch = post;
  fixture.githubClient.rest.actions.listWorkflowRuns = vi.fn().mockResolvedValue({ status: 200, data: { workflow_runs: [] } });
  const options = {
    assignment: fixture.assignment,
    githubClient: fixture.githubClient,
    dispatchClient: fixture.githubClient,
    validateDispatchCredential: fixture.validateDispatchCredential,
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

function setupPoolInstallation(newWorker = false) {
  const result = setup({
    granted: false,
    configurePolicy: policy => {
      policy.authorization = "aw";
      policy.producers = {};
      delete policy.pools.default.profiles.default.principal;
      policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
    },
  });
  const { fixture, options } = result;
  const proposal = structuredClone(fixture.policy);
  proposal.pools.smoke = structuredClone(proposal.pools.default);
  proposal.pools.smoke.logical_limit = 1;
  proposal.pools.smoke.native_limit = 1;
  if (newWorker) {
    const profile = { ...proposal.pools.default.profiles.default, workflow: ".github/workflows/smoke-work-queue-worker.lock.yml", max_claims: 1, share_keys: false };
    proposal.pools.default.profiles["smoke-work-queue-worker"] = profile;
    proposal.pools.smoke.profiles["smoke-work-queue-worker"] = structuredClone(profile);
    proposal.pools.smoke.default_profile = "smoke-work-queue-worker";
    options.config.work_queue_workflows.push("smoke-work-queue-worker");
    options.config.aw_context_workflows.push("smoke-work-queue-worker");
  }
  const contents = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${"a".repeat(64)}"\n`;
  fixture.githubClient.rest.repos.getContent = vi.fn(async ({ path: workflowPath }) => ({
    data: {
      type: "file",
      path: workflowPath,
      encoding: "base64",
      content: Buffer.from(workflowPath.endsWith(".md") ? "---\ntools:\n  work-queue:\n    worker: true\n---\nWorker.\n" : contents).toString("base64"),
    },
  }));
  fixture.githubClient.rest.repos.get = async () => ({ status: 200, data: { full_name: REPOSITORY, id: 7, default_branch: "main", permissions: { push: true } } });
  const getRef = fixture.githubClient.rest.git.getRef;
  fixture.githubClient.rest.git.getRef = async parameters => (parameters.ref === "heads/main" ? { data: { ref: "refs/heads/main", object: { type: "commit", sha: REF } } } : getRef(parameters));
  fixture.githubClient.rest.actions.getWorkflow = vi.fn(async ({ workflow_id }) => ({ data: { path: `.github/workflows/${workflow_id}`, state: "active" } }));
  return { ...result, options: { ...options, policyProposal: proposal }, proposal };
}

function failedFixture(disposition) {
  const result = setup({ bound: true, count: 1, workerPrincipal: "22" });
  const { fixture } = result;
  const assignment = fixture.assignment;
  const member = assignment.claims[0];
  fixture.append("finish", { dispatch_id: assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: assignment.dispatch_id, claim_handle: member.handle });
  const state = fixture.state;
  const profile = state.dispatches.get(assignment.dispatch_id).profile;
  fixture.append(
    "delivery_failure",
    {
      operations: [
        {
          kind: "DeliveryFailure",
          work_id: member.work_id,
          claim_id: member.claim_id,
          completion_id: state.works.get(member.work_id).completion_id,
          reason: "verification_exhausted",
          disposition,
          evidence: {
            kind: "terminal_run",
            source: "github_api",
            repository: REPOSITORY,
            workflow: profile.workflow,
            ref: profile.ref,
            principal: profile.principal,
            checked_at: fixture.at,
            run_id: "42",
            run_attempt: 1,
            status: "completed",
            conclusion: "failure",
            attempts: state.policy.pools[assignment.pool].reconciliation.max_attempts,
            effects: disposition,
          },
        },
      ],
    },
    { ...fixture.dispatcher, role: "reconciler" }
  );
  return { ...result, member };
}

function compactQueue(options, fixture, transactions = fixture.transactions) {
  let ledger = compactTransactions(transactions, "c".repeat(40), fixture.administrator, fixture.at);
  options.readWorkQueueLog = async () => ({ sha: "compacted-head", transactions: ledger, state: replayTransactions(ledger) });
  options.publishWorkQueueRequest = async parameters => {
    const state = replayTransactions(ledger);
    const at = ledger.at(-1).at + 1;
    const id = `post-checkpoint-${ledger.length}`;
    const decision = (parameters.generateOperations || generateRequestOperations)(state, parameters.request, parameters.actor, at, id);
    const commit = { version: 3, id, previous: state.tip, request: parameters.request, actor: parameters.actor, policy_epoch: state.policy_epoch, at, operations: decision.operations };
    const result = appendCommit(ledger, commit);
    ledger = result.transactions;
    return { ...result, ...decision, persisted: true, publishedNow: true };
  };
  return ledger;
}

describe("native queue launch fencing and conservative recovery", () => {
  it.each(["cancelled", "failure", "success"])("reclaims never-started singleton reservations after the exact originating attempt ends with %s", async conclusion => {
    const { fixture, options, post } = setup({ batch: 1 });
    const getAttempt = vi.fn().mockImplementation(async parameters => {
      expect(parameters).toMatchObject({ run_id: "15", attempt_number: 1 });
      return { status: 200, data: { ...fixture.nativeRun(fixture.dispatcherContext), status: "completed", conclusion } };
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = getAttempt;
    const result = await reconcileQueue({ ...options, pool: "default", verifyEffects: vi.fn() });
    expect(result.results).toHaveLength(3);
    expect(result.results.every(result => result.released)).toBe(true);
    expect([...fixture.state.dispatches.values()].every(dispatch => dispatch.released && dispatch.state === "reserved")).toBe(true);
    expect([...fixture.state.works.values()].every(work => work.state === "available")).toBe(true);
    expect([...fixture.state.claims.values()].every(claim => claim.state === "cancelled")).toBe(true);
    expect(getAttempt).toHaveBeenCalledTimes(3);
    expect(post).not.toHaveBeenCalled();
    const before = canonical(fixture.transactions);
    expect((await reconcileQueue(options)).reconciled).toBe(0);
    expect(canonical(fixture.transactions)).toBe(before);
  });

  it("releases an abandoned reservation before the next dispatcher grants and launches retry Work", async () => {
    const { fixture, options, post } = setup({ count: 1, configurePolicy: policy => (policy.pools.default.retry.backoff_ms = 1) });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async parameters =>
      parameters.run_id === "15" ? { status: 200, data: { ...fixture.nativeRun(fixture.dispatcherContext), status: "completed", conclusion: "cancelled" } } : fixture.githubClient.rest.actions.getWorkflowRun(parameters);
    const context = { ...fixture.dispatcherContext, runId: "16" };
    fixture.githubClient.rest.actions.getWorkflowRun = async ({ run_id }) => ({
      status: 200,
      data: run_id === "42" ? { ...fixture.nativeRun(), display_title: `gh-aw work-queue ${[...fixture.state.dispatches.values()].at(-1).dispatch_id}` } : fixture.nativeRun(context),
    });
    const result = await dispatchQueueIntent({
      ...options,
      context,
      assignment: null,
      now: fixture.at,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "recover-abandoned", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ success: true, dispatches: 1, launches: [expect.objectContaining({ state: "bound", run_id: "42" })] });
    expect([...fixture.state.works.values()][0]).toMatchObject({ state: "claimed", attempts: 2 });
    expect([...fixture.state.dispatches.values()].map(dispatch => dispatch.released)).toEqual([true, false]);
    expect(post).toHaveBeenCalledTimes(1);
  });

  it.each([
    { status: "in_progress", conclusion: null },
    { status: "queued", conclusion: null },
    { status: "completed", conclusion: null },
  ])("retains a never-started reservation without terminal origin evidence: %j", async override => {
    const { fixture, options, post } = setup({ count: 1 });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async () => ({ status: 200, data: { ...fixture.nativeRun(fixture.dispatcherContext), ...override } });
    const before = canonical(fixture.transactions);
    expect(await reconcileDispatch(options)).toEqual({ state: "reserved", released: false });
    expect(canonical(fixture.transactions)).toBe(before);
    expect(post).not.toHaveBeenCalled();
  });

  it.each([{ id: "99" }, { run_attempt: 2 }, { path: ".github/workflows/foreign.lock.yml" }, { actor: { id: "99" } }, { repository: { full_name: REPOSITORY, id: 99 } }, { repository: { full_name: "foreign/repo", id: 7 } }])(
    "does not release on mismatched terminal dispatcher evidence: %j",
    async override => {
      const { fixture, options, post } = setup({ count: 1 });
      fixture.githubClient.rest.actions.getWorkflowRunAttempt = async () => ({ status: 200, data: { ...fixture.nativeRun(fixture.dispatcherContext), status: "completed", conclusion: "cancelled", ...override } });
      const before = canonical(fixture.transactions);
      expect(await reconcileQueue(options)).toMatchObject({ results: [{ state: "launch_unresolved", released: false }] });
      expect(canonical(fixture.transactions)).toBe(before);
      expect(post).not.toHaveBeenCalled();
    }
  );

  it("retains reservations when origin evidence is unavailable and reads the exact later attempt when available", async () => {
    const { fixture, options, post } = setup({ count: 1, dispatcherRunAttempt: 2 });
    const getAttempt = vi.fn().mockRejectedValue(Object.assign(new Error("unavailable"), { status: 403 }));
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = getAttempt;
    const before = canonical(fixture.transactions);
    expect(await reconcileQueue(options)).toMatchObject({ results: [{ state: "launch_unresolved", released: false }] });
    expect(canonical(fixture.transactions)).toBe(before);
    expect(getAttempt.mock.calls[0][0]).toMatchObject({ run_id: "15", attempt_number: 2, request: { retries: 0, timeout: 15000 } });
    getAttempt.mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(fixture.dispatcherContext), status: "completed", conclusion: "cancelled" } });
    expect(await reconcileDispatch(options)).toMatchObject({ released: true });
    expect(post).not.toHaveBeenCalled();
  });

  it("rechecks the ledger after terminal origin evidence and cannot release a concurrent start marker", async () => {
    const { fixture, options, post } = setup({ count: 1 });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async () => {
      fixture.append("dispatch", { operations: [{ kind: "Dispatch", dispatch_id: fixture.assignment.dispatch_id, state: "started", sender: fixture.dispatcher }] }, fixture.dispatcher);
      return { status: 200, data: { ...fixture.nativeRun(fixture.dispatcherContext), status: "completed", conclusion: "cancelled" } };
    };
    await expect(reconcileDispatch(options)).rejects.toThrow(/may_have_started/);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ state: "started", released: false });
    expect(post).not.toHaveBeenCalled();
  });

  it("rejects prelaunch release publication when a start marker wins after the fresh ledger check", async () => {
    const { fixture, options, post } = setup({ count: 1 });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async () => ({ status: 200, data: { ...fixture.nativeRun(fixture.dispatcherContext), status: "completed", conclusion: "cancelled" } });
    const publish = options.publishWorkQueueRequest;
    options.publishWorkQueueRequest = async args => {
      if (args.request.kind === "release") fixture.append("dispatch", { operations: [{ kind: "Dispatch", dispatch_id: fixture.assignment.dispatch_id, state: "started", sender: fixture.dispatcher }] }, fixture.dispatcher);
      return publish(args);
    };
    expect(await reconcileQueue(options)).toMatchObject({ results: [{ state: "launch_unresolved", released: false }] });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ state: "started", released: false });
    expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
    expect(post).not.toHaveBeenCalled();
  });

  it("includes never-started reservations in bounded, pool-scoped reconciliation", () => {
    const dispatches = new Map([
      ["reserved", { dispatch_id: "reserved", pool: "default", state: "reserved", released: false }],
      ["bound", { dispatch_id: "bound", pool: "default", state: "bound", run: {}, released: false }],
      ["foreign", { dispatch_id: "foreign", pool: "other", state: "reserved", released: false }],
      ["released", { dispatch_id: "released", pool: "default", state: "reserved", released: true }],
    ]);
    expect(reconciliationCandidates({ dispatches }, "default", 16).map(dispatch => dispatch.dispatch_id)).toEqual(["bound", "reserved"]);
    expect(reconciliationCandidates({ dispatches }, "default", 1, 1)).toHaveLength(1);
  });

  it("retains released pending keep-open barriers after Work ownership changes", () => {
    const retained = {
      dispatch_id: "retained",
      pool: "default",
      state: "bound",
      run: { run_id: "42" },
      released: true,
      claims: [{ claim_id: "original", work_id: "work", handle: "h1" }],
    };
    const ordinary = {
      dispatch_id: "ordinary",
      pool: "default",
      state: "bound",
      run: { run_id: "43" },
      released: true,
      claims: [{ claim_id: "old", work_id: "other", handle: "h2" }],
    };
    const projection = {
      dispatches: new Map([
        [retained.dispatch_id, retained],
        [ordinary.dispatch_id, ordinary],
      ]),
      claims: new Map([
        ["original", { state: "completed", keep_open: true, barrier: "pending" }],
        ["old", { state: "completed", barrier: "pending" }],
      ]),
      works: new Map([
        ["work", { claim_id: "regrant", barrier: "pending" }],
        ["other", { claim_id: "new-owner", barrier: "pending" }],
      ]),
    };

    expect(reconciliationCandidates(projection, "default", 16)).toEqual([retained]);
  });

  it.each([undefined, false])("does not run delivery expiry from reconciliation when opt-in is %s", async expireUnverified => {
    const { fixture, options } = setup({ bound: true, count: 1 });
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    const finalizer = require("./finish_work_queue_claim.cjs");
    const finalize = vi.spyOn(finalizer, "finalizeWorkerResults");
    try {
      await reconcileQueue({ ...options, ...(expireUnverified === undefined ? {} : { expireUnverified }) });
      expect(finalize).not.toHaveBeenCalled();
      expect(fixture.state.works.get(member.work_id).barrier).toBe("pending");
    } finally {
      finalize.mockRestore();
    }
  });

  it("rotates bounded reconciliation sweeps so every outstanding dispatch is revisited", () => {
    const dispatches = new Map(
      Array.from({ length: 17 }, (_, index) => {
        const dispatch_id = `dispatch-${String(index + 1).padStart(2, "0")}`;
        return [dispatch_id, { dispatch_id, pool: "default", released: false, state: "uncertain" }];
      })
    );
    const seen = new Set();
    for (let runNumber = 1; runNumber <= 17; runNumber++) {
      const batch = reconciliationCandidates({ dispatches }, "default", 16, runNumber);
      expect(batch).toHaveLength(16);
      for (const dispatch of batch) seen.add(dispatch.dispatch_id);
    }
    expect(seen).toEqual(new Set(dispatches.keys()));
  });

  it("canonicalizes context casing before start, binding and release without changing existing request history", async () => {
    const { fixture, options, post } = setup();
    options.context = { ...options.context, repo: { owner: "OWNER", repo: "REPO" } };
    const before = canonical(fixture.transactions);
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "bound", run_id: "42" });
    expect(canonical(fixture.transactions.slice(0, 3))).toBe(before);
    expect(post.mock.calls[0][0]).toMatchObject({ owner: "owner", repo: "repo" });
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch.sender.repository).toBe(REPOSITORY);
    expect(dispatch.run.repository).toBe(REPOSITORY);
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async () => ({ status: 200, data: { ...fixture.nativeRun(), status: "completed", conclusion: "cancelled" } });
    expect(await reconcileDispatch(options)).toMatchObject({ released: true });
  });

  it("denies unavailable or foreign repository proof before a start marker or dispatch POST", async () => {
    const { fixture, options, post } = setup();
    const before = canonical(fixture.transactions);
    fixture.githubClient.rest.repos.get = vi.fn().mockResolvedValue({ status: 200, data: { full_name: "foreign/repo", id: 7 } });
    await expect(launchAssignment(options, fixture.assignment)).rejects.toThrow(/repository_identity_mismatch/);
    expect(canonical(fixture.transactions)).toBe(before);
    expect(post).not.toHaveBeenCalled();
    delete fixture.githubClient.rest.repos.get;
    await expect(launchAssignment(options, fixture.assignment)).rejects.toThrow(/repository_access_unavailable/);
    expect(canonical(fixture.transactions)).toBe(before);
    expect(post).not.toHaveBeenCalled();
  });

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

  it.each(["paused", "ledger_watermark"])("skips all optional native dependency reads when the shared %s refresh budget is zero", async condition => {
    const { fixture, options, post } = setup(externalSettings);
    if (condition === "paused") fixture.append("control", { operations: [{ kind: "Control", control: "grants_paused", value: true, reason: "operator_pause" }] });
    const state = fixture.state;
    if (condition === "ledger_watermark") state.ledgerBytes = state.policy.limits.ledger_bytes;
    const before = serializeProjection(state);
    const log = canonical(fixture.transactions);
    const resolver = resolverFor(fixture);
    const operations = await refreshDependencies({ ...options, dependencyResolver: resolver }, state, "default", fixture.dispatcher);
    expect(operations).toEqual([]);
    expect(resolver.getClient).not.toHaveBeenCalled();
    expect(resolver.client.rest.repos.get).not.toHaveBeenCalled();
    expect(resolver.client.rest.issues.get).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
    expect(serializeProjection(state)).toEqual(before);
    expect(canonical(fixture.transactions)).toBe(log);
  });

  it("reserves a Claim operation before reading any dependency frontier", async () => {
    const { fixture, options } = setup({
      ...externalSettings,
      workDefaults: { depends_on: [edge, { ...edge, resource: { ...edge.resource, resource_id: "31", number: "8" } }, { ...edge, resource: { ...edge.resource, resource_id: "32", number: "9" } }] },
      configurePolicy: policy => {
        externalSettings.configurePolicy(policy);
        policy.pools.default.profiles.default.max_claims = 1;
        policy.limits.operations = 3;
      },
    });
    const resolver = resolverFor(fixture);
    resolver.client.rest.issues.get.mockImplementation(async ({ issue_number }) => ({ status: 200, data: { id: Number(issue_number) + 23, number: Number(issue_number), state: "closed", state_reason: "completed" } }));
    const before = canonical(fixture.transactions);
    const operations = await refreshDependencies({ ...options, dependencyResolver: resolver }, fixture.state, "default", fixture.dispatcher);
    expect(operations).toHaveLength(2);
    expect(operations.map(operation => operation.resource.number)).toEqual(["7", "8"]);
    expect(operations.every(operation => operation.kind === "Observation" && operation.state === "ready")).toBe(true);
    expect(resolver.getClient).toHaveBeenCalledTimes(2);
    expect(resolver.client.rest.repos.get).toHaveBeenCalledTimes(1);
    expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(2);
    expect(fixture.state.observations.size).toBe(0);
    expect(canonical(fixture.transactions)).toBe(before);
  });

  it("grants independent ready Work with three unavailable gates under the three-operation limit", async () => {
    const { fixture, options, post } = setup({
      ...externalSettings,
      workDefaults: { depends_on: [edge, { ...edge, resource: { ...edge.resource, resource_id: "31", number: "8" } }, { ...edge, resource: { ...edge.resource, resource_id: "32", number: "9" } }] },
      configurePolicy: policy => {
        externalSettings.configurePolicy(policy);
        policy.pools.default.profiles.default.max_claims = 1;
        policy.limits.operations = 3;
      },
    });
    const waitingId = fixture.transactions[1].operations[0].work_id;
    const readyId = nodeId("budget-frontier", "ready");
    fixture.append(
      "submit",
      {
        nodes: [{ ...fixture.transactions[1].operations[0], work_id: readyId, graph_id: "budget-frontier", node_key: "ready", depends_on: [] }],
      },
      { role: "producer", principal: "11", repository: REPOSITORY }
    );
    const trusted = await intentContext(options, {});
    const resolver = { ...resolverFor(fixture), scopes: [] };
    const request = requestForIntent(trusted, "budget-independent-ready", "dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: fixture.policy.limits.assignment_bytes });
    const result = await fixture.publishWorkQueueRequest({
      context: trusted,
      request,
      refreshObservations: (state, stable, actor) => refreshDependencies({ ...options, dependencyResolver: resolver, now: fixture.at }, state, stable.parameters.pool, actor),
    });
    expect(result.commit.operations.map(operation => operation.kind)).toEqual(["Observation", "Observation", "Claim"]);
    expect(result.commit.operations.slice(0, 2).every(operation => operation.state === "unknown" && operation.read_status === "external_not_allowlisted")).toBe(true);
    expect(result.commit.operations[2].work_id).toBe(readyId);
    expect(fixture.state.works.get(waitingId).state).toBe("available");
    expect(fixture.state.observations.size).toBe(2);
    expect(resolver.getClient).not.toHaveBeenCalled();
    expect(resolver.client.rest.repos.get).not.toHaveBeenCalled();
    expect(resolver.client.rest.issues.get).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
  });

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
    const beforeSnapshot = await fixture.readWorkQueueLog();
    const beforeProjection = serializeProjection(beforeSnapshot.state);
    const beforeLog = canonical(beforeSnapshot.transactions);
    const beforeClock = fixture.at;
    const resolver = resolverFor(fixture, "open");
    const result = await dispatchQueueIntent({
      ...options,
      dependencyResolver: resolver,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "no-grant-observation", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ status: "no_grant", dispatches: 0 });
    expect(fixture.transactions).toEqual(before);
    const afterSnapshot = await fixture.readWorkQueueLog();
    expect(afterSnapshot.sha).toBe(beforeSnapshot.sha);
    expect(canonical(afterSnapshot.transactions)).toBe(beforeLog);
    expect(serializeProjection(afterSnapshot.state)).toEqual(beforeProjection);
    expect(fixture.at).toBe(beforeClock);
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
    await expect(refreshDependencies(configured, fixture.state, "default", fixture.workerActor)).rejects.toThrow(/actor_unauthorized/);
    expect(resolver.getClient).toHaveBeenCalledTimes(1);
    expect(fixture.state.observations.size).toBe(0);
  });

  it("never probes worker dependencies despite a configured resolver and preserves independent ready grants", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 1, workerPrincipal: "22", configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design") });
    const parent = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle });
    const template = fixture.transactions[1].operations[0];
    const waitingId = nodeId("foreign-frontier", "child");
    const readyId = nodeId("foreign-frontier", "ready");
    fixture.append(
      "submit",
      {
        nodes: [
          { ...template, work_id: waitingId, graph_id: "foreign-frontier", node_key: "child", depends_on: [edge] },
          { ...template, work_id: readyId, graph_id: "foreign-frontier", node_key: "ready", depends_on: [] },
        ],
      },
      { role: "producer", principal: "11", repository: REPOSITORY }
    );
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
    expect(result.commit.operations.map(operation => operation.kind)).toEqual(["Claim"]);
    expect(result.commit.operations[0].work_id).toBe(readyId);
    expect(fixture.state.works.get(waitingId).state).toBe("available");
    expect(fixture.state.observations.size).toBe(0);
    expect(resolver.getClient).not.toHaveBeenCalled();
    expect(resolver.client.rest.repos.get).not.toHaveBeenCalled();
    expect(resolver.client.rest.issues.get).not.toHaveBeenCalled();
    const reads = resolver.getClient.mock.calls.length;
    const corrupted = structuredClone(fixture.state);
    corrupted.works.get(parent.work_id).barrier = "failed";
    await expect(refreshDependencies({ ...options, dependencyResolver: resolver }, corrupted, "default", trusted)).rejects.toThrow(/claim_effects_unauthorized/);
    expect(resolver.getClient).toHaveBeenCalledTimes(reads);
    expect(post).not.toHaveBeenCalled();
  });

  it("requires a host read resolver before a worker proposes observations, even when independent Work can be granted", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 1, workerPrincipal: "22", configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design") });
    const parent = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle });
    const template = fixture.transactions[1].operations[0];
    const waitingId = nodeId("reader-capability", "waiting");
    const readyId = nodeId("reader-capability", "ready");
    fixture.append(
      "submit",
      {
        nodes: [
          { ...template, work_id: waitingId, graph_id: "reader-capability", node_key: "waiting", depends_on: [edge] },
          { ...template, work_id: readyId, graph_id: "reader-capability", node_key: "ready" },
        ],
      },
      { role: "producer", principal: "11", repository: REPOSITORY }
    );
    const trusted = await intentContext({ ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}` }, { claim_handle: parent.handle });
    expect(await refreshDependencies(options, fixture.state, "default", trusted)).toEqual([]);
    await expect(refreshDependencies(options, fixture.state, "default", { ...trusted, ref: "b".repeat(40) })).rejects.toThrow(/run_binding_conflict/);
    const request = requestForIntent(trusted, "worker-without-reader", "dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: fixture.policy.limits.assignment_bytes });
    const result = await fixture.publishWorkQueueRequest({
      context: trusted,
      request,
      refreshObservations: (state, stable) => refreshDependencies(options, state, stable.parameters.pool, trusted),
    });
    expect(result.commit.actor).toMatchObject({ role: "worker", principal: "22", dispatch_id: fixture.assignment.dispatch_id, claim_handle: parent.handle });
    expect(result.commit.operations.map(operation => operation.kind)).toEqual(["Claim"]);
    expect(result.state.works.get(readyId).state).toBe("claimed");
    expect(result.state.works.get(waitingId).state).toBe("available");
    expect(result.state.observations.size).toBe(0);
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

  it("requires a host remediation backend despite agent-supplied inspection or disposition strings", () => {
    const failed = { barrier: "failed", disposition: "unknown" };
    const state = { works: new Map([["failed", failed]]) };
    const parameters = { nodes: [{ replacement_of: { work_id: "failed", disposition: "inspection", evidence: "agent-assertion" } }] };
    expect(() => assertRemediationBackend(state, parameters)).toThrow("work_queue_trusted_remediation_required");
    failed.disposition = "partial";
    parameters.nodes[0].replacement_of.disposition = "idempotent";
    expect(() => assertRemediationBackend(state, parameters)).toThrow("work_queue_trusted_remediation_required");
    expect(() => assertRemediationBackend(state, parameters, "agent-assertion")).toThrow("work_queue_trusted_remediation_required");
    expect(() => assertRemediationBackend(state, parameters, async () => true)).not.toThrow();
    failed.disposition = "none";
    expect(() => assertRemediationBackend(state, parameters)).not.toThrow();
  });

  it.each(["partial", "unknown"])("publishes %s replacements only through a genuine host verifier and recovers accepted requests read-only", async disposition => {
    const { fixture, options, post, member } = failedFixture(disposition);
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-remediation-"));
    const filename = path.join(directory, "intents.jsonl");
    const payload = { plan: "inspect independently verified effects", effect_contract: { kind: "none" } };
    const node = { graph_id: "g1", node_key: "replacement", payload, replacement_of: { work_id: member.work_id, disposition: "inspection", evidence: "agent-assertion" } };
    const intent = { version: 3, intent_id: "remediate", kind: "submit", parameters: { nodes: [node] } };
    try {
      fs.writeFileSync(filename, `${JSON.stringify(intent)}\n`);
      const producer = { ...options, assignment: null, intentPath: filename };
      const before = fixture.transactions;
      for (const remediationVerifier of [
        undefined,
        async () => false,
        async () => ({ verified: true }),
        async () => {
          throw new Error("domain proof unavailable");
        },
      ]) {
        expect((await processWorkQueueIntents({ ...producer, remediationVerifier })).receipts[0].status).toBe("blocked");
        expect(fixture.transactions).toEqual(before);
      }
      const remediationVerifier = vi.fn(async (current, candidate, trusted) => {
        expect(current.works.get(member.work_id).disposition).toBe(disposition);
        expect(candidate.replacement_of.work_id).toBe(member.work_id);
        expect(trusted.principal).toBe(fixture.dispatcher.principal);
        candidate.payload.plan = "a verifier cannot rewrite admission semantics";
        return true;
      });
      const result = await processWorkQueueIntents({ ...producer, remediationVerifier });
      expect(result.receipts[0].status).toBe("durable");
      expect(remediationVerifier).toHaveBeenCalledTimes(1);
      expect(fixture.state.works.get(nodeId("g1", "replacement")).payload).toEqual(payload);
      const committed = fixture.transactions;
      const recovered = await processWorkQueueIntents(producer);
      expect(recovered.receipts[0]).toEqual(result.receipts[0]);
      expect(fixture.transactions).toEqual(committed);
      expect(remediationVerifier).toHaveBeenCalledTimes(1);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each(["partial", "unknown"])("carries protected %s remediation proof through the live entry point and real checked CAS publisher", async disposition => {
    const { fixture, options, post, member } = failedFixture(disposition);
    const fake = fakeGitHub(fixture.transactions);
    fake.githubClient.rest.repos = fixture.githubClient.rest.repos;
    fake.githubClient.rest.actions = fixture.githubClient.rest.actions;
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-live-remediation-"));
    const intentPath = path.join(directory, "intents.jsonl");
    const payload = { effect_contract: { kind: "none" }, remediation_verifier: "approved", job_success: true };
    const node = { graph_id: "g1", node_key: "replacement", payload, replacement_of: { work_id: member.work_id, disposition: "inspection", evidence: "agent-assertion" } };
    const producer = {
      ...options,
      assignment: null,
      intentPath,
      githubClient: fake.githubClient,
      dispatchClient: fake.githubClient,
      readWorkQueueLog: undefined,
      publishWorkQueueRequest: undefined,
      core: {
        info: vi.fn(),
        setOutput: vi.fn(),
        setFailed: vi.fn(),
        summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
      },
    };
    const call = remediationVerifier => publishQueueControls({ ...producer, remediationVerifier });
    try {
      fs.writeFileSync(intentPath, `${JSON.stringify({ version: 3, intent_id: "live-remediate", kind: "submit", parameters: { nodes: [node] } })}\n`);
      const original = canonical(fake.log());
      const originalRef = fake.refs.get("work-queue");
      for (const verifier of [
        undefined,
        "approved",
        async () => false,
        async () => ({ verified: true }),
        async () => {
          throw new Error("proof unavailable");
        },
      ]) {
        expect((await call(verifier)).receipts[0].status).toBe("blocked");
        expect(canonical(fake.log())).toBe(original);
        expect(fake.refs.get("work-queue")).toBe(originalRef);
      }
      const competingId = nodeId("g1", "competing");
      const proof = vi.fn(async current => !current.works.has(competingId));
      fake.state.beforeUpdate = async ({ install }) => {
        const template = fixture.transactions[1].operations[0];
        fixture.append("submit", { nodes: [{ ...template, work_id: competingId, node_key: "competing" }] }, { ...fixture.dispatcher, role: "producer" }, "competing-remediation");
        install(fixture.transactions);
      };
      expect((await call(proof)).receipts[0].status).toBe("blocked");
      expect(proof).toHaveBeenCalledTimes(2);
      expect(proof.mock.calls[0][0].tip).not.toBe(proof.mock.calls[1][0].tip);
      expect(canonical(fake.log())).toBe(canonical(fixture.transactions));
      const approved = vi.fn(async (current, candidate, trusted) => {
        expect(current.works.get(member.work_id).disposition).toBe(disposition);
        expect(candidate.replacement_of.work_id).toBe(member.work_id);
        expect(trusted).toMatchObject({ role: "dispatcher", principal: "11", repository: REPOSITORY, workflow: DISPATCHER, run_id: "15", run_attempt: 1 });
        return true;
      });
      const accepted = await call(approved);
      expect(accepted.receipts[0].status).toBe("durable");
      expect(approved).toHaveBeenCalledTimes(1);
      const authoritative = canonical(fake.log());
      const head = fake.refs.get("work-queue");
      expect((await call(undefined)).receipts[0]).toEqual(accepted.receipts[0]);
      expect(canonical(fake.log())).toBe(authoritative);
      expect(fake.refs.get("work-queue")).toBe(head);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
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
      expect(parameters.ref).toBe(`refs/tags/gh-aw/work-queue/${REF}`);
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

  it.each(["not_found", "unavailable", "timeout", "nested_socket", "nested_reset", "nested_timeout", "title_pending"])("verifies an accepted %s launch with bounded reads and exactly one POST", async failure => {
    const { fixture, post, options } = setup({ count: 1 });
    const get = fixture.githubClient.rest.actions.getWorkflowRunAttempt;
    const read = vi.fn(async parameters => {
      if (read.mock.calls.length === 1) {
        if (failure === "title_pending") return { status: 200, data: { ...fixture.nativeRun(), display_title: "worker" } };
        if (failure.startsWith("nested_")) throw { cause: { cause: { code: failure === "nested_socket" ? "UND_ERR_SOCKET" : failure === "nested_reset" ? "ECONNRESET" : "ETIMEDOUT" } } };
        throw Object.assign(new Error("private response"), failure === "timeout" ? { code: "ETIMEDOUT" } : { status: failure === "not_found" ? 404 : 503 });
      }
      return get(parameters);
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    const sleepFn = vi.fn(async () => {});
    expect(await launchAssignment({ ...options, sleepFn }, fixture.assignment)).toMatchObject({ state: "bound", run_id: "42", dispatched: true });
    expect(read).toHaveBeenCalledTimes(2);
    expect(read.mock.calls.every(([parameters]) => parameters.run_id === "42" && parameters.attempt_number === 1 && parameters.request.retries === 0)).toBe(true);
    expect(sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    expect(post).toHaveBeenCalledTimes(1);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ state: "bound", released: false, run: { run_id: "42", run_attempt: 1 } });
  });

  it.each(["repository", "attempt"])("retains the reservation after exhausting outer %s verification attempts without nested retries", async endpoint => {
    const { fixture, post, options } = setup({ count: 1, configurePolicy: policy => (policy.pools.default.reconciliation.max_attempts = 2) });
    const read = vi.fn().mockRejectedValue({ status: 503 });
    if (endpoint === "attempt") fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    else {
      const get = fixture.githubClient.rest.repos.get;
      fixture.githubClient.rest.repos.get = parameters => (post.mock.calls.length ? read(parameters) : get(parameters));
    }
    const sleepFn = vi.fn(async () => {});
    expect(await launchAssignment({ ...options, sleepFn }, fixture.assignment)).toMatchObject({ state: "launch_unresolved", run_id: "42", released: false });
    expect(read).toHaveBeenCalledTimes(2);
    expect(sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch).toMatchObject({ state: "uncertain", released: false });
    expect(dispatch.run).toBeUndefined();
    expect(fixture.transactions.at(-1).operations[0]).not.toHaveProperty("evidence");
    expect(post).toHaveBeenCalledTimes(1);
    expect(fixture.state.claims.size).toBe(1);
  });

  it("verifies the original launch attempt even when the latest native attempt is a rerun", async () => {
    const { fixture, post, options } = setup({ count: 1 });
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = vi.fn(async parameters => (parameters.run_id === "42" ? { status: 200, data: { ...fixture.nativeRun(), run_attempt: 2 } } : get(parameters)));
    const getAttempt = vi.fn(async parameters => {
      expect(parameters).toMatchObject({ run_id: "42", attempt_number: 1, request: { retries: 0 } });
      return { status: 200, data: fixture.nativeRun() };
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = getAttempt;
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "bound", run_id: "42", dispatched: true });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ state: "bound", run: { run_id: "42", run_attempt: 1 }, released: false });
    expect(getAttempt).toHaveBeenCalledTimes(1);
    expect(fixture.githubClient.rest.actions.getWorkflowRun.mock.calls.every(([parameters]) => parameters.run_id !== "42")).toBe(true);
    expect(post).toHaveBeenCalledTimes(1);
  });

  it.each([0, 1, 2])("recovers saved native evidence after a binding publication failure and %s compactions without discovery or redispatch", async compactions => {
    const { fixture, post, options } = setup({ count: 1 });
    const publish = options.publishWorkQueueRequest;
    options.publishWorkQueueRequest = async parameters => {
      if (parameters.request.parameters.operations?.some(operation => operation.kind === "Dispatch" && operation.state === "bound")) throw new Error("publication unavailable");
      return publish(parameters);
    };
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "launch_unresolved", run_id: "42", released: false });
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch.run).toBeUndefined();
    expect(fixture.transactions.at(-1).operations[0]).toMatchObject({ state: "uncertain", reason: "binding_unconfirmed", evidence: { run_id: "42", run_attempt: 1, principal: "11" } });
    options.publishWorkQueueRequest = publish;
    let transactions = fixture.transactions;
    for (let index = 0; index < compactions; index++) transactions = compactQueue(options, fixture, transactions);
    const restored = (await options.readWorkQueueLog()).state;
    if (compactions) {
      expect(restored.transactions).toHaveLength(1);
      expect(restored.historicalTransactions.flatMap(commit => commit.operations)).toContainEqual(expect.objectContaining({ reason: "binding_unconfirmed", evidence: expect.objectContaining({ run_id: "42" }) }));
    }
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [] } });
    const getAttempt = vi.fn(async parameters => {
      expect(parameters).toMatchObject({ run_id: "42", attempt_number: 1 });
      return { status: 200, data: fixture.nativeRun() };
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = getAttempt;
    expect(await reconcileDispatch(options)).toMatchObject({ state: "bound", run_id: "42", released: false });
    expect(getAttempt).toHaveBeenCalledTimes(2);
    expect(fixture.githubClient.rest.actions.listWorkflowRuns).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledTimes(1);
    const recovered = (await options.readWorkQueueLog()).state;
    expect(recovered.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ state: "bound", run: { run_id: "42", run_attempt: 1 }, released: false });
    expect(recovered.claims.size).toBe(1);
  });

  it.each(["current", "historical", "checkpoint_boundary"])("prefers the newest retained native run hint across %s transactions", async history => {
    const { fixture, post, options } = setup({ count: 1, started: true });
    const operation = runId => ({
      kind: "Dispatch",
      dispatch_id: fixture.assignment.dispatch_id,
      state: "uncertain",
      reason: "binding_unconfirmed",
      evidence: { kind: "reconciliation", source: "github_api", repository: REPOSITORY, workflow: fixture.binding.workflow, ref: REF, principal: "11", checked_at: fixture.at, run_id: runId, run_attempt: 1 },
    });
    fixture.append("dispatch", { operations: [operation("43")] }, fixture.dispatcher);
    if (history === "checkpoint_boundary") {
      compactQueue(options, fixture);
      await options.publishWorkQueueRequest({
        actor: fixture.dispatcher,
        request: newRequest("newer-returned-hint", "dispatch", fixture.dispatcher, { operations: [operation("42")] }),
      });
    } else {
      fixture.append("dispatch", { operations: [operation("42")] }, fixture.dispatcher);
      if (history === "historical") compactQueue(options, fixture);
    }
    const getAttempt = vi.fn(async parameters => {
      expect(parameters).toMatchObject({ run_id: "42", attempt_number: 1 });
      return { status: 200, data: fixture.nativeRun() };
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = getAttempt;
    expect(await reconcileDispatch(options)).toMatchObject({ state: "bound", run_id: "42", released: false });
    expect(getAttempt).toHaveBeenCalledTimes(2);
    expect(fixture.githubClient.rest.actions.listWorkflowRuns).toHaveBeenCalledExactlyOnceWith(expect.any(Object));
    expect(post).not.toHaveBeenCalled();
    const recovered = (await options.readWorkQueueLog()).state;
    expect(recovered.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ run: { run_id: "42", run_attempt: 1 }, released: false });
  });

  it.each([{ status: 403 }, { head_sha: "b".repeat(40) }, { actor: { id: "99" } }, { run_attempt: 2 }])("does not retry or bind a denied or mismatched returned run: %j", async failure => {
    const { fixture, post, options } = setup({ count: 1 });
    const read = vi.fn(async parameters => {
      expect(parameters).toMatchObject({ run_id: "42", attempt_number: 1 });
      if (failure.status) throw failure;
      return { status: 200, data: { ...fixture.nativeRun(), ...failure } };
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    const sleepFn = vi.fn();
    expect(await launchAssignment({ ...options, sleepFn }, fixture.assignment)).toMatchObject({ state: "launch_unresolved", run_id: "42", released: false });
    expect(read).toHaveBeenCalledTimes(1);
    expect(sleepFn).not.toHaveBeenCalled();
    expect(post).toHaveBeenCalledTimes(1);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
  });

  it.each([
    { endpoint: "attempt", deadline: 1 },
    { endpoint: "attempt", deadline: 1000 },
    { endpoint: "repository", deadline: 1000 },
  ])("stops $endpoint verification reads at the $deadline ms deadline without releasing an uncertain launch", async ({ endpoint, deadline }) => {
    const { fixture, post, options } = setup({ count: 1, configurePolicy: policy => (policy.pools.default.reconciliation.deadline_ms = deadline) });
    const get = fixture.githubClient.rest.repos.get;
    const failure = { status: endpoint === "attempt" ? 404 : 503 };
    const read = vi
      .fn()
      .mockRejectedValueOnce(failure)
      .mockRejectedValueOnce(failure)
      .mockImplementation(async parameters => (endpoint === "attempt" ? { status: 200, data: fixture.nativeRun() } : get(parameters)));
    if (endpoint === "attempt") fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    else fixture.githubClient.rest.repos.get = parameters => (post.mock.calls.length ? read(parameters) : get(parameters));
    const clock = vi.spyOn(Date, "now").mockReturnValue(0);
    const sleepFn = vi.fn(async () => clock.mockReturnValue(deadline));
    try {
      expect(await launchAssignment({ ...options, sleepFn }, fixture.assignment)).toMatchObject({ state: "launch_unresolved", released: false });
      expect(read).toHaveBeenCalledTimes(1);
      expect(sleepFn).toHaveBeenCalledExactlyOnceWith(deadline);
      expect(post).toHaveBeenCalledTimes(1);
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ state: "uncertain", released: false });
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
      expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Release", "Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
    } finally {
      clock.mockRestore();
    }
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

  it.each(["post_response_unconfirmed", "binding_unconfirmed"])("reports %s launch receipts as unsuccessful", async reason => {
    const { fixture, options, post } = setup({ granted: false, count: 1 });
    if (reason === "post_response_unconfirmed") post.mockRejectedValue(new Error("request timed out"));
    else {
      const get = fixture.githubClient.rest.actions.getWorkflowRun;
      fixture.githubClient.rest.actions.getWorkflowRun = async parameters => {
        if (parameters.run_id === "42") throw new Error("binding read timed out");
        return get(parameters);
      };
    }
    const result = await dispatchQueueIntent({
      ...options,
      assignment: null,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "unresolved-is-failure", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ success: false, launches: [expect.objectContaining({ state: "launch_unresolved" })] });
    expect([...fixture.state.dispatches.values()][0]).toMatchObject({ state: "uncertain", reason, released: false });
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

  it("cannot substitute the authenticated source actor for the explicit installed worker principal", async () => {
    const { fixture, options, post } = setup({ count: 1, workerPrincipal: "22" });
    const read = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => {
      const response = await read(parameters);
      return parameters.run_id === "42" ? { ...response, data: { ...response.data, actor: { id: "11" }, triggering_actor: { id: "11" } } } : response;
    };
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "launch_unresolved" });
    const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
    expect(dispatch.profile.principal).toBe("22");
    expect(dispatch.sender).toMatchObject({ principal: "11", run_id: "15", run_attempt: 1 });
    expect(dispatch.run).toBeUndefined();
    expect(dispatch.released).toBe(false);
    expect(post).toHaveBeenCalledTimes(1);
    await launchAssignment(options, fixture.assignment);
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
    let failRelease = true;
    const publish = rejected.options.publishWorkQueueRequest;
    rejected.options.publishWorkQueueRequest = async args => {
      if (failRelease && args.request.kind === "release") {
        failRelease = false;
        throw new Error("injected release publication failure");
      }
      return publish(args);
    };
    await expect(launchAssignment(rejected.options, rejected.fixture.assignment)).rejects.toThrow(/injected release publication failure/);
    expect(rejected.fixture.state.dispatches.get(rejected.fixture.assignment.dispatch_id)).toMatchObject({
      state: "rejected",
      released: false,
      reason: expect.stringMatching(/^github_rejection:/),
    });
    expect(await launchAssignment(rejected.options, rejected.fixture.assignment)).toMatchObject({ state: "rejected", released: true, dispatched: false });
    expect([...rejected.fixture.state.claims.values()].every(claim => claim.state === "cancelled")).toBe(true);
    const before = rejected.fixture.transactions.length;
    expect(await launchAssignment(rejected.options, rejected.fixture.assignment)).toMatchObject({ state: "rejected", released: true, dispatched: false });
    expect(rejected.fixture.transactions).toHaveLength(before);
    expect(rejected.post).toHaveBeenCalledTimes(1);
    const uncertain = setup({ started: true });
    expect((await reconcileDispatch(uncertain.options)).state).toBe("launch_unresolved");
    expect(uncertain.fixture.state.dispatches.get(uncertain.fixture.assignment.dispatch_id).released).toBe(false);
    expect(uncertain.post).not.toHaveBeenCalled();
  });

  it.each(["mismatch", "timeout", "missing_request_id"])("releases a definitive tag setup %s without sending the workflow POST", async failure => {
    const { fixture, options, post } = setup({ count: 1 });
    const getRef = fixture.githubClient.rest.git.getRef;
    fixture.githubClient.rest.git.getRef = vi.fn().mockImplementationOnce(getRef);
    if (failure === "mismatch")
      fixture.githubClient.rest.git.getRef.mockResolvedValue({
        status: 200,
        data: { ref: `refs/tags/gh-aw/work-queue/${fixture.assignment.ref}`, object: { type: "commit", sha: "b".repeat(40) } },
      });
    else fixture.githubClient.rest.git.getRef.mockRejectedValue(failure === "timeout" ? new Error("tag request timed out") : { status: 403, response: { status: 403, headers: {} } });
    expect(await launchAssignment(options, fixture.assignment)).toMatchObject({ state: "rejected", released: true, dispatched: false });
    const marker = fixture.transactions.flatMap(commit => commit.operations).find(operation => operation.kind === "Dispatch" && operation.state === "rejected");
    expect(marker).toMatchObject({ reason: expect.stringMatching(/^tag_prepost:/), evidence: { kind: "nonlaunch", source: "github_api" } });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(true);
    expect([...fixture.state.claims.values()].every(claim => claim.state === "cancelled")).toBe(true);
    expect(post).not.toHaveBeenCalled();
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

  it("uses exact matching Release proof backoff without publication padding even after a delayed append", () => {
    const { fixture } = setup({ bound: true, count: 1 });
    const at = fixture.at;
    const dispatchId = fixture.assignment.dispatch_id;
    const cancellations = cancellationOperations(fixture.state, fixture.assignment, at, "native_run_terminal");
    expect(cancellations[0].retry_not_before).toBe(at + fixture.policy.pools.default.retry.backoff_ms);
    const operations = [
      ...cancellations,
      {
        kind: "Release",
        dispatch_id: dispatchId,
        evidence: {
          kind: "terminal_run",
          source: "github_api",
          repository: REPOSITORY,
          workflow: fixture.binding.workflow,
          ref: REF,
          principal: fixture.binding.principal,
          checked_at: at,
          run_id: "42",
          run_attempt: 1,
          status: "completed",
          conclusion: "failure",
        },
      },
    ];
    const actor = { ...fixture.dispatcher, role: "reconciler" };
    const request = newRequest("delayed-release-request", "release", actor, { operations });
    const commit = { version: 3, id: "delayed-release-commit", previous: fixture.state.tip, request, actor, policy_epoch: fixture.state.policy_epoch, at: at + 10000, operations };
    const result = appendCommit(fixture.transactions, commit);
    expect(result.state.dispatches.get(dispatchId).released).toBe(true);
    expect(result.state.works.get(fixture.assignment.claims[0].work_id).retry_not_before).toBe(at + fixture.policy.pools.default.retry.backoff_ms);
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

  it.each([
    { endpoint: "repository", returnedHint: false, deadline: 1, maxAttempts: 5, reads: 1, sleep: 1 },
    { endpoint: "repository", returnedHint: true, deadline: 1, maxAttempts: 5, reads: 1, sleep: 1 },
    { endpoint: "attempt", returnedHint: false, deadline: 1, maxAttempts: 5, reads: 1, sleep: 1 },
    { endpoint: "attempt", returnedHint: true, deadline: 1, maxAttempts: 5, reads: 1, sleep: 1 },
    { endpoint: "repository", returnedHint: false, deadline: 1000, maxAttempts: 2, reads: 2, sleep: 50 },
    { endpoint: "attempt", returnedHint: false, deadline: 1000, maxAttempts: 2, reads: 2, sleep: 50 },
  ])("keeps $endpoint recovery inside reconciliation budgets with hint=$returnedHint, deadline=$deadline, max_attempts=$maxAttempts", async ({ endpoint, returnedHint, deadline, maxAttempts, reads, sleep }) => {
    const { fixture, options, post } = setup({
      count: 1,
      started: true,
      configurePolicy: policy => {
        policy.pools.default.reconciliation.deadline_ms = deadline;
        policy.pools.default.reconciliation.max_attempts = maxAttempts;
      },
    });
    fixture.githubClient.rest.actions.listWorkflowRuns.mockResolvedValue({ status: 200, data: { workflow_runs: [fixture.nativeRun()] } });
    const read = vi.fn().mockRejectedValue({ status: 503 });
    if (endpoint === "attempt") fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    else {
      const get = fixture.githubClient.rest.repos.get;
      let publisherRead = false;
      fixture.githubClient.rest.repos.get = parameters => {
        if (publisherRead) return read(parameters);
        publisherRead = true;
        return get(parameters);
      };
    }
    const clock = vi.spyOn(Date, "now").mockReturnValue(0);
    const sleepFn = vi.fn(async delay => clock.mockReturnValue(Date.now() + delay));
    try {
      expect(await reconcileDispatch({ ...options, sleepFn, ...(returnedHint ? { returnedRunId: "42" } : {}) })).toMatchObject({ state: "launch_unresolved", released: false });
      expect(read).toHaveBeenCalledTimes(reads);
      expect(sleepFn).toHaveBeenCalledExactlyOnceWith(sleep);
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
      expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Release", "Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
      expect(post).not.toHaveBeenCalled();
    } finally {
      clock.mockRestore();
    }
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
    expect(fixture.state.dispatches.get(dispatchId).lifecycle_writes).toBe(4);
    expect(fixture.state.dispatches.get(dispatchId).lifecycle_writes).toBeLessThanOrEqual(fixture.policy.pools.default.reconciliation.max_attempts + 4);
    expect(post).not.toHaveBeenCalled();
  });

  it("allows definitive prelaunch cancellation only before the marker race is won", async () => {
    const reserved = setup();
    expect((await cancelBeforeLaunch(reserved.options)).released).toBe(true);
    const started = setup({ started: true });
    await expect(cancelBeforeLaunch(started.options)).rejects.toThrow(/may_have_started/);
    expect(started.fixture.state.dispatches.get(started.fixture.assignment.dispatch_id).released).toBe(false);
  });

  it("checks the selected launch credential against the frozen worker before START and never rechecks a fenced launch", async () => {
    const { fixture, options, post } = setup({ count: 1, workerPrincipal: "22" });
    const validateDispatchCredential = vi.fn().mockResolvedValue({ principal: "11" });
    const before = fixture.transactions;
    await expect(launchAssignment({ ...options, validateDispatchCredential }, fixture.assignment)).rejects.toThrow(/credential_principal_mismatch/);
    expect(fixture.transactions).toEqual(before);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).state).toBe("reserved");
    expect(post).not.toHaveBeenCalled();
    validateDispatchCredential.mockImplementation(options.validateDispatchCredential);
    expect(await launchAssignment({ ...options, validateDispatchCredential }, fixture.assignment)).toMatchObject({ state: "bound", dispatched: true, run_id: "42" });
    expect(validateDispatchCredential).toHaveBeenCalledWith(expect.objectContaining({ assignment: fixture.assignment, profile: expect.objectContaining({ principal: "22" }) }));
    expect(validateDispatchCredential).toHaveBeenCalledTimes(2);
    expect(post).toHaveBeenCalledTimes(1);
    await launchAssignment({ ...options, validateDispatchCredential }, fixture.assignment);
    expect(validateDispatchCredential).toHaveBeenCalledTimes(2);
    expect(post).toHaveBeenCalledTimes(1);
  });

  it.each(["missing", "copied", "foreign_client"])("requires private selected-client credential proof before START/POST: %s", async mode => {
    const { fixture, options, post } = setup({ count: 1 });
    const proof = await options.validateDispatchCredential({ profile: fixture.state.dispatches.get(fixture.assignment.dispatch_id).profile, repository: REPOSITORY });
    const configured =
      mode === "missing"
        ? { ...options, validateDispatchCredential: undefined }
        : mode === "copied"
          ? { ...options, validateDispatchCredential: async () => JSON.parse(JSON.stringify(proof)) }
          : { ...options, dispatchClient: { rest: fixture.githubClient.rest } };
    const before = canonical(fixture.transactions);
    await expect(launchAssignment(configured, fixture.assignment)).rejects.toThrow(/credential_(validator_required|proof_invalid)/);
    expect(canonical(fixture.transactions)).toBe(before);
    expect(post).not.toHaveBeenCalled();
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

  it("installs a configured missing pool before dispatch without changing existing Work or the Policy epoch", async () => {
    const { fixture, options, post, proposal } = setupPoolInstallation();
    const beforeWorks = canonical([...fixture.state.works]);
    const beforeEpoch = fixture.state.policy_epoch;
    const result = await dispatchQueueIntent({
      ...options,
      assignment: null,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "missing-pool", pool: "smoke", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ success: true, status: "no_grant", dispatches: 0 });
    expect(fixture.state.policy.pools.smoke).toEqual(proposal.pools.smoke);
    expect(fixture.state.policy.pools.default).toEqual(fixture.policy.pools.default);
    expect(fixture.state.policy_epoch).toBe(beforeEpoch);
    expect(canonical([...fixture.state.works])).toBe(beforeWorks);
    expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "PoolInstall")).toHaveLength(1);
    expect(post).not.toHaveBeenCalled();
  });

  it.each(["budget", "claims", "unknown", "preview", "missing-proposal"])("does not install a pool for %s dispatch", async mode => {
    const { fixture, options, post } = setupPoolInstallation();
    const before = canonical(fixture.transactions);
    const pending = dispatchQueueIntent({
      ...options,
      assignment: null,
      remainingDispatches: mode === "budget" ? 0 : 1,
      ...(mode === "preview" ? { staged: true } : {}),
      ...(mode === "missing-proposal" ? { policyProposal: undefined } : {}),
      message: { type: "work_queue_dispatch_next", intent_id: "invalid-pool-dispatch", pool: mode === "unknown" ? "unknown" : "smoke", max_claims: mode === "claims" ? 0 : 1, max_dispatches: 1 },
    });
    if (mode === "unknown" || mode === "missing-proposal") await expect(pending).rejects.toMatchObject({ code: "work_queue_pool_not_installed" });
    else await expect(pending).rejects.toThrow();
    expect(canonical(fixture.transactions)).toBe(before);
    expect(fixture.githubClient.rest.repos.getContent).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
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

  it("inherits omitted child accounting metadata from authoritative parent Work, not global defaults", async () => {
    const { fixture, options } = setup({
      bound: true,
      count: 1,
      workerPrincipal: "22",
      workDefaults: { priority: 1, fairness_key: "tenant" },
      configurePolicy: policy => {
        policy.accounting_weights.tenant = 1;
        policy.producers["11"].fairness_keys.push("tenant");
      },
    });
    const actor = await authenticatePublisher({
      ...options,
      context: fixture.workerContext,
      workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`,
      role: "worker",
      dispatch_id: fixture.assignment.dispatch_id,
      claim_handle: "h1",
    });
    const requested = { nodes: [{ graph_id: "child", node_key: "n", payload: { plan: "follow-up" } }] };
    expect(() => inheritWorkerSubmission(fixture.state, actor, requested)).toThrow(/claim_effects_unauthorized/);
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    expect(inheritWorkerSubmission(fixture.state, actor, requested).nodes[0]).toMatchObject({ pool: "default", priority: 1, fairness_key: "tenant" });
    const withoutGraph = { nodes: [{ node_key: "inherited-graph-child", payload: { plan: "follow-up" } }] };
    expect(inheritWorkerSubmission(fixture.state, actor, withoutGraph).nodes[0]).toMatchObject({ graph_id: fixture.state.works.get(fixture.assignment.claims[0].work_id).graph_id, pool: "default", priority: 1, fairness_key: "tenant" });
    expect(withoutGraph.nodes[0]).not.toHaveProperty("graph_id");
    expect(requested.nodes[0]).not.toHaveProperty("priority");
    expect(inheritWorkerSubmission(fixture.state, actor, { nodes: [{ ...requested.nodes[0], priority: 5, fairness_key: "foreign" }] }).nodes[0]).toMatchObject({ priority: 5, fairness_key: "foreign" });
    const before = fixture.transactions;
    for (const spoof of [
      { principal: "11" },
      { run_id: "43" },
      { run_attempt: 2 },
      { dispatch_id: "another-dispatch" },
      { claim_handle: "foreign" },
      { ref: "b".repeat(40) },
      { event: "push" },
      { role: "administrator" },
      { principal: "11", logical_origin: actor },
    ]) {
      expect(() => inheritWorkerSubmission(fixture.state, { ...actor, ...spoof }, requested)).toThrow();
    }
    expect(inheritWorkerSubmission(fixture.state, { ...actor, logical_origin: { ...actor, principal: "11" } }, requested)).toEqual(inheritWorkerSubmission(fixture.state, actor, requested));
    expect(fixture.transactions).toEqual(before);
    expect(fixture.policy.producers).not.toHaveProperty("22");
  });

  it("requires fresh active native binding and same-Claim Completion for queue controls", async () => {
    const { fixture, options } = setup({ bound: true, count: 2 });
    const worker = { ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}` };
    await expect(intentContext(worker, { claim_handle: "h1" })).rejects.toThrow(/claim_effects_unauthorized/);
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h2", outcome: "cancelled" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h2" });
    expect(await intentContext(worker, { claim_handle: "h1" })).toMatchObject({ role: "worker", authenticated: true, claim_handle: "h1" });
    await expect(intentContext(worker, { claim_handle: "h2" })).rejects.toThrow(/claim_effects_unauthorized/);
  });

  it("admits bot fanout through completed original Claim capability without granting the bot root-producer entitlement", async () => {
    const { fixture, options, post } = setup({
      bound: true,
      count: 1,
      workerPrincipal: "22",
      workDefaults: { priority: 1, fairness_key: "tenant" },
      configurePolicy: policy => {
        policy.accounting_weights.tenant = 1;
        policy.producers["11"].fairness_keys.push("tenant");
        policy.pools.other = structuredClone(policy.pools.default);
      },
    });
    expect(fixture.policy.producers["22"]).toBeUndefined();
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-bot-fanout-"));
    const filename = path.join(directory, "intents.jsonl");
    const child = { graph_id: "bot-fanout", node_key: "child", payload: { plan: "inherited child", effect_contract: { kind: "none" } } };
    const intents = [
      { node: child, intent_id: "valid-child" },
      { node: { ...child, node_key: "better-priority", priority: 3 }, intent_id: "changed-priority" },
      { node: { ...child, node_key: "new-share", fairness_key: "" }, intent_id: "changed-key" },
      { node: { ...child, node_key: "foreign-pool", pool: "other" }, intent_id: "changed-pool" },
    ];
    try {
      fs.writeFileSync(filename, intents.map(({ node, intent_id }) => JSON.stringify({ version: 3, intent_id, kind: "submit", claim_handle: "h1", parameters: { nodes: [node] } })).join("\n"));
      const result = await processWorkQueueIntents({ ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`, intentPath: filename });
      expect(result.receipts.map(receipt => receipt.status)).toEqual(["durable", "blocked", "blocked", "blocked"]);
      const admitted = fixture.state.works.get(nodeId(child.graph_id, child.node_key));
      expect(admitted).toMatchObject({ pool: "default", priority: 1, fairness_key: "tenant" });
      expect(fixture.transactions.at(-1).actor).toMatchObject({ role: "worker", principal: "22", dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
      const before = fixture.transactions;
      const template = fixture.transactions[1].operations[0];
      expect(() => fixture.append("submit", { nodes: [{ ...template, graph_id: "unscoped-bot", node_key: "root", work_id: nodeId("unscoped-bot", "root") }] }, { role: "producer", principal: "22", repository: REPOSITORY })).toThrow(
        /entitlement/
      );
      expect(fixture.transactions).toEqual(before);
      expect(fixture.state.works.size).toBe(2);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
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

  it.each([false, true])("recovers accepted child submission identities after Release without foreign reads or changed intent semantics (historical graph: %s)", async historicalGraph => {
    const { fixture, options, post } = setup({ bound: true, count: 1, configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design") });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-worker-ack-"));
    const filename = path.join(directory, "intents.jsonl");
    const intent = {
      version: 3,
      intent_id: "accepted-worker-submit",
      kind: "submit",
      claim_handle: "h1",
      parameters: {
        nodes: [
          {
            ...(historicalGraph ? { graph_id: digest({ plan: "original child" }) } : {}),
            node_key: "n",
            payload: { plan: "original child" },
            depends_on: [{ ...edge, resource: { kind: "issue", host: "github.com", repository: "foreign/design", number: "7" } }],
          },
        ],
      },
    };
    const resolver = resolverFor(fixture);
    const worker = { ...options, context: fixture.workerContext, workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`, intentPath: filename, dependencyResolver: resolver };
    try {
      fs.writeFileSync(filename, `${JSON.stringify(intent)}\n`);
      const original = await processWorkQueueIntents(worker);
      expect(original.receipts[0].status).toBe("durable");
      expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(1);
      const retryIntent = { ...intent, parameters: { nodes: intent.parameters.nodes.map(({ graph_id, ...node }) => node) } };
      fs.writeFileSync(filename, `${JSON.stringify(retryIntent)}\n`);
      releaseCompletedParent(fixture);
      const before = fixture.transactions;
      const recovered = await processWorkQueueIntents({ ...worker, dependencyResolver: undefined });
      expect(recovered.receipts[0]).toEqual(original.receipts[0]);
      fs.writeFileSync(filename, `${JSON.stringify({ ...retryIntent, parameters: { nodes: [{ ...retryIntent.parameters.nodes[0], payload: { plan: "changed child" } }] } })}\n`);
      expect((await processWorkQueueIntents(worker)).receipts[0].status).toBe("blocked");
      fs.writeFileSync(filename, `${JSON.stringify({ ...retryIntent, intent_id: "new-after-release" })}\n`);
      expect((await processWorkQueueIntents(worker)).receipts[0].status).toBe("blocked");
      expect(fixture.transactions).toEqual(before);
      expect(resolver.client.rest.issues.get).toHaveBeenCalledTimes(1);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("reuses default roots across authenticated native origins and preserves explicit distinct nodes", async () => {
    const { fixture, options, post } = setup({ granted: false, count: 1 });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-independent-root-"));
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
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("publishes valid sibling controls despite malformed JSON, invalid selector or foreign Claim intents", async () => {
    const { fixture, options, post } = setup({ bound: true, count: 2 });
    for (const claim of fixture.assignment.claims)
      fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: claim.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: claim.handle });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-worker-siblings-"));
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
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("normalizes every worker preview selector without publishing Completion or queue controls", async () => {
    const { fixture, options, post } = setup({ count: 2, bound: true });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-worker-preview-"));
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
      expect(result.receipts.find(receipt => receipt.status === "staged_preview").claim_handle).toBe("h2");
      expect(result.receipts.filter(receipt => receipt.status === "blocked")).toHaveLength(3);
      expect(read).toHaveBeenCalledTimes(1);
      expect(publish).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
      const message = { type: "work_queue_dispatch_next", intent_id: "preview-dispatch", pool: "default", max_claims: 1, max_dispatches: 1 };
      await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message })).rejects.toMatchObject({ code: "claim_scope_required" });
      await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { ...message, claim_handle: "h99" } })).rejects.toThrow(/foreign/);
      await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { ...message, claim_handle: null } })).rejects.toThrow();
      expect(await dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { ...message, claim_handle: "h2" } })).toMatchObject({ staged: true, dispatches: 0, claim_handle: "h2" });
      const previewDispatch = { version: 3, kind: "dispatch_next", claim_handle: "h2", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
      fs.writeFileSync(
        filename,
        [
          JSON.stringify({ ...previewDispatch, intent_id: "valid-dispatch" }),
          JSON.stringify({ ...previewDispatch, intent_id: "foreign-pool", parameters: { ...previewDispatch.parameters, pool: "other" } }),
          JSON.stringify({ ...previewDispatch, intent_id: "invalid-bound", parameters: { ...previewDispatch.parameters, max_claims: 0 } }),
        ].join("\n")
      );
      const dispatchPreviews = await processWorkQueueIntents(worker);
      expect(dispatchPreviews.receipts.filter(receipt => receipt.status === "staged_preview")).toEqual([expect.objectContaining({ intent_id: "valid-dispatch", claim_handle: "h2", dispatches: 0 })]);
      expect(dispatchPreviews.receipts.filter(receipt => receipt.status === "blocked")).toHaveLength(2);
      expect(publish).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
      expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each(["absent", "empty"])("fails an unassigned dispatcher with %s intents instead of reporting a successful activation", async state => {
    const { options, post } = setup({ granted: false });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-missing-intents-"));
    const filename = path.join(directory, "intents.jsonl");
    const core = { info: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() };
    const publish = vi.fn(options.publishWorkQueueRequest);
    try {
      if (state === "empty") fs.writeFileSync(filename, "");
      const direct = await processWorkQueueIntents({ ...options, assignment: null, role: "dispatcher", intentPath: filename, publishWorkQueueRequest: publish });
      expect(direct.receipts).toEqual([{ reason: "work_queue_dispatcher_intents_missing", status: "blocked" }]);
      const result = await publishQueueControls({ ...options, assignment: null, role: "dispatcher", intentPath: filename, publishWorkQueueRequest: publish, core });
      expect(result).toMatchObject({ status: "recovery_required", success: false });
      expect(result.receipts).toEqual([{ reason: "work_queue_dispatcher_intents_missing", status: "blocked" }]);
      expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("produced no control intents"));
      expect(publish).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("rejects dispatcher intents appended to a trusted prepared plan before admission", async () => {
    const { options, post } = setup({ granted: false });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-prepared-plan-"));
    const filename = path.join(directory, "intents.jsonl");
    const publish = vi.fn(options.publishWorkQueueRequest);
    const plan = {
      version: 1,
      nodes: [{ pool: "default", payload: { task: "prepared" } }],
      dispatch: { pool: "default", max_claims: 1, max_dispatches: 1 },
    };
    try {
      stagePlanIntents(filename, plan);
      fs.appendFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "agent-extra", kind: "dispatch_next", parameters: plan.dispatch })}\n`);
      const result = await processWorkQueueIntents({ ...options, assignment: null, role: "dispatcher", intentPath: filename, publishWorkQueueRequest: publish });
      expect(result.receipts).toEqual([{ reason: "work_queue_plan_intent_batch_invalid", status: "blocked" }]);
      expect(publish).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("auto-installs a compiler-approved pool without changing installed routes or the Policy epoch", async () => {
    const { fixture, options } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
      },
    });
    const beforePolicy = structuredClone(fixture.state.policy);
    const beforeWorks = canonical([...fixture.state.works]);
    const beforeEpoch = fixture.state.policy_epoch;
    const proposal = structuredClone(fixture.policy);
    proposal.pools.smoke = structuredClone(proposal.pools.default);
    const workerContents = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${"a".repeat(64)}"\n`;
    fixture.githubClient.rest.repos.getContent = vi.fn(async ({ path: workflowPath }) => ({
      data: { type: "file", path: workflowPath, encoding: "base64", content: Buffer.from(workerContents).toString("base64") },
    }));
    fixture.githubClient.rest.actions.getWorkflow = vi.fn(async () => ({ data: { path: ".github/workflows/worker.lock.yml", state: "active" } }));
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };

    await installWorkQueuePools({ ...options, policyProposal: proposal }, trusted, ["smoke"]);

    expect(fixture.state.policy.pools.smoke).toEqual(proposal.pools.smoke);
    expect(fixture.state.policy.pools.default).toEqual(beforePolicy.pools.default);
    expect(fixture.state.policy_epoch).toBe(beforeEpoch);
    expect(canonical([...fixture.state.works])).toBe(beforeWorks);
    expect(fixture.transactions.at(-1).operations).toEqual([{ kind: "PoolInstall", pool: "smoke", config: proposal.pools.smoke }]);
  });

  it.each(["installed", "later-ref", "later-ref-and-contract"])("resolves proposal %s against repository-wide installed authority", async revision => {
    const { fixture, options } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
        policy.pools.default.profiles.default.workflow = ".github/workflows/unrelated.lock.yml";
      },
    });
    const original = structuredClone(fixture.state.policy.pools.default);
    const proposal = structuredClone(fixture.policy);
    if (revision !== "installed") proposal.pools.default.profiles.default.ref = "b".repeat(40);
    if (revision === "later-ref-and-contract") proposal.pools.default.profiles.default.logical_contract = "b".repeat(64);
    proposal.pools.smoke = structuredClone(proposal.pools.default);
    const workerContents = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${"a".repeat(64)}"\n`;
    fixture.githubClient.rest.repos.getContent = vi.fn(async ({ path: workflowPath }) => ({
      data: { type: "file", path: workflowPath, encoding: "base64", content: Buffer.from(workerContents).toString("base64") },
    }));
    fixture.githubClient.rest.actions.getWorkflow = vi.fn(async () => ({ data: { path: original.profiles.default.workflow, state: "active" } }));
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };
    const configured = { ...options, policyProposal: proposal, config: { work_queue_workflows: ["caller-local"], aw_context_workflows: ["caller-local"] } };
    await installWorkQueuePools(configured, trusted, ["smoke"]);
    expect(fixture.state.policy.pools.default).toEqual(original);
    expect(fixture.state.policy.pools.smoke).toEqual(original);
    expect(fixture.githubClient.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ ref: original.profiles.default.ref }));
    const count = fixture.transactions.length;
    await installWorkQueuePools(configured, trusted, ["smoke"]);
    expect(fixture.transactions).toHaveLength(count);
    if (revision === "later-ref-and-contract") {
      fixture.githubClient.rest.repos.get = vi.fn(async () => ({ data: { full_name: REPOSITORY, default_branch: "main" } }));
      fixture.githubClient.rest.git.getRef = vi.fn(async () => ({ data: { ref: "refs/heads/main", object: { sha: "b".repeat(40) } } }));
      fixture.githubClient.rest.repos.getContent = vi.fn(async ({ path: workflowPath }) => {
        const contents = workflowPath.endsWith(".md") ? "---\ntools:\n  work-queue:\n    worker: true\n---\nApproved worker.\n" : workerContents.replace("a".repeat(64), "b".repeat(64));
        return { data: { type: "file", path: workflowPath, encoding: "base64", content: Buffer.from(contents).toString("base64") } };
      });
      await synchronizeDeployments({ ...configured, config: { work_queue_workflows: ["unrelated"], aw_context_workflows: ["unrelated"] } }, trusted, ["smoke"]);
      expect(fixture.state.deployments.get("smoke").get("default")).toMatchObject({ current_ref: "b".repeat(40), current_contract: "b".repeat(64) });
      expect(fixture.state.deployments.get("default").get("default")).toMatchObject({ current_ref: original.profiles.default.ref, current_contract: original.profiles.default.logical_contract });
      expect(fixture.state.policy.pools.smoke).toEqual(original);
    }
  });

  it("accepts an exact same-name registered revision without changing existing deployments", async () => {
    const { fixture, options } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
      },
    });
    const profile = { ...fixture.policy.pools.default.profiles.default, ref: "b".repeat(40), logical_contract: "b".repeat(64) };
    fixture.append(
      "deployment",
      { operations: [{ kind: "Deployment", pool: "default", worker_profile: "default", expected_ref: REF, expected_contract: "a".repeat(64), profile, available: true, reason: "compiler_deployment" }] },
      fixture.dispatcher
    );
    const before = canonical(serializeProjection(fixture.state).deployments.default);
    const config = structuredClone(fixture.policy.pools.default);
    config.profiles.default = profile;
    fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: "deployed-copy", config }] }, fixture.dispatcher);
    expect(fixture.state.policy.pools["deployed-copy"].profiles.default).toEqual(profile);
    expect(canonical(serializeProjection(fixture.state).deployments.default)).toBe(before);
    config.profiles = { renamed: profile };
    config.default_profile = "renamed";
    expect(() => fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: "renamed-copy", config }] }, fixture.dispatcher)).toThrow(/registered worker revision authority/);
    const proposal = structuredClone(fixture.state.policy);
    proposal.pools["helper-copy"] = structuredClone(proposal.pools["deployed-copy"]);
    for (const pool of Object.values(proposal.pools)) {
      pool.profiles.default.ref = "c".repeat(40);
      pool.profiles.default.logical_contract = "c".repeat(64);
    }
    const contents = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${"b".repeat(64)}"\n`;
    fixture.githubClient.rest.repos.getContent = vi.fn(async ({ path: workflowPath }) => ({
      data: { type: "file", path: workflowPath, encoding: "base64", content: Buffer.from(contents).toString("base64") },
    }));
    fixture.githubClient.rest.actions.getWorkflow = vi.fn(async () => ({ data: { path: profile.workflow, state: "active" } }));
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };
    await installWorkQueuePools({ ...options, policyProposal: proposal }, trusted, ["helper-copy"]);
    expect(fixture.state.policy.pools["helper-copy"].profiles.default).toEqual(profile);
    expect(fixture.githubClient.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ ref: profile.ref }));
    expect(canonical(serializeProjection(fixture.state).deployments.default)).toBe(before);
  });

  it.each([false, true])("does not revive an unavailable installed or deployed worker (new ref: %s)", async newRef => {
    const { fixture, options } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
      },
    });
    const original = structuredClone(fixture.policy.pools.default.profiles.default);
    const profile = { ...original, ...(newRef ? { ref: "b".repeat(40) } : {}) };
    fixture.append(
      "deployment",
      { operations: [{ kind: "Deployment", pool: "default", worker_profile: "default", expected_ref: original.ref, expected_contract: original.logical_contract, profile, available: false, reason: "worker_unavailable" }] },
      fixture.dispatcher
    );
    const config = structuredClone(fixture.policy.pools.default);
    config.profiles.default = profile;
    const before = canonical(serializeProjection(fixture.state));
    expect(() => fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: "unavailable-copy", config }] }, fixture.dispatcher)).toThrow(/registered worker revision authority/);
    const proposal = structuredClone(fixture.state.policy);
    proposal.pools["unavailable-copy"] = config;
    // Prevent the adapter from selecting the still-available original revision.
    if (newRef) {
      fixture.append(
        "deployment",
        {
          operations: [
            {
              kind: "Deployment",
              pool: "default",
              worker_profile: "default",
              expected_ref: profile.ref,
              expected_contract: profile.logical_contract,
              profile: original,
              available: false,
              activate: false,
              reason: "worker_unavailable",
            },
          ],
        },
        fixture.dispatcher
      );
    }
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };
    await expect(installWorkQueuePools({ ...options, policyProposal: proposal }, trusted, ["unavailable-copy"])).rejects.toMatchObject({ code: "work_queue_worker_not_compiler_approved" });
    if (!newRef) expect(canonical(serializeProjection(fixture.state))).toBe(before);
    expect(Object.hasOwn(fixture.state.policy.pools, "unavailable-copy")).toBe(false);
  });

  it.each([false, true])("rechecks concurrent pool installation after CAS refresh (different: %s)", async different => {
    const { fixture, options } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
      },
    });
    const proposal = structuredClone(fixture.policy);
    proposal.pools.smoke = structuredClone(proposal.pools.default);
    const contents = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n";
    fixture.githubClient.rest.repos.getContent = vi.fn(async ({ path: workflowPath }) => ({
      data: { type: "file", path: workflowPath, encoding: "base64", content: Buffer.from(contents).toString("base64") },
    }));
    fixture.githubClient.rest.actions.getWorkflow = vi.fn(async () => ({ data: { path: ".github/workflows/worker.lock.yml", state: "active" } }));
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };
    const count = fixture.transactions.length;
    const publish = async args => {
      const config = structuredClone(proposal.pools.smoke);
      if (different) config.logical_limit += 1;
      fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: "smoke", config }] }, fixture.dispatcher);
      return fixture.publishWorkQueueRequest(args);
    };
    const pending = installWorkQueuePools({ ...options, policyProposal: proposal, publishWorkQueueRequest: publish }, trusted, ["smoke"]);
    if (different) await expect(pending).rejects.toMatchObject({ code: "work_queue_policy_proposal_mismatch" });
    else await pending;
    expect(fixture.transactions).toHaveLength(count + 1);
  });

  it.each([
    ["route", profile => (profile.workflow = ".github/workflows/unapproved.lock.yml")],
    ["ref", profile => (profile.ref = "b".repeat(40))],
    ["contract", profile => (profile.logical_contract = "b".repeat(64))],
    ["credential", profile => (profile.credential_scope = "unapproved")],
    ["trust", profile => (profile.trust_domain = "unapproved")],
    ["claims", profile => (profile.max_claims += 1)],
    ["sharing", profile => (profile.share_keys = !profile.share_keys)],
  ])("rejects raw PoolInstall changed %s authority", (_name, mutate) => {
    const { fixture } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
      },
    });
    const before = canonical(serializeProjection(fixture.state));
    const pool = structuredClone(fixture.policy.pools.default);
    mutate(pool.profiles.default);
    expect(() => fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: "unapproved", config: pool }] }, fixture.dispatcher)).toThrow();
    expect(canonical(serializeProjection(fixture.state))).toBe(before);
  });

  it("rejects raw PoolInstall that expands the installed repository allowlist", () => {
    const { fixture } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
      },
    });
    const before = canonical(serializeProjection(fixture.state));
    const pool = structuredClone(fixture.policy.pools.default);
    pool.allowed_repositories.push("foreign/repo");

    expect(() => fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: "expanded", config: pool }] }, fixture.dispatcher)).toThrow(/repository allowlist/);
    expect(canonical(serializeProjection(fixture.state))).toBe(before);
  });

  it.each(["__proto__", "constructor", "prototype", "", "é".repeat(129)])("rejects unsafe automatic pool identity %s", async name => {
    const { fixture, options } = setup({ granted: false });
    const before = canonical(fixture.transactions);
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };
    await expect(installWorkQueuePools(options, trusted, [name])).rejects.toThrow();
    expect(() => fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: name, config: fixture.policy.pools.default }] }, fixture.dispatcher)).toThrow();
    expect(canonical(fixture.transactions)).toBe(before);
  });

  it.each(["é".repeat(128), "x".repeat(256)])("installs bounded UTF-8 pool identity %s with a stable bounded request", async name => {
    const { fixture, options } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
      },
    });
    const proposal = structuredClone(fixture.policy);
    proposal.pools[name] = structuredClone(proposal.pools.default);
    const contents = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n";
    fixture.githubClient.rest.repos.getContent = vi.fn(async ({ path: workflowPath }) => ({
      data: { type: "file", path: workflowPath, encoding: "base64", content: Buffer.from(contents).toString("base64") },
    }));
    fixture.githubClient.rest.actions.getWorkflow = vi.fn(async () => ({ data: { path: ".github/workflows/worker.lock.yml", state: "active" } }));
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };
    await installWorkQueuePools({ ...options, policyProposal: proposal }, trusted, [name]);
    expect(fixture.state.policy.pools[name]).toEqual(proposal.pools.default);
    expect(Buffer.byteLength(fixture.transactions.at(-1).request.id)).toBeLessThanOrEqual(256);
    const count = fixture.transactions.length;
    await installWorkQueuePools({ ...options, policyProposal: proposal }, trusted, [name]);
    expect(fixture.transactions).toHaveLength(count);
  });
  it("installs a configured pool on first submission and recovers the accepted request without reinstalling", async () => {
    const { fixture, options, proposal } = setupPoolInstallation();
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-first-use-pool-"));
    const filename = path.join(directory, "intents.jsonl");
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "first-smoke-use", kind: "submit", parameters: { nodes: [{ pool: "smoke", payload: { task: "approved" } }] } })}\n`);
      const beforeWorks = canonical([...fixture.state.works]);
      const beforeEpoch = fixture.state.policy_epoch;
      const publish = vi.fn(options.publishWorkQueueRequest);
      const result = await processWorkQueueIntents({ ...options, assignment: null, role: "dispatcher", policyProposal: proposal, intentPath: filename, publishWorkQueueRequest: publish });

      expect(result.receipts).toMatchObject([{ status: "durable" }]);
      expect(fixture.state.policy.pools.smoke).toEqual(proposal.pools.smoke);
      expect(fixture.state.policy.pools.default).toEqual(fixture.policy.pools.default);
      expect(fixture.state.policy_epoch).toBe(beforeEpoch);
      expect(canonical([...fixture.state.works].filter(([, work]) => work.pool === "default"))).toBe(beforeWorks);
      expect([...fixture.state.works.values()].filter(work => work.pool === "smoke")).toHaveLength(1);
      expect(publish.mock.calls.map(([args]) => args.request.kind)).toEqual(["pool_install", "submit"]);
      const beforeRecovery = canonical(fixture.transactions);
      expect((await processWorkQueueIntents({ ...options, assignment: null, role: "dispatcher", intentPath: filename })).receipts).toMatchObject([{ status: "durable", request_id: result.receipts[0].request_id }]);
      expect(canonical(fixture.transactions)).toBe(beforeRecovery);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each([false, true])("installs the pool, admits prepared Work and binds one worker launch end to end (new worker: %s)", async newWorker => {
    const { fixture, options, post } = setupPoolInstallation(newWorker);
    const getRun = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => {
      const response = await getRun(parameters);
      const dispatch = [...fixture.state.dispatches.values()].at(-1);
      return parameters.run_id === "42" ? { ...response, data: { ...response.data, path: dispatch.profile.workflow, display_title: `gh-aw work-queue ${dispatch.dispatch_id}` } } : response;
    };
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-pool-plan-"));
    const filename = path.join(directory, "intents.jsonl");
    try {
      stagePlanIntents(filename, {
        version: 1,
        nodes: [{ pool: "smoke", worker_profile: newWorker ? "smoke-work-queue-worker" : "default", payload: { task: "synthetic smoke", effect_contract: { kind: "none" } } }],
        dispatch: { pool: "smoke", max_claims: 1, max_dispatches: 1 },
      });
      const configured = { ...options, assignment: null, role: "dispatcher", intentPath: filename, maxDispatches: 1 };
      const result = await processWorkQueueIntents(configured);
      expect(result.receipts).toMatchObject([{ status: "durable" }, { status: "durable", success: true, dispatches: 1, launches: [{ state: "bound", run_id: "42", dispatched: true }] }]);
      expect(result.remaining_dispatches).toBe(0);
      expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "PoolInstall")).toHaveLength(1);
      expect([...fixture.state.works.values()].find(work => work.pool === "smoke")).toMatchObject({ state: "claimed", attempts: 1 });
      expect(post).toHaveBeenCalledTimes(1);
      if (newWorker) {
        expect(fixture.state.policy.pools.smoke.profiles["smoke-work-queue-worker"]).toMatchObject({ ref: REF, max_claims: 1 });
        expect(fixture.transactions.flatMap(commit => commit.operations).find(operation => operation.kind === "PoolInstall")).toHaveProperty("config_ref", REF);
        expect([...fixture.state.dispatches.values()].at(-1).worker_profile).toBe("smoke-work-queue-worker");
        expect(fixture.state.policy.pools.default).toEqual(fixture.policy.pools.default);
      }
      const beforeRecovery = canonical(fixture.transactions);
      expect((await processWorkQueueIntents(configured)).receipts).toMatchObject([{ status: "durable" }, { status: "durable", success: true, dispatches: 1 }]);
      expect(canonical(fixture.transactions)).toBe(beforeRecovery);
      expect(post).toHaveBeenCalledTimes(1);
      const assignment = JSON.parse(post.mock.calls[0][0].inputs.work_queue_assignment);
      const dispatch = fixture.state.dispatches.get(assignment.dispatch_id);
      const finishIntentPath = path.join(directory, "finish.jsonl");
      fs.writeFileSync(finishIntentPath, `${JSON.stringify({ version: 3, kind: "finish", intent_id: "smoke-finished", claim_handle: "h1", parameters: { outcome: "completed" } })}\n`);
      const workerOptions = {
        ...options,
        config: undefined,
        assignment,
        logicalContract: dispatch.profile.logical_contract,
        context: fixture.workerContext,
        workflowRef: `${REPOSITORY}/${dispatch.profile.workflow}@${REF}`,
        finishIntentPath,
      };
      expect((await reconcileWorkerClaim(workerOptions)).claims.h1).toMatchObject({ state: "completed", authorized: true });
      expect((await finalizeWorkerResults({ ...workerOptions, verifyEffects: noWriteClaimVerifier(workerOptions) })).claims.h1).toMatchObject({ state: "result" });
      expect(fixture.state.works.get(assignment.claims[0].work_id)).toMatchObject({ state: "completed", barrier: "verified" });
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("registers a new worker in an existing pool on submission without resetting a deployed worker or active Claims", async () => {
    const { fixture, options, proposal } = setupPoolInstallation(true);
    delete proposal.pools.smoke;
    options.config.work_queue_workflows = ["smoke-work-queue-worker"];
    options.config.aw_context_workflows = ["smoke-work-queue-worker"];
    fixture.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 }, fixture.dispatcher, "existing-claim");
    const original = fixture.state.deployments.get("default").get("default");
    fixture.append(
      "deployment",
      { operations: [{ kind: "Deployment", pool: "default", worker_profile: "default", expected_ref: REF, expected_contract: "a".repeat(64), profile: original.revisions[REF].profile, available: false, reason: "worker_unavailable" }] },
      fixture.dispatcher
    );
    const before = fixture.state;
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-new-worker-"));
    const filename = path.join(directory, "intents.jsonl");
    try {
      fs.writeFileSync(
        filename,
        `${JSON.stringify({ version: 3, intent_id: "new-worker-submit", kind: "submit", parameters: { nodes: [{ pool: "default", worker_profile: "smoke-work-queue-worker", payload: { task: "new worker" } }] } })}\n`
      );
      const result = await processWorkQueueIntents({ ...options, assignment: null, role: "dispatcher", intentPath: filename });
      expect(result.receipts).toMatchObject([{ status: "durable" }]);
      expect(fixture.state.policy_epoch).toBe(before.policy_epoch);
      expect(fixture.state.clocks).toEqual(before.clocks);
      expect(fixture.state.claims).toEqual(before.claims);
      expect(fixture.state.dispatches).toEqual(before.dispatches);
      expect(fixture.state.deployments.get("default").get("default")).toEqual(before.deployments.get("default").get("default"));
      expect(fixture.state.deployments.get("default").get("smoke-work-queue-worker")).toMatchObject({ current_ref: REF, current_contract: "a".repeat(64) });
      expect([...fixture.state.works.values()].filter(work => work.worker_profile === "smoke-work-queue-worker")).toHaveLength(1);
      const checkpoint = compactTransactions(fixture.transactions, "c".repeat(40), fixture.administrator, fixture.at);
      const restored = replayTransactions(compactTransactions(checkpoint, "d".repeat(40), fixture.administrator, fixture.at));
      expect(restored.policy).toEqual(fixture.state.policy);
      expect(restored.deployments).toEqual(fixture.state.deployments);
      expect(restored.works).toEqual(fixture.state.works);
      const beforeRecovery = canonical(fixture.transactions);
      expect((await processWorkQueueIntents({ ...options, assignment: null, role: "dispatcher", intentPath: filename })).receipts).toMatchObject([{ status: "durable" }]);
      expect(canonical(fixture.transactions)).toBe(beforeRecovery);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each(["source", "contract", "registration", "default-ref", "batch", "effect-scope", "principal"])("rejects a new worker when %s cannot be verified without publishing partial installation", async failure => {
    const { fixture, options, proposal } = setupPoolInstallation(true);
    const profile = proposal.pools.smoke.profiles["smoke-work-queue-worker"];
    if (failure === "source" || failure === "contract") {
      const getContent = fixture.githubClient.rest.repos.getContent;
      fixture.githubClient.rest.repos.getContent = vi.fn(async args => {
        const response = await getContent(args);
        if (args.path.includes("smoke-work-queue-worker") && args.path.endsWith(failure === "source" ? ".md" : ".yml"))
          response.data.content = Buffer.from(failure === "source" ? "---\ntools: {}\n---\nNot a worker." : "on: push\n").toString("base64");
        return response;
      });
    }
    if (failure === "registration") fixture.githubClient.rest.actions.getWorkflow.mockResolvedValue({ data: { path: profile.workflow, state: "disabled_manually" } });
    if (failure === "default-ref") fixture.githubClient.rest.git.getRef = async () => ({ data: { ref: "refs/heads/main", object: { sha: "main" } } });
    if (failure === "batch") profile.max_claims = 2;
    if (failure === "effect-scope") profile.effect_scope = "foreign/repo";
    if (failure === "principal") profile.principal = "22";
    const before = canonical(fixture.transactions);
    await expect(installWorkQueuePools(options, { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] }, ["smoke"])).rejects.toThrow();
    expect(canonical(fixture.transactions)).toBe(before);
  });

  it.each(["preview", "policy", "route", "verification", "missing"])("does not install or admit a missing pool when %s prevents first use", async mode => {
    const { fixture, options, proposal, post } = setupPoolInstallation();
    if (mode === "policy") proposal.pools.default.logical_limit += 1;
    if (mode === "route") proposal.pools.smoke.profiles.default.workflow = ".github/workflows/unapproved.lock.yml";
    if (mode === "verification") fixture.githubClient.rest.repos.getContent.mockRejectedValue(Object.assign(new Error("unavailable"), { status: 404 }));
    if (mode === "missing") delete proposal.pools.smoke;
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-pool-rejection-"));
    const filename = path.join(directory, "intents.jsonl");
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "blocked-first-use", kind: "submit", parameters: { nodes: [{ pool: "smoke", payload: {} }] } })}\n`);
      const before = canonical(fixture.transactions);
      const result = await processWorkQueueIntents({ ...options, assignment: null, role: "dispatcher", intentPath: filename, ...(mode === "preview" ? { staged: true } : {}) });
      expect(result.receipts).toMatchObject([{ status: mode === "preview" ? "staged_preview" : "blocked" }]);
      expect(canonical(fixture.transactions)).toBe(before);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("does not allow an assigned worker to install a missing pool", async () => {
    const { fixture, options, post } = setup({
      bound: true,
      count: 1,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
      },
    });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const proposal = structuredClone(fixture.policy);
    proposal.pools.smoke = structuredClone(proposal.pools.default);
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-worker-pool-"));
    const filename = path.join(directory, "intents.jsonl");
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "worker-pool", kind: "submit", parameters: { nodes: [{ pool: "smoke", payload: {} }] } })}\n`);
      const before = canonical(fixture.transactions);
      const result = await processWorkQueueIntents({
        ...options,
        policyProposal: proposal,
        context: fixture.workerContext,
        workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`,
        intentPath: filename,
      });
      expect(result.receipts).toMatchObject([{ status: "blocked", reason: "work_queue_pool_not_installed" }]);
      expect(canonical(fixture.transactions)).toBe(before);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("does not auto-install a pool from a proposal that also changes installed Policy", async () => {
    const { fixture, options } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
      },
    });
    const proposal = structuredClone(fixture.policy);
    proposal.pools.smoke = structuredClone(proposal.pools.default);
    proposal.pools.default.logical_limit += 1;
    const trusted = { ...fixture.dispatcher, authenticated: true, roles: ["dispatcher"] };
    const before = canonical(fixture.transactions);

    await expect(installWorkQueuePools({ ...options, policyProposal: proposal }, trusted, ["smoke"])).rejects.toMatchObject({ code: "work_queue_policy_proposal_mismatch" });

    expect(canonical(fixture.transactions)).toBe(before);
    expect(fixture.state.policy.pools).not.toHaveProperty("smoke");
  });

  it("rejects automatic AW pool installation with a foreign effect scope", () => {
    const { fixture } = setup({
      granted: false,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "a".repeat(64);
      },
    });
    const pool = structuredClone(fixture.policy.pools.default);
    pool.profiles.default.effect_scope = "other/repo";
    const before = canonical(fixture.transactions);

    expect(() => fixture.append("pool_install", { operations: [{ kind: "PoolInstall", pool: "foreign", config: pool }] }, fixture.dispatcher)).toThrow(/repository-scoped authority/);

    expect(canonical(fixture.transactions)).toBe(before);
    expect(fixture.state.policy.pools).not.toHaveProperty("foreign");
  });

  it.each(["checkpoint_invalid", "secret-error-code"])("reports checkpoint corruption without leaking arbitrary error codes or messages: %s", async code => {
    const { options, post } = setup({ granted: false });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-checkpoint-error-"));
    const filename = path.join(directory, "intents.jsonl");
    try {
      stagePlanIntents(filename, { version: 1, nodes: [{ pool: "default", payload: { task: "prepared" } }], dispatch: { pool: "default", max_claims: 1, max_dispatches: 1 } });
      const result = await processWorkQueueIntents({
        ...options,
        assignment: null,
        role: "dispatcher",
        intentPath: filename,
        readWorkQueueLog: async () => {
          throw Object.assign(new Error("secret error detail"), { code });
        },
      });
      expect(result.receipts).toHaveLength(2);
      expect(result.receipts.every(receipt => receipt.status === "blocked" && receipt.reason === (code === "checkpoint_invalid" ? "queue_control_checkpoint_invalid" : "queue_control_authority_or_publication_failed"))).toBe(true);
      expect(JSON.stringify(result)).not.toContain("secret");
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each([undefined, null, { kind: "unsupported" }, { kind: "github_app" }])("rejects public dispatch credential metadata %j before constructing a client or publishing", async credential => {
    const { fixture, options, post } = setup({ granted: false });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-public-credential-"));
    const filename = path.join(directory, "intents.jsonl");
    const publish = vi.fn(options.publishWorkQueueRequest);
    const read = vi.fn(options.readWorkQueueLog);
    const config = { ...options.config, work_queue_dispatch_credential: credential };
    const tokenAccess = vi.fn(() => {
      throw new Error("invalid protected metadata consumed a credential");
    });
    Object.defineProperty(config, "github-token", { get: tokenAccess });
    const core = {
      info: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
    };
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "missing-protected-binding", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
      const result = await publishQueueControls({
        ...options,
        config,
        intentPath: filename,
        dispatchClient: undefined,
        validateDispatchCredential: undefined,
        publishWorkQueueRequest: publish,
        readWorkQueueLog: read,
        core,
      });
      expect(result.receipts).toEqual([expect.objectContaining({ status: "blocked" })]);
      expect(result).toMatchObject({ status: "recovery_required", success: false });
      expect(core.setOutput).toHaveBeenCalledWith("work_queue_controls_status", "recovery_required");
      expect(core.setFailed).toHaveBeenCalled();
      expect(core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Blocked intents: 1"));
      expect(tokenAccess).not.toHaveBeenCalled();
      expect(read).not.toHaveBeenCalled();
      expect(publish).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
      expect(fixture.state.claims.size).toBe(0);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each(["uncertain", "rejected"])("fails the controls step for a %s launch without retrying the POST", async outcome => {
    const { fixture, options, post } = setup({ granted: false, count: 1 });
    post.mockRejectedValue(outcome === "rejected" ? { status: 422, response: { status: 422, headers: { "x-github-request-id": "trusted-provider-request" } } } : new Error("request timed out"));
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-control-unresolved-"));
    const filename = path.join(directory, "intents.jsonl");
    const core = {
      info: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
    };
    const config = { ...options.config, work_queue_dispatch_credential: { kind: "authenticated" }, work_queue_enabled: true };
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "uncertain-launch", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
      const result = await publishQueueControls({ ...options, config, intentPath: filename, core });
      expect(result).toMatchObject({ status: "recovery_required", success: false });
      expect(result.receipts[0]).toMatchObject({ status: "durable", launches: [expect.objectContaining({ state: outcome === "rejected" ? "rejected" : "launch_unresolved" })] });
      expect(core.setOutput).toHaveBeenCalledWith("work_queue_controls_status", "recovery_required");
      expect(core.setFailed).toHaveBeenCalled();
      expect(core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(outcome === "rejected" ? "Rejected launches: 1" : "Unresolved launches: 1"));
      expect(post).toHaveBeenCalledTimes(1);
      expect(fixture.state.dispatches.values().next().value).toMatchObject({ released: outcome === "rejected" });
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("reports an exhausted submit metadata read while independently launching and binding older Work", async () => {
    const contract = "a".repeat(64);
    const { fixture, options, post } = setup({
      granted: false,
      count: 3,
      batch: 1,
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = contract;
      },
    });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-mixed-metadata-"));
    const filename = path.join(directory, "intents.jsonl");
    const requestId = "1B41:A4F74:930C3C:1DF33D1:6AD4920C";
    let metadataReads = 0;
    const repository = fixture.githubClient.rest.repos.get;
    fixture.githubClient.rest.repos.get = vi.fn(async parameters => {
      if (!parameters.headers && parameters.request?.retryCount === 0 && ++metadataReads <= 3) {
        throw Object.assign(new Error("private-provider-body"), { status: 500, response: { headers: { "x-github-request-id": requestId, authorization: "private-token" } } });
      }
      const response = await repository(parameters);
      return { ...response, data: { ...response.data, default_branch: "main" } };
    });
    const reference = fixture.githubClient.rest.git.getRef;
    fixture.githubClient.rest.git.getRef = async parameters => (parameters.ref === "heads/main" ? { data: { ref: "refs/heads/main", object: { sha: REF } } } : reference(parameters));
    const source = "---\ntools:\n  work-queue:\n    worker: true\n---\nExecute approved instructions.\n";
    const lock = `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${contract}"\n`;
    fixture.githubClient.rest.repos.getContent = async ({ path }) => ({ data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? source : lock).toString("base64") } });
    fixture.githubClient.rest.actions.getWorkflow = async () => ({ data: { path: ".github/workflows/worker.lock.yml", state: "active" } });
    const runs = new Map();
    post.mockImplementation(async parameters => {
      const assignment = JSON.parse(parameters.inputs.work_queue_assignment);
      const runId = String(42 + runs.size);
      runs.set(runId, { ...fixture.nativeRun(), id: runId, display_title: `gh-aw work-queue ${assignment.dispatch_id}` });
      return { status: 200, data: { workflow_run_id: runId, run_url: `https://api.github.com/repos/owner/repo/actions/runs/${runId}`, html_url: `https://github.com/owner/repo/actions/runs/${runId}` } };
    });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async ({ run_id }) => ({ status: 200, data: runs.get(run_id) });
    const core = {
      info: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
    };
    const sleepFn = vi.fn(async () => {});
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    try {
      stagePlanIntents(filename, {
        version: 1,
        nodes: [{ graph_id: "new-cohort", node_key: "new", pool: "default", worker_profile: "default", payload: { task: "new submission" } }],
        dispatch: { pool: "default", max_claims: 3, max_dispatches: 3 },
      });
      const result = await publishQueueControls({ ...options, assignment: null, role: "dispatcher", intentPath: filename, policyProposal: fixture.policy, maxDispatches: 3, sleepFn, core });
      expect(result).toMatchObject({ status: "recovery_required", success: false, remaining_dispatches: 0 });
      expect(result.receipts).toEqual([
        expect.objectContaining({ status: "blocked", reason: "queue_control_github_api_unavailable", failure_stage: "intent_processing" }),
        expect.objectContaining({
          status: "durable",
          success: true,
          dispatches: 3,
          launches: [expect.objectContaining({ state: "bound", run_id: "42" }), expect.objectContaining({ state: "bound", run_id: "43" }), expect.objectContaining({ state: "bound", run_id: "44" })],
        }),
      ]);
      expect(metadataReads).toBe(4);
      expect(sleepFn.mock.calls).toEqual([[1000], [1000]]);
      expect(fixture.state.works.size).toBe(3);
      expect(fixture.state.works.has(nodeId("new-cohort", "new"))).toBe(false);
      expect(fixture.state.claims.size).toBe(3);
      expect([...fixture.state.dispatches.values()].every(dispatch => dispatch.state === "bound")).toBe(true);
      expect(post).toHaveBeenCalledTimes(3);
      expect(post.mock.calls.every(([parameters]) => parameters.request.retries === 0 && parameters.request.retryCount === 0)).toBe(true);
      expect(core.setFailed).toHaveBeenCalledOnce();
      const output = stderr.mock.calls.map(call => call[0]).join("");
      expect(output).toContain("deployment.repository.read.failed");
      expect(output).toContain(`GitHub request ID: ${requestId}.`);
      expect(output).toContain("controls.complete durable=1 blocked=1 rejected=0 unresolved=0 recovery_required=true");
      expect(output + JSON.stringify(result)).not.toContain("private");
    } finally {
      stderr.mockRestore();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each(["work_queue_dispatch_credential_client_mismatch", "private-token-response"])("retains committed grants and reports prelaunch failure safely: %s", async message => {
    const { fixture, options, post } = setup({ granted: false, count: 3, batch: 1 });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-prelaunch-"));
    const filename = path.join(directory, "intents.jsonl");
    const core = {
      info: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
    };
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    const validator = vi.fn().mockRejectedValue(new Error(message));
    const config = { ...options.config, work_queue_dispatch_credential: { kind: "authenticated" }, work_queue_enabled: true };
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "prelaunch-failure", kind: "dispatch_next", parameters: { pool: "default", max_claims: 3, max_dispatches: 3 } })}\n`);
      const result = await publishQueueControls({ ...options, config, intentPath: filename, validateDispatchCredential: validator, core });
      expect(result).toMatchObject({ status: "recovery_required", success: false, remaining_dispatches: 0 });
      expect(result.receipts[0]).toMatchObject({ status: "durable", success: false, dispatches: 3 });
      expect(result.receipts[0].request_id).toBeTruthy();
      expect(result.receipts[0].commit_id).toBe(fixture.state.tip);
      expect(result.receipts[0].launches).toHaveLength(3);
      const reason = message.startsWith("work_queue_") ? message : "queue_control_authority_or_publication_failed";
      for (const launch of result.receipts[0].launches) {
        expect(launch).toMatchObject({ state: "blocked", reason, failure_stage: "launch_processing" });
        expect(launch).not.toHaveProperty("released");
        expect(launch).not.toHaveProperty("dispatched");
      }
      expect(core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(reason));
      expect(core.setFailed).toHaveBeenCalledOnce();
      const explanation = message === "work_queue_dispatch_credential_client_mismatch" ? "credential validation and workflow dispatch use the same selected token" : "raw exception details are withheld to protect credentials and task data";
      expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining(explanation));
      expect(result.summary).toContain(explanation);
      expect(result.receipts[0].launches[0].explanation).toContain(explanation);
      expect(validator).toHaveBeenCalledTimes(3);
      expect(post).not.toHaveBeenCalled();
      expect([...fixture.state.dispatches.values()].every(dispatch => dispatch.state === "reserved" && !dispatch.sender && !dispatch.run)).toBe(true);
      const logs = stderr.mock.calls.map(call => call[0]).join("");
      expect(logs).toContain("launch.processing.blocked");
      expect(logs).toContain(`${reason}=true`);
      expect(logs).toContain(explanation);
      expect(result.summary).toContain("Blocked intents: 0");
      expect(result.summary).toContain("Unresolved launches: 3");
      expect(logs).toContain("controls.complete durable=1 blocked=0 rejected=0 unresolved=3 recovery_required=true");
      expect(logs + JSON.stringify(result)).not.toContain("private-token-response");
    } finally {
      stderr.mockRestore();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("does not misreport a post-launch publication exception as definitive nonlaunch", async () => {
    const { fixture, options, post } = setup({ granted: false, count: 1 });
    post.mockRejectedValue({ status: 422, response: { status: 422, headers: { "x-github-request-id": "trusted-provider-request" } } });
    const publish = options.publishWorkQueueRequest;
    const result = await dispatchQueueIntent({
      ...options,
      publishWorkQueueRequest: args => {
        if (args.request.kind === "release") throw new Error("private-release-error");
        return publish(args);
      },
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "post-launch-failure", pool: "default", max_claims: 1, max_dispatches: 1 },
    });
    expect(result).toMatchObject({ success: false, status: "durable", dispatches: 1 });
    expect(result.launches[0]).toMatchObject({ state: "blocked", failure_stage: "launch_processing" });
    expect(result.launches[0]).not.toHaveProperty("dispatched");
    expect(result.launches[0]).not.toHaveProperty("released");
    expect([...fixture.state.dispatches.values()][0]).toMatchObject({ state: "rejected", released: false });
    expect(post).toHaveBeenCalledOnce();
    expect(JSON.stringify(result)).not.toContain("private-release-error");
  });

  it.each(["after_start", "after_post", "release"])("counts a processing exception at %s only as an unresolved launch", async stage => {
    const { fixture, options, post } = setup({ granted: false, count: 1 });
    const filename = path.join(process.cwd(), `.gh-aw-queue-outcome-${stage}.jsonl`);
    const core = { info: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() };
    const config = { ...options.config, work_queue_dispatch_credential: { kind: "authenticated" } };
    post.mockRejectedValue({ status: 422, response: { status: 422, headers: { "x-github-request-id": "trusted-provider-request" } } });
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "unknown-outcome", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } })}\n`);
      const result = await publishQueueControls({
        ...options,
        config,
        intentPath: filename,
        core,
        readWorkQueueLog: args => {
          if (stage === "after_start" && [...fixture.state.dispatches.values()].some(dispatch => dispatch.state === "started")) throw new Error("private-read-error");
          return options.readWorkQueueLog(args);
        },
        publishWorkQueueRequest: args => {
          if ((stage === "after_post" && args.request.parameters.operations?.some(operation => operation.state === "rejected")) || (stage === "release" && args.request.kind === "release")) throw new Error("private-publication-error");
          return options.publishWorkQueueRequest(args);
        },
      });
      expect(result).toMatchObject({ status: "recovery_required", success: false });
      const launch = result.receipts[0].launches[0];
      expect(launch).toMatchObject({ state: "blocked", failure_stage: "launch_processing" });
      expect(launch).not.toHaveProperty("dispatched");
      expect(launch).not.toHaveProperty("released");
      expect(result.summary).toContain("Blocked intents: 0");
      expect(result.summary).toContain("Unresolved launches: 1");
      expect(core.setFailed).toHaveBeenCalledOnce();
      expect([...fixture.state.dispatches.values()][0]).toMatchObject({ state: stage === "release" ? "rejected" : "started", released: false });
      expect(post).toHaveBeenCalledTimes(stage === "after_start" ? 0 : 1);
      expect(JSON.stringify(result)).not.toContain("private-");
    } finally {
      fs.rmSync(filename, { force: true });
    }
  });

  it("publishes submit-only intents without accessing protected launch credentials", async () => {
    const { fixture, options, post } = setup({ granted: false });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-submit-only-"));
    const filename = path.join(directory, "intents.jsonl");
    const config = { ...options.config };
    const credentialAccess = vi.fn(() => {
      throw new Error("submit-only processing consumed a launch credential");
    });
    Object.defineProperty(config, "github-token", { get: credentialAccess });
    Object.defineProperty(config, "work_queue_dispatch_credential", { get: credentialAccess });
    try {
      fs.writeFileSync(
        filename,
        `${JSON.stringify({ version: 3, intent_id: "submit-without-launch-credentials", kind: "submit", parameters: { nodes: [{ graph_id: "public-submit-only", node_key: "root", payload: { task: "submit without launch" }, depends_on: [] }] } })}\n`
      );
      const before = fixture.state.works.size;
      const result = await publishQueueControls({ ...options, config, intentPath: filename, dispatchClient: undefined, validateDispatchCredential: undefined, core: { info: vi.fn(), setOutput: vi.fn() } });
      expect(result.receipts).toEqual([expect.objectContaining({ status: "durable", intent_id: "submit-without-launch-credentials" })]);
      expect(fixture.state.works.size).toBe(before + 1);
      expect(credentialAccess).not.toHaveBeenCalled();
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each(["options", "config"])("keeps the public %s staged preview independent of dispatch credentials", async location => {
    for (const credential of [undefined, null, { kind: "unsupported" }, { kind: "github_app" }, { kind: "github_token" }, "unreadable"]) {
      const { fixture, options, post } = setup({ granted: false });
      const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-public-preview-"));
      const filename = path.join(directory, "intents.jsonl");
      const before = canonical(fixture.transactions);
      const publish = vi.fn(options.publishWorkQueueRequest);
      const core = { info: vi.fn(), setOutput: vi.fn() };
      const config = { ...options.config, ...(location === "config" ? { staged: true } : {}) };
      Object.defineProperty(config, "github-token", {
        get: () => {
          throw new Error("preview read a dispatch token");
        },
      });
      Object.defineProperty(config, "work_queue_dispatch_credential", {
        get: () => {
          if (credential === "unreadable") throw new Error("preview read dispatch credential metadata");
          return credential;
        },
      });
      fixture.githubClient.auth = vi.fn().mockRejectedValue(new Error("preview authenticated a dispatch client"));
      try {
        fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "preview", kind: "submit", parameters: { nodes: [{ payload: { plan: "read-only preview" } }] } })}\n`);
        const result = await publishQueueControls({
          ...options,
          ...(location === "options" ? { staged: true } : {}),
          config,
          core,
          dispatchClient: undefined,
          validateDispatchCredential: undefined,
          publishWorkQueueRequest: publish,
          intentPath: filename,
        });
        expect(result.receipts).toEqual([expect.objectContaining({ intent_id: "preview", status: "staged_preview" })]);
        expect(canonical(fixture.transactions)).toBe(before);
        expect(publish).not.toHaveBeenCalled();
        expect(post).not.toHaveBeenCalled();
        expect(fixture.githubClient.auth).not.toHaveBeenCalled();
      } finally {
        fs.rmSync(directory, { recursive: true, force: true });
      }
    }
  });

  it.each(["unbound", "forged_assignment", "wrong_native", "rerun"])("rejects %s worker previews without recording Completion or effects", async failure => {
    const { fixture, options, post } = setup({ count: 1, bound: failure !== "unbound" });
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-preview-proof-"));
    const filename = path.join(directory, "intents.jsonl");
    const assignment = structuredClone(fixture.assignment);
    if (failure === "forged_assignment") assignment.claims[0].work.plan = "not the stored immutable Work";
    if (failure === "wrong_native") fixture.githubClient.rest.actions.getWorkflowRun = async () => ({ status: 200, data: { ...fixture.nativeRun(), head_sha: "b".repeat(40) } });
    const worker = {
      ...options,
      assignment,
      staged: true,
      context: { ...fixture.workerContext, ...(failure === "rerun" ? { runAttempt: 2 } : {}) },
      workflowRef: `${REPOSITORY}/.github/workflows/worker.lock.yml@${REF}`,
      intentPath: filename,
    };
    const before = fixture.transactions;
    try {
      fs.writeFileSync(filename, `${JSON.stringify({ version: 3, intent_id: "preview", kind: "submit", parameters: { nodes: [{ payload: { plan: "read-only preview" } }] } })}\n`);
      expect((await processWorkQueueIntents(worker)).receipts[0].status).toBe("blocked");
      await expect(dispatchQueueIntent({ ...worker, remainingDispatches: 1, message: { type: "work_queue_dispatch_next", intent_id: "preview-dispatch", pool: "default", max_claims: 1, max_dispatches: 1 } })).rejects.toThrow();
      expect(fixture.transactions).toEqual(before);
      expect(post).not.toHaveBeenCalled();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
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
