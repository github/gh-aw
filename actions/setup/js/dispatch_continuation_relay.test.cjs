// @ts-check
"use strict";

import { afterEach, describe, expect, it, vi } from "vitest";
const { main } = require("./dispatch_continuation_relay.cjs");

afterEach(() => {
  vi.unstubAllEnvs();
});

function fixture() {
  vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
  const summary = { addHeading: vi.fn(() => summary), addRaw: vi.fn(() => summary), write: vi.fn(async () => {}) };
  const core = { info: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn(), summary };
  const github = {
    rest: {
      actions: { createWorkflowDispatch: vi.fn(async () => ({ status: 200, data: { workflow_run_id: 300 } })) },
      repos: { get: vi.fn(async () => ({ data: { default_branch: "trunk" } })) },
    },
  };
  const context = { runId: 100, repo: { owner: "owner", repo: "repo" }, payload: { repository: { default_branch: "main" } } };
  return { github, context, core };
}

describe("dispatch_continuation_relay", () => {
  it("dispatches the relay on the default branch with only the parent run identity", async () => {
    const options = fixture();
    await main(options);
    expect(options.github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "owner",
      repo: "repo",
      workflow_id: "agentic-continuation-relay.yml",
      ref: "refs/heads/main",
      inputs: { parent_run_id: "100", parent_run_attempt: "2" },
      return_run_details: true,
    });
    expect(options.core.setOutput).toHaveBeenCalledWith("relay_run_id", "300");
    expect(options.core.setFailed).not.toHaveBeenCalled();
  });

  it("looks up the default branch when the event payload has no repository", async () => {
    const options = fixture();
    options.context.payload = {};
    await main(options);
    expect(options.github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith(expect.objectContaining({ ref: "refs/heads/trunk" }));
  });

  it("fails the step with a manual recovery command when dispatch fails", async () => {
    const options = fixture();
    options.github.rest.actions.createWorkflowDispatch.mockRejectedValue(Object.assign(new Error("Not Found"), { status: 404 }));
    await main(options);
    expect(options.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("gh workflow run agentic-continuation-relay.yml -f parent_run_id=100 -f parent_run_attempt=2"));
  });
});
