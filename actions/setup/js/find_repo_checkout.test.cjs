const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync } = require("child_process");
const { extractRepoSlugFromUrl, extractRemoteHost, isTrustedRemoteHost, normalizeRepoSlug, findGitDirectories, findRepoCheckout, buildRepoCheckoutMap } = require("./find_repo_checkout.cjs");
const { getPatchPathForBranchInRepo, sanitizeBranchNameForPatch, sanitizeRepoSlugForPatch } = require("./generate_git_patch.cjs");
const { _resetCache: resetCheckoutManifestCache } = require("./checkout_manifest.cjs");

describe("find_repo_checkout", () => {
  describe("extractRepoSlugFromUrl", () => {
    it("should extract slug from HTTPS URL", () => {
      expect(extractRepoSlugFromUrl("https://github.com/owner/repo.git")).toBe("owner/repo");
      expect(extractRepoSlugFromUrl("https://github.com/owner/repo")).toBe("owner/repo");
    });

    it("should extract slug from SSH URL", () => {
      expect(extractRepoSlugFromUrl("git@github.com:owner/repo.git")).toBe("owner/repo");
      expect(extractRepoSlugFromUrl("git@github.com:owner/repo")).toBe("owner/repo");
    });

    it("should handle GitHub Enterprise URLs", () => {
      expect(extractRepoSlugFromUrl("https://github.example.com/org/project.git")).toBe("org/project");
      expect(extractRepoSlugFromUrl("git@github.example.com:org/project.git")).toBe("org/project");
    });

    it("should normalize to lowercase", () => {
      expect(extractRepoSlugFromUrl("https://github.com/Owner/Repo.git")).toBe("owner/repo");
      expect(extractRepoSlugFromUrl("git@github.com:OWNER/REPO")).toBe("owner/repo");
    });

    it("should return null for invalid URLs", () => {
      expect(extractRepoSlugFromUrl("")).toBeNull();
      expect(extractRepoSlugFromUrl("invalid")).toBeNull();
      expect(extractRepoSlugFromUrl(null)).toBeNull();
      expect(extractRepoSlugFromUrl(undefined)).toBeNull();
    });

    it("should handle URLs with ports", () => {
      expect(extractRepoSlugFromUrl("https://github.example.com:8443/org/repo.git")).toBe("org/repo");
    });

    it("should handle HTTP URLs", () => {
      expect(extractRepoSlugFromUrl("http://github.local/owner/repo")).toBe("owner/repo");
    });
  });

  describe("normalizeRepoSlug", () => {
    it("should normalize to lowercase", () => {
      expect(normalizeRepoSlug("Owner/Repo")).toBe("owner/repo");
      expect(normalizeRepoSlug("ORG/PROJECT")).toBe("org/project");
    });

    it("should trim whitespace", () => {
      expect(normalizeRepoSlug("  owner/repo  ")).toBe("owner/repo");
    });

    it("should return empty string for invalid input", () => {
      expect(normalizeRepoSlug("")).toBe("");
      expect(normalizeRepoSlug(null)).toBe("");
      expect(normalizeRepoSlug(undefined)).toBe("");
    });

    it("should handle tabs and newlines", () => {
      expect(normalizeRepoSlug("\towner/repo\n")).toBe("owner/repo");
    });
  });

  describe("findGitDirectories", () => {
    let testDir;

    beforeEach(() => {
      testDir = `/tmp/test-find-git-dirs-${Date.now()}`;
      fs.mkdirSync(testDir, { recursive: true });
    });

    afterEach(() => {
      try {
        fs.rmSync(testDir, { recursive: true, force: true });
      } catch {
        // Ignore cleanup errors
      }
    });

    it("should find git directories in workspace", () => {
      // Create a mock git repo structure
      fs.mkdirSync(path.join(testDir, "repo-a", ".git"), { recursive: true });
      fs.mkdirSync(path.join(testDir, "repo-b", ".git"), { recursive: true });

      const dirs = findGitDirectories(testDir);

      expect(dirs).toHaveLength(2);
      expect(dirs).toContain(path.join(testDir, "repo-a"));
      expect(dirs).toContain(path.join(testDir, "repo-b"));
    });

    it("should handle nested repos", () => {
      // Create a nested structure
      fs.mkdirSync(path.join(testDir, "projects", "frontend", ".git"), { recursive: true });
      fs.mkdirSync(path.join(testDir, "projects", "backend", ".git"), { recursive: true });

      const dirs = findGitDirectories(testDir);

      expect(dirs).toHaveLength(2);
      expect(dirs).toContain(path.join(testDir, "projects", "frontend"));
      expect(dirs).toContain(path.join(testDir, "projects", "backend"));
    });

    it("should skip node_modules", () => {
      fs.mkdirSync(path.join(testDir, "node_modules", "some-pkg", ".git"), { recursive: true });
      fs.mkdirSync(path.join(testDir, "actual-repo", ".git"), { recursive: true });

      const dirs = findGitDirectories(testDir);

      expect(dirs).toHaveLength(1);
      expect(dirs).toContain(path.join(testDir, "actual-repo"));
    });

    it("should detect repos with .git gitdir-link files", () => {
      const worktreeRepo = path.join(testDir, "worktree-repo");
      fs.mkdirSync(worktreeRepo, { recursive: true });
      fs.writeFileSync(path.join(worktreeRepo, ".git"), "gitdir: /tmp/example/.git/worktrees/worktree-repo\n");

      const dirs = findGitDirectories(testDir);

      expect(dirs).toContain(worktreeRepo);
    });

    it("should return empty array when no git dirs found", () => {
      fs.mkdirSync(path.join(testDir, "empty-folder"), { recursive: true });

      const dirs = findGitDirectories(testDir);

      expect(dirs).toEqual([]);
    });

    it("should respect maxDepth", () => {
      // Create a deeply nested repo
      fs.mkdirSync(path.join(testDir, "a", "b", "c", "d", "e", "f", ".git"), { recursive: true });

      const dirs = findGitDirectories(testDir, 3);

      // Should not find the deeply nested repo
      expect(dirs).toEqual([]);
    });
  });

  describe("findRepoCheckout", () => {
    it("should return error for invalid repo slug", () => {
      const result = findRepoCheckout("");
      expect(result.success).toBe(false);
      expect(result.error).toBe("Invalid repo slug provided");
    });

    it("should return error for null repo slug", () => {
      const result = findRepoCheckout(null);
      expect(result.success).toBe(false);
      expect(result.error).toBe("Invalid repo slug provided");
    });

    it("should return not found for missing repo", () => {
      const testDir = `/tmp/test-find-repo-${Date.now()}`;
      fs.mkdirSync(testDir, { recursive: true });

      try {
        const result = findRepoCheckout("owner/missing-repo", testDir);
        expect(result.success).toBe(false);
        expect(result.error).toContain("not found in workspace");
      } finally {
        fs.rmSync(testDir, { recursive: true, force: true });
      }
    });
  });

  describe("checkout manifest fallback (issue #37545)", () => {
    let workspaceDir;
    let manifestPath;
    let originalManifestEnv;

    beforeEach(() => {
      originalManifestEnv = process.env.GH_AW_CHECKOUT_MANIFEST;
      workspaceDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "test-manifest-"));
      manifestPath = path.join(workspaceDir, "checkout-manifest.json");
      process.env.GH_AW_CHECKOUT_MANIFEST = manifestPath;
      resetCheckoutManifestCache();
    });

    afterEach(() => {
      if (originalManifestEnv === undefined) {
        delete process.env.GH_AW_CHECKOUT_MANIFEST;
      } else {
        process.env.GH_AW_CHECKOUT_MANIFEST = originalManifestEnv;
      }
      resetCheckoutManifestCache();
      try {
        fs.rmSync(workspaceDir, { recursive: true, force: true });
      } catch {
        // ignore
      }
    });

    it("findRepoCheckout returns the manifest path even when remote.origin.url has been clobbered", () => {
      // Reproduces the bug from #37545: the workflow checked out githubnext/gh-aw-side-repo
      // at the workspace root, but a later "Configure Git credentials" step rewrote
      // origin to point at githubnext/gh-aw-test. The manifest is the source of truth.
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "githubnext/gh-aw-side-repo": {
            repository: "githubnext/gh-aw-side-repo",
            path: "",
            default_branch: "main",
          },
        })
      );

      const result = findRepoCheckout("githubnext/gh-aw-side-repo", workspaceDir);

      expect(result.success).toBe(true);
      expect(result.path).toBe(workspaceDir);
      expect(result.repoSlug).toBe("githubnext/gh-aw-side-repo");
    });

    it("findRepoCheckout resolves manifest entries with a non-empty path to a subdirectory", () => {
      fs.mkdirSync(path.join(workspaceDir, "repos/sub-repo"), { recursive: true });
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "owner/sub-repo": {
            repository: "owner/sub-repo",
            path: "repos/sub-repo",
            default_branch: "main",
          },
        })
      );

      const result = findRepoCheckout("owner/sub-repo", workspaceDir);

      expect(result.success).toBe(true);
      expect(result.path).toBe(path.join(workspaceDir, "repos/sub-repo"));
    });

    it("findRepoCheckout is case-insensitive on the repo slug", () => {
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "githubnext/gh-aw-side-repo": {
            repository: "githubnext/gh-aw-side-repo",
            path: "",
            default_branch: "main",
          },
        })
      );

      const result = findRepoCheckout("GithubNext/gh-aw-side-repo", workspaceDir);

      expect(result.success).toBe(true);
      expect(result.path).toBe(workspaceDir);
    });

    it("findRepoCheckout still applies allowedRepos validation when the manifest matches", () => {
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "owner/blocked": { repository: "owner/blocked", path: "blocked", default_branch: "main" },
        })
      );

      const result = findRepoCheckout("owner/blocked", workspaceDir, {
        allowedRepos: ["owner/allowed"],
      });

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it("findRepoCheckout falls back to the git-remote scan when the manifest has no entry", () => {
      fs.writeFileSync(manifestPath, JSON.stringify({}));

      const result = findRepoCheckout("owner/missing", workspaceDir);

      expect(result.success).toBe(false);
      expect(result.error).toContain("not found in workspace");
    });

    it("buildRepoCheckoutMap seeds from manifest entries", () => {
      fs.mkdirSync(path.join(workspaceDir, "repos/sub-repo"), { recursive: true });
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "githubnext/gh-aw-side-repo": {
            repository: "githubnext/gh-aw-side-repo",
            path: "",
            default_branch: "main",
          },
          "owner/sub-repo": {
            repository: "owner/sub-repo",
            path: "repos/sub-repo",
            default_branch: "main",
          },
        })
      );

      const map = buildRepoCheckoutMap(workspaceDir);

      expect(map.get("githubnext/gh-aw-side-repo")).toBe(workspaceDir);
      expect(map.get("owner/sub-repo")).toBe(path.join(workspaceDir, "repos/sub-repo"));
    });

    it("findRepoCheckout falls back to git-scan when GH_AW_CHECKOUT_MANIFEST is unset", () => {
      // Pin the contract that loadManifest() returns an empty object (not
      // throws) when the manifest env var is unset and no $RUNNER_TEMP fallback
      // exists. Critical for backward compatibility with workflows that have no
      // multi-repo checkout.
      delete process.env.GH_AW_CHECKOUT_MANIFEST;
      const originalRunnerTemp = process.env.RUNNER_TEMP;
      delete process.env.RUNNER_TEMP;
      resetCheckoutManifestCache();

      try {
        const result = findRepoCheckout("owner/missing", workspaceDir);

        expect(result.success).toBe(false);
        expect(result.error).toContain("not found in workspace");
      } finally {
        if (originalRunnerTemp !== undefined) {
          process.env.RUNNER_TEMP = originalRunnerTemp;
        }
      }
    });

    it("findRepoCheckout falls back to git-scan when GH_AW_CHECKOUT_MANIFEST points to a non-existent file", () => {
      process.env.GH_AW_CHECKOUT_MANIFEST = path.join(workspaceDir, "does-not-exist.json");
      resetCheckoutManifestCache();

      const result = findRepoCheckout("owner/missing", workspaceDir);

      expect(result.success).toBe(false);
      expect(result.error).toContain("not found in workspace");
    });

    it("findRepoCheckout falls back to git-scan when the manifest path does not exist on disk", () => {
      // A stale manifest entry (failed checkout, workspace wiped) must not be
      // returned as a valid checkout — the git scan is authoritative for paths
      // that actually exist on disk.
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "owner/stale": {
            repository: "owner/stale",
            path: "never-checked-out",
            default_branch: "main",
          },
        })
      );

      const result = findRepoCheckout("owner/stale", workspaceDir);

      expect(result.success).toBe(false);
      expect(result.error).toContain("not found in workspace");
    });

    it("findRepoCheckout rejects manifest entries with absolute paths", () => {
      // resolveManifestPath() must refuse absolute paths so a tampered manifest
      // cannot redirect lookups outside $GITHUB_WORKSPACE.
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "owner/escape": {
            repository: "owner/escape",
            path: "/etc",
            default_branch: "main",
          },
        })
      );

      const result = findRepoCheckout("owner/escape", workspaceDir);

      expect(result.success).toBe(false);
      expect(result.error).toContain("not found in workspace");
    });

    it("findRepoCheckout rejects manifest entries that escape the workspace via .. traversal", () => {
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "owner/escape": {
            repository: "owner/escape",
            path: "../outside",
            default_branch: "main",
          },
        })
      );

      const result = findRepoCheckout("owner/escape", workspaceDir);

      expect(result.success).toBe(false);
      expect(result.error).toContain("not found in workspace");
    });

    it("buildRepoCheckoutMap: manifest entry wins over git-scan for the same slug", () => {
      // The !map.has(slug) guard added in buildRepoCheckoutMap is the core of
      // the bug fix; without this test the guard could be silently removed and
      // the regression would go undetected.
      const manifestRepoPath = path.join(workspaceDir, "from-manifest");
      const gitScanRepoPath = path.join(workspaceDir, "from-git-scan");
      fs.mkdirSync(manifestRepoPath, { recursive: true });
      fs.mkdirSync(path.join(gitScanRepoPath, ".git"), { recursive: true });
      // Real git config so getRemoteOriginUrl resolves the same slug as the
      // manifest entry, simulating the clobbered-origin scenario.
      fs.writeFileSync(path.join(gitScanRepoPath, ".git", "config"), `[remote "origin"]\n\turl = https://github.com/owner/conflict.git\n`);
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "owner/conflict": {
            repository: "owner/conflict",
            path: "from-manifest",
            default_branch: "main",
          },
        })
      );

      const map = buildRepoCheckoutMap(workspaceDir);

      expect(map.get("owner/conflict")).toBe(manifestRepoPath);
    });

    it("buildRepoCheckoutMap skips manifest entries whose path does not exist", () => {
      fs.writeFileSync(
        manifestPath,
        JSON.stringify({
          "owner/stale": {
            repository: "owner/stale",
            path: "missing-on-disk",
            default_branch: "main",
          },
        })
      );

      const map = buildRepoCheckoutMap(workspaceDir);

      expect(map.has("owner/stale")).toBe(false);
    });
  });

  describe("git-scan fallback reads remotes with scoped safe.directory trust (issue #60021)", () => {
    let workspaceDir;
    let oldPath;
    let oldGitScanEnvLog;
    let oldGitConfigCount;
    let oldGitConfigKey0;
    let oldGitConfigValue0;
    let oldCore;

    beforeEach(() => {
      oldPath = process.env.PATH;
      oldGitScanEnvLog = process.env.GIT_SCAN_ENV_LOG;
      oldGitConfigCount = process.env.GIT_CONFIG_COUNT;
      oldGitConfigKey0 = process.env.GIT_CONFIG_KEY_0;
      oldGitConfigValue0 = process.env.GIT_CONFIG_VALUE_0;
      oldCore = globalThis.core;
      globalThis.core = { debug: () => {}, error: () => {} };
      delete process.env.GIT_CONFIG_COUNT;
      delete process.env.GIT_CONFIG_KEY_0;
      delete process.env.GIT_CONFIG_VALUE_0;
      workspaceDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "test-safedir-"));
    });

    afterEach(() => {
      oldPath === undefined ? delete process.env.PATH : (process.env.PATH = oldPath);
      oldGitScanEnvLog === undefined ? delete process.env.GIT_SCAN_ENV_LOG : (process.env.GIT_SCAN_ENV_LOG = oldGitScanEnvLog);
      oldGitConfigCount === undefined ? delete process.env.GIT_CONFIG_COUNT : (process.env.GIT_CONFIG_COUNT = oldGitConfigCount);
      oldGitConfigKey0 === undefined ? delete process.env.GIT_CONFIG_KEY_0 : (process.env.GIT_CONFIG_KEY_0 = oldGitConfigKey0);
      oldGitConfigValue0 === undefined ? delete process.env.GIT_CONFIG_VALUE_0 : (process.env.GIT_CONFIG_VALUE_0 = oldGitConfigValue0);
      oldCore === undefined ? delete globalThis.core : (globalThis.core = oldCore);
      try {
        fs.rmSync(workspaceDir, { recursive: true, force: true });
      } catch {
        // ignore
      }
    });

    it("injects safe.directory only for the git config read", () => {
      const nestedRepo = path.join(workspaceDir, "repos", "analytics");
      fs.mkdirSync(path.join(nestedRepo, ".git"), { recursive: true });
      const binDir = path.join(workspaceDir, "bin");
      fs.mkdirSync(binDir, { recursive: true });
      const envLog = path.join(workspaceDir, "git-env.log");
      const fakeGit = path.join(binDir, "git");
      fs.writeFileSync(
        fakeGit,
        `#!/bin/sh
printf '%s\\t%s\\t%s\\t%s\\n' "$PWD" "$GIT_CONFIG_COUNT" "$GIT_CONFIG_KEY_0" "$GIT_CONFIG_VALUE_0" >> "$GIT_SCAN_ENV_LOG"
if [ "$1" = "config" ] && [ "$2" = "--get" ] && [ "$3" = "remote.origin.url" ]; then
  echo "https://github.com/owner/analytics.git"
  exit 0
fi
exit 1
`
      );
      fs.chmodSync(fakeGit, 0o755);
      process.env.PATH = `${binDir}${path.delimiter}${process.env.PATH || ""}`;
      process.env.GIT_SCAN_ENV_LOG = envLog;

      const result = findRepoCheckout("owner/analytics", workspaceDir);

      expect(result.success).toBe(true);
      expect(result.path).toBe(nestedRepo);
      expect(process.env.GIT_CONFIG_COUNT).toBeUndefined();
      expect(process.env.GIT_CONFIG_KEY_0).toBeUndefined();
      expect(process.env.GIT_CONFIG_VALUE_0).toBeUndefined();
      const configRead = fs.readFileSync(envLog, "utf8").trim().split("\n")[0].split("\t");
      expect(configRead).toEqual([nestedRepo, "1", "safe.directory", path.resolve(nestedRepo)]);
    });

    it("reports a not-found error that mentions both checkout: and steps: clones", () => {
      const result = findRepoCheckout("owner/analytics", workspaceDir);

      expect(result.success).toBe(false);
      expect(result.error).toContain("checkout:");
      expect(result.error).toContain("steps:");
    });

    it("ignores a scanned repository whose remote points at another host", () => {
      const plantedRepo = path.join(workspaceDir, "repos", "planted");
      fs.mkdirSync(path.join(plantedRepo, ".git"), { recursive: true });
      const binDir = path.join(workspaceDir, "bin");
      fs.mkdirSync(binDir, { recursive: true });
      const fakeGit = path.join(binDir, "git");
      fs.writeFileSync(
        fakeGit,
        `#!/bin/sh
if [ "$1" = "config" ] && [ "$2" = "--get" ] && [ "$3" = "remote.origin.url" ]; then
  echo "https://attacker.example/owner/analytics.git"
  exit 0
fi
exit 1
`
      );
      fs.chmodSync(fakeGit, 0o755);
      process.env.PATH = `${binDir}${path.delimiter}${process.env.PATH || ""}`;
      process.env.GIT_SCAN_ENV_LOG = path.join(workspaceDir, "git-env.log");

      const result = findRepoCheckout("owner/analytics", workspaceDir);

      expect(result.success).toBe(false);
    });
  });

  describe("git-scan fallback with a dubious-ownership checkout", () => {
    let workspaceDir;
    let originalEnv;
    let originalCore;

    beforeEach(() => {
      workspaceDir = fs.mkdtempSync(path.join(os.tmpdir(), "test-safedir-git-"));
      originalEnv = {
        GIT_CONFIG_GLOBAL: process.env.GIT_CONFIG_GLOBAL,
        GIT_CONFIG_NOSYSTEM: process.env.GIT_CONFIG_NOSYSTEM,
        GITHUB_ENV: process.env.GITHUB_ENV,
        GITHUB_SERVER_URL: process.env.GITHUB_SERVER_URL,
      };
      originalCore = globalThis.core;
      globalThis.core = { debug: () => {}, error: () => {} };
      process.env.GIT_CONFIG_GLOBAL = os.devNull;
      process.env.GIT_CONFIG_NOSYSTEM = "1";
      process.env.GITHUB_SERVER_URL = "https://github.com";
      delete process.env.GITHUB_ENV;
    });

    afterEach(() => {
      spawnSync("sudo", ["-n", "rm", "-rf", workspaceDir]);
      for (const [key, value] of Object.entries(originalEnv)) {
        value === undefined ? delete process.env[key] : (process.env[key] = value);
      }
      originalCore === undefined ? delete globalThis.core : (globalThis.core = originalCore);
    });

    it("discovers a nested checkout using per-invocation safe.directory trust", () => {
      const sudoAvailable = spawnSync("sudo", ["-n", "true"], { encoding: "utf8" });
      expect(sudoAvailable.status).toBe(0);

      const nestedRepo = path.join(workspaceDir, "repos", "analytics");
      fs.mkdirSync(nestedRepo, { recursive: true });
      expect(spawnSync("git", ["init", "-q"], { cwd: nestedRepo }).status).toBe(0);
      expect(spawnSync("git", ["remote", "add", "origin", "https://github.com/owner/analytics.git"], { cwd: nestedRepo }).status).toBe(0);
      expect(spawnSync("sudo", ["-n", "chown", "-R", "12345:12345", nestedRepo]).status).toBe(0);

      const untrustedRead = spawnSync("git", ["config", "--get", "remote.origin.url"], {
        cwd: nestedRepo,
        encoding: "utf8",
        env: process.env,
      });
      expect(untrustedRead.status).not.toBe(0);

      expect(findRepoCheckout("owner/analytics", workspaceDir)).toMatchObject({
        success: true,
        path: nestedRepo,
        repoSlug: "owner/analytics",
      });
    });
  });

  describe("remote host trust", () => {
    let oldServerUrl;

    beforeEach(() => {
      oldServerUrl = process.env.GITHUB_SERVER_URL;
    });

    afterEach(() => {
      oldServerUrl === undefined ? delete process.env.GITHUB_SERVER_URL : (process.env.GITHUB_SERVER_URL = oldServerUrl);
    });

    it("extracts the host from HTTPS and SSH remotes", () => {
      expect(extractRemoteHost("https://github.com/owner/repo.git")).toBe("github.com");
      expect(extractRemoteHost("https://user@github.com/owner/repo.git")).toBe("github.com");
      expect(extractRemoteHost("https://github.example.com:8443/owner/repo.git")).toBe("github.example.com");
      expect(extractRemoteHost("git@github.com:owner/repo.git")).toBe("github.com");
      expect(extractRemoteHost("/local/path/repo")).toBeNull();
      expect(extractRemoteHost("")).toBeNull();
    });

    it("trusts github.com and the configured GitHub server host only", () => {
      delete process.env.GITHUB_SERVER_URL;
      expect(isTrustedRemoteHost("https://github.com/owner/repo.git")).toBe(true);
      expect(isTrustedRemoteHost("git@GitHub.com:owner/repo.git")).toBe(true);
      expect(isTrustedRemoteHost("https://attacker.example/owner/repo.git")).toBe(false);

      process.env.GITHUB_SERVER_URL = "https://github.example.com";
      expect(isTrustedRemoteHost("https://github.example.com/owner/repo.git")).toBe(true);
      expect(isTrustedRemoteHost("https://github.com/owner/repo.git")).toBe(true);
      expect(isTrustedRemoteHost("https://attacker.example/owner/repo.git")).toBe(false);

      process.env.GITHUB_SERVER_URL = "not a url";
      expect(isTrustedRemoteHost("https://github.com/owner/repo.git")).toBe(true);
      expect(isTrustedRemoteHost("https://attacker.example/owner/repo.git")).toBe(false);
    });
  });

  describe("buildRepoCheckoutMap", () => {
    let testDir;

    beforeEach(() => {
      testDir = `/tmp/test-build-map-${Date.now()}`;
      fs.mkdirSync(testDir, { recursive: true });
    });

    afterEach(() => {
      try {
        fs.rmSync(testDir, { recursive: true, force: true });
      } catch {
        // Ignore cleanup errors
      }
    });

    it("should return empty map when no repos found", () => {
      const map = buildRepoCheckoutMap(testDir);
      expect(map.size).toBe(0);
    });

    it("should find repos with valid git remotes", () => {
      // Create a mock repo with a config file
      const repoPath = path.join(testDir, "my-repo", ".git");
      fs.mkdirSync(repoPath, { recursive: true });
      fs.writeFileSync(
        path.join(repoPath, "config"),
        `[remote "origin"]
	url = https://github.com/owner/my-repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
      );

      // Without a real git binary, this won't work, so we expect an empty map
      // since execGitSync will fail
      const map = buildRepoCheckoutMap(testDir);

      // In a real git repo this would work, but in tests without git setup it's ok to be empty
      expect(map).toBeDefined();
    });
  });
});

describe("generate_git_patch multi-repo support", () => {
  describe("getPatchPathForBranchInRepo", () => {
    it("should include repo slug in path", () => {
      const filePath = getPatchPathForBranchInRepo("feature-branch", "owner/repo");
      expect(filePath).toBe("/tmp/gh-aw/aw-owner-repo-feature-branch.patch");
    });

    it("should sanitize repo slug", () => {
      const filePath = getPatchPathForBranchInRepo("main", "org/my-project");
      expect(filePath).toBe("/tmp/gh-aw/aw-org-my-project-main.patch");
    });

    it("should sanitize branch name", () => {
      const filePath = getPatchPathForBranchInRepo("feature/add-login", "owner/repo");
      expect(filePath).toBe("/tmp/gh-aw/aw-owner-repo-feature-add-login.patch");
    });

    it("should handle complex repo names", () => {
      const filePath = getPatchPathForBranchInRepo("main", "github/gh-aw");
      expect(filePath).toBe("/tmp/gh-aw/aw-github-gh-aw-main.patch");
    });

    it("should handle uppercase input", () => {
      const filePath = getPatchPathForBranchInRepo("Feature-Branch", "Owner/Repo");
      expect(filePath).toBe("/tmp/gh-aw/aw-owner-repo-feature-branch.patch");
    });
  });

  describe("sanitizeRepoSlugForPatch", () => {
    it("should replace slash with dash", () => {
      expect(sanitizeRepoSlugForPatch("owner/repo")).toBe("owner-repo");
    });

    it("should handle special characters", () => {
      expect(sanitizeRepoSlugForPatch("org:name/proj*test")).toBe("org-name-proj-test");
    });

    it("should collapse multiple dashes", () => {
      expect(sanitizeRepoSlugForPatch("org//repo")).toBe("org-repo");
    });

    it("should remove leading/trailing dashes", () => {
      expect(sanitizeRepoSlugForPatch("/owner/repo/")).toBe("owner-repo");
    });

    it("should convert to lowercase", () => {
      expect(sanitizeRepoSlugForPatch("Owner/Repo")).toBe("owner-repo");
    });

    it("should return empty string for null/undefined", () => {
      expect(sanitizeRepoSlugForPatch(null)).toBe("");
      expect(sanitizeRepoSlugForPatch(undefined)).toBe("");
      expect(sanitizeRepoSlugForPatch("")).toBe("");
    });
  });

  describe("sanitizeBranchNameForPatch", () => {
    it("should replace path separators with dashes", () => {
      expect(sanitizeBranchNameForPatch("feature/login")).toBe("feature-login");
      expect(sanitizeBranchNameForPatch("fix\\bug")).toBe("fix-bug");
    });

    it("should replace special characters", () => {
      expect(sanitizeBranchNameForPatch("feature:test")).toBe("feature-test");
      expect(sanitizeBranchNameForPatch("fix*bug")).toBe("fix-bug");
    });

    it("should collapse multiple dashes", () => {
      expect(sanitizeBranchNameForPatch("feature//login")).toBe("feature-login");
    });

    it("should remove leading/trailing dashes", () => {
      expect(sanitizeBranchNameForPatch("-feature-")).toBe("feature");
    });

    it("should convert to lowercase", () => {
      expect(sanitizeBranchNameForPatch("Feature-Branch")).toBe("feature-branch");
    });

    it("should handle empty/null input", () => {
      expect(sanitizeBranchNameForPatch("")).toBe("unknown");
      expect(sanitizeBranchNameForPatch(null)).toBe("unknown");
      expect(sanitizeBranchNameForPatch(undefined)).toBe("unknown");
    });

    it("should handle question marks and pipes", () => {
      expect(sanitizeBranchNameForPatch("branch?name|test")).toBe("branch-name-test");
    });

    it("should handle angle brackets", () => {
      expect(sanitizeBranchNameForPatch("branch<>name")).toBe("branch-name");
    });
  });
});
