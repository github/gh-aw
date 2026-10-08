import { afterEach, describe, it, expect, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { buildCopilotDiagnosticHooks } = require("./copilot_sdk_diagnostics.cjs");
const piDiagnosticsExtension = require("./pi_diagnostics_extension.cjs");
const { diagnoseToolResult } = require("./command_diagnostics_tools.cjs");
const { renderDiagnostics } = require("./command_diagnostics_render.cjs");
const { transformPiV3Entries } = require("./pi_session.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");
const { parseCopilotSDKToolConfig } = require("./copilot_sdk_tool_config.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const { generateCopilotCliStyleSummary, formatToolUse } = require("./log_parser_shared.cjs");

afterEach(() => vi.unstubAllEnvs());
const fixtures = [
  { language: "go", command: "go test ./...", output: "main_test.go:4: expected 2, got 1" },
  { language: "typescript", command: "npx tsc --noEmit", output: "src/main.ts(4,2): error TS2322: Wrong type" },
  { language: "python", command: "python main.py", output: 'Traceback (most recent call last):\n  File "/repo/main.py", line 4\nValueError: invalid value' },
];

describe("SDK-agnostic diagnostic engine parity", () => {
  it.each(fixtures)("delivers $language diagnostics to both agents and preserves Pi payloads", fixture => {
    const languages = [fixture.language];
    const context = { root: "/repo", cwd: "/repo" };
    const report = diagnoseToolResult("bash", { command: fixture.command }, fixture.output, languages, context);
    const hooks = buildCopilotDiagnosticHooks(languages, context);
    const success = { resultType: "success", textResultForLlm: fixture.output };
    const sdkResult = hooks.onPostToolUse({ toolName: "bash", toolArgs: { command: fixture.command }, toolResult: success, workingDirectory: "/repo" });
    expect(sdkResult).toEqual({ additionalContext: renderDiagnostics(report) });
    expect(success.resultType).toBe("success");
    const failure = hooks.onPostToolUseFailure({ toolName: "bash", toolArgs: { command: fixture.command }, error: fixture.output, workingDirectory: "/repo" });
    expect(failure).toEqual({ additionalContext: sdkResult.additionalContext });
    expect(failure).not.toHaveProperty("modifiedResult");

    vi.stubEnv("GITHUB_WORKSPACE", "/repo");
    vi.stubEnv("GH_AW_DIAGNOSTICS", JSON.stringify(languages));
    const handlers = {};
    piDiagnosticsExtension({
      on: (event, handler) => {
        handlers[event] = handler;
      },
    });
    const content = [
      { type: "text", text: fixture.output },
      { type: "image", data: "image-data", mimeType: "image/png" },
    ];
    const structuredContent = { exitCode: 1 };
    const event = { toolName: "bash", input: { command: fixture.command }, content, details: { exitCode: 1 }, structuredContent, isError: true };
    const piResult = handlers.tool_result(event, { cwd: "/repo" });
    expect(piResult.content.slice(0, 2)).toEqual(content);
    expect(piResult.content.at(-1).text).toBe(sdkResult.additionalContext);
    expect(piResult.details).toEqual({ exitCode: 1, diagnostics: report });
    expect(piResult.structuredContent).toBe(structuredContent);
    expect(piResult).not.toHaveProperty("isError");
    expect(event.isError).toBe(true);

    const events = transformPiV3Entries([
      { type: "tool_execution_end", toolCallId: "call-1", toolName: "bash", isError: true, result: piResult },
      { type: "message_end", message: { role: "toolResult", toolCallId: "call-1", toolName: "bash", content: piResult.content, details: piResult.details, isError: true } },
    ]);
    expect(events.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    expect(events[0].data).toMatchObject({ success: false, diagnostics: report });
    expect(normalizeUnifiedSessionEvent(events[0]).data.diagnostics).toEqual(report);
  });

  it("does nothing when omitted, for non-shell tools, or for unrelated output", () => {
    const on = vi.fn();
    vi.stubEnv("GH_AW_DIAGNOSTICS", "");
    piDiagnosticsExtension({ on });
    expect(on).not.toHaveBeenCalled();
    expect(diagnoseToolResult("view", {}, "main.go:1: bad", ["go"], { root: "/repo" })).toBeUndefined();
    expect(buildCopilotDiagnosticHooks(["go"], {}).onPostToolUseFailure({ toolName: "bash", toolArgs: {}, error: "permission denied", workingDirectory: "/repo" })).toBeUndefined();
  });

  it("rejects invalid compiler-owned SDK diagnostic selections", () => {
    const base = { version: 1, capabilities: { bash: false, edit: false, webFetch: false, webSearch: false, mcp: false, cliProxy: false }, permissions: { allowedTools: ["read"] } };
    expect(parseCopilotSDKToolConfig(JSON.stringify({ ...base, diagnostics: ["go", "python"] })).diagnostics).toEqual(["go", "python"]);
    expect(() => parseCopilotSDKToolConfig(JSON.stringify({ ...base, diagnostics: ["external"] }))).toThrow();
  });

  it("retains and safely renders reports in tool details and canonical/unified summaries", () => {
    const report = diagnoseToolResult("bash", {}, "main.go:4: <script>::error::bad", ["go"], { root: "/repo" });
    const events = [
      { type: "tool.execution_start", data: { toolCallId: "go-1", toolName: "bash", input: { command: "go test" } } },
      { type: "tool.execution_complete", data: { toolCallId: "go-1", toolName: "bash", success: false, diagnostics: report } },
    ];
    const unified = [
      { type: "session.format", data: { version: 1 }, provenance: { component: "collector", phase: "conclusion", path: "collector", index: 0 } },
      ...events.map((event, index) => ({ ...event, provenance: { component: "agent", phase: "agent", path: "events.jsonl", index } })),
    ];
    const details = formatToolUse({ name: "Bash", input: { command: "go test" } }, { is_error: true, diagnostics: report });
    for (const text of [details, generateCopilotCliStyleSummary(events), generateCopilotCliStyleSummary(unified)]) {
      expect(text).toContain("main.go:4");
      expect(text).toContain("Command diagnostics");
      expect(text).not.toContain("<script>");
      expect(text).not.toContain("::error::");
    }
    expect(normalizeUnifiedSessionEvent(events[1]).data.diagnostics).toEqual(report);
  });

  it("wires both Copilot hooks alongside the budget hook and persists error-only diagnostics", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-diagnostics-"));
    vi.stubEnv("GITHUB_WORKSPACE", "/repo");
    const stderr = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    let configuration;
    let handler;
    try {
      const session = {
        sessionId: "diagnostic-session",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          const guidance = configuration.hooks.onPostToolUseFailure({ toolName: "bash", toolArgs: { command: "go test ./..." }, error: "main.go:2: missing", workingDirectory: "/repo" });
          expect(guidance.additionalContext).toContain("main.go:2");
          handler({ type: "tool.execution_start", data: { toolCallId: "go-1", toolName: "bash", input: { command: "go test ./..." } } });
          handler({ type: "tool.execution_complete", data: { toolCallId: "go-1", success: false, error: { message: "main.go:2: missing" } } });
          handler({ type: "assistant.message", data: { content: "Fixing main.go:2" } });
        },
        disconnect: async () => {},
      };
      class Client {
        async start() {}
        async stop() {}
        async createSession(config) {
          configuration = config;
          return session;
        }
      }
      class ToolSet {
        addBuiltIn() {}
        addMcp() {}
      }
      await runWithCopilotSDK({
        sdkUri: "http://localhost:1234",
        prompt: "repair",
        logger: () => {},
        sessionStateBaseDir: directory,
        toolConfig: { version: 1, capabilities: { bash: true }, permissions: { allowedTools: ["read", "shell"] }, explicitlyDisabledTools: [], maxToolCalls: 5, diagnostics: ["go"] },
        sdkModule: { CopilotClient: Client, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow", ToolSet, BuiltInTools: { Isolated: [] } },
      });
      expect(configuration.hooks.onPreToolUse).toBeTypeOf("function");
      expect(configuration.hooks.onPostToolUse).toBeTypeOf("function");
      const events = fs
        .readFileSync(path.join(directory, session.sessionId, "events.jsonl"), "utf8")
        .trim()
        .split("\n")
        .map(JSON.parse);
      expect(events.find(event => event.type === "tool.execution_complete").data).toMatchObject({
        toolCallId: "go-1",
        success: false,
        error: { message: "main.go:2: missing" },
        diagnostics: { version: 1, diagnostics: [{ location: { path: "main.go", line: 2 } }] },
      });
    } finally {
      stderr.mockRestore();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});
