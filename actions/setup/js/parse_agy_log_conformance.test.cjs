import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseAgyLog } from "./parse_agy_log.cjs";
import { collectUnifiedSession, parseEngineSession } from "./unified_session.cjs";
import { writeSessionArtifact } from "./session_artifact.cjs";
import { agySmoke } from "./fixtures/agy_ci_sessions.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const jsonl = records => records.map(record => JSON.stringify(record)).join("\n") + "\n";
const roots = [];
afterEach(() => roots.splice(0).forEach(root => fs.rmSync(root, { recursive: true, force: true })));

describe("run-backed AGY canonical session", () => {
  it("maps observed initialization, contentless messages, outputless completions and per-step accounting", () => {
    const events = parseAgyLog(jsonl(agySmoke)).logEntries;
    expect(events.find(event => event.type === "session.init").data).toMatchObject({
      sourceEngine: "agy",
      sessionId: "agy-ci-conversation",
      cwd: "/workspace/gh-aw",
      tools: ["write_to_file", "call_mcp_tool"],
    });
    expect(events.find(event => event.type === "user.message").data).not.toHaveProperty("content");
    const completion = events.find(event => event.type === "tool.execution_complete" && event.data.toolName === "write_to_file");
    expect(completion.data).toMatchObject({ stepIndex: 20, sessionId: "agy-ci-conversation", durationMs: 0.020827639 * 1000, status: "DONE" });
    expect(completion.data).not.toHaveProperty("output");
    expect(completion.data).not.toHaveProperty("success");
    expect(completion.data).not.toHaveProperty("toolCallId");
    expect(events.filter(event => event.type === "session.collection_warning")).toEqual([]);
    const mcp = events.find(event => event.type === "tool.execution_start" && event.data.toolName === "call_mcp_tool");
    expect(mcp.data).toMatchObject({ mcpServerName: "agy-native", input: agySmoke.find(record => record.step_update?.tool_name === "call_mcp_tool").step_update.tool_info.parameters });
    expect(events.filter(event => event.type === "usage.report")).toHaveLength(2);
    expect(events.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(events.at(-1).data.usage).toMatchObject({ input_tokens: 79491, cache_read_input_tokens: 298918, input_tokens_include_cache: false });
    expect(events.every(event => !Object.hasOwn(event, "id"))).toBe(true);
    expect(parseAgyLog(jsonl(events)).logEntries).toEqual(events);
  });

  it("preserves the exact text and essential evidence through canonical persistence and unified publication", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "agy-session-"));
    roots.push(root);
    const canonical = parseEngineSession(jsonl(agySmoke), "agy");
    fs.writeFileSync(path.join(root, "agent-stdio.log"), jsonl(agySmoke));
    writeSessionArtifact(path.join(root, "agent-session.jsonl"), canonical);
    const persisted = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(persisted).toEqual(canonical);
    expect(validateSession(jsonl(persisted), "agent")).toBe(persisted.length);
    const unified = collectUnifiedSession({ rootDir: root, engine: "agy", dailyAIC: {}, warn: () => {} }).events;
    const agent = unified.filter(event => event.provenance.component === "agent");
    expect(
      agent
        .filter(event => event.type === "assistant.message")
        .map(event => event.data.content ?? "")
        .join("")
    ).toBe(agySmoke.at(-1).result.response);
    expect(agent.find(event => event.type === "session.init").data).toMatchObject({ sessionId: "agy-ci-conversation", cwd: "/workspace/gh-aw" });
    expect(agent.find(event => event.type === "session.result").data).toMatchObject({
      sourceType: "SUCCESS",
      durationMs: 51779.892838,
      usage: { inputTokens: 79491, reasoningOutputTokens: 3221, cacheReadInputTokens: 298918, inputTokensIncludeCache: false },
    });
    expect(agent.filter(event => event.type === "usage.report")).toHaveLength(2);
    expect(agent.find(event => event.type === "usage.report").data).toMatchObject({ stepIndex: 1, sessionId: "agy-ci-conversation" });
    expect(agent.find(event => event.type === "tool.execution_complete" && event.data.toolName === "write_to_file").data).toMatchObject({ stepIndex: 20, sessionId: "agy-ci-conversation" });
    expect(agent.every(event => event.provenance.path === "agent-session.jsonl")).toBe(true);
    expect(validateSession(jsonl(unified), "unified")).toBe(unified.length);
  });
});

