import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
const { ERR_API } = require("./error_codes.cjs");
describe("checkout_pr_branch.cjs", () => {
  let mockCore;
  let mockExec;
  let mockFetch;
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

    mockFetch = vi.fn().mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });
    // Mock exec
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
      // Default: repository is shallow (so --depth is preserved), and HEAD resolves
      // to a fixed commit for the checked-out-HEAD baseline lookup.
      getExecOutput: vi.fn().mockImplementation((_cmd, args, options) => {
        if (args[0] === "fetch") {
          return mockFetch(_cmd, args, options);
        }
        if (Array.isArray(args) && args.includes("HEAD^{commit}")) {
          return Promise.resolve({ stdout: "1111111111111111111111111111111111111111\n", stderr: "", exitCode: 0 });
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
        comment: { created_at: "2026-10-27T12:00:00Z", commit_id: "1111111111111111111111111111111111111111" },
        review: { commit_id: "1111111111111111111111111111111111111111" },
        repository: {
          fork: false,
        },
        pull_request: {
          number: 123,
          state: "open",
          head: {
            ref: "feature-branch",
            sha: "1111111111111111111111111111111111111111",
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
              updated_at: "2026-10-27T12:00:00Z",
              commits: 1,
              head: {
                ref: "feature-branch",
                sha: "1111111111111111111111111111111111111111",
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
    delete process.env.GH_AW_ALLOWED_BOTS;
  });

  afterEach(() => {
    delete global.core;
    delete global.exec;
    delete global.context;
    delete global.github;
    delete process.env.GITHUB_TOKEN;
    delete process.env.GITHUB_SERVER_URL;
    delete process.env.GH_AW_ALLOWED_BOTS;
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
      if (module === "./diagnostic_sanitization.cjs") {
        return require("./diagnostic_sanitization.cjs");
      }
      if (module === "./check_permissions_utils.cjs") {
        return require("./check_permissions_utils.cjs");
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

  describe("immutable revision binding", () => {
    const sha = "1111111111111111111111111111111111111111";
    const newerSha = "2222222222222222222222222222222222222222";
    const expectRejectedBeforeCheckout = () => {
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).toHaveBeenCalled();
      expect(mockCore.exportVariable).not.toHaveBeenCalled();
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
    };

    it("fetches the event commit rather than a mutable pull ref", async () => {
      mockFetch.mockImplementation(async () => {
        mockContext.payload.pull_request.head.sha = newerSha;
        return { exitCode: 0, stdout: "", stderr: "" };
      });
      await runScript();
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", `+${sha}:refs/remotes/origin/pr-head`, "--depth=2"], expect.anything());
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", sha);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it.each([undefined, null, "", "main", "abc123", "-option", {}, "g".repeat(40)])("rejects invalid PR event head SHA %j", async invalidSha => {
      mockContext.payload.pull_request.head.sha = invalidSha;
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it.each(["pull_request_review", "pull_request_review_comment"])("rejects %s without a recorded commit", async eventName => {
      mockContext.eventName = eventName;
      delete mockContext.payload[eventName === "pull_request_review" ? "review" : "comment"].commit_id;
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it.each(["pull_request_review", "pull_request_review_comment"])("rejects a stale %s even if the PR payload has the current head", async eventName => {
      mockContext.eventName = eventName;
      mockContext.payload[eventName === "pull_request_review" ? "review" : "comment"].commit_id = newerSha;
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("PR head changed since the triggering revision"));
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it.each([
      ["pull_request", "labeled"],
      ["pull_request", "unlabeled"],
      ["pull_request_target", "labeled"],
      ["pull_request_target", "unlabeled"],
    ])("rejects stale %s:%s revision evidence", async (eventName, action) => {
      mockContext.eventName = eventName;
      mockContext.payload.action = action;
      const current = (await mockGithub.rest.pulls.get()).data;
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...current, head: { ...current.head, sha: newerSha } } });
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("PR head changed since the triggering revision"));
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it.each(["fork", "closed same-repository"])("accepts an ordinary %s PR event revision even when the API head moved", async checkoutPath => {
      mockContext.payload.action = "synchronize";
      mockContext.payload.pull_request.commits = 10;
      if (checkoutPath === "fork") {
        mockContext.payload.pull_request.head.repo.full_name = "fork-owner/test-repo";
      } else {
        mockContext.payload.pull_request.state = "closed";
      }
      const current = (await mockGithub.rest.pulls.get()).data;
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...current, commits: 1, head: { ...current.head, sha: newerSha } } });
      await runScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", `+${sha}:refs/remotes/origin/pr-head`, "--depth=11"], expect.anything());
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", sha);
    });

    it.each(["pull_request", "pull_request_review", "pull_request_review_comment", "workflow_dispatch", "issue_comment"])("normalizes hexadecimal SHA case for %s", async eventName => {
      const canonicalSha = "abcdef0123456789abcdef0123456789abcdef01";
      mockContext.eventName = eventName;
      mockContext.payload.pull_request.head.sha = canonicalSha.toUpperCase();
      mockContext.payload.review.commit_id = canonicalSha.toUpperCase();
      mockContext.payload.comment.commit_id = canonicalSha.toUpperCase();
      const current = (await mockGithub.rest.pulls.get()).data;
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...current, head: { ...current.head, sha: eventName === "issue_comment" ? canonicalSha.toUpperCase() : canonicalSha } } });
      if (eventName === "workflow_dispatch") {
        mockContext.payload = {
          repository: { fork: false },
          inputs: { aw_context: JSON.stringify({ item_type: "pull_request", item_number: 123, pr_head_sha: canonicalSha.toUpperCase() }) },
        };
      }
      const original = mockExec.getExecOutput.getMockImplementation();
      mockExec.getExecOutput.mockImplementation((cmd, args, options) => (args.includes("HEAD^{commit}") ? { exitCode: 0, stdout: `${canonicalSha}\n`, stderr: "" } : original(cmd, args, options)));
      await runScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockFetch).toHaveBeenCalledWith("git", ["fetch", "origin", `+${canonicalSha}:refs/remotes/origin/pr-head`, "--depth=2"], expect.anything());
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", canonicalSha);
    });

    it.each(["created", "edited"])("reports issue_comment:%s staleness without claiming revision approval", async action => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.action = action;
      mockContext.payload.comment.created_at = "2026-10-27T10:00:00Z";
      mockContext.payload.comment.updated_at = "2026-10-27T11:00:00Z";
      await runScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.warning).toHaveBeenCalledWith("PR was updated after the triggering comment; selecting the current head, not an approved revision.");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", sha);
    });

    it("uses the edit timestamp for a new command in an existing comment", async () => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.action = "edited";
      mockContext.payload.comment.created_at = "2026-10-26T10:00:00Z";
      mockContext.payload.comment.updated_at = "2026-10-27T12:00:00Z";
      await runScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("do not prove approval"));
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", sha);
    });

    it.each([undefined, "", "not-a-date", 123])("rejects unavailable comment freshness evidence %j", async timestamp => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.comment = { created_at: timestamp };
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it("rejects unavailable PR update evidence", async () => {
      mockContext.eventName = "issue_comment";
      const current = (await mockGithub.rest.pulls.get()).data;
      delete current.updated_at;
      mockGithub.rest.pulls.get.mockResolvedValue({ data: current });
      await runScript();
      expectRejectedBeforeCheckout();
    });

    it("rejects a head change between the API read and fetch", async () => {
      mockContext.eventName = "issue_comment";
      const current = (await mockGithub.rest.pulls.get()).data;
      mockFetch.mockImplementation(async () => {
        mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...current, head: { ...current.head, sha: newerSha } } });
        return { exitCode: 0, stdout: "", stderr: "" };
      });
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", `+${sha}:refs/remotes/origin/pr-head`, "--depth=2"], expect.anything());
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("PR head changed while fetching"));
    });

    it.each([0, 1])("fails closed when the PR API fails at read %s", async successfulReads => {
      mockContext.eventName = "issue_comment";
      const current = (await mockGithub.rest.pulls.get()).data;
      mockGithub.rest.pulls.get.mockReset();
      if (successfulReads) mockGithub.rest.pulls.get.mockResolvedValueOnce({ data: current });
      mockGithub.rest.pulls.get.mockRejectedValue(new Error("PR API unavailable"));
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("PR API unavailable"));
    });

    it.each([newerSha, "", "not-a-sha"])("fails rather than exporting a mismatched checked-out commit %j", async actualSha => {
      const original = mockExec.getExecOutput.getMockImplementation();
      mockExec.getExecOutput.mockImplementation((cmd, args, options) => (args.includes("HEAD^{commit}") ? { exitCode: 0, stdout: actualSha, stderr: "" } : original(cmd, args, options)));
      await runScript();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.exportVariable).not.toHaveBeenCalled();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("checked-out HEAD does not match expected commit"));
    });

    it("fails closed when HEAD cannot be resolved", async () => {
      const original = mockExec.getExecOutput.getMockImplementation();
      mockExec.getExecOutput.mockImplementation((cmd, args, options) => (args.includes("HEAD^{commit}") ? { exitCode: 128, stdout: "", stderr: "unavailable commit" } : original(cmd, args, options)));
      await runScript();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("checked-out HEAD does not match expected commit"));
      expect(mockCore.exportVariable).not.toHaveBeenCalled();
    });

    it.each([
      { command_name: "test" },
      { trigger_label: "test" },
      { command_name: "test", pr_head_sha: "main" },
      { command_name: "test", event_type: "pull_request_review" },
      { command_name: "test", event_type: "issue_comment" },
      { pr_head_sha: newerSha },
      { pr_head_sha: null },
      { pr_head_sha: 0 },
      { pr_head_sha: false },
    ])("rejects dispatch without valid required revision context: %j", async extra => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = { repository: { fork: false }, inputs: { aw_context: JSON.stringify({ item_type: "pull_request", item_number: 123, ...extra }) } };
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it("preserves explicit latest-head/non-approval semantics for dispatched issue comments", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {
        repository: { fork: false },
        inputs: { aw_context: JSON.stringify({ item_type: "pull_request", item_number: 123, event_type: "issue_comment", command_name: "test", comment_updated_at: "2026-10-27T12:00:00Z", pr_head_sha: "" }) },
      };
      await runScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("do not prove approval"));
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", sha);
    });

    it.each([null, [], 123, "string", true])("rejects non-object dispatch context %j", async awContext => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = { inputs: { aw_context: JSON.stringify(awContext) } };
      await runScript();
      expectRejectedBeforeCheckout();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("aw_context must be an object"));
    });
  });

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
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch SHA + checkout");

      // Verify actual checkout commands
      // commits is undefined in mock payload, so defaults to 1; depth = 1+1 = 2
      expect(mockCore.info).toHaveBeenCalledWith("Fetching PR #123 commit 1111111111111111111111111111111111111111 (depth: 2 for 1 PR commit(s))");
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });

      expect(mockCore.info).toHaveBeenCalledWith("Checking out branch: feature-branch");
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);

      expect(mockCore.info).toHaveBeenCalledWith("✅ Successfully checked out branch: feature-branch");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should omit --depth when the repository is not shallow", async () => {
      const original = mockExec.getExecOutput.getMockImplementation();
      mockExec.getExecOutput.mockImplementation((cmd, args, options) => (args.includes("--is-shallow-repository") ? { stdout: "false\n", stderr: "", exitCode: 0 } : original(cmd, args, options)));

      const script = require("./checkout_pr_branch.cjs");
      await script.main();

      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], expect.anything());
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should omit --depth for refs/pull fetch when the repository is not shallow", async () => {
      const original = mockExec.getExecOutput.getMockImplementation();
      mockExec.getExecOutput.mockImplementation((cmd, args, options) => (args.includes("--is-shallow-repository") ? { stdout: "false\n", stderr: "", exitCode: 0 } : original(cmd, args, options)));
      mockContext.eventName = "pull_request_target";

      const script = require("./checkout_pr_branch.cjs");
      await script.main();

      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head"], { silent: true, ignoreReturnCode: true });
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    describe("runtime checkout safety assertions", () => {
      it("should allow a same-repository PR when the runtime repository is itself a fork", async () => {
        mockContext.payload.repository.fork = true;

        await runScript();

        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
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

        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], expect.anything());
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
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
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
        expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
      });

      it.each([null, undefined])("should preserve collaborator fallback when the bot event repository is %s", async repository => {
        mockContext.actor = "automation[bot]";
        mockContext.payload.sender = { login: "automation[bot]", type: "Bot" };
        mockContext.payload.action = "opened";
        mockContext.payload.repository = repository;

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "automation[bot]" }));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(["pull_request", "pull_request_target"])("should allow a same-repository GitHub App PR on %s despite collaborator permission 'none'", async eventName => {
        mockContext.eventName = eventName;
        mockContext.actor = "automation[bot]";
        mockContext.payload.sender = { login: "automation[bot]", type: "Bot" };
        mockContext.payload.action = "opened";
        mockContext.payload.repository.id = 42;
        mockContext.payload.pull_request.head.repo.id = 42;
        mockContext.payload.pull_request.base.repo.id = 42;
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", expect.arrayContaining(["fetch", "origin"]), { silent: true, ignoreReturnCode: true });
      });

      it.each([
        ["pull_request", "synchronize"],
        ["pull_request", "ready_for_review"],
        ["pull_request", "reopened"],
        ["pull_request_target", "ready_for_review"],
        ["pull_request_target", "reopened"],
      ])("should allow a same-repository GitHub App PR on %s with action %s", async (eventName, action) => {
        mockContext.eventName = eventName;
        mockContext.actor = "automation[bot]";
        mockContext.payload.sender = { login: "automation[bot]", type: "Bot" };
        mockContext.payload.action = action;
        mockContext.payload.repository.id = 42;
        mockContext.payload.pull_request.head.repo.id = 42;
        mockContext.payload.pull_request.base.repo.id = 42;
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", expect.arrayContaining(["fetch", "origin"]), { silent: true, ignoreReturnCode: true });
      });

      it.each([
        ["forked PR", 43, 42, 42, "automation[bot]", "Bot", "opened"],
        ["missing runtime repository ID", 42, undefined, 42, "automation[bot]", "Bot", "opened"],
        ["different sender", 42, 42, 42, "other[bot]", "Bot", "opened"],
        ["non-bot sender", 42, 42, 42, "automation[bot]", "User", "opened"],
        ["untrusted PR action", 42, 42, 42, "automation[bot]", "Bot", "edited"],
        ["missing base repository ID", 42, 42, undefined, "automation[bot]", "Bot", "opened"],
        ["mismatched base repository ID", 42, 42, 43, "automation[bot]", "Bot", "opened"],
        ["forked PR marked ready for review", 43, 42, 42, "automation[bot]", "Bot", "ready_for_review"],
        ["reopened forked PR", 43, 42, 42, "automation[bot]", "Bot", "reopened"],
        ["labeled forked PR", 43, 42, 42, "automation[bot]", "Bot", "labeled"],
        ["ready_for_review event with mismatched base", 42, 42, 43, "automation[bot]", "Bot", "ready_for_review"],
        ["reopened event with mismatched base", 42, 42, 43, "automation[bot]", "Bot", "reopened"],
        ["labeled event with mismatched base", 42, 42, 43, "automation[bot]", "Bot", "labeled"],
        ["ready_for_review event with different sender", 42, 42, 42, "other[bot]", "Bot", "ready_for_review"],
        ["reopened event with different sender", 42, 42, 42, "other[bot]", "Bot", "reopened"],
        ["labeled event with different sender", 42, 42, 42, "other[bot]", "Bot", "labeled"],
        ["ready_for_review event with non-bot sender", 42, 42, 42, "automation[bot]", "User", "ready_for_review"],
        ["reopened event with non-bot sender", 42, 42, 42, "automation[bot]", "User", "reopened"],
        ["labeled event with non-bot sender", 42, 42, 42, "automation[bot]", "User", "labeled"],
      ])("should not bypass permission verification for a %s", async (_reason, headId, repositoryId, baseId, senderLogin, senderType, action) => {
        mockContext.actor = "automation[bot]";
        mockContext.payload.sender = { login: senderLogin, type: senderType };
        mockContext.payload.action = action;
        mockContext.payload.repository.id = repositoryId;
        mockContext.payload.pull_request.head.repo.id = headId;
        mockContext.payload.pull_request.base.repo.id = baseId;
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      });

      describe.each(["pull_request", "pull_request_target"])("allow-listed bot edited events on %s", eventName => {
        beforeEach(() => {
          mockContext.eventName = eventName;
          mockContext.actor = "automation[bot]";
          mockContext.payload.sender = { login: "automation[bot]", type: "Bot" };
          mockContext.payload.action = "edited";
          mockContext.payload.changes = { body: { from: "Original PR description" } };
          mockContext.payload.repository.id = 42;
          mockContext.payload.pull_request.head.repo.id = 42;
          mockContext.payload.pull_request.base.repo.id = 42;
          process.env.GH_AW_ALLOWED_BOTS = "automation";
          mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });
        });

        it.each(["automation", "automation[bot]"])("should check out a same-repository PR when on.bots contains %s", async bots => {
          process.env.GH_AW_ALLOWED_BOTS = bots;

          await runScript();

          expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
          expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
          expect(mockCore.setFailed).not.toHaveBeenCalled();
          expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", expect.arrayContaining(["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head"]), { silent: true, ignoreReturnCode: true });
          expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
        });

        it.each([
          ["unlisted bot", { bots: "other-app" }],
          ["missing allowlist", { bots: undefined }],
          ["missing changes", { changes: undefined }],
          ["missing body change", { changes: { title: { from: "Original PR title" } } }],
          ["invalid body change", { changes: { body: null } }],
          ["body and title changed", { changes: { body: { from: "Original PR description" }, title: { from: "Original PR title" } } }],
          ["body and base changed", { changes: { body: { from: "Original PR description" }, base: { ref: "main" } } }],
          ["fork PR", { headId: 43 }],
          ["missing head repository ID", { headId: undefined }],
          ["missing base repository ID", { baseId: undefined }],
          ["mismatched base repository ID", { baseId: 43 }],
          ["missing runtime repository ID", { repositoryId: undefined }],
          ["invalid runtime repository ID", { repositoryId: 0, headId: 0, baseId: 0 }],
          ["different sender", { sender: { login: "other[bot]", type: "Bot" } }],
          ["non-bot sender", { sender: { login: "automation[bot]", type: "User" } }],
          ["missing sender", { sender: undefined }],
          ["untrusted action", { action: "labeled" }],
        ])("should retain permission checks for %s", async (_reason, overrides) => {
          if ("bots" in overrides) {
            if (overrides.bots === undefined) {
              delete process.env.GH_AW_ALLOWED_BOTS;
            } else {
              process.env.GH_AW_ALLOWED_BOTS = overrides.bots;
            }
          }
          if ("changes" in overrides) {
            if (overrides.changes === undefined) {
              delete mockContext.payload.changes;
            } else {
              mockContext.payload.changes = overrides.changes;
            }
          }
          if ("headId" in overrides) mockContext.payload.pull_request.head.repo.id = overrides.headId;
          if ("baseId" in overrides) mockContext.payload.pull_request.base.repo.id = overrides.baseId;
          if ("repositoryId" in overrides) mockContext.payload.repository.id = overrides.repositoryId;
          if ("sender" in overrides) mockContext.payload.sender = overrides.sender;
          if ("action" in overrides) mockContext.payload.action = overrides.action;

          await runScript();

          expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "automation[bot]" }));
          expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
          expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("requires write or higher"));
          expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
          expect(mockExec.exec).not.toHaveBeenCalled();
        });
      });

      it.each(["pull_request", "pull_request_target"])("should require collaborator permission for a same-repository GitHub App PR on %s with action labeled", async eventName => {
        mockContext.eventName = eventName;
        mockContext.actor = "automation[bot]";
        mockContext.payload.sender = { login: "automation[bot]", type: "Bot" };
        mockContext.payload.action = "labeled";
        mockContext.payload.repository.id = 42;
        mockContext.payload.pull_request.head.repo.id = 42;
        mockContext.payload.pull_request.base.repo.id = 42;
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "automation[bot]" }));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      });

      it.each([
        ["forked PR", 43, 42, 42, "automation[bot]", "Bot"],
        ["mismatched base repository ID", 42, 42, 43, "automation[bot]", "Bot"],
        ["different sender", 42, 42, 42, "other[bot]", "Bot"],
        ["non-bot sender", 42, 42, 42, "automation[bot]", "User"],
      ])("should retain permission checks for a pull_request_target labeled event with %s", async (_reason, headId, repositoryId, baseId, senderLogin, senderType) => {
        mockContext.eventName = "pull_request_target";
        mockContext.actor = "automation[bot]";
        mockContext.payload.sender = { login: senderLogin, type: senderType };
        mockContext.payload.action = "labeled";
        mockContext.payload.repository.id = repositoryId;
        mockContext.payload.pull_request.head.repo.id = headId;
        mockContext.payload.pull_request.base.repo.id = baseId;
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
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
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], expect.anything());
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
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
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], expect.anything());
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
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

      it.each(["workflow_dispatch", "issue_comment", "pull_request_review_comment", "pull_request_review"])("should reject bot actor without write-or-higher permission for %s events", async eventName => {
        mockContext.eventName = eventName;
        mockContext.actor = "third-party-bot[bot]";
        mockContext.payload.sender = { login: "third-party-bot[bot]", type: "Bot" };
        mockContext.payload.repository.id = 42;
        mockContext.payload.pull_request.head.repo.id = 42;
        mockContext.payload.pull_request.base.repo.id = 42;
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
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
        expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("requires write or higher"));
      });

      it.each(["issue_comment", "pull_request_review_comment", "pull_request_review"].flatMap(eventName => ["my-app", "my-app[bot]"].flatMap(actor => ["my-app", "my-app[bot]"].map(login => [eventName, actor, login]))))(
        "should allow an allow-listed App comment or review on a same-repository PR for %s with actor %s and login %s",
        async (eventName, actor, login) => {
          mockContext.eventName = eventName;
          mockContext.actor = actor;
          mockContext.payload.sender = { login, type: "Bot" };
          mockContext.payload[eventName === "pull_request_review" ? "review" : "comment"] = { created_at: "2026-10-27T12:00:00Z", commit_id: "1111111111111111111111111111111111111111", user: { login } };
          mockContext.payload.repository.id = 42;
          mockContext.payload.pull_request = { number: 123, state: "open" };
          if (eventName === "issue_comment") {
            mockContext.payload.issue = { number: 123, state: "open", pull_request: {} };
            delete mockContext.payload.pull_request;
          }
          mockGithub.rest.pulls.get.mockResolvedValue({
            data: {
              commits: 1,
              updated_at: "2026-10-27T12:00:00Z",
              head: { sha: "1111111111111111111111111111111111111111", ref: "feature-branch", repo: { id: 42, full_name: "test-owner/test-repo" } },
              base: { repo: { id: 42, full_name: "test-owner/test-repo" } },
            },
          });
          process.env.GH_AW_ALLOWED_BOTS = "other-bot,my-app";
          mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

          await runScript();

          expect(mockGithub.rest.pulls.get).toHaveBeenCalledTimes(2);
          expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
          expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
          expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", expect.arrayContaining(["fetch", "origin"]), { silent: true, ignoreReturnCode: true });
        }
      );

      it.each([
        ["bot not allow-listed", "", "my-app[bot]", "Bot", 42, 42, 42],
        ["different comment author", "my-app[bot]", "other[bot]", "Bot", 42, 42, 42],
        ["different author login form", "my-app[bot]", "my-app", "Bot", 42, 42, 42],
        ["missing comment author", "my-app[bot]", undefined, "Bot", 42, 42, 42],
        ["non-bot sender", "my-app[bot]", "my-app[bot]", "User", 42, 42, 42],
        ["fork PR", "my-app[bot]", "my-app[bot]", "Bot", 43, 42, 42],
        ["missing head ID", "my-app[bot]", "my-app[bot]", "Bot", undefined, 42, 42],
        ["mismatched base ID", "my-app[bot]", "my-app[bot]", "Bot", 42, 43, 42],
        ["missing runtime ID", "my-app[bot]", "my-app[bot]", "Bot", 42, 42, undefined],
      ])("should require collaborator permission for %s on a comment", async (_reason, bots, author, senderType, headId, baseId, repositoryId) => {
        mockContext.eventName = "issue_comment";
        mockContext.actor = "my-app[bot]";
        mockContext.payload.sender = { login: "my-app[bot]", type: senderType };
        mockContext.payload.comment = { user: { login: author } };
        mockContext.payload.repository.id = repositoryId;
        mockContext.payload.issue = { number: 123, state: "open", pull_request: {} };
        delete mockContext.payload.pull_request;
        mockGithub.rest.pulls.get.mockResolvedValue({
          data: {
            commits: 1,
            updated_at: "2026-10-27T12:00:00Z",
            head: { sha: "1111111111111111111111111111111111111111", ref: "feature-branch", repo: { id: headId, full_name: "test-owner/test-repo" } },
            base: { repo: { id: baseId, full_name: "test-owner/test-repo" } },
          },
        });
        process.env.GH_AW_ALLOWED_BOTS = bots;
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      });

      it.each(["issue_comment", "pull_request_review_comment"])("should reject an allow-listed comment sender that differs from the runtime actor for %s", async eventName => {
        mockContext.eventName = eventName;
        mockContext.actor = "other-app";
        mockContext.payload.sender = { login: "my-app[bot]", type: "Bot" };
        mockContext.payload.comment = { user: { login: "my-app[bot]" } };
        mockContext.payload.repository.id = 42;
        mockContext.payload.issue = { number: 123, state: "open", pull_request: {} };
        process.env.GH_AW_ALLOWED_BOTS = "my-app,other-app";
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "other-app" }));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      });

      const commentCheckoutConformanceCases = [
        {
          name: "allow-listed App slug matches its bot-form sender",
          actor: "my-app",
          sender: "my-app[bot]",
          commentAuthor: "my-app[bot]",
          allowListed: true,
          senderType: "Bot",
          repositoryRelation: "same",
          permission: "none",
          expectedBypass: true,
        },
        {
          name: "allow-listed App bot identity matches its slug-form sender",
          actor: "my-app[bot]",
          sender: "my-app",
          commentAuthor: "my-app",
          allowListed: true,
          senderType: "Bot",
          repositoryRelation: "same",
          permission: "none",
          expectedBypass: true,
        },
        {
          name: "sender and comment author must match exactly",
          actor: "my-app",
          sender: "my-app[bot]",
          commentAuthor: "my-app",
          allowListed: true,
          senderType: "Bot",
          repositoryRelation: "same",
          permission: "none",
          expectedBypass: false,
        },
        {
          name: "runtime actor must canonically match the sender",
          actor: "other",
          sender: "my-app[bot]",
          commentAuthor: "my-app[bot]",
          allowListed: true,
          senderType: "Bot",
          repositoryRelation: "same",
          permission: "none",
          expectedBypass: false,
        },
        {
          name: "sender must be allow-listed",
          actor: "my-app",
          sender: "my-app[bot]",
          commentAuthor: "my-app[bot]",
          allowListed: false,
          senderType: "Bot",
          repositoryRelation: "same",
          permission: "none",
          expectedBypass: false,
        },
        {
          name: "both PR repositories must match the workflow repository",
          actor: "my-app",
          sender: "my-app[bot]",
          commentAuthor: "my-app[bot]",
          allowListed: true,
          senderType: "Bot",
          repositoryRelation: "fork",
          permission: "none",
          expectedBypass: false,
        },
        {
          name: "head repository identity must be present",
          actor: "my-app",
          sender: "my-app[bot]",
          commentAuthor: "my-app[bot]",
          allowListed: true,
          senderType: "Bot",
          repositoryRelation: "missing-head",
          permission: "none",
          expectedBypass: false,
        },
        {
          name: "base repository identity must be present",
          actor: "my-app",
          sender: "my-app[bot]",
          commentAuthor: "my-app[bot]",
          allowListed: true,
          senderType: "Bot",
          repositoryRelation: "missing-base",
          permission: "none",
          expectedBypass: false,
        },
        {
          name: "sender must be a bot",
          actor: "my-app",
          sender: "my-app[bot]",
          commentAuthor: "my-app[bot]",
          allowListed: true,
          senderType: "User",
          repositoryRelation: "same",
          permission: "none",
          expectedBypass: false,
        },
      ];

      it.each(["issue_comment", "pull_request_review_comment", "pull_request_review"].flatMap(eventName => commentCheckoutConformanceCases.map(testCase => ({ eventName, ...testCase }))))(
        "conforms to the comment and review checkout policy for $eventName: $name",
        async testCase => {
          mockContext.eventName = testCase.eventName;
          mockContext.actor = testCase.actor;
          mockContext.payload.sender = { login: testCase.sender, type: testCase.senderType };
          mockContext.payload[testCase.eventName === "pull_request_review" ? "review" : "comment"] = { created_at: "2026-10-27T12:00:00Z", commit_id: "1111111111111111111111111111111111111111", user: { login: testCase.commentAuthor } };
          mockContext.payload.repository.id = 42;
          mockContext.payload.pull_request = { number: 123, state: "open" };
          if (testCase.eventName === "issue_comment") {
            mockContext.payload.issue = { number: 123, state: "open", pull_request: {} };
            delete mockContext.payload.pull_request;
          }
          const headId = testCase.repositoryRelation === "same" || testCase.repositoryRelation === "missing-base" ? 42 : testCase.repositoryRelation === "missing-head" ? undefined : 43;
          const baseId = testCase.repositoryRelation === "missing-base" ? undefined : 42;
          mockGithub.rest.pulls.get.mockResolvedValue({
            data: {
              commits: 1,
              updated_at: "2026-10-27T12:00:00Z",
              head: { sha: "1111111111111111111111111111111111111111", ref: "feature-branch", repo: { id: headId, full_name: "test-owner/test-repo" } },
              base: { repo: { id: baseId, full_name: "test-owner/test-repo" } },
            },
          });
          process.env.GH_AW_ALLOWED_BOTS = testCase.allowListed ? "my-app" : "";
          mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
            data: { permission: testCase.permission },
          });

          await runScript();

          if (testCase.expectedBypass) {
            expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
            expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
            expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", expect.arrayContaining(["fetch", "origin"]), { silent: true, ignoreReturnCode: true });
          } else {
            expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
            expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
            expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch", "origin"]), expect.anything());
          }
        }
      );

      it("does not trust a review using a matching comment author instead of the review author", async () => {
        mockContext.eventName = "pull_request_review";
        mockContext.actor = "my-app[bot]";
        mockContext.payload.sender = { login: "my-app[bot]", type: "Bot" };
        mockContext.payload.review = { user: { login: "other[bot]" } };
        mockContext.payload.comment = { user: { login: "my-app[bot]" } };
        mockContext.payload.repository.id = 42;
        mockContext.payload.pull_request = { number: 123, state: "open" };
        process.env.GH_AW_ALLOWED_BOTS = "my-app";
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      });

      it("should validate centralized workflow_dispatch using the originating aw_context actor", async () => {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          repository: { fork: false },
          sender: { login: "github-actions[bot]", type: "Bot" },
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              pr_head_sha: "1111111111111111111111111111111111111111",
              actor: "trusted-maintainer",
              item_type: "pull_request",
              item_number: 123,
            }),
          },
        };

        await runScript();

        expect(mockCore.info).toHaveBeenCalledWith("Validating centralized workflow_dispatch against originating actor 'trusted-maintainer'");
        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "trusted-maintainer" }));
        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalledWith(expect.objectContaining({ username: "github-actions[bot]" }));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      });

      it("should reject centralized workflow_dispatch without an originating aw_context actor", async () => {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          repository: { fork: false },
          sender: { login: "github-actions[bot]", type: "Bot" },
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              pr_head_sha: "1111111111111111111111111111111111111111",
              item_type: "pull_request",
              item_number: 123,
            }),
          },
        };

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unable to determine originating actor"));
      });

      it("should reject github-actions bot PR dispatch without centralized router markers", async () => {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          repository: { fork: false },
          sender: { login: "github-actions[bot]", type: "Bot" },
          inputs: {
            aw_context: JSON.stringify({
              actor: "trusted-maintainer",
              item_type: "pull_request",
              item_number: 123,
            }),
          },
        };

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unable to identify centralized workflow_dispatch"));
      });

      it("should not trust a propagated actor from a third-party bot dispatcher", async () => {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "third-party-bot[bot]";
        mockContext.payload = {
          repository: { fork: false },
          sender: { login: "third-party-bot[bot]", type: "Bot" },
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              pr_head_sha: "1111111111111111111111111111111111111111",
              actor: "trusted-maintainer",
              item_type: "pull_request",
              item_number: 123,
            }),
          },
        };
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
          data: { permission: "read" },
        });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "third-party-bot[bot]" }));
        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalledWith(expect.objectContaining({ username: "trusted-maintainer" }));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("requires write or higher"));
      });

      it("should not trust a propagated actor without GitHub's Bot sender signal", async () => {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          repository: { fork: false },
          sender: { login: "github-actions[bot]", type: "User" },
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              pr_head_sha: "1111111111111111111111111111111111111111",
              actor: "trusted-maintainer",
              item_type: "pull_request",
              item_number: 123,
            }),
          },
        };
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "write" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalledWith(expect.objectContaining({ username: "trusted-maintainer" }));
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unable to verify centralized workflow_dispatch identity"));
      });

      it("should reject centralized dispatch when the propagated actor is the router itself", async () => {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          repository: { fork: false },
          sender: { login: "github-actions[bot]", type: "Bot" },
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              pr_head_sha: "1111111111111111111111111111111111111111",
              actor: "github-actions[bot]",
              item_type: "pull_request",
              item_number: 123,
            }),
          },
        };

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("must identify an originating actor"));
      });

      it("should reject a propagated originating actor who is not a collaborator", async () => {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          repository: { fork: false },
          sender: { login: "github-actions[bot]", type: "Bot" },
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              pr_head_sha: "1111111111111111111111111111111111111111",
              actor: "external-user",
              item_type: "pull_request",
              item_number: 123,
            }),
          },
        };
        const notFound = Object.assign(new Error("Not Found"), { status: 404 });
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(notFound);
        mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { login: "external-user" } });

        await runScript();

        expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith(expect.objectContaining({ username: "external-user" }));
        expect(mockGithub.rest.users.getByUsername).toHaveBeenCalledWith({ username: "external-user" });
        expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is not a collaborator"));
      });
    });

    it("should handle git fetch errors", async () => {
      mockFetch.mockRejectedValueOnce(new Error("git fetch failed"));

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
            sha: "1111111111111111111111111111111111111111",
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
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching an immutable PR head SHA from origin");

      // Verify strategy is git fetch SHA + checkout
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch SHA + checkout");
      expect(mockCore.info).toHaveBeenCalledWith("Reason: pull_request event from fork repository; fetching an immutable PR head SHA");

      // Verify git fetch refs/pull/N/head is used
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
      expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", ["fetch", "origin", "feature-branch", "--depth=2"], expect.anything());
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_BRANCH", "feature-branch");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_REPO", "test-owner/test-repo");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_REPO", "fork-owner/test-repo");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_REF", "refs/remotes/origin/pr-head");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", "1111111111111111111111111111111111111111");
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

      // The same-repository fast path also uses the retained PR ref.
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("gh", ["pr", "checkout", "123"]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should use git fetch for non-fork pull_request event", async () => {
      // Default mock context is non-fork (same repo)

      await runScript();

      // Verify non-fork detection
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: false (same repository)");

      // Verify the retained PR ref is used for non-fork PRs too.
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
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
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch SHA + checkout");

      // Verify git fetch refs/pull/N/head and checkout
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);

      expect(mockCore.info).toHaveBeenCalledWith("✅ Successfully checked out PR #123");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should handle git fetch errors for PR ref", async () => {
      mockFetch.mockRejectedValueOnce(new Error("git fetch failed"));

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
      mockContext.eventName = "issue_comment";
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
          updated_at: "2026-10-27T12:00:00Z",
          commits: 3,
          head: { sha: "1111111111111111111111111111111111111111", ref: "my-feature", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
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
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=4"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "my-feature", "refs/remotes/origin/pr-head"]);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should detect fork PR from API when payload has minimal PR", async () => {
      mockContext.eventName = "issue_comment";
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
          updated_at: "2026-10-27T12:00:00Z",
          commits: 2,
          head: { sha: "1111111111111111111111111111111111111111", ref: "fork-feature", repo: { full_name: "fork-owner/test-repo", owner: { login: "fork-owner" } } },
          base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        },
      });

      await runScript();

      // Fork status should be "unknown" initially, then resolved from API
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR: unknown (head/base repo details not available in event payload)");
      expect(mockCore.info).toHaveBeenCalledWith("Is fork PR (from API): true (different repository names)");
      // Should emit fork warning
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching an immutable PR head SHA from origin");
      // Should successfully checkout
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=3"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "fork-feature", "refs/remotes/origin/pr-head"]);
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

      // workflow_dispatch uses git fetch SHA + checkout (not the fast pull_request path)
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);

      // fetchPRDetails must be called with the correct PR number to resolve head ref / commit count
      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith(expect.objectContaining({ pull_number: 123 }));

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should refuse PR replay when the runtime repository is a fork", async () => {
      mockContext.payload.repository.fork = true;

      await runScript();

      expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Refusing PR checkout in forked repository runtime context"));
    });

    it("should refuse PR replay when the workflow_dispatch payload has no repository data", async () => {
      delete mockContext.payload.repository;

      await runScript();

      expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unable to determine repository fork status for workflow_dispatch"));
    });

    it.each([{ fork: undefined }, { fork: null }, { fork: "false" }, { fork: 0 }])("should refuse PR replay when repository fork status is unverifiable: %j", async repository => {
      mockContext.payload.repository = repository;

      await runScript();

      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
      expect(mockExec.getExecOutput).not.toHaveBeenCalledWith("git", expect.arrayContaining(["fetch"]), expect.anything());
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unable to verify repository is not a fork"));
    });

    it.each([true, false, " 123", "123 ", "1e3", "0x7b", "123.0", "-1", -1, 1.5, Number.MAX_SAFE_INTEGER + 1, {}, []])("should reject adversarial non-canonical PR number %j before trust or API operations", async itemNumber => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request", item_number: itemNumber });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
      expect(mockGithub.rest.pulls.get).not.toHaveBeenCalled();
      expect(mockExec.exec).not.toHaveBeenCalled();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("canonical positive PR number"));
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

    it("should fail checkout when aw_context is invalid JSON", async () => {
      mockContext.payload.inputs.aw_context = "not-valid-json{";

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("invalid aw_context"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should fail checkout when aw_context pull_request has no item_number", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request" });

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("canonical positive PR number"));
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should fail checkout when aw_context item_number is a non-numeric string", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request", item_number: "abc" });

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("canonical positive PR number"));
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should fail checkout when aw_context item_number is zero", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request", item_number: 0 });

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("canonical positive PR number"));
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("should fail checkout when aw_context item_number is a non-integer float", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({ item_type: "pull_request", item_number: 1.5 });

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("canonical positive PR number"));
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
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockCore.warning).toHaveBeenCalledWith("Manual PR dispatch selects the current head, not an approved revision.");
    });

    it("should fail checkout when aw_context repo does not match current repository", async () => {
      mockContext.payload.inputs.aw_context = JSON.stringify({
        item_type: "pull_request",
        item_number: 123,
        repo: "other-owner/other-repo",
      });

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Cross-repository workflow_dispatch is not supported"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
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
      // pull_request_target uses git fetch SHA + checkout
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
    });

    it("should handle pull_request_review event", async () => {
      mockContext.eventName = "pull_request_review";

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event: pull_request_review");
      // pull_request_review uses git fetch SHA + checkout
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
    });

    it("should handle pull_request_review_comment event", async () => {
      mockContext.eventName = "pull_request_review_comment";

      await runScript();

      // pull_request_review_comment uses git fetch SHA + checkout
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
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

      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature-branch", "refs/remotes/origin/pr-head"]);
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

      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "feature/new-feature", "refs/remotes/origin/pr-head"]);
    });

    it("should handle branches with special characters", async () => {
      mockContext.payload.pull_request.head.ref = "fix-issue-#123";

      await runScript();

      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["checkout", "-B", "fix-issue-#123", "refs/remotes/origin/pr-head"]);
    });

    it("should handle very long branch names", async () => {
      const longBranchName = "feature/" + "x".repeat(200);
      mockContext.payload.pull_request.head.ref = longBranchName;

      await runScript();

      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
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
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
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
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching an immutable PR head SHA from origin");
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
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
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
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
          head: { sha: "1111111111111111111111111111111111111111", ref: "feature-branch", repo: null },
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
      expect(mockCore.warning).toHaveBeenCalledWith("⚠️ Fork PR detected - fetching an immutable PR head SHA from origin");
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
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch SHA + checkout");
      expect(mockCore.info).toHaveBeenCalledWith("Reason: same-repository pull_request event; immutable event SHA survives branch deletion");
    });

    it("should log checkout strategy for pull_request_target events", async () => {
      mockContext.eventName = "pull_request_target";

      await runScript();

      expect(mockCore.startGroup).toHaveBeenCalledWith("🔄 Checkout Strategy");
      expect(mockCore.info).toHaveBeenCalledWith("Strategy: git fetch SHA + checkout");
      expect(mockCore.info).toHaveBeenCalledWith("Reason: pull_request_target runs in base repo context; fetching an immutable PR head SHA");
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
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["status"], { silent: true });
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["remote", "-v"], { silent: true });
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["branch", "--show-current"], { silent: true });
    });

    it("should handle git diagnostic command failures gracefully", async () => {
      mockExec.exec.mockRejectedValueOnce(new Error("checkout failed"));
      const getExecOutput = mockExec.getExecOutput.getMockImplementation();
      mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
        if (args[0] === "status") throw new Error("git command not available");
        return getExecOutput(cmd, args, options);
      });

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringMatching(/Could not retrieve git state/));
    });
  });

  describe("closed pull request handling", () => {
    it("stops a closed-event workflow when its PR head cannot be checked out", async () => {
      mockContext.payload.pull_request.state = "closed";
      mockExec.exec.mockRejectedValueOnce(new Error("git fetch failed - branch deleted"));

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Failed to checkout PR branch: git fetch failed - branch deleted`);
    });

    it("does not continue on the base checkout after a closed-PR checkout failure", async () => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.pull_request.state = "closed";
      mockExec.exec.mockRejectedValueOnce(new Error("git fetch failed - PR closed"));

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
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
      expect(mockExec.getExecOutput).toHaveBeenCalledWith("git", ["fetch", "origin", "+1111111111111111111111111111111111111111:refs/remotes/origin/pr-head", "--depth=2"], { silent: true, ignoreReturnCode: true });
    });

    it("should handle closed PR without head ref", async () => {
      mockContext.payload.pull_request.state = "closed";
      delete mockContext.payload.pull_request.head;
      mockExec.exec.mockRejectedValueOnce(new Error("no branch info"));

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
    });

    it("should include PR state in context logging", async () => {
      mockContext.payload.pull_request.state = "closed";

      await runScript();

      // State is already logged in logPRContext
      expect(mockCore.info).toHaveBeenCalledWith("PR state: closed");
    });
  });

  describe("race condition - PR merged after workflow trigger", () => {
    const missingCommit = "fatal: remote error: upload-pack: not our ref 1111111111111111111111111111111111111111";

    it.each([
      ["open", true, false],
      ["open", false, false],
      ["closed", true, false],
      ["closed", false, false],
      ["open", true, true],
      ["open", false, true],
      ["closed", true, true],
      ["open", true, true, true],
    ])("checks out the event commit after branch deletion (payload state %s, shallow %s, head moved %s, fork %s)", async (state, shallow, headMoved, fork) => {
      const actionsExec = await import("@actions/exec");
      const root = mkdtempSync(join(import.meta.dirname, ".checkout-pr-race-"));
      const upstream = join(root, "upstream");
      const workspace = join(root, "workspace");
      const env = {
        PATH: process.env.PATH,
        HOME: root,
        GIT_CONFIG_NOSYSTEM: "1",
        GIT_CONFIG_GLOBAL: "/dev/null",
        GIT_TERMINAL_PROMPT: "0",
        GIT_AUTHOR_NAME: "Checkout Test",
        GIT_AUTHOR_EMAIL: "checkout-test@example.com",
        GIT_COMMITTER_NAME: "Checkout Test",
        GIT_COMMITTER_EMAIL: "checkout-test@example.com",
      };
      const git = (cwd, args) => execFileSync("git", args, { cwd, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
      try {
        git(root, ["init", "--initial-branch=main", upstream]);
        writeFileSync(join(upstream, "content.txt"), "base\n");
        git(upstream, ["add", "content.txt"]);
        git(upstream, ["commit", "-m", "base"]);
        const baseSha = git(upstream, ["rev-parse", "HEAD"]);
        git(upstream, ["checkout", "-b", "feature-branch"]);
        writeFileSync(join(upstream, "content.txt"), "PR head\n");
        git(upstream, ["commit", "-am", "PR head"]);
        if (headMoved) {
          for (let i = 2; i <= 10; i++) {
            writeFileSync(join(upstream, "content.txt"), `PR commit ${i}\n`);
            git(upstream, ["commit", "-am", `PR commit ${i}`]);
          }
        }
        const headSha = git(upstream, ["rev-parse", "HEAD"]);
        if (headMoved) {
          git(upstream, ["update-ref", "refs/pull/123/merge", headSha]);
          git(upstream, ["checkout", "-B", "feature-branch", "main"]);
          writeFileSync(join(upstream, "content.txt"), "unapproved newer head\n");
          git(upstream, ["commit", "-am", "force-push a single replacement commit"]);
        }
        const currentHeadSha = git(upstream, ["rev-parse", "HEAD"]);
        git(upstream, ["update-ref", "refs/pull/123/head", currentHeadSha]);
        git(upstream, ["checkout", "main"]);
        git(upstream, ["update-ref", "-d", "refs/heads/feature-branch"]);
        git(root, ["clone", "--branch", "main", ...(shallow ? ["--depth=1"] : []), pathToFileURL(upstream).href, workspace]);
        git(workspace, ["branch", "feature-branch"]);
        git(workspace, ["update-ref", "refs/remotes/origin/pr-head", baseSha]);

        const deletedBranchFetch = spawnSync("git", ["fetch", "origin", "feature-branch"], { cwd: workspace, env, encoding: "utf8" });
        expect(deletedBranchFetch.status).toBe(128);
        expect(deletedBranchFetch.stderr).toContain("couldn't find remote ref feature-branch");
        mockContext.payload.pull_request.state = state;
        mockContext.payload.pull_request.head.sha = headSha;
        mockContext.payload.pull_request.commits = headMoved ? 10 : 1;
        if (fork) mockContext.payload.pull_request.head.repo.full_name = "fork-owner/test-repo";
        const fullPR = (await mockGithub.rest.pulls.get()).data;
        mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...fullPR, commits: 1, head: { ...fullPR.head, sha: currentHeadSha }, state: "closed", merged: true } });
        mockExec.getExecOutput.mockImplementation((cmd, args, options) => actionsExec.getExecOutput(cmd, args, { ...options, cwd: workspace, env }));
        mockExec.exec.mockImplementation((cmd, args) => actionsExec.exec(cmd, args, { cwd: workspace, env, silent: true }));

        await runScript();

        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "true");
        expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
        expect(git(workspace, ["rev-parse", "HEAD"])).toBe(headSha);
        expect(git(workspace, ["branch", "--show-current"])).toBe("feature-branch");
        expect(git(workspace, ["rev-parse", "refs/remotes/origin/pr-head"])).toBe(headSha);
        expect(git(workspace, ["rev-parse", "--is-shallow-repository"])).toBe(String(shallow));
        expect(git(workspace, ["merge-base", "HEAD", "origin/main"])).toBe(baseSha);
        expect(readFileSync(join(workspace, "content.txt"), "utf8")).toBe(headMoved ? "PR commit 10\n" : "PR head\n");
        expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_SHA", headSha);
        expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PR_HEAD_BASE_BRANCH", "feature-branch");
        expect(git(upstream, ["for-each-ref", "--format=%(refname)", "refs/heads/feature-branch"])).toBe("");
      } finally {
        rmSync(root, { recursive: true, force: true });
      }
    });

    it.each([
      ["pull_request", "Authentication failed"],
      ["pull_request", "Permission denied"],
      ["pull_request", "network error"],
      ["pull_request", "unknown git error"],
      ["pull_request", ""],
      ["issue_comment", "Authentication failed"],
      ["issue_comment", "Permission denied"],
      ["issue_comment", "network error"],
      ["issue_comment", "unknown git error"],
      ["issue_comment", ""],
    ])("does not suppress a %s fetch failure with stderr %j even when the PR has closed", async (eventName, message) => {
      mockContext.eventName = eventName;
      const fullPR = (await mockGithub.rest.pulls.get()).data;
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...fullPR, state: "closed" } });
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: message });
      await runScript();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("git fetch failed with exit code 128"));
      if (message) expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(message));
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
    });
    it.each([missingCommit, "fatal: couldn't find remote ref 1111111111111111111111111111111111111111"])("marks confirmed lifecycle invalidation but fails the step to stop the agent: %s", async diagnostic => {
      // The selected commit is unavailable after closure.
      mockContext.payload.pull_request.state = "open";
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: diagnostic });
      // Non-fork pull_request uses fast path (no fetchPRDetails call).
      // Only the error handler re-check calls pulls.get → returns closed.
      mockGithub.rest.pulls.get.mockResolvedValueOnce({
        data: {
          state: "closed",
          commits: 1,
          head: { sha: "1111111111111111111111111111111111111111", ref: "feature-branch", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
          base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        },
      });

      await runScript();

      // Should log info about the state change
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("is now closed"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("was 'open' in webhook payload"));

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalled();
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
    });

    it("uses real @actions/exec output to detect missing-commit evidence", async () => {
      const actionsExec = await import("@actions/exec");
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { state: "closed" } });
      mockFetch.mockImplementation((_cmd, _args, options) => actionsExec.getExecOutput(process.execPath, ["-e", `process.stderr.write(${JSON.stringify(`${missingCommit}\n`)}); process.exit(128);`], options));
      await runScript();
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalled();
    });

    it("does not classify a non-fetch error containing missing-commit text as invalidation", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { state: "closed" } });
      mockExec.exec.mockRejectedValueOnce(new Error(missingCommit));
      await runScript();
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalled();
    });

    it.each(["pull_request", "issue_comment"])("reports actionable SHA-fetch guidance for an unavailable commit on %s while the PR remains open", async eventName => {
      mockContext.eventName = eventName;
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: missingCommit });
      await runScript();
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("ask your administrator to verify commit availability and the upload-pack SHA fetch policy"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(missingCommit));
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
    });

    it.each(["pull_request", "issue_comment"])("reports enterprise SHA-fetch policy restrictions without treating them as lifecycle invalidation on %s", async eventName => {
      mockContext.eventName = eventName;
      const current = (await mockGithub.rest.pulls.get()).data;
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...current, state: "closed" } });
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: "error: Server does not allow request for unadvertised object 1111111111111111111111111111111111111111" });
      await runScript();
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GitHub Enterprise Server"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("do not fall back to a mutable ref"));
      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
    });

    it("does not classify missing-commit text on stdout as fetch failure evidence", async () => {
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { state: "closed" } });
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: missingCommit, stderr: "unknown git error" });
      await runScript();
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("unknown git error"));
    });

    it("keeps an intentional closed-event missing-commit failure visible", async () => {
      mockContext.payload.action = "closed";
      mockContext.payload.pull_request.state = "closed";
      const fullPR = (await mockGithub.rest.pulls.get()).data;
      mockGithub.rest.pulls.get.mockResolvedValue({ data: { ...fullPR, state: "closed" } });
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: missingCommit });
      await runScript();
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalled();
    });

    it("redacts credentials from captured fetch failures and git diagnostics", async () => {
      const credentials = ["test-token", "url-password-fixture", "basic-auth-fixture", "custom-secret-fixture"];
      vi.stubEnv("GH_AW_SECRET_NAMES", "CHECKOUT_TEST");
      vi.stubEnv("SECRET_CHECKOUT_TEST", credentials[3]);
      const diagnostic = `fatal: Authentication failed for https://user:${credentials[1]}@github.com/owner/repo.git\nAuthorization: Basic ${credentials[2]}\n${credentials[0]} ${credentials[3]}`;
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: diagnostic });
      const getExecOutput = mockExec.getExecOutput.getMockImplementation();
      mockExec.getExecOutput.mockImplementation((cmd, args, options) => {
        if (["status", "remote", "branch"].includes(args[0])) {
          return Promise.resolve({ exitCode: 0, stdout: diagnostic, stderr: diagnostic });
        }
        return getExecOutput(cmd, args, options);
      });
      try {
        await runScript();
        const emitted = JSON.stringify([mockCore.info.mock.calls, mockCore.warning.mock.calls, mockCore.error.mock.calls, mockCore.setFailed.mock.calls, mockCore.summary.addRaw.mock.calls]);
        for (const secret of credentials) expect(emitted).not.toContain(secret);
        expect(emitted).toContain("Authentication failed");
        expect(emitted).toContain("git fetch failed with exit code 128");
        expect(mockCore.setOutput).not.toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      } finally {
        vi.unstubAllEnvs();
      }
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
          head: { sha: "1111111111111111111111111111111111111111", ref: "feature-branch", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
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

    it("handles missing-commit lifecycle invalidation for the API checkout path (issue_comment event)", async () => {
      mockContext.eventName = "issue_comment";
      mockContext.payload.pull_request.state = "open";
      // First pulls.get (fetchPRDetails): succeeds
      // Second pulls.get (re-check): shows PR was merged
      const fullPRData = {
        state: "open",
        updated_at: "2026-10-27T12:00:00Z",
        commits: 1,
        head: { sha: "1111111111111111111111111111111111111111", ref: "feature-branch", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
        base: { ref: "main", repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
      };
      const closedPRData = { ...fullPRData, state: "closed" };
      mockGithub.rest.pulls.get.mockResolvedValueOnce({ data: fullPRData }).mockResolvedValueOnce({ data: closedPRData });
      mockFetch.mockResolvedValueOnce({ exitCode: 128, stdout: "", stderr: missingCommit });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_success", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("checkout_pr_invalidated", "true");
      expect(mockCore.setFailed).toHaveBeenCalled();
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", expect.arrayContaining(["checkout"]));
    });
  });
});
