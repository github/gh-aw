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
  });
});
