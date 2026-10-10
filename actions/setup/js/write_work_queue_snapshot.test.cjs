// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "node:os";
import { main, resolveWorkerAssignment } from "./write_work_queue_snapshot.cjs";
import { loadWorkQueueSnapshot, readWorkQueueState } from "./work_queue_mcp_server.cjs";
import { normalizeRuntimeMessage, readClaimScopeContext } from "./work_queue_claim_scope.cjs";
import { queueFixture, REF, REPOSITORY, WORKFLOW, DISPATCHER } from "./work_queue_lifecycle.test_helpers.cjs";
import { newRequest, newState, replayTransactions, serializeTransactionLog } from "./work_queue_replay.cjs";
import { fakeGitHub } from "./work_queue_store_checks.cjs";
import { MAX_SNAPSHOT_PARSE_BYTES } from "./work_queue_codec.cjs";
import { loadQueue } from "./work_queue_binding.cjs";
import { publishWorkQueueRequest } from "./work_queue_store.cjs";
import { newWork } from "./work_queue_graph.cjs";

const directories = [];
function setup(options = {}) {
  const fixture = queueFixture(options);
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-snapshot-"));
  directories.push(directory);
  const context = fixture.assignment ? fixture.workerContext : fixture.dispatcherContext;
  return {
    fixture,
    options: {
      githubClient: fixture.githubClient,
      context,
      workflowRef: `${REPOSITORY}/${fixture.assignment ? WORKFLOW : DISPATCHER}@${REF}`,
      readWorkQueueLog: fixture.readWorkQueueLog,
      publishWorkQueueRequest: fixture.publishWorkQueueRequest,
      snapshotPath: path.join(directory, "snapshot.json"),
      core: { info: vi.fn(), setOutput: vi.fn() },
      now: fixture.at,
    },
  };
}
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("authenticated immutable activation snapshots", () => {
  it("uses installed producer authority for a distinct native worker and rejects caller-relative proposals when supplied as pins", async () => {
    const { fixture, options } = setup({ started: true, count: 1, workerPrincipal: "22" });
    const fake = fakeGitHub(fixture.transactions);
    const githubClient = { rest: { ...fixture.githubClient.rest, git: fake.githubClient.rest.git } };
    const configured = { ...options, githubClient, readWorkQueueLog: undefined, publishWorkQueueRequest: undefined, requireAssignment: true };
    const callerRelative = structuredClone(fixture.policy);
    callerRelative.producers = { "22": callerRelative.producers["11"] };
    await expect(main({ ...configured, policyProposal: callerRelative })).rejects.toThrow("work_queue_policy_proposal_mismatch");
    expect(fake.state.updates).toBe(0);
    expect(options.core.setOutput).not.toHaveBeenCalled();
    expect(fs.existsSync(options.snapshotPath)).toBe(false);
    const snapshot = await main({ ...configured, policyProposal: undefined });
    expect(snapshot.origin.principal).toBe("22");
    expect(snapshot.worker).toEqual(fixture.assignment);
    const installed = loadWorkQueueSnapshot(options.snapshotPath).projection.policy;
    expect(installed.producers).toEqual(fixture.policy.producers);
    expect(installed.producers).not.toHaveProperty("22");
    expect(fake.log()[0].actor.principal).toBe("11");
    expect(fake.log().at(-1).actor.principal).toBe("22");
    expect(fake.state.updates).toBe(1);
  });

  it.each(["owner/repo", "Owner/Repo"])("preserves actual API repository %s through the real store and authenticated worker binding despite uppercase lookup aliases", async repository => {
    const { fixture, options } = setup({ started: true, count: 1 });
    const transactions = structuredClone(fixture.transactions);
    for (const commit of transactions) {
      commit.actor.repository = repository;
      for (const operation of commit.operations) if (operation.kind === "Dispatch" && operation.sender) operation.sender.repository = repository;
      if (commit.request.kind === "dispatch") commit.request.parameters.operations = commit.operations;
      commit.request = newRequest(commit.request.id, commit.request.kind, commit.actor, commit.request.parameters);
    }
    const fake = fakeGitHub(transactions);
    fake.githubClient.rest.repos.get = vi.fn(async () => ({ status: 200, data: { full_name: repository, id: 7, default_branch: "main", size: 1 } }));
    const githubClient = { rest: { ...fake.githubClient.rest, actions: fixture.githubClient.rest.actions } };
    const snapshot = await main({
      ...options,
      githubClient,
      context: { ...options.context, repo: { owner: "OWNER", repo: "REPO" } },
      readWorkQueueLog: undefined,
      publishWorkQueueRequest: undefined,
      requireAssignment: true,
    });
    expect(snapshot.origin.repository).toBe(repository);
    expect(snapshot.worker).toEqual(fixture.assignment);
    expect(JSON.parse(options.core.setOutput.mock.calls[0][1]).repository).toBe(repository);
    const binding = fake.log().at(-1);
    expect(binding.actor.repository).toBe(repository);
    expect(binding.operations[0]).toMatchObject({ kind: "Dispatch", state: "bound", run: { repository, run_attempt: 1, run_id: "42" }, evidence: { repository, source: "trusted_activation" } });
    expect(fake.state.updates).toBe(1);
  });

  it("rejects a replay-valid foreign genesis through the real store before native authentication, snapshot output, or writes", async () => {
    const { fixture, options } = setup({ granted: false });
    const genesis = structuredClone(fixture.transactions[0]);
    genesis.actor.repository = "foreign/repo";
    genesis.request = newRequest(genesis.request.id, genesis.request.kind, genesis.actor, genesis.request.parameters);
    const fake = fakeGitHub([genesis]);
    fake.githubClient.rest.repos.get = vi.fn(async () => ({ status: 200, data: { full_name: REPOSITORY, id: 7, default_branch: "main", size: 1 } }));
    const getWorkflowRun = vi.fn(fixture.githubClient.rest.actions.getWorkflowRun);
    const githubClient = { rest: { ...fake.githubClient.rest, actions: { ...fixture.githubClient.rest.actions, getWorkflowRun } } };
    await expect(main({ ...options, githubClient, readWorkQueueLog: undefined, publishWorkQueueRequest: undefined })).rejects.toThrow(/actor_unauthorized/);
    expect(getWorkflowRun).not.toHaveBeenCalled();
    expect(options.core.setOutput).not.toHaveBeenCalled();
    expect(fs.existsSync(options.snapshotPath)).toBe(false);
    expect(fake.state.updates).toBe(0);
  });

  it("rejects obsolete administrator seeding without calling the initializer", async () => {
    const { fixture, options } = setup({ granted: false });
    const log = { sha: null, transactions: [], state: newState() };
    const initialize = vi.fn();
    const approved = { role: "administrator", roles: ["administrator"], authenticated: true, principal: "11", repository: REPOSITORY };
    const configured = { ...options, policyProposal: fixture.policy, initializationContext: approved, initializeWorkQueue: initialize, readWorkQueueLog: async () => log };
    await expect(main(configured)).rejects.toThrow(/standalone_seeding_unsupported/);
    expect(initialize).not.toHaveBeenCalled();
    expect(log.sha).toBeNull();
    expect(fs.existsSync(options.snapshotPath)).toBe(false);
  });

  it("never treats an existing policyless or malformed ledger, a dispatcher, rerun, or preview as approved genesis", async () => {
    const { fixture, options } = setup({ granted: false });
    const administrator = { role: "administrator", roles: ["administrator"], authenticated: true, principal: "11", repository: REPOSITORY };
    const initialize = vi.fn();
    const configured = { ...options, policyProposal: fixture.policy, initializationContext: administrator, initializeWorkQueue: initialize, readWorkQueueLog: async () => ({ sha: null, transactions: [], state: newState() }) };
    await expect(main({ ...configured, initializationContext: { ...administrator, role: "dispatcher", roles: ["dispatcher"] } })).rejects.toThrow(/standalone_seeding_unsupported/);
    await expect(main({ ...configured, initializationContext: { ...administrator, principal: "12" } })).rejects.toThrow(/standalone_seeding_unsupported/);
    await expect(main({ ...configured, staged: true })).rejects.toThrow(/standalone_seeding_unsupported/);
    await expect(main({ ...configured, readWorkQueueLog: async () => ({ sha: "existing", transactions: [], state: newState() }) })).rejects.toThrow(/standalone_seeding_unsupported/);
    await expect(main({ ...configured, policyProposal: undefined })).rejects.toThrow(/standalone_seeding_unsupported/);
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => ({ status: 200, data: { ...(await get(parameters)).data, run_attempt: 2 } });
    await expect(main(configured)).rejects.toThrow(/standalone_seeding_unsupported/);
    expect(initialize).not.toHaveBeenCalled();
  });

  it("does not overwrite a differing policy installed by a concurrent bootstrap winner", async () => {
    const { fixture, options } = setup({ granted: false });
    const winner = queueFixture({
      granted: false,
      configurePolicy: policy => {
        policy.class_weights[0]++;
      },
    });
    const transactions = winner.transactions.slice(0, 1);
    const log = { sha: "concurrent-winner", transactions, state: replayTransactions(transactions) };
    await expect(
      main({
        ...options,
        policyProposal: fixture.policy,
        readWorkQueueLog: async () => log,
      })
    ).rejects.toThrow(/proposal_mismatch/);
    expect(log.state.policy).toEqual(winner.policy);
    expect(fs.existsSync(options.snapshotPath)).toBe(false);
  });

  it("records a bounded current-only snapshot with trusted origin and no credentials", async () => {
    const { options } = setup({ granted: false });
    const snapshot = await main(options);
    expect(snapshot).toMatchObject({ version: 3, worker: null, captured_at: options.now, origin: { role: "dispatcher", principal: "11", repository: REPOSITORY, workflow: DISPATCHER, run_id: "15", run_attempt: 1 } });
    expect(snapshot.origin).not.toHaveProperty("native_run");
    expect(snapshot.origin).not.toHaveProperty("authenticated");
    expect(JSON.parse(options.core.setOutput.mock.calls[0][1])).toEqual(snapshot.origin);
    expect(options.core.setOutput.mock.calls[0][0]).toBe("work_queue_origin");
    expect(fs.statSync(options.snapshotPath).mode & 0o777).toBe(0o444);
    expect(loadWorkQueueSnapshot(options.snapshotPath).projection.policy_epoch).toBe("e1");
  });

  it("reports an absent queue as uninitialized without claiming worker readiness or installing Policy", async () => {
    const { options } = setup({ granted: false });
    const initialize = vi.fn();
    const snapshot = await main({
      ...options,
      branch: "custom-queue",
      readWorkQueueLog: async () => ({ sha: null, transactions: [], state: newState() }),
      initializeWorkQueue: initialize,
    });
    expect(snapshot).toMatchObject({ sha: null, worker: null, role: "dispatcher" });
    expect(initialize).not.toHaveBeenCalled();
    expect(readWorkQueueState(loadWorkQueueSnapshot(options.snapshotPath)).queue_state).toBe("uninitialized");
    expect(options.core.info).toHaveBeenCalledWith(expect.stringContaining("protected queue branch 'custom-queue'"));
  });

  it("defers branch creation with a compiled Policy until the first checked Policy-and-Work commit", async () => {
    const { fixture, options } = setup({ granted: false });
    const fake = fakeGitHub();
    const githubClient = {
      rest: {
        ...fixture.githubClient.rest,
        actions: { ...fixture.githubClient.rest.actions, ...fake.githubClient.rest.actions },
        git: fake.githubClient.rest.git,
        repos: {
          ...fake.githubClient.rest.repos,
          get: async () => ({ status: 200, data: { full_name: REPOSITORY, id: 7, default_branch: "main", size: 1 } }),
        },
      },
    };
    const configured = { ...options, githubClient, policyProposal: fixture.policy, readWorkQueueLog: undefined, publishWorkQueueRequest: undefined };
    const snapshot = await main(configured);
    expect(snapshot).toMatchObject({ sha: null, worker: null, transactionLog: "" });
    expect(readWorkQueueState(loadWorkQueueSnapshot(options.snapshotPath))).toMatchObject({ queue_state: "uninitialized", total: 0 });
    expect(fake.refs.size).toBe(0);
    expect(fake.blobs.size).toBe(0);

    const nodes = [newWork({ plan: "first admitted task", effect_contract: { kind: "none" } }, "activation-bootstrap", "root", "default", fixture.policy, fixture.at)];
    const request = newRequest("activation-bootstrap", "submit", snapshot.origin, { nodes });
    const createRef = vi.spyOn(githubClient.rest.git, "createRef");
    const result = await publishWorkQueueRequest({
      githubClient,
      owner: "owner",
      repo: "repo",
      request,
      context: { ...snapshot.origin, authenticated: true, roles: ["dispatcher"] },
      policyProposal: fixture.policy,
      now: () => fixture.at,
    });
    expect(result.publishedNow).toBe(true);
    expect(result.transactions).toHaveLength(1);
    expect(result.commit.operations.map(operation => operation.kind)).toEqual(["Policy", "Work"]);
    expect(createRef).toHaveBeenCalledExactlyOnceWith({ owner: "owner", repo: "repo", ref: "refs/heads/work-queue", sha: result.sha });

    const installedPath = path.join(path.dirname(options.snapshotPath), "installed.json");
    const installed = await main({ ...configured, snapshotPath: installedPath });
    expect(installed.sha).toBe(result.sha);
    expect(readWorkQueueState(loadWorkQueueSnapshot(installedPath))).toMatchObject({ queue_state: "initialized", total: 1 });
    expect(fake.log()).toHaveLength(1);
  });

  it("rejects invalid bootstrap proposals and missing worker queues without creating a branch", async () => {
    const { fixture, options } = setup({ granted: false });
    const absent = async () => ({ sha: null, transactions: [], state: newState() });
    await expect(main({ ...options, policyProposal: { ...fixture.policy, mode: "invalid" }, readWorkQueueLog: absent })).rejects.toThrow();
    expect(fs.existsSync(options.snapshotPath)).toBe(false);
    const assigned = setup({ started: true });
    await expect(main({ ...assigned.options, policyProposal: assigned.fixture.policy, readWorkQueueLog: absent })).rejects.toThrow(/policy_missing/);
    expect(fs.existsSync(assigned.options.snapshotPath)).toBe(false);
    await expect(loadQueue({ ...options, policyProposal: fixture.policy, readWorkQueueLog: async () => ({ sha: "existing", transactions: [], state: newState() }) })).rejects.toThrow(/policy_missing/);
  });

  it("rejects actual oversized encoded framing before creating a snapshot or publishing origin output", async () => {
    const { fixture, options } = setup({ granted: false });
    const before = fixture.transactions;
    const visibleWorkIds = ["\0".repeat(Math.ceil(MAX_SNAPSHOT_PARSE_BYTES / 6))];
    await expect(main({ ...options, visibleWorkIds })).rejects.toThrow(/snapshot exceeds.*bounded/);
    expect(fs.existsSync(options.snapshotPath)).toBe(false);
    expect(options.core.setOutput).not.toHaveBeenCalled();
    expect(fixture.transactions).toEqual(before);
  });

  it("rejects a replay-valid ledger from another repository without treating casing as another authority", async () => {
    const { fixture, options } = setup({ granted: false });
    const logFor = repository => {
      const genesis = structuredClone(fixture.transactions[0]);
      genesis.actor.repository = repository;
      genesis.request = newRequest(genesis.request.id, genesis.request.kind, genesis.actor, genesis.request.parameters);
      const transactions = [genesis];
      return { sha: "checked-ledger", transactions, state: replayTransactions(transactions) };
    };
    await expect(main({ ...options, readWorkQueueLog: async () => logFor("foreign/repo") })).rejects.toThrow("work_queue_ledger_repository_mismatch");
    expect(fs.existsSync(options.snapshotPath)).toBe(false);
    expect((await main({ ...options, readWorkQueueLog: async () => logFor(REPOSITORY.toUpperCase()) })).sha).toBe("checked-ledger");
  });

  it("recovers lost binding through actual authenticated activation rather than an input run ID", async () => {
    const { fixture, options } = setup({ started: true });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
    const snapshot = await main({ ...options, requireAssignment: true });
    expect(snapshot.worker).toEqual(fixture.assignment);
    expect(snapshot.worker.claims[0].work).toEqual({ plan: "stored task 1", effect_contract: { kind: "none" } });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run.run_id).toBe("42");
    const bound = fixture.transactions.flatMap(commit => commit.operations).find(operation => operation.kind === "Dispatch" && operation.state === "bound");
    expect(bound.evidence.source).toBe("trusted_activation");
  });

  it("checks exact stored plans, membership, profiles and request/commit provenance", () => {
    const { fixture } = setup({ bound: true });
    expect(resolveWorkerAssignment(fixture.workerContext.payload, fixture.transactions)).toEqual(fixture.assignment);
    for (const changes of [
      { commit_id: "forged" },
      { request_id: "forged" },
      { worker_profile: "forged" },
      { claims: [...fixture.assignment.claims].reverse() },
      { claims: fixture.assignment.claims.map((member, index) => (index ? member : { ...member, work: { plan: "changed" } })) },
    ]) {
      expect(() => resolveWorkerAssignment({ inputs: { work_queue_assignment: JSON.stringify({ ...fixture.assignment, ...changes }) } }, fixture.transactions)).toThrow();
    }
    expect(() => resolveWorkerAssignment({ inputs: { work_queue_claim: '{"work_id":"w","claim_id":"c"}' } }, fixture.transactions)).toThrow(/legacy/);
  });

  it("fails closed for reruns, wrong workflows/principals and missing native markers", async () => {
    const reserved = setup();
    await expect(main(reserved.options)).rejects.toThrow(/launch_marker/);
    for (const override of [{ run_attempt: 2 }, { path: ".github/workflows/other.yml" }, { actor: { id: "12" } }, { head_sha: "b".repeat(40) }]) {
      const { fixture, options } = setup({ started: true });
      const get = fixture.githubClient.rest.actions.getWorkflowRun;
      fixture.githubClient.rest.actions.getWorkflowRun = async args => ({ status: 200, data: { ...(await get(args)).data, ...override } });
      await expect(main(options)).rejects.toThrow();
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
      expect(fs.existsSync(options.snapshotPath)).toBe(false);
    }
  });

  it("blocks an unassigned queue worker before an agent can start", async () => {
    const { options } = setup({ granted: false });
    await expect(main({ ...options, requireAssignment: true })).rejects.toThrow(/assignment_required/);
  });

  it("captures explicit read-only observers without a Claim, queue mutation or dispatcher origin", async () => {
    const { fixture, options } = setup({ granted: false });
    const before = fixture.transactions.length;
    const snapshot = await main({ ...options, role: "observer" });
    expect(snapshot).toMatchObject({ role: "observer", worker: null, origin: { role: "producer" } });
    expect(loadWorkQueueSnapshot(options.snapshotPath)).toMatchObject({ role: "observer", worker: null });
    expect(fixture.transactions).toHaveLength(before);
    expect(options.core.info).toHaveBeenCalledWith(expect.stringContaining("read-only observer"));
    await expect(main({ ...options, role: "observer", initializationContext: { role: "administrator" } })).rejects.toThrow(/observer_read_only/);
  });

  it("passes the actual observer writer envelope through the scope reader without stripping its role", async () => {
    const { fixture, options } = setup({ granted: false });
    const before = serializeTransactionLog(fixture.transactions);
    const snapshot = await main({ ...options, role: "observer" });
    const keys = ["GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_WORK_QUEUE_SNAPSHOT"];
    const previous = keys.map(key => process.env[key]);
    const hadContext = Object.hasOwn(global, "context");
    const previousContext = global.context;
    try {
      process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
      process.env.GH_AW_WORK_QUEUE_ROLE = "observer";
      process.env.GH_AW_WORK_QUEUE_SNAPSHOT = options.snapshotPath;
      global.context = options.context;
      expect(readClaimScopeContext()).toBeNull();
      const message = { type: "report_incomplete", body: "Read-only observation" };
      expect(normalizeRuntimeMessage(message)).toBe(message);
      expect(() => normalizeRuntimeMessage({ type: "work_queue_submit", parameters: { nodes: [] } })).toThrow(/observers/);
      expect(JSON.parse(fs.readFileSync(options.snapshotPath, "utf8"))).toEqual(snapshot);
      expect(snapshot.role).toBe("observer");
      expect(serializeTransactionLog(fixture.transactions)).toBe(before);
    } finally {
      keys.forEach((key, index) => {
        if (previous[index] === undefined) delete process.env[key];
        else process.env[key] = previous[index];
      });
      if (hadContext) global.context = previousContext;
      else delete global.context;
    }
  });

  it("lets explicit observers read genuine queue absence without installing a compiled policy proposal", async () => {
    const { fixture, options } = setup({ granted: false });
    const initialize = vi.fn();
    const snapshot = await main({
      ...options,
      role: "observer",
      policyProposal: fixture.policy,
      initializeWorkQueue: initialize,
      readWorkQueueLog: async () => ({ sha: null, transactions: [], state: newState() }),
    });

    expect(snapshot).toMatchObject({ role: "observer", sha: null, worker: null, transactionLog: "", origin: { role: "producer" } });
    expect(loadWorkQueueSnapshot(options.snapshotPath).projection.policy).toBeNull();
    expect(initialize).not.toHaveBeenCalled();
    await expect(main({ ...options, role: "observer", readWorkQueueLog: async () => ({ sha: "existing", transactions: [], state: newState() }) })).rejects.toThrow(/policy_missing/);
  });

  it("captures a genuinely absent queue for a dispatcher without creating policy or granting worker authority", async () => {
    const { options } = setup({ granted: false });
    const readWorkQueueLog = vi.fn(async () => ({ sha: null, transactions: [], state: newState() }));
    const snapshot = await main({ ...options, readWorkQueueLog });
    expect(snapshot).toMatchObject({ role: "dispatcher", sha: null, worker: null, transactionLog: "" });
    expect(loadWorkQueueSnapshot(options.snapshotPath).projection.policy).toBeNull();
    expect(readWorkQueueLog).toHaveBeenCalledTimes(2);
    const keys = ["GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_WORK_QUEUE_SNAPSHOT"];
    const previous = keys.map(key => process.env[key]);
    try {
      process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
      process.env.GH_AW_WORK_QUEUE_ROLE = "dispatcher";
      process.env.GH_AW_WORK_QUEUE_SNAPSHOT = options.snapshotPath;
      expect(readClaimScopeContext()).toMatchObject({ assignment: null, snapshot: { sha: null } });
    } finally {
      keys.forEach((key, index) => {
        if (previous[index] === undefined) delete process.env[key];
        else process.env[key] = previous[index];
      });
    }
    const existing = setup({ granted: false });
    await expect(main({ ...existing.options, readWorkQueueLog: async () => ({ sha: "existing", transactions: [], state: newState() }) })).rejects.toThrow(/policy_missing/);
    expect(fs.existsSync(existing.options.snapshotPath)).toBe(false);
  });

  it("cannot downgrade declared workers or supplied assignments into read-only observers", async () => {
    const unassigned = setup({ granted: false });
    await expect(main({ ...unassigned.options, role: "worker" })).rejects.toThrow(/assignment_required/);
    const assigned = setup({ bound: true });
    await expect(main({ ...assigned.options, role: "observer" })).rejects.toThrow(/observer_assignment/);
    for (const value of [null, "", {}, '{"version":3,"version":2}']) {
      await expect(main({ ...unassigned.options, role: "observer", context: { ...unassigned.options.context, payload: { inputs: { work_queue_assignment: value } } } })).rejects.toThrow();
    }
    expect(fs.existsSync(unassigned.options.snapshotPath)).toBe(false);
    expect(fs.existsSync(assigned.options.snapshotPath)).toBe(false);
  });

  it("checks resolved compiler policy proposals without installing or overwriting authority", async () => {
    const matching = setup({ granted: false });
    const before = matching.fixture.transactions.length;
    await main({ ...matching.options, policyProposal: matching.fixture.policy });
    expect(matching.fixture.transactions).toHaveLength(before);
    expect(loadWorkQueueSnapshot(matching.options.snapshotPath).projection.policy).toEqual(matching.fixture.policy);
    const mismatched = setup({ started: true });
    const proposal = JSON.parse(JSON.stringify(mismatched.fixture.policy));
    proposal.class_weights[0]++;
    const original = mismatched.fixture.transactions.length;
    await expect(main({ ...mismatched.options, policyProposal: proposal })).rejects.toThrow(/proposal_mismatch/);
    expect(mismatched.fixture.transactions).toHaveLength(original);
    expect(mismatched.fixture.state.dispatches.get(mismatched.fixture.assignment.dispatch_id).run).toBeUndefined();
    expect(fs.existsSync(mismatched.options.snapshotPath)).toBe(false);
    const missing = setup({ granted: false });
    const snapshot = await main({ ...missing.options, policyProposal: missing.fixture.policy, readWorkQueueLog: async () => ({ sha: null, transactions: [], state: newState() }) });
    expect(snapshot).toMatchObject({ sha: null, transactionLog: "", worker: null });
    expect(loadWorkQueueSnapshot(missing.options.snapshotPath).projection.policy).toBeNull();
  });

  it("reads only a bounded canonical environment proposal, never credentials or an authority flag", async () => {
    const { fixture, options } = setup({ granted: false });
    vi.stubEnv("GH_AW_WORK_QUEUE_POLICY", JSON.stringify(fixture.policy));
    try {
      const snapshot = await main(options);
      expect(snapshot).not.toHaveProperty("policyProposal");
      expect(snapshot).not.toHaveProperty("credentials");
      await expect(main({ ...options, policyProposal: { ...fixture.policy, authenticated: true } })).rejects.toThrow();
      await expect(main({ ...options, policyProposal: '{"mode":"strict-priority","mode":"weighted-priority"}' })).rejects.toThrow();
    } finally {
      vi.unstubAllEnvs();
    }
  });
});
