// @ts-check
import { describe, expect, it, vi } from "vitest";
import { canonical } from "./work_queue_codec.cjs";
import { synchronizeDeployments } from "./work_queue_deployment_control.cjs";
import { executionProfile, futurePolicy } from "./work_queue_deployment.cjs";
import { normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { queueFixture, REF, WORKFLOW, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";

const DEFAULT_REF = "b".repeat(40);
const BRANCH_REF = "e".repeat(40);
const CONTRACT = "a".repeat(64);
const NEXT_CONTRACT = "b".repeat(64);
const SOURCE = "---\ntools:\n  work-queue:\n    worker: true\n---\nExecute approved instructions.\n";
const lock = (contract = CONTRACT) => `on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\nenv:\n  GH_AW_WORK_QUEUE_CONTRACT: "${contract}"\n`;

function fixture() {
  const f = queueFixture({
    count: 3,
    batch: 1,
    granted: false,
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
      if (index === 2) work.worker_profile = "other";
    },
  });
  f.githubClient.rest.repos.get = vi.fn(async () => ({ data: { full_name: REPOSITORY, default_branch: "release/main" } }));
  f.githubClient.rest.git = { getRef: vi.fn(async () => ({ data: { ref: "refs/heads/release/main", object: { sha: DEFAULT_REF } } })) };
  f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => {
    expect([DEFAULT_REF, REF]).toContain(ref);
    return { data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock()).toString("base64") } };
  });
  f.githubClient.rest.actions.getWorkflow = vi.fn(async ({ workflow_id }) => ({ data: { path: `.github/workflows/${workflow_id}`, state: "active" } }));
  const options = {
    githubClient: f.githubClient,
    context: { ...f.dispatcherContext, sha: BRANCH_REF },
    policyProposal: futurePolicy(f.state),
    config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"] },
    readWorkQueueLog: f.readWorkQueueLog,
    publishWorkQueueRequest: f.publishWorkQueueRequest,
  };
  const trusted = { ...f.dispatcher, authenticated: true, roles: ["dispatcher"] };
  return { f, options, trusted };
}

describe("verified default worker deployment authority", () => {
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
    expect(f.githubClient.rest.git.getRef).toHaveBeenCalledWith({ owner: "owner", repo: "repo", ref: "heads/release/main" });
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

  it("pauses incompatible pending unpinned Work without charging service or affecting an exact old pin", async () => {
    const { f, options, trusted } = fixture();
    f.githubClient.rest.repos.getContent = vi.fn(async ({ path, ref }) => ({
      data: { type: "file", path, encoding: "base64", content: Buffer.from(path.endsWith(".md") ? SOURCE : lock(ref === DEFAULT_REF ? NEXT_CONTRACT : CONTRACT)).toString("base64") },
    }));
    await synchronizeDeployments(options, trusted);
    const work = [...f.state.works.values()];
    expect(executionProfile(f.state, work[0]).reason).toBe("worker_incompatible");
    expect(executionProfile(f.state, work[1]).profile.ref).toBe(REF);
    const dispatched = f.append("dispatch_next", { pool: "default", max_claims: 3, max_dispatches: 3, max_bytes: 48000 }, f.dispatcher);
    expect(dispatched.assignments).toHaveLength(2);
    expect(f.state.stats.claims).toBe(2);
  });

  it.each(["repository", "reference", "source", "lock", "registration"])("propagates permission/rate/transport errors at %s without publishing unavailability", async endpoint => {
    for (const status of [401, 403, 429, 500, undefined]) {
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
          if (args.path.endsWith(endpoint === "source" ? ".md" : ".lock.yml")) throw failure;
          return read(args);
        });
      }
      const before = canonical(f.transactions);
      await expect(synchronizeDeployments(options, trusted)).rejects.toBe(failure);
      expect(canonical(f.transactions)).toBe(before);
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
