import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { execSync } from "child_process";
import { createRequire } from "module";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";

const require = createRequire(import.meta.url);

describe("generateGitPatch", () => {
  let originalEnv;

  beforeEach(() => {
    // Save original environment
    originalEnv = {
      GITHUB_SHA: process.env.GITHUB_SHA,
      GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE,
      DEFAULT_BRANCH: process.env.DEFAULT_BRANCH,
      GH_AW_BASE_BRANCH: process.env.GH_AW_BASE_BRANCH,
    };
  });

  afterEach(() => {
    // Restore original environment
    Object.keys(originalEnv).forEach(key => {
      if (originalEnv[key] !== undefined) {
        process.env[key] = originalEnv[key];
      } else {
        delete process.env[key];
      }
    });
  });

  it("does not embed invalid base commit metadata", async () => {
    const { embedBaseCommit } = await import("./generate_git_patch.cjs");

    const patchContent = "From abc123 Mon Sep 17 00:00:00 2001\nFrom: Test\n";

    expect(embedBaseCommit(patchContent, "main\nX-Injected: true")).toBe(patchContent);
  });

  it("should return error when no commits can be found", async () => {
    delete process.env.GITHUB_SHA;
    process.env.GITHUB_WORKSPACE = "/tmp/test-repo";

    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    const result = await generateGitPatch(null, "main");

    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
  });

  it("should return success false when no commits found", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    // Set up environment but in a way that won't find commits
    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch("nonexistent-branch", "main");

    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
    expect(result).toHaveProperty("patchPath");
  });

  it("should create patch directory if it doesn't exist", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    // Even if it fails, it should try to create the directory
    const result = await generateGitPatch("test-branch", "main");

    expect(result).toHaveProperty("patchPath");
    // Patch path includes sanitized branch name
    expect(result.patchPath).toBe("/tmp/gh-aw/aw-test-branch.patch");
  });

  it("should return patch info structure", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch("test-branch", "main");

    expect(result).toHaveProperty("success");
    expect(result).toHaveProperty("patchPath");
    expect(typeof result.success).toBe("boolean");
  });

  it("should handle null branch name", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch(null, "main");

    expect(result).toHaveProperty("success");
    expect(result).toHaveProperty("patchPath");
  });

  it("should handle empty branch name", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch("", "main");

    expect(result).toHaveProperty("success");
    expect(result).toHaveProperty("patchPath");
  });

  it("should use provided base branch", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch("feature-branch", "develop");

    expect(result).toHaveProperty("success");
    // Should use develop as base branch
  });

  it("should use provided master branch as base", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch("feature-branch", "master");

    expect(result).toHaveProperty("success");
    // Should use master as base branch
  });

  it("should safely handle branch names with special characters", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    // Test with various special characters that could cause shell injection
    const maliciousBranchNames = ["feature; rm -rf /", "feature && echo hacked", "feature | cat /etc/passwd", "feature$(whoami)", "feature`whoami`", "feature\nrm -rf /"];

    for (const branchName of maliciousBranchNames) {
      const result = await generateGitPatch(branchName, "main");

      // Should not throw an error and should handle safely
      expect(result).toHaveProperty("success");
      expect(result.success).toBe(false);
      // Should fail gracefully without executing injected commands
    }
  });

  it("should safely handle GITHUB_SHA with special characters", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";

    // Test with malicious SHA that could cause shell injection
    process.env.GITHUB_SHA = "abc123; echo hacked";

    const result = await generateGitPatch("test-branch", "main");

    // Should not throw an error and should handle safely
    expect(result).toHaveProperty("success");
    expect(result.success).toBe(false);
  });
});

describe("generateGitPatch - cross-repo checkout scenarios", () => {
  let originalEnv;

  beforeEach(() => {
    // Save original environment
    originalEnv = {
      GITHUB_SHA: process.env.GITHUB_SHA,
      GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE,
      DEFAULT_BRANCH: process.env.DEFAULT_BRANCH,
      GH_AW_CUSTOM_BASE_BRANCH: process.env.GH_AW_CUSTOM_BASE_BRANCH,
    };
  });

  afterEach(() => {
    // Restore original environment
    Object.keys(originalEnv).forEach(key => {
      if (originalEnv[key] !== undefined) {
        process.env[key] = originalEnv[key];
      } else {
        delete process.env[key];
      }
    });
  });

  it("should handle GITHUB_SHA not existing in cross-repo checkout", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    // In cross-repo checkout, GITHUB_SHA is from the workflow repo,
    // not the target repo that's checked out
    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "deadbeef123456789"; // SHA that doesn't exist in target repo

    const result = await generateGitPatch("feature-branch", "main");

    // Should fail gracefully, not crash
    expect(result).toHaveProperty("success");
    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
  });

  it("should fall back gracefully when persist-credentials is false", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    // Simulate cross-repo checkout where fetch fails due to persist-credentials: false
    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch("feature-branch", "main");

    // Should try multiple strategies and fail gracefully
    expect(result).toHaveProperty("success");
    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
    expect(result).toHaveProperty("patchPath");
  });

  it("should check local refs before attempting network fetch", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    // This tests that Strategy 1 checks for local refs before fetching
    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";

    const result = await generateGitPatch("feature-branch", "main");

    // Should complete without hanging or crashing due to fetch attempts
    expect(result).toHaveProperty("success");
    expect(result).toHaveProperty("patchPath");
  });

  it("should return meaningful error for cross-repo scenarios", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "sha-from-workflow-repo";

    const result = await generateGitPatch("agent-created-branch", "main");

    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
    // Error should be informative
    expect(typeof result.error).toBe("string");
    expect(result.error.length).toBeGreaterThan(0);
  });

  it("should handle incremental mode failure in cross-repo checkout", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";

    // Incremental mode requires origin/branchName to exist - should fail clearly
    const result = await generateGitPatch("feature-branch", "main", { mode: "incremental" });

    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
    // Should indicate the branch doesn't exist or can't be fetched
    expect(result.error).toMatch(/branch|fetch|incremental/i);
  });

  it("should return actionable guidance when branch is missing in incremental mode", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";

    const result = await generateGitPatch("feature-branch", "main", { mode: "incremental" });

    expect(result.success).toBe(false);
    expect(result.error).toContain("wrong repository checkout");
    expect(result.error).toContain("GITHUB_WORKSPACE");
    expect(result.error).toContain("/tmp/nonexistent-repo");
  });

  it("should handle SideRepoOps pattern where workflow repo != target repo", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    // Simulates: workflow in org/side-repo, checkout of org/target-repo
    // GITHUB_SHA would be from side-repo, not target-repo
    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-target-repo";
    process.env.GITHUB_SHA = "side-repo-sha-not-in-target";

    const result = await generateGitPatch("agent-changes", "main");

    // Should not crash, should return failure with helpful error
    expect(result).toHaveProperty("success");
    expect(result.success).toBe(false);
    expect(result).toHaveProperty("patchPath");
  });
});

