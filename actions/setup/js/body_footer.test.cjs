import { beforeEach, describe, expect, it } from "vitest";

const { appendConfiguredBodyFooter } = require("./body_footer.cjs");

describe("appendConfiguredBodyFooter", () => {
  beforeEach(() => {
    global.context = {
      serverUrl: "https://github.com",
      repo: { owner: "owner", repo: "repo" },
      runId: 123,
    };
    process.env.GH_AW_WORKFLOW_NAME = "Policy Workflow";
  });

  it("appends and renders the configured footer", () => {
    expect(appendConfiguredBodyFooter("Body", "From {workflow_name}: {run_url}")).toBe("Body\n\nFrom Policy Workflow: https://github.com/owner/repo/actions/runs/123");
  });

  it("preserves the body when no footer is configured", () => {
    expect(appendConfiguredBodyFooter("Body", undefined)).toBe("Body");
  });
});
