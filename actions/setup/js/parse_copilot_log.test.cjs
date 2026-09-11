import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";

describe("parse_copilot_log.cjs", () => {
  let mockCore, originalConsole, originalProcess;
  let main, parseCopilotLog;

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
    const module = await import("./parse_copilot_log.cjs?" + Date.now());
    main = module.main;
    parseCopilotLog = module.parseCopilotLog;
  });

  afterEach(() => {
    delete process.env.GH_AW_AGENT_OUTPUT;
    global.console = originalConsole;
    process.env = originalProcess.env;
    delete global.core;
  });

  describe("parseCopilotLog function", () => {
    const getSessionResultData = entries => entries.find(e => e.type === "session.result")?.data;

    it("should parse JSON array format", () => {
      const jsonArrayLog = JSON.stringify([
        { type: "system", subtype: "init", session_id: "copilot-test-123", tools: ["Bash", "Read", "mcp__github__create_issue"], model: "gpt-5" },
        {
          type: "assistant",
          message: {
            content: [
              { type: "text", text: "I'll help you with this task." },
              { type: "tool_use", id: "tool_123", name: "Bash", input: { command: "echo 'Hello World'", description: "Print greeting" } },
            ],
          },
        },
        { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "tool_123", content: "Hello World\n" }] } },
        { type: "result", total_cost_usd: 0.0015, usage: { input_tokens: 150, output_tokens: 50 }, num_turns: 1 },
      ]);
      const result = parseCopilotLog(jsonArrayLog);

      expect(result.markdown).toContain("<summary>Initialization</summary>");
      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      expect(result.markdown).toContain("copilot-test-123");
      expect(result.markdown).toContain("echo 'Hello World'");
      expect(result.markdown).toContain("Total Cost");
      expect(result.markdown).toContain("<details>");
      expect(result.markdown).toContain("<summary>");
    });

    it("should parse mixed format with debug logs and JSON array", () => {
      const result = parseCopilotLog(
        '[DEBUG] Starting Copilot CLI\n[ERROR] Some error occurred\n[{"type":"system","subtype":"init","session_id":"copilot-456","tools":["Bash","mcp__safe_outputs__missing-tool"],"model":"gpt-5"},{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_123","name":"mcp__safe_outputs__missing-tool","input":{"tool":"draw_pelican","reason":"Tool needed to draw pelican artwork"}}]}},{"type":"result","total_cost_usd":0.1789264,"usage":{"input_tokens":25,"output_tokens":832},"num_turns":10}]\n[DEBUG] Session completed'
      );

      expect(result.markdown).toContain("<summary>Initialization</summary>");
      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      expect(result.markdown).toContain("copilot-456");
      expect(result.markdown).toContain("safe_outputs::missing-tool");
      expect(result.markdown).toContain("Total Cost");
    });

    it("should parse mixed format with individual JSON lines (JSONL)", () => {
      const result = parseCopilotLog(
        '[DEBUG] Starting Copilot CLI\n{"type":"system","subtype":"init","session_id":"copilot-789","tools":["Bash","Read"],"model":"gpt-5"}\n[DEBUG] Processing user prompt\n{"type":"assistant","message":{"content":[{"type":"text","text":"I\'ll help you."},{"type":"tool_use","id":"tool_123","name":"Bash","input":{"command":"ls -la"}}]}}\n{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_123","content":"file1.txt\\nfile2.txt"}]}}\n{"type":"result","total_cost_usd":0.002,"usage":{"input_tokens":100,"output_tokens":25},"num_turns":2}\n[DEBUG] Workflow completed'
      );

      expect(result.markdown).toContain("<summary>Initialization</summary>");
      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      expect(result.markdown).toContain("copilot-789");
      expect(result.markdown).toContain("ls -la");
      expect(result.markdown).toContain("Total Cost");
    });

    it("normalizes Copilot SDK events.jsonl entries into trace entries for rendering", () => {
      const sdkEventsLog = [
        '{"type":"user.message","timestamp":"2026-06-05T00:44:01.367Z","data":{}}',
        '{"type":"tool.execution_start","timestamp":"2026-06-05T00:44:04.520Z","data":{"toolName":"report_intent","mcpServerName":""}}',
        '{"type":"tool.execution_complete","timestamp":"2026-06-05T00:44:04.700Z","data":{"toolName":"report_intent","mcpServerName":"","success":true}}',
        '{"type":"assistant.message","timestamp":"2026-06-05T00:44:59.769Z","data":{"content":"Rendered summary content"}}',
      ].join("\n");

      const result = parseCopilotLog(sdkEventsLog);

      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      expect(result.markdown).toContain("report_intent");
      expect(result.markdown).toContain("Rendered summary content");
      const resultData = getSessionResultData(result.logEntries);
      expect(resultData?.numTurns).toBe(1);
    });

    it("renders tool output preview from result.content in Copilot CLI events.jsonl", () => {
      const eventsLog = [
        '{"type":"user.message","timestamp":"2026-06-05T00:44:01.367Z","data":{}}',
        '{"type":"tool.execution_start","timestamp":"2026-06-05T00:44:04.520Z","data":{"toolName":"bash","mcpServerName":""}}',
        '{"type":"tool.execution_complete","timestamp":"2026-06-05T00:44:04.700Z","data":{"toolName":"bash","mcpServerName":"","success":true,"result":{"content":"file1.txt\\nfile2.txt\\nfile3.txt"}}}',
        '{"type":"assistant.message","timestamp":"2026-06-05T00:44:59.769Z","data":{"content":"Done"}}',
      ].join("\n");

      const result = parseCopilotLog(eventsLog);

      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      expect(result.markdown).toContain("file1.txt");
    });

    it("renders the bash command from data.command in Copilot CLI events.jsonl", () => {
      // Real Copilot SDK events carry the executed command as a top-level data.command
      // field on tool.execution_start, not nested inside data.input/data.parameters.
      const eventsLog = [
        '{"type":"user.message","timestamp":"2026-06-05T00:44:01.367Z","data":{}}',
        '{"type":"tool.execution_start","timestamp":"2026-06-05T00:44:04.520Z","data":{"toolName":"bash","mcpServerName":"","command":"cat /tmp/gh-aw/agent/candidates.txt"}}',
        '{"type":"tool.execution_complete","timestamp":"2026-06-05T00:44:04.700Z","data":{"toolName":"bash","mcpServerName":"","success":true,"result":{"content":"candidate-list-output"}}}',
        '{"type":"assistant.message","timestamp":"2026-06-05T00:44:59.769Z","data":{"content":"Done"}}',
      ].join("\n");

      const result = parseCopilotLog(eventsLog);

      expect(result.markdown).toContain("cat /tmp/gh-aw/agent/candidates.txt");
      expect(result.markdown).toContain("candidate-list-output");
    });

    it("merges data.command into existing data.input when command is absent there", () => {
      const eventsLog = [
        '{"type":"user.message","timestamp":"2026-06-05T00:44:01.367Z","data":{}}',
        '{"type":"tool.execution_start","timestamp":"2026-06-05T00:44:04.520Z","data":{"toolName":"bash","mcpServerName":"","input":{"cwd":"/tmp"},"command":"ls"}}',
        '{"type":"tool.execution_complete","timestamp":"2026-06-05T00:44:04.700Z","data":{"toolName":"bash","mcpServerName":"","success":true,"result":{"content":"file1.txt\\nfile2.txt"}}}',
        '{"type":"assistant.message","timestamp":"2026-06-05T00:44:59.769Z","data":{"content":"Done"}}',
      ].join("\n");

      const result = parseCopilotLog(eventsLog);

      expect(result.markdown).toContain("ls");
      expect(result.markdown).toContain("file1.txt");
      // cwd is part of input parameters which are not rendered to avoid secret leakage
      expect(result.markdown).not.toContain('"cwd"');
    });

    it("preserves structured input for orphaned completion events without inventing a command", () => {
      const eventsLog = [
        '{"type":"user.message","timestamp":"2026-06-05T00:44:01.367Z","data":{}}',
        '{"type":"tool.execution_complete","timestamp":"2026-06-05T00:44:04.700Z","data":{"toolName":"bash","mcpServerName":"","input":{"cwd":"/tmp"},"success":true,"result":{"content":"file1.txt\\nfile2.txt"}}}',
        '{"type":"assistant.message","timestamp":"2026-06-05T00:44:59.769Z","data":{"content":"Done"}}',
      ].join("\n");

      const result = parseCopilotLog(eventsLog);

      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      expect(result.markdown).toContain("file1.txt");
      // input parameters are not rendered to avoid secret leakage
      expect(result.markdown).not.toContain('"cwd"');
    });

    it("renders tool output preview from array-based result.content in Copilot CLI events.jsonl", () => {
      const eventsLog = [
        '{"type":"user.message","timestamp":"2026-06-05T00:44:01.367Z","data":{}}',
        '{"type":"tool.execution_start","timestamp":"2026-06-05T00:44:04.520Z","data":{"toolName":"bash","mcpServerName":""}}',
        '{"type":"tool.execution_complete","timestamp":"2026-06-05T00:44:04.700Z","data":{"toolName":"bash","mcpServerName":"","success":true,"result":{"content":[{"type":"text","text":"fileA.txt\\\\nfileB.txt"}]}}}',
        '{"type":"assistant.message","timestamp":"2026-06-05T00:44:59.769Z","data":{"content":"Done"}}',
      ].join("\n");

      const result = parseCopilotLog(eventsLog);

      expect(result.markdown).toContain("fileA.txt");
      expect(result.markdown).toContain("fileB.txt");
    });

    it("should handle tool calls with details in HTML format", () => {
      const logWithHtmlDetails = JSON.stringify([
        { type: "system", subtype: "init", session_id: "html-test", tools: ["Bash"], model: "gpt-5" },
        {
          type: "assistant",
          message: {
            content: [
              {
                type: "tool_use",
                id: "tool_1",
                name: "Bash",
                input: { command: "cat file.txt", description: "Read file contents" },
              },
            ],
          },
        },
        {
          type: "user",
          message: {
            content: [
              {
                type: "tool_result",
                tool_use_id: "tool_1",
                content: "File contents here",
              },
            ],
          },
        },
      ]);
      const result = parseCopilotLog(logWithHtmlDetails);

      expect(result.markdown).toContain("<details>");
      expect(result.markdown).toContain("</details>");
      expect(result.markdown).toContain("File contents here");
    });

    it("should handle MCP tools", () => {
      const logWithMcpTools = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          session_id: "mcp-test",
          tools: ["Bash", "mcp__github__create_issue", "mcp__github__list_pull_requests"],
          model: "gpt-5",
        },
        { type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "mcp__github__create_issue", input: { title: "Test" } }] } },
        { type: "result", total_cost_usd: 0.01, usage: { input_tokens: 100, output_tokens: 50 }, num_turns: 1 },
      ]);
      const result = parseCopilotLog(logWithMcpTools);

      expect(result.markdown).toContain("github::create_issue");
      expect(result.markdown).toContain("github::list_pull_requests");
    });

    it("should handle unrecognized log format", () => {
      const result = parseCopilotLog("This is not JSON or valid format");
      expect(result.markdown).toContain("Log format not recognized");
    });

    it("should handle empty log content", () => {
      const result = parseCopilotLog("");
      expect(result.markdown).toContain("Log format not recognized");
    });

    it("should parse pretty-print format with success (●) and failure (✗) markers", () => {
      const prettyLog = ["● Bash", "    └ echo hello", "✗ mcp__github__create_issue", "    └ Error: permission denied", "Task completed."].join("\n");

      const result = parseCopilotLog(prettyLog);

      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      // MCP tool name is formatted as server::name
      expect(result.markdown).toContain("github::create_issue");
      // Continuation output appears in the details
      expect(result.markdown).toContain("echo hello");
      expect(result.markdown).toContain("Error: permission denied");
    });

    it("should parse pretty-print format with success (✓) marker", () => {
      const prettyLog = ["✓ Read", "    └ file contents here", "✓ Bash", "    └ command output"].join("\n");

      const result = parseCopilotLog(prettyLog);

      expect(result.markdown).toContain("<summary>Commands and Tools</summary>");
      // Read tool name appears in the Reasoning section summary
      expect(result.markdown).toContain("Read");
      // Continuation output appears in the details
      expect(result.markdown).toContain("file contents here");
      expect(result.markdown).toContain("command output");
    });

    it("should capture continuation/indented output for pretty-print tool calls", () => {
      const prettyLog = ["● Bash", "    └ line1 of output", "    └ line2 of output", "✗ Write", "    └ error details here"].join("\n");

      const result = parseCopilotLog(prettyLog);

      expect(result.markdown).toContain("line1 of output");
      expect(result.markdown).toContain("error details here");
    });

    it("should extract token counts from pretty-print format breakdown section", () => {
      const prettyLog = ["● Bash", "    └ ok", "", "Breakdown by AI model:", "  gpt-4o  10k in, 2k out, 1k cached", "", "Total usage est: 12k tokens"].join("\n");

      const result = parseCopilotLog(prettyLog);

      // Token counts should be reflected in the Information section
      expect(result.markdown).toContain("10,000");
      expect(result.markdown).toContain("2,000");
    });

    it("should use Turns: count from pretty-print format when available", () => {
      const prettyLog = ["● Bash", "    └ ok", "● Bash", "    └ ok2", "", "Turns: 5", "Total usage est: 100 tokens"].join("\n");

      const result = parseCopilotLog(prettyLog);

      // num_turns should be 5 (from Turns: line), not 2 (from toolEntries.length)
      expect(result.logEntries).toBeDefined();
      const resultData = getSessionResultData(result.logEntries);
      expect(resultData).toBeDefined();
      expect(resultData?.numTurns).toBe(5);
    });

    it("strips harness driver lines from rendered pretty-print output", () => {
      const prettyLog = [
        "[copilot-harness] 2026-05-16T08:21:00.991Z starting: command=/usr/local/bin/copilot",
        "[copilot-harness] 2026-05-16T08:21:01.135Z attempt 1: spawning copilot",
        "● Bash",
        "    └ ok",
        "Some final agent thought.",
        "[copilot-harness] 2026-05-16T08:21:33.527Z attempt 1: process exit event exitCode=0",
        "[copilot-harness] 2026-05-16T08:21:33.532Z done: exitCode=0 totalDuration=32s",
      ].join("\n");

      const result = parseCopilotLog(prettyLog);

      expect(result.markdown).not.toContain("[copilot-harness]");
      expect(result.markdown).not.toContain("attempt 1: spawning");
      expect(result.markdown).toContain("Some final agent thought.");
    });

    it("suppresses the new Copilot CLI footer stats (Changes/Duration/Tokens) from agent text", () => {
      const prettyLog = ["● Bash", "    └ ok", "The work is done.", "", "Changes   +0 -0", "Duration  31s", "Tokens    ↑ 290.1k • ↓ 1.4k • 247.4k (cached)"].join("\n");

      const result = parseCopilotLog(prettyLog);

      expect(result.markdown).toContain("The work is done.");
      expect(result.markdown).not.toMatch(/^Changes\s+\+0 -0$/m);
      expect(result.markdown).not.toMatch(/^Duration\s+31s$/m);
      expect(result.markdown).not.toMatch(/^Tokens\s+↑/m);
    });

    it("extracts token counts from the new Copilot CLI footer (Tokens ↑X • ↓Y • Z (cached))", () => {
      const prettyLog = ["● Bash", "    └ ok", "The work is done.", "", "Changes   +0 -0", "Duration  11s", "Tokens    ↑ 163.9k • ↓ 567 • 149.2k (cached)"].join("\n");

      const result = parseCopilotLog(prettyLog);
      const resultEntry = getSessionResultData(result.logEntries);

      expect(resultEntry).toBeDefined();
      expect(resultEntry.usage).toEqual(
        expect.objectContaining({
          input_tokens: 163900,
          output_tokens: 567,
          cache_read_input_tokens: 149200,
        })
      );
      // Information section should render the parsed tokens
      expect(result.markdown).toContain("Token Usage");
      expect(result.markdown).toContain("163,900");
      expect(result.markdown).toContain("567");
      expect(result.markdown).toContain("149,200");
    });

    it("extracts token counts when cached is shown inline after the up-arrow (Tokens ↑X (Y cached) • ↓Z)", () => {
      // Format emitted by Copilot CLI 1.0.55: the cached count appears inline in
      // parentheses after the up-arrow rather than trailing the line.
      const prettyLog = ["● Bash", "    └ ok", "The work is done.", "", "Changes    +0 -0", "Duration   3m 13s", "Tokens     ↑ 422.2k (375.0k cached) • ↓ 2.4k"].join("\n");

      const result = parseCopilotLog(prettyLog);
      const resultEntry = getSessionResultData(result.logEntries);

      expect(resultEntry).toBeDefined();
      expect(resultEntry.usage).toEqual(
        expect.objectContaining({
          input_tokens: 422200,
          output_tokens: 2400,
          cache_read_input_tokens: 375000,
        })
      );
      expect(result.markdown).toContain("Token Usage");
      expect(result.markdown).toContain("422,200");
      expect(result.markdown).toContain("2,400");
      expect(result.markdown).toContain("375,000");
    });

    it("extracts million-scale tokens from the current Copilot CLI footer", () => {
      const prettyLog = ["● Bash", "    └ ok", "The work is done.", "", "Tokens     ↑ 17.7m (17.5m cached, 216.2k written) • ↓ 46.5k (20.0k reasoning)"].join("\n");

      const result = parseCopilotLog(prettyLog);
      const resultEntry = getSessionResultData(result.logEntries);

      expect(resultEntry.usage).toEqual(
        expect.objectContaining({
          input_tokens: 17700000,
          output_tokens: 46500,
          cache_read_input_tokens: 17500000,
          cache_creation_input_tokens: 216200,
        })
      );
    });

    it("strips the columnar 'Resume' footer hint from rendered pretty-print output", () => {
      // Copilot CLI footer includes a "Resume   copilot --resume=<id>" line aligned in the
      // same column block as Changes/Duration/Tokens. It is CLI chrome, not agent reasoning,
      // and must not leak into the rendered reasoning/agent-text section.
      const prettyLog = ["● Bash", "    └ ok", "The work is done.", "", "Changes    +0 -0", "Duration   1m 0s", "Tokens     ↑ 195.4k (166.2k cached) • ↓ 2.9k", "Resume     copilot --resume=d21d3356-9296-4d1b-a392-49e5069e4e3f"].join("\n");

      const result = parseCopilotLog(prettyLog);

      expect(result.markdown).toContain("The work is done.");
      expect(result.markdown).not.toContain("--resume=");
      expect(result.markdown).not.toMatch(/^Resume\s+copilot/m);
    });

    it("handles the new footer without a cached segment", () => {
      const prettyLog = ["● Bash", "    └ ok", "", "Tokens    ↑ 1.2k • ↓ 50"].join("\n");

      const result = parseCopilotLog(prettyLog);
      const resultEntry = getSessionResultData(result.logEntries);

      expect(resultEntry.usage.input_tokens).toBe(1200);
      expect(resultEntry.usage.output_tokens).toBe(50);
      expect(resultEntry.usage.cache_read_input_tokens).toBeUndefined();
    });

    it("should parse debug log format with reasoning_text", () => {
      const debugLog = [
        "2026-02-21T00:06:13.708Z [INFO] Starting Copilot CLI: 0.0.412",
        "2026-02-21T00:06:23.701Z [DEBUG] data:",
        "2026-02-21T00:06:23.702Z [DEBUG] {",
        '  "model": "claude-sonnet-4.6",',
        '  "usage": { "prompt_tokens": 100, "completion_tokens": 50 },',
        '  "choices": [',
        "    {",
        '      "message": {',
        '        "reasoning_text": "Let me think about this task carefully.",',
        '        "content": null,',
        '        "tool_calls": [',
        "          {",
        '            "id": "tool_1",',
        '            "type": "function",',
        '            "function": { "name": "bash", "arguments": "{\\"command\\": \\"echo hello\\"}" }',
        "          }",
        "        ]",
        "      }",
        "    }",
        "  ]",
        "}",
        "2026-02-21T00:06:24.000Z [INFO] Done",
      ].join("\n");

      const result = parseCopilotLog(debugLog);

      expect(result.markdown).toContain("claude-sonnet-4.6");
      expect(result.markdown).toContain("Let me think about this task carefully.");
      expect(result.markdown).toContain("echo hello");
    });

    it("should render reasoning_text with open circle icon and italic styling", () => {
      const debugLog = [
        "2026-02-21T00:06:13.708Z [INFO] Starting Copilot CLI: 0.0.412",
        "2026-02-21T00:06:23.701Z [DEBUG] data:",
        "2026-02-21T00:06:23.702Z [DEBUG] {",
        '  "model": "gpt-5",',
        '  "usage": { "prompt_tokens": 100, "completion_tokens": 50 },',
        '  "choices": [',
        "    {",
        '      "message": {',
        '        "reasoning_text": "I need to think carefully about the approach.",',
        '        "content": "Here is my answer.",',
        '        "tool_calls": null',
        "      }",
        "    }",
        "  ]",
        "}",
        "2026-02-21T00:06:24.000Z [INFO] Done",
      ].join("\n");

      const result = parseCopilotLog(debugLog);

      // Reasoning should appear in the reasoning section
      expect(result.markdown).toContain("I need to think carefully about the approach.");
      // Regular content should appear without open circle
      expect(result.markdown).toContain("Here is my answer.");
    });

    it("should handle model info with cost multiplier", () => {
      const structuredLog = JSON.stringify([
        { type: "system", subtype: "init", session_id: "cost-test", tools: ["Bash"], model: "gpt-4", model_info: { is_premium: true, cost_multiplier: 3 } },
        { type: "result", num_turns: 2, usage: { input_tokens: 500, output_tokens: 200 } },
      ]);
      const result = parseCopilotLog(structuredLog);

      expect(result.markdown).toContain("gpt-4");
    });

    it("renders AWF token steering warnings from structured log entries", () => {
      const structuredLog = JSON.stringify([
        { type: "system", subtype: "init", session_id: "steering-test", tools: ["Bash"], model: "gpt-5" },
        {
          type: "system",
          subtype: "event",
          message: {
            content: [
              {
                type: "text",
                text: "[AWF TOKEN WARNING] You have used 90% of your effective token budget. Complete your current task and prepare final output.",
              },
            ],
          },
        },
        { type: "result", num_turns: 1, usage: { input_tokens: 120, output_tokens: 40 } },
      ]);

      const result = parseCopilotLog(structuredLog);

      expect(result.markdown).toContain("Firewall Steering");
      expect(result.markdown).toContain("[AWF TOKEN WARNING] You have used 90% of your effective token budget.");
    });
  });

  describe("main function integration", () => {
    it("should handle valid log file", async () => {
      const validLog = JSON.stringify([
        { type: "system", subtype: "init", session_id: "integration-test", tools: ["Bash"], model: "gpt-5" },
        { type: "result", total_cost_usd: 0.001, usage: { input_tokens: 50, output_tokens: 25 }, num_turns: 1 },
      ]);

      const tempFile = path.join(process.cwd(), `test_log_${Date.now()}.txt`);
      fs.writeFileSync(tempFile, validLog);
      process.env.GH_AW_AGENT_OUTPUT = tempFile;

      try {
        await main();

        expect(mockCore.summary.addRaw).toHaveBeenCalled();
        expect(mockCore.summary.write).toHaveBeenCalled();
      } finally {
        if (fs.existsSync(tempFile)) {
          fs.unlinkSync(tempFile);
        }
      }
    });

    it("should handle missing log file", async () => {
      process.env.GH_AW_AGENT_OUTPUT = "/nonexistent/file.log";
      await main();
      expect(mockCore.info).toHaveBeenCalledWith("Log path not found: /nonexistent/file.log");
    });

    it("should handle missing environment variable", async () => {
      delete process.env.GH_AW_AGENT_OUTPUT;
      await main();
      expect(mockCore.info).toHaveBeenCalledWith("No agent log file specified");
    });
  });

  describe("helper function tests", () => {
    it("should format bash commands correctly", () => {
      const result = parseCopilotLog(JSON.stringify([{ type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: "echo 'hello world'\n  && ls -la\n  && pwd" } }] } }]));
      expect(result.markdown).toContain("echo 'hello world' && ls -la && pwd");
    });

    it("should truncate long strings appropriately", () => {
      const longCommand = "a".repeat(400);
      const result = parseCopilotLog(JSON.stringify([{ type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "Bash", input: { command: longCommand } }] } }]));
      expect(result.markdown).toContain("...");
    });

    it("should format MCP tool names correctly", () => {
      const result = parseCopilotLog(JSON.stringify([{ type: "assistant", message: { content: [{ type: "tool_use", id: "tool_1", name: "mcp__github__create_pull_request", input: { title: "Test PR" } }] } }]));
      expect(result.markdown).toContain("github::create_pull_request");
    });

    it("should display all tool types correctly", () => {
      const result = parseCopilotLog(
        JSON.stringify([
          {
            type: "system",
            subtype: "init",
            session_id: "all-tools",
            tools: ["Bash", "Read", "Write", "Edit", "LS", "Grep", "mcp__github__list_issues", "mcp__github__create_pull_request", "mcp__safe_outputs__create_issue"],
            model: "gpt-5",
          },
        ])
      );

      expect(result.markdown).toContain("Bash");
      expect(result.markdown).toContain("Read");
      expect(result.markdown).toContain("Write");
      expect(result.markdown).toContain("Edit");
      expect(result.markdown).toContain("LS");
      expect(result.markdown).toContain("Grep");
      expect(result.markdown).toContain("github::list_issues");
      expect(result.markdown).toContain("github::create_pull_request");
      expect(result.markdown).toContain("safe_outputs::create_issue");
    });
  });
});
