import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { GRAPH_NODES, OPERATIONS_PER_COMMIT } = require("./work_queue_limits.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { validateGraphAdmission } = require("./work_queue_graph.cjs");
const { nodeId } = require("./work_queue_graph.cjs");

describe("queue graph scaling", () => {
  it("admits a maximum-size dependency chain and plans only its ready head", () => {
    const chain = Array.from({ length: GRAPH_NODES }, (_, index) => h.work(`n${index}`, index ? { depends_on: [{ kind: "work", work_id: nodeId("graph", `n${index - 1}`) }] } : {}));
    let log = [];
    for (let offset = 0; offset < chain.length; offset += OPERATIONS_PER_COMMIT) log = h.submit(log, chain.slice(offset, offset + OPERATIONS_PER_COMMIT), { ...h.producer, run_id: String(1000 + offset) });
    const state = replayTransactions(log);
    expect(state.stats).toMatchObject({ work: GRAPH_NODES, available: GRAPH_NODES, nodes: GRAPH_NODES });
    expect(h.dispatchNext(log).decision.operations.map(operation => operation.work_id)).toEqual([chain[0].work_id]);
    expect(() => validateGraphAdmission(state, [h.work("overflow")])).toThrow(expect.objectContaining({ code: "resource_limit" }));
  });

  it("reports a maximum-length cycle iteratively", () => {
    const size = OPERATIONS_PER_COMMIT;
    const cycle = Array.from({ length: size }, (_, index) => h.work(`c${index}`, { graph_id: "cycle", depends_on: [{ kind: "work", work_id: nodeId("cycle", `c${(index + 1) % size}`) }] }));
    expect(() => validateGraphAdmission(replayTransactions([]), cycle)).toThrow(expect.objectContaining({ code: "dependency_cycle", message: expect.stringContaining("c0 -> c1 -> c2") }));
  });
});
