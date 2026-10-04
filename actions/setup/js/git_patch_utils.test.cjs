/**
 * Tests for git_patch_utils.cjs
 *
 * Pure helpers (sanitize, path, pathspec) are tested as simple unit tests.
 * `computeIncrementalDiffSize` is tested against a REAL local git repository
 * created in a temp directory, so it exercises `git diff --output=<file>`,
 * the O(1) stat-based size measurement, and temp-file cleanup end-to-end.
 *
 * These tests require `git` to be installed on the runner.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

import {
  sanitizeForFilename,
  sanitizeBranchNameForPatch,
  sanitizeRepoSlugForPatch,
  getPatchPathForBranch,
  getPatchPathForBranchInRepo,
  buildExcludePathspecs,
  computeIncrementalDiffSize,
  getPatchDiffSizeBytes,
  getStagedPatchDiffSizeBytes,
  isAncestorCommit,
  isPartialClone,
  describeGitFailure,
} from "./git_patch_utils.cjs";

// computeIncrementalDiffSize delegates to execGitSync from git_helpers.cjs,
// which calls the GitHub Actions `core.debug` / `core.error` globals. Stub
// them so this test suite can run outside of a workflow runner.
global.core = {
  debug: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
};

function execGit(args, options = {}) {
  const result = spawnSync("git", args, { encoding: "utf8", ...options });
  if (result.error) throw result.error;
  if (result.status !== 0 && !options.allowFailure) {
    throw new Error(`git ${args.join(" ")} failed: ${result.stderr}`);
  }
  return result;
}

function createTestRepo() {
  const repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "git-patch-utils-"));
  execGit(["init", "-q"], { cwd: repoDir });
  execGit(["config", "user.name", "Test"], { cwd: repoDir });
  execGit(["config", "user.email", "test@example.com"], { cwd: repoDir });
  execGit(["config", "commit.gpgsign", "false"], { cwd: repoDir });
  fs.writeFileSync(path.join(repoDir, "README.md"), "# Test\n");
  execGit(["add", "."], { cwd: repoDir });
  execGit(["commit", "-q", "-m", "Initial commit"], { cwd: repoDir });
  execGit(["branch", "-M", "main"], { cwd: repoDir });
  return repoDir;
}

function cleanupRepo(repoDir) {
  if (repoDir && fs.existsSync(repoDir)) {
    fs.rmSync(repoDir, { recursive: true, force: true });
  }
}

describe("git_patch_utils - pure helpers", () => {
  describe("sanitizeForFilename", () => {
    it("returns fallback when value is empty or nullish", () => {
      expect(sanitizeForFilename("", "fallback")).toBe("fallback");
      expect(sanitizeForFilename(null, "fallback")).toBe("fallback");
      expect(sanitizeForFilename(undefined, "fallback")).toBe("fallback");
    });

    it("replaces path separators and special characters with dashes", () => {
      expect(sanitizeForFilename("feat/foo", "x")).toBe("feat-foo");
      expect(sanitizeForFilename('a\\b:c*d?e"f<g>h|i', "x")).toBe("a-b-c-d-e-f-g-h-i");
    });

    it("collapses runs of dashes and trims leading/trailing dashes", () => {
      expect(sanitizeForFilename("--foo//bar--", "x")).toBe("foo-bar");
    });

    it("lowercases the result", () => {
      expect(sanitizeForFilename("Feature/Mixed-Case", "x")).toBe("feature-mixed-case");
    });
  });

  describe("sanitizeBranchNameForPatch / sanitizeRepoSlugForPatch", () => {
    it("uses 'unknown' fallback for empty branch names", () => {
      expect(sanitizeBranchNameForPatch("")).toBe("unknown");
    });
    it("uses empty fallback for empty repo slugs", () => {
      expect(sanitizeRepoSlugForPatch("")).toBe("");
    });
    it("sanitizes owner/repo slugs", () => {
      expect(sanitizeRepoSlugForPatch("github/Gh-AW")).toBe("github-gh-aw");
    });
  });

  describe("getPatchPathForBranch / getPatchPathForBranchInRepo", () => {
    it("returns the /tmp/gh-aw path with the sanitized branch name", () => {
      expect(getPatchPathForBranch("feat/foo")).toBe("/tmp/gh-aw/aw-feat-foo.patch");
    });
    it("includes a sanitized repo slug for multi-repo scenarios", () => {
      expect(getPatchPathForBranchInRepo("feat/foo", "github/gh-aw")).toBe("/tmp/gh-aw/aw-github-gh-aw-feat-foo.patch");
    });
  });

  describe("buildExcludePathspecs", () => {
    it("returns [] for undefined/null/empty inputs", () => {
      expect(buildExcludePathspecs(undefined)).toEqual([]);
      expect(buildExcludePathspecs(null)).toEqual([]);
      expect(buildExcludePathspecs([])).toEqual([]);
    });

    it("produces [--, :(exclude)<pat>, ...] for non-empty inputs", () => {
      expect(buildExcludePathspecs(["*.lock", "dist/**"])).toEqual(["--", ":(exclude)*.lock", ":(exclude)dist/**"]);
    });

    it("ignores non-array inputs (returns [])", () => {
      // @ts-expect-error - exercising runtime guard
      expect(buildExcludePathspecs("not-an-array")).toEqual([]);
    });
  });
});

describe("git_patch_utils.computeIncrementalDiffSize - real git repo", () => {
  let repoDir;

  beforeEach(() => {
    repoDir = createTestRepo();
  });

  afterEach(() => {
    cleanupRepo(repoDir);
  });

  it("returns a positive size that matches the actual diff bytes for a single-file change", () => {
    const baseSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    const body = "line\n".repeat(200); // deterministic content
    fs.writeFileSync(path.join(repoDir, "file.txt"), body);
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "add file"], { cwd: repoDir });
    const headSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();

    const tmpPath = path.join(repoDir, ".diffsize.tmp");
    const size = computeIncrementalDiffSize({
      baseRef: baseSha,
      headRef: headSha,
      cwd: repoDir,
      tmpPath,
    });

    // Cross-check against `git diff` run independently.
    const expected = Buffer.byteLength(execGit(["diff", "--binary", `${baseSha}..${headSha}`], { cwd: repoDir }).stdout, "utf8");

    expect(size).toBe(expected);
    expect(size).toBeGreaterThan(body.length - 50); // at minimum ~the file contents
  });

  it("always cleans up the temp file, even on success", () => {
    const baseSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    fs.writeFileSync(path.join(repoDir, "b.txt"), "hello\n");
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "add b"], { cwd: repoDir });
    const headSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();

    const tmpPath = path.join(repoDir, ".diffsize.tmp");
    computeIncrementalDiffSize({ baseRef: baseSha, headRef: headSha, cwd: repoDir, tmpPath });

    expect(fs.existsSync(tmpPath)).toBe(false);
  });

  it("returns 0 when the two refs are identical (empty net diff)", () => {
    const sha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    const tmpPath = path.join(repoDir, ".diffsize.tmp");
    const size = computeIncrementalDiffSize({ baseRef: sha, headRef: sha, cwd: repoDir, tmpPath });
    expect(size).toBe(0);
    expect(fs.existsSync(tmpPath)).toBe(false);
  });

  it("honors excludedFiles pathspecs (excluded content does not contribute to diff size)", () => {
    const baseSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    // Two files: one kept, one excluded. The excluded file is much larger.
    fs.writeFileSync(path.join(repoDir, "keep.txt"), "keep\n");
    fs.writeFileSync(path.join(repoDir, "big.lock"), "x".repeat(20 * 1024));
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "add both"], { cwd: repoDir });
    const headSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();

    const tmpPath = path.join(repoDir, ".diffsize.tmp");
    const sizeWithExclude = computeIncrementalDiffSize({
      baseRef: baseSha,
      headRef: headSha,
      cwd: repoDir,
      tmpPath,
      excludedFiles: ["*.lock"],
    });
    const sizeNoExclude = computeIncrementalDiffSize({
      baseRef: baseSha,
      headRef: headSha,
      cwd: repoDir,
      tmpPath,
    });

    expect(sizeWithExclude).toBeGreaterThan(0);
    expect(sizeNoExclude).toBeGreaterThan(sizeWithExclude);
    // The excluded 20 KB lock file should account for the majority of the delta.
    expect(sizeNoExclude - sizeWithExclude).toBeGreaterThan(10 * 1024);
  });

  it("returns null for an invalid baseRef (and still cleans up)", () => {
    const tmpPath = path.join(repoDir, ".diffsize.tmp");
    const size = computeIncrementalDiffSize({
      baseRef: "does-not-exist-ref",
      headRef: "HEAD",
      cwd: repoDir,
      tmpPath,
    });
    expect(size).toBeNull();
    expect(fs.existsSync(tmpPath)).toBe(false);
  });

  it("returns null when required arguments are missing", () => {
    expect(computeIncrementalDiffSize({ baseRef: "", headRef: "HEAD", cwd: "/tmp", tmpPath: "/tmp/x" })).toBeNull();
    expect(computeIncrementalDiffSize({ baseRef: "HEAD", headRef: "", cwd: "/tmp", tmpPath: "/tmp/x" })).toBeNull();
    expect(computeIncrementalDiffSize({ baseRef: "HEAD", headRef: "HEAD", cwd: "", tmpPath: "/tmp/x" })).toBeNull();
    expect(computeIncrementalDiffSize({ baseRef: "HEAD", headRef: "HEAD", cwd: "/tmp", tmpPath: "" })).toBeNull();
  });
});

describe("getPatchDiffSizeBytes", () => {
  it("returns 0 for an empty diff", () => {
    expect(getPatchDiffSizeBytes("")).toBe(0);
  });

  it("counts only addition bytes for a new file (first push)", () => {
    const diff = ["diff --git a/history.jsonl b/history.jsonl", "new file mode 100644", "index 0000000..abc1234", "--- /dev/null", "+++ b/history.jsonl", "@@ -0,0 +1,2 @@", '+{"event":"a"}', '+{"event":"b"}'].join("\n");
    const result = getPatchDiffSizeBytes(diff);
    // Both lines are additions with no deletions → net = additions
    const expected = Buffer.byteLength('+{"event":"a"}\n', "utf8") + Buffer.byteLength('+{"event":"b"}\n', "utf8");
    expect(result).toBe(expected);
  });

  it("returns a small net value when a file is appended (not rewritten)", () => {
    // Simulates appending one new line to an existing JSONL file
    const diff = ["diff --git a/history.jsonl b/history.jsonl", "index abc1234..def5678 100644", "--- a/history.jsonl", "+++ b/history.jsonl", "@@ -1,2 +1,3 @@", ' {"event":"a"}', ' {"event":"b"}', '+{"event":"c"}'].join("\n");
    const result = getPatchDiffSizeBytes(diff);
    // Only the added line contributes; no deletions
    const expected = Buffer.byteLength('+{"event":"c"}\n', "utf8");
    expect(result).toBe(expected);
  });

  it("returns near-zero for a complete rewrite with the same size (the key bug fix)", () => {
    // Simulates a JSON object completely regenerated with equivalent size.
    // Previously getAddedPatchSizeBytesFromDiff would return the full new-file
    // size ("entire source code size").  getPatchDiffSizeBytes returns the net
    // change (additions − deletions), which is ≈ 0 for same-size rewrites.
    const oldLine = '{"key":"old_value_aaaa"}';
    const newLine = '{"key":"new_value_bbbb"}';
    const diff = ["diff --git a/state.json b/state.json", "index abc1234..def5678 100644", "--- a/state.json", "+++ b/state.json", "@@ -1 +1 @@", `-${oldLine}`, `+${newLine}`].join("\n");
    const result = getPatchDiffSizeBytes(diff);
    const additionBytes = Buffer.byteLength(`+${newLine}\n`, "utf8");
    const deletionBytes = Buffer.byteLength(`-${oldLine}\n`, "utf8");
    // Both lines have the same length, so net ≈ 0
    expect(result).toBe(Math.max(0, additionBytes - deletionBytes));
    // Explicitly: this should NOT equal the full new-file size
    expect(result).toBeLessThan(additionBytes);
  });

  it("returns 0 when deletions exceed additions (content shrinks)", () => {
    const diff = [
      "diff --git a/state.json b/state.json",
      "index abc1234..def5678 100644",
      "--- a/state.json",
      "+++ b/state.json",
      "@@ -1,3 +1,1 @@",
      '-{"key":"value_one"}',
      '-{"key":"value_two"}',
      '-{"key":"value_three"}',
      '+{"key":"v"}',
    ].join("\n");
    const result = getPatchDiffSizeBytes(diff);
    // Deletions > additions → clamped to 0
    expect(result).toBe(0);
  });

  it("handles multiple files in one diff", () => {
    const diff = [
      "diff --git a/a.json b/a.json",
      "index 0000000..abc1234 100644",
      "--- /dev/null",
      "+++ b/a.json",
      "@@ -0,0 +1 @@",
      '+{"a":1}',
      "diff --git a/b.json b/b.json",
      "index 0000000..def5678 100644",
      "--- /dev/null",
      "+++ b/b.json",
      "@@ -0,0 +1 @@",
      '+{"b":2}',
    ].join("\n");
    const result = getPatchDiffSizeBytes(diff);
    const expected = Buffer.byteLength('+{"a":1}\n', "utf8") + Buffer.byteLength('+{"b":2}\n', "utf8");
    expect(result).toBe(expected);
  });

  it("does not let deletions in one file mask additions in another file (per-file clamping)", () => {
    // File A: pure addition of new content
    // File B: pure deletion of similar-sized content
    // Global net ≈ 0 would be wrong — the added content must still count toward the limit
    const addLine = '+{"new_key":"aaaaaaaaaaaaaaaaaaa"}';
    const delLine = '-{"old_key":"bbbbbbbbbbbbbbbbbbb"}';
    const diff = [
      "diff --git a/new_file.json b/new_file.json",
      "index 0000000..abc1234 100644",
      "--- /dev/null",
      "+++ b/new_file.json",
      "@@ -0,0 +1 @@",
      addLine,
      "diff --git a/old_file.json b/old_file.json",
      "index def5678..0000000 100644",
      "--- a/old_file.json",
      "+++ /dev/null",
      "@@ -1 +0,0 @@",
      delLine,
    ].join("\n");
    const result = getPatchDiffSizeBytes(diff);
    const addBytes = Buffer.byteLength(addLine + "\n", "utf8");
    // File A contributes its addBytes; File B contributes max(0, 0 - delBytes) = 0
    // Total must equal addBytes, not near-zero
    expect(result).toBe(addBytes);
  });

  it("does not count +++ file header lines (they appear before any @@ hunk)", () => {
    const diff = [
      "diff --git a/file.json b/file.json",
      "index 0000000..abc1234 100644",
      "--- /dev/null",
      "+++ b/file.json",
      // No @@ yet — the +++ line above must NOT be counted
      "@@ -0,0 +1 @@",
      '+{"x":1}',
    ].join("\n");
    const result = getPatchDiffSizeBytes(diff);
    const expected = Buffer.byteLength('+{"x":1}\n', "utf8");
    expect(result).toBe(expected);
  });
});

describe("getStagedPatchDiffSizeBytes", () => {
  it("calls git diff --cached and returns net patch diff size", () => {
    const diffOutput = ["diff --git a/data.json b/data.json", "index abc..def 100644", "--- a/data.json", "+++ b/data.json", "@@ -1 +1,2 @@", '-{"old":1}', '+{"new":1}', '+{"extra":2}'].join("\n");

    const execGitSyncFn = (/** @type {string[]} */ args, /** @type {any} */ _opts) => {
      if (args[0] === "diff" && args[1] === "--cached" && !args.includes("--")) return diffOutput;
      return "";
    };

    const result = getStagedPatchDiffSizeBytes({ execGitSyncFn, cwd: "/some/dir" });
    const additions = Buffer.byteLength('+{"new":1}\n', "utf8") + Buffer.byteLength('+{"extra":2}\n', "utf8");
    const deletions = Buffer.byteLength('-{"old":1}\n', "utf8");
    expect(result).toBe(Math.max(0, additions - deletions));
  });

  it("passes the cwd option to execGitSyncFn", () => {
    const calls = /** @type {Array<{args: string[], opts: any}>} */ [];
    const execGitSyncFn = (/** @type {string[]} */ args, /** @type {any} */ opts) => {
      calls.push({ args, opts });
      return "";
    };

    getStagedPatchDiffSizeBytes({ execGitSyncFn, cwd: "/memory/dir" });
    expect(calls).toHaveLength(1);
    expect(calls[0].args).toEqual(["diff", "--cached", "--no-renames"]);
    expect(calls[0].opts.cwd).toBe("/memory/dir");
  });

  it("limits patch size to the supplied pathspecs", () => {
    const calls = /** @type {string[][]} */ [];
    const execGitSyncFn = (/** @type {string[]} */ args, /** @type {any} */ _opts) => {
      calls.push(args);
      return "";
    };

    getStagedPatchDiffSizeBytes({ execGitSyncFn, cwd: "/memory/dir", pathspecs: [":(literal)state.json"] });

    expect(calls).toEqual([["diff", "--cached", "--no-renames", "--", ":(literal)state.json"]]);
  });

  it("returns zero without running git when the pathspec list is empty", () => {
    const execGitSyncFn = vi.fn();

    expect(getStagedPatchDiffSizeBytes({ execGitSyncFn, cwd: "/memory/dir", pathspecs: [] })).toBe(0);
    expect(execGitSyncFn).not.toHaveBeenCalled();
  });
});

