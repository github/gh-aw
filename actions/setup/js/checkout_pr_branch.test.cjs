import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
const { ERR_API } = require("./error_codes.cjs");
describe("checkout_pr_branch.cjs", () => {
  let mockCore;
  let mockExec;
  let mockContext;
  let mockGithub;

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
      exportVariable: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    // Mock exec
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
      // Default: repository is shallow (so --depth is preserved), and HEAD resolves
      // to a fixed commit for the checked-out-HEAD baseline lookup.
      getExecOutput: vi.fn().mockImplementation((_cmd, args) => {
        if (Array.isArray(args) && args.includes("HEAD^{commit}")) {
          return Promise.resolve({ stdout: "checked-out-head-sha\n", stderr: "", exitCode: 0 });
        }
        return Promise.resolve({ stdout: "true\n", stderr: "", exitCode: 0 });
      }),
    };

    // Mock context
    mockContext = {
      eventName: "pull_request",
      actor: "test-actor",
      sha: "abc123def456",
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
      payload: {
        repository: {
          fork: false,
        },
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

    // Mock GitHub API for fetchPRDetails (used in the else branch for non-fork PR events)
    mockGithub = {
      rest: {
        repos: {
          getCollaboratorPermissionLevel: vi.fn().mockResolvedValue({
            data: {
              permission: "write",
            },
          }),
        },
        users: {
          getByUsername: vi.fn().mockResolvedValue({
            data: {
              login: "test-actor",
            },
          }),
        },
        pulls: {
          get: vi.fn().mockResolvedValue({
            data: {
              state: "open",
              commits: 1,
              head: {
                ref: "feature-branch",
                sha: "head-sha-123",
                repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } },
              },
              base: {
                ref: "main",
                repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } },
              },
            },
          }),
        },
      },
    };
    global.github = mockGithub;

    process.env.GITHUB_TOKEN = "test-token";
    process.env.GITHUB_SERVER_URL = "https://github.com";
  });

  afterEach(() => {
    delete global.core;
    delete global.exec;
    delete global.context;
    delete global.github;
    delete process.env.GITHUB_TOKEN;
    delete process.env.GITHUB_SERVER_URL;
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
          getPromptPath: name => `/mock/prompts/${name}`,
          renderTemplateFromFile: (templatePath, context) => {
            const template = mockRequire("fs").readFileSync(templatePath, "utf8");
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
            if (!pullRequest.head?.repo) {
              return { isFork: true, reason: "head repository deleted (was likely a fork)" };
            }
            if (pullRequest.head.repo.full_name !== pullRequest.base?.repo?.full_name) {
              return { isFork: true, reason: "different repository names" };
            }
            return { isFork: false, reason: "same repository" };
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
      if (module === "./error_codes.cjs") {
        return require("./error_codes.cjs");
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
      // commits is undefined in mock payload, so defaults to 1; depth = 1+1 = 2
      expect(mockCore.info).toHaveBeenCalledWith("Fetching branch: feature-branch from origin (depth: 2 for 1 PR commit(s))");
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);

      expect(mockCore.info).toHaveBeenCalledWith("Checking out branch: feature-branch");
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);

      expect(mockCore.info).toHaveBeenCalledWith("✅ Successfully checked out branch: feature-branch");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should omit --depth when the repository is not shallow", async () => {
      mockExec.getExecOutput.mockResolvedValue({ stdout: "false\n", stderr: "", exitCode: 0 });

      const script = require("./checkout_pr_branch.cjs");
      await script.main();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should omit --depth for refs/pull fetch when the repository is not shallow", async () => {
      mockExec.getExecOutput.mockResolvedValue({ stdout: "false\n", stderr: "", exitCode: 0 });
      mockContext.eventName = "pull_request_target";

      const script = require("./checkout_pr_branch.cjs");
      await script.main();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head"]);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    describe("runtime checkout safety assertions", () => {
      it("should allow a same-repository PR when the runtime repository is itself a fork", async () => {
        mockContext.payload.repository.fork = true;

        await runScript();

        expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it("should fail when actor does not have write-or-higher permission", async () => {
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
          data: {
            permission: "triage",
          },
        });

        await runScript();

        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("requires write or higher"));
      });

      it("should allow checkout for Bot actor only after repository permission verification", async () => {
        mockContext.actor = "Copilot";
        mockContext.payload.sender = { login: "Copilot", type: "Bot" };

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "Copilot" }));
        expect(mockCore.info).toHaveBeenCalledWith("Runtime safety check passed for actor 'Copilot' with 'write' permission");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);
      });

      it("should fail closed when bot/app actor repository permission cannot be verified", async () => {
        mockContext.actor = "Copilot";
        mockContext.payload.sender = { login: "Copilot", type: "Bot" };
        const notAUserError = Object.assign(new Error("Copilot is not a user"), { status: 404 });
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(notAUserError);
        mockGithub.rest.users.getByUsername.mockRejectedValue(notAUserError);

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
        expect(mockGithub.rest.users.getByUsername).toHaveBeenCalledWith({ username: "Copilot" });
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("bot/app actor 'Copilot' repository permission could not be verified"));
      });

      it("should fail when collaborator API returns 404 for a regular non-collaborator user", async () => {
        mockContext.actor = "real-user";
        const notCollaboratorError = Object.assign(new Error("Not Found"), { status: 404 });
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(notCollaboratorError);
        mockGithub.rest.users.getByUsername.mockResolvedValue({
          data: {
            login: "real-user",
          },
        });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
        expect(mockGithub.rest.users.getByUsername).toHaveBeenCalledWith({ username: "real-user" });
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is not a collaborator"));
      });

      it("should fail when collaborator API returns a non-404 error", async () => {
        const serverError = Object.assign(new Error("Internal Server Error"), { status: 500 });
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(serverError);

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Internal Server Error"));
      });

      it.each(["workflow_dispatch", "issue_comment", "pull_request_review_comment"])("should reject bot actor without write-or-higher permission for %s events", async eventName => {
        mockContext.eventName = eventName;
        mockContext.actor = "third-party-bot[bot]";
        mockContext.payload.sender = { login: "third-party-bot[bot]", type: "Bot" };
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
          data: {
            permission: "read",
          },
        });
        if (eventName === "workflow_dispatch") {
          mockContext.payload = {
            repository: { fork: false },
            sender: { login: "third-party-bot[bot]", type: "Bot" },
            inputs: {
              aw_context: JSON.stringify({ item_type: "pull_request", item_number: 123 }),
            },
          };
        }

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "third-party-bot[bot]" }));
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]));
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("requires write or higher"));
      });
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

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: git fetch failed`);
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

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: git checkout failed`);
    });

    it("should use git fetch refs/pull for fork PR in pull_request event", async () => {
      // Set up fork PR: head repo is different from base repo
      mockContext.payload.pull_request.head.repo.full_name = "fork-owner/test-repo";
      mockContext.payload.pull_request.head.repo.owner.login = "fork-owner";
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          state: "open",
          commits: 1,
          head: {
            ref: "feature-branch",
            sha: "head-sha-123",
            repo: { full_name: "fork-owner/test-repo", owner: { login: "fork-owner" } },
          },
          base: {
            ref: "main",
            repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } },
          },
        },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: pull_request");

      // Verify fork is detected
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: true (different repository names)");
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching via refs/pull/N/head from origin");

      // Verify strategy is git fetch refs/pull + checkout
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch refs/pull + checkout");
      expect(mockCore.info).toHaveBeenCalledWith("Reason: pull_request event from fork repository; fetching via refs/pull/N/head");

      // Verify git fetch refs/pull/N/head is used
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "origin/pr-head"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_BRANCH", "feature-branch");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_REPO", "test-owner/test-repo");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_REPO", "fork-owner/test-repo");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_REF", "refs/remotes/origin/pr-head");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", "checked-out-head-sha");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_PR_NUMBER", "123");

      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should use git fetch for same-repo PR even when repo has fork flag", async () => {
      // A repo that is itself a fork has fork=true, but same-repo PRs
      // should still use fast git fetch, not gh pr checkout (#24208)
      mockContext.payload.pull_request.head.repo.fork = true;

      await runScript();

      // Verify NOT detected as fork (same full_name)
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: false (same repository)");

      // Verify git fetch + checkout is used (fast path)
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should use git fetch for non-fork pull_request event", async () => {
      // Default mock context is non-fork (same repo)

      await runScript();

      // Verify non-fork detection
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: false (same repository)");

      // Verify git fetch + checkout is used for non-fork
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature-branch"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);
    });
  });

  describe("comment events on PRs", () => {
    beforeEach(() => {
      mockContext.eventName = "issue_comment";
    });

    it("should checkout PR using git fetch refs/pull", async () => {
      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: issue_comment");
      expect(mockCore.info).toHaveBeenCalledWith("Pull Request #123");

      // Verify detailed context logging
      expect(mockCore.startGroup).toHaveBeenCalledWith("📋 PR Context Details");

      // Verify strategy logging
      expect(mockCore.startGroup).toHaveBeenCalledWith("🔄 Checkout Strategy");
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch refs/pull + checkout");

      // Verify git fetch refs/pull/N/head and checkout
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "origin/pr-head"]);

      expect(mockCore.info).toHaveBeenCalledWith("✅ Successfully checked out PR #123");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should handle git fetch errors for PR ref", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("git fetch failed"));

      await runScript();

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryCall).toContain("Failed to Checkout PR Branch");
      expect(summaryCall).toContain("git fetch failed");
      expect(summaryCall).toContain("pull request has been closed");

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: git fetch failed`);
    });

    it("should resolve fork status from API when payload has minimal PR (no head/base)", async () => {
      // Simulate issue_comment with no pull_request in payload, only issue.pull_request
      mockContext.payload.pull_request = null;
      mockContext.payload.issue = {
        number: 456,
        state: "open",
        pull_request: { url: "https://api.github.com/repos/test-owner/test-repo/pulls/456" },
      };
      // fetchPRDetails returns full PR data with same-repo (non-fork)
      mockGithub.rest.pulls.get.mockResolvedValueOnce({
        data: {
          state: "open",
          commits: 3,
          head: { ref: "my-feature", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
          base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        },
      });

      await runScript();

      // Fork status should be "unknown" initially (minimal PR object)
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: unknown (head/base repo details not available in event payload)");
      // After API call, fork status should be resolved
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR (from API): false (same repository)");
      // Should NOT emit fork warning for a non-fork PR
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fork PR detected"));
      // Should successfully checkout
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/456/head:refs/remotes/origin/pr-head", "--depth=4"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "my-feature", "origin/pr-head"]);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should detect fork PR from API when payload has minimal PR", async () => {
      // Simulate issue_comment with no pull_request in payload
      mockContext.payload.pull_request = null;
      mockContext.payload.issue = {
        number: 789,
        state: "open",
        pull_request: { url: "https://api.github.com/repos/test-owner/test-repo/pulls/789" },
      };
      // fetchPRDetails returns full PR data from a fork
      mockGithub.rest.pulls.get.mockResolvedValueOnce({
        data: {
          state: "open",
          commits: 2,
          head: { ref: "fork-feature", repo: { full_name: "fork-owner/test-repo", owner: { login: "fork-owner" } } },
          base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        },
      });

      await runScript();

      // Fork status should be "unknown" initially, then resolved from API
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: unknown (head/base repo details not available in event payload)");
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR (from API): true (different repository names)");
      // Should emit fork warning
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching via refs/pull/N/head from origin");
      // Should successfully checkout
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/789/head:refs/remotes/origin/pr-head", "--depth=3"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "fork-feature", "origin/pr-head"]);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
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

    it("should skip checkout for the native 'fork' webhook event without reaching assertTrustedCheckoutRuntime, regardless of repository.fork", async () => {
      // The GitHub-native `fork` event (on: repository forked / on: {fork: null}) fires
      // in the base/upstream repository when someone forks it. It carries no PR context
      // (no `pull_request` payload, and it is not `workflow_dispatch`), so RS-05a's
      // fork-runtime rejection in assertTrustedCheckoutRuntime() is never reached for it.
      // This holds independent of `payload.repository.fork`, which is unrelated to this event.
      mockContext.eventName = "fork";
      mockContext.payload = { repository: { fork: true }, forkee: { full_name: "some-user/test-repo" } };

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("workflow_dispatch events with aw_context", () => {
    beforeEach(() => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {
        repository: { fork: false },
        inputs: {
          aw_context: JSON.stringify({ item_type: "pull_request", item_number: 123 }),
        },
      };
    });

    it("should checkout PR using git fetch refs/pull when aw_context has item_type pull_request", async () => {
      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Detected workflow_dispatch event for PR #123 via aw_context, will fetch PR ref");
      expect(mockCore.info).toHaveBeenCalledWith("Event: workflow_dispatch");
      expect(mockCore.info).toHaveBeenCalledWith("Pull Request #123");

      // workflow_dispatch uses git fetch refs/pull + checkout (not the fast pull_request path)
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "origin/pr-head"]);

      // fetchPRDetails must be called with the correct PR number to resolve head ref / commit count
      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith(expect.objectContaining({ pull_number: 123 }));

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should refuse PR replay when the runtime repository is a fork", async () => {
      mockContext.payload.repository.fork = true;

      await runScript();

      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]));
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Refusing PR checkout in forked repository runtime context"));
    });

    it("should refuse PR replay when the workflow_dispatch payload has no repository data", async () => {
      delete mockContext.payload.repository;

      await runScript();

      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]));
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unable to determine repository fork status for workflow_dispatch"));
    });

    it("should skip checkout when aw_context item_type is not pull_request", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "issue", item_number: 42 });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should skip checkout when workflow_dispatch has no aw_context input", async () => {
      mockContext.payload.inputs = {};

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should skip checkout when workflow_dispatch has no inputs at all", async () => {
      mockContext.payload.inputs = undefined;

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should warn and skip checkout when aw_context is invalid JSON", async () => {
      mockContext.payload.inputs.aw_context = "not-valid-json{";

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to parse aw_context:"));
      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should skip checkout when aw_context pull_request has no item_number", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request" });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should skip checkout when aw_context item_number is a non-numeric string", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request", item_number: "abc" });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should skip checkout when aw_context item_number is zero", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request", item_number: 0 });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should skip checkout when aw_context item_number is a non-integer float", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request", item_number: 1.5 });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should checkout PR when aw_context repo matches current repository", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({
        item_type: "pull_request",
        item_number: 123,
        repo: "test-owner/test-repo",
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Detected workflow_dispatch event for PR #123 via aw_context, will fetch PR ref");
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockCore.warning).not.toHaveBeenCalled();
    });

    it("should warn and skip checkout when aw_context repo does not match current repository", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({
        item_type: "pull_request",
        item_number: 123,
        repo: "other-owner/other-repo",
      });

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Cross-repository workflow_dispatch is not supported"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("other-owner/other-repo"));
      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should set output to true on successful workflow_dispatch PR checkout", async () => {
      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("different event types", () => {
    it("should handle pull_request_target event", async () => {
      mockContext.eventName = "pull_request_target";

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: pull_request_target");
      // pull_request_target uses git fetch refs/pull + checkout
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "origin/pr-head"]);
    });

    it("should handle pull_request_review event", async () => {
      mockContext.eventName = "pull_request_review";

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: pull_request_review");
      // pull_request_review uses git fetch refs/pull + checkout
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "origin/pr-head"]);
    });

    it("should handle pull_request_review_comment event", async () => {
      mockContext.eventName = "pull_request_review_comment";

      await runScript();

      // pull_request_review_comment uses git fetch refs/pull + checkout
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "origin/pr-head"]);
    });
  });

  describe("risk matrix: forked runtime repository scope (RS-05a)", () => {
    // Formal coverage for the risk matrix from PR #63011: the repository.fork
    // guard in assertTrustedCheckoutRuntime() MUST be scoped to workflow_dispatch
    // PR replays only. Every other PR-capable trigger MUST remain unaffected by
    // a structurally forked runtime repository.
    it.each(["pull_request_target", "pull_request_review", "pull_request_review_comment", "issue_comment"])("should allow checkout for %s events even when the runtime repository is a fork", async eventName => {
      mockContext.eventName = eventName;
      mockContext.payload.repository = { fork: true };

      await runScript();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "origin/pr-head"]);
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should skip checkout without evaluating the fork guard when workflow_dispatch has no PR context", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = { repository: { fork: true }, inputs: {} };

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("No pull request context available, skipping checkout");
      expect(mockExec.exec).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("error handling", () => {
    it("should handle non-Error exceptions", async () => {
      mockExec.exec.mockRejectedValueOnce("string error");

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: string error`);
    });

    it("should handle errors with custom messages", async () => {
      const customError = new Error("Permission denied: unable to access repository");
      mockExec.exec.mockRejectedValueOnce(customError);

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: Permission denied: unable to access repository`);
    });
  });

  describe("branch name variations", () => {
    it("should handle branches with slashes", async () => {
      mockContext.payload.pull_request.head.ref = "feature/new-feature";

      await runScript();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "feature/new-feature", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "feature/new-feature"]);
    });

    it("should handle branches with special characters", async () => {
      mockContext.payload.pull_request.head.ref = "fix-issue-#123";

      await runScript();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "fix-issue-#123", "--depth=2"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "fix-issue-#123"]);
    });

    it("should handle very long branch names", async () => {
      const longBranchName = "feature/" + "x".repeat(200);
      mockContext.payload.pull_request.head.ref = longBranchName;

      await runScript();

      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", longBranchName, "--depth=2"]);
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
      // Verify git operations were used (not gh CLI)
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
    });

    it("should set output to false on checkout failure", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("checkout failed"));

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: checkout failed`);
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
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching via refs/pull/N/head from origin");
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
    });

    it("should NOT detect fork when repo has fork flag but same full_name", async () => {
      mockContext.eventName = "pull_request_target";
      // A repo that is itself a fork has fork=true, but same full_name
      // means it's NOT a cross-repo fork PR (#24208)
      mockContext.payload.pull_request.head.repo.fork = true;
      mockContext.payload.pull_request.head.repo.full_name = "test-owner/test-repo";

      await runScript();

      // Same full_name = not a fork PR
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: false (same repository)");
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("Fork PR detected"));
      // Still uses git fetch refs/pull because pull_request_target always does
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "origin", "+refs/pull/123/head:refs/remotes/origin/pr-head", "--depth=2"]);
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

      // fetchPRDetails returns full PR data with deleted head repo
      mockGithub.rest.pulls.get.mockResolvedValueOnce({
        data: {
          state: "open",
          commits: 1,
          head: { ref: "feature-branch", repo: null },
          base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        },
      });

      await runScript();

      // Verify deleted fork detection
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Head repo information not available (repo may be deleted)");
      // logPRContext reports unknown because head.repo is missing in the payload
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: unknown (head/base repo details not available in event payload)");
      // After API call, fork status is resolved via detectForkPR
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR (from API): true (head repository deleted (was likely a fork))");
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching via refs/pull/N/head from origin");
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
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch refs/pull + checkout");
      expect(mockCore.info).toHaveBeenCalledWith("Reason: pull_request_target runs in base repo context; fetching via refs/pull/N/head");
    });

    it("should log current branch after successful refs/pull checkout", async () => {
      mockContext.eventName = "issue_comment";

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

  describe("closed pull request handling", () => {
    it("should treat checkout failure as warning for closed PR (pull_request event)", async () => {
      mockContext.payload.pull_request.state = "closed";
      mockExec.exec.mockRejectedValueOnce(new Error("git fetch failed - branch deleted"));

      await runScript();

      // Should log as warning, not error
      expect(mockCore.startGroup).toHaveBeenCalledWith("⚠️ Closed PR Checkout Warning");
      expect(mockCore.warning).toHaveBeenCalledWith("Event type: pull_request");
      expect(mockCore.warning).toHaveBeenCalledWith("PR number: 123");
      expect(mockCore.warning).toHaveBeenCalledWith("PR state: closed");
      expect(mockCore.warning).toHaveBeenCalledWith("Checkout failed (expected for closed PR): git fetch failed - branch deleted");
      expect(mockCore.warning).toHaveBeenCalledWith("Branch likely deleted: feature-branch");
      expect(mockCore.warning).toHaveBeenCalledWith("This is expected behavior when a PR is closed - the branch may have been deleted.");

      // Should write summary with warning message
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryCall).toContain("⚠️ Closed Pull Request");
      expect(summaryCall).toContain("Pull request #123 is closed");
      expect(summaryCall).toContain("This is not an error");

      // Should set output to true (success)
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");

      // Should NOT fail the step
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should treat checkout failure as warning for closed PR (refs/pull checkout)", async () => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.pull_request.state = "closed";
      mockExec.exec.mockRejectedValueOnce(new Error("git fetch failed - PR closed"));

      await runScript();

      // Should log as warning, not error
      expect(mockCore.startGroup).toHaveBeenCalledWith("⚠️ Closed PR Checkout Warning");
      expect(mockCore.warning).toHaveBeenCalledWith("Checkout failed (expected for closed PR): git fetch failed - PR closed");

      // Should NOT fail the step
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
    });

    it("should still fail for open PR with checkout error", async () => {
      // PR is open (default state in mockContext)
      mockContext.payload.pull_request.state = "open";
      mockExec.exec.mockRejectedValueOnce(new Error("network error"));

      await runScript();

      // Should log as error
      expect(mockCore.startGroup).toHaveBeenCalledWith("❌ Checkout Error Details");
      expect(mockCore.error).toHaveBeenCalledWith("Event type: pull_request");

      // Should fail the step
      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: network error`);
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
    });

    it("should log closed PR info before checkout attempt", async () => {
      mockContext.payload.pull_request.state = "closed";

      await runScript();

      // Should log that PR is closed
      expect(mockCore.info).toHaveBeenCalledWith("⚠️ Pull request is closed");

      // If checkout succeeds (branch still exists), should still succeed
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should handle closed PR without head ref", async () => {
      mockContext.payload.pull_request.state = "closed";
      delete mockContext.payload.pull_request.head;
      mockExec.exec.mockRejectedValueOnce(new Error("no branch info"));

      await runScript();

      // Should treat as warning
      expect(mockCore.startGroup).toHaveBeenCalledWith("⚠️ Closed PR Checkout Warning");

      // Should not try to log branch name
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringMatching(/Branch likely deleted:/));

      // Should NOT fail the step
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
    });

    it("should include PR state in context logging", async () => {
      mockContext.payload.pull_request.state = "closed";

      await runScript();

      // State is already logged in logPRContext
      expect(mockCore.info).toHaveBeenCalledWith("PR state: closed");
    });
  });

  describe("race condition - PR merged after workflow trigger", () => {
    it("should treat checkout failure as warning when PR was merged after workflow triggered", async () => {
      // PR was "open" in webhook payload, but branch was deleted after merge
      mockContext.payload.pull_request.state = "open";
      mockExec.exec.mockRejectedValueOnce(new Error("fatal: couldn't find remote ref feature-branch"));
      // Non-fork pull_request uses fast path (no fetchPRDetails call).
      // Only the error handler re-check calls pulls.get → returns closed.
      mockGithub.rest.pulls.get.mockResolvedValueOnce({
        data: {
          state: "closed",
          commits: 1,
          head: { ref: "feature-branch", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
          base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        },
      });

      await runScript();

      // Should log info about the state change
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("is now closed"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("was 'open' in webhook payload"));

      // Should log as warning, not error
      expect(mockCore.startGroup).toHaveBeenCalledWith("⚠️ Closed PR Checkout Warning");
      expect(mockCore.warning).toHaveBeenCalledWith("Event type: pull_request");
      expect(mockCore.warning).toHaveBeenCalledWith("PR number: 123");
      expect(mockCore.warning).toHaveBeenCalledWith("PR state: closed (merged after workflow was triggered)");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("couldn't find remote ref"));
      expect(mockCore.warning).toHaveBeenCalledWith("Branch likely deleted: feature-branch");
      expect(mockCore.warning).toHaveBeenCalledWith("This is expected behavior when a PR is closed - the branch may have been deleted.");

      // Should write summary with the "merged after" message
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryCall).toContain("⚠️ Closed Pull Request");
      expect(summaryCall).toContain("was merged after this workflow was triggered");
      expect(summaryCall).toContain("This is not an error");

      // Should set output to true (handled gracefully)
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");

      // Should NOT fail the step
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should still fail when PR is still open and checkout fails", async () => {
      mockContext.payload.pull_request.state = "open";
      mockExec.exec.mockRejectedValueOnce(new Error("network error"));
      // Non-fork pull_request: error handler re-check confirms still open (default mock)

      await runScript();

      // Should log as error (not a closed PR)
      expect(mockCore.startGroup).toHaveBeenCalledWith("❌ Checkout Error Details");
      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: network error`);
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.startGroup).not.toHaveBeenCalledWith("⚠️ Closed PR Checkout Warning");
    });

    it("should still fail when API re-check itself fails", async () => {
      mockContext.payload.pull_request.state = "open";
      mockExec.exec.mockRejectedValueOnce(new Error("fetch failed"));
      // Non-fork pull_request: error handler re-check fails
      const apiError = new Error("API rate limited");
      apiError.status = 429;
      mockGithub.rest.pulls.get.mockRejectedValueOnce(apiError);

      await runScript();

      // Should warn about the failed API check with HTTP status
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not fetch current PR state"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("HTTP 429"));

      // Cannot confirm PR is closed, so should still fail
      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: fetch failed`);
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
    });

    it("should include HTTP status code in API re-check failure warning", async () => {
      mockContext.payload.pull_request.state = "open";
      mockExec.exec.mockRejectedValueOnce(new Error("fetch failed"));
      // Non-fork pull_request: error handler re-check fails with 404
      const apiError = new Error("Not Found");
      apiError.status = 404;
      mockGithub.rest.pulls.get.mockRejectedValueOnce(apiError);

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("HTTP 404"));
    });

    it("should omit HTTP status suffix when API error has no status code", async () => {
      mockContext.payload.pull_request.state = "open";
      mockExec.exec.mockRejectedValueOnce(new Error("fetch failed"));
      // Non-fork pull_request: error handler re-check fails without status
      mockGithub.rest.pulls.get.mockRejectedValueOnce(new Error("network timeout"));

      await runScript();

      const warningCalls = mockCore.warning.mock.calls.map(c => c[0]);
      const apiWarning = warningCalls.find(w => typeof w === "string" && w.includes("Could not fetch current PR state"));
      expect(apiWarning).toBeDefined();
      expect(apiWarning).not.toMatch(/HTTP \d+/);
      expect(apiWarning).toContain("network timeout");
    });

    it("should call the GitHub API with the correct PR number and repo", async () => {
      mockContext.payload.pull_request.state = "open";
      mockExec.exec.mockRejectedValueOnce(new Error("fetch failed"));
      // Non-fork pull_request: error handler re-check returns closed
      mockGithub.rest.pulls.get.mockResolvedValueOnce({
        data: {
          state: "closed",
          commits: 1,
          head: { ref: "feature-branch", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
          base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        },
      });

      await runScript();

      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        pull_number: 123,
      });
    });

    it("should handle race condition for refs/pull checkout path (issue_comment event)", async () => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.pull_request.state = "open";
      // First pulls.get (fetchPRDetails): succeeds
      // Second pulls.get (re-check): shows PR was merged
      const fullPRData = {
        state: "open",
        commits: 1,
        head: { ref: "feature-branch", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
      };
      const closedPRData = { ...fullPRData, state: "closed" };
      mockGithub.rest.pulls.get.mockResolvedValueOnce({ data: fullPRData }).mockResolvedValueOnce({ data: closedPRData });
      mockExec.exec.mockRejectedValueOnce(new Error("git fetch failed - PR closed"));

      await runScript();

      // Should treat as warning, not error
      expect(mockCore.startGroup).toHaveBeenCalledWith("⚠️ Closed PR Checkout Warning");
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });
});
