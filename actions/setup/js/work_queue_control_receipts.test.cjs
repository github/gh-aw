// @ts-check
import { describe, expect, it } from "vitest";
import { blockedReceipt, controlResult } from "./work_queue_control_receipts.cjs";

describe("work_queue_requests output", () => {
  it("succeeds when every intent committed, including failed launches", () => {
    const receipts = [
      { intent_id: "submit", request_id: "request:1", commit_id: "q_1", status: "committed" },
      {
        intent_id: "dispatch",
        request_id: "request:2",
        commit_id: "q_2",
        status: "committed",
        dispatches: 2,
        launches: [
          { dispatch_id: "d_1", state: "started" },
          { dispatch_id: "d_2", state: "launch_failed", reason: "http_5xx" },
        ],
      },
    ];
    const result = controlResult(/** @type {any} */ receipts, 1);
    expect(result).toMatchObject({ version: 4, success: true, status: "committed", receipts, remaining_dispatches: 1 });
    expect(result.summary).toContain("Started launches: 1");
    expect(result.summary).toContain("`d_2` failed (`http_5xx`)");
  });

  it("is blocked when any intent is blocked", () => {
    const result = controlResult([blockedReceipt("work_queue_pending_limit", "submit")], 0);
    expect(result).toMatchObject({ success: false, status: "blocked", receipts: [{ intent_id: "submit", status: "blocked", reason: "work_queue_pending_limit" }] });
    expect(result.summary).toContain("pending_limit");
    expect(blockedReceipt("staged_queue_intents_invalid")).toEqual({ status: "blocked", reason: "staged_queue_intents_invalid" });
  });
});
