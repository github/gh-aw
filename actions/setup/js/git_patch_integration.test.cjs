/**
 * Integration tests for git patch generation and application
 *
 * These tests run REAL git commands to verify:
 * 1. git format-patch generates valid patches
 * 2. git am can apply patches correctly
 * 3. Emoji and unicode in commit messages work
 * 4. Merge conflict detection works
 * 5. Concurrent push scenarios are handled
 *
 * These tests require git to be installed and create temporary git repos.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { spawnSync } from "child_process";
import os from "os";
import { generateGitPatch } from "./generate_git_patch.cjs";
import { generateGitBundle } from "./generate_git_bundle.cjs";

// generateGitPatch uses execGitSync from git_helpers.cjs which calls core.debug / core.error
// as GitHub Actions globals. Provide a no-op mock so these tests work outside of Actions.
global.core = {
  debug: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
};

/**
 * Execute git command safely with args array
 */
function execGit(args, options = {}) {
  const result = spawnSync("git", args, {
    encoding: "utf8",
    ...options,
  });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0 && !options.allowFailure) {
    throw new Error(`git ${args.join(" ")} failed: ${result.stderr}`);
  }
  return result;
}

/**
 * Create a temporary git repository for testing
 */
function createTestRepo() {
  const repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "git-patch-test-"));

  execGit(["init"], { cwd: repoDir });
  execGit(["config", "user.name", "Test User"], { cwd: repoDir });
  execGit(["config", "user.email", "test@example.com"], { cwd: repoDir });

  // Create initial commit on main branch
  fs.writeFileSync(path.join(repoDir, "README.md"), "# Test Repo\n");
  execGit(["add", "."], { cwd: repoDir });
  execGit(["commit", "-m", "Initial commit"], { cwd: repoDir });

  // Create main as the default branch
  execGit(["branch", "-M", "main"], { cwd: repoDir });

  return repoDir;
}

/**
 * Clean up test repository
 */
function cleanupTestRepo(repoDir) {
  if (repoDir && fs.existsSync(repoDir)) {
    fs.rmSync(repoDir, { recursive: true, force: true });
  }
}

