import { describe, it, expect, beforeEach, vi } from "vitest";

describe("parse_codex_log.cjs", () => {
  let mockCore;
  let parseCodexLog;
  let formatCodexToolCall;
  let formatCodexBashCall;
  let truncateString;
  let estimateTokens;
  let formatDuration;
  let extractMCPInitialization;

  beforeEach(async () => {
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
    global.core = mockCore;

    // Import the parse_codex_log module
    const module = await import("./parse_codex_log.cjs");
    parseCodexLog = module.parseCodexLog;
    formatCodexToolCall = module.formatCodexToolCall;
    formatCodexBashCall = module.formatCodexBashCall;
    extractMCPInitialization = module.extractMCPInitialization;

    // Import shared utilities from log_parser_shared.cjs
    const sharedModule = await import("./log_parser_shared.cjs");
    truncateString = sharedModule.truncateString;
    estimateTokens = sharedModule.estimateTokens;
    formatDuration = sharedModule.formatDuration;
  });

  describe("parseCodexLog function", () => {
    it("should parse basic tool call with success", () => {
      const logContent = `tool github.list_pull_requests({"state":"open"})
github.list_pull_requests(...) success in 123ms:
{"items": [{"number": 1}]}`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("## 🤖 Reasoning");
      expect(result.markdown).toContain("## 🤖 Commands and Tools");
      expect(result.markdown).toContain("github::list_pull_requests");
      expect(result.markdown).toContain("✅");
    });

    it("should parse tool call with failure", () => {
      const logContent = `tool github.create_issue({"title":"Test"})
github.create_issue(...) failed in 456ms:
{"error": "permission denied"}`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("github::create_issue");
      expect(result.markdown).toContain("❌");
    });

    it("should parse thinking sections", () => {
      const logContent = `thinking
I need to analyze the repository structure to understand the codebase
Let me start by listing the files in the root directory`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("## 🤖 Reasoning");
      expect(result.markdown).toContain("I need to analyze the repository structure");
      expect(result.markdown).toContain("Let me start by listing the files");
    });

    it("should skip metadata lines", () => {
      const logContent = `OpenAI Codex v1.0
--------
workdir: /tmp/test
model: gpt-4
provider: openai
thinking
This is actual thinking content`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).not.toContain("OpenAI Codex");
      expect(result.markdown).not.toContain("workdir");
      expect(result.markdown).not.toContain("model:");
      expect(result.markdown).toContain("This is actual thinking content");
    });

    it("should skip debug and timestamp lines", () => {
      const logContent = `DEBUG codex: starting session
2024-01-15T12:30:00.000Z DEBUG processing request
INFO codex: tool call completed
thinking
Actual thinking content that is long enough to be included`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).not.toContain("DEBUG codex");
      expect(result.markdown).not.toContain("INFO codex");
      expect(result.markdown).toContain("Actual thinking content");
    });

    it("should parse bash commands", () => {
      const logContent = `[2024-01-15T12:30:00.000Z] exec bash -lc 'ls -la'
bash -lc 'ls -la' succeeded in 50ms:
total 8
-rw-r--r-- 1 user user 100 Jan 15 12:30 file.txt`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("bash: ls -la");
      expect(result.markdown).toContain("✅");
    });

    it("should extract total tokens from log", () => {
      const logContent = `tool github.list_issues({})
total_tokens: 1500
tokens used
1,500`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("📊 Information");
      expect(result.markdown).toContain("Total Tokens Used");
      expect(result.markdown).toContain("1,500");
    });

    it("should count tool calls", () => {
      const logContent = `ToolCall: github__list_issues {}
ToolCall: github__create_comment {}
ToolCall: github__add_labels {}`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("**Tool Calls:** 3");
    });

    it("should handle empty log content", () => {
      const result = parseCodexLog("");

      expect(result.markdown).toContain("## 🤖 Reasoning");
      expect(result.markdown).toContain("## 🤖 Commands and Tools");
    });

    it("should handle log with errors gracefully", () => {
      const malformedLog = null;
      const result = parseCodexLog(malformedLog);

      expect(result.markdown).toContain("No log content provided");
      expect(result.markdown).toContain("## 🤖 Commands and Tools");
      expect(result.markdown).toContain("## 🤖 Reasoning");
    });

    it("should handle tool calls without responses", () => {
      const logContent = `tool github.list_issues({})`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("github::list_issues");
      expect(result.markdown).toContain("❓"); // Unknown status
    });

    it("should filter out short lines in thinking sections", () => {
      const logContent = `thinking
Short
This is a long enough line to be included in the thinking section
x`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("This is a long enough line");
      expect(result.markdown).not.toContain("Short\n\n");
      expect(result.markdown).not.toContain("x\n\n");
    });

    it("should handle ToolCall format", () => {
      const logContent = `ToolCall: github__create_issue {"title":"Test"}`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("📊 Information");
      expect(result.markdown).toContain("**Tool Calls:** 1");
    });

    it("should handle tokens with commas in final count", () => {
      const logContent = `tokens used
12,345`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("12,345");
    });
  });

  describe("formatCodexToolCall function", () => {
    it("should format tool call with response", () => {
      const result = formatCodexToolCall("github", "list_issues", '{"state":"open"}', '{"items":[]}', "✅");

      expect(result).toContain("<details>");
      expect(result).toContain("<summary>");
      expect(result).toContain("github::list_issues");
      expect(result).toContain("✅");
      expect(result).toContain("Parameters:");
      expect(result).toContain("Response:");
      expect(result).toContain("```json");
    });

    it("should format tool call without response - shows parameters in details", () => {
      const result = formatCodexToolCall("github", "create_issue", '{"title":"Test"}', "", "❌");

      // With the new consistent behavior, tool calls always use details when there are parameters
      expect(result).toContain("<details>");
      expect(result).toContain("github::create_issue");
      expect(result).toContain("❌");
      expect(result).toContain("Parameters:");
      expect(result).not.toContain("Response:");
    });

    it("should format tool call without any content - no details", () => {
      const result = formatCodexToolCall("github", "ping", "", "", "✅");

      // When there are no parameters and no response, no details section
      expect(result).not.toContain("<details>");
      expect(result).toContain("github::ping");
      expect(result).toContain("✅");
    });

    it("should include token estimate", () => {
      const result = formatCodexToolCall("github", "get_issue", '{"number":123}', '{"title":"Test issue"}', "✅");

      expect(result).toMatch(/~\d+t/);
    });
  });

  describe("formatCodexBashCall function", () => {
    it("should format bash call with output", () => {
      const result = formatCodexBashCall("ls -la", "file1.txt\nfile2.txt", "✅");

      expect(result).toContain("<details>");
      expect(result).toContain("bash: ls -la");
      expect(result).toContain("✅");
      expect(result).toContain("Command:");
      expect(result).toContain("Output:");
    });

    it("should format bash call without output - shows command in details", () => {
      const result = formatCodexBashCall("mkdir test_dir", "", "✅");

      // With the new consistent behavior, bash calls always include the command in details
      expect(result).toContain("<details>");
      expect(result).toContain("bash: mkdir test_dir");
      expect(result).toContain("✅");
      expect(result).toContain("Command:");
      expect(result).not.toContain("Output:");
    });

    it("should truncate long commands", () => {
      const longCommand = "echo " + "x".repeat(100);
      const result = formatCodexBashCall(longCommand, "output", "✅");

      expect(result).toContain("...");
      expect(result.split("...")[0].length).toBeLessThan(longCommand.length);
    });
  });

  describe("truncateString function", () => {
    it("should not truncate short strings", () => {
      expect(truncateString("hello", 10)).toBe("hello");
    });

    it("should truncate long strings", () => {
      expect(truncateString("hello world this is a long string", 10)).toBe("hello worl...");
    });

    it("should handle empty strings", () => {
      expect(truncateString("", 10)).toBe("");
    });

    it("should handle null/undefined", () => {
      expect(truncateString(null, 10)).toBe("");
      expect(truncateString(undefined, 10)).toBe("");
    });
  });

  describe("estimateTokens function", () => {
    it("should estimate tokens using 4 chars per token", () => {
      expect(estimateTokens("1234")).toBe(1);
      expect(estimateTokens("12345678")).toBe(2);
    });

    it("should handle empty strings", () => {
      expect(estimateTokens("")).toBe(0);
    });

    it("should handle null/undefined", () => {
      expect(estimateTokens(null)).toBe(0);
      expect(estimateTokens(undefined)).toBe(0);
    });

    it("should round up", () => {
      expect(estimateTokens("123")).toBe(1); // 3/4 = 0.75, rounds up to 1
      expect(estimateTokens("12345")).toBe(2); // 5/4 = 1.25, rounds up to 2
    });
  });

  describe("formatDuration function", () => {
    it("should format seconds", () => {
      expect(formatDuration(1000)).toBe("1s");
      expect(formatDuration(5000)).toBe("5s");
      expect(formatDuration(59000)).toBe("59s");
    });

    it("should format minutes", () => {
      expect(formatDuration(60000)).toBe("1m");
      expect(formatDuration(120000)).toBe("2m");
    });

    it("should format minutes and seconds", () => {
      expect(formatDuration(90000)).toBe("1m 30s");
      expect(formatDuration(125000)).toBe("2m 5s");
    });

    it("should handle zero or negative values", () => {
      expect(formatDuration(0)).toBe("");
      expect(formatDuration(-1000)).toBe("");
    });

    it("should handle null/undefined", () => {
      expect(formatDuration(null)).toBe("");
      expect(formatDuration(undefined)).toBe("");
    });

    it("should round to nearest second", () => {
      expect(formatDuration(1499)).toBe("1s");
      expect(formatDuration(1500)).toBe("2s");
    });
  });

  describe("extractMCPInitialization function", () => {
    it("should extract MCP server initialization", () => {
      const lines = [
        "2025-01-15T12:00:00.123Z DEBUG codex_core::mcp: Initializing MCP servers from config",
        "2025-01-15T12:00:00.234Z DEBUG codex_core::mcp: Found 3 MCP servers in configuration",
        "2025-01-15T12:00:00.345Z DEBUG codex_core::mcp::client: Connecting to MCP server: github",
        "2025-01-15T12:00:01.567Z INFO codex_core::mcp::client: MCP server 'github' connected successfully",
      ];

      const result = extractMCPInitialization(lines);

      expect(result.hasInfo).toBe(true);
      expect(result.markdown).toContain("**MCP Servers:**");
      expect(result.markdown).toContain("github");
      expect(result.markdown).toContain("✅");
      expect(result.markdown).toContain("connected");
      expect(result.servers).toHaveLength(1);
      expect(result.servers[0].name).toBe("github");
      expect(result.servers[0].status).toBe("connected");
    });

    it("should detect failed MCP server connections", () => {
      const lines = ["2025-01-15T12:00:00.345Z DEBUG codex_core::mcp::client: Connecting to MCP server: time", "2025-01-15T12:00:02.123Z ERROR codex_core::mcp::client: Failed to connect to MCP server 'time': Connection timeout"];

      const result = extractMCPInitialization(lines);

      expect(result.hasInfo).toBe(true);
      expect(result.markdown).toContain("❌");
      expect(result.markdown).toContain("time");
      expect(result.markdown).toContain("failed");
      expect(result.markdown).toContain("Connection timeout");
      expect(result.servers).toHaveLength(1);
      expect(result.servers[0].status).toBe("failed");
      expect(result.servers[0].error).toBe("Connection timeout");
    });

    it("should extract available MCP tools", () => {
      const lines = ["2025-01-15T12:00:02.678Z INFO codex_core: Available tools: github.list_issues, github.create_comment, safe_outputs.create_issue"];

      const result = extractMCPInitialization(lines);

      expect(result.hasInfo).toBe(true);
      expect(result.markdown).toContain("**Available MCP Tools:**");
      expect(result.markdown).toContain("3 tools");
      expect(result.markdown).toContain("github.list_issues");
    });

    it("should handle multiple MCP servers with mixed status", () => {
      const lines = [
        "2025-01-15T12:00:00.234Z DEBUG codex_core::mcp: Found 3 MCP servers in configuration",
        "2025-01-15T12:00:00.345Z DEBUG codex_core::mcp::client: Connecting to MCP server: github",
        "2025-01-15T12:00:01.567Z INFO codex_core::mcp::client: MCP server 'github' connected successfully",
        "2025-01-15T12:00:01.789Z DEBUG codex_core::mcp::client: Connecting to MCP server: time",
        "2025-01-15T12:00:02.123Z ERROR codex_core::mcp::client: Failed to connect to MCP server 'time': Connection timeout",
        "2025-01-15T12:00:02.345Z DEBUG codex_core::mcp::client: Connecting to MCP server: safe_outputs",
        "2025-01-15T12:00:02.456Z INFO codex_core::mcp::client: MCP server 'safe_outputs' connected successfully",
        "2025-01-15T12:00:02.567Z DEBUG codex_core::mcp: MCP initialization complete: 2/3 servers connected",
      ];

      const result = extractMCPInitialization(lines);

      expect(result.hasInfo).toBe(true);
      expect(result.markdown).toContain("Total: 3");
      expect(result.markdown).toContain("Connected: 2");
      expect(result.markdown).toContain("Failed: 1");
      expect(result.servers).toHaveLength(3);

      const github = result.servers.find(s => s.name === "github");
      const time = result.servers.find(s => s.name === "time");
      const safeOutputs = result.servers.find(s => s.name === "safe_outputs");

      expect(github.status).toBe("connected");
      expect(time.status).toBe("failed");
      expect(safeOutputs.status).toBe("connected");
    });

    it("should handle logs with no MCP information", () => {
      const lines = ["[2025-08-31T12:37:08] OpenAI Codex v0.27.0 (research preview)", "--------", "workdir: /home/runner/work/gh-aw/gh-aw"];

      const result = extractMCPInitialization(lines);

      expect(result.hasInfo).toBe(false);
      expect(result.markdown).toBe("");
      expect(result.servers).toHaveLength(0);
    });

    it("should handle initialization failed pattern", () => {
      const lines = ["2025-01-15T12:00:01.789Z DEBUG codex_core::mcp::client: Connecting to MCP server: custom", "2025-01-15T12:00:02.234Z WARN codex_core::mcp: MCP server 'custom' initialization failed, continuing without it"];

      const result = extractMCPInitialization(lines);

      expect(result.hasInfo).toBe(true);
      expect(result.markdown).toContain("custom");
      expect(result.markdown).toContain("failed");
      expect(result.servers[0].status).toBe("failed");
      expect(result.servers[0].error).toBe("Initialization failed");
    });

    it("should truncate tool list if too many tools", () => {
      const tools = Array.from({ length: 15 }, (_, i) => `tool${i}`).join(", ");
      const lines = [`2025-01-15T12:00:02.678Z INFO codex_core: Available tools: ${tools}`];

      const result = extractMCPInitialization(lines);

      expect(result.hasInfo).toBe(true);
      expect(result.markdown).toContain("15 tools");
      expect(result.markdown).toContain("...");
    });
  });

  describe("parseCodexLog with MCP initialization", () => {
    it("should include MCP initialization section when present", () => {
      const logContent = `2025-01-15T12:00:00.123Z DEBUG codex_core::mcp: Initializing MCP servers from config
2025-01-15T12:00:00.234Z DEBUG codex_core::mcp: Found 2 MCP servers in configuration
2025-01-15T12:00:00.345Z DEBUG codex_core::mcp::client: Connecting to MCP server: github
2025-01-15T12:00:01.567Z INFO codex_core::mcp::client: MCP server 'github' connected successfully
2025-01-15T12:00:01.789Z DEBUG codex_core::mcp::client: Connecting to MCP server: safe_outputs
2025-01-15T12:00:02.456Z INFO codex_core::mcp::client: MCP server 'safe_outputs' connected successfully
thinking
I will now use the GitHub API to list issues`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).toContain("## 🚀 Initialization");
      expect(result.markdown).toContain("**MCP Servers:**");
      expect(result.markdown).toContain("Total: 2");
      expect(result.markdown).toContain("Connected: 2");
      expect(result.markdown).toContain("✅");
      expect(result.markdown).toContain("github");
      expect(result.markdown).toContain("safe_outputs");
      expect(result.markdown).toContain("## 🤖 Reasoning");
    });

    it("should skip initialization section when no MCP info present", () => {
      const logContent = `[2025-08-31T12:37:08] OpenAI Codex v0.27.0
thinking
I will analyze the code`;

      const result = parseCodexLog(logContent);

      expect(result.markdown).not.toContain("## 🚀 Initialization");
      expect(result.markdown).toContain("## 🤖 Reasoning");
    });
  });
});
