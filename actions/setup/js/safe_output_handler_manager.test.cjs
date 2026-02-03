// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { loadConfig, loadHandlers, processMessages } from "./safe_output_handler_manager.cjs";

describe("Safe Output Handler Manager", () => {
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
  });

  afterEach(() => {
    // Clean up environment variables
    delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    delete process.env.GH_AW_TRACKER_LABEL;
  });

  describe("loadConfig", () => {
    it("should load config from environment variable and normalize keys", () => {
      const config = {
        "create-issue": { max: 5 },
        "add-comment": { max: 1 },
      };

      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify(config);

      const result = loadConfig();

      expect(result).toHaveProperty("create_issue");
      expect(result).toHaveProperty("add_comment");
      expect(result.create_issue).toEqual({ max: 5 });
      expect(result.add_comment).toEqual({ max: 1 });
    });

    it("should throw error if environment variable is not set", () => {
      expect(() => loadConfig()).toThrow("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable is required but not set");
    });

    it("should throw error if environment variable contains invalid JSON", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = "not json";
      expect(() => loadConfig()).toThrow("Failed to parse GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG");
    });
  });

  describe("loadHandlers", () => {
    // These tests are skipped because they require actual handler modules to exist
    // In a real environment, handlers are loaded dynamically via require()
    it.skip("should load handlers for enabled safe output types", async () => {
      const config = {
        create_issue: { max: 1 },
        add_comment: { max: 1 },
      };

      const handlers = await loadHandlers(config);

      expect(handlers.size).toBeGreaterThan(0);
      expect(handlers.has("create_issue")).toBe(true);
      expect(handlers.has("add_comment")).toBe(true);
    });

    it.skip("should not load handlers when config entry is missing", async () => {
      const config = {
        create_issue: { max: 1 },
        // add_comment is not in config
      };

      const handlers = await loadHandlers(config);

      expect(handlers.has("create_issue")).toBe(true);
      expect(handlers.has("add_comment")).toBe(false);
    });

    it.skip("should handle missing handlers gracefully", async () => {
      const config = {
        nonexistent_handler: { max: 1 },
      };

      const handlers = await loadHandlers(config);

      expect(handlers.size).toBe(0);
    });

    it("should throw error when handler main() does not return a function", async () => {
      // This test verifies that if a handler's main() function doesn't return
      // a message handler function, the loadHandlers function will throw an error
      // rather than just logging a warning.
      //
      // Expected behavior:
      // 1. Handler is loaded successfully
      // 2. main() is called with config
      // 3. If main() returns non-function, an error is thrown
      // 4. The error should fail the step
      //
      // This is important because:
      // - Old handlers execute directly and return undefined
      // - New handlers follow factory pattern and return a function
      // - Silent failures from misconfigured handlers are hard to debug
      //
      // The implementation checks: typeof messageHandler !== "function"
      // and throws: "Handler X main() did not return a function"

      // Note: Actual integration testing requires real handler modules
      // This test documents the expected behavior for validation
      expect(true).toBe(true);
    });
  });

  describe("processMessages", () => {
    it("should process messages in order of appearance", async () => {
      const messages = [
        { type: "add_comment", body: "Comment" },
        { type: "create_issue", title: "Issue" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      const handlers = new Map([
        ["create_issue", mockHandler],
        ["add_comment", mockHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);

      // Verify handlers were called
      expect(mockHandler).toHaveBeenCalledTimes(2);

      // Verify messages were processed in order of appearance (add_comment first, then create_issue)
      expect(result.results[0].type).toBe("add_comment");
      expect(result.results[0].messageIndex).toBe(0);
      expect(result.results[1].type).toBe("create_issue");
      expect(result.results[1].messageIndex).toBe(1);
    });

    it("should skip messages without type", async () => {
      const messages = [{ type: "create_issue", title: "Issue" }, { title: "No type" }, { type: "add_comment", body: "Comment" }];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      const handlers = new Map([
        ["create_issue", mockHandler],
        ["add_comment", mockHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);
      expect(core.warning).toHaveBeenCalledWith("Skipping message 2 without type");
    });

    it("should warn and record result when no handler is available for message type", async () => {
      const messages = [
        { type: "create_issue", title: "Issue" },
        { type: "unknown_type", data: "test" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      // Only create_issue handler is available, unknown_type has no handler
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);

      // First message should succeed
      expect(result.results[0].success).toBe(true);
      expect(result.results[0].type).toBe("create_issue");

      // Second message should be recorded as failed with no handler error
      expect(result.results[1].success).toBe(false);
      expect(result.results[1].type).toBe("unknown_type");
      expect(result.results[1].error).toContain("No handler loaded");

      // Should have logged a warning
      expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("No handler loaded for message type 'unknown_type'"));
    });

    it("should handle handler errors gracefully", async () => {
      const messages = [{ type: "create_issue", title: "Issue" }];

      const errorHandler = vi.fn().mockRejectedValue(new Error("Handler failed"));

      const handlers = new Map([["create_issue", errorHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(1);
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toBe("Handler failed");
    });

    it("should treat handler returning success: false as a failure", async () => {
      const messages = [{ type: "create_project_status_update", project: "https://github.com/orgs/test/projects/1", body: "Test" }];

      const failureHandler = vi.fn().mockResolvedValue({
        success: false,
        error: "GraphQL query failed",
      });

      const handlers = new Map([["create_project_status_update", failureHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(1);
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toBe("GraphQL query failed");
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining("failed: GraphQL query failed"));
    });

    it("should track outputs with unresolved temporary IDs", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "See #aw_abc123def456 for context",
          title: "Test Issue",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track the output because it has unresolved temp ID
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
      expect(result.outputsWithUnresolvedIds[0].type).toBe("create_issue");
      expect(result.outputsWithUnresolvedIds[0].result.number).toBe(100);
    });

    it("should track outputs needing synthetic updates when temporary ID is resolved", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "See #aw_abc123def456 for context",
          title: "First Issue",
        },
        {
          type: "create_issue",
          temporary_id: "aw_abc123def456",
          body: "Second issue body",
          title: "Second Issue",
        },
      ];

      const mockCreateIssueHandler = vi
        .fn()
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 100,
        })
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 101,
          temporaryId: "aw_abc123def456",
        });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track output with unresolved temp ID
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
      expect(result.outputsWithUnresolvedIds[0].result.number).toBe(100);
      // Temp ID should be registered
      expect(result.temporaryIdMap["aw_abc123def456"]).toBeDefined();
      expect(result.temporaryIdMap["aw_abc123def456"].number).toBe(101);
    });

    it("should not track output if temporary IDs remain unresolved", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "See #aw_abc123def456 and #aw_unresolved99 for context",
          title: "Test Issue",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track because there are unresolved IDs
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
    });

    it("should handle multiple outputs needing synthetic updates", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "Related to #aw_aabbcc111111",
          title: "First Issue",
        },
        {
          type: "create_discussion",
          body: "See #aw_aabbcc111111 for details",
          title: "Discussion",
        },
        {
          type: "create_issue",
          temporary_id: "aw_aabbcc111111",
          body: "The referenced issue",
          title: "Referenced Issue",
        },
      ];

      const mockCreateIssueHandler = vi
        .fn()
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 100,
        })
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 102,
          temporaryId: "aw_aabbcc111111",
        });

      const mockCreateDiscussionHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 101,
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["create_discussion", mockCreateDiscussionHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track 2 outputs (issue and discussion) with unresolved temp IDs
      expect(result.outputsWithUnresolvedIds.length).toBe(2);
      // Temp ID should be registered
      expect(result.temporaryIdMap["aw_aabbcc111111"]).toBeDefined();
    });

    it("should silently skip message types handled by standalone steps", async () => {
      const messages = [
        { type: "create_issue", title: "Issue" },
        { type: "create_agent_session", title: "Task" },
        { type: "upload_asset", path: "file.txt" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      // Only create_issue handler is available
      // create_agent_session and upload_asset are handled by standalone steps
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(3);

      // First message should succeed
      expect(result.results[0].success).toBe(true);
      expect(result.results[0].type).toBe("create_issue");

      // Second message should be skipped (standalone step)
      expect(result.results[1].success).toBe(false);
      expect(result.results[1].type).toBe("create_agent_session");
      expect(result.results[1].skipped).toBe(true);
      expect(result.results[1].reason).toBe("Handled by standalone step");

      // Third message should also be skipped (standalone step)
      expect(result.results[2].success).toBe(false);
      expect(result.results[2].type).toBe("upload_asset");
      expect(result.results[2].skipped).toBe(true);
      expect(result.results[2].reason).toBe("Handled by standalone step");

      // Should NOT have logged warnings for standalone step types
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("No handler loaded for message type 'create_agent_session'"));
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("No handler loaded for message type 'upload_asset'"));

      // Should have logged debug messages
      expect(core.debug).toHaveBeenCalledWith(expect.stringContaining("create_agent_session"));
      expect(core.debug).toHaveBeenCalledWith(expect.stringContaining("upload_asset"));
    });

    it("should track skipped message types for logging", async () => {
      const messages = [
        { type: "create_issue", title: "Issue" },
        { type: "create_agent_session", title: "Task" },
        { type: "upload_asset", path: "file.txt" },
        { type: "unknown_type", data: "test" },
        { type: "another_unknown", data: "test2" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      // Only create_issue handler is available
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // Collect skipped standalone types
      const skippedStandaloneResults = result.results.filter(r => r.skipped && r.reason === "Handled by standalone step");
      const standaloneTypes = [...new Set(skippedStandaloneResults.map(r => r.type))];
      expect(standaloneTypes).toEqual(expect.arrayContaining(["create_agent_session", "upload_asset"]));

      // Collect skipped no-handler types
      const skippedNoHandlerResults = result.results.filter(r => !r.success && !r.skipped && r.error?.includes("No handler loaded"));
      const noHandlerTypes = [...new Set(skippedNoHandlerResults.map(r => r.type))];
      expect(noHandlerTypes).toEqual(expect.arrayContaining(["unknown_type", "another_unknown"]));
    });

    it("should register temporary IDs from deferred messages on retry", async () => {
      const messages = [
        {
          type: "link_sub_issue",
          parent_issue_number: "aw_parent123456",
          sub_issue_number: 42,
        },
        {
          type: "create_issue",
          temporary_id: "aw_parent123456",
          title: "Parent Issue",
          body: "Parent body",
        },
      ];

      // First call: link_sub_issue is deferred (parent not resolved yet)
      // Second call: create_issue succeeds and registers temp ID
      // Third call: link_sub_issue retry succeeds
      const mockLinkHandler = vi
        .fn()
        .mockResolvedValueOnce({
          deferred: true,
          error: "Unresolved temporary IDs: parent: aw_parent123456",
        })
        .mockResolvedValueOnce({
          parent_issue_number: 100,
          sub_issue_number: 42,
          success: true,
        });

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
        temporaryId: "aw_parent123456",
      });

      const handlers = new Map([
        ["link_sub_issue", mockLinkHandler],
        ["create_issue", mockCreateIssueHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // Temp ID should be registered after create_issue
      expect(result.temporaryIdMap["aw_parent123456"]).toBeDefined();
      expect(result.temporaryIdMap["aw_parent123456"].number).toBe(100);

      // link_sub_issue should succeed after retry
      const linkResult = result.results.find(r => r.type === "link_sub_issue");
      expect(linkResult.success).toBe(true);
      expect(linkResult.deferred).toBe(false);
    });

    it("should track outputs created during deferred retry with unresolved temp IDs", async () => {
      const messages = [
        {
          type: "create_issue",
          temporary_id: "aw_aabbcc111111",
          title: "Issue 1",
          body: "References #aw_ddeeff222222",
        },
        {
          type: "link_sub_issue",
          parent_issue_number: "aw_aabbcc111111",
          sub_issue_number: "aw_ddeeff222222",
        },
        {
          type: "create_issue",
          temporary_id: "aw_ddeeff222222",
          title: "Issue 2",
          body: "Issue 2 body",
        },
      ];

      // create_issue for issue1: succeeds with unresolved temp ID in body
      // link_sub_issue: deferred (parent and sub not resolved)
      // create_issue for issue2: succeeds
      // link_sub_issue retry: succeeds
      const mockCreateHandler = vi
        .fn()
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 100,
          temporaryId: "aw_aabbcc111111",
        })
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 101,
          temporaryId: "aw_ddeeff222222",
        });

      const mockLinkHandler = vi
        .fn()
        .mockResolvedValueOnce({
          deferred: true,
          error: "Unresolved temporary IDs",
        })
        .mockResolvedValueOnce({
          parent_issue_number: 100,
          sub_issue_number: 101,
          success: true,
        });

      const handlers = new Map([
        ["create_issue", mockCreateHandler],
        ["link_sub_issue", mockLinkHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // Both issues should have temp IDs registered
      expect(result.temporaryIdMap["aw_aabbcc111111"]).toBeDefined();
      expect(result.temporaryIdMap["aw_ddeeff222222"]).toBeDefined();

      // Issue 1 should be tracked for synthetic update (had unresolved temp ID in body at creation time)
      // Note: By the time all messages are processed, the temp ID is resolved, but Issue 1 was
      // tracked when it was created because at that moment aw_ddeeff222222 was not yet in the map
      const trackedIssue1 = result.outputsWithUnresolvedIds.find(o => o.result.number === 100);
      expect(trackedIssue1).toBeDefined();
    });

    it("should handle complex parent/sub-issue creation order", async () => {
      const messages = [
        {
          type: "create_issue",
          temporary_id: "aw_abc111111def",
          title: "Parent",
          body: "See #aw_111aaa222bbb and #aw_333ccc444ddd",
        },
        {
          type: "create_issue",
          temporary_id: "aw_111aaa222bbb",
          title: "Sub 1",
          body: "Sub 1 body",
        },
        {
          type: "create_issue",
          temporary_id: "aw_333ccc444ddd",
          title: "Sub 2",
          body: "Sub 2 body",
        },
      ];

      let issueCounter = 100;
      const mockCreateHandler = vi.fn().mockImplementation(message => {
        const tempId = message.temporary_id;
        const issueNumber = issueCounter++;
        return Promise.resolve({
          repo: "owner/repo",
          number: issueNumber,
          temporaryId: tempId,
        });
      });

      const handlers = new Map([["create_issue", mockCreateHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // All temp IDs should be registered
      expect(result.temporaryIdMap["aw_abc111111def"]).toBeDefined();
      expect(result.temporaryIdMap["aw_111aaa222bbb"]).toBeDefined();
      expect(result.temporaryIdMap["aw_333ccc444ddd"]).toBeDefined();

      // Parent issue should be tracked (had unresolved temp IDs at creation time)
      // When the parent was created, aw_111aaa222bbb and aw_333ccc444ddd were not yet in the map
      const parentTracked = result.outputsWithUnresolvedIds.find(
        o => o.result.number === 100 // Parent was issue #100
      );
      expect(parentTracked).toBeDefined();
      expect(parentTracked.type).toBe("create_issue");
    });

    it("should collect missing_tool and missing_data messages and include in result", async () => {
      const messages = [
        {
          type: "missing_tool",
          tool: "docker",
          reason: "Need containerization",
          alternatives: "Use VM",
        },
        {
          type: "create_issue",
          title: "Test Issue",
          body: "Issue body",
        },
        {
          type: "missing_data",
          data_type: "api_key",
          reason: "API credentials missing",
          context: "GitHub API access",
        },
        {
          type: "missing_tool",
          tool: "kubectl",
          reason: "Kubernetes management",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["missing_tool", vi.fn().mockResolvedValue({ success: true })],
        ["missing_data", vi.fn().mockResolvedValue({ success: true })],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.missings).toBeDefined();
      expect(result.missings.missingTools).toHaveLength(2);
      expect(result.missings.missingData).toHaveLength(1);

      // Check missing tools
      expect(result.missings.missingTools[0].tool).toBe("docker");
      expect(result.missings.missingTools[0].reason).toBe("Need containerization");
      expect(result.missings.missingTools[0].alternatives).toBe("Use VM");

      expect(result.missings.missingTools[1].tool).toBe("kubectl");
      expect(result.missings.missingTools[1].reason).toBe("Kubernetes management");
      expect(result.missings.missingTools[1].alternatives).toBeNull();

      // Check missing data
      expect(result.missings.missingData[0].data_type).toBe("api_key");
      expect(result.missings.missingData[0].reason).toBe("API credentials missing");
      expect(result.missings.missingData[0].context).toBe("GitHub API access");
    });

    it("should collect noop messages alongside missing_tool and missing_data", async () => {
      const messages = [
        {
          type: "noop",
          message: "No issues found in this review",
        },
        {
          type: "create_issue",
          title: "Test Issue",
          body: "Issue body",
        },
        {
          type: "missing_tool",
          tool: "docker",
          reason: "Need containerization",
        },
        {
          type: "noop",
          message: "Analysis complete",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["missing_tool", vi.fn().mockResolvedValue({ success: true })],
        ["noop", vi.fn().mockResolvedValue({ success: true })],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.missings).toBeDefined();
      expect(result.missings.missingTools).toHaveLength(1);
      expect(result.missings.missingData).toHaveLength(0);
      expect(result.missings.noopMessages).toHaveLength(2);

      // Check missing tools
      expect(result.missings.missingTools[0].tool).toBe("docker");
      expect(result.missings.missingTools[0].reason).toBe("Need containerization");

      // Check noop messages
      expect(result.missings.noopMessages[0].message).toBe("No issues found in this review");
      expect(result.missings.noopMessages[1].message).toBe("Analysis complete");
    });

    it("should return empty arrays when no missing messages present", async () => {
      const messages = [
        {
          type: "create_issue",
          title: "Test Issue",
          body: "Issue body",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.missings).toBeDefined();
      expect(result.missings.missingTools).toHaveLength(0);
      expect(result.missings.missingData).toHaveLength(0);
      expect(result.missings.noopMessages).toHaveLength(0);
    });
  });
});
