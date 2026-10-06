import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
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
            expect(mockCore.summary.addRaw).toHaveBeenCalledWith("<details open>\n<summary>Agentic Conversation</summary>\n\n## Parsed Log\n\nSuccess!\n</details>"),
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
            expect(mockCore.summary.addRaw).toHaveBeenCalledWith("<details open>\n<summary>Agentic Conversation</summary>\n\n## Result\n\n</details>"),
            expect(mockCore.setFailed).not.toHaveBeenCalled(),
            fs.unlinkSync(logFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should fail Claude runs when no structured log entries are parsed", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "unstructured log output");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(
              `${ERR_CONFIG}: Claude execution failed: no structured log entries were produced. Claude startup failed before structured logging (exitCode=unknown). startup/configuration failure detected.`
            );
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should report an AWF process exit code when Claude produces no structured logs", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "awf gateway startup failed\nProcess exiting with code: 17\n");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(
              `${ERR_CONFIG}: Claude execution failed: no structured log entries were produced. Claude startup failed before structured logging (exitCode=17). startup/configuration failure detected.`
            );
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
          }
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
            expect(summaryCall[0]).toContain("◆ Hello"),
            expect(summaryCall[0]).toContain("Statistics:"),
            expect(summaryCall[0]).toContain("  Turns: 2"),
            expect(summaryCall[0]).toContain("  Duration: 5s"),
            fs.unlinkSync(logFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should handle MCP failures", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: ["server1", "server2"], maxTurnsHit: !1 });
          (await runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }),
            expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: MCP server(s) failed to launch: server1, server2`),
            fs.unlinkSync(logFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should warn instead of failing MCP failures when safe outputs exist", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const safeOutputsFile = path.join(tmpDir, "safe-outputs.jsonl");

          fs.writeFileSync(logFile, "content");
          fs.writeFileSync(safeOutputsFile, `  ${JSON.stringify({ type: "create_issue", title: "Test", body: "Test body" })}\r\n`);
          process.env.GH_AW_AGENT_OUTPUT = logFile;
          process.env.GH_AW_SAFE_OUTPUTS = safeOutputsFile;

          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: ["server1"], maxTurnsHit: !1 });

          (await runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }),
            expect(mockCore.warning).toHaveBeenCalledWith("MCP server(s) failed to launch (server1), but agent completed with 1 safe output entry"),
            expect(mockCore.setFailed).not.toHaveBeenCalled(),
            fs.unlinkSync(logFile),
            fs.unlinkSync(safeOutputsFile),
            fs.rmdirSync(tmpDir));
        }),
        it("should warn (non-fatal) when MCP fails but agent ran turns (legacy result entry)", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            // No safe outputs — simulates a workflow that uses GitHub MCP directly (e.g. creates a
            // discussion) without writing safe-output file entries.
            const mockParseLog = vi.fn().mockReturnValue({
              markdown: "## Result\n",
              mcpFailures: ["github"],
              maxTurnsHit: false,
              logEntries: [
                { type: "system", subtype: "init", model: "claude-3-7-sonnet" },
                { type: "assistant", message: { content: [{ type: "text", text: "Analysis complete" }] } },
                { type: "result", num_turns: 34, duration_ms: 60000 },
              ],
            });
            await runLogParser({ parseLog: mockParseLog, parserName: "TestParser" });
            expect(mockCore.warning).toHaveBeenCalledWith("MCP server(s) failed to launch (github), but agent completed turns — treating as non-fatal post-completion relaunch");
            expect(mockCore.setFailed).not.toHaveBeenCalled();
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should warn (non-fatal) when MCP fails but agent ran turns (Copilot event session.result)", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            // Copilot event format entries (as returned by parse_claude_log.cjs via
            // convertLegacyLogEntriesToCopilotEvents): session.result with data.numTurns.
            const mockParseLog = vi.fn().mockReturnValue({
              markdown: "## Result\n",
              mcpFailures: ["github"],
              maxTurnsHit: false,
              logEntries: [
                { type: "session.init", data: { model: "claude-opus-4-5" } },
                { type: "assistant.message", data: { content: "Done" } },
                { type: "session.result", data: { numTurns: 34, durationMs: 60000 } },
              ],
            });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.warning).toHaveBeenCalledWith("MCP server(s) failed to launch (github), but agent completed turns — treating as non-fatal post-completion relaunch");
            expect(mockCore.setFailed).not.toHaveBeenCalled();
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should still fail when MCP fails and agent ran no turns", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            // logEntries has no result entry — agent never ran any turns (startup failure).
            const mockParseLog = vi.fn().mockReturnValue({
              markdown: "## Result\n",
              mcpFailures: ["github"],
              maxTurnsHit: false,
              logEntries: [{ type: "system", subtype: "init" }],
            });
            await runLogParser({ parseLog: mockParseLog, parserName: "TestParser" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: MCP server(s) failed to launch: github`);
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should warn (non-fatal) when Claude has empty logEntries but safe outputs exist", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const safeOutputsFile = path.join(tmpDir, "safe-outputs.jsonl");
          try {
            fs.writeFileSync(logFile, "some raw content");
            fs.writeFileSync(safeOutputsFile, JSON.stringify({ type: "create_issue", title: "Test", body: "Test body" }));
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            process.env.GH_AW_SAFE_OUTPUTS = safeOutputsFile;
            // Parser returns markdown but no structured logEntries — simulates sandbox teardown
            // race leaving agent-stdio.log unreadable after the agent completed successfully.
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).not.toHaveBeenCalled();
            expect(mockCore.warning).toHaveBeenCalledWith("Claude produced no structured log entries, but agent completed with 1 safe output entry — treating as non-fatal post-completion infrastructure failure");
          } finally {
            fs.unlinkSync(logFile);
            fs.unlinkSync(safeOutputsFile);
            delete process.env.GH_AW_SAFE_OUTPUTS;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should fail when Claude has empty logEntries and no safe outputs (startup failure)", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "some raw content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(
              `${ERR_CONFIG}: Claude execution failed: no structured log entries were produced. Claude startup failed before structured logging (exitCode=unknown). startup/configuration failure detected.`
            );
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should omit Claude startup diagnostics tail when no startup lines are matched", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "secret-token-value\nplain stdout line");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.summary.addRaw).toHaveBeenCalledTimes(1);
            expect(mockCore.summary.addRaw.mock.calls.flat().join("\n")).not.toContain("secret-token-value");
          } finally {
            fs.unlinkSync(logFile);
            delete process.env.GH_AW_AGENT_OUTPUT;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should escape Claude startup diagnostics summary content", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const logContentWithHtmlChars = "[claude-harness] attempt 1 failed: exitCode=1 hasOutput=false\n[claude-harness] stderr: <pre>&```</pre>\n[claude-harness] done: exitCode=1";
          try {
            fs.writeFileSync(logFile, logContentWithHtmlChars);
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            const diagnosticsSummary = mockCore.summary.addRaw.mock.calls[1][0];
            expect(diagnosticsSummary).toContain("<pre><code>");
            expect(diagnosticsSummary).toContain("&lt;pre&gt;&amp;```&lt;/pre&gt;");
          } finally {
            fs.unlinkSync(logFile);
            delete process.env.GH_AW_AGENT_OUTPUT;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should not print 'parsed successfully' for Claude when logEntries is empty", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const safeOutputsFile = path.join(tmpDir, "safe-outputs.jsonl");
          try {
            fs.writeFileSync(logFile, "some raw content");
            fs.writeFileSync(safeOutputsFile, JSON.stringify({ type: "add_comment", body: "Done" }));
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            process.env.GH_AW_SAFE_OUTPUTS = safeOutputsFile;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            const infoCalls = mockCore.info.mock.calls.map(c => c[0]);
            expect(infoCalls.some(msg => msg.includes("Claude log parsed successfully"))).toBe(false);
            expect(mockCore.setFailed).not.toHaveBeenCalled();
            expect(mockCore.warning).toHaveBeenCalledWith("Claude produced no structured log entries, but agent completed with 1 safe output entry — treating as non-fatal post-completion infrastructure failure");
          } finally {
            fs.unlinkSync(logFile);
            fs.unlinkSync(safeOutputsFile);
            delete process.env.GH_AW_SAFE_OUTPUTS;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should treat logEntries: null as missing entries for Claude guardrail (no safe outputs → setFailed)", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, "some raw content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: null });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(
              `${ERR_CONFIG}: Claude execution failed: no structured log entries were produced. Claude startup failed before structured logging (exitCode=unknown). startup/configuration failure detected.`
            );
          } finally {
            fs.unlinkSync(logFile);
            delete process.env.GH_AW_AGENT_OUTPUT;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should classify Claude empty-log startup rate-limit signatures as transient inference availability", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, `[claude-harness] attempt 1 failed: exitCode=1 hasOutput=false\n[claude-harness] done: exitCode=1\nAPI Error: Request rejected (429)`);
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(
              `${ERR_API}: Claude execution failed: no structured log entries were produced. Claude startup failed before structured logging (exitCode=1). transient inference availability signal detected.`
            );
            expect(mockCore.setOutput).toHaveBeenCalledWith("ai_credits_rate_limit_error", "true");
          } finally {
            fs.unlinkSync(logFile);
            delete process.env.GH_AW_AGENT_OUTPUT;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should classify Claude empty-log startup inference-access denials separately", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, `[claude-harness] attempt 1 failed: exitCode=1 hasOutput=false\nAccess denied by policy settings\n[claude-harness] done: exitCode=1`);
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Claude execution failed: no structured log entries were produced. Claude startup failed before structured logging (exitCode=1). inference access denied by policy.`);
            expect(mockCore.setOutput).toHaveBeenCalledWith("inference_access_error", "true");
            expect(mockCore.setOutput).not.toHaveBeenCalledWith("ai_credits_rate_limit_error", "true");
          } finally {
            fs.unlinkSync(logFile);
            delete process.env.GH_AW_AGENT_OUTPUT;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should not classify non-HTTP 500 text as transient inference availability", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          try {
            fs.writeFileSync(logFile, `[claude-harness] attempt 1 failed: exitCode=1 hasOutput=false\nerror code 500\n[claude-harness] done: exitCode=1`);
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: [] });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).toHaveBeenCalledWith(
              `${ERR_CONFIG}: Claude execution failed: no structured log entries were produced. Claude startup failed before structured logging (exitCode=1). startup/configuration failure detected.`
            );
          } finally {
            fs.unlinkSync(logFile);
            delete process.env.GH_AW_AGENT_OUTPUT;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should treat logEntries: null as missing entries for Claude guardrail (safe outputs → warning)", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const safeOutputsFile = path.join(tmpDir, "safe-outputs.jsonl");
          try {
            fs.writeFileSync(logFile, "some raw content");
            fs.writeFileSync(safeOutputsFile, JSON.stringify({ type: "create_issue", title: "Test", body: "Test body" }));
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            process.env.GH_AW_SAFE_OUTPUTS = safeOutputsFile;
            const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: false, logEntries: null });
            await runLogParser({ parseLog: mockParseLog, parserName: "Claude" });
            expect(mockCore.setFailed).not.toHaveBeenCalled();
            expect(mockCore.warning).toHaveBeenCalledWith("Claude produced no structured log entries, but agent completed with 1 safe output entry — treating as non-fatal post-completion infrastructure failure");
          } finally {
            fs.unlinkSync(logFile);
            fs.unlinkSync(safeOutputsFile);
            delete process.env.GH_AW_AGENT_OUTPUT;
            delete process.env.GH_AW_SAFE_OUTPUTS;
            fs.rmdirSync(tmpDir);
          }
        }),
        it("should handle max-turns limit reached", async () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-")),
            logFile = path.join(tmpDir, "test.log");
          (fs.writeFileSync(logFile, "content"), (process.env.GH_AW_AGENT_OUTPUT = logFile));
          const mockParseLog = vi.fn().mockReturnValue({ markdown: "## Result\n", mcpFailures: [], maxTurnsHit: !0 });
          (await runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }),
            expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Agent execution stopped: max-turns limit reached. The agent did not complete its task successfully.`),
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
          (runLogParser({ parseLog: mockParseLog, parserName: "TestParser" }), expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_API}: Parser error`), fs.unlinkSync(logFile), fs.rmdirSync(tmpDir));
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
        it("should write result entry to agent-stdio.log when logEntries has a result entry", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const stdioLogPath = "/tmp/gh-aw/agent-stdio.log";
          try {
            fs.writeFileSync(logFile, "content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            fs.mkdirSync(path.dirname(stdioLogPath), { recursive: true });
            if (fs.existsSync(stdioLogPath)) fs.unlinkSync(stdioLogPath);
            const mockParseLog = vi.fn().mockReturnValue({
              markdown: "## Result\n",
              mcpFailures: [],
              maxTurnsHit: false,
              logEntries: [{ type: "result", num_turns: 5, usage: { input_tokens: 100, output_tokens: 50 } }],
            });
            runLogParser({ parseLog: mockParseLog, parserName: "Copilot" });
            expect(fs.existsSync(stdioLogPath)).toBe(true);
            const parsed = JSON.parse(fs.readFileSync(stdioLogPath, "utf8").trim());
            expect(parsed).toEqual({
              type: "result",
              num_turns: 5,
              usage: { input_tokens: 100, output_tokens: 50 },
            });
            expect(mockCore.info).toHaveBeenCalledWith("[log-parser] Wrote Copilot result entry to agent-stdio.log: num_turns=5");
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
            if (fs.existsSync(stdioLogPath)) fs.unlinkSync(stdioLogPath);
          }
        }),
        it("should not overwrite result entry when agent-stdio.log already has one in JSON array line", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const stdioLogPath = "/tmp/gh-aw/agent-stdio.log";
          try {
            fs.writeFileSync(logFile, "content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            fs.mkdirSync(path.dirname(stdioLogPath), { recursive: true });
            fs.writeFileSync(stdioLogPath, `[ {"type":"result","num_turns":3} ]\n`);
            const mockParseLog = vi.fn().mockReturnValue({
              markdown: "## Result\n",
              mcpFailures: [],
              maxTurnsHit: false,
              logEntries: [{ type: "result", num_turns: 5, usage: { input_tokens: 100, output_tokens: 50 } }],
            });
            runLogParser({ parseLog: mockParseLog, parserName: "Copilot" });
            const written = fs.readFileSync(stdioLogPath, "utf8");
            expect(written.trim().split("\n")).toHaveLength(1);
            expect(JSON.parse(written.trim())[0].num_turns).toBe(3);
            expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("[log-parser] Wrote"));
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
            if (fs.existsSync(stdioLogPath)) fs.unlinkSync(stdioLogPath);
          }
        }),
        it("should warn (non-fatal) when writing to agent-stdio.log fails", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const stdioLogPath = "/tmp/gh-aw/agent-stdio.log";
          try {
            fs.writeFileSync(logFile, "content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            fs.mkdirSync(path.dirname(stdioLogPath), { recursive: true });
            if (fs.existsSync(stdioLogPath)) fs.unlinkSync(stdioLogPath);
            vi.spyOn(fs, "appendFileSync").mockImplementationOnce(() => {
              throw new Error("Permission denied");
            });
            const mockParseLog = vi.fn().mockReturnValue({
              markdown: "## Result\n",
              mcpFailures: [],
              maxTurnsHit: false,
              logEntries: [{ type: "result", num_turns: 5, usage: { input_tokens: 100, output_tokens: 50 } }],
            });
            runLogParser({ parseLog: mockParseLog, parserName: "Copilot" });
            expect(mockCore.warning).toHaveBeenCalledWith("[log-parser] Failed to enrich agent-stdio.log with result entry: Permission denied");
            expect(mockCore.setFailed).not.toHaveBeenCalled();
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
            if (fs.existsSync(stdioLogPath)) fs.unlinkSync(stdioLogPath);
          }
        }),
        it("redacts add-mask values in agent-stdio.log before artifact upload", () => {
          const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
          const logFile = path.join(tmpDir, "test.log");
          const stdioLogPath = "/tmp/gh-aw/agent-stdio.log";
          const secret = "mask_" + "a1b2c3d4".repeat(6);
          try {
            fs.writeFileSync(logFile, "content");
            process.env.GH_AW_AGENT_OUTPUT = logFile;
            fs.mkdirSync(path.dirname(stdioLogPath), { recursive: true });
            fs.writeFileSync(stdioLogPath, `before\n::add-mask::${secret}\nvalue=${secret}\nafter\n`, "utf8");
            const mockParseLog = vi.fn().mockReturnValue({
              markdown: "## Result\n",
              mcpFailures: [],
              maxTurnsHit: false,
              logEntries: [],
            });
            runLogParser({ parseLog: mockParseLog, parserName: "Copilot" });
            const redacted = fs.readFileSync(stdioLogPath, "utf8");
            expect(redacted).not.toContain(secret);
            expect(redacted).not.toContain("::add-mask::");
            expect(redacted).toContain("value=***");
            expect(mockCore.info).toHaveBeenCalledWith("[log-parser] Sanitized agent-stdio.log before artifact upload using 1 collected add-mask value(s)");
          } finally {
            fs.unlinkSync(logFile);
            fs.rmdirSync(tmpDir);
            if (fs.existsSync(stdioLogPath)) fs.unlinkSync(stdioLogPath);
          }
        }));
    }));

  describe("Copilot session source fallback", () => {
    let root;
    beforeEach(() => {
      root = fs.mkdtempSync(path.join(__dirname, "test-copilot-sources-"));
      process.env.GH_AW_AGENT_OUTPUT = path.join(root, "logs");
      delete process.env.GH_AW_SAFE_OUTPUTS;
    });
    afterEach(() => {
      fs.rmSync(root, { recursive: true, force: true });
    });
    const stdio = JSON.stringify({ type: "assistant.message", data: { content: "Recovered stdio conversation." } });
    const native = JSON.stringify({ type: "assistant.message", data: { content: "Native conversation." } });
    const runCopilot = rootDir => runLogParser({ parseLog: require("./parse_copilot_log.cjs").parseCopilotLog, parserName: "Copilot", supportsDirectories: true, rootDir });

    it.each(["missing", "empty", "malformed", "debug-only"])("renders stdio when session logs are %s", async state => {
      const logs = path.join(root, "logs");
      if (state !== "missing") fs.mkdirSync(logs);
      if (state === "empty" || state === "malformed") fs.writeFileSync(path.join(logs, "events.jsonl"), state === "empty" ? "" : "{bad\n");
      if (state === "debug-only") fs.writeFileSync(path.join(logs, "process.log"), native);
      fs.writeFileSync(path.join(root, "agent-stdio.log"), stdio);
      await runCopilot(root);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw.mock.calls[0][0]).toContain("Recovered stdio conversation.");
      expect(mockCore.summary.addRaw.mock.calls[0][0]).not.toContain("Native conversation.");
      expect(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8")).toContain("Recovered stdio conversation.");
      if (state === "empty" || state === "malformed") expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("trying"));
    });

    it.each([
      [JSON.stringify({ type: "session.start", data: { sessionId: "native" } })],
      [`${JSON.stringify({ type: "session.start", data: { sessionId: "native" } })}\n{"type":"assistant.message",`],
      [`${native}\n{"type":"assistant.message",`],
    ])("falls back to stdio when native events are incomplete or lack a conversation", async nativeContent => {
      fs.mkdirSync(path.join(root, "logs"));
      fs.writeFileSync(path.join(root, "logs/events.jsonl"), nativeContent);
      fs.writeFileSync(path.join(root, "agent-stdio.log"), stdio);
      await runCopilot(root);
      expect(mockCore.summary.addRaw.mock.calls[0][0]).toContain("Recovered stdio conversation.");
      expect(mockCore.summary.addRaw.mock.calls[0][0]).not.toContain("Native conversation.");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("trying"));
    });

    it("prefers native session events without duplicating stdio or debug observations", async () => {
      fs.mkdirSync(path.join(root, "logs"));
      fs.writeFileSync(path.join(root, "logs/events.jsonl"), native);
      fs.writeFileSync(path.join(root, "logs/process.log"), stdio);
      fs.writeFileSync(path.join(root, "agent-stdio.log"), stdio);
      await runCopilot(root);
      const summary = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summary).toContain("Native conversation.");
      expect(summary).not.toContain("Recovered stdio conversation.");
      const events = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
      expect(events.filter(event => event.type === "assistant.message")).toHaveLength(1);
    });

    it("falls back to debug logs when stdio contains only infrastructure output", async () => {
      fs.mkdirSync(path.join(root, "logs"));
      fs.writeFileSync(path.join(root, "logs/process.log"), native);
      fs.writeFileSync(path.join(root, "agent-stdio.log"), "[INFO] Containers stopped successfully\n");
      await runCopilot(root);
      expect(mockCore.summary.addRaw.mock.calls[0][0]).toContain("Native conversation.");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("trying"));
    });

    it("does not read debug logs when a preferred native conversation is usable", async () => {
      fs.mkdirSync(path.join(root, "logs"));
      fs.writeFileSync(path.join(root, "logs/events.jsonl"), native);
      fs.mkdirSync(path.join(root, "logs/unreadable.log"));
      await runCopilot(root);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw.mock.calls[0][0]).toContain("Native conversation.");
    });

    it("reports unrecognized Copilot logs rather than claiming successful parsing", async () => {
      fs.mkdirSync(path.join(root, "logs"));
      fs.writeFileSync(path.join(root, "logs/process.log"), "unrecognized log\n");
      await runCopilot(root);
      expect(mockCore.warning).toHaveBeenCalledWith("Copilot produced no structured session events; publishing log diagnostics only");
      expect(mockCore.info).not.toHaveBeenCalledWith("Copilot log parsed successfully");
      expect(mockCore.summary.addRaw.mock.calls[0][0]).toContain("Log format not recognized");
    });

    it("redacts stdio masks in a preferred native conversation before publication", async () => {
      const secret = "private-runtime-mask";
      fs.mkdirSync(path.join(root, "logs"));
      fs.writeFileSync(path.join(root, "logs/events.jsonl"), JSON.stringify({ type: "assistant.message", data: { content: `Native ${secret}` } }));
      fs.writeFileSync(path.join(root, "agent-stdio.log"), `::add-mask::${secret}\n`);
      await runCopilot(root);
      expect(mockCore.summary.addRaw.mock.calls[0][0]).not.toContain(secret);
      expect(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8")).not.toContain(secret);
    });
  });

  describe("step summary secret redaction", () => {
    // Built from parts so the fixtures are never literal credential strings in source.
    const FAKE_PAT = "ghp_" + "a1b2c3d4e5".repeat(3) + "f6g7h8";
    const FAKE_AWS_KEY = "AKIA" + "IOSFODNN7EXAMPLE";

    it("should redact credential-shaped tool input and output from the fallback summary", async () => {
      const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
      const logFile = path.join(tmpDir, "test.log");
      try {
        fs.writeFileSync(logFile, "content");
        process.env.GH_AW_AGENT_OUTPUT = logFile;
        const mockParseLog = vi.fn().mockReturnValue({
          markdown: `### Bash\n\ncurl -H "Authorization: ${FAKE_PAT}" https://example.com\n\nOutput: key ${FAKE_AWS_KEY}\n`,
          mcpFailures: [],
          maxTurnsHit: false,
        });
        await runLogParser({ parseLog: mockParseLog, parserName: "TestParser" });
        const summaryCall = mockCore.summary.addRaw.mock.calls[0];
        expect(summaryCall).toBeDefined();
        expect(summaryCall[0]).not.toContain(FAKE_PAT);
        expect(summaryCall[0]).not.toContain(FAKE_AWS_KEY);
        expect(summaryCall[0]).toContain("***REDACTED***");
      } finally {
        fs.unlinkSync(logFile);
        fs.rmdirSync(tmpDir);
      }
    });

    it("should redact credential-shaped tool input and output from the structured summary", async () => {
      const tmpDir = fs.mkdtempSync(path.join(__dirname, "test-"));
      const logFile = path.join(tmpDir, "test.log");
      try {
        fs.writeFileSync(logFile, "content");
        process.env.GH_AW_AGENT_OUTPUT = logFile;
        const mockParseLog = vi.fn().mockReturnValue({
          markdown: "## Result\n",
          mcpFailures: [],
          maxTurnsHit: false,
          logEntries: [
            { type: "system", subtype: "init", model: "gpt-5" },
            {
              type: "assistant",
              message: {
                content: [{ type: "tool_use", id: "tool-1", name: "Bash", input: { command: `curl -H "Authorization: ${FAKE_PAT}" https://example.com` } }],
              },
            },
            { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "tool-1", content: `aws key ${FAKE_AWS_KEY}` }] } },
            { type: "result", num_turns: 1, duration_ms: 1000 },
          ],
        });
        await runLogParser({ parseLog: mockParseLog, parserName: "TestParser" });
        const summaryCall = mockCore.summary.addRaw.mock.calls[0];
        expect(summaryCall).toBeDefined();
        expect(summaryCall[0]).not.toContain(FAKE_PAT);
        expect(summaryCall[0]).not.toContain(FAKE_AWS_KEY);
        expect(summaryCall[0]).toContain("***REDACTED***");
      } finally {
        fs.unlinkSync(logFile);
        fs.rmdirSync(tmpDir);
      }
    });
  });
});
