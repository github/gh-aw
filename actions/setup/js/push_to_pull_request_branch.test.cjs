import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";

const { getPatchPathForBranch, getPatchPathForBranchInRepo } = require("./git_patch_utils.cjs");
const { getBundlePathForBranch, getBundlePathForBranchInRepo } = require("./generate_git_bundle.cjs");
const promptsSourceDir = path.resolve(__dirname, "../md");

// The privileged handler derives patch/bundle paths from `branch` (and `repo`)
// via resolveTransportPaths, so tests must write transport files at the
// canonical derived location and let the handler discover them.
//
// `/tmp/gh-aw` is a process-global path shared by every test file. Vitest runs
// test files in parallel processes, so a cleanup that globbed the whole
// directory would delete another file's in-flight transport files mid-test.
// Track only the paths this file created and delete just those.
const createdTransportPaths = new Set();
function canonicalPatchPath(branch, repo) {
  fs.mkdirSync("/tmp/gh-aw", { recursive: true });
  const p = repo ? getPatchPathForBranchInRepo(branch, repo) : getPatchPathForBranch(branch);
  createdTransportPaths.add(p);
  return p;
}
function canonicalBundlePath(branch, repo) {
  fs.mkdirSync("/tmp/gh-aw", { recursive: true });
  const p = repo ? getBundlePathForBranchInRepo(branch, repo) : getBundlePathForBranch(branch);
  createdTransportPaths.add(p);
  return p;
}
function cleanupCanonicalTransports() {
  for (const p of createdTransportPaths) {
    try {
      fs.rmSync(p, { force: true });
    } catch {}
  }
  createdTransportPaths.clear();
}

beforeEach(() => {
  cleanupCanonicalTransports();
  process.env.GH_AW_PROMPTS_DIR = promptsSourceDir;
});
afterEach(() => {
  cleanupCanonicalTransports();
});

/**
 * Test Matrix for push_to_pull_request_branch.cjs
 *
 * This test file covers the following scenarios:
 *
 * 1. GitHub Actions Context Scenarios:
 *    - pull_request event (direct PR trigger)
 *    - issue_comment event (comment on PR)
 *    - schedule event (scheduled task)
 *    - workflow_dispatch (manual trigger)
 *
 * 2. Repository State Scenarios:
 *    - PR branch has had new pushes (needs merge/rebase)
 *    - Base branch (main/default) has new commits
 *    - PR branch has been force-pushed
 *    - Normal push scenario
 *
 * 3. Empty Commit Handling:
 *    - if-no-changes: warn (default)
 *    - if-no-changes: error
 *    - if-no-changes: ignore
 *
 * 4. Fork PR Scenarios:
 *    - Fork PR detection
 *    - Early failure for fork PRs (no push access)
 *    - Same-repo PR (normal case)
 *
 * 5. Error Scenarios:
 *    - git fetch failure
 *    - git checkout failure
 *    - git am (patch apply) failure
 *    - git push failure
 *    - Patch size too large
 *    - Patch file not found
 *    - Invalid patch content
 */

