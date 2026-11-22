// @ts-check
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
// The real safe-output manager writes its manifests under /tmp/gh-aw; keep them in a scratch root.
const scratch = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-continuation-root-"));
const constants = require("./constants.cjs");
const originalConstants = { ...constants };
for (const [key, value] of Object.entries(constants)) {
  if (typeof value === "string" && value.startsWith("/tmp/gh-aw/")) constants[key] = path.join(scratch, value.slice("/tmp/gh-aw/".length));
}
const manager = require("./safe_output_handler_manager.cjs");
const h = require("./work_queue_test_helpers.cjs");
const { workerContext, workerPolicy, workerQueue } = require("./work_queue_worker.test_helpers.cjs");
const { main: finish } = require("./finish_work_queue_claim.cjs");
const { dispatchQueueIntent } = require("./work_queue_dispatch.cjs");
const { createCompilerDependencyResolver } = require("./work_queue_control_adapter.cjs");
const { parseTransactionLog, replayTransactionLog } = require("./work_queue_replay.cjs");
const { assignmentForDispatch } = require("./work_queue_scheduler.cjs");
const { continuationChild, gateKey } = require("./work_queue_graph.cjs");
const { loadWorkQueueSnapshot, readWorkQueueState } = require("./work_queue_mcp_server.cjs");
const { createApprovalIssue, messageDigest, workqueueReceipt } = require("./request_continuation_routes.cjs");
const { VERSION } = require("./safe_output_authorization.cjs");
const { matchContinuations, parseContinuationReceipts, readContinuationConfig } = require("./work_queue_continuation.cjs");

const WORKER_REF = `owner/repo/${h.WORKER_WORKFLOW}@refs/heads/main`;
const DISPATCHER_REF = "owner/repo/.github/workflows/dispatcher.lock.yml@refs/heads/main";
const TASK = { task: "audit pages", effect_contract: { kind: "none" } };
const WRITABLE_TASK = { task: "audit pages" };
const MESSAGE = "Audit page 2";
const APPROVAL = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "55", number: "5" };
const WRITER = { login: "octo-writer", id: 4242, type: "User" };
const dispatcherContext = { repo: { owner: "owner", repo: "repo" }, runId: 100, runAttempt: 1, payload: {}, ref: "refs/heads/main" };
// Each dispatcher run is a distinct actor, so its dispatch_next request is fresh.
let dispatcherRun = 1000;

const directories = [];

function tempDir() {
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-continuation-"));
  directories.push(directory);
  return directory;
}

function core() {
  return { info: vi.fn(), warning: vi.fn(), debug: vi.fn(), setSecret: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) } };
}

function policy(overrides = {}) {
  return { ...workerPolicy(), ...overrides };
}

function ledgerState(queue) {
  return replayTransactionLog(queue.fake.contents());
}

function ledgerLog(queue) {
  return parseTransactionLog(queue.fake.contents());
}

function lastCommit(queue) {
  const { transactions } = ledgerState(queue);
  return transactions[transactions.length - 1];
}

function continuationWorks(state) {
  return [...state.works.values()].filter(work => work.continuation);
}

/**
 * One routed worker attempt: the agent output and its bound evidence, the finish
 * intents, then (with `manager`) the real Claim-partitioned `Process Safe Outputs`
 * pass whose `work_queue_continuations` output feeds the finalizer; otherwise the
 * given `receipts` (null leaves the output unset). Only GitHub I/O is substituted.
 */
async function runFinish(queue, options = {}) {
  const { route = "workqueue", maxDepth = 1, items = [{ type: "request_continuation", message: MESSAGE, claim_handle: "h1" }], outcome = "completed", context = queue.context, finishPolicy = queue.policy } = options;
  const outcomes = options.outcomes ?? (outcome ? { h1: outcome } : {});
  const directory = tempDir();
  const agentOutput = path.join(directory, "agent_output.json");
  const bytes = Buffer.from(JSON.stringify({ items: items.map(item => ({ ...item, timestamp: "2026-11-22T00:00:00.000Z" })), errors: [] }));
  fs.writeFileSync(agentOutput, bytes);
  const sha = crypto.createHash("sha256").update(bytes).digest("hex");
  const intents = path.join(directory, "finish.jsonl");
  const effects = path.join(directory, "effects.jsonl");
  const staged = Object.entries(outcomes).map(([handle, value]) => `${JSON.stringify({ version: 4, claim_handle: handle, outcome: value })}\n`);
  if (staged.length) fs.writeFileSync(intents, staged.join(""));
  vi.stubEnv("GITHUB_WORKFLOW_REF", WORKER_REF);
  vi.stubEnv("GH_AW_CONTINUATION_ROUTE", route);
  vi.stubEnv("GH_AW_CONTINUATION_MAX_DEPTH", String(maxDepth));
  vi.stubEnv("GH_AW_AGENT_OUTPUT", options.noOutput ? path.join(directory, "missing.json") : agentOutput);
  vi.stubEnv("GH_AW_CONTINUATION_EVIDENCE", options.evidence ?? (options.noOutput ? "" : `${context.runId}:${context.runAttempt}:${sha}`));
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify(options.handlerConfig ?? { request_continuation: { max: 1, route, ...(route === "issue" ? {} : { max_depth: maxDepth }) } }));
  let managed;
  if (options.manager) {
    managed = await runManager(queue, { directory, intents, effects, context, policy: finishPolicy, github: options.manager.github });
    vi.stubEnv("GH_AW_WORK_QUEUE_CONTINUATIONS", managed.receipts === undefined ? "" : JSON.stringify(managed.receipts));
  } else {
    vi.stubEnv("GH_AW_WORK_QUEUE_CONTINUATIONS", options.receipts === null ? "" : JSON.stringify(options.receipts ?? []));
  }
  const coreApi = options.core ?? core();
  const result = await finish({ githubClient: queue.githubClient, context, core: coreApi, policy: finishPolicy, finishIntentPath: intents, effectsPath: effects, authorization: options.authorization });
  return { result, core: coreApi, managed };
}

