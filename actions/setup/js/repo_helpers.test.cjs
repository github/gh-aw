import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock the context global
const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
};

global.context = mockContext;

describe("repo_helpers", () => {
  beforeEach(() => {
    vi.resetModules();
    delete process.env.GH_AW_TARGET_REPO_SLUG;
    delete process.env.GITHUB_REPOSITORY;
    global.context = mockContext;
  });

  describe("parseAllowedRepos", () => {
    it("should return empty set when value is undefined", async () => {
      const { parseAllowedRepos } = await import("./repo_helpers.cjs");
      const result = parseAllowedRepos(undefined);
      expect(result.size).toBe(0);
    });

    it("should parse single repo from string", async () => {
      const { parseAllowedRepos } = await import("./repo_helpers.cjs");
      const result = parseAllowedRepos("org/repo-a");
      expect(result.size).toBe(1);
      expect(result.has("org/repo-a")).toBe(true);
    });

    it("should parse multiple repos from comma-separated string", async () => {
      const { parseAllowedRepos } = await import("./repo_helpers.cjs");
      const result = parseAllowedRepos("org/repo-a, org/repo-b, org/repo-c");
      expect(result.size).toBe(3);
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
      expect(result.has("org/repo-c")).toBe(true);
    });

    it("should parse repos from array", async () => {
      const { parseAllowedRepos } = await import("./repo_helpers.cjs");
      const result = parseAllowedRepos(["org/repo-a", "org/repo-b"]);
      expect(result.size).toBe(2);
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });

    it("should trim whitespace from repo names in string", async () => {
      const { parseAllowedRepos } = await import("./repo_helpers.cjs");
      const result = parseAllowedRepos("  org/repo-a  ,  org/repo-b  ");
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });

    it("should trim whitespace from repo names in array", async () => {
      const { parseAllowedRepos } = await import("./repo_helpers.cjs");
      const result = parseAllowedRepos(["  org/repo-a  ", "  org/repo-b  "]);
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });

    it("should filter out empty strings", async () => {
      const { parseAllowedRepos } = await import("./repo_helpers.cjs");
      const result = parseAllowedRepos("org/repo-a,,org/repo-b,  ,");
      expect(result.size).toBe(2);
    });
  });

  describe("getDefaultTargetRepo", () => {
    it("should return target-repo from config when provided", async () => {
      const { getDefaultTargetRepo } = await import("./repo_helpers.cjs");
      const config = { "target-repo": "config-org/config-repo" };
      const result = getDefaultTargetRepo(config);
      expect(result).toBe("config-org/config-repo");
    });

    it("should prefer config target-repo over env variable", async () => {
      process.env.GH_AW_TARGET_REPO_SLUG = "env-org/env-repo";
      const { getDefaultTargetRepo } = await import("./repo_helpers.cjs");
      const config = { "target-repo": "config-org/config-repo" };
      const result = getDefaultTargetRepo(config);
      expect(result).toBe("config-org/config-repo");
    });

    it("should return target-repo override when set", async () => {
      process.env.GH_AW_TARGET_REPO_SLUG = "override-org/override-repo";
      const { getDefaultTargetRepo } = await import("./repo_helpers.cjs");
      const result = getDefaultTargetRepo();
      expect(result).toBe("override-org/override-repo");
    });

    it("should fall back to context repo when no override", async () => {
      const { getDefaultTargetRepo } = await import("./repo_helpers.cjs");
      const result = getDefaultTargetRepo();
      expect(result).toBe("test-owner/test-repo");
    });

    it("should use GITHUB_REPOSITORY env var when context is not defined", async () => {
      process.env.GITHUB_REPOSITORY = "env-owner/env-repo";
      // @ts-expect-error - Simulating standalone daemon where context is not available
      delete global.context;
      const { getDefaultTargetRepo } = await import("./repo_helpers.cjs");
      const result = getDefaultTargetRepo();
      expect(result).toBe("env-owner/env-repo");
    });

    it("should prefer GH_AW_TARGET_REPO_SLUG over GITHUB_REPOSITORY", async () => {
      process.env.GH_AW_TARGET_REPO_SLUG = "slug-org/slug-repo";
      process.env.GITHUB_REPOSITORY = "env-owner/env-repo";
      const { getDefaultTargetRepo } = await import("./repo_helpers.cjs");
      const result = getDefaultTargetRepo();
      expect(result).toBe("slug-org/slug-repo");
    });

    it("should return empty string when no config, no env vars, and no context", async () => {
      // @ts-expect-error - Simulating standalone daemon where context is not available
      delete global.context;
      const { getDefaultTargetRepo } = await import("./repo_helpers.cjs");
      const result = getDefaultTargetRepo();
      expect(result).toBe("");
    });
  });

  describe("isRepoAllowed", () => {
    it("should return true for exact match", async () => {
      const { isRepoAllowed } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["org/repo-a"]);
      expect(isRepoAllowed("org/repo-a", allowedRepos)).toBe(true);
    });

    it('should return true when "*" is in the set', async () => {
      const { isRepoAllowed } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["*"]);
      expect(isRepoAllowed("any-org/any-repo", allowedRepos)).toBe(true);
    });

    it('should return true for org wildcard "github/*"', async () => {
      const { isRepoAllowed } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/*"]);
      expect(isRepoAllowed("github/gh-aw", allowedRepos)).toBe(true);
    });

    it("should return false when repo does not match org wildcard", async () => {
      const { isRepoAllowed } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/*"]);
      expect(isRepoAllowed("other-org/gh-aw", allowedRepos)).toBe(false);
    });

    it("should return false for empty set", async () => {
      const { isRepoAllowed } = await import("./repo_helpers.cjs");
      expect(isRepoAllowed("org/repo", new Set())).toBe(false);
    });

    it("should return false when no pattern matches", async () => {
      const { isRepoAllowed } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["org/repo-a", "org/repo-b"]);
      expect(isRepoAllowed("org/repo-c", allowedRepos)).toBe(false);
    });
  });

  describe("validateRepo", () => {
    it("should allow default repo", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const result = validateRepo("default/repo", "default/repo", new Set());
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it("should allow repos in allowed list", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["org/repo-a", "org/repo-b"]);
      const result = validateRepo("org/repo-a", "default/repo", allowedRepos);
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it("should reject repos not in allowed list", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["org/repo-a"]);
      const result = validateRepo("org/other-repo", "default/repo", allowedRepos);
      expect(result.valid).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });

    it("should include allowed repos in error message", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["org/repo-a", "org/repo-b"]);
      const result = validateRepo("org/other-repo", "default/repo", allowedRepos);
      expect(result.error).toContain("default/repo");
      expect(result.error).toContain("org/repo-a");
      expect(result.error).toContain("org/repo-b");
    });

    it("should qualify bare repo name with default repo's org", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/gh-aw"]);
      const result = validateRepo("gh-aw", "github/other-repo", allowedRepos);
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it("should allow bare repo name matching default repo", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const result = validateRepo("gh-aw", "github/gh-aw", new Set());
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it("should reject bare repo name not in allowed list", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/other-repo"]);
      const result = validateRepo("gh-aw", "github/default-repo", allowedRepos);
      expect(result.valid).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });

    it("should not qualify repo name that already has org", async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/gh-aw"]);
      const result = validateRepo("other-org/gh-aw", "github/default-repo", allowedRepos);
      expect(result.valid).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });

    it('should allow any repo when "*" is in allowed list', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["*"]);
      const result = validateRepo("any-org/any-repo", "default/repo", allowedRepos);
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it('should allow any repo when defaultRepo is "*" (wildcard target-repo config)', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const result = validateRepo("org/any-repo", "*", new Set());
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it('should allow any repo when defaultRepo is "*" regardless of allowed list', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const result = validateRepo("org/some-repo", "*", new Set(["org/other-repo"]));
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
      expect(result.qualifiedRepo).toBe("org/some-repo");
    });

    it('should reject invalid slug when defaultRepo is "*" (no slash)', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const result = validateRepo("repo-only", "*", new Set());
      expect(result.valid).toBe(false);
      expect(result.error).toContain("not a valid 'owner/repo' slug");
    });

    it('should reject invalid slug when defaultRepo is "*" (too many slashes)', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const result = validateRepo("owner/repo/extra", "*", new Set());
      expect(result.valid).toBe(false);
      expect(result.error).toContain("not a valid 'owner/repo' slug");
    });

    it('should allow org-scoped wildcard "github/*"', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/*"]);
      const result = validateRepo("github/gh-aw", "default/repo", allowedRepos);
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it('should reject repo not matching org-scoped wildcard "github/*"', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/*"]);
      const result = validateRepo("other-org/gh-aw", "default/repo", allowedRepos);
      expect(result.valid).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });

    it('should allow repo-scoped wildcard "*/gh-aw"', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["*/gh-aw"]);
      const result = validateRepo("any-org/gh-aw", "default/repo", allowedRepos);
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it('should allow prefix wildcard "github/gh-*"', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/gh-*"]);
      const result = validateRepo("github/gh-aw", "default/repo", allowedRepos);
      expect(result.valid).toBe(true);
      expect(result.error).toBe(null);
    });

    it('should reject repo not matching prefix wildcard "github/gh-*"', async () => {
      const { validateRepo } = await import("./repo_helpers.cjs");
      const allowedRepos = new Set(["github/gh-*"]);
      const result = validateRepo("github/other-repo", "default/repo", allowedRepos);
      expect(result.valid).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });
  });

  describe("parseRepoSlug", () => {
    it("should parse valid repo slug", async () => {
      const { parseRepoSlug } = await import("./repo_helpers.cjs");
      const result = parseRepoSlug("owner/repo");
      expect(result).toEqual({ owner: "owner", repo: "repo" });
    });

    it("should return null for invalid slug without slash", async () => {
      const { parseRepoSlug } = await import("./repo_helpers.cjs");
      const result = parseRepoSlug("invalid");
      expect(result).toBeNull();
    });

    it("should return null for slug with too many slashes", async () => {
      const { parseRepoSlug } = await import("./repo_helpers.cjs");
      const result = parseRepoSlug("owner/repo/extra");
      expect(result).toBeNull();
    });

    it("should return null for empty owner", async () => {
      const { parseRepoSlug } = await import("./repo_helpers.cjs");
      const result = parseRepoSlug("/repo");
      expect(result).toBeNull();
    });

    it("should return null for empty repo", async () => {
      const { parseRepoSlug } = await import("./repo_helpers.cjs");
      const result = parseRepoSlug("owner/");
      expect(result).toBeNull();
    });
  });

  describe("resolveAndValidateRepo", () => {
    it("should successfully resolve and validate default repo", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = {}; // No repo field
      const defaultRepo = "default/repo";
      const allowedRepos = new Set();

      const result = resolveAndValidateRepo(item, defaultRepo, allowedRepos, "test");

      expect(result.success).toBe(true);
      expect(result.repo).toBe("default/repo");
      expect(result.repoParts).toEqual({ owner: "default", repo: "repo" });
    });

    it("should successfully resolve and validate repo from item", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = { repo: "org/other-repo" };
      const defaultRepo = "default/repo";
      const allowedRepos = new Set(["org/other-repo"]);

      const result = resolveAndValidateRepo(item, defaultRepo, allowedRepos, "test");

      expect(result.success).toBe(true);
      expect(result.repo).toBe("org/other-repo");
      expect(result.repoParts).toEqual({ owner: "org", repo: "other-repo" });
    });

    it("should fail when repo not in allowed list", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = { repo: "org/unauthorized-repo" };
      const defaultRepo = "default/repo";
      const allowedRepos = new Set(["org/allowed-repo"]);

      const result = resolveAndValidateRepo(item, defaultRepo, allowedRepos, "test");

      expect(result.success).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });

    it("should fail with invalid repo format", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      // Use a repo with slash but invalid format (empty parts)
      const item = { repo: "owner/" };
      const defaultRepo = "default/repo";
      const allowedRepos = new Set(["owner/"]);

      const result = resolveAndValidateRepo(item, defaultRepo, allowedRepos, "test");

      expect(result.success).toBe(false);
      expect(result.error).toContain("Invalid repository format");
      expect(result.error).toContain("owner/repo");
    });

    it("should trim whitespace from repo field", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = { repo: "  org/trimmed-repo  " };
      const defaultRepo = "default/repo";
      const allowedRepos = new Set(["org/trimmed-repo"]);

      const result = resolveAndValidateRepo(item, defaultRepo, allowedRepos, "test");

      expect(result.success).toBe(true);
      expect(result.repo).toBe("org/trimmed-repo");
    });

    it("should qualify bare repo name and return qualified version", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = { repo: "gh-aw" };
      const defaultRepo = "github/other-repo";
      const allowedRepos = new Set(["github/gh-aw"]);

      const result = resolveAndValidateRepo(item, defaultRepo, allowedRepos, "test");

      expect(result.success).toBe(true);
      expect(result.repo).toBe("github/gh-aw");
      expect(result.repoParts).toEqual({ owner: "github", repo: "gh-aw" });
    });

    it("should qualify bare repo name matching default repo", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = { repo: "gh-aw" };
      const defaultRepo = "github/gh-aw";
      const allowedRepos = new Set();

      const result = resolveAndValidateRepo(item, defaultRepo, allowedRepos, "test");

      expect(result.success).toBe(true);
      expect(result.repo).toBe("github/gh-aw");
      expect(result.repoParts).toEqual({ owner: "github", repo: "gh-aw" });
    });

    it("should fail with descriptive error when defaultTargetRepo is empty", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = {};
      const allowedRepos = new Set();

      const result = resolveAndValidateRepo(item, "", allowedRepos, "pull request");

      expect(result.success).toBe(false);
      expect(result.error).toContain("Unable to determine target repository for pull request");
      expect(result.error).toContain("GH_AW_TARGET_REPO_SLUG");
      expect(result.error).toContain("GITHUB_REPOSITORY");
    });

    it("should succeed when item.repo is provided even if defaultTargetRepo is empty", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const item = { repo: "org/explicit-repo" };
      const allowedRepos = new Set(["org/explicit-repo"]);

      const result = resolveAndValidateRepo(item, "", allowedRepos, "pull request");

      expect(result.success).toBe(true);
      expect(result.repo).toBe("org/explicit-repo");
      expect(result.repoParts).toEqual({ owner: "org", repo: "explicit-repo" });
    });
  });

  describe("resolveTargetRepoConfig", () => {
    it("should resolve config with target-repo and allowed-repos", async () => {
      const { resolveTargetRepoConfig } = await import("./repo_helpers.cjs");
      const config = {
        "target-repo": "org/target-repo",
        allowed_repos: ["org/allowed-a", "org/allowed-b"],
      };

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("org/target-repo");
      expect(result.allowedRepos.size).toBe(2);
      expect(result.allowedRepos.has("org/allowed-a")).toBe(true);
      expect(result.allowedRepos.has("org/allowed-b")).toBe(true);
    });

    it("should resolve config with env var and no allowed-repos", async () => {
      process.env.GH_AW_TARGET_REPO_SLUG = "env/target-repo";
      const { resolveTargetRepoConfig } = await import("./repo_helpers.cjs");
      const config = {};

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("env/target-repo");
      expect(result.allowedRepos.size).toBe(0);
    });

    it("should resolve config with context fallback", async () => {
      delete process.env.GH_AW_TARGET_REPO_SLUG;
      const { resolveTargetRepoConfig } = await import("./repo_helpers.cjs");
      const config = {};

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("test-owner/test-repo");
      expect(result.allowedRepos.size).toBe(0);
    });

    it("should handle comma-separated allowed-repos string", async () => {
      const { resolveTargetRepoConfig } = await import("./repo_helpers.cjs");
      const config = {
        "target-repo": "org/main",
        allowed_repos: "org/repo-1, org/repo-2, org/repo-3",
      };

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("org/main");
      expect(result.allowedRepos.size).toBe(3);
      expect(result.allowedRepos.has("org/repo-1")).toBe(true);
      expect(result.allowedRepos.has("org/repo-2")).toBe(true);
      expect(result.allowedRepos.has("org/repo-3")).toBe(true);
    });

    it('should pass through wildcard "*" as defaultTargetRepo', async () => {
      const { resolveTargetRepoConfig } = await import("./repo_helpers.cjs");
      const config = { "target-repo": "*" };

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("*");
      expect(result.allowedRepos.size).toBe(0);
    });
  });

  describe('resolveAndValidateRepo with wildcard target-repo "*"', () => {
    it("should allow any valid owner/repo slug when defaultTargetRepo is *", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const result = resolveAndValidateRepo({ repo: "any-org/any-repo" }, "*", new Set(), "test");

      expect(result.success).toBe(true);
      expect(result.repo).toBe("any-org/any-repo");
      expect(result.repoParts).toEqual({ owner: "any-org", repo: "any-repo" });
    });

    it("should use item.repo when defaultTargetRepo is * and no repo in item", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      // When item has no repo and defaultTargetRepo is "*", itemRepo becomes "*"
      // which fails parseRepoSlug — so the caller should always supply item.repo with *
      const result = resolveAndValidateRepo({}, "*", new Set(), "test");

      // "*" is not a valid owner/repo slug (no slash with two parts)
      expect(result.success).toBe(false);
    });

    it("should reject bare repo name (no slash) when defaultTargetRepo is *", async () => {
      const { resolveAndValidateRepo } = await import("./repo_helpers.cjs");
      const result = resolveAndValidateRepo({ repo: "bare-repo" }, "*", new Set(), "test");

      // bare-repo qualifies using defaultRepo's org, but "*" has no org → falls back to bare name
      // which is invalid as owner/repo
      expect(result.success).toBe(false);
    });
  });
});