describe("generateGitPatch - workspacePath option", () => {
  let workspaceDir;
  let repoDir;
  let remoteDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE, GITHUB_SHA: process.env.GITHUB_SHA };
    global.core = { debug: () => {}, info: () => {}, warning: () => {}, error: () => {} };
    workspaceDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-workspace-"));
    repoDir = path.join(workspaceDir, "proxy-frontend");
    fs.mkdirSync(repoDir, { recursive: true });

    execSync("git init -b main", { cwd: repoDir, stdio: "pipe" });
    execSync('git config user.email "test@example.com"', { cwd: repoDir, stdio: "pipe" });
    execSync('git config user.name "Test"', { cwd: repoDir, stdio: "pipe" });

    fs.writeFileSync(path.join(repoDir, "README.md"), "# Repo\n");
    execSync("git add README.md", { cwd: repoDir, stdio: "pipe" });
    execSync('git commit -m "init"', { cwd: repoDir, stdio: "pipe" });

    execSync("git checkout -b feature/workspace-path", { cwd: repoDir, stdio: "pipe" });
    fs.writeFileSync(path.join(repoDir, "feature.txt"), "new change\n");
    execSync("git add feature.txt", { cwd: repoDir, stdio: "pipe" });
    execSync('git commit -m "feature"', { cwd: repoDir, stdio: "pipe" });

    remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-workspace-remote-"));
    execSync("git init --bare -b main", { cwd: remoteDir, stdio: "pipe" });
    execSync(`git remote add origin ${remoteDir}`, { cwd: repoDir, stdio: "pipe" });
    execSync("git checkout main", { cwd: repoDir, stdio: "pipe" });
    execSync("git push origin main", { cwd: repoDir, stdio: "pipe" });
    execSync("git checkout feature/workspace-path", { cwd: repoDir, stdio: "pipe" });

    process.env.GITHUB_WORKSPACE = workspaceDir;
    delete process.env.GITHUB_SHA;
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
  });

  afterEach(() => {
    Object.entries(originalEnv).forEach(([key, value]) => {
      if (value !== undefined) process.env[key] = value;
      else delete process.env[key];
    });
    if (workspaceDir && fs.existsSync(workspaceDir)) {
      fs.rmSync(workspaceDir, { recursive: true, force: true });
    }
    if (remoteDir && fs.existsSync(remoteDir)) {
      fs.rmSync(remoteDir, { recursive: true, force: true });
    }
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
    delete global.core;
  });

  it("should generate the patch from workspacePath when provided", async () => {
    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("feature/workspace-path", "main", { workspacePath: "proxy-frontend" });

    expect(result.success).toBe(true);
    const patch = fs.readFileSync(result.patchPath, "utf8");
    expect(patch).toContain("feature.txt");
  });

  it("should reject workspacePath that escapes GITHUB_WORKSPACE", async () => {
    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("feature/workspace-path", "main", { workspacePath: "../outside" });

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid workspacePath");
  });
});

describe("sanitizeBranchNameForPatch", () => {
  it("should sanitize branch names with path separators", async () => {
    const { sanitizeBranchNameForPatch } = await import("./generate_git_patch.cjs");

    expect(sanitizeBranchNameForPatch("feature/add-login")).toBe("feature-add-login");
    expect(sanitizeBranchNameForPatch("user\\branch")).toBe("user-branch");
  });

  it("should sanitize branch names with special characters", async () => {
    const { sanitizeBranchNameForPatch } = await import("./generate_git_patch.cjs");

    expect(sanitizeBranchNameForPatch("feature:test")).toBe("feature-test");
    expect(sanitizeBranchNameForPatch("branch*name")).toBe("branch-name");
    expect(sanitizeBranchNameForPatch('branch?"name')).toBe("branch-name");
    expect(sanitizeBranchNameForPatch("branch<>name")).toBe("branch-name");
    expect(sanitizeBranchNameForPatch("branch|name")).toBe("branch-name");
  });

  it("should collapse multiple dashes", async () => {
    const { sanitizeBranchNameForPatch } = await import("./generate_git_patch.cjs");

    expect(sanitizeBranchNameForPatch("feature//double")).toBe("feature-double");
    expect(sanitizeBranchNameForPatch("a---b")).toBe("a-b");
  });

  it("should remove leading and trailing dashes", async () => {
    const { sanitizeBranchNameForPatch } = await import("./generate_git_patch.cjs");

    expect(sanitizeBranchNameForPatch("/feature")).toBe("feature");
    expect(sanitizeBranchNameForPatch("feature/")).toBe("feature");
    expect(sanitizeBranchNameForPatch("/feature/")).toBe("feature");
  });

  it("should convert to lowercase", async () => {
    const { sanitizeBranchNameForPatch } = await import("./generate_git_patch.cjs");

    expect(sanitizeBranchNameForPatch("Feature-Branch")).toBe("feature-branch");
    expect(sanitizeBranchNameForPatch("UPPER")).toBe("upper");
  });

  it("should handle null and empty strings", async () => {
    const { sanitizeBranchNameForPatch } = await import("./generate_git_patch.cjs");

    expect(sanitizeBranchNameForPatch(null)).toBe("unknown");
    expect(sanitizeBranchNameForPatch("")).toBe("unknown");
    expect(sanitizeBranchNameForPatch(undefined)).toBe("unknown");
  });
});

describe("generateGitPatch - standardized error codes", () => {
  let originalEnv;

  beforeEach(() => {
    originalEnv = {
      GITHUB_SHA: process.env.GITHUB_SHA,
      GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE,
    };
  });

  afterEach(() => {
    Object.keys(originalEnv).forEach(key => {
      if (originalEnv[key] !== undefined) {
        process.env[key] = originalEnv[key];
      } else {
        delete process.env[key];
      }
    });
  });

  it("should fail gracefully and return a non-empty error string when no commits can be found", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = await generateGitPatch("feature-branch", "main");

    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
    // Note: E005 is used as an internal control-flow signal in Strategy 1 (full mode)
    // and is caught before reaching the final return value. The conformance check
    // validates the E005 code at source level via check-safe-outputs-conformance.sh.
    expect(typeof result.error).toBe("string");
    expect(result.error.length).toBeGreaterThan(0);
  });
});

describe("getPatchPathForBranch", () => {
  it("should return correct path format", async () => {
    const { getPatchPathForBranch } = await import("./generate_git_patch.cjs");

    expect(getPatchPathForBranch("feature-branch")).toBe("/tmp/gh-aw/aw-feature-branch.patch");
  });

  it("should sanitize branch name in path", async () => {
    const { getPatchPathForBranch } = await import("./generate_git_patch.cjs");

    expect(getPatchPathForBranch("feature/branch")).toBe("/tmp/gh-aw/aw-feature-branch.patch");
    expect(getPatchPathForBranch("Feature/BRANCH")).toBe("/tmp/gh-aw/aw-feature-branch.patch");
  });
});

