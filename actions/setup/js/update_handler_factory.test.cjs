import { describe, it, expect, beforeEach, vi } from "vitest";

// Import the factory function
let factoryModule;

// Mock the global objects that GitHub Actions provides
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

const mockGithub = {
  rest: {
    issues: {
      update: vi.fn(),
    },
  },
};

const mockContext = {
  eventName: "issues",
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
  serverUrl: "https://github.com",
  runId: 12345,
  payload: {
    issue: {
      number: 42,
    },
  },
};

// Set up global mocks
global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

describe("update_handler_factory.cjs", () => {
  beforeEach(async () => {
    // Reset all mocks before each test
    vi.clearAllMocks();
    vi.resetModules();

    // Import the module fresh for each test
    factoryModule = await import("./update_handler_factory.cjs");
  });

  describe("createUpdateHandlerFactory", () => {
    it("should create a handler factory with default configuration", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({ success: true, data: { title: "Test" } });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com", title: "Test" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true, number: 42 });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      // Create handler with default config
      const handler = await handlerFactory({});

      // Execute handler
      const result = await handler({ title: "Test" });

      // Verify default configuration was logged
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("max=10"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("target=triggering"));

      // Verify handler was successful
      expect(result.success).toBe(true);
      expect(mockResolveItemNumber).toHaveBeenCalled();
      expect(mockBuildUpdateData).toHaveBeenCalled();
      expect(mockExecuteUpdate).toHaveBeenCalled();
      expect(mockFormatSuccessResult).toHaveBeenCalled();
    });

    it("should respect custom max count configuration", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({ success: true, data: { title: "Test" } });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      // Create handler with max=2
      const handler = await handlerFactory({ max: 2 });

      // Process 2 messages (should succeed)
      const result1 = await handler({ title: "Test 1" });
      expect(result1.success).toBe(true);

      const result2 = await handler({ title: "Test 2" });
      expect(result2.success).toBe(true);

      // Third message should be rejected due to max count
      const result3 = await handler({ title: "Test 3" });
      expect(result3.success).toBe(false);
      expect(result3.error).toContain("Max count of 2 reached");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("max count of 2 reached"));
    });

    it("should handle resolution errors gracefully", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({
        success: false,
        error: "Resolution failed",
      });
      const mockBuildUpdateData = vi.fn();
      const mockExecuteUpdate = vi.fn();
      const mockFormatSuccessResult = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(false);
      expect(result.error).toBe("Resolution failed");
      expect(mockCore.warning).toHaveBeenCalledWith("Resolution failed");
      // Should not proceed to build/execute
      expect(mockBuildUpdateData).not.toHaveBeenCalled();
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should handle build errors gracefully", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({
        success: false,
        error: "No fields to update",
      });
      const mockExecuteUpdate = vi.fn();
      const mockFormatSuccessResult = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(false);
      expect(result.error).toBe("No fields to update");
      expect(mockCore.warning).toHaveBeenCalledWith("No fields to update");
      // Should not proceed to execute
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should handle empty update data as no-op", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({ success: true, data: {} });
      const mockExecuteUpdate = vi.fn();
      const mockFormatSuccessResult = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(result.reason).toBe("No update fields provided");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No update fields provided"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("treating as no-op"));
      // Should not proceed to execute
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should ignore internal fields starting with underscore", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({
        success: true,
        data: { _internal: "value", title: "Test" },
      });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining('["title"]'));
    });

    it("should NOT skip when _rawBody is present (body updates)", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({
        success: true,
        data: { _rawBody: "New body content", _operation: "append" },
      });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_issue",
        itemTypeName: "issue",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({});
      const result = await handler({ body: "New body content" });

      // Should NOT skip - _rawBody indicates a body update
      expect(result.success).toBe(true);
      expect(result.skipped).toBeUndefined();
      // Should proceed to execute the update
      expect(mockExecuteUpdate).toHaveBeenCalled();
    });

    it("should sanitize _rawBody content before passing to executeUpdate", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      // Body content with a mention that sanitizeContent will neutralize (wraps in backticks)
      const unsafeBody = "/run-command unsafe content @bot-trigger";
      const mockBuildUpdateData = vi.fn().mockReturnValue({
        success: true,
        data: { _rawBody: unsafeBody, _operation: "replace" },
      });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_issue",
        itemTypeName: "issue",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({});
      const result = await handler({ body: unsafeBody });

      expect(result.success).toBe(true);
      // executeUpdate must be called with sanitized body content
      expect(mockExecuteUpdate).toHaveBeenCalled();
      const passedUpdateData = mockExecuteUpdate.mock.calls[0][3];
      // ensure the body content was changed by the sanitizer
      expect(passedUpdateData._rawBody).not.toBe(unsafeBody);
    });

    it("should handle execution errors gracefully", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({ success: true, data: { title: "Test" } });
      const mockExecuteUpdate = vi.fn().mockRejectedValue(new Error("API Error"));
      const mockFormatSuccessResult = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(false);
      expect(result.error).toContain("API Error");
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to update test item"));
    });

    it("should attach execution metadata when capture hooks are configured", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({ success: true, data: { title: "Test" } });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com/issues/42", title: "Updated title" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true, number: 42, url: "https://example.com/issues/42" });
      const captureBefore = vi.fn().mockResolvedValue({ title: "Before title" });
      const captureAfter = vi.fn().mockResolvedValue({ title: "After title" });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
        captureExecutionMetadata: {
          captureBefore,
          captureAfter,
        },
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(true);
      expect(result.before_state).toEqual({ title: "Before title" });
      expect(result.after_state).toEqual({ title: "After title" });
      expect(result.repo).toBe("testowner/testrepo");
      expect(captureBefore).toHaveBeenCalled();
      expect(captureAfter).toHaveBeenCalledWith({ html_url: "https://example.com/issues/42", title: "Updated title" }, { title: "Before title" }, expect.objectContaining({ title: "Test" }));
    });

    it("should pass additional config to log message", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({ success: true, data: { title: "Test" } });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
        additionalConfig: {
          allow_title: true,
          allow_body: true,
        },
      });

      // Create handler with additional config
      const handler = await handlerFactory({ allow_title: false, allow_body: true });

      // Verify additional config items in log
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("allow_title=false"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("allow_body=true"));
    });

    it("should track processed count across multiple calls", async () => {
      const mockResolveItemNumber = vi.fn().mockReturnValue({ success: true, number: 42 });
      const mockBuildUpdateData = vi.fn().mockReturnValue({ success: true, data: { title: "Test" } });
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com" });
      const mockFormatSuccessResult = vi.fn().mockReturnValue({ success: true });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: mockResolveItemNumber,
        buildUpdateData: mockBuildUpdateData,
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: mockFormatSuccessResult,
      });

      const handler = await handlerFactory({ max: 3 });

      // First call should succeed
      const result1 = await handler({ title: "Test 1" });
      expect(result1.success).toBe(true);

      // Second call should succeed
      const result2 = await handler({ title: "Test 2" });
      expect(result2.success).toBe(true);

      // Third call should succeed
      const result3 = await handler({ title: "Test 3" });
      expect(result3.success).toBe(true);

      // Fourth call should fail due to max count
      const result4 = await handler({ title: "Test 4" });
      expect(result4.success).toBe(false);
      expect(result4.error).toContain("Max count of 3 reached");
    });
  });

  describe("createStandardResolveNumber", () => {
    it("UI-001 defaults an omitted target to the triggering issue", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const result = resolveNumber({ issue_number: 99 }, undefined, mockContext);

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
    });

    it("UI-002 ignores a conflicting issue_number when target is triggering", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const item = { issue_number: 99 };
      const updateTarget = "triggering";
      const context = mockContext;

      const result = resolveNumber(item, updateTarget, context);

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
    });

    it("UI-003 ignores a conflicting issue_number when target is fixed", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const result = resolveNumber({ issue_number: 99 }, "17", mockContext);

      expect(result.success).toBe(true);
      expect(result.number).toBe(17);
    });

    it("UI-004 accepts issue_number when target is wildcard", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const result = resolveNumber({ issue_number: 99 }, "*", mockContext);

      expect(result.success).toBe(true);
      expect(result.number).toBe(99);
    });

    it("UI-005 ignores unresolved temporary IDs when target is triggering", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const result = resolveNumber({ issue_number: "aw_pending" }, "triggering", mockContext, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
    });

    it("should handle different item number fields", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_pull_request",
        itemNumberField: "pull_request_number",
        supportsPR: false,
        supportsIssue: false,
      });

      const item = { pull_request_number: 123 };
      const updateTarget = "triggering";
      // Create a context with PR payload instead of issue
      const prContext = {
        ...mockContext,
        eventName: "pull_request",
        payload: {
          pull_request: {
            number: 123,
          },
        },
      };

      const result = resolveNumber(item, updateTarget, prContext);

      expect(result.success).toBe(true);
      expect(result.number).toBe(123);
    });

    it("should return error when resolveTarget fails", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      // No issue number in item or context
      const item = {};
      const updateTarget = "triggering";
      const context = { ...mockContext, payload: {} };

      const result = resolveNumber(item, updateTarget, context);

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it("should propagate shouldFail:false when not in issue context (schedule event)", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      // schedule event — no issue context, resolveTarget returns shouldFail:false
      const scheduleContext = { ...mockContext, eventName: "schedule", payload: {} };
      const result = resolveNumber({}, "triggering", scheduleContext);

      expect(result.success).toBe(false);
      expect(result.shouldFail).toBe(false);
      expect(result.error).toContain("issue context");
    });

    it("should resolve temporary ID in issue_number field", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const item = { issue_number: "aw_analysis" };
      const updateTarget = "*";
      const resolvedTemporaryIds = {
        aw_analysis: { repo: "testowner/testrepo", number: 42 },
      };

      const result = resolveNumber(item, updateTarget, mockContext, resolvedTemporaryIds);

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
    });

    it("should defer when temporary ID is not yet resolved", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const item = { issue_number: "aw_pending" };
      const updateTarget = "*";
      const resolvedTemporaryIds = {};

      const result = resolveNumber(item, updateTarget, mockContext, resolvedTemporaryIds);

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("aw_pending");
    });

    it("should pass through regular numbers even when resolvedTemporaryIds is provided", async () => {
      const resolveNumber = factoryModule.createStandardResolveNumber({
        itemType: "update_issue",
        itemNumberField: "issue_number",
        supportsPR: false,
        supportsIssue: true,
      });

      const item = { issue_number: 99 };
      const updateTarget = "*";
      const resolvedTemporaryIds = {
        aw_other: { repo: "testowner/testrepo", number: 42 },
      };

      const result = resolveNumber(item, updateTarget, mockContext, resolvedTemporaryIds);

      expect(result.success).toBe(true);
      expect(result.number).toBe(99);
    });
  });

  describe("createStandardFormatResult", () => {
    it("should format result with standard fields (issue pattern)", async () => {
      const formatResult = factoryModule.createStandardFormatResult({
        numberField: "number",
        urlField: "url",
        urlSource: "html_url",
      });

      const updatedItem = {
        html_url: "https://github.com/owner/repo/issues/42",
        title: "Test Issue",
      };

      const result = formatResult(42, updatedItem);

      expect(result).toEqual({
        success: true,
        number: 42,
        url: "https://github.com/owner/repo/issues/42",
        title: "Test Issue",
      });
    });

    it("should format result with PR-specific fields", async () => {
      const formatResult = factoryModule.createStandardFormatResult({
        numberField: "pull_request_number",
        urlField: "pull_request_url",
        urlSource: "html_url",
      });

      const updatedItem = {
        html_url: "https://github.com/owner/repo/pull/123",
        title: "Test PR",
      };

      const result = formatResult(123, updatedItem);

      expect(result).toEqual({
        success: true,
        pull_request_number: 123,
        pull_request_url: "https://github.com/owner/repo/pull/123",
        title: "Test PR",
      });
    });

    it("should format result with discussion-specific fields", async () => {
      const formatResult = factoryModule.createStandardFormatResult({
        numberField: "number",
        urlField: "url",
        urlSource: "url",
      });

      const updatedItem = {
        url: "https://github.com/owner/repo/discussions/456",
        title: "Test Discussion",
      };

      const result = formatResult(456, updatedItem);

      expect(result).toEqual({
        success: true,
        number: 456,
        url: "https://github.com/owner/repo/discussions/456",
        title: "Test Discussion",
      });
    });
  });

  describe("authentication: github-token and cross-repo routing", () => {
    it("should use the global github client when no github-token in config", async () => {
      // Capture which github client is passed to executeUpdate
      /** @type {any} */
      let capturedClient = null;
      const mockExecuteUpdate = vi.fn().mockImplementation(async (githubClient, context, num, data) => {
        capturedClient = githubClient;
        return { html_url: "https://example.com", title: "Updated" };
      });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      // No github-token in config
      const handler = await handlerFactory({});
      await handler({ title: "Test" });

      // The global github client should be used
      expect(capturedClient).toBe(mockGithub);
    });

    it("should route to target-repo when no message.repo is set", async () => {
      /** @type {any} */
      let capturedContext = null;
      const mockExecuteUpdate = vi.fn().mockImplementation(async (githubClient, context, num, data) => {
        capturedContext = context;
        return { html_url: "https://example.com", title: "Updated" };
      });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({ "target-repo": "owner/myrepo" });
      // Message without repo field — should route to the configured target-repo
      await handler({ title: "Test" });

      expect(capturedContext.repo.owner).toBe("owner");
      expect(capturedContext.repo.repo).toBe("myrepo");
    });

    it("should route to message.repo when it matches the configured target-repo", async () => {
      /** @type {any} */
      let capturedContext = null;
      const mockExecuteUpdate = vi.fn().mockImplementation(async (githubClient, context, num, data) => {
        capturedContext = context;
        return { html_url: "https://example.com", title: "Updated" };
      });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 99 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({ "target-repo": "other-owner/side-repo" });
      // Message specifies a cross-repo target
      await handler({ issue_number: 99, repo: "other-owner/side-repo" });

      // effectiveContext.repo should be the target repo
      expect(capturedContext.repo.owner).toBe("other-owner");
      expect(capturedContext.repo.repo).toBe("side-repo");
    });

    it("should reject message.repo when it is not in allowed-repos", async () => {
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com", title: "Updated" });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({
        "target-repo": "allowed-owner/allowed-repo",
        allowed_repos: ["allowed-owner/allowed-repo"],
      });

      // This repo is not in allowed-repos
      const result = await handler({ issue_number: 42, repo: "malicious-owner/other-repo" });

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
      // executeUpdate should not have been called
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should inject _workflowRepo into updateData before calling executeUpdate", async () => {
      /** @type {any} */
      let capturedUpdateData = null;
      const mockExecuteUpdate = vi.fn().mockImplementation(async (githubClient, context, num, data) => {
        capturedUpdateData = data;
        return { html_url: "https://example.com", title: "Updated" };
      });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({ "target-repo": "other-owner/side-repo", allowed_repos: ["other-owner/side-repo"] });
      await handler({ issue_number: 42, repo: "other-owner/side-repo" });

      // _workflowRepo must reference the original context.repo (the current workflow)
      expect(capturedUpdateData._workflowRepo).toBeDefined();
      expect(capturedUpdateData._workflowRepo.owner).toBe(mockContext.repo.owner);
      expect(capturedUpdateData._workflowRepo.repo).toBe(mockContext.repo.repo);
    });

    it("should route to message.repo when target-repo is wildcard", async () => {
      /** @type {any} */
      let capturedContext = null;
      const mockExecuteUpdate = vi.fn().mockImplementation(async (githubClient, context, num, data) => {
        capturedContext = context;
        return { html_url: "https://example.com", title: "Updated" };
      });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({ "target-repo": "*" });
      // Message specifies a cross-repo target; wildcard allows any repo
      await handler({ issue_number: 42, repo: "org/other-repo" });

      expect(capturedContext.repo.owner).toBe("org");
      expect(capturedContext.repo.repo).toBe("other-repo");
    });

    it("should fail when target-repo is wildcard and message has no repo", async () => {
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com", title: "Updated" });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({ "target-repo": "*" });
      // No repo field — must fail because "*" is not a valid repo slug
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(false);
      expect(result.error).toContain("not a valid 'owner/repo' slug");
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should include body in log fields when _rawBody is present", async () => {
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com", title: "Updated" });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { _rawBody: "new body content" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({});
      await handler({ body: "new body content" });

      // The log should mention "body" even though _rawBody starts with underscore
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining('"body"'));
    });
  });

  describe("handler control flow", () => {
    it("should return staged result when config.staged is true", async () => {
      const mockExecuteUpdate = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "New title" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({ staged: true });
      const result = await handler({ title: "New title" });

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo).toMatchObject({ number: 42 });
      // executeUpdate must NOT be called in staged mode
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should return skipped result when buildUpdateData returns skipped:true", async () => {
      const mockExecuteUpdate = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({
          success: true,
          skipped: true,
          reason: "No supported fields provided",
        }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(result.reason).toBe("No supported fields provided");
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should propagate deferred flag from resolveItemNumber", async () => {
      const mockExecuteUpdate = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({
          success: false,
          deferred: true,
          error: "Temporary ID not yet resolved: aw_pending",
        }),
        buildUpdateData: vi.fn(),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn(),
      });

      const handler = await handlerFactory({});
      const result = await handler({ issue_number: "aw_pending" });

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("aw_pending");
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should return skipped result when resolveItemNumber returns shouldFail:false", async () => {
      const mockExecuteUpdate = vi.fn();

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_issue",
        itemTypeName: "issue",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({
          success: false,
          shouldFail: false,
          error: 'Target is "triggering" but not running in issue context, skipping update_issue',
        }),
        buildUpdateData: vi.fn(),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn(),
      });

      const handler = await handlerFactory({});
      const result = await handler({ body: "Report body" });

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("issue context");
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
      // Should be logged as info (not error/warning)
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("issue context"));
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("issue context"));
    });

    it("should call itemFilter and skip update when filter returns a result", async () => {
      const mockExecuteUpdate = vi.fn();
      const mockItemFilter = vi.fn().mockResolvedValue({
        success: false,
        skipped: true,
        error: "Required label not present",
      });

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
        itemFilter: mockItemFilter,
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(mockItemFilter).toHaveBeenCalled();
      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toBe("Required label not present");
      expect(mockExecuteUpdate).not.toHaveBeenCalled();
    });

    it("should call itemFilter and proceed when filter returns null", async () => {
      const mockExecuteUpdate = vi.fn().mockResolvedValue({ html_url: "https://example.com", title: "Updated" });
      const mockItemFilter = vi.fn().mockResolvedValue(null);

      const handlerFactory = factoryModule.createUpdateHandlerFactory({
        itemType: "update_test",
        itemTypeName: "test item",
        supportsPR: false,
        resolveItemNumber: vi.fn().mockReturnValue({ success: true, number: 42 }),
        buildUpdateData: vi.fn().mockReturnValue({ success: true, data: { title: "Test" } }),
        executeUpdate: mockExecuteUpdate,
        formatSuccessResult: vi.fn().mockReturnValue({ success: true }),
        itemFilter: mockItemFilter,
      });

      const handler = await handlerFactory({});
      const result = await handler({ title: "Test" });

      expect(mockItemFilter).toHaveBeenCalled();
      expect(result.success).toBe(true);
      expect(mockExecuteUpdate).toHaveBeenCalled();
    });
  });
});
