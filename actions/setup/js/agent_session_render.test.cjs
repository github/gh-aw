import { describe, it, expect } from "vitest";
import { generateConversationMarkdown, generatePlainTextSummary, generateCopilotCliStyleSummary, formatToolUse, formatInitializationSummary, convertCopilotEventsToLegacyLogEntries, MAX_STEP_SUMMARY_SIZE } from "./log_parser_shared.cjs";
import { redactSessionForPublication } from "./agent_session_render.cjs";

const options = { formatToolCallback: formatToolUse, formatInitCallback: init => formatInitializationSummary(init, { includeSlashCommands: true }) };

function freeze(value) {
  if (value && typeof value === "object") {
    Object.freeze(value);
    for (const child of Object.values(value)) freeze(child);
  }
  return value;
}

const trace = freeze([
  {
    type: "session.init",
    data: {
      sourceEngine: "fixture",
      model: "first-model",
      sessionId: "source-session",
      cwd: "/fixture",
      tools: ["Read", { name: "lookup" }],
      mcpServers: [{ name: "catalog", status: "connected" }],
      slashCommands: ["/fixture"],
      modelInfo: { billing: { is_premium: false, multiplier: 0 } },
    },
  },
  { type: "session.init", data: { model: "latest-model" } },
  { type: "user.message", data: { content: "PRIVATE_PROMPT" } },
  { type: "assistant.reasoning", data: { content: { text: "reason <tag>" } } },
  { type: "tool.execution_start", data: { toolCallId: "read", toolName: "Read", input: { path: "/fixture/file" } } },
  { type: "tool.execution_complete", data: { toolCallId: "read", success: false, output: false, error: { code: "DENIED", message: "permission denied" }, durationMs: 0 } },
  { type: "tool.execution_start", data: { toolCallId: "todo", toolName: "TodoWrite", input: 0 } },
  { type: "tool.execution_start", data: { toolCallId: "mcp", toolName: "bash", mcpServerName: "catalog", input: null } },
  { type: "tool.execution_complete", data: { toolCallId: "mcp", output: 0 } },
  { type: "tool.execution_complete", data: { toolCallId: "orphan", toolName: "lookup", success: true, output: "" } },
  { type: "assistant.message", data: { content: "Done <details>.\n" } },
  {
    type: "session.result",
    data: {
      numTurns: 0,
      durationMs: 0,
      totalCostUsd: 0,
      usage: { input_tokens: 0, outputTokens: 2, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
      errors: [{ code: "PROVIDER", message: "provider <failure>" }],
      permissionDenials: [{ toolName: "Read", permission: "read" }],
    },
  },
  { type: "vendor.extension", data: { content: "PRIVATE_PROMPT" } },
]);

describe("standard agent trace rendering", () => {
  it("T-UAS-009/042: both publication views merge partial initialization without inventing inventories", () => {
    const original = JSON.stringify(trace);
    for (const output of [generatePlainTextSummary(trace), generateCopilotCliStyleSummary(trace)]) {
      expect(output).toContain("Model: latest-model");
      expect(output).toContain("Engine: fixture");
      expect(output).toContain("Session ID: source-session");
      expect(output).toContain("Working Directory: /fixture");
      expect(output).toContain("Available Tools (2): Read, lookup");
      expect(output).toContain("catalog (connected)");
      expect(output).toContain("/fixture");
      expect(output).toContain('"is_premium": false');
      expect(output).toContain('"multiplier": 0');
      expect(output).not.toContain("PRIVATE_PROMPT");
    }
    expect(JSON.stringify(trace)).toBe(original);
    const sparse = generatePlainTextSummary([{ type: "session.init", data: { model: "sparse" } }]);
    expect(sparse).not.toContain("Available Tools");
    expect(sparse).not.toContain("MCP Servers");
  });

  it("T-UAS-014/023/024/044: built-ins, pending calls, unknown outcomes and orphan results stay visible", () => {
    for (const output of [generatePlainTextSummary(trace), generateCopilotCliStyleSummary(trace)]) {
      expect(output).toContain("✗ Read");
      expect(output).toContain("TodoWrite(0) [pending]");
      expect(output).toContain("catalog-bash(null) [outcome unknown]");
      expect(output).toContain("lookup [start unavailable]");
      expect(output).toContain("[empty output]");
      expect(output).toContain("Error:");
      expect(output).toContain("DENIED");
      expect(output).toContain("Tools: 1/4 succeeded");
      expect(output).toContain("Failed Tools: 1");
      expect(output).toContain("Pending Tools: 1");
      expect(output).toContain("Unknown Outcomes: 1");
      expect(output).toContain("[0s]");
    }
  });

  it("T-UAS-015/030/042: all accounting and diagnostics are available in both summary views", () => {
    for (const output of [generatePlainTextSummary(trace), generateCopilotCliStyleSummary(trace)]) {
      expect(output).toContain("Turns: 0");
      expect(output).toContain("Duration: 0s");
      expect(output).toContain("Cost: $0.0000");
      expect(output).toContain("Tokens: 2 total (0 in / 2 out)");
      expect(output).toContain("Cache Read Tokens: 0");
      expect(output).toContain("Cache Creation Tokens: 0");
      expect(output).toContain("PROVIDER");
      expect(output).toContain("Permission Denials: 1");
      expect(output).toContain('"permission": "read"');
    }
  });

  it("T-UAS-042/045/049: formatted summaries cover all core fields and safely display source markup", () => {
    const result = generateConversationMarkdown(trace, options);
    expect(result.markdown).toContain("<summary>Information</summary>");
    expect(result.markdown).toContain("Turns:** 0");
    expect(result.markdown).toContain("Permission Denials:** 1");
    expect(result.markdown).toContain("TodoWrite");
    expect(result.markdown).toContain("**Error:**");
    expect(result.markdown).toContain("false");
    expect(result.markdown).toContain("reason &lt;tag&gt;");
    expect(result.markdown).toContain("Done &lt;details&gt;");
    expect(result.markdown).not.toContain("PRIVATE_PROMPT");
    expect(result.commandSummary).toHaveLength(4);
  });

  it("uses progressive disclosure with outcomes before the detailed trace", () => {
    const output = generateCopilotCliStyleSummary(trace);
    expect(output.startsWith("### Agent session\n")).toBe(true);
    expect(output.indexOf("Tools: 1/4 succeeded")).toBeLessThan(output.indexOf("<details><summary>Trace details</summary>"));
    expect(output).toContain("\n</details>");
    expect(output).not.toContain("<details open>");
  });

  it.each([false, 0, null, [], {}, ""])("T-UAS-005/012/013: typed arguments/output %j remain readable", value => {
    const events = freeze([
      { type: "tool.execution_start", data: { toolCallId: "", toolName: "lookup", input: value } },
      { type: "tool.execution_complete", data: { toolCallId: "", success: true, output: value } },
    ]);
    const expected = value === "" ? "[empty output]" : JSON.stringify(value);
    expect(generatePlainTextSummary(events)).toContain(expected);
    expect(generateCopilotCliStyleSummary(events)).toContain(expected);
    expect(generateConversationMarkdown(events, { ...options, formatToolCallback: (call, result) => formatToolUse(call, result, { includeDetailedParameters: true }) }).markdown).toContain(expected);
    expect(events[0].data.input).toEqual(value);
  });

  it("T-UAS-022: renderer-only generated IDs cannot collide with a supplied native ID", () => {
    const events = [
      { type: "tool.execution_start", data: { toolCallId: "sdk_tool_1", toolName: "first", input: {} } },
      { type: "tool.execution_start", data: { toolName: "second", input: {} } },
      { type: "tool.execution_complete", data: { toolCallId: "sdk_tool_1", success: false, output: "first failed" } },
    ];
    const output = generatePlainTextSummary(events);
    expect(output).toContain("✗ first");
    expect(output).toContain("? second");
    expect(output).toContain("Tools: 0/2 succeeded");
    const projected = convertCopilotEventsToLegacyLogEntries(events);
    expect(projected[0].message.content[0].id).not.toBe(projected[1].message.content[0].id);
  });

  it("T-UAS-043: an explicit scalar input survives a separate supplied command", () => {
    const events = freeze([{ type: "tool.execution_start", data: { toolCallId: "command", toolName: "bash", input: null, command: "echo fixture" } }]);
    expect(convertCopilotEventsToLegacyLogEntries(events)[0].message.content[0].input).toBeNull();
    const text = generatePlainTextSummary(events);
    expect(text).toContain("$ echo fixture");
    expect(text).toContain("Arguments: null");
  });

  it("T-UAS-049: redaction copies preserve matching identities and do not rewrite the input", () => {
    const events = freeze([
      { type: "tool.execution_start", data: { toolCallId: "private-secret", toolName: "lookup", input: { key: "secret-value" } } },
      { type: "tool.execution_complete", data: { toolCallId: "private-secret", success: true, output: { id: "secret-value" } } },
    ]);
    const copy = redactSessionForPublication(events, value => value.replaceAll("secret", "masked"));
    expect(copy[0].data.toolCallId).toBe("private-secret");
    expect(copy[1].data.toolCallId).toBe("private-secret");
    expect(copy[0].data.input.key).toBe("masked-value");
    expect(copy[1].data.output.id).toBe("masked-value");
    expect(events[0].data.input.key).toBe("secret-value");
    expect(generatePlainTextSummary(copy)).toContain("Tools: 1/1 succeeded");
  });

  it("reports invalid non-JSON publication data instead of returning an empty successful view", () => {
    expect(() => redactSessionForPublication([{ type: "vendor.data", data: { value: 1n } }], value => value)).toThrow("Failed to prepare agent session for publication");
  });

  it("T-UAS-049: tool output fences cannot close their generated code container", () => {
    const output = formatToolUse({ name: "lookup", input: {}, standard_trace: true }, { content: "````````\n</details>\nraw output", is_error: false });
    expect(output).toContain("\n`````````\n````````\n</details>");
    expect(output).toContain("\n`````````\n</details>");
  });

  it("T-UAS-050: console and Actions artifacts stay within the byte budget with explicit truncation", () => {
    const events = freeze(Array.from({ length: 1300 }, (_, i) => ({ type: "assistant.message", data: { content: `${i}: ${"a".repeat(1900)}` } })));
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(Buffer.byteLength(output, "utf8")).toBeLessThanOrEqual(MAX_STEP_SUMMARY_SIZE);
      expect(output).toContain("summary truncated: byte limit reached");
    }
    const markdown = generateConversationMarkdown(events, options);
    expect(markdown.sizeLimitReached).toBe(true);
    expect(Buffer.byteLength(markdown.markdown, "utf8")).toBeLessThanOrEqual(MAX_STEP_SUMMARY_SIZE);
    expect(events[1299].data.content).toContain("1299:");
  });
});
