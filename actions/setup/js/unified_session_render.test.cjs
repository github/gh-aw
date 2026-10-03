import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { generatePlainTextSummary, generateCopilotCliStyleSummary, generateConversationMarkdown, formatToolUse, formatInitializationSummary, MAX_STEP_SUMMARY_SIZE } from "./log_parser_shared.cjs";
import { publishUnifiedSessionSummary, validateSessionFileHeader } from "./unified_session_render.cjs";
import { main } from "./unified_session.cjs";

function event(type, data, component, index, timestampMs, sourcePath = `${component}.jsonl`) {
  return { type, data, provenance: { component, phase: component === "agent" ? "agent" : "conclusion", path: sourcePath, index, ...(timestampMs !== undefined ? { timestampMs } : {}) } };
}
const header = event("session.format", { version: 1 }, "collector", 0);
const trace = [
  header,
  event("tool.execution_start", { toolName: "lookup", toolCallId: "shared" }, "agent", 0, 1, "session-a.jsonl"),
  event("tool.execution_start", { toolName: "lookup", toolCallId: "shared" }, "agent", 0, 2, "session-b.jsonl"),
  event("mcp.rpc.request", { server_id: "github", method: "tools/call", payload: { id: 0, params: { name: "list_issues", arguments: { prompt: "PRIVATE_RPC_ARGUMENT" } } } }, "mcp", 0, 3),
  event("firewall.http_access", { host: "example.com", method: "CONNECT", status: 403, decision: "TCP_DENIED" }, "firewall", 0, 4),
  event("tool.execution_complete", { toolCallId: "shared", success: false, output: "failed" }, "agent", 1, 5, "session-a.jsonl"),
  event("tool.execution_complete", { toolCallId: "shared", success: true, output: "done" }, "agent", 1, 6, "session-b.jsonl"),
  event("user.message", { content: "PRIVATE_USER_PROMPT" }, "agent", 2, 7, "session-a.jsonl"),
  event("assistant.message", { content: "Done <details>.\n" }, "agent", 3, 8, "session-a.jsonl"),
  event("session.result", { numTurns: 1, usage: { input_tokens: 0, output_tokens: 2 } }, "agent", 4, 9, "session-a.jsonl"),
  event("session.result", { numTurns: 2, usage: { input_tokens: 10, output_tokens: 3 } }, "agent", 2, 10, "session-b.jsonl"),
  event("mcp.rpc.response", { server_id: "github", payload: { id: 0, result: { content: "PRIVATE_RPC_RESPONSE" } } }, "mcp", 1, 11),
  event("mcp.difc.filtered", { tool_name: "lookup", reason: "policy" }, "mcp", 2, 12),
  event("mcp.guard.blocked", { server_name: "github", reason: "guard" }, "mcp", 3, 13),
  event("mcp.tool_call", { tool_name: "get_issue", status: "failed", duration: 0 }, "mcp", 4, 14),
  event("mcp.event", { event: "future", payload: "PRIVATE_EXTENSION" }, "mcp", 5, 15),
  event("firewall.token_usage", { input_tokens: 999, output_tokens: 5, ai_credits_total: 0 }, "firewall", 1, 16),
  event("firewall.steering", { eventName: "token_steering", message: "budget warning" }, "firewall", 2, 17),
  event("firewall.event", { event: "TRACK_END", status: 200, body: "PRIVATE_EXTENSION" }, "firewall", 3, 18),
  event("safe_output.request", { type: "create_issue", title: "PRIVATE_REQUEST_TITLE" }, "safe_output", 0, 19),
  event("safe_output.result", { type: "create_issue", number: 1, repo: "example/repo" }, "safe_output", 1, 20),
  event("safe_output.error", { errors: [{ error: "PRIVATE_ERROR_CONTEXT" }] }, "safe_output", 2, 21),
  event("experiment.assignment", { experiment: "B" }, "experiment", 0, 22),
  event("experiment.state", { run_id: "1", assignments: { experiment: "B" } }, "experiment", 1, 23),
  event("grader.manifest", { graders: [{ id: "quality", script: "PRIVATE_EVALUATOR" }] }, "grader", 0),
  event("grader.result", { results: [{ id: "quality", value: 0, unit: "ratio", passed: false, message: "PRIVATE_GRADER_MESSAGE" }] }, "grader", 1),
  event("eval.result", { id: "builds", answer: "no", question: "PRIVATE_EVAL_QUESTION" }, "eval", 0),
  event("usage.report", { input_tokens: 999, output_tokens: 50 }, "usage", 0),
  event("execution.result", { outcome: "failure", duration_ms: 0 }, "execution", 0),
  event("detection.result", { conclusion: "failure", secret_leak: false, reasons: "PRIVATE_DETECTION_REASON" }, "detection", 0),
  event("workflow.info", { engine_id: "copilot", model: "fixture", run_id: 1 }, "workflow", 0),
  event("session.runtime", { engine: "copilot", engineVersion: "1.0.90", sandboxRuntime: "cloud-hypervisor" }, "workflow", 1),
  event("workflow.aw_info", { engine_id: "copilot", model: "fixture", workflow_name: "fixture-workflow", run_id: 1, context: { sensitive: "PRIVATE_RAW_METADATA" } }, "workflow", 2),
  event(
    "session.sandbox",
    { runtime: "cloud-hypervisor", firewallEnabled: true, firewallType: "squid", firewallVersion: "v0.30.1", mcpGatewayVersion: "v1.0.0", allowedDomains: ["example.com"], extra: "PRIVATE_SANDBOX_METADATA" },
    "workflow",
    3
  ),
  event("vendor.progress", { private: "PRIVATE_EXTENSION" }, "agent", 5, undefined, "session-a.jsonl"),
  event("session.collection_warning", { path: "gateway.jsonl", line: 2, code: "malformed_jsonl" }, "collector", 1),
  event("session.collection", { sources: [], warnings: 1, untimedEvents: 10, absentComponents: [] }, "collector", 2),
];