/**
 * The real handler manager's Claim-partitioned pass for this worker attempt: the
 * trusted activation snapshot of the current ledger, the default Claim authorizer
 * against the fake ledger, and `github` for the Claim-scoped effect client.
 */
async function runManager(queue, { directory, intents, effects, context, policy: compiled, github = { rest: {} } }) {
  const snapshot = path.join(directory, "snapshot.json");
  const state = replayTransactionLog(queue.fake.contents());
  fs.writeFileSync(snapshot, JSON.stringify({ version: 4, sha: "head", transactionLog: queue.fake.contents(), origin: { role: "worker", repository: "owner/repo" }, worker: assignmentForDispatch(state, queue.dispatchId), role: "worker" }));
  vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
  vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", "worker");
  vi.stubEnv("GH_AW_WORK_QUEUE_SNAPSHOT", snapshot);
  vi.stubEnv("GH_AW_WORK_QUEUE_EFFECTS", effects);
  vi.stubEnv("GH_AW_WORK_QUEUE_FINISH_INTENT", intents);
  vi.stubEnv("GH_AW_WORK_QUEUE_POLICY", JSON.stringify(compiled));
  vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
  vi.stubEnv("GITHUB_RUN_ID", String(context.runId));
  vi.stubEnv("GITHUB_RUN_ATTEMPT", String(context.runAttempt));
  vi.stubEnv("GITHUB_SERVER_URL", "https://github.com");
  vi.stubEnv("RUNNER_TEMP", directory);
  const coreApi = { ...core(), debug: vi.fn(), error: vi.fn(), notice: vi.fn() };
  vi.stubGlobal("core", coreApi);
  vi.stubGlobal("github", github);
  vi.stubGlobal("context", { ...context, serverUrl: "https://github.com", eventName: "workflow_dispatch", payload: { ...context.payload, repository: { full_name: "owner/repo", id: 1 } } });
  try {
    const result = await manager.main({ queueClient: queue.githubClient });
    const emitted = coreApi.setOutput.mock.calls.filter(([key]) => key === "work_queue_continuations").at(-1);
    return { result, core: coreApi, receipts: emitted ? JSON.parse(emitted[1]) : undefined };
  } finally {
    // The finalizer and later dispatcher jobs run in their own environments.
    for (const name of ["GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_WORK_QUEUE_SNAPSHOT", "GH_AW_WORK_QUEUE_EFFECTS", "GH_AW_WORK_QUEUE_FINISH_INTENT", "GH_AW_WORK_QUEUE_POLICY"]) vi.stubEnv(name, undefined);
    vi.unstubAllGlobals();
  }
}

/** The manager's workqueue receipt for the assigned Claim, computed by the routes module. */
function readyReceipt(assignment, message = MESSAGE, maxDepth = 1) {
  return workqueueReceipt({ member: assignment.claims[0], request: { message, message_sha256: messageDigest(message) }, maxDepth });
}

function issueReceipt(assignment, message = MESSAGE, issue = APPROVAL) {
  return { claim_handle: "h1", claim_id: assignment.claims[0].claim_id, message_sha256: messageDigest(message), status: "ready", issue };
}

/** Dispatch, launch and bind the next available Work to a worker run; returns that run's view. */
function nextWorker(queue, runId) {
  const planned = h.dispatchNext(ledgerLog(queue), { max_claims: 1, max_dispatches: 1 }, { ...h.dispatcher, run_id: String(dispatcherRun++) });
  const dispatchId = planned.decision.dispatches[0];
  if (!dispatchId) return null;
  const log = h.bind(h.launch(planned.log, dispatchId), dispatchId, h.workerActor(runId, 1));
  queue.fake.install(h.ledgerText(log));
  return { ...queue, dispatchId, context: workerContext(dispatchId, runId, 1), assignment: assignmentForDispatch(replayTransactionLog(h.ledgerText(log)), dispatchId) };
}

function issueReader({ data, permission = { permission: "write", role_name: "write", user: { login: WRITER.login, id: WRITER.id } } }) {
  const reads = { issues: 0, permissions: 0 };
  const reader = {
    rest: {
      repos: {
        get: async () => ({ status: 200, data: { full_name: "owner/repo", id: 1 } }),
        getCollaboratorPermissionLevel: async input => {
          reads.permissions++;
          if (permission instanceof Error) throw permission;
          expect(input.username).toBe(WRITER.login);
          return { status: 200, data: permission };
        },
      },
      issues: {
        get: async input => {
          reads.issues++;
          if (data instanceof Error) throw data;
          return { status: 200, data: typeof data === "function" ? data(input) : data };
        },
      },
    },
  };
  return { reader, reads };
}

const closedBy = (closer, extra = {}) => ({ id: 55, number: 5, state: "closed", state_reason: "completed", closed_by: closer, ...extra });

async function dispatch(queue, { reader = undefined, maxClaims = 1, poolPolicy = queue.policy } = {}) {
  vi.stubEnv("GITHUB_WORKFLOW_REF", DISPATCHER_REF);
  const posts = vi.fn(async () => ({ status: 204 }));
  const receipt = await dispatchQueueIntent({
    githubClient: queue.githubClient,
    dispatchClient: { rest: { actions: { createWorkflowDispatch: posts } } },
    context: { ...dispatcherContext, runId: dispatcherRun++ },
    core: core(),
    policy: poolPolicy,
    ...(reader ? { dependencyResolver: createCompilerDependencyResolver({ context: dispatcherContext, env: {}, githubClient: reader }) } : {}),
    remainingDispatches: maxClaims,
    message: { intent_id: "dispatch", pool: "default", max_claims: maxClaims, max_dispatches: maxClaims },
  });
  return { receipt, posts };
}

