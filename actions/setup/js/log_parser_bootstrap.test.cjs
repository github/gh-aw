import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
const __filename = fileURLToPath(import.meta.url),
  __dirname = path.dirname(__filename);
describe("log_parser_bootstrap.cjs", () => {
  let mockCore, runLogParser, originalProcess;
  (beforeEach(() => {
    ((originalProcess = { ...process }),
      (mockCore = {
        debug: vi.fn(),
        info: vi.fn(),
        notice: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
        setFailed: vi.fn(),
        setOutput: vi.fn(),
        exportVariable: vi.fn(),
        summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(void 0) },
      }),
      (global.core = mockCore));
    const module = require("./log_parser_bootstrap.cjs");
    runLogParser = module.runLogParser;
  }),
    afterEach(() => {
      ((process.env = originalProcess.env), vi.restoreAllMocks(), delete global.core);
    }),
    describe("runLogParser", () => {
      (it("should handle missing GH_AW_AGENT_OUTPUT environment variable", () => {
        delete process.env.GH_AW_AGENT_OUTPUT;
        const mockParseLog = vi.fn();
        (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }), expect(mockCore.info).toHaveBeenCalledWith("No agent log file specified"), expect(mockParseLog).not.toHaveBeenCalled());
      }),
        it("should handle non-existent log file", () => {
          process.env.GH_AW_AGENT_OUTPUT = "/non/existent/file.log";
          const mockParseLog = vi.fn();
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }), expect(mockCore.info).toHaveBeenCalledWith("Log path not found: /non/existent/file.log"), expect(mockParseLog).not.toHaveBeenCalled());
        }),
        it("should read and parse a single log file", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "Test log content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue("## Parsed Log\n\nSuccess!");
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }),
            expect(mockParseLog).toHaveBeenCalledWith("Test log content"),
            expect(mockCore.info).toHaveBeenCalledWith("TestParser log parsed successfully"),
            expect(mockCore.summary.addRaw).toHaveBeenCalledWith("<details open>\n<summary>🤖 TestParser CLI Session</summary>\n\n## Parsed Log\n\nSuccess!\n</details>"),
            expect(mockCore.summary.write).toHaveBeenCalled(),
            fs.unlinkSync(logFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should handle parser returning object with markdown", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: !1 });
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }),
            expect(mockCore.info).toHaveBeenCalledWith("TestParser log parsed successfully"),
            expect(mockCore.summary.addRaw).toHaveBeenCalledWith("<details open>\n<summary>🤖 TestParser CLI Session</summary>\n\n## Result\n\n</details>"),
            expect(mockCore.setFailed).not.toHaveBeenCalled(),
            fs.unlinkSync(logFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should generate plain text summary when logEntries are available", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue({
            markdown: "## Result\n",
            mcpFailures: [],
            maxTurnsHit: !1,
            logEntries: [
              { type: "system", subtype: "init", model: "gpt-5" },
              { type: "assistant", message: { content: [{ type: "text", text: "Hello" }] } },
              { type: "result", num_turns: 2, duration_ms: 5e3 },
            ],
          });
          runLogParser({ parseLog: mockParseLog, parserName: "TestParser" });
          const infoCall = mockCore.info.mock.calls.find(call => call[0].includes("=== TestParser Execution Summary ==="));
          (expect(infoCall).toBeDefined(), expect(infoCall[0]).toContain("Model: gpt-5"), expect(infoCall[0]).toContain("Turns: 2"));
          const summaryCall = mockCore.summary.addRaw.mock.calls[0];
          (expect(summaryCall).toBeDefined(),
            expect(summaryCall[0]).toContain("```"),
            expect(summaryCall[0]).toContain("Conversation:"),
            expect(summaryCall[0]).toContain("Agent: Hello"),
            expect(summaryCall[0]).toContain("Statistics:"),
            expect(summaryCall[0]).toContain("  Turns: 2"),
            expect(summaryCall[0]).toContain("  Duration: 5s"),
            fs.unlinkSync(logFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should handle MCP failures", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: ["server1", "server2"], maxTurnsHit: !1 });
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }), expect(mockCore.setFailed).toHaveBeenCalledWith("MCP server(s) failed to launch: server1, server2"), fs.unlinkSync(logFile), fs.rmdirSync(tmpDir));
        }),
        it("should handle max-turns limit reached", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: !0 });
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }),
            expect(mockCore.setFailed).toHaveBeenCalledWith("Agent execution stopped: max-turns limit reached. The agent did not complete its task successfully."),
            fs.unlinkSync(logFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should read and concatenate multiple log files from directory when supportsDirectories is true", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile1 = path.join(tmpDir, "1.log"),
            logFile2 = path.join(tmpDir, "2.log");
          (fs.writeFileSync(logFile1, "First log"), fs.writeFileSync(logFile2, "Second log"), (process.env.GH_AW_AGENT_OUTPUT = tmpDir));
          const mockParseLog = vi.fn().mockReturnValue("## Parsed");
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser", supportsDirectories: !0 }),
            expect(mockParseLog).toHaveBeenCalledWith("First log\nSecond log"),
            fs.unlinkSync(logFile1),
            fs.unlinkSync(logFile2),
            fs.rmdirSync(tmpDir));
        }),
        it("should reject directories when supportsDirectories is false", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          (fs.writeFileSync(path.join(tmpDir, "1.log"), "content"), (process.env.GH_AW_AGENT_OUTPUT = tmpDir));
          const mockParseLog = vi.fn();
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser", supportsDirectories: !1 }),
            expect(mockCore.info).toHaveBeenCalledWith(`Log path is a directory but TestParser parser does not support directories: ${tmpDir}`),
            expect(mockParseLog).not.toHaveBeenCalled(),
            fs.unlinkSync(path.join(tmpDir, "1.log")),
            fs.rmdirSync(tmpDir));
        }),
        it("should handle empty directory gracefully", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          process.env.GH_AW_AGENT_OUTPUT = tmpDir;
          const mockParseLog = vi.fn();
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser", supportsDirectories: !0 }),
            expect(mockCore.info).toHaveBeenCalledWith(`No log files found in directory: ${tmpDir}`),
            expect(mockParseLog).not.toHaveBeenCalled(),
            fs.rmdirSync(tmpDir));
        }),
        it("should handle parser errors", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockImplementation(() => {
            throw new Error("Parser error");
          });
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }), expect(mockCore.setFailed).toHaveBeenCalledWith(expect.any(Error)), fs.unlinkSync(logFile), fs.rmdirSync(tmpDir));
        }),
        it("should handle failed parse (empty result)", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue("");
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }), expect(mockCore.error).toHaveBeenCalledWith("Failed to parse TestParser log"), fs.unlinkSync(logFile), fs.rmdirSync(tmpDir));
        }),
        it("should include safe outputs preview when GH_AW_SAFE_OUTPUTS is set", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log"),
            safeOutputsFile = path.join(tmpDir, "safe-outputs.jsonl");
          const safeOutputsContent = JSON.stringify({ type: "create_issue", title: "Test Issue", body: "Test body" });
          (fs.writeFileSync(logFile, "content"), fs.writeFileSync(safeOutputsFile, safeOutputsContent), (process.env.GH_AW_AGENT_OUTPUT = logFile), (process.env.GH_AW_SAFE_OUTPUTS = safeOutputsFile));
          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false });
          runLogParser({ parseLog: mockParseLog, parserName: "TestParser" });
          const summaryCall = mockCore.summary.addRaw.mock.calls[0];
          (expect(summaryCall).toBeDefined(),
            expect(summaryCall[0]).toContain("<summary>Safe Outputs</summary>"),
            expect(summaryCall[0]).toContain("**Total Entries:** 1"),
            expect(summaryCall[0]).toContain("create_issue"),
            fs.unlinkSync(logFile),
            fs.unlinkSync(safeOutputsFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should include safe outputs preview in core.info when logEntries available", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log"),
            safeOutputsFile = path.join(tmpDir, "safe-outputs.jsonl");
          const safeOutputsContent = JSON.stringify({ type: "add_comment", body: "Test comment" });
          (fs.writeFileSync(logFile, "content"), fs.writeFileSync(safeOutputsFile, safeOutputsContent), (process.env.GH_AW_AGENT_OUTPUT = logFile), (process.env.GH_AW_SAFE_OUTPUTS = safeOutputsFile));
          const mockParseLog = vi.fn().mockReturnValue({
            markdown: "## Result\n",
            mcpFailures: [],
            maxTurnsHit: false,
            logEntries: [
              { type: "system", subtype: "init", model: "gpt-4" },
              { type: "assistant", message: { content: [{ type: "text", text: "Hello" }] } },
              { type: "result", num_turns: 1, duration_ms: 3000 },
            ],
          });
          runLogParser({ parseLog: mockParseLog, parserName: "TestParser" });
          const infoCall = mockCore.info.mock.calls.find(call => call[0].includes("Safe Outputs Preview:"));
          (expect(infoCall).toBeDefined(),
            expect(infoCall[0]).toContain("Total: 1 entry"),
            expect(infoCall[0]).toContain("[1] add_comment"),
            expect(infoCall[0]).toContain("Body: Test comment"),
            fs.unlinkSync(logFile),
            fs.unlinkSync(safeOutputsFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should handle missing safe outputs file gracefully", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile), (process.env.GH_AW_SAFE_OUTPUTS = "/non/existent/file.jsonl"));
          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false });
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }), expect(mockCore.warning).not.toHaveBeenCalled(), fs.unlinkSync(logFile), fs.rmdirSync(tmpDir));
        }),
        it("should transform conversation.md headers for Copilot parser", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const conversationMd = path.join(tmpDir, "conversation.md");
          const conversationContent = `# Main Title

## Section 1

Some content here.

### Subsection

More content.`;

          fs.writeFileSync(conversationMd, conversationContent);
          process.env.GH_AW_AGENT_OUTPUT = tmpDir;

          const mockParseLog = vi.fn();
          runLogParser({ parseLog: mockParseLog, parserName: "Copilot", supportsDirectories: true });

          // Should transform headers (# to ##, ## to ###, etc.)
          const summaryCall = mockCore.summary.addRaw.mock.calls[0];
          expect(summaryCall).toBeDefined();
          expect(summaryCall[0]).toContain("## Main Title");
          expect(summaryCall[0]).toContain("### Section 1");
          expect(summaryCall[0]).toContain("#### Subsection");
          // Verify the original header level was transformed (check start of line)
          expect(summaryCall[0].split("\n")[0]).toBe("## Main Title");

          // Parser should not be called since conversation.md is used directly
          expect(mockParseLog).not.toHaveBeenCalled();

          fs.unlinkSync(conversationMd);
          fs.rmdirSync(tmpDir);
        }));
    }));
});