describe("resolveExecutionOwnerRepo", () => {
  beforeEach(() => {
    vi.resetModules();
    delete process.env.GH_AW_TARGET_REPO_SLUG;
    global.context = { repo: { owner: "ctx-owner", repo: "ctx-repo" } };
  });

  it("should fall back to context.repo when env var is not set", async () => {
    const { resolveExecutionOwnerRepo } = await import("./repo_helpers.cjs");
    const result = resolveExecutionOwnerRepo();
    expect(result).toEqual({ owner: "ctx-owner", repo: "ctx-repo" });
  });

  it("should use GH_AW_TARGET_REPO_SLUG when set to a valid slug", async () => {
    process.env.GH_AW_TARGET_REPO_SLUG = "my-org/target-repo";
    const { resolveExecutionOwnerRepo } = await import("./repo_helpers.cjs");
    const result = resolveExecutionOwnerRepo();
    expect(result).toEqual({ owner: "my-org", repo: "target-repo" });
  });

  it("should throw when slug is malformed (no slash)", async () => {
    process.env.GH_AW_TARGET_REPO_SLUG = "malformed-slug";
    const { resolveExecutionOwnerRepo } = await import("./repo_helpers.cjs");
    expect(() => resolveExecutionOwnerRepo()).toThrow(/Invalid GH_AW_TARGET_REPO_SLUG/);
  });

  it("should throw when slug has multiple slashes", async () => {
    process.env.GH_AW_TARGET_REPO_SLUG = "a/b/c";
    const { resolveExecutionOwnerRepo } = await import("./repo_helpers.cjs");
    expect(() => resolveExecutionOwnerRepo()).toThrow(/Invalid GH_AW_TARGET_REPO_SLUG/);
  });
});

