// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main, resolveCurrentWorkflowFile, readContinuationDepth } from "./request_continuation.cjs";

const createWorkflowDispatch = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  createWorkflowDispatch.mockResolvedValue({ data: { workflow_run_id: 4242 } });
  global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn() } };
  global.github = { rest: { actions: { createWorkflowDispatch } } };
  global.context = {
    repo: { owner: "octo", repo: "app" },
    runId: 100,
    eventName: "workflow_dispatch",
    ref: "refs/heads/feature",
    workflow: "Continue",
    actor: "octocat",
    sha: "abc123",
    payload: {},
  };
  vi.stubEnv("GITHUB_WORKFLOW_REF", "octo/app/.github/workflows/continue.lock.yml@refs/heads/feature");
  vi.stubEnv("GITHUB_REF", "refs/heads/feature");
  vi.stubEnv("GITHUB_HEAD_REF", "");
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "");
});

afterEach(() => {
  vi.unstubAllEnvs();
});

function dispatchedContext() {
  expect(createWorkflowDispatch).toHaveBeenCalledTimes(1);
  const request = createWorkflowDispatch.mock.calls[0][0];
  return { request, awContext: JSON.parse(request.inputs.aw_context) };
}

describe("request_continuation handler", () => {
  it("dispatches the current workflow on the same ref with continuation lineage", async () => {
    const handler = await main({ max_continuations: 3 });
    const result = await handler({ type: "request_continuation", hints: "  Finished step 1; next open the PR.  " });
    expect(result).toMatchObject({ success: true, workflow_name: "continue.lock.yml", depth: 1, run_id: 4242 });
    const { request, awContext } = dispatchedContext();
    expect(request).toMatchObject({ owner: "octo", repo: "app", workflow_id: "continue.lock.yml", ref: "refs/heads/feature" });
    expect(String(awContext.run_id)).toBe("100");
    expect(awContext.continuation).toEqual({ depth: 1, max: 3, hints: "Finished step 1; next open the PR." });
  });

  it("increments the inbound continuation depth", async () => {
    global.context.payload = { inputs: { aw_context: JSON.stringify({ run_id: "50", continuation: { depth: 2, max: 5, hints: "old" } }) } };
    const handler = await main({});
    const result = await handler({ hints: "keep going" });
    expect(result).toMatchObject({ success: true, depth: 3 });
    expect(dispatchedContext().awContext.continuation).toEqual({ depth: 3, max: 5, hints: "keep going" });
  });

  it("skips dispatch once the chain reaches the configured maximum", async () => {
    global.context.payload = { inputs: { aw_context: JSON.stringify({ run_id: "50", continuation: { depth: 2, max: 2, hints: "old" } }) } };
    const handler = await main({ max_continuations: 2 });
    const result = await handler({ hints: "more" });
    expect(result).toMatchObject({ success: true, skipped: true });
    expect(createWorkflowDispatch).not.toHaveBeenCalled();
  });

  it("uses the pull request head branch for pull_request runs", async () => {
    vi.stubEnv("GITHUB_HEAD_REF", "topic");
    vi.stubEnv("GITHUB_REF", "refs/pull/7/merge");
    const handler = await main({});
    await handler({ hints: "next" });
    expect(dispatchedContext().request.ref).toBe("refs/heads/topic");
  });

  it("accepts only one request per run and rejects empty or oversized hints", async () => {
    expect(await (await main({}))({ hints: "   " })).toMatchObject({ success: false });
    expect(await (await main({}))({ hints: "x".repeat(4001) })).toMatchObject({ success: false });
    const handler = await main({});
    expect(await handler({ hints: "first" })).toMatchObject({ success: true });
    expect(await handler({ hints: "second" })).toMatchObject({ success: false });
    expect(createWorkflowDispatch).toHaveBeenCalledTimes(1);
  });

  it("previews without dispatching in staged mode", async () => {
    const handler = await main({ staged: true });
    const result = await handler({ hints: "next" });
    expect(result).toMatchObject({ success: true, staged: true, depth: 1 });
    expect(createWorkflowDispatch).not.toHaveBeenCalled();
  });

  it("reports dispatch failures", async () => {
    createWorkflowDispatch.mockRejectedValue(new Error("403 Resource not accessible"));
    const handler = await main({});
    const result = await handler({ hints: "next" });
    expect(result.success).toBe(false);
    expect(result.error).toContain("403");
  });
});

describe("request_continuation helpers", () => {
  it("resolves the workflow file from GITHUB_WORKFLOW_REF", () => {
    expect(resolveCurrentWorkflowFile("o/r/.github/workflows/a.b.lock.yml@refs/heads/main")).toBe("a.b.lock.yml");
    expect(() => resolveCurrentWorkflowFile("")).toThrow("GITHUB_WORKFLOW_REF");
  });

  it("validates continuation depth", () => {
    expect(readContinuationDepth(null)).toBe(0);
    expect(readContinuationDepth({ continuation: { depth: 4 } })).toBe(4);
    expect(() => readContinuationDepth({ continuation: { depth: -1 } })).toThrow("non-negative");
  });
});
