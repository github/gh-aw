// @ts-check
import { describe, expect, it } from "vitest";
import { normalizeWorkQueueContext, readInboundWorkQueueAssignment, readWorkQueueAssignment, resolveWorkQueueRuntime } from "./aw_context.cjs";

const assignment = {
  version: 3,
  dispatch_id: "d1",
  request_id: "request1",
  commit_id: "commit1",
  policy_epoch: "e1",
  pool: "default",
  worker_profile: "worker",
  claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: { plan: "immutable plan" }, result_refs: [] }],
};

describe("current-only work queue assignment context", () => {
  it("reads immutable Claim arrays without changing stored payload or provenance", () => {
    const parsed = readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: JSON.stringify(assignment) } });
    expect(parsed).toEqual(assignment);
    expect(Object.isFrozen(parsed.claims[0].work)).toBe(true);
    expect(readInboundWorkQueueAssignment({ client_payload: { work_queue_assignment: assignment } })).toEqual(assignment);
    expect(readWorkQueueAssignment({ work_queue_assignment: assignment })).toEqual(assignment);
    expect(normalizeWorkQueueContext({ run_id: "1" }, assignment)).toEqual({ run_id: "1", work_queue_assignment: assignment });
  });

  it("rejects legacy scalars, duplicate sources and supplied null assignments", () => {
    for (const field of ["work_queue_claim", "work_queue", "work_claim"]) {
      expect(() => readInboundWorkQueueAssignment({ inputs: { [field]: { work_id: "w", claim_id: "c" } } })).toThrow(/legacy/);
      expect(() => readWorkQueueAssignment({ [field]: {} })).toThrow(/legacy/);
    }
    expect(() => readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: assignment, aw_context: JSON.stringify({ work_queue_assignment: assignment }) } })).toThrow(/exactly one source/);
    expect(() => readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: assignment }, client_payload: { work_queue_assignment: assignment } })).toThrow(/exactly one source/);
    for (const value of [null, "", [], { work_id: "w", claim_id: "c", work: {} }]) {
      expect(() => readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: value } })).toThrow();
    }
    expect(readInboundWorkQueueAssignment({ inputs: { aw_context: "{}" } })).toBeNull();
  });

  it("rejects duplicate JSON keys rather than replacing Claim authority", () => {
    expect(() => readInboundWorkQueueAssignment({ inputs: { work_queue_assignment: '{"dispatch_id":"d1","dispatch_id":"d2"}' } })).toThrow(/duplicate/);
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
