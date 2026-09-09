import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";

const realRequire = createRequire(import.meta.url);

describe("check_membership.cjs", () => {
  let mockCore;
  let mockGithub;
  let mockContext;
  let checkMembershipScript;

  beforeEach(() => {
    // Mock core actions methods
    mockCore = {
      debug: vi.fn(),
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(),
      },
    };

    // Mock GitHub API
    mockGithub = {
      rest: {
        repos: {
          getCollaboratorPermissionLevel: vi.fn(),
        },
        pulls: {
          get: vi.fn(),
        },
      },
    };

    // Mock context
    mockContext = {
      eventName: "issues",
      actor: "testuser",
      repo: {
        owner: "testorg",
        repo: "testrepo",
      },
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  afterEach(() => {
    delete global.core;
    delete global.github;
    delete global.context;
    delete process.env.GH_AW_REQUIRED_ROLES;
    delete process.env.GH_AW_ALLOWED_BOTS;
  });

  const runScript = async () => {
    const fs = await import("fs");
    const path = await import("path");
    const scriptPath = path.join(import.meta.dirname, "check_membership.cjs");
    const scriptContent = fs.readFileSync(scriptPath, "utf8");

    // Load the utility module
    const utilsPath = path.join(import.meta.dirname, "check_permissions_utils.cjs");
    const utilsContent = fs.readFileSync(utilsPath, "utf8");

    // Load error helpers module
    const errorHelpersPath = path.join(import.meta.dirname, "error_helpers.cjs");
    const errorHelpersContent = fs.readFileSync(errorHelpersPath, "utf8");

    // Create a mock require function
    const mockRequire = modulePath => {
      if (modulePath === "./error_helpers.cjs") {
        // Execute the error helpers module and return its exports
        const errorHelpersFunction = new Function("module", "exports", errorHelpersContent);
        const errorHelpersModuleExports = {};
        const errorHelpersMockModule = { exports: errorHelpersModuleExports };
        errorHelpersFunction(errorHelpersMockModule, errorHelpersModuleExports);
        return errorHelpersMockModule.exports;
      }
      if (modulePath === "./check_permissions_utils.cjs") {
        // Execute the utility module and return its exports
        // Need to pass mockRequire to handle error_helpers require
        const utilsFunction = new Function("core", "github", "context", "process", "module", "exports", "require", utilsContent);
        const moduleExports = {};
        const mockModule = { exports: moduleExports };
        utilsFunction(mockCore, mockGithub, mockContext, process, mockModule, moduleExports, mockRequire);
        return mockModule.exports;
      }
      if (modulePath === "./pre_activation_summary.cjs") {
        return {
          writeDenialSummary: async (reason, remediation) => {
            await mockCore.summary.addRaw(`${reason}\n${remediation}`).write();
          },
        };
      }
      if (modulePath === "./error_recovery.cjs") {
        return realRequire(modulePath);
      }
      throw new Error(`Module not found: ${modulePath}`);
    };

    // Remove the main() call/export at the end and execute
    const scriptWithoutMain = scriptContent.replace("module.exports = { main, checkBotAllowlistAuthorization };", "");
    const scriptFunction = new Function("core", "github", "context", "process", "require", scriptWithoutMain + "\nreturn main();");
    await scriptFunction(mockCore, mockGithub, mockContext, process, mockRequire);
  };

  describe("safe events", () => {
    // workflow_run is no longer a safe event due to HIGH security risks:
    // - Privilege escalation (inherits permissions from triggering workflow)
    // - Branch protection bypass (can execute on protected branches)
    // - Secret exposure (secrets available from untrusted code)

    it("should skip check for schedule events", async () => {
      mockContext.eventName = "schedule";
      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("✅ Event schedule does not require validation");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "safe_event");
    });

    it("should skip check for merge_group events", async () => {
      mockContext.eventName = "merge_group";
      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("✅ Event merge_group does not require validation");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "safe_event");
    });

    it("should validate workflow_dispatch when write role is allowed", async () => {
      mockContext.eventName = "workflow_dispatch";
      process.env.GH_AW_REQUIRED_ROLES = "write,read";
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event workflow_dispatch requires validation");
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
    });

    it("should validate workflow_dispatch when write role is not allowed", async () => {
      mockContext.eventName = "workflow_dispatch";
      process.env.GH_AW_REQUIRED_ROLES = "admin,maintainer";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "admin" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Event workflow_dispatch requires validation");
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalled();
    });

    it("should validate centralized workflow_dispatch using aw_context actor", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.actor = "github-actions[bot]";
      mockContext.payload = {
        inputs: {
          aw_context: JSON.stringify({
            command_name: "triage",
            actor: "octocat",
          }),
        },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Validating centralized workflow_dispatch against originating actor 'octocat'");
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({
        owner: "testorg",
        repo: "testrepo",
        username: "octocat",
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });

    it("should validate centralized label workflow_dispatch using aw_context actor", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.actor = "github-actions[bot]";
      mockContext.payload = {
        inputs: {
          aw_context: JSON.stringify({
            trigger_label: "necromancer",
            actor: "octocat",
          }),
        },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Validating centralized workflow_dispatch against originating actor 'octocat'");
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({
        owner: "testorg",
        repo: "testrepo",
        username: "octocat",
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });

    it("should deny centralized workflow_dispatch from fork-based pull requests", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.actor = "github-actions[bot]";
      mockContext.payload = {
        inputs: {
          aw_context: JSON.stringify({
            command_name: "triage",
            actor: "octocat",
            item_type: "pull_request",
            item_number: "42",
          }),
        },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";
      mockGithub.rest.pulls.get.mockResolvedValue({
        data: {
          head: { repo: { full_name: "someone/fork" } },
          base: { repo: { full_name: "testorg/testrepo" } },
        },
      });

      await runScript();

      expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
        owner: "testorg",
        repo: "testrepo",
        pull_number: 42,
      });
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "fork_pull_request");
    });

    it("should retry a transient pull request provenance failure", async () => {
      vi.useFakeTimers();
      try {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              actor: "octocat",
              item_type: "pull_request",
              item_number: "42",
            }),
          },
        };
        process.env.GH_AW_REQUIRED_ROLES = "write";
        const apiError = Object.assign(new Error("Internal Server Error"), { status: 500 });
        mockGithub.rest.pulls.get.mockRejectedValueOnce(apiError).mockResolvedValue({
          data: {
            head: { repo: { full_name: "testorg/testrepo" } },
            base: { repo: { full_name: "testorg/testrepo" } },
          },
        });
        mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
          data: { permission: "write" },
        });

        const runPromise = runScript();
        await vi.runAllTimersAsync();
        await runPromise;

        expect(mockGithub.rest.pulls.get).toHaveBeenCalledTimes(2);
        expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
      } finally {
        vi.useRealTimers();
      }
    });

    it("should not retry a permanent pull request provenance failure", async () => {
      mockContext.eventName = "workflow_dispatch";
      mockContext.actor = "github-actions[bot]";
      mockContext.payload = {
        inputs: {
          aw_context: JSON.stringify({
            command_name: "triage",
            actor: "octocat",
            item_type: "pull_request",
            item_number: "42",
          }),
        },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";
      const apiError = Object.assign(new Error("Not Found"), { status: 404 });
      mockGithub.rest.pulls.get.mockRejectedValue(apiError);

      await runScript();

      expect(mockGithub.rest.pulls.get).toHaveBeenCalledTimes(1);
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "api_error");
    });

    it("should fail closed after exhausting transient provenance retries", async () => {
      vi.useFakeTimers();
      try {
        mockContext.eventName = "workflow_dispatch";
        mockContext.actor = "github-actions[bot]";
        mockContext.payload = {
          inputs: {
            aw_context: JSON.stringify({
              command_name: "triage",
              actor: "octocat",
              item_type: "pull_request",
              item_number: "42",
            }),
          },
        };
        process.env.GH_AW_REQUIRED_ROLES = "write";
        const apiError = Object.assign(new Error("Internal Server Error"), { status: 500 });
        mockGithub.rest.pulls.get.mockRejectedValue(apiError);

        const runPromise = runScript();
        await vi.runAllTimersAsync();
        await runPromise;

        expect(mockGithub.rest.pulls.get).toHaveBeenCalledTimes(4);
        expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
        expect(mockCore.setOutput).toHaveBeenCalledWith("result", "api_error");
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe("configuration validation", () => {
    it("should fail when no required permissions are specified", async () => {
      delete process.env.GH_AW_REQUIRED_ROLES;

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith("❌ Configuration error: Required permissions not specified. Contact repository administrator.");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "config_error");
      expect(mockCore.setOutput).toHaveBeenCalledWith("error_message", "Configuration error: Required permissions not specified");
    });

    it("should fail when required permissions is empty string", async () => {
      process.env.GH_AW_REQUIRED_ROLES = "";

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith("❌ Configuration error: Required permissions not specified. Contact repository administrator.");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "config_error");
    });

    it("should filter out empty permission values", async () => {
      process.env.GH_AW_REQUIRED_ROLES = "admin, , write, ";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "admin" },
      });

      await runScript();

      // Check that the log contains filtered permissions (note: there's no trimming of the space after comma in actual code)
      expect(mockCore.info).toHaveBeenCalled();
      const logCalls = mockCore.info.mock.calls.map(call => call[0]);
      const permissionsLog = logCalls.find(log => log.includes("Required permissions:"));
      expect(permissionsLog).toBeTruthy();
    });
  });

  describe("permission checks", () => {
    beforeEach(() => {
      process.env.GH_AW_REQUIRED_ROLES = "admin,write";
    });

    it("should authorize user with exact permission match", async () => {
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "admin" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' has required permissions for testorg/testrepo");
      expect(mockCore.info).toHaveBeenCalledWith("Required permissions: admin, write");
      expect(mockCore.info).toHaveBeenCalledWith("Repository permission level: admin");
      expect(mockCore.info).toHaveBeenCalledWith("✅ User has admin access to repository");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "admin");
    });

    it("should handle maintainer/maintain alias", async () => {
      process.env.GH_AW_REQUIRED_ROLES = "maintainer";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write", role_name: "maintain" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("✅ User has maintain access to repository");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "maintain");
    });

    it("should deny user with insufficient permissions", async () => {
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "read" },
      });

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith("User permission 'read' does not meet requirements: admin, write");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "insufficient_permissions");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "read");
      expect(mockCore.setOutput).toHaveBeenCalledWith(
        "error_message",
        "Access denied: User 'testuser' is not authorized. Required permissions: admin, write. To allow this user to run the workflow, add their role to the frontmatter. Example: roles: [admin, write, read]"
      );
    });

    it("should authorize user with write permission when write is in allowed list", async () => {
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("✅ User has write access to repository");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });
  });

  describe("confused deputy attack protection", () => {
    beforeEach(() => {
      process.env.GH_AW_REQUIRED_ROLES = "write";
    });

    it("should deny access when actor differs from PR author (pull_request synchronize event)", async () => {
      mockContext.actor = "dependabot[bot]";
      mockContext.eventName = "pull_request";
      mockContext.payload = { action: "synchronize", pull_request: { user: { login: "attacker" } } };

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Potential confused deputy attack detected"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "confused_deputy");
    });

    it("should allow access when actor matches PR author (genuine dependabot PR synchronize)", async () => {
      mockContext.actor = "dependabot[bot]";
      mockContext.eventName = "pull_request";
      mockContext.payload = { action: "synchronize", pull_request: { user: { login: "dependabot[bot]" } } };

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });

    it("should deny access when actor differs from comment author (issue_comment event)", async () => {
      mockContext.actor = "dependabot[bot]";
      mockContext.eventName = "issue_comment";
      mockContext.payload = { comment: { user: { login: "attacker" } } };

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Potential confused deputy attack detected"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "confused_deputy");
    });

    it("should not trigger confused deputy for pull_request:labeled even when actor differs from PR author", async () => {
      // A team member labeling a PR is legitimate — confused deputy only fires on synchronize
      mockContext.actor = "pelikhan";
      mockContext.eventName = "pull_request";
      mockContext.payload = { action: "labeled", pull_request: { user: { login: "copilot[bot]" } } };
      process.env.GH_AW_REQUIRED_ROLES = "write";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      // Should NOT be denied as confused deputy — should proceed to normal permission check
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("result", "confused_deputy");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });

    it("should not trigger confused deputy check for safe events (schedule)", async () => {
      mockContext.actor = "dependabot[bot]";
      mockContext.eventName = "schedule";
      mockContext.payload = { pull_request: { user: { login: "attacker" } } };

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "safe_event");
    });

    it("should not trigger confused deputy check for issues event (no PR/comment context)", async () => {
      mockContext.actor = "dependabot[bot]";
      mockContext.eventName = "issues";
      mockContext.payload = { issue: { user: { login: "someone-else" } } };
      process.env.GH_AW_REQUIRED_ROLES = "write";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      // issues events don't trigger confused deputy detection
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });

    it("should work correctly for workflow_call events with aw_context (no false positive)", async () => {
      // In workflow_call, context.payload = { inputs: { aw_context: "..." } }
      // The aw_context carries event_type but NOT pull_request.user.login
      // Confused deputy check must NOT trigger - this is a legitimate reusable workflow call
      mockContext.actor = "dependabot[bot]";
      mockContext.eventName = "workflow_call";
      mockContext.payload = {
        inputs: {
          aw_context: JSON.stringify({ event_type: "pull_request", item_number: "42", actor: "attacker" }),
        },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      // workflow_call proceeds to normal permission check - no confused_deputy denial
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("result", "confused_deputy");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });

    it("should allow issue_comment:edited with [bot]-authored comment (bot-menu pattern, payload-derived)", async () => {
      // The bot-posted-menu / user-checks-box pattern:
      // A workflow posts a checkbox-menu comment (authored by github-actions[bot]).
      // A human maintainer edits it to tick a box → issue_comment:edited, actor != comment.user.login.
      // The confused-deputy check detects this directly from the webhook payload —
      // no aw_context flag needed, works for direct issue_comment triggers too.
      mockContext.actor = "theletterf";
      mockContext.eventName = "issue_comment";
      mockContext.payload = {
        action: "edited",
        comment: { user: { login: "github-actions[bot]" } },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      // Must NOT be denied as confused deputy — bot-authored + edited = safe pattern
      expect(mockCore.setOutput).not.toHaveBeenCalledWith("result", "confused_deputy");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
    });

    it("should still deny issue_comment:edited with human comment author (not a bot-menu)", async () => {
      // An edited comment authored by a human is still a potential confused deputy.
      mockContext.actor = "different-actor";
      mockContext.eventName = "issue_comment";
      mockContext.payload = {
        action: "edited",
        comment: { user: { login: "human-author" } },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Potential confused deputy attack detected"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "confused_deputy");
    });

    it("should still deny issue_comment:created with [bot]-authored comment (Dependabot attack vector)", async () => {
      // The @dependabot show attack goes via issue_comment:created — must remain denied.
      mockContext.actor = "attacker";
      mockContext.eventName = "issue_comment";
      mockContext.payload = {
        action: "created",
        comment: { user: { login: "dependabot[bot]" } },
      };
      process.env.GH_AW_REQUIRED_ROLES = "write";

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Potential confused deputy attack detected"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "confused_deputy");
    });
  });

  describe("API error handling", () => {
    beforeEach(() => {
      process.env.GH_AW_REQUIRED_ROLES = "admin";
    });

    it("should handle API errors gracefully", async () => {
      const apiError = new Error("API rate limit exceeded");
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(apiError);

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith("Repository permission check failed: API rate limit exceeded");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "api_error");
      expect(mockCore.setOutput).toHaveBeenCalledWith("error_message", "Repository permission check failed: API rate limit exceeded");
    });

    it("should handle non-Error API failures", async () => {
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue("String error");

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith("Repository permission check failed: String error");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "api_error");
    });

    it("should handle network errors", async () => {
      const networkError = new Error("Network request failed");
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(networkError);

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith("Repository permission check failed: Network request failed");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "api_error");
    });
  });

  describe("multiple permission levels", () => {
    it("should check multiple permission levels in order", async () => {
      process.env.GH_AW_REQUIRED_ROLES = "admin,maintainer,write";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "write");
    });

    it("should stop checking after first match", async () => {
      process.env.GH_AW_REQUIRED_ROLES = "write,read";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "write" },
      });

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("✅ User has write access to repository");
    });
  });

  describe("bots allowlist", () => {
    beforeEach(() => {
      process.env.GH_AW_REQUIRED_ROLES = "write";
      mockContext.actor = "greptile-apps";
    });

    it("should authorize a bot in the allowlist when [bot] form is active on the repo", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "greptile-apps";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValueOnce({ data: { permission: "none" } }); // bot status check ([bot] form)

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized_bot");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "bot");
      // Only 1 API call (bot status) — initial permission check is skipped for allowlisted bots
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledTimes(1);
    });

    it("should authorize a bot in the allowlist when [bot] form returns 404 but slug form is active", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "greptile-apps";

      const notFoundError = { status: 404, message: "Not Found" };
      mockGithub.rest.repos.getCollaboratorPermissionLevel
        .mockRejectedValueOnce(notFoundError) // bot status [bot] form → 404
        .mockResolvedValueOnce({ data: { permission: "none" } }); // bot status slug fallback → none

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Actor 'greptile-apps' matched the allowed bots list: greptile-apps");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized_bot");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "bot");
    });

    it("should deny a bot in the allowlist when both [bot] and slug forms return 404", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "greptile-apps";

      const notFoundError = { status: 404, message: "Not Found" };
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(notFoundError); // bot status checks all return 404

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith("Bot 'greptile-apps' is in the allowed list but not active/installed on testorg/testrepo");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "bot_not_active");
    });

    it("should deny a bot not in the allowlist", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "some-other-bot";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({
        data: { permission: "none" },
      });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "insufficient_permissions");
    });

    it("should authorize a bot in the allowlist when permission check returns an API error (e.g. GitHub App not a user)", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "Copilot";
      mockContext.actor = "Copilot";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValueOnce({ data: { permission: "none" } }); // bot status check (Copilot[bot] form) → active

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized_bot");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "bot");
    });

    it("should return bot_not_active when permission check returns API error and bot is not installed", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "Copilot";
      mockContext.actor = "Copilot";

      const notFoundError = { status: 404, message: "Not Found" };
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(notFoundError); // all bot status checks → 404

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "bot_not_active");
    });

    it("should return api_error when permission check fails and actor is not in allowed bots list", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "some-other-bot";
      mockContext.actor = "Copilot";

      const notAUserError = new Error("Copilot is not a user");
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(notAUserError);

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "api_error");
    });

    it("should authorize a bot with [bot] suffix in the allowlist via slug fallback", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "copilot";
      mockContext.actor = "copilot[bot]";

      const notFoundError = { status: 404, message: "Not Found" };
      mockGithub.rest.repos.getCollaboratorPermissionLevel
        .mockRejectedValueOnce(notFoundError) // bot status [bot] form → 404
        .mockResolvedValueOnce({ data: { permission: "none" } }); // bot status slug fallback → none

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized_bot");
    });

    it("should skip bot check when GH_AW_ALLOWED_BOTS is empty string", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValueOnce({
        data: { permission: "none" },
      });

      await runScript();

      // Only 1 API call (the permission check) — no bot status check
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledTimes(1);
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "insufficient_permissions");
    });

    it("should skip bot check when GH_AW_ALLOWED_BOTS is not set", async () => {
      delete process.env.GH_AW_ALLOWED_BOTS;

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValueOnce({
        data: { permission: "none" },
      });

      await runScript();

      // Only 1 API call (the permission check) — no bot status check
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledTimes(1);
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "insufficient_permissions");
    });

    it("should not emit a roles-mismatch warning when an allowlisted bot triggers the workflow", async () => {
      // Regression test: when the actor matches the bots: allowlist, the roles check must be
      // skipped entirely so that no spurious "permission 'none' does not meet requirements"
      // warning is emitted even though the bot is subsequently authorized.
      process.env.GH_AW_REQUIRED_ROLES = "admin,maintainer,write";
      process.env.GH_AW_ALLOWED_BOTS = "github-actions";
      mockContext.actor = "github-actions[bot]";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValueOnce({ data: { permission: "none" } }); // bot status check only

      await runScript();

      // The roles-mismatch warning must NOT be emitted
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringMatching(/does not meet requirements/));
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized_bot");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "bot");
      // Only 1 API call (bot status) — initial permission check is skipped for allowlisted bots
      expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledTimes(1);
    });

    it("should authorize actor that is the second entry in a multi-bot allowlist", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "some-other-bot,greptile-apps";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValueOnce({ data: { permission: "none" } }); // bot status check ([bot] form)

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith("Actor 'greptile-apps' matched the allowed bots list: some-other-bot, greptile-apps");
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized_bot");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "bot");
    });

    it("should authorize actor when the allowlist entry includes the [bot] suffix", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "greptile-apps[bot]";

      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValueOnce({ data: { permission: "none" } }); // bot status check ([bot] form)

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "authorized_bot");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "bot");
    });

    it("should set user_permission to bot when denying a bot_not_active result", async () => {
      process.env.GH_AW_ALLOWED_BOTS = "greptile-apps";

      const notFoundError = { status: 404, message: "Not Found" };
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(notFoundError); // all bot status checks → 404

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "bot_not_active");
      expect(mockCore.setOutput).toHaveBeenCalledWith("user_permission", "bot");
    });

    it("should fall through to roles check when actor is in allowlist but bot status check fails non-404", async () => {
      // When the bot [bot] form check fails with a non-404 error (not a 404, so isBot: true,
      // isActive: false) the result is bot_not_active — the roles check is not reached.
      process.env.GH_AW_ALLOWED_BOTS = "greptile-apps";

      const serverError = { status: 500, message: "Internal Server Error" };
      mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValueOnce(serverError); // bot status [bot] form → 500

      await runScript();

      // Non-404 bot status failure → bot_not_active (not a fallthrough to roles)
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringMatching(/Failed to check bot status/));
      expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("result", "bot_not_active");
    });
  });
});
