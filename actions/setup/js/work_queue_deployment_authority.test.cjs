// @ts-check
import { describe, expect, it, vi } from "vitest";
import { canonical, utf8Compare } from "./work_queue_codec.cjs";
import { synchronizeDeployments, reconcileObsoleteWork } from "./work_queue_deployment_control.cjs";
import { executionProfile, obsoleteWork, futurePolicy } from "./work_queue_deployment.cjs";
import { normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { nodeId } from "./work_queue_graph.cjs";
import { queueFixture, REF, WORKFLOW, REPOSITORY, DISPATCHER } from "./work_queue_lifecycle.test_helpers.cjs";

const DEFAULT_REF = "b".repeat(40);
const BRANCH_REF = "e".repeat(40);
const CONTRACT = "a".repeat(64);
const NEXT_CONTRACT = "b".repeat(64);
const lock = (contract = CONTRACT) =>
  `# gh-aw-manifest: ${JSON.stringify({ work_queue: { worker: true, logical_contract: contract } })}\non:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${contract}"\n`;

function fixture({ otherPool = false, operationLimit = 256 } = {}) {
  const f = queueFixture({
    count: 3,
    batch: 1,
    granted: false,
    configurePolicy: policy => {
      policy.authorization = "aw";
      policy.producers = {};
      policy.limits.operations = operationLimit;
      const profile = policy.pools.default.profiles.default;
      delete profile.principal;
      profile.logical_contract = CONTRACT;
      policy.pools.default.profiles.other = { ...profile, workflow: ".github/workflows/other.lock.yml" };
      if (otherPool) policy.pools.unrelated = structuredClone(policy.pools.default);
    },
    configureWork: (work, index) => {
      if (index === 1) work.execution_ref = REF;
      if (index === 2) work.worker_profile = "other";
    },
  });
  f.githubClient.rest.repos.get = vi.fn(async () => ({ status: 200, data: { full_name: REPOSITORY, id: 7, default_branch: "release/main" } }));
  f.githubClient.rest.git = { getRef: vi.fn(async () => ({ data: { ref: "refs/heads/release/main", object: { sha: DEFAULT_REF } } })) };
  f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => {
    expect([DEFAULT_REF, REF]).toContain(ref);
    expect(path.endsWith(".lock.yml")).toBe(true);
    return { data: { type: "file", path, encoding: "base64", content: Buffer.from(lock()).toString("base64") } };
  });
  f.githubClient.rest.actions.getWorkflow = vi.fn(async ({ workflow_id }) => ({ data: { path: `.github/workflows/${workflow_id}`, state: "active" } }));
  const options = {
    githubClient: f.githubClient,
    context: { ...f.dispatcherContext, sha: BRANCH_REF },
    policyProposal: futurePolicy(f.state),
    config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
    readWorkQueueLog: f.readWorkQueueLog,
    publishWorkQueueRequest: f.publishWorkQueueRequest,
    sleepFn: vi.fn(async () => {}),
  };
  const trusted = { ...f.dispatcher, authenticated: true, roles: ["dispatcher"] };
  return { f, options, trusted };
}

