import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { withProjectionLocks } = require("./work_queue_issue_coordination.cjs");

function simulateDeadlines() {
  vi.useFakeTimers();
  vi.spyOn(AbortSignal, "timeout").mockImplementation(milliseconds => {
    const controller = new AbortController();
    setTimeout(() => controller.abort(new DOMException("Request deadline exceeded", "TimeoutError")), milliseconds);
    return controller.signal;
  });
}

function waitForAbort(request) {
  return new Promise((_resolve, reject) => request?.signal?.addEventListener("abort", () => reject(request.signal.reason), { once: true }));
}

describe("projection lock acquisition diagnostics", () => {
  let stderr;
  const output = () => stderr.mock.calls.map(call => call[0]).join("");

  beforeEach(() => {
    vi.stubEnv("DEBUG", "");
    vi.stubEnv("ACTIONS_RUNNER_DEBUG", "");
    vi.stubEnv("RUNNER_DEBUG", "");
    stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it("reports incomplete acquisition before rejecting an unscoped projection without inventing a target count", async () => {
    const githubClient = { graphql: vi.fn().mockResolvedValue({ l0: null }) };
    const operation = vi.fn();

    await expect(withProjectionLocks({ githubClient, repositoryId: "private-repository", head: "a".repeat(40), keys: ["work:private-work"] }, operation)).rejects.toMatchObject({ code: "projection_coordination_pending" });

    expect(operation).not.toHaveBeenCalled();
    expect(githubClient.graphql).toHaveBeenCalledTimes(1);
    expect(githubClient.graphql.mock.calls[0][1].request.retries).toBe(0);
    expect(output()).toContain("projection.locks.acquire.pending requested=1 acknowledged=0");
    expect(output()).not.toContain("pending_targets");
    expect(output()).not.toContain("private");
  });

  it("retains the known pending target count for scoped projections", async () => {
    const githubClient = { graphql: vi.fn().mockResolvedValue({ l0: null }) };
    const operation = vi.fn();

    const result = await withProjectionLocks({ githubClient, repositoryId: "private-repository", head: "a".repeat(40), keys: ["work:private-work"], scopes: [{ work_id: "private-work", keys: ["work:private-work"] }] }, operation);

    expect(result.pending).toHaveLength(1);
    expect(operation).not.toHaveBeenCalled();
    expect(githubClient.graphql).toHaveBeenCalledTimes(1);
    expect(output()).toContain("projection.locks.acquire.pending requested=1 acknowledged=0 pending_targets=1");
    expect(output()).not.toContain("private");
  });

  it("bounds an unacknowledged acquisition without projecting, retrying, or deleting uncertain locks", async () => {
    simulateDeadlines();
    const githubClient = { graphql: vi.fn((_query, options) => waitForAbort(options.request)) };
    const operation = vi.fn();
    let settled = false;
    const pending = withProjectionLocks({ githubClient, repositoryId: "repository", head: "a".repeat(40), keys: ["work:work"], scopes: [{ work_id: "work", keys: ["work:work"] }] }, operation).then(result => {
      settled = true;
      return result;
    });
    await vi.advanceTimersByTimeAsync(29999);
    expect(settled).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    expect((await pending).pending).toHaveLength(1);
    expect(operation).not.toHaveBeenCalled();
    expect(githubClient.graphql).toHaveBeenCalledTimes(1);
  });

  it("surfaces a release deadline without retrying an uncertain delete", async () => {
    simulateDeadlines();
    const githubClient = {
      graphql: vi.fn((query, options) => {
        if (query.includes("WorkQueueProjectionLocks")) return Promise.resolve({ l0: { ref: { id: "lock", prefix: "refs/heads/", name: options.l0.name.slice(11), target: { oid: options.l0.oid } } } });
        return waitForAbort(options.request);
      }),
    };
    const operation = vi.fn().mockResolvedValue({});
    const pending = withProjectionLocks({ githubClient, repositoryId: "repository", head: "a".repeat(40), keys: ["work:work"] }, operation);
    const rejected = expect(pending).rejects.toMatchObject({ name: "TimeoutError" });
    await vi.advanceTimersByTimeAsync(30000);
    await rejected;
    expect(operation).toHaveBeenCalledTimes(1);
    expect(githubClient.graphql).toHaveBeenCalledTimes(2);
    expect(output()).toContain("projection.locks.release.failed");
  });
});
