// @ts-check
import { describe, expect, it } from "vitest";
import { normalizeWorkQueueContext, readInboundWorkQueueAssignment, readWorkQueueAssignment, resolveWorkQueueRuntime } from "./aw_context.cjs";

const assignment = { version: 4, dispatch_id: "d_1" };

describe("v4 work queue assignment input", () => {
  it("reads the dispatch reference from inputs, client payloads and aw_context", () => {
    const parsed = readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: JSON.stringify(assignment) } });
    expect(parsed).toEqual(assignment);
    expect(Object.isFrozen(parsed)).toBe(true);
    expect(readInboundWorkQueueAssignment({ client_payload: { work_queue_assignment: assignment } })).toEqual(assignment);
    expect(readWorkQueueAssignment({ work_queue_assignment: assignment })).toEqual(assignment);
    expect(normalizeWorkQueueContext({ run_id: "1" }, assignment)).toEqual({ run_id: "1", work_queue_assignment: assignment });
  });

  it("rejects legacy scalars, duplicate sources and any other assignment shape", () => {
    for (const field of ["work_queue_claim", "work_queue", "work_claim"]) {
      expect(() => readInboundWorkQueueAssignment({ inputs: { [field]: { work_id: "w", claim_id: "c" } } })).toThrow(/legacy/);
      expect(() => readWorkQueueAssignment({ [field]: {} })).toThrow(/legacy/);
    }
    expect(() => readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: assignment, aw_context: JSON.stringify({ work_queue_assignment: assignment }) } })).toThrow(/exactly one source/);
    expect(() => readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: assignment }, client_payload: { work_queue_assignment: assignment } })).toThrow(/exactly one source/);
    const invalid = [null, "", [], { dispatch_id: "d_1" }, { version: 3, dispatch_id: "d_1" }, { ...assignment, claims: [] }, { version: 4, dispatch_id: "" }, '{"version":4,"dispatch_id":"d1","dispatch_id":"d2"}'];
    for (const value of invalid) {
      expect(() => readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: value } })).toThrow(/work_queue_assignment_invalid/);
    }
    expect(readInboundWorkQueueAssignment({ inputs: { aw_context: "{}" } })).toBeNull();
  });

  it("distinguishes explicit read-only observers from required workers without an unassigned downgrade", () => {
    expect(resolveWorkQueueRuntime({}, { role: "observer" })).toEqual({ role: "observer", assignment: null });
    expect(resolveWorkQueueRuntime({})).toEqual({ role: "dispatcher", assignment: null });
    expect(resolveWorkQueueRuntime({ inputs: { work_queue_assignment: assignment } }).role).toBe("worker");
    expect(() => resolveWorkQueueRuntime({}, { role: "worker" })).toThrow(/assignment_required/);
    expect(() => resolveWorkQueueRuntime({}, { requireAssignment: true })).toThrow(/assignment_required/);
    expect(() => resolveWorkQueueRuntime({}, { role: "observer", requireAssignment: true })).toThrow(/role_conflict/);
    for (const supplied of [assignment, null, "", {}, '{"version":3,"version":2}']) {
      expect(() => resolveWorkQueueRuntime({ inputs: { work_queue_assignment: supplied } }, { role: "observer" })).toThrow();
    }
    expect(() => resolveWorkQueueRuntime({}, { role: "other" })).toThrow(/role_invalid/);
  });

  it("does not hide malformed or shadowed embedded assignments behind observer context parsing", () => {
    for (const embedded of ['{"work_queue_assignment":', '{"work_queue_assignment":null}', '{"work_queue_assignment":{},"work_queue_assignment":null}', "[]", "42"]) {
      expect(() => resolveWorkQueueRuntime({ inputs: { aw_context: embedded } }, { role: "observer" })).toThrow();
    }
    expect(() => resolveWorkQueueRuntime({ inputs: { aw_context: "{}" }, client_payload: { aw_context: JSON.stringify({ work_queue_assignment: assignment }) } }, { role: "observer" })).toThrow(/observer_assignment/);
    expect(() => readInboundWorkQueueAssignment({ inputs: { aw_context: JSON.stringify({ work_queue_assignment: assignment }) }, client_payload: { aw_context: { work_queue_assignment: assignment } } })).toThrow(/exactly one source/);
  });
});