beforeEach(() => {
  vi.stubEnv("GH_AW_PROMPTS_DIR", path.resolve(import.meta.dirname, "../md"));
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "");
  vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "");
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

afterAll(() => {
  Object.assign(constants, originalConstants);
  fs.rmSync(scratch, { recursive: true, force: true });
});

describe("workqueue continuation end to end", () => {
  it("completes the parent and admits one successor in a single finish commit, then dispatches it with the message", async () => {
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    const parent = ledgerState(queue).works.get(queue.assignment.claims[0].work_id);
    const github = { rest: { issues: { create: vi.fn() }, actions: { createWorkflowDispatch: vi.fn() } } };
    const { result, managed } = await runFinish(queue, { manager: { github } });
    expect(managed.result.settlements).toEqual([expect.objectContaining({ claim_handle: "h1", success: true })]);
    expect(managed.core.setFailed).not.toHaveBeenCalled();
    expect(managed.receipts).toEqual([readyReceipt(queue.assignment)]);
    expect(github.rest.issues.create).not.toHaveBeenCalled();
    expect(github.rest.actions.createWorkflowDispatch).not.toHaveBeenCalled();

    const child = continuationChild(parent, { route: "workqueue", message: MESSAGE, remaining: 0 }, undefined);
    expect(lastCommit(queue).request).toMatchObject({ kind: "finish", parameters: { dispatch_id: queue.dispatchId } });
    expect(lastCommit(queue).operations).toEqual([{ kind: "Completion", work_id: parent.work_id, claim_id: queue.assignment.claims[0].claim_id, dispatch_id: queue.dispatchId, claim_handle: "h1", effects: [] }, child]);
    expect(child).toMatchObject({ payload: TASK, depends_on: [{ kind: "work", work_id: parent.work_id }], continuation: { route: "workqueue", message: MESSAGE, remaining: 0 } });
    expect(child).not.toHaveProperty("backing_issue");
    expect(result).toMatchObject({ outcome: "completed", committed: true, continuations: [{ claim_handle: "h1", status: "admitted", work_id: child.work_id }] });

    const { receipt, posts } = await dispatch(queue);
    expect(receipt.dispatches).toBe(1);
    expect(posts).toHaveBeenCalledTimes(1);
    const state = ledgerState(queue);
    expect(state.works.get(child.work_id).state).toBe("claimed");
    const dispatchId = [...state.dispatches.values()].find(entry => entry.request_id === receipt.request_id).dispatch_id;
    const bound = h.bind(ledgerLog(queue), dispatchId, h.workerActor("201", 1));
    const snapshotPath = path.join(tempDir(), "snapshot.json");
    fs.writeFileSync(
      snapshotPath,
      JSON.stringify({ version: 4, sha: "head", transactionLog: h.ledgerText(bound), origin: h.dispatcher, role: "worker", worker: assignmentForDispatch(replayTransactionLog(h.ledgerText(bound)), dispatchId) })
    );
    const view = readWorkQueueState(loadWorkQueueSnapshot(snapshotPath), {});
    expect(view.assignment.claims).toEqual([
      {
        claim_handle: "h1",
        claim_id: expect.any(String),
        work_id: child.work_id,
        work: TASK,
        result_refs: [{ work_id: parent.work_id, result_commit_id: state.works.get(parent.work_id).completion_id, descriptor: [] }],
        continuation: { route: "workqueue", message: MESSAGE, remaining: 0 },
      },
    ]);
  });

  it("re-plans after a CAS conflict and recovers a lost acknowledgement without a second child", async () => {
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK }), h.work("n2")] });
    const concurrent = h.submit(ledgerLog(queue), [h.work("n3")]);
    queue.fake.state.beforeUpdate = () => queue.fake.install(h.ledgerText(concurrent));
    await runFinish(queue, { receipts: [readyReceipt(queue.assignment)] });
    expect(continuationWorks(ledgerState(queue))).toHaveLength(1);
    expect(ledgerState(queue).works.has(h.work("n3").work_id)).toBe(true);

    const lost = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    lost.fake.state.ambiguousOnce = true;
    const { result } = await runFinish(lost, { receipts: [readyReceipt(lost.assignment)] });
    expect(result).toMatchObject({ committed: true, continuations: [{ status: "admitted" }] });
    const state = ledgerState(lost);
    expect(continuationWorks(state)).toHaveLength(1);
    expect(state.transactions.filter(commit => commit.request.kind === "finish")).toHaveLength(1);
    // A rerun of the finish step finds the committed request: recovery is a read.
    const again = await runFinish(lost, { receipts: [readyReceipt(lost.assignment)] });
    expect(again.result.committed).toBe(true);
    expect(continuationWorks(ledgerState(lost))).toHaveLength(1);
  });

  it("replaces the completed parent with its successor at pending_limit 1 but never exceeds the limit", async () => {
    const tight = policy({ pending_limit: 1 });
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    await runFinish(queue, { receipts: [readyReceipt(queue.assignment)], finishPolicy: tight });
    expect(continuationWorks(ledgerState(queue))).toHaveLength(1);

    const full = workerQueue({ works: [h.work("n1", { payload: TASK }), h.work("n2")] });
    const before = full.fake.contents();
    await expect(runFinish(full, { receipts: [readyReceipt(full.assignment)], finishPolicy: tight })).rejects.toMatchObject({ code: "work_queue_pending_limit" });
    expect(full.fake.contents()).toBe(before);
  });

  it("decrements the budget along a depth-2 chain and keeps the zero-budget request observable without Work", async () => {
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    await runFinish(queue, { maxDepth: 2, receipts: [readyReceipt(queue.assignment, MESSAGE, 2)] });
    expect(continuationWorks(ledgerState(queue)).map(work => work.continuation.remaining)).toEqual([1]);

    const second = nextWorker(queue, "201");
    expect(second.assignment.claims[0].continuation).toEqual({ route: "workqueue", message: MESSAGE, remaining: 1 });
    await runFinish(second, { maxDepth: 2, items: [{ type: "request_continuation", message: "Audit page 3" }], receipts: [readyReceipt(second.assignment, "Audit page 3", 2)] });
    expect(continuationWorks(ledgerState(queue)).map(work => work.continuation.remaining)).toEqual([1, 0]);

    const third = nextWorker(queue, "202");
    expect(third.assignment.claims[0].continuation.remaining).toBe(0);
    // A configured budget of 16 cannot reset an inbound zero.
    const zero = readyReceipt(third.assignment, "Audit page 4", 16);
    expect(zero.status).toBe("depth_limit");
    await expect(runFinish(third, { maxDepth: 16, items: [{ type: "request_continuation", message: "Audit page 4" }], receipts: [{ ...zero, status: "ready" }] })).rejects.toMatchObject({ code: "continuation_receipt_mismatch" });
    const { result } = await runFinish(third, { maxDepth: 16, items: [{ type: "request_continuation", message: "Audit page 4" }], receipts: [zero] });
    expect(result).toMatchObject({ outcome: "completed", continuations: [{ claim_handle: "h1", status: "depth_limit" }] });
    expect(lastCommit(queue).operations.map(operation => operation.kind)).toEqual(["Completion"]);
    expect(continuationWorks(ledgerState(queue))).toHaveLength(2);
  });

  it("routes each Claim's own request through the real manager into its own successor", async () => {
    // The scheduler emits one Claim per Dispatch; the protocol also admits a Dispatch grouping
    // compatible Claims of one dispatch_next commit, which is the multi-Claim worker case.
    const submitted = h.submit([], [h.work("n1", { payload: TASK }), h.work("n2", { payload: TASK })]);
    const parameters = { pool: "default", max_claims: 2, max_dispatches: 2, concurrency: 16, per_account_limit: 0, profiles: h.PROFILES };
    const planned = h.dispatchNext(submitted, parameters).log.at(-1).operations;
    const dispatchId = planned[0].dispatch_id;
    const grouped = planned.map((operation, index) => ({ ...operation, dispatch_id: dispatchId, handle: `h${index + 1}` }));
    let log = h.commit(submitted, h.dispatcher, "dispatch_next", parameters, grouped);
    log = h.bind(h.launch(log, dispatchId), dispatchId, h.workerActor("200", 1));
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const assignment = assignmentForDispatch(replayTransactionLog(h.ledgerText(log)), dispatchId);
    const queue = { fake, dispatchId, githubClient: fake.githubClient, context: workerContext(dispatchId, "200", 1), assignment, policy: workerPolicy() };
    expect(assignment.claims.map(claim => claim.handle)).toEqual(["h1", "h2"]);
    const items = [
      { type: "request_continuation", message: "Second Claim work", claim_handle: "h2" },
      { type: "request_continuation", message: "First Claim work", claim_handle: "h1" },
    ];
    const { result, managed } = await runFinish(queue, { items, outcomes: { h1: "completed", h2: "completed" }, manager: {} });
    expect(managed.result.settlements.map(settlement => [settlement.claim_handle, settlement.success])).toEqual([
      ["h1", true],
      ["h2", true],
    ]);
    expect(managed.receipts).toEqual([
      { claim_handle: "h1", claim_id: assignment.claims[0].claim_id, message_sha256: messageDigest("First Claim work"), status: "ready" },
      { claim_handle: "h2", claim_id: assignment.claims[1].claim_id, message_sha256: messageDigest("Second Claim work"), status: "ready" },
    ]);
    expect(result).toMatchObject({
      committed: true,
      continuations: [
        { claim_handle: "h1", status: "admitted" },
        { claim_handle: "h2", status: "admitted" },
      ],
    });
    const operations = lastCommit(queue).operations;
    expect(operations.map(operation => operation.kind)).toEqual(["Completion", "Work", "Completion", "Work"]);
    for (const [index, member] of assignment.claims.entries()) {
      const [completion, child] = operations.slice(index * 2, index * 2 + 2);
      expect(completion).toMatchObject({ kind: "Completion", claim_handle: member.handle, work_id: member.work_id });
      expect(child).toMatchObject({ depends_on: [{ kind: "work", work_id: member.work_id }], continuation: { route: "workqueue", message: index === 0 ? "First Claim work" : "Second Claim work", remaining: 0 } });
    }
  });

  it("binds requests to their own Claim handle", async () => {
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    const before = queue.fake.contents();
    await expect(runFinish(queue, { items: [{ type: "request_continuation", message: MESSAGE, claim_handle: "h2" }], receipts: [readyReceipt(queue.assignment)] })).rejects.toThrow(/continuation_request_invalid/);
    expect(queue.fake.contents()).toBe(before);

    const claims = [
      { handle: "h1", claim_id: "c1" },
      { handle: "h2", claim_id: "c2" },
    ];
    const requests = new Map([
      ["h1", { message: "one", message_sha256: messageDigest("one"), index: 0 }],
      ["h2", { message: "two", message_sha256: messageDigest("two"), index: 1 }],
    ]);
    const intents = new Map([
      ["h1", "completed"],
      ["h2", "completed"],
    ]);
    const config = { route: "workqueue", maxDepth: 1 };
    const receipt = (handle, claimId, message) => ({ claim_handle: handle, claim_id: claimId, message_sha256: messageDigest(message), status: "ready" });
    expect(() => matchContinuations({ config, requests, intents, claims, receipts: [receipt("h1", "c1", "two"), receipt("h2", "c2", "two")] })).toThrow(/digest/);
    expect(() => matchContinuations({ config, requests, intents, claims, receipts: [receipt("h1", "c2", "one")] })).toThrow(/original Claim/);
    const matched = matchContinuations({ config, requests, intents, claims, receipts: [receipt("h1", "c1", "one"), receipt("h2", "c2", "two")] });
    expect([...matched].map(([handle, entry]) => [handle, entry?.message])).toEqual([
      ["h1", "one"],
      ["h2", "two"],
    ]);
    expect(matchContinuations({ config, requests, intents, claims, receipts: [receipt("h2", "c2", "two")] }).get("h1")).toBeNull();
  });

  it("suppresses the request of a failed or missing finish", async () => {
    for (const outcome of ["failed", null]) {
      const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
      const { result } = await runFinish(queue, { outcome, receipts: [] });
      expect(result.outcome).toBe("failed");
      expect(lastCommit(queue).operations.map(operation => operation.kind)).toEqual(["ClaimCancellation"]);
      expect(continuationWorks(ledgerState(queue))).toHaveLength(0);
    }
  });

  it("never silently completes a parent whose valid request lacks a matching receipt", async () => {
    const cases = [
      { name: "missing receipt (refused contract or failed creation)", receipts: [], code: "continuation_receipt_missing" },
      { name: "unset receipts", receipts: null, code: "continuation_receipt_missing" },
      { name: "hash mismatch", receipts: assignment => [{ ...readyReceipt(assignment), message_sha256: messageDigest("Audit page 9") }], code: "continuation_receipt_mismatch" },
      { name: "foreign Claim ID", receipts: assignment => [{ ...readyReceipt(assignment), claim_id: "c_foreign" }], code: "continuation_receipt_mismatch" },
      { name: "route mismatch", receipts: assignment => [{ ...readyReceipt(assignment), issue: APPROVAL }], code: "continuation_receipt_mismatch" },
    ];
    for (const entry of cases) {
      const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
      const before = queue.fake.contents();
      const receipts = typeof entry.receipts === "function" ? entry.receipts(queue.assignment) : entry.receipts;
      await expect(runFinish(queue, { receipts }), entry.name).rejects.toMatchObject({ code: entry.code });
      expect(queue.fake.contents(), entry.name).toBe(before);
    }
  });

  it("rejects tampered or foreign-attempt evidence and finishes normally without any agent output", async () => {
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    const before = queue.fake.contents();
    await expect(runFinish(queue, { receipts: [readyReceipt(queue.assignment)], evidence: `200:1:${"0".repeat(64)}` })).rejects.toThrow(/continuation_evidence_invalid/);
    await expect(runFinish(queue, { receipts: [readyReceipt(queue.assignment)], evidence: `200:2:${"0".repeat(64)}` })).rejects.toThrow(/continuation_evidence_invalid/);
    await expect(runFinish(queue, { receipts: [readyReceipt(queue.assignment)], evidence: "" })).rejects.toMatchObject({ code: "continuation_evidence_invalid" });
    expect(queue.fake.contents()).toBe(before);

    // A run that failed before recording a request (e.g. exhausted token budget) keeps its normal recovery.
    const { result } = await runFinish(queue, { noOutput: true, receipts: null });
    expect(result).toMatchObject({ outcome: "completed", continuations: [] });
    expect(continuationWorks(ledgerState(queue))).toHaveLength(0);
  });

  it("produces no child from a stale run binding", async () => {
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    queue.fake.install(h.ledgerText(h.release(ledgerLog(queue), queue.dispatchId, "run_missing")));
    const { result } = await runFinish(queue, { receipts: [readyReceipt(queue.assignment)] });
    expect(result).toMatchObject({ committed: false, continuations: [] });
    expect(continuationWorks(ledgerState(queue))).toHaveLength(0);

    const rerun = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    const before = rerun.fake.contents();
    await expect(runFinish(rerun, { context: workerContext(rerun.dispatchId, "200", 2), receipts: [] })).rejects.toThrow();
    expect(rerun.fake.contents()).toBe(before);
  });

  it("rejects an oversized Unicode successor and a profile removed from the pool", async () => {
    const big = { task: "é".repeat(7000) };
    const message = "ü".repeat(1200);
    const queue = workerQueue({ works: [h.work("n1", { payload: big })] });
    const before = queue.fake.contents();
    const receipt = { claim_handle: "h1", claim_id: queue.assignment.claims[0].claim_id, message_sha256: messageDigest(message), status: "ready" };
    await expect(runFinish(queue, { items: [{ type: "request_continuation", message }], receipts: [receipt] })).rejects.toMatchObject({ code: expect.stringMatching(/resource_limit|assignment_limit/) });
    expect(queue.fake.contents()).toBe(before);

    const moved = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    const profiles = { other: { workflow: h.WORKER_WORKFLOW, effect_scope: "owner/repo" } };
    const removed = policy({ pools: { default: { ...workerPolicy().pools.default, default_profile: "other", profiles } } });
    await expect(runFinish(moved, { receipts: [readyReceipt(moved.assignment)], finishPolicy: removed })).rejects.toMatchObject({ code: "continuation_destination_invalid" });
  });

  it("validates the compiled route and receipt shapes", () => {
    expect(readContinuationConfig({})).toBeNull();
    expect(readContinuationConfig({ GH_AW_CONTINUATION_ROUTE: "issue" })).toEqual({ route: "issue", maxDepth: 1 });
    for (const env of [{ GH_AW_CONTINUATION_ROUTE: "dispatch" }, { GH_AW_CONTINUATION_ROUTE: "workqueue", GH_AW_CONTINUATION_MAX_DEPTH: "17" }]) expect(() => readContinuationConfig(env)).toThrow();
    expect(parseContinuationReceipts("")).toBeUndefined();
    const ok = { claim_handle: "h1", claim_id: "c1", message_sha256: "a".repeat(64), status: "ready" };
    expect(parseContinuationReceipts(JSON.stringify([ok]))).toEqual([ok]);
    for (const bad of [
      { ...ok, extra: 1 },
      { ...ok, status: "done" },
      { ...ok, status: "depth_limit", issue: APPROVAL },
      { ...ok, issue: { ...APPROVAL, kind: "pull_request" } },
    ]) {
      expect(() => parseContinuationReceipts(JSON.stringify([bad]))).toThrow(/continuation_receipt_invalid|receipt/);
    }
    expect(() => parseContinuationReceipts(JSON.stringify([ok, ok]))).toThrow();
  });
});

