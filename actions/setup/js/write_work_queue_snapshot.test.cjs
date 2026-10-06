// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import { randomUUID } from "crypto";
import { main, resolveWorkerAssignment } from "./write_work_queue_snapshot.cjs";
import { loadWorkQueueSnapshot } from "./work_queue_mcp_server.cjs";
import { queueFixture, REF, REPOSITORY, WORKFLOW, DISPATCHER } from "./work_queue_lifecycle.test_helpers.cjs";
import { newRequest, newState, replayTransactions } from "./work_queue_replay.cjs";

const directories = [];
function setup(options = {}) {
  const fixture = queueFixture(options);
  const directory = path.join(process.cwd(), `.queue-snapshot-test-${randomUUID()}`);
  fs.mkdirSync(directory);
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
  it("bootstraps only genuine absence with an explicit approved and natively authenticated administrator", async () => {
    const { fixture, options } = setup({ granted: false });
    let log = { sha: null, transactions: [], state: newState() };
    const initialize = vi.fn(async request => {
      expect(request.context).toMatchObject({ role: "administrator", authenticated: true, roles: ["administrator"], principal: "11", run_id: "15" });
      expect(request.policyProposal).toEqual(fixture.policy);
      const transactions = fixture.transactions.slice(0, 1);
      log = { sha: "checked-genesis", transactions, state: replayTransactions(transactions) };
    });
    const approved = { role: "administrator", roles: ["administrator"], authenticated: true, principal: "11", repository: REPOSITORY };
    const configured = { ...options, policyProposal: fixture.policy, initializationContext: approved, initializeWorkQueue: initialize, readWorkQueueLog: async () => log };
    const snapshot = await main(configured);
    expect(snapshot.sha).toBe("checked-genesis");
    expect(loadWorkQueueSnapshot(options.snapshotPath).projection.works.size).toBe(0);
    expect(initialize).toHaveBeenCalledTimes(1);
    await main({ ...configured, snapshotPath: path.join(path.dirname(options.snapshotPath), "again.json") });
    expect(initialize).toHaveBeenCalledTimes(1);
  });

  it("never treats an existing policyless or malformed ledger, a dispatcher, rerun, or preview as approved genesis", async () => {
    const { fixture, options } = setup({ granted: false });
    const administrator = { role: "administrator", roles: ["administrator"], authenticated: true, principal: "11", repository: REPOSITORY };
    const initialize = vi.fn();
    const configured = { ...options, policyProposal: fixture.policy, initializationContext: administrator, initializeWorkQueue: initialize, readWorkQueueLog: async () => ({ sha: null, transactions: [], state: newState() }) };
    await expect(main({ ...configured, initializationContext: { ...administrator, role: "dispatcher", roles: ["dispatcher"] } })).rejects.toThrow(/not_authorized/);
    await expect(main({ ...configured, initializationContext: { ...administrator, principal: "12" } })).rejects.toThrow(/native_mismatch/);
    await expect(main({ ...configured, staged: true })).rejects.toThrow(/policy_missing/);
    await expect(main({ ...configured, readWorkQueueLog: async () => ({ sha: "existing", transactions: [], state: newState() }) })).rejects.toThrow(/genesis_not_absent/);
    await expect(main({ ...configured, policyProposal: undefined })).rejects.toThrow(/proposal_required/);
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => ({ status: 200, data: { ...(await get(parameters)).data, run_attempt: 2 } });
    await expect(main(configured)).rejects.toThrow(/rerun/);
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
    let log = { sha: null, transactions: [], state: newState() };
    const initialize = vi.fn(async () => {
      const transactions = winner.transactions.slice(0, 1);
      log = { sha: "concurrent-winner", transactions, state: replayTransactions(transactions) };
    });
    await expect(
      main({
        ...options,
        policyProposal: fixture.policy,
        initializationContext: { role: "administrator", roles: ["administrator"], authenticated: true, principal: "11", repository: REPOSITORY },
        initializeWorkQueue: initialize,
        readWorkQueueLog: async () => log,
      })
    ).rejects.toThrow(/proposal_mismatch/);
    expect(initialize).toHaveBeenCalledTimes(1);
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
    await expect(main({ ...missing.options, policyProposal: missing.fixture.policy, readWorkQueueLog: async () => ({ sha: null, transactions: [], state: newState() }) })).rejects.toThrow(/policy_missing/);
    expect(fs.existsSync(missing.options.snapshotPath)).toBe(false);
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
