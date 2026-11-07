// @ts-check
import { describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import { canonical } from "./work_queue_codec.cjs";
import { synchronizeDeployments } from "./work_queue_deployment_control.cjs";
import { compiledBridgeRuntime } from "./work_queue_contract_bridges.cjs";
import { executionProfile, futurePolicy } from "./work_queue_deployment.cjs";
import { normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { compactTransactions, replayTransactions } from "./work_queue_replay.cjs";
import { queueFixture, REF, WORKFLOW, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";

const DEFAULT_REF = "b".repeat(40);
const BRANCH_REF = "e".repeat(40);
const CONTRACT = "a".repeat(64);
const NEXT_CONTRACT = "b".repeat(64);
const SOURCE = "---\ntools:\n  work-queue:\n    worker: true\n---\nExecute approved instructions.\n";
const lock = (contract = CONTRACT) => `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${contract}"\n`;
const bridgeLock = (contract = CONTRACT) => lock(contract) + `jobs:\n  worker:\n    steps:\n      - uses: actions/checkout@${"a".repeat(40)}\n      - uses: ./actions/setup\n`;

function fixture({ otherPool = false, mixed = false, bridges = false } = {}) {
  const f = queueFixture({
    count: mixed ? 9 : 3,
    batch: 1,
    granted: false,
    configurePolicy: policy => {
      policy.authorization = "aw";
      policy.producers = {};
      const profile = policy.pools.default.profiles.default;
      delete profile.principal;
      profile.logical_contract = CONTRACT;
      policy.pools.default.profiles.other = { ...profile, workflow: ".github/workflows/other.lock.yml" };
      if (otherPool) policy.pools.unrelated = structuredClone(policy.pools.default);
    },
    configureWork: (work, index) => {
      if (!mixed && index === 1) work.execution_ref = REF;
      if (!mixed && index === 2) work.worker_profile = "other";
    },
  });
  f.githubClient.rest.repos.get = vi.fn(async () => ({ data: { full_name: REPOSITORY, default_branch: "release/main" } }));
  f.githubClient.rest.git = { getRef: vi.fn(async () => ({ data: { ref: "refs/heads/release/main", object: { sha: DEFAULT_REF } } })) };
  f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => {
    if (path === ".github/aw/work-queue-contract-routes.json") throw Object.assign(new Error("no bridge approval"), { status: 404 });
    expect([DEFAULT_REF, REF]).toContain(ref);
    return { data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock()).toString("base64") } };
  });
  f.githubClient.rest.actions.getWorkflow = vi.fn(async ({ workflow_id }) => ({ data: { path: `.github/workflows/${workflow_id}`, state: "active" } }));
  const options = {
    githubClient: {
      ...f.githubClient,
      rest: {
        ...f.githubClient.rest,
        repos: new Proxy(f.githubClient.rest.repos, {
          get(target, key) {
            if (key !== "getContent") return target[key];
            return async args => {
              let content;
              if (args.path === ".github/aw/work-queue-contract-routes.json") {
                if (!f.bridgeApproval) throw Object.assign(new Error("no bridge approval"), { status: 404 });
                content = JSON.stringify(f.bridgeApproval);
              } else if (f.bridgeReaders?.[args.path]) content = f.bridgeReaders[args.path + "\n" + args.ref] ?? f.bridgeReaders[args.path];
              else return target.getContent(args);
              return { data: { type: "file", path: args.path, encoding: "base64", content: Buffer.from(content).toString("base64") } };
            };
          },
        }),
      },
    },
    context: { ...f.dispatcherContext, sha: BRANCH_REF },
    policyProposal: futurePolicy(f.state),
    config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
    readWorkQueueLog: f.readWorkQueueLog,
    publishWorkQueueRequest: f.publishWorkQueueRequest,
    sleepFn: vi.fn(async () => {}),
  };
  const trusted = { ...f.dispatcher, authenticated: true, roles: ["dispatcher"] };
  if (bridges) installBridgeFixture(f, { [CONTRACT]: REF, [NEXT_CONTRACT]: DEFAULT_REF });
  return { f, options, trusted };
}

