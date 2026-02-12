import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock the context global
const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
};

global.context = mockContext;

describe("allowed_repos_helpers", () => {
  beforeEach(() => {
    vi.resetModules();
    delete process.env.GH_AW_TARGET_REPO_SLUG;
    global.context = mockContext;
  });

  describe("parseAllowedRepos", () => {
    it("should return empty set when value is undefined", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos(undefined);
      expect(result.size).toBe(0);
    });

    it("should parse single repo from string", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos("org/repo-a");
      expect(result.size).toBe(1);
      expect(result.has("org/repo-a")).toBe(true);
    });

    it("should parse multiple repos from comma-separated string", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos("org/repo-a, org/repo-b, org/repo-c");
      expect(result.size).toBe(3);
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
      expect(result.has("org/repo-c")).toBe(true);
    });

    it("should parse repos from array", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos(["org/repo-a", "org/repo-b"]);
      expect(result.size).toBe(2);
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });

    it("should trim whitespace from repo names in string", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos("  org/repo-a  ,  org/repo-b  ");
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });

    it("should trim whitespace from repo names in array", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos(["  org/repo-a  ", "  org/repo-b  "]);
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });

    it("should filter out empty strings", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos("org/repo-a,,org/repo-b,  ,");
      expect(result.size).toBe(2);
    });

    it("should handle arrays with empty strings", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos(["org/repo-a", "", "org/repo-b", "  "]);
      expect(result.size).toBe(2);
      expect(result.has("org/repo-a")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });

    it("should deduplicate repos", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos(["org/repo-a", "org/repo-a", "org/repo-b"]);
      expect(result.size).toBe(2);
    });

    it("should handle mixed case in repo names", async () => {
      const { parseAllowedRepos } = await import("./allowed_repos_helpers.cjs");
      const result = parseAllowedRepos(["Org/Repo-A", "org/repo-b"]);
      expect(result.size).toBe(2);
      expect(result.has("Org/Repo-A")).toBe(true);
      expect(result.has("org/repo-b")).toBe(true);
    });
  });

  describe("getDefaultTargetRepo", () => {
    it("should return target-repo from config when provided", async () => {
      const { getDefaultTargetRepo } = await import("./allowed_repos_helpers.cjs");
      const config = { "target-repo": "config-org/config-repo" };
      const result = getDefaultTargetRepo(config);
      expect(result).toBe("config-org/config-repo");
    });

    it("should prefer config target-repo over env variable", async () => {
      process.env.GH_AW_TARGET_REPO_SLUG = "env-org/env-repo";
      const { getDefaultTargetRepo } = await import("./allowed_repos_helpers.cjs");
      const config = { "target-repo": "config-org/config-repo" };
      const result = getDefaultTargetRepo(config);
      expect(result).toBe("config-org/config-repo");
    });

    it("should return target-repo override when set", async () => {
      process.env.GH_AW_TARGET_REPO_SLUG = "override-org/override-repo";
      const { getDefaultTargetRepo } = await import("./allowed_repos_helpers.cjs");
      const result = getDefaultTargetRepo();
      expect(result).toBe("override-org/override-repo");
    });

    it("should fall back to context repo when no override", async () => {
      const { getDefaultTargetRepo } = await import("./allowed_repos_helpers.cjs");
      const result = getDefaultTargetRepo();
      expect(result).toBe("test-owner/test-repo");
    });

    it("should handle empty config object", async () => {
      const { getDefaultTargetRepo } = await import("./allowed_repos_helpers.cjs");
      const result = getDefaultTargetRepo({});
      expect(result).toBe("test-owner/test-repo");
    });

    it("should handle null config", async () => {
      const { getDefaultTargetRepo } = await import("./allowed_repos_helpers.cjs");
      const result = getDefaultTargetRepo(null);
      expect(result).toBe("test-owner/test-repo");
    });

    it("should handle config with empty target-repo string", async () => {
      const { getDefaultTargetRepo } = await import("./allowed_repos_helpers.cjs");
      const config = { "target-repo": "" };
      const result = getDefaultTargetRepo(config);
      // Empty string should fall back to env or context
      expect(result).toBe("test-owner/test-repo");
    });
  });

  describe("resolveTargetRepoConfig", () => {
    it("should resolve config with target-repo and allowed-repos", async () => {
      const { resolveTargetRepoConfig } = await import("./allowed_repos_helpers.cjs");
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
      const { resolveTargetRepoConfig } = await import("./allowed_repos_helpers.cjs");
      const config = {};

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("env/target-repo");
      expect(result.allowedRepos.size).toBe(0);
    });

    it("should resolve config with context fallback", async () => {
      delete process.env.GH_AW_TARGET_REPO_SLUG;
      const { resolveTargetRepoConfig } = await import("./allowed_repos_helpers.cjs");
      const config = {};

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("test-owner/test-repo");
      expect(result.allowedRepos.size).toBe(0);
    });

    it("should handle comma-separated allowed-repos string", async () => {
      const { resolveTargetRepoConfig } = await import("./allowed_repos_helpers.cjs");
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

    it("should handle empty allowed_repos", async () => {
      const { resolveTargetRepoConfig } = await import("./allowed_repos_helpers.cjs");
      const config = {
        "target-repo": "org/target",
        allowed_repos: [],
      };

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("org/target");
      expect(result.allowedRepos.size).toBe(0);
    });

    it("should handle undefined allowed_repos", async () => {
      const { resolveTargetRepoConfig } = await import("./allowed_repos_helpers.cjs");
      const config = {
        "target-repo": "org/target",
      };

      const result = resolveTargetRepoConfig(config);

      expect(result.defaultTargetRepo).toBe("org/target");
      expect(result.allowedRepos.size).toBe(0);
    });

    it("should handle microsoft/vscode example from issue", async () => {
      const { resolveTargetRepoConfig } = await import("./allowed_repos_helpers.cjs");
      const config = {
        allowed_repos: ["microsoft/vscode"],
      };

      const result = resolveTargetRepoConfig(config);

      expect(result.allowedRepos.size).toBe(1);
      expect(result.allowedRepos.has("microsoft/vscode")).toBe(true);
    });
  });
});
