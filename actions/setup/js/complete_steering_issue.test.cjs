// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

describe("complete_steering_issue", () => {
  let originalGlobals;
  let originalEnv;

  beforeEach(() => {
    originalGlobals = {
      core: global.core,
      github: global.github,
      context: global.context,
    };
    originalEnv = { ...process.env };
    global.core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn(), setFailed: vi.fn() };
    global.context = { repo: { owner: "owner", repo: "repo" } };
    global.github = {
      rest: {
        issues: {
          createComment: vi.fn().mockResolvedValue({ data: {} }),
          update: vi.fn().mockResolvedValue({ data: {} }),
        },
      },
    };
    process.env.GH_AW_STEERING_ISSUE_NUMBER = "42";
    delete process.env.GH_AW_FAILURE_ISSUE_NUMBER;
    delete process.env.GH_AW_CREATED_PR_NUMBER;
    delete process.env.GH_AW_CREATED_PR_URL;
    process.env.GH_AW_NEEDS = JSON.stringify({
      agent: { result: "success" },
      safe_outputs: { result: "success" },
    });
  });

  afterEach(() => {
    global.core = originalGlobals.core;
    global.github = originalGlobals.github;
    global.context = originalGlobals.context;
    process.env = originalEnv;
    vi.useRealTimers();
    vi.resetModules();
  });

  it("closes the steering issue and links the created pull request", async () => {
    process.env.GH_AW_CREATED_PR_NUMBER = "7";
    process.env.GH_AW_CREATED_PR_URL = "https://github.com/owner/repo/pull/7";
    const { main } = await import("./complete_steering_issue.cjs");
    await main();

    expect(global.github.rest.issues.createComment).toHaveBeenCalledWith(
      expect.objectContaining({
        issue_number: 42,
        body: "The workflow completed successfully and created [pull request \\#7](https://github.com/owner/repo/pull/7).",
      })
    );
    expect(global.github.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 42, state: "closed", state_reason: "completed" }));
  });

  it("keeps the issue open when it was reused for failure reporting", async () => {
    process.env.GH_AW_FAILURE_ISSUE_NUMBER = "42";
    const { main } = await import("./complete_steering_issue.cjs");
    await main();

    expect(global.github.rest.issues.update).not.toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith("Keeping steering issue #42 open as the agent failure issue");
  });

  it("still closes the issue when the completion comment is rate limited", async () => {
    global.github.rest.issues.createComment.mockRejectedValue(Object.assign(new Error("API rate limit exceeded for installation"), { status: 403 }));
    const { main } = await import("./complete_steering_issue.cjs");
    await main();

    expect(global.core.warning).toHaveBeenCalledWith("Failed to comment on steering issue #42: API rate limit exceeded for installation");
    expect(global.github.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 42, state: "closed" }));
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("retries a rate-limited closure without duplicating the completion comment", async () => {
    vi.useFakeTimers();
    const startedAt = Date.now();
    global.github.rest.issues.update.mockRejectedValueOnce(Object.assign(new Error("API rate limit exceeded for installation"), { status: 403 }));
    const { main } = await import("./complete_steering_issue.cjs");
    const completion = main();
    await vi.runAllTimersAsync();
    await completion;

    expect(Date.now() - startedAt).toBeGreaterThanOrEqual(30_000);
    expect(global.github.rest.issues.createComment).toHaveBeenCalledTimes(1);
    expect(global.github.rest.issues.update).toHaveBeenCalledTimes(2);
    expect(global.core.info).toHaveBeenCalledWith("Closed steering issue #42");
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("fails the step when rate limiting persists through all closure retries", async () => {
    vi.useFakeTimers();
    const startedAt = Date.now();
    global.github.rest.issues.update.mockRejectedValue(Object.assign(new Error("API rate limit exceeded for installation"), { status: 403 }));
    const { main } = await import("./complete_steering_issue.cjs");
    const completion = main();
    await vi.runAllTimersAsync();
    await completion;

    expect(global.github.rest.issues.update).toHaveBeenCalledTimes(4);
    expect(Date.now() - startedAt).toBeGreaterThanOrEqual(210_000);
    expect(Date.now() - startedAt).toBeLessThan(225_000);
    expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to close steering issue #42:"));
    expect(global.core.info).not.toHaveBeenCalledWith("Closed steering issue #42");
  });

  it("fails the step without retrying a non-transient closure error", async () => {
    global.github.rest.issues.update.mockRejectedValue(Object.assign(new Error("Resource not accessible by integration"), { status: 403 }));
    const { main } = await import("./complete_steering_issue.cjs");
    await main();

    expect(global.github.rest.issues.update).toHaveBeenCalledTimes(1);
    expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to close steering issue #42:"));
  });

  it("keeps the issue open when a job failed and failure reporting did not complete", async () => {
    process.env.GH_AW_NEEDS = JSON.stringify({ agent: { result: "failure" } });
    const { main } = await import("./complete_steering_issue.cjs");
    await main();

    expect(global.github.rest.issues.update).not.toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith("Keeping steering issue #42 open because the workflow did not complete successfully");
  });

  it("does nothing when no steering issue was created", async () => {
    delete process.env.GH_AW_STEERING_ISSUE_NUMBER;
    const { main } = await import("./complete_steering_issue.cjs");
    await main();

    expect(global.github.rest.issues.update).not.toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith("No steering issue to complete");
  });
});
