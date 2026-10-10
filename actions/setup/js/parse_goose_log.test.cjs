import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseGooseLog } from "./parse_goose_log.cjs";
import { parseEngineSession, collectUnifiedSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";
import { serializeSessionArtifact } from "./session_artifact.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { success, failedTool, terminalFailure, startupFailure } from "./fixtures/goose_ci_sessions.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

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

  it("maps CI MCP identity and observed shell outcomes into essential unified fields", () => {
    for (const events of [success, failedTool]) {
      const parsed = parseGooseLog(jsonl(events));
      const starts = parsed.logEntries.filter(event => event.type === "tool.execution_start");
      expect(starts.every(event => typeof event.data.mcpServerName === "string")).toBe(true);
      const shell = parsed.logEntries.find(event => event.type === "tool.execution_complete" && event.data.toolName === "shell");
      const native = events.flatMap(event => event.message?.content ?? []).find(item => item.type === "toolResponse" && item.id === shell.data.toolCallId).toolResult.value;
      expect(shell.data).toMatchObject({ mcpServerName: "developer", success: !native.isError, isError: native.isError, structuredContent: native.structuredContent, output: native.content, exitCode: native.structuredContent.exit_code });
      expect(normalizeUnifiedSessionEvent(shell).data).toEqual({
        toolCallId: shell.data.toolCallId,
        toolName: "shell",
        mcpServerName: "developer",
        success: !native.isError,
        isError: native.isError,
        output: native.content,
        exitCode: native.structuredContent.exit_code,
      });
      expect(parsed.logEntries.at(-1).data.status).toBe("completed");
      expect(parsed.logEntries.at(-1).data.totalCostUsd).toBe(0);
      expect(parsed.logEntries.at(-1).data.usage.cache_creation_input_tokens).toBe(0);
      expect(parseGooseLog(jsonl(parsed.logEntries)).logEntries).toEqual(parsed.logEntries);
    }
  });

  it("retains CI terminal failure without fabricating usage or confusing credit limits with turn limits", () => {
    const parsed = parseGooseLog(jsonl(terminalFailure));
    expect(parsed.logEntries.find(event => event.type === "session.error").data.error).toBe(terminalFailure.at(-1).error);
    expect(parsed.logEntries.at(-1).data).toEqual({ status: "error", errors: [terminalFailure.at(-1).error], numTurns: 1 });
    expect(parsed.maxTurnsHit).toBe(false);
  });

  it("uses standard diagnostic events and retains flags on canonical round trips", () => {
    const parsed = parseGooseLog(`${startupFailure}\n${jsonl([{ type: "error", error: "Maximum turns reached" }])}`);
    expect(parsed.logEntries.some(event => event.type.startsWith("goose."))).toBe(false);
    expect(parsed.logEntries.filter(event => event.type === "mcp.event").map(event => event.data)).toEqual([
      { event: "extension_start_failure", serverName: "github", status: "error" },
      { event: "extension_start_failure", serverName: "safeoutputs", status: "error" },
    ]);
    expect(parsed.logEntries.find(event => event.type === "session.error").data).toEqual({ error: "Maximum turns reached" });
    const reparsed = parseGooseLog(jsonl(parsed.logEntries));
    expect(reparsed.logEntries).toEqual(parsed.logEntries);
    expect(reparsed.mcpFailures).toEqual(["github", "safeoutputs"]);
    expect(reparsed.maxTurnsHit).toBe(true);
  });

  it.each([null, false, 0, "", {}, []])("preserves explicit tool arguments %j instead of replacing them with defaults", input => {
    const events = [message("assistant-1", "assistant", [{ ...request, toolCall: { status: "success", value: { name: "tool", arguments: input } } }])];
    expect(parseGooseLog(jsonl(events)).logEntries.find(event => event.type === "tool.execution_start").data.input).toEqual(input);
  });

  it("does not invent arguments for a request without them", () => {
    const events = [message("assistant-1", "assistant", [{ ...request, toolCall: { status: "success", value: { name: "tool" } } }])];
    expect(parseGooseLog(jsonl(events)).logEntries.find(event => event.type === "tool.execution_start").data).not.toHaveProperty("input");
  });

  it("retains explicit envelope metadata rather than overwriting it with message defaults", () => {
    const native = { ...message("message-id", "assistant", [{ type: "text", text: "" }]), id: "envelope-id", timestamp: 0, parentId: null, vendor: { enabled: false } };
    const events = parseGooseLog(jsonl([native])).logEntries;
    expect(events.every(event => event.id === "envelope-id" && event.timestamp === 0 && event.parentId === null)).toBe(true);
    expect(events.find(event => event.type === "assistant.message")).toMatchObject({ vendor: { enabled: false }, data: { content: "" }, message: { id: "message-id" } });
  });

  it("preserves canonical extensions, refusals and their metadata without guessing refusals from text", () => {
    const events = [
      { type: "vendor.progress", data: { enabled: false, count: 0, detail: null }, id: "extension-id", parentId: null, timestamp: 0, extra: [] },
      { type: "assistant.refusal", data: { reason: "content_filter", content: "", partial: false, policyCategory: null } },
    ];
    const parsed = parseGooseLog(jsonl([...events, message("text-id", "assistant", [{ type: "text", text: "I cannot do that." }])]));
    expect(parsed.logEntries.slice(0, 2)).toEqual(events);
    expect(parsed.logEntries.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(parsed.logEntries.find(event => event.type === "assistant.message").data.content).toBe("I cannot do that.");
  });

  it("reconciles cumulative snapshots and retains accounting when a later terminal error is observed", () => {
    const events = [complete, { type: "complete", input_tokens: 110 }, { type: "error", error: "Interrupted" }];
    const result = parseGooseLog(jsonl(events));
    expect(result.logEntries.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(result.logEntries.at(-1).data).toEqual({
      status: "error",
      errors: ["Interrupted"],
      totalCostUsd: 0.01,
      usage: { input_tokens: 110, output_tokens: 20, total_tokens: 120, cache_read_input_tokens: 30, cache_creation_input_tokens: 4, input_tokens_include_cache: true },
    });
  });

  it("does not move text across an intervening reasoning or tool observation", () => {
    const result = parseGooseLog(
      jsonl([
        message("assistant-1", "assistant", [{ type: "text", text: "Before." }, request]),
        message("assistant-1", "assistant", [
          { type: "thinking", thinking: "Check." },
          { type: "text", text: "After." },
        ]),
      ])
    );
    expect(result.logEntries.filter(event => event.type !== "session.init").map(event => [event.type, event.data.content])).toEqual([
      ["assistant.message", "Before."],
      ["tool.execution_start", undefined],
      ["assistant.reasoning", "Check."],
      ["assistant.message", "After."],
    ]);
  });

  it("retains changing streaming metadata instead of combining distinct observations", () => {
    const first = message("assistant-1", "assistant", [{ type: "text", text: "First", annotations: { priority: 0 } }]);
    const second = message("assistant-1", "assistant", [{ type: "text", text: "Second", annotations: { priority: 1 } }]);
    const third = { ...second, message: { ...second.message, metadata: { ...second.message.metadata, agentVisible: false } } };
    const events = parseGooseLog(jsonl([first, second, third])).logEntries.filter(event => event.type === "assistant.message");
    expect(events.map(event => event.data.content)).toEqual(["First", "Second", "Second"]);
    expect(events.map(event => event.message)).toEqual([first.message, second.message, third.message]);
  });

  it("does not fabricate lifecycle events for malformed error requests or lose an observed empty model", () => {
    const native = message("assistant-1", "assistant", [{ ...request, toolCall: { status: "error" } }]);
    native.message.metadata.inference.requestedModel = "";
    const result = parseGooseLog(jsonl([native]));
    expect(result.logEntries.find(event => event.type === "session.init").data.model).toBe("");
    expect(result.logEntries.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toEqual(["malformed_tool_request"]);
    expect(result.logEntries.some(event => event.type.startsWith("tool.") || event.type === "session.error")).toBe(false);
  });

  it("reports missing native correlation IDs rather than silently dropping tool observations", () => {
    const result = parseGooseLog(
      jsonl([
        message("assistant-1", "assistant", [
          { ...request, id: null },
          { ...response, id: 0 },
        ]),
      ])
    );
    expect(result.logEntries.filter(event => event.type === "session.collection_warning").map(event => event.data)).toEqual([{ code: "malformed_tool_request" }, { code: "malformed_tool_response" }]);
    expect(result.logEntries.some(event => event.type.startsWith("tool."))).toBe(false);
  });

  it("retains old canonical Goose diagnostics without emitting new vendor wrappers", () => {
    const events = [
      { type: "goose.max_turns", data: { error: "Maximum turns reached" } },
      { type: "goose.mcp_failure", data: { serverName: "github" } },
    ];
    const result = parseGooseLog(jsonl(events));
    expect(result.logEntries).toEqual(events);
    expect(result.maxTurnsHit).toBe(true);
    expect(result.mcpFailures).toEqual(["github"]);
  });

  it.each([false, true])("preserves CI tool outcomes through canonical serialization and collection with canonical=%s", canonical => {
    for (const fixture of [success, failedTool, terminalFailure]) {
      const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-goose-ci-"));
      roots.push(root);
      const native = jsonl(fixture);
      fs.writeFileSync(path.join(root, "agent-stdio.log"), native);
      const parsed = parseGooseLog(native).logEntries;
      const artifact = serializeSessionArtifact(parsed);
      expect(validateSession(artifact, "agent")).toBe(parsed.length);
      if (canonical) fs.writeFileSync(path.join(root, "agent-session.jsonl"), artifact);
      const collected = collectUnifiedSession({ rootDir: root, engine: "goose" });
      expect(validateSession(serializeSessionArtifact(collected.events), "unified")).toBe(collected.events.length);
      const unified = collected.events.filter(event => event.type === "tool.execution_complete");
      expect(unified.map(event => event.data)).toEqual(parsed.filter(event => event.type === "tool.execution_complete").map(event => normalizeUnifiedSessionEvent(event).data));
      const nativeText = fixture
        .flatMap(event => event.message?.content ?? [])
        .filter(item => item.type === "text")
        .map(item => item.text)
        .join("");
      expect(
        collected.events
          .filter(event => event.type === "assistant.message")
          .map(event => event.data.content)
          .join("")
      ).toBe(nativeText);
      expect(collected.events.find(event => event.type === "session.result").data.status).toBe(fixture === terminalFailure ? "error" : "completed");
    }
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