describe("issue-gated continuation end to end", () => {
  /** GitHub I/O of the worker's safe-output job: the trusted repository and the one approval Issue POST. */
  function issueGitHub(create = async () => ({ status: 201, data: { id: 55, number: 5, html_url: "https://github.com/owner/repo/issues/5" } })) {
    return { rest: { repos: { get: vi.fn(async () => ({ data: { id: 1, full_name: "owner/repo" } })) }, issues: { create: vi.fn(create) } } };
  }

  async function gatedQueue(extraWorks = [], options = {}) {
    const queue = workerQueue({ works: [h.work("n1", { payload: WRITABLE_TASK }), ...extraWorks] });
    if (options.finish === false) {
      // The approval Issue POST happened, then the job crashed before its finish step.
      const create = vi.fn(async () => ({ status: 201, data: { id: 55, number: 5, html_url: "https://github.com/owner/repo/issues/5" } }));
      const created = await createApprovalIssue({
        client: { rest: { repos: { get: async () => ({ data: { id: 1, full_name: "owner/repo" } }) }, issues: { create } } },
        context: queue.context,
        member: queue.assignment.claims[0],
        request: { message: MESSAGE },
      });
      expect(created.resource).toEqual(APPROVAL);
      expect(create).toHaveBeenCalledTimes(1);
      return { queue, created };
    }
    const github = issueGitHub();
    const { managed } = await runFinish(queue, { route: "issue", manager: { github }, ...options });
    expect(managed.result.settlements).toEqual([expect.objectContaining({ claim_handle: "h1", success: true })]);
    expect(managed.receipts).toEqual([issueReceipt(queue.assignment, MESSAGE, APPROVAL)]);
    expect(github.rest.issues.create).toHaveBeenCalledTimes(1);
    const [parameters] = github.rest.issues.create.mock.calls[0];
    expect(parameters).toMatchObject({ owner: "owner", repo: "repo", request: { retries: 0 } });
    expect(parameters.body).toContain(MESSAGE);
    return { queue, created: { resource: managed.receipts[0].issue, number: 5 } };
  }

  it("admits an unschedulable successor pinned to the created approval Issue", async () => {
    const { queue } = await gatedQueue();
    const state = ledgerState(queue);
    const parent = state.works.get(queue.assignment.claims[0].work_id);
    const [child] = continuationWorks(state);
    expect(parent.state).toBe("completed");
    expect(child).toMatchObject({
      state: "available",
      depends_on: [
        { kind: "work", work_id: parent.work_id },
        { kind: "issue", resource: APPROVAL, condition: "approved" },
      ],
      continuation: { route: "issue", message: MESSAGE, remaining: 0 },
    });
    expect(child).not.toHaveProperty("backing_issue");
    const { receipt, posts } = await dispatch(queue);
    expect(receipt.dispatches).toBe(0);
    expect(posts).not.toHaveBeenCalled();
  });

  it.each([
    ["open", { data: { id: 55, number: 5, state: "open" } }],
    ["closed as not planned", { data: { id: 55, number: 5, state: "closed", state_reason: "not_planned", closed_by: WRITER } }],
    ["closed by a bot", { data: closedBy({ login: "app[bot]", id: 9, type: "Bot" }) }],
    ["closed by a read collaborator", { data: closedBy(WRITER), permission: { permission: "read", role_name: "read", user: { login: WRITER.login, id: WRITER.id } } }],
    ["closed by a triage collaborator", { data: closedBy(WRITER), permission: { permission: "read", role_name: "triage", user: { login: WRITER.login, id: WRITER.id } } }],
    ["closed with an unreadable permission", { data: closedBy(WRITER), permission: Object.assign(new Error("Forbidden"), { status: 403 }) }],
    ["inaccessible", { data: Object.assign(new Error("Not Found"), { status: 404 }) }],
    ["a different Issue identity", { data: closedBy(WRITER, { id: 56 }) }],
  ])("launches nothing when the approval Issue is %s", async (_name, scenario) => {
    const { queue } = await gatedQueue();
    const { reader } = issueReader(scenario);
    const { receipt, posts } = await dispatch(queue, { reader });
    expect(receipt.dispatches).toBe(0);
    expect(posts).not.toHaveBeenCalled();
    const state = ledgerState(queue);
    expect(continuationWorks(state)[0].state).toBe("available");
    expect([...state.observations.values()].filter(observation => observation.state === "ready")).toEqual([]);
  });

  it("claims the successor in the same scheduler commit as a writer's approval, exactly once", async () => {
    const { queue } = await gatedQueue();
    const { reader, reads } = issueReader({ data: closedBy(WRITER) });
    const { receipt, posts } = await dispatch(queue, { reader });
    expect(receipt.dispatches).toBe(1);
    expect(posts).toHaveBeenCalledTimes(1);
    expect(reads).toEqual({ issues: 1, permissions: 1 });
    const commit = ledgerState(queue).transactions.find(entry => entry.request.id === receipt.request_id);
    expect(commit.operations.map(operation => operation.kind)).toEqual(["Observation", "Claim"]);
    expect(commit.operations[0]).toMatchObject({ resource: APPROVAL, condition: "approved", state: "ready", state_reason: "completed", approved_by: "4242" });
    expect(commit.operations[1].work_id).toBe(continuationWorks(ledgerState(queue))[0].work_id);

    // Polling again (or a reopen/reclose) creates no Work and no extra grant.
    const again = await dispatch(queue, { reader });
    expect(again.receipt.dispatches).toBe(0);
    expect(continuationWorks(ledgerState(queue))).toHaveLength(1);
    expect(reads.issues).toBe(1);
  });

  it("defers a new approval while pool capacity is full, then grants it with the Claim", async () => {
    const { queue } = await gatedQueue([h.work("busy")]);
    const narrow = policy({ pools: { default: { ...workerPolicy().pools.default, concurrency: 1 } } });
    expect((await dispatch(queue, { poolPolicy: narrow })).receipt.dispatches).toBe(1);
    const { reader } = issueReader({ data: closedBy(WRITER) });
    const blocked = await dispatch(queue, { reader, poolPolicy: narrow });
    expect(blocked.receipt.dispatches).toBe(0);
    expect([...ledgerState(queue).observations.values()].filter(observation => observation.condition === "approved")).toEqual([]);

    const later = await dispatch(queue, { reader });
    expect(later.receipt.dispatches).toBe(1);
    expect(continuationWorks(ledgerState(queue))[0].state).toBe("claimed");
  });

  it("never treats a cached ordinary completed observation of the same Issue as approval", async () => {
    const ordinary = h.work("ordinary", { depends_on: [{ kind: "issue", resource: APPROVAL, condition: "completed" }] });
    const { queue } = await gatedQueue([ordinary]);
    const { reader } = issueReader({ data: closedBy({ login: "app[bot]", id: 9, type: "Bot" }) });
    const { receipt } = await dispatch(queue, { reader, maxClaims: 2 });
    const state = ledgerState(queue);
    expect(receipt.dispatches).toBe(1);
    expect(state.works.get(ordinary.work_id).state).toBe("claimed");
    expect(state.observations.get(gateKey(APPROVAL, "completed"))).toMatchObject({ state: "ready" });
    expect(continuationWorks(state)[0].state).toBe("available");
  });

  it("ignores an unrelated Issue close", async () => {
    const { queue } = await gatedQueue();
    const { reader } = issueReader({ data: input => (input.issue_number === 6 ? closedBy(WRITER, { id: 66, number: 6 }) : { id: 55, number: 5, state: "open" }) });
    expect((await dispatch(queue, { reader })).receipt.dispatches).toBe(0);
  });

  it("cannot release work through an orphan approval Issue after a crash or failed finish", async () => {
    // Crash after the Issue POST, before finish: no successor pins the Issue.
    const { queue: crashed } = await gatedQueue([], { finish: false });
    const { reader } = issueReader({ data: closedBy(WRITER) });
    const crashedResult = await dispatch(crashed, { reader });
    expect(crashedResult.receipt.dispatches).toBe(0);
    expect(continuationWorks(ledgerState(crashed))).toHaveLength(0);

    // A failed creation (no valid Issue identity) leaves no receipt: the finish refuses to complete the parent.
    const failed = workerQueue({ works: [h.work("n1", { payload: WRITABLE_TASK })] });
    const before = failed.fake.contents();
    const github = issueGitHub(async () => ({ status: 201, data: { number: 5 } }));
    await expect(runFinish(failed, { route: "issue", manager: { github } })).rejects.toMatchObject({ code: "continuation_receipt_missing" });
    expect(github.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(failed.fake.contents()).toBe(before);
  });

  it("reports the known approval Issue number when finish publication fails", async () => {
    const queue = workerQueue({ works: [h.work("n1", { payload: WRITABLE_TASK })] });
    const coreApi = core();
    const tight = policy({ pending_limit: 1 });
    const crowded = h.submit(ledgerLog(queue), [h.work("n2")]);
    queue.fake.install(h.ledgerText(crowded));
    await expect(runFinish(queue, { route: "issue", receipts: [issueReceipt(queue.assignment)], finishPolicy: tight, core: coreApi })).rejects.toMatchObject({ code: "work_queue_pending_limit" });
    expect(coreApi.warning).toHaveBeenCalledWith(expect.stringContaining("#5"));
  });
});

describe("continuation admission under global authorization", () => {
  const clock = Date.parse("2026-11-22T07:00:00.000Z");
  const requirement = { endpoint: "https://authority.example.test/authorize", audience: "https://authority.example.test/gh-aw" };

  function authority({ decision = "allow", onDecision = () => {} } = {}) {
    const token = `fixture.${Buffer.from(JSON.stringify({ aud: requirement.audience, exp: clock / 1000 + 300 })).toString("base64url")}.signature`;
    const requests = [];
    const fetchImpl = vi.fn(async (_url, request) => {
      if (request.method === "GET") return Response.json({ value: token });
      const envelope = JSON.parse(request.body);
      requests.push(envelope.invocation);
      onDecision();
      return Response.json({
        version: VERSION,
        decision,
        request_digest: envelope.request_digest,
        invocation_id: envelope.invocation.invocation_id,
        run_attempt: envelope.invocation.caller.run_attempt,
        policy_revision: "policy-v1",
        decision_id: `decision-${requests.length}`,
        issued_at: new Date(clock).toISOString(),
        expires_at: new Date(clock + 15000).toISOString(),
      });
    });
    return { fetchImpl, requests };
  }

  function authorizedEnv() {
    vi.stubEnv("RUNNER_TEMP", tempDir());
    vi.stubEnv("GITHUB_SERVER_URL", "https://github.com");
    vi.stubEnv("GITHUB_REPOSITORY_ID", "1");
    vi.stubEnv("GITHUB_WORKFLOW_SHA", "a".repeat(40));
    vi.stubEnv("GH_AW_AUTHORIZATION_WORKFLOW_HASH", "b".repeat(64));
    vi.stubEnv("GITHUB_RUN_ID", "200");
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
    vi.stubEnv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://oidc.example.test/token");
    vi.stubEnv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "bootstrap");
  }

  const handlerConfig = { authorization: requirement, request_continuation: { max: 1, route: "workqueue", max_depth: 1 } };

  it("authorizes request_continuation:admit at the queue executor before publication", async () => {
    authorizedEnv();
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    const { fetchImpl, requests } = authority();
    await runFinish(queue, { receipts: [readyReceipt(queue.assignment)], handlerConfig, authorization: { fetchImpl, now: () => 0, wallNow: () => clock } });
    expect(requests).toHaveLength(1);
    expect(requests[0].output).toMatchObject({ type: "request_continuation", name: "request_continuation:admit", executor: "queue" });
    expect(requests[0].context).toMatchObject({ claim_id: queue.assignment.claims[0].claim_id, delivery_id: queue.dispatchId });
    expect(requests[0].batch.record_index).toBe(0);
    expect(JSON.stringify(requests[0])).not.toContain(MESSAGE);
    expect(continuationWorks(ledgerState(queue))).toHaveLength(1);
  });

  it("publishes nothing when the admission expires before publication", async () => {
    authorizedEnv();
    const queue = workerQueue({ works: [h.work("n1", { payload: TASK })] });
    let elapsed = 0;
    let armed = false;
    const { fetchImpl } = authority({ onDecision: () => (armed = true) });
    const getRef = queue.githubClient.rest.git.getRef;
    queue.githubClient.rest.git.getRef = async input => {
      if (armed) elapsed = 25000;
      return getRef(input);
    };
    const before = queue.fake.contents();
    await expect(runFinish(queue, { receipts: [readyReceipt(queue.assignment)], handlerConfig, authorization: { fetchImpl, now: () => elapsed, wallNow: () => clock } })).rejects.toThrow(/invalid_validity/);
    expect(queue.fake.contents()).toBe(before);
  });

  it("launches nothing after a denied admission even when its approval Issue is closed", async () => {
    authorizedEnv();
    const queue = workerQueue({ works: [h.work("n1", { payload: WRITABLE_TASK })] });
    const before = queue.fake.contents();
    const { fetchImpl } = authority({ decision: "deny" });
    const config = { authorization: requirement, request_continuation: { max: 1, route: "issue", max_depth: 1 } };
    await expect(runFinish(queue, { route: "issue", receipts: [issueReceipt(queue.assignment)], handlerConfig: config, authorization: { fetchImpl, now: () => 0, wallNow: () => clock } })).rejects.toThrow(/denied/);
    expect(queue.fake.contents()).toBe(before);
    // The parent Claim stays open for maintenance; closing the orphan Issue releases nothing.
    const { reader, reads } = issueReader({ data: closedBy(WRITER) });
    expect((await dispatch(queue, { reader })).receipt.dispatches).toBe(0);
    expect(reads).toEqual({ issues: 0, permissions: 0 });
    expect(continuationWorks(ledgerState(queue))).toHaveLength(0);
  });
});

