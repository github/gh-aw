// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";
import { loadConfig, setupProjectGitHubClient, processMessages } from "./safe_output_unified_handler_manager.cjs";
import { resolveIssueNumber } from "./temporary_id.cjs";

// Mock @actions/github
vi.mock("@actions/github", () => ({
  getOctokit: vi.fn(() => ({
    graphql: vi.fn(),
    request: vi.fn(),
    rest: {},
  })),
}));

describe("Unified Safe Output Handler Manager", () => {
  beforeEach(() => {
    // Mock global core
    global.core = {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
    };

    // Mock global context
    global.context = {
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
      payload: {},
    };

    // Clean up environment variables
    delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    delete process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG;
    delete process.env.GH_AW_PROJECT_GITHUB_TOKEN;
    delete process.env.GH_AW_TEMPORARY_ID_MAP;
  });

  describe("processMessages chaining scenarios", () => {
    it("processes dependencies first even if messages are out of order", async () => {
      const createIssueHandler = vi.fn(async message => {
        // Should be normalized by the dispatcher before handler invocation.
        expect(message.temporary_id).toBe("aw_deadbe");
        return { repo: "testowner/testrepo", number: 123, temporaryId: message.temporary_id };
      });

      const addCommentHandler = vi.fn(async (_message, resolvedTemporaryIds) => {
        expect(resolvedTemporaryIds).toHaveProperty("aw_deadbe");
        expect(resolvedTemporaryIds.aw_deadbe).toEqual({ repo: "testowner/testrepo", number: 123 });
        return { ok: true };
      });

      const handlers = new Map([
        ["create_issue", createIssueHandler],
        ["add_comment", addCommentHandler],
      ]);

      // Intentionally out-of-order: comment references temp ID created by create_issue.
      const messages = [
        {
          type: "add_comment",
          repo: "testowner/testrepo",
          issue_number: "#aw_deadbe",
          body: "See #aw_deadbe for details",
        },
        {
          type: "create_issue",
          repo: "testowner/testrepo",
          title: "Create first",
          body: "Body",
          temporary_id: "aw_deadbe",
        },
      ];

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(createIssueHandler).toHaveBeenCalledTimes(1);
      expect(addCommentHandler).toHaveBeenCalledTimes(1);
      expect(result.temporaryIdMap).toHaveProperty("aw_deadbe");
      expect(result.temporaryIdMap.aw_deadbe).toEqual({ repo: "testowner/testrepo", number: 123 });
    });

    it("normalizes temporaryId camelCase + # prefix + casing to strict lowercase", async () => {
      const createIssueHandler = vi.fn(async message => {
        expect(message.temporary_id).toBe("aw_abc123");
        return { repo: "testowner/testrepo", number: 321, temporaryId: message.temporary_id };
      });

      const handlers = new Map([["create_issue", createIssueHandler]]);
      const messages = [
        {
          type: "create_issue",
          repo: "testowner/testrepo",
          title: "Normalize",
          body: "Body",
          temporaryId: "  #AW_ABC123  ",
        },
      ];

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(createIssueHandler).toHaveBeenCalledTimes(1);
      expect(result.temporaryIdMap).toHaveProperty("aw_abc123");
      expect(result.temporaryIdMap.aw_abc123).toEqual({ repo: "testowner/testrepo", number: 321 });
    });

    it("rejects malformed temporary_id before invoking handler", async () => {
      const createIssueHandler = vi.fn(async () => ({ repo: "testowner/testrepo", number: 1, temporaryId: "aw_deadbe" }));

      const handlers = new Map([["create_issue", createIssueHandler]]);
      const messages = [
        {
          type: "create_issue",
          repo: "testowner/testrepo",
          title: "Bad temp ID",
          body: "Body",
          temporary_id: "aw_bundle_npm001_invalid",
        },
      ];

      const result = await processMessages(handlers, messages);

      expect(createIssueHandler).not.toHaveBeenCalled();
      expect(result.results).toHaveLength(1);
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toMatch(/invalid temporary_id/i);
    });

    it("chains update_project draft mapping across multiple messages", async () => {
      const updateProjectHandler = vi.fn(async (message, temporaryIdMap) => {
        if (message.operation === "create") {
          expect(temporaryIdMap.size).toBe(0);
          return { temporaryId: "aw_deadbe", draftItemId: "draft-item-1" };
        }

        expect(temporaryIdMap.get("aw_deadbe")).toEqual({ draftItemId: "draft-item-1" });
        return { ok: true };
      });

      const handlers = new Map([["update_project", updateProjectHandler]]);
      const messages = [
        { type: "update_project", operation: "create", temporary_id: "aw_deadbe" },
        { type: "update_project", operation: "update", draft_issue_id: "#aw_deadbe" },
      ];

      const result = await processMessages(handlers, messages, {});

      expect(result.success).toBe(true);
      expect(updateProjectHandler).toHaveBeenCalledTimes(2);
      expect(result.temporaryIdMap).toHaveProperty("aw_deadbe");
      expect(result.temporaryIdMap.aw_deadbe).toEqual({ draftItemId: "draft-item-1" });
    });

    it("chains create_issue → update_project via content_number temporary ID", async () => {
      const createIssueHandler = vi.fn(async message => {
        expect(message.temporary_id).toBe("aw_deadbe");
        return { repo: "testowner/testrepo", number: 456, temporaryId: message.temporary_id };
      });

      const updateProjectHandler = vi.fn(async (message, temporaryIdMap) => {
        // Ensure the mapping exists in the live Map for project handlers.
        expect(temporaryIdMap.get("aw_deadbe")).toEqual({ repo: "testowner/testrepo", number: 456 });

        // And validate that project handlers can resolve it using shared helper.
        const resolved = resolveIssueNumber(message.content_number, temporaryIdMap);
        expect(resolved.errorMessage).toBeNull();
        expect(resolved.wasTemporaryId).toBe(true);
        expect(resolved.resolved).toEqual({ repo: "testowner/testrepo", number: 456 });

        return { ok: true };
      });

      const handlers = new Map([
        ["create_issue", createIssueHandler],
        ["update_project", updateProjectHandler],
      ]);

      // Intentionally out-of-order: update_project references temp ID created by create_issue.
      const messages = [
        {
          type: "update_project",
          project: "https://github.com/orgs/testowner/projects/60",
          content_type: "issue",
          content_number: "#aw_deadbe",
        },
        {
          type: "create_issue",
          repo: "testowner/testrepo",
          title: "Create an issue",
          body: "Body",
          temporary_id: "aw_deadbe",
        },
      ];

      const result = await processMessages(handlers, messages, {});

      expect(result.success).toBe(true);
      expect(createIssueHandler).toHaveBeenCalledTimes(1);
      expect(updateProjectHandler).toHaveBeenCalledTimes(1);
      expect(result.temporaryIdMap.aw_deadbe).toEqual({ repo: "testowner/testrepo", number: 456 });
    });
  });

  describe("loadConfig", () => {
    it("should load regular handler config", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        create_issue: { max: 5 },
        add_comment: {},
      });

      const config = loadConfig();

      expect(config).toHaveProperty("regular");
      expect(config).toHaveProperty("project");
      expect(config.regular).toHaveProperty("create_issue");
      expect(config.regular.create_issue).toEqual({ max: 5 });
      expect(config.regular).toHaveProperty("add_comment");
    });

    it("should load project handler config", () => {
      process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG = JSON.stringify({
        create_project: { max: 1 },
        update_project: { max: 100 },
      });

      const config = loadConfig();

      expect(config).toHaveProperty("project");
      expect(config.project).toHaveProperty("create_project");
      expect(config.project.create_project).toEqual({ max: 1 });
      expect(config.project).toHaveProperty("update_project");
    });

    it("should load both regular and project configs", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        create_issue: { max: 5 },
      });
      process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG = JSON.stringify({
        create_project: { max: 1 },
      });

      const config = loadConfig();

      expect(config.regular).toHaveProperty("create_issue");
      expect(config.project).toHaveProperty("create_project");
    });

    it("should throw error if no config is provided", () => {
      expect(() => loadConfig()).toThrow(/At least one of .* is required/);
    });

    it("should normalize hyphenated keys to underscores", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        "create-issue": { max: 5 },
      });

      const config = loadConfig();

      expect(config.regular).toHaveProperty("create_issue");
      expect(config.regular).not.toHaveProperty("create-issue");
    });

    it("should automatically split project handlers from unified config", () => {
      // Simulate Go compiler putting all handlers in one config
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        create_issue: { max: 5 },
        add_comment: {},
        update_project: { max: 20, project: "https://github.com/orgs/test/projects/1" },
        create_project: { max: 1 },
        create_project_status_update: { max: 1, project: "https://github.com/orgs/test/projects/1" },
      });

      const config = loadConfig();

      // Regular handlers should stay in regular config
      expect(config.regular).toHaveProperty("create_issue");
      expect(config.regular).toHaveProperty("add_comment");
      expect(config.regular).not.toHaveProperty("update_project");
      expect(config.regular).not.toHaveProperty("create_project");
      expect(config.regular).not.toHaveProperty("create_project_status_update");

      // Project handlers should be moved to project config
      expect(config.project).toHaveProperty("update_project");
      expect(config.project).toHaveProperty("create_project");
      expect(config.project).toHaveProperty("create_project_status_update");
      expect(config.project.update_project).toEqual({ max: 20, project: "https://github.com/orgs/test/projects/1" });
    });

    it("should handle hyphenated project handler names and split correctly", () => {
      // Test with hyphenated names (common in YAML/JSON configs)
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        "create-issue": { max: 5 },
        "update-project": { max: 20 },
        "create-project-status-update": { max: 1 },
      });

      const config = loadConfig();

      // Check normalization and splitting
      expect(config.regular).toHaveProperty("create_issue");
      expect(config.regular).not.toHaveProperty("update_project");
      expect(config.project).toHaveProperty("update_project");
      expect(config.project).toHaveProperty("create_project_status_update");
    });

    it("should prioritize explicit project config over auto-split handlers", () => {
      // Both configs provided - explicit project config should take precedence
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        create_issue: { max: 5 },
        update_project: { max: 20, project: "url1" },
      });
      process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG = JSON.stringify({
        update_project: { max: 50, project: "url2" }, // Should override auto-split config
        create_project: { max: 1 },
      });

      const config = loadConfig();

      expect(config.regular).toHaveProperty("create_issue");
      expect(config.project).toHaveProperty("update_project");
      expect(config.project).toHaveProperty("create_project");
      // Explicit config should take precedence
      expect(config.project.update_project).toEqual({ max: 50, project: "url2" });
    });

    it("should handle smoke-project workflow scenario correctly", () => {
      // Simulate the exact config from smoke-project workflow
      // where Go compiler puts all handlers (including project handlers) in one config
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        add_comment: { hide_older_comments: true, max: 2 },
        add_labels: { allowed: ["smoke-project"], "target-repo": "github-agentic-workflows/demo-repository" },
        create_issue: { close_older_issues: true, expires: 2, group: true, max: 1, "target-repo": "github-agentic-workflows/demo-repository" },
        create_project_status_update: { "github-token": "***", max: 1, project: "https://github.com/orgs/github-agentic-workflows/projects/1" },
        missing_data: {},
        missing_tool: {},
        remove_labels: { allowed: ["smoke-project"], "target-repo": "github-agentic-workflows/demo-repository" },
        update_project: {
          "github-token": "***",
          max: 20,
          project: "https://github.com/orgs/github-agentic-workflows/projects/1",
          views: [
            { name: "Smoke Test Board", layout: "board", filter: "is:open" },
            { name: "Smoke Test Table", layout: "table" },
          ],
        },
      });

      const config = loadConfig();

      // Regular handlers should stay in regular config
      expect(config.regular).toHaveProperty("add_comment");
      expect(config.regular).toHaveProperty("add_labels");
      expect(config.regular).toHaveProperty("create_issue");
      expect(config.regular).toHaveProperty("missing_data");
      expect(config.regular).toHaveProperty("missing_tool");
      expect(config.regular).toHaveProperty("remove_labels");

      // Project handlers should be automatically moved to project config
      expect(config.project).toHaveProperty("update_project");
      expect(config.project).toHaveProperty("create_project_status_update");

      // Regular handlers should NOT contain project handlers
      expect(config.regular).not.toHaveProperty("update_project");
      expect(config.regular).not.toHaveProperty("create_project_status_update");

      // Verify project handler configs are intact
      expect(config.project.update_project).toHaveProperty("max", 20);
      expect(config.project.update_project).toHaveProperty("project");
      expect(config.project.update_project).toHaveProperty("views");
      expect(config.project.create_project_status_update).toHaveProperty("max", 1);
    });
  });

  describe("setupProjectGitHubClient", () => {
    it("should throw error if GH_AW_PROJECT_GITHUB_TOKEN is not set", async () => {
      await expect(setupProjectGitHubClient()).rejects.toThrow(/GH_AW_PROJECT_GITHUB_TOKEN environment variable is required/);
    });

    it("should create Octokit instance when token is provided", async () => {
      process.env.GH_AW_PROJECT_GITHUB_TOKEN = "test-project-token";

      const octokit = await setupProjectGitHubClient();

      expect(octokit).toBeDefined();
      expect(octokit).toHaveProperty("graphql");
      expect(octokit).toHaveProperty("request");
    });
  });
});