// ──────────────────────────────────────────────────────
// excludedFiles option – end-to-end with a real git repo
// ──────────────────────────────────────────────────────

describe("generateGitPatch – excludedFiles option", () => {
  let repoDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE, GITHUB_SHA: process.env.GITHUB_SHA };

    // Set up the core global required by git_helpers.cjs
    global.core = { debug: () => {}, info: () => {}, warning: () => {}, error: () => {} };

    // Create an isolated git repo in a temp directory
    repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-test-"));
    execSync("git init -b main", { cwd: repoDir });
    execSync('git config user.email "test@example.com"', { cwd: repoDir });
    execSync('git config user.name "Test"', { cwd: repoDir });

    // Initial commit so the repo has a base
    fs.writeFileSync(path.join(repoDir, "README.md"), "# Repo\n");
    execSync("git add .", { cwd: repoDir });
    execSync('git commit -m "init"', { cwd: repoDir });

    // Record the initial commit SHA for GITHUB_SHA (Strategy 2 base)
    const sha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();
    process.env.GITHUB_SHA = sha;
    // Clear GITHUB_WORKSPACE so the cwd option is used instead
    delete process.env.GITHUB_WORKSPACE;

    // Reset module cache so each test gets a fresh module instance
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
  });

  afterEach(() => {
    // Restore env
    Object.entries(originalEnv).forEach(([k, v]) => {
      if (v !== undefined) process.env[k] = v;
      else delete process.env[k];
    });
    // Clean up temp repo
    if (repoDir && fs.existsSync(repoDir)) {
      fs.rmSync(repoDir, { recursive: true, force: true });
    }
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
    delete global.core;
  });

  function commitFiles(files) {
    for (const [filePath, content] of Object.entries(files)) {
      const abs = path.join(repoDir, filePath);
      fs.mkdirSync(path.dirname(abs), { recursive: true });
      fs.writeFileSync(abs, content);
    }
    execSync("git add .", { cwd: repoDir });
    execSync('git commit -m "add files"', { cwd: repoDir });
  }

  it("should include all files when excludedFiles is not set", async () => {
    commitFiles({
      "src/index.js": "console.log('hello');\n",
      "dist/bundle.js": "/* bundled */\n",
    });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch(null, "main", { cwd: repoDir });

    expect(result.success).toBe(true);
    const patch = fs.readFileSync(result.patchPath, "utf8");
    expect(patch).toContain("src/index.js");
    expect(patch).toContain("dist/bundle.js");
  });

  it("should exclude files matching excludedFiles patterns from the patch", async () => {
    commitFiles({
      "src/index.js": "console.log('hello');\n",
      "dist/bundle.js": "/* bundled */\n",
    });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch(null, "main", { cwd: repoDir, excludedFiles: ["dist/**"] });

    expect(result.success).toBe(true);
    const patch = fs.readFileSync(result.patchPath, "utf8");
    expect(patch).toContain("src/index.js");
    expect(patch).not.toContain("dist/bundle.js");
  });

  it("should return no patch when all files are ignored", async () => {
    commitFiles({
      "dist/bundle.js": "/* bundled */\n",
    });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch(null, "main", { cwd: repoDir, excludedFiles: ["dist/**"] });

    // All changes were excluded — patch is empty so generation reports no changes
    expect(result.success).toBe(false);
  });
});

// ──────────────────────────────────────────────────────────────────────────
// Full mode base ref selection — must use merge-base, never stale origin/<branch>
// Regression test for: create_pull_request patch uses stale origin/branchName
// instead of merge-base with default branch.
// ──────────────────────────────────────────────────────────────────────────

