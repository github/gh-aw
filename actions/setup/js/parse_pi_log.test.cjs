import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

describe("parse_pi_log.cjs", () => {
  let mockCore;
  let parsePiLog, transformPiEntries, isPiV3Schema, transformPiV3Entries, computePiV3Stats, normalizePiToolName;

  beforeEach(async () => {
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

    const module = await import("./parse_pi_log.cjs?" + Date.now());
    parsePiLog = module.parsePiLog;
    transformPiEntries = module.transformPiEntries;
    isPiV3Schema = module.isPiV3Schema;
    transformPiV3Entries = module.transformPiV3Entries;
    computePiV3Stats = module.computePiV3Stats;
    normalizePiToolName = module.normalizePiToolName;
  });

  afterEach(() => {
    delete global.core;
  });

  describe("parsePiLog function", () => {
    it("should return a default message for empty input", () => {
      const result = parsePiLog("");

      expect(result.markdown).toContain("No log content provided");
      expect(result.logEntries).toEqual([]);
      expect(result.mcpFailures).toEqual([]);
      expect(result.maxTurnsHit).toBe(false);
    });

    it("should return error message for null input", () => {
      const result = parsePiLog(null);

      expect(result.markdown).toContain("No log content provided");
    });

    it("should return unrecognized format message for non-JSON content", () => {
      const result = parsePiLog("plain text log content\nnot json at all");

      expect(result.markdown).toContain("Log format not recognized as Pi JSONL");
    });

    it("should parse init entry and show model in initialization section", () => {
      const logContent = [JSON.stringify({ type: "init", session_id: "sess-abc", model: "pi-3" })].join("\n");

      const result = parsePiLog(logContent);

      expect(result.markdown).toContain("<summary>Initialization</summary>");
      expect(result.markdown).toContain("pi-3");
      expect(result.markdown).toContain("sess-abc");
    });

    it("should merge consecutive assistant delta messages into one entry", () => {
      const logContent = [JSON.stringify({ type: "assistant", content: "I will analyze", delta: true }), JSON.stringify({ type: "assistant", content: " the repository.", delta: true })].join("\n");

      const result = parsePiLog(logContent);

      expect(result.markdown).toContain("<summary>Reasoning</summary>");
      expect(result.markdown).toContain("I will analyze the repository.");
    });

    it("should render tool use with success status", () => {
      const logContent = [
        JSON.stringify({ type: "tool_use", tool_name: "list_pull_requests", tool_id: "tool_001", parameters: { owner: "github", repo: "gh-aw" } }),
        JSON.stringify({ type: "tool_result", tool_id: "tool_001", status: "success", output: '{"items":[]}' }),
      ].join("\n");

      const result = parsePiLog(logContent);

      expect(result.markdown).toContain("✅");
      expect(result.markdown).toContain("list_pull_requests");
    });

    it("should render tool error when status is not success", () => {
      const logContent = [
        JSON.stringify({ type: "tool_use", tool_name: "read_file", tool_id: "tool_002", parameters: { path: "/nonexistent" } }),
        JSON.stringify({ type: "tool_result", tool_id: "tool_002", status: "error", output: "File not found" }),
      ].join("\n");

      const result = parsePiLog(logContent);

      expect(result.markdown).toContain("❌");
      expect(result.markdown).toContain("read_file");
    });

    it("should extract token usage from result stats", () => {
      const logContent = [JSON.stringify({ type: "result", stats: { input_tokens: 500, output_tokens: 200, duration_ms: 3000, turns: 2 } })].join("\n");

      const result = parsePiLog(logContent);

      // The Information section should include token counts
      expect(result.markdown).toContain("500");
      expect(result.markdown).toContain("200");
    });

    it("should include normalized result entry in logEntries for OTEL telemetry enrichment", () => {
      const logContent = [JSON.stringify({ type: "result", stats: { input_tokens: 500, output_tokens: 200, duration_ms: 3000, turns: 2 } })].join("\n");

      const result = parsePiLog(logContent);

      const resultEntry = result.logEntries && result.logEntries.find(e => e.type === "result");
      expect(resultEntry).toBeDefined();
      expect(resultEntry.num_turns).toBe(2);
      expect(resultEntry.usage).toEqual({ input_tokens: 500, output_tokens: 200 });
    });
  });

  describe("transformPiEntries function", () => {
    it("should transform init entry to system/init", () => {
      const raw = [{ type: "init", model: "pi-3", session_id: "s1" }];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(1);
      expect(entries[0].type).toBe("system");
      expect(entries[0].subtype).toBe("init");
      expect(entries[0].model).toBe("pi-3");
    });

    it("should transform assistant entry to canonical assistant type", () => {
      const raw = [{ type: "assistant", content: "Hello world", delta: false }];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(1);
      expect(entries[0].type).toBe("assistant");
      expect(entries[0].message.content[0].text).toBe("Hello world");
    });

    it("should merge consecutive delta assistant entries", () => {
      const raw = [
        { type: "assistant", content: "Part one", delta: true },
        { type: "assistant", content: " part two", delta: true },
      ];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(1);
      expect(entries[0].message.content[0].text).toBe("Part one part two");
    });

    it("should not merge non-delta assistant entries", () => {
      const raw = [
        { type: "assistant", content: "First message", delta: false },
        { type: "assistant", content: "Second message", delta: false },
      ];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(2);
    });

    it("should transform tool_use entry correctly", () => {
      const raw = [{ type: "tool_use", tool_name: "bash", tool_id: "t1", parameters: { command: "ls" } }];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(1);
      expect(entries[0].type).toBe("assistant");
      expect(entries[0].message.content[0].type).toBe("tool_use");
      expect(entries[0].message.content[0].name).toBe("Bash");
      expect(entries[0].message.content[0].id).toBe("t1");
    });

    it("should transform tool_result entry correctly", () => {
      const raw = [{ type: "tool_result", tool_id: "t1", status: "success", output: "output text" }];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(1);
      expect(entries[0].type).toBe("user");
      expect(entries[0].message.content[0].type).toBe("tool_result");
      expect(entries[0].message.content[0].content).toBe("output text");
      expect(entries[0].message.content[0].is_error).toBe(false);
    });

    it("should mark tool_result as error when status is not success", () => {
      const raw = [{ type: "tool_result", tool_id: "t1", status: "error", output: "failed" }];
      const entries = transformPiEntries(raw);

      expect(entries[0].message.content[0].is_error).toBe(true);
    });

    it("should skip empty assistant content", () => {
      const raw = [
        { type: "assistant", content: "", delta: false },
        { type: "assistant", content: "   ", delta: false },
      ];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(0);
    });

    it("should ignore unknown event types", () => {
      const raw = [{ type: "unknown_event", data: "something" }];
      const entries = transformPiEntries(raw);

      expect(entries).toHaveLength(0);
    });
  });

  describe("Pi v3 streaming schema", () => {
    // A minimal but representative v3 stream: session init, one turn that calls a tool
    // (with its tool_execution_end result emitted before the finalizing turn_end, as the
    // real Pi CLI does), then a final turn with the closing assistant text.
    const v3Lines = [
      { type: "session", version: 3, id: "sess-v3", timestamp: "2026-07-14T08:25:33.707Z", cwd: "/repo" },
      { type: "agent_start" },
      { type: "turn_start" },
      {
        type: "tool_execution_start",
        toolCallId: "call_1",
        toolName: "bash",
        args: { command: "ls" },
      },
      {
        type: "tool_execution_end",
        toolCallId: "call_1",
        toolName: "bash",
        result: { content: [{ type: "text", text: "file-a\nfile-b" }] },
      },
      {
        type: "turn_end",
        message: {
          role: "assistant",
          model: "gpt-5.4",
          content: [
            { type: "text", text: "Let me list the files." },
            { type: "toolCall", id: "call_1", name: "bash", arguments: { command: "ls" } },
          ],
          usage: { input: 1000, output: 40, totalTokens: 1040 },
        },
      },
      { type: "turn_end", message: { role: "assistant", model: "gpt-5.4", content: [{ type: "text", text: "All done." }], usage: { input: 1200, output: 15, totalTokens: 1215 } } },
      { type: "agent_end", messages: [] },
    ];
    const v3Log = v3Lines.map(l => JSON.stringify(l)).join("\n");

    it("detects the v3 schema and rejects the legacy schema", () => {
      expect(isPiV3Schema(v3Lines)).toBe(true);
      expect(
        isPiV3Schema([
          { type: "init", model: "pi-3" },
          { type: "assistant", content: "hi" },
        ])
      ).toBe(false);
    });

    it("renders assistant text, tool calls, and tool results from a v3 stream", () => {
      const result = parsePiLog(v3Log);

      expect(result.markdown).toContain("Let me list the files.");
      expect(result.markdown).toContain("All done.");
      expect(result.markdown).toContain("`ls`");
      expect(result.markdown).toContain("gpt-5.4");
      // The conversation must not be empty for a real v3 log.
      expect(result.markdown.length).toBeGreaterThan(0);
    });

    it("emits each tool_use before its paired tool_result in a v3 turn", () => {
      const entries = transformPiV3Entries(v3Lines);
      const toolUseIdx = entries.findIndex(e => e.type === "assistant" && e.message.content[0].type === "tool_use" && e.message.content[0].id === "call_1");
      const toolResultIdx = entries.findIndex(e => e.type === "user" && e.message.content[0].type === "tool_result" && e.message.content[0].tool_use_id === "call_1");

      expect(toolUseIdx).toBeGreaterThanOrEqual(0);
      expect(toolResultIdx).toBeGreaterThan(toolUseIdx);
    });

    it("computes v3 stats: turns counted, output summed, input summed", () => {
      const stats = computePiV3Stats(v3Lines);

      expect(stats).not.toBeNull();
      expect(stats.turns).toBe(2);
      expect(stats.output_tokens).toBe(55); // 40 + 15
      expect(stats.input_tokens).toBe(2200); // 1000 + 1200
    });

    it("includes a normalized v3 result entry for OTEL enrichment", () => {
      const result = parsePiLog(v3Log);
      const resultEntry = result.logEntries && result.logEntries.find(e => e.type === "result");

      expect(resultEntry).toBeDefined();
      expect(resultEntry.num_turns).toBe(2);
      expect(resultEntry.usage).toEqual({ input_tokens: 2200, output_tokens: 55 });
    });

    it("marks failed tool results as errors", () => {
      const entries = transformPiV3Entries([
        { type: "session", version: 3, id: "s" },
        { type: "tool_execution_end", toolCallId: "t1", isError: true, result: { content: [{ type: "text", text: "boom" }] } },
        { type: "turn_end", message: { role: "assistant", model: "m", content: [{ type: "toolCall", id: "t1", name: "bash", arguments: {} }], usage: { input: 1, output: 1 } } },
      ]);
      const toolResult = entries.find(e => e.type === "user" && e.message.content[0].type === "tool_result");

      expect(toolResult).toBeDefined();
      expect(toolResult.message.content[0].is_error).toBe(true);
      expect(toolResult.message.content[0].content).toContain("boom");
    });

    it("surfaces a provider-level errorMessage from an empty-content turn_end (e.g. model_not_supported)", () => {
      // Reproduces a real failed run: the provider request itself fails (HTTP 400), so
      // turn_end carries content: [] with no assistant text or tool calls — the only trace
      // of the failure is the top-level errorMessage on the turn_end message.
      const failedTurnLines = [
        { type: "session", version: 3, id: "s1" },
        { type: "agent_start" },
        { type: "turn_start" },
        {
          type: "turn_end",
          message: {
            role: "assistant",
            model: "auto",
            content: [],
            stopReason: "error",
            usage: { input: 0, output: 0 },
            errorMessage: '400: {"message":"The requested model is not supported.","code":"model_not_supported","param":"model","type":"invalid_request_error"}',
          },
          toolResults: [],
        },
        { type: "agent_end", messages: [], willRetry: false },
        { type: "agent_settled" },
      ];
      const failedLog = failedTurnLines.map(l => JSON.stringify(l)).join("\n");

      const stats = computePiV3Stats(failedTurnLines);
      expect(stats.errors).toEqual([expect.stringContaining("model_not_supported")]);

      const result = parsePiLog(failedLog);
      expect(result.markdown).toContain("model_not_supported");

      const resultEntry = result.logEntries.find(e => e.type === "result");
      expect(resultEntry.errors).toEqual([expect.stringContaining("model_not_supported")]);
    });
  });

  describe("normalizePiToolName", () => {
    it("maps lowercase bash to Bash", () => {
      expect(normalizePiToolName("bash")).toBe("Bash");
    });

    it("maps uppercase and mixed-case bash variants to Bash", () => {
      expect(normalizePiToolName("BASH")).toBe("Bash");
      expect(normalizePiToolName("Bash")).toBe("Bash");
    });

    it("passes through non-bash tool names unchanged", () => {
      expect(normalizePiToolName("str_replace_editor")).toBe("str_replace_editor");
      expect(normalizePiToolName("read")).toBe("read");
    });
  });
});
