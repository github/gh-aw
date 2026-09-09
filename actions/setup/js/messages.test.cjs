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
import path from "path";
import { fileURLToPath } from "url";

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
    delete process.env.GH_AW_PRIMARY_MODEL;
    delete process.env.GH_AW_TRACKER_ID;
    delete process.env.GITHUB_RUN_ID;
    delete process.env.GH_AW_WORKFLOW_ID;
    delete process.env.GH_AW_EFFECTIVE_TOKENS;
    delete process.env.GH_AW_AIC;
    delete process.env.GH_AW_AMBIENT_CONTEXT;
    delete process.env.GH_AW_AGENT_AIC;
    delete process.env.GH_AW_EVALS_AIC;
    delete process.env.GH_AW_THREAT_DETECTION_AIC;
    delete process.env.GH_AW_LABEL_COMMANDS;
    delete process.env.GH_AW_DETECTION_CONCLUSION;
    delete process.env.GH_AW_DETECTION_REASON;
    // Point GH_AW_PROMPTS_DIR to the source md/ directory so getPromptPath()
    // resolves template files from the source tree in test environments where
    // RUNNER_TEMP is set but the runtime prompts directory is not populated.
    process.env.GH_AW_PROMPTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "../md");
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

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) for #42");
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

    it("should render a handler body footer template", async () => {
      const { getBodyFooterMessage } = await import("./messages.cjs");

      const result = getBodyFooterMessage("Generated by [{workflow_name}]({run_url})", {
        workflowName: "Custom Workflow",
        runUrl: "https://example.com/run/456",
      });

      expect(result).toBe("Generated by [Custom Workflow](https://example.com/run/456)");
    });

    it("should NOT append triggering number suffix when custom footer is configured", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Custom Workflow",
        runUrl: "https://example.com/run/456",
        triggeringNumber: 42,
      });

      expect(result).toBe("> Custom: [Custom Workflow](https://example.com/run/456)");
      expect(result).not.toContain("for #");
    });

    it("should allow custom footer template to include triggering number via placeholder", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url}) (#{triggering_number})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Custom Workflow",
        runUrl: "https://example.com/run/456",
        triggeringNumber: 42,
      });

      expect(result).toBe("> Custom: [Custom Workflow](https://example.com/run/456) (#42)");
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

    it("should append history link when historyUrl is provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        historyUrl: "https://github.com/search?q=repo:test/repo+is:issue&type=issues",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · [◷](https://github.com/search?q=repo:test/repo+is:issue&type=issues)");
    });

    it("should include both triggering number and history link when both are provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        triggeringNumber: 42,
        historyUrl: "https://github.com/search?q=repo:test/repo+is:issue&type=issues",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) for #42 · [◷](https://github.com/search?q=repo:test/repo+is:issue&type=issues)");
    });

    it("should not append history link when historyUrl is not provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).not.toContain("history");
      expect(result).not.toContain("◷");
    });

    it("should expose {history_link} placeholder in custom footer templates", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> 🤖 *Generated by [{workflow_name}]({run_url})*{history_link}",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const historyUrl = "https://github.com/search?q=repo:test/repo+is:issue&type=issues";
      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        historyUrl,
      });

      expect(result).toBe(`> 🤖 *Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)* · [◷](${historyUrl})`);
    });

    it("should render empty string for {history_link} when historyUrl is not provided", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> 🤖 *Generated by [{workflow_name}]({run_url})*{history_link}",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> 🤖 *Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)*");
      expect(result).not.toContain("{history_link}");
    });

    it("should expose {agentic_workflow_url} placeholder in custom footer templates", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Generated by [{workflow_name}]({agentic_workflow_url})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123/agentic_workflow)");
    });

    it("should use explicit agenticWorkflowUrl from context in custom templates", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Generated by [{workflow_name}]({agentic_workflow_url})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        agenticWorkflowUrl: "https://github.com/test/repo/actions/runs/123/custom_path",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123/custom_path)");
    });

    it("should not include effective tokens from env var in default footer", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "12500";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should not append model-prefixed effective tokens in default footer", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "12500";
      process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should include history link without effective tokens in default footer", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "5000";
      const historyUrl = "https://github.com/search?q=repo:test/repo+is:issue&type=issues";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        historyUrl,
      });

      expect(result).toBe(`> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · [◷](${historyUrl})`);
    });

    it("should include AI Credits without effective tokens when GH_AW_AIC is set", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "5000";
      process.env.GH_AW_AIC = "0.125";
      process.env.GH_AW_ENGINE_ID = "claude";
      process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · claude · sonnet46 · 0.125 AIC");
    });

    it("should include ambient context in the default footer when GH_AW_AMBIENT_CONTEXT is set", async () => {
      process.env.GH_AW_AMBIENT_CONTEXT = "900";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · ⊞ 900");
    });

    it("should render separate agent and threat-detection AI Credits entries in the default footer", async () => {
      process.env.GH_AW_AGENT_AIC = "0.125";
      process.env.GH_AW_THREAT_DETECTION_AIC = "0.025";
      process.env.GH_AW_ENGINE_ID = "claude";
      process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · claude · sonnet46 · 0.125 AIC · ⌖ 0.025 AIC");
    });

    it("should fall back to GH_AW_AIC for the agent AI Credits entry when GH_AW_AGENT_AIC is absent", async () => {
      process.env.GH_AW_AIC = "0.125";
      process.env.GH_AW_THREAT_DETECTION_AIC = "0.025";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · 0.125 AIC · ⌖ 0.025 AIC");
    });

    it("should place ambient context after threat-detection AI Credits in the default footer", async () => {
      process.env.GH_AW_AGENT_AIC = "0.125";
      process.env.GH_AW_THREAT_DETECTION_AIC = "0.025";
      process.env.GH_AW_AMBIENT_CONTEXT = "900";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · 0.125 AIC · ⌖ 0.025 AIC · ⊞ 900");
    });

    it("should include evals AI Credits with a Unicode icon in the default footer", async () => {
      process.env.GH_AW_AGENT_AIC = "0.125";
      process.env.GH_AW_EVALS_AIC = "0.01";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · 0.125 AIC · ◇ 0.01 AIC");
    });

    it("should not include effective tokens when GH_AW_EFFECTIVE_TOKENS is not set and not passed in context", async () => {
      delete process.env.GH_AW_EFFECTIVE_TOKENS;

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)");
      expect(result).not.toContain("●");
    });

    it("should ignore context and env effectiveTokens for default footer", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "99000";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        effectiveTokens: 5000,
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)");
      expect(result).not.toContain("99K");
    });

    it("should expose ai_credits_suffix in custom footer templates", async () => {
      process.env.GH_AW_AIC = "1.25";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url}){ai_credits_suffix}",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123) · 1.25 AIC");
    });

    it("should expose separate AI Credits entries in custom footer templates", async () => {
      process.env.GH_AW_AGENT_AIC = "1.25";
      process.env.GH_AW_THREAT_DETECTION_AIC = "0.25";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url}){ai_credits_suffix}",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123) · 1.25 AIC · ⌖ 0.25 AIC");
    });

    it("should expose evals AI Credits entry in custom footer templates", async () => {
      process.env.GH_AW_AGENT_AIC = "1.25";
      process.env.GH_AW_EVALS_AIC = "0.25";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url}){ai_credits_suffix}",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123) · 1.25 AIC · ◇ 0.25 AIC");
    });

    it("should expose detection and evals AI Credits entries in order in custom footer templates", async () => {
      process.env.GH_AW_AGENT_AIC = "1.25";
      process.env.GH_AW_THREAT_DETECTION_AIC = "0.25";
      process.env.GH_AW_EVALS_AIC = "0.05";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url}){ai_credits_suffix}",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123) · 1.25 AIC · ⌖ 0.25 AIC · ◇ 0.05 AIC");
    });

    it("should include ambient context in ai_credits_suffix for custom footer templates", async () => {
      process.env.GH_AW_AGENT_AIC = "1.25";
      process.env.GH_AW_THREAT_DETECTION_AIC = "0.25";
      process.env.GH_AW_AMBIENT_CONTEXT = "900";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url}){ai_credits_suffix}",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123) · 1.25 AIC · ⌖ 0.25 AIC · ⊞ 900");
    });

    it("should not include mini-tier model identifiers in default footer suffixes", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        model: "gpt-5.4-mini-2026-03-17",
        effectiveTokens: 5000,
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should append slash command hint when slashCommand is provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        slashCommand: "review-bot",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)\n> <sub>Comment <em>/review-bot</em> to run again</sub>");
    });

    it("should not append slash command hint when slashCommand is not provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).not.toContain("<sub>");
      expect(result).not.toContain("<em>");
    });

    it("should include slash command hint after history link", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "5000";
      const historyUrl = "https://github.com/search?q=repo:test";

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        historyUrl,
        slashCommand: "deploy",
      });

      expect(result).toBe(`> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123) · [◷](${historyUrl})\n> <sub>Comment <em>/deploy</em> to run again</sub>`);
    });

    it("should include slash command hint in custom footer templates", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        slashCommand: "review-bot",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123)\n> <sub>Comment <em>/review-bot</em> to run again</sub>");
    });

    it("should include label command hint in custom footer templates", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        footer: "> Custom: [{workflow_name}]({run_url})",
      });

      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        labelCommand: "deploy",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123)\n> <sub>Add label <em>deploy</em> to run again</sub>");
    });

    it("should use slashCommandPlaceholder as hint text when provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        slashCommand: "review-bot",
        slashCommandPlaceholder: "to review this PR",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)\n> <sub>Comment <em>/review-bot</em> to review this PR</sub>");
    });

    it("should append label command hint when labelCommand is provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        labelCommand: "deploy",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)\n> <sub>Add label <em>deploy</em> to run again</sub>");
    });

    it("should include both slash and label command hints when both are provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        slashCommand: "review-bot",
        labelCommand: "deploy",
      });

      expect(result).toBe("> Generated by [Test Workflow](https://github.com/test/repo/actions/runs/123)\n> <sub>Comment <em>/review-bot</em> to run again</sub>\n> <sub>Add label <em>deploy</em> to run again</sub>");
    });

    it("should fall back to 'to run again' when slashCommandPlaceholder is not provided", async () => {
      const { getFooterMessage } = await import("./messages.cjs");

      const result = getFooterMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        slashCommand: "review-bot",
      });

      expect(result).toContain("to run again");
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

      expect(result).toContain("<details>");
      expect(result).toContain("<summary><sub>Add this agentic workflow to your repo</sub></summary>");
      expect(result).toContain("To install this agentic workflow, run");
      expect(result).toContain("gh aw add owner/repo/workflow.md@main");
      expect(result).toContain("</details>");
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

    it("should not start with blank lines when there are no guard notices", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result.startsWith("> Generated by [Test Workflow]")).toBe(true);
    });

    it("should separate guard notices from footer with exactly one blank line when guard notices are present", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "test reason";
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      delete process.env.GH_AW_DETECTION_CONCLUSION;
      delete process.env.GH_AW_DETECTION_REASON;

      // The footer attribution line must be preceded by exactly one blank line (no leading blank lines
      // from guard notices, no extra blank lines from the separator)
      expect(result).toMatch(/\n\n> Generated by \[Test Workflow\]/);
      expect(result).not.toMatch(/\n\n\n> Generated by \[Test Workflow\]/);
    });

    it("should include triggering issue number", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", 42, undefined, undefined);

      expect(result).toContain("for #42");
    });

    it("should include triggering PR number when no issue", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, 99, undefined);

      expect(result).toContain("for #99");
    });

    it("should include triggering discussion number", async () => {
      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, 7);

      expect(result).toContain("for discussion #7");
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

    it("should include slash command hint when GH_AW_COMMANDS is set", async () => {
      process.env.GH_AW_COMMANDS = JSON.stringify(["review-bot"]);

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("<sub>Comment <em>/review-bot</em> to run again</sub>");

      delete process.env.GH_AW_COMMANDS;
    });

    it("should not include slash command hint when GH_AW_COMMANDS is not set", async () => {
      delete process.env.GH_AW_COMMANDS;

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).not.toContain("<sub>");
      expect(result).not.toContain("<em>");
    });

    it("should use first command from GH_AW_COMMANDS when multiple commands are configured", async () => {
      process.env.GH_AW_COMMANDS = JSON.stringify(["deploy", "analyze"]);

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("<sub>Comment <em>/deploy</em> to run again</sub>");
      expect(result).not.toContain("/analyze");

      delete process.env.GH_AW_COMMANDS;
    });

    it("should use GH_AW_COMMAND_PLACEHOLDER as hint text when set", async () => {
      process.env.GH_AW_COMMANDS = JSON.stringify(["review-bot"]);
      process.env.GH_AW_COMMAND_PLACEHOLDER = "to analyze this PR";

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("<sub>Comment <em>/review-bot</em> to analyze this PR</sub>");
      expect(result).not.toContain("to run again");

      delete process.env.GH_AW_COMMANDS;
      delete process.env.GH_AW_COMMAND_PLACEHOLDER;
    });

    it("should include label command hint when GH_AW_LABEL_COMMANDS is set", async () => {
      process.env.GH_AW_LABEL_COMMANDS = JSON.stringify(["deploy"]);

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("<sub>Add label <em>deploy</em> to run again</sub>");

      delete process.env.GH_AW_LABEL_COMMANDS;
    });

    it("should use first label command from GH_AW_LABEL_COMMANDS when multiple are configured", async () => {
      process.env.GH_AW_LABEL_COMMANDS = JSON.stringify(["deploy", "release"]);

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("<sub>Add label <em>deploy</em> to run again</sub>");
      expect(result).not.toContain("<sub>Add label <em>release</em> to run again</sub>");

      delete process.env.GH_AW_LABEL_COMMANDS;
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

  describe("getFooterAgentFailureIssueMessage", () => {
    it("should return default footer without effective tokens when env var is not set", async () => {
      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123)");
      expect(result).not.toContain("●");
    });

    it("should not include effective tokens in default footer when env var is set", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "12500";

      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should include history link without effective tokens in default footer", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "5000";
      const historyUrl = "https://github.com/search?q=repo:test/repo+is:issue&type=issues";

      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        historyUrl,
      });

      expect(result).toBe(`> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123) · [◷](${historyUrl})`);
    });

    it("should include AIC and ambient context in default footer when available", async () => {
      process.env.GH_AW_AIC = "1.25";
      process.env.GH_AW_AMBIENT_CONTEXT = "900";
      process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123) · sonnet46 · 1.25 AIC · ⊞ 900");
    });

    it("should include evals AI Credits in the default footer when available", async () => {
      process.env.GH_AW_AIC = "1.25";
      process.env.GH_AW_EVALS_AIC = "0.25";

      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123) · 1.25 AIC · ◇ 0.25 AIC");
    });

    it("should suppress env AIC fallback when explicit context AIC is zero", async () => {
      process.env.GH_AW_AIC = "1.25";

      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        aiCredits: 0,
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should not include effective tokens in custom footer unless placeholder is used", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "5000";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        agentFailureIssue: "> Custom: [{workflow_name}]({run_url})",
      });

      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123)");
      expect(result).not.toContain("●");
    });
  });

  describe("getFooterAgentFailureCommentMessage", () => {
    it("should return default footer without effective tokens when env var is not set", async () => {
      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123)");
      expect(result).not.toContain("●");
    });

    it("should not include effective tokens in default footer when env var is set", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "12500";

      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123)");
    });

    it("should include history link without effective tokens in default footer", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "5000";
      const historyUrl = "https://github.com/search?q=repo:test/repo+is:issue&type=issues";

      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        historyUrl,
      });

      expect(result).toBe(`> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123) · [◷](${historyUrl})`);
    });

    it("should include explicit context AIC in the default footer", async () => {
      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        aiCredits: "2.5",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123) · 2.5 AIC");
    });

    it("should include ambient context in the default footer when available", async () => {
      process.env.GH_AW_AIC = "1.25";
      process.env.GH_AW_AMBIENT_CONTEXT = "900";
      process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123) · sonnet46 · 1.25 AIC · ⊞ 900");
    });

    it("should include evals AI Credits in the default comment footer when available", async () => {
      process.env.GH_AW_AIC = "1.25";
      process.env.GH_AW_EVALS_AIC = "0.25";

      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Generated from [Test Workflow](https://github.com/test/repo/actions/runs/123) · 1.25 AIC · ◇ 0.25 AIC");
    });

    it("should expose ai_credits_suffix in custom comment footer templates", async () => {
      process.env.GH_AW_AMBIENT_CONTEXT = "900";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        agentFailureComment: "> Custom: [{workflow_name}]({run_url}){ai_credits_suffix}",
      });

      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
        aiCredits: "2.5",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123) · 2.5 AIC · ⊞ 900");
    });

    it("should not include effective tokens in custom footer unless placeholder is used", async () => {
      process.env.GH_AW_EFFECTIVE_TOKENS = "5000";
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        agentFailureComment: "> Custom: [{workflow_name}]({run_url})",
      });

      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).toBe("> Custom: [Test Workflow](https://github.com/test/repo/actions/runs/123)");
      expect(result).not.toContain("●");
    });
  });

  describe("getDetectionCautionAlert", () => {
    it("should return empty string when detection conclusion is not set", async () => {
      const { getDetectionCautionAlert } = await import("./messages.cjs");

      const result = getDetectionCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("");
    });

    it("should return empty string when detection conclusion is success", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "success";

      const { getDetectionCautionAlert } = await import("./messages.cjs");

      const result = getDetectionCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("");
    });

    it("should return caution alert when detection conclusion is warning", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "threat_detected";

      const { getDetectionCautionAlert } = await import("./messages.cjs");

      const result = getDetectionCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toContain("> [!CAUTION]");
      expect(result).toContain("agentic threat detected");
      expect(result).toContain("<!-- gh-aw-threat-detected -->");
      expect(result).toContain("Potential security threats were detected");
    });

    it("should return warning alert with agent_failure reason (tooling failure, not security finding)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "agent_failure";

      const { getDetectionCautionAlert } = await import("./messages.cjs");

      const result = getDetectionCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toContain("> [!WARNING]");
      expect(result).toContain("**Threat Detection Engine Failure**");
      expect(result).toContain("What happened");
      expect(result).toContain("<!-- gh-aw-threat-engine-error -->");
      expect(result).not.toContain("<!-- gh-aw-threat-detected -->");
      expect(result).not.toContain("> [!CAUTION]");
      expect(result).not.toContain("agentic threat detected");
      expect(result).toContain("threat detection engine failed");
    });

    it("should return caution alert with default reason when reason is empty (unknown, not tooling failure)", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";

      const { getDetectionCautionAlert } = await import("./messages.cjs");

      const result = getDetectionCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toContain("> [!CAUTION]");
      expect(result).toContain("could not be completed");
    });

    it("should return empty string when detection conclusion is failure", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "failure";

      const { getDetectionCautionAlert } = await import("./messages.cjs");

      const result = getDetectionCautionAlert("Test Workflow", "https://github.com/test/repo/actions/runs/123");

      expect(result).toBe("");
    });
  });

  describe("detection caution alert in footers", () => {
    it("should include caution alert in generateFooterWithMessages when detection is warning", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "threat_detected";

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).toContain("> [!CAUTION]");
      expect(result).toContain("agentic threat detected");
      expect(result).toContain("> Generated by [Test Workflow]");
    });

    it("should not include caution alert in generateFooterWithMessages when detection is not warning", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "success";

      const { generateFooterWithMessages } = await import("./messages.cjs");

      const result = generateFooterWithMessages("Test Workflow", "https://github.com/test/repo/actions/runs/123", "", "", undefined, undefined, undefined);

      expect(result).not.toContain("> [!CAUTION]");
      expect(result).toContain("> Generated by [Test Workflow]");
    });

    it("should not include caution alert in getFooterAgentFailureIssueMessage when detection is warning", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "threat_detected";

      const { getFooterAgentFailureIssueMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureIssueMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).not.toContain("> [!CAUTION]");
      expect(result).toContain("> Generated from [Test Workflow]");
    });

    it("should not include caution alert in getFooterAgentFailureCommentMessage when detection is warning", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "parse_error";

      const { getFooterAgentFailureCommentMessage } = await import("./messages.cjs");

      const result = getFooterAgentFailureCommentMessage({
        workflowName: "Test Workflow",
        runUrl: "https://github.com/test/repo/actions/runs/123",
      });

      expect(result).not.toContain("> [!CAUTION]");
      expect(result).toContain("> Generated from [Test Workflow]");
    });
  });
});