describe("generateGitPatch – full mode base ref (merge-base, not stale origin)", () => {
  let repoDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE, GITHUB_SHA: process.env.GITHUB_SHA };

    global.core = { debug: () => {}, info: () => {}, warning: () => {}, error: () => {} };

    repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-fullmode-"));
    execSync("git init -b main", { cwd: repoDir });
    execSync('git config user.email "test@example.com"', { cwd: repoDir });
    execSync('git config user.name "Test"', { cwd: repoDir });

    // Initial commit on main
    fs.writeFileSync(path.join(repoDir, "README.md"), "# Repo\n");
    execSync("git add .", { cwd: repoDir });
    execSync('git commit -m "init"', { cwd: repoDir });

    delete process.env.GITHUB_WORKSPACE;
    delete process.env.GITHUB_SHA;
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
  });

  afterEach(() => {
    Object.entries(originalEnv).forEach(([k, v]) => {
      if (v !== undefined) process.env[k] = v;
      else delete process.env[k];
    });
    if (repoDir && fs.existsSync(repoDir)) {
      fs.rmSync(repoDir, { recursive: true, force: true });
    }
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
    delete global.core;
  });

  it("should NOT include phantom commits when stale origin/<branch> exists (regression test)", async () => {
    // Reproduce the stale-remote-tracking-ref bug scenario:
    //   1. A feature branch was pushed in the past at some old commit
    //      (origin/feature-branch points there — this is the "stale" remote-tracking ref)
    //   2. main has since advanced with many "phantom" commits the agent never made
    //   3. The agent fast-forwards the local branch to main, then makes one new commit
    //   4. Full-mode patch (create_pull_request) must contain ONLY the agent's commit,
    //      NOT the phantom commits between the old branch tip and main.

    // Set up a "remote" repo to act as origin
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-fullmode-remote-"));
    try {
      execSync("git init --bare -b main", { cwd: remoteDir });
      execSync(`git remote add origin ${remoteDir}`, { cwd: repoDir });
      execSync("git push origin main", { cwd: repoDir });

      // Step 1: Create the feature branch at the initial commit and push it.
      // This becomes the "old" position of origin/feature-branch.
      execSync("git checkout -b feature-branch", { cwd: repoDir });
      execSync("git push origin feature-branch", { cwd: repoDir });
      // Explicitly fetch to ensure refs/remotes/origin/feature-branch exists in the
      // local repo. `git push` typically updates this automatically, but in repos
      // created via `git init` + `git remote add` (no clone) the remote-tracking
      // ref population can be inconsistent across git versions, so be explicit.
      execSync("git fetch origin feature-branch:refs/remotes/origin/feature-branch", { cwd: repoDir });
      const oldBranchSha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();

      // Step 2: main advances with phantom commits the agent will not make
      execSync("git checkout main", { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "phantom1.md"), "# phantom 1\n");
      execSync("git add phantom1.md", { cwd: repoDir });
      execSync('git commit -m "phantom commit 1"', { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "phantom2.md"), "# phantom 2\n");
      execSync("git add phantom2.md", { cwd: repoDir });
      execSync('git commit -m "phantom commit 2"', { cwd: repoDir });
      execSync("git push origin main", { cwd: repoDir });

      // Step 3: Simulate the agent run — fast-forward the local feature branch to
      // main, then make one new commit. Note that origin/feature-branch is still
      // pointing at oldBranchSha (we deliberately do NOT push the branch update).
      execSync("git checkout feature-branch", { cwd: repoDir });
      execSync("git reset --hard main", { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "agent-change.txt"), "the only real change\n");
      execSync("git add agent-change.txt", { cwd: repoDir });
      execSync('git commit -m "agent change"', { cwd: repoDir });

      // Sanity check: origin/feature-branch is still stale (points to oldBranchSha)
      const remoteBranchSha = execSync("git rev-parse origin/feature-branch", { cwd: repoDir }).toString().trim();
      expect(remoteBranchSha).toBe(oldBranchSha);

      // Step 4: Generate the full-mode patch
      const { generateGitPatch } = require("./generate_git_patch.cjs");
      const result = await generateGitPatch("feature-branch", "main", { cwd: repoDir, mode: "full" });

      expect(result.success).toBe(true);
      const patch = fs.readFileSync(result.patchPath, "utf8");

      // The patch MUST contain the agent's commit
      expect(patch).toContain("agent-change.txt");
      // The patch MUST NOT contain the phantom commits from main
      expect(patch).not.toContain("phantom1.md");
      expect(patch).not.toContain("phantom2.md");
    } finally {
      if (fs.existsSync(remoteDir)) {
        fs.rmSync(remoteDir, { recursive: true, force: true });
      }
    }
  });

  it("should fall back to local base branch when origin/<base> is unavailable", async () => {
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-fullmode-remote-empty-"));
    try {
      execSync("git init --bare -b main", { cwd: remoteDir });
      execSync(`git remote add origin ${remoteDir}`, { cwd: repoDir });

      execSync("git checkout -b feature/local-fallback", { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "feature.txt"), "feature-only\n");
      execSync("git add feature.txt", { cwd: repoDir });
      execSync('git commit -m "feature commit"', { cwd: repoDir });

      // Ensure origin/main is not present so Strategy 1 must use local `main`.
      expect(() => execSync("git rev-parse --verify refs/remotes/origin/main", { cwd: repoDir, stdio: "pipe" })).toThrow();

      const { generateGitPatch } = require("./generate_git_patch.cjs");
      const result = await generateGitPatch("feature/local-fallback", "main", { cwd: repoDir, mode: "full" });

      expect(result.success).toBe(true);
      const patch = fs.readFileSync(result.patchPath, "utf8");
      expect(patch).toContain("feature.txt");
    } finally {
      if (fs.existsSync(remoteDir)) {
        fs.rmSync(remoteDir, { recursive: true, force: true });
      }
    }
  });
});

describe("generateGitPatch – shallow clone merge-base error surfaces to caller", () => {
  let repoDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE, GITHUB_SHA: process.env.GITHUB_SHA };
    global.core = { debug: () => {}, info: () => {}, warning: () => {}, error: () => {} };

    repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-shallow-"));
    execSync("git init -b main", { cwd: repoDir });
    execSync('git config user.email "test@example.com"', { cwd: repoDir });
    execSync('git config user.name "Test"', { cwd: repoDir });

    delete process.env.GITHUB_WORKSPACE;
    delete process.env.GITHUB_SHA;
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
  });

  afterEach(() => {
    Object.entries(originalEnv).forEach(([k, v]) => {
      if (v !== undefined) process.env[k] = v;
      else delete process.env[k];
    });
    if (repoDir && fs.existsSync(repoDir)) {
      fs.rmSync(repoDir, { recursive: true, force: true });
    }
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
    delete global.core;
  });

  it("should return a shallow-clone diagnostic (not a generic error) when merge-base fails due to shallow clone", async () => {
    // Set up remote + local repo
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-shallow-remote-"));
    try {
      execSync("git init --bare -b main", { cwd: remoteDir });
      execSync(`git remote add origin ${remoteDir}`, { cwd: repoDir });

      // Commit several times on main so there is depth
      for (let i = 0; i < 5; i++) {
        fs.writeFileSync(path.join(repoDir, `commit${i}.txt`), `content ${i}\n`);
        execSync("git add .", { cwd: repoDir });
        execSync(`git commit -m "main commit ${i}"`, { cwd: repoDir });
      }
      execSync("git push origin main", { cwd: repoDir });

      // Create feature branch
      execSync("git checkout -b feature", { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "feature.txt"), "feature\n");
      execSync("git add .", { cwd: repoDir });
      execSync('git commit -m "feature commit"', { cwd: repoDir });

      // Simulate a shallow clone by creating a .git/shallow file that grafts
      // history so merge-base cannot be resolved
      const tipSha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();
      fs.writeFileSync(path.join(repoDir, ".git", "shallow"), `${tipSha}\n`);

      // Also set up origin/main as if it were fetched
      execSync("git fetch origin main:refs/remotes/origin/main", { cwd: repoDir });

      const { generateGitPatch } = require("./generate_git_patch.cjs");
      const result = await generateGitPatch("feature", "main", { cwd: repoDir, mode: "full" });

      // The error must surface to the caller with the shallow-clone explanation
      expect(result.success).toBe(false);
      expect(result.error).toMatch(/shallow clone/i);
      expect(result.error).toMatch(/fetch-depth.*0|deepen/i);
    } finally {
      if (fs.existsSync(remoteDir)) {
        fs.rmSync(remoteDir, { recursive: true, force: true });
      }
    }
  });
});

