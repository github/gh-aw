import { describe, it, expect, vi, beforeEach } from "vitest";

describe("pr_helpers.cjs", () => {
  let detectForkPR;
  let getPullRequestNumber;

  // Import the helper before each test
  beforeEach(async () => {
    const helpers = await import("./pr_helpers.cjs");
    detectForkPR = helpers.detectForkPR;
    getPullRequestNumber = helpers.getPullRequestNumber;
  });

  describe("detectForkPR", () => {
    it("should NOT treat same-repo PR as fork even when repo has fork flag", () => {
      // A repository that is itself a fork of another repo has fork=true,
      // but a same-repo PR within it is NOT a cross-repo fork PR (#24208)
      const pullRequest = {
        head: {
          repo: {
            fork: true,
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "test-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(false);
      expect(result.reason).toBe("same repository");
    });

    it("should detect fork using different repository names", () => {
      const pullRequest = {
        head: {
          repo: {
            fork: false,
            full_name: "fork-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "original-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("different repository names");
    });

    it("should detect deleted fork (null head repo)", () => {
      const pullRequest = {
        head: {
          // repo is missing/null
        },
        base: {
          repo: {
            full_name: "original-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("head repository deleted (was likely a fork)");
    });

    it("should detect non-fork when repos match and fork flag is false", () => {
      const pullRequest = {
        head: {
          repo: {
            fork: false,
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "test-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(false);
      expect(result.reason).toBe("same repository");
    });

    it("should handle missing fork flag with same repo names", () => {
      const pullRequest = {
        head: {
          repo: {
            // fork flag not present
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "test-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(false);
      expect(result.reason).toBe("same repository");
    });

    it("should detect cross-repo fork PR by different full_name", () => {
      // A real fork PR: head is in a different repo than base
      const pullRequest = {
        head: {
          repo: {
            fork: true,
            full_name: "contributor/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "upstream/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("different repository names");
    });

    it("should handle null base repo gracefully", () => {
      const pullRequest = {
        head: {
          repo: {
            fork: false,
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          // repo is missing/null
        },
      };

      const result = detectForkPR(pullRequest);

      // When base.repo is null, comparison with undefined returns true (different)
      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("different repository names");
    });

    it("should handle both repos being null", () => {
      const pullRequest = {
        head: {
          // repo is missing/null
        },
        base: {
          // repo is missing/null
        },
      };

      const result = detectForkPR(pullRequest);

      // Deleted fork takes precedence
      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("head repository deleted (was likely a fork)");
    });
  });

  describe("getPullRequestNumber", () => {
    it("should extract PR number from message", () => {
      const message = { pull_request_number: 123 };
      const context = { payload: {} };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBe(123);
      expect(result.error).toBeNull();
    });

    it("should handle PR number as string", () => {
      const message = { pull_request_number: "456" };
      const context = { payload: {} };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBe(456);
      expect(result.error).toBeNull();
    });

    it("should return error for invalid PR number", () => {
      const message = { pull_request_number: "invalid" };
      const context = { payload: {} };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBeNull();
      expect(result.error).toBe("Invalid pull_request_number: invalid");
    });

    it("should return error for NaN PR number", () => {
      const message = { pull_request_number: NaN };
      const context = { payload: {} };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBeNull();
      expect(result.error).toBe("Invalid pull_request_number: NaN");
    });

    it("should fall back to context when message has no PR number", () => {
      const message = {};
      const context = {
        payload: {
          pull_request: {
            number: 789,
          },
        },
      };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBe(789);
      expect(result.error).toBeNull();
    });

    it("should fall back to context when message is undefined", () => {
      const context = {
        payload: {
          pull_request: {
            number: 101,
          },
        },
      };

      const result = getPullRequestNumber(undefined, context);

      expect(result.prNumber).toBe(101);
      expect(result.error).toBeNull();
    });

    it("should return error when no PR number is available", () => {
      const message = {};
      const context = { payload: {} };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBeNull();
      expect(result.error).toBe("No pull_request_number provided and not in pull request context");
    });

    it("should prefer message PR number over context", () => {
      const message = { pull_request_number: 999 };
      const context = {
        payload: {
          pull_request: {
            number: 888,
          },
        },
      };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBe(999);
      expect(result.error).toBeNull();
    });

    it("should handle context without payload", () => {
      const message = {};
      const context = {};

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBeNull();
      expect(result.error).toBe("No pull_request_number provided and not in pull request context");
    });

    it("should handle zero as a valid PR number from message", () => {
      const message = { pull_request_number: 0 };
      const context = { payload: {} };

      const result = getPullRequestNumber(message, context);

      expect(result.prNumber).toBe(0);
      expect(result.error).toBeNull();
    });
  });
});

describe("resolvePullRequestRepo", () => {
  const { resolvePullRequestRepo } = require("./pr_helpers.cjs");

  it("returns effectiveBaseBranch from explicit config and resolvedDefaultBranch", async () => {
    const fakeGithub = {
      rest: {
        repos: {
          get: vi.fn().mockResolvedValue({ data: { node_id: "MDEwOlJlcG9zaXRvcnkx", default_branch: "develop" } }),
        },
      },
    };
    const result = await resolvePullRequestRepo(fakeGithub, "owner", "repo", "feature");
    expect(result.resolvedDefaultBranch).toBe("develop");
    // explicit config wins over fetched default
    expect(result.effectiveBaseBranch).toBe("feature");
  });

  it("falls back to repo default branch when no explicit base branch configured", async () => {
    const fakeGithub = {
      rest: {
        repos: {
          get: vi.fn().mockResolvedValue({ data: { node_id: "MDEwOlJlcG9zaXRvcnkx", default_branch: "trunk" } }),
        },
      },
    };
    const result = await resolvePullRequestRepo(fakeGithub, "owner", "repo", undefined);
    expect(result.resolvedDefaultBranch).toBe("trunk");
    expect(result.effectiveBaseBranch).toBe("trunk");
  });

  it("uses REST repos.get and returns repoSlug", async () => {
    const fakeGithub = {
      rest: {
        repos: {
          get: vi.fn().mockResolvedValue({ data: { node_id: "MDEwOlJlcG9zaXRvcnkx", default_branch: "main" } }),
        },
      },
    };
    const result = await resolvePullRequestRepo(fakeGithub, "owner", "repo", undefined);
    expect(result.repoSlug).toBe("owner/repo");
    expect(result.resolvedDefaultBranch).toBe("main");
    expect(result.effectiveBaseBranch).toBe("main");
    expect(fakeGithub.rest.repos.get).toHaveBeenCalledWith({ owner: "owner", repo: "repo" });
  });

  it("explicit configuredBaseBranch overrides REST default_branch", async () => {
    const fakeGithub = {
      rest: {
        repos: {
          get: vi.fn().mockResolvedValue({ data: { node_id: "MDEwOlJlcG9zaXRvcnkx", default_branch: "main" } }),
        },
      },
    };
    const result = await resolvePullRequestRepo(fakeGithub, "owner", "repo", "release");
    expect(result.resolvedDefaultBranch).toBe("main");
    expect(result.effectiveBaseBranch).toBe("release");
  });

  it("throws when REST response is missing node_id", async () => {
    const fakeGithub = {
      rest: {
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
      },
    };
    await expect(resolvePullRequestRepo(fakeGithub, "owner", "repo", undefined)).rejects.toThrow("Repository owner/repo did not return a valid node_id from the REST API");
  });
});

describe("buildBranchInstruction", () => {
  const { buildBranchInstruction } = require("./pr_helpers.cjs");

  it("produces a plain instruction when effective branch equals resolved default", () => {
    const instruction = buildBranchInstruction("main", "main");
    expect(instruction).toBe("IMPORTANT: Create your branch from the 'main' branch.");
    expect(instruction).not.toContain("NOT from");
  });

  it("includes NOT clause when effective branch differs from resolved default", () => {
    const instruction = buildBranchInstruction("feature", "develop");
    expect(instruction).toBe("IMPORTANT: Create your branch from the 'feature' branch, NOT from 'develop'.");
  });

  it("omits NOT clause when resolvedDefaultBranch is null", () => {
    const instruction = buildBranchInstruction("feature", null);
    expect(instruction).toBe("IMPORTANT: Create your branch from the 'feature' branch.");
    expect(instruction).not.toContain("NOT from");
  });
});

describe("checkBranchPushable", () => {
  const { checkBranchPushable } = require("./pr_helpers.cjs");

  const mockCore = { info: vi.fn(), warning: vi.fn(), error: vi.fn() };
  beforeEach(() => {
    global.core = mockCore;
    vi.clearAllMocks();
  });

  const makeClient = (defaultBranch, protectionStatus, isProtected = false) => ({
    graphql: vi.fn().mockResolvedValue({ repository: { ref: { refUpdateRule: null } } }),
    rest: {
      repos: {
        get: vi.fn().mockResolvedValue({ data: { default_branch: defaultBranch } }),
        getBranch: protectionStatus === null ? vi.fn().mockResolvedValue({ data: { protected: isProtected } }) : vi.fn().mockRejectedValue(Object.assign(new Error("error"), { status: protectionStatus })),
        getBranchRules: vi.fn().mockResolvedValue({ data: [{ type: "copilot_code_review" }] }),
        getBranchProtection: vi.fn().mockRejectedValue(new Error("Administration permission must not be required")),
      },
    },
  });

  it("returns null when branch does not exist (404)", async () => {
    const client = makeClient("main", 404);
    const result = await checkBranchPushable(client, "owner", "repo", "feature", true);
    expect(result).toBeNull();
    expect(mockCore.info).toHaveBeenCalledWith('Branch "feature" not found; proceeding to push');
  });

  it("blocks push when branch is the default branch", async () => {
    const client = makeClient("main", 404);
    const result = await checkBranchPushable(client, "owner", "repo", "main", true);
    expect(result).toContain("default branch");
  });

  it("blocks push when branch has protection rules", async () => {
    const client = makeClient("main", null, true);
    const result = await checkBranchPushable(client, "owner", "repo", "feature", true);
    expect(result).toContain("protection rules");
    expect(client.rest.repos.getBranch).toHaveBeenCalledWith({ owner: "owner", repo: "repo", branch: "feature" });
  });

  it("returns null when branch is not protected", async () => {
    const client = makeClient("main", null, false);
    const result = await checkBranchPushable(client, "owner", "repo", "feature", true);
    expect(result).toBeNull();
    expect(client.rest.repos.getBranchRules).not.toHaveBeenCalled();
  });

  it("allows Copilot-review-only rulesets when classic protection is explicitly disabled", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({
      data: { protected: true, protection: { enabled: false, required_status_checks: { enforcement_level: "off", contexts: [], checks: [] } } },
    });

    const result = await checkBranchPushable(client, "owner", "repo", "feature", true);

    expect(result).toBeNull();
    expect(client.rest.repos.getBranchRules).toHaveBeenCalledWith({ owner: "owner", repo: "repo", branch: "feature", per_page: 100, page: 1 });
    expect(client.graphql).toHaveBeenCalledWith(expect.stringContaining("refUpdateRule { pattern }"), { owner: "owner", repo: "repo", ref: "refs/heads/feature" });
    expect(client.rest.repos.getBranchProtection).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith('Branch "feature" only has Copilot code review rules and no classic protection; proceeding to push');
  });

  it.each([undefined, null, {}, { enabled: true }])("blocks protected branches without explicit absence of classic protection: %j", async protection => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection } });

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("protection rules");
    expect(client.rest.repos.getBranchRules).not.toHaveBeenCalled();
    expect(client.rest.repos.getBranchProtection).not.toHaveBeenCalled();
  });

  it.each(["pull_request", "update", "required_signatures", "required_status_checks", "non_fast_forward", "deletion", "unknown_rule"])("blocks %s even alongside Copilot code review", async type => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.rest.repos.getBranchRules.mockResolvedValue({ data: [{ type: "copilot_code_review" }, { type }] });

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("protection rules");
    expect(client.rest.repos.getBranchProtection).not.toHaveBeenCalled();
  });

  it("checks all paginated rules, including a restrictive rule after the first page", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.rest.repos.getBranchRules.mockResolvedValueOnce({ data: Array.from({ length: 100 }, () => ({ type: "copilot_code_review" })) }).mockResolvedValueOnce({ data: [{ type: "pull_request" }] });

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("protection rules");
    expect(client.rest.repos.getBranchRules).toHaveBeenNthCalledWith(2, { owner: "owner", repo: "repo", branch: "feature", per_page: 100, page: 2 });
    expect(client.rest.repos.getBranchProtection).not.toHaveBeenCalled();
  });

  it("allows a complete rules list ending with an empty page", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.rest.repos.getBranchRules.mockResolvedValueOnce({ data: Array.from({ length: 100 }, () => ({ type: "copilot_code_review" })) }).mockResolvedValueOnce({ data: [] });

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toBeNull();
    expect(client.rest.repos.getBranchRules).toHaveBeenCalledTimes(2);
  });

  it("fails closed when the rules endpoint repeats full Copilot-only pages", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.rest.repos.getBranchRules.mockResolvedValue({ data: Array.from({ length: 100 }, () => ({ type: "copilot_code_review" })) });

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("protection rules");
    expect(client.rest.repos.getBranchRules).toHaveBeenCalledTimes(50);
    expect(client.rest.repos.getBranchProtection).not.toHaveBeenCalled();
  });

  it("fails closed on a later-page HTTP 409 rather than accepting a partial rules list", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.rest.repos.getBranchRules.mockResolvedValueOnce({ data: Array.from({ length: 100 }, () => ({ type: "copilot_code_review" })) }).mockRejectedValueOnce(Object.assign(new Error("Conflict"), { status: 409 }));

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("Cannot verify branch protection rules");
    expect(client.rest.repos.getBranchProtection).not.toHaveBeenCalled();
  });

  it.each([[], null, {}, [null], [{}], [{ type: "copilot_code_review" }, null]])("blocks unexplained protection or malformed rules: %j", async rules => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.rest.repos.getBranchRules.mockResolvedValue({ data: rules });

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("protection rules");
  });

  it.each([403, 404, 409, 500, undefined])("fails closed when the rules query fails with status %s", async status => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.rest.repos.getBranchRules.mockRejectedValue(Object.assign(new Error("Rules unavailable"), { status }));

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("Cannot verify branch protection rules");
  });

  it("blocks classic protection even if branch metadata says enabled:false", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.graphql.mockResolvedValue({ repository: { ref: { refUpdateRule: { pattern: "feature*" } } } });

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("protection rules");
  });

  it.each([403, 409, 500, undefined])("fails closed when classic protection cannot be confirmed absent: %s", async status => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.graphql.mockRejectedValue(Object.assign(new Error("Classic protection unavailable"), { status }));

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("Cannot verify branch protection rules");
  });

  it("fails closed on a classic protection 404", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.graphql.mockRejectedValue(Object.assign(new Error("Not Found"), { status: 404 }));

    expect(await checkBranchPushable(client, "owner", "repo", "feature", true)).toContain("Cannot verify branch protection rules");
  });

  it.each([undefined, null, {}, { repository: null }, { repository: {} }, { repository: { ref: null } }, { repository: { ref: {} } }])("fails closed on an incomplete classic protection response: %j", async response => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });
    client.graphql.mockResolvedValue(response);

    const result = await checkBranchPushable(client, "owner", "repo", "feature", true);

    expect(result).toContain("Cannot verify branch protection rules");
    expect(result).toContain("Push blocked");
  });

  it("still blocks the default branch when its only ruleset is Copilot code review", async () => {
    const client = makeClient("main", null, true);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });

    expect(await checkBranchPushable(client, "owner", "repo", "main", true)).toContain("default branch");
    expect(client.rest.repos.getBranch).not.toHaveBeenCalled();
    expect(client.rest.repos.getBranchRules).not.toHaveBeenCalled();
  });

  it.each([
    ["repository metadata unavailable", client => client.rest.repos.get.mockRejectedValue(new Error("API unavailable"))],
    ["default branch missing", client => client.rest.repos.get.mockResolvedValue({ data: {} })],
    ["default branch empty", client => client.rest.repos.get.mockResolvedValue({ data: { default_branch: "" } })],
    ["default branch malformed", client => client.rest.repos.get.mockResolvedValue({ data: { default_branch: 123 } })],
  ])("blocks a protected branch when %s", async (_scenario, setMetadata) => {
    const client = makeClient(null, null, true);
    setMetadata(client);
    client.rest.repos.getBranch.mockResolvedValue({ data: { protected: true, protection: { enabled: false } } });

    expect(await checkBranchPushable(client, "owner", "repo", "main", true)).toContain("protection rules");
    expect(client.rest.repos.getBranchRules).not.toHaveBeenCalled();
    expect(client.rest.repos.getBranchProtection).not.toHaveBeenCalled();
  });

  it("returns null and skips protection check when checkBranchProtection is false", async () => {
    const client = makeClient("main", null);
    const result = await checkBranchPushable(client, "owner", "repo", "feature", false);
    expect(result).toBeNull();
    expect(client.rest.repos.getBranch).not.toHaveBeenCalled();
  });

  it("returns error on unexpected protection check failure (5xx)", async () => {
    const client = makeClient("main", 500);
    const result = await checkBranchPushable(client, "owner", "repo", "feature", true);
    expect(result).toContain("Cannot verify branch protection rules");
  });

  it("returns null and warns on 403 (insufficient permissions)", async () => {
    const client = makeClient("main", 403);
    const result = await checkBranchPushable(client, "owner", "repo", "feature", true);
    expect(result).toBeNull();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("insufficient permissions"));
  });
});