describe("resolveFailureIssueRepo", () => {
  beforeEach(() => {
    vi.resetModules();
    delete process.env.GH_AW_FAILURE_ISSUE_REPO;
    delete process.env.GH_AW_FAILURE_ISSUE_REPO_FROM_EXPRESSION;
  });

  it("should return null when no failure issue repo is configured", async () => {
    const { resolveFailureIssueRepo } = await import("./repo_helpers.cjs");
    expect(resolveFailureIssueRepo("ctx-owner/ctx-repo")).toBeNull();
  });

  it("should trust a compile-time literal pointing at another owner", async () => {
    process.env.GH_AW_FAILURE_ISSUE_REPO = "other-owner/failure-repo";
    const { resolveFailureIssueRepo } = await import("./repo_helpers.cjs");
    expect(resolveFailureIssueRepo("ctx-owner/ctx-repo")).toEqual({ owner: "other-owner", repo: "failure-repo" });
  });

  it("should allow an expression-derived repo within the current owner", async () => {
    process.env.GH_AW_FAILURE_ISSUE_REPO = "ctx-owner/failure-repo";
    process.env.GH_AW_FAILURE_ISSUE_REPO_FROM_EXPRESSION = "true";
    const { resolveFailureIssueRepo } = await import("./repo_helpers.cjs");
    expect(resolveFailureIssueRepo("ctx-owner/ctx-repo")).toEqual({ owner: "ctx-owner", repo: "failure-repo" });
  });

  it("should reject an expression-derived repo outside the current owner", async () => {
    process.env.GH_AW_FAILURE_ISSUE_REPO = "attacker-owner/exfil";
    process.env.GH_AW_FAILURE_ISSUE_REPO_FROM_EXPRESSION = "true";
    const warn = vi.fn();
    const { resolveFailureIssueRepo } = await import("./repo_helpers.cjs");
    expect(resolveFailureIssueRepo("ctx-owner/ctx-repo", warn)).toBeNull();
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("attacker-owner/exfil"));
  });

  it("should ignore a malformed value", async () => {
    process.env.GH_AW_FAILURE_ISSUE_REPO = "not-a-repo";
    const warn = vi.fn();
    const { resolveFailureIssueRepo } = await import("./repo_helpers.cjs");
    expect(resolveFailureIssueRepo("ctx-owner/ctx-repo", warn)).toBeNull();
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("not-a-repo"));
  });
});
