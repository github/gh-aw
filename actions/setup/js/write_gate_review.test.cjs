// @ts-check

import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { resolveGateReviewRepo, buildGateReviewBlocks, createGateReviewIssue } from "./write_gate_review.cjs";
import { useExecutionProvenanceFixture, expectExecutionProvenance } from "./test_execution_provenance.js";

useExecutionProvenanceFixture();

describe("write gate review issues", () => {
  const message = { type: "create_pull_request", output_id: "so_123456789abc", title: "Proposed change", body: "Fixes #42\n\n````\nHostile fences ```\n````" };
  let client;
  let assertUnchanged;
  beforeEach(() => {
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() };
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, payload: {}, runId: 12345, serverUrl: "https://github.com" };
    client = {
      rest: {
        repos: { get: vi.fn().mockResolvedValue({ data: { has_issues: true } }) },
        issues: { create: vi.fn().mockResolvedValue({ data: { number: 17, html_url: "https://github.com/review-owner/review-repo/issues/17" } }) },
      },
    };
    assertUnchanged = vi.fn();
  });
  afterEach(() => {
    delete global.core;
    delete global.context;
    vi.unstubAllEnvs();
  });
  const options = () => ({ message, reviewRepo: "review-owner/review-repo", defaultRepo: "test-owner/test-repo", allowedRepos: new Set(["review-owner/review-repo"]), client, assertUnchanged });

  it("publishes the review only to its allowed target, reusing safe patch rendering and original-run attribution", async () => {
    const result = await createGateReviewIssue({ ...options(), patchContent: "diff --git a/file b/file\n+change\n" });
    expect(result).toMatchObject({ success: true, reviewed: true, gate_decision: "review", repo: "review-owner/review-repo", issue_number: 17 });
    expect(assertUnchanged).toHaveBeenCalledTimes(2);
    expect(client.rest.issues.create).toHaveBeenCalledTimes(1);
    const request = client.rest.issues.create.mock.calls[0][0];
    expect(request).toMatchObject({ owner: "review-owner", repo: "review-repo" });
    expect(request.assignees).toBeUndefined();
    expect(request.labels).toBeUndefined();
    expect(request.body).toContain("The proposed operation was not applied.");
    expect(request.body).toContain("diff --git a/file b/file");
    expect(request.body).not.toContain("Fixes #42");
    expectExecutionProvenance(request.body);
  });

  it.each([undefined, "", "review-repo", "elsewhere/repo", "review-owner/*", "bad/repo/path"])("stages unusable review repository %s without any API calls", async reviewRepo => {
    const result = await createGateReviewIssue({ ...options(), reviewRepo });
    expect(result).toMatchObject({ staged: true, skipped: true, gate_decision: "stage", reason: "gate_review_repo_unusable" });
    expect(client.rest.repos.get).not.toHaveBeenCalled();
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(global.core.warning).toHaveBeenCalled();
  });

  it("uses normal repository allowlisting without treating absence as the default", () => {
    expect(resolveGateReviewRepo("test-owner/test-repo", "test-owner/test-repo", new Set())).toEqual({ owner: "test-owner", repo: "test-repo" });
    expect(resolveGateReviewRepo("review-owner/review-repo", "test-owner/test-repo", new Set(["review-owner/*"]))).toEqual({ owner: "review-owner", repo: "review-repo" });
    expect(resolveGateReviewRepo(undefined, "test-owner/test-repo", new Set())).toBeNull();
  });

  it("stages disabled or inaccessible issues, never retrying in the workflow repository", async () => {
    client.rest.repos.get.mockResolvedValueOnce({ data: { has_issues: false } });
    expect(await createGateReviewIssue(options())).toMatchObject({ staged: true });
    client.rest.repos.get.mockRejectedValueOnce(Object.assign(new Error("not found"), { status: 404 }));
    expect(await createGateReviewIssue(options())).toMatchObject({ staged: true });
    client.rest.issues.create.mockRejectedValueOnce(Object.assign(new Error("issues disabled"), { status: 410 }));
    expect(await createGateReviewIssue(options())).toMatchObject({ staged: true });
    expect(client.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(client.rest.issues.create.mock.calls[0][0].owner).toBe("review-owner");
  });

  it("does not hide permission failures or failed integrity rechecks", async () => {
    const forbidden = Object.assign(new Error("permission denied"), { status: 403 });
    client.rest.repos.get.mockRejectedValueOnce(forbidden);
    await expect(createGateReviewIssue(options())).rejects.toBe(forbidden);
    assertUnchanged.mockImplementation(() => {
      throw new Error("integrity mismatch");
    });
    await expect(createGateReviewIssue(options())).rejects.toThrow("integrity mismatch");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
  });

  it("staging veto precedes reads and writes and secrets are redacted before tree construction", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    expect(await createGateReviewIssue(options())).toMatchObject({ staged: true });
    expect(client.rest.repos.get).not.toHaveBeenCalled();
    const blocks = buildGateReviewBlocks({ ...message, body: "token=known-sensitive-value" }, "+known-sensitive-value", ["known-sensitive-value"]);
    expect(JSON.stringify(blocks)).not.toContain("known-sensitive-value");
    expect(blocks.at(-1)).toMatchObject({ type: "details", children: [{ type: "code_block", language: "diff" }] });
  });
});