describe("push_to_pull_request_branch.cjs", () => {
  let mockCore;
  let mockExec;
  let mockContext;
  let mockGithub;
  let tempDir;
  let originalEnv;

  beforeEach(async () => {
    originalEnv = { ...process.env };

    // Set GITHUB_REPOSITORY to match the default test owner/repo so the
    // cross-repo guard in extra_empty_commit doesn't interfere.
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";

    // Create temp directory for test artifacts
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "push-to-pr-test-"));

    // Mock core actions methods
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    // Default exec mock
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    // Default pull request context
    mockContext = {
      eventName: "pull_request",
      sha: "abc123def456",
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
      payload: {
        pull_request: {
          number: 123,
          state: "open",
          title: "Test PR",
          labels: [],
          head: {
            ref: "feature-branch",
            sha: "head-sha-123",
            repo: {
              full_name: "test-owner/test-repo",
              fork: false,
              owner: {
                login: "test-owner",
              },
            },
          },
          base: {
            ref: "main",
            sha: "base-sha-456",
            repo: {
              full_name: "test-owner/test-repo",
              owner: {
                login: "test-owner",
              },
            },
          },
        },
        repository: {
          html_url: "https://github.com/test-owner/test-repo",
        },
      },
    };

    // Default GitHub API mock
    mockGithub = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({
            data: {
              head: {
                ref: "feature-branch",
                repo: {
                  full_name: "test-owner/test-repo",
                  fork: false,
                },
              },
              base: {
                repo: {
                  full_name: "test-owner/test-repo",
                },
              },
              title: "Test PR",
              labels: [],
            },
          }),
          create: vi.fn().mockResolvedValue({
            data: {
              number: 999,
              html_url: "https://github.com/test-owner/test-repo/pull/999",
            },
          }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({
            data: { default_branch: "main" },
          }),
          getBranchProtection: vi.fn().mockRejectedValue(Object.assign(new Error("Branch not protected"), { status: 404 })),
        },
        issues: {
          create: vi.fn().mockResolvedValue({
            data: {
              number: 456,
              html_url: "https://github.com/test-owner/test-repo/issues/456",
            },
          }),
        },
      },
      graphql: vi.fn(),
    };

    global.core = mockCore;
    global.exec = mockExec;
    global.context = mockContext;
    global.github = mockGithub;

    // Clear module cache
    delete require.cache[require.resolve("./push_to_pull_request_branch.cjs")];
    delete require.cache[require.resolve("./staged_preview.cjs")];
    delete require.cache[require.resolve("./update_activation_comment.cjs")];
    delete require.cache[require.resolve("./extra_empty_commit.cjs")];
  });

  afterEach(() => {
    // Restore environment by mutating process.env in place
    // (replacing process.env with a plain object breaks Node's special env handling)
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    // Clean up temp directory
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    // Clean up globals
    delete global.core;
    delete global.exec;
    delete global.context;
    delete global.github;
    vi.clearAllMocks();
  });

  /**
   * Helper to load the module with mocked dependencies
   */
  async function loadModule() {
    // Module loads when imported
    const module = require("./push_to_pull_request_branch.cjs");
    return module;
  }

  async function runFallbackPullRequestScenario(branch, detectionReason = "unknown") {
    createPatchFile(branch);
    process.env.GH_AW_DETECTION_REASON = detectionReason;

    mockExec.exec.mockResolvedValueOnce(0); // fetch
    mockExec.exec.mockResolvedValueOnce(0); // rev-parse
    mockExec.exec.mockResolvedValueOnce(0); // checkout

    mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" }); // git rev-parse HEAD (before patch)

    mockExec.exec.mockResolvedValueOnce(0); // git am

    const originalGetExecOutput = mockExec.getExecOutput;
    mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
      const argList = Array.isArray(args) ? args : [];
      if (argList[0] === "rev-parse" && argList[1] === "origin/feature-branch^{commit}") {
        return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\n", stderr: "" };
      }
      if (argList[0] === "rev-list" && argList[1] === "--merges") {
        return { exitCode: 0, stdout: "0\n", stderr: "" };
      }
      if (argList[0] === "rev-list" && argList[1] === "--parents") {
        return {
          exitCode: 0,
          stdout: "2222222222222222222222222222222222222222 1111111111111111111111111111111111111111\n",
          stderr: "",
        };
      }
      if (argList[0] === "ls-remote" && argList[2] === "refs/heads/feature-branch") {
        return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\trefs/heads/feature-branch\n", stderr: "" };
      }
      if (argList[0] === "log") {
        if (argList.includes(".github/workflows/")) {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        return { exitCode: 0, stdout: "Test commit\n", stderr: "" };
      }
      if (argList[0] === "diff-tree") {
        return { exitCode: 0, stdout: "", stderr: "" };
      }
      return originalGetExecOutput(cmd, args);
    });

    mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error: branch protection"));
    mockExec.exec.mockRejectedValueOnce(new Error("! [rejected] feature-branch -> feature-branch (non-fast-forward)"));

    const module = await loadModule();
    const handler = await module.main({});
    return handler({ branch }, {});
  }

  /**
   * Helper to create a valid patch file at the canonical path derived from the
   * message branch. The privileged handler always re-derives the patch path
   * from the validated branch, so tests must write at that canonical location.
   */
  function createPatchFile(branch, content = null, baseCommit = "") {
    const patchPath = canonicalPatchPath(branch);
    const defaultPatch = `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

Test changes

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`;
    let patchContent = content !== null ? content : defaultPatch;
    if (baseCommit) {
      const firstNewline = patchContent.indexOf("\n");
      patchContent = `${patchContent.slice(0, firstNewline + 1)}X-GH-AW-Base-Commit: ${baseCommit}\n${patchContent.slice(firstNewline + 1)}`;
    }
    fs.writeFileSync(patchPath, patchContent);
    return patchPath;
  }

  // ──────────────────────────────────────────────────────
  // Configuration Parsing Tests
  // ──────────────────────────────────────────────────────

  describe("configuration parsing", () => {
    it('should default target to "triggering" when not specified', async () => {
      const module = await loadModule();
      const handler = await module.main({});

      expect(mockCore.info).toHaveBeenCalledWith("Target: triggering");
    });

    it("should accept target from config", async () => {
      const module = await loadModule();
      const handler = await module.main({ target: "*" });

      expect(mockCore.info).toHaveBeenCalledWith("Target: *");
    });

    it("should accept numeric target for specific PR", async () => {
      const module = await loadModule();
      const handler = await module.main({ target: "456" });

      expect(mockCore.info).toHaveBeenCalledWith("Target: 456");
    });

    it("should accept disabling signed commits", async () => {
      const module = await loadModule();
      await module.main({ signed_commits: false });

      expect(mockCore.info).toHaveBeenCalledWith("Push signed commits: false");
    });

    it('should default if_no_changes to "warn"', async () => {
      const module = await loadModule();
      const handler = await module.main({});

      expect(mockCore.info).toHaveBeenCalledWith("If no changes: warn");
    });

    it("should accept title_prefix configuration", async () => {
      const module = await loadModule();
      const handler = await module.main({ title_prefix: "[bot] " });

      expect(mockCore.info).toHaveBeenCalledWith("Title prefix: [bot] ");
    });

    it("should accept labels configuration as array", async () => {
      const module = await loadModule();
      const handler = await module.main({ labels: ["automated", "bot"] });

      expect(mockCore.info).toHaveBeenCalledWith("Required labels: automated, bot");
    });

    it("should accept labels configuration as comma-separated string", async () => {
      const module = await loadModule();
      const handler = await module.main({ labels: "automated,bot" });

      expect(mockCore.info).toHaveBeenCalledWith("Required labels: automated, bot");
    });

    it("should accept required_labels configuration and prefer it over labels", async () => {
      const module = await loadModule();
      const handler = await module.main({ required_labels: ["approved"], labels: ["legacy"] });

      expect(mockCore.info).toHaveBeenCalledWith("Required labels: approved");
    });

    it("should fall back to labels when required_labels is undefined", async () => {
      const module = await loadModule();
      const handler = await module.main({ labels: ["fallback-label"] });

      expect(mockCore.info).toHaveBeenCalledWith("Required labels: fallback-label");
    });

    it("should default max_patch_size to 4096 KB", async () => {
      const module = await loadModule();
      const handler = await module.main({});

      expect(mockCore.info).toHaveBeenCalledWith("Max patch size: 4096 KB");
    });
  });

  // ──────────────────────────────────────────────────────
  // GitHub Actions Context Tests
  // ──────────────────────────────────────────────────────

  describe("GitHub Actions context scenarios", () => {
    it('should handle pull_request event with target "triggering"', async () => {
      mockContext.eventName = "pull_request";
      const patchPath = createPatchFile("should-handle-pull-request-event-with-target-triggering");

      // Mock git commands
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-handle-pull-request-event-with-target-triggering" }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        pull_number: 123,
      });
    });

    it("should handle issue_comment event (comment on PR)", async () => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.issue = { number: 123 };
      const patchPath = createPatchFile("should-handle-issue-comment-event-comment-on-pr");

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-handle-issue-comment-event-comment-on-pr" }, {});

      expect(result.success).toBe(true);
    });

    it('should fail for schedule event with target "triggering"', async () => {
      mockContext.eventName = "schedule";
      delete mockContext.payload.pull_request;
      delete mockContext.payload.issue;

      const patchPath = createPatchFile("should-fail-for-schedule-event-with-target-triggering");

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-fail-for-schedule-event-with-target-triggering" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("pull request context");
    });

    it('should fail gracefully with target "triggering" when context is undefined (MCP daemon mode)', async () => {
      delete global.context;

      const patchPath = createPatchFile("should-fail-gracefully-with-target-triggering-when-context-i");

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-fail-gracefully-with-target-triggering-when-context-i" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("pull request context");
    });

    it('should handle schedule event with target "*" and explicit PR number', async () => {
      mockContext.eventName = "schedule";
      delete mockContext.payload.pull_request;
      const patchPath = createPatchFile("should-handle-schedule-event-with-target-and-explicit-pr-num");

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ target: "*" });
      const result = await handler({ pull_request_number: 456, branch: "should-handle-schedule-event-with-target-and-explicit-pr-num" }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        pull_number: 456,
      });
    });

    it("should handle workflow_dispatch event with explicit PR number", async () => {
      mockContext.eventName = "workflow_dispatch";
      delete mockContext.payload.pull_request;
      const patchPath = createPatchFile("should-handle-workflow-dispatch-event-with-explicit-pr-numbe");

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ target: "789" });
      const result = await handler({ branch: "should-handle-workflow-dispatch-event-with-explicit-pr-numbe" }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        pull_number: 789,
      });
    });

    it('should resolve target "triggering" from workflow_dispatch aw_context pull_request metadata', async () => {
      mockContext.eventName = "workflow_dispatch";
      delete mockContext.payload.pull_request;
      delete mockContext.payload.issue;
      mockContext.payload.inputs = {
        aw_context: JSON.stringify({
          item_type: "pull_request",
          item_number: "456",
        }),
      };
      const patchPath = createPatchFile("should-resolve-target-triggering-from-workflow-dispatch-aw-c");

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-resolve-target-triggering-from-workflow-dispatch-aw-c" }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        pull_number: 456,
      });
    });

    it('should fail when workflow_dispatch aw_context item_number is not a positive integer for target "triggering"', async () => {
      mockContext.eventName = "workflow_dispatch";
      delete mockContext.payload.pull_request;
      delete mockContext.payload.issue;
      mockContext.payload.inputs = {
        aw_context: JSON.stringify({
          item_type: "pull_request",
          item_number: "0",
        }),
      };
      const patchPath = createPatchFile("should-fail-when-workflow-dispatch-aw-context-item-number-is");

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-fail-when-workflow-dispatch-aw-context-item-number-is" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("pull request context");
    });
  });

  // ──────────────────────────────────────────────────────
  // Fork PR Detection Tests
  // ──────────────────────────────────────────────────────

  describe("fork PR detection and handling", () => {
    it("should detect fork PR via different repository names and fail early", async () => {
      // Set up fork scenario - head repo is different from base repo
      mockContext.payload.pull_request.head.repo.full_name = "fork-owner/test-repo";
      mockContext.payload.pull_request.head.repo.owner.login = "fork-owner";

      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: {
              full_name: "fork-owner/test-repo",
              fork: true,
            },
          },
          base: {
            repo: {
              full_name: "test-owner/test-repo",
            },
          },
          title: "Fork PR",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-detect-fork-pr-via-different-repository-names-and-fai");

      global.getOctokit = vi.fn().mockReturnValue(mockGithub);
      const module = await loadModule();
      const handler = await module.main({
        target: "triggering",
        "github-token": "pat-that-may-have-fork-access",
      });
      const result = await handler({ branch: "should-detect-fork-pr-via-different-repository-names-and-fai" }, {});

      // A PAT does not bypass the explicit head-repo destination policy.
      expect(result.success).toBe(false);
      expect(result.error).toContain("fork");
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Cannot push to fork PR"));
      // Should not attempt any git operations
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should allow updates to a configured automation fork head repo", async () => {
      mockContext.payload.pull_request.head.repo.full_name = "fork-owner/test-repo";
      mockContext.payload.pull_request.head.repo.owner.login = "fork-owner";

      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: {
              full_name: "fork-owner/test-repo",
              fork: true,
            },
          },
          base: {
            repo: {
              full_name: "test-owner/test-repo",
            },
          },
          title: "Fork PR",
          labels: [],
        },
      });

      createPatchFile("should-allow-updates-to-a-configured-automation-fork-head-r");
      mockExec.getExecOutput
        .mockResolvedValueOnce({ exitCode: 0, stdout: "preflight-sha\trefs/heads/feature-branch\n", stderr: "" })
        .mockResolvedValueOnce({ exitCode: 0, stdout: "remote-head-before\n", stderr: "" })
        .mockResolvedValueOnce({ exitCode: 0, stdout: "abc123\n", stderr: "" })
        .mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("fork-head-sha");

      try {
        const module = await loadModule();
        const handler = await module.main({
          target: "triggering",
          "head-repo": "fork-owner/test-repo",
          allowed_repos: ["test-owner/test-repo", "fork-owner/test-repo"],
        });
        const result = await handler({ branch: "should-allow-updates-to-a-configured-automation-fork-head-r" }, {});

        expect(result.success).toBe(true);
        expect(result.head_repo).toBe("fork-owner/test-repo");
        expect(result.commit_url).toContain("fork-owner/test-repo/commit/fork-head-sha");
        expect(pushSignedSpy).toHaveBeenCalledWith(
          expect.objectContaining({
            owner: "fork-owner",
            repo: "test-repo",
          })
        );
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should NOT treat same-repo PR as fork even when repo has fork flag", async () => {
      // A repository that is itself a fork of another repo has fork=true,
      // but a same-repo PR within it is NOT a cross-repo fork PR (#24208)
      mockContext.payload.pull_request.head.repo.fork = true;

      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: {
              full_name: "test-owner/test-repo",
              fork: true,
            },
          },
          base: {
            repo: {
              full_name: "test-owner/test-repo",
            },
          },
          title: "Same-repo PR in forked repo",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-not-treat-same-repo-pr-as-fork-even-when-repo-has-for");

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-not-treat-same-repo-pr-as-fork-even-when-repo-has-for" }, {});

      // Same full_name means same repo — push should proceed, not fail
      expect(result.success).toBe(true);
    });

    it("should handle deleted head repo (likely a fork) and fail early", async () => {
      delete mockContext.payload.pull_request.head.repo;

      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: null, // Deleted fork
          },
          base: {
            repo: {
              full_name: "test-owner/test-repo",
            },
          },
          title: "Deleted Fork PR",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-handle-deleted-head-repo-likely-a-fork-and-fail-early");

      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-handle-deleted-head-repo-likely-a-fork-and-fail-early" }, {});

      // When head.repo is null, the handler should reject immediately before any fork checks
      expect(result.success).toBe(false);
      expect(result.error).toContain("null");
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("head repository is null"));
    });

    it("should reject when head.repo is null even with configured head-repo", async () => {
      // Even when head-repo is explicitly configured, a null head.repo means we
      // cannot verify which fork the PR came from — reject to prevent writes to an
      // unverifiable PR.
      delete mockContext.payload.pull_request.head.repo;

      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: null, // Deleted fork
          },
          base: {
            repo: {
              full_name: "test-owner/test-repo",
            },
          },
          title: "Deleted Fork PR with configured head-repo",
          labels: [],
        },
      });

      createPatchFile("should-reject-when-head-repo-is-null-even-with-configured-h");

      const module = await loadModule();
      const handler = await module.main({
        target: "triggering",
        "head-repo": "fork-owner/test-repo",
        allowed_repos: ["test-owner/test-repo", "fork-owner/test-repo"],
      });
      const result = await handler({ branch: "should-reject-when-head-repo-is-null-even-with-configured-h" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("null");
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("head repository is null"));
    });
  });

  // ──────────────────────────────────────────────────────
  // Protected Branch Detection Tests
  // ──────────────────────────────────────────────────────

  describe("protected branch detection", () => {
    it("should block push to the repository default branch", async () => {
      // PR head branch is the repo default branch
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "main",
            repo: { full_name: "test-owner/test-repo", fork: false },
          },
          base: { repo: { full_name: "test-owner/test-repo" } },
          title: "Test PR",
          labels: [],
        },
      });
      mockGithub.rest.repos.get.mockResolvedValue({ data: { default_branch: "main" } });

      const patchPath = createPatchFile("main");
      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "main" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("default branch");
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("default branch"));
      // Should not attempt any git operations
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should block push to a branch with protection rules", async () => {
      // Branch has protection rules - getBranchProtection resolves successfully
      mockGithub.rest.repos.getBranchProtection.mockResolvedValue({ data: {} });

      const patchPath = createPatchFile("should-block-push-to-a-branch-with-protection-rules");
      const module = await loadModule();
      const handler = await module.main({ target: "triggering" });
      const result = await handler({ branch: "should-block-push-to-a-branch-with-protection-rules" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("protection rules");
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("protection rules"));
      // Should not attempt any git operations
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should allow push when branch is not default and has no protection rules", async () => {
      // Default mock: repos.get returns "main", getBranchProtection returns 404
      // PR head is "feature-branch" (not the default)
      const patchPath = createPatchFile("should-allow-push-when-branch-is-not-default-and-has-no-prot");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-allow-push-when-branch-is-not-default-and-has-no-prot" }, {});

      expect(result.success).toBe(true);
      expect(result.branch_name).toBe("feature-branch");
    });

    it("should warn and continue when repos.get fails", async () => {
      // repos.get fails - cannot determine default branch, warn and continue
      mockGithub.rest.repos.get.mockRejectedValue(new Error("API unavailable"));
      const patchPath = createPatchFile("should-warn-and-continue-when-repos-get-fails");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-warn-and-continue-when-repos-get-fails" }, {});

      // Should warn but still allow the push since we can't confirm it's the default branch
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not check repository default branch"));
      expect(result.success).toBe(true);
    });

    it("should warn and continue when getBranchProtection returns 403 (permission denied)", async () => {
      // getBranchProtection returns 403 (no permission to read protection)
      // Platform will still enforce protection at push time, so we warn and allow
      mockGithub.rest.repos.getBranchProtection.mockRejectedValue(Object.assign(new Error("Forbidden"), { status: 403 }));
      const patchPath = createPatchFile("should-warn-and-continue-when-getbranchprotection-returns-40");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-warn-and-continue-when-getbranchprotection-returns-40" }, {});

      // Should warn but allow push since the platform still enforces protection at push time
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not check branch protection rules"));
      expect(result.success).toBe(true);
    });

    it("should block push when getBranchProtection returns an unexpected error (fail closed)", async () => {
      // getBranchProtection returns 500 - unexpected error, fail closed for security
      mockGithub.rest.repos.getBranchProtection.mockRejectedValue(Object.assign(new Error("Internal Server Error"), { status: 500 }));
      const patchPath = createPatchFile("should-block-push-when-getbranchprotection-returns-an-unexpe");

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-block-push-when-getbranchprotection-returns-an-unexpe" }, {});

      // Should block the push when branch protection status cannot be verified
      expect(result.success).toBe(false);
      expect(result.error).toContain("Cannot verify branch protection rules");
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Cannot verify branch protection rules"));
      // Should not attempt any git operations
      expect(mockExec.exec).not.toHaveBeenCalled();
    });
  });

  // ──────────────────────────────────────────────────────
  // Repository State Scenarios
  // ──────────────────────────────────────────────────────

  describe("repository state scenarios", () => {
    it("should handle successful normal push", async () => {
      const patchPath = createPatchFile("should-handle-successful-normal-push");
      mockExec.getExecOutput
        .mockResolvedValueOnce({ exitCode: 0, stdout: "preflight-sha\trefs/heads/feature-branch\n", stderr: "" })
        .mockResolvedValueOnce({ exitCode: 0, stdout: "remote-head-before\n", stderr: "" })
        .mockResolvedValueOnce({ exitCode: 0, stdout: "abc123\n", stderr: "" })
        .mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-successful-normal-push" }, {});

      expect(result.success).toBe(true);
      expect(result.branch_name).toBe("feature-branch");
      expect(result.commit_url).toContain("test-owner/test-repo/commit/");
      expect(result.before_state).toEqual({ head_sha: "remote-head-before" });
      expect(result.after_state).toEqual({ head_sha: "abc123" });
    });

    it("should reset to the patch-embedded base commit before applying patch transport", async () => {
      const recordedBaseCommit = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef";
      const patchPath = createPatchFile("should-reset-to-message-base-commit-before-applying-patch-tr", null, recordedBaseCommit);
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });
      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("abc123");

      try {
        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "should-reset-to-message-base-commit-before-applying-patch-tr" }, {});

        expect(result.success).toBe(true);
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["cat-file", "-e", recordedBaseCommit], expect.any(Object));
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["reset", "--hard", recordedBaseCommit], expect.any(Object));
        expect(pushSignedSpy).toHaveBeenCalledWith(expect.objectContaining({ baseRef: recordedBaseCommit }));
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should fall back to current HEAD when the patch-embedded base commit is unavailable", async () => {
      const recordedBaseCommit = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef";
      const patchPath = createPatchFile("should-fall-back-to-current-head-when-base-commit-is-unavail", null, recordedBaseCommit);
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });
      mockExec.exec.mockImplementation(async (cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "cat-file" && args[1] === "-e" && args[2] === recordedBaseCommit) {
          throw new Error("not in object store");
        }
        return 0;
      });

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("abc123");

      try {
        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "should-fall-back-to-current-head-when-base-commit-is-unavail" }, {});

        expect(result.success).toBe(true);
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["reset", "--hard", recordedBaseCommit], expect.any(Object));
        expect(pushSignedSpy).toHaveBeenCalledWith(expect.objectContaining({ baseRef: "abc123" }));
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should ignore agent-supplied base_commit for patch transport", async () => {
      const patchPath = createPatchFile("should-ignore-invalid-message-base-commit-for-patch-transpor");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ base_commit: "not-a-sha --bad", branch: "should-ignore-invalid-message-base-commit-for-patch-transpor" }, {});

      expect(result.success).toBe(true);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["cat-file", "-e", "not-a-sha --bad"], expect.any(Object));
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["reset", "--hard", "not-a-sha --bad"], expect.any(Object));
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("base_commit"));
    });

    it("should use pushed commit SHA returned by pushSignedCommits for activation comment commit link", async () => {
      const patchPath = createPatchFile("should-use-pushed-commit-sha-returned-by-pushsignedcommits-f");
      const updateActivationCommentModule = require("./update_activation_comment.cjs");
      const updateCommitSpy = vi.spyOn(updateActivationCommentModule, "updateActivationCommentWithCommit").mockResolvedValue(undefined);
      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("remote-head-after");

      try {
        mockExec.getExecOutput
          .mockResolvedValueOnce({ exitCode: 0, stdout: "preflight-sha\trefs/heads/feature-branch\n", stderr: "" }) // preflight ls-remote
          .mockResolvedValueOnce({ exitCode: 0, stdout: "local-head-before\n", stderr: "" }) // rev-parse HEAD before patch
          .mockResolvedValueOnce({ exitCode: 0, stdout: "test.txt\n", stderr: "" }) // post-apply git diff --name-only --no-renames
          .mockResolvedValueOnce({ exitCode: 0, stdout: "0\n", stderr: "" }) // rev-list --merges --count (no merge commits)
          .mockResolvedValueOnce({ exitCode: 0, stdout: "1\n", stderr: "" }) // rev-list --count (new commits)
          .mockResolvedValueOnce({ exitCode: 0, stdout: " file.txt | 1 +\n 1 file changed, 1 insertion(+)\n", stderr: "" }); // git diff --stat (non-empty = has file changes)

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "should-use-pushed-commit-sha-returned-by-pushsignedcommits-f" }, {});

        expect(result.success).toBe(true);
        expect(result.commit_sha).toBe("remote-head-after");
        expect(result.commit_url).toContain("/commit/remote-head-after");
        expect(updateCommitSpy).toHaveBeenCalledWith(mockGithub, mockContext, mockCore, "remote-head-after", "https://github.com/test-owner/test-repo/commit/remote-head-after", {
          targetIssueNumber: 123,
          targetRepo: "test-owner/test-repo",
          targetGithubClient: expect.anything(),
        });
      } finally {
        pushSignedSpy.mockRestore();
        updateCommitSpy.mockRestore();
      }
    });

    it("should skip activation comment for empty commits (no file changes)", async () => {
      const patchPath = createPatchFile("should-skip-activation-comment-for-empty-commits-no-file-cha");
      const updateActivationCommentModule = require("./update_activation_comment.cjs");
      const updateCommitSpy = vi.spyOn(updateActivationCommentModule, "updateActivationCommentWithCommit").mockResolvedValue(undefined);
      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("remote-head-after");

      try {
        mockExec.getExecOutput
          .mockResolvedValueOnce({ exitCode: 0, stdout: "preflight-sha\trefs/heads/feature-branch\n", stderr: "" }) // preflight ls-remote
          .mockResolvedValueOnce({ exitCode: 0, stdout: "local-head-before\n", stderr: "" }) // rev-parse HEAD before patch
          .mockResolvedValueOnce({ exitCode: 0, stdout: "0\n", stderr: "" }) // rev-list --merges --count (no merge commits)
          .mockResolvedValueOnce({ exitCode: 0, stdout: "1\n", stderr: "" }) // rev-list --count (new commits)
          .mockResolvedValueOnce({ exitCode: 0, stdout: "", stderr: "" }); // git diff --stat (empty = no file changes)

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "should-skip-activation-comment-for-empty-commits-no-file-cha" }, {});

        expect(result.success).toBe(true);
        expect(result.commit_sha).toBe("remote-head-after");
        expect(mockCore.info).toHaveBeenCalledWith("Skipping activation comment: pushed commit has no file changes (empty commit)");
        expect(updateCommitSpy).not.toHaveBeenCalled();
      } finally {
        pushSignedSpy.mockRestore();
        updateCommitSpy.mockRestore();
      }
    });

    it("should detect deleted branch before fetch", async () => {
      const patchPath = createPatchFile("should-detect-deleted-branch-before-fetch");

      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 2, stdout: "", stderr: "fatal: couldn't find remote ref feature-branch" });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-detect-deleted-branch-before-fetch" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("no longer exists on origin");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should skip deleted branch failure when ignore_missing_branch_failure is enabled", async () => {
      const patchPath = createPatchFile("should-skip-deleted-branch-failure-when-ignore-missing-branc");

      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 2, stdout: "", stderr: "fatal: couldn't find remote ref feature-branch" });

      const module = await loadModule();
      const handler = await module.main({ ignore_missing_branch_failure: true });
      const result = await handler({ branch: "should-skip-deleted-branch-failure-when-ignore-missing-branc" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("no longer exists on origin");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("ignore-missing-branch-failure"));
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should fail with diagnostic error when branch existence check fails for other reasons", async () => {
      const patchPath = createPatchFile("should-fail-with-diagnostic-error-when-branch-existence-chec");

      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: "fatal: Authentication failed" });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-fail-with-diagnostic-error-when-branch-existence-chec" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to verify branch");
      expect(result.error).toContain("Authentication failed");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should handle git fetch failure", async () => {
      const patchPath = createPatchFile("should-handle-git-fetch-failure");

      // First exec call (git fetch) fails
      mockExec.exec.mockRejectedValueOnce(new Error("Failed to fetch: network error"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-git-fetch-failure" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to fetch branch");
    });

    it("should handle branch not existing on origin", async () => {
      const patchPath = createPatchFile("should-handle-branch-not-existing-on-origin");

      // git fetch succeeds, but git rev-parse fails
      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockRejectedValueOnce(new Error("fatal: Needed a single revision"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-branch-not-existing-on-origin" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("does not exist on origin");
    });

    it("should skip rev-parse missing branch failure when ignore_missing_branch_failure is enabled", async () => {
      const patchPath = createPatchFile("should-skip-rev-parse-missing-branch-failure-when-ignore-mis");

      // git fetch succeeds, but git rev-parse fails
      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockRejectedValueOnce(new Error("fatal: Needed a single revision"));

      const module = await loadModule();
      const handler = await module.main({ ignore_missing_branch_failure: true });
      const result = await handler({ branch: "should-skip-rev-parse-missing-branch-failure-when-ignore-mis" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("no longer exists on origin");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("ignore-missing-branch-failure"));
    });

    it("should handle git checkout failure", async () => {
      const patchPath = createPatchFile("should-handle-git-checkout-failure");

      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockRejectedValueOnce(new Error("checkout failed"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-git-checkout-failure" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to checkout branch");
    });

    it("should handle git am (patch apply) failure with investigation", async () => {
      const patchPath = createPatchFile("should-handle-git-am-patch-apply-failure-with-investigation");

      // Set up successful fetch, rev-parse, checkout
      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout

      // git rev-parse HEAD for commit tracking
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" });

      // git am fails
      mockExec.exec.mockRejectedValueOnce(new Error("Patch does not apply"));

      // No unresolved conflicts for automatic add/add recovery
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "", stderr: "" }); // git diff --name-only --diff-filter=U

      // Investigation commands succeed
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "M file.txt\n", stderr: "" }); // git status
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "abc123 commit 1\n", stderr: "" }); // git log
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "", stderr: "" }); // git diff
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "patch diff", stderr: "" }); // git am --show-current-patch=diff
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "full patch", stderr: "" }); // git am --show-current-patch

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-git-am-patch-apply-failure-with-investigation" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to apply patch");
      expect(mockCore.info).toHaveBeenCalledWith("Investigating patch failure...");
    });

    it("should resolve add/add conflicts by preferring patch version and continuing git am", async () => {
      const patchPath = createPatchFile("should-resolve-add-add-conflicts-by-preferring-patch-version");
      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("pushed-sha");

      try {
        mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
          const argList = Array.isArray(args) ? args : [];

          if (argList[0] === "ls-remote" && argList[1] === "--exit-code") {
            return { exitCode: 0, stdout: "before-sha\trefs/heads/feature-branch\n", stderr: "" };
          }
          if (argList[0] === "rev-parse" && argList[1] === "HEAD") {
            return { exitCode: 0, stdout: "before-sha\n", stderr: "" };
          }
          if (argList[0] === "diff" && argList[1] === "--name-only") {
            return { exitCode: 0, stdout: "docs/findings.md\n", stderr: "" };
          }
          if (argList[0] === "status" && argList[1] === "--porcelain") {
            return { exitCode: 0, stdout: "AA docs/findings.md\n", stderr: "" };
          }
          if (argList[0] === "rev-list" && argList[1] === "--count") {
            return { exitCode: 0, stdout: "1\n", stderr: "" };
          }

          return { exitCode: 0, stdout: "", stderr: "" };
        });

        // Set up successful fetch, rev-parse, checkout
        mockExec.exec.mockResolvedValueOnce(0); // fetch
        mockExec.exec.mockResolvedValueOnce(0); // rev-parse
        mockExec.exec.mockResolvedValueOnce(0); // checkout

        // git am fails first with conflict
        mockExec.exec.mockRejectedValueOnce(new Error("Patch does not apply"));

        mockExec.exec.mockResolvedValueOnce(0); // git checkout --theirs -- docs/findings.md
        mockExec.exec.mockResolvedValueOnce(0); // git add -- docs/findings.md
        mockExec.exec.mockResolvedValueOnce(0); // git am --continue

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "should-resolve-add-add-conflicts-by-preferring-patch-version" }, {});

        expect(result.success).toBe(true);
        expect(mockCore.info).toHaveBeenCalledWith("Patch applied successfully after resolving add/add conflict(s)");
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "--theirs", "--", "docs/findings.md"], expect.any(Object));
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "--", "docs/findings.md"], expect.any(Object));
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["am", "--continue"], expect.any(Object));
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should create fallback pull request on non-fast-forward push rejection by default", async () => {
      const result = await runFallbackPullRequestScenario("should-create-fallback-pull-request-on-non-fast-forward-push");

      expect(result.success).toBe(true);
      expect(result.fallback_used).toBe(true);
      expect(result.fallback_type).toBe("pull_request");
      expect(result.pull_request_number).toBe(999);
      expect(mockGithub.rest.pulls.create).toHaveBeenCalled();
    });

    it("should not create fallback pull request when fallback-as-pull-request is disabled", async () => {
      const patchPath = createPatchFile("should-not-create-fallback-pull-request-when-fallback-as-pul");

      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout

      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" }); // git rev-parse HEAD (before patch)

      mockExec.exec.mockResolvedValueOnce(0); // git am

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
        const argList = Array.isArray(args) ? args : [];
        if (argList[0] === "rev-parse" && argList[1] === "origin/feature-branch^{commit}") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--merges") {
          return { exitCode: 0, stdout: "0\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--parents") {
          return {
            exitCode: 0,
            stdout: "2222222222222222222222222222222222222222 1111111111111111111111111111111111111111\n",
            stderr: "",
          };
        }
        if (argList[0] === "ls-remote" && argList[2] === "refs/heads/feature-branch") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\trefs/heads/feature-branch\n", stderr: "" };
        }
        if (argList[0] === "log") {
          return { exitCode: 0, stdout: "Test commit\n", stderr: "" };
        }
        if (argList[0] === "diff-tree") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        return originalGetExecOutput(cmd, args);
      });

      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error: branch protection"));
      mockExec.exec.mockRejectedValueOnce(new Error("! [rejected] feature-branch -> feature-branch (non-fast-forward)"));

      const module = await loadModule();
      const handler = await module.main({ fallback_as_pull_request: false });
      const result = await handler({ branch: "should-not-create-fallback-pull-request-when-fallback-as-pul" }, {});

      expect(result.success).toBe(false);
      expect(result.error_type).toBe("push_failed");
      expect(result.error).toContain("non-fast-forward");
      expect(mockGithub.rest.pulls.create).not.toHaveBeenCalled();
    });

    it("should skip non-fatally when fallback branch has pre-existing workflow files (agent has none)", async () => {
      createPatchFile("fallback-branch-workflows-scope-rejection");

      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout

      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" }); // git rev-parse HEAD (before patch)

      mockExec.exec.mockResolvedValueOnce(0); // git am

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (argList[0] === "rev-parse" && argList[1] === "origin/feature-branch^{commit}") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--merges") {
          return { exitCode: 0, stdout: "0\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--parents") {
          return {
            exitCode: 0,
            stdout: "2222222222222222222222222222222222222222 1111111111111111111111111111111111111111\n",
            stderr: "",
          };
        }
        if (argList[0] === "ls-remote" && argList[2] === "refs/heads/feature-branch") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\trefs/heads/feature-branch\n", stderr: "" };
        }
        if (argList[0] === "log") {
          // Pre-flight git log targets .github/workflows/ — simulate pre-existing workflow
          // files in branch history without the agent having added them.
          if (argList.includes(".github/workflows/")) {
            return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
          }
          return { exitCode: 0, stdout: "Test commit\n", stderr: "" };
        }
        if (argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          // Agent's post-apply diff has no workflow files
          return { exitCode: 0, stdout: "src/index.js\n", stderr: "" };
        }
        if (argList[0] === "diff-tree") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          return {
            exitCode: 1,
            stdout: "",
            stderr: "! [remote rejected] branch -> branch (`workflows` scope may be required.)",
          };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      // GraphQL call fails, triggering fallback to git push
      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error: branch protection"));
      // Git push fails with non-fast-forward, triggering fallback branch creation
      mockExec.exec.mockRejectedValueOnce(new Error("! [rejected] feature-branch -> feature-branch (non-fast-forward)"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "fallback-branch-workflows-scope-rejection" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error_type).toBeUndefined();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("pre-existing commits"));
    });

    it("should diagnose deleted branch when push fails", async () => {
      const patchPath = createPatchFile("should-diagnose-deleted-branch-when-push-fails");

      // Set up successful operations until push
      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout

      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" }); // git rev-parse HEAD (before patch)

      mockExec.exec.mockResolvedValueOnce(0); // git am

      // pushSignedCommits + post-failure diagnosis responses
      const originalGetExecOutput = mockExec.getExecOutput;
      let exitCodeLsRemoteCallCount = 0;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
        const argList = Array.isArray(args) ? args : [];
        if (argList[0] === "rev-parse" && argList[1] === "HEAD") {
          return { exitCode: 0, stdout: "before-sha\n", stderr: "" };
        }
        if (argList[0] === "rev-list") {
          return { exitCode: 0, stdout: "abc123\n", stderr: "" };
        }
        if (argList[0] === "diff-tree") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        if (argList[0] === "ls-remote" && argList[1] === "--exit-code") {
          exitCodeLsRemoteCallCount += 1;
          if (exitCodeLsRemoteCallCount === 1) {
            // Initial preflight check before fetch
            return { exitCode: 0, stdout: "remote-oid\trefs/heads/feature-branch\n", stderr: "" };
          }
          // Post-push diagnosis call from push_to_pull_request_branch catch block
          return { exitCode: 2, stdout: "", stderr: "fatal: couldn't find remote ref feature-branch" };
        }
        if (argList[0] === "ls-remote") {
          return { exitCode: 0, stdout: "remote-oid\trefs/heads/feature-branch\n", stderr: "" };
        }
        if (argList[0] === "log") {
          return { exitCode: 0, stdout: "Test commit\n", stderr: "" };
        }
        if (argList[0] === "diff" && argList[1] === "--name-status") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        return originalGetExecOutput(cmd, args);
      });

      // GraphQL call fails, triggering fallback to git push
      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error: branch protection"));
      // Fallback git push fails
      mockExec.exec.mockRejectedValueOnce(new Error("remote: Internal Server Error"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-diagnose-deleted-branch-when-push-fails" }, {});

      expect(result.success).toBe(false);
      expect(result.error_type).toBe("push_failed");
      expect(result.error).toContain("appears to have been deleted");
    });

    it("should detect force-pushed branch via ref mismatch", async () => {
      const patchPath = createPatchFile("should-detect-force-pushed-branch-via-ref-mismatch");

      // This tests the scenario where the branch SHA we have doesn't match remote
      // The current implementation doesn't explicitly detect this, but git operations would fail
      mockContext.payload.pull_request.head.sha = "old-sha-123";

      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            sha: "new-sha-456", // Remote has different SHA
          },
          title: "Test PR",
          labels: [],
        },
      });

      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "new-sha-456\n", stderr: "" });
      mockExec.exec.mockResolvedValueOnce(0); // git am
      mockExec.exec.mockResolvedValueOnce(0); // git push
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "final-sha\n", stderr: "" });
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "1\n", stderr: "" }); // commit count

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-detect-force-pushed-branch-via-ref-mismatch" }, {});

      // The push proceeds - force-push detection would need to be added
      expect(mockGithub.rest.pulls.get).toHaveBeenCalled();
    });
  });

  // ──────────────────────────────────────────────────────
  // Threat Detection: Review Branch Push
  // ──────────────────────────────────────────────────────

  describe("threat detection: review branch push", () => {
    let savedGetExecOutput;

    beforeEach(() => {
      savedGetExecOutput = mockExec.getExecOutput;
    });

    afterEach(() => {
      mockExec.getExecOutput = savedGetExecOutput;
    });

    it.each([
      ["agent_failure", "> [!WARNING]", "> threat detection engine error", "<!-- gh-aw-threat-engine-error -->", "<!-- gh-aw-threat-detected -->"],
      ["threat_detected", "> [!CAUTION]", "> agentic threat detected", "<!-- gh-aw-threat-detected -->", "<!-- gh-aw-threat-engine-error -->"],
    ])("should create a review PR body for %s with the correct admonition and marker", async (reason, admonition, title, expectedMarker, unexpectedMarker) => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = reason;
      mockContext.runId = 12345;
      createPatchFile(`review-branch-body-${reason}`);

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: `review-branch-body-${reason}` }, {});

      expect(result.success).toBe(true);
      const [params] = mockGithub.rest.pulls.create.mock.calls.at(-1);
      expect(params.body).toContain(admonition);
      expect(params.body).toContain(title);
      expect(params.body).toContain(expectedMarker);
      expect(params.body).not.toContain(unexpectedMarker);
    });

    it("should skip non-fatally when review branch is rejected for workflows scope (timeout variant, agent has none)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      createPatchFile("review-branch-workflows-scope-timeout");

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          return {
            exitCode: 1,
            stdout: "",
            stderr: "remote: error: Unable to determine if workflow can be created or updated due to timeout\n" + "error: failed to push some refs to 'https://github.com/test-owner/test-repo.git'",
          };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-workflows-scope-timeout" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error_type).toBeUndefined();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("pre-existing commits"));
      // Should NOT fall through to the generic "Failed to create review PR" catch message
      const errorCalls = mockCore.error.mock.calls.map(c => c[0]);
      expect(errorCalls.some(msg => msg.includes("Failed to create review PR"))).toBe(false);
    });

    it("should skip non-fatally when review branch is rejected with backtick workflows scope (agent has none)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      createPatchFile("review-branch-workflows-scope-backtick");

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          return {
            exitCode: 1,
            stdout: "",
            stderr: "! [remote rejected] branch -> branch (`workflows` scope may be required.)",
          };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-workflows-scope-backtick" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error_type).toBeUndefined();
    });

    it("should wrap generic review branch push failure in actionable error message", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      createPatchFile("review-branch-generic-push-failure");

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          return { exitCode: 1, stdout: "", stderr: "error: authentication failed for 'https://github.com/test-owner/test-repo.git'" };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-generic-push-failure" }, {});

      expect(result.success).toBe(false);
      // Generic push failure should NOT be typed as workflows_scope_required
      expect(result.error_type).toBeUndefined();
      expect(result.error).toContain("Failed to create review PR");
    });

    it("should skip non-fatally when branch history has pre-existing workflow files (pre-flight fires before checkout)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      createPatchFile("review-branch-preflight-workflow-files");

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        // Pre-flight git log targets .github/workflows/ directory — returns a workflow
        // file path to simulate branch history containing pre-existing workflow changes.
        if (cmd === "git" && argList[0] === "log" && argList.includes(".github/workflows/")) {
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        // Agent's post-apply diff has no workflow files
        if (cmd === "git" && argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: "src/app.js\n", stderr: "" };
        }
        // The git push should NOT be reached — pre-flight check returns skip first
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          throw new Error("git push should not be called when pre-flight check fires");
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      // allow_workflows not set (default false) — pre-flight check is active
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-preflight-workflow-files" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error_type).toBeUndefined();
      // Pre-flight fires before checkout — no "Failed to create review PR" message
      const errorCalls = mockCore.error.mock.calls.map(c => c[0]);
      expect(errorCalls.some(msg => msg.includes("Failed to create review PR"))).toBe(false);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Pre-flight check"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("pre-existing commits"));
    });

    it("should skip pre-flight check and attempt push when allow_workflows is true", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      createPatchFile("review-branch-allow-workflows-skip-preflight");

      let preflightCalled = false;
      let pushCalled = false;
      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (cmd === "git" && argList[0] === "log" && argList.includes(".github/workflows/")) {
          preflightCalled = true;
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          pushCalled = true;
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      // allow_workflows: true — skip the pre-flight check
      const handler = await module.main({ allow_workflows: true });
      const result = await handler({ branch: "review-branch-allow-workflows-skip-preflight" }, {});

      // Pre-flight check should NOT have run
      expect(preflightCalled).toBe(false);
      // Push should have been attempted
      expect(pushCalled).toBe(true);
      expect(result.success).toBe(true);
    });

    // ──────────────────────────────────────────────────────
    // Workflow scope: agent-added vs pre-existing files
    // ──────────────────────────────────────────────────────

    it("should return hard workflows_scope_required error when agent itself adds a workflow file (review branch pre-flight)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      createPatchFile("review-branch-agent-adds-workflow-preflight");

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        // Pre-flight detects workflow file in branch history
        if (cmd === "git" && argList[0] === "log" && argList.includes(".github/workflows/")) {
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        // Agent's post-apply diff includes a workflow file
        if (cmd === "git" && argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          throw new Error("git push should not be called when pre-flight fires a hard error");
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-agent-adds-workflow-preflight" }, {});

      expect(result.success).toBe(false);
      expect(result.error_type).toBe("workflows_scope_required");
      expect(result.error).toContain("'workflows' scope");
      expect(result.error).toContain("allow-workflows");
      expect(result.skipped).toBeUndefined();
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("'workflows' scope"));
      const errorCalls = mockCore.error.mock.calls.map(c => c[0]);
      expect(errorCalls.some(msg => msg.includes("Failed to create review PR"))).toBe(false);
    });

    it("should return hard workflows_scope_required error when agent itself adds a workflow file (review branch post-push rejection)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      createPatchFile("review-branch-agent-adds-workflow-postpush");

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        // Pre-flight finds no workflow files — push proceeds
        if (cmd === "git" && argList[0] === "log" && argList.includes(".github/workflows/")) {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        // Agent's post-apply diff includes a workflow file
        if (cmd === "git" && argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        // Push is rejected for workflows scope
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          return {
            exitCode: 1,
            stdout: "",
            stderr: "! [remote rejected] branch -> branch (`workflows` scope may be required.)",
          };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-agent-adds-workflow-postpush" }, {});

      expect(result.success).toBe(false);
      expect(result.error_type).toBe("workflows_scope_required");
      expect(result.error).toContain("'workflows' scope");
      expect(result.skipped).toBeUndefined();
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("'workflows' scope"));
    });

    it("should return hard workflows_scope_required error when agent adds workflow file (fallback branch pre-flight)", async () => {
      createPatchFile("fallback-branch-agent-adds-workflow-preflight");

      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" }); // git rev-parse HEAD (before patch)
      mockExec.exec.mockResolvedValueOnce(0); // git am

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (argList[0] === "rev-parse" && argList[1] === "origin/feature-branch^{commit}") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--merges") {
          return { exitCode: 0, stdout: "0\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--parents") {
          return { exitCode: 0, stdout: "2222222222222222222222222222222222222222 1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "ls-remote" && argList[2] === "refs/heads/feature-branch") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\trefs/heads/feature-branch\n", stderr: "" };
        }
        if (argList[0] === "log") {
          if (argList.includes(".github/workflows/")) {
            // Pre-flight detects workflow file in branch history
            return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
          }
          return { exitCode: 0, stdout: "Test commit\n", stderr: "" };
        }
        // Agent's post-apply diff includes a workflow file
        if (argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        if (argList[0] === "diff-tree") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error: branch protection"));
      mockExec.exec.mockRejectedValueOnce(new Error("! [rejected] feature-branch -> feature-branch (non-fast-forward)"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "fallback-branch-agent-adds-workflow-preflight" }, {});

      expect(result.success).toBe(false);
      expect(result.error_type).toBe("workflows_scope_required");
      expect(result.error).toContain("'workflows' scope");
      expect(result.skipped).toBeUndefined();
    });

    it("should return hard workflows_scope_required error when agent adds workflow file (fallback branch post-push rejection)", async () => {
      createPatchFile("fallback-branch-agent-adds-workflow-postpush");

      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" }); // git rev-parse HEAD (before patch)
      mockExec.exec.mockResolvedValueOnce(0); // git am

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (argList[0] === "rev-parse" && argList[1] === "origin/feature-branch^{commit}") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--merges") {
          return { exitCode: 0, stdout: "0\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--parents") {
          return { exitCode: 0, stdout: "2222222222222222222222222222222222222222 1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "ls-remote" && argList[2] === "refs/heads/feature-branch") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\trefs/heads/feature-branch\n", stderr: "" };
        }
        if (argList[0] === "log") {
          if (argList.includes(".github/workflows/")) {
            // Pre-flight finds nothing — push proceeds
            return { exitCode: 0, stdout: "", stderr: "" };
          }
          return { exitCode: 0, stdout: "Test commit\n", stderr: "" };
        }
        // Agent's post-apply diff includes a workflow file
        if (argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        if (argList[0] === "diff-tree") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        // Fallback branch push rejected for workflows scope
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          return {
            exitCode: 1,
            stdout: "",
            stderr: "! [remote rejected] branch -> branch (`workflows` scope may be required.)",
          };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error: branch protection"));
      mockExec.exec.mockRejectedValueOnce(new Error("! [rejected] feature-branch -> feature-branch (non-fast-forward)"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "fallback-branch-agent-adds-workflow-postpush" }, {});

      expect(result.success).toBe(false);
      expect(result.error_type).toBe("workflows_scope_required");
      expect(result.error).toContain("'workflows' scope");
      expect(result.skipped).toBeUndefined();
    });

    it("should skip non-fatally when GITHUB_BASE_SHA fallback detects pre-existing workflow files and agent has none (review branch pre-flight)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GITHUB_BASE_SHA = "base-sha-from-github-actions";
      createPatchFile("review-branch-github-base-sha-fallback-skip");

      let workflowLogCallCount = 0;
      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (cmd === "git" && argList[0] === "log" && argList.includes(".github/workflows/")) {
          workflowLogCallCount++;
          if (workflowLogCallCount === 1) {
            // First call: primary baseline (origin/<base>) unavailable in shallow clone
            return { exitCode: 128, stdout: "", stderr: "fatal: ambiguous argument: unknown revision" };
          }
          // Second call: GITHUB_BASE_SHA fallback detects pre-existing workflow file
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        // Agent's post-apply diff has no workflow files
        if (cmd === "git" && argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: "README.md\n", stderr: "" };
        }
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          throw new Error("git push should not be called when pre-flight fires");
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-github-base-sha-fallback-skip" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error_type).toBeUndefined();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("pre-existing commits"));
      // Verify both baselines were tried (primary failed, GITHUB_BASE_SHA succeeded)
      expect(workflowLogCallCount).toBe(2);
    });

    it("should return hard workflows_scope_required when GITHUB_BASE_SHA fallback detects agent-added workflow files", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GITHUB_BASE_SHA = "base-sha-from-github-actions";
      createPatchFile("review-branch-github-base-sha-fallback-hard-error");

      let workflowLogCallCount = 0;
      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (cmd === "git" && argList[0] === "log" && argList.includes(".github/workflows/")) {
          workflowLogCallCount++;
          if (workflowLogCallCount === 1) {
            // Primary baseline unavailable in shallow clone
            return { exitCode: 128, stdout: "", stderr: "fatal: ambiguous argument: unknown revision" };
          }
          // GITHUB_BASE_SHA fallback detects workflow file
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        // Agent's post-apply diff includes a workflow file
        if (cmd === "git" && argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: ".github/workflows/ci.yml\n", stderr: "" };
        }
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          throw new Error("git push should not be called when pre-flight fires a hard error");
        }
        return originalGetExecOutput(cmd, args, options);
      });

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "review-branch-github-base-sha-fallback-hard-error" }, {});

      expect(result.success).toBe(false);
      expect(result.error_type).toBe("workflows_scope_required");
      expect(result.skipped).toBeUndefined();
      expect(workflowLogCallCount).toBe(2);
    });

    it("should skip non-fatally when fallback branch push rejected for workflows scope and agent has no workflow files (post-push path)", async () => {
      createPatchFile("fallback-branch-scope-skip-postpush");

      mockExec.exec.mockResolvedValueOnce(0); // fetch
      mockExec.exec.mockResolvedValueOnce(0); // rev-parse
      mockExec.exec.mockResolvedValueOnce(0); // checkout
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 0, stdout: "before-sha\n", stderr: "" }); // git rev-parse HEAD (before patch)
      mockExec.exec.mockResolvedValueOnce(0); // git am

      const originalGetExecOutput = mockExec.getExecOutput;
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args, options) => {
        const argList = Array.isArray(args) ? args : [];
        if (argList[0] === "rev-parse" && argList[1] === "origin/feature-branch^{commit}") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--merges") {
          return { exitCode: 0, stdout: "0\n", stderr: "" };
        }
        if (argList[0] === "rev-list" && argList[1] === "--parents") {
          return { exitCode: 0, stdout: "2222222222222222222222222222222222222222 1111111111111111111111111111111111111111\n", stderr: "" };
        }
        if (argList[0] === "ls-remote" && argList[2] === "refs/heads/feature-branch") {
          return { exitCode: 0, stdout: "1111111111111111111111111111111111111111\trefs/heads/feature-branch\n", stderr: "" };
        }
        if (argList[0] === "log") {
          // Pre-flight finds nothing (no workflow files in branch history)
          if (argList.includes(".github/workflows/")) {
            return { exitCode: 0, stdout: "", stderr: "" };
          }
          return { exitCode: 0, stdout: "Test commit\n", stderr: "" };
        }
        // Agent's post-apply diff has no workflow files
        if (argList[0] === "diff" && argList[1] === "--name-only" && argList[2] === "--no-renames") {
          return { exitCode: 0, stdout: "docs/readme.md\n", stderr: "" };
        }
        if (argList[0] === "diff-tree") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        // Fallback branch push rejected for workflows scope
        if (cmd === "git" && argList[0] === "push" && argList[1] === "origin") {
          return {
            exitCode: 1,
            stdout: "",
            stderr: "! [remote rejected] branch -> branch (`workflows` scope may be required.)",
          };
        }
        return originalGetExecOutput(cmd, args, options);
      });

      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error: branch protection"));
      mockExec.exec.mockRejectedValueOnce(new Error("! [rejected] feature-branch -> feature-branch (non-fast-forward)"));

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "fallback-branch-scope-skip-postpush" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error_type).toBeUndefined();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("pre-existing commits"));
    });
  });

  // ──────────────────────────────────────────────────────
  // Empty Commit / No Changes Handling
  // ──────────────────────────────────────────────────────

  describe("empty commit / no changes handling", () => {
    it("should warn when patch is empty and if-no-changes is warn", async () => {
      const patchPath = createPatchFile("should-warn-when-patch-is-empty-and-if-no-changes-is-warn", ""); // Empty patch

      const module = await loadModule();
      const handler = await module.main({ if_no_changes: "warn" });
      const result = await handler({ branch: "should-warn-when-patch-is-empty-and-if-no-changes-is-warn" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("noop"));
    });

    it("should error when patch is empty and if-no-changes is error", async () => {
      const patchPath = createPatchFile("should-error-when-patch-is-empty-and-if-no-changes-is-error", ""); // Empty patch

      const module = await loadModule();
      const handler = await module.main({ if_no_changes: "error" });
      const result = await handler({ branch: "should-error-when-patch-is-empty-and-if-no-changes-is-error" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No changes");
    });

    it("should silently skip when patch is empty and if-no-changes is ignore", async () => {
      const patchPath = createPatchFile("should-silently-skip-when-patch-is-empty-and-if-no-changes-i", ""); // Empty patch

      const module = await loadModule();
      const handler = await module.main({ if_no_changes: "ignore" });
      const result = await handler({ branch: "should-silently-skip-when-patch-is-empty-and-if-no-changes-i" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
    });

    it("should handle missing patch file", async () => {
      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-missing-patch-file" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No patch file");
    });

    it("should handle patch file with error message", async () => {
      const patchPath = createPatchFile("should-handle-patch-file-with-error-message", "Failed to generate patch: some error");

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-patch-file-with-error-message" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("error message");
    });
  });

  // ──────────────────────────────────────────────────────
  // Patch Size Validation
  // ──────────────────────────────────────────────────────

  describe("patch size validation", () => {
    it("should reject patches exceeding max size", async () => {
      // Create a patch larger than 1KB
      const largePatch = "x".repeat(2 * 1024 * 1024); // 2MB
      const patchPath = createPatchFile("should-reject-patches-exceeding-max-size", largePatch);

      const module = await loadModule();
      const handler = await module.main({ max_patch_size: 1024 }); // 1MB max
      const result = await handler({ branch: "should-reject-patches-exceeding-max-size" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("exceeds maximum");
    });

    it("should accept patches within max size", async () => {
      const patchPath = createPatchFile("should-accept-patches-within-max-size"); // Small valid patch

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ max_patch_size: 1024 });
      const result = await handler({ branch: "should-accept-patches-within-max-size" }, {});

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith("Patch size validation passed");
    });

    it("should ignore message.diff_size and enforce the patch file size", async () => {
      const largePatch = "x".repeat(2 * 1024 * 1024); // 2 MB format-patch file
      const patchPath = createPatchFile("should-prefer-message-diff-size-incremental-net-diff-over-pa", largePatch);

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ max_patch_size: 1024 }); // 1 MB max
      const result = await handler({ diff_size: 5 * 1024, branch: "should-prefer-message-diff-size-incremental-net-diff-over-pa" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Patch size");
    });

    it("should ignore an oversized message.diff_size when the patch is within limits", async () => {
      const patchPath = createPatchFile("should-reject-when-message-diff-size-exceeds-max-size-even-i"); // small valid patch

      const module = await loadModule();
      const handler = await module.main({ max_patch_size: 1024 }); // 1 MB max
      const result = await handler({ diff_size: 2 * 1024 * 1024, branch: "should-reject-when-message-diff-size-exceeds-max-size-even-i" }, {});

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith("Patch size validation passed");
    });

    it("should fall back to patch file size when message.diff_size is not provided", async () => {
      // Backward-compat: older MCP servers (or non-incremental code paths)
      // do not set diff_size. The check must continue to work using the patch
      // file size as the measurement.
      const largePatch = "x".repeat(2 * 1024 * 1024); // 2 MB
      const patchPath = createPatchFile("should-fall-back-to-patch-file-size-when-message-diff-size-i", largePatch);

      const module = await loadModule();
      const handler = await module.main({ max_patch_size: 1024 }); // 1 MB max
      const result = await handler({ branch: "should-fall-back-to-patch-file-size-when-message-diff-size-i" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("exceeds maximum");
    });

    it("should enforce max_patch_size against the uncompressed patch for bundle transport", async () => {
      const bundlePath = canonicalBundlePath("should-enforce-max-patch-size-against-bundle-size-when-bundl");
      const patchPath = createPatchFile("should-enforce-max-patch-size-against-bundle-size-when-bundl", "small patch content");
      // 2 MB dummy bundle file (contents don't matter; only size is checked)
      fs.writeFileSync(bundlePath, Buffer.alloc(2 * 1024 * 1024));

      const module = await loadModule();
      const handler = await module.main({ max_patch_size: 1024 }); // 1 MB max
      const result = await handler({ branch: "should-enforce-max-patch-size-against-bundle-size-when-bundl" }, {});

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Patch size: 1 KB"));
    });

    it("should ignore diff_size for bundle size checks", async () => {
      const bundlePath = canonicalBundlePath("should-prefer-diff-size-over-bundle-file-size-for-the-limit-");
      const patchPath = createPatchFile("should-prefer-diff-size-over-bundle-file-size-for-the-limit-", "small patch content");
      fs.writeFileSync(bundlePath, Buffer.alloc(2 * 1024 * 1024));

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ max_patch_size: 1024 }); // 1 MB max
      const result = await handler({ diff_size: 5 * 1024, branch: "should-prefer-diff-size-over-bundle-file-size-for-the-limit-" }, {});

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith("Patch size validation passed");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Patch size: 1 KB"));
    });

    it("should enforce max_patch_size against expanded post-apply content", async () => {
      const branch = "should-reject-expanded-post-apply-content";
      const patchPath = createPatchFile(branch, "small git binary patch");
      const changedFilePath = path.join(process.cwd(), "test.txt");
      fs.writeFileSync(changedFilePath, Buffer.alloc(2 * 1024 * 1024));
      mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
        const argList = Array.isArray(args) ? args : [];
        if (cmd === "git" && argList[0] === "diff" && argList[1] === "--name-only") {
          return { exitCode: 0, stdout: "test.txt\0", stderr: "" };
        }
        return { exitCode: 0, stdout: "abc123\n", stderr: "" };
      });

      try {
        const module = await loadModule();
        const handler = await module.main({ max_patch_size: 1024 }); // 1 MB max
        const result = await handler({ branch }, {});

        expect(result.success).toBe(false);
        expect(result.error).toContain("Changed content size");
      } finally {
        fs.rmSync(changedFilePath, { force: true });
      }
    });
  });

  // ──────────────────────────────────────────────────────
  // Bundle Transport Application
  // ──────────────────────────────────────────────────────

  describe("bundle transport application", () => {
    it("should apply bundle transport by updating the branch ref instead of merging", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("bundle-tip");

      try {
        const prereqSha = "a".repeat(40);
        let prereqFetched = false;
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "config") {
            return Promise.resolve({ exitCode: 1, stdout: "", stderr: "" });
          }
          if (cmd === "git" && args[0] === "bundle" && args[1] === "verify") {
            return Promise.resolve({ exitCode: 1, stdout: "", stderr: `The bundle requires this ref:\n${prereqSha}\n` });
          }
          if (cmd === "git" && args[0] === "cat-file" && args[1] === "-e") {
            // Missing until the direct SHA fetch brings it in.
            return Promise.resolve({ exitCode: prereqFetched ? 0 : 1, stdout: "", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "2\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });
        mockExec.exec.mockImplementation((cmd, args) => {
          if (cmd === "git" && Array.isArray(args) && args[0] === "fetch" && args.includes("origin") && args.includes(prereqSha)) {
            prereqFetched = true;
          }
          return Promise.resolve(0);
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);
        // Initial bundle fetch is via getExecOutput (with ignoreReturnCode: true), not exec.exec
        const bundleFetchCall = mockExec.getExecOutput.mock.calls.find(([, args, options]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode);
        expect(bundleFetchCall).toBeDefined();
        expect(bundleFetchCall[1]).toEqual(["fetch", bundlePath, "refs/heads/feature-branch:refs/bundles/push-feature-branch"]);
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["update-ref", "refs/heads/feature-branch", "refs/bundles/push-feature-branch", "remote-head"], expect.any(Object));
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["reset", "--hard"], expect.any(Object));
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["merge", "--ff-only", "refs/bundles/push-feature-branch"], expect.any(Object));
        // Primary path: the exact prerequisite SHA is fetched directly from origin; no broad deepen.
        const directFetch = mockExec.exec.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && args.includes("origin") && args.includes(prereqSha));
        expect(directFetch).toBeTruthy();
        const deepenCallIndex = mockExec.exec.mock.calls.findIndex(([, args]) => Array.isArray(args) && args[0] === "fetch" && typeof args[1] === "string" && args[1].startsWith("--deepen="));
        expect(deepenCallIndex).toBe(-1);
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should use authoritative bundle file detection before apply and match post-apply verification", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile(
        "feature-branch",
        `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/.changeset/patch-fix.md b/.changeset/patch-fix.md
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/.changeset/patch-fix.md
@@ -0,0 +1 @@
+content
--
2.34.1
`
      );
      fs.writeFileSync(bundlePath, "bundle content");

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("bundle-tip");

      try {
        const actualFiles = [".changeset/patch-fix.md", "pkg/workflow/pi_byok_env_passthrough_integration_test.go", "pkg/workflow/pi_engine.go", "pkg/workflow/pi_engine_test.go"];
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode) {
            return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
          }
          if (cmd === "git" && args[0] === "diff" && args[1] === "--name-only" && args[2] === "--no-renames") {
            return Promise.resolve({ exitCode: 0, stdout: `${actualFiles.join("\0")}\0`, stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "2\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({ allowed_files: [".changeset/**", "pkg/workflow/**"] });
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);
        expect(mockCore.info).toHaveBeenCalledWith("Pre-apply bundle verification: 4 file(s) detected from bundle transport");

        const diffCalls = mockExec.getExecOutput.mock.calls.filter(([, args]) => Array.isArray(args) && args[0] === "diff" && args[1] === "--name-only" && args[2] === "--no-renames");
        expect(diffCalls.map(([, args]) => args[4])).toContain("remote-head..refs/bundles/push-feature-branch");
        expect(diffCalls.map(([, args]) => args[4])).toContain("remote-head..HEAD");
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should use sanitized branch name (not agent-supplied message.branch) in bundle fetch refspec", async () => {
      // The agent may supply a message.branch value; the bundle fetch must use the
      // sanitized branchName from the GitHub API — never the raw agent input.
      const bundlePath = canonicalBundlePath("feature-branch; rm -rf /");
      const patchPath = createPatchFile("feature-branch; rm -rf /", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("bundle-tip");

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "2\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        // message.branch contains shell metacharacters; branchName from the GitHub API is "feature-branch"
        const result = await handler({ branch: "feature-branch; rm -rf /", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);

        // Bundle fetch must use the sanitized API branch name, not the agent-supplied message.branch
        const bundleFetchCall = mockExec.getExecOutput.mock.calls.find(([, args, opts]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath && opts && opts.ignoreReturnCode);
        expect(bundleFetchCall).toBeDefined();
        // Refspec must use the sanitized name "feature-branch", not "feature-branch; rm -rf /"
        expect(bundleFetchCall[1][2]).toMatch(/^refs\/heads\/feature-branch:/);
        expect(bundleFetchCall[1][2]).not.toContain(";");
        expect(bundleFetchCall[1][2]).not.toContain("rm");
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should fetch a HEAD-only filtered bundle when the named branch ref is absent", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");
      const bundleHead = "4f80191700da9afc6d0b20b9ec6c81fb376f8714";
      const prerequisiteSha = "e226f0c6c0f3e37d601fb64cf101fe11de68f7b9";

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue(bundleHead);

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && args[2].startsWith("refs/heads/") && options && options.ignoreReturnCode) {
            return Promise.resolve({ exitCode: 128, stdout: "", stderr: "fatal: couldn't find remote ref refs/heads/feature-branch" });
          }
          if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads") {
            return Promise.resolve({ exitCode: 0, stdout: `${bundleHead} HEAD\n`, stderr: "" });
          }
          if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && args[2].startsWith("HEAD:") && options && options.ignoreReturnCode) {
            return Promise.resolve({ exitCode: 1, stdout: "", stderr: `error: Repository lacks these prerequisite commits:\nerror: ${prerequisiteSha}` });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["bundle", "list-heads", bundlePath], expect.any(Object));
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", bundlePath, "HEAD:refs/bundles/push-feature-branch"], expect.objectContaining({ ignoreReturnCode: true }));
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "--filter=blob:none", "origin", prerequisiteSha], expect.any(Object));
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", bundlePath, "HEAD:refs/bundles/push-feature-branch"], expect.any(Object));
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should accept a valid bundle branch ref that contains a plus sign", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");
      const bundleHead = "4f80191700da9afc6d0b20b9ec6c81fb376f8714";
      const bundleSourceRef = "refs/heads/feature+fix";

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue(bundleHead);

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && args[2].startsWith("refs/heads/feature-branch:") && options?.ignoreReturnCode) {
            return Promise.resolve({ exitCode: 128, stdout: "", stderr: "fatal: couldn't find remote ref refs/heads/feature-branch" });
          }
          if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads") {
            return Promise.resolve({ exitCode: 0, stdout: `${bundleHead} ${bundleSourceRef}\n`, stderr: "" });
          }
          if (cmd === "git" && args[0] === "check-ref-format") {
            return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["check-ref-format", bundleSourceRef], expect.objectContaining({ ignoreReturnCode: true }));
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", bundlePath, `${bundleSourceRef}:refs/bundles/push-feature-branch`], expect.objectContaining({ ignoreReturnCode: true }));
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should ignore an invalid bundle branch ref when resolving a HEAD-only bundle", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");
      const bundleHead = "4f80191700da9afc6d0b20b9ec6c81fb376f8714";
      const invalidBranchRef = "refs/heads/foo..bar";

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue(bundleHead);

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && args[2].startsWith("refs/heads/feature-branch:") && options?.ignoreReturnCode) {
            return Promise.resolve({ exitCode: 128, stdout: "", stderr: "fatal: couldn't find remote ref refs/heads/feature-branch" });
          }
          if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads") {
            return Promise.resolve({ exitCode: 0, stdout: `${bundleHead} ${invalidBranchRef}\n${bundleHead} HEAD\n`, stderr: "" });
          }
          if (cmd === "git" && args[0] === "check-ref-format") {
            return Promise.resolve({ exitCode: 1, stdout: "", stderr: "invalid ref" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", bundlePath, "HEAD:refs/bundles/push-feature-branch"], expect.objectContaining({ ignoreReturnCode: true }));
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should reject ambiguous valid bundle branch refs", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");
      const bundleHead = "4f80191700da9afc6d0b20b9ec6c81fb376f8714";
      const bundleSourceRefs = ["refs/heads/feature-one", "refs/heads/feature-two"];

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue(bundleHead);

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && args[2].startsWith("refs/heads/feature-branch:") && options?.ignoreReturnCode) {
            return Promise.resolve({ exitCode: 128, stdout: "", stderr: "fatal: couldn't find remote ref refs/heads/feature-branch" });
          }
          if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads") {
            return Promise.resolve({ exitCode: 0, stdout: bundleSourceRefs.map(ref => `${bundleHead} ${ref}`).join("\n"), stderr: "" });
          }
          if (cmd === "git" && args[0] === "check-ref-format") {
            return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(false);
        expect(result.error).toContain("expected exactly 1 refs/heads entry, found 2");
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should fetch prerequisite commits and retry bundle fetch when bundle lacks prerequisites", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");
      const missingSha = "172f87a830f57a29470efe7646d141069434a893";

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("bundle-tip");

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode) {
            // Initial bundle fetch fails with prerequisite error (the real race condition scenario)
            return Promise.resolve({ exitCode: 1, stderr: `error: Repository lacks these prerequisite commits:\nerror: ${missingSha}`, stdout: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);
        // Repo is not shallow and not sparse in this scenario, so no --filter=blob:none is used:
        // the local clone already has all blobs and we must not convert it to a partial clone.
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", missingSha], expect.any(Object));
        // Should have retried the bundle fetch via exec after fetching prerequisites
        const bundleRetryFetch = mockExec.exec.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath);
        expect(bundleRetryFetch).toBeDefined();
        expect(bundleRetryFetch[1]).toEqual(["fetch", bundlePath, "refs/heads/feature-branch:refs/bundles/push-feature-branch"]);
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should squash merge commits into a single regular commit before pushSignedCommits", async () => {
      // Scenario: agent ran `git merge origin/main` producing a merge commit.
      // The handler must detect the merge commit in the range and perform a
      // soft-reset + recommit to linearize it, then call pushSignedCommits with
      // the linearized history.
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("linearized-tip");

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list" && args[1] === "--merges" && args[2] === "--count") {
            // Report 1 merge commit in the range
            return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
          }
          // First non-merge commit message lookup (--no-merges --format=%B --reverse <base>..HEAD)
          if (cmd === "git" && args[0] === "log" && args.includes("--no-merges")) {
            return Promise.resolve({ exitCode: 0, stdout: "Fix typo in README\n", stderr: "" });
          }
          // Staged-changes validation after soft reset
          if (cmd === "git" && args[0] === "diff" && args[1] === "--cached" && args[2] === "--name-only") {
            return Promise.resolve({ exitCode: 0, stdout: "README.md\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "2\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);

        // Should have detected the merge commit count with the correct range
        const mergeCountCall = mockExec.getExecOutput.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "rev-list" && args[1] === "--merges" && args[2] === "--count");
        expect(mergeCountCall).toBeDefined();
        // Verify the range parameter: squashBase (= remoteHeadBeforePatch = "remote-head") to HEAD
        expect(mergeCountCall[1]).toEqual(["rev-list", "--merges", "--count", "remote-head..HEAD"]);

        // Verify only the first non-merge commit message is fetched (--max-count=1)
        const firstNonMergeCall = mockExec.getExecOutput.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "log" && args.includes("--no-merges"));
        expect(firstNonMergeCall).toBeDefined();
        expect(firstNonMergeCall[1]).toContain("--max-count=1");

        // Should have performed the soft-reset to squash the merge commit
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["reset", "--soft", "remote-head"], expect.any(Object));

        // Should have used the first non-merge commit's message for the squash commit
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["commit", "--allow-empty", "--no-verify", "-m", "Fix typo in README"], expect.any(Object));

        // pushSignedCommits should still be called after linearization
        expect(pushSignedSpy).toHaveBeenCalled();
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should fall back to merge commit message when no non-merge commits exist in range", async () => {
      // Scenario: the only commit in the range is the merge commit itself (agent only ran
      // `git merge origin/main`, no additional work commits).
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("linearized-tip");

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list" && args[1] === "--merges" && args[2] === "--count") {
            return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
          }
          // No non-merge commits exist → return empty string
          if (cmd === "git" && args[0] === "log" && args.includes("--no-merges")) {
            return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
          }
          // Fall back: last commit message is the merge commit's message
          if (cmd === "git" && args[0] === "log" && args[1] === "-1" && args[2] === "--format=%B") {
            return Promise.resolve({ exitCode: 0, stdout: "Merge branch 'main' into feature-branch\n", stderr: "" });
          }
          // Staged-changes validation after soft reset
          if (cmd === "git" && args[0] === "diff" && args[1] === "--cached" && args[2] === "--name-only") {
            return Promise.resolve({ exitCode: 0, stdout: "src/index.js\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "2\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);

        // Should have fallen back to the merge commit's message
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["commit", "--allow-empty", "--no-verify", "-m", "Merge branch 'main' into feature-branch"], expect.any(Object));
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should skip linearization when no merge commits are present", async () => {
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("tip-sha");

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list" && args[1] === "--merges" && args[2] === "--count") {
            // No merge commits in range
            return Promise.resolve({ exitCode: 0, stdout: "0\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "2\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        expect(result.success).toBe(true);

        // soft-reset should NOT have been called (no merge commits to squash)
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["reset", "--soft", expect.any(String)], expect.any(Object));

        // pushSignedCommits should still be called
        expect(pushSignedSpy).toHaveBeenCalled();
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should roll back to original HEAD and emit warning when staged-changes validation fails after soft reset", async () => {
      // Scenario: merge commit detected, soft reset succeeds, but no staged changes are
      // found afterwards (e.g. the range was already empty / only no-op commits).
      // The handler should roll back to the originalHead and warn, then let
      // pushSignedCommits proceed (and potentially surface its own error).
      const bundlePath = canonicalBundlePath("feature-branch");
      const patchPath = createPatchFile("feature-branch", "small patch content");
      fs.writeFileSync(bundlePath, "bundle content");

      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("tip-sha");
      const warningSpy = vi.spyOn(core, "warning");

      try {
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
          if (cmd === "git" && args[0] === "ls-remote") {
            return Promise.resolve({ exitCode: 0, stdout: "remote-head\trefs/heads/feature-branch\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
            return Promise.resolve({ exitCode: 0, stdout: "original-sha\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
            return Promise.resolve({ exitCode: 0, stdout: "false\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list" && args[1] === "--merges" && args[2] === "--count") {
            return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
          }
          if (cmd === "git" && args[0] === "log" && args.includes("--no-merges")) {
            return Promise.resolve({ exitCode: 0, stdout: "Fix something\n", stderr: "" });
          }
          // No staged changes after soft reset → triggers validation error
          if (cmd === "git" && args[0] === "diff" && args[1] === "--cached" && args[2] === "--name-only") {
            return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
          }
          if (cmd === "git" && args[0] === "rev-list") {
            return Promise.resolve({ exitCode: 0, stdout: "2\n", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        });

        const module = await loadModule();
        const handler = await module.main({});
        const result = await handler({ branch: "feature-branch", diff_size: 5 * 1024 }, {});

        // The overall push should still succeed (warning path, not fatal error)
        expect(result.success).toBe(true);

        // Should have attempted rollback to originalHead
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["reset", "--hard", "original-sha"], expect.any(Object));

        // Should have emitted the outer warning mentioning linearization failure
        const outerWarning = warningSpy.mock.calls.find(([msg]) => typeof msg === "string" && msg.includes("failed to linearize merge commits"));
        expect(outerWarning).toBeDefined();

        // pushSignedCommits should still be called after the failed linearization
        expect(pushSignedSpy).toHaveBeenCalled();
      } finally {
        pushSignedSpy.mockRestore();
        warningSpy.mockRestore();
      }
    });
  });

  // ──────────────────────────────────────────────────────
  // Title Prefix and Labels Validation
  // ──────────────────────────────────────────────────────

  describe("title prefix and labels validation", () => {
    it("should reject PR without required title prefix", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: { full_name: "test-owner/test-repo", fork: false },
          },
          base: { repo: { full_name: "test-owner/test-repo" } },
          title: "Some PR Title",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-reject-pr-without-required-title-prefix");

      const module = await loadModule();
      const handler = await module.main({ title_prefix: "[bot] " });
      const result = await handler({ branch: "should-reject-pr-without-required-title-prefix" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("does not start with required prefix");
    });

    it("should accept PR with required title prefix", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: { full_name: "test-owner/test-repo", fork: false },
          },
          base: { repo: { full_name: "test-owner/test-repo" } },
          title: "[bot] Automated PR",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-accept-pr-with-required-title-prefix");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ title_prefix: "[bot] " });
      const result = await handler({ branch: "should-accept-pr-with-required-title-prefix" }, {});

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith('✓ Title prefix validation passed: "[bot] "');
    });

    it("should reject PR without required labels", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: { full_name: "test-owner/test-repo", fork: false },
          },
          base: { repo: { full_name: "test-owner/test-repo" } },
          title: "Test PR",
          labels: [{ name: "bug" }],
        },
      });

      const patchPath = createPatchFile("should-reject-pr-without-required-labels");

      const module = await loadModule();
      const handler = await module.main({ labels: ["automated", "enhancement"] });
      const result = await handler({ branch: "should-reject-pr-without-required-labels" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("missing required labels");
    });

    it("should accept PR with all required labels", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: { full_name: "test-owner/test-repo", fork: false },
          },
          base: { repo: { full_name: "test-owner/test-repo" } },
          title: "Test PR",
          labels: [{ name: "automated" }, { name: "enhancement" }],
        },
      });

      const patchPath = createPatchFile("should-accept-pr-with-all-required-labels");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ labels: ["automated", "enhancement"] });
      const result = await handler({ branch: "should-accept-pr-with-all-required-labels" }, {});

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith("✓ Labels validation passed: automated, enhancement");
    });
  });

  // ──────────────────────────────────────────────────────
  // Commit Title Suffix
  // ──────────────────────────────────────────────────────

  describe("commit title suffix", () => {
    it("should append suffix to commit messages in patch", async () => {
      const patchPath = createPatchFile("should-append-suffix-to-commit-messages-in-patch");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ commit_title_suffix: " [skip ci]" });
      const result = await handler({ branch: "should-append-suffix-to-commit-messages-in-patch" }, {});

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith('Appending commit title suffix: " [skip ci]"');

      // Verify patch was modified
      const modifiedPatch = fs.readFileSync(patchPath, "utf8");
      expect(modifiedPatch).toContain("[skip ci]");
    });
  });

  // ──────────────────────────────────────────────────────
  // Staged Mode
  // ──────────────────────────────────────────────────────

  describe("staged mode", () => {
    it("should generate preview instead of pushing in staged mode", async () => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      const patchPath = createPatchFile("should-generate-preview-instead-of-pushing-in-staged-mode");

      // Re-import the module to pick up env var
      delete require.cache[require.resolve("./push_to_pull_request_branch.cjs")];

      const module = require("./push_to_pull_request_branch.cjs");
      const handler = await module.main({});
      const result = await handler({ branch: "should-generate-preview-instead-of-pushing-in-staged-mode" }, {});

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      // Should not have made any git commands
      expect(mockExec.exec).not.toHaveBeenCalled();
    });
  });

  // ──────────────────────────────────────────────────────
  // Max Count Limiting
  // ──────────────────────────────────────────────────────

  describe("max count limiting", () => {
    it("should skip messages after max count reached", async () => {
      const patchPath = createPatchFile("should-skip-messages-after-max-count-reached");
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({ max: 1 });

      // First call succeeds
      const result1 = await handler({ branch: "should-skip-messages-after-max-count-reached" }, {});
      expect(result1.success).toBe(true);

      // Second call is skipped
      const result2 = await handler({ branch: "should-skip-messages-after-max-count-reached" }, {});
      expect(result2.success).toBe(false);
      expect(result2.skipped).toBe(true);
      expect(result2.error).toContain("Max count");
    });
  });

  // ──────────────────────────────────────────────────────
  // Branch Name Sanitization
  // ──────────────────────────────────────────────────────

  describe("branch name sanitization", () => {
    it("should sanitize branch names with shell metacharacters", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature;rm -rf /",
            repo: { full_name: "test-owner/test-repo", fork: false },
          },
          base: { repo: { full_name: "test-owner/test-repo" } },
          title: "Test PR",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-sanitize-branch-names-with-shell-metacharacters");

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-sanitize-branch-names-with-shell-metacharacters" }, {});

      // The normalizeBranchName should sanitize dangerous characters
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Branch name sanitized"));
    });

    it("should reject empty branch name after sanitization", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: ";$`|&", // All dangerous chars
            repo: { full_name: "test-owner/test-repo", fork: false },
          },
          base: { repo: { full_name: "test-owner/test-repo" } },
          title: "Test PR",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-reject-empty-branch-name-after-sanitization");

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-reject-empty-branch-name-after-sanitization" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Invalid branch name");
    });
  });

  // ──────────────────────────────────────────────────────
  // PR API Error Handling
  // ──────────────────────────────────────────────────────

  describe("PR API error handling", () => {
    it("should handle PR not found error", async () => {
      mockGithub.rest.pulls.get.mockRejectedValue(new Error("Not Found"));

      const patchPath = createPatchFile("should-handle-pr-not-found-error");

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-pr-not-found-error" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to determine branch name");
    });

    it("should handle network error fetching PR", async () => {
      mockGithub.rest.pulls.get.mockRejectedValue(new Error("Network timeout"));

      const patchPath = createPatchFile("should-handle-network-error-fetching-pr");

      const module = await loadModule();
      const handler = await module.main({});
      const result = await handler({ branch: "should-handle-network-error-fetching-pr" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to determine branch name");
    });
  });

  // ──────────────────────────────────────────────────────
  // Extra Empty Commit (CI Trigger)
  // ──────────────────────────────────────────────────────

  describe("extra empty commit for CI trigger", () => {
    it("should push extra empty commit when CI trigger token is set", async () => {
      process.env.GH_AW_CI_TRIGGER_TOKEN = "ghp_test_token";

      // Re-import modules to pick up env var
      delete require.cache[require.resolve("./push_to_pull_request_branch.cjs")];
      delete require.cache[require.resolve("./extra_empty_commit.cjs")];

      const patchPath = createPatchFile("should-push-extra-empty-commit-when-ci-trigger-token-is-set");

      // Mock successful commands
      mockExec.exec.mockResolvedValue(0);
      mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
        if (args && args[0] === "rev-parse") {
          return { exitCode: 0, stdout: "abc123\n", stderr: "" };
        }
        if (args && args[0] === "rev-list") {
          return { exitCode: 0, stdout: "1\n", stderr: "" }; // 1 new commit
        }
        if (args && args[0] === "log") {
          return { exitCode: 0, stdout: "COMMIT:abc\nfile.txt\n", stderr: "" };
        }
        return { exitCode: 0, stdout: "", stderr: "" };
      });

      const module = require("./push_to_pull_request_branch.cjs");
      const handler = await module.main({});
      const result = await handler({ branch: "should-push-extra-empty-commit-when-ci-trigger-token-is-set" }, {});

      expect(result.success).toBe(true);
      // The extra empty commit should have been attempted
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Extra empty commit"));
    });
  });

  // ──────────────────────────────────────────────────────
  // Handler Type Export
  // ──────────────────────────────────────────────────────

  describe("module exports", () => {
    it("should export HANDLER_TYPE as push_to_pull_request_branch", async () => {
      const module = await loadModule();

      expect(module.HANDLER_TYPE).toBe("push_to_pull_request_branch");
    });

    it("should export main function", async () => {
      const module = await loadModule();

      expect(typeof module.main).toBe("function");
    });
  });

  // ──────────────────────────────────────────────────────
  // allowed-files strict allowlist
  // ──────────────────────────────────────────────────────

  describe("allowed-files strict allowlist", () => {
    /**
     * Helper to create a patch that touches only the given file path(s).
     * Produces minimal but valid `diff --git` headers so extractPathsFromPatch works.
     */
    function createPatchWithFiles(...filePaths) {
      const diffs = filePaths
        .map(
          p => `diff --git a/${p} b/${p}
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/${p}
@@ -0,0 +1 @@
+content
`
        )
        .join("\n");
      return `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

${diffs}
--
2.34.1
`;
    }

    it("should reject files outside the allowed-files allowlist", async () => {
      const patchPath = createPatchFile("should-reject-files-outside-the-allowed-files-allowlist", createPatchWithFiles("src/index.js"));

      const module = await loadModule();
      const handler = await module.main({ allowed_files: [".changeset/**"] });
      const result = await handler({ branch: "should-reject-files-outside-the-allowed-files-allowlist" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.reasonCode).toBe("POLICY_FILE_PROTECTION_DENIED");
      expect(result.error).toContain("outside the allowed-files list");
      expect(result.error).toContain("src/index.js");
    });

    it("should accept files that match the allowed-files pattern", async () => {
      const patchPath = createPatchFile("should-accept-files-that-match-the-allowed-files-pattern", createPatchWithFiles(".changeset/my-feature-fix.md"));
      mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "diff" && args[1] === "--name-only" && args[2] === "--no-renames") {
          return { exitCode: 0, stdout: ".changeset/my-feature-fix.md\0", stderr: "" };
        }
        return { exitCode: 0, stdout: "abc123\n", stderr: "" };
      });

      const module = await loadModule();
      const handler = await module.main({ allowed_files: [".changeset/**"] });
      const result = await handler({ branch: "should-accept-files-that-match-the-allowed-files-pattern" }, {});

      expect(result.success).toBe(true);
    });

    it("should still block a protected file when it is in the allowlist but protected-files: allowed is not set", async () => {
      // allowed-files and protected-files are orthogonal: both checks must pass.
      // Matching the allowlist does NOT bypass the protected-files policy.
      const patchPath = createPatchFile("should-still-block-a-protected-file-when-it-is-in-the-allowl", createPatchWithFiles("package.json"));

      const module = await loadModule();
      const handler = await module.main({
        allowed_files: ["package.json"],
        protected_files: ["package.json"],
        protected_files_policy: "blocked",
      });
      const result = await handler({ branch: "should-still-block-a-protected-file-when-it-is-in-the-allowl" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.reasonCode).toBe("POLICY_FILE_PROTECTION_DENIED");
      expect(result.error).toContain("protected files");
      expect(result.error).toContain("package.json");
    });

    it("should allow a protected file when both allowed-files matches and protected-files: allowed is set", async () => {
      // Both checks are satisfied explicitly: allowlist scope + protected-files permission.
      const patchPath = createPatchFile("should-allow-a-protected-file-when-both-allowed-files-matche", createPatchWithFiles("package.json"));
      mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "diff" && args[1] === "--name-only" && args[2] === "--no-renames") {
          return { exitCode: 0, stdout: "package.json\0", stderr: "" };
        }
        return { exitCode: 0, stdout: "abc123\n", stderr: "" };
      });

      const module = await loadModule();
      const handler = await module.main({
        allowed_files: ["package.json"],
        protected_files: ["package.json"],
        protected_files_policy: "allowed",
      });
      const result = await handler({ branch: "should-allow-a-protected-file-when-both-allowed-files-matche" }, {});

      expect(result.success).toBe(true);
    });

    it("should create a fallback issue instead of pushing when post-apply detects protected files", async () => {
      const patchPath = createPatchFile("should-create-a-fallback-issue-instead-of-pushing-when-post-", createPatchWithFiles("README.md"));
      const pushSignedCommitsModule = require("./push_signed_commits.cjs");
      const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("remote-head-after");
      const promptsDir = path.join(tempDir, "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.writeFileSync(path.join(promptsDir, "manifest_protection_push_to_pr_fallback.md"), "Protected files: {{files}}", "utf8");
      process.env.GH_AW_PROMPTS_DIR = promptsDir;
      mockExec.getExecOutput.mockImplementation(async (cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "ls-remote") {
          return { exitCode: 0, stdout: "preflight-sha\trefs/heads/feature-branch\n", stderr: "" };
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "rev-parse") {
          return { exitCode: 0, stdout: "local-head-before\n", stderr: "" };
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "diff" && args[1] === "--name-only" && args[2] === "--no-renames") {
          return { exitCode: 0, stdout: ".github/CODEOWNERS\n", stderr: "" };
        }
        return { exitCode: 0, stdout: "", stderr: "" };
      });

      const module = await loadModule();
      const handler = await module.main({
        protected_path_prefixes: [".github/"],
        protected_files_policy: "fallback-to-issue",
      });
      try {
        const result = await handler({ branch: "should-create-a-fallback-issue-instead-of-pushing-when-post-" }, {});

        expect(result.success).toBe(true);
        expect(result.fallback_used).toBe(true);
        expect(result.issue_number).toBe(456);
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["reset", "--hard", "local-head-before"], expect.any(Object));
        expect(mockGithub.rest.issues.create).toHaveBeenCalledTimes(1);
        expect(pushSignedSpy).not.toHaveBeenCalled();
      } finally {
        pushSignedSpy.mockRestore();
      }
    });

    it("should give patch-based manual apply instructions for protected-files fallback (patch transport)", async () => {
      createPatchFile("should-give-patch-based-manual-apply-instructions", createPatchWithFiles("package.json"));

      const module = await loadModule();
      const handler = await module.main({
        protected_files: ["package.json"],
        protected_files_policy: "fallback-to-issue",
      });
      const result = await handler({ branch: "should-give-patch-based-manual-apply-instructions" }, {});

      expect(result.success).toBe(true);
      expect(result.fallback_used).toBe(true);

      const issueBody = mockGithub.rest.issues.create.mock.calls[0][0].body;
      expect(issueBody).toContain("gh run download");
      expect(issueBody).toContain("git am --3way");
      expect(issueBody).not.toContain("refs/bundles/");
    });

    it("should give bundle-based manual apply instructions for protected-files fallback (bundle transport)", async () => {
      createPatchFile("should-give-bundle-based-manual-apply-instructions", createPatchWithFiles("package.json"));
      const bundlePath = canonicalBundlePath("should-give-bundle-based-manual-apply-instructions");
      fs.writeFileSync(bundlePath, "bundle content");

      const module = await loadModule();
      const handler = await module.main({
        protected_files: ["package.json"],
        protected_files_policy: "fallback-to-issue",
      });
      const result = await handler({ branch: "should-give-bundle-based-manual-apply-instructions" }, {});

      expect(result.success).toBe(true);
      expect(result.fallback_used).toBe(true);

      const issueBody = mockGithub.rest.issues.create.mock.calls[0][0].body;
      expect(issueBody).toContain("gh run download");
      // Bundle transport: instructions should fetch the bundle and reset --hard,
      // not use git am (which fails on bundle files - see issue #55509)
      expect(issueBody).toContain("refs/bundles/manual-apply");
      expect(issueBody).toContain("git reset --hard refs/bundles/manual-apply");
      expect(issueBody).not.toContain("git am --3way");
    });

    it("should use configured fork remote in protected-files manual apply instructions", async () => {
      const branch = "should-use-configured-fork-remote-in-manual-apply-instr";
      createPatchFile(branch, createPatchWithFiles("package.json"));
      const bundlePath = canonicalBundlePath(branch);
      fs.writeFileSync(bundlePath, "bundle content");
      mockContext.payload.pull_request.head.repo = {
        full_name: "fork-owner/test-repo",
        fork: true,
        owner: { login: "fork-owner" },
      };
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: {
              full_name: "fork-owner/test-repo",
              fork: true,
            },
          },
          base: {
            repo: {
              full_name: "test-owner/test-repo",
            },
          },
          title: "Test PR",
          labels: [],
        },
      });

      const module = await loadModule();
      const handler = await module.main({
        protected_files: ["package.json"],
        protected_files_policy: "fallback-to-issue",
        "head-repo": "fork-owner/test-repo",
        allowed_repos: ["test-owner/test-repo", "fork-owner/test-repo"],
      });
      const result = await handler({ branch }, {});

      expect(result.success).toBe(true);
      expect(result.fallback_used).toBe(true);

      const issueBody = mockGithub.rest.issues.create.mock.calls[0][0].body;
      expect(issueBody).toContain("git fetch 'https://github.com/fork-owner/test-repo.git'");
      expect(issueBody).toContain("git push 'https://github.com/fork-owner/test-repo.git'");
      expect(issueBody).not.toContain("git fetch origin");
      expect(issueBody).not.toContain("git push origin");
    });

    it("should block a protected file when no allowed-files list is configured", async () => {
      const patchPath = createPatchFile("should-block-a-protected-file-when-no-allowed-files-list-is-", createPatchWithFiles("package.json"));

      const module = await loadModule();
      const handler = await module.main({
        protected_files: ["package.json"],
        protected_files_policy: "blocked",
      });
      const result = await handler({ branch: "should-block-a-protected-file-when-no-allowed-files-list-is-" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.reasonCode).toBe("POLICY_FILE_PROTECTION_DENIED");
      expect(result.error).toContain("protected files");
      expect(result.error).toContain("package.json");
    });

    it("should reject a mixed patch where at least one file is outside the allowlist", async () => {
      const patchPath = createPatchFile("should-reject-a-mixed-patch-where-at-least-one-file-is-outsi", createPatchWithFiles(".changeset/my-fix.md", "src/index.js"));

      const module = await loadModule();
      const handler = await module.main({ allowed_files: [".changeset/**"] });
      const result = await handler({ branch: "should-reject-a-mixed-patch-where-at-least-one-file-is-outsi" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.reasonCode).toBe("POLICY_FILE_PROTECTION_DENIED");
      expect(result.error).toContain("outside the allowed-files list");
      expect(result.error).toContain("src/index.js");
      expect(result.error).not.toContain(".changeset/my-fix.md");
    });
  });

  // excluded-files exclusion list
  // ──────────────────────────────────────────────────────

  describe("excluded-files exclusion list", () => {
    /**
     * Helper to create a patch that touches only the given file path(s).
     */
    function createPatchWithFiles(...filePaths) {
      const diffs = filePaths
        .map(
          p => `diff --git a/${p} b/${p}
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/${p}
@@ -0,0 +1 @@
+content
`
        )
        .join("\n");
      return `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

${diffs}
--
2.34.1
`;
    }

    it("should ignore files matching excluded-files patterns (not blocked by allowed-files)", async () => {
      // excluded-files are excluded at patch generation time via git :(exclude) pathspecs.
      // Simulate post-generation: the patch already contains only the non-ignored file.
      const patchPath = createPatchFile("should-ignore-files-matching-excluded-files-patterns-not-blo", createPatchWithFiles("src/index.js"));
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({
        excluded_files: ["auto-generated/**"],
        allowed_files: ["src/**"],
      });
      const result = await handler({ branch: "should-ignore-files-matching-excluded-files-patterns-not-blo" }, {});

      expect(result.error || "").not.toContain("outside the allowed-files list");
    });

    it("should still block non-ignored files that violate the allowed-files list", async () => {
      const patchPath = createPatchFile("should-still-block-non-ignored-files-that-violate-the-allowe", createPatchWithFiles("src/index.js", "other/file.txt"));

      const module = await loadModule();
      const handler = await module.main({
        excluded_files: ["auto-generated/**"],
        allowed_files: ["src/**"],
      });
      const result = await handler({ branch: "should-still-block-non-ignored-files-that-violate-the-allowe" }, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.reasonCode).toBe("POLICY_FILE_PROTECTION_DENIED");
      expect(result.error).toContain("outside the allowed-files list");
      expect(result.error).toContain("other/file.txt");
      expect(result.error).not.toContain("src/index.js");
    });

    it("should ignore files matching excluded-files patterns (not blocked by protected-files)", async () => {
      // excluded-files are excluded at patch generation time via git :(exclude) pathspecs.
      // Simulate post-generation: the patch already contains only the non-ignored file.
      const patchPath = createPatchFile("should-ignore-files-matching-excluded-files-patterns-not-blo", createPatchWithFiles("src/index.js"));
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const module = await loadModule();
      const handler = await module.main({
        excluded_files: ["package.json"],
        protected_files: ["package.json"],
        protected_files_policy: "blocked",
      });
      const result = await handler({ branch: "should-ignore-files-matching-excluded-files-patterns-not-blo" }, {});

      expect(result.error || "").not.toContain("protected files");
    });

    it("should allow when all patch files are ignored (even with allowed-files set)", async () => {
      // excluded-files are excluded at patch generation time via git :(exclude) pathspecs.
      // Simulate post-generation: all files were excluded so no patch file is produced.
      const nonexistentPath = canonicalPatchPath("should-allow-when-all-patch-files-are-ignored-even-with-allo");

      const module = await loadModule();
      const handler = await module.main({
        excluded_files: ["dist/**"],
        allowed_files: ["src/**"],
      });
      const result = await handler({ branch: "should-allow-when-all-patch-files-are-ignored-even-with-allo" }, {});

      // No patch → treated as no changes, not an allowlist violation
      expect(result.error || "").not.toContain("outside the allowed-files list");
    });
  });

  // ──────────────────────────────────────────────────────
  // Cross-Repo Checkout Scenarios
  // ──────────────────────────────────────────────────────

  describe("cross-repo checkout", () => {
    it("should return error when target repo differs from workflow repo and is not found in workspace", async () => {
      // GITHUB_REPOSITORY is set to "test-owner/test-repo" in beforeEach
      // Targeting "other-owner/other-repo" - different repo, not checked out
      mockGithub.rest.pulls.get = vi.fn().mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: { full_name: "other-owner/other-repo", fork: false, owner: { login: "other-owner" } },
          },
          base: {
            repo: { full_name: "other-owner/other-repo", owner: { login: "other-owner" } },
          },
          title: "Cross-repo PR",
          labels: [],
        },
      });

      const patchPath = createPatchFile("should-return-error-when-target-repo-differs-from-workflow-r");
      const module = await loadModule();
      const handler = await module.main({ "target-repo": "other-owner/other-repo" });

      const result = await handler({ pull_request_number: 42, branch: "should-return-error-when-target-repo-differs-from-workflow-r" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("other-owner/other-repo");
      expect(result.error).toContain("not found in workspace");
    });

    it("should pass cwd to git commands when target repo is checked out in a subdirectory", async () => {
      // Create a subdirectory checkout with a remote that matches "other-owner/other-repo"
      const subRepoDir = path.join(tempDir, "other-repo");
      fs.mkdirSync(subRepoDir, { recursive: true });
      const { execSync } = await import("child_process");
      execSync("git init -b main", { cwd: subRepoDir, stdio: "pipe" });
      execSync("git config user.email 'test@example.com'", { cwd: subRepoDir, stdio: "pipe" });
      execSync("git config user.name 'Test User'", { cwd: subRepoDir, stdio: "pipe" });
      execSync("git remote add origin https://github.com/other-owner/other-repo.git", { cwd: subRepoDir, stdio: "pipe" });

      // Set workspace to tempDir so findRepoCheckout scans it
      process.env.GITHUB_WORKSPACE = tempDir;

      mockGithub.rest.pulls.get = vi.fn().mockResolvedValue({
        data: {
          head: {
            ref: "feature-branch",
            repo: { full_name: "other-owner/other-repo", fork: false, owner: { login: "other-owner" } },
          },
          base: {
            repo: { full_name: "other-owner/other-repo", owner: { login: "other-owner" } },
          },
          title: "Cross-repo PR",
          labels: [],
        },
      });
      mockGithub.rest.repos.get = vi.fn().mockResolvedValue({ data: { default_branch: "main" } });
      mockGithub.rest.repos.getBranchProtection = vi.fn().mockRejectedValue(Object.assign(new Error("not protected"), { status: 404 }));

      mockExec.getExecOutput.mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" });

      const patchPath = createPatchFile("main");
      const module = await loadModule();
      const handler = await module.main({ "target-repo": "other-owner/other-repo" });

      await handler({ pull_request_number: 42, branch: "main" }, {});

      // Verify git ls-remote was called with cwd pointing at the subdirectory
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining(`Found checkout for other-owner/other-repo at: ${subRepoDir}`));

      // Verify at least one exec call received cwd pointing at the subdirectory
      const allExecCalls = [...mockExec.exec.mock.calls, ...mockExec.getExecOutput.mock.calls];
      const cwdCalls = allExecCalls.filter(call => {
        const opts = call.find(arg => arg && typeof arg === "object" && !Array.isArray(arg) && "cwd" in arg);
        return opts && opts.cwd === subRepoDir;
      });
      expect(cwdCalls.length).toBeGreaterThan(0);
    });
  });
});

