import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

describe("merge_pull_request branch validation", () => {
  beforeEach(() => {
    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };
  });

  afterEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    delete global.core;
  });

  it("sanitizes and rejects invalid branch names", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const valid = __testables.sanitizeBranchName("feature/ok-branch", "source");
    expect(valid).toEqual({ valid: true, value: "feature/ok-branch" });

    const invalid = __testables.sanitizeBranchName("feature/unsafe\nbranch", "source");
    expect(invalid.valid).toBe(false);
    expect(invalid.error).toContain("contains invalid characters");
  });

  it("marks protected base branch as protected", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const githubClient = {
      rest: {
        repos: {
          getBranch: vi.fn().mockResolvedValue({ data: { protected: true } }),
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
      },
    };

    const policy = await __testables.getBranchPolicy(githubClient, "github", "gh-aw", "release/1.0");
    expect(policy.isProtected).toBe(true);
    expect(policy.requiredChecks).toEqual([]);
  });

  it("detects repository default branch", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const githubClient = {
      rest: {
        repos: {
          getBranch: vi.fn().mockResolvedValue({
            data: {
              protected: false,
            },
          }),
          getBranchProtection: vi.fn().mockResolvedValue({
            data: { required_status_checks: { contexts: ["ci/test"] } },
          }),
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
      },
    };

    const policy = await __testables.getBranchPolicy(githubClient, "github", "gh-aw", "main");
    expect(policy.isDefault).toBe(true);
    expect(policy.requiredChecks).toEqual(["ci/test"]);
  });

  it("does not mark non-default branches as default", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const githubClient = {
      rest: {
        repos: {
          getBranch: vi.fn().mockResolvedValue({ data: { protected: false } }),
          getBranchProtection: vi.fn().mockRejectedValue({ status: 404 }),
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
      },
    };

    const policy = await __testables.getBranchPolicy(githubClient, "github", "gh-aw", "feature-branch");
    expect(policy.isDefault).toBe(false);
  });

  it("rejects unsafe base branch names before branch policy lookup", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const githubClient = {
      rest: {
        repos: {
          getBranch: vi.fn(),
          get: vi.fn(),
        },
      },
    };

    await expect(__testables.getBranchPolicy(githubClient, "github", "gh-aw", "main;rm -rf /")).rejects.toThrow("E001: Invalid target base branch for policy evaluation");
    expect(githubClient.rest.repos.getBranch).not.toHaveBeenCalled();
  });

  it("findMissingRequiredLabels returns labels not present on the item (conjunctive check)", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    // all required labels present → none missing
    expect(__testables.findMissingRequiredLabels(["automerge", "safe-to-merge"], ["automerge", "safe-to-merge"])).toEqual([]);
    // one required label missing
    expect(__testables.findMissingRequiredLabels(["automerge"], ["automerge", "safe-to-merge"])).toEqual(["safe-to-merge"]);
    // no required labels → none missing
    expect(__testables.findMissingRequiredLabels(["automerge"], [])).toEqual([]);
    // all required labels missing
    expect(__testables.findMissingRequiredLabels([], ["automerge", "safe-to-merge"])).toEqual(["automerge", "safe-to-merge"]);
    // case-sensitive: wrong case counts as missing
    expect(__testables.findMissingRequiredLabels(["AutoMerge"], ["automerge"])).toEqual(["automerge"]);
  });

  it("getOpenPullRequestForBranch returns PR when one is open", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const githubClient = {
      rest: {
        pulls: {
          list: vi.fn().mockResolvedValue({ data: [{ number: 42, html_url: "https://github.com/github/gh-aw/pull/42" }] }),
        },
      },
    };

    const result = await __testables.getOpenPullRequestForBranch(githubClient, "github", "gh-aw", "release/1.0");
    expect(result).toEqual({ number: 42, html_url: "https://github.com/github/gh-aw/pull/42" });
    expect(githubClient.rest.pulls.list).toHaveBeenCalledWith({
      owner: "github",
      repo: "gh-aw",
      state: "open",
      head: "github:release/1.0",
      per_page: 1,
    });
  });

  it("getOpenPullRequestForBranch returns null when no open PR exists", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const githubClient = {
      rest: {
        pulls: {
          list: vi.fn().mockResolvedValue({ data: [] }),
        },
      },
    };

    const result = await __testables.getOpenPullRequestForBranch(githubClient, "github", "gh-aw", "orphan-branch");
    expect(result).toBeNull();
  });

  it("getOpenPullRequestForBranch returns null when API returns null data", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");

    const githubClient = {
      rest: {
        pulls: {
          list: vi.fn().mockResolvedValue({ data: null }),
        },
      },
    };

    const result = await __testables.getOpenPullRequestForBranch(githubClient, "github", "gh-aw", "some-branch");
    expect(result).toBeNull();
  });

  it("resolves temporary ID for pull_request_number", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");
    const result = __testables.resolvePullRequestNumber({ pull_request_number: "aw_pr1" }, { aw_pr1: { number: 42 } });
    expect(result).toEqual({ success: true, pullNumber: 42, fromTemporaryId: true });
  });

  it("fails on unresolved temporary ID for pull_request_number", async () => {
    const { __testables } = await import("./merge_pull_request.cjs");
    const result = __testables.resolvePullRequestNumber({ pull_request_number: "aw_missing" }, {});
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error).toContain("Unresolved temporary ID");
    }
  });

  it("main() fires target_branch_has_no_open_pr when base branch has no upstream open PR", async () => {
    global.context = {
      eventName: "pull_request",
      repo: { owner: "github", repo: "gh-aw" },
      payload: {},
    };
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({
            data: {
              number: 100,
              html_url: "https://github.com/github/gh-aw/pull/100",
              merged: false,
              state: "open",
              draft: false,
              mergeable: true,
              mergeable_state: "clean",
              head: { ref: "feature/my-feature", sha: "abc123" },
              base: { ref: "integration-branch" },
              labels: [],
              title: "Test PR",
              requested_reviewers: [],
              requested_teams: [],
            },
          }),
          list: vi.fn().mockResolvedValue({ data: [] }),
        },
        repos: {
          getBranch: vi.fn().mockResolvedValue({ data: { protected: false } }),
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
          getBranchProtection: vi.fn().mockRejectedValue({ status: 404 }),
        },
      },
      graphql: vi.fn().mockResolvedValue({
        repository: {
          pullRequest: {
            headRefOid: "abc123",
            reviewDecision: null,
            reviewThreads: { pageInfo: { hasNextPage: false, endCursor: null }, nodes: [] },
          },
        },
      }),
      paginate: vi.fn().mockResolvedValue([]),
    };

    const { main } = await import("./merge_pull_request.cjs");
    const handler = await main({ "target-repo": "github/gh-aw", target: "*" });
    const result = await handler({ pull_request_number: 100 }, {});

    expect(result.success).toBe(false);
    expect(result.failure_reasons).toEqual(expect.arrayContaining([expect.objectContaining({ code: "target_branch_has_no_open_pr" })]));

    delete global.context;
    delete global.github;
  });
});