describe("generateGitPatch – Strategy 3 picks closest remote merge-base", () => {
  let repoDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE, GITHUB_SHA: process.env.GITHUB_SHA };

    global.core = { debug: () => {}, info: () => {}, warning: () => {}, error: () => {} };

    repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-strategy3-"));
    execSync("git init -b master", { cwd: repoDir });
    execSync('git config user.email "test@example.com"', { cwd: repoDir });
    execSync('git config user.name "Test"', { cwd: repoDir });

    fs.writeFileSync(path.join(repoDir, "README.md"), "# Repo\n");
    execSync("git add .", { cwd: repoDir });
    execSync('git commit -m "init"', { cwd: repoDir });

    delete process.env.GITHUB_WORKSPACE;
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
  });

  afterEach(() => {
    Object.entries(originalEnv).forEach(([k, v]) => {
      if (v !== undefined) process.env[k] = v;
      else delete process.env[k];
    });
    if (repoDir && fs.existsSync(repoDir)) {
      fs.rmSync(repoDir, { recursive: true, force: true });
    }
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
    delete global.core;
  });

  it("should avoid stale lexicographically-first remote refs when choosing Strategy 3 base", async () => {
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-strategy3-remote-"));
    try {
      execSync("git init --bare -b master", { cwd: remoteDir });
      execSync(`git remote add origin ${remoteDir}`, { cwd: repoDir });
      execSync("git push origin master", { cwd: repoDir });

      // Create a stale remote branch that will sort before origin/master.
      execSync("git checkout -b aaa-stale", { cwd: repoDir });
      execSync("git push origin aaa-stale", { cwd: repoDir });
      execSync("git fetch origin aaa-stale:refs/remotes/origin/aaa-stale", { cwd: repoDir });

      // Advance master with phantom commits.
      execSync("git checkout master", { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "phantom1.md"), "# phantom 1\n");
      execSync("git add phantom1.md", { cwd: repoDir });
      execSync('git commit -m "phantom commit 1"', { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "phantom2.md"), "# phantom 2\n");
      execSync("git add phantom2.md", { cwd: repoDir });
      execSync('git commit -m "phantom commit 2"', { cwd: repoDir });
      execSync("git push origin master", { cwd: repoDir });
      execSync("git fetch origin master:refs/remotes/origin/master", { cwd: repoDir });
      const masterTip = execSync("git rev-parse master", { cwd: repoDir }).toString().trim();

      // Agent branch has one new commit on top of master.
      execSync("git checkout -b agent-branch", { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "agent-change.txt"), "the only real change\n");
      execSync("git add agent-change.txt", { cwd: repoDir });
      execSync('git commit -m "agent change"', { cwd: repoDir });

      // Ensure origin/HEAD doesn't influence lexical ordering in this regression test.
      try {
        execSync("git update-ref -d refs/remotes/origin/HEAD", { cwd: repoDir });
      } catch {
        // origin/HEAD may not exist in repos created via git init + remote add;
        // absence is expected and safe to ignore for this ordering-focused test.
      }

      // Force Strategy 1 to produce no patch (base_branch == branch => no new commits).
      // Force Strategy 2 to fail (GITHUB_SHA does not exist in this repo).
      // Strategy 3 is therefore the only path that can produce a non-empty patch.
      process.env.GITHUB_SHA = "side-repo-sha-not-in-target-repo";
      const { generateGitPatch } = require("./generate_git_patch.cjs");
      const result = await generateGitPatch("agent-branch", "agent-branch", { cwd: repoDir, mode: "full" });

      expect(result.success).toBe(true);
      expect(result.baseCommit).toBe(masterTip);
      const patch = fs.readFileSync(result.patchPath, "utf8");
      expect(patch).toContain("agent-change.txt");
      expect(patch).not.toContain("phantom1.md");
      expect(patch).not.toContain("phantom2.md");
    } finally {
      if (fs.existsSync(remoteDir)) {
        fs.rmSync(remoteDir, { recursive: true, force: true });
      }
    }
  });

  it("reports filtered Strategy 3 synthesis failures instead of a generic no-changes error", async () => {
    const base = execSync("git rev-parse HEAD", { cwd: repoDir, encoding: "utf8" }).trim();
    execSync(`git update-ref refs/remotes/origin/secondary ${base}`, { cwd: repoDir });
    execSync("git checkout -b agent-branch", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "agent.txt"), "retained source\n");
    execSync("git add . && git commit -m 'agent change'", { cwd: repoDir });
    process.env.GITHUB_SHA = "side-repo-sha-not-in-target-repo";

    const gitHelpers = require("./git_helpers.cjs");
    const originalExecGitSync = gitHelpers.execGitSync;
    gitHelpers.execGitSync = (args, options) => {
      if (args[0] === "write-tree") throw new Error("synthetic tree creation failed");
      return originalExecGitSync(args, options);
    };
    try {
      const { generateGitPatch } = require("./generate_git_patch.cjs");
      const result = await generateGitPatch("agent-branch", "missing-default", { cwd: repoDir, excludedFiles: ["*.yml"] });
      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to generate filtered patch: synthetic tree creation failed");
    } finally {
      gitHelpers.execGitSync = originalExecGitSync;
    }
  });
});

// ──────────────────────────────────────────────────────────────────────────
// Incremental mode diffSize — must not inflate when agent merges base branch
//
// Regression test for: push_to_pull_request_branch diffSize is inflated when
// the agent does `git merge origin/<baseBranch>` to resolve stale-PR conflicts.
// Without the fix, git diff origin/<prBranch>..localBranch includes all upstream
// commits merged from the base branch, producing an artificially large diffSize
// that fails max_patch_size validation even when the agent's own changes are tiny.
// ──────────────────────────────────────────────────────────────────────────