describe("verified default worker deployment authority", () => {
  it("reconciles only the requested pools without publishing unrelated worker changes", async () => {
    const { f, options, trusted } = fixture({ otherPool: true });
    const unrelated = canonical([...f.state.deployments.get("unrelated")]);
    await synchronizeDeployments(options, trusted, ["default"]);
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(DEFAULT_REF);
    expect(canonical([...f.state.deployments.get("unrelated")])).toBe(unrelated);
    expect(
      f.transactions
        .filter(transaction => transaction.request.kind === "deployment")
        .flatMap(transaction => transaction.operations)
        .every(operation => operation.pool === "default")
    ).toBe(true);
    const before = canonical(f.transactions);
    await synchronizeDeployments(options, trusted, []);
    expect(canonical(f.transactions)).toBe(before);
  });

  describe("obsolete approved worker backlog recovery", () => {
    async function outdated() {
      const { f, options, trusted } = fixture();
      options.context = f.dispatcherContext;
      options.workflowRef = `${REPOSITORY}/${DISPATCHER}@${REF}`;
      f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
        data: { type: "file", path, encoding: "base64", content: Buffer.from(lock(ref === DEFAULT_REF ? NEXT_CONTRACT : CONTRACT)).toString("base64") },
      }));
      await synchronizeDeployments(options, trusted);
      return { f, options };
    }

    it("terminally cancels obsolete unpinned Work once, retaining pins, compatible Work, policy, debt and history", async () => {
      const { f, options } = await outdated();
      const before = f.state;
      const [old, pinned, other] = [...before.works.values()];
      expect(await reconcileObsoleteWork(options)).toEqual({ cancelled: 1 });
      const after = f.state;
      expect(after.works.get(old.work_id)).toMatchObject({ state: "cancelled", cancellation_reason: "worker_contract_obsolete" });
      expect(after.works.get(pinned.work_id)).toEqual(pinned);
      expect(after.works.get(other.work_id)).toEqual(other);
      for (const key of ["policy", "policy_epoch", "claims", "dispatches", "clocks", "terminalBarriers"]) expect(after[key]).toEqual(before[key]);
      expect(f.transactions.slice(0, before.transactions.length)).toEqual(before.transactions);
      expect(f.transactions.at(-1).actor.role).toBe("reconciler");
      const history = canonical(f.transactions);
      expect(await reconcileObsoleteWork(options)).toEqual({ cancelled: 0 });
      expect(canonical(f.transactions)).toBe(history);
    });

    it.each(["pending", "failed", "verified"])("preserves %s delivery and all owned or pinned Work", async barrier => {
      const { f } = await outdated();
      const state = f.state;
      const work = [...state.works.values()][0];
      expect(obsoleteWork(state, work)).toBe(true);
      for (const changes of [{ barrier }, { state: "claimed" }, { claim_id: "owned" }, { execution_ref: REF }, { state: "completed" }]) {
        expect(obsoleteWork(state, { ...work, ...changes })).toBe(false);
      }
    });

    it("does not mistake unavailable deployments for obsolete Work or run in staged mode", async () => {
      const { f, options } = await outdated();
      expect(await reconcileObsoleteWork({ ...options, staged: true })).toEqual({ cancelled: 0 });
      const worker = f.state.deployments.get("default").get("default");
      f.append("deployment", {
        operations: [
          {
            kind: "Deployment",
            pool: "default",
            worker_profile: "default",
            expected_ref: worker.current_ref,
            expected_contract: worker.current_contract,
            profile: worker.revisions[worker.current_ref].profile,
            available: false,
            reason: "worker_unavailable",
          },
        ],
      });
      expect(await reconcileObsoleteWork(options)).toEqual({ cancelled: 0 });
    });

    it("rejects an entire stale cancellation batch after a concurrent compatible route update", async () => {
      const { f, options } = await outdated();
      options.publishWorkQueueRequest = async args => {
        const worker = f.state.deployments.get("default").get("default");
        f.append("deployment", {
          operations: [
            {
              kind: "Deployment",
              pool: "default",
              worker_profile: "default",
              expected_ref: worker.current_ref,
              expected_contract: worker.current_contract,
              profile: worker.revisions[REF].profile,
              available: true,
              reason: "compiler_deployment",
            },
          ],
        });
        return f.publishWorkQueueRequest(args);
      };
      await expect(reconcileObsoleteWork(options)).rejects.toMatchObject({ code: "deployment_conflict" });
      expect([...f.state.works.values()].every(work => work.state === "available")).toBe(true);
    });

    it("fails closed on authentication or publication errors", async () => {
      const { f, options } = await outdated();
      const failure = new Error("denied");
      options.publishWorkQueueRequest = vi.fn().mockRejectedValue(failure);
      await expect(reconcileObsoleteWork(options)).rejects.toBe(failure);
      expect([...f.state.works.values()].every(work => work.state === "available")).toBe(true);
      options.publishWorkQueueRequest = vi.fn();
      f.githubClient.rest.actions.getWorkflowRun = vi.fn().mockRejectedValue(failure);
      await expect(reconcileObsoleteWork(options)).rejects.toBe(failure);
      expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    });

    it("retires a bounded deterministic prefix and leaves the rest for the next maintenance run", async () => {
      const { f, options, trusted } = fixture({ operationLimit: 3 });
      const original = f.transactions[1].operations[0];
      f.append(
        "submit",
        {
          nodes: Array.from({ length: 3 }, (_, index) => ({ ...original, graph_id: "g2", node_key: `n${index}`, work_id: nodeId("g2", `n${index}`) })),
        },
        { role: "producer", principal: "11", repository: REPOSITORY }
      );
      options.context = f.dispatcherContext;
      options.workflowRef = `${REPOSITORY}/${DISPATCHER}@${REF}`;
      f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
        data: { type: "file", path, encoding: "base64", content: Buffer.from(lock(ref === DEFAULT_REF ? NEXT_CONTRACT : CONTRACT)).toString("base64") },
      }));
      await synchronizeDeployments(options, trusted);
      const eligible = [...f.state.works.values()]
        .filter(work => obsoleteWork(f.state, work))
        .map(work => work.work_id)
        .sort(utf8Compare);
      expect(eligible).toHaveLength(4);
      expect(await reconcileObsoleteWork(options)).toEqual({ cancelled: 3 });
      expect(f.transactions.at(-1).operations.map(operation => operation.work_id)).toEqual(eligible.slice(0, 3));
      expect(await reconcileObsoleteWork(options)).toEqual({ cancelled: 1 });
      expect(await reconcileObsoleteWork(options)).toEqual({ cancelled: 0 });
    });

    it("orders a controlled read projection independently of locale before selecting a bounded prefix", async () => {
      const { f, options } = await outdated();
      const projection = structuredClone(f.state);
      const work = [...projection.works.values()].find(candidate => obsoleteWork(projection, candidate));
      projection.policy.limits.operations = 3;
      // Synthetic IDs distinguish collation order without entering the replayed ledger.
      projection.works = new Map(["work_a", "work-a", "Work-B", "work-B"].map(work_id => [work_id, { ...work, work_id }]));
      options.readWorkQueueLog = async () => ({ ...(await f.readWorkQueueLog()), state: projection });
      const failure = new Error("stop before ledger publication");
      options.publishWorkQueueRequest = vi.fn().mockRejectedValue(failure);
      const history = canonical(f.transactions);
      await expect(reconcileObsoleteWork(options)).rejects.toBe(failure);
      expect(options.publishWorkQueueRequest).toHaveBeenCalledOnce();
      expect(options.publishWorkQueueRequest.mock.calls[0][0].request.parameters.operations.map(operation => operation.work_id)).toEqual(["Work-B", "work-B", "work-a"]);
      expect(canonical(f.transactions)).toBe(history);
    });
  });

  it.each([
    ["identical branch contract", {}],
    ["altered branch authority", { workflow: ".github/workflows/branch-worker.lock.yml", max_claims: 16, logical_contract: NEXT_CONTRACT }],
  ])("ignores %s and missing branch routes without retargeting or pausing shared workers", async (_, changes) => {
    const { f, options, trusted } = fixture();
    const proposed = options.policyProposal.pools.default.profiles.default;
    Object.assign(proposed, { ref: BRANCH_REF, ...changes });
    const before = f.state;
    await synchronizeDeployments(options, trusted);
    const worker = f.state.deployments.get("default").get("default");
    expect(worker.current_ref).toBe(DEFAULT_REF);
    expect(worker.current_contract).toBe(CONTRACT);
    expect(worker.revisions[DEFAULT_REF]).toEqual({ profile: { ...before.policy.pools.default.profiles.default, ref: DEFAULT_REF }, available: true });
    expect(worker.revisions[BRANCH_REF]).toBeUndefined();
    expect(f.state.deployments.get("default").get("other")).toEqual(before.deployments.get("default").get("other"));
    expect(f.state.clocks).toEqual(before.clocks);
    expect(f.githubClient.rest.git.getRef).toHaveBeenCalledWith({ owner: "owner", repo: "repo", ref: "heads/release/main", request: { retries: 0, retryCount: 0, signal: expect.any(AbortSignal) } });
    expect(f.githubClient.rest.repos.getContent.mock.calls.every(([call]) => call.path.startsWith(".github/workflows/worker.") && call.ref !== BRANCH_REF)).toBe(true);
    const transactions = canonical(f.transactions);
    await synchronizeDeployments(options, trusted);
    expect(canonical(f.transactions)).toBe(transactions);
    expect(worker.revisions[REF].available).toBe(true);
  });

  it("retains verified 64-digit default revisions without a caller SHA fallback", async () => {
    const { f, options, trusted } = fixture();
    const revision = "c".repeat(64);
    f.githubClient.rest.git.getRef = vi.fn(async () => ({ data: { ref: "refs/heads/release/main", object: { sha: revision } } }));
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => {
      expect([revision, REF]).toContain(ref);
      return { data: { type: "file", path, encoding: "base64", content: Buffer.from(lock()).toString("base64") } };
    });
    await synchronizeDeployments(options, trusted);
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(revision);
  });

  it("uses the compiler manifest without requiring Markdown source or an environment stamp", async () => {
    const { f, options, trusted } = fixture();
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path }) => {
      expect(path.endsWith(".lock.yml")).toBe(true);
      const content = lock().replace(`env:\n  GH_AW_WORK_QUEUE_CONTRACT: "${CONTRACT}"\n`, "");
      return { data: { type: "file", path, encoding: "base64", content: Buffer.from(content).toString("base64") } };
    });
    await synchronizeDeployments(options, trusted);
    const worker = f.state.deployments.get("default").get("default");
    expect(worker.current_ref).toBe(DEFAULT_REF);
    expect(worker.current_contract).toBe(CONTRACT);
    expect(worker.revisions[DEFAULT_REF].available).toBe(true);
  });

  it.each([CONTRACT, NEXT_CONTRACT])("automatically upgrades verified default revisions with contract %s while preserving old obligations", async contract => {
    const { f, options, trusted } = fixture();
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(lock(ref === DEFAULT_REF ? contract : CONTRACT)).toString("base64") },
    }));
    f.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 48000 }, f.dispatcher);
    const before = f.state;
    await synchronizeDeployments(options, trusted);
    const after = f.state;
    expect(after.deployments.get("default").get("default").current_ref).toBe(DEFAULT_REF);
    expect(after.deployments.get("default").get("default").current_contract).toBe(contract);
    expect(after.deployments.get("default").get("default").revisions[REF].available).toBe(true);
    for (const key of ["works", "claims", "dispatches"]) expect(after[key]).toEqual(before[key]);
    expect(after.policy).toEqual(before.policy);
    expect(after.clocks).toEqual(before.clocks);
    const pinned = [...after.works.values()].find(work => work.execution_ref);
    expect(executionProfile(after, pinned).profile.ref).toBe(REF);
    const submission = normalizeSubmitParameters({ nodes: [{ payload: { task: "future" } }] }, futurePolicy(after), 5000, after);
    expect(submission.nodes[0].logical_contract).toBe(contract);
    f.append("submit", submission, { role: "producer", principal: "11", repository: REPOSITORY });
    expect(executionProfile(f.state, f.state.works.get(submission.nodes[0].work_id)).profile.ref).toBe(DEFAULT_REF);
  });

  it("pauses incompatible pending unpinned Work without charging service or affecting an exact old pin", async () => {
    const { f, options, trusted } = fixture();
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(lock(ref === DEFAULT_REF ? NEXT_CONTRACT : CONTRACT)).toString("base64") },
    }));
    await synchronizeDeployments(options, trusted);
    const work = [...f.state.works.values()];
    expect(executionProfile(f.state, work[0]).reason).toBe("worker_incompatible");
    expect(executionProfile(f.state, work[1]).profile.ref).toBe(REF);
    const dispatched = f.append("dispatch_next", { pool: "default", max_claims: 3, max_dispatches: 3, max_bytes: 48000 }, f.dispatcher);
    expect(dispatched.assignments).toHaveLength(2);
    expect(f.state.stats.claims).toBe(2);
  });

  it.each([
    ["repository", { status: 500 }],
    ["repository", { response: { status: 503 } }],
    ["repository", { code: "ETIMEDOUT" }],
    ["repository", { code: "ECONNRESET" }],
    ["repository", { cause: { cause: { code: "UND_ERR_SOCKET" } } }],
    ["reference", { status: 500 }],
    ["reference", { response: { status: 503 } }],
    ["reference", { code: "ETIMEDOUT" }],
    ["reference", { code: "ECONNRESET" }],
    ["reference", { cause: { code: "UND_ERR_CONNECT_TIMEOUT" } }],
  ])("retries only the transient %s metadata read before updating deployments: %j", async (endpoint, failure) => {
    const { f, options, trusted } = fixture();
    const read = endpoint === "repository" ? f.githubClient.rest.repos.get : f.githubClient.rest.git.getRef;
    read.mockRejectedValueOnce(failure);
    await synchronizeDeployments(options, trusted);
    expect(read).toHaveBeenCalledTimes(2);
    expect(read.mock.calls.every(([parameters]) => parameters.request.retries === 0 && parameters.request.retryCount === 0 && parameters.request.signal instanceof AbortSignal)).toBe(true);
    expect(options.sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(DEFAULT_REF);
    expect(f.state.claims.size).toBe(0);
  });

  it.each(["repository", "reference"])("stops after three failed %s reads without publishing or exposing provider data", async endpoint => {
    const { f, options, trusted } = fixture();
    const requestId = "1B41:A4F74:930C3C:1DF33D1:6AD4920C";
    const failure = Object.assign(new Error("private-token-and-body"), { status: 500, response: { headers: { "x-github-request-id": requestId, authorization: "private-token" } } });
    const read = endpoint === "repository" ? f.githubClient.rest.repos.get : f.githubClient.rest.git.getRef;
    read.mockRejectedValue(failure);
    const before = canonical(f.transactions);
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    try {
      await expect(synchronizeDeployments(options, trusted)).rejects.toBe(failure);
      expect(read).toHaveBeenCalledTimes(3);
      expect(options.sleepFn.mock.calls).toEqual([[1000], [1000]]);
      expect(canonical(f.transactions)).toBe(before);
      expect(f.githubClient.rest.repos.getContent).not.toHaveBeenCalled();
      const output = stderr.mock.calls.map(call => call[0]).join("");
      expect(output).toContain(`deployment.${endpoint}.read.failed`);
      expect(output).toContain(`GitHub request ID: ${requestId}.`);
      expect(output).not.toContain("private");
    } finally {
      stderr.mockRestore();
    }
  });

  it.each([
    { status: 403, code: "ETIMEDOUT", response: { status: 503 } },
    { status: 503, code: "ERR_TRANSPORT_INTEGRITY" },
  ])("does not retry access denials or transport-integrity failures: %j", async failure => {
    const { f, options, trusted } = fixture();
    f.githubClient.rest.repos.get.mockRejectedValue(failure);
    const before = canonical(f.transactions);
    await expect(synchronizeDeployments(options, trusted)).rejects.toBe(failure);
    expect(f.githubClient.rest.repos.get).toHaveBeenCalledTimes(1);
    expect(options.sleepFn).not.toHaveBeenCalled();
    expect(canonical(f.transactions)).toBe(before);
  });

  it.each(["repository", "reference", "lock", "registration"])("propagates permission/rate/transport errors at %s without publishing unavailability", async endpoint => {
    for (const status of [401, 403, 422, 429, 500, undefined, ...(["repository", "reference"].includes(endpoint) ? [404] : [])]) {
      const { f, options, trusted } = fixture();
      const failure = Object.assign(new Error("verified metadata denied"), status === undefined ? {} : { status });
      const reject = vi.fn(async () => {
        throw failure;
      });
      if (endpoint === "repository") f.githubClient.rest.repos.get = reject;
      else if (endpoint === "reference") f.githubClient.rest.git.getRef = reject;
      else if (endpoint === "registration") f.githubClient.rest.actions.getWorkflow = reject;
      else {
        const read = f.githubClient.rest.repos.getContent;
        f.githubClient.rest.repos.getContent = vi.fn(async args => {
          if (args.path.endsWith(".lock.yml")) return reject();
          return read(args);
        });
      }
      const before = canonical(f.transactions);
      await expect(synchronizeDeployments(options, trusted)).rejects.toBe(failure);
      expect(canonical(f.transactions)).toBe(before);
      expect(reject).toHaveBeenCalledTimes(status === 500 ? 3 : 1);
      expect(options.sleepFn).toHaveBeenCalledTimes(status === 500 ? 2 : 0);
    }
  });

  it.each([
    ["repository mismatch", { full_name: "foreign/repo", default_branch: "main" }, null],
    ["missing default", { full_name: REPOSITORY }, null],
    ["invalid default", { full_name: REPOSITORY, default_branch: "../main" }, null],
    ["wrong ref", null, { ref: "refs/heads/feature", object: { sha: DEFAULT_REF } }],
    ["mutable ref", null, { ref: "refs/heads/release/main", object: { sha: "main" } }],
    ["zero ref", null, { ref: "refs/heads/release/main", object: { sha: "0".repeat(40) } }],
  ])("rejects %s without consulting caller proposal as a fallback", async (_, repository, reference) => {
    const { f, options, trusted } = fixture();
    if (repository) f.githubClient.rest.repos.get = vi.fn(async () => ({ data: repository }));
    if (reference) f.githubClient.rest.git.getRef = vi.fn(async () => ({ data: reference }));
    const before = canonical(f.transactions);
    await expect(synchronizeDeployments(options, trusted)).rejects.toMatchObject({ code: "policy_invalid" });
    expect(canonical(f.transactions)).toBe(before);
    expect(f.githubClient.rest.repos.getContent).not.toHaveBeenCalled();
  });

  it.each([
    ["missing lock", undefined],
    ["missing manifest", lock().replace(/^# gh-aw-manifest:.*\n/, "")],
    ["unapproved worker", lock().replace('"worker":true', '"worker":false')],
    ["string approval", lock().replace('"worker":true', '"worker":"true"')],
    ["malformed manifest", lock().replace(/^# gh-aw-manifest:.*\n/, "# gh-aw-manifest: {\n")],
    ["mutable contract", lock("${{ github.sha }}")],
    ["malformed lock", lock() + "broken: [\n"],
    ["missing assignment", lock().replace("work_queue_assignment:", "foreign:")],
  ])("handles %s locally without installing unknown code or pausing unrelated workers", async (_, content) => {
    const { f, options, trusted } = fixture();
    options.config = { work_queue_workflows: ["worker", "other"], aw_context_workflows: ["worker", "other"] };
    const read = f.githubClient.rest.repos.getContent;
    f.githubClient.rest.repos.getContent = vi.fn(async args => {
      if (args.ref === DEFAULT_REF && args.path === WORKFLOW) {
        if (content === undefined) throw Object.assign(new Error("missing approved artifact"), { status: 404 });
        return { data: { type: "file", path: args.path, encoding: "base64", content: Buffer.from(content).toString("base64") } };
      }
      return read(args);
    });
    await synchronizeDeployments(options, trusted);
    const worker = f.state.deployments.get("default").get("default");
    expect(worker.current_ref).toBe(REF);
    expect(worker.revisions[REF].available).toBe(false);
    expect(worker.revisions[DEFAULT_REF]).toBeUndefined();
    expect(f.state.deployments.get("default").get("other").revisions[DEFAULT_REF].available).toBe(true);
    expect(f.state.stats.claims).toBe(0);
  });

  it("does not promote or reinterpret historical pins when their exact source or contract becomes unavailable", async () => {
    const { f, options, trusted } = fixture();
    await synchronizeDeployments(options, trusted);
    const read = f.githubClient.rest.repos.getContent;
    f.githubClient.rest.repos.getContent = vi.fn(async args => {
      if (args.ref === REF && args.path.endsWith(".lock.yml")) return { data: { type: "file", path: args.path, encoding: "base64", content: Buffer.from(lock(NEXT_CONTRACT)).toString("base64") } };
      return read(args);
    });
    await synchronizeDeployments(options, trusted);
    const worker = f.state.deployments.get("default").get("default");
    expect(worker.current_ref).toBe(DEFAULT_REF);
    expect(worker.revisions[REF].profile.logical_contract).toBe(CONTRACT);
    expect(worker.revisions[REF].available).toBe(false);
    expect(worker.revisions[DEFAULT_REF].available).toBe(true);
  });

  it("refreshes a deployment CAS conflict without duplicate publication, service or caller authority drift", async () => {
    const { f, options, trusted } = fixture();
    let conflicting = true;
    options.publishWorkQueueRequest = vi.fn(async args => {
      if (conflicting) {
        conflicting = false;
        const worker = f.state.deployments.get("default").get("default");
        f.append("deployment", { operations: [{ ...args.request.parameters.operations[0], expected_ref: worker.current_ref, expected_contract: worker.current_contract }] }, f.dispatcher);
        throw Object.assign(new Error("concurrent deployment won"), { code: "deployment_conflict" });
      }
      return f.publishWorkQueueRequest(args);
    });
    await synchronizeDeployments(options, trusted);
    expect(options.publishWorkQueueRequest).toHaveBeenCalledTimes(1);
    expect(f.transactions.filter(transaction => transaction.request.kind === "deployment")).toHaveLength(1);
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(DEFAULT_REF);
    expect(f.state.stats.claims).toBe(0);
  });
});
