/**
 * Integration tests for push_signed_commits.cjs
 *
 * These tests run REAL git commands to verify that pushSignedCommits:
 * 1. Correctly enumerates new commits via `git rev-list`
 * 2. Reads file contents and builds the GraphQL payload
 * 3. Calls the GitHub GraphQL `createCommitOnBranch` mutation for each commit
 * 4. Falls back to `git push` when the GraphQL mutation fails
 *
 * A bare git repository is used as the stand-in "remote" so that ls-remote
 * and push commands work without a real network connection.
 * The GraphQL layer is always mocked because it requires a real GitHub API.
 */

// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import path from "path";
import { spawnSync } from "child_process";
import os from "os";

const require = createRequire(import.meta.url);

// Import module once – globals are resolved at call time, not import time.
const { pushSignedCommits, unquoteCPath } = require("./push_signed_commits.cjs");
const { resolveExperimentStateRebaseConflict } = require("./push_experiment_state.cjs");

// ──────────────────────────────────────────────────────────────────────────────
// Unit tests for unquoteCPath
// ──────────────────────────────────────────────────────────────────────────────

describe("unquoteCPath", () => {
  it("should return unquoted strings unchanged", () => {
    expect(unquoteCPath("simple.txt")).toBe("simple.txt");
    expect(unquoteCPath("path/to/file.txt")).toBe("path/to/file.txt");
    expect(unquoteCPath("")).toBe("");
  });

  it("should strip surrounding double-quotes from plain filenames", () => {
    expect(unquoteCPath('"hello.txt"')).toBe("hello.txt");
    expect(unquoteCPath('"path/to/file.txt"')).toBe("path/to/file.txt");
  });

  it("should unescape standard C escape sequences", () => {
    expect(unquoteCPath('"back\\\\slash"')).toBe("back\\slash");
    expect(unquoteCPath('"double\\"quote"')).toBe('double"quote');
    expect(unquoteCPath('"new\\nline"')).toBe("new\nline");
    expect(unquoteCPath('"tab\\there"')).toBe("tab\there");
    expect(unquoteCPath('"carriage\\rreturn"')).toBe("carriage\rreturn");
    expect(unquoteCPath('"form\\ffeed"')).toBe("form\ffeed");
    expect(unquoteCPath('"bell\\achar"')).toBe("bell\x07char");
    expect(unquoteCPath('"back\\bspace"')).toBe("back\bspace");
    expect(unquoteCPath('"vertical\\vtab"')).toBe("vertical\x0btab");
  });

  it("should decode octal sequences as UTF-8 bytes (unicode filenames)", () => {
    // é = U+00E9 → UTF-8: 0xC3 0xA9 → octal \303\251
    expect(unquoteCPath('"h\\303\\251llo.txt"')).toBe("héllo.txt");
    // ö = U+00F6 → UTF-8: 0xC3 0xB6 → octal \303\266
    expect(unquoteCPath('"w\\303\\266rld.txt"')).toBe("wörld.txt");
  });

  it("should decode filenames with spaces (git quotes when core.quotePath=true)", () => {
    // git does NOT actually quote spaces alone (only non-ASCII), but the function
    // must correctly pass through quoted strings that happen to contain spaces.
    expect(unquoteCPath('"hello world.txt"')).toBe("hello world.txt");
  });

  it("should preserve unknown escape sequences with backslash intact", () => {
    // '\x' is not a known escape – backslash is kept
    expect(unquoteCPath('"foo\\xbar"')).toBe("foo\\xbar");
  });

  it("should handle a quoted string with only one character", () => {
    expect(unquoteCPath('"a"')).toBe("a");
  });

  it("should handle a quoted empty string", () => {
    expect(unquoteCPath('""')).toBe("");
  });

  it("should handle 1-, 2-, and 3-digit octal sequences", () => {
    // \0 = 0x00 (NUL – unusual but valid)
    expect(unquoteCPath('"\\0"')).toBe("\x00");
    // \77 = 0x3F = '?'
    expect(unquoteCPath('"\\77"')).toBe("?");
    // \101 = 0x41 = 'A'
    expect(unquoteCPath('"\\101"')).toBe("A");
  });
});

// ──────────────────────────────────────────────────────────────────────────────
// Git helpers (real subprocess – no mocking)
// ──────────────────────────────────────────────────────────────────────────────

/**
 * @param {string[]} args
 * @param {{ cwd?: string, allowFailure?: boolean }} [options]
 */
function execGit(args, options = {}) {
  const result = spawnSync("git", args, {
    encoding: "utf8",
    env: {
      ...process.env,
      GIT_CONFIG_NOSYSTEM: "1",
      HOME: os.tmpdir(),
      // Allow git commands to run inside bare repositories. git 2.36+ changed the
      // default safe.bareRepository from "all" to "explicit", which prevents running
      // commands in a bare repo unless --git-dir is set explicitly.
      GIT_CONFIG_COUNT: "1",
      GIT_CONFIG_KEY_0: "safe.bareRepository",
      GIT_CONFIG_VALUE_0: "all",
    },
    ...options,
  });
  if (result.error) throw result.error;
  if (result.status !== 0 && !options.allowFailure) {
    throw new Error(`git ${args.join(" ")} failed (cwd=${options.cwd}):\n${result.stderr}`);
  }
  return result;
}

/**
 * Create a bare repository that acts as the remote "origin".
 * @returns {string} Path to the bare repository
 */
function createBareRepo() {
  const bareDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-signed-bare-"));
  execGit(["init", "--bare"], { cwd: bareDir });
  // Ensure the bare repo uses "main" as the default branch
  execGit(["symbolic-ref", "HEAD", "refs/heads/main"], { cwd: bareDir });
  return bareDir;
}

/**
 * Clone the bare repo and set up a working copy with an initial commit on `main`.
 * @param {string} bareDir
 * @returns {string} Path to the working copy
 */
function createWorkingRepo(bareDir) {
  const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-signed-work-"));
  execGit(["clone", bareDir, "."], { cwd: workDir });
  execGit(["config", "user.name", "Test User"], { cwd: workDir });
  execGit(["config", "user.email", "test@example.com"], { cwd: workDir });

  // Initial commit on main
  fs.writeFileSync(path.join(workDir, "README.md"), "# Test\n");
  execGit(["add", "."], { cwd: workDir });
  execGit(["commit", "-m", "Initial commit"], { cwd: workDir });
  // Rename to main if git defaulted to master
  execGit(["branch", "-M", "main"], { cwd: workDir });
  execGit(["push", "-u", "origin", "main"], { cwd: workDir });

  return workDir;
}