describe("merge_pull_request policy hardening", () => {
  let pr;
  let client;
  let reviewPR;

  beforeEach(() => {
    global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), debug: vi.fn() };
    global.context = {
      eventName: "pull_request",
      repo: { owner: "github", repo: "gh-aw" },
      payload: { pull_request: { number: 100 } },
    };
    pr = {
      number: 100,
      html_url: "https://github.com/github/gh-aw/pull/100",
      state: "open",
      merged: false,
      draft: false,
      mergeable: true,
      mergeable_state: "clean",
      head: { ref: "feature/test", sha: "abc123" },
      base: { ref: "integration", sha: "base123" },
      user: { login: "author" },
      labels: [{ name: "ready" }],
      title: "Test PR",
      requested_reviewers: [],
      requested_teams: [],
    };
    reviewPR = {
      headRefOid: "abc123",
      reviewDecision: null,
      reviewThreads: { pageInfo: { hasNextPage: false, endCursor: null }, nodes: [] },
    };
    client = {
      rest: {
        pulls: {
          get: vi.fn().mockImplementation(async () => ({ data: structuredClone(pr) })),
          list: vi.fn().mockResolvedValue({ data: [{ number: 42, html_url: "https://github.com/github/gh-aw/pull/42" }] }),
          listReviews: vi.fn(),
          merge: vi.fn().mockResolvedValue({ data: { merged: true, sha: "merged123" } }),
        },
        repos: {
          getBranch: vi.fn().mockResolvedValue({ data: { protected: false } }),
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
          getBranchProtection: vi.fn().mockRejectedValue({ status: 404 }),
        },
        checks: { listForRef: vi.fn() },
      },
      graphql: vi.fn().mockImplementation(async query => (query.includes("viewer") ? { viewer: { login: "merge-bot" } } : { repository: { pullRequest: structuredClone(reviewPR) } })),
      paginate: vi.fn().mockResolvedValue([]),
    };
    global.github = client;
  });

  afterEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    delete global.core;
    delete global.context;
    delete global.github;
  });

  async function run(config = {}, message = {}) {
    const { main } = await import("./merge_pull_request.cjs");
    const handler = await main(config);
    return handler(message, {});
  }

  function approval(overrides = {}) {
    return { id: 1, state: "APPROVED", commit_id: "abc123", user: { login: "reviewer", type: "User" }, ...overrides };
  }

  it("binds the merge to the evaluated head and records no-objection evidence without claiming approval", async () => {
    const result = await run({}, { pull_request_number: 999 });
    expect(result.success).toBe(true);
    expect(client.rest.pulls.merge).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ pull_number: 100, sha: "abc123" }));
    expect(result.decision_evidence).toMatchObject({
      head_sha: "abc123",
      base_sha: "base123",
      upstream_pull_request: 42,
      checks: { required: [], passing: [] },
      reviews: { reviewDecision: null, unresolvedThreadCount: 0 },
      approval_policy: { required_approvers: [], approvals: [] },
    });
    expect(client.paginate).not.toHaveBeenCalled();
  });

  it.each([
    ["missing PR", null],
    ["missing decision", { headRefOid: "abc123", reviewThreads: { pageInfo: { hasNextPage: false, endCursor: null }, nodes: [] } }],
    ["unknown decision", { headRefOid: "abc123", reviewDecision: "UNKNOWN" }],
    ["missing threads", { headRefOid: "abc123", reviewDecision: null }],
    ["stale revision", { headRefOid: "new-sha", reviewDecision: null }],
    ["null thread", { headRefOid: "abc123", reviewDecision: null, reviewThreads: { pageInfo: { hasNextPage: false, endCursor: null }, nodes: [null] } }],
    ["missing resolution", { headRefOid: "abc123", reviewDecision: null, reviewThreads: { pageInfo: { hasNextPage: false, endCursor: null }, nodes: [{}] } }],
    ["missing page info", { headRefOid: "abc123", reviewDecision: null, reviewThreads: { nodes: [] } }],
    ["missing cursor", { headRefOid: "abc123", reviewDecision: null, reviewThreads: { pageInfo: { hasNextPage: true }, nodes: [] } }],
    ["invalid page flag", { headRefOid: "abc123", reviewDecision: null, reviewThreads: { pageInfo: { hasNextPage: "false", endCursor: null }, nodes: [] } }],
  ])("fails closed on %s in review evidence", async (_name, data) => {
    reviewPR = data;
    const result = await run();
    expect(result.success).toBe(false);
    expect(result.error).toContain("review evidence");
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("rejects repeating pagination cursors instead of looping", async () => {
    reviewPR.reviewThreads = { pageInfo: { hasNextPage: true, endCursor: "repeat" }, nodes: [{ isResolved: true }] };
    const result = await run();
    expect(result.error).toContain("Invalid review thread pagination");
    expect(client.graphql).toHaveBeenCalledTimes(2);
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("collects all review thread pages and blocks unresolved threads", async () => {
    client.graphql
      .mockResolvedValueOnce({ repository: { pullRequest: { ...reviewPR, reviewThreads: { pageInfo: { hasNextPage: true, endCursor: "next" }, nodes: [{ isResolved: true }] } } } })
      .mockResolvedValueOnce({ repository: { pullRequest: { ...reviewPR, reviewThreads: { pageInfo: { hasNextPage: false, endCursor: null }, nodes: [{ isResolved: false }] } } } });
    const result = await run();
    expect(result.failure_reasons).toContainEqual(expect.objectContaining({ code: "unresolved_review_threads" }));
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it.each(["head", "base", "labels", "draft", "requested_reviewers", "state"])("blocks changed %s at final revalidation", async field => {
    const changed = structuredClone(pr);
    const changes = { head: { ...pr.head, sha: "new-sha" }, base: { ...pr.base, ref: "main" }, labels: [], draft: true, requested_reviewers: [{ login: "reviewer" }], state: "closed" };
    changed[field] = changes[field];
    client.rest.pulls.get.mockResolvedValueOnce({ data: structuredClone(pr) }).mockResolvedValueOnce({ data: changed });
    const result = await run();
    expect(result.error).toContain("policy state changed");
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("does not retry a SHA-mismatch merge error", async () => {
    client.rest.pulls.merge.mockRejectedValue({ status: 409, message: "Head branch was modified" });
    const result = await run();
    expect(result.success).toBe(false);
    expect(result.decision_evidence.head_sha).toBe("abc123");
    expect(client.rest.pulls.merge).toHaveBeenCalledTimes(1);
  });

  it.each(["protected", "default", "upstream", "reviews", "checks"])("blocks changed %s policy before merge", async policy => {
    if (policy === "protected") client.rest.repos.getBranch.mockResolvedValueOnce({ data: { protected: false } }).mockResolvedValueOnce({ data: { protected: true } });
    if (policy === "default") client.rest.repos.get.mockResolvedValueOnce({ data: { default_branch: "main" } }).mockResolvedValueOnce({ data: { default_branch: "integration" } });
    if (policy === "upstream") client.rest.pulls.list.mockResolvedValueOnce({ data: [{ number: 42 }] }).mockResolvedValueOnce({ data: [] });
    if (policy === "reviews") client.graphql.mockResolvedValueOnce({ repository: { pullRequest: structuredClone(reviewPR) } }).mockResolvedValueOnce({ repository: { pullRequest: { ...reviewPR, reviewDecision: "CHANGES_REQUESTED" } } });
    const check = { id: 1, name: "ci", head_sha: "abc123", status: "completed", conclusion: "success" };
    client.paginate.mockResolvedValueOnce([check]).mockResolvedValueOnce([{ ...check, conclusion: "failure" }]);
    const result = await run(policy === "checks" ? { required_checks: ["ci"] } : {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("policy evidence changed");
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it.each(["missing", "failure", "neutral", "skipped", "pending", "wrong-sha"])("requires configured CI even on unprotected branches: %s", async outcome => {
    const check = { id: 1, name: "ci", head_sha: outcome === "wrong-sha" ? "other" : "abc123", status: outcome === "pending" ? "in_progress" : "completed", conclusion: outcome === "wrong-sha" ? "success" : outcome };
    client.paginate.mockResolvedValue(outcome === "missing" ? [] : [check]);
    const result = await run({ required_checks: ["ci"] });
    expect(result.success).toBe(false);
    expect(result.failure_reasons[0].code).toBe(outcome === "missing" ? "required_checks_missing" : "required_checks_failing");
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("enforces the union of branch and workflow check requirements on the evaluated SHA", async () => {
    client.rest.repos.getBranchProtection.mockResolvedValue({ data: { required_status_checks: { contexts: ["branch-ci"] } } });
    client.paginate.mockResolvedValue(["branch-ci", "workflow-ci"].map((name, i) => ({ id: i + 1, name, head_sha: "abc123", status: "completed", conclusion: "success", app: { id: 5 } })));
    const result = await run({ required_checks: ["workflow-ci", "branch-ci"] });
    expect(result.success).toBe(true);
    expect(result.checks_evaluated).toEqual(["branch-ci", "workflow-ci"]);
    expect(result.decision_evidence.checks.passing).toHaveLength(2);
    expect(client.paginate).toHaveBeenCalledWith(client.rest.checks.listForRef, expect.objectContaining({ ref: "abc123" }));
  });

  it("does not accept an older successful check while a newer rerun is queued", async () => {
    client.paginate.mockResolvedValue([
      { id: 1, name: "ci", head_sha: "abc123", status: "completed", conclusion: "success", started_at: "2026-01-01T00:00:00Z" },
      { id: 2, name: "ci", head_sha: "abc123", status: "queued", conclusion: null, started_at: null },
    ]);
    const result = await run({ required_checks: ["ci"] });
    expect(result.failure_reasons).toContainEqual(expect.objectContaining({ code: "required_checks_failing" }));
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it.each([
    ["no approval", []],
    ["stale approval", [{ commit_id: "old" }]],
    ["bot approval", [{ user: { login: "reviewer", type: "Bot" } }]],
    ["PR author approval", [{ user: { login: "author", type: "User" } }]],
    ["merge identity approval", [{ user: { login: "merge-bot", type: "User" } }]],
    ["unlisted approval", [{ user: { login: "outsider", type: "User" } }]],
    ["dismissed approval", [{}, { id: 2, state: "DISMISSED" }]],
    ["changes requested", [{}, { id: 2, state: "CHANGES_REQUESTED" }]],
  ])("refuses %s as independent authorization", async (_name, reviews) => {
    client.paginate.mockResolvedValue(reviews.map(overrides => approval(overrides)));
    const result = await run({ required_approvers: ["reviewer", "author", "merge-bot"] });
    expect(result.success).toBe(false);
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("accepts current independent human approval and records provenance", async () => {
    client.paginate.mockResolvedValue([approval(), approval({ id: 2, state: "COMMENTED" })]);
    const result = await run({ required_approvers: ["Reviewer"] });
    expect(result.success).toBe(true);
    expect(result.decision_evidence.approval_policy.approvals).toEqual([{ login: "reviewer", review_id: 1, commit_id: "abc123" }]);
  });

  it("uses the latest decisive review rather than superseded changes requested", async () => {
    client.paginate.mockResolvedValue([approval({ state: "CHANGES_REQUESTED", commit_id: "old" }), approval({ id: 2 })]);
    const result = await run({ required_approvers: ["reviewer"] });
    expect(result.success).toBe(true);
    expect(result.decision_evidence.approval_policy.approvals[0].review_id).toBe(2);
  });

  it("blocks approval dismissal during final revalidation", async () => {
    client.paginate.mockResolvedValueOnce([approval()]).mockResolvedValueOnce([approval({ state: "DISMISSED" })]);
    const result = await run({ required_approvers: ["reviewer"] });
    expect(result.success).toBe(false);
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("staged mode evaluates and revalidates the same gates without merging", async () => {
    client.paginate.mockResolvedValue([approval()]);
    const result = await run({ staged: true, required_approvers: ["reviewer"] });
    expect(result).toMatchObject({ success: true, staged: true, merged: false });
    expect(result.decision_evidence.revalidated_at).toBeDefined();
    expect(client.rest.pulls.get).toHaveBeenCalledTimes(2);
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("staged mode also refuses missing independent approval", async () => {
    const result = await run({ staged: true, required_approvers: ["reviewer"] });
    expect(result.success).toBe(false);
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("preserves max=1", async () => {
    const { main } = await import("./merge_pull_request.cjs");
    const handler = await main();
    expect((await handler({})).success).toBe(true);
    expect((await handler({})).success).toBe(false);
    expect(client.rest.pulls.merge).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["draft", { draft: true }, "pr_is_draft"],
    ["conflicts", { mergeable: false, mergeable_state: "dirty" }, "merge_conflicts"],
    ["closed", { state: "closed" }, "pr_not_open"],
    ["reviewer", { requested_reviewers: [{ login: "reviewer" }] }, "pending_reviewers"],
    ["team", { requested_teams: [{ slug: "maintainers" }] }, "pending_reviewers"],
  ])("preserves the %s gate", async (_name, update, code) => {
    Object.assign(pr, update);
    const result = await run();
    expect(result.failure_reasons).toContainEqual(expect.objectContaining({ code }));
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it.each(["protected", "default"])("refuses a %s base branch even when approval and CI pass", async policy => {
    if (policy === "protected") client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true } });
    else client.rest.repos.get.mockResolvedValue({ data: { default_branch: "integration" } });
    client.paginate.mockImplementation(async method => (method === client.rest.pulls.listReviews ? [approval()] : [{ id: 1, name: "ci", head_sha: "abc123", status: "completed", conclusion: "success" }]));
    const result = await run({ required_checks: ["ci"], required_approvers: ["reviewer"] });
    expect(result.failure_reasons).toContainEqual(expect.objectContaining({ code: policy === "protected" ? "target_branch_protected" : "target_branch_default" }));
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("preserves repository allowlisting before making API calls", async () => {
    const result = await run({ target: "*" }, { pull_request_number: 100, repo: "untrusted/repo" });
    expect(result.success).toBe(false);
    expect(client.rest.pulls.get).not.toHaveBeenCalled();
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("preserves idempotent success without claiming fresh policy evaluation", async () => {
    pr.merged = true;
    delete pr.head.sha;
    const result = await run();
    expect(result).toMatchObject({ success: true, alreadyMerged: true });
    expect(result.decision_evidence).toBeUndefined();
    expect(client.graphql).not.toHaveBeenCalled();
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("preserves useful review API errors and partial revision evidence", async () => {
    client.graphql.mockRejectedValue(new Error("Resource not accessible by integration"));
    const result = await run();
    expect(result.error).toContain("Resource not accessible by integration");
    expect(result.decision_evidence.head_sha).toBe("abc123");
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });

  it("fails closed when final review data disappears", async () => {
    client.graphql.mockResolvedValueOnce({ repository: { pullRequest: reviewPR } }).mockResolvedValueOnce({ repository: { pullRequest: null } });
    const result = await run();
    expect(result.success).toBe(false);
    expect(client.rest.pulls.merge).not.toHaveBeenCalled();
  });
});
