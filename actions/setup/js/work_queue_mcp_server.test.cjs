// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import { randomUUID } from "crypto";
import {
  createWorkQueueDispatchTool,
  createWorkQueueExplainTool,
  createWorkQueueFinishTool,
  createWorkQueueSubmitTool,
  createWorkQueueStateTool,
  createWorkQueueTools,
  loadWorkQueueSnapshot,
  parseSnapshotEnvelope,
  readWorkQueueState,
} from "./work_queue_mcp_server.cjs";
import { serializeTransactionLog } from "./work_queue_replay.cjs";
import { readStagedIntents } from "./work_queue_intents.cjs";
import { createServer, handleMessage, registerTool } from "./mcp_server_core.cjs";
import { queueFixture } from "./work_queue_lifecycle.test_helpers.cjs";

const directories = [];
function setup(options = {}) {
  const fixture = queueFixture({ granted: false, ...options });
  const directory = path.join(process.cwd(), `.queue-mcp-test-${randomUUID()}`);
  fs.mkdirSync(directory);
  directories.push(directory);
  const envelope = { version: 3, sha: "activation-head", captured_at: fixture.at, origin: fixture.dispatcher, worker: fixture.assignment, transactionLog: serializeTransactionLog(fixture.transactions) };
  const snapshotPath = path.join(directory, "snapshot.json");
  fs.writeFileSync(snapshotPath, JSON.stringify(envelope));
  return { fixture, envelope, snapshot: loadWorkQueueSnapshot(snapshotPath), snapshotPath, intentPath: path.join(directory, "intents.jsonl"), finishIntentPath: path.join(directory, "finish.jsonl") };
}
const response = value => JSON.parse(value.content[0].text);
afterEach(() => {
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("bounded credential-free work queue MCP", () => {
  it("supports the observer smoke read/explain protocol before queue initialization without any finish artifact", () => {
    const test = setup();
    fs.writeFileSync(test.snapshotPath, JSON.stringify({ ...test.envelope, sha: null, transactionLog: "", role: "observer" }));
    const snapshot = loadWorkQueueSnapshot(test.snapshotPath);
    const work = "__gh_aw_smoke__-15";
    const result = response(createWorkQueueStateTool(snapshot).handler({ work }));
    expect(result).toMatchObject({ view: "activation_snapshot", snapshot_sha: null, work: { id: work, state: "absent" }, queue_state: "uninitialized", prediction: { work_id: null, reason: "queue_uninitialized", authoritative: false } });
    const explained = response(createWorkQueueExplainTool(snapshot).handler({ work, pool: "default" }));
    expect(explained).toMatchObject({ view: "activation_snapshot", snapshot_sha: null, explanation: { work_id: work, state: "absent", ready: false, authoritative: false } });
    expect(createWorkQueueTools(snapshot).map(tool => tool.name)).toEqual(["work_queue_read", "work_queue_explain"]);
    expect(() => createWorkQueueFinishTool({ snapshot, finishIntentPath: test.finishIntentPath }).handler({ outcome: "completed" })).toThrow();
    expect(fs.existsSync(test.finishIntentPath)).toBe(false);
    fs.writeFileSync(test.snapshotPath, JSON.stringify({ ...test.envelope, sha: "existing", transactionLog: "", role: "observer" }));
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow(/policy_missing/);
  });

  it("reports independent Work/Claim states, pending delivery, native accounting and absent Work without payloads", () => {
    const test = setup({ granted: true, bound: true });
    test.fixture.append("finish", { dispatch_id: test.fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...test.fixture.workerActor, dispatch_id: test.fixture.assignment.dispatch_id, claim_handle: "h1" });
    test.fixture.append("finish", { dispatch_id: test.fixture.assignment.dispatch_id, claim_handle: "h2", outcome: "cancelled" }, { ...test.fixture.workerActor, dispatch_id: test.fixture.assignment.dispatch_id, claim_handle: "h2" });
    const snapshot = { ...test.snapshot, role: "observer", worker: null, projection: test.fixture.state };
    expect(readWorkQueueState(snapshot)).toMatchObject({
      queue_state: "initialized",
      policy_mode: "weighted-priority",
      fairness_units: "durable_claims",
      counts: { completed: 1, available: 1, claimed: 1 },
      claim_counts: { completed: 1, cancelled: 1, open: 1 },
      barrier_counts: { pending: 1, none: 2 },
      native_reservations: { outstanding: 1, unbound: 0 },
    });
    expect(readWorkQueueState(snapshot, { work: "absent" }).work.state).toBe("absent");
    expect(response(createWorkQueueExplainTool(snapshot).handler({ work: "absent" })).explanation).toMatchObject({ state: "absent", reason: "work_absent", authoritative: false });
    expect(JSON.stringify(readWorkQueueState(snapshot))).not.toContain("stored task");
  });

  it("exposes only read/explain to explicit observers and never stages queue mutations", () => {
    const test = setup();
    const snapshot = { ...test.snapshot, role: "observer" };
    expect(createWorkQueueTools(snapshot).map(tool => tool.name)).toEqual(["work_queue_read", "work_queue_explain"]);
    expect(readWorkQueueState(snapshot).total).toBe(3);
    const work = readWorkQueueState(snapshot).works[0].id;
    expect(response(createWorkQueueExplainTool(snapshot).handler({ work })).view).toBe("activation_snapshot");
    expect(() => createWorkQueueSubmitTool(snapshot, { intentPath: test.intentPath }).handler({ nodes: [{ graph_id: "g", node_key: "n", payload: {} }] })).toThrow(/observer_read_only/);
    expect(() => createWorkQueueDispatchTool(snapshot, { intentPath: test.intentPath }).handler({ pool: "default", max_claims: 1, max_dispatches: 1 })).toThrow(/observer_read_only/);
    expect(() => createWorkQueueFinishTool({ snapshot, finishIntentPath: test.finishIntentPath }).handler({})).toThrow();
    expect(fs.existsSync(test.intentPath)).toBe(false);
    expect(fs.existsSync(test.finishIntentPath)).toBe(false);
  });

  it("rejects forged observer snapshots when the compiled role declares a worker", () => {
    const test = setup();
    vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", "worker");
    expect(() => createWorkQueueTools({ ...test.snapshot, role: "observer" })).toThrow(/role_conflict/);
    expect(() => createWorkQueueTools({ ...test.snapshot, role: "worker" })).toThrow(/assignment_required/);
    fs.writeFileSync(test.snapshotPath, JSON.stringify({ ...test.envelope, role: "observer" }));
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow(/role_conflict/);
  });

  it("reads and explains only activation state with explicit stale prediction provenance", () => {
    const { snapshot, fixture } = setup();
    const result = readWorkQueueState(snapshot, { limit: 1 });
    expect(result).toMatchObject({ view: "activation_snapshot", snapshot_sha: "activation-head", total: 3, next_offset: 1, prediction: { authoritative: false } });
    expect(result.prediction.work_id).toBe([...fixture.state.works.keys()][0]);
    expect(result.works).toHaveLength(1);
    expect(result.works[0]).not.toHaveProperty("payload");
    const explanation = response(createWorkQueueExplainTool(snapshot).handler({ work: result.works[0].id }));
    expect(explanation.view).toBe("activation_snapshot");
    expect(explanation.explanation).toMatchObject({ work_id: result.works[0].id, state: "available" });
  });

  it("makes presentation sorting independent of the scheduler's next fair winner", () => {
    const { snapshot } = setup();
    const baseline = readWorkQueueState(snapshot);
    const sorted = readWorkQueueState(snapshot, { sort: [{ key: "id", direction: "desc" }] });
    expect(sorted.works.map(work => work.id)).toEqual(
      baseline.works
        .map(work => work.id)
        .sort()
        .reverse()
    );
    expect(sorted.prediction).toEqual(baseline.prediction);
    for (const args of [{ limit: 129 }, { offset: -1 }, { sort: [] }, { sort: [{ key: "state", direction: "asc" }] }, { actor: "forged" }]) expect(() => readWorkQueueState(snapshot, args)).toThrow();
    expect(createWorkQueueStateTool(snapshot).inputSchema.properties.limit.maximum).toBe(128);
  });

  it("rejects old snapshots, unsupported logs and duplicate keys without any upgrade", () => {
    const test = setup();
    for (const envelope of [
      { ...test.envelope, version: 2 },
      { ...test.envelope, transactionLog: '{"version":2,"kind":"Work","work":"old"}\n' },
    ]) {
      fs.writeFileSync(test.snapshotPath, JSON.stringify(envelope));
      expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow();
    }
    fs.writeFileSync(test.snapshotPath, '{"version":3,"version":2}');
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow(/duplicate/);
  });

  it("frames escaped ledger strings independently without weakening strict metadata or ledger parsing", () => {
    const test = setup();
    const encoded = JSON.stringify(test.envelope).replace('"transactionLog":', '"transaction\\u004cog":');
    expect(parseSnapshotEnvelope(encoded)).toEqual(test.envelope);
    fs.writeFileSync(test.snapshotPath, encoded);
    expect(loadWorkQueueSnapshot(test.snapshotPath).projection.tip).toBe(test.fixture.state.tip);
    for (const invalid of [
      '{"transactionLog":"x","transaction\\u004cog":"y"}',
      '{"transactionLog":"x","metadata":{"a":1,"a":2}}',
      '{"transactionLog":"x","metadata":1e2}',
      '{"transactionLog":"x","metadata":-0}',
      '{"transactionLog":"\\ud800"}',
      '{"transactionLog":"\\x20"}',
      '{"metadata":"transactionLog":"x"}',
      '{"transactionLog":"unterminated}',
    ])
      expect(() => parseSnapshotEnvelope(invalid)).toThrow();
    const nested = { transactionLog: '{"nested":"transactionLog: \\\\ end"}\n', worker: { transactionLog: "must stay strictly parsed" } };
    expect(parseSnapshotEnvelope(JSON.stringify(nested))).toEqual(nested);
    const corrupt = Buffer.from(JSON.stringify(test.envelope));
    corrupt[corrupt.indexOf("activation-head")] = 0x80;
    fs.writeFileSync(test.snapshotPath, corrupt);
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow(/encoded data/);
    fs.writeFileSync(test.snapshotPath, Buffer.concat([Buffer.from([0xef, 0xbb, 0xbf]), Buffer.from(JSON.stringify(test.envelope))]));
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow();
  });

  it("accepts outer escaping above 80 MiB while retaining the decoded 80 MiB ledger bound", () => {
    const ledger = '"'.repeat(40 * 1024 * 1024 + 1);
    const encoded = JSON.stringify({ transactionLog: ledger, version: 3 });
    expect(Buffer.byteLength(encoded)).toBeGreaterThan(80 * 1024 * 1024);
    expect(parseSnapshotEnvelope(encoded).transactionLog).toBe(ledger);
  });

  it("rejects oversized decoded ledgers and outer files before authoritative replay", () => {
    expect(() => parseSnapshotEnvelope('{"transactionLog":"' + "x".repeat(80 * 1024 * 1024 + 1) + '"}')).toThrow(/ledger exceeds.*bounded/);
    const test = setup();
    fs.truncateSync(test.snapshotPath, 162 * 1024 * 1024 + 1);
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow(/snapshot exceeds.*bounded/);
  });

  it("stages submit/dispatch-next without Work selectors, authority or durable mutation", () => {
    const test = setup();
    const options = { intentPath: test.intentPath, createIntentId: () => "intent1" };
    const dispatch = createWorkQueueDispatchTool(test.snapshot, options);
    const result = response(dispatch.handler({ pool: "default", max_claims: 3, max_dispatches: 1 }));
    expect(result).toEqual({ intent_id: "intent1", status: "staged" });
    expect(result).not.toHaveProperty("claims");
    expect(test.fixture.state.claims.size).toBe(0);
    expect(readStagedIntents(test.intentPath)).toEqual([{ version: 3, intent_id: "intent1", kind: "dispatch_next", parameters: { pool: "default", max_claims: 3, max_dispatches: 1 } }]);
    for (const extra of [{ work_id: "chosen" }, { ref: "main" }, { actor: "forged" }, { workflow: "worker" }, { claim_handle: "foreign" }])
      expect(() => dispatch.handler({ pool: "default", max_claims: 1, max_dispatches: 1, ...extra })).toThrow();
    const submit = createWorkQueueSubmitTool(test.snapshot, { ...options, createIntentId: () => "intent2" });
    expect(response(submit.handler({ nodes: [{ graph_id: "g2", node_key: "n", payload: { plan: "stored task" }, worker_profile: "default" }] })).status).toBe("staged");
    expect(() => submit.handler({ nodes: [{ graph_id: "g2", node_key: "n", payload: {}, batch_trust_domain: "forged" }] })).toThrow();
  });

  it("stages independent finishes and requires multi-Claim handles from the original assignment", () => {
    const batch = setup({ granted: true, bound: true });
    const finish = createWorkQueueFinishTool({ snapshot: batch.snapshot, finishIntentPath: batch.finishIntentPath, createIntentId: () => "finish1" });
    for (const args of [{}, { claim_handle: null }, { claim_handle: "" }, { claim_handle: "foreign" }, { claim_handle: "h1", work_id: "forged" }]) expect(() => finish.handler(args)).toThrow();
    const staged = response(finish.handler({ claim_handle: "h1", outcome: "completed" }));
    expect(staged).toEqual({ intent_id: "finish1", status: "staged", claim_handle: "h1" });
    expect(response(finish.handler({ claim_handle: "h1", outcome: "completed" }))).toEqual(staged);
    expect(() => finish.handler({ claim_handle: "h1", outcome: "cancelled" })).toThrow(/conflict/);
    expect(readStagedIntents(batch.finishIntentPath)).toHaveLength(1);
    expect(fs.statSync(batch.finishIntentPath).mode & 0o777).toBe(0o644);
    const single = setup({ granted: true, bound: true, count: 1 });
    const singleton = createWorkQueueFinishTool({ snapshot: single.snapshot, finishIntentPath: single.finishIntentPath, createIntentId: () => "single" });
    expect(response(singleton.handler({})).claim_handle).toBe("h1");
    expect(() => singleton.handler({ outcome: null })).toThrow();
    expect(() => createWorkQueueFinishTool({ snapshot: setup().snapshot }).handler({})).toThrow(/assignment_required/);
  });

  it("stages omitted root identities for trusted resolution without accepting explicit invalid identities", () => {
    const test = setup();
    const payload = { plan: "independent root", effect_contract: { kind: "none" } };
    const submit = createWorkQueueSubmitTool(test.snapshot, { intentPath: test.intentPath, createIntentId: () => "default-root" });
    expect(response(submit.handler({ nodes: [{ payload }] })).status).toBe("staged");
    expect(readStagedIntents(test.intentPath)[0].parameters).toEqual({ nodes: [{ payload }] });
    expect(test.fixture.state.works.size).toBe(3);
    for (const field of ["graph_id", "node_key"]) {
      for (const value of ["", null, undefined]) expect(() => submit.handler({ nodes: [{ payload, [field]: value }] })).toThrow();
    }
    expect(readStagedIntents(test.intentPath)).toHaveLength(1);
  });

  it("attributes worker-generated queue control to its Claim but never rewrites its ownership", () => {
    const test = setup({ granted: true, bound: true });
    const tool = createWorkQueueDispatchTool(test.snapshot, { intentPath: test.intentPath, createIntentId: () => "scoped" });
    expect(() => tool.handler({ pool: "default", max_claims: 1, max_dispatches: 1 })).toThrow(/claim_handle/);
    expect(response(tool.handler({ pool: "default", max_claims: 1, max_dispatches: 1, claim_handle: "h2" })).claim_handle).toBe("h2");
    expect(test.fixture.state.claims.get(test.fixture.assignment.claims[1].claim_id).state).toBe("open");
  });

  it("serves bounded read results through the existing MCP stdio transport", async () => {
    const { snapshot } = setup();
    const server = createServer({ name: "work-queue", version: "3.0.0" });
    server.debug = vi.fn();
    server.writeMessage = vi.fn();
    registerTool(server, createWorkQueueStateTool(snapshot));
    await handleMessage(server, { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "work_queue_read", arguments: { limit: 1 } } });
    expect(server.writeMessage).toHaveBeenCalledWith({ jsonrpc: "2.0", id: 1, result: { ...createWorkQueueStateTool(snapshot).handler({ limit: 1 }), isError: false } });
  });
});
