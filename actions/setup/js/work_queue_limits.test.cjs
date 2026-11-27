import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const limits = require("./work_queue_limits.cjs");

describe("v4 protocol bounds", () => {
  it("pins the wire bounds shared with the Go engine", () => {
    expect({
      PAYLOAD_BYTES: limits.PAYLOAD_BYTES,
      OPERATIONS_PER_COMMIT: limits.OPERATIONS_PER_COMMIT,
      GRAPH_NODES: limits.GRAPH_NODES,
      PREDECESSORS: limits.PREDECESSORS,
      ASSIGNMENT_BYTES: limits.ASSIGNMENT_BYTES,
      EFFECTS_BYTES: limits.EFFECTS_BYTES,
      LEDGER_BYTES: limits.LEDGER_BYTES,
      CHECKPOINT_AFTER_LINES: limits.CHECKPOINT_AFTER_LINES,
      MAX_PENDING_WORK: limits.MAX_PENDING_WORK,
    }).toEqual({
      PAYLOAD_BYTES: 16384,
      OPERATIONS_PER_COMMIT: 256,
      GRAPH_NODES: 4096,
      PREDECESSORS: 64,
      ASSIGNMENT_BYTES: 49152,
      EFFECTS_BYTES: 4096,
      LEDGER_BYTES: 67108864,
      CHECKPOINT_AFTER_LINES: 512,
      MAX_PENDING_WORK: 4096,
    });
  });

  it("leaves room for every Claim when refreshing observations", () => {
    expect(limits.observationRefreshBudget(1)).toBe(128);
    expect(limits.observationRefreshBudget(200)).toBe(56);
    expect(limits.observationRefreshBudget(256)).toBe(0);
    expect(limits.observationRefreshBudget(1, 255)).toBe(255);
    for (const [claims, maximum] of [
      [0, 1],
      [257, 1],
      [1, 256],
      [1, -1],
      [1, 0.5],
    ])
      expect(() => limits.observationRefreshBudget(claims, maximum)).toThrow();
  });

  it("measures canonical UTF-8 bytes", () => {
    expect(() => limits.boundedBytes({ text: "é".repeat(5) }, 21, "value")).not.toThrow();
    expect(() => limits.boundedBytes({ text: "é".repeat(5) }, 20, "value")).toThrow(expect.objectContaining({ code: "resource_limit" }));
  });
});