/** @param {string} dir */
function cleanupDir(dir) {
  if (dir && fs.existsSync(dir)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

// ──────────────────────────────────────────────────────────────────────────────
// Global stubs required by push_signed_commits.cjs
// ──────────────────────────────────────────────────────────────────────────────

/**
 * Build an `exec` global stub that runs real git commands via spawnSync.
 * @param {string} cwd
 */
function makeRealExec(cwd) {
  return {
    /**
     * @param {string} program
     * @param {string[]} args
     * @param {{ cwd?: string }} [opts]
     */
    getExecOutput: async (program, args, opts = {}) => {
      const result = spawnSync(program, args, {
        encoding: "utf8",
        cwd: opts.cwd ?? cwd,
        env: {
          ...process.env,
          GIT_CONFIG_NOSYSTEM: "1",
          HOME: os.tmpdir(),
          ...(opts.env || {}),
        },
      });
      if (result.error) throw result.error;
      return { exitCode: result.status ?? 0, stdout: result.stdout ?? "", stderr: result.stderr ?? "" };
    },
    /**
     * @param {string} program
     * @param {string[]} args
     * @param {{ cwd?: string, env?: NodeJS.ProcessEnv, silent?: boolean, listeners?: { stdout?: (data: Buffer) => void } }} [opts]
     */
    exec: async (program, args, opts = {}) => {
      const stdoutListener = opts.listeners?.stdout;
      const result = spawnSync(program, args, {
        // Use raw Buffer encoding when a stdout listener is provided so binary
        // content is not corrupted by UTF-8 decoding.
        encoding: stdoutListener ? null : "utf8",
        cwd: opts.cwd ?? cwd,
        env: opts.env ?? { ...process.env, GIT_CONFIG_NOSYSTEM: "1", HOME: os.tmpdir() },
      });
      if (result.error) throw result.error;
      if (result.status !== 0) {
        const stderr = Buffer.isBuffer(result.stderr) ? result.stderr.toString("utf8") : (result.stderr ?? "");
        throw new Error(`${program} ${args.join(" ")} failed:\n${stderr}`);
      }
      if (stdoutListener && result.stdout) {
        stdoutListener(Buffer.isBuffer(result.stdout) ? result.stdout : Buffer.from(result.stdout));
      }
      return result.status ?? 0;
    },
  };
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

describe("push_signed_commits integration tests", () => {
  let bareDir;
  let workDir;
  let mockCore;
  let capturedGraphQLCalls;

  /** @returns {any} */
  function makeMockGithubClient(options = {}) {
    const { failWithError = null, oid = "signed-oid-abc123" } = options;
    return {
      graphql: vi.fn(async query => {
        if (failWithError) throw failWithError;
        capturedGraphQLCalls.push({ oid, query });
        return { createCommitOnBranch: { commit: { oid } } };
      }),
      rest: {
        git: {
          createRef: vi.fn(async () => ({ data: {} })),
        },
      },
    };
  }

  beforeEach(() => {
    bareDir = createBareRepo();
    workDir = createWorkingRepo(bareDir);
    capturedGraphQLCalls = [];

    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
    };

    global.core = mockCore;
  });

  afterEach(() => {
    cleanupDir(bareDir);
    cleanupDir(workDir);
    delete global.core;
    delete global.exec;
    vi.clearAllMocks();
  });

  // ──────────────────────────────────────────────────────
  // Happy path – GraphQL succeeds
  // ──────────────────────────────────────────────────────

  describe("GraphQL signed commits (happy path)", () => {
    it("should call GraphQL for a single new commit", async () => {
      // Create a feature branch with one new file
      execGit(["checkout", "-b", "feature-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "hello.txt"), "Hello World\n");
      execGit(["add", "hello.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add hello.txt"], { cwd: workDir });
      // Push the branch so ls-remote can resolve its OID
      execGit(["push", "-u", "origin", "feature-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "feature-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      // Verify the mutation query targets createCommitOnBranch
      const [query, variables] = githubClient.graphql.mock.calls[0];
      expect(query).toContain("createCommitOnBranch");
      expect(query).toContain("CreateCommitOnBranchInput");
      // Verify the input structure
      expect(variables.input.branch.branchName).toBe("feature-branch");
      expect(variables.input.branch.repositoryNameWithOwner).toBe("test-owner/test-repo");
      expect(variables.input.message.headline).toBe("Add hello.txt");
      // hello.txt should appear in additions with base64 content
      expect(variables.input.fileChanges.additions).toHaveLength(1);
      expect(variables.input.fileChanges.additions[0].path).toBe("hello.txt");
      expect(Buffer.from(variables.input.fileChanges.additions[0].contents, "base64").toString()).toBe("Hello World\n");
    });

    it("should resolve temporary ID references in text file contents before GraphQL replay", async () => {
      execGit(["checkout", "-b", "temp-id-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "quarantine.cs"), '[QuarantinedTest("https://github.com/test-owner/test-repo/issues/#aw_test1")]\n// linked: #aw_test1\n');
      execGit(["add", "quarantine.cs"], { cwd: workDir });
      execGit(["commit", "-m", "Add quarantine reference"], { cwd: workDir });
      execGit(["push", "-u", "origin", "temp-id-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "temp-id-branch",
        baseRef: "origin/main",
        cwd: workDir,
        resolvedTemporaryIds: {
          aw_test1: { repo: "test-owner/test-repo", number: 66708 },
        },
        currentRepo: "test-owner/test-repo",
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const additions = githubClient.graphql.mock.calls[0][1].input.fileChanges.additions;
      expect(additions).toHaveLength(1);
      const resolvedContent = Buffer.from(additions[0].contents, "base64").toString();
      expect(resolvedContent).toContain("https://github.com/test-owner/test-repo/issues/66708");
      expect(resolvedContent).toContain("#66708");
      expect(resolvedContent).not.toContain("#aw_test1");
    });

    it("should still run replacement logic for malformed temporary ID candidates and emit warning", async () => {
      execGit(["checkout", "-b", "temp-id-malformed-candidate-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "quarantine.cs"), "// malformed link: #aw_test-id\n");
      execGit(["add", "quarantine.cs"], { cwd: workDir });
      execGit(["commit", "-m", "Add malformed temporary ID reference"], { cwd: workDir });
      execGit(["push", "-u", "origin", "temp-id-malformed-candidate-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "temp-id-malformed-candidate-branch",
        baseRef: "origin/main",
        cwd: workDir,
        resolvedTemporaryIds: {
          aw_test: { repo: "test-owner/test-repo", number: 66708 },
        },
        currentRepo: "test-owner/test-repo",
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const additions = githubClient.graphql.mock.calls[0][1].input.fileChanges.additions;
      const replayedContent = Buffer.from(additions[0].contents, "base64").toString();
      expect(replayedContent).toContain("#66708-id");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Malformed temporary ID reference '#aw_test-id'"));
    });

    it("should ignore invalid resolved temporary ID numbers instead of replacing with NaN", async () => {
      execGit(["checkout", "-b", "temp-id-invalid-number-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "quarantine.cs"), "// linked: #aw_test2\n");
      execGit(["add", "quarantine.cs"], { cwd: workDir });
      execGit(["commit", "-m", "Add temporary ID with invalid map entry"], { cwd: workDir });
      execGit(["push", "-u", "origin", "temp-id-invalid-number-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "temp-id-invalid-number-branch",
        baseRef: "origin/main",
        cwd: workDir,
        resolvedTemporaryIds: {
          aw_test2: { repo: "test-owner/test-repo", number: "not-a-number" },
        },
        currentRepo: "test-owner/test-repo",
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const additions = githubClient.graphql.mock.calls[0][1].input.fileChanges.additions;
      const replayedContent = Buffer.from(additions[0].contents, "base64").toString();
      expect(replayedContent).toContain("#aw_test2");
      expect(replayedContent).not.toContain("#NaN");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("ignoring invalid resolved temporary ID number for 'aw_test2'"));
    });

    it("should call GraphQL once per commit for multiple new commits", async () => {
      execGit(["checkout", "-b", "multi-commit-branch"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "file-a.txt"), "File A\n");
      execGit(["add", "file-a.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add file-a.txt"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "file-b.txt"), "File B\n");
      execGit(["add", "file-b.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add file-b.txt"], { cwd: workDir });

      execGit(["push", "-u", "origin", "multi-commit-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "multi-commit-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(2);
      const headlines = githubClient.graphql.mock.calls.map(c => c[1].input.message.headline);
      expect(headlines).toEqual(["Add file-a.txt", "Add file-b.txt"]);
    });

    it("each commit in a series should carry its own file content, not the working-tree tip", async () => {
      // Regression test for the bug where fs.readFileSync always read from the
      // working tree (HEAD), so intermediate commits A and B would contain the
      // content of C when A→B→C were replayed.
      execGit(["checkout", "-b", "versioned-branch"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "data.txt"), "version A\n");
      execGit(["add", "data.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Version A"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "data.txt"), "version B\n");
      execGit(["add", "data.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Version B"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "data.txt"), "version C\n");
      execGit(["add", "data.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Version C"], { cwd: workDir });

      execGit(["push", "-u", "origin", "versioned-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "versioned-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(3);
      const calls = githubClient.graphql.mock.calls.map(c => c[1].input);

      // Each commit must carry its own version of data.txt, not the working-tree tip (C)
      expect(Buffer.from(calls[0].fileChanges.additions[0].contents, "base64").toString()).toBe("version A\n");
      expect(Buffer.from(calls[1].fileChanges.additions[0].contents, "base64").toString()).toBe("version B\n");
      expect(Buffer.from(calls[2].fileChanges.additions[0].contents, "base64").toString()).toBe("version C\n");
    });

    it("should include deletions when files are removed in a commit", async () => {
      execGit(["checkout", "-b", "delete-branch"], { cwd: workDir });

      // First add a file, push, then delete it
      fs.writeFileSync(path.join(workDir, "to-delete.txt"), "Will be deleted\n");
      execGit(["add", "to-delete.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add file to delete"], { cwd: workDir });
      execGit(["push", "-u", "origin", "delete-branch"], { cwd: workDir });

      // Now delete the file
      fs.unlinkSync(path.join(workDir, "to-delete.txt"));
      execGit(["add", "-u"], { cwd: workDir });
      execGit(["commit", "-m", "Delete file"], { cwd: workDir });
      execGit(["push", "origin", "delete-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "delete-branch",
        // Only replay the delete commit
        baseRef: "delete-branch^",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.fileChanges.deletions).toEqual([{ path: "to-delete.txt" }]);
      expect(callArg.fileChanges.additions).toHaveLength(0);
    });

    it("should handle commit with no file changes (empty commit)", async () => {
      execGit(["checkout", "-b", "empty-diff-branch"], { cwd: workDir });
      execGit(["push", "-u", "origin", "empty-diff-branch"], { cwd: workDir });

      // Allow an empty commit
      execGit(["commit", "--allow-empty", "-m", "Empty commit"], { cwd: workDir });
      execGit(["push", "origin", "empty-diff-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "empty-diff-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.fileChanges.additions).toHaveLength(0);
      expect(callArg.fileChanges.deletions).toHaveLength(0);
    });

    it("should do nothing when there are no new commits", async () => {
      execGit(["checkout", "-b", "no-commits-branch"], { cwd: workDir });
      execGit(["push", "-u", "origin", "no-commits-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      // baseRef points to the same HEAD – no commits to replay
      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "no-commits-branch",
        baseRef: "origin/no-commits-branch",
        cwd: workDir,
      });

      expect(githubClient.graphql).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("no new commits"));
    });
  });

  // ──────────────────────────────────────────────────────
  // New branch – branch does not yet exist on remote
  // ──────────────────────────────────────────────────────

  describe("new branch (does not exist on remote)", () => {
    it("should create remote branch via REST and use parent OID for first commit (single commit)", async () => {
      // Create a local branch with one commit but do NOT push it
      execGit(["checkout", "-b", "new-unpushed-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "new-file.txt"), "New file content\n");
      execGit(["add", "new-file.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add new-file.txt"], { cwd: workDir });

      // Capture the local parent OID (main HEAD before the new commit)
      const expectedParentOid = execGit(["rev-parse", "HEAD^"], { cwd: workDir }).stdout.trim();

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "new-unpushed-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // Branch must be created via REST before the GraphQL mutation
      expect(githubClient.rest.git.createRef).toHaveBeenCalledTimes(1);
      expect(githubClient.rest.git.createRef).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        ref: "refs/heads/new-unpushed-branch",
        sha: expectedParentOid,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      // expectedHeadOid must be the parent commit OID, not empty
      expect(callArg.expectedHeadOid).toBe(expectedParentOid);
      expect(callArg.branch.branchName).toBe("new-unpushed-branch");
      expect(callArg.message.headline).toBe("Add new-file.txt");
      // Verify the info log was emitted
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not yet on the remote"));
    });

    it("should ignore an injected baseRef boundary commit in rev-list output", async () => {
      execGit(["checkout", "-b", "new-boundary-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "boundary-file.txt"), "Boundary file content\n");
      execGit(["add", "boundary-file.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add boundary-file.txt"], { cwd: workDir });

      const baseRefOid = execGit(["rev-parse", "origin/main"], { cwd: workDir }).stdout.trim();
      const newCommitOid = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      const realExec = makeRealExec(workDir);
      global.exec = {
        ...realExec,
        getExecOutput: vi.fn(async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "rev-list" && args[1] === "--parents") {
            return {
              exitCode: 0,
              stdout: `${baseRefOid}\n${newCommitOid} ${baseRefOid}\n`,
              stderr: "",
            };
          }
          return realExec.getExecOutput(program, args, opts);
        }),
      };
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "new-boundary-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.rest.git.createRef).toHaveBeenCalledTimes(1);
      expect(githubClient.rest.git.createRef).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        ref: "refs/heads/new-boundary-branch",
        sha: baseRefOid,
      });
      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.message.headline).toBe("Add boundary-file.txt");
      expect(callArg.expectedHeadOid).toBe(baseRefOid);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("baseRef boundary commit(s)"));
    });

    it("should drop injected baseRef boundary entries and replay only new commits in order", async () => {
      execGit(["checkout", "-b", "new-boundary-multi-branch"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "boundary-alpha.txt"), "Alpha boundary content\n");
      execGit(["add", "boundary-alpha.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add boundary-alpha.txt"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "boundary-beta.txt"), "Beta boundary content\n");
      execGit(["add", "boundary-beta.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add boundary-beta.txt"], { cwd: workDir });

      const baseRefOid = execGit(["rev-parse", "origin/main"], { cwd: workDir }).stdout.trim();
      const alphaCommitOid = execGit(["rev-parse", "HEAD~1"], { cwd: workDir }).stdout.trim();
      const betaCommitOid = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      const realExec = makeRealExec(workDir);
      global.exec = {
        ...realExec,
        getExecOutput: vi.fn(async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "rev-list" && args[1] === "--parents") {
            return {
              exitCode: 0,
              stdout: `${baseRefOid}\n${alphaCommitOid} ${baseRefOid}\n${betaCommitOid} ${alphaCommitOid}\n`,
              stderr: "",
            };
          }
          return realExec.getExecOutput(program, args, opts);
        }),
      };
      const githubClient = makeMockGithubClient({ oid: "signed-oid-first" });

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "new-boundary-multi-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.rest.git.createRef).toHaveBeenCalledTimes(1);
      expect(githubClient.rest.git.createRef).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        ref: "refs/heads/new-boundary-multi-branch",
        sha: baseRefOid,
      });
      expect(githubClient.graphql).toHaveBeenCalledTimes(2);
      expect(githubClient.graphql.mock.calls[0][1].input.expectedHeadOid).toBe(baseRefOid);
      expect(githubClient.graphql.mock.calls[1][1].input.expectedHeadOid).toBe("signed-oid-first");
      const committedHeadlines = githubClient.graphql.mock.calls.map(call => call[1].input.message.headline);
      expect(committedHeadlines).toEqual(["Add boundary-alpha.txt", "Add boundary-beta.txt"]);

      const attemptedBoundaryParentResolution = global.exec.getExecOutput.mock.calls.some(([program, args]) => program === "git" && args[0] === "rev-parse" && args[1] === `${baseRefOid}^`);
      expect(attemptedBoundaryParentResolution).toBe(false);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("dropped 1 baseRef boundary commit"));
    });

    it("should fall back to per-commit parent resolution when baseRef OID resolution fails", async () => {
      execGit(["checkout", "-b", "new-base-ref-failure-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "fallback-file.txt"), "Fallback content\n");
      execGit(["add", "fallback-file.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add fallback-file.txt"], { cwd: workDir });

      const newCommitOid = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();
      const expectedParentOid = execGit(["rev-parse", "HEAD^"], { cwd: workDir }).stdout.trim();

      const realExec = makeRealExec(workDir);
      global.exec = {
        ...realExec,
        getExecOutput: vi.fn(async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "rev-parse" && args[1] === "origin/main^{commit}") {
            throw new Error("simulated rev-parse failure");
          }
          return realExec.getExecOutput(program, args, opts);
        }),
      };
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "new-base-ref-failure-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("boundary-commit filter is disabled for this run"));
      expect(githubClient.rest.git.createRef).toHaveBeenCalledTimes(1);
      expect(githubClient.rest.git.createRef).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        ref: "refs/heads/new-base-ref-failure-branch",
        sha: expectedParentOid,
      });
      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      expect(githubClient.graphql.mock.calls[0][1].input.message.headline).toBe("Add fallback-file.txt");
      expect(githubClient.graphql.mock.calls[0][1].input.expectedHeadOid).toBe(expectedParentOid);

      const hasPerCommitParentCall = global.exec.getExecOutput.mock.calls.some(([program, args]) => program === "git" && args[0] === "rev-parse" && args[1] === `${newCommitOid}^`);
      expect(hasPerCommitParentCall).toBe(true);
    });

    it("should create remote branch once then chain GraphQL OIDs for multiple commits on a new branch", async () => {
      // Create a local branch with two commits but do NOT push it
      execGit(["checkout", "-b", "new-multi-commit-branch"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "alpha.txt"), "Alpha\n");
      execGit(["add", "alpha.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add alpha.txt"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "beta.txt"), "Beta\n");
      execGit(["add", "beta.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add beta.txt"], { cwd: workDir });

      // The parent OID of the first commit is main's HEAD (two commits back from current)
      const expectedParentOid = execGit(["rev-parse", "HEAD^^"], { cwd: workDir }).stdout.trim();

      global.exec = makeRealExec(workDir);
      // Mock returns the same OID for all calls; second call must use that OID
      const githubClient = makeMockGithubClient({ oid: "signed-oid-first" });

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "new-multi-commit-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // Branch must be created via REST only once (for the first commit)
      expect(githubClient.rest.git.createRef).toHaveBeenCalledTimes(1);
      expect(githubClient.rest.git.createRef).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        ref: "refs/heads/new-multi-commit-branch",
        sha: expectedParentOid,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(2);

      // First call: expectedHeadOid is the parent commit OID (resolved via git rev-parse)
      const firstCallArg = githubClient.graphql.mock.calls[0][1].input;
      expect(firstCallArg.expectedHeadOid).toBe(expectedParentOid);
      expect(firstCallArg.message.headline).toBe("Add alpha.txt");

      // Second call: expectedHeadOid is the OID returned by the first GraphQL mutation
      const secondCallArg = githubClient.graphql.mock.calls[1][1].input;
      expect(secondCallArg.expectedHeadOid).toBe("signed-oid-first");
      expect(secondCallArg.message.headline).toBe("Add beta.txt");
    });

    it("should continue with signed commits when createRef returns 422 (concurrent branch creation)", async () => {
      execGit(["checkout", "-b", "race-condition-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "race.txt"), "Race content\n");
      execGit(["add", "race.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Race commit"], { cwd: workDir });

      const expectedParentOid = execGit(["rev-parse", "HEAD^"], { cwd: workDir }).stdout.trim();

      const realExec = makeRealExec(workDir);
      let lsRemoteCallCount = 0;
      global.exec = {
        ...realExec,
        getExecOutput: vi.fn(async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "ls-remote" && args[2] === "refs/heads/race-condition-branch") {
            lsRemoteCallCount += 1;
            if (lsRemoteCallCount === 1) {
              return { exitCode: 0, stdout: "", stderr: "" };
            }
            return { exitCode: 0, stdout: `${expectedParentOid}\trefs/heads/race-condition-branch\n`, stderr: "" };
          }
          return realExec.getExecOutput(program, args, opts);
        }),
      };

      // Simulate concurrent branch creation: createRef throws 422 (GitHub API exact format)
      const concurrentError = Object.assign(new Error("Reference refs/heads/race-condition-branch already exists"), { status: 422 });
      const githubClient = {
        graphql: vi.fn(async () => ({ createCommitOnBranch: { commit: { oid: "signed-oid-race" } } })),
        rest: {
          git: {
            createRef: vi.fn(async () => {
              throw concurrentError;
            }),
          },
        },
      };

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "race-condition-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // Signed replay should proceed even if branch creation races; depending on
      // ls-remote timing, createRef may be attempted once or skipped.
      expect(githubClient.rest.git.createRef.mock.calls.length).toBeLessThanOrEqual(1);
      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.expectedHeadOid).toBe(expectedParentOid);
      expect(callArg.message.headline).toBe("Race commit");
    });
  });

  // ──────────────────────────────────────────────────────
  // Orphan branch – empty baseRef (push_experiment_state first push)
  // ──────────────────────────────────────────────────────

  describe("orphan branch first push (empty baseRef)", () => {
    it("should bypass GraphQL and use git push directly when baseRef is empty (orphan branch root commit)", async () => {
      // Simulate checkoutOrCreateBranch() returning "" for a brand-new orphan branch,
      // which is exactly the scenario in push_experiment_state.cjs.
      // Orphan-branch first commits are root commits (no parent), so the GraphQL
      // createCommitOnBranch path cannot resolve a parent OID. The fix detects
      // !baseRef upfront and uses git push directly instead of attempting GraphQL.
      execGit(["checkout", "--orphan", "experiments/state"], { cwd: workDir });
      execGit(["read-tree", "--empty"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "state.json"), JSON.stringify({ runs: 1 }));
      execGit(["add", "state.json"], { cwd: workDir });
      execGit(["commit", "-m", "Initial experiment state"], { cwd: workDir });

      const expectedSha = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      const result = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "experiments/state",
        baseRef: "",
        cwd: workDir,
      });

      // GraphQL must NOT be called (orphan root commit has no parent to resolve).
      expect(githubClient.graphql).not.toHaveBeenCalled();

      // An info-level log (not a warning) should indicate the direct-push path.
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("empty baseRef detected"));
      expect(mockCore.warning).not.toHaveBeenCalled();

      // The commit must now be on the remote – state was NOT silently discarded.
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/experiments/state"], { cwd: workDir });
      const remoteOid = lsRemote.stdout.trim().split(/\s+/)[0];
      expect(remoteOid).toBe(expectedSha);

      // Return value should be the HEAD SHA.
      expect(result).toBe(expectedSha);
    });

    it("should throw with manual seeding instructions when orphan-branch git push fails", async () => {
      // Simulate a repo where "Require signed commits" is enforced. The orphan-branch
      // first push uses git push directly, which will be rejected by the remote with
      // GH013. We simulate this by using a bare repo that refuses the push via a
      // pre-receive hook.
      execGit(["checkout", "--orphan", "experiments/signed-required"], { cwd: workDir });
      execGit(["read-tree", "--empty"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "state.json"), JSON.stringify({ runs: 1 }));
      execGit(["add", "state.json"], { cwd: workDir });
      execGit(["commit", "-m", "Initial experiment state"], { cwd: workDir });

      // Install a pre-receive hook in the bare repo that mimics GH013 by rejecting all pushes.
      const hooksDir = path.join(bareDir, "hooks");
      fs.mkdirSync(hooksDir, { recursive: true });
      const hookPath = path.join(hooksDir, "pre-receive");
      fs.writeFileSync(hookPath, "#!/bin/sh\necho 'remote: error: GH013: Repository rule violations found.' >&2\necho 'remote: - Commits must have verified signatures.' >&2\nexit 1\n");
      fs.chmodSync(hookPath, "0755");

      // Use the real exec so git push actually runs and hits the hook.
      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      let thrownErr;
      try {
        await pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "experiments/signed-required",
          baseRef: "",
          cwd: workDir,
        });
      } catch (err) {
        thrownErr = err;
      }
      expect(thrownErr).toBeDefined();
      expect(thrownErr.message).toContain("failed to push orphan branch");
      expect(thrownErr.message).toContain("git switch --orphan experiments/signed-required");
      expect(thrownErr.message).toContain("git commit --allow-empty -S");
      expect(thrownErr.message).toContain("git push origin experiments/signed-required");
      expect(thrownErr.message).toContain("signed commits");
    });
  });

  // ──────────────────────────────────────────────────────
  // Fallback path – GraphQL fails → git push
  // ──────────────────────────────────────────────────────

  describe("git push fallback when GraphQL fails", () => {
    it("should fall back to git push when GraphQL throws", async () => {
      execGit(["checkout", "-b", "fallback-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "fallback.txt"), "Fallback content\n");
      execGit(["add", "fallback.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Fallback commit"], { cwd: workDir });
      execGit(["push", "-u", "origin", "fallback-branch"], { cwd: workDir });

      // Add another commit that will be pushed via git push fallback
      fs.writeFileSync(path.join(workDir, "extra.txt"), "Extra content\n");
      execGit(["add", "extra.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Extra commit"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient({ failWithError: new Error("GraphQL: not supported on GHES") });

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "fallback-branch",
        baseRef: "origin/fallback-branch",
        cwd: workDir,
      });

      // Should warn and fall back
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("falling back to git push"));

      // The commit should now be on the remote (verified via ls-remote)
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/fallback-branch"], { cwd: workDir });
      const remoteOid = lsRemote.stdout.trim().split(/\s+/)[0];
      const localOid = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();
      expect(remoteOid).toBe(localOid);
    });

    it("should refuse git push fallback when explicitly disabled", async () => {
      execGit(["checkout", "-b", "no-fallback-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "base.txt"), "Base content\n");
      execGit(["add", "base.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Base commit"], { cwd: workDir });
      execGit(["push", "-u", "origin", "no-fallback-branch"], { cwd: workDir });

      const remoteOidBefore = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      fs.writeFileSync(path.join(workDir, "extra.txt"), "Extra content\n");
      execGit(["add", "extra.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Extra commit"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient({ failWithError: new Error("GraphQL: not supported on GHES") });

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "no-fallback-branch",
          baseRef: "origin/no-fallback-branch",
          cwd: workDir,
          allowGitPushFallback: false,
        })
      ).rejects.toThrow("git push fallback is disabled");

      const remoteOidAfter = execGit(["rev-parse", "refs/heads/no-fallback-branch"], { cwd: bareDir }).stdout.trim();
      expect(remoteOidAfter).toBe(remoteOidBefore);
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("falling back to git push"));
    });
  });

  describe("git auth environment propagation", () => {
    it("should pass gitAuthEnv to ls-remote in the signed-commit path", async () => {
      const gitAuthEnv = {
        GIT_CONFIG_COUNT: "1",
        GIT_CONFIG_KEY_0: "http.https://github.com/.extraheader",
        GIT_CONFIG_VALUE_0: "Authorization: basic test-token",
      };
      const sentinelKey = "PUSH_SIGNED_COMMITS_ENV_SENTINEL_1";
      const sentinelValue = "sentinel-1";
      const previousSentinel = process.env[sentinelKey];
      process.env[sentinelKey] = sentinelValue;

      const getExecOutput = vi.fn(async (_program, args) => {
        if (args[0] === "rev-list") {
          return {
            exitCode: 0,
            stdout: "1111111111111111111111111111111111111111 0000000000000000000000000000000000000000\n",
            stderr: "",
          };
        }
        if (args[0] === "diff-tree") {
          return {
            exitCode: 0,
            stdout: ":100644 100644 0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa M\tmemory.json\n",
            stderr: "",
          };
        }
        if (args[0] === "ls-remote") {
          return {
            exitCode: 0,
            stdout: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/heads/auth-check-branch\n",
            stderr: "",
          };
        }
        if (args[0] === "log") {
          return { exitCode: 0, stdout: "Auth check commit\n", stderr: "" };
        }
        throw new Error(`Unexpected git command: ${args.join(" ")}`);
      });

      const execProgram = vi.fn(async (_program, args, opts = {}) => {
        if (args[0] === "cat-file" && args[1] === "blob") {
          opts.listeners?.stdout?.(Buffer.from("memory data\n"));
          return 0;
        }
        throw new Error(`Unexpected exec command: ${args.join(" ")}`);
      });

      global.exec = {
        getExecOutput,
        exec: execProgram,
      };

      const githubClient = makeMockGithubClient();

      try {
        await pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "auth-check-branch",
          baseRef: "origin/main",
          cwd: workDir,
          gitAuthEnv,
        });
      } finally {
        if (previousSentinel === undefined) {
          delete process.env[sentinelKey];
        } else {
          process.env[sentinelKey] = previousSentinel;
        }
      }

      const lsRemoteCall = getExecOutput.mock.calls.find(call => call[1][0] === "ls-remote");
      expect(lsRemoteCall).toBeDefined();
      expect(lsRemoteCall[2]).toEqual(
        expect.objectContaining({
          cwd: workDir,
          env: expect.objectContaining({
            ...gitAuthEnv,
            [sentinelKey]: sentinelValue,
          }),
        })
      );
    });

    it("should include auth env on ls-remote getExecOutput git call", async () => {
      const gitAuthEnv = {
        GIT_CONFIG_COUNT: "1",
        GIT_CONFIG_KEY_0: "http.https://github.com/.extraheader",
        GIT_CONFIG_VALUE_0: "Authorization: basic test-token",
      };
      const sentinelKey = "PUSH_SIGNED_COMMITS_ENV_SENTINEL_2";
      const sentinelValue = "sentinel-2";
      const previousSentinel = process.env[sentinelKey];
      process.env[sentinelKey] = sentinelValue;

      const getExecOutput = vi.fn(async (_program, args) => {
        if (args[0] === "rev-list") {
          return {
            exitCode: 0,
            stdout: "2222222222222222222222222222222222222222 1111111111111111111111111111111111111111\n",
            stderr: "",
          };
        }
        if (args[0] === "diff-tree") {
          return {
            exitCode: 0,
            stdout: ":100644 100644 0000000000000000000000000000000000000000 bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb M\tmemory.json\n",
            stderr: "",
          };
        }
        if (args[0] === "ls-remote") {
          return {
            exitCode: 0,
            stdout: "cafebabecafebabecafebabecafebabecafebabe\trefs/heads/auth-check-branch\n",
            stderr: "",
          };
        }
        if (args[0] === "log") {
          return { exitCode: 0, stdout: "Auth guard commit\n", stderr: "" };
        }
        throw new Error(`Unexpected git command: ${args.join(" ")}`);
      });

      global.exec = {
        getExecOutput,
        exec: async (_program, args, opts = {}) => {
          if (args[0] === "cat-file" && args[1] === "blob") {
            opts.listeners?.stdout?.(Buffer.from("memory data\n"));
            return 0;
          }
          throw new Error(`Unexpected exec command: ${args.join(" ")}`);
        },
      };

      const githubClient = makeMockGithubClient();

      try {
        await pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "auth-check-branch",
          baseRef: "origin/main",
          cwd: workDir,
          gitAuthEnv,
        });
      } finally {
        if (previousSentinel === undefined) {
          delete process.env[sentinelKey];
        } else {
          process.env[sentinelKey] = previousSentinel;
        }
      }

      const networkGitCalls = getExecOutput.mock.calls.filter(call => call[1][0] === "ls-remote");
      expect(networkGitCalls.length).toBeGreaterThanOrEqual(1);
      for (const call of networkGitCalls) {
        expect(call[2]).toEqual(
          expect.objectContaining({
            env: expect.objectContaining({
              ...gitAuthEnv,
              [sentinelKey]: sentinelValue,
            }),
          })
        );
      }
    });
  });

  // ──────────────────────────────────────────────────────
  // Commit message – multi-line body
  // ──────────────────────────────────────────────────────

  describe("commit message handling", () => {
    it("should include the commit body when present", async () => {
      execGit(["checkout", "-b", "body-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "described.txt"), "content\n");
      execGit(["add", "described.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Subject line\n\nDetailed body text\n\nMore details here"], { cwd: workDir });
      execGit(["push", "-u", "origin", "body-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "body-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.message.headline).toBe("Subject line");
      expect(callArg.message.body).toContain("Detailed body text");
    });

    it("should omit the body field when commit message has no body", async () => {
      execGit(["checkout", "-b", "no-body-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "nodesc.txt"), "nodesc\n");
      execGit(["add", "nodesc.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Subject only"], { cwd: workDir });
      execGit(["push", "-u", "origin", "no-body-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "no-body-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.message.headline).toBe("Subject only");
      expect(callArg.message.body).toBeUndefined();
    });
  });

  // ──────────────────────────────────────────────────────
  // File mode handling – symlinks and executables
  // ──────────────────────────────────────────────────────

  describe("file mode handling", () => {
    it("should refuse unsigned push and not fall back to git push when commit contains a symlink", async () => {
      execGit(["checkout", "-b", "symlink-branch"], { cwd: workDir });

      // Create a regular file to serve as symlink target
      fs.writeFileSync(path.join(workDir, "target.txt"), "Symlink target\n");
      execGit(["add", "target.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add target file"], { cwd: workDir });
      execGit(["push", "-u", "origin", "symlink-branch"], { cwd: workDir });

      // Add a symlink in a new commit
      fs.symlinkSync("target.txt", path.join(workDir, "link.txt"));
      execGit(["add", "link.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add symlink"], { cwd: workDir });
      execGit(["push", "origin", "symlink-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "symlink-branch",
          // Only replay the symlink commit
          baseRef: "symlink-branch^",
          cwd: workDir,
        })
      ).rejects.toThrow("refusing unsigned push for branch 'symlink-branch'");

      // GraphQL should NOT have been called – symlink is detected pre-flight
      expect(githubClient.graphql).not.toHaveBeenCalled();
      // Warning about symlink must be emitted (diagnostic log value preserved)
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("symlink link.txt cannot be pushed as a signed commit"));
    });

    it("should warn about executable bit loss but continue with GraphQL signed commit", async () => {
      execGit(["checkout", "-b", "executable-branch"], { cwd: workDir });

      // Create an executable file
      fs.writeFileSync(path.join(workDir, "script.sh"), "#!/bin/bash\necho hello\n");
      fs.chmodSync(path.join(workDir, "script.sh"), 0o755);
      execGit(["add", "script.sh"], { cwd: workDir });
      execGit(["commit", "-m", "Add executable script"], { cwd: workDir });
      execGit(["push", "-u", "origin", "executable-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "executable-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // GraphQL SHOULD still be called – executable bit triggers a warning but not a fallback
      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      // Warning about executable bit must be emitted
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("executable bit on script.sh will be lost in signed commit"));
      // The file content should be in the additions payload
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.fileChanges.additions).toHaveLength(1);
      expect(callArg.fileChanges.additions[0].path).toBe("script.sh");
      expect(Buffer.from(callArg.fileChanges.additions[0].contents, "base64").toString()).toContain("echo hello");
    });

    it("should refuse unsigned push and not fall back to git push when commit contains a submodule entry", async () => {
      execGit(["checkout", "-b", "submodule-branch"], { cwd: workDir });

      // Create a gitlink (mode 160000) entry directly via update-index so we don't
      // need a real submodule URL.  git diff-tree --raw will report this as mode 160000.
      // The cacheinfo format is: <mode>,<objectId>,<path>
      const headSha = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();
      execGit(["update-index", "--add", "--cacheinfo", `160000,${headSha},mysubmodule`], { cwd: workDir });
      execGit(["commit", "-m", "Add submodule"], { cwd: workDir });
      execGit(["push", "-u", "origin", "submodule-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "submodule-branch",
          // Only replay the submodule commit
          baseRef: "submodule-branch^",
          cwd: workDir,
        })
      ).rejects.toThrow("refusing unsigned push for branch 'submodule-branch'");

      // GraphQL should NOT have been called – submodule is detected pre-flight
      expect(githubClient.graphql).not.toHaveBeenCalled();
      // Warning about submodule must be emitted (diagnostic log value preserved)
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("submodule change detected in mysubmodule"));
    });

    it("should not warn for regular files (mode 100644)", async () => {
      execGit(["checkout", "-b", "regular-file-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "regular.txt"), "Regular file content\n");
      execGit(["add", "regular.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add regular file"], { cwd: workDir });
      execGit(["push", "-u", "origin", "regular-file-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "regular-file-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      // No warnings should be emitted for regular files
      expect(mockCore.warning).not.toHaveBeenCalled();
    });
  });

  // ──────────────────────────────────────────────────────
  // C-quoted (special character) filenames
  // ──────────────────────────────────────────────────────

  describe("C-quoted filenames (spaces and unicode)", () => {
    it("should handle filenames with spaces", async () => {
      execGit(["checkout", "-b", "spaces-branch"], { cwd: workDir });

      const spacedName = "hello world.txt";
      fs.writeFileSync(path.join(workDir, spacedName), "spaced content\n");
      execGit(["add", spacedName], { cwd: workDir });
      execGit(["commit", "-m", "Add file with spaces"], { cwd: workDir });
      execGit(["push", "-u", "origin", "spaces-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "spaces-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.fileChanges.additions).toHaveLength(1);
      expect(callArg.fileChanges.additions[0].path).toBe(spacedName);
      expect(Buffer.from(callArg.fileChanges.additions[0].contents, "base64").toString()).toBe("spaced content\n");
    });

    it("should handle filenames with unicode characters", async () => {
      execGit(["checkout", "-b", "unicode-branch"], { cwd: workDir });

      const unicodeName = "héllo_wörld.txt";
      fs.writeFileSync(path.join(workDir, unicodeName), "unicode content\n");
      execGit(["add", unicodeName], { cwd: workDir });
      execGit(["commit", "-m", "Add file with unicode name"], { cwd: workDir });
      execGit(["push", "-u", "origin", "unicode-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "unicode-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.fileChanges.additions).toHaveLength(1);
      expect(callArg.fileChanges.additions[0].path).toBe(unicodeName);
      expect(Buffer.from(callArg.fileChanges.additions[0].contents, "base64").toString()).toBe("unicode content\n");
    });
  });

  // ──────────────────────────────────────────────────────
  // Rename and copy file handling
  // ──────────────────────────────────────────────────────

  describe("rename and copy file handling", () => {
    it("should add old path to deletions and new path to additions on rename", async () => {
      execGit(["checkout", "-b", "rename-branch"], { cwd: workDir });

      // Add a file that will be renamed
      fs.writeFileSync(path.join(workDir, "original.txt"), "rename me\n");
      execGit(["add", "original.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add original.txt"], { cwd: workDir });
      execGit(["push", "-u", "origin", "rename-branch"], { cwd: workDir });

      // Rename the file
      fs.renameSync(path.join(workDir, "original.txt"), path.join(workDir, "renamed.txt"));
      execGit(["add", "-A"], { cwd: workDir });
      execGit(["commit", "-m", "Rename original.txt to renamed.txt"], { cwd: workDir });
      execGit(["push", "origin", "rename-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "rename-branch",
        baseRef: "rename-branch^",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      // Old path must be deleted, new path must be in additions
      expect(callArg.fileChanges.deletions).toEqual([{ path: "original.txt" }]);
      expect(callArg.fileChanges.additions).toHaveLength(1);
      expect(callArg.fileChanges.additions[0].path).toBe("renamed.txt");
      expect(Buffer.from(callArg.fileChanges.additions[0].contents, "base64").toString()).toBe("rename me\n");
    });

    it("should not add source to deletions on copy (only destination in additions)", async () => {
      execGit(["checkout", "-b", "copy-branch"], { cwd: workDir });

      // Add a file that will be copied
      fs.writeFileSync(path.join(workDir, "source.txt"), "copy source\n");
      execGit(["add", "source.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add source.txt"], { cwd: workDir });
      execGit(["push", "-u", "origin", "copy-branch"], { cwd: workDir });

      // Copy the file (source kept, destination added)
      fs.copyFileSync(path.join(workDir, "source.txt"), path.join(workDir, "destination.txt"));
      execGit(["add", "destination.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Copy source.txt to destination.txt"], { cwd: workDir });
      execGit(["push", "origin", "copy-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "copy-branch",
        baseRef: "copy-branch^",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      // Source file must NOT appear in deletions
      expect(callArg.fileChanges.deletions).toHaveLength(0);
      // Destination file must appear in additions
      expect(callArg.fileChanges.additions).toHaveLength(1);
      expect(callArg.fileChanges.additions[0].path).toBe("destination.txt");
      expect(Buffer.from(callArg.fileChanges.additions[0].contents, "base64").toString()).toBe("copy source\n");
    });
  });

  // ──────────────────────────────────────────────────────
  // Topological ordering (--topo-order)
  // ──────────────────────────────────────────────────────

  describe("topological commit ordering", () => {
    it("should replay commits in DAG order even when commit dates are out of sync", async () => {
      execGit(["checkout", "-b", "topo-order-branch"], { cwd: workDir });

      // Create two commits where the second commit has an earlier author/committer date
      // than the first, simulating the situation after `git rebase --committer-date-is-author-date`
      // or manual date manipulation. Without --topo-order git would return them in wrong order.
      const laterDate = "2020-01-02T00:00:00+00:00";
      const earlierDate = "2020-01-01T00:00:00+00:00";

      fs.writeFileSync(path.join(workDir, "first.txt"), "first\n");
      execGit(["add", "first.txt"], { cwd: workDir });
      // Commit A has a later date (chronologically second)
      spawnSync("git", ["commit", "-m", "First commit (later date)"], {
        cwd: workDir,
        encoding: "utf8",
        env: {
          ...process.env,
          GIT_CONFIG_NOSYSTEM: "1",
          HOME: os.tmpdir(),
          GIT_AUTHOR_DATE: laterDate,
          GIT_COMMITTER_DATE: laterDate,
        },
      });

      fs.writeFileSync(path.join(workDir, "second.txt"), "second\n");
      execGit(["add", "second.txt"], { cwd: workDir });
      // Commit B has an earlier date (chronologically first) – without --topo-order
      // a date-based sort would put this before commit A, which would be wrong.
      spawnSync("git", ["commit", "-m", "Second commit (earlier date)"], {
        cwd: workDir,
        encoding: "utf8",
        env: {
          ...process.env,
          GIT_CONFIG_NOSYSTEM: "1",
          HOME: os.tmpdir(),
          GIT_AUTHOR_DATE: earlierDate,
          GIT_COMMITTER_DATE: earlierDate,
        },
      });

      execGit(["push", "-u", "origin", "topo-order-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "topo-order-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // Both commits must be replayed via GraphQL in DAG order: first → second
      expect(githubClient.graphql).toHaveBeenCalledTimes(2);
      const headlines = githubClient.graphql.mock.calls.map(c => c[1].input.message.headline);
      expect(headlines).toEqual(["First commit (later date)", "Second commit (earlier date)"]);
    });
  });

  // ──────────────────────────────────────────────────────
  // Merge commit fallback
  // ──────────────────────────────────────────────────────

  describe("merge commit fallback", () => {
    it("should refuse unsigned push and not fall back to git push when the commit range contains a merge commit", async () => {
      // Set up: main already has an initial commit. Create a side branch with an extra commit,
      // then merge it back into a feature branch to produce a merge commit in the range.
      execGit(["checkout", "-b", "side-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "side.txt"), "side branch content\n");
      execGit(["add", "side.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Side branch commit"], { cwd: workDir });

      // Back to main, create feature branch, and merge side-branch into it
      execGit(["checkout", "main"], { cwd: workDir });
      execGit(["checkout", "-b", "merge-test-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "feature.txt"), "feature content\n");
      execGit(["add", "feature.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Feature commit"], { cwd: workDir });

      // Merge side-branch – this creates a merge commit with two parents
      execGit(["merge", "--no-ff", "side-branch", "-m", "Merge side-branch into merge-test-branch"], { cwd: workDir });

      // Push initial state so ls-remote has a base
      execGit(["push", "-u", "origin", "merge-test-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "merge-test-branch",
          baseRef: "origin/main",
          cwd: workDir,
        })
      ).rejects.toThrow("refusing unsigned push for branch 'merge-test-branch'");

      // GraphQL must NOT have been called – merge commit is detected pre-flight
      expect(githubClient.graphql).not.toHaveBeenCalled();

      // Warning about the merge commit must be emitted (diagnostic log value preserved)
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringMatching(/merge commit [0-9a-f]{7,40} detected/));
    });

    it("should use direct git push for merge commits when signed commits are disabled", async () => {
      execGit(["checkout", "-b", "unsigned-side-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "unsigned-side.txt"), "side branch content\n");
      execGit(["add", "unsigned-side.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Unsigned side branch commit"], { cwd: workDir });

      execGit(["checkout", "main"], { cwd: workDir });
      execGit(["checkout", "-b", "unsigned-merge-test-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "unsigned-feature.txt"), "feature content\n");
      execGit(["add", "unsigned-feature.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Unsigned feature commit"], { cwd: workDir });
      execGit(["merge", "--no-ff", "unsigned-side-branch", "-m", "Merge unsigned-side-branch into unsigned-merge-test-branch"], { cwd: workDir });
      const expectedHead = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      const pushedSha = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "unsigned-merge-test-branch",
        baseRef: "origin/main",
        cwd: workDir,
        signedCommits: false,
      });

      expect(pushedSha).toBe(expectedHead);
      expect(githubClient.graphql).not.toHaveBeenCalled();
      expect(execGit(["rev-parse", "refs/heads/unsigned-merge-test-branch"], { cwd: bareDir }).stdout.trim()).toBe(expectedHead);
      expect(mockCore.info).toHaveBeenCalledWith("pushSignedCommits: signed-commits disabled (using direct git push) for branch unsigned-merge-test-branch");
    });

    it("should not trigger merge-commit fallback for a commit message that starts with 'parent '", async () => {
      // Regression test: a commit whose message body starts with "parent " must not be misidentified
      // as a merge commit. The old cat-file approach would have counted this as an extra parent.
      execGit(["checkout", "-b", "tricky-message-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "tricky.txt"), "tricky content\n");
      execGit(["add", "tricky.txt"], { cwd: workDir });
      // Write the multi-line commit message to a file to avoid shell interpretation issues
      const msgFile = path.join(workDir, ".git", "TRICKY_MSG");
      fs.writeFileSync(msgFile, "Normal headline\n\nparent this line starts with parent but is not a git parent header\n");
      execGit(["commit", "-F", msgFile], { cwd: workDir });
      execGit(["push", "-u", "origin", "tricky-message-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "tricky-message-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // Must proceed via GraphQL – not incorrectly fallen back to git push
      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("merge commit"));
    });
  });

  // ──────────────────────────────────────────────────────
  // baseRef as a full commit SHA (push_experiment_state path)
  // ──────────────────────────────────────────────────────

  describe("baseRef as a full commit SHA", () => {
    it("should correctly compute rev-list range when baseRef is a 40-char SHA (push_experiment_state real-world path)", async () => {
      // push_experiment_state.cjs records: baseRef = execGitSync(["rev-parse", "HEAD"]).trim()
      // on a pre-existing branch, yielding a full SHA not a symbolic ref.
      execGit(["checkout", "-b", "sha-baseref-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "state.json"), JSON.stringify({ run: 1 }));
      execGit(["add", "state.json"], { cwd: workDir });
      execGit(["commit", "-m", "First state"], { cwd: workDir });
      execGit(["push", "-u", "origin", "sha-baseref-branch"], { cwd: workDir });

      // Record the SHA of the current HEAD (simulates what push_experiment_state does)
      const baseRefSha = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      // Add a new commit that should be picked up by rev-list <sha>..HEAD
      fs.writeFileSync(path.join(workDir, "state.json"), JSON.stringify({ run: 2 }));
      execGit(["add", "state.json"], { cwd: workDir });
      execGit(["commit", "-m", "Second state"], { cwd: workDir });
      execGit(["push", "origin", "sha-baseref-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "sha-baseref-branch",
        baseRef: baseRefSha, // full 40-char SHA, not a branch ref
        cwd: workDir,
      });

      // Only the new commit must be found and sent to GraphQL
      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.message.headline).toBe("Second state");
    });
  });

  // ──────────────────────────────────────────────────────
  // Binary file content (readBlobAsBase64 binary-safety)
  // ──────────────────────────────────────────────────────

  describe("binary file content", () => {
    it("should base64-encode binary files without corruption (readBlobAsBase64 binary-safe path)", async () => {
      // readBlobAsBase64 uses exec.exec with a listeners.stdout Buffer callback to avoid
      // the UTF-8 decoding that exec.getExecOutput applies. This test verifies that binary
      // bytes (including NUL, 0xFF, 0xFE, and bytes invalid in UTF-8) are preserved.
      execGit(["checkout", "-b", "binary-branch"], { cwd: workDir });

      // Arbitrary binary bytes that are NOT valid UTF-8.  0x89 0x50 0x4E 0x47 is the PNG
      // magic header; 0x00 0xFF 0xFE are bytes that would be corrupted by UTF-8 decoding.
      const binaryContent = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe]);
      fs.writeFileSync(path.join(workDir, "image.bin"), binaryContent);
      execGit(["add", "image.bin"], { cwd: workDir });
      execGit(["commit", "-m", "Add binary file"], { cwd: workDir });
      execGit(["push", "-u", "origin", "binary-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "binary-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.fileChanges.additions).toHaveLength(1);
      expect(callArg.fileChanges.additions[0].path).toBe("image.bin");

      // Decode the base64 payload and verify every byte is intact
      const decoded = Buffer.from(callArg.fileChanges.additions[0].contents, "base64");
      expect(decoded.equals(binaryContent)).toBe(true);
    });
  });

  // ──────────────────────────────────────────────────────
  // Orphan branch with multiple commits (baseRef="")
  // ──────────────────────────────────────────────────────

  describe("orphan branch with multiple commits (empty baseRef)", () => {
    it("should push all commits when orphan branch has more than one commit", async () => {
      // The single-commit orphan test verifies the happy path. This test ensures that
      // git push (not just the first commit) lands all local commits on the remote.
      execGit(["checkout", "--orphan", "experiments/multi"], { cwd: workDir });
      execGit(["read-tree", "--empty"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "state.json"), JSON.stringify({ run: 1 }));
      execGit(["add", "state.json"], { cwd: workDir });
      execGit(["commit", "-m", "First experiment commit"], { cwd: workDir });

      const firstSha = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      fs.writeFileSync(path.join(workDir, "meta.json"), JSON.stringify({ ts: 42 }));
      execGit(["add", "meta.json"], { cwd: workDir });
      execGit(["commit", "-m", "Second experiment commit"], { cwd: workDir });

      const expectedSha = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();
      expect(expectedSha).not.toBe(firstSha);

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      const result = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "experiments/multi",
        baseRef: "",
        cwd: workDir,
      });

      // Both commits must be present on the remote
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/experiments/multi"], { cwd: workDir });
      const remoteOid = lsRemote.stdout.trim().split(/\s+/)[0];
      expect(remoteOid).toBe(expectedSha);

      // Return value is the HEAD SHA
      expect(result).toBe(expectedSha);

      // GraphQL must never be called for orphan first push
      expect(githubClient.graphql).not.toHaveBeenCalled();
      expect(mockCore.warning).not.toHaveBeenCalled();
    });
  });

  // ──────────────────────────────────────────────────────
  // Rename with executable bit (R-status + dstMode=100755)
  // ──────────────────────────────────────────────────────

  describe("rename with executable bit", () => {
    it("should warn about executable bit loss on renamed destination but continue with GraphQL", async () => {
      // git diff-tree detects renames (diff.renames=true by default).
      // When the renamed destination has mode 100755, production code (line 247) warns
      // but does NOT fall back to git push. This path has no coverage without this test.
      execGit(["checkout", "-b", "rename-exec-branch"], { cwd: workDir });

      fs.writeFileSync(path.join(workDir, "script.sh"), "#!/bin/bash\necho hello\n");
      execGit(["add", "script.sh"], { cwd: workDir });
      execGit(["commit", "-m", "Add script.sh"], { cwd: workDir });
      execGit(["push", "-u", "origin", "rename-exec-branch"], { cwd: workDir });

      // Rename and set executable bit on the destination
      fs.renameSync(path.join(workDir, "script.sh"), path.join(workDir, "run.sh"));
      fs.chmodSync(path.join(workDir, "run.sh"), 0o755);
      execGit(["add", "-A"], { cwd: workDir });
      execGit(["commit", "-m", "Rename script.sh to run.sh with exec bit"], { cwd: workDir });
      execGit(["push", "origin", "rename-exec-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "rename-exec-branch",
        baseRef: "rename-exec-branch^",
        cwd: workDir,
      });

      // GraphQL must still be called – executable bit loss is a warning, not a fallback
      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
      // Warning about executable bit loss on the renamed destination
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("executable bit on run.sh will be lost in signed commit"));

      // Payload: original path deleted, new path added with correct content
      const callArg = githubClient.graphql.mock.calls[0][1].input;
      expect(callArg.fileChanges.deletions).toContainEqual({ path: "script.sh" });
      expect(callArg.fileChanges.additions.find(a => a.path === "run.sh")).toBeTruthy();
      const decoded = Buffer.from(callArg.fileChanges.additions.find(a => a.path === "run.sh").contents, "base64").toString();
      expect(decoded).toContain("echo hello");
    });
  });

  // ──────────────────────────────────────────────────────
  // Deleted submodule (D status + srcMode=160000) fallback
  // ──────────────────────────────────────────────────────

  describe("deleted submodule fallback", () => {
    it("should refuse unsigned push and not fall back to git push when a submodule entry is deleted", async () => {
      // The existing submodule test only covers ADDING a submodule.
      // This test covers the D-status + srcMode=160000 code path at line 226,
      // where a previously-committed gitlink entry is removed in a new commit.
      execGit(["checkout", "-b", "submodule-delete-branch"], { cwd: workDir });

      // Add a fake gitlink (submodule) via update-index, commit, and push
      const headSha = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();
      execGit(["update-index", "--add", "--cacheinfo", `160000,${headSha},mysubmodule`], { cwd: workDir });
      execGit(["commit", "-m", "Add submodule"], { cwd: workDir });
      execGit(["push", "-u", "origin", "submodule-delete-branch"], { cwd: workDir });

      // Now remove the submodule entry and commit
      execGit(["update-index", "--remove", "mysubmodule"], { cwd: workDir });
      execGit(["commit", "-m", "Remove submodule"], { cwd: workDir });
      execGit(["push", "origin", "submodule-delete-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "submodule-delete-branch",
          // Only replay the delete commit
          baseRef: "submodule-delete-branch^",
          cwd: workDir,
        })
      ).rejects.toThrow("refusing unsigned push for branch 'submodule-delete-branch'");

      // GraphQL must NOT be called – deleted submodule is detected pre-flight
      expect(githubClient.graphql).not.toHaveBeenCalled();
      // Warning about submodule detection (diagnostic log value preserved)
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("submodule change detected in mysubmodule"));
    });
  });

  describe("stale-base and synthesized payload safety", () => {
    it("should fall back to an unsigned push of the un-rebased commits when rebasing stale commits onto current base conflicts", async () => {
      // Base branch starts with shared file.
      fs.writeFileSync(path.join(workDir, "shared.txt"), "base\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add shared file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges from old main and edits shared.txt.
      execGit(["checkout", "-b", "stale-conflict-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "agent change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit shared"], { cwd: workDir });
      const localOidBeforePush = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

      // Base branch advances with conflicting edit.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "upstream change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit shared"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "stale-conflict-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      const result = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "stale-conflict-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // GraphQL is never invoked for this path — the un-rebased commits are pushed directly.
      expect(githubClient.graphql).not.toHaveBeenCalled();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("failed to rebase commit range onto current GraphQL parent"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Falling back to an unsigned git push of the un-rebased commit(s)"));

      // The branch was pushed as-is (still based on the stale parent) so GitHub can create the
      // pull request and surface the conflict for manual resolution, instead of the whole
      // operation failing outright.
      expect(result).toBe(localOidBeforePush);
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/stale-conflict-branch"], { cwd: workDir });
      const remoteOid = lsRemote.stdout.trim().split(/\s+/)[0];
      expect(remoteOid).toBe(localOidBeforePush);
    });

    it("should surface a combined error (not swallow it) when the unsigned push fallback is itself rejected, e.g. by branch protection requiring signed commits", async () => {
      // Simulate a branch-protection rule that requires signed commits by rejecting every
      // push with a pre-receive hook on the bare "remote" repo.
      const preReceiveHookPath = path.join(bareDir, "hooks", "pre-receive");
      fs.writeFileSync(
        preReceiveHookPath,
        "#!/bin/sh\n" +
          "while read oldrev newrev refname; do\n" +
          '  case "$refname" in\n' +
          "    refs/heads/protected-conflict-branch)\n" +
          '      echo "error: commits must be signed" >&2\n' +
          "      exit 1\n" +
          "      ;;\n" +
          "  esac\n" +
          "done\n" +
          "exit 0\n"
      );
      fs.chmodSync(preReceiveHookPath, 0o755);

      // Base branch starts with shared file.
      fs.writeFileSync(path.join(workDir, "shared.txt"), "base\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add shared file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges from old main and edits shared.txt.
      execGit(["checkout", "-b", "protected-conflict-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "agent change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit shared"], { cwd: workDir });

      // Base branch advances with conflicting edit.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "upstream change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit shared"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "protected-conflict-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "protected-conflict-branch",
          baseRef: "origin/main",
          cwd: workDir,
        })
      ).rejects.toThrow(/failed to rebase commit range onto current GraphQL parent[\s\S]*Unsigned git push fallback was also rejected/);

      expect(githubClient.graphql).not.toHaveBeenCalled();
      // The rejected push must not have landed on the "remote".
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/protected-conflict-branch"], { cwd: workDir });
      expect(lsRemote.stdout.trim()).toBe("");
    });

    it("should refuse the unsigned push fallback (not push) when the un-rebased conflicting range violates a file-protection policy", async () => {
      // Base branch starts with shared file.
      fs.writeFileSync(path.join(workDir, "shared.txt"), "base\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add shared file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges from old main, edits shared.txt AND touches a protected file.
      execGit(["checkout", "-b", "policy-conflict-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "agent change\n");
      fs.writeFileSync(path.join(workDir, "CODEOWNERS"), "* @octocat\n");
      execGit(["add", "shared.txt", "CODEOWNERS"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit shared and touch CODEOWNERS"], { cwd: workDir });

      // Base branch advances with conflicting edit.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "upstream change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit shared"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "policy-conflict-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "policy-conflict-branch",
          baseRef: "origin/main",
          cwd: workDir,
          validationConfig: {
            protected_files: ["CODEOWNERS"],
            protected_files_policy: "blocked",
          },
        })
      ).rejects.toThrow("Signed-commit payload violates file-protection policy");

      // The blocked commits must not be pushed unsigned just because the rebase conflicted.
      expect(githubClient.graphql).not.toHaveBeenCalled();
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/policy-conflict-branch"], { cwd: workDir });
      expect(lsRemote.stdout.trim()).toBe("");
    });

    it("should still fail (without pushing) when git push fallback is explicitly disabled and rebasing stale commits conflicts", async () => {
      // Base branch starts with shared file.
      fs.writeFileSync(path.join(workDir, "shared.txt"), "base\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add shared file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges from old main and edits shared.txt.
      execGit(["checkout", "-b", "stale-conflict-no-fallback-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "agent change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit shared"], { cwd: workDir });

      // Base branch advances with conflicting edit.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "upstream change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit shared"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "stale-conflict-no-fallback-branch"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "stale-conflict-no-fallback-branch",
          baseRef: "origin/main",
          cwd: workDir,
          allowGitPushFallback: false,
        })
      ).rejects.toThrow("failed to rebase commit range onto current GraphQL parent");

      expect(githubClient.graphql).not.toHaveBeenCalled();
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/stale-conflict-no-fallback-branch"], { cwd: workDir });
      expect(lsRemote.stdout.trim()).toBe("");
    });

    it("should fail fatally (without attempting any push) when 'git rebase --abort' itself fails after a genuine conflict", async () => {
      // Base branch starts with shared file.
      fs.writeFileSync(path.join(workDir, "shared.txt"), "base\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add shared file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges from old main and edits shared.txt.
      execGit(["checkout", "-b", "abort-failure-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "agent change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit shared"], { cwd: workDir });

      // Base branch advances with conflicting edit.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "upstream change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit shared"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "abort-failure-branch"], { cwd: workDir });

      // Wrap the real exec so that only "git rebase --abort" is forced to fail, simulating a
      // broken worktree (e.g. a corrupted rebase-merge state) that prevents cleanup.
      const realExec = makeRealExec(workDir);
      global.exec = {
        ...realExec,
        exec: async (program, args, opts) => {
          if (program === "git" && args[0] === "rebase" && args[1] === "--abort") {
            throw new Error("simulated: could not restore original branch state");
          }
          return realExec.exec(program, args, opts);
        },
      };
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "abort-failure-branch",
          baseRef: "origin/main",
          cwd: workDir,
        })
      ).rejects.toThrow(/git rebase --abort.*also failed/);

      // Nothing must have been pushed — the abort failure must short-circuit before any
      // unsigned-push fallback is attempted.
      expect(githubClient.graphql).not.toHaveBeenCalled();
      const lsRemote = execGit(["ls-remote", bareDir, "refs/heads/abort-failure-branch"], { cwd: workDir });
      expect(lsRemote.stdout.trim()).toBe("");
    });

    it("should recover from a partial-clone object failure by backfilling the exact commit objects and retrying the rebase", async () => {
      // Base branch starts with a file.
      fs.writeFileSync(path.join(workDir, "base.txt"), "base\n");
      execGit(["add", "base.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add base file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges from old main and edits a DIFFERENT file (no conflict).
      execGit(["checkout", "-b", "promisor-recover-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "agent.txt"), "agent change\n");
      execGit(["add", "agent.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit"], { cwd: workDir });

      // Base branch advances with a non-conflicting edit.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "upstream.txt"), "upstream change\n");
      execGit(["add", "upstream.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "promisor-recover-branch"], { cwd: workDir });

      // Wrap the real exec so the FIRST `git rebase --onto` simulates a
      // promisor object fetch failure, the targeted `git fetch --no-filter`
      // backfill is intercepted to succeed (the test repo is not actually a
      // partial clone), and the SECOND rebase runs for real and succeeds.
      const realExec = makeRealExec(workDir);
      let rebaseAttempts = 0;
      /** @type {any} */
      let backfillTargets = null;
      global.exec = {
        getExecOutput: async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "rebase" && args[1] === "--onto") {
            rebaseAttempts++;
            if (rebaseAttempts === 1) {
              return {
                exitCode: 128,
                stdout: "",
                stderr: "fatal: remote error: upload-pack: not our ref 0035eb55fe03ab52d8b95e7fcfaee53548b5e8d6\nfatal: could not fetch 4f0af08119278bacff5772a1ddf987d4b4045be8 from promisor remote\n",
              };
            }
          }
          if (program === "git" && args[0] === "fetch" && args.includes("--no-filter")) {
            // Targeted backfill of the exact anchor commit SHAs; must NOT use
            // --unshallow or --deepen.
            expect(args).not.toContain("--unshallow");
            expect(args.some(arg => String(arg).startsWith("--deepen"))).toBe(false);
            backfillTargets = args.slice(args.indexOf("origin") + 1);
            return { exitCode: 0, stdout: "", stderr: "" };
          }
          return realExec.getExecOutput(program, args, opts);
        },
        exec: realExec.exec,
      };

      const githubClient = makeMockGithubClient();

      const result = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "promisor-recover-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // The rebase was attempted twice (initial failure + post-recovery retry).
      expect(rebaseAttempts).toBe(2);
      // The recovery fetched a bounded set of exact commit SHAs (anchors), not
      // an unshallow / deepen of the whole history.
      expect(backfillTargets).not.toBeNull();
      expect(backfillTargets.length).toBeGreaterThan(0);
      expect(backfillTargets.every(sha => /^[0-9a-f]{40}$/i.test(sha))).toBe(true);
      // The signed-commit GraphQL replay proceeded after recovery.
      expect(githubClient.graphql).toHaveBeenCalled();
      expect(result).toBeDefined();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("backfilling object content"));
    });

    it("should not attempt history backfill for a genuine merge conflict", async () => {
      // Base branch starts with shared file.
      fs.writeFileSync(path.join(workDir, "shared.txt"), "base\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add shared file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges and edits shared.txt.
      execGit(["checkout", "-b", "conflict-no-backfill-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "agent change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit shared"], { cwd: workDir });

      // Base branch advances with a conflicting edit.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "upstream change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit shared"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "conflict-no-backfill-branch"], { cwd: workDir });

      const realExec = makeRealExec(workDir);
      let backfillAttempted = false;
      global.exec = {
        getExecOutput: async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "fetch" && (args.includes("--no-filter") || args.includes("--unshallow") || args.some(arg => String(arg).startsWith("--deepen")))) {
            backfillAttempted = true;
          }
          return realExec.getExecOutput(program, args, opts);
        },
        exec: realExec.exec,
      };

      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "conflict-no-backfill-branch",
          baseRef: "origin/main",
          cwd: workDir,
          allowGitPushFallback: false,
        })
      ).rejects.toThrow("failed to rebase commit range onto current GraphQL parent");

      // A genuine merge conflict must not trigger the partial-clone backfill path.
      expect(backfillAttempted).toBe(false);
      expect(githubClient.graphql).not.toHaveBeenCalled();
    });

    it("should fall back to an unsigned push when a genuine merge conflict is only revealed after a successful object backfill", async () => {
      // Base branch starts with a shared file.
      fs.writeFileSync(path.join(workDir, "shared.txt"), "base\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add shared file"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      // Agent branch diverges and edits the shared file.
      execGit(["checkout", "-b", "backfill-then-conflict-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "agent change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Agent edit shared"], { cwd: workDir });

      // Base branch advances with a conflicting edit to the same file.
      execGit(["checkout", "main"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "shared.txt"), "upstream change\n");
      execGit(["add", "shared.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Upstream edit shared"], { cwd: workDir });
      execGit(["push", "origin", "main"], { cwd: workDir });

      execGit(["checkout", "backfill-then-conflict-branch"], { cwd: workDir });

      // Simulate the FIRST rebase attempt failing due to a promisor object-fetch
      // failure (recoverable), the backfill "succeeding" (mocked), and then the
      // SECOND (real) rebase attempt hitting the genuine content conflict.
      const realExec = makeRealExec(workDir);
      let rebaseAttempts = 0;
      global.exec = {
        getExecOutput: async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "rebase" && args[1] === "--onto") {
            rebaseAttempts++;
            if (rebaseAttempts === 1) {
              return {
                exitCode: 128,
                stdout: "",
                stderr: "fatal: remote error: upload-pack: not our ref 0035eb55fe03ab52d8b95e7fcfaee53548b5e8d6\nfatal: could not fetch 4f0af08119278bacff5772a1ddf987d4b4045be8 from promisor remote\n",
              };
            }
          }
          if (program === "git" && args[0] === "fetch" && args.includes("--no-filter")) {
            return { exitCode: 0, stdout: "", stderr: "" };
          }
          return realExec.getExecOutput(program, args, opts);
        },
        exec: realExec.exec,
      };

      const githubClient = makeMockGithubClient();

      const result = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: "backfill-then-conflict-branch",
        baseRef: "origin/main",
        cwd: workDir,
      });

      // The rebase was attempted twice (initial promisor failure + post-backfill
      // retry, which then hits the real content conflict).
      expect(rebaseAttempts).toBe(2);
      // The genuine post-backfill conflict falls back to an unsigned push instead
      // of throwing, and no GraphQL replay is attempted.
      expect(githubClient.graphql).not.toHaveBeenCalled();
      expect(result).toBeDefined();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("even after backfilling the required commit objects"));

      // The original (un-rebased) agent commit landed on the remote as-is.
      const remoteLog = execGit(["log", "--format=%s", "backfill-then-conflict-branch"], { cwd: bareDir }).stdout.trim().split("\n");
      expect(remoteLog).toContain("Agent edit shared");
    });

    it("should merge state.json conflicts when a custom resolver is provided", async () => {
      const concurrentDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-signed-concurrent-"));
      try {
        execGit(["checkout", "-b", "experiment-state-merge-branch"], { cwd: workDir });
        const baseState = {
          counts: { prompt_style: { concise: 1, detailed: 1 } },
          runs: [{ run_id: "100", timestamp: "2026-07-31T12:00:00.000Z", assignments: { prompt_style: "concise" } }],
        };
        fs.writeFileSync(path.join(workDir, "state.json"), JSON.stringify(baseState, null, 2) + "\n");
        fs.writeFileSync(path.join(workDir, "assignments.json"), JSON.stringify({ prompt_style: "concise" }, null, 2) + "\n");
        execGit(["add", "state.json", "assignments.json"], { cwd: workDir });
        execGit(["commit", "-m", "Seed experiment state"], { cwd: workDir });
        execGit(["push", "-u", "origin", "experiment-state-merge-branch"], { cwd: workDir });

        const baseRef = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

        execGit(["clone", bareDir, "."], { cwd: concurrentDir });
        execGit(["config", "user.name", "Test User"], { cwd: concurrentDir });
        execGit(["config", "user.email", "test@example.com"], { cwd: concurrentDir });

        const localState = {
          counts: { prompt_style: { concise: 2, detailed: 1 } },
          runs: [...baseState.runs, { run_id: "200", timestamp: "2026-07-31T12:01:00.000Z", assignments: { prompt_style: "concise" } }],
        };
        fs.writeFileSync(path.join(workDir, "state.json"), JSON.stringify(localState, null, 2) + "\n");
        fs.writeFileSync(path.join(workDir, "assignments.json"), JSON.stringify({ prompt_style: "concise" }, null, 2) + "\n");
        execGit(["add", "state.json", "assignments.json"], { cwd: workDir });
        execGit(["commit", "-m", "Local experiment update"], { cwd: workDir });

        execGit(["checkout", "experiment-state-merge-branch"], { cwd: concurrentDir });
        const remoteState = {
          counts: { prompt_style: { concise: 2, detailed: 1 } },
          runs: [...baseState.runs, { run_id: "300", timestamp: "2026-07-31T12:02:00.000Z", assignments: { prompt_style: "concise" } }],
        };
        fs.writeFileSync(path.join(concurrentDir, "state.json"), JSON.stringify(remoteState, null, 2) + "\n");
        fs.writeFileSync(path.join(concurrentDir, "assignments.json"), JSON.stringify({ prompt_style: "concise" }, null, 2) + "\n");
        execGit(["add", "state.json", "assignments.json"], { cwd: concurrentDir });
        execGit(["commit", "-m", "Remote experiment update"], { cwd: concurrentDir });
        execGit(["push", "origin", "experiment-state-merge-branch"], { cwd: concurrentDir });
        execGit(["fetch", "origin", "refs/heads/experiment-state-merge-branch"], { cwd: workDir });

        global.exec = makeRealExec(workDir);
        const githubClient = makeMockGithubClient();

        await pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "experiment-state-merge-branch",
          baseRef,
          cwd: workDir,
          resolveRebaseConflict: resolveExperimentStateRebaseConflict,
        });

        expect(githubClient.graphql).toHaveBeenCalledTimes(1);
        const additions = githubClient.graphql.mock.calls[0][1].input.fileChanges.additions;
        const stateAddition = additions.find(entry => entry.path === "state.json");
        expect(stateAddition).toBeDefined();
        const mergedState = JSON.parse(Buffer.from(stateAddition.contents, "base64").toString("utf8"));
        expect(mergedState.counts.prompt_style.concise).toBe(3);
        expect(mergedState.counts.prompt_style.detailed).toBe(1);
        expect(mergedState.runs).toHaveLength(3);
        expect(mergedState.runs.map(run => run.run_id).sort()).toEqual(["100", "200", "300"]);
      } finally {
        cleanupDir(concurrentDir);
      }
    });

    it("should merge state.jsonl conflicts when a custom resolver is provided", async () => {
      const concurrentDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-signed-concurrent-jsonl-"));
      try {
        execGit(["checkout", "-b", "experiment-state-jsonl-merge-branch"], { cwd: workDir });
        const seedRuns = [
          { run_id: "100", timestamp: "2026-07-31T12:00:00.000Z", assignments: { prompt_style: "concise" } },
          { run_id: "101", timestamp: "2026-07-31T12:00:30.000Z", assignments: { prompt_style: "detailed" } },
        ];
        fs.writeFileSync(path.join(workDir, "state.jsonl"), `${seedRuns.map(run => JSON.stringify(run)).join("\n")}\n`);
        fs.writeFileSync(path.join(workDir, "assignments.json"), JSON.stringify({ prompt_style: "concise" }, null, 2) + "\n");
        execGit(["add", "state.jsonl", "assignments.json"], { cwd: workDir });
        execGit(["commit", "-m", "Seed experiment state jsonl"], { cwd: workDir });
        execGit(["push", "-u", "origin", "experiment-state-jsonl-merge-branch"], { cwd: workDir });

        const baseRef = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

        execGit(["clone", bareDir, "."], { cwd: concurrentDir });
        execGit(["config", "user.name", "Test User"], { cwd: concurrentDir });
        execGit(["config", "user.email", "test@example.com"], { cwd: concurrentDir });

        fs.writeFileSync(path.join(workDir, "state.jsonl"), `${seedRuns.map(run => JSON.stringify(run)).join("\n")}\n${JSON.stringify({ run_id: "200", timestamp: "2026-07-31T12:01:00.000Z", assignments: { prompt_style: "concise" } })}\n`);
        fs.writeFileSync(path.join(workDir, "assignments.json"), JSON.stringify({ prompt_style: "concise" }, null, 2) + "\n");
        execGit(["add", "state.jsonl", "assignments.json"], { cwd: workDir });
        execGit(["commit", "-m", "Local experiment update jsonl"], { cwd: workDir });

        execGit(["checkout", "experiment-state-jsonl-merge-branch"], { cwd: concurrentDir });
        fs.writeFileSync(
          path.join(concurrentDir, "state.jsonl"),
          `${seedRuns.map(run => JSON.stringify(run)).join("\n")}\n${JSON.stringify({ run_id: "300", timestamp: "2026-07-31T12:02:00.000Z", assignments: { prompt_style: "concise" } })}\n`
        );
        fs.writeFileSync(path.join(concurrentDir, "assignments.json"), JSON.stringify({ prompt_style: "concise" }, null, 2) + "\n");
        execGit(["add", "state.jsonl", "assignments.json"], { cwd: concurrentDir });
        execGit(["commit", "-m", "Remote experiment update jsonl"], { cwd: concurrentDir });
        execGit(["push", "origin", "experiment-state-jsonl-merge-branch"], { cwd: concurrentDir });
        execGit(["fetch", "origin", "refs/heads/experiment-state-jsonl-merge-branch"], { cwd: workDir });

        global.exec = makeRealExec(workDir);
        const githubClient = makeMockGithubClient();

        await pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "experiment-state-jsonl-merge-branch",
          baseRef,
          cwd: workDir,
          resolveRebaseConflict: resolveExperimentStateRebaseConflict,
        });

        expect(githubClient.graphql).toHaveBeenCalledTimes(1);
        const additions = githubClient.graphql.mock.calls[0][1].input.fileChanges.additions;
        const stateAddition = additions.find(entry => entry.path === "state.jsonl");
        expect(stateAddition).toBeDefined();
        const mergedLines = Buffer.from(stateAddition.contents, "base64")
          .toString("utf8")
          .trim()
          .split("\n")
          .map(line => JSON.parse(line));
        const runIds = mergedLines
          .filter(entry => entry.run_id)
          .map(entry => entry.run_id)
          .sort();
        expect(runIds).toEqual(["100", "101", "200", "300"]);
      } finally {
        cleanupDir(concurrentDir);
      }
    });

    it("should merge evals.jsonl conflicts when configured as append-only state file", async () => {
      const concurrentDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-signed-concurrent-evals-"));
      const originalStateFiles = process.env.GH_AW_STATE_FILES;
      process.env.GH_AW_STATE_FILES = "evals.jsonl";
      try {
        execGit(["checkout", "-b", "evals-state-merge-branch"], { cwd: workDir });
        const seed = { id: "seed", timestamp: "2026-07-31T12:00:00.000Z", runid: "100" };
        fs.writeFileSync(path.join(workDir, "evals.jsonl"), `${JSON.stringify(seed)}\n`);
        execGit(["add", "evals.jsonl"], { cwd: workDir });
        execGit(["commit", "-m", "Seed eval state"], { cwd: workDir });
        execGit(["push", "-u", "origin", "evals-state-merge-branch"], { cwd: workDir });

        const baseRef = execGit(["rev-parse", "HEAD"], { cwd: workDir }).stdout.trim();

        execGit(["clone", bareDir, "."], { cwd: concurrentDir });
        execGit(["config", "user.name", "Test User"], { cwd: concurrentDir });
        execGit(["config", "user.email", "test@example.com"], { cwd: concurrentDir });

        const shared = { id: "shared", timestamp: "2026-07-31T12:01:00.000Z", runid: "101" };
        const local = { id: "local", timestamp: "2026-07-31T12:02:00.000Z", runid: "102" };
        fs.writeFileSync(path.join(workDir, "evals.jsonl"), `${JSON.stringify(seed)}\n${JSON.stringify(shared)}\n${JSON.stringify(local)}\n`);
        execGit(["add", "evals.jsonl"], { cwd: workDir });
        execGit(["commit", "-m", "Local eval update"], { cwd: workDir });

        execGit(["checkout", "evals-state-merge-branch"], { cwd: concurrentDir });
        const remote = { id: "remote", timestamp: "2026-07-31T12:03:00.000Z", runid: "103" };
        fs.writeFileSync(path.join(concurrentDir, "evals.jsonl"), `${JSON.stringify(seed)}\n${JSON.stringify(shared)}\n${JSON.stringify(remote)}\n`);
        execGit(["add", "evals.jsonl"], { cwd: concurrentDir });
        execGit(["commit", "-m", "Remote eval update"], { cwd: concurrentDir });
        execGit(["push", "origin", "evals-state-merge-branch"], { cwd: concurrentDir });
        execGit(["fetch", "origin", "refs/heads/evals-state-merge-branch"], { cwd: workDir });

        global.exec = makeRealExec(workDir);
        const githubClient = makeMockGithubClient();

        await pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "evals-state-merge-branch",
          baseRef,
          cwd: workDir,
          resolveRebaseConflict: resolveExperimentStateRebaseConflict,
        });

        expect(githubClient.graphql).toHaveBeenCalledTimes(1);
        const additions = githubClient.graphql.mock.calls[0][1].input.fileChanges.additions;
        const stateAddition = additions.find(entry => entry.path === "evals.jsonl");
        expect(stateAddition).toBeDefined();
        const mergedLines = Buffer.from(stateAddition.contents, "base64")
          .toString("utf8")
          .trim()
          .split("\n")
          .map(line => JSON.parse(line));
        expect(mergedLines.map(entry => entry.id)).toEqual(["seed", "shared", "remote", "local"]);
      } finally {
        cleanupDir(concurrentDir);
        if (originalStateFiles === undefined) {
          delete process.env.GH_AW_STATE_FILES;
        } else {
          process.env.GH_AW_STATE_FILES = originalStateFiles;
        }
      }
    });

    it("should enforce protected-files policy against synthesized GraphQL payload", async () => {
      execGit(["checkout", "-b", "protected-payload-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "CODEOWNERS"), "* @octocat\n");
      execGit(["add", "CODEOWNERS"], { cwd: workDir });
      execGit(["commit", "-m", "Touch CODEOWNERS"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "protected-payload-branch",
          baseRef: "origin/main",
          cwd: workDir,
          validationConfig: {
            protected_files: ["CODEOWNERS"],
            protected_files_policy: "blocked",
          },
        })
      ).rejects.toThrow("Signed-commit payload violates file-protection policy");

      expect(githubClient.graphql).not.toHaveBeenCalled();
    });

    it("should allow request_review protected-files policy against synthesized GraphQL payload", async () => {
      execGit(["checkout", "-b", "protected-payload-request-review-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "CODEOWNERS"), "* @octocat\n");
      execGit(["add", "CODEOWNERS"], { cwd: workDir });
      execGit(["commit", "-m", "Touch CODEOWNERS"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "protected-payload-request-review-branch",
          baseRef: "origin/main",
          cwd: workDir,
          validationConfig: {
            protected_files: ["CODEOWNERS"],
            protected_files_policy: "request_review",
          },
        })
      ).resolves.toBe("signed-oid-abc123");

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
    });

    it("should allow fallback-to-issue protected-files policy against synthesized GraphQL payload", async () => {
      execGit(["checkout", "-b", "protected-payload-fallback-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "CODEOWNERS"), "* @octocat\n");
      execGit(["add", "CODEOWNERS"], { cwd: workDir });
      execGit(["commit", "-m", "Touch CODEOWNERS"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "protected-payload-fallback-branch",
          baseRef: "origin/main",
          cwd: workDir,
          validationConfig: {
            protected_files: ["CODEOWNERS"],
            protected_files_policy: "fallback-to-issue",
          },
        })
      ).resolves.toBe("signed-oid-abc123");

      expect(githubClient.graphql).toHaveBeenCalledTimes(1);
    });

    it("should enforce max-patch-files against synthesized GraphQL payload", async () => {
      execGit(["checkout", "-b", "max-files-payload-branch"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "alpha.txt"), "alpha\n");
      fs.writeFileSync(path.join(workDir, "beta.txt"), "beta\n");
      execGit(["add", "alpha.txt", "beta.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Touch two files"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      await expect(
        pushSignedCommits({
          githubClient,
          owner: "test-owner",
          repo: "test-repo",
          branch: "max-files-payload-branch",
          baseRef: "origin/main",
          cwd: workDir,
          validationConfig: {
            max_patch_files: 1,
          },
        })
      ).rejects.toThrow("exceeds max-patch-files");

      expect(githubClient.graphql).not.toHaveBeenCalled();
    });
  });

  // ──────────────────────────────────────────────────────
  // Fork-backed push (pushRemoteUrl + pushToken)
  // ──────────────────────────────────────────────────────

  describe("fork-backed push via pushRemoteUrl", () => {
    let forkBareDir;

    beforeEach(() => {
      forkBareDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-signed-fork-"));
      execGit(["init", "--bare"], { cwd: forkBareDir });
      execGit(["symbolic-ref", "HEAD", "refs/heads/main"], { cwd: forkBareDir });
      // Seed the fork with the same initial commit as origin so pushes have a common base
      execGit(["push", forkBareDir, "main"], { cwd: workDir });
    });

    afterEach(() => {
      cleanupDir(forkBareDir);
    });

    it("routes direct git push to the fork remote, not origin, when signedCommits=false", async () => {
      const branchName = "fork/direct-push-branch";
      execGit(["checkout", "-b", branchName], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "fork-file.txt"), "fork content\n");
      execGit(["add", "fork-file.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add fork-file.txt"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      const headSha = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: branchName,
        baseRef: "origin/main",
        cwd: workDir,
        pushRemoteUrl: forkBareDir,
        pushToken: "fake-token",
        signedCommits: false,
      });

      // The returned SHA must be a valid 40-char OID
      expect(headSha).toMatch(/^[0-9a-f]{40}$/i);

      // The branch must exist on the fork bare repo
      const forkRefs = execGit(["ls-remote", "--heads", forkBareDir, `refs/heads/${branchName}`], { cwd: workDir });
      expect(forkRefs.stdout).toContain(branchName);

      // The branch must NOT exist on origin (upstream)
      const originRefs = execGit(["ls-remote", "--heads", bareDir, `refs/heads/${branchName}`], { cwd: workDir });
      expect(originRefs.stdout).toBe("");

      // GraphQL must not have been called (signedCommits=false)
      expect(githubClient.graphql).not.toHaveBeenCalled();
    });

    it("routes git push fallback to the fork remote when GraphQL mutation fails", async () => {
      const branchName = "fork/graphql-fallback-branch";
      execGit(["checkout", "-b", branchName], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "fallback.txt"), "fallback content\n");
      execGit(["add", "fallback.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add fallback.txt"], { cwd: workDir });

      // Push the branch to the fork so ls-remote can probe its current tip
      execGit(["push", forkBareDir, branchName], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient({ failWithError: new Error("GraphQL unavailable") });

      const headSha = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: branchName,
        baseRef: "origin/main",
        cwd: workDir,
        pushRemoteUrl: forkBareDir,
        pushToken: "fake-token",
      });

      expect(headSha).toMatch(/^[0-9a-f]{40}$/i);

      // Branch must exist on the fork
      const forkRefs = execGit(["ls-remote", "--heads", forkBareDir, `refs/heads/${branchName}`], { cwd: workDir });
      expect(forkRefs.stdout).toContain(branchName);

      // Branch must NOT exist on origin
      const originRefs = execGit(["ls-remote", "--heads", bareDir, `refs/heads/${branchName}`], { cwd: workDir });
      expect(originRefs.stdout).toBe("");
    });

    it("routes ls-remote probe to the fork so a new fork branch is detected as absent", async () => {
      const branchName = "fork/ls-remote-probe-branch";
      execGit(["checkout", "-b", branchName], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "probe.txt"), "probe content\n");
      execGit(["add", "probe.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Add probe.txt"], { cwd: workDir });

      // The branch exists on origin but NOT on the fork bare repo.
      // This means lsRemoteHeadOid should see no existing tip on the fork,
      // which forces the GraphQL createRef path.
      execGit(["push", "origin", branchName], { cwd: workDir });

      const lsRemoteTargets = [];
      const realExec = makeRealExec(workDir);
      global.exec = {
        getExecOutput: async (program, args, opts = {}) => {
          if (program === "git" && args[0] === "ls-remote") {
            lsRemoteTargets.push(args[1]);
          }
          return realExec.getExecOutput(program, args, opts);
        },
        exec: realExec.exec,
      };

      const githubClient = makeMockGithubClient();

      await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: branchName,
        baseRef: "origin/main",
        cwd: workDir,
        pushRemoteUrl: forkBareDir,
        pushToken: "fake-token",
      });

      // Every ls-remote call must target the fork bare directory, not "origin"
      expect(lsRemoteTargets.length).toBeGreaterThan(0);
      for (const target of lsRemoteTargets) {
        expect(target).toBe(forkBareDir);
        expect(target).not.toBe("origin");
      }
    });

    it("pushes a new orphan branch to the fork when baseRef is empty", async () => {
      const branchName = "fork/orphan-branch";
      execGit(["checkout", "--orphan", branchName], { cwd: workDir });
      execGit(["reset", "--hard"], { cwd: workDir });
      fs.writeFileSync(path.join(workDir, "orphan.txt"), "first commit\n");
      execGit(["add", "orphan.txt"], { cwd: workDir });
      execGit(["commit", "-m", "Orphan first commit"], { cwd: workDir });

      global.exec = makeRealExec(workDir);
      const githubClient = makeMockGithubClient();

      const headSha = await pushSignedCommits({
        githubClient,
        owner: "test-owner",
        repo: "test-repo",
        branch: branchName,
        baseRef: "",
        cwd: workDir,
        pushRemoteUrl: forkBareDir,
        pushToken: "fake-token",
      });

      expect(headSha).toMatch(/^[0-9a-f]{40}$/i);

      const forkRefs = execGit(["ls-remote", "--heads", forkBareDir, `refs/heads/${branchName}`], { cwd: workDir });
      expect(forkRefs.stdout).toContain(branchName);

      // Upstream must not receive the orphan branch
      const originRefs = execGit(["ls-remote", "--heads", bareDir, `refs/heads/${branchName}`], { cwd: workDir });
      expect(originRefs.stdout).toBe("");
    });
  });
});
