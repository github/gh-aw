// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "node:os";
import { createRequire } from "node:module";
import {
  createWorkQueueDispatchTool,
  createWorkQueueFinishTool,
  createWorkQueueStateTool,
  createWorkQueueSubmitTool,
  createWorkQueueTools,
  loadWorkQueueSnapshot,
  parseSnapshotEnvelope,
  readWorkQueueState,
} from "./work_queue_mcp_server.cjs";
import { readFinishIntents, readStagedIntents } from "./work_queue_intents.cjs";
import { createServer, handleMessage, registerTool } from "./mcp_server_core.cjs";
import { MAX_PARSE_BYTES, MAX_SNAPSHOT_PARSE_BYTES } from "./work_queue_codec.cjs";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { assignmentForDispatch } = require("./work_queue_scheduler.cjs");

const directories = [];

/** Three Work in pool default; "a" is claimed by a dispatch bound to worker run 200. */
function setup(role = "dispatcher", envelopeOverrides = {}) {
  const works = ["a", "b", "c"].map((key, index) => h.work(key, { priority: [1, 3, 2][index] }));
  const planned = h.dispatchNext(h.submit([], works), { max_claims: 1, max_dispatches: 1 });
  const dispatchId = planned.decision.dispatches[0];
  const log = h.bind(h.launch(planned.log, dispatchId), dispatchId, h.workerActor("200", 1));
  const state = replayTransactions(log);
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-mcp-"));
  directories.push(directory);
  const envelope = { version: 4, sha: "activation-head", transactionLog: h.ledgerText(log), origin: h.dispatcher, role, worker: role === "worker" ? assignmentForDispatch(state, dispatchId) : null, ...envelopeOverrides };
  const snapshotPath = path.join(directory, "snapshot.json");
  fs.writeFileSync(snapshotPath, JSON.stringify(envelope));
  return { works, state, envelope, snapshotPath, snapshot: loadWorkQueueSnapshot(snapshotPath), intentPath: path.join(directory, "intents.jsonl"), finishIntentPath: path.join(directory, "finish.jsonl") };
}
const response = value => JSON.parse(value.content[0].text);

