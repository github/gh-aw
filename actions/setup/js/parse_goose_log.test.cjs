import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseGooseLog } from "./parse_goose_log.cjs";
import { parseEngineSession, collectUnifiedSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";
import { serializeSessionArtifact } from "./session_artifact.cjs";

const jsonl = events => events.map(event => JSON.stringify(event)).join("\n");
const message = (id, role, content) => ({ type: "message", message: { id, role, created: 1791174000, content, metadata: { inference: { requestedModel: "gpt-5.4" } } } });
const request = { type: "toolRequest", id: "call-1", toolCall: { status: "success", value: { name: "github__pull_request_read", arguments: { method: "get", pullNumber: 42 } } } };
const response = { type: "toolResponse", id: "call-1", toolResult: { status: "success", value: { content: [{ type: "text", text: "PR 42" }] } } };
const complete = { type: "complete", input_tokens: 100, output_tokens: 20, total_tokens: 120, cache_read_input_tokens: 30, cache_write_input_tokens: 4, cost_usd: 0.01 };
const fixture = [
  message("assistant-1", "assistant", [{ type: "thinking", thinking: "Check the PR." }, request]),
  message("user-1", "user", [response]),
  message("assistant-2", "assistant", [{ type: "text", text: "Smoke " }]),
  message("assistant-2", "assistant", [{ type: "text", text: "passed.\n" }]),
  complete,
];
const roots = [];
afterEach(() => {
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("Goose unified session parser", () => {
  it("preserves source identities, timestamps, reasoning, tool pairs, streaming text, and cumulative usage", () => {
    const result = parseGooseLog(`[INFO] infrastructure\n${jsonl(fixture)}`);
    expect(result.logEntries.every(event => event.type.includes(".") && event.data)).toBe(true);
    expect(result.logEntries.find(event => event.type === "session.init")).toMatchObject({
      id: "assistant-1",
      timestamp: "2026-10-05T04:20:00.000Z",
      data: { sourceEngine: "goose", model: "gpt-5.4" },
    });
    expect(result.logEntries.find(event => event.type === "assistant.reasoning").data.content).toBe("Check the PR.");
    expect(result.logEntries.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(result.logEntries.find(event => event.type === "assistant.message")).toMatchObject({ id: "assistant-2", data: { content: "Smoke passed.\n" } });
    expect(result.logEntries.find(event => event.type === "tool.execution_start").data).toEqual({ toolCallId: "call-1", toolName: "github__pull_request_read", input: { method: "get", pullNumber: 42 } });
    expect(result.logEntries.find(event => event.type === "tool.execution_complete").data).toMatchObject({ toolCallId: "call-1", toolName: "github__pull_request_read", success: true, output: [{ type: "text", text: "PR 42" }] });
    expect(result.logEntries.at(-1)).toMatchObject({
      type: "session.result",
      data: {
        status: "completed",
        numTurns: 2,
        totalCostUsd: 0.01,
        usage: { input_tokens: 100, output_tokens: 20, total_tokens: 120, cache_read_input_tokens: 30, cache_creation_input_tokens: 4, input_tokens_include_cache: true },
      },
    });
    expect(result.markdown).toContain("Smoke passed.");
    expect(result.maxTurnsHit).toBe(false);
    expect(parseEngineSession(jsonl(fixture), "goose")).toEqual(result.logEntries);
    expect(parseCustomLog(jsonl(fixture)).logEntries).toEqual(result.logEntries);
    expect(parseGooseLog(jsonl(result.logEntries)).logEntries).toEqual(result.logEntries);
  });

  it("deduplicates tool observations and never sums cumulative usage snapshots", () => {
    const result = parseGooseLog(jsonl([fixture[0], fixture[0], fixture[1], fixture[1], complete, complete]));
    expect(result.logEntries.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(result.logEntries.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    expect(result.logEntries.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(result.logEntries.at(-1).data.usage.input_tokens).toBe(100);
  });

  it("does not invent results, completed tools, or zero usage for partial or unsupported logs", () => {
    const partial = parseGooseLog(jsonl([fixture[0]]));
    expect(partial.logEntries.some(event => event.type === "session.result" || event.type === "tool.execution_complete")).toBe(false);
    const empty = parseGooseLog("PRIVATE_PROMPT mentioning max-turns");
    expect(empty.logEntries).toEqual([]);
    expect(empty.markdown).not.toContain("PRIVATE_PROMPT");
    expect(empty.maxTurnsHit).toBe(false);
  });

  it("preserves failed tool results and terminal errors without successful completion", () => {
    const failed = { ...response, toolResult: { status: "error", error: "MCP rejected the call" } };
    const result = parseGooseLog(jsonl([fixture[0], message("user-1", "user", [failed]), { type: "error", error: "Maximum turns reached" }]));
    expect(result.logEntries.find(event => event.type === "tool.execution_complete").data).toMatchObject({ success: false, error: "MCP rejected the call", toolName: "github__pull_request_read" });
    expect(result.logEntries.at(-1).data).toEqual({ status: "error", errors: ["Maximum turns reached"], numTurns: 1 });
    expect(result.logEntries.at(-1).data).not.toHaveProperty("usage");
    expect(result.maxTurnsHit).toBe(true);
    expect(parseGooseLog(jsonl(result.logEntries)).maxTurnsHit).toBe(true);
  });

  it("records malformed JSON and explicit MCP startup failures without publishing raw diagnostics", () => {
    const result = parseGooseLog(`{PRIVATE_SECRET\nWarning: Failed to start extension 'github' (connection error)\n${jsonl(fixture)}`);
    expect(result.logEntries.find(event => event.type === "session.collection_warning").data).toEqual({ code: "malformed_jsonl", line: 1 });
    expect(result.mcpFailures).toEqual(["github"]);
    expect(result.markdown).not.toContain("PRIVATE_SECRET");
  });

  it("retains zero metrics and omits invalid or missing metrics", () => {
    const result = parseGooseLog(jsonl([{ ...complete, input_tokens: 0, output_tokens: -1, total_tokens: null, cache_read_input_tokens: "30", cache_write_input_tokens: 0, cost_usd: 0 }]));
    expect(result.logEntries.at(-1).data).toEqual({ status: "completed", totalCostUsd: 0, usage: { input_tokens: 0, cache_creation_input_tokens: 0, input_tokens_include_cache: true } });
    expect(result.logEntries.at(-1).data).not.toHaveProperty("numTurns");
  });

  it("reports malformed content blocks and tool requests while retaining supported messages", () => {
    const result = parseGooseLog(jsonl([message("assistant-1", "assistant", [null, { type: "toolRequest", id: "bad", toolCall: { status: "success" } }, { type: "text", text: "Retained." }])]));
    expect(result.logEntries.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toEqual(["malformed_message_content", "malformed_tool_request"]);
    expect(result.logEntries.find(event => event.type === "assistant.message").data.content).toBe("Retained.");
    expect(result.logEntries.some(event => event.type === "session.result")).toBe(false);
  });

  it.each([{ status: "success" }, { status: "success", value: { content: [], isError: "false" } }, { status: "error" }])("does not invent a successful tool completion for malformed result %j", toolResult => {
    const result = parseGooseLog(jsonl([fixture[0], message("user-1", "user", [{ ...response, toolResult }])]));
    expect(result.logEntries.some(event => event.type === "tool.execution_complete")).toBe(false);
    expect(result.logEntries.find(event => event.type === "session.collection_warning").data).toEqual({ code: "malformed_tool_response", toolCallId: "call-1" });
  });

  it("retains successful structured-only MCP results without inventing text", () => {
    const toolResult = { status: "success", value: { structuredContent: { answer: 42 } } };
    const result = parseGooseLog(jsonl([fixture[0], message("user-1", "user", [{ ...response, toolResult }])]));
    expect(result.logEntries.find(event => event.type === "tool.execution_complete").data).toMatchObject({ success: true, output: { answer: 42 } });
  });

  it.each([false, true])("collects Goose session events with canonical artifact present=%s", canonical => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-goose-session-"));
    roots.push(root);
    fs.writeFileSync(path.join(root, "agent-stdio.log"), jsonl(fixture));
    if (canonical) fs.writeFileSync(path.join(root, "agent-session.jsonl"), serializeSessionArtifact(parseGooseLog(jsonl(fixture)).logEntries));
    const warnings = [];
    const collected = collectUnifiedSession({ rootDir: root, engine: "goose", warn: warning => warnings.push(warning) });
    expect(warnings).toEqual([]);
    const agent = collected.events.filter(event => event.provenance.component === "agent");
    expect(agent.some(event => event.type === "assistant.message" && event.data.content === "Smoke passed.\n")).toBe(true);
    expect(agent.some(event => event.type === "tool.execution_complete" && event.data.success === true)).toBe(true);
    expect(agent.some(event => event.type === "session.result" && event.data.usage.inputTokens === 100)).toBe(true);
    expect(agent.every(event => event.provenance.path === (canonical ? "agent-session.jsonl" : "agent-stdio.log"))).toBe(true);
  });
});
