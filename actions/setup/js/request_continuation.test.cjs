// @ts-check
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main, readContinuationDepth } from "./request_continuation.cjs";

let tmpDir;
let requestFile;

beforeEach(() => {
  vi.clearAllMocks();
  tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "request-continuation-"));
  requestFile = path.join(tmpDir, "continuation", "continuation-request.json");
  global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn() } };
  global.github = { rest: { actions: { createWorkflowDispatch: vi.fn() } } };
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
  vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "");
  vi.stubEnv("GH_AW_CONTINUATION_REQUEST_FILE", requestFile);
});

afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(tmpDir, { recursive: true, force: true });
});

function recordedRequest() {
  return JSON.parse(fs.readFileSync(requestFile, "utf8"));
}

describe("request_continuation handler", () => {
  it("records a continuation request for the relay instead of dispatching", async () => {
    const handler = await main({ max_continuations: 3 });
    const result = await handler({ type: "request_continuation", hints: "  Finished step 1; next open the PR.  " });
    expect(result).toMatchObject({ success: true, depth: 1 });
    expect(global.github.rest.actions.createWorkflowDispatch).not.toHaveBeenCalled();
    expect(global.core.setOutput).toHaveBeenCalledWith("continuation_requested", "true");
    const request = recordedRequest();
    expect(request).toMatchObject({ version: 1, hints: "Finished step 1; next open the PR.", depth: 1, max: 3, parent_run_id: "100", parent_run_attempt: "2" });
    expect(request.aw_context).toMatchObject({ run_id: "100", repo: "octo/app" });
  });

  it("increments the inbound continuation depth", async () => {
    global.context.payload = { inputs: { aw_context: JSON.stringify({ run_id: "50", continuation: { depth: 2, max: 5 } }) } };
    const handler = await main({});
    expect(await handler({ hints: "keep going" })).toMatchObject({ success: true, depth: 3 });
    expect(recordedRequest()).toMatchObject({ depth: 3, max: 5, hints: "keep going" });
  });

  it("does not record a request once the chain reaches the configured maximum", async () => {
    global.context.payload = { inputs: { aw_context: JSON.stringify({ run_id: "50", continuation: { depth: 2, max: 2 } }) } };
    const handler = await main({ max_continuations: 2 });
    expect(await handler({ hints: "more" })).toMatchObject({ success: true, skipped: true });
    expect(fs.existsSync(requestFile)).toBe(false);
    expect(global.core.setOutput).not.toHaveBeenCalled();
  });

  it("accepts only one request per run and rejects empty or oversized hints", async () => {
    expect(await (await main({}))({ hints: "   " })).toMatchObject({ success: false });
    expect(await (await main({}))({ hints: "x".repeat(4001) })).toMatchObject({ success: false });
    const handler = await main({});
    expect(await handler({ hints: "first" })).toMatchObject({ success: true });
    expect(await handler({ hints: "second" })).toMatchObject({ success: false });
    expect(recordedRequest().hints).toBe("first");
  });

  it("previews without recording a request in staged mode", async () => {
    const handler = await main({ staged: true });
    expect(await handler({ hints: "next" })).toMatchObject({ success: true, staged: true, depth: 1 });
    expect(fs.existsSync(requestFile)).toBe(false);
    expect(global.core.setOutput).not.toHaveBeenCalled();
  });
});

describe("request_continuation helpers", () => {
  it("validates continuation depth", () => {
    expect(readContinuationDepth(null)).toBe(0);
    expect(readContinuationDepth({ continuation: { depth: 4 } })).toBe(4);
    expect(() => readContinuationDepth({ continuation: { depth: -1 } })).toThrow("non-negative");
  });
});