describe("unified session publication views", () => {
  let previousEnv;
  let previousCore;
  let directory;
  beforeEach(() => {
    previousEnv = { ...process.env };
    previousCore = global.core;
    global.core = { info: vi.fn(), warning: vi.fn() };
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-unified-render-"));
  });
  afterEach(() => {
    process.env = previousEnv;
    global.core = previousCore;
    vi.restoreAllMocks();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it("renders every known message in both sinks without dumping prompts or opaque payloads", () => {
    const original = structuredClone(trace);
    for (const output of [generatePlainTextSummary(trace), generateCopilotCliStyleSummary(trace)]) {
      expect(output).toContain("File format version: 1");
      for (const type of new Set(trace.map(record => record.type))) {
        if (type !== "user.message") expect(output).toContain(type);
      }
      expect(output).toContain("list_issues");
      expect(output).toContain("rpcId=0");
      expect(output).toContain("TCP_DENIED");
      expect(output).toContain("budget warning");
      expect(output).toContain("[requested, not executed]");
      expect(output).toContain("[execution recorded]");
      expect(output).toContain("value=0 unit=ratio passed=false");
      expect(output).toContain("answer=no");
      expect(output).toContain("durationMs=0");
      expect(output).toContain("secretLeak=false");
      expect(output).toContain("engine=copilot engineVersion=1.0.90 sandboxRuntime=cloud-hypervisor");
      expect(output).toContain("engine_id=copilot model=fixture workflow_name=fixture-workflow run_id=1");
      expect(output).toContain("runtime=cloud-hypervisor firewallEnabled=true firewallType=squid firewallVersion=v0.30.1 mcpGatewayVersion=v1.0.0 allowedDomains=");
      expect(output).toMatch(/allowedDomains=\[\s*"example.com"\s*\]/);
      expect(output).toContain("untimedEvents=10");
      expect(output).toContain("malformed_jsonl");
      expect(output).not.toContain("PRIVATE_");
      expect(output.indexOf("mcp.rpc.request")).toBeLessThan(output.indexOf("firewall.http_access"));
      expect(output.indexOf("firewall.http_access")).toBeLessThan(output.indexOf("mcp.rpc.response"));
      expect(output).toContain("untimed");
    }
    expect(trace).toEqual(original);
    const markdown = generateCopilotCliStyleSummary(trace);
    expect(markdown).toContain("Done &lt;details&gt;");
    expect(markdown).toContain("<details><summary>Unified trace details</summary>");
  });

  it("renders explicit disabled sandboxing and an empty domain list in both publication sinks", () => {
    const sandbox = event("session.sandbox", { runtime: "none", firewallEnabled: false, allowedDomains: [] }, "workflow", 0);
    for (const output of [generatePlainTextSummary([header, sandbox]), generateCopilotCliStyleSummary([header, sandbox])]) {
      expect(output).toContain("session.sandbox runtime=none firewallEnabled=false allowedDomains=[]");
    }
  });

  it("scopes agent pairing, snapshots and accounting without adding firewall usage", () => {
    const output = generatePlainTextSummary(trace);
    const first = output.slice(output.indexOf("Agent source: agent/session-a.jsonl"), output.indexOf("Agent source: agent/session-b.jsonl"));
    const second = output.slice(output.indexOf("Agent source: agent/session-b.jsonl"), output.indexOf("Chronological trace"));
    expect(first).toContain("Tools: 0/1 succeeded");
    expect(first).toContain("Failed Tools: 1");
    expect(first).toContain("Tokens: 2 total (0 in / 2 out)");
    expect(second).toContain("Tools: 1/1 succeeded");
    expect(second).toContain("Tokens: 13 total (10 in / 3 out)");
    expect(first + second).not.toContain("999");
  });

  it("retains private pairing keys until projection so redaction cannot merge distinct tool calls", () => {
    process.env.GH_AW_SECRET_NAMES = "FIRST_ID,SECOND_ID";
    process.env.SECRET_FIRST_ID = "secret-first-tool";
    process.env.SECRET_SECOND_ID = "secret-second-tool";
    const events = [
      header,
      event("tool.execution_start", { toolName: "first", toolCallId: "secret-first-tool" }, "agent", 0, 0),
      event("tool.execution_start", { toolName: "second", toolCallId: "secret-second-tool" }, "agent", 1, 1),
      event("tool.execution_complete", { toolCallId: "secret-first-tool", success: false }, "agent", 2, 2),
      event("tool.execution_complete", { toolCallId: "secret-second-tool", success: true }, "agent", 3, 3),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("Tools: 1/2 succeeded");
      expect(output).toContain("Failed Tools: 1");
      expect(output).not.toContain("secret-first-tool");
      expect(output).not.toContain("secret-second-tool");
    }
    const projection = generateConversationMarkdown(events, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary });
    expect(projection.commandSummary[0]).toContain("❌");
    expect(projection.commandSummary[1]).toContain("✅");
  });

  it("uses the unified view through the conversation formatter without breaking source tool summaries", () => {
    const result = generateConversationMarkdown(trace, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary });
    expect(result.markdown).toContain("firewall.http_access");
    expect(result.commandSummary).toHaveLength(2);
    expect(result.sizeLimitReached).toBe(false);
    expect(result.markdown).not.toContain("PRIVATE_");
  });

  it("rejects unsupported, missing or misplaced version headers without assuming compatibility", () => {
    expect(() => validateSessionFileHeader([])).toThrow("missing");
    for (const version of [0, "1", 2, undefined]) {
      expect(() => generatePlainTextSummary([{ ...header, data: { version } }])).toThrow("Unsupported");
    }
    expect(() => generateCopilotCliStyleSummary([trace[1], header])).toThrow("missing");
    expect(() => generatePlainTextSummary([header, header])).toThrow("multiple");
  });

  it("redacts escaped secret leaves before shortening previews and neutralizes hostile markup/fences", () => {
    const secret = 'secret"\\\n' + "b".repeat(200);
    const token = "ghp_" + "a".repeat(36);
    process.env.GH_AW_SECRET_NAMES = "RENDER_KEY";
    process.env.SECRET_RENDER_KEY = secret;
    const events = [header, event("assistant.message", { content: `${secret} ${token}\n\`\`\`\`\`\`\`\`\`\n</details>\n::warning::untrusted` }, "agent", 0, 0)];
    const original = structuredClone(events);
    const plain = generatePlainTextSummary(events);
    const markdown = generateCopilotCliStyleSummary(events);
    for (const output of [plain, markdown]) {
      expect(output).not.toContain(token);
      expect(output).not.toContain(secret.slice(0, 40));
      expect(output).toContain("***REDACTED***");
      expect(output).not.toMatch(/^::warning::/m);
    }
    expect(markdown).toContain("&lt;/details&gt;");
    expect(markdown).toContain("\n``````````\n");
    expect(plain).not.toContain("::warning::");
    expect(events).toEqual(original);
  });

  it("clips oversized views with explicit notices without trimming the source artifact", () => {
    const events = [header, ...Array.from({ length: 2000 }, (_, index) => event("assistant.message", { content: "a".repeat(2000) }, "agent", index, index))];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(Buffer.byteLength(output)).toBeLessThanOrEqual(MAX_STEP_SUMMARY_SIZE);
      expect(output).toContain("summary truncated: byte limit reached");
    }
    expect(events.at(-1).data.content).toHaveLength(2000);
  });

  it("publishes the final redacted file into logs and appends to the existing step summary", async () => {
    const summary = path.join(directory, "summary.md");
    fs.writeFileSync(summary, "Earlier summary\n");
    process.env.GITHUB_STEP_SUMMARY = summary;
    fs.writeFileSync(path.join(directory, "aw_info.json"), JSON.stringify({ engine_id: "copilot", agent_version: "1.0.90", agent_runtime: "docker" }));
    fs.writeFileSync(path.join(directory, "agent-session.jsonl"), JSON.stringify({ type: "assistant.message", data: { content: "done" } }) + "\n");
    fs.mkdirSync(path.join(directory, "mcp-logs"));
    fs.writeFileSync(path.join(directory, "mcp-logs", "gateway.jsonl"), JSON.stringify({ event: "tool_call", tool_name: "lookup" }) + "\n");
    await main({ rootDir: directory });
    const artifact = fs.readFileSync(path.join(directory, "usage", "aw_session.jsonl"), "utf8");
    expect(JSON.parse(artifact.split("\n")[0]).data.version).toBe(1);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("mcp.tool_call"));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("engine=copilot engineVersion=1.0.90 sandboxRuntime=docker"));
    const output = fs.readFileSync(summary, "utf8");
    expect(output.startsWith("Earlier summary\n")).toBe(true);
    expect(output).toContain("### Unified session");
    expect(output).toContain("mcp.tool_call");
    expect(output).toContain("engine=copilot engineVersion=1.0.90 sandboxRuntime=docker");
    expect(output).toContain("workflow.aw_info");
    expect(output).toContain("session.sandbox runtime=docker");
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("session.sandbox runtime=docker"));
  });

  it("respects the remaining byte budget of an existing step summary and still publishes logs", async () => {
    const source = path.join(directory, "aw_session.jsonl");
    fs.writeFileSync(source, trace.map(record => JSON.stringify(record)).join("\n") + "\n");
    const summary = path.join(directory, "summary.md");
    const existing = "x".repeat(MAX_STEP_SUMMARY_SIZE - 10);
    fs.writeFileSync(summary, existing);
    process.env.GITHUB_STEP_SUMMARY = summary;
    await publishUnifiedSessionSummary(source);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("firewall.http_access"));
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("byte limit"));
    expect(fs.readFileSync(summary, "utf8")).toBe(existing);
  });

  it("reports publication I/O failures while retaining the valid unified artifact", async () => {
    const source = path.join(directory, "aw_session.jsonl");
    fs.writeFileSync(source, JSON.stringify(header) + "\n");
    process.env.GITHUB_STEP_SUMMARY = path.join(directory, "absent", "summary.md");
    await expect(publishUnifiedSessionSummary(source)).rejects.toThrow("Failed to publish unified session");
    expect(fs.readFileSync(source, "utf8")).toBe(JSON.stringify(header) + "\n");
  });
});
