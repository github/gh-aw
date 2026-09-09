// @ts-check
import { createRequire } from "node:module";
import { beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);

const now = new Date("2026-08-30T00:00:00Z").getTime();

function workflowRun(id, minutesAgo) {
  const completedAt = new Date(now - minutesAgo * 60 * 1000).toISOString();
  return {
    id,
    run_completed_at: completedAt,
    updated_at: completedAt,
  };
}

function agentJob(stepOverrides = {}) {
  return {
    name: "agent",
    conclusion: "success",
    started_at: new Date(now - 10 * 60 * 1000).toISOString(),
    ...stepOverrides,
  };
}

describe("check_cooldown", () => {
  let mockCore;
  let mockGithub;
  let checkCooldown;

  beforeEach(() => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      setOutput: vi.fn(),
    };
    mockGithub = {
      paginate: vi.fn(),
      rest: {
        actions: {
          getWorkflowRun: vi.fn().mockResolvedValue({ data: { workflow_id: 12345 } }),
          listJobsForWorkflowRun: vi.fn(),
          listWorkflowRuns: vi.fn(),
        },
        rateLimit: {
          get: vi.fn().mockResolvedValue({ data: { resources: {} } }),
        },
      },
    };
    global.core = mockCore;
    global.github = mockGithub;
    global.context = {
      repo: { owner: "octo-org", repo: "octo-repo" },
      workflow: "Cooldown workflow",
      runId: 100,
    };
    process.env.GH_AW_COOLDOWN_SECONDS = "3600";

    delete require.cache[require.resolve("./check_cooldown.cjs")];
    checkCooldown = require("./check_cooldown.cjs");
  });

  it("allows the first agent execution", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({ data: { workflow_runs: [] } });

    await checkCooldown.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
    expect(mockGithub.rest.actions.getWorkflowRun).toHaveBeenCalledWith({
      owner: "octo-org",
      repo: "octo-repo",
      run_id: 100,
    });
    expect(mockGithub.rest.actions.listWorkflowRuns).toHaveBeenCalledWith({
      owner: "octo-org",
      repo: "octo-repo",
      workflow_id: 12345,
      status: "completed",
      per_page: 100,
      page: 1,
    });
  });

  it("blocks when the agent job started within the cooldown", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: [workflowRun(99, 10)] },
    });
    mockGithub.paginate.mockResolvedValue([agentJob()]);

    await checkCooldown.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "false");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("completed within the cooldown period"));
  });

  it("allows execution without fetching jobs when completed runs are older than the cooldown", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: [workflowRun(98, 120)] },
    });

    await checkCooldown.main();

    expect(mockGithub.paginate).not.toHaveBeenCalled();
    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
  });

  it("blocks when the started agent job failed", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: [workflowRun(97, 5)] },
    });
    mockGithub.paginate.mockResolvedValue([agentJob({ conclusion: "failure" })]);

    await checkCooldown.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "false");
  });

  it("ignores completed runs where the agent job was skipped", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: [workflowRun(96, 5)] },
    });
    mockGithub.paginate.mockResolvedValue([agentJob({ conclusion: "skipped", started_at: null })]);

    await checkCooldown.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
  });

  it("checks newer completions before allowing older completions on the same page", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: [workflowRun(95, 120), workflowRun(94, 10)] },
    });
    mockGithub.paginate.mockImplementation((_method, options) => {
      if (options.run_id === 94) {
        return Promise.resolve([agentJob()]);
      }
      return Promise.reject(new Error(`unexpected run_id ${options.run_id}`));
    });

    await checkCooldown.main();

    expect(mockGithub.paginate).toHaveBeenCalledTimes(1);
    expect(mockGithub.paginate.mock.calls[0][1]).toMatchObject({ run_id: 94 });
    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "false");
  });

  it("uses run_completed_at instead of updated_at for the cooldown window", async () => {
    const run = workflowRun(93, 120);
    run.updated_at = new Date(now - 10 * 60 * 1000).toISOString();
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: [run] },
    });

    await checkCooldown.main();

    expect(mockGithub.paginate).not.toHaveBeenCalled();
    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
  });

  it("stops after the in-window job lookup budget is reached", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: Array.from({ length: 51 }, (_, index) => workflowRun(200 + index, 10)) },
    });
    mockGithub.paginate.mockResolvedValue([]);

    await checkCooldown.main();

    expect(mockGithub.paginate).toHaveBeenCalledTimes(50);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("job lookup budget"));
    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
  });

  it("warns when the run page scan reaches its limit", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: { workflow_runs: Array.from({ length: 100 }, (_, index) => workflowRun(300 + index, 120)) },
    });

    await checkCooldown.main();

    expect(mockGithub.rest.actions.listWorkflowRuns).toHaveBeenCalledTimes(5);
    expect(mockGithub.paginate).not.toHaveBeenCalled();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("5 workflow run pages"));
    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
  });

  it("fails open when the workflow id cannot be resolved", async () => {
    mockGithub.rest.actions.getWorkflowRun.mockResolvedValue({ data: {} });

    await checkCooldown.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Cannot resolve workflow id"));
  });

  it("fails open when run history cannot be queried", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockRejectedValue(new Error("API unavailable"));

    await checkCooldown.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("cooldown_ok", "true");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Cooldown check failed"));
  });
});
