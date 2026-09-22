import { describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { createOrUpdatePullRequest } = require("./create_or_update_pull_request.cjs");

describe("createOrUpdatePullRequest", () => {
  const options = {
    repoParts: { owner: "example-org", repo: "target-repo" },
    title: "Test pull request",
    body: "Test body",
    branchName: "example-org:feature",
    baseBranch: "main",
    draft: false,
  };

  it("includes head_repo for a same-organization fork", async () => {
    const githubClient = { rest: { pulls: { create: vi.fn().mockResolvedValue({ data: {} }) } } };

    await createOrUpdatePullRequest({ ...options, githubClient, headRepo: "example-org/automation-fork" });

    expect(githubClient.rest.pulls.create).toHaveBeenCalledWith(expect.objectContaining({ head: "example-org:feature", head_repo: "example-org/automation-fork" }));
  });

  it("omits head_repo for a pull request in the target repository", async () => {
    const githubClient = { rest: { pulls: { create: vi.fn().mockResolvedValue({ data: {} }) } } };

    await createOrUpdatePullRequest({ ...options, githubClient });
    expect(githubClient.rest.pulls.create).toHaveBeenCalledWith(expect.not.objectContaining({ head_repo: expect.anything() }));
  });
});