describe("git patch integration tests", () => {
  let repoDir;
  let patchDir;

  beforeEach(() => {
    repoDir = createTestRepo();
    patchDir = fs.mkdtempSync(path.join(os.tmpdir(), "git-patch-output-"));
  });

  afterEach(() => {
    cleanupTestRepo(repoDir);
    cleanupTestRepo(patchDir);
  });

  // ──────────────────────────────────────────────────────
  // Basic Patch Generation and Application
  // ──────────────────────────────────────────────────────

  describe("basic patch generation and application", () => {
    it("should generate and apply a simple patch", () => {
      // Create feature branch with changes
      execGit(["checkout", "-b", "feature-branch"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "test.txt"), "Hello World\n");
      execGit(["add", "test.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Add test file"], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "test.patch");
      const patchResult = execGit(["format-patch", "main..feature-branch", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Verify patch has content
      const patchContent = fs.readFileSync(patchPath, "utf8");
      expect(patchContent).toContain("Subject:");
      expect(patchContent).toContain("Add test file");
      expect(patchContent).toContain("Hello World");

      // Create a clean branch to test apply
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-test"], { cwd: repoDir });

      // Apply patch
      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);

      // Verify file was created
      const fileContent = fs.readFileSync(path.join(repoDir, "test.txt"), "utf8");
      expect(fileContent).toBe("Hello World\n");
    });

    it("should handle multi-commit patches", () => {
      execGit(["checkout", "-b", "multi-commit"], { cwd: repoDir });

      // First commit
      fs.writeFileSync(path.join(repoDir, "file1.txt"), "File 1\n");
      execGit(["add", "file1.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Add file 1"], { cwd: repoDir });

      // Second commit
      fs.writeFileSync(path.join(repoDir, "file2.txt"), "File 2\n");
      execGit(["add", "file2.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Add file 2"], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "multi.patch");
      const patchResult = execGit(["format-patch", "main..multi-commit", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Apply to clean branch
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-multi"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);

      // Verify both files exist
      expect(fs.existsSync(path.join(repoDir, "file1.txt"))).toBe(true);
      expect(fs.existsSync(path.join(repoDir, "file2.txt"))).toBe(true);
    });
  });

  // ──────────────────────────────────────────────────────
  // Emoji and Unicode in Commit Messages
  // ──────────────────────────────────────────────────────

  describe("emoji and unicode handling", () => {
    it("should handle emoji in commit message", () => {
      execGit(["checkout", "-b", "emoji-branch"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "emoji.txt"), "Bug fixed!\n");
      execGit(["add", "emoji.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "🐛 Fix bug with login"], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "emoji.patch");
      const patchResult = execGit(["format-patch", "main..emoji-branch", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Check patch content - git may RFC2047 encode or keep UTF-8
      const patchContent = fs.readFileSync(patchPath, "utf8");
      // Either raw emoji or RFC2047 encoded is acceptable
      const hasEmoji = patchContent.includes("🐛") || patchContent.includes("=?UTF-8?");
      expect(hasEmoji).toBe(true);

      // Apply to clean branch
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-emoji"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);

      // Verify commit message was preserved
      const logResult = execGit(["log", "-1", "--format=%s"], { cwd: repoDir });
      expect(logResult.stdout.trim()).toContain("Fix bug");
    });

    it("should handle unicode characters in commit message", () => {
      execGit(["checkout", "-b", "unicode-branch"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "unicode.txt"), "International text\n");
      execGit(["add", "unicode.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "日本語のコミットメッセージ"], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "unicode.patch");
      const patchResult = execGit(["format-patch", "main..unicode-branch", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Apply to clean branch
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-unicode"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);
    });

    it("should handle multi-line commit messages", () => {
      execGit(["checkout", "-b", "multiline-branch"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "multiline.txt"), "Content\n");
      execGit(["add", "multiline.txt"], { cwd: repoDir });

      // Commit with multi-line message
      const commitMsg = "Short title\n\nThis is the body of the commit.\nIt has multiple lines.\n\n- Item 1\n- Item 2";
      execGit(["commit", "-m", commitMsg], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "multiline.patch");
      const patchResult = execGit(["format-patch", "main..multiline-branch", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Verify patch structure
      const patchContent = fs.readFileSync(patchPath, "utf8");
      expect(patchContent).toContain("Subject:");
      expect(patchContent).toContain("Short title");

      // Apply to clean branch
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-multiline"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);

      // Verify body was preserved
      const logResult = execGit(["log", "-1", "--format=%B"], { cwd: repoDir });
      expect(logResult.stdout).toContain("body of the commit");
    });

    it("should handle special characters in commit message", () => {
      execGit(["checkout", "-b", "special-chars"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "special.txt"), "Content\n");
      execGit(["add", "special.txt"], { cwd: repoDir });

      // Commit with special characters that might cause issues
      const commitMsg = "Fix: Handle \"quotes\" and 'apostrophes' and $variables and `backticks`";
      execGit(["commit", "-m", commitMsg], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "special.patch");
      const patchResult = execGit(["format-patch", "main..special-chars", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Apply to clean branch
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-special"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);
    });
  });

  // ──────────────────────────────────────────────────────
  // Merge Conflict Scenarios
  // ──────────────────────────────────────────────────────

  describe("merge conflict scenarios", () => {
    it("should detect and report patch application failure due to conflict", () => {
      // Create conflicting changes
      fs.writeFileSync(path.join(repoDir, "conflict.txt"), "Original content\n");
      execGit(["add", "conflict.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Add conflict file"], { cwd: repoDir });

      // Create feature branch with one change
      execGit(["checkout", "-b", "feature-conflict"], { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "conflict.txt"), "Feature branch content\n");
      execGit(["add", "conflict.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Feature change"], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "conflict.patch");
      const patchResult = execGit(["format-patch", "main~1..feature-conflict", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Go back to main and make a different change
      execGit(["checkout", "main"], { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "conflict.txt"), "Main branch different content\n");
      execGit(["add", "conflict.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Main change"], { cwd: repoDir });

      // Try to apply patch - should fail
      const applyResult = execGit(["am", patchPath], { cwd: repoDir, allowFailure: true });
      expect(applyResult.status).not.toBe(0);
      // Error message varies - could be "patch does not apply" or "already exists in index"
      const errorOutput = applyResult.stderr.toLowerCase();
      expect(errorOutput.includes("patch does not apply") || errorOutput.includes("already exists") || errorOutput.includes("conflict")).toBe(true);

      // Abort the failed am
      execGit(["am", "--abort"], { cwd: repoDir, allowFailure: true });
    });

    it("should handle patch based on old commit that no longer exists in history", () => {
      // This simulates force-push scenario where base commit was rewritten
      execGit(["checkout", "-b", "force-push-branch"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "force.txt"), "Content\n");
      execGit(["add", "force.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Initial force change"], { cwd: repoDir });

      // Get the commit SHA
      const shaResult = execGit(["rev-parse", "HEAD"], { cwd: repoDir });
      const originalSha = shaResult.stdout.trim();

      // Generate patch
      const patchPath = path.join(patchDir, "force.patch");
      const patchResult = execGit(["format-patch", "main..force-push-branch", "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Simulate force-push by amending the commit
      fs.writeFileSync(path.join(repoDir, "force.txt"), "Different content\n");
      execGit(["add", "force.txt"], { cwd: repoDir });
      execGit(["commit", "--amend", "-m", "Amended force change"], { cwd: repoDir });

      // The original SHA should no longer match HEAD
      const newShaResult = execGit(["rev-parse", "HEAD"], { cwd: repoDir });
      expect(newShaResult.stdout.trim()).not.toBe(originalSha);

      // The patch should still be applicable to main (it's based on main)
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-force"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);
    });

    it("should recover add/add conflicts with checkout --theirs and git am --continue", () => {
      const baseCommit = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();

      execGit(["checkout", "-b", "feature-add-add"], { cwd: repoDir });
      fs.mkdirSync(path.join(repoDir, "docs"), { recursive: true });
      fs.writeFileSync(path.join(repoDir, "docs", "conflict.md"), "Patch branch content\n");
      execGit(["add", "docs/conflict.md"], { cwd: repoDir });
      execGit(["commit", "-m", "Patch adds conflict file"], { cwd: repoDir });

      const patchPath = path.join(patchDir, "add-add.patch");
      const patchResult = execGit(["format-patch", `${baseCommit}..feature-add-add`, "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      execGit(["checkout", "main"], { cwd: repoDir });
      fs.mkdirSync(path.join(repoDir, "docs"), { recursive: true });
      fs.writeFileSync(path.join(repoDir, "docs", "conflict.md"), "Main branch content\n");
      execGit(["add", "docs/conflict.md"], { cwd: repoDir });
      execGit(["commit", "-m", "Main adds same file differently"], { cwd: repoDir });

      execGit(["checkout", "-b", "apply-add-add"], { cwd: repoDir });
      const amResult = execGit(["am", "--3way", patchPath], { cwd: repoDir, allowFailure: true });
      expect(amResult.status).not.toBe(0);

      const unresolved = execGit(["diff", "--name-only", "--diff-filter=U", "-z"], { cwd: repoDir }).stdout.split("\0").filter(Boolean);
      expect(unresolved).toContain("docs/conflict.md");

      const statusPorcelain = execGit(["status", "--porcelain", "-z"], { cwd: repoDir }).stdout.split("\0").filter(Boolean);
      expect(statusPorcelain).toContain("AA docs/conflict.md");

      execGit(["checkout", "--theirs", "--", "docs/conflict.md"], { cwd: repoDir });
      execGit(["add", "--", "docs/conflict.md"], { cwd: repoDir });
      execGit(["am", "--continue"], { cwd: repoDir });

      const content = fs.readFileSync(path.join(repoDir, "docs", "conflict.md"), "utf8");
      expect(content).toBe("Patch branch content\n");
      const subject = execGit(["log", "-1", "--format=%s"], { cwd: repoDir }).stdout.trim();
      expect(subject).toBe("Patch adds conflict file");
    });
  });

  // ──────────────────────────────────────────────────────
  // Concurrent Push Scenarios
  // ──────────────────────────────────────────────────────

  describe("concurrent push scenarios", () => {
    let bareRepoDir;
    let workingRepo1;
    let workingRepo2;

    beforeEach(() => {
      // Create a bare repository to simulate remote
      bareRepoDir = fs.mkdtempSync(path.join(os.tmpdir(), "bare-repo-"));
      execGit(["init", "--bare"], { cwd: bareRepoDir });

      // Clone it twice to simulate two workers
      workingRepo1 = fs.mkdtempSync(path.join(os.tmpdir(), "working1-"));
      execGit(["clone", bareRepoDir, "."], { cwd: workingRepo1 });
      execGit(["config", "user.name", "User 1"], { cwd: workingRepo1 });
      execGit(["config", "user.email", "user1@example.com"], { cwd: workingRepo1 });

      // Make initial commit
      fs.writeFileSync(path.join(workingRepo1, "README.md"), "# Repo\n");
      execGit(["add", "."], { cwd: workingRepo1 });
      execGit(["commit", "-m", "Initial"], { cwd: workingRepo1 });
      execGit(["push", "-u", "origin", "main"], { cwd: workingRepo1, allowFailure: true });

      workingRepo2 = fs.mkdtempSync(path.join(os.tmpdir(), "working2-"));
      execGit(["clone", bareRepoDir, "."], { cwd: workingRepo2 });
      execGit(["config", "user.name", "User 2"], { cwd: workingRepo2 });
      execGit(["config", "user.email", "user2@example.com"], { cwd: workingRepo2 });
    });

    afterEach(() => {
      cleanupTestRepo(bareRepoDir);
      cleanupTestRepo(workingRepo1);
      cleanupTestRepo(workingRepo2);
    });

    it("should fail when branch was updated after patch was generated", () => {
      // Create PR branch in working repo 1
      execGit(["checkout", "-b", "pr-branch"], { cwd: workingRepo1 });
      fs.writeFileSync(path.join(workingRepo1, "file1.txt"), "User 1 content\n");
      execGit(["add", "file1.txt"], { cwd: workingRepo1 });
      execGit(["commit", "-m", "User 1 commit"], { cwd: workingRepo1 });
      execGit(["push", "-u", "origin", "pr-branch"], { cwd: workingRepo1 });

      // Fetch in working repo 2 and make changes
      execGit(["fetch", "origin"], { cwd: workingRepo2 });
      execGit(["checkout", "-b", "pr-branch", "--track", "origin/pr-branch"], { cwd: workingRepo2 });

      fs.writeFileSync(path.join(workingRepo2, "file2.txt"), "User 2 content\n");
      execGit(["add", "file2.txt"], { cwd: workingRepo2 });
      execGit(["commit", "-m", "User 2 commit"], { cwd: workingRepo2 });

      // Generate patch in repo 2 (before pushing)
      const patchPath = path.join(patchDir, "concurrent.patch");
      const patchResult = execGit(["format-patch", "origin/pr-branch..pr-branch", "--stdout"], { cwd: workingRepo2 });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Simulate concurrent push - User 1 pushes first
      fs.writeFileSync(path.join(workingRepo1, "file3.txt"), "Another User 1 commit\n");
      execGit(["add", "file3.txt"], { cwd: workingRepo1 });
      execGit(["commit", "-m", "Concurrent commit from User 1"], { cwd: workingRepo1 });
      execGit(["push", "origin", "pr-branch"], { cwd: workingRepo1 });

      // Now User 2 tries to push (without fetching) - should fail with non-fast-forward
      const pushResult = execGit(["push", "origin", "pr-branch"], { cwd: workingRepo2, allowFailure: true });
      expect(pushResult.status).not.toBe(0);
      // Error message varies by git version but contains rejection info
      const errorOutput = pushResult.stderr.toLowerCase();
      expect(errorOutput.includes("rejected") || errorOutput.includes("failed") || errorOutput.includes("non-fast-forward")).toBe(true);
    });

    it("should succeed when patch apply is re-anchored to the recorded base commit", () => {
      const baseCommit = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();

      // Simulate remote branch advancing after patch generation.
      execGit(["checkout", "-b", "pr-branch"], { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "drift.txt"), "remote-head-change\n");
      execGit(["add", "drift.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Remote branch advanced"], { cwd: repoDir });

      // Simulate patch generated from the earlier base commit.
      execGit(["checkout", "-b", "patch-generated-from-base", baseCommit], { cwd: repoDir });
      fs.writeFileSync(path.join(repoDir, "drift.txt"), "patch-change\n");
      execGit(["add", "drift.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Patch commit from recorded base"], { cwd: repoDir });

      const patchPath = path.join(patchDir, "recorded-base.patch");
      const patchResult = execGit(["format-patch", `${baseCommit}..patch-generated-from-base`, "--stdout"], { cwd: repoDir });
      fs.writeFileSync(patchPath, patchResult.stdout);

      // Applying directly on the advanced head fails.
      execGit(["checkout", "pr-branch"], { cwd: repoDir });
      const applyOnAdvancedHead = execGit(["am", "--3way", patchPath], { cwd: repoDir, allowFailure: true });
      expect(applyOnAdvancedHead.status).not.toBe(0);
      const conflictOutput = applyOnAdvancedHead.stderr.toLowerCase();
      expect(conflictOutput.includes("patch does not apply") || conflictOutput.includes("conflict") || conflictOutput.includes("failed to merge")).toBe(true);
      execGit(["am", "--abort"], { cwd: repoDir, allowFailure: true });

      // Re-anchor to the recorded base commit first, then apply.
      execGit(["reset", "--hard", baseCommit], { cwd: repoDir });
      const applyAfterReanchor = execGit(["am", "--3way", patchPath], { cwd: repoDir });
      expect(applyAfterReanchor.status).toBe(0);
      expect(fs.readFileSync(path.join(repoDir, "drift.txt"), "utf8")).toBe("patch-change\n");
    });
  });

  // ──────────────────────────────────────────────────────
  // Commit Title Suffix Modification
  // ──────────────────────────────────────────────────────

  describe("commit title suffix modification", () => {
    it("should correctly modify Subject line to add suffix", () => {
      execGit(["checkout", "-b", "suffix-test"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "suffix.txt"), "Content\n");
      execGit(["add", "suffix.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "Original title"], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "suffix.patch");
      const patchResult = execGit(["format-patch", "main..suffix-test", "--stdout"], { cwd: repoDir });
      let patchContent = patchResult.stdout;

      // Simulate the suffix modification logic from push_to_pull_request_branch.cjs
      const commitTitleSuffix = " [bot]";
      patchContent = patchContent.replace(/^Subject: (?:\[PATCH\] )?(.*)$/gm, (match, title) => `Subject: [PATCH] ${title}${commitTitleSuffix}`);

      fs.writeFileSync(patchPath, patchContent);

      // Verify the modification
      const modifiedPatch = fs.readFileSync(patchPath, "utf8");
      expect(modifiedPatch).toContain("Subject: [PATCH] Original title [bot]");

      // Apply the modified patch
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-suffix"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);

      // Verify commit message has suffix
      const logResult = execGit(["log", "-1", "--format=%s"], { cwd: repoDir });
      expect(logResult.stdout.trim()).toBe("Original title [bot]");
    });

    it("should handle emoji in commit title with suffix modification", () => {
      execGit(["checkout", "-b", "emoji-suffix"], { cwd: repoDir });

      fs.writeFileSync(path.join(repoDir, "emoji.txt"), "Content\n");
      execGit(["add", "emoji.txt"], { cwd: repoDir });
      execGit(["commit", "-m", "🚀 Launch feature"], { cwd: repoDir });

      // Generate patch
      const patchPath = path.join(patchDir, "emoji-suffix.patch");
      const patchResult = execGit(["format-patch", "main..emoji-suffix", "--stdout"], { cwd: repoDir });
      let patchContent = patchResult.stdout;

      // Check if emoji is RFC2047 encoded
      const isEncoded = patchContent.includes("=?UTF-8?");

      // Apply suffix modification
      const commitTitleSuffix = " [bot]";

      if (isEncoded) {
        // For RFC2047 encoded subjects, we need different handling
        // This test documents the current limitation
        console.log("Note: Emoji commit titles are RFC2047 encoded - suffix cannot be applied with simple regex");
        // The regex won't match encoded subjects properly
      } else {
        // If not encoded, apply the suffix
        patchContent = patchContent.replace(/^Subject: (?:\[PATCH\] )?(.*)$/gm, (match, title) => `Subject: [PATCH] ${title}${commitTitleSuffix}`);
      }

      fs.writeFileSync(patchPath, patchContent);

      // Apply the patch
      execGit(["checkout", "main"], { cwd: repoDir });
      execGit(["checkout", "-b", "apply-emoji-suffix"], { cwd: repoDir });

      const applyResult = execGit(["am", patchPath], { cwd: repoDir });
      expect(applyResult.status).toBe(0);

      // The commit should be applied (suffix may or may not be present depending on encoding)
      const logResult = execGit(["log", "-1", "--format=%s"], { cwd: repoDir });
      expect(logResult.stdout).toContain("Launch feature");
    });
  });

  // ──────────────────────────────────────────────────────
  // Incremental Mode Tests
  // ──────────────────────────────────────────────────────

  describe("incremental mode", () => {
    let bareRepoDir;
    let workingRepo;

    beforeEach(() => {
      // Create a bare repo to simulate remote
      bareRepoDir = fs.mkdtempSync(path.join(os.tmpdir(), "bare-incremental-"));
      execGit(["init", "--bare", "--initial-branch=main"], { cwd: bareRepoDir });

      // Clone the bare repo
      workingRepo = fs.mkdtempSync(path.join(os.tmpdir(), "working-incremental-"));
      execGit(["clone", bareRepoDir, "."], { cwd: workingRepo });

      // Set up git config (after clone, we have a proper repo)
      execGit(["config", "user.email", "test@test.com"], { cwd: workingRepo });
      execGit(["config", "user.name", "Test User"], { cwd: workingRepo });

      // Checkout main (might be master on some systems)
      execGit(["checkout", "-b", "main"], { cwd: workingRepo, allowFailure: true });

      // Create initial commit on main
      fs.writeFileSync(path.join(workingRepo, "README.md"), "# Initial\n");
      execGit(["add", "README.md"], { cwd: workingRepo });
      execGit(["commit", "-m", "Initial commit"], { cwd: workingRepo });

      // Push main to origin (this MUST succeed for full mode tests)
      execGit(["push", "-u", "origin", "main"], { cwd: workingRepo });
    });

    afterEach(() => {
      // Clean up
      cleanupTestRepo(bareRepoDir);
      cleanupTestRepo(workingRepo);
    });

    it("should only include new commits after origin/branch in incremental mode", async () => {
      // Create a feature branch with first commit
      execGit(["checkout", "-b", "feature-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "feature.txt"), "First commit content\n");
      execGit(["add", "feature.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "First commit on feature branch"], { cwd: workingRepo });

      // Push to origin
      execGit(["push", "-u", "origin", "feature-branch"], { cwd: workingRepo });

      // Add a second commit (the "new" commit)
      fs.writeFileSync(path.join(workingRepo, "feature2.txt"), "Second commit content\n");
      execGit(["add", "feature2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Second commit - the new one"], { cwd: workingRepo });

      // Generate patch in incremental mode
      // Set environment
      const origWorkspace = process.env.GITHUB_WORKSPACE;
      const origDefaultBranch = process.env.DEFAULT_BRANCH;
      process.env.GITHUB_WORKSPACE = workingRepo;
      process.env.DEFAULT_BRANCH = "main";

      try {
        const result = await generateGitPatch("feature-branch", "main", { mode: "incremental" });

        expect(result.success).toBe(true);
        expect(result.patchPath).toBeDefined();

        // Read the patch content
        const patchContent = fs.readFileSync(result.patchPath, "utf8");

        // Should only have ONE patch [PATCH 1/1], not [PATCH 1/2], [PATCH 2/2]
        expect(patchContent).toContain("Subject:");

        // Should contain the second commit
        expect(patchContent).toContain("Second commit - the new one");

        // Should NOT contain the first commit (it's already on origin/feature-branch)
        expect(patchContent).not.toContain("First commit on feature branch");

        // Verify it's [PATCH 1/1] or just [PATCH], not [PATCH 1/2]
        const patchHeaders = patchContent.match(/\[PATCH[^\]]*\]/g);
        // If there are numbered patches, should be just 1
        if (patchHeaders) {
          expect(patchHeaders.length).toBe(1);
        }
      } finally {
        process.env.GITHUB_WORKSPACE = origWorkspace;
        process.env.DEFAULT_BRANCH = origDefaultBranch;
      }
    });

    it("should fail clearly when origin/branch doesnt exist in incremental mode", async () => {
      // Create a local branch without pushing
      execGit(["checkout", "-b", "local-only-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "local.txt"), "Local content\n");
      execGit(["add", "local.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Local commit"], { cwd: workingRepo });

      // Don't push - origin/local-only-branch doesn't exist

      const origWorkspace = process.env.GITHUB_WORKSPACE;
      const origDefaultBranch = process.env.DEFAULT_BRANCH;
      process.env.GITHUB_WORKSPACE = workingRepo;
      process.env.DEFAULT_BRANCH = "main";

      try {
        const result = await generateGitPatch("local-only-branch", "main", { mode: "incremental" });

        // Should fail with a clear error message
        expect(result.success).toBe(false);
        expect(result.error).toContain("Cannot generate incremental patch");
        expect(result.error).toContain("origin/local-only-branch");
      } finally {
        process.env.GITHUB_WORKSPACE = origWorkspace;
        process.env.DEFAULT_BRANCH = origDefaultBranch;
      }
    });

    it("should report diffSize as the net diff between origin/branch and HEAD in incremental mode", async () => {
      // Reproduces the long-running branch scenario from the issue:
      //   - origin/<branch> already has accumulated history (e.g. many KB)
      //   - the agent makes a small new commit on top
      //   - the format-patch file size only reflects the *new* commit (because
      //     baseRef = origin/<branch>), but the returned diffSize must also be
      //     small and must NOT reflect the divergence from main.

      // Create the long-running branch with a "large" accumulated payload.
      execGit(["checkout", "-b", "long-running-branch"], { cwd: workingRepo });
      const accumulated = "accumulated content line\n".repeat(2000); // ~50 KB
      fs.writeFileSync(path.join(workingRepo, "accumulated.txt"), accumulated);
      execGit(["add", "accumulated.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Accumulated work from previous iterations"], { cwd: workingRepo });
      execGit(["push", "-u", "origin", "long-running-branch"], { cwd: workingRepo });

      // Now the agent's "new iteration": a tiny incremental change.
      fs.writeFileSync(path.join(workingRepo, "tiny.txt"), "tiny change\n");
      execGit(["add", "tiny.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Tiny new iteration"], { cwd: workingRepo });

      const origWorkspace = process.env.GITHUB_WORKSPACE;
      const origDefaultBranch = process.env.DEFAULT_BRANCH;
      process.env.GITHUB_WORKSPACE = workingRepo;
      process.env.DEFAULT_BRANCH = "main";

      try {
        const result = await generateGitPatch("long-running-branch", "main", { mode: "incremental" });

        expect(result.success).toBe(true);
        expect(typeof result.diffSize).toBe("number");

        // The incremental net diff is just the tiny.txt addition (well under 1 KB).
        expect(result.diffSize).toBeGreaterThan(0);
        expect(result.diffSize).toBeLessThan(1024);

        // And the diffSize must NOT include the accumulated 50 KB payload that
        // already exists on origin/long-running-branch — that is the entire
        // point of the fix.
        expect(result.diffSize).toBeLessThan(2000);
      } finally {
        process.env.GITHUB_WORKSPACE = origWorkspace;
        process.env.DEFAULT_BRANCH = origDefaultBranch;
      }
    });

    /**
     * Sets GITHUB_WORKSPACE, DEFAULT_BRANCH, GITHUB_TOKEN, and GITHUB_SERVER_URL for
     * a test, then restores the original values (or deletes them if they were unset).
     * Returns a restore function to call in `finally`.
     */
    function setTestEnv(workspaceDir) {
      const saved = {
        GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE,
        DEFAULT_BRANCH: process.env.DEFAULT_BRANCH,
        GITHUB_TOKEN: process.env.GITHUB_TOKEN,
        GITHUB_SERVER_URL: process.env.GITHUB_SERVER_URL,
      };
      process.env.GITHUB_WORKSPACE = workspaceDir;
      process.env.DEFAULT_BRANCH = "main";
      process.env.GITHUB_TOKEN = "ghs_test_token_for_cleanup_verification";
      process.env.GITHUB_SERVER_URL = "https://github.example.com";
      return () => {
        for (const [key, value] of Object.entries(saved)) {
          if (value === undefined) {
            delete process.env[key];
          } else {
            process.env[key] = value;
          }
        }
      };
    }

    it("should not write auth extraheader during local artifact generation", async () => {
      // Set up a feature branch, push a first commit, then add a second commit so
      // incremental mode has something new to patch.
      execGit(["checkout", "-b", "auth-cleanup-success"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "auth.txt"), "auth test\n");
      execGit(["add", "auth.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Auth cleanup base commit"], { cwd: workingRepo });
      execGit(["push", "-u", "origin", "auth-cleanup-success"], { cwd: workingRepo });

      // Add a second commit that will become the incremental patch
      fs.writeFileSync(path.join(workingRepo, "auth2.txt"), "auth test 2\n");
      execGit(["add", "auth2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Auth cleanup new commit"], { cwd: workingRepo });

      const restore = setTestEnv(workingRepo);
      try {
        const result = await generateGitPatch("auth-cleanup-success", "main", { mode: "incremental" });

        expect(result.success).toBe(true);

        // Artifact generation does not configure authentication.
        const configCheck = spawnSync("git", ["config", "--local", "--get", "http.https://github.example.com/.extraheader"], { cwd: workingRepo, encoding: "utf8" });
        // exit status 1 means the key does not exist — that is what we want
        expect(configCheck.status).toBe(1);
      } finally {
        restore();
      }
    });

    it("should not write auth extraheader when the required local ref is missing", async () => {
      // Create a local-only branch without a pre-agent tracking ref.
      execGit(["checkout", "-b", "auth-cleanup-failure"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "auth-fail.txt"), "auth fail test\n");
      execGit(["add", "auth-fail.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Auth cleanup failure test commit"], { cwd: workingRepo });
      // Do NOT push: there is no tracking ref.

      const restore = setTestEnv(workingRepo);
      try {
        const result = await generateGitPatch("auth-cleanup-failure", "main", { mode: "incremental" });

        // The missing baseline must fail without a fetch.
        expect(result.success).toBe(false);
        expect(result.error).toContain("Cannot generate incremental patch");

        // Artifact generation does not configure authentication.
        const configCheck = spawnSync("git", ["config", "--local", "--get", "http.https://github.example.com/.extraheader"], { cwd: workingRepo, encoding: "utf8" });
        // exit status 1 means the key does not exist — that is what we want
        expect(configCheck.status).toBe(1);
      } finally {
        restore();
      }
    });

    it("should not fetch a missing baseline even when legacy options.token is provided", async () => {
      // Set up a feature branch with a commit to push
      execGit(["checkout", "-b", "token-option-test"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "token-test.txt"), "token option test\n");
      execGit(["add", "token-test.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Token option base commit"], { cwd: workingRepo });
      execGit(["push", "-u", "origin", "token-option-test"], { cwd: workingRepo });

      // Add a second commit that will become the incremental patch
      fs.writeFileSync(path.join(workingRepo, "token-test2.txt"), "token option test 2\n");
      execGit(["add", "token-test2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Token option new commit"], { cwd: workingRepo });

      // Delete the tracking ref; artifact generation must not restore it.
      execGit(["update-ref", "-d", "refs/remotes/origin/token-option-test"], { cwd: workingRepo });

      const restore = setTestEnv(workingRepo);
      try {
        // Legacy token options must not enable a fetch.
        core.warning.mockClear();
        const result = await generateGitPatch("token-option-test", "main", {
          mode: "incremental",
          token: "ghs_custom_token_for_cross_repo",
        });

        expect(result.success).toBe(false);
        expect(core.warning).toHaveBeenCalledTimes(1);
        expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("options.token is deprecated and ignored"));
        expect(JSON.stringify(core.warning.mock.calls)).not.toContain("ghs_custom_token_for_cross_repo");
        expect(result.error).toContain("Artifact generation never fetches");
        expect(execGit(["show-ref", "--verify", "--quiet", "refs/remotes/origin/token-option-test"], { cwd: workingRepo, allowFailure: true }).status).toBe(1);

        // Artifact generation does not configure authentication.
        const configCheck = spawnSync("git", ["config", "--local", "--get", "http.https://github.example.com/.extraheader"], { cwd: workingRepo, encoding: "utf8" });
        // exit status 1 means the key does not exist — that is what we want
        expect(configCheck.status).toBe(1);
      } finally {
        restore();
      }
    });

    it("should use the existing remote tracking ref without contacting the remote", async () => {
      // Simulate a shallow checkout scenario:
      // 1. feature-branch is created, first commit pushed to origin (origin/feature-branch exists)
      // 2. Agent adds a new commit locally
      // 3. Remote URL is broken so git fetch fails
      // 4. We expect patch generation to succeed using the existing origin/feature-branch ref
      execGit(["checkout", "-b", "shallow-fetch-fail"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "base.txt"), "Base content\n");
      execGit(["add", "base.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Base commit (already on remote)"], { cwd: workingRepo });

      // Push so origin/shallow-fetch-fail tracking ref is set up (simulating shallow checkout)
      execGit(["push", "-u", "origin", "shallow-fetch-fail"], { cwd: workingRepo });

      // Add a new commit (the agent's work)
      fs.writeFileSync(path.join(workingRepo, "agent-change.txt"), "Agent change\n");
      execGit(["add", "agent-change.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Agent commit - should appear in patch"], { cwd: workingRepo });

      // Break the remote URL to simulate fetch failure (e.g. missing credentials or network issue)
      execGit(["remote", "set-url", "origin", "https://invalid.example.invalid/nonexistent-repo.git"], { cwd: workingRepo });

      const restore = setTestEnv(workingRepo);
      try {
        // origin/shallow-fetch-fail still points to the base commit even though fetch will fail
        const result = await generateGitPatch("shallow-fetch-fail", "main", { mode: "incremental" });

        // Should succeed using the existing remote tracking ref as the base
        expect(result.success).toBe(true);
        expect(result.patchPath).toBeDefined();

        const patchContent = fs.readFileSync(result.patchPath, "utf8");

        // Should contain the agent's new commit
        expect(patchContent).toContain("Agent commit - should appear in patch");

        // Should NOT include the already-pushed base commit
        expect(patchContent).not.toContain("Base commit (already on remote)");
      } finally {
        // Restore remote URL before cleanup so afterEach can delete the directory
        execGit(["remote", "set-url", "origin", bareRepoDir], { cwd: workingRepo });
        restore();
      }
    });

    it("should include all commits in full mode even when origin/branch exists", async () => {
      // Create a feature branch with first commit
      execGit(["checkout", "-b", "full-mode-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "full1.txt"), "First commit\n");
      execGit(["add", "full1.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "First commit in full mode test"], { cwd: workingRepo });

      // Push to origin
      execGit(["push", "-u", "origin", "full-mode-branch"], { cwd: workingRepo });

      // Add second commit
      fs.writeFileSync(path.join(workingRepo, "full2.txt"), "Second commit\n");
      execGit(["add", "full2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Second commit in full mode test"], { cwd: workingRepo });

      // Delete origin ref to force merge-base fallback
      execGit(["update-ref", "-d", "refs/remotes/origin/full-mode-branch"], { cwd: workingRepo });

      // Fetch origin/main so merge-base can work
      execGit(["fetch", "origin", "main"], { cwd: workingRepo });

      const origWorkspace = process.env.GITHUB_WORKSPACE;
      const origDefaultBranch = process.env.DEFAULT_BRANCH;
      process.env.GITHUB_WORKSPACE = workingRepo;
      process.env.DEFAULT_BRANCH = "main";

      try {
        // Full mode (default) - should fall back to merge-base and include all commits
        const result = await generateGitPatch("full-mode-branch", "main", { mode: "full" });

        // Debug output if test fails
        if (!result.success) {
          console.log("Full mode test failed with error:", result.error);
        }

        expect(result.success).toBe(true);

        const patchContent = fs.readFileSync(result.patchPath, "utf8");

        // Should contain BOTH commits (using merge-base with main)
        expect(patchContent).toContain("First commit in full mode test");
        expect(patchContent).toContain("Second commit in full mode test");
      } finally {
        process.env.GITHUB_WORKSPACE = origWorkspace;
        process.env.DEFAULT_BRANCH = origDefaultBranch;
      }
    });

    it("should base the full-mode patch on GITHUB_SHA when dispatched from a non-default branch", async () => {
      // Simulate workflow_dispatch from a branch that is ahead of main: the
      // dispatched branch has commits that are not on main, and the agent adds
      // one commit on top of it.
      execGit(["checkout", "-b", "dispatch-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "dispatch1.txt"), "dispatched work 1\n");
      execGit(["add", "dispatch1.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Dispatched branch commit 1"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "dispatch2.txt"), "dispatched work 2\n");
      execGit(["add", "dispatch2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Dispatched branch commit 2"], { cwd: workingRepo });

      const dispatchedSha = execGit(["rev-parse", "HEAD"], { cwd: workingRepo }).stdout.trim();

      // Agent branch created from the dispatched commit with a single commit
      execGit(["checkout", "-b", "agent-fix/example"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "agent.txt"), "agent change\n");
      execGit(["add", "agent.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Agent commit on dispatched branch"], { cwd: workingRepo });

      execGit(["fetch", "origin", "main"], { cwd: workingRepo });

      const origSha = process.env.GITHUB_SHA;
      process.env.GITHUB_SHA = dispatchedSha;
      const restore = setTestEnv(workingRepo);
      try {
        const result = await generateGitPatch("agent-fix/example", "main", { mode: "full" });

        expect(result.success).toBe(true);
        expect(result.baseCommit).toBe(dispatchedSha);

        const patchContent = fs.readFileSync(result.patchPath, "utf8");
        expect(patchContent).toContain("Agent commit on dispatched branch");
        expect(patchContent).not.toContain("Dispatched branch commit 1");
        expect(patchContent).not.toContain("Dispatched branch commit 2");
      } finally {
        if (origSha === undefined) {
          delete process.env.GITHUB_SHA;
        } else {
          process.env.GITHUB_SHA = origSha;
        }
        restore();
      }
    });

    it("should fall back to merge-base when GITHUB_SHA equals the agent branch tip (no agent commits yet)", async () => {
      // Simulate workflow_dispatch from a branch that is ahead of main, where the
      // agent branch was created from it but no commits have been made yet, so
      // GITHUB_SHA equals the agent branch tip. Basing the patch on GITHUB_SHA in
      // this case would produce an empty patch; the merge-base fallback must be
      // used instead so the dispatched branch's commits are still visible.
      execGit(["checkout", "-b", "dispatch-branch-2"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "dispatch3.txt"), "dispatched work 3\n");
      execGit(["add", "dispatch3.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Dispatched branch commit 3"], { cwd: workingRepo });

      const dispatchedSha = execGit(["rev-parse", "HEAD"], { cwd: workingRepo }).stdout.trim();

      // Agent branch created from the dispatched commit, but no agent commits yet.
      execGit(["checkout", "-b", "agent-fix/no-commits-yet"], { cwd: workingRepo });

      execGit(["fetch", "origin", "main"], { cwd: workingRepo });

      const origSha = process.env.GITHUB_SHA;
      process.env.GITHUB_SHA = dispatchedSha;
      const restore = setTestEnv(workingRepo);
      try {
        const result = await generateGitPatch("agent-fix/no-commits-yet", "main", { mode: "full" });

        expect(result.success).toBe(true);
        // Must not use GITHUB_SHA as the base (that would be a zero-commit patch);
        // the merge-base with main is used instead.
        expect(result.baseCommit).not.toBe(dispatchedSha);

        const patchContent = fs.readFileSync(result.patchPath, "utf8");
        expect(patchContent).toContain("Dispatched branch commit 3");
      } finally {
        if (origSha === undefined) {
          delete process.env.GITHUB_SHA;
        } else {
          process.env.GITHUB_SHA = origSha;
        }
        restore();
      }
    });

    it("should still use the merge-base in full mode when GITHUB_SHA is on the default branch", async () => {
      const mainSha = execGit(["rev-parse", "main"], { cwd: workingRepo }).stdout.trim();

      execGit(["checkout", "-b", "default-dispatch-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "agent1.txt"), "agent change 1\n");
      execGit(["add", "agent1.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Agent commit one"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "agent2.txt"), "agent change 2\n");
      execGit(["add", "agent2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Agent commit two"], { cwd: workingRepo });

      execGit(["fetch", "origin", "main"], { cwd: workingRepo });

      const origSha = process.env.GITHUB_SHA;
      process.env.GITHUB_SHA = mainSha;
      const restore = setTestEnv(workingRepo);
      try {
        const result = await generateGitPatch("default-dispatch-branch", "main", { mode: "full" });

        expect(result.success).toBe(true);
        expect(result.baseCommit).toBe(mainSha);

        const patchContent = fs.readFileSync(result.patchPath, "utf8");
        expect(patchContent).toContain("Agent commit one");
        expect(patchContent).toContain("Agent commit two");

        const logLines = execGit(["log", "--oneline", `${mainSha}..HEAD`], { cwd: workingRepo })
          .stdout.trim()
          .split("\n");
        expect(logLines).toHaveLength(2);
      } finally {
        if (origSha === undefined) {
          delete process.env.GITHUB_SHA;
        } else {
          process.env.GITHUB_SHA = origSha;
        }
        restore();
      }
    });

    it("should base the full-mode bundle on GITHUB_SHA when dispatched from a non-default branch", async () => {
      execGit(["checkout", "-b", "bundle-dispatch-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "bundle-dispatch.txt"), "dispatched work\n");
      execGit(["add", "bundle-dispatch.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Bundle dispatched branch commit"], { cwd: workingRepo });

      const dispatchedSha = execGit(["rev-parse", "HEAD"], { cwd: workingRepo }).stdout.trim();

      execGit(["checkout", "-b", "agent-bundle-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "bundle-agent.txt"), "agent change\n");
      execGit(["add", "bundle-agent.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Agent bundle commit"], { cwd: workingRepo });

      execGit(["fetch", "origin", "main"], { cwd: workingRepo });

      const origSha = process.env.GITHUB_SHA;
      process.env.GITHUB_SHA = dispatchedSha;
      const restore = setTestEnv(workingRepo);
      try {
        const result = await generateGitBundle("agent-bundle-branch", "main", { mode: "full" });

        expect(result.success).toBe(true);
        expect(result.baseCommit).toBe(dispatchedSha);
      } finally {
        if (origSha === undefined) {
          delete process.env.GITHUB_SHA;
        } else {
          process.env.GITHUB_SHA = origSha;
        }
        restore();
      }
    });

    it("should fall back to merge-base for the bundle when GITHUB_SHA equals the agent branch tip (no agent commits yet)", async () => {
      execGit(["checkout", "-b", "bundle-dispatch-branch-2"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "bundle-dispatch2.txt"), "dispatched work 2\n");
      execGit(["add", "bundle-dispatch2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Bundle dispatched branch commit 2"], { cwd: workingRepo });

      const dispatchedSha = execGit(["rev-parse", "HEAD"], { cwd: workingRepo }).stdout.trim();

      // Agent branch created from the dispatched commit, but no agent commits yet.
      execGit(["checkout", "-b", "agent-bundle-branch-no-commits"], { cwd: workingRepo });

      execGit(["fetch", "origin", "main"], { cwd: workingRepo });

      const origSha = process.env.GITHUB_SHA;
      process.env.GITHUB_SHA = dispatchedSha;
      const restore = setTestEnv(workingRepo);
      try {
        const result = await generateGitBundle("agent-bundle-branch-no-commits", "main", { mode: "full" });

        expect(result.success).toBe(true);
        // Must not use GITHUB_SHA as the base (that would be a zero-commit bundle);
        // the merge-base with main is used instead.
        expect(result.baseCommit).not.toBe(dispatchedSha);
      } finally {
        if (origSha === undefined) {
          delete process.env.GITHUB_SHA;
        } else {
          process.env.GITHUB_SHA = origSha;
        }
        restore();
      }
    });

    it("should choose origin/main as the closest Strategy 3 base in full mode", async () => {
      // Create a stale remote ref that sorts before origin/main.
      execGit(["checkout", "-b", "aaa-stale"], { cwd: workingRepo });
      execGit(["push", "-u", "origin", "aaa-stale"], { cwd: workingRepo });

      // Advance main with commits that should not appear in the generated patch.
      execGit(["checkout", "main"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "phantom1.txt"), "phantom 1\n");
      execGit(["add", "phantom1.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Phantom commit 1"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "phantom2.txt"), "phantom 2\n");
      execGit(["add", "phantom2.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Phantom commit 2"], { cwd: workingRepo });
      execGit(["push", "origin", "main"], { cwd: workingRepo });

      // Agent branch with one actual change on top of current main.
      execGit(["checkout", "-b", "strategy3-branch"], { cwd: workingRepo });
      fs.writeFileSync(path.join(workingRepo, "agent-change.txt"), "real change\n");
      execGit(["add", "agent-change.txt"], { cwd: workingRepo });
      execGit(["commit", "-m", "Agent commit for strategy 3"], { cwd: workingRepo });

      // Keep origin/main and stale refs available for Strategy 3 candidate scoring.
      execGit(["fetch", "origin", "main"], { cwd: workingRepo });
      execGit(["fetch", "origin", "aaa-stale"], { cwd: workingRepo });
      // Ensure the lexicographically first remote ref is stale in this regression setup.
      execGit(["update-ref", "-d", "refs/remotes/origin/HEAD"], { cwd: workingRepo, allowFailure: true });

      // Force full-mode fallthrough into Strategy 3.
      const origSha = process.env.GITHUB_SHA;
      process.env.GITHUB_SHA = "side-repo-sha-not-in-target-repo";

      const restore = setTestEnv(workingRepo);
      try {
        const mainTip = execGit(["rev-parse", "main"], { cwd: workingRepo }).stdout.trim();
        const result = await generateGitPatch("strategy3-branch", "strategy3-branch", { mode: "full" });

        expect(result.success).toBe(true);
        expect(result.baseCommit).toBe(mainTip);

        const patchContent = fs.readFileSync(result.patchPath, "utf8");
        expect(patchContent).toContain("Agent commit for strategy 3");
        expect(patchContent).toContain("agent-change.txt");
        expect(patchContent).not.toContain("Phantom commit 1");
        expect(patchContent).not.toContain("Phantom commit 2");
      } finally {
        if (origSha === undefined) {
          delete process.env.GITHUB_SHA;
        } else {
          process.env.GITHUB_SHA = origSha;
        }
        restore();
      }
    });
  });
});

describe("generateGitBundle hook bypass", () => {
  let remoteDir;
  let workDir;

  beforeEach(() => {
    remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-it-remote-"));
    workDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-it-work-"));
    execGit(["init", "--bare"], { cwd: remoteDir });
    execGit(["clone", remoteDir, workDir]);
    execGit(["config", "user.name", "Test User"], { cwd: workDir });
    execGit(["config", "user.email", "test@example.com"], { cwd: workDir });
  });

  afterEach(() => {
    cleanupTestRepo(remoteDir);
    cleanupTestRepo(workDir);
  });

  it("succeeds when repository post-checkout hook exits non-zero (e.g. git-lfs missing)", async () => {
    // Simulate a repository whose post-checkout hook fails as git-lfs would when
    // git-lfs is not on PATH.  Filtered bundle synthesis creates a temporary worktree
    // internally and must bypass this hook via core.hooksPath.
    fs.writeFileSync(path.join(workDir, "base.txt"), "base\n");
    execGit(["add", "base.txt"], { cwd: workDir });
    execGit(["commit", "-m", "base commit"], { cwd: workDir });
    execGit(["branch", "-M", "main"], { cwd: workDir });
    execGit(["push", "-u", "origin", "main"], { cwd: workDir });

    execGit(["checkout", "-b", "pr-branch"], { cwd: workDir });
    fs.writeFileSync(path.join(workDir, "change.txt"), "pr change\n");
    execGit(["add", "change.txt"], { cwd: workDir });
    execGit(["commit", "-m", "pr commit"], { cwd: workDir });
    execGit(["push", "-u", "origin", "pr-branch"], { cwd: workDir });

    // Add a second (unpushed) commit to force incremental filtered mode.
    fs.writeFileSync(path.join(workDir, "change2.txt"), "second change\n");
    execGit(["add", "change2.txt"], { cwd: workDir });
    execGit(["commit", "-m", "second pr commit"], { cwd: workDir });

    // Install a failing post-checkout hook (simulates git-lfs or similar absent tooling).
    const hooksDir = path.join(workDir, "test-hooks");
    fs.mkdirSync(hooksDir, { recursive: true });
    execGit(["config", "core.hooksPath", hooksDir], { cwd: workDir });
    const hookPath = path.join(hooksDir, "post-checkout");
    fs.writeFileSync(hookPath, "#!/bin/sh\necho 'git-lfs filter-process: git-lfs not found' >&2\nexit 2\n");
    fs.chmodSync(hookPath, 0o755);

    const result = await generateGitBundle("pr-branch", "main", {
      mode: "incremental",
      cwd: workDir,
    });

    expect(result.success).toBe(true);
    expect(result.bundlePath).toBeTruthy();
    expect(fs.statSync(result.bundlePath).size).toBeGreaterThan(0);

    // Verify the bundle contains the expected commits by listing its heads.
    const bundleHeads = execGit(["bundle", "list-heads", result.bundlePath], { cwd: workDir }).stdout;
    expect(bundleHeads).toContain("refs/heads/pr-branch");

    fs.rmSync(result.bundlePath, { force: true });
  });
});
