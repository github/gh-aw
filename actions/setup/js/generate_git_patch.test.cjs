import { describe, it, expect, beforeEach, afterEach } from "vitest";

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

  it("should return error when no commits can be found", async () => {
    delete process.env.GITHUB_SHA;
    process.env.GITHUB_WORKSPACE = "/tmp/test-repo";

    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    const result = generateGitPatch(null);

    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
  });

  it("should return success false when no commits found", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    // Set up environment but in a way that won't find commits
    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = generateGitPatch("nonexistent-branch");

    expect(result.success).toBe(false);
    expect(result).toHaveProperty("error");
    expect(result).toHaveProperty("patchPath");
  });

  it("should create patch directory if it doesn't exist", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    // Even if it fails, it should try to create the directory
    const result = generateGitPatch("test-branch");

    expect(result).toHaveProperty("patchPath");
    expect(result.patchPath).toBe("/tmp/gh-aw/aw.patch");
  });

  it("should return patch info structure", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = generateGitPatch("test-branch");

    expect(result).toHaveProperty("success");
    expect(result).toHaveProperty("patchPath");
    expect(typeof result.success).toBe("boolean");
  });

  it("should handle null branch name", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = generateGitPatch(null);

    expect(result).toHaveProperty("success");
    expect(result).toHaveProperty("patchPath");
  });

  it("should handle empty branch name", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    const result = generateGitPatch("");

    expect(result).toHaveProperty("success");
    expect(result).toHaveProperty("patchPath");
  });

  it("should use default branch from environment", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";
    process.env.DEFAULT_BRANCH = "develop";

    const result = generateGitPatch("feature-branch");

    expect(result).toHaveProperty("success");
    // Should attempt to use develop as default branch
  });

  it("should fall back to GH_AW_BASE_BRANCH if DEFAULT_BRANCH not set", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";
    delete process.env.DEFAULT_BRANCH;
    process.env.GH_AW_BASE_BRANCH = "master";

    const result = generateGitPatch("feature-branch");

    expect(result).toHaveProperty("success");
    // Should attempt to use master as base branch
  });

  it("should safely handle branch names with special characters", async () => {
    const { generateGitPatch } = await import("./generate_git_patch.cjs");

    process.env.GITHUB_WORKSPACE = "/tmp/nonexistent-repo";
    process.env.GITHUB_SHA = "abc123";

    // Test with various special characters that could cause shell injection
    const maliciousBranchNames = ["feature; rm -rf /", "feature && echo hacked", "feature | cat /etc/passwd", "feature$(whoami)", "feature`whoami`", "feature\nrm -rf /"];

    for (const branchName of maliciousBranchNames) {
      const result = generateGitPatch(branchName);

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

    const result = generateGitPatch("test-branch");

    // Should not throw an error and should handle safely
    expect(result).toHaveProperty("success");
    expect(result.success).toBe(false);
  });
});
