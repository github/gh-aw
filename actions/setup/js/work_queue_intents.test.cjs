// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import path from "path";
import { randomUUID } from "crypto";
import { defaultPolicy } from "./work_queue_policy.cjs";
import { newRequest, generateRequestOperations } from "./work_queue_replay.cjs";
import { queueFixture } from "./work_queue_lifecycle.test_helpers.cjs";
import { digest } from "./work_queue_codec.cjs";
import { nodeId } from "./work_queue_graph.cjs";
import { MAX_INTENTS, MAX_INTENT_BYTES, readStagedIntentBatch, readStagedIntents, requestIdForIntent, requestForIntent, normalizeDispatchParameters, normalizeSubmitParameters, stageIntent } from "./work_queue_intents.cjs";

const directories = [];
const origin = { authenticated: true, roles: ["dispatcher"], role: "dispatcher", principal: "11", repository: "owner/repo", workflow: ".github/workflows/dispatcher.lock.yml", run_id: "15", run_attempt: 1 };
const policy = defaultPolicy({ repository: "owner/repo", principal: "11", ref: "a".repeat(40) });
function filename() {
  const directory = path.join(process.cwd(), `.queue-intents-test-${randomUUID()}`);
  fs.mkdirSync(directory);
  directories.push(directory);
  return path.join(directory, "intents.jsonl");
}
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("trusted stable intent ingestion", () => {
  it("binds request identities to native origin, never agent authority", () => {
    const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 1000 };
    const request = requestForIntent(origin, "intent1", "dispatch_next", parameters);
    expect(requestForIntent(origin, "intent1", "dispatch_next", parameters)).toEqual(request);
    expect(requestForIntent({ ...origin, run_id: "16" }, "intent1", "dispatch_next", parameters).id).not.toBe(request.id);
    expect(requestForIntent(origin, "intent2", "dispatch_next", parameters).id).not.toBe(request.id);
    expect(() => requestForIntent({ ...origin, authenticated: false }, "intent1", "dispatch_next", parameters)).toThrow();
  });

  it("uses the checked request constructor and detaches fingerprinted parameters", () => {
    const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 1000 };
    const request = requestForIntent(origin, "constructor", "dispatch_next", parameters);
    expect(request.id).toBe(requestIdForIntent(origin, "constructor"));
    parameters.max_claims = 2;
    expect(request.parameters.max_claims).toBe(1);
    expect(() => requestForIntent(origin, "constructor", "dispatch_next", {})).toThrow();
  });

  it("records staged mutations, deduplicates identical intents and rejects conflicting IDs", () => {
    const file = filename();
    const intent = { version: 3, intent_id: "i1", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
    stageIntent(file, intent);
    stageIntent(file, intent);
    expect(readStagedIntents(file)).toEqual([intent]);
    expect(fs.statSync(file).mode & 0o777).toBe(0o644);
    fs.appendFileSync(file, `${JSON.stringify({ ...intent, parameters: { ...intent.parameters, max_claims: 2 } })}\n`);
    expect(() => readStagedIntents(file)).toThrow(/conflict/);
  });

  it("isolates malformed lines and disputed IDs while retaining independent valid siblings", () => {
    const file = filename();
    const first = { version: 3, intent_id: "first", kind: "dispatch_next", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
    const disputed = { ...first, intent_id: "disputed" };
    const second = { ...first, intent_id: "second" };
    fs.writeFileSync(
      file,
      [
        JSON.stringify(first),
        "{not JSON",
        JSON.stringify({ ...first, intent_id: "bad-scope", claim_handle: null }),
        JSON.stringify(disputed),
        JSON.stringify({ ...disputed, parameters: { ...disputed.parameters, max_claims: 2 } }),
        JSON.stringify(second),
        JSON.stringify(first),
      ].join("\n")
    );
    const batch = readStagedIntentBatch(file);
    expect(batch.intents).toEqual([first, second]);
    expect(batch.errors.map(error => error.line)).toEqual([2, 3, 4, 5]);
    expect(batch.errors.every(error => ["work_queue_intent_invalid", "work_queue_intent_conflict"].includes(error.reason))).toBe(true);
    expect(() => readStagedIntents(file)).toThrow(/conflict/);
  });

  it("keeps byte, count and invalid UTF-8 failures transport-wide", () => {
    const file = filename();
    fs.writeFileSync(file, "{}\n".repeat(MAX_INTENTS + 1));
    expect(() => readStagedIntentBatch(file)).toThrow(/limit/);
    fs.writeFileSync(file, Buffer.alloc(MAX_INTENT_BYTES + 1, 0x20));
    expect(() => readStagedIntentBatch(file)).toThrow(/limit/);
    fs.writeFileSync(file, Buffer.from([0xff]));
    expect(() => readStagedIntentBatch(file)).toThrow(/transport_invalid/);
  });

  it("rejects selectors, trace overrides, over-budget dispatch and unresolved profile authority", () => {
    const args = { pool: "default", max_claims: 1, max_dispatches: 1 };
    expect(normalizeDispatchParameters(args, policy, 1)).toEqual({ ...args, max_bytes: policy.limits.assignment_bytes });
    for (const extra of [{ work_id: "preferred" }, { workflow: "preferred" }, { actor: origin }, { trace_id: "forged" }]) expect(() => normalizeDispatchParameters({ ...args, ...extra }, policy, 1)).toThrow();
    expect(() => normalizeDispatchParameters(args, policy, 0)).toThrow(/budget/);
  });

  it("resolves stored Work metadata through installed policy without agent-controlled trust fields", () => {
    const node = { graph_id: "g", node_key: "n", payload: { plan: "stored" } };
    const normalized = normalizeSubmitParameters({ nodes: [node] }, policy, 100);
    expect(normalized.nodes[0]).toMatchObject({ ...node, priority: 3, fairness_key: "", pool: "default", worker_profile: "default", batch_trust_domain: "default", enqueued: 100, depends_on: [] });
    expect(() => normalizeSubmitParameters({ nodes: [{ ...node, batch_trust_domain: "forged" }] }, policy, 100)).toThrow();
  });

  it("reuses immutable enqueue time across native runs without accepting changed definitions", () => {
    const fixture = queueFixture({ granted: false, count: 1 });
    const stored = [...fixture.state.works.values()][0];
    const node = { graph_id: stored.graph_id, node_key: stored.node_key, payload: stored.payload };
    const parameters = normalizeSubmitParameters({ nodes: [node] }, fixture.policy, 2000, fixture.state);
    expect(parameters.nodes[0].enqueued).toBe(stored.enqueued);
    const actor = { ...fixture.dispatcher, run_id: "16" };
    const request = newRequest("repeat-existing", "submit", actor, parameters);
    expect(generateRequestOperations(fixture.state, request, actor, 2001, "repeat-commit").operations).toEqual([]);
    const conflicting = normalizeSubmitParameters({ nodes: [{ ...node, payload: { plan: "changed immutable payload" } }] }, fixture.policy, 2000, fixture.state);
    expect(() => generateRequestOperations(fixture.state, newRequest("conflict-existing", "submit", actor, conflicting), actor, 2001, "conflict-commit")).toThrow();
    expect(stored.payload).toEqual(node.payload);
    expect(stored.enqueued).toBe(1000);
  });

  it("defaults independent roots to canonical payload hashes while allowing explicit distinct nodes", () => {
    const payload = { plan: "independent", effect_contract: { kind: "none" } };
    const root = normalizeSubmitParameters({ nodes: [{ payload }] }, policy, 100).nodes[0];
    expect(root).toMatchObject({ graph_id: digest(payload), node_key: "root", work_id: nodeId(digest(payload), "root"), enqueued: 100 });
    const reordered = { effect_contract: { kind: "none" }, plan: "independent" };
    expect(normalizeSubmitParameters({ nodes: [{ payload: reordered }] }, policy, 100).nodes[0]).toEqual(root);
    const graph = normalizeSubmitParameters({ nodes: [{ graph_id: "explicit-graph", payload }] }, policy, 100).nodes[0];
    expect(graph).toMatchObject({ graph_id: "explicit-graph", node_key: "root" });
    const distinct = normalizeSubmitParameters({ nodes: [{ node_key: "second", payload }] }, policy, 100).nodes[0];
    expect(distinct).toMatchObject({ graph_id: root.graph_id, node_key: "second" });
    expect(distinct.work_id).not.toBe(root.work_id);
    const fixture = queueFixture({ granted: false, count: 1 });
    fixture.append("submit", { nodes: [root] }, fixture.dispatcher, "first-root");
    const repeat = normalizeSubmitParameters({ nodes: [{ payload }] }, fixture.policy, 2000, fixture.state);
    expect(repeat.nodes[0]).toEqual(root);
    const actor = { ...fixture.dispatcher, run_id: "16" };
    expect(generateRequestOperations(fixture.state, newRequest("second-run-root", "submit", actor, repeat), actor, 2001, "repeat-root").operations).toEqual([]);
    expect(generateRequestOperations(fixture.state, newRequest("distinct-root", "submit", actor, { nodes: [distinct] }), actor, 2001, "distinct-root").operations).toEqual([distinct]);
  });

  it("does not silently default explicit empty, null or undefined graph/node identities", () => {
    for (const field of ["graph_id", "node_key"]) {
      for (const value of ["", null, undefined]) expect(() => normalizeSubmitParameters({ nodes: [{ payload: {}, [field]: value }] }, policy, 100)).toThrow();
    }
  });
});