describe("AGY source-dependent observations (synthetic edge cases)", () => {
  it("preserves native identities, empty content, user/reasoning channels and unknown extensions", () => {
    const records = [
      { event: "user", id: "user-native", parentId: null, timestamp: 0, message: { content: [{ type: "text", text: "  prompt\n" }] } },
      { event: "step_update", id: "thought-native", parentId: "user-native", timestamp: 1, step_update: { step_type: "reasoning", step_index: 0, text_delta: "" } },
      { event: "step_update", id: "answer-native", timestamp: 2, step_update: { step_type: "agent_response", step_index: 0, text_delta: "  answer\n" } },
      { event: "future", id: "extension-native", future: { flag: false, value: 0, nested: [null, ""] } },
      { type: "vendor.progress", id: "opaque", parentId: null, data: { state: false, count: 0 } },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.find(event => event.type === "user.message")).toMatchObject({ id: "user-native", parentId: null, timestamp: 0, data: { content: records[0].message.content } });
    expect(events.find(event => event.type === "assistant.reasoning")).toMatchObject({ id: "thought-native", data: { content: "" } });
    expect(events.find(event => event.type === "assistant.message")).toMatchObject({ id: "answer-native", data: { content: "  answer\n" } });
    expect(events.find(event => event.type === "agy.future")).toMatchObject({ id: "extension-native", data: records[3].future });
    expect(events.at(-1)).toEqual(records.at(-1));
    expect(events.some(event => event.type === "session.init")).toBe(false);
  });

  it.each([false, 0, "", null, [], {}])("retains non-object tool input/output %j without synthesizing an orphan start", value => {
    const raw = { event: "step_update", id: "native-complete", step_update: { step_type: "tool", step_index: 0, state: "DONE", duration_seconds: 0, tool_info: { name: "", parameters: value, output: value, error: null } } };
    const events = parseAgyLog(jsonl([raw])).logEntries;
    expect(events.filter(event => event.type === "tool.execution_start")).toEqual([]);
    expect(events.find(event => event.type === "tool.execution_complete")).toMatchObject({ id: "native-complete", data: { toolName: "", output: value, error: null, durationMs: 0 } });
  });

  it("preserves result order, bare successes, missing status/metrics and structured failures", () => {
    const records = [
      { conversation_id: "", status: "SUCCESS", response: "", usage: { input_tokens: 0, output_tokens: 0 }, id: "bare-result" },
      { event: "result", id: "partial-result", result: {} },
      { event: "result", id: "failure", result: { conversation_id: "failure-session", status: "ERROR", error: { code: 0, type: "api_error", detail: false } } },
      { type: "vendor.after", data: { done: true } },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.find(event => event.type === "assistant.message").data.content).toBe("");
    expect(events.find(event => event.id === "partial-result" && event.type === "session.result").data).not.toHaveProperty("status");
    expect(events.find(event => event.type === "session.error").data.error).toEqual(records[2].result.error);
    expect(events.at(-1)).toEqual(records.at(-1));
  });

  it("maps explicit provider refusal signals but never mines user/tool prose", () => {
    const records = [
      { event: "step_update", id: "refusal", step_update: { step_type: "agent_response", step_index: 0, state: "DONE", text_delta: "", finish_reason: "content_filter" } },
      { event: "user", message: { content: { refusal: "user quotation" } } },
      { event: "step_update", step_update: { step_type: "tool", state: "DONE", tool_info: { name: "read", output: { refusal: "tool quotation" } } } },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(events.find(event => event.type === "assistant.refusal")).toMatchObject({ id: "refusal", data: { reason: "content_filter", content: "" } });
  });

  it("never concatenates streamed text across a tool or extension boundary", () => {
    const delta = text_delta => ({ event: "step_update", step_update: { step_index: 0, step_type: "agent_response", state: "ACTIVE", text_delta } });
    const records = [
      delta("before "),
      { event: "step_update", step_update: { step_index: 1, step_type: "tool", state: "ACTIVE", tool_info: { name: "read", parameters: null } } },
      delta("after\n"),
      { type: "vendor.progress", data: { complete: false } },
      delta("tail"),
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.map(event => event.type)).toEqual(["assistant.message", "tool.execution_start", "assistant.message", "vendor.progress", "assistant.message"]);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["before ", "after\n", "tail"]);
    expect(events[1].data.input).toBeNull();
  });

  it("maps checkpoint usage without adding it to terminal snapshots or inventing missing metrics", () => {
    const records = [
      { event: "step_update", step_update: { step_index: 4, step_type: "checkpoint", state: "DONE", usage: { input_tokens: 0, thinking_tokens: 0 }, duration_seconds: 0 } },
      { event: "result", result: { status: "SUCCESS", num_turns: -1, duration_seconds: Infinity, usage: { input_tokens: -1 } } },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.find(event => event.type === "usage.report").data).toMatchObject({ durationMs: 0, usage: { input_tokens: 0, reasoning_output_tokens: 0 } });
    expect(events.at(-1).data).toMatchObject({ usage: {} });
    expect(events.at(-1).data).not.toHaveProperty("numTurns");
    expect(events.at(-1).data).not.toHaveProperty("durationMs");
    expect(validateSession(jsonl(events), "agent")).toBe(events.length);
  });

  it("retains structured tool errors and reported command/server/exit metadata, including zero", () => {
    const record = {
      event: "step_update",
      step_update: {
        step_type: "tool",
        state: "DONE",
        tool_info: { name: "call_mcp_tool", parameters: { ServerName: "", CommandLine: "" }, output: false, success: true, error: { code: 0, type: "denied" }, exitCode: 0 },
      },
    };
    const event = parseAgyLog(jsonl([record])).logEntries[0];
    expect(event.data).toMatchObject({ success: false, output: false, error: record.step_update.tool_info.error, command: "", mcpServerName: "", exitCode: 0 });
    expect(event.data).not.toHaveProperty("toolCallId");
  });

  it("prioritizes genuine call IDs over conflicting step keys and preserves native update metadata", () => {
    const step = (id, toolCallId, state, extra = {}) => ({
      event: "step_update",
      id,
      timestamp: 0,
      step_update: { conversation_id: "native-conversation", step_index: 0, step_type: "tool", state, tool_info: { toolCallId, ...extra } },
    });
    const records = [
      step("native-start", "call-a", "ACTIVE", { name: "read", parameters: 0 }),
      step("native-update", "call-a", "ACTIVE", { parameters: false }),
      step("native-orphan", "call-b", "DONE", { output: null }),
      step("native-complete", "call-a", "DONE", { output: "" }),
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "tool.execution_update", "tool.execution_complete", "tool.execution_complete"]);
    expect(events.map(event => event.id)).toEqual(records.map(record => record.id));
    expect(events[1].data).toMatchObject({ toolCallId: "call-a", stepIndex: 0, input: false });
    expect(events[2].data).not.toHaveProperty("toolName");
    expect(events[3].data).toMatchObject({ toolCallId: "call-a", toolName: "read", output: "" });
  });

  it("retains native snapshot identities, reported zero cost and empty accounting/initialization additions", () => {
    const records = [
      { event: "init", conversation_id: "", init: { cwd: "", tools: [], mcp_servers: [], model_info: {}, slash_commands: [] } },
      { event: "result", id: "early", timestamp: 0, result: { status: "SUCCESS", total_cost_usd: 0, usage: { input_tokens: 0, billing: { measured: false } } } },
      { event: "result", id: "latest", timestamp: 1, result: { status: "SUCCESS", errors: [], permission_denials: [] } },
      { event: "future_scalar", future_scalar: false },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events[0].data).toMatchObject({ sessionId: "", cwd: "", tools: [], mcpServers: [], modelInfo: {}, slashCommands: [] });
    const terminal = events.find(event => event.type === "session.result");
    expect(terminal).toMatchObject({ id: "latest", timestamp: 1, data: { totalCostUsd: 0, errors: [], permissionDenials: [], usage: { input_tokens: 0, billing: { measured: false } }, nativeSnapshots: [records[1]] } });
    expect(events.at(-1)).toMatchObject({ type: "agy.future_scalar", data: { future_scalar: false } });
    expect(validateSession(jsonl(events), "agent")).toBe(events.length);
  });

  it("does not fabricate errors or successful completion from a partial/unknown terminal state", () => {
    for (const result of [{}, { status: "RUNNING" }, { status: "WAITING" }]) {
      const events = parseAgyLog(jsonl([{ event: "result", result }])).logEntries;
      expect(events.map(event => event.type)).toEqual(["session.result"]);
      expect(events[0].data.status).not.toBe("completed");
      expect(events[0].data).not.toHaveProperty("errors");
      expect(events[0].data).not.toHaveProperty("usage");
    }
  });

  it("does not carry cumulative usage or tool/message state into a new anonymous initialization", () => {
    const records = [
      { event: "init", init: {} },
      { event: "step_update", step_update: { step_index: 0, step_type: "tool", state: "ACTIVE", tool_info: { name: "old-tool" } } },
      { event: "result", result: { status: "SUCCESS", response: "same answer", usage: { input_tokens: 100 } } },
      { event: "init", init: {} },
      { event: "step_update", step_update: { step_index: 0, step_type: "tool", state: "DONE", tool_info: { output: 0 } } },
      { event: "result", result: { status: "SUCCESS", response: "same answer" } },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.filter(event => event.type === "session.result")).toHaveLength(2);
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(2);
    expect(events.at(-1).data).not.toHaveProperty("usage");
    expect(events.find(event => event.type === "tool.execution_complete").data).not.toHaveProperty("toolName");
    expect(events.every(event => !Object.hasOwn(event.data, "sessionId"))).toBe(true);
  });

  it("adds only the unobserved suffix of a terminal response snapshot", () => {
    const records = [
      { event: "step_update", step_update: { step_index: 0, step_type: "agent_response", text_delta: "partial " } },
      { event: "result", result: { status: "SUCCESS", response: "partial answer\n" } },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["partial ", "answer\n"]);
    expect(events.find(event => event.type === "session.result").result.response).toBe("partial answer\n");
  });

  it("does not reinterpret an agent response error as an answer", () => {
    const record = { event: "step_update", step_update: { step_index: 1, step_type: "agent_response", state: "DONE", error: { type: "api_error", code: 0 } } };
    const events = parseAgyLog(jsonl([record])).logEntries;
    expect(events.map(event => event.type)).toEqual(["session.error"]);
    expect(events[0].data.error).toEqual(record.step_update.error);
  });

  it("does not duplicate explicitly refused streaming text in its final result snapshot", () => {
    const records = [
      { event: "step_update", step_update: { step_index: 1, step_type: "agent_response", text_delta: "  refused\n", finish_reason: "content_filter" } },
      { event: "result", result: { status: "SUCCESS", response: "  refused\n", finish_reason: "content_filter" } },
    ];
    const events = parseAgyLog(jsonl(records)).logEntries;
    expect(events.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(events.find(event => event.type === "assistant.refusal").data).toMatchObject({ content: "  refused\n", reason: "content_filter" });
    expect(events.some(event => event.type === "assistant.message")).toBe(false);
  });
});
