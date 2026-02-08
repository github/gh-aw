import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

describe("checkout_pr_branch.cjs", () => {
  let mockCore;
  let mockExec;
  let mockContext;

  beforeEach(() => {
    // Mock core actions methods
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    // Mock exec
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
    };

    // Mock context
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
          head: {
            ref: "feature-branch",
            sha: "head-sha-123",
            repo: {
              full_name: "test-owner/test-repo",
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
      },
    };

    global.core = mockCore;
    global.exec = mockExec;
    global.context = mockContext;
    process.env.GITHUB_TOKEN = "test-token";
  });

  afterEach(() => {
    delete global.core;
    delete global.exec;
    delete global.context;
    delete process.env.GITHUB_TOKEN;
    vi.clearAllMocks();
  });

  const runScript = async () => {
    // Import the script directly to access its main function
    const { execFileSync } = await import("child_process");
    const fs = await import("fs");
    const path = await import("path");

    const scriptPath = path.join(import.meta.dirname, "checkout_pr_branch.cjs");
    const scriptContent = fs.readFileSync(scriptPath, "utf8");

    // Mock require for the script
    const mockRequire = module => {
      if (module === "./error_helpers.cjs") {
        return { getErrorMessage: error => (error instanceof Error ? error.message : String(error)) };
      }
      if (module === "./messages_core.cjs") {
        return {
          renderTemplate: (template, context) => {
            return template.replace(/\{(\w+)\}/g, (match, key) => {
              const value = context[key];
              return value !== undefined && value !== null ? String(value) : match;
            });
          },
        };
      }
      if (module === "./pr_helpers.cjs") {
        return {
          detectForkPR: pullRequest => {
            // Replicate the actual logic for testing
            let isFork = false;
            let reason = "same repository";

            if (!pullRequest.head?.repo) {
              isFork = true;
              reason = "head repository deleted (was likely a fork)";
            } else if (pullRequest.head.repo.fork === true) {
              isFork = true;
              reason = "head.repo.fork flag is true";
            } else if (pullRequest.head.repo.full_name !== pullRequest.base?.repo?.full_name) {
              isFork = true;
              reason = "different repository names";
            }

            return { isFork, reason };
          },
        };
      }
      if (module === "fs") {
        return {
          readFileSync: (path, encoding) => {
            // Return mock template for pr_checkout_failure.md
            if (path.includes("pr_checkout_failure.md")) {
              return `## ❌ Failed to Checkout PR Branch

**Error:** {error_message}

### Possible Reasons

This failure typically occurs when:
- The pull request has been closed or merged
- The branch has been deleted
- There are insufficient permissions to access the PR

### What to Do

If the pull request is closed, you may need to:
1. Reopen the pull request, or
2. Create a new pull request with the changes

If the pull request is still open, verify that:
- The branch still exists in the repository
- You have the necessary permissions to access it
`;
            }
            throw new Error(`Unexpected file read: ${path}`);
          },
        };
      }
      throw new Error(`Module ${module} not mocked in test`);
    };

    // Execute the script in a new context with our mocks
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
    const wrappedScript = new AsyncFunction("core", "exec", "context", "require", scriptContent.replace(/module\.exports = \{ main \};?\s*$/s, "await main();"));

    try {
      await wrappedScript(mockCore, mockExec, mockContext, mockRequire);
    } catch (error) {
      // Errors are handled by the script itself via core.setFailed
    }
  };

  describe("pull_request events", () => {
    it("should checkout PR branch using git fetch and checkout", async () => {
      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: pull_request");
      expect(mockCore.info).toHaveBeenCalledWith("Pull Request #123");

      // Verify detailed context logging
      expect(mockCore.startGroup).toHaveBeenCalledWith("📋 PR Context Details");
      expect(mockCore.info).toHaveBeenCalledWith("Event type: pull_request");

      // Verify strategy logging
      expect(mockCore.startGroup).toHaveBeenCalledWith("🔄 Checkout Strategy");
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch + checkout");

      // Verify actual checkout commands
      expect(mockCore.info).toHaveBeenCalledWith("Fetching branch: feature-branch from origin");
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch"]);

      expect(mockCore.info).toHaveBeenCalledWith("Checking out branch: feature-branch");
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);

      expect(mockCore.info).toHaveBeenCalledWith("✅ Successfully checked out branch: feature-branch");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should handle git fetch errors", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("git fetch failed"));

      await runScript();

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryCall).toContain("Failed to Checkout PR Branch");
      expect(summaryCall).toContain("git fetch failed");
      expect(summaryCall).toContain("pull request has been closed");

      expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to checkout PR branch: git fetch failed");
    });

    it("should handle git checkout errors", async () => {
      mockExec.exec.mockResolvedValueOnce(0); // fetch succeeds
      mockExec.exec.mockRejectedValueOnce(new Error("git checkout failed"));

      await runScript();

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryCall).toContain("Failed to Checkout PR Branch");
      expect(summaryCall).toContain("git checkout failed");

      expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to checkout PR branch: git checkout failed");
    });
  });

  describe("comment events on PRs", () => {
    beforeEach(() => {
      mockContext.eventName = "issue_comment";
    });

    it("should checkout PR using gh pr checkout", async () => {
      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: issue_comment");
      expect(mockCore.info).toHaveBeenCalledWith("Pull Request #123");

      // Verify detailed context logging
      expect(mockCore.startGroup).toHaveBeenCalledWith("📋 PR Context Details");

      // Verify strategy logging
      expect(mockCore.startGroup).toHaveBeenCalledWith("🔄 Checkout Strategy");
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: gh pr checkout");

      expect(mockCore.info).toHaveBeenCalledWith("Checking out PR #123 using gh CLI");

      // Updated expectation: no env options passed, GH_TOKEN comes from step environment
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);

      expect(mockCore.info).toHaveBeenCalledWith("✅ Successfully checked out PR #123");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should handle gh pr checkout errors", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("gh pr checkout failed"));

      await runScript();

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryCall).toContain("Failed to Checkout PR Branch");
      expect(summaryCall).toContain("gh pr checkout failed");
      expect(summaryCall).toContain("pull request has been closed");

      expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to checkout PR branch: gh pr checkout failed");
    });

    it("should pass environment variables to gh command", async () => {
      // This test is no longer relevant since we don't pass env options explicitly
      // The GH_TOKEN is now set at the step level, not in the exec options
      // Keeping the test but updating to verify the call without env options
      process.env.CUSTOM_VAR = "custom-value";

      await runScript();

      // Verify exec is called without env options
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);

      delete process.env.CUSTOM_VAR;
    });
  });

  describe("no pull request context", () => {
    it("should skip checkout when no pull request context", async () => {
      mockContext.payload.pull_request = null;

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should skip checkout for push events", async () => {
      mockContext.eventName = "push";
      mockContext.payload = {};

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should skip checkout for issue events", async () => {
      mockContext.eventName = "issues";
      mockContext.payload = { issue: { number: 456 } };

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });
  });

  describe("different event types", () => {
    it("should handle pull_request_target event", async () => {
      mockContext.eventName = "pull_request_target";

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: pull_request_target");
      // pull_request_target uses gh pr checkout, not git
      // Updated expectation: no third argument (env options removed)
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);
    });

    it("should handle pull_request_review event", async () => {
      mockContext.eventName = "pull_request_review";

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: pull_request_review");
      // pull_request_review uses gh pr checkout, not git
      // Updated expectation: no third argument (env options removed)
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);
    });

    it("should handle pull_request_review_comment event", async () => {
      mockContext.eventName = "pull_request_review_comment";

      await runScript();

      // Updated expectation: no third argument (env options removed)
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);
    });
  });

  describe("error handling", () => {
    it("should handle non-Error exceptions", async () => {
      mockExec.exec.mockRejectedValueOnce("string error");

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to checkout PR branch: string error");
    });

    it("should handle errors with custom messages", async () => {
      const customError = new Error("Permission denied: unable to access repository");
      mockExec.exec.mockRejectedValueOnce(customError);

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to checkout PR branch: Permission denied: unable to access repository");
    });
  });

  describe("branch name variations", () => {
    it("should handle branches with slashes", async () => {
      mockContext.payload.pull_request.head.ref = "feature/new-feature";

      await runScript();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature/new-feature"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature/new-feature"]);
    });

    it("should handle branches with special characters", async () => {
      mockContext.payload.pull_request.head.ref = "fix-issue-#123";

      await runScript();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "fix-issue-#123"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "fix-issue-#123"]);
    });

    it("should handle very long branch names", async () => {
      const longBranchName = "feature/" + "x".repeat(200);
      mockContext.payload.pull_request.head.ref = longBranchName;

      await runScript();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", longBranchName]);
    });
  });

  describe("checkout output", () => {
    it("should set output to true on successful checkout (pull_request event)", async () => {
      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should set output to true on successful checkout (comment event)", async () => {
      mockContext.eventName = "issue_comment";

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should set output to false on checkout failure", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("checkout failed"));

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to checkout PR branch: checkout failed");
    });

    it("should set output to true when no PR context", async () => {
      mockContext.payload.pull_request = null;

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("fork PR detection and logging", () => {
    it("should detect and log fork PRs in pull_request_target events", async () => {
      mockContext.eventName = "pull_request_target";
      // Set up fork PR scenario
      mockContext.payload.pull_request.head.repo.full_name = "fork-owner/test-repo";
      mockContext.payload.pull_request.head.repo.owner.login = "fork-owner";

      await runScript();

      // Verify fork detection logging with reason
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: true (different repository names)");
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - gh pr checkout will fetch from fork repository");
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);
    });

    it("should detect fork using GitHub's fork flag", async () => {
      mockContext.eventName = "pull_request_target";
      // Set fork flag explicitly
      mockContext.payload.pull_request.head.repo.fork = true;
      mockContext.payload.pull_request.head.repo.full_name = "test-owner/test-repo"; // Same name but forked

      await runScript();

      // Verify fork detection using fork flag
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: true (head.repo.fork flag is true)");
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - gh pr checkout will fetch from fork repository");
    });

    it("should detect non-fork PRs in pull_request_target events", async () => {
      mockContext.eventName = "pull_request_target";
      // Same repo PR - ensure fork flag is false
      mockContext.payload.pull_request.head.repo.full_name = "test-owner/test-repo";
      mockContext.payload.pull_request.head.repo.fork = false;

      await runScript();

      // Verify non-fork detection
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: false (same repository)");
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fork PR detected"));
    });

    it("should detect deleted fork (null head repo)", async () => {
      mockContext.eventName = "pull_request_target";
      // Simulate deleted fork scenario
      delete mockContext.payload.pull_request.head.repo;

      await runScript();

      // Verify deleted fork detection
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Head repo information not available (repo may be deleted)");
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: true (head repository deleted (was likely a fork))");
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - gh pr checkout will fetch from fork repository");
    });

    it("should log detailed PR context with startGroup/endGroup", async () => {
      await runScript();

      // Verify group logging is used
      expect(mockCore.startGroup).toHaveBeenCalledWith("📋 PR Context Details");
      expect(mockCore.endGroup).toHaveBeenCalled();

      // Verify detailed context logging
      expect(mockCore.info).toHaveBeenCalledWith("Event type: pull_request");
      expect(mockCore.info).toHaveBeenCalledWith("PR number: 123");
      expect(mockCore.info).toHaveBeenCalledWith("Head ref: feature-branch");
      expect(mockCore.info).toHaveBeenCalledWith("Base ref: main");
    });

    it("should log checkout strategy for pull_request events", async () => {
      await runScript();

      expect(mockCore.startGroup).toHaveBeenCalledWith("🔄 Checkout Strategy");
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch + checkout");
      expect(mockCore.info).toHaveBeenCalledWith("Reason: pull_request event runs in merge commit context with PR branch available");
    });

    it("should log checkout strategy for pull_request_target events", async () => {
      mockContext.eventName = "pull_request_target";

      await runScript();

      expect(mockCore.startGroup).toHaveBeenCalledWith("🔄 Checkout Strategy");
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: gh pr checkout");
      expect(mockCore.info).toHaveBeenCalledWith("Reason: pull_request_target runs in base repo context; for fork PRs, head branch doesn't exist in origin");
    });

    it("should log current branch after successful gh pr checkout", async () => {
      mockContext.eventName = "issue_comment";

      // Mock the git branch command to return a branch name
      mockExec.exec.mockImplementation((cmd, args, options) => {
        if (cmd === "git" && args[0] === "branch" && args[1] === "--show-current") {
          if (options?.listeners?.stdout) {
            options.listeners.stdout(Buffer.from("feature-branch\n"));
          }
        }
        return Promise.resolve(0);
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Current branch: feature-branch");
    });
  });

  describe("enhanced error logging", () => {
    it("should log detailed error context on checkout failure", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("checkout failed"));

      await runScript();

      // Verify error group logging
      expect(mockCore.startGroup).toHaveBeenCalledWith("❌ Checkout Error Details");
      expect(mockCore.error).toHaveBeenCalledWith("Event type: pull_request");
      expect(mockCore.error).toHaveBeenCalledWith("PR number: 123");
      expect(mockCore.error).toHaveBeenCalledWith("Error message: checkout failed");
      expect(mockCore.error).toHaveBeenCalledWith("Attempted to check out: feature-branch");
    });

    it("should attempt to log git status on error", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("checkout failed")).mockResolvedValue(0); // Subsequent git commands succeed

      await runScript();

      // Verify diagnostic git commands were attempted
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["status"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["remote", "-v"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["branch", "--show-current"]);
    });

    it("should handle git diagnostic command failures gracefully", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("checkout failed")).mockRejectedValue(new Error("git command not available"));

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringMatching(/Could not retrieve git state/));
    });
  });
});
