import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import path from "node:path";

const { withProjectionLocks } = require("./work_queue_issue_coordination.cjs");
const { digest } = require("./work_queue_codec.cjs");
const { registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
const { fixture, mock } = require("./work_queue_issues_checks.cjs");
const { wrapGithubClient } = require("./work_queue_issue_pacing.cjs");
const { main } = require("./work_queue_issues.cjs");

function simulateDeadlines() {
  vi.useFakeTimers();
  vi.spyOn(AbortSignal, "timeout").mockImplementation(milliseconds => {
    const controller = new AbortController();
    setTimeout(() => controller.abort(new DOMException("Request deadline exceeded", "TimeoutError")), milliseconds);
    return controller.signal;
  });
}

function waitForAbort(request) {
  return new Promise((_resolve, reject) => {
    if (request?.signal?.aborted) reject(request.signal.reason);
    else request?.signal?.addEventListener("abort", () => reject(request.signal.reason), { once: true });
  });
}

describe("projection lock acquisition diagnostics", () => {
  let stderr;
  const output = () => stderr.mock.calls.map(call => call[0]).join("");

  beforeEach(() => {
    vi.stubEnv("DEBUG", "");
    vi.stubEnv("ACTIONS_RUNNER_DEBUG", "");
    vi.stubEnv("RUNNER_DEBUG", "");
    vi.stubEnv("GH_AW_PROMPTS_DIR", path.resolve(import.meta.dirname, "../md"));
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

  it("starts paced GraphQL acquisition and release deadlines at dispatch", async () => {
    simulateDeadlines();
    const keys = Array.from({ length: 50 }, (_, index) => `work:${index}`);
    const githubClient = {
      graphql: vi.fn((query, variables) => {
        expect(variables.request.signal.aborted).toBe(false);
        if (query.includes("WorkQueueProjectionUnlocks")) return waitForAbort(variables.request);
        return Object.fromEntries(
          keys.map((_key, index) => {
            const input = variables[`l${index}`];
            return [`l${index}`, { ref: { id: `lock-${index}`, prefix: "refs/heads/", name: input.name.slice("refs/heads/".length), target: { oid: input.oid } } }];
          })
        );
      }),
    };
    const { client } = wrapGithubClient(githubClient);
    const pending = withProjectionLocks({ githubClient: client, repositoryId: "repository", head: "a".repeat(40), keys }, async () => ({}));
    const rejected = expect(pending).rejects.toMatchObject({ code: "projection_coordination_pending" });

    await vi.advanceTimersByTimeAsync(50000);
    expect(githubClient.graphql).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(50000);
    expect(githubClient.graphql).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(29999);
    expect(githubClient.graphql).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1);
    await rejected;
    expect(githubClient.graphql).toHaveBeenCalledTimes(2);
  });

  it.each([false, true])("bounds REST acquisition at 15 seconds and cleans up only acknowledged locks (partial=%s)", async partial => {
    simulateDeadlines();
    const createRef = vi.fn(options => waitForAbort(options.request));
    if (partial) createRef.mockResolvedValueOnce({});
    const deleteRef = vi.fn().mockResolvedValue({});
    const githubClient = { rest: { git: { createRef, deleteRef } } };
    registerQueueLedgerClient(githubClient, githubClient);
    const operation = vi.fn();
    let settled = false;
    const pending = withProjectionLocks({ githubClient, owner: "owner", repo: "repo", head: "a".repeat(40), keys: partial ? ["work:a", "work:b"] : ["work:a"] }, operation).finally(() => {
      settled = true;
    });
    const rejected = expect(pending).rejects.toMatchObject({ code: "projection_coordination_pending" });

    await vi.advanceTimersByTimeAsync(14999);
    expect(settled).toBe(false);
    expect(operation).not.toHaveBeenCalled();
    expect(deleteRef).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await rejected;
    expect(settled).toBe(true);
    expect(operation).not.toHaveBeenCalled();
    expect(createRef).toHaveBeenCalledTimes(partial ? 2 : 1);
    expect(createRef.mock.calls.every(([options]) => options.request.retries === 0)).toBe(true);
    expect(deleteRef).toHaveBeenCalledTimes(partial ? 1 : 0);
    if (partial) expect(deleteRef).toHaveBeenCalledWith(expect.objectContaining({ ref: `heads/gh-aw-issue-projection/${digest("work:a")}`, request: expect.objectContaining({ retries: 0 }) }));
    await vi.advanceTimersByTimeAsync(30000);
    expect(createRef).toHaveBeenCalledTimes(partial ? 2 : 1);
    expect(deleteRef).toHaveBeenCalledTimes(partial ? 1 : 0);
  });

  it("classifies a REST release deadline at 15 seconds without retrying an uncertain delete", async () => {
    simulateDeadlines();
    const createRef = vi.fn().mockResolvedValue({});
    const deleteRef = vi.fn(options => waitForAbort(options.request));
    const githubClient = { rest: { git: { createRef, deleteRef } } };
    registerQueueLedgerClient(githubClient, githubClient);
    const operation = vi.fn().mockResolvedValue({});
    let settled = false;
    const pending = withProjectionLocks({ githubClient, owner: "owner", repo: "repo", head: "a".repeat(40), keys: ["work:work"] }, operation).finally(() => {
      settled = true;
    });
    const rejected = expect(pending).rejects.toMatchObject({ code: "projection_coordination_pending", cause: { name: "TimeoutError" }, originalError: { name: "TimeoutError" } });

    await vi.advanceTimersByTimeAsync(14999);
    expect(settled).toBe(false);
    expect(operation).toHaveBeenCalledTimes(1);
    expect(deleteRef).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    await rejected;
    expect(settled).toBe(true);
    expect(createRef).toHaveBeenCalledTimes(1);
    expect(deleteRef).toHaveBeenCalledTimes(1);
    expect(deleteRef.mock.calls[0][0].request.retries).toBe(0);
    expect(output()).toContain("projection.locks.release.failed");
    await vi.advanceTimersByTimeAsync(30000);
    expect(deleteRef).toHaveBeenCalledTimes(1);
  });

  it("classifies a GraphQL release deadline without retrying an uncertain delete", async () => {
    simulateDeadlines();
    const githubClient = {
      graphql: vi.fn((query, options) => {
        if (query.includes("WorkQueueProjectionLocks")) return Promise.resolve({ l0: { ref: { id: "lock", prefix: "refs/heads/", name: options.l0.name.slice(11), target: { oid: options.l0.oid } } } });
        return waitForAbort(options.request);
      }),
    };
    const operation = vi.fn().mockResolvedValue({});
    let settled = false;
    const pending = withProjectionLocks({ githubClient, repositoryId: "repository", head: "a".repeat(40), keys: ["work:work"] }, operation).finally(() => {
      settled = true;
    });
    const rejected = expect(pending).rejects.toMatchObject({ code: "projection_coordination_pending", cause: { name: "TimeoutError" }, originalError: { name: "TimeoutError" } });
    await vi.advanceTimersByTimeAsync(29999);
    expect(settled).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    await rejected;
    expect(settled).toBe(true);
    expect(operation).toHaveBeenCalledTimes(1);
    expect(githubClient.graphql).toHaveBeenCalledTimes(2);
    expect(githubClient.graphql.mock.calls[1][1].request.retries).toBe(0);
    expect(output()).toContain("projection.locks.release.failed");
    await vi.advanceTimersByTimeAsync(30000);
    expect(githubClient.graphql).toHaveBeenCalledTimes(2);
  });

  it.each(["REST", "GraphQL"])("reports %s release timeouts as pending coordination through the Issue projection caller", async transport => {
    simulateDeadlines();
    const m = mock(fixture());
    const deadline = transport === "REST" ? 15000 : 30000;
    let release;
    if (transport === "REST") {
      release = vi.spyOn(m.options.githubClient.rest.git, "deleteRef").mockImplementation(options => waitForAbort(options.request));
    } else {
      const read = m.options.readCheckedQueue;
      m.options.readCheckedQueue = async (...args) => ({ ...(await read(...args)), repositoryId: "Repository1" });
      const graphql = m.options.githubClient.graphql;
      release = vi.fn(options => waitForAbort(options.request));
      m.options.githubClient.graphql = (query, options) => (query.includes("WorkQueueProjectionUnlocks") ? release(options) : graphql(query, options));
    }
    let settled = false;
    const pending = main(m.options).finally(() => {
      settled = true;
    });

    await vi.advanceTimersByTimeAsync(deadline - 1);
    expect(settled).toBe(false);
    expect(release).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    const result = await pending;
    expect(result.pending).toHaveLength(1);
    expect(result.pending[0].reason).toContain("(projection_coordination_pending)");
    expect(result.pending[0].reason).not.toContain("(projection_unavailable)");
    expect(m.locks.size).toBeGreaterThan(0);
    expect(release).toHaveBeenCalledTimes(1);
    const diagnostics = JSON.stringify([result, m.warnings, output()]);
    expect(diagnostics).not.toContain("Request deadline exceeded");
    expect(output()).toContain("projection.locks.release.failed");
    await vi.advanceTimersByTimeAsync(deadline);
    expect(release).toHaveBeenCalledTimes(1);
  });

  it.each(["REST", "GraphQL"])("preserves a non-deadline %s release error for sanitizing diagnostics", async transport => {
    const failure = Object.assign(new Error("private-release-payload"), { status: 403 });
    const operation = vi.fn().mockResolvedValue({});
    let options;
    let release;
    if (transport === "REST") {
      release = vi.fn().mockRejectedValue(failure);
      const githubClient = { rest: { git: { createRef: vi.fn().mockResolvedValue({}), deleteRef: release } } };
      registerQueueLedgerClient(githubClient, githubClient);
      options = { githubClient, owner: "owner", repo: "repo", head: "a".repeat(40), keys: ["work:work"] };
    } else {
      release = vi.fn().mockRejectedValue(failure);
      const githubClient = {
        graphql: vi.fn((query, options) => {
          if (query.includes("WorkQueueProjectionLocks")) return Promise.resolve({ l0: { ref: { id: "lock", prefix: "refs/heads/", name: options.l0.name.slice(11), target: { oid: options.l0.oid } } } });
          return release();
        }),
      };
      options = { githubClient, repositoryId: "repository", head: "a".repeat(40), keys: ["work:work"] };
    }

    await expect(withProjectionLocks(options, operation)).rejects.toBe(failure);
    expect(operation).toHaveBeenCalledTimes(1);
    expect(release).toHaveBeenCalledTimes(1);
    expect(output()).toContain("projection.locks.release.failed");
    expect(output()).toContain("http_status=403");
    expect(output()).not.toContain("private-release-payload");
  });
});
