// @ts-check
import { describe, expect, it, vi } from "vitest";
import { launchFailureReason, nativeId, postQueueDispatch } from "./work_queue_native.cjs";

describe("worker launch transport", () => {
  it("posts workflow_dispatch for the worker workflow at the configured ref", async () => {
    const createWorkflowDispatch = vi.fn(async () => ({ status: 204 }));
    const inputs = { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: "d_1" }) };
    await postQueueDispatch({ rest: { actions: { createWorkflowDispatch } } }, { owner: "owner", repo: "repo", workflow: ".github/workflows/worker.lock.yml", ref: "refs/heads/main" }, inputs);
    expect(createWorkflowDispatch).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", workflow_id: "worker.lock.yml", ref: "refs/heads/main", inputs }));
    expect(createWorkflowDispatch.mock.calls[0][0]).not.toHaveProperty("return_run_details");
  });

  it("refuses a launch without a ref and propagates API errors", async () => {
    const client = { rest: { actions: { createWorkflowDispatch: vi.fn(async () => Promise.reject(Object.assign(new Error("Server Error"), { status: 503 }))) } } };
    await expect(postQueueDispatch(client, { owner: "o", repo: "r", workflow: ".github/workflows/w.lock.yml", ref: "" }, {})).rejects.toThrow("work_queue_dispatch_ref_missing");
    expect(client.rest.actions.createWorkflowDispatch).not.toHaveBeenCalled();
    await expect(postQueueDispatch(client, { owner: "o", repo: "r", workflow: ".github/workflows/w.lock.yml", ref: "main" }, {})).rejects.toMatchObject({ status: 503 });
  });

  it("classifies launch failures for receipts", () => {
    expect(launchFailureReason({ status: 404 })).toBe("http_4xx");
    expect(launchFailureReason({ status: 502 })).toBe("http_5xx");
    expect(launchFailureReason(new Error("socket hang up"))).toBe("transport");
  });

  it("accepts only lossless positive identities", () => {
    expect(nativeId(12)).toBe("12");
    expect(nativeId("12")).toBe("12");
    for (const value of [0, -1, 1.5, "012", "", Number.MAX_SAFE_INTEGER + 1]) expect(() => nativeId(value)).toThrow();
  });
});
