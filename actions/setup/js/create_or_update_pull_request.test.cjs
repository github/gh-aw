// @ts-check

import { describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { createOrUpdatePullRequest } = require("./create_or_update_pull_request.cjs");

describe("createOrUpdatePullRequest", () => {
  it.each([
    ["includes head_repo for a cross-repository pull request", "test-owner/docs-automation", "test-owner/docs-automation"],
    ["omits head_repo for a same-repository pull request", undefined, undefined],
  ])("%s", async (_, headRepo, expectedHeadRepo) => {
    const create = vi.fn().mockResolvedValue({ data: { number: 1 } });

    await createOrUpdatePullRequest({
      githubClient: { rest: { pulls: { create } } },
      repoParts: { owner: "test-owner", repo: "test-repo" },
      title: "Test PR",
      body: "Test body",
      branchName: "test-owner:feature",
      headRepo,
      baseBranch: "main",
      draft: false,
    });

    expect(create).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      title: "Test PR",
      body: "Test body",
      head: "test-owner:feature",
      ...(expectedHeadRepo ? { head_repo: expectedHeadRepo } : {}),
      base: "main",
      draft: false,
    });
  });
});
