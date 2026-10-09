import { describe, it, expect, afterEach, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

const require = createRequire(import.meta.url);
const { getBundlePreApplyFiles } = require("./push_to_pull_request_branch.cjs");
const { checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");

global.core = {
  debug: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
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

function createRepo(prefix) {
  const repoDir = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  execGit(["init"], { cwd: repoDir });
  execGit(["config", "user.name", "Test User"], { cwd: repoDir });
  execGit(["config", "user.email", "test@example.com"], { cwd: repoDir });
  return repoDir;
}

function writeRepoFile(repoDir, relativePath, content) {
  const fullPath = path.join(repoDir, relativePath);
  fs.mkdirSync(path.dirname(fullPath), { recursive: true });
  fs.writeFileSync(fullPath, content);
}

function createExecApi(cwd) {
  return {
    async getExecOutput(command, args = [], options = {}) {
      if (command !== "git") {
        throw new Error(`unexpected command: ${command}`);
      }
      const result = execGit(args, { cwd, allowFailure: true });
      if (result.status !== 0 && !options.ignoreReturnCode) {
        throw new Error(result.stderr || result.stdout);
      }
      return { exitCode: result.status, stdout: result.stdout, stderr: result.stderr };
    },
  };
}

function fetchBaseCommit(targetRepo, sourceRepo, baseSha, branchName) {
  execGit(["remote", "add", "origin", sourceRepo], { cwd: targetRepo });
  execGit(["fetch", "origin", baseSha], { cwd: targetRepo });
  execGit(["checkout", "-b", branchName, "FETCH_HEAD"], { cwd: targetRepo });
}

describe("push_to_pull_request_branch bundle integration", () => {
  const tempDirs = [];

  afterEach(() => {
    for (const tempDir of tempDirs.splice(0)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
    vi.clearAllMocks();
  });

  it("lists files from a fetched bundle before applying it", async () => {
    const branchName = "autoloop/simple-bundle";
    const sourceRepo = createRepo("push-pr-bundle-source-");
    const targetRepo = createRepo("push-pr-bundle-target-");
    tempDirs.push(sourceRepo, targetRepo);

    writeRepoFile(sourceRepo, "README.md", "base\n");
    execGit(["add", "README.md"], { cwd: sourceRepo });
    execGit(["commit", "-m", "base"], { cwd: sourceRepo });
    execGit(["branch", "-M", "main"], { cwd: sourceRepo });
    const baseSha = execGit(["rev-parse", "HEAD"], { cwd: sourceRepo }).stdout.trim();

    execGit(["checkout", "-b", branchName], { cwd: sourceRepo });
    writeRepoFile(sourceRepo, ".changeset/fix.md", "patch\n");
    writeRepoFile(sourceRepo, "docs/guide.md", "guide\n");
    execGit(["add", ".changeset/fix.md", "docs/guide.md"], { cwd: sourceRepo });
    execGit(["commit", "-m", "bundle change"], { cwd: sourceRepo });

    const bundlePath = path.join(sourceRepo, "bundle.bundle");
    execGit(["bundle", "create", bundlePath, `refs/heads/${branchName}`], { cwd: sourceRepo });

    fetchBaseCommit(targetRepo, sourceRepo, baseSha, branchName);
    const bundleRef = "refs/bundles/test-simple-bundle";
    execGit(["fetch", bundlePath, `refs/heads/${branchName}:${bundleRef}`], { cwd: targetRepo });

    const actualFiles = await getBundlePreApplyFiles(createExecApi(targetRepo), {}, baseSha, bundleRef);

    expect(actualFiles.sort()).toEqual([".changeset/fix.md", "docs/guide.md"]);
  });

  it("checks only bundle commits added after an existing PR head", async () => {
    const branchName = "feature/existing-pr";
    const sourceRepo = createRepo("push-pr-existing-source-");
    const targetRepo = createRepo("push-pr-existing-target-");
    tempDirs.push(sourceRepo, targetRepo);

    writeRepoFile(sourceRepo, "README.md", "base\n");
    execGit(["add", "README.md"], { cwd: sourceRepo });
    execGit(["commit", "-m", "base"], { cwd: sourceRepo });
    execGit(["checkout", "-b", branchName], { cwd: sourceRepo });
    writeRepoFile(sourceRepo, "tools/__pycache__/author.pyc", "author file\n");
    execGit(["add", "tools/__pycache__/author.pyc"], { cwd: sourceRepo });
    execGit(["commit", "-m", "author change"], { cwd: sourceRepo });
    const existingHead = execGit(["rev-parse", "HEAD"], { cwd: sourceRepo }).stdout.trim();

    writeRepoFile(sourceRepo, "config/agent.json", "{}\n");
    execGit(["add", "config/agent.json"], { cwd: sourceRepo });
    execGit(["commit", "-m", "agent change"], { cwd: sourceRepo });
    const bundlePath = path.join(sourceRepo, "incremental.bundle");
    execGit(["bundle", "create", bundlePath, `refs/heads/${branchName}`], { cwd: sourceRepo });

    fetchBaseCommit(targetRepo, sourceRepo, existingHead, branchName);
    const bundleRef = "refs/bundles/test-existing-pr";
    execGit(["fetch", bundlePath, `refs/heads/${branchName}:${bundleRef}`], { cwd: targetRepo });
    const files = await getBundlePreApplyFiles(createExecApi(targetRepo), {}, existingHead, bundleRef);

    expect(files).toEqual(["config/agent.json"]);
    expect(checkFileProtectionPostApply(files, { allowed_files: ["**/*.json"] }).action).toBe("allow");
  });

  it("fetches a HEAD-only bundle after its named branch ref is absent", () => {
    const branchName = "autoloop/head-only-bundle";
    const sourceRepo = createRepo("push-pr-head-only-source-");
    const targetRepo = createRepo("push-pr-head-only-target-");
    tempDirs.push(sourceRepo, targetRepo);

    writeRepoFile(sourceRepo, "README.md", "base\n");
    execGit(["add", "README.md"], { cwd: sourceRepo });
    execGit(["commit", "-m", "base"], { cwd: sourceRepo });
    execGit(["branch", "-M", "main"], { cwd: sourceRepo });
    const baseSha = execGit(["rev-parse", "HEAD"], { cwd: sourceRepo }).stdout.trim();

    execGit(["checkout", "-b", branchName], { cwd: sourceRepo });
    writeRepoFile(sourceRepo, "head-only.txt", "bundle change\n");
    execGit(["add", "head-only.txt"], { cwd: sourceRepo });
    execGit(["commit", "-m", "HEAD-only bundle change"], { cwd: sourceRepo });
    const bundleHead = execGit(["rev-parse", "HEAD"], { cwd: sourceRepo }).stdout.trim();

    const bundlePath = path.join(sourceRepo, "head-only.bundle");
    execGit(["bundle", "create", bundlePath, "HEAD"], { cwd: sourceRepo });

    fetchBaseCommit(targetRepo, sourceRepo, baseSha, branchName);
    const bundleRef = "refs/bundles/test-head-only-bundle";
    const namedRefFetch = execGit(["fetch", bundlePath, `refs/heads/${branchName}:${bundleRef}`], { cwd: targetRepo, allowFailure: true });
    expect(namedRefFetch.status).not.toBe(0);
    expect(execGit(["bundle", "list-heads", bundlePath], { cwd: targetRepo }).stdout).toBe(`${bundleHead} HEAD\n`);

    execGit(["fetch", bundlePath, `HEAD:${bundleRef}`], { cwd: targetRepo });

    expect(execGit(["rev-parse", bundleRef], { cwd: targetRepo }).stdout.trim()).toBe(bundleHead);
  });

  it("includes files introduced through merge-commit bundle history", async () => {
    const branchName = "autoloop/merge-bundle";
    const sourceRepo = createRepo("push-pr-merge-source-");
    const targetRepo = createRepo("push-pr-merge-target-");
    tempDirs.push(sourceRepo, targetRepo);

    writeRepoFile(sourceRepo, "README.md", "base\n");
    execGit(["add", "README.md"], { cwd: sourceRepo });
    execGit(["commit", "-m", "base"], { cwd: sourceRepo });
    execGit(["branch", "-M", "main"], { cwd: sourceRepo });
    const baseSha = execGit(["rev-parse", "HEAD"], { cwd: sourceRepo }).stdout.trim();

    execGit(["checkout", "-b", "feature"], { cwd: sourceRepo });
    writeRepoFile(sourceRepo, "feature.txt", "feature branch change\n");
    execGit(["add", "feature.txt"], { cwd: sourceRepo });
    execGit(["commit", "-m", "feature commit"], { cwd: sourceRepo });

    execGit(["checkout", "main"], { cwd: sourceRepo });
    writeRepoFile(sourceRepo, "main.txt", "main branch change\n");
    execGit(["add", "main.txt"], { cwd: sourceRepo });
    execGit(["commit", "-m", "main commit"], { cwd: sourceRepo });
    execGit(["merge", "--no-ff", "feature", "-m", "merge feature"], { cwd: sourceRepo });
    execGit(["checkout", "-b", branchName], { cwd: sourceRepo });

    const bundlePath = path.join(sourceRepo, "merge.bundle");
    execGit(["bundle", "create", bundlePath, `refs/heads/${branchName}`], { cwd: sourceRepo });

    fetchBaseCommit(targetRepo, sourceRepo, baseSha, branchName);
    const bundleRef = "refs/bundles/test-merge-bundle";
    execGit(["fetch", bundlePath, `refs/heads/${branchName}:${bundleRef}`], { cwd: targetRepo });

    const actualFiles = await getBundlePreApplyFiles(createExecApi(targetRepo), {}, baseSha, bundleRef);

    expect(actualFiles.sort()).toEqual(["feature.txt", "main.txt"]);
  });

  it("resolves filenames that git would otherwise quote (non-ASCII characters)", async () => {
    const branchName = "autoloop/quoted-path-bundle";
    const sourceRepo = createRepo("push-pr-quoted-source-");
    const targetRepo = createRepo("push-pr-quoted-target-");
    tempDirs.push(sourceRepo, targetRepo);

    writeRepoFile(sourceRepo, "README.md", "base\n");
    execGit(["add", "README.md"], { cwd: sourceRepo });
    execGit(["commit", "-m", "base"], { cwd: sourceRepo });
    execGit(["branch", "-M", "main"], { cwd: sourceRepo });
    const baseSha = execGit(["rev-parse", "HEAD"], { cwd: sourceRepo }).stdout.trim();

    execGit(["checkout", "-b", branchName], { cwd: sourceRepo });
    const quotedFileName = "résumé.txt";
    writeRepoFile(sourceRepo, quotedFileName, "quoted path change\n");
    execGit(["add", quotedFileName], { cwd: sourceRepo });
    execGit(["commit", "-m", "quoted path change"], { cwd: sourceRepo });

    const bundlePath = path.join(sourceRepo, "quoted.bundle");
    execGit(["bundle", "create", bundlePath, `refs/heads/${branchName}`], { cwd: sourceRepo });

    fetchBaseCommit(targetRepo, sourceRepo, baseSha, branchName);
    const bundleRef = "refs/bundles/test-quoted-bundle";
    execGit(["fetch", bundlePath, `refs/heads/${branchName}:${bundleRef}`], { cwd: targetRepo });

    const actualFiles = await getBundlePreApplyFiles(createExecApi(targetRepo), {}, baseSha, bundleRef);

    // Without NUL-delimited (`-z`) diff output, git would quote this path (e.g. "r\303\251sum\303\251.txt"),
    // causing downstream filesystem lookups keyed on the raw name to fail and silently drop the file
    // from size/protection checks.
    expect(actualFiles).toEqual([quotedFileName]);
  });
});
