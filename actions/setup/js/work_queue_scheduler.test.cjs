import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { replayTransactions, serializeProjection } = require("./work_queue_replay.cjs");
const { assignmentForDispatch, assignmentsForRequest, claimIdentity, planDispatch, selectionOnly, validateDispatchParameters } = require("./work_queue_scheduler.cjs");

describe("priority-FIFO planning", () => {
  it("derives Claim and Dispatch identities from the request and Work only", () => {
    const seed = createHash("sha256").update("request:x\nwork-1").digest("hex");
    expect(claimIdentity("request:x", "work-1")).toEqual({ claim_id: `c_${seed}_1`, dispatch_id: `d_${seed}_1`, handle: "h1" });
    const state = replayTransactions(h.submit([], [h.work("a"), h.work("b")]));
    const before = canonical(serializeProjection(state));
    const first = planDispatch(state, h.dispatchParameters(), "request:x");
    expect(planDispatch(state, h.dispatchParameters(), "request:x")).toEqual(first);
    expect(canonical(serializeProjection(state))).toBe(before);
    expect(selectionOnly(first)).toEqual({ reason: "prefix_complete", work_ids: [h.work("a").work_id, h.work("b").work_id] });
  });

  it("counts concurrency per pool", () => {
    const other = h.work("o", { graph_id: "other", pool: "other" });
    const planned = h.dispatchNext(h.submit([], [h.work("a"), h.work("b"), other]), { concurrency: 1 });
    expect(planned.decision.operations.map(operation => operation.work_id)).toEqual([h.work("a").work_id]);
    const state = replayTransactions(planned.log);
    expect(planDispatch(state, h.dispatchParameters({ pool: "other", concurrency: 1 }), "request:other").operations.map(operation => operation.work_id)).toEqual([other.work_id]);
    expect(planDispatch(state, h.dispatchParameters({ concurrency: 1 }), "request:more").reason).toBe("capacity_blocked");
  });

  it("validates closed dispatch parameters", () => {
    expect(() => validateDispatchParameters({ ...h.dispatchParameters(), max_bytes: 1 })).toThrow(expect.objectContaining({ code: "request_invalid" }));
    expect(() => validateDispatchParameters({ ...h.dispatchParameters(), concurrency: 0 })).toThrow();
    expect(() => validateDispatchParameters({ ...h.dispatchParameters(), per_account_limit: -1 })).toThrow();
  });
});

describe("worker assignments", () => {
  it("exposes open Claims with payloads and predecessor effects", () => {
    const a = h.work("a");
    const b = h.work("b", { depends_on: [{ kind: "work", work_id: a.work_id }] });
    let planned = h.dispatchNext(h.submit([], [a, b]));
    const first = planned.decision.dispatches[0];
    const effects = [{ kind: "create_issue", resource: { number: 3 } }];
    let log = h.finish(h.bind(h.launch(planned.log, first, true, h.dispatcher, "7"), first, h.workerActor("7")), first, h.workerActor("7"), {}, effects);
    expect(assignmentForDispatch(replayTransactions(log), first)).toEqual({ dispatch_id: first, claims: [] });
    planned = h.dispatchNext(log, {}, { ...h.dispatcher, run_id: "101" });
    log = planned.log;
    const state = replayTransactions(log);
    const dispatch = planned.decision.dispatches[0];
    expect(assignmentForDispatch(state, dispatch)).toEqual({
      dispatch_id: dispatch,
      claims: [
        { handle: "h1", claim_id: state.dispatches.get(dispatch).claims[0], work_id: b.work_id, work: b.payload, result_refs: [{ work_id: a.work_id, result_commit_id: state.works.get(a.work_id).completion_id, descriptor: effects }] },
      ],
    });
    expect(assignmentsForRequest(state, state.dispatches.get(dispatch).request_id)).toEqual([assignmentForDispatch(state, dispatch)]);
    expect(() => assignmentForDispatch(state, "d_missing")).toThrow(expect.objectContaining({ code: "dispatch_missing" }));
  });
});