// ──────────────────────────────────────────────────────
// Integration Test Recommendations
// ──────────────────────────────────────────────────────

/**
 * INTEGRATION TEST RECOMMENDATIONS
 *
 * The following scenarios require integration testing with real Git repositories.
 * These tests should be added to a separate file (e.g., push_to_pull_request_branch.integration.test.cjs)
 * and run in a CI environment with actual Git operations.
 *
 * 1. CONCURRENT PUSH SCENARIOS:
 *    - Create a test repo with a PR branch
 *    - Push a commit to the PR branch from another process
 *    - Attempt to push from the handler
 *    - Verify proper error handling for non-fast-forward rejection
 *
 * 2. FORCE-PUSH DETECTION:
 *    - Create a test repo with a PR branch
 *    - Force-push to rewrite the branch history
 *    - Attempt to apply a patch based on old history
 *    - Verify proper error message about force-push
 *
 * 3. BASE BRANCH UPDATES:
 *    - Create a test repo with a PR branch
 *    - Push commits to the base branch
 *    - Verify that pushing to PR branch still works
 *    - (Note: This should work as PR branch is independent)
 *
 * 4. MERGE CONFLICT SCENARIOS:
 *    - Create a test repo with conflicting changes
 *    - Attempt to apply a patch that conflicts
 *    - Verify clear error message about merge conflict
 *
 * 5. FORK PR EARLY FAILURE:
 *    - Create a fork repository (or simulate fork context)
 *    - Verify early failure before attempting push
 *    - Verify clear error message about fork permissions
 *
 * TEST SETUP RECOMMENDATIONS:
 * - Use GitHub API to create test repositories
 * - Use git commands to set up test scenarios
 * - Clean up test repos after each test
 * - Run these tests on a schedule, not on every commit
 */
