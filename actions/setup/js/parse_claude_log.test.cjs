import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";

describe("parse_claude_log.cjs", () => {
  let mockCore, originalConsole, originalProcess;
  let main, parseClaudeLog;

  beforeEach(async () => {
    originalConsole = global.console;
    originalProcess = { ...process };
    global.console = { log: vi.fn(), error: vi.fn() };

    mockCore = {
      debug: vi.fn(),
      info: vi.fn(),
      notice: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      exportVariable: vi.fn(),
      setSecret: vi.fn(),
      getInput: vi.fn(),
      getBooleanInput: vi.fn(),
      getMultilineInput: vi.fn(),
      getState: vi.fn(),
      saveState: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      group: vi.fn(),
      addPath: vi.fn(),
      setCommandEcho: vi.fn(),
      isDebug: vi.fn().mockReturnValue(false),
      getIDToken: vi.fn(),
      toPlatformPath: vi.fn(),
      toPosixPath: vi.fn(),
      toWin32Path: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
    };

    global.core = mockCore;

    // Import the module to get the exported functions
    const module = await import("./parse_claude_log.cjs?" + Date.now());
    main = module.main;
    parseClaudeLog = module.parseClaudeLog;
  });

  afterEach(() => {
    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_MAX_TURNS;
    global.console = originalConsole;
    process.env = originalProcess.env;
    delete global.core;
  });

  const runScript = async logContent => {
    const tempFile = path.join(process.cwd(), `test_log_${Date.now()}.txt`);
    fs.writeFileSync(tempFile, logContent);
    process.env.GH_AW_AGENT_OUTPUT = tempFile;
    try {
      await main();
    } finally {
      if (fs.existsSync(tempFile)) {
        fs.unlinkSync(tempFile);
      }
    }
  };

  describe("parseClaudeLog function", () => {
    it("should parse old JSON array format", () => {
      const jsonArrayLog = JSON.stringify([
        { type: "system", subtype: "init", session_id: "test-123", tools: ["Bash", "Read"], model: "claude-sonnet-4-20250514" },
        {
          type: "assistant",
          message: {
            content: [
              { type: "text", text: "I'll help you with this task." },
              { type: "tool_use", id: "tool_123", name: "Bash", input: { command: "echo 'Hello World'" } },
            ],
          },
        },
        { type: "result", total_cost_usd: 0.0015, usage: { input_tokens: 150, output_tokens: 50 }, num_turns: 1 },
      ]);
      const result = parseClaudeLog(jsonArrayLog);

      expect(result.markdown).toContain("🚀 Initialization");
      expect(result.markdown).toContain("🤖 Commands and Tools");
      expect(result.markdown).toContain("test-123");
      expect(result.markdown).toContain("echo 'Hello World'");
      expect(result.markdown).toContain("Total Cost");
      expect(result.mcpFailures).toEqual([]);
    });

    it("should parse new mixed format with debug logs and JSON array", () => {
      const result = parseClaudeLog(
        '[DEBUG] Starting Claude Code CLI\n[ERROR] Some error occurred\nnpm warn exec The following package was not found\n[{"type":"system","subtype":"init","session_id":"29d324d8-1a92-43c6-8740-babc2875a1d6","tools":["Task","Bash","mcp__safe_outputs__missing-tool"],"model":"claude-sonnet-4-20250514"},{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_123","name":"mcp__safe_outputs__missing-tool","input":{"tool":"draw_pelican","reason":"Tool needed to draw pelican artwork"}}]}},{"type":"result","total_cost_usd":0.1789264,"usage":{"input_tokens":25,"output_tokens":832},"num_turns":10}]\n[DEBUG] Session completed'
      );

      expect(result.markdown).toContain("🚀 Initialization");
      expect(result.markdown).toContain("🤖 Commands and Tools");
      expect(result.markdown).toContain("29d324d8-1a92-43c6-8740-babc2875a1d6");
      expect(result.markdown).toContain("safe_outputs::missing-tool");
      expect(result.markdown).toContain("Total Cost");
      expect(result.mcpFailures).toEqual([]);
    });

    it("should parse mixed format with individual JSON lines", () => {
      const result = parseClaudeLog(
        '[DEBUG] Starting Claude Code CLI\n{"type":"system","subtype":"init","session_id":"test-456","tools":["Bash","Read"],"model":"claude-sonnet-4-20250514"}\n[DEBUG] Processing user prompt\n{"type":"assistant","message":{"content":[{"type":"text","text":"I\'ll help you."},{"type":"tool_use","id":"tool_123","name":"Bash","input":{"command":"ls -la"}}]}}\n{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_123","content":"file1.txt\\nfile2.txt"}]}}\n{"type":"result","total_cost_usd":0.002,"usage":{"input_tokens":100,"output_tokens":25},"num_turns":2}\n[DEBUG] Workflow completed'
      );

      expect(result.markdown).toContain("🚀 Initialization");
      expect(result.markdown).toContain("🤖 Commands and Tools");
      expect(result.markdown).toContain("test-456");
      expect(result.markdown).toContain("ls -la");
      expect(result.markdown).toContain("Total Cost");
      expect(result.mcpFailures).toEqual([]);
    });

    it("should handle MCP server failures", () => {
      const logWithFailures = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "test-789",
          tools: ["Bash"],
          mcp_servers: [
            { name: "github", status: "connected" },
            { name: "failed_server", status: "failed" },
          ],
          model: "claude-sonnet-4-20250514",
        },
      ]);
      const result = parseClaudeLog(logWithFailures);

      expect(result.markdown).toContain("🚀 Initialization");
      expect(result.markdown).toContain("failed_server (failed)");
      expect(result.mcpFailures).toEqual(["failed_server"]);
    });

    it("should display detailed error information for failed MCP servers", () => {
      const logWithDetailedErrors = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "test-detailed-errors",
          tools: ["Bash"],
          mcp_servers: [
            { name: "working_server", status: "connected" },
            {
              name: "failed_with_error",
              status: "failed",
              error: "Connection timeout after 30s",
              stderr: "Error: ECONNREFUSED connect ECONNREFUSED 127.0.0.1:3000\n    at TCPConnectWrap.afterConnect",
              exitCode: 1,
              command: "npx @github/github-mcp-server",
            },
          ],
          model: "claude-sonnet-4-20250514",
        },
      ]);
      const result = parseClaudeLog(logWithDetailedErrors);

      expect(result.markdown).toContain("🚀 Initialization");
      expect(result.markdown).toContain("failed_with_error (failed)");
      expect(result.markdown).toContain("**Error:** Connection timeout after 30s");
      expect(result.markdown).toContain("**Stderr:**");
      expect(result.markdown).toContain("**Exit Code:** 1");
      expect(result.markdown).toContain("**Command:** `npx @github/github-mcp-server`");
      expect(result.mcpFailures).toEqual(["failed_with_error"]);
    });

    it("should handle MCP server failures with message and reason fields", () => {
      const logWithMessageAndReason = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "test-message-reason",
          tools: ["Bash"],
          mcp_servers: [{ name: "failed_server", status: "failed", message: "Failed to initialize", reason: "Network error" }],
          model: "claude-sonnet-4-20250514",
        },
      ]);
      const result = parseClaudeLog(logWithMessageAndReason);

      expect(result.markdown).toContain("failed_server (failed)");
      expect(result.markdown).toContain("**Message:** Failed to initialize");
      expect(result.markdown).toContain("**Reason:** Network error");
    });

    it("should truncate long stderr output", () => {
      const longStderr = "x".repeat(600);
      const logWithLongStderr = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "test-long-stderr",
          tools: ["Bash"],
          mcp_servers: [{ name: "failed_server", status: "failed", stderr: longStderr }],
          model: "claude-sonnet-4-20250514",
        },
      ]);
      const result = parseClaudeLog(logWithLongStderr);

      expect(result.markdown).toContain("...");
      expect(result.markdown).not.toContain(longStderr);
    });

    it("should handle MCP server failures with partial error information", () => {
      const logWithPartialInfo = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "test-partial",
          tools: ["Bash"],
          mcp_servers: [
            { name: "partial_error_1", status: "failed", error: "Connection refused" },
            { name: "partial_error_2", status: "failed", stderr: "Something went wrong" },
            { name: "partial_error_3", status: "failed", exitCode: 127 },
          ],
          model: "claude-sonnet-4-20250514",
        },
      ]);
      const result = parseClaudeLog(logWithPartialInfo);

      expect(result.markdown).toContain("partial_error_1 (failed)");
      expect(result.markdown).toContain("**Error:** Connection refused");
      expect(result.markdown).toContain("partial_error_2 (failed)");
      expect(result.markdown).toContain("**Stderr:**");
      expect(result.markdown).toContain("partial_error_3 (failed)");
      expect(result.markdown).toContain("**Exit Code:** 127");
    });

    it("should handle exitCode zero for failed servers", () => {
      const logWithExitCodeZero = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "test-exit-zero",
          tools: ["Bash"],
          mcp_servers: [{ name: "failed_but_exit_zero", status: "failed", exitCode: 0, error: "Server exited unexpectedly" }],
          model: "claude-sonnet-4-20250514",
        },
      ]);
      const result = parseClaudeLog(logWithExitCodeZero);

      expect(result.markdown).toContain("failed_but_exit_zero (failed)");
      expect(result.markdown).toContain("**Error:** Server exited unexpectedly");
      expect(result.markdown).toContain("**Exit Code:** 0");
    });

    it("should handle unrecognized log format", () => {
      const result = parseClaudeLog("This is not JSON or valid format");
      expect(result.markdown).toContain("Log format not recognized");
    });

    it("should handle empty log content", () => {
      const result = parseClaudeLog("");
      expect(result.markdown).toContain("Log format not recognized");
    });

    it("should skip debug lines that look like arrays but aren't JSON", () => {
      const result = parseClaudeLog('[DEBUG] Starting\n[INFO] Processing\n[{"type":"system","subtype":"init","session_id":"test","tools":["Bash"],"model":"claude-sonnet-4-20250514"}]\n[DEBUG] Done');
      expect(result.markdown).toContain("🚀 Initialization");
    });

    it("should handle tool use with MCP tools", () => {
      const logWithMcpTools = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "mcp-test",
          tools: ["Bash", "mcp__github__create_issue"],
          model: "claude-sonnet-4-20250514",
        },
        { type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "mcp__github__create_issue", input: { title: "Test" } }] } },
        { type: "result", total_cost_usd: 0.01, usage: { input_tokens: 100, output_tokens: 50 }, num_turns: 1 },
      ]);
      const result = parseClaudeLog(logWithMcpTools);

      expect(result.markdown).toContain("github::create_issue");
      expect(result.mcpFailures).toEqual([]);
    });

    it("should detect when max-turns limit is hit", () => {
      process.env.GH_AW_MAX_TURNS = "5";
      const logWithMaxTurns = JSON.stringify([
        { type: "system", subtype: "init", session_id: "max-turns", tools: ["Bash"], model: "claude-sonnet-4-20250514" },
        { type: "result", total_cost_usd: 0.05, usage: { input_tokens: 500, output_tokens: 250 }, num_turns: 5 },
      ]);
      const result = parseClaudeLog(logWithMaxTurns);

      expect(result.markdown).toContain("**Turns:** 5");
      expect(result.maxTurnsHit).toBe(true);
    });

    it("should not flag max-turns when turns is less than limit", () => {
      process.env.GH_AW_MAX_TURNS = "5";
      const logBelowMaxTurns = JSON.stringify([
        { type: "system", subtype: "init", session_id: "below-max", tools: ["Bash"], model: "claude-sonnet-4-20250514" },
        { type: "result", total_cost_usd: 0.01, usage: { input_tokens: 100, output_tokens: 50 }, num_turns: 3 },
      ]);
      const result = parseClaudeLog(logBelowMaxTurns);

      expect(result.markdown).toContain("**Turns:** 3");
      expect(result.maxTurnsHit).toBe(false);
    });

    it("should not flag max-turns when environment variable is not set", () => {
      delete process.env.GH_AW_MAX_TURNS;
      const logWithoutMaxTurnsEnv = JSON.stringify([
        { type: "system", subtype: "init", session_id: "no-env", tools: ["Bash"], model: "claude-sonnet-4-20250514" },
        { type: "result", total_cost_usd: 0.01, usage: { input_tokens: 100, output_tokens: 50 }, num_turns: 10 },
      ]);
      const result = parseClaudeLog(logWithoutMaxTurnsEnv);

      expect(result.markdown).toContain("**Turns:** 10");
      expect(result.maxTurnsHit).toBe(false);
    });

    it("should render error messages from errors array", () => {
      const logWithErrors = JSON.stringify([
        { type: "system", subtype: "init", session_id: "test-errors", tools: ["Bash"], model: "claude-sonnet-4-20250514" },
        {
          type: "result",
          subtype: "error_during_execution",
          duration_ms: 0,
          duration_api_ms: 0,
          is_error: true,
          num_turns: 0,
          session_id: "9536d10a-effb-4160-8584-ff2407ffdaa9",
          total_cost_usd: 0,
          usage: { input_tokens: 0, cache_creation_input_tokens: 0, cache_read_input_tokens: 0, output_tokens: 0 },
          errors: ["only prompt commands are supported in streaming mode"],
        },
      ]);
      const result = parseClaudeLog(logWithErrors);

      expect(result.markdown).toContain("**Errors:**");
      expect(result.markdown).toContain("- only prompt commands are supported in streaming mode");
    });

    it("should render multiple error messages", () => {
      const logWithMultipleErrors = JSON.stringify([
        { type: "system", subtype: "init", session_id: "test-multi-errors", tools: ["Bash"], model: "claude-sonnet-4-20250514" },
        {
          type: "result",
          subtype: "error_during_execution",
          is_error: true,
          num_turns: 0,
          total_cost_usd: 0,
          usage: { input_tokens: 0, output_tokens: 0 },
          errors: ["Error 1: Connection failed", "Error 2: Timeout occurred", "Error 3: Invalid response"],
        },
      ]);
      const result = parseClaudeLog(logWithMultipleErrors);

      expect(result.markdown).toContain("**Errors:**");
      expect(result.markdown).toContain("- Error 1: Connection failed");
      expect(result.markdown).toContain("- Error 2: Timeout occurred");
      expect(result.markdown).toContain("- Error 3: Invalid response");
    });
  });

  describe("main function integration", () => {
    it("should handle valid log file", async () => {
      const validLog = JSON.stringify([
        { type: "system", subtype: "init", session_id: "integration-test", tools: ["Bash"], model: "claude-sonnet-4-20250514" },
        { type: "result", total_cost_usd: 0.001, usage: { input_tokens: 50, output_tokens: 25 }, num_turns: 1 },
      ]);

      await runScript(validLog);

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();

      const markdownCall = mockCore.summary.addRaw.mock.calls[0];
      expect(markdownCall[0]).toContain("```");
      expect(markdownCall[0]).toContain("Conversation:");
      expect(markdownCall[0]).toContain("Statistics:");
      expect(mockCore.info).toHaveBeenCalled();

      const infoCall = mockCore.info.mock.calls.find(call => call[0].includes("=== Claude Execution Summary ==="));
      expect(infoCall).toBeDefined();
      expect(infoCall[0]).toContain("Model: claude-sonnet-4-20250514");
    });

    it("should handle log with MCP failures", async () => {
      const logWithFailures = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "failure-test",
          mcp_servers: [
            { name: "working_server", status: "connected" },
            { name: "broken_server", status: "failed" },
          ],
          tools: ["Bash"],
          model: "claude-sonnet-4-20250514",
        },
      ]);

      await runScript(logWithFailures);

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
      expect(mockCore.setFailed).toHaveBeenCalledWith("MCP server(s) failed to launch: broken_server");
    });

    it("should call setFailed when max-turns limit is hit", async () => {
      process.env.GH_AW_MAX_TURNS = "3";
      const logHittingMaxTurns = JSON.stringify([
        { type: "system", subtype: "init", session_id: "max-turns-test", tools: ["Bash"], model: "claude-sonnet-4-20250514" },
        { type: "result", total_cost_usd: 0.02, usage: { input_tokens: 200, output_tokens: 100 }, num_turns: 3 },
      ]);

      await runScript(logHittingMaxTurns);

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
      expect(mockCore.setFailed).toHaveBeenCalledWith("Agent execution stopped: max-turns limit reached. The agent did not complete its task successfully.");
    });

    it("should handle missing log file", async () => {
      process.env.GH_AW_AGENT_OUTPUT = "/nonexistent/file.log";
      await main();
      expect(mockCore.info).toHaveBeenCalledWith("Log path not found: /nonexistent/file.log");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should handle missing environment variable", async () => {
      delete process.env.GH_AW_AGENT_OUTPUT;
      await main();
      expect(mockCore.info).toHaveBeenCalledWith("No agent log file specified");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("helper function tests", () => {
    it("should format bash commands correctly", () => {
      const result = parseClaudeLog(JSON.stringify([{ type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: "echo 'hello world'\n  && ls -la\n  && pwd" } }] } }]));
      expect(result.markdown).toContain("echo 'hello world' && ls -la && pwd");
    });

    it("should truncate long strings appropriately", () => {
      const longCommand = "a".repeat(400);
      const result = parseClaudeLog(JSON.stringify([{ type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: longCommand } }] } }]));
      expect(result.markdown).toContain("...");
    });

    it("should format MCP tool names correctly", () => {
      const result = parseClaudeLog(JSON.stringify([{ type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "mcp__github__create_pull_request", input: { title: "Test PR" } }] } }]));
      expect(result.markdown).toContain("github::create_pull_request");
    });

    it("should render tool outputs in collapsible HTML details elements", () => {
      const result = parseClaudeLog(
        JSON.stringify([
          { type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: "ls -la", description: "List files" } }] } },
          {
            type: "user",
            message: {
              content: [
                {
                  type: "tool_result",
                  tool_use_id: "tool_1",
                  content: "total 24\ndrwxr-xr-x  6 user staff  192 Jan  1 12:00 .\ndrwxr-xr-x 10 user staff  320 Jan  1 12:00 ..",
                },
              ],
            },
          },
        ])
      );

      expect(result.markdown).toContain("<details>");
      expect(result.markdown).toContain("</details>");
      expect(result.markdown).toContain("total 24");
    });

    it("should include token estimates in tool call rendering", () => {
      const result = parseClaudeLog(JSON.stringify([{ type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: "echo 'test'" } }] } }]));
      expect(result.markdown).toContain("~");
    });

    it("should include duration when available in tool_result", () => {
      const result = parseClaudeLog(
        JSON.stringify([
          { type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: "sleep 1" } }] } },
          { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "tool_1", content: "done", duration_ms: 1234 }] } },
        ])
      );
      expect(result.markdown).toContain("1s"); // Duration is shown in summary
    });

    it("should truncate long tool outputs", () => {
      const longOutput = "x".repeat(2000);
      const result = parseClaudeLog(
        JSON.stringify([
          { type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: "cat bigfile" } }] } },
          { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "tool_1", content: longOutput }] } },
        ])
      );
      expect(result.markdown).toContain("...");
      expect(result.markdown).not.toContain("x".repeat(2000));
    });

    it("should show summary only when no tool output", () => {
      const result = parseClaudeLog(
        JSON.stringify([
          { type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: "echo test" } }] } },
          { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "tool_1", content: "" }] } },
        ])
      );
      expect(result.markdown).toContain("echo test"); // Tool is shown even with empty output
    });

    it("should display all tools even when there are many (more than 5)", () => {
      const result = parseClaudeLog(
        JSON.stringify([
          {
            type: "system",
            subtype: "init",
            session_id: "many-tools",
            tools: [
              "Bash",
              "Read",
              "Write",
              "Edit",
              "LS",
              "Grep",
              "mcp__github__list_issues",
              "mcp__github__get_issue",
              "mcp__github__create_pull_request",
              "mcp__github__list_pull_requests",
              "mcp__github__get_pull_request",
              "mcp__github__create_discussion",
              "mcp__github__list_discussions",
              "mcp__safe_outputs__create_issue",
              "mcp__safe_outputs__add-comment",
            ],
            model: "claude-sonnet-4-20250514",
          },
        ])
      );

      expect(result.markdown).toContain("github::list_issues");
      expect(result.markdown).toContain("github::get_issue");
      expect(result.markdown).toContain("github::create_pull_request");
      expect(result.markdown).toContain("github::list_pull_requests");
      expect(result.markdown).toContain("github::get_pull_request");
      expect(result.markdown).toContain("github::create_discussion");
      expect(result.markdown).toContain("github::list_discussions");
      expect(result.markdown).toContain("safe_outputs::create_issue");
      expect(result.markdown).toContain("safe_outputs::add-comment");
      expect(result.markdown).toContain("Read");
      expect(result.markdown).toContain("Write");
      expect(result.markdown).toContain("Edit");
      expect(result.markdown).toContain("LS");
      expect(result.markdown).toContain("Grep");
      expect(result.markdown).toContain("Bash");

      const toolsSection = result.markdown.split("## 🤖 Reasoning")[0];
      expect(toolsSection).not.toMatch(/and \d+ more/);
    });

    it("should handle ToolsSearch with array of objects (issue fix)", () => {
      const logWithToolsSearch = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "test-tools-search",
          tools: ["Bash", "mcp__serena__ToolsSearch"],
          model: "claude-sonnet-4-20250514",
        },
        {
          type: "assistant",
          message: {
            content: [
              {
                type: "tool_use",
                id: "tool_1",
                name: "mcp__serena__ToolsSearch",
                input: {
                  query: "search term",
                  tools: [
                    { name: "tool1", type: "function" },
                    { name: "tool2", type: "class" },
                    { name: "tool3", type: "function" },
                    { name: "tool4", type: "function" },
                    { name: "tool5", type: "class" },
                  ],
                },
              },
            ],
          },
        },
        { type: "result", total_cost_usd: 0.01, usage: { input_tokens: 100, output_tokens: 50 }, num_turns: 1 },
      ]);
      const result = parseClaudeLog(logWithToolsSearch);

      // Should NOT contain [object Object] - the bug we're fixing
      expect(result.markdown).not.toContain("[object Object]");

      // Should contain properly formatted JSON array representation
      expect(result.markdown).toContain("serena::ToolsSearch");
      expect(result.markdown).toMatch(/tools:.*\[.*"name":"tool1"/);

      // Should properly render the tool in initialization section
      expect(result.markdown).toContain("serena::ToolsSearch");
    });
  });
});