describe("same-repository approval read facade", () => {
  it("exposes the collaborator permission read only on the job's own-repository client", async () => {
    const calls = [];
    const own = { rest: { repos: { get: async () => ({}), getCollaboratorPermissionLevel: async input => (calls.push(input), { status: 200, data: {} }) }, issues: { get: async () => ({}) }, pulls: { get: async () => ({}) } } };
    const resolver = createCompilerDependencyResolver({ context: dispatcherContext, env: {}, githubClient: own });
    const client = await resolver.getClient({ host: "github.com", repository: "owner/repo" });
    await client.rest.repos.getCollaboratorPermissionLevel({ owner: "owner", repo: "repo", username: "octo-writer" });
    expect(calls[0]).toMatchObject({ owner: "owner", repo: "repo", username: "octo-writer", request: { retries: 0 } });
    for (const input of [
      { owner: "other", repo: "repo", username: "octo-writer" },
      { owner: "owner", repo: "repo", username: "app[bot]" },
      { owner: "owner", repo: "repo", username: "octo", permission: "admin" },
    ]) {
      await expect(client.rest.repos.getCollaboratorPermissionLevel(input)).rejects.toThrow(/scope_invalid/);
    }
    expect(Object.keys(client.rest.repos).sort()).toEqual(["get", "getCollaboratorPermissionLevel"]);
    expect(client.rest.issues).not.toHaveProperty("update");

    const env = { GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS: JSON.stringify({ "foreign/design": "GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0" }), GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0: "token" };
    const external = createCompilerDependencyResolver({ context: dispatcherContext, env, githubClient: own, getOctokit: () => own });
    expect((await external.getClient({ host: "github.com", repository: "foreign/design" })).rest.repos).not.toHaveProperty("getCollaboratorPermissionLevel");
  });
});
