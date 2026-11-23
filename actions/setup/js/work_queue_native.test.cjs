// @ts-check
import { describe, expect, it, vi } from "vitest";
import { launchFailureReason, launchedRunEvidence, nativeId, postQueueDispatch, verifyLaunchedRun } from "./work_queue_native.cjs";
import { WORKER_REVISION, WORKER_WORKFLOW, workflowRun } from "./work_queue_test_helpers.cjs";

const destination = { owner: "owner", repo: "repo", workflow: WORKER_WORKFLOW };

function runsClient(run) {
  return { rest: { actions: { getWorkflowRun: vi.fn(async () => (run instanceof Error ? Promise.reject(run) : { status: 200, data: run })) } } };
}

describe("worker launch transport", () => {
  it("posts workflow_dispatch for the worker workflow at the configured ref and asks for the created run", async () => {
    const createWorkflowDispatch = vi.fn(async () => ({ status: 200, data: { workflow_run_id: 200 } }));
    const inputs = { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: "d_1" }) };
    await postQueueDispatch({ rest: { actions: { createWorkflowDispatch } } }, { owner: "owner", repo: "repo", workflow: ".github/workflows/worker.lock.yml", ref: "refs/heads/main" }, inputs);
    expect(createWorkflowDispatch).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", workflow_id: "worker.lock.yml", ref: "refs/heads/main", inputs, return_run_details: true }));
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
    expect(launchFailureReason({ code: "work_queue_launch_unverified", status: 404 })).toBe("unverified");
  });

  it("accepts only lossless positive identities", () => {
    expect(nativeId(12)).toBe("12");
    expect(nativeId("12")).toBe("12");
    for (const value of [0, -1, 1.5, "012", "", Number.MAX_SAFE_INTEGER + 1]) expect(() => nativeId(value)).toThrow();
  });
});

describe("dispatcher launch evidence", () => {
  it("reads back the created run and returns its ID and revision", async () => {
    const client = runsClient(workflowRun("200"));
    await expect(launchedRunEvidence(client, destination, { status: 200, data: { workflow_run_id: 200 } })).resolves.toEqual({ run_id: "200", revision: WORKER_REVISION });
    expect(client.rest.actions.getWorkflowRun).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", run_id: 200 }));
  });

  it("has no evidence without a returned run ID (return_run_details unsupported) and never reads a guessed run", async () => {
    const client = runsClient(workflowRun("200"));
    for (const response of [{ status: 204 }, { status: 200, data: {} }, { status: 200, data: { workflow_run_id: "0" } }]) {
      await expect(launchedRunEvidence(client, destination, response)).rejects.toMatchObject({ code: "work_queue_launch_unverified" });
    }
    expect(client.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
  });

  it("has no evidence when the created run cannot be read or is not the launched workflow_dispatch run", async () => {
    const response = { status: 200, data: { workflow_run_id: 200 } };
    await expect(launchedRunEvidence(runsClient(Object.assign(new Error("Not Found"), { status: 404 })), destination, response)).rejects.toMatchObject({ code: "work_queue_launch_unverified" });
    for (const overrides of [{ path: ".github/workflows/other.lock.yml" }, { event: "push" }, { head_sha: "main" }, { id: 201 }, { repository: { full_name: "other/repo" } }]) {
      await expect(launchedRunEvidence(runsClient(workflowRun("200", overrides)), destination, response)).rejects.toMatchObject({ code: "work_queue_launch_unverified" });
    }
  });

  it("verifies a worker's own run against the recorded revision", () => {
    const expected = { ...destination, run_id: "200", revision: WORKER_REVISION };
    expect(verifyLaunchedRun(workflowRun("200", { path: `${WORKER_WORKFLOW}@refs/heads/main` }), expected)).toBe(WORKER_REVISION);
    expect(() => verifyLaunchedRun(workflowRun("200", { head_sha: "d".repeat(40) }), expected)).toThrow(/work_queue_launch_unverified/);
  });
});
