import { describe, it, expect, afterEach } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

const require = createRequire(import.meta.url);

global.core = {
  debug: () => {},
  error: () => {},
  info: () => {},
  warning: () => {},
};

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

describe("generateGitBundle (incremental)", () => {
  const tempDirs = [];
  const bundlePaths = [];

  afterEach(() => {
    for (const tempDir of tempDirs.splice(0)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
    for (const bundlePath of bundlePaths.splice(0)) {
      fs.rmSync(bundlePath, { force: true });
    }
  });

  it("reduces bundle size by excluding origin/base branch objects already on remote", async () => {
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-remote-"));
    const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-work-"));
    tempDirs.push(remoteDir, workDir);

    execGit(["init", "--bare"], { cwd: remoteDir });
    execGit(["clone", remoteDir, workDir]);
    execGit(["config", "user.name", "Test User"], { cwd: workDir });
    execGit(["config", "user.email", "test@example.com"], { cwd: workDir });

    fs.writeFileSync(path.join(workDir, "base.txt"), "base\n");
    execGit(["add", "base.txt"], { cwd: workDir });
    execGit(["commit", "-m", "base"], { cwd: workDir });
    execGit(["branch", "-M", "main"], { cwd: workDir });
    execGit(["push", "-u", "origin", "main"], { cwd: workDir });

    execGit(["checkout", "-b", "pr-branch"], { cwd: workDir });
    fs.writeFileSync(path.join(workDir, "pr.txt"), "pr start\n");
    execGit(["add", "pr.txt"], { cwd: workDir });
    execGit(["commit", "-m", "pr start"], { cwd: workDir });
    execGit(["push", "-u", "origin", "pr-branch"], { cwd: workDir });

    execGit(["checkout", "main"], { cwd: workDir });
    for (let commitIndex = 0; commitIndex < 4; commitIndex++) {
      fs.writeFileSync(path.join(workDir, `upstream-${commitIndex}.txt`), `upstream ${commitIndex}\n`);
      execGit(["add", `upstream-${commitIndex}.txt`], { cwd: workDir });
      execGit(["commit", "-m", `upstream ${commitIndex}`], { cwd: workDir });
    }
    execGit(["push", "origin", "main"], { cwd: workDir });

    execGit(["checkout", "pr-branch"], { cwd: workDir });
    execGit(["merge", "--no-ff", "origin/main", "-m", "merge main into pr"], { cwd: workDir });
    fs.writeFileSync(path.join(workDir, "resolution.txt"), "resolved\n");
    execGit(["add", "resolution.txt"], { cwd: workDir });
    execGit(["commit", "-m", "resolution commit"], { cwd: workDir });

    const { generateGitBundle } = require("./generate_git_bundle.cjs");
    const result = await generateGitBundle("pr-branch", "main", { mode: "incremental", cwd: workDir });
    expect(result.success).toBe(true);
    expect(result.bundlePath).toBeTruthy();
    bundlePaths.push(result.bundlePath);

    const naiveBundlePath = path.join(workDir, "naive.bundle");
    const optimizedBundlePath = path.join(workDir, "optimized.bundle");
    execGit(["bundle", "create", naiveBundlePath, "origin/pr-branch..pr-branch"], { cwd: workDir });
    execGit(["bundle", "create", optimizedBundlePath, "origin/pr-branch..pr-branch", "^origin/main"], { cwd: workDir });

    const prBranchHeadSha = execGit(["rev-parse", "pr-branch"], { cwd: workDir }).stdout.trim();
    const generatedBundleHeads = execGit(["bundle", "list-heads", result.bundlePath], { cwd: workDir }).stdout.trim();
    const optimizedBundleHeads = execGit(["bundle", "list-heads", optimizedBundlePath], { cwd: workDir }).stdout.trim();
    const generatedSize = fs.statSync(result.bundlePath).size;
    const naiveSize = fs.statSync(naiveBundlePath).size;

    expect(prBranchHeadSha).toBeTruthy();
    expect(prBranchHeadSha).toMatch(/^[a-f0-9]{40}$/);
    expect(optimizedBundleHeads).toContain(prBranchHeadSha);
    expect(generatedBundleHeads).toBe(optimizedBundleHeads);
    expect(generatedSize).toBeLessThan(naiveSize);
  });

  it("falls back to non-exclusion bundle generation when origin/base branch is unavailable", async () => {
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-remote-"));
    const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-work-"));
    tempDirs.push(remoteDir, workDir);

    execGit(["init", "--bare"], { cwd: remoteDir });
    execGit(["clone", remoteDir, workDir]);
    execGit(["config", "user.name", "Test User"], { cwd: workDir });
    execGit(["config", "user.email", "test@example.com"], { cwd: workDir });

    execGit(["checkout", "-b", "pr-branch"], { cwd: workDir });
    fs.writeFileSync(path.join(workDir, "pr.txt"), "pr start\n");
    execGit(["add", "pr.txt"], { cwd: workDir });
    execGit(["commit", "-m", "pr start"], { cwd: workDir });
    execGit(["push", "-u", "origin", "pr-branch"], { cwd: workDir });

    fs.writeFileSync(path.join(workDir, "pr-2.txt"), "pr second\n");
    execGit(["add", "pr-2.txt"], { cwd: workDir });
    execGit(["commit", "-m", "pr second"], { cwd: workDir });

    const { generateGitBundle } = require("./generate_git_bundle.cjs");
    const result = await generateGitBundle("pr-branch", "main", { mode: "incremental", cwd: workDir });
    expect(result.success).toBe(true);
    expect(result.bundlePath).toBeTruthy();
    bundlePaths.push(result.bundlePath);

    const naiveBundlePath = path.join(workDir, "naive.bundle");
    execGit(["bundle", "create", naiveBundlePath, "origin/pr-branch..pr-branch"], { cwd: workDir });
    expect(fs.existsSync(naiveBundlePath)).toBe(true);

    const generatedBundleHeads = execGit(["bundle", "list-heads", result.bundlePath], { cwd: workDir }).stdout.trim();
    const naiveBundleHeads = execGit(["bundle", "list-heads", naiveBundlePath], { cwd: workDir }).stdout.trim();

    expect(generatedBundleHeads).toBe(naiveBundleHeads);
  });

  it("uses explicit PR-head baseline when a fork PR has no origin/<branch> ref", async () => {
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-remote-"));
    const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-work-"));
    tempDirs.push(remoteDir, workDir);

    execGit(["init", "--bare"], { cwd: remoteDir });
    execGit(["clone", remoteDir, workDir]);
    execGit(["config", "user.name", "Test User"], { cwd: workDir });
    execGit(["config", "user.email", "test@example.com"], { cwd: workDir });

    fs.writeFileSync(path.join(workDir, "base.txt"), "base\n");
    execGit(["add", "base.txt"], { cwd: workDir });
    execGit(["commit", "-m", "base"], { cwd: workDir });
    execGit(["branch", "-M", "main"], { cwd: workDir });
    execGit(["push", "-u", "origin", "main"], { cwd: workDir });

    execGit(["checkout", "-b", "feature/fork-only"], { cwd: workDir });
    fs.writeFileSync(path.join(workDir, "fork.txt"), "fork PR head\n");
    execGit(["add", "fork.txt"], { cwd: workDir });
    execGit(["commit", "-m", "fork PR head"], { cwd: workDir });
    const prHeadSha = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();
    execGit(["update-ref", "refs/remotes/origin/pr-head", prHeadSha], { cwd: workDir });

    fs.writeFileSync(path.join(workDir, "agent.txt"), "agent follow-up\n");
    execGit(["add", "agent.txt"], { cwd: workDir });
    execGit(["commit", "-m", "agent follow-up"], { cwd: workDir });

    expect(execGit(["rev-parse", "--verify", "refs/remotes/origin/feature/fork-only"], { cwd: workDir, allowFailure: true }).status).not.toBe(0);

    const { generateGitBundle } = require("./generate_git_bundle.cjs");
    const result = await generateGitBundle("feature/fork-only", "main", {
      mode: "incremental",
      cwd: workDir,
      incrementalBaseRef: "refs/remotes/origin/pr-head",
      incrementalBaseSha: prHeadSha,
    });
    expect(result.success).toBe(true);
    expect(result.baseCommit).toBe(prHeadSha);
    bundlePaths.push(result.bundlePath);

    const bundleHeads = execGit(["bundle", "list-heads", result.bundlePath], { cwd: workDir }).stdout.trim();
    const currentHead = execGit(["rev-parse", "feature/fork-only"], { cwd: workDir }).stdout.trim();
    expect(bundleHeads).toContain(currentHead);
  });

  it("includes refs/heads/<branchName> in bundle when agent is on the target branch (non-main dispatch scenario)", async () => {
    // Simulates: scanner dispatches worker from a feature branch (non-main ref).
    // The worker checks out the feature branch, creates a new fix branch, commits on
    // it, then calls create_pull_request.  In this scenario, HEAD is on fix-branch
    // when generateGitBundle is called.  Strategy 1 (merge-base) fails in a shallow
    // clone because the common ancestor of main and the fix branch is beyond the
    // shallow boundary.  Strategy 2 must then produce a bundle that includes
    // refs/heads/fix-branch (not just HEAD) so applyBundleToBranch can locate the ref.
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-nonmain-remote-"));
    const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-nonmain-work-"));
    tempDirs.push(remoteDir, workDir);

    // Build origin: main and feature-branch diverge from a common ancestor
    execGit(["init", "--bare"], { cwd: remoteDir });
    const seedDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-nonmain-seed-"));
    tempDirs.push(seedDir);
    execGit(["clone", remoteDir, seedDir]);
    execGit(["config", "user.name", "Test User"], { cwd: seedDir });
    execGit(["config", "user.email", "test@example.com"], { cwd: seedDir });

    // Common ancestor commit A
    fs.writeFileSync(path.join(seedDir, "base.txt"), "base\n");
    execGit(["add", "base.txt"], { cwd: seedDir });
    execGit(["commit", "-m", "common ancestor"], { cwd: seedDir });
    execGit(["branch", "-M", "main"], { cwd: seedDir });
    execGit(["push", "-u", "origin", "main"], { cwd: seedDir });

    // Advance main with extra commits so its tip diverges from feature-branch
    fs.writeFileSync(path.join(seedDir, "main-extra.txt"), "main-extra\n");
    execGit(["add", "main-extra.txt"], { cwd: seedDir });
    execGit(["commit", "-m", "main advance"], { cwd: seedDir });
    execGit(["push", "origin", "main"], { cwd: seedDir });

    // Create feature-branch from common ancestor (before main diverged)
    execGit(["checkout", "-b", "feature-branch", "HEAD~1"], { cwd: seedDir });
    fs.writeFileSync(path.join(seedDir, "feature.txt"), "feature\n");
    execGit(["add", "feature.txt"], { cwd: seedDir });
    execGit(["commit", "-m", "feature commit"], { cwd: seedDir });
    const featureBranchTip = execGit(["rev-parse", "HEAD"], { cwd: seedDir }).stdout.trim();
    execGit(["push", "-u", "origin", "feature-branch"], { cwd: seedDir });

    // Simulate Actions checkout: shallow clone of feature-branch (depth=1)
    execGit(["clone", "--depth=1", "--no-local", "--branch=feature-branch", remoteDir, workDir]);
    execGit(["config", "user.name", "Test User"], { cwd: workDir });
    execGit(["config", "user.email", "test@example.com"], { cwd: workDir });

    // Worker agent creates and checks out a new fix branch
    execGit(["checkout", "-b", "fix-branch"], { cwd: workDir });

    // Agent makes new commits on fix-branch
    fs.writeFileSync(path.join(workDir, "fix1.txt"), "fix 1\n");
    execGit(["add", "fix1.txt"], { cwd: workDir });
    execGit(["commit", "-m", "fix commit 1"], { cwd: workDir });
    fs.writeFileSync(path.join(workDir, "fix2.txt"), "fix 2\n");
    execGit(["add", "fix2.txt"], { cwd: workDir });
    execGit(["commit", "-m", "fix commit 2"], { cwd: workDir });

    const fixBranchHead = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

    const savedGithubSha = process.env.GITHUB_SHA;
    try {
      // GITHUB_SHA is the feature-branch tip at workflow trigger time
      process.env.GITHUB_SHA = featureBranchTip;

      const { generateGitBundle } = require("./generate_git_bundle.cjs");
      const result = await generateGitBundle("fix-branch", "main", { mode: "full", cwd: workDir });
      expect(result.success).toBe(true);
      expect(result.bundlePath).toBeTruthy();
      bundlePaths.push(result.bundlePath);

      // The bundle MUST contain refs/heads/fix-branch so applyBundleToBranch can locate the ref
      const bundleHeads = execGit(["bundle", "list-heads", result.bundlePath], { cwd: workDir }).stdout.trim();
      expect(bundleHeads).toContain(fixBranchHead);
      expect(bundleHeads).toContain("refs/heads/fix-branch");
    } finally {
      if (savedGithubSha === undefined) {
        delete process.env.GITHUB_SHA;
      } else {
        process.env.GITHUB_SHA = savedGithubSha;
      }
    }
  });

  it("generates a filtered bundle even when the repository configures failing checkout and apply-patch hooks", async () => {
    // Repository hooks requiring tooling absent from the safe-outputs environment must not
    // break filtered bundle synthesis, which reuses an index-only synthesized commit.
    const remoteDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-hooks-remote-"));
    const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-bundle-hooks-work-"));
    tempDirs.push(remoteDir, workDir);

    execGit(["init", "--bare"], { cwd: remoteDir });
    execGit(["clone", remoteDir, workDir]);
    execGit(["config", "user.name", "Test User"], { cwd: workDir });
    execGit(["config", "user.email", "test@example.com"], { cwd: workDir });

    fs.writeFileSync(path.join(workDir, "base.txt"), "base\n");
    execGit(["add", "base.txt"], { cwd: workDir });
    execGit(["commit", "-m", "base"], { cwd: workDir });
    execGit(["branch", "-M", "main"], { cwd: workDir });
    execGit(["push", "-u", "origin", "main"], { cwd: workDir });

    execGit(["checkout", "-b", "pr-branch"], { cwd: workDir });
    fs.writeFileSync(path.join(workDir, "pr.txt"), "pr change\n");
    fs.writeFileSync(path.join(workDir, "secret.txt"), "secret\n");
    execGit(["add", "pr.txt", "secret.txt"], { cwd: workDir });
    execGit(["commit", "-m", "pr commit"], { cwd: workDir });
    execGit(["push", "-u", "origin", "pr-branch"], { cwd: workDir });

    fs.writeFileSync(path.join(workDir, "pr-2.txt"), "pr second\n");
    execGit(["add", "pr-2.txt"], { cwd: workDir });
    execGit(["commit", "-m", "pr second"], { cwd: workDir });

    // Use an explicit hooks path so global Git configuration cannot bypass the regression.
    const hooksDir = path.join(workDir, "configured-hooks");
    fs.mkdirSync(hooksDir, { recursive: true });
    execGit(["config", "core.hooksPath", hooksDir], { cwd: workDir });
    for (const hookName of ["post-checkout", "pre-applypatch"]) {
      const hookPath = path.join(hooksDir, hookName);
      fs.writeFileSync(hookPath, `#!/bin/sh\necho '${hookName} dependency not found' >&2\nexit 2\n`);
      fs.chmodSync(hookPath, 0o755);
    }

    const { generateGitBundle } = require("./generate_git_bundle.cjs");
    const result = await generateGitBundle("pr-branch", "main", {
      mode: "incremental",
      cwd: workDir,
      excludedFiles: ["secret.txt"],
    });

    expect(result.success).toBe(true);
    expect(result.bundlePath).toBeTruthy();
    bundlePaths.push(result.bundlePath);
    expect(fs.statSync(result.bundlePath).size).toBeGreaterThan(0);
  });

  it("returns actionable guidance when branch is missing in incremental mode", async () => {
    const { generateGitBundle } = require("./generate_git_bundle.cjs");

    const result = await generateGitBundle("feature-branch", "main", {
      mode: "incremental",
      cwd: "/tmp/nonexistent-repo",
    });

    expect(result.success).toBe(false);
    expect(result.error).toContain("correct repository");
    expect(result.error).toContain("GITHUB_WORKSPACE");
    expect(result.error).not.toContain("/tmp/nonexistent-repo");
  });
});
