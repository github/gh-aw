// @ts-check

import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { readReplayEnvironmentApproval, withHeldReplayAuthorization } from "./write_gate_replay_approval.cjs";

describe("actual held replay approval evidence", () => {
  const review = () => ({ state: "approved", user: { id: 17, login: "actual-reviewer", type: "User" }, environments: [{ id: 7, name: "production" }], comment: "Reviewed held writes" });
  beforeEach(() => {
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
    vi.stubEnv("GITHUB_JOB", "apply_held_safe_outputs");
    vi.stubEnv("GH_AW_JOB_CHECK_RUN_ID", "456");
    vi.stubEnv("GITHUB_ACTOR", "dispatch-actor");
    vi.stubEnv("GH_AW_APPROVED", "true");
    vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, runId: 999 });
    vi.stubGlobal("github", {
      rest: { actions: { listJobsForWorkflowRunAttempt: vi.fn(), getReviewsForRun: vi.fn().mockResolvedValue({ data: [review()] }) } },
      paginate: vi
        .fn()
        .mockResolvedValue([{ name: "Apply approved held safe outputs", check_run_url: "https://api.github.com/repos/owner/repo/check-runs/456", status: "in_progress", steps: [{ name: "Apply approved held safe outputs" }] }]),
    });
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("records the actual required-environment reviewer, never inferring approval from the actor", async () => {
    expect(await readReplayEnvironmentApproval("production")).toEqual({ environment: "production", environment_id: 7, approvers: ["actual-reviewer"] });
    expect(global.github.rest.actions.getReviewsForRun).toHaveBeenCalledWith({ owner: "owner", repo: "repo", run_id: 999 });
  });

  it.each([{ reviews: [] }, { reviews: [{ ...review(), state: "rejected" }] }, { reviews: [{ ...review(), state: "pending" }] }, { reviews: [{ ...review(), environments: [{ id: 8, name: "unrelated" }] }] }])(
    "rejects absent or unusable reviews despite an authored approved flag",
    async ({ reviews }) => {
      global.github.rest.actions.getReviewsForRun.mockResolvedValue({ data: reviews });
      await expect(readReplayEnvironmentApproval("production")).rejects.toThrow("approved environment review");
    }
  );

  it("rejects ambiguous job/environment identities and malformed approvers", async () => {
    global.github.rest.actions.getReviewsForRun.mockResolvedValueOnce({ data: [review(), { ...review(), environments: [{ id: 8, name: "production" }] }] });
    await expect(readReplayEnvironmentApproval("production")).rejects.toThrow("identities conflict");
    global.github.rest.actions.getReviewsForRun.mockResolvedValueOnce({ data: [{ ...review(), user: { id: 1, login: "a-bot", type: "Bot" } }] });
    await expect(readReplayEnvironmentApproval("production")).rejects.toThrow("reviewer identity");
    global.github.paginate.mockResolvedValueOnce([]);
    await expect(readReplayEnvironmentApproval("production")).rejects.toThrow("job identity");
  });

  it("does not authorize held writes through the ordinary maintenance job or a serialized flag", async () => {
    vi.stubEnv("GITHUB_JOB", "apply_safe_outputs");
    await expect(readReplayEnvironmentApproval("production")).rejects.toThrow("reviewed approval-environment job");
    const write = vi.fn();
    expect(() => withHeldReplayAuthorization({ approved: true, approvers: ["dispatch-actor"] }, write)).toThrow("capability is missing");
    expect(write).not.toHaveBeenCalled();
  });
});