describe("isAncestorCommit", () => {
  /** @type {string} */
  let repoDir;

  beforeEach(() => {
    repoDir = createTestRepo();
  });

  afterEach(() => {
    cleanupRepo(repoDir);
  });

  it("returns true when ancestor commit is an ancestor of descendant", () => {
    const rootSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    fs.writeFileSync(path.join(repoDir, "file.txt"), "content\n");
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "Second commit"], { cwd: repoDir });
    const tipSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();

    expect(isAncestorCommit(rootSha, tipSha, repoDir)).toBe(true);
  });

  it("returns true when ancestor and descendant are identical", () => {
    const sha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    expect(isAncestorCommit(sha, sha, repoDir)).toBe(true);
  });

  it("returns false when ancestor commit is not an ancestor of descendant", () => {
    const rootSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    fs.writeFileSync(path.join(repoDir, "file.txt"), "content\n");
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "Second commit"], { cwd: repoDir });
    const tipSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();

    expect(isAncestorCommit(tipSha, rootSha, repoDir)).toBe(false);
  });

  it("returns false for an unknown revision instead of throwing", () => {
    const rootSha = execGit(["rev-parse", "HEAD"], { cwd: repoDir }).stdout.trim();
    expect(isAncestorCommit("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", rootSha, repoDir)).toBe(false);
  });
});

