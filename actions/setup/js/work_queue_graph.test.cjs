import { afterEach, describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { ASSIGNMENT_BYTES, EFFECTS_BYTES, GRAPH_NODES, PAYLOAD_BYTES, PREDECESSORS } = require("./work_queue_limits.cjs");
const { continuationChild, dependencyStatus, gateKey, nodeId, resultReferences, validateContinuation, validateGraphAdmission, validateResource, validateWork } = require("./work_queue_graph.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");

const fails = (callback, code) => expect(callback).toThrow(expect.objectContaining({ code }));
const issue = (id, extra = {}) => ({ kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: String(id), number: String(id), ...extra });
const gate = (id, condition = "completed") => ({ kind: "issue", resource: issue(id), condition });
const after = work => ({ kind: "work", work_id: work.work_id });
const previousServer = process.env.GITHUB_SERVER_URL;

afterEach(() => {
  if (previousServer === undefined) delete process.env.GITHUB_SERVER_URL;
  else process.env.GITHUB_SERVER_URL = previousServer;
});

describe("typed resources and gates", () => {
  it("requires the configured GitHub server host and opaque decimal identities", () => {
    for (const [server, host] of [
      [undefined, "github.com"],
      ["https://contoso.ghe.com/", "contoso.ghe.com"],
    ]) {
      if (server === undefined) delete process.env.GITHUB_SERVER_URL;
      else process.env.GITHUB_SERVER_URL = server;
      const resource = issue(2, { host });
      expect(validateResource(resource)).toBe(resource);
      fails(() => validateResource({ ...resource, host: "other.example" }), "resource_unauthorized");
    }
    delete process.env.GITHUB_SERVER_URL;
    expect(validateResource(issue(2, { repository_id: "9".repeat(256) }))).toBeTruthy();
    for (const invalid of [issue(2, { number: 42 }), issue(2, { number: "042" }), issue(2, { repository_id: "0" }), issue(2, { kind: "work" }), { ...issue(2), extra: 1 }]) expect(() => validateResource(invalid)).toThrow();
  });

  it("keys gates by stable IDs and condition, not display coordinates", () => {
    const renamed = issue(2, { repository: "owner/renamed", number: "30" });
    expect(gateKey(issue(2), "completed")).toBe(gateKey(renamed, "completed"));
    expect(gateKey(issue(2), "completed")).not.toBe(gateKey(issue(2), "closed"));
    expect(gateKey(issue(2), "completed")).not.toBe(gateKey(issue(3), "completed"));
  });
});

describe("Work structure", () => {
  it("binds identity to graph and node key and bounds payload, edges and assignment size", () => {
    expect(validateWork(h.work("a"))).toBeTruthy();
    expect(h.work("a").work_id).toBe(nodeId("graph", "a"));
    fails(() => validateWork({ ...h.work("a"), work_id: nodeId("graph", "b") }), "work_identity_invalid");
    fails(() => validateWork({ ...h.work("a"), priority: 6 }), "ledger_invalid");
    fails(() => validateWork({ ...h.work("a"), payload: [] }), "work_invalid");
    fails(() => validateWork({ ...h.work("a"), payload: { text: "x".repeat(PAYLOAD_BYTES) } }), "resource_limit");
    fails(() => validateWork({ ...h.work("a"), depends_on: Array.from({ length: PREDECESSORS + 1 }, (_, index) => gate(index + 1)) }), "resource_limit");
    fails(() => validateWork({ ...h.work("a"), depends_on: [gate(1), gate(1)] }), "dependency_invalid");
    fails(() => validateWork({ ...h.work("a"), depends_on: [{ kind: "issue", resource: issue(1), condition: "merged" }] }), "dependency_invalid");
    fails(() => validateWork({ ...h.work("a"), backing_issue: issue(1, { kind: "pull_request" }) }), "resource_invalid");
    fails(() => validateWork({ ...h.work("a"), replacement_of: { work_id: h.work("a").work_id } }), "work_invalid");
    fails(() => validateWork({ ...h.work("a"), expires_at: 1 }), "request_invalid");
    const edges = Math.floor(ASSIGNMENT_BYTES / EFFECTS_BYTES);
    const predecessors = Array.from({ length: edges }, (_, index) => ({ kind: "work", work_id: nodeId("graph", `p${index}`) }));
    fails(() => validateWork({ ...h.work("a"), payload: { blob: "x".repeat(1024) }, depends_on: predecessors }), "assignment_limit");
  });
});

describe("graph admission", () => {
  it("requires predecessors in the same graph and pool and rejects cycles and duplicate nodes", () => {
    const a = h.work("a");
    const state = replayTransactions(h.submit([], [a]));
    expect(validateGraphAdmission(state, [h.work("b", { depends_on: [after(a)] })])).toHaveLength(1);
    fails(() => validateGraphAdmission(state, [h.work("b", { graph_id: "other", depends_on: [after(a)] })]), "dependency_invalid");
    fails(() => validateGraphAdmission(state, [h.work("b", { pool: "other", depends_on: [after(a)] })]), "dependency_invalid");
    fails(() => validateGraphAdmission(state, [h.work("b", { depends_on: [{ kind: "work", work_id: nodeId("graph", "missing") }] })]), "dependency_invalid");
    fails(() => validateGraphAdmission(state, [h.work("c", { pool: "other" })]), "graph_invalid");
    fails(() => validateGraphAdmission(state, [h.work("c"), h.work("c")]), "graph_invalid");
    const x = h.work("x", { depends_on: [after(h.work("y"))] });
    const y = h.work("y", { depends_on: [after(h.work("x"))] });
    fails(() => validateGraphAdmission(state, [x, y]), "dependency_cycle");
  });

  it("allows one Work per backing Issue across the ledger and within a batch", () => {
    const state = replayTransactions(h.submit([], [h.work("a", { backing_issue: issue(7) })]));
    fails(() => validateGraphAdmission(state, [h.work("b", { backing_issue: issue(7, { repository: "owner/renamed" }) })]), "issue_binding_conflict");
    fails(() => validateGraphAdmission(replayTransactions(h.submit([], [h.work("z")])), [h.work("b", { backing_issue: issue(8) }), h.work("c", { backing_issue: issue(8) })]), "issue_binding_conflict");
  });

  it("counts Work and distinct gates against the graph bound", () => {
    const state = replayTransactions(h.submit([], [h.work("a", { depends_on: [gate(1), gate(1, "closed")] })]));
    expect(state.stats.nodes).toBe(3);
    const nodes = Array.from({ length: GRAPH_NODES - 3 }, (_, index) => h.work(`n${index}`));
    expect(validateGraphAdmission(state, nodes)).toHaveLength(GRAPH_NODES - 3);
    fails(() => validateGraphAdmission(state, [...nodes, h.work("overflow")]), "resource_limit");
  });
});

describe("dependency readiness", () => {
  it("is ready only after predecessors complete and gates are observed ready", () => {
    const a = h.work("a");
    const b = h.work("b", { depends_on: [after(a), gate(5)] });
    let log = h.submit([], [a, b]);
    const ready = { kind: "Observation", observation_id: "o", resource: issue(5), condition: "completed", state: "ready", state_reason: "completed" };
    const planned = h.dispatchNext(log, {}, h.dispatcher, [ready]);
    expect(dependencyStatus(replayTransactions(planned.log), replayTransactions(planned.log).works.get(b.work_id))).toEqual({ ready: false, reason: "dependency_pending", work_id: a.work_id });
    const dispatch = planned.decision.dispatches[0];
    log = h.finish(h.bind(h.launch(planned.log, dispatch, true, h.dispatcher, "7"), dispatch, h.workerActor("7")), dispatch, h.workerActor("7"), {}, [{ kind: "create_issue" }]);
    const state = replayTransactions(log);
    expect(dependencyStatus(state, state.works.get(b.work_id))).toEqual({ ready: true, reason: "ready" });
    expect(resultReferences(state, state.works.get(b.work_id))).toEqual([{ work_id: a.work_id, result_commit_id: state.works.get(a.work_id).completion_id, descriptor: [{ kind: "create_issue" }] }]);
    const unobserved = replayTransactions(h.submit([], [h.work("g", { depends_on: [gate(6)] })]));
    expect(dependencyStatus(unobserved, unobserved.works.get(h.work("g").work_id)).reason).toBe("external_unobserved");
  });
});

describe("continuation Work", () => {
  const parent = h.work("p", { payload: { task: "audit pages" }, priority: 2, fairness_key: "account", subject: issue(7), backing_issue: issue(8), replacement_of: { work_id: "older" } });

  it("inherits exactly the parent's scheduling and payload behind a deterministic key", () => {
    const child = continuationChild(parent, { route: "workqueue", message: "Audit page 2", remaining: 1 });
    expect(child).toEqual({
      kind: "Work",
      work_id: nodeId("graph", `continuation:${parent.work_id}`),
      graph_id: "graph",
      node_key: `continuation:${parent.work_id}`,
      pool: "default",
      priority: 2,
      fairness_key: "account",
      worker_profile: "worker",
      payload: { task: "audit pages" },
      subject: issue(7),
      depends_on: [after(parent)],
      continuation: { route: "workqueue", message: "Audit page 2", remaining: 1 },
    });
    expect(continuationChild(parent, { route: "issue", message: "Audit page 2", remaining: 0 }, issue(55)).depends_on).toEqual([after(parent), gate(55, "approved")]);
  });

  it("validates the continuation member and its shared budget", () => {
    for (const [continuation, code] of [
      [{ route: "dispatch", message: "x", remaining: 0 }, "request_invalid"],
      [{ route: "workqueue", message: " \t\u00a0\ufeff", remaining: 0 }, "request_invalid"],
      [{ route: "workqueue", message: "x", remaining: 16 }, "request_invalid"],
      [{ route: "workqueue", message: "x", remaining: -1 }, "request_invalid"],
      [{ route: "issue", message: "x", remaining: 1 }, "request_invalid"],
      [{ route: "workqueue", message: "x", remaining: 0, extra: true }, "request_invalid"],
      [{ route: "workqueue", message: "é".repeat(8193), remaining: 0 }, "resource_limit"],
    ])
      fails(() => validateContinuation(continuation), code);
    expect(validateContinuation({ route: "workqueue", message: "\u0085", remaining: 0 }).message).toBe("\u0085");
    fails(() => continuationChild(parent, { route: "issue", message: "x", remaining: 0 }), "request_invalid");
    fails(() => continuationChild(parent, { route: "workqueue", message: "x", remaining: 0 }, issue(55)), "request_invalid");
    const big = h.work("big", { payload: { task: "x".repeat(PAYLOAD_BYTES - 64) } });
    expect(validateWork(big)).toBe(big);
    fails(() => continuationChild(big, { route: "workqueue", message: "y".repeat(128), remaining: 0 }), "resource_limit");
  });

  it("keeps an approved gate distinct from a completed one", () => {
    expect(gateKey(issue(55), "approved")).not.toBe(gateKey(issue(55), "completed"));
    fails(() => validateWork(h.work("plain", { depends_on: [gate(55, "approved")] })), "request_invalid");
  });
});