afterEach(() => {
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("work queue MCP server", () => {
  it("registers tools by snapshot role", () => {
    expect(createWorkQueueTools(setup("observer").snapshot).map(tool => tool.name)).toEqual(["work_queue_read"]);
    expect(createWorkQueueTools(setup("dispatcher").snapshot).map(tool => tool.name)).toEqual(["work_queue_read", "work_queue_submit", "work_queue_dispatch_next"]);
    expect(createWorkQueueTools(setup("worker").snapshot).map(tool => tool.name)).toEqual(["work_queue_read", "work_queue_submit", "work_queue_claim_finish"]);
  });

  it("reads one pool's states and predicts the next Work by priority then FIFO", () => {
    const test = setup();
    const view = readWorkQueueState(test.snapshot, {});
    expect(view).toMatchObject({ view: "activation_snapshot", snapshot_sha: "activation-head", counts: { claimed: 1, available: 2 }, claim_counts: { open: 1 }, open_dispatches: { total: 1, unbound: 0 }, total: 3 });
    expect(view.prediction).toEqual({ work_id: test.works[2].work_id, authoritative: false });
    expect(view.works.map(work => work.id)).toEqual(test.works.map(work => work.work_id));
    expect(view.works[1]).toMatchObject({ state: "available", attempts: 0, readiness: "ready", claim_id: null });
    expect(view.works.every(work => !Object.hasOwn(work, "payload"))).toBe(true);
    const sorted = readWorkQueueState(test.snapshot, { sort: [{ key: "priority", direction: "asc" }] });
    expect(sorted.works.map(work => work.id)).toEqual([0, 2, 1].map(index => test.works[index].work_id));
    expect(sorted.prediction).toEqual(view.prediction);
    expect(readWorkQueueState(test.snapshot, { work: "absent" }).work).toEqual({ id: "absent", state: "absent" });
    for (const args of [{ limit: 0 }, { sort: [{ key: "enqueued", direction: "asc" }] }, { unexpected: true }]) expect(() => readWorkQueueState(test.snapshot, args)).toThrow();
  });

  it("hides Work outside the snapshot's visible set", () => {
    const test = setup("dispatcher", { visible_work_ids: [] });
    expect(readWorkQueueState(test.snapshot, {})).toMatchObject({ total: 0, prediction: { work_id: null } });
  });

  it("shows a worker its assigned Claims and defaults the read to their pool", () => {
    const works = [h.work("smoke", { pool: "smoke", graph_id: "smoke-graph" }), h.work("other")];
    const planned = h.dispatchNext(h.submit(h.submit([], [works[0]]), [works[1]]), { pool: "smoke" });
    const dispatchId = planned.decision.dispatches[0];
    const log = h.bind(h.launch(planned.log, dispatchId), dispatchId, h.workerActor("200", 1));
    const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-mcp-"));
    directories.push(directory);
    const snapshotPath = path.join(directory, "snapshot.json");
    const worker = assignmentForDispatch(replayTransactions(log), dispatchId);
    fs.writeFileSync(snapshotPath, JSON.stringify({ version: 4, sha: "head", transactionLog: h.ledgerText(log), origin: h.dispatcher, role: "worker", worker }));
    const view = readWorkQueueState(loadWorkQueueSnapshot(snapshotPath), {});
    expect(view).toMatchObject({ pool: "smoke", total: 1, counts: { claimed: 1 } });
    expect(view.assignment).toEqual({
      dispatch_id: dispatchId,
      claims: [{ claim_handle: worker.claims[0].handle, claim_id: worker.claims[0].claim_id, work_id: works[0].work_id, work: works[0].payload, result_refs: [] }],
    });
    expect(readWorkQueueState(setup("dispatcher").snapshot, {})).not.toHaveProperty("assignment");
  });

  it("validates the v4 envelope strictly", () => {
    const test = setup("worker");
    expect(parseSnapshotEnvelope(JSON.stringify(test.envelope))).toEqual(test.envelope);
    const settled = { ...test.envelope, worker: null };
    expect(parseSnapshotEnvelope(JSON.stringify(settled)).worker).toBeNull();
    for (const invalid of [
      { ...test.envelope, version: 3 },
      { ...test.envelope, captured_at: 1 },
      { ...test.envelope, role: "administrator" },
      { ...test.envelope, role: "dispatcher" },
      { ...test.envelope, worker: { ...test.envelope.worker, version: 4 } },
      { ...test.envelope, origin: null },
    ])
      expect(() => parseSnapshotEnvelope(JSON.stringify(invalid))).toThrow();
    expect(() => parseSnapshotEnvelope('{"version":4,"version":4}')).toThrow(/duplicate/);
    expect(() => parseSnapshotEnvelope(JSON.stringify({ ...test.envelope, transactionLog: "x".repeat(MAX_PARSE_BYTES + 1) }))).toThrow(/ledger exceeds/);
    fs.truncateSync(test.snapshotPath, MAX_SNAPSHOT_PARSE_BYTES + 1);
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow(/snapshot exceeds/);
  });

  it("rejects a snapshot whose role conflicts with the compiled role", () => {
    const test = setup("observer");
    vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", "worker");
    expect(() => loadWorkQueueSnapshot(test.snapshotPath)).toThrow(/role_conflict/);
  });

  it("stages dispatch_next and submit intents without Work selectors", () => {
    const test = setup();
    const dispatch = createWorkQueueDispatchTool(test.snapshot, { intentPath: test.intentPath, createIntentId: () => "intent1" });
    expect(response(dispatch.handler({ pool: "default", max_claims: 3, max_dispatches: 1 }))).toEqual({ intent_id: "intent1", status: "staged" });
    for (const extra of [{ work_id: "chosen" }, { ref: "main" }, { workflow: "worker" }, { claim_handle: "h1" }]) expect(() => dispatch.handler({ pool: "default", max_claims: 1, max_dispatches: 1, ...extra })).toThrow();
    const submit = createWorkQueueSubmitTool(test.snapshot, { intentPath: test.intentPath, createIntentId: () => "intent2" });
    expect(response(submit.handler({ nodes: [{ payload: { plan: "root" } }] })).status).toBe("staged");
    expect(() => submit.handler({ nodes: [{ payload: {}, expires_at: 1 }] })).toThrow();
    expect(() => submit.handler({ nodes: [{ payload: {}, graph_id: "" }] })).toThrow();
    expect(readStagedIntents(test.intentPath)).toEqual([
      { version: 4, intent_id: "intent1", kind: "dispatch_next", parameters: { pool: "default", max_claims: 3, max_dispatches: 1 } },
      { version: 4, intent_id: "intent2", kind: "submit", parameters: { nodes: [{ payload: { plan: "root" } }] } },
    ]);
  });

  it("stages one finish outcome per assigned Claim", () => {
    const test = setup("worker");
    const finish = createWorkQueueFinishTool(test.snapshot, { finishIntentPath: test.finishIntentPath });
    for (const args of [{ claim_handle: "foreign" }, { outcome: "cancelled" }, { keep_open: true }]) expect(() => finish.handler(args)).toThrow();
    expect(response(finish.handler({}))).toEqual({ claim_handle: "h1", outcome: "completed", status: "staged" });
    expect(response(finish.handler({ claim_handle: "h1", outcome: "completed" })).status).toBe("staged");
    expect(() => finish.handler({ outcome: "failed" })).toThrow(/conflict/);
    expect(readFinishIntents(test.finishIntentPath)).toEqual(new Map([["h1", "completed"]]));
    expect(() => createWorkQueueFinishTool(setup("worker", { worker: null }).snapshot).handler({})).toThrow(/assignment_required/);
  });

  it("attributes a worker's child submission to its Claim handle", () => {
    const test = setup("worker");
    const submit = createWorkQueueSubmitTool(test.snapshot, { intentPath: test.intentPath, createIntentId: () => "child" });
    expect(response(submit.handler({ nodes: [{ node_key: "child", payload: {} }] }))).toEqual({ intent_id: "child", status: "staged", claim_handle: "h1" });
    expect(() => submit.handler({ nodes: [{ payload: {} }], claim_handle: "h9" })).toThrow(/claim_scope/);
  });

  it("serves bounded read results through the MCP stdio transport", async () => {
    const { snapshot } = setup();
    const server = createServer({ name: "work-queue", version: "4.0.0" });
    server.debug = vi.fn();
    server.writeMessage = vi.fn();
    registerTool(server, createWorkQueueStateTool(snapshot));
    await handleMessage(server, { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "work_queue_read", arguments: { limit: 1 } } });
    expect(server.writeMessage).toHaveBeenCalledWith({ jsonrpc: "2.0", id: 1, result: { ...createWorkQueueStateTool(snapshot).handler({ limit: 1 }), isError: false } });
  });
});