describe("isPartialClone", () => {
  /** @type {string} */
  let repoDir;

  beforeEach(() => {
    repoDir = createTestRepo();
  });

  afterEach(() => {
    cleanupRepo(repoDir);
  });

  it("returns false when remote.origin.promisor is not set", () => {
    expect(isPartialClone(repoDir)).toBe(false);
  });

  it("returns true when remote.origin.promisor is set to true", () => {
    execGit(["config", "remote.origin.promisor", "true"], { cwd: repoDir });
    expect(isPartialClone(repoDir)).toBe(true);
  });

  it("returns false when remote.origin.promisor is set to false", () => {
    execGit(["config", "remote.origin.promisor", "false"], { cwd: repoDir });
    expect(isPartialClone(repoDir)).toBe(false);
  });
});

describe("describeGitFailure", () => {
  /** @type {string} */
  let repoDir;

  beforeEach(() => {
    repoDir = createTestRepo();
  });

  afterEach(() => {
    cleanupRepo(repoDir);
  });

  it("leaves the message unchanged when the repo is not a partial clone", () => {
    const message = "fatal: remote error: Invalid username or token.";
    expect(describeGitFailure(message, repoDir)).toBe(message);
  });

  it("leaves the message unchanged when it does not look like an auth/promisor failure", () => {
    execGit(["config", "remote.origin.promisor", "true"], { cwd: repoDir });
    const message = "fatal: bad object HEAD";
    expect(describeGitFailure(message, repoDir)).toBe(message);
  });

  it("appends the partial-clone diagnostic when the repo is a partial clone and the message matches", () => {
    execGit(["config", "remote.origin.promisor", "true"], { cwd: repoDir });
    const message = "remote: Invalid username or token.";
    const result = describeGitFailure(message, repoDir);
    expect(result).toContain(message);
    expect(result).toContain("partial clone");
    expect(result).toContain("persist-credentials: false");
  });

  it("matches on 'promisor' in the message even without the exact auth wording", () => {
    execGit(["config", "remote.origin.promisor", "true"], { cwd: repoDir });
    const message = "error: promisor remote fetch failed";
    const result = describeGitFailure(message, repoDir);
    expect(result).toContain("partial clone");
  });

  it("does not append the diagnostic for unrelated network errors even on a partial clone", () => {
    execGit(["config", "remote.origin.promisor", "true"], { cwd: repoDir });
    const message = "fatal: unable to access 'https://example.com/': Could not resolve host";
    expect(describeGitFailure(message, repoDir)).toBe(message);
  });
});
