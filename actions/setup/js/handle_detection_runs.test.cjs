// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import path from "path";

const { main, classifyDetectionOutcome } = require("./handle_detection_runs.cjs");

describe("handle_detection_runs", () => {
  let originalEnv;
  let mockCore;
  let mockGithub;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_PROMPTS_DIR = path.resolve(import.meta.dirname, "../md");
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    delete process.env.GH_AW_DETECTION_THREAT_KINDS;
    mockCore = { info: vi.fn(), warning: vi.fn() };
    mockGithub = {
      rest: {
        search: {
          issuesAndPullRequests: vi.fn().mockResolvedValue({
            data: { total_count: 1, items: [{ number: 42, node_id: "ID", html_url: "https://github.com/test-owner/test-repo/issues/42" }] },
          }),
        },
        issues: { create: vi.fn(), createComment: vi.fn().mockResolvedValue({ data: {} }) },
      },
    };
    vi.stubGlobal("core", mockCore);
    vi.stubGlobal("github", mockGithub);
    vi.stubGlobal("context", { repo: { owner: "test-owner", repo: "test-repo" } });
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) delete process.env[key];
    }
    Object.assign(process.env, originalEnv);
    vi.unstubAllGlobals();
  });

  it.each([
    ["agent_failure", "detector_execution_failure"],
    ["parse_error", "detector_result_invalid"],
    ["threat_detected", "threat_finding"],
    ["github_api_quota_exhausted", "detection_deferred"],
    ["", "detection_outcome_unknown"],
  ])("classifies structured reason %s as %s under both policies", async (reason, category) => {
    process.env.GH_AW_DETECTION_REASON = reason;
    for (const conclusion of ["warning", "failure"]) {
      process.env.GH_AW_DETECTION_CONCLUSION = conclusion;
      await main();
      const call = mockGithub.rest.issues.createComment.mock.lastCall[0];
      expect(call.issue_number).toBe(42);
      expect(call.body).toContain(`**Outcome:** \`${category}\``);
      expect(call.body).toContain(`| Conclusion | \`${conclusion}\` |`);
      expect(mockCore.info).toHaveBeenCalledWith(`Detection conclusion: ${conclusion}`);
      expect(call.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123");
    }
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledTimes(2);
    expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
  });

  it.each(["private prompt and tool result: threat_detected agent_failure", "detection_skipped", "future_reason"])("does not publish or log unrecognized problem reason %s", async privateText => {
    process.env.GH_AW_DETECTION_REASON = privateText;
    process.env.GH_AW_DETECTION_CONCLUSION = "failure";
    await main();
    const body = mockGithub.rest.issues.createComment.mock.lastCall[0].body;
    expect(body).toContain("detection_outcome_unknown");
    expect(body).toContain("| Reason | `unknown` |");
    expect(body).not.toContain(privateText);
    expect(JSON.stringify(mockCore.info.mock.calls)).not.toContain(privateText);
    expect(classifyDetectionOutcome(privateText).category).toBe("detection_outcome_unknown");
  });

  it.each(["success", "skipped", "cancelled"])("logs but does not report recognized conclusion %s", async conclusion => {
    process.env.GH_AW_DETECTION_CONCLUSION = conclusion;
    process.env.GH_AW_DETECTION_REASON = "threat_detected";
    await main();
    expect(mockCore.info).toHaveBeenCalledWith(`Detection conclusion: ${conclusion}`);
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it.each(["warning", "failure"])("publishes only allowlisted threat categories under %s policy", async conclusion => {
    process.env.GH_AW_DETECTION_CONCLUSION = conclusion;
    process.env.GH_AW_DETECTION_REASON = "threat_detected";
    process.env.GH_AW_DETECTION_THREAT_KINDS = "secret_leak,malicious_patch,PRIVATE_TEXT";
    await main();
    const body = mockGithub.rest.issues.createComment.mock.lastCall[0].body;
    expect(body).toContain("Detected threat types: secret leak, malicious patch.");
    expect(body).not.toContain("prompt injection");
    expect(body).not.toContain("PRIVATE_TEXT");
  });

  it.each(["", "private conclusion: warning\nfailure", "SUCCESS", "future_conclusion"])("redacts unrecognized conclusion %s", async conclusion => {
    process.env.GH_AW_DETECTION_CONCLUSION = conclusion;
    process.env.GH_AW_DETECTION_REASON = "threat_detected";
    await main();
    expect(mockCore.info).toHaveBeenCalledWith("Detection conclusion: unknown");
    if (conclusion) {
      expect([...mockCore.info.mock.calls, ...mockCore.warning.mock.calls].flat().join("\n")).not.toContain(conclusion);
    }
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("warns on publication failure without changing detection policy", async () => {
    process.env.GH_AW_DETECTION_CONCLUSION = "failure";
    process.env.GH_AW_DETECTION_REASON = "agent_failure";
    mockGithub.rest.issues.createComment.mockRejectedValue(new Error("API unavailable"));
    await main();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to post comment"));
    expect(process.env.GH_AW_DETECTION_CONCLUSION).toBe("failure");
  });
});
