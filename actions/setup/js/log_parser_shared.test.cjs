import { describe, it, expect } from "vitest";

describe("log_parser_shared.cjs", () => {
  describe("formatDuration", () => {
    it("should format duration less than 60 seconds", async () => {
      const { formatDuration } = await import("./log_parser_shared.cjs");

      expect(formatDuration(5000)).toBe("5s");
      expect(formatDuration(30000)).toBe("30s");
      expect(formatDuration(59499)).toBe("59s"); // Just under 60s
    });

    it("should format duration in minutes without seconds", async () => {
      const { formatDuration } = await import("./log_parser_shared.cjs");

      expect(formatDuration(60000)).toBe("1m");
      expect(formatDuration(120000)).toBe("2m");
      expect(formatDuration(300000)).toBe("5m");
    });

    it("should format duration in minutes with seconds", async () => {
      const { formatDuration } = await import("./log_parser_shared.cjs");

      expect(formatDuration(65000)).toBe("1m 5s");
      expect(formatDuration(90000)).toBe("1m 30s");
      expect(formatDuration(125000)).toBe("2m 5s");
    });

    it("should handle zero and negative durations", async () => {
      const { formatDuration } = await import("./log_parser_shared.cjs");

      expect(formatDuration(0)).toBe("");
      expect(formatDuration(-1000)).toBe("");
    });

    it("should handle null and undefined", async () => {
      const { formatDuration } = await import("./log_parser_shared.cjs");

      expect(formatDuration(null)).toBe("");
      expect(formatDuration(undefined)).toBe("");
    });
  });

  describe("formatBashCommand", () => {
    it("should normalize whitespace in commands", async () => {
      const { formatBashCommand } = await import("./log_parser_shared.cjs");

      const command = "echo    hello\n  world\t\tthere";
      const result = formatBashCommand(command);

      expect(result).toBe("echo hello world there");
    });

    it("should escape backticks", async () => {
      const { formatBashCommand } = await import("./log_parser_shared.cjs");

      const command = "echo `date`";
      const result = formatBashCommand(command);

      expect(result).toBe("echo \\`date\\`");
    });

    it("should truncate long commands", async () => {
      const { formatBashCommand } = await import("./log_parser_shared.cjs");

      const longCommand = "a".repeat(400);
      const result = formatBashCommand(longCommand);

      expect(result.length).toBe(303); // 300 chars + "..."
      expect(result.endsWith("...")).toBe(true);
    });

    it("should handle empty and null commands", async () => {
      const { formatBashCommand } = await import("./log_parser_shared.cjs");

      expect(formatBashCommand("")).toBe("");
      expect(formatBashCommand(null)).toBe("");
      expect(formatBashCommand(undefined)).toBe("");
    });

    it("should remove leading and trailing whitespace", async () => {
      const { formatBashCommand } = await import("./log_parser_shared.cjs");

      const command = "   echo hello   ";
      const result = formatBashCommand(command);

      expect(result).toBe("echo hello");
    });

    it("should handle multi-line commands", async () => {
      const { formatBashCommand } = await import("./log_parser_shared.cjs");

      const command = "echo line1\necho line2\necho line3";
      const result = formatBashCommand(command);

      expect(result).toBe("echo line1 echo line2 echo line3");
    });
  });

  describe("truncateString", () => {
    it("should truncate strings longer than max length", async () => {
      const { truncateString } = await import("./log_parser_shared.cjs");

      const longStr = "a".repeat(100);
      const result = truncateString(longStr, 50);

      expect(result.length).toBe(53); // 50 chars + "..."
      expect(result.endsWith("...")).toBe(true);
    });

    it("should not truncate strings at or below max length", async () => {
      const { truncateString } = await import("./log_parser_shared.cjs");

      expect(truncateString("hello", 10)).toBe("hello");
      expect(truncateString("1234567890", 10)).toBe("1234567890");
    });

    it("should handle empty and null strings", async () => {
      const { truncateString } = await import("./log_parser_shared.cjs");

      expect(truncateString("", 10)).toBe("");
      expect(truncateString(null, 10)).toBe("");
      expect(truncateString(undefined, 10)).toBe("");
    });

    it("should handle zero max length", async () => {
      const { truncateString } = await import("./log_parser_shared.cjs");

      const result = truncateString("hello", 0);
      expect(result).toBe("...");
    });
  });

  describe("estimateTokens", () => {
    it("should estimate tokens using 4 chars per token", async () => {
      const { estimateTokens } = await import("./log_parser_shared.cjs");

      expect(estimateTokens("test")).toBe(1); // 4 chars = 1 token
      expect(estimateTokens("hello world")).toBe(3); // 11 chars = 2.75 -> 3 tokens
      expect(estimateTokens("a".repeat(100))).toBe(25); // 100 chars = 25 tokens
    });

    it("should round up partial tokens", async () => {
      const { estimateTokens } = await import("./log_parser_shared.cjs");

      expect(estimateTokens("a")).toBe(1); // 1 char = 0.25 -> 1 token
      expect(estimateTokens("ab")).toBe(1); // 2 chars = 0.5 -> 1 token
      expect(estimateTokens("abc")).toBe(1); // 3 chars = 0.75 -> 1 token
    });

    it("should handle empty and null text", async () => {
      const { estimateTokens } = await import("./log_parser_shared.cjs");

      expect(estimateTokens("")).toBe(0);
      expect(estimateTokens(null)).toBe(0);
      expect(estimateTokens(undefined)).toBe(0);
    });

    it("should handle large text", async () => {
      const { estimateTokens } = await import("./log_parser_shared.cjs");

      const largeText = "a".repeat(10000);
      expect(estimateTokens(largeText)).toBe(2500); // 10000 chars = 2500 tokens
    });
  });

  describe("formatMcpName", () => {
    it("should format MCP tool names", async () => {
      const { formatMcpName } = await import("./log_parser_shared.cjs");

      expect(formatMcpName("mcp__github__search_issues")).toBe("github::search_issues");
      expect(formatMcpName("mcp__playwright__navigate")).toBe("playwright::navigate");
      expect(formatMcpName("mcp__server__tool_name")).toBe("server::tool_name");
    });

    it("should handle tool names with multiple underscores", async () => {
      const { formatMcpName } = await import("./log_parser_shared.cjs");

      expect(formatMcpName("mcp__github__get_pull_request_files")).toBe("github::get_pull_request_files");
    });

    it("should return non-MCP names unchanged", async () => {
      const { formatMcpName } = await import("./log_parser_shared.cjs");

      expect(formatMcpName("Bash")).toBe("Bash");
      expect(formatMcpName("Read")).toBe("Read");
      expect(formatMcpName("regular_tool")).toBe("regular_tool");
    });

    it("should handle malformed MCP names", async () => {
      const { formatMcpName } = await import("./log_parser_shared.cjs");

      expect(formatMcpName("mcp__")).toBe("mcp__");
      expect(formatMcpName("mcp__github")).toBe("mcp__github");
    });
  });

  describe("isLikelyCustomAgent", () => {
    it("should identify custom agent names with kebab-case", async () => {
      const { isLikelyCustomAgent } = await import("./log_parser_shared.cjs");

      expect(isLikelyCustomAgent("add-safe-output-type")).toBe(true);
      expect(isLikelyCustomAgent("cli-consistency-checker")).toBe(true);
      expect(isLikelyCustomAgent("agentic-workflows")).toBe(true);
      expect(isLikelyCustomAgent("interactive-agent-designer")).toBe(true);
      expect(isLikelyCustomAgent("technical-doc-writer")).toBe(true);
      expect(isLikelyCustomAgent("shell-2-script")).toBe(true);
    });

    it("should reject single word names without hyphens", async () => {
      const { isLikelyCustomAgent } = await import("./log_parser_shared.cjs");

      expect(isLikelyCustomAgent("bash")).toBe(false);
      expect(isLikelyCustomAgent("CustomTool")).toBe(false);
      expect(isLikelyCustomAgent("read")).toBe(false);
    });

    it("should reject MCP tool names", async () => {
      const { isLikelyCustomAgent } = await import("./log_parser_shared.cjs");

      expect(isLikelyCustomAgent("mcp__github__search_issues")).toBe(false);
      expect(isLikelyCustomAgent("mcp__playwright__navigate")).toBe(false);
    });

    it("should reject safe output/input prefixed names", async () => {
      const { isLikelyCustomAgent } = await import("./log_parser_shared.cjs");

      expect(isLikelyCustomAgent("safeoutputs-create_discussion")).toBe(false);
      expect(isLikelyCustomAgent("safeinputs-get_data")).toBe(false);
      expect(isLikelyCustomAgent("safe-outputs")).toBe(false);
    });

    it("should reject names with uppercase letters", async () => {
      const { isLikelyCustomAgent } = await import("./log_parser_shared.cjs");

      expect(isLikelyCustomAgent("Add-Safe-Output-Type")).toBe(false);
      expect(isLikelyCustomAgent("CLI-Consistency-Checker")).toBe(false);
    });

    it("should handle null and undefined", async () => {
      const { isLikelyCustomAgent } = await import("./log_parser_shared.cjs");

      expect(isLikelyCustomAgent(null)).toBe(false);
      expect(isLikelyCustomAgent(undefined)).toBe(false);
      expect(isLikelyCustomAgent("")).toBe(false);
    });
  });

  describe("generateConversationMarkdown", () => {
    it("should generate markdown from log entries", async () => {
      const { generateConversationMarkdown } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "system",
          subtype: "init",
          model: "test-model",
        },
        {
          type: "assistant",
          message: {
            content: [
              { type: "text", text: "Let me help with that." },
              { type: "tool_use", id: "tool1", name: "Bash", input: { command: "echo hello" } },
            ],
          },
        },
        {
          type: "user",
          message: {
            content: [{ type: "tool_result", tool_use_id: "tool1", content: "hello", is_error: false }],
          },
        },
      ];

      const formatToolCallback = (content, toolResult) => {
        return `Tool: ${content.name}\n\n`;
      };

      const formatInitCallback = initEntry => {
        return `Model: ${initEntry.model}\n\n`;
      };

      const result = generateConversationMarkdown(logEntries, {
        formatToolCallback,
        formatInitCallback,
      });

      expect(result.markdown).toContain("## 🚀 Initialization");
      expect(result.markdown).toContain("Model: test-model");
      expect(result.markdown).toContain("## 🤖 Reasoning");
      expect(result.markdown).toContain("Let me help with that.");
      expect(result.markdown).toContain("Tool: Bash");
      expect(result.markdown).toContain("## 🤖 Commands and Tools");
      expect(result.commandSummary).toHaveLength(1);
      expect(result.commandSummary[0]).toContain("✅");
      expect(result.commandSummary[0]).toContain("echo hello");
    });

    it("should handle empty log entries", async () => {
      const { generateConversationMarkdown } = await import("./log_parser_shared.cjs");

      const result = generateConversationMarkdown([], {
        formatToolCallback: () => "",
        formatInitCallback: () => "",
      });

      expect(result.markdown).toContain("## 🤖 Reasoning");
      expect(result.markdown).toContain("## 🤖 Commands and Tools");
      expect(result.markdown).toContain("No commands or tools used.");
      expect(result.commandSummary).toHaveLength(0);
    });

    it("should skip internal tools in command summary", async () => {
      const { generateConversationMarkdown } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [
              { type: "tool_use", id: "tool1", name: "Read", input: { path: "/file.txt" } },
              { type: "tool_use", id: "tool2", name: "Bash", input: { command: "ls" } },
              { type: "tool_use", id: "tool3", name: "Edit", input: { path: "/file.txt" } },
            ],
          },
        },
      ];

      const result = generateConversationMarkdown(logEntries, {
        formatToolCallback: () => "",
        formatInitCallback: () => "",
      });

      // Should only include Bash, not Read or Edit
      expect(result.commandSummary).toHaveLength(1);
      expect(result.commandSummary[0]).toContain("ls");
    });

    it("should format MCP tool names in command summary", async () => {
      const { generateConversationMarkdown } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [{ type: "tool_use", id: "tool1", name: "mcp__github__search_issues", input: { query: "test" } }],
          },
        },
      ];

      const result = generateConversationMarkdown(logEntries, {
        formatToolCallback: () => "",
        formatInitCallback: () => "",
      });

      expect(result.commandSummary).toHaveLength(1);
      expect(result.commandSummary[0]).toContain("github::search_issues");
    });
  });

  describe("generateInformationSection", () => {
    it("should generate information section with metadata", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const lastEntry = {
        num_turns: 5,
        duration_ms: 125000,
        total_cost_usd: 0.0123,
        usage: {
          input_tokens: 1000,
          output_tokens: 500,
        },
      };

      const result = generateInformationSection(lastEntry);

      expect(result).toContain("## 📊 Information");
      expect(result).toContain("**Turns:** 5");
      expect(result).toContain("**Duration:** 2m 5s");
      expect(result).toContain("**Total Cost:** $0.0123");
      expect(result).toContain("**Token Usage:**");
      expect(result).toContain("- Input: 1,000");
      expect(result).toContain("- Output: 500");
    });

    it("should handle additional info callback", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const lastEntry = {
        num_turns: 3,
      };

      const result = generateInformationSection(lastEntry, {
        additionalInfoCallback: () => "**Custom Info:** test\n\n",
      });

      expect(result).toContain("**Turns:** 3");
      expect(result).toContain("**Custom Info:** test");
    });

    it("should handle cache tokens", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const lastEntry = {
        usage: {
          input_tokens: 1000,
          cache_creation_input_tokens: 500,
          cache_read_input_tokens: 200,
          output_tokens: 300,
        },
      };

      const result = generateInformationSection(lastEntry);

      expect(result).toContain("- Input: 1,000");
      expect(result).toContain("- Cache Creation: 500");
      expect(result).toContain("- Cache Read: 200");
      expect(result).toContain("- Output: 300");
    });

    it("should handle permission denials", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const lastEntry = {
        permission_denials: ["tool1", "tool2", "tool3"],
      };

      const result = generateInformationSection(lastEntry);

      expect(result).toContain("**Permission Denials:** 3");
    });

    it("should handle errors array", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const lastEntry = {
        errors: ["only prompt commands are supported in streaming mode", "another error message"],
      };

      const result = generateInformationSection(lastEntry);

      expect(result).toContain("**Errors:**");
      expect(result).toContain("- only prompt commands are supported in streaming mode");
      expect(result).toContain("- another error message");
    });

    it("should handle single error in errors array", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const lastEntry = {
        errors: ["Connection timeout"],
      };

      const result = generateInformationSection(lastEntry);

      expect(result).toContain("**Errors:**");
      expect(result).toContain("- Connection timeout");
    });

    it("should ignore empty errors array", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const lastEntry = {
        errors: [],
      };

      const result = generateInformationSection(lastEntry);

      expect(result).not.toContain("**Errors:**");
    });

    it("should handle empty lastEntry", async () => {
      const { generateInformationSection } = await import("./log_parser_shared.cjs");

      const result = generateInformationSection(null);

      expect(result).toBe("\n## 📊 Information\n\n");
    });
  });

  describe("formatMcpParameters", () => {
    it("should format MCP parameters", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        query: "test query",
        limit: "10",
        status: "open",
      };

      const result = formatMcpParameters(input);

      expect(result).toContain("query: test query");
      expect(result).toContain("limit: 10");
      expect(result).toContain("status: open");
    });

    it("should truncate long parameter values", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        longValue: "a".repeat(100),
      };

      const result = formatMcpParameters(input);

      expect(result).toContain("...");
      expect(result.length).toBeLessThan(60);
    });

    it("should limit to 4 parameters", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        param1: "value1",
        param2: "value2",
        param3: "value3",
        param4: "value4",
        param5: "value5",
        param6: "value6",
      };

      const result = formatMcpParameters(input);

      expect(result).toContain("param1");
      expect(result).toContain("param2");
      expect(result).toContain("param3");
      expect(result).toContain("param4");
      expect(result).not.toContain("param5");
      expect(result).not.toContain("param6");
      expect(result).toContain("...");
    });

    it("should handle empty input", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const result = formatMcpParameters({});

      expect(result).toBe("");
    });

    it("should format array values correctly", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        items: ["item1", "item2", "item3"],
      };

      const result = formatMcpParameters(input);

      expect(result).toContain("items: [item1, item2, item3]");
      expect(result).not.toContain("[object Object]");
    });

    it("should format small arrays (3 or fewer items)", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        tags: ["tag1", "tag2"],
      };

      const result = formatMcpParameters(input);

      expect(result).toBe("tags: [tag1, tag2]");
    });

    it("should format large arrays with ellipsis", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        items: ["item1", "item2", "item3", "item4", "item5"],
      };

      const result = formatMcpParameters(input);

      expect(result).toContain("items: [item1, item2, ...3 more]");
    });

    it("should format empty arrays", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        emptyList: [],
      };

      const result = formatMcpParameters(input);

      expect(result).toBe("emptyList: []");
    });

    it("should format object values as JSON", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        config: { enabled: true, timeout: 30 },
      };

      const result = formatMcpParameters(input);

      expect(result).toContain('config: {"enabled":true,"timeout":30}');
      expect(result).not.toContain("[object Object]");
    });

    it("should format arrays containing objects", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        tools: [{ name: "tool1" }, { name: "tool2" }],
      };

      const result = formatMcpParameters(input);

      expect(result).toContain('tools: [{"name":"tool1"}, {"name":"tool2"}]');
      expect(result).not.toContain("[object Object]");
    });

    it("should handle mixed parameter types", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        query: "search term",
        filters: ["filter1", "filter2"],
        options: { caseSensitive: false },
        limit: 10,
      };

      const result = formatMcpParameters(input);

      expect(result).toContain("query: search term");
      expect(result).toContain("filters: [filter1, filter2]");
      expect(result).toContain('options: {"caseSensitive":false}');
      expect(result).toContain("limit: 10");
    });

    it("should truncate long array representations", async () => {
      const { formatMcpParameters } = await import("./log_parser_shared.cjs");

      const input = {
        longArray: ["a".repeat(50), "b".repeat(50)],
      };

      const result = formatMcpParameters(input);

      // Should be truncated to 40 chars
      expect(result.length).toBeLessThan(60);
      expect(result).toContain("...");
    });
  });

  describe("formatInitializationSummary", () => {
    it("should format basic initialization info", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        model: "test-model",
        session_id: "session-123",
        cwd: "/home/runner/work/repo/repo",
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Model:** test-model");
      expect(result.markdown).toContain("**Session ID:** session-123");
      expect(result.markdown).toContain("**Working Directory:** .");
    });

    it("should format tools by category", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: ["Bash", "Read", "Edit", "mcp__github__search_issues", "mcp__playwright__navigate", "CustomTool"],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Available Tools:**");
      expect(result.markdown).toContain("**Core:**");
      expect(result.markdown).toContain("Bash");
      expect(result.markdown).toContain("**File Operations:**");
      expect(result.markdown).toContain("Read");
      expect(result.markdown).toContain("Edit");
      expect(result.markdown).toContain("**Git/GitHub:**");
      expect(result.markdown).toContain("github::search_issues");
      expect(result.markdown).toContain("**Playwright:**");
      expect(result.markdown).toContain("playwright::navigate");
      expect(result.markdown).toContain("**Other:**");
      expect(result.markdown).toContain("CustomTool");
    });

    it("should categorize safe output tools", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: ["safeoutputs-create_discussion", "safeoutputs-close_discussion", "safeoutputs-upload_asset", "safeoutputs-missing_tool", "safeoutputs-noop"],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Safe Outputs:**");
      expect(result.markdown).toContain("create_discussion");
      expect(result.markdown).toContain("close_discussion");
      expect(result.markdown).toContain("upload_asset");
      expect(result.markdown).toContain("missing_tool");
      expect(result.markdown).toContain("noop");
      // Should NOT contain the prefix in output
      expect(result.markdown).not.toContain("safeoutputs-");
    });

    it("should categorize builtin tools", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: ["bash", "write_bash", "read_bash", "stop_bash", "list_bash", "grep", "glob", "view", "create", "edit"],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Builtin:**");
      expect(result.markdown).toContain("bash");
      expect(result.markdown).toContain("write_bash");
      expect(result.markdown).toContain("read_bash");
      expect(result.markdown).toContain("stop_bash");
      expect(result.markdown).toContain("list_bash");
      expect(result.markdown).toContain("grep");
      expect(result.markdown).toContain("glob");
      expect(result.markdown).toContain("view");
      expect(result.markdown).toContain("create");
      expect(result.markdown).toContain("edit");
    });

    it("should categorize custom agents", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: ["add-safe-output-type", "cli-consistency-checker", "agentic-workflows", "interactive-agent-designer", "technical-doc-writer"],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Custom Agents:**");
      expect(result.markdown).toContain("add-safe-output-type");
      expect(result.markdown).toContain("cli-consistency-checker");
      expect(result.markdown).toContain("agentic-workflows");
      expect(result.markdown).toContain("interactive-agent-designer");
      expect(result.markdown).toContain("technical-doc-writer");
    });

    it("should categorize mixed tool set correctly", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: [
          // Core
          "Bash",
          // File Operations
          "Read",
          "Edit",
          // Builtin
          "bash",
          "write_bash",
          "grep",
          "report_intent",
          // Safe Outputs
          "safeoutputs-create_discussion",
          "safeoutputs-noop",
          // Safe Inputs
          "safeinputs-get_data",
          // Git/GitHub
          "mcp__github__search_issues",
          // Playwright
          "mcp__playwright__navigate",
          // Serena
          "mcp__serena__replace_symbol_body",
          // MCP (other)
          "mcp__other__tool",
          // Custom Agents
          "add-safe-output-type",
          "technical-doc-writer",
          // Other
          "CustomTool",
        ],
      };

      const result = formatInitializationSummary(initEntry);

      // Verify all categories are present
      expect(result.markdown).toContain("**Core:**");
      expect(result.markdown).toContain("**File Operations:**");
      expect(result.markdown).toContain("**Builtin:**");
      expect(result.markdown).toContain("**Safe Outputs:**");
      expect(result.markdown).toContain("**Safe Inputs:**");
      expect(result.markdown).toContain("**Git/GitHub:**");
      expect(result.markdown).toContain("**Playwright:**");
      expect(result.markdown).toContain("**Serena:**");
      expect(result.markdown).toContain("**MCP:**");
      expect(result.markdown).toContain("**Custom Agents:**");
      expect(result.markdown).toContain("**Other:**");

      // Verify specific tools are in correct categories
      expect(result.markdown).toContain("Bash");
      expect(result.markdown).toContain("github::search_issues");
      expect(result.markdown).toContain("playwright::navigate");
      expect(result.markdown).toContain("serena::replace_symbol_body");
      expect(result.markdown).toContain("other::tool");
    });

    it("should format MCP servers status", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        mcp_servers: [
          { name: "github", status: "connected" },
          { name: "playwright", status: "failed" },
        ],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**MCP Servers:**");
      expect(result.markdown).toContain("✅ github (connected)");
      expect(result.markdown).toContain("❌ playwright (failed)");
      expect(result.mcpFailures).toEqual(["playwright"]);
    });

    it("should call mcpFailureCallback for failed servers", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        mcp_servers: [
          {
            name: "test-server",
            status: "failed",
            error: "Connection timeout",
            stderr: "Error output",
          },
        ],
      };

      const result = formatInitializationSummary(initEntry, {
        mcpFailureCallback: server => {
          return `  - Error: ${server.error}\n  - Stderr: ${server.stderr}\n`;
        },
      });

      expect(result.markdown).toContain("Error: Connection timeout");
      expect(result.markdown).toContain("Stderr: Error output");
    });

    it("should call modelInfoCallback for custom model info", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        model: "premium-model",
        model_info: {
          name: "GPT-5",
          billing: { is_premium: true },
        },
      };

      const result = formatInitializationSummary(initEntry, {
        modelInfoCallback: entry => {
          if (entry.model_info?.billing?.is_premium) {
            return "**Premium:** Yes\n\n";
          }
          return "";
        },
      });

      expect(result.markdown).toContain("**Premium:** Yes");
    });

    it("should include slash commands when option is enabled", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        slash_commands: ["/help", "/reset", "/debug"],
      };

      const result = formatInitializationSummary(initEntry, {
        includeSlashCommands: true,
      });

      expect(result.markdown).toContain("**Slash Commands:** 3 available");
      expect(result.markdown).toContain("/help, /reset, /debug");
    });

    it("should not include slash commands when option is disabled", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        slash_commands: ["/help", "/reset", "/debug"],
      };

      const result = formatInitializationSummary(initEntry, {
        includeSlashCommands: false,
      });

      expect(result.markdown).not.toContain("Slash Commands");
    });

    it("should categorize playwright tools separately", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: ["mcp__playwright__navigate", "mcp__playwright__click", "mcp__playwright__snapshot", "mcp__playwright__take_screenshot"],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Playwright:**");
      expect(result.markdown).toContain("playwright::navigate");
      expect(result.markdown).toContain("playwright::click");
      expect(result.markdown).toContain("playwright::snapshot");
      expect(result.markdown).toContain("playwright::take_screenshot");
      // Should NOT be in the generic MCP category
      expect(result.markdown).not.toMatch(/\*\*MCP:\*\*.*playwright/s);
    });

    it("should categorize serena tools separately", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: ["mcp__serena__replace_symbol_body", "mcp__serena__insert_after_symbol", "mcp__serena__list_symbols"],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Serena:**");
      expect(result.markdown).toContain("serena::replace_symbol_body");
      expect(result.markdown).toContain("serena::insert_after_symbol");
      expect(result.markdown).toContain("serena::list_symbols");
      // Should NOT be in the generic MCP category
      expect(result.markdown).not.toMatch(/\*\*MCP:\*\*.*serena/s);
    });

    it("should categorize safeinputs tools separately", async () => {
      const { formatInitializationSummary } = await import("./log_parser_shared.cjs");

      const initEntry = {
        tools: ["safeinputs-get_data", "safeinputs-query_database", "safe_inputs-fetch_config"],
      };

      const result = formatInitializationSummary(initEntry);

      expect(result.markdown).toContain("**Safe Inputs:**");
      expect(result.markdown).toContain("get_data");
      expect(result.markdown).toContain("query_database");
      expect(result.markdown).toContain("fetch_config");
      // Should NOT contain the prefix in output
      expect(result.markdown).not.toContain("safeinputs-");
      expect(result.markdown).not.toContain("safe_inputs-");
    });
  });

  describe("parseLogEntries", () => {
    it("should parse JSON array format", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = JSON.stringify([
        { type: "system", subtype: "init" },
        { type: "assistant", message: { content: [{ type: "text", text: "Hello" }] } },
      ]);

      const result = parseLogEntries(logContent);

      expect(result).toHaveLength(2);
      expect(result[0].type).toBe("system");
      expect(result[1].type).toBe("assistant");
    });

    it("should parse JSONL format (newline-separated JSON objects)", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = ['{"type": "system", "subtype": "init"}', '{"type": "assistant", "message": {"content": []}}', '{"type": "result", "num_turns": 1}'].join("\n");

      const result = parseLogEntries(logContent);

      expect(result).toHaveLength(3);
      expect(result[0].type).toBe("system");
      expect(result[1].type).toBe("assistant");
      expect(result[2].type).toBe("result");
    });

    it("should handle mixed format with debug logs", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = ["2024-01-01T12:00:00.000Z [DEBUG] Starting...", '{"type": "system", "subtype": "init"}', "2024-01-01T12:00:01.000Z [INFO] Processing...", '{"type": "assistant", "message": {"content": []}}'].join("\n");

      const result = parseLogEntries(logContent);

      expect(result).toHaveLength(2);
      expect(result[0].type).toBe("system");
      expect(result[1].type).toBe("assistant");
    });

    it("should parse JSON array embedded in a line", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = '[{"type": "system", "subtype": "init"}, {"type": "assistant", "message": {"content": []}}]';

      const result = parseLogEntries(logContent);

      expect(result).toHaveLength(2);
      expect(result[0].type).toBe("system");
      expect(result[1].type).toBe("assistant");
    });

    it("should handle lines starting with array notation", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = ["Some debug output", '[{"type": "system", "subtype": "init"}]', '[{"type": "assistant", "message": {"content": []}}]'].join("\n");

      const result = parseLogEntries(logContent);

      expect(result).toHaveLength(2);
      expect(result[0].type).toBe("system");
      expect(result[1].type).toBe("assistant");
    });

    it("should skip empty lines", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = ['{"type": "system", "subtype": "init"}', "", "   ", '{"type": "assistant", "message": {"content": []}}'].join("\n");

      const result = parseLogEntries(logContent);

      expect(result).toHaveLength(2);
    });

    it("should skip invalid JSON lines", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = ['{"type": "system", "subtype": "init"}', '{"invalid json here', '{"type": "assistant", "message": {"content": []}}'].join("\n");

      const result = parseLogEntries(logContent);

      expect(result).toHaveLength(2);
      expect(result[0].type).toBe("system");
      expect(result[1].type).toBe("assistant");
    });

    it("should return null for unrecognized formats", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = "Just plain text with no JSON";

      const result = parseLogEntries(logContent);

      expect(result).toBeNull();
    });

    it("should return null for empty content", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const result = parseLogEntries("");

      expect(result).toBeNull();
    });

    it("should handle content that is not a JSON array", async () => {
      const { parseLogEntries } = await import("./log_parser_shared.cjs");

      const logContent = '{"type": "single-object"}';

      const result = parseLogEntries(logContent);

      // Should fall back to JSONL parsing
      expect(result).toHaveLength(1);
      expect(result[0].type).toBe("single-object");
    });
  });

  describe("formatToolUse", () => {
    it("should format Bash tool use", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "Bash",
        input: { command: "echo hello" },
      };
      const toolResult = {
        is_error: false,
        content: "hello",
      };

      const result = formatToolUse(toolUse, toolResult);

      expect(result).toContain("✅");
      expect(result).toContain("echo hello");
    });

    it("should format Read tool use", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "Read",
        input: { file_path: "/home/runner/work/repo/repo/src/file.js" },
      };
      const toolResult = { is_error: false };

      const result = formatToolUse(toolUse, toolResult);

      expect(result).toContain("✅");
      expect(result).toContain("Read");
      expect(result).toContain("src/file.js");
    });

    it("should format Write/Edit tool use", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "Edit",
        input: { file_path: "/home/runner/work/repo/repo/src/file.js" },
      };
      const toolResult = { is_error: false };

      const result = formatToolUse(toolUse, toolResult);

      expect(result).toContain("✅");
      expect(result).toContain("Write");
      expect(result).toContain("src/file.js");
    });

    it("should format MCP tool use", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "mcp__github__search_issues",
        input: { query: "bug", limit: 10 },
      };
      const toolResult = { is_error: false };

      const result = formatToolUse(toolUse, toolResult);

      expect(result).toContain("✅");
      expect(result).toContain("github::search_issues");
      expect(result).toContain("query: bug");
    });

    it("should show error icon for failed tools", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "Bash",
        input: { command: "false" },
      };
      const toolResult = {
        is_error: true,
        content: "Command failed",
      };

      const result = formatToolUse(toolUse, toolResult);

      expect(result).toContain("❌");
    });

    it("should include details when content is present", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "Bash",
        input: { command: "echo test" },
      };
      const toolResult = {
        is_error: false,
        content: "test output",
      };

      const result = formatToolUse(toolUse, toolResult, { includeDetailedParameters: false });

      expect(result).toContain("<details>");
      expect(result).toContain("<summary>");
      expect(result).toContain("test output");
    });

    it("should include detailed parameters when option is enabled", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "Bash",
        input: { command: "echo test", description: "Test command" },
      };
      const toolResult = {
        is_error: false,
        content: "test output",
      };

      const result = formatToolUse(toolUse, toolResult, { includeDetailedParameters: true });

      expect(result).toContain("**Parameters:**");
      expect(result).toContain("**Response:**");
      expect(result).toContain('"command": "echo test"');
    });

    it("should skip TodoWrite tool", async () => {
      const { formatToolUse } = await import("./log_parser_shared.cjs");

      const toolUse = {
        name: "TodoWrite",
        input: { content: "test" },
      };
      const toolResult = { is_error: false };

      const result = formatToolUse(toolUse, toolResult);

      expect(result).toBe("");
    });
  });

  describe("formatToolCallAsDetails", () => {
    it("should format tool call with sections as HTML details", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "<code>github::list_issues</code>",
        statusIcon: "✅",
        sections: [
          { label: "Parameters", content: '{"state":"open"}', language: "json" },
          { label: "Response", content: '{"items":[]}', language: "json" },
        ],
      });

      expect(result).toContain("<details>");
      expect(result).toContain("<summary>");
      expect(result).toContain("✅");
      expect(result).toContain("github::list_issues");
      expect(result).toContain("**Parameters:**");
      expect(result).toContain("**Response:**");
      expect(result).toContain("```json");
      expect(result).toContain("</details>");
    });

    it("should return summary only when no sections provided", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "<code>github::ping</code>",
        statusIcon: "✅",
      });

      expect(result).not.toContain("<details>");
      expect(result).toContain("✅");
      expect(result).toContain("github::ping");
    });

    it("should return summary only when sections are empty", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "<code>github::ping</code>",
        statusIcon: "✅",
        sections: [
          { label: "Response", content: "" },
          { label: "Error", content: "   " },
        ],
      });

      expect(result).not.toContain("<details>");
      expect(result).toContain("✅");
      expect(result).toContain("github::ping");
    });

    it("should include metadata in summary", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "<code>github::list_issues</code>",
        statusIcon: "✅",
        metadata: "<code>~100t</code>",
        sections: [{ label: "Response", content: '{"items":[]}' }],
      });

      expect(result).toContain("~100t");
    });

    it("should skip status icon if already in summary", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "✅ <code>github::list_issues</code>",
        statusIcon: "✅",
        sections: [{ label: "Response", content: '{"items":[]}' }],
      });

      // Should not have double status icon
      expect(result.match(/✅/g)).toHaveLength(1);
    });

    it("should format bash command correctly", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "<code>bash: ls -la</code>",
        statusIcon: "✅",
        sections: [
          { label: "Command", content: "ls -la", language: "bash" },
          { label: "Output", content: "file1.txt\nfile2.txt" },
        ],
      });

      expect(result).toContain("<details>");
      expect(result).toContain("**Command:**");
      expect(result).toContain("```bash");
      expect(result).toContain("**Output:**");
      expect(result).toContain("file1.txt");
    });

    it("should handle sections without language", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "Tool output",
        statusIcon: "✅",
        sections: [{ label: "Output", content: "Plain text output" }],
      });

      expect(result).toContain("<details>");
      expect(result).toContain("**Output:**");
      expect(result).toContain("``````\n");
      expect(result).toContain("Plain text output");
    });

    it("should skip empty sections", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const result = formatToolCallAsDetails({
        summary: "Tool call",
        statusIcon: "✅",
        sections: [
          { label: "Parameters", content: '{"key":"value"}', language: "json" },
          { label: "Response", content: "" },
          { label: "Error", content: null },
        ],
      });

      expect(result).toContain("**Parameters:**");
      expect(result).not.toContain("**Response:**");
      expect(result).not.toContain("**Error:**");
    });

    it("should truncate content exceeding maxContentLength", async () => {
      const { formatToolCallAsDetails, MAX_TOOL_OUTPUT_LENGTH } = await import("./log_parser_shared.cjs");

      const longContent = "a".repeat(1000);
      const result = formatToolCallAsDetails({
        summary: "Tool call",
        statusIcon: "✅",
        sections: [{ label: "Response", content: longContent }],
      });

      // Should contain truncated content (256 chars) plus "... (truncated)"
      expect(result).toContain("... (truncated)");
      expect(result.length).toBeLessThan(longContent.length + 200);

      // Verify truncation happens at MAX_TOOL_OUTPUT_LENGTH (256)
      expect(MAX_TOOL_OUTPUT_LENGTH).toBe(256);
    });

    it("should allow custom maxContentLength", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const longContent = "a".repeat(200);
      const result = formatToolCallAsDetails({
        summary: "Tool call",
        statusIcon: "✅",
        sections: [{ label: "Response", content: longContent }],
        maxContentLength: 100,
      });

      // Should contain truncated content (100 chars) plus "... (truncated)"
      expect(result).toContain("... (truncated)");
      expect(result).toContain("a".repeat(100));
      expect(result).not.toContain("a".repeat(101));
    });

    it("should not truncate content within maxContentLength", async () => {
      const { formatToolCallAsDetails } = await import("./log_parser_shared.cjs");

      const shortContent = "short content";
      const result = formatToolCallAsDetails({
        summary: "Tool call",
        statusIcon: "✅",
        sections: [{ label: "Response", content: shortContent }],
      });

      expect(result).toContain(shortContent);
      expect(result).not.toContain("truncated");
    });
  });

  describe("StepSummaryTracker", () => {
    it("should track content size", async () => {
      const { StepSummaryTracker } = await import("./log_parser_shared.cjs");

      const tracker = new StepSummaryTracker(1000);

      expect(tracker.add("hello")).toBe(true);
      expect(tracker.getSize()).toBe(5);
      expect(tracker.isLimitReached()).toBe(false);

      expect(tracker.add(" world")).toBe(true);
      expect(tracker.getSize()).toBe(11);
    });

    it("should detect when limit is reached", async () => {
      const { StepSummaryTracker } = await import("./log_parser_shared.cjs");

      const tracker = new StepSummaryTracker(10);

      expect(tracker.add("12345")).toBe(true);
      expect(tracker.isLimitReached()).toBe(false);

      // This should fail because it would exceed the limit
      expect(tracker.add("123456")).toBe(false);
      expect(tracker.isLimitReached()).toBe(true);
    });

    it("should reject all content after limit is reached", async () => {
      const { StepSummaryTracker } = await import("./log_parser_shared.cjs");

      const tracker = new StepSummaryTracker(5);

      expect(tracker.add("123456")).toBe(false);
      expect(tracker.isLimitReached()).toBe(true);

      // Any subsequent add should fail
      expect(tracker.add("a")).toBe(false);
      expect(tracker.add("")).toBe(false);
    });

    it("should reset properly", async () => {
      const { StepSummaryTracker } = await import("./log_parser_shared.cjs");

      const tracker = new StepSummaryTracker(10);

      tracker.add("12345678901"); // Exceeds limit
      expect(tracker.isLimitReached()).toBe(true);

      tracker.reset();
      expect(tracker.isLimitReached()).toBe(false);
      expect(tracker.getSize()).toBe(0);

      expect(tracker.add("hello")).toBe(true);
    });

    it("should use MAX_STEP_SUMMARY_SIZE as default", async () => {
      const { StepSummaryTracker, MAX_STEP_SUMMARY_SIZE } = await import("./log_parser_shared.cjs");

      const tracker = new StepSummaryTracker();

      // Default is 1000KB
      expect(MAX_STEP_SUMMARY_SIZE).toBe(1000 * 1024);
      expect(tracker.maxSize).toBe(1000 * 1024);
    });
  });

  describe("generateConversationMarkdown with size tracking", () => {
    it("should return sizeLimitReached when tracker is not provided", async () => {
      const { generateConversationMarkdown } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [{ type: "text", text: "Hello" }],
          },
        },
      ];

      const result = generateConversationMarkdown(logEntries, {
        formatToolCallback: () => "",
        formatInitCallback: () => "",
      });

      expect(result.sizeLimitReached).toBe(false);
    });

    it("should stop rendering when size limit is reached", async () => {
      const { generateConversationMarkdown, StepSummaryTracker } = await import("./log_parser_shared.cjs");

      // Use a very small limit to trigger truncation
      const tracker = new StepSummaryTracker(50);

      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [
              { type: "text", text: "This is a longer message that should trigger the size limit" },
              { type: "text", text: "This should not be included" },
            ],
          },
        },
      ];

      const result = generateConversationMarkdown(logEntries, {
        formatToolCallback: () => "",
        formatInitCallback: () => "",
        summaryTracker: tracker,
      });

      expect(result.sizeLimitReached).toBe(true);
      expect(result.markdown).toContain("size limit reached");
    });
  });

  describe("constants", () => {
    it("should export MAX_TOOL_OUTPUT_LENGTH as 256", async () => {
      const { MAX_TOOL_OUTPUT_LENGTH } = await import("./log_parser_shared.cjs");
      expect(MAX_TOOL_OUTPUT_LENGTH).toBe(256);
    });

    it("should export MAX_STEP_SUMMARY_SIZE as 1000KB", async () => {
      const { MAX_STEP_SUMMARY_SIZE } = await import("./log_parser_shared.cjs");
      expect(MAX_STEP_SUMMARY_SIZE).toBe(1000 * 1024);
    });
  });

  describe("generatePlainTextSummary", () => {
    it("should generate plain text summary with model info", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init", model: "gpt-5" },
        { type: "result", num_turns: 3, duration_ms: 60000 },
      ];

      const result = generatePlainTextSummary(logEntries, {
        model: "gpt-5",
        parserName: "TestParser",
      });

      expect(result).toContain("=== TestParser Execution Summary ===");
      expect(result).toContain("Model: gpt-5");
      expect(result).toContain("Turns: 3");
      expect(result).toContain("Duration: 1m");
    });

    it("should include tool usage in conversation", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init", model: "test-model" },
        {
          type: "assistant",
          message: {
            content: [
              { type: "tool_use", id: "1", name: "Bash", input: { command: "echo test" } },
              { type: "tool_use", id: "2", name: "mcp__github__create_issue", input: {} },
            ],
          },
        },
        {
          type: "user",
          message: {
            content: [
              { type: "tool_result", tool_use_id: "1", is_error: false },
              { type: "tool_result", tool_use_id: "2", is_error: true },
            ],
          },
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Conversation:");
      expect(result).toContain("✓ $ echo test");
      expect(result).toContain("✗ github-create_issue");
      expect(result).toContain("Tools: 1/2 succeeded");
    });

    it("should limit conversation output", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      // Create 2600 tool uses (which will exceed MAX_CONVERSATION_LINES of 5000)
      // Each tool use creates 2 lines (tool + blank), so 2600 * 2 = 5200 lines
      const toolUses = [];
      const toolResults = [];
      for (let i = 0; i < 2600; i++) {
        toolUses.push({ type: "tool_use", id: `${i}`, name: "Bash", input: { command: `cmd${i}` } });
        toolResults.push({ type: "tool_result", tool_use_id: `${i}`, is_error: false });
      }

      const logEntries = [
        { type: "system", subtype: "init" },
        { type: "assistant", message: { content: toolUses } },
        { type: "user", message: { content: toolResults } },
        { type: "result", num_turns: 1 },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("... (conversation truncated)");
    });

    it("should include token usage and cost", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init" },
        {
          type: "result",
          num_turns: 2,
          usage: { input_tokens: 1000, output_tokens: 500 },
          total_cost_usd: 0.0025,
        },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Tokens: 1,500 total (1,000 in / 500 out)");
      expect(result).toContain("Cost: $0.0025");
    });

    it("should skip internal tools in conversation", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init" },
        {
          type: "assistant",
          message: {
            content: [
              { type: "tool_use", id: "1", name: "Read", input: {} },
              { type: "tool_use", id: "2", name: "Write", input: {} },
              { type: "tool_use", id: "3", name: "Bash", input: { command: "test" } },
            ],
          },
        },
        {
          type: "user",
          message: {
            content: [
              { type: "tool_result", tool_use_id: "1", is_error: false },
              { type: "tool_result", tool_use_id: "2", is_error: false },
              { type: "tool_result", tool_use_id: "3", is_error: false },
            ],
          },
        },
        { type: "result" },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      // Should only show Bash, not Read or Write
      expect(result).toContain("✓ $ test");
      expect(result).not.toContain("Read");
      expect(result).not.toContain("Write");
    });

    it("should include conversation section", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "system",
          subtype: "init",
          model: "test-model",
          tools: [
            "bash",
            "view",
            "create",
            "edit",
            "mcp__github__search_issues",
            "mcp__github__create_issue",
            "mcp__playwright__navigate",
            "safeoutputs-create_issue",
            "safeoutputs-add_comment",
            "agentic-workflows",
            "interactive-agent-designer",
          ],
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Conversation:");
      expect(result).toContain("Statistics:");
    });

    it("should handle empty conversation gracefully", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init", model: "test-model", tools: [] },
        { type: "result", num_turns: 1 },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      // Should contain Conversation section but no content
      expect(result).toContain("Conversation:");
      expect(result).toContain("Statistics:");
    });

    it("should work without init entry", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [{ type: "result", num_turns: 1 }];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      // Should work without init entry
      expect(result).toContain("=== Agent Execution Summary ===");
      expect(result).toContain("Conversation:");
    });

    it("should include agent response text in conversation", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init", model: "test-model" },
        {
          type: "assistant",
          message: {
            content: [
              { type: "text", text: "I'll help you with that task." },
              { type: "tool_use", id: "1", name: "Bash", input: { command: "echo hello" } },
            ],
          },
        },
        {
          type: "user",
          message: {
            content: [{ type: "tool_result", tool_use_id: "1", is_error: false, content: "hello" }],
          },
        },
        {
          type: "assistant",
          message: {
            content: [{ type: "text", text: "The command executed successfully!" }],
          },
        },
        { type: "result", num_turns: 2 },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Conversation:");
      expect(result).toContain("Agent: I'll help you with that task.");
      expect(result).toContain("✓ $ echo hello");
      expect(result).toContain("Agent: The command executed successfully!");
    });

    it("should truncate long agent responses", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const longText = "a".repeat(600); // Longer than MAX_TEXT_LENGTH of 500
      const logEntries = [
        { type: "system", subtype: "init" },
        {
          type: "assistant",
          message: {
            content: [{ type: "text", text: longText }],
          },
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Agent: " + "a".repeat(500) + "...");
      expect(result).not.toContain("a".repeat(600));
    });

    it("should handle multi-line agent responses", async () => {
      const { generatePlainTextSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init" },
        {
          type: "assistant",
          message: {
            content: [{ type: "text", text: "Line 1\nLine 2\nLine 3" }],
          },
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generatePlainTextSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Agent: Line 1");
      expect(result).toContain("Agent: Line 2");
      expect(result).toContain("Agent: Line 3");
    });
  });

  describe("generateCopilotCliStyleSummary", () => {
    it("should generate markdown-formatted Copilot CLI style summary", async () => {
      const { generateCopilotCliStyleSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        { type: "system", subtype: "init", model: "gpt-5" },
        {
          type: "assistant",
          message: {
            content: [{ type: "text", text: "I'll help you explore the repository structure first." }],
          },
        },
        {
          type: "assistant",
          message: {
            content: [{ type: "tool_use", id: "1", name: "Bash", input: { command: "ls -la" } }],
          },
        },
        {
          type: "user",
          message: {
            content: [{ type: "tool_result", tool_use_id: "1", is_error: false, content: "file1.txt\nfile2.txt\nfile3.txt" }],
          },
        },
        { type: "result", num_turns: 3, duration_ms: 45000, usage: { input_tokens: 1500, output_tokens: 800 }, total_cost_usd: 0.0023 },
      ];

      const result = generateCopilotCliStyleSummary(logEntries, { parserName: "Agent" });

      // Check that output is wrapped in code block
      expect(result).toMatch(/^```\n/);
      expect(result).toMatch(/\n```$/);

      // Check for Conversation section
      expect(result).toContain("Conversation:");

      // Check for Agent message
      expect(result).toContain("Agent: I'll help you explore the repository structure first.");

      // Check for tool execution with success icon
      expect(result).toContain("✓ $ ls -la");
      expect(result).toContain("   └ 3 lines...");

      // Check for Statistics section
      expect(result).toContain("Statistics:");
      expect(result).toContain("  Turns: 3");
      expect(result).toContain("  Duration: 45s");
      expect(result).toContain("  Tools: 1/1 succeeded");
      expect(result).toContain("  Tokens: 2,300 total (1,500 in / 800 out)");
      expect(result).toContain("  Cost: $0.0023");
    });

    it("should show error icon for failed tools", async () => {
      const { generateCopilotCliStyleSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [{ type: "tool_use", id: "1", name: "mcp__github__search_issues", input: {} }],
          },
        },
        {
          type: "user",
          message: {
            content: [{ type: "tool_result", tool_use_id: "1", is_error: true, content: "Error: API rate limit exceeded" }],
          },
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generateCopilotCliStyleSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("✗ github-search_issues");
      expect(result).toContain("   └ Error: API rate limit exceeded");
      expect(result).toContain("  Tools: 0/1 succeeded");
    });

    it("should truncate long agent messages", async () => {
      const { generateCopilotCliStyleSummary } = await import("./log_parser_shared.cjs");

      const longText = "a".repeat(600);
      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [{ type: "text", text: longText }],
          },
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generateCopilotCliStyleSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Agent: " + "a".repeat(500) + "...");
    });

    it("should skip internal file operation tools", async () => {
      const { generateCopilotCliStyleSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [
              { type: "tool_use", id: "1", name: "Read", input: { path: "/test.txt" } },
              { type: "tool_use", id: "2", name: "Bash", input: { command: "echo test" } },
              { type: "tool_use", id: "3", name: "Write", input: { path: "/output.txt" } },
            ],
          },
        },
        {
          type: "user",
          message: {
            content: [
              { type: "tool_result", tool_use_id: "1", is_error: false },
              { type: "tool_result", tool_use_id: "2", is_error: false },
              { type: "tool_result", tool_use_id: "3", is_error: false },
            ],
          },
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generateCopilotCliStyleSummary(logEntries, { parserName: "Agent" });

      // Should only show Bash command, not Read/Write
      expect(result).toContain("✓ $ echo test");
      expect(result).not.toContain("Read");
      expect(result).not.toContain("Write");

      // Tool count should only include external tools (Bash)
      expect(result).toContain("  Tools: 1/1 succeeded");
    });

    it("should handle multiline agent responses", async () => {
      const { generateCopilotCliStyleSummary } = await import("./log_parser_shared.cjs");

      const logEntries = [
        {
          type: "assistant",
          message: {
            content: [{ type: "text", text: "Line 1\nLine 2\nLine 3" }],
          },
        },
        { type: "result", num_turns: 1 },
      ];

      const result = generateCopilotCliStyleSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("Agent: Line 1");
      expect(result).toContain("Agent: Line 2");
      expect(result).toContain("Agent: Line 3");
    });

    it("should truncate conversation when it exceeds max lines", async () => {
      const { generateCopilotCliStyleSummary } = await import("./log_parser_shared.cjs");

      // Create 2600 tool uses (which will exceed MAX_CONVERSATION_LINES of 5000)
      // Each tool use creates 2 lines (tool + blank), so 2600 * 2 = 5200 lines
      const toolUses = [];
      const toolResults = [];
      for (let i = 0; i < 2600; i++) {
        toolUses.push({ type: "tool_use", id: `${i}`, name: "Bash", input: { command: `cmd${i}` } });
        toolResults.push({ type: "tool_result", tool_use_id: `${i}`, is_error: false });
      }

      const logEntries = [
        { type: "system", subtype: "init" },
        { type: "assistant", message: { content: toolUses } },
        { type: "user", message: { content: toolResults } },
        { type: "result", num_turns: 1 },
      ];

      const result = generateCopilotCliStyleSummary(logEntries, { parserName: "Agent" });

      expect(result).toContain("... (conversation truncated)");
    });
  });

  describe("formatSafeOutputsPreview", () => {
    it("should return empty string for empty content", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      expect(formatSafeOutputsPreview("")).toBe("");
      expect(formatSafeOutputsPreview("   ")).toBe("");
    });

    it("should format single entry in plain text mode", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const safeOutputs = JSON.stringify({ type: "create_issue", title: "Bug found", body: "Description here" });
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: true });

      expect(result).toContain("Safe Outputs Preview:");
      expect(result).toContain("Total: 1 entry");
      expect(result).toContain("[1] create_issue");
      expect(result).toContain("Title: Bug found");
      expect(result).toContain("Body: Description here");
    });

    it("should format single entry in markdown mode", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const safeOutputs = JSON.stringify({ type: "create_issue", title: "Bug found", body: "Description here" });
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: false });

      expect(result).toContain("<summary>Safe Outputs</summary>");
      expect(result).toContain("**Total Entries:** 1");
      expect(result).toContain("**1. create_issue**");
      expect(result).toContain("**Title:** Bug found");
      expect(result).toContain("<details>");
      expect(result).toContain("<summary>Preview</summary>");
    });

    it("should format multiple entries", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const entries = [
        { type: "create_issue", title: "Issue 1", body: "Body 1" },
        { type: "add_comment", body: "Comment text" },
        { type: "add_labels", labels: ["bug", "enhancement"] },
      ];
      const safeOutputs = entries.map(e => JSON.stringify(e)).join("\n");
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: false });

      expect(result).toContain("**Total Entries:** 3");
      expect(result).toContain("**1. create_issue**");
      expect(result).toContain("**2. add_comment**");
      expect(result).toContain("**3. add_labels**");
    });

    it("should truncate and show more indicator", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const entries = [];
      for (let i = 0; i < 10; i++) {
        entries.push({ type: "create_issue", title: `Issue ${i}`, body: `Body ${i}` });
      }
      const safeOutputs = entries.map(e => JSON.stringify(e)).join("\n");
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: false, maxEntries: 3 });

      expect(result).toContain("**Total Entries:** 10");
      expect(result).toContain("**1. create_issue**");
      expect(result).toContain("**2. create_issue**");
      expect(result).toContain("**3. create_issue**");
      expect(result).toContain("... and 7 more entries");
    });

    it("should handle entries without title or body", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const safeOutputs = JSON.stringify({ type: "noop" });
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: true });

      expect(result).toContain("[1] noop");
      expect(result).not.toContain("Title:");
      expect(result).not.toContain("Body:");
    });

    it("should skip invalid JSON lines", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const safeOutputs = `{"type": "create_issue", "title": "Valid"}\ninvalid json line\n{"type": "add_comment", "body": "Also valid"}`;
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: false });

      expect(result).toContain("**Total Entries:** 2");
      expect(result).toContain("**1. create_issue**");
      expect(result).toContain("**2. add_comment**");
    });

    it("should truncate long titles and bodies", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const longTitle = "A".repeat(100);
      const longBody = "B".repeat(300);
      const safeOutputs = JSON.stringify({ type: "create_issue", title: longTitle, body: longBody });
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: true });

      // Plain text truncates title to 60 chars
      expect(result).toContain("Title:");
      expect(result).not.toContain("A".repeat(61));

      // Plain text truncates body to 80 chars
      expect(result).toContain("Body:");
      expect(result).not.toContain("B".repeat(81));
    });

    it("should handle entries with newlines in body", async () => {
      const { formatSafeOutputsPreview } = await import("./log_parser_shared.cjs");

      const safeOutputs = JSON.stringify({ type: "create_issue", title: "Multi-line", body: "Line 1\nLine 2\nLine 3" });
      const result = formatSafeOutputsPreview(safeOutputs, { isPlainText: true });

      // In plain text mode, newlines should be replaced with spaces
      expect(result).toContain("Body: Line 1 Line 2 Line 3");
    });
  });

  describe("wrapAgentLogInSection", () => {
    it("should wrap markdown in details/summary with default open attribute", async () => {
      const { wrapAgentLogInSection } = await import("./log_parser_shared.cjs");

      const markdown = "```\nConversation:\n\nAgent: Hello\n```";
      const result = wrapAgentLogInSection(markdown, { parserName: "Copilot" });

      expect(result).toContain("<details open>");
      expect(result).toContain("<summary>🤖 Copilot CLI Session</summary>");
      expect(result).toContain(markdown);
      expect(result).toContain("</details>");
    });

    it("should support custom parser names", async () => {
      const { wrapAgentLogInSection } = await import("./log_parser_shared.cjs");

      const markdown = "Test content";
      const result = wrapAgentLogInSection(markdown, { parserName: "Claude" });

      expect(result).toContain("🤖 Claude CLI Session");
    });

    it("should allow closed state when open is false", async () => {
      const { wrapAgentLogInSection } = await import("./log_parser_shared.cjs");

      const markdown = "Test content";
      const result = wrapAgentLogInSection(markdown, { parserName: "Copilot", open: false });

      expect(result).toContain("<details>");
      expect(result).not.toContain("<details open>");
    });

    it("should default to Agent parser name when not provided", async () => {
      const { wrapAgentLogInSection } = await import("./log_parser_shared.cjs");

      const markdown = "Test content";
      const result = wrapAgentLogInSection(markdown);

      expect(result).toContain("🤖 Agent CLI Session");
    });

    it("should return empty string for empty or undefined markdown", async () => {
      const { wrapAgentLogInSection } = await import("./log_parser_shared.cjs");

      expect(wrapAgentLogInSection("")).toBe("");
      expect(wrapAgentLogInSection("   ")).toBe("");
    });

    it("should properly escape markdown content", async () => {
      const { wrapAgentLogInSection } = await import("./log_parser_shared.cjs");

      const markdown = "Content with <tags> and `code`";
      const result = wrapAgentLogInSection(markdown, { parserName: "Copilot" });

      expect(result).toContain(markdown);
      expect(result).toContain("<details open>");
      expect(result).toContain("</details>");
    });
  });

  describe("wrapLogParser", () => {
    it("should call parser function and return result on success", async () => {
      const { wrapLogParser } = await import("./log_parser_shared.cjs");

      const mockParser = content => {
        return {
          markdown: "## Success\n\nParsed content",
          mcpFailures: [],
          maxTurnsHit: false,
          logEntries: [{ type: "system" }],
        };
      };

      const result = wrapLogParser(mockParser, "TestParser", "test log content");

      expect(result.markdown).toContain("## Success");
      expect(result.markdown).toContain("Parsed content");
      expect(result.mcpFailures).toEqual([]);
      expect(result.maxTurnsHit).toBe(false);
      expect(result.logEntries).toHaveLength(1);
    });

    it("should catch errors and return error result", async () => {
      const { wrapLogParser } = await import("./log_parser_shared.cjs");

      const mockParser = content => {
        throw new Error("Parse failed");
      };

      const result = wrapLogParser(mockParser, "TestParser", "test log content");

      expect(result.markdown).toContain("Error parsing TestParser log");
      expect(result.markdown).toContain("Parse failed");
      expect(result.mcpFailures).toEqual([]);
      expect(result.maxTurnsHit).toBe(false);
      expect(result.logEntries).toEqual([]);
    });

    it("should handle non-Error exceptions", async () => {
      const { wrapLogParser } = await import("./log_parser_shared.cjs");

      const mockParser = content => {
        throw "String error";
      };

      const result = wrapLogParser(mockParser, "TestParser", "test log content");

      expect(result.markdown).toContain("Error parsing TestParser log");
      expect(result.markdown).toContain("String error");
    });

    it("should preserve engine-specific result properties", async () => {
      const { wrapLogParser } = await import("./log_parser_shared.cjs");

      const mockParser = content => {
        return {
          markdown: "## Result",
          customField: "custom value",
        };
      };

      const result = wrapLogParser(mockParser, "TestParser", "test content");

      expect(result.markdown).toBe("## Result");
      expect(result.customField).toBe("custom value");
    });

    it("should use correct parser name in error message", async () => {
      const { wrapLogParser } = await import("./log_parser_shared.cjs");

      const mockParser = content => {
        throw new Error("Test error");
      };

      const claudeResult = wrapLogParser(mockParser, "Claude", "content");
      const copilotResult = wrapLogParser(mockParser, "Copilot", "content");
      const codexResult = wrapLogParser(mockParser, "Codex", "content");

      expect(claudeResult.markdown).toContain("Error parsing Claude log");
      expect(copilotResult.markdown).toContain("Error parsing Copilot log");
      expect(codexResult.markdown).toContain("Error parsing Codex log");
    });
  });

  describe("createEngineLogParser", () => {
    it("should create a main function with correct configuration", async () => {
      const { createEngineLogParser } = await import("./log_parser_shared.cjs");

      const mockParseFunction = logContent => ({
        markdown: `Parsed: ${logContent}`,
        mcpFailures: [],
        maxTurnsHit: false,
        logEntries: [],
      });

      const main = createEngineLogParser({
        parserName: "TestEngine",
        parseFunction: mockParseFunction,
        supportsDirectories: true,
      });

      expect(typeof main).toBe("function");
      expect(main.constructor.name).toBe("AsyncFunction");
    });

    it("should use default supportsDirectories value", async () => {
      const { createEngineLogParser } = await import("./log_parser_shared.cjs");

      const mockParseFunction = logContent => ({
        markdown: `Parsed: ${logContent}`,
      });

      const main = createEngineLogParser({
        parserName: "TestEngine",
        parseFunction: mockParseFunction,
        // supportsDirectories not specified, should default to false
      });

      expect(typeof main).toBe("function");
    });

    it("should accept all valid configuration options", async () => {
      const { createEngineLogParser } = await import("./log_parser_shared.cjs");

      const mockParseFunction = logContent => "Result";

      // Test with supportsDirectories: false
      const main1 = createEngineLogParser({
        parserName: "Engine1",
        parseFunction: mockParseFunction,
        supportsDirectories: false,
      });
      expect(typeof main1).toBe("function");

      // Test with supportsDirectories: true
      const main2 = createEngineLogParser({
        parserName: "Engine2",
        parseFunction: mockParseFunction,
        supportsDirectories: true,
      });
      expect(typeof main2).toBe("function");
    });
  });
});
