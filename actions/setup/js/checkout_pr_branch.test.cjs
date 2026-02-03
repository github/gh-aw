import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

describe("checkout_pr_branch.cjs", () => {
  let mockCore;
  let mockExec;
  let mockContext;

  beforeEach(() => {
    // Mock core actions methods
    mockCore = {
      info: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
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
      payload: {
        pull_request: {
          number: 123,
          head: {
            ref: "feature-branch",
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
      expect(mockCore.info).toHaveBeenCalledWith("Checking out PR branch: feature-branch");

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch"]);
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
      expect(mockCore.info).toHaveBeenCalledWith("Checking out PR #123 using gh pr checkout");

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
});
