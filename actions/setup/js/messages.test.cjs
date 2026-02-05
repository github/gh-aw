/**
 * Test Suite: messages.cjs
 *
 * Tests for the safe-output messages module functionality including:
 * - Environment variable parsing (GH_AW_SAFE_OUTPUT_MESSAGES)
 * - Template rendering with placeholder replacement
 * - Footer message generation (default and custom)
 * - Installation instructions generation
 * - Staged mode title and description generation
 * - Run status messages (started, success, failure)
 */
import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock core for GitHub Actions environment
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
};

// Set up global mocks
global.core = mockCore;

describe("messages.cjs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Clear environment variables before each test
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    delete process.env.GH_AW_ENGINE_ID;
    delete process.env.GH_AW_ENGINE_VERSION;
    delete process.env.GH_AW_ENGINE_MODEL;
    delete process.env.GH_AW_TRACKER_ID;
    // Clear cache by reimporting
    vi.resetModules();
  });

  describe("getMessages", () => {
    it("should return null when env var is not set", async () => {
      const { getMessages } = await import("./messages.cjs");
      const result = getMessages();
      expect(result).toBeNull();
    });

    it("should parse valid JSON config with camelCase keys (Go struct)", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom footer by [{workflow_name}]({run_url})",
        footerInstall: "> Custom install: `gh aw add {workflow_source}`",
        stagedTitle: "## Custom Preview: {operation}",
        stagedDescription: "Preview of {operation}:",
      });

      const { getMessages } = await import("./messages.cjs");
      const result = getMessages();

      expect(result).toEqual({
        footer: "> Custom footer by [{workflow_name}]({run_url})",
        footerInstall: "> Custom install: `gh aw add {workflow_source}`",
        stagedTitle: "## Custom Preview: {operation}",
        stagedDescription: "Preview of {operation}:",
      });
    });

    it("should parse valid JSON config with partial camelCase keys", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom footer",
        footerInstall: "> Custom install",
      });

      const { getMessages } = await import("./messages.cjs");
      const result = getMessages();

      expect(result.footer).toBe("> Custom footer");
      expect(result.footerInstall).toBe("> Custom install");
    });

    it("should handle invalid JSON gracefully", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = "not valid json";

      const { getMessages } = await import("./messages.cjs");
      const result = getMessages();

      expect(result).toBeNull();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to parse GH_AW_SAFE_OUTPUT_MESSAGES"));
    });

    it("should read fresh env var value on each call (no caching)", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "first value",
      });

      const { getMessages } = await import("./messages.cjs");

      const result1 = getMessages();
      expect(result1.footer).toBe("first value");

      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "second value",
      });

      const result2 = getMessages();
      expect(result2.footer).toBe("second value");
    });
  });

  describe("renderTemplate", () => {
    it("should replace simple placeholders", async () => {
      const { renderTemplate } = await import("./messages.cjs");

      const result = renderTemplate("Hello {name}!", { name: "World" });
      expect(result).toBe("Hello World!");
    });

    it("should replace multiple placeholders", async () => {
      const { renderTemplate } = await import("./messages.cjs");

      const result = renderTemplate("{greeting} {name}, you have {count} messages", {
        greeting: "Hello",
        name: "User",
        count: 5,
      });
      expect(result).toBe("Hello User, you have 5 messages");
    });

    it("should leave unknown placeholders unchanged", async () => {
      const { renderTemplate } = await import("./messages.cjs");

      const result = renderTemplate("Hello {name}, {unknown} placeholder", { name: "User" });
      expect(result).toBe("Hello User, {unknown} placeholder");
    });

    it("should handle snake_case placeholders", async () => {
      const { renderTemplate } = await import("./messages.cjs");

      const result = renderTemplate("{workflow_name} at {run_url}", {
        workflow_name: "My Workflow",
        run_url: "https://example.com",
      });
      expect(result).toBe("My Workflow at https://example.com");
    });

    it("should handle numbers as values", async () => {
      const { renderTemplate } = await import("./messages.cjs");

      const result = renderTemplate("Issue #{issue_number}", { issue_number: 42 });
      expect(result).toBe("Issue #42");
    });

    it("should handle undefined values by keeping placeholder", async () => {
      const { renderTemplate } = await import("./messages.cjs");

      const result = renderTemplate("Value: {value}", { value: undefined });
      expect(result).toBe("Value: {value}");
    });
  });

  describe("getFooterMessage", () => {
    it("should return default footer when no custom config", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should append triggering number when provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        triggeringNumber: 42,
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) for issue #42");
    });

    it("should use custom footer template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Custom Workflow",
        runUrl: "https://example.com/run/456",
      });

      expect(result).toBe("> Custom: [Custom Workflow](https://example.com/run/456)");
    });

    it("should support both snake_case and camelCase in custom templates", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> {workflowName} ({workflow_name})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test",
        runUrl: "https://example.com",
      });

      expect(result).toBe("> Test (Test)");
    });
  });

  describe("getFooterInstallMessage", () => {
    it("should return empty string when no workflow source", async () => {
      const { getFooterInstallMessage } = await import("./messages.cjs");

      const result = getFooterInstallMessage({
        workflowName: "Test",
        runUrl: "https://example.com",
      });

      expect(result).toBe("");
    });

    it("should return default install message when source is provided", async () => {
      const { getFooterInstallMessage } = await import("./messages.cjs");

      const result = getFooterInstallMessage({
        workflowName: "Test",
        runUrl: "https://example.com",
        workflowSource: "owner/repo/workflow.md@main",
        workflowSourceUrl: "https://github.com/owner/repo",
      });

      expect(result).toContain("gh aw add owner/repo/workflow.md@main");
      expect(result).toContain("View source at");
    });

    it("should use custom install template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footerInstall: "> Install: `gh aw add {workflow_source}`",
      });

      const { getFooterInstallMessage } = await import("./messages.cjs");

      const result = getFooterInstallMessage({
        workflowName: "Test",
        runUrl: "https://example.com",
        workflowSource: "owner/repo/workflow.md@main",
        workflowSourceUrl: "https://github.com/owner/repo",
      });

      expect(result).toBe("> Install: `gh aw add owner/repo/workflow.md@main`");
    });
  });

  describe("generateFooterWithMessages", () => {
    it("should generate complete default footer", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("> Generated by [Test Workflow]");
      expect(result).toContain("https://github.com/test/repo/actions/runs/123");
    });

    it("should include triggering issue number", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", 42, undefined, undefined);

      expect(result).toContain("for issue #42");
    });

    it("should include triggering PR number when no issue", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, 99, undefined);

      expect(result).toContain("for issue #99");
    });

    it("should include triggering discussion number", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, 7);

      expect(result).toContain("for issue #discussion #7");
    });

    it("should include installation instructions when source is provided", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "owner/repo/workflow.md@main", "https://github.com/owner/repo", undefined, undefined, undefined);

      expect(result).toContain("gh aw add owner/repo/workflow.md@main");
    });

    it("should include XML comment marker for traceability", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("<!-- gh-aw-agentic-workflow: Test Workflow");
      expect(result).toContain("run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include engine metadata in XML marker when env vars are set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_VERSION = "1.0.0";
      process.env.GH_AW_ENGINE_MODEL = "gpt-5";

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("<!-- gh-aw-agentic-workflow: Test Workflow");
      expect(result).toContain("engine: copilot");
      expect(result).toContain("version: 1.0.0");
      expect(result).toContain("model: gpt-5");
      expect(result).toContain("run: https://github.com/test/repo/actions/runs/123 -->");

      // Clean up env vars
      delete process.env.GH_AW_ENGINE_ID;
      delete process.env.GH_AW_ENGINE_VERSION;
      delete process.env.GH_AW_ENGINE_MODEL;
    });
  });

  describe("generateXMLMarker", () => {
    it("should generate basic XML marker with workflow name and run URL", async () => {
      const { generateXMLMarker } = await import("./messages.cjs");

      const result = generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, run: https://github.com/test/repo/actions/runs/123 -->");
    });

    it("should include engine ID when env var is set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";

      vi.resetModules();
      const { generateXMLMarker } = await import("./messages.cjs");

      const result = generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, engine: copilot, run: https://github.com/test/repo/actions/runs/123 -->");

      delete process.env.GH_AW_ENGINE_ID;
    });

    it("should include all engine metadata when all env vars are set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_VERSION = "1.0.0";
      process.env.GH_AW_ENGINE_MODEL = "gpt-5";

      vi.resetModules();
      const { generateXMLMarker } = await import("./messages.cjs");

      const result = generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, engine: copilot, version: 1.0.0, model: gpt-5, run: https://github.com/test/repo/actions/runs/123 -->");

      delete process.env.GH_AW_ENGINE_ID;
      delete process.env.GH_AW_ENGINE_VERSION;
      delete process.env.GH_AW_ENGINE_MODEL;
    });

    it("should include tracker-id when env var is set", async () => {
      process.env.GH_AW_TRACKER_ID = "my-tracker-12345";

      vi.resetModules();
      const { generateXMLMarker } = await import("./messages.cjs");

      const result = generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, gh-aw-tracker-id: my-tracker-12345, run: https://github.com/test/repo/actions/runs/123 -->");

      delete process.env.GH_AW_TRACKER_ID;
    });

    it("should include tracker-id with engine metadata when all env vars are set", async () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_VERSION = "1.0.0";
      process.env.GH_AW_ENGINE_MODEL = "gpt-5";
      process.env.GH_AW_TRACKER_ID = "workflow-2024-q1";

      vi.resetModules();
      const { generateXMLMarker } = await import("./messages.cjs");

      const result = generateXMLMarker("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("<!-- gh-aw-agentic-workflow: Test Workflow, gh-aw-tracker-id: workflow-2024-q1, engine: copilot, version: 1.0.0, model: gpt-5, run: https://github.com/test/repo/actions/runs/123 -->");

      delete process.env.GH_AW_ENGINE_ID;
      delete process.env.GH_AW_ENGINE_VERSION;
      delete process.env.GH_AW_ENGINE_MODEL;
      delete process.env.GH_AW_TRACKER_ID;
    });
  });

  describe("getStagedTitle", () => {
    it("should return default staged title", async () => {
      const { getStagedTitle } = await import("./messages.cjs");

      const result = getStagedTitle({ operation: "Create Issues" });

      expect(result).toBe("## 🔍 Preview: Create Issues");
    });

    it("should use custom staged title template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        stagedTitle: "## 🔍 Preview: {operation}",
      });

      const { getStagedTitle } = await import("./messages.cjs");

      const result = getStagedTitle({ operation: "Add Comments" });

      expect(result).toBe("## 🔍 Preview: Add Comments");
    });
  });

  describe("getStagedDescription", () => {
    it("should return default staged description", async () => {
      const { getStagedDescription } = await import("./messages.cjs");

      const result = getStagedDescription({ operation: "Create Issues" });

      expect(result).toBe("📋 The following operations would be performed if staged mode was disabled:");
    });

    it("should use custom staged description template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        stagedDescription: "Preview of {operation} - nothing will be created:",
      });

      const { getStagedDescription } = await import("./messages.cjs");

      const result = getStagedDescription({ operation: "pull requests" });

      expect(result).toBe("Preview of pull requests - nothing will be created:");
    });
  });

  describe("getRunStartedMessage", () => {
    it("should return default run-started message", async () => {
      const { getRunStartedMessage } = await import("./messages.cjs");

      const result = getRunStartedMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        eventType: "issue",
      });

      expect(result).toBe("🚀 [Test Workflow](https://github.com/test/repo/actions/runs/123) has started processing this issue");
    });

    it("should use custom run-started template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        runStarted: "[{workflow_name}]({run_url}) started for {event_type}",
      });

      const { getRunStartedMessage } = await import("./messages.cjs");

      const result = getRunStartedMessage({
        workflowName: "Custom Bot",
        runUrl: "https://example.com/run/456",
        eventType: "pull request",
      });

      expect(result).toBe("[Custom Bot](https://example.com/run/456) started for pull request");
    });
  });

  describe("getRunSuccessMessage", () => {
    it("should return default run-success message", async () => {
      const { getRunSuccessMessage } = await import("./messages.cjs");

      const result = getRunSuccessMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("✅ [Test Workflow](https://github.com/test/repo/actions/runs/123) completed successfully!");
    });

    it("should use custom run-success template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        runSuccess: "✅ [{workflow_name}]({run_url}) finished!",
      });

      const { getRunSuccessMessage } = await import("./messages.cjs");

      const result = getRunSuccessMessage({
        workflowName: "Custom Bot",
        runUrl: "https://example.com/run/456",
      });

      expect(result).toBe("✅ [Custom Bot](https://example.com/run/456) finished!");
    });
  });

  describe("getRunFailureMessage", () => {
    it("should return default run-failure message", async () => {
      const { getRunFailureMessage } = await import("./messages.cjs");

      const result = getRunFailureMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        status: "failed",
      });

      expect(result).toBe("❌ [Test Workflow](https://github.com/test/repo/actions/runs/123) failed. Please review the logs for details.");
    });

    it("should use custom run-failure template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        runFailure: "❌ [{workflow_name}]({run_url}) {status}.",
      });

      const { getRunFailureMessage } = await import("./messages.cjs");

      const result = getRunFailureMessage({
        workflowName: "Custom Bot",
        runUrl: "https://example.com/run/456",
        status: "timed out",
      });

      expect(result).toBe("❌ [Custom Bot](https://example.com/run/456) timed out.");
    });

    it("should handle cancelled status", async () => {
      const { getRunFailureMessage } = await import("./messages.cjs");

      const result = getRunFailureMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        status: "was cancelled",
      });

      expect(result).toBe("❌ [Test Workflow](https://github.com/test/repo/actions/runs/123) was cancelled. Please review the logs for details.");
    });
  });

  describe("getCloseOlderDiscussionMessage", () => {
    it("should return default close older discussion message", async () => {
      const { getCloseOlderDiscussionMessage } = await import("./messages.cjs");

      const result = getCloseOlderDiscussionMessage({
        newDiscussionUrl: "https://github.com/test/repo/discussions/10",
        newDiscussionNumber: 10,
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toContain("This discussion has been marked as **outdated**");
      expect(result).toContain("[Test Workflow](https://github.com/test/repo/actions/runs/123)");
      expect(result).toContain("[Discussion #10](https://github.com/test/repo/discussions/10)");
    });

    it("should use custom close older discussion template", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        closeOlderDiscussion: "This is outdated. See [{new_discussion_number}]({new_discussion_url}).",
      });

      const { getCloseOlderDiscussionMessage } = await import("./messages.cjs");

      const result = getCloseOlderDiscussionMessage({
        newDiscussionUrl: "https://github.com/test/repo/discussions/15",
        newDiscussionNumber: 15,
        workflowName: "Custom Bot",
        runUrl: "https://example.com/run/456",
      });

      expect(result).toBe("This is outdated. See [15](https://github.com/test/repo/discussions/15).");
    });

    it("should support snake_case placeholders", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        closeOlderDiscussion: "Outdated by {workflow_name}. New: #{new_discussion_number}",
      });

      const { getCloseOlderDiscussionMessage } = await import("./messages.cjs");

      const result = getCloseOlderDiscussionMessage({
        newDiscussionUrl: "https://github.com/test/repo/discussions/20",
        newDiscussionNumber: 20,
        workflowName: "Weekly Report",
        runUrl: "https://example.com/run/789",
      });

      expect(result).toBe("Outdated by Weekly Report. New: #20");
    });
  });
});