function installBridgeFixture(f, routes) {
  const readers = {
    "actions/setup/js/work_queue_replay.cjs": "reviewed_replay",
    "actions/setup/js/work_queue_deployment.cjs": "reviewed_deployment",
  };
  f.bridgeApproval = { version: 1, readers: Object.fromEntries(Object.entries(readers).map(([path, content]) => [path, createHash("sha256").update(content).digest("hex")])), routes: { [WORKFLOW]: routes } };
  f.bridgeReaders = readers;
}

describe("verified default worker deployment authority", () => {
  it.each([false, true])("retains protected approval after route clearing (checkpoint=%s)", async compacted => {
    const { f, options, trusted } = fixture();
    f.append("deployment", {
      operations: [
        {
          kind: "Deployment",
          pool: "default",
          worker_profile: "default",
          expected_ref: REF,
          expected_contract: CONTRACT,
          profile: futurePolicy(f.state).pools.default.profiles.default,
          available: true,
          contract_routes: {},
          reason: "compiler_deployment",
        },
      ],
    });
    expect(f.state.deployments.get("default").get("default").contract_routes).toBeUndefined();
    options.config = { work_queue_workflows: ["other"], aw_context_workflows: ["other"] };
    if (compacted) {
      const transactions = compactTransactions(f.transactions, "c".repeat(40), { ...f.dispatcher, role: "administrator" }, 6000);
      options.readWorkQueueLog = async () => ({ sha: "checkpoint-head", transactions, state: replayTransactions(transactions) });
    }
    const before = canonical(f.transactions);
    await expect(synchronizeDeployments(options, trusted)).rejects.toThrow("deployment history requires protected bridge approval");
    expect(canonical(f.transactions)).toBe(before);
  });

  it.each([
    ["local", bridgeLock(), true],
    ["foreign setup", bridgeLock().replace("./actions/setup", `github/gh-aw/actions/setup@${"d".repeat(40)}`), false],
    ["mixed external setup", bridgeLock() + `  external:\n    steps:\n      - uses: github/gh-aw-actions/setup@${"d".repeat(40)}\n`, false],
    ["mutable checkout", bridgeLock().replace(`actions/checkout@${"a".repeat(40)}`, "actions/checkout@main"), false],
    ["foreign repository", bridgeLock().replace("      - uses: ./actions/setup", "        with:\n          repository: other/repo\n      - uses: ./actions/setup"), false],
    ["script loader", bridgeLock().replace("      - uses: ./actions/setup", "      - run: echo replace-actions\n      - uses: ./actions/setup"), false],
  ])("binds reviewed reader bytes to the actual %s loader", (_, content, expected) => {
    expect(compiledBridgeRuntime(content, REPOSITORY, DEFAULT_REF)).toBe(expected);
  });

  it("rejects a foreign runtime even when both approved reader files exist", async () => {
    const { f, options, trusted } = fixture();
    installBridgeFixture(f, {});
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : bridgeLock() + `  external:\n    steps:\n      - uses: github/gh-aw-actions/setup@${"d".repeat(40)}\n`).toString("base64") },
    }));
    const before = canonical(f.transactions);
    await expect(synchronizeDeployments(options, trusted)).rejects.toThrow("same-repository");
    expect(canonical(f.transactions)).toBe(before);
  });

  it("automatically routes nine legacy admissions and one newer admission through a restored default", async () => {
    const { f, options, trusted } = fixture({ mixed: true });
    f.append(
      "deployment",
      {
        operations: [
          {
            kind: "Deployment",
            pool: "default",
            worker_profile: "default",
            expected_ref: REF,
            expected_contract: CONTRACT,
            profile: { ...futurePolicy(f.state).pools.default.profiles.default, ref: DEFAULT_REF, logical_contract: NEXT_CONTRACT },
            available: true,
            reason: "compiler_deployment",
          },
        ],
      },
      f.dispatcher
    );
    const submission = normalizeSubmitParameters({ nodes: [{ payload: { task: "newer admission" } }] }, futurePolicy(f.state), 5000, f.state);
    f.append("submit", submission, { role: "producer", principal: "11", repository: REPOSITORY });
    const restoredRef = "c".repeat(40);
    const bridgeRef = "d".repeat(40);
    f.githubClient.rest.git.getRef = vi.fn(async () => ({ data: { ref: "refs/heads/release/main", object: { sha: restoredRef } } }));
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : bridgeLock([DEFAULT_REF, bridgeRef].includes(ref) ? NEXT_CONTRACT : CONTRACT)).toString("base64") },
    }));
    installBridgeFixture(f, { [NEXT_CONTRACT]: bridgeRef });
    const before = f.state;
    await synchronizeDeployments(options, trusted);
    expect(f.state.works.size).toBe(10);
    for (const key of ["works", "claims", "dispatches", "clocks", "policy"]) expect(f.state[key]).toEqual(before[key]);
    const worker = f.state.deployments.get("default").get("default");
    expect(worker.contract_routes).toEqual({ [NEXT_CONTRACT]: bridgeRef });
    expect(f.transactions.flatMap(transaction => transaction.operations).some(operation => operation.kind === "Deployment" && operation.register && operation.profile.ref === bridgeRef)).toBe(true);
    expect(worker.current_ref).toBe(restoredRef);
    let legacy = 0;
    for (const work of f.state.works.values()) {
      const selected = executionProfile(f.state, work);
      expect(selected.reason).toBe("ready");
      expect(selected.profile.logical_contract).toBe(work.admission_contract);
      expect(selected.profile.ref).toBe(work.admission_contract === CONTRACT ? restoredRef : bridgeRef);
      if (work.admission_contract === CONTRACT) legacy++;
    }
    expect(legacy).toBe(9);
    const future = normalizeSubmitParameters({ nodes: [{ payload: { task: "next default admission" } }] }, futurePolicy(f.state), 6000, f.state);
    expect(future.nodes[0].logical_contract).toBe(CONTRACT);
    f.append("cancel_work", { operations: [{ kind: "WorkCancellation", work_id: submission.nodes[0].work_id, reason: "fixture_settlement" }] });
    await synchronizeDeployments(options, trusted);
    expect(f.state.deployments.get("default").get("default").contract_routes).toBeUndefined();
  });

  it("publishes no partial activation when retained immutable metadata is denied", async () => {
    const { f, options, trusted } = fixture();
    const failure = Object.assign(new Error("retained metadata denied"), { status: 403 });
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => {
      if (ref === REF) throw failure;
      return { data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock(NEXT_CONTRACT)).toString("base64") } };
    });
    installBridgeFixture(f, { [CONTRACT]: REF });
    const before = canonical(f.transactions);
    await expect(synchronizeDeployments(options, trusted)).rejects.toBe(failure);
    expect(canonical(f.transactions)).toBe(before);
  });

  it("refreshes bridge reader evidence after a concurrent admission without partial activation", async () => {
    const { f, options, trusted } = fixture({ mixed: true, bridges: true });
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : bridgeLock()).toString("base64") },
    }));
    const before = f.transactions.length;
    options.publishWorkQueueRequest = vi.fn(async args => {
      if (options.publishWorkQueueRequest.mock.calls.length === 1) {
        const submission = normalizeSubmitParameters({ nodes: [{ payload: { task: "concurrent admission" } }] }, futurePolicy(f.state), 5000, f.state);
        f.append("submit", submission, { role: "producer", principal: "11", repository: REPOSITORY });
      }
      return f.publishWorkQueueRequest(args);
    });
    await synchronizeDeployments(options, trusted);
    expect(options.publishWorkQueueRequest).toHaveBeenCalledTimes(2);
    expect(f.transactions.slice(before).map(transaction => transaction.request.kind)).toEqual(["submit", "deployment"]);
    expect(f.state.works.size).toBe(10);
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(DEFAULT_REF);
    expect([...f.state.works.values()].every(work => executionProfile(f.state, work).reason === "ready")).toBe(true);
  });

  it("rejects an unavailable unregistered bridge without choosing an older available fallback", async () => {
    const { f, options, trusted } = fixture();
    const later = "d".repeat(40);
    f.append(
      "deployment",
      {
        operations: [
          {
            kind: "Deployment",
            pool: "default",
            worker_profile: "default",
            expected_ref: REF,
            expected_contract: CONTRACT,
            profile: { ...futurePolicy(f.state).pools.default.profiles.default, ref: later },
            available: false,
            reason: "worker_unavailable",
          },
        ],
      },
      f.dispatcher
    );
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => {
      if (ref === later) throw Object.assign(new Error("removed latest revision"), { status: 404 });
      return { data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock(ref === DEFAULT_REF ? NEXT_CONTRACT : CONTRACT)).toString("base64") } };
    });
    installBridgeFixture(f, { [CONTRACT]: "e".repeat(40) });
    const read = f.githubClient.rest.repos.getContent;
    f.githubClient.rest.repos.getContent = vi.fn(args => (args.ref === "e".repeat(40) && args.path.endsWith(".lock.yml") ? Promise.reject(Object.assign(new Error("missing bridge"), { status: 404 })) : read(args)));
    await expect(synchronizeDeployments(options, trusted)).rejects.toThrow("deployment_invalid");
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(later);
  });

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
      return { data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock()).toString("base64") } };
    });
    await synchronizeDeployments(options, trusted);
    expect(f.state.deployments.get("default").get("default").current_ref).toBe(revision);
  });

  it.each([CONTRACT, NEXT_CONTRACT])("automatically upgrades verified default revisions with contract %s while preserving old obligations", async contract => {
    const { f, options, trusted } = fixture();
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock(ref === DEFAULT_REF ? contract : CONTRACT)).toString("base64") },
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

  it("preserves the legacy incompatible pause without protected bridge approval", async () => {
    const { f, options, trusted } = fixture();
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock(ref === DEFAULT_REF ? NEXT_CONTRACT : CONTRACT)).toString("base64") },
    }));
    const read = f.githubClient.rest.repos.getContent;
    f.githubClient.rest.repos.getContent = vi.fn(args => (args.path === ".github/aw/work-queue-contract-routes.json" ? Promise.reject(Object.assign(new Error("no approval"), { status: 404 })) : read(args)));
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

  it.each(["repository", "reference", "source", "lock", "registration"])("propagates permission/rate/transport errors at %s without publishing unavailability", async endpoint => {
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
          if (args.path.endsWith(endpoint === "source" ? ".md" : ".lock.yml")) return reject();
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
    ["missing source", "source", undefined],
    ["unapproved source", "source", "---\ntools: {work-queue: {worker: false}}\n---\n"],
    ["string approval", "source", '---\ntools: {work-queue: {worker: "true"}}\n---\n'],
    ["malformed source", "source", "---\ntools: [\n---\n"],
    ["missing lock", "lock", undefined],
    ["missing stamp", "lock", lock().replace(`env:\n  GH_AW_WORK_QUEUE_CONTRACT: "${CONTRACT}"\n`, "")],
    ["mutable stamp", "lock", lock("${{ github.sha }}")],
    ["non-environment stamp", "lock", lock().replace("env:", "example:\n  env:").replace("  GH_AW", "    GH_AW")],
    ["malformed lock", "lock", lock() + "broken: [\n"],
    ["missing assignment", "lock", lock().replace("work_queue_assignment:", "foreign:")],
  ])("handles %s locally without installing unknown code or pausing unrelated workers", async (_, endpoint, content) => {
    const { f, options, trusted } = fixture();
    options.config = { work_queue_workflows: ["worker", "other"], aw_context_workflows: ["worker", "other"] };
    const read = f.githubClient.rest.repos.getContent;
    f.githubClient.rest.repos.getContent = vi.fn(async args => {
      if (args.ref === DEFAULT_REF && args.path === WORKFLOW.replace(/\.lock\.yml$/, endpoint === "source" ? ".md" : ".lock.yml")) {
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