describe("generateGitPatch – incremental mode diffSize excludes merged base-branch commits", () => {
  let repoDir;
  let remoteDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE, GITHUB_SHA: process.env.GITHUB_SHA };

    global.core = { debug: () => {}, info: () => {}, warning: () => {}, error: () => {} };

    repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-incremental-merge-"));
    remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-patch-incremental-merge-remote-"));

    execSync("git init --bare -b main", { cwd: remoteDir });
    execSync("git init -b main", { cwd: repoDir });
    execSync('git config user.email "test@example.com"', { cwd: repoDir });
    execSync('git config user.name "Test"', { cwd: repoDir });
    execSync(`git remote add origin ${remoteDir}`, { cwd: repoDir });

    // Initial commit on main
    fs.writeFileSync(path.join(repoDir, "README.md"), "# Repo\n");
    execSync("git add .", { cwd: repoDir });
    execSync('git commit -m "init"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    delete process.env.GITHUB_WORKSPACE;
    delete process.env.GITHUB_SHA;
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
  });

  afterEach(() => {
    Object.entries(originalEnv).forEach(([k, v]) => {
      if (v !== undefined) process.env[k] = v;
      else delete process.env[k];
    });
    if (repoDir && fs.existsSync(repoDir)) {
      fs.rmSync(repoDir, { recursive: true, force: true });
    }
    if (remoteDir && fs.existsSync(remoteDir)) {
      fs.rmSync(remoteDir, { recursive: true, force: true });
    }
    delete require.cache[require.resolve("./generate_git_patch.cjs")];
    delete global.core;
  });

  it("should not inflate diffSize when agent merges base branch into a stale PR branch", async () => {
    // Reproduce the scenario from the issue:
    //   1. PR branch is created and pushed (P2 = PR head on GitHub)
    //   2. main advances with many upstream commits (M1..M480)
    //   3. Agent checks out PR branch, does `git merge origin/main` (M = merge commit)
    //   4. Agent makes their own small change (C1)
    //   5. diffSize should only measure C1, NOT the 480 merged upstream commits

    // Step 1: Create PR branch with a small change
    execSync("git checkout -b pr-branch", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "pr-file.txt"), "PR initial change\n");
    execSync("git add pr-file.txt", { cwd: repoDir });
    execSync('git commit -m "PR: initial change"', { cwd: repoDir });
    execSync("git push origin pr-branch", { cwd: repoDir });
    execSync("git fetch origin pr-branch:refs/remotes/origin/pr-branch", { cwd: repoDir });
    const prHeadSha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();

    // Step 2: main advances with many upstream commits (simulating 480 commits).
    // We use a large file to make the upstream changes substantial in size.
    const UPSTREAM_CONTENT_SIZE_BYTES = 50 * 1024; // 50 KB: much larger than agent's tiny change
    const ADDITIONAL_UPSTREAM_COMMITS = 5; // several commits to simulate multi-commit upstream advance
    execSync("git checkout main", { cwd: repoDir });
    const bigContent = "x".repeat(UPSTREAM_CONTENT_SIZE_BYTES);
    fs.writeFileSync(path.join(repoDir, "upstream-big.txt"), bigContent);
    execSync("git add upstream-big.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: large change"', { cwd: repoDir });
    // Add more commits to simulate the 480-commit scenario
    for (let i = 1; i <= ADDITIONAL_UPSTREAM_COMMITS; i++) {
      fs.writeFileSync(path.join(repoDir, `upstream-${i}.txt`), `upstream commit ${i}\n`);
      execSync("git add .", { cwd: repoDir });
      execSync(`git commit -m "upstream: commit ${i}"`, { cwd: repoDir });
    }
    execSync("git push origin main", { cwd: repoDir });

    // Step 3: Agent checks out pr-branch and merges origin/main (simulating conflict resolution)
    execSync("git checkout pr-branch", { cwd: repoDir });
    execSync("git fetch origin main", { cwd: repoDir });
    execSync("git merge origin/main --no-edit", { cwd: repoDir });

    // Step 4: Agent makes their own small change (the actual contribution)
    fs.writeFileSync(path.join(repoDir, "agent-fix.txt"), "small fix by agent\n");
    execSync("git add agent-fix.txt", { cwd: repoDir });
    execSync('git commit -m "agent: small fix"', { cwd: repoDir });

    // Verify setup: origin/pr-branch still points to the old PR head (P2)
    const remotePrHead = execSync("git rev-parse refs/remotes/origin/pr-branch", { cwd: repoDir }).toString().trim();
    expect(remotePrHead).toBe(prHeadSha);

    // Generate the incremental patch
    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("pr-branch", "main", { cwd: repoDir, mode: "incremental" });

    expect(result.success).toBe(true);

    // The patch file itself will include the merge commit + upstream commits (transport artifact).
    // But diffSize should only reflect the agent's actual contribution (agent-fix.txt),
    // NOT the large upstream-big.txt and upstream-*.txt files that were merged in.
    expect(typeof result.diffSize).toBe("number");

    // The agent only changed agent-fix.txt (~18 bytes). The upstream changes are 50+ KB.
    // Without the fix, diffSize would be > 50 KB (inflated by upstream-big.txt).
    // With the fix, diffSize should be < 1 KB (just agent-fix.txt).
    const diffSizeKb = (result.diffSize ?? 0) / 1024;
    expect(diffSizeKb).toBeLessThan(1); // Agent's change is tiny

    // Sanity check: the patch itself DOES include the merged upstream content
    // (the transport includes all commits for git-am/bundle to work),
    // but the SIZE CHECK uses the smaller diffSize, not the patch file size.
    const patchSizeKb = (result.patchSize ?? 0) / 1024;
    expect(patchSizeKb).toBeGreaterThan(1); // Transport patch includes upstream commits
  });

  it("should preserve correct diffSize when agent does NOT merge base branch", async () => {
    // Verify that the fix does not regress the normal incremental case where
    // the agent simply adds commits on top of the existing PR branch without
    // merging the base branch.

    // Step 1: Create PR branch
    execSync("git checkout -b pr-no-merge", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "pr-file.txt"), "PR initial\n");
    execSync("git add pr-file.txt", { cwd: repoDir });
    execSync('git commit -m "PR: initial"', { cwd: repoDir });
    execSync("git push origin pr-no-merge", { cwd: repoDir });
    execSync("git fetch origin pr-no-merge:refs/remotes/origin/pr-no-merge", { cwd: repoDir });

    // Step 2: main advances (agent does NOT merge it)
    execSync("git checkout main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "upstream.txt"), "x".repeat(10 * 1024));
    execSync("git add upstream.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: change"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    // Step 3: Agent adds commits on top of PR branch without merging main
    execSync("git checkout pr-no-merge", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "agent-change.txt"), "agent contribution\n");
    execSync("git add agent-change.txt", { cwd: repoDir });
    execSync('git commit -m "agent: change"', { cwd: repoDir });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("pr-no-merge", "main", { cwd: repoDir, mode: "incremental" });

    expect(result.success).toBe(true);
    expect(typeof result.diffSize).toBe("number");

    // diffSize should reflect only the agent's change (< 1 KB), not upstream.txt (10 KB).
    const diffSizeKb = (result.diffSize ?? 0) / 1024;
    expect(diffSizeKb).toBeLessThan(1);
  });

  it("should skip merge-base adjustment and use original base when history was rewritten (rebase)", async () => {
    // Regression guard: when the agent rebases the PR branch (rewriting history),
    // baseCommitSha (origin/pr-branch) is NOT an ancestor of the local branch tip.
    // The merge-base adjustment must be skipped so we don't undercount the diff.

    // Step 1: Create PR branch with an initial change
    execSync("git checkout -b pr-rebase", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "pr-file.txt"), "PR change\n");
    execSync("git add pr-file.txt", { cwd: repoDir });
    execSync('git commit -m "PR: initial change"', { cwd: repoDir });
    execSync("git push origin pr-rebase", { cwd: repoDir });
    execSync("git fetch origin pr-rebase:refs/remotes/origin/pr-rebase", { cwd: repoDir });
    const prHeadSha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();

    // Step 2: main advances
    execSync("git checkout main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "upstream.txt"), "upstream content\n");
    execSync("git add upstream.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: advance"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    // Step 3: Agent rebases the PR branch on top of the new main (rewrites history)
    execSync("git checkout pr-rebase", { cwd: repoDir });
    execSync("git fetch origin main", { cwd: repoDir });
    execSync("git rebase origin/main", { cwd: repoDir });

    // Verify: origin/pr-rebase is NOT an ancestor of the rebased local tip
    const localHead = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();
    expect(localHead).not.toBe(prHeadSha);
    expect(() => execSync(`git merge-base --is-ancestor ${prHeadSha} HEAD`, { cwd: repoDir, stdio: "pipe" })).toThrow();

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("pr-rebase", "main", { cwd: repoDir, mode: "incremental" });

    // The patch succeeds (the transport patch covers the range)
    expect(result.success).toBe(true);
    // diffSize is present (computed without the merge-base adjustment)
    expect(typeof result.diffSize).toBe("number");
    // The rebase preserved pr-file.txt — diffSize should include it (agent's change > 0)
    expect(result.diffSize ?? 0).toBeGreaterThan(0);
  });

  it("should not inflate diffSize when agent merges base branch multiple times", async () => {
    // Corner case: agent merges origin/main twice (e.g. first to resolve conflicts,
    // then again after main advances further). The merge-base adjustment must still
    // identify the latest merge-base as the diff base, keeping diffSize small.

    // Step 1: Create PR branch with an initial agent commit
    execSync("git checkout -b pr-multi-merge", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "pr-file.txt"), "PR initial\n");
    execSync("git add pr-file.txt", { cwd: repoDir });
    execSync('git commit -m "PR: initial"', { cwd: repoDir });
    execSync("git push origin pr-multi-merge", { cwd: repoDir });
    execSync("git fetch origin pr-multi-merge:refs/remotes/origin/pr-multi-merge", { cwd: repoDir });

    const UPSTREAM_SIZE = 20 * 1024; // 20 KB each batch

    // Step 2: First upstream batch — main advances with large content
    execSync("git checkout main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "upstream-batch1.txt"), "a".repeat(UPSTREAM_SIZE));
    execSync("git add upstream-batch1.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: batch 1"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    // Step 3: Agent does first merge
    execSync("git checkout pr-multi-merge", { cwd: repoDir });
    execSync("git fetch origin main", { cwd: repoDir });
    execSync("git merge origin/main --no-edit", { cwd: repoDir });

    // Step 4: Agent makes a tiny change between the two merges
    fs.writeFileSync(path.join(repoDir, "agent-mid.txt"), "mid change\n");
    execSync("git add agent-mid.txt", { cwd: repoDir });
    execSync('git commit -m "agent: mid change"', { cwd: repoDir });

    // Step 5: Second upstream batch — main advances again with more large content
    execSync("git checkout main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "upstream-batch2.txt"), "b".repeat(UPSTREAM_SIZE));
    execSync("git add upstream-batch2.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: batch 2"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    // Step 6: Agent does second merge
    execSync("git checkout pr-multi-merge", { cwd: repoDir });
    execSync("git fetch origin main", { cwd: repoDir });
    execSync("git merge origin/main --no-edit", { cwd: repoDir });

    // Step 7: Agent makes final tiny change
    fs.writeFileSync(path.join(repoDir, "agent-final.txt"), "final change\n");
    execSync("git add agent-final.txt", { cwd: repoDir });
    execSync('git commit -m "agent: final change"', { cwd: repoDir });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("pr-multi-merge", "main", { cwd: repoDir, mode: "incremental" });

    expect(result.success).toBe(true);
    expect(typeof result.diffSize).toBe("number");

    // diffSize should only reflect the tiny agent files (pr-file.txt, agent-mid.txt,
    // agent-final.txt), NOT the two 20 KB upstream batches (40 KB total).
    const diffSizeKb = (result.diffSize ?? 0) / 1024;
    expect(diffSizeKb).toBeLessThan(2); // Agent's contribution is < 2 KB

    // The transport patch includes all commits (merges + upstream), so patchSize > diffSize
    const patchSizeKb = (result.patchSize ?? 0) / 1024;
    expect(patchSizeKb).toBeGreaterThan(diffSizeKb);
  });

  it("should include large agent contribution in diffSize even when base branch was merged", async () => {
    // Corner case: the fix must not under-count when the agent makes substantial changes.
    // A large agent contribution (> 10 KB) must appear in diffSize even though upstream
    // was also merged.

    const AGENT_FILE_SIZE = 15 * 1024; // 15 KB agent change
    const UPSTREAM_SIZE = 20 * 1024; // 20 KB upstream (should NOT be counted)

    // Step 1: Create PR branch
    execSync("git checkout -b pr-large-agent", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "large-agent-file.txt"), "A".repeat(AGENT_FILE_SIZE));
    execSync("git add large-agent-file.txt", { cwd: repoDir });
    execSync('git commit -m "agent: large change"', { cwd: repoDir });
    execSync("git push origin pr-large-agent", { cwd: repoDir });
    execSync("git fetch origin pr-large-agent:refs/remotes/origin/pr-large-agent", { cwd: repoDir });

    // Step 2: main advances with large content
    execSync("git checkout main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "upstream-large.txt"), "B".repeat(UPSTREAM_SIZE));
    execSync("git add upstream-large.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: large change"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    // Step 3: Agent merges origin/main then adds another large commit
    execSync("git checkout pr-large-agent", { cwd: repoDir });
    execSync("git fetch origin main", { cwd: repoDir });
    execSync("git merge origin/main --no-edit", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "large-agent-file2.txt"), "C".repeat(AGENT_FILE_SIZE));
    execSync("git add large-agent-file2.txt", { cwd: repoDir });
    execSync('git commit -m "agent: second large change"', { cwd: repoDir });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("pr-large-agent", "main", { cwd: repoDir, mode: "incremental" });

    expect(result.success).toBe(true);
    expect(typeof result.diffSize).toBe("number");

    // diffSize must include both agent files (2 × 15 KB = 30 KB) but NOT upstream-large.txt (20 KB).
    // The total agent contribution is well above 20 KB.
    const diffSizeKb = (result.diffSize ?? 0) / 1024;
    expect(diffSizeKb).toBeGreaterThan(20); // Both large agent files should be counted
    expect(diffSizeKb).toBeLessThan(50); // Upstream 20 KB should NOT be included
  });

  it("should produce small diffSize when agent only merges base branch without extra commits", async () => {
    // Corner case: agent's only action is to merge origin/main to update the PR branch.
    // No additional commits are added. diffSize should reflect only the PR-specific
    // content (pr-file.txt), not the upstream content that was merged in.

    // Step 1: Create PR branch with one small file
    execSync("git checkout -b pr-merge-only", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "pr-file.txt"), "PR-only content\n");
    execSync("git add pr-file.txt", { cwd: repoDir });
    execSync('git commit -m "PR: initial"', { cwd: repoDir });
    execSync("git push origin pr-merge-only", { cwd: repoDir });
    execSync("git fetch origin pr-merge-only:refs/remotes/origin/pr-merge-only", { cwd: repoDir });

    // Step 2: main advances with large content
    const UPSTREAM_SIZE = 30 * 1024; // 30 KB
    execSync("git checkout main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "upstream-only.txt"), "U".repeat(UPSTREAM_SIZE));
    execSync("git add upstream-only.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: large change"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    // Step 3: Agent ONLY merges origin/main — no extra commits
    execSync("git checkout pr-merge-only", { cwd: repoDir });
    execSync("git fetch origin main", { cwd: repoDir });
    execSync("git merge origin/main --no-edit", { cwd: repoDir });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("pr-merge-only", "main", { cwd: repoDir, mode: "incremental" });

    expect(result.success).toBe(true);
    expect(typeof result.diffSize).toBe("number");

    // diffSize: git diff origin/main..HEAD = only pr-file.txt ("PR-only content\n") visible
    // from main's perspective. upstream-only.txt was added by main and is also in HEAD,
    // so it cancels out in the diff — only PR-specific changes remain.
    // Either way, diffSize should be much smaller than the 30 KB upstream content.
    const diffSizeKb = (result.diffSize ?? 0) / 1024;
    expect(diffSizeKb).toBeLessThan(5); // Only PR-specific content remains in diff
  });

  it("should fall back gracefully when origin/main remote ref is not available", async () => {
    // Corner case: origin/<defaultBranch> was never fetched (or was pruned), so the
    // merge-base adjustment cannot run. The function must still succeed and report
    // diffSize using the original baseCommitSha — even if diffSize is inflated.

    // Step 1: Create PR branch
    execSync("git checkout -b pr-no-remote-main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "pr-file.txt"), "PR content\n");
    execSync("git add pr-file.txt", { cwd: repoDir });
    execSync('git commit -m "PR: initial"', { cwd: repoDir });
    execSync("git push origin pr-no-remote-main", { cwd: repoDir });
    execSync("git fetch origin pr-no-remote-main:refs/remotes/origin/pr-no-remote-main", { cwd: repoDir });

    // Step 2: main advances
    execSync("git checkout main", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "upstream.txt"), "upstream content\n");
    execSync("git add upstream.txt", { cwd: repoDir });
    execSync('git commit -m "upstream: change"', { cwd: repoDir });
    execSync("git push origin main", { cwd: repoDir });

    // Step 3: Agent fetches and merges, then adds a commit
    execSync("git checkout pr-no-remote-main", { cwd: repoDir });
    execSync("git fetch origin main", { cwd: repoDir });
    execSync("git merge origin/main --no-edit", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "agent-change.txt"), "agent\n");
    execSync("git add agent-change.txt", { cwd: repoDir });
    execSync('git commit -m "agent: change"', { cwd: repoDir });

    // Step 4: Remove the locally cached origin/main ref to simulate it being unavailable
    execSync("git update-ref -d refs/remotes/origin/main", { cwd: repoDir });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("pr-no-remote-main", "main", { cwd: repoDir, mode: "incremental" });

    // Must succeed — the fallback to baseCommitSha must not crash
    expect(result.success).toBe(true);
    expect(typeof result.diffSize).toBe("number");
    // diffSize > 0: the fallback base (origin/pr-no-remote-main) is ancestor of HEAD,
    // so at least the agent commits are included
    expect(result.diffSize ?? 0).toBeGreaterThan(0);
  });

  it("uses explicit PR-head baseline when the fork branch has no origin/<branch> ref", async () => {
    // Simulate checkout_pr_branch.cjs for a fork PR: refs/pull/N/head was fetched
    // to origin/pr-head, then checked out as a local branch named after head.ref.
    execSync("git checkout -b feature/fork-only", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "fork-file.txt"), "fork PR content\n");
    execSync("git add fork-file.txt", { cwd: repoDir });
    execSync('git commit -m "fork PR head"', { cwd: repoDir });
    const prHeadSha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();
    execSync(`git update-ref refs/remotes/origin/pr-head ${prHeadSha}`, { cwd: repoDir });

    fs.writeFileSync(path.join(repoDir, "agent-change.txt"), "agent follow-up\n");
    execSync("git add agent-change.txt", { cwd: repoDir });
    execSync('git commit -m "agent follow-up"', { cwd: repoDir });

    expect(() => execSync("git rev-parse --verify refs/remotes/origin/feature/fork-only", { cwd: repoDir, stdio: "pipe" })).toThrow();

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("feature/fork-only", "main", {
      cwd: repoDir,
      mode: "incremental",
      incrementalBaseRef: "refs/remotes/origin/pr-head",
      incrementalBaseSha: prHeadSha,
    });

    expect(result.success).toBe(true);
    expect(result.baseCommit).toBe(prHeadSha);
    const patch = fs.readFileSync(result.patchPath, "utf8");
    expect(patch).toContain("agent-change.txt");
    expect(patch).not.toContain("fork-file.txt");
  });

  it("prefers explicit PR-head baseline over same-named origin branch from base repo", async () => {
    // A base repository can have a branch with the same name as a fork PR head.
    // Incremental patch generation must not use that origin/<branch> ref because
    // it belongs to the base repo, not the fork PR.
    execSync("git checkout -b feature/native_versions", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "base-same-name.txt"), "base repo branch content\n");
    execSync("git add base-same-name.txt", { cwd: repoDir });
    execSync('git commit -m "base repo same-named branch"', { cwd: repoDir });
    const baseRepoSameNameSha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();
    execSync(`git update-ref refs/remotes/origin/feature/native_versions ${baseRepoSameNameSha}`, { cwd: repoDir });

    execSync("git checkout main", { cwd: repoDir });
    execSync("git checkout -B feature/native_versions", { cwd: repoDir });
    fs.writeFileSync(path.join(repoDir, "fork-file.txt"), "fork PR content\n");
    execSync("git add fork-file.txt", { cwd: repoDir });
    execSync('git commit -m "fork PR head"', { cwd: repoDir });
    const forkPrHeadSha = execSync("git rev-parse HEAD", { cwd: repoDir }).toString().trim();
    execSync(`git update-ref refs/remotes/origin/pr-head ${forkPrHeadSha}`, { cwd: repoDir });

    fs.writeFileSync(path.join(repoDir, "agent-change.txt"), "agent follow-up\n");
    execSync("git add agent-change.txt", { cwd: repoDir });
    execSync('git commit -m "agent follow-up"', { cwd: repoDir });

    const { generateGitPatch } = require("./generate_git_patch.cjs");
    const result = await generateGitPatch("feature/native_versions", "main", {
      cwd: repoDir,
      mode: "incremental",
      incrementalBaseRef: "refs/remotes/origin/pr-head",
      incrementalBaseSha: forkPrHeadSha,
    });

    expect(result.success).toBe(true);
    expect(result.baseCommit).toBe(forkPrHeadSha);
    const patch = fs.readFileSync(result.patchPath, "utf8");
    expect(patch).toContain("agent-change.txt");
    expect(patch).not.toContain("fork-file.txt");
    expect(patch).not.toContain("base-same-name.txt");
  });
});
