import { describe, expect, it } from "vitest";
import { parseOpenCodeLog, isOpenCodeEvent } from "./parse_opencode_log.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";
import { parseEngineSession, mergeSessionSources, writeUnifiedSession } from "./unified_session.cjs";
import { opencodeCiExcerpt, opencodeCiStreamErrors } from "./fixtures/opencode_ci_sessions.cjs";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const sessionID = "ses_fixture";
const timestamp = 1791040000000;
const record = (type, part, extra = {}) => ({ type, timestamp, sessionID, part: { id: `prt_${type}`, sessionID, messageID: "msg_fixture", ...part }, ...extra });
const jsonl = records => records.map(value => JSON.stringify(value)).join("\n");

describe("OpenCode JSON session parser", () => {
  it("preserves text, reasoning, tools, errors, timestamps and per-step usage", () => {
    const records = [
      record("step_start", { type: "step-start" }),
      record("reasoning", { type: "reasoning", text: "Consider the repository." }),
      record("tool_use", { type: "tool", callID: "call_fixture", tool: "bash", state: { status: "completed", input: { command: "pwd" }, output: "/workspace\n", time: { start: timestamp + 10, end: timestamp + 40 } } }),
      record("step_finish", { type: "step-finish", reason: "tool-calls", cost: 0.01, tokens: { input: 10, output: 5, reasoning: 2, cache: { read: 20, write: 3 } } }),
      record("text", { type: "text", text: "Finished.\n" }, { timestamp: timestamp + 50 }),
      record("step_finish", { id: "prt_finish_2", type: "step-finish", reason: "stop", cost: 0, tokens: { input: 4, output: 2, reasoning: 0, cache: { read: 0, write: 0 } } }, { timestamp: timestamp + 60 }),
    ];
    const content = `[INFO] AWF setup\nDEBUG OpenCode startup\n{broken\n${jsonl(records)}\nProcess exiting with code: 0`;
    const result = parseOpenCodeLog(content);
    expect(result.logEntries.find(event => event.type === "session.init")).toMatchObject({ timestamp, data: { sourceEngine: "opencode", sessionId: sessionID } });
    expect(result.logEntries.find(event => event.type === "assistant.reasoning").data.content).toBe("Consider the repository.");
    expect(result.logEntries.find(event => event.type === "assistant.message").data.content).toBe("Finished.\n");
    expect(result.logEntries.find(event => event.type === "assistant.message")).toMatchObject({ id: "prt_text", parentId: "msg_fixture" });
    expect(result.logEntries.find(event => event.type === "tool.execution_start")).toMatchObject({ timestamp, data: { toolCallId: "call_fixture", toolName: "bash", input: { command: "pwd" } } });
    expect(result.logEntries.find(event => event.type === "tool.execution_complete")).toMatchObject({
      timestamp,
      data: { toolCallId: "call_fixture", toolName: "bash", output: "/workspace\n", durationMs: 30, success: true },
    });
    expect(result.logEntries.at(-1)).toMatchObject({
      type: "session.result",
      timestamp: timestamp + 60,
      data: { numTurns: 2, totalCostUsd: 0.01, usage: { input_tokens: 14, output_tokens: 7, reasoning_output_tokens: 2, cache_read_input_tokens: 20, cache_creation_input_tokens: 3, input_tokens_include_cache: false } },
    });
    expect(result.markdown).not.toContain("OpenCode startup");
    expect(result.mcpFailures).toEqual([]);
    expect(result.maxTurnsHit).toBe(false);
    expect(parseEngineSession(content, "opencode")).toEqual(result.logEntries);
    expect(parseCustomLog(content).logEntries).toEqual(result.logEntries);
  });

  it("retains zero usage and zero cost instead of treating them as absent", () => {
    const result = parseOpenCodeLog(jsonl([record("step_finish", { cost: 0, tokens: { total: 0, input: 0, output: 0, cache: { read: 0, write: 0 } } })]));
    expect(result.logEntries.at(-1).data).toMatchObject({ numTurns: 1, totalCostUsd: 0, usage: { total_tokens: 0, input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 } });
  });

  it("deduplicates completed parts within a session but not across sessions", () => {
    const finish = record("step_finish", { cost: 1, tokens: { input: 3 } });
    const tool = record("tool_use", { callID: "call", tool: "github_search", state: { status: "completed", input: {}, output: "ok" } });
    const result = parseOpenCodeLog(jsonl([finish, tool, finish, tool, { ...finish, sessionID: "ses_other" }]));
    expect(result.logEntries.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(result.logEntries.filter(event => event.type === "session.init")).toHaveLength(2);
    expect(result.logEntries.at(-1).data).toMatchObject({ numTurns: 2, totalCostUsd: 2, usage: { input_tokens: 6 } });
  });

  it("does not invent successful completions or session results for partial logs", () => {
    const partial = record("tool_use", { callID: "call", tool: "bash", state: { status: "running", input: { command: "sleep 1" } } });
    const result = parseOpenCodeLog(jsonl([partial]));
    expect(result.logEntries.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(result.logEntries.some(event => event.type === "tool.execution_complete" || event.type === "session.result")).toBe(false);
  });

  it("preserves failed tool output and structured API errors", () => {
    const error = { name: "APIError", data: { message: "Authentication failed", statusCode: 401, isRetryable: false } };
    const result = parseOpenCodeLog(
      jsonl([
        record("tool_use", { callID: "call_error", tool: "github_search", state: { status: "error", input: { query: "q" }, error: "MCP rejected request", time: { start: timestamp, end: timestamp + 1 } } }),
        { type: "error", timestamp: timestamp + 2, sessionID, error },
      ])
    );
    expect(result.logEntries.find(event => event.type === "tool.execution_complete").data).toMatchObject({ success: false, error: "MCP rejected request", durationMs: 1 });
    expect(result.logEntries.at(-1).data).toMatchObject({ errors: [error], status: "error" });
  });

  it.each([
    { status: "completed", metadata: { exit: 2 }, success: false },
    { status: "completed", metadata: { exit: 0 }, success: true },
    { status: "completed", metadata: { exit: 0 }, error: "Command rejected", success: false },
    { status: "error", metadata: { exit: 0 }, success: false },
    { status: "completed", metadata: { exit: 0 }, nativeSuccess: false, success: false },
  ])("gives failures precedence over completed status and zero exit: %j", ({ status, metadata, error, nativeSuccess, success }) => {
    const state = { status, input: { command: "make build" }, output: "command output", metadata, ...(error !== undefined ? { error } : {}), ...(nativeSuccess !== undefined ? { success: nativeSuccess } : {}) };
    const result = parseOpenCodeLog(jsonl([record("tool_use", { callID: "call_exit", tool: "bash", state })]));
    const completion = result.logEntries.find(event => event.type === "tool.execution_complete");
    expect(completion.data).toMatchObject({ toolCallId: "call_exit", toolName: "bash", output: "command output", status: success ? "completed" : "error", success, exitCode: metadata.exit, metadata });
    expect(completion.part.state).toEqual(state);
    if (error !== undefined) expect(completion.data.error).toBe(error);
  });

  it("retains native exit metadata and payload additions in the canonical completion", () => {
    const metadata = { exit: 2, truncated: false, output: "make: build failed\n" };
    const content = jsonl([record("tool_use", { callID: "call_build", tool: "bash", state: { status: "completed", input: { command: "make build" }, output: metadata.output, metadata } }, { data: { traceId: "trace_fixture" } })]);
    const result = parseOpenCodeLog(content);
    const completion = result.logEntries.find(event => event.type === "tool.execution_complete");
    expect(completion.data).toMatchObject({ success: false, status: "error", exitCode: 2, metadata, traceId: "trace_fixture" });
    expect(completion.part.state.metadata).toEqual(metadata);
    expect(result.markdown).toContain("Tools: 0/1 succeeded");
    expect(result.markdown).toContain("Failed Tools: 1");
    expect(result.markdown).toContain("✗ $ make build");
    expect(parseEngineSession(content, "opencode")).toEqual(result.logEntries);
    expect(parseCustomLog(content).logEntries).toEqual(result.logEntries);
    expect(parseOpenCodeLog(jsonl(result.logEntries)).logEntries).toEqual(result.logEntries);
  });

  it("does not invent an exit code for absent or malformed native metadata", () => {
    for (const metadata of [undefined, {}, { exit: "2" }, { exit: 1.5 }, { exit: null }]) {
      const result = parseOpenCodeLog(jsonl([record("tool_use", { callID: "call_no_exit", tool: "bash", state: { status: "completed", input: {}, output: "ok", ...(metadata !== undefined ? { metadata } : {}) } })]));
      const completion = result.logEntries.find(event => event.type === "tool.execution_complete");
      expect(completion.data).not.toHaveProperty("exitCode");
      expect(completion.data.success).toBe(true);
      if (metadata !== undefined) expect(completion.data.metadata).toEqual(metadata);
    }
  });

  it.each(["", "prompt with a secret and max-turns", '{"type":"text","text":"not OpenCode"}', '{"type":"error","message":"not OpenCode"}', '{"sessionID":"s","type":"text","part":null}', '{"sessionID":"s","type":"text","part":[]}'])(
    "rejects unsupported input without publishing raw data: %s",
    content => {
      expect(parseOpenCodeLog(content).logEntries).toEqual([]);
      expect(parseOpenCodeLog(content).markdown).not.toContain("prompt with a secret");
    }
  );

  it("accepts canonical events without fabricating another result", () => {
    const events = [{ type: "assistant.message", timestamp, data: { content: "native" } }];
    expect(parseOpenCodeLog(jsonl(events)).logEntries).toEqual(events);
  });

  it("marks token overflow unavailable and ignores negative or invalid usage", () => {
    const result = parseOpenCodeLog(jsonl([record("step_finish", { tokens: { input: Number.MAX_SAFE_INTEGER, output: -1, cache: { read: "12" } } }), record("step_finish", { id: "prt_other", tokens: { input: 1, output: 2 } })]));
    expect(result.logEntries.at(-1).data.usage).toMatchObject({ output_tokens: 2, overflowed_tokens: ["input_tokens"] });
    expect(result.logEntries.at(-1).data.usage).not.toHaveProperty("input_tokens");
    expect(result.logEntries.at(-1).data.usage).not.toHaveProperty("cache_read_input_tokens");
  });

  it("does not mistake generic JSON errors for OpenCode events", () => {
    expect(isOpenCodeEvent({ type: "error", error: {} })).toBe(false);
    expect(isOpenCodeEvent({ sessionID, type: "unknown", part: {} })).toBe(false);
    expect(isOpenCodeEvent({ sessionID, type: "text", part: {} })).toBe(false);
    expect(isOpenCodeEvent({ sessionID, type: "tool_use", part: { tool: "bash", state: [] } })).toBe(false);
  });

  it("does not let a malformed observation hide a supported part with the same ID", () => {
    const result = parseOpenCodeLog(jsonl([record("text", { text: 1 }), record("text", { text: "recovered" })]));
    expect(result.logEntries.find(event => event.type === "assistant.message").data.content).toBe("recovered");
  });

  it("reports only explicit budget events rather than prompt or diagnostic text", () => {
    const result = parseOpenCodeLog(jsonl([{ type: "opencode.max_turns", timestamp, data: { maxTurns: 2 } }]));
    expect(result.maxTurnsHit).toBe(true);
    expect(result.logEntries.at(-1).data).toMatchObject({ status: "error", errors: [{ type: "opencode.max_turns", maxTurns: 2 }] });
    expect(result.logEntries.at(-1).data).not.toHaveProperty("numTurns");
    expect(parseOpenCodeLog(jsonl(result.logEntries)).logEntries).toEqual(result.logEntries);
    expect(parseOpenCodeLog(jsonl([record("text", { text: "The prompt mentions maximum turns reached." })])).maxTurnsHit).toBe(false);
  });

  it("recognizes native MCP failure diagnostics without treating prose as failure evidence", () => {
    const failure = 'timestamp=2026-10-03T17:28:22.606Z level=WARN run=fixture message="server unavailable" key=github type=remote status=failed';
    const content = `${failure}\n${failure}\n${jsonl([{ type: "opencode.mcp_failure", data: { serverName: "github" } }])}`;
    expect(parseOpenCodeLog(content).mcpFailures).toEqual(["github"]);
    expect(parseOpenCodeLog('The prompt says MCP server github failed.\nmessage="server unavailable" key=github type=remote status=failed').mcpFailures).toEqual([]);
    expect(parseOpenCodeLog(failure.replace("key=github", 'key="custom server"')).mcpFailures).toEqual(["custom server"]);
  });

  it("does not restore overflowing native total/reasoning aggregates from a later partial report", () => {
    const records = [
      record("step_finish", { id: "first", tokens: { total: Number.MAX_SAFE_INTEGER, reasoning: Number.MAX_SAFE_INTEGER } }),
      record("step_finish", { id: "second", tokens: { total: 1, reasoning: 1 } }),
      record("step_finish", { id: "third", tokens: { total: 2, reasoning: 2 } }),
    ];
    const result = parseOpenCodeLog(jsonl(records));
    expect(result.logEntries.at(-1).data.usage).not.toHaveProperty("total_tokens");
    expect(result.logEntries.at(-1).data.usage).not.toHaveProperty("reasoning_output_tokens");
    expect(result.logEntries.at(-1).data.usage.overflowed_tokens).toEqual(expect.arrayContaining(["total_tokens", "reasoning_output_tokens"]));
  });

  it("preserves a CI-shaped agent session without treating downstream failure as an agent failure", () => {
    const content = jsonl(opencodeCiExcerpt);
    const events = parseOpenCodeLog(content).logEntries;
    expect(events.filter(event => event.type === "tool.execution_complete")).toMatchObject([
      { id: opencodeCiExcerpt[0].part.id, data: { success: false, exitCode: 2, output: "example: build failed\n", durationMs: 683 } },
      { id: opencodeCiExcerpt[1].part.id, data: { success: false, error: '{"result":"error","error":"MCP rejected example"}', durationMs: 26 } },
    ]);
    expect(events.find(event => event.type === "assistant.message").data.content).toBe("Example finished.\n");
    expect(events.at(-1).data).toEqual({
      numTurns: 2,
      totalCostUsd: 0,
      usage: { total_tokens: 46187, input_tokens: 23222, output_tokens: 53, reasoning_output_tokens: 0, cache_creation_input_tokens: 0, cache_read_input_tokens: 22912, input_tokens_include_cache: false },
    });
    expect(events.at(-1).data).not.toHaveProperty("status");
    expect(events.at(-1).data).not.toHaveProperty("errors");
    expect(parseCustomLog(content).logEntries).toEqual(events);
    expect(parseEngineSession(content, "opencode")).toEqual(events);
    expect(parseOpenCodeLog(jsonl(events)).logEntries).toEqual(events);
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
    expect(unified.at(-1).data.usage).toMatchObject({ totalTokens: 46187, inputTokens: 23222, cacheReadInputTokens: 22912 });
    expect(unified.find(event => event.type === "tool.execution_complete").data).toMatchObject({ success: false, exitCode: 2, output: "example: build failed\n" });
  });

  it("recovers native logfmt stream errors without inventing terminal results or accounting", () => {
    const result = parseOpenCodeLog(opencodeCiStreamErrors.join("\n"));
    expect(result.logEntries).toHaveLength(2);
    expect(result.logEntries.map(event => event.type)).toEqual(["session.error", "session.error"]);
    expect(result.logEntries[0]).toMatchObject({
      timestamp: "2026-10-09T00:37:13.776Z",
      data: { error: "AI_APICallError: Too Many Requests", sessionId: "ses_ee1e84b56ffeap38OrBZh2U7qY", providerID: "awf-proxy", modelID: "auto", small: "false" },
    });
    expect(result.logEntries[1].data.error).toBe("AI_RetryError: Failed after 3 attempts. Last error: Too Many Requests");
    expect(result.logEntries.some(event => event.type === "assistant.message" || event.type === "session.result")).toBe(false);
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: result.logEntries }]);
    expect(unified[0].data).toEqual(result.logEntries[0].data);
    expect(unified[0].provenance.timestampMs).toBe(1791506233776);
    expect(parseEngineSession(opencodeCiStreamErrors.join("\n"), "opencode").filter(event => event.type !== "agent.execution")).toEqual(result.logEntries);
    expect(parseOpenCodeLog(jsonl(result.logEntries)).logEntries).toEqual(result.logEntries);
  });

  it("does not reinterpret diagnostic prose, malformed logfmt, or unrelated error fields", () => {
    const native = opencodeCiStreamErrors[0];
    for (const content of [
      `The prompt quotes ${native}`,
      `\`\`\`log\n${native}\n\`\`\``,
      `~~~log\n${native}\n~~~`,
      native.replace("level=ERROR", "level=WARN"),
      native.replace('message="stream error"', 'message="unrelated error"'),
      native.replace("session.id=", "notSession="),
      native.replace('error.error="AI_APICallError: Too Many Requests"', 'error.error="unterminated'),
      `${native} malformed`,
    ])
      expect(parseOpenCodeLog(content).logEntries).toEqual([]);
    const escaped = native.replace('error.error="AI_APICallError: Too Many Requests"', 'error.error="line\\nwith \\"quotes\\""');
    expect(parseOpenCodeLog(escaped).logEntries[0].data.error).toBe('line\nwith "quotes"');
  });

  it("retains error/message arrival order without assuming a retried stream error was terminal", () => {
    const message = record("text", { text: "Recovered.\n" });
    const events = parseOpenCodeLog(`${opencodeCiStreamErrors[0]}\n${jsonl([message])}\n${opencodeCiStreamErrors[1]}`).logEntries;
    expect(events.map(event => event.type)).toEqual(["session.error", "session.init", "assistant.message", "session.error"]);
    expect(events[2].data.content).toBe("Recovered.\n");
  });

  it("reconciles changed step snapshots once while retaining previous independently known fields", () => {
    const first = record("step_finish", { cost: 1, tokens: { input: 3, output: 2, total: 5, reasoning: 0 } });
    const updated = { ...first, timestamp: timestamp + 1, part: { ...first.part, cost: 2, tokens: { input: 4 } } };
    const events = parseOpenCodeLog(jsonl([first, first, updated, updated])).logEntries;
    expect(events.filter(event => event.type === "opencode.step_finish")).toHaveLength(2);
    expect(events.at(-1).data).toMatchObject({ numTurns: 1, totalCostUsd: 2, usage: { input_tokens: 4, output_tokens: 2, total_tokens: 5, reasoning_output_tokens: 0 } });
    expect(events.filter(event => event.type === "opencode.step_finish").map(event => event.part)).toEqual([first.part, updated.part]);
  });

  it.each(["text", "reasoning"])("reconciles %s snapshots without duplicating content or losing native revisions", type => {
    const first = record(type, { text: "  first\n", partial: true }, { nativeMetadata: { value: false } });
    const final = { ...first, timestamp: timestamp + 1, part: { ...first.part, text: "  first\n\tlast \n", partial: false } };
    const events = parseOpenCodeLog(jsonl([first, final, final])).logEntries;
    const messages = events.filter(event => event.type === (type === "text" ? "assistant.message" : "assistant.reasoning"));
    expect(messages).toHaveLength(1);
    expect(messages[0].data).toMatchObject({ content: "  first\n\tlast \n", partial: false });
    expect(messages[0].nativeSnapshots).toEqual([first, final]);
    expect(messages[0].nativeMetadata).toEqual({ value: false });
    expect(messages[0].id).toBe(first.part.id);
    expect(parseOpenCodeLog(jsonl(events)).logEntries).toEqual(events);
  });

  it("retains unknown canonical extensions and structured/empty values without inventing metrics", () => {
    const extension = { type: "vendor.progress", id: "native-id", timestamp: 0, parentId: null, nativeMetadata: [], data: { value: null, empty: "", list: [], nested: { flag: false, number: 0 } } };
    expect(parseOpenCodeLog(jsonl([extension])).logEntries).toEqual([extension]);
    const events = parseOpenCodeLog(jsonl([record("step_finish", { tokens: {}, cost: -1 })])).logEntries;
    expect(events.at(-1).data).toEqual({ numTurns: 1 });
  });

  it("maps explicit structured refusals, not ordinary disclaimers or tool payloads", () => {
    const refusal = { type: "assistant.message", id: "refusal", data: { content: "", refusal: "", partial: true } };
    const events = parseOpenCodeLog(
      jsonl([refusal, record("text", { text: "I cannot tell whether this is supported." }), record("tool_use", { tool: "example", state: { status: "completed", input: { refusal: "not a model response" }, output: null } })])
    ).logEntries;
    expect(events[0]).toEqual({ ...refusal, type: "assistant.refusal", data: { ...refusal.data, reason: "refusal" } });
    expect(events.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(events.find(event => event.type === "tool.execution_complete").data.output).toBeNull();
    const native = parseOpenCodeLog(jsonl([record("text", { text: "", stop_reason: "refusal", stop_details: { category: null, explanation: "" } })])).logEntries;
    expect(native.find(event => event.type === "assistant.refusal").data).toEqual({ reason: "refusal", content: "", policyCategory: null, explanation: "" });
  });

  it.each([
    ["content-filter", "content_filter"],
    ["refusal", "refusal"],
  ])("maps the explicit %s finish signal without duplicating its associated text", (finishReason, reason) => {
    const text = record("text", { text: "  partial\n", partial: true });
    const finish = record("step_finish", { reason: finishReason });
    const events = parseOpenCodeLog(jsonl([text, finish])).logEntries;
    expect(events.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(events.find(event => event.type === "assistant.refusal").data).toEqual({ reason, content: "  partial\n", partial: true });
    expect(events.some(event => event.type === "assistant.message")).toBe(false);
    expect(events.at(-1).data).toEqual({ numTurns: 1 });
    const noText = parseOpenCodeLog(jsonl([finish])).logEntries.find(event => event.type === "assistant.refusal");
    expect(noText.data).toEqual({ reason });
    const other = record("text", { text: "Unrelated.", messageID: "other-message" });
    expect(parseOpenCodeLog(jsonl([other, finish])).logEntries.find(event => event.type === "assistant.message").data.content).toBe("Unrelated.");
    expect(parseOpenCodeLog(jsonl([text, record("step_finish", { reason: "length" })])).logEntries.some(event => event.type === "assistant.refusal")).toBe(false);
  });

  it("deduplicates changed tool snapshots and recovers input unavailable at the first observation", () => {
    const pending = record("tool_use", { tool: "example", state: { status: "running" } });
    const completed = { ...pending, timestamp: timestamp + 1, part: { ...pending.part, state: { status: "completed", input: false, output: "", result: { content: [] }, metadata: { exit: 0 } } } };
    const updated = { ...completed, timestamp: timestamp + 2, part: { ...completed.part, state: { ...completed.part.state, output: "Updated.\n", metadata: { exit: 2 } } } };
    const events = parseOpenCodeLog(jsonl([pending, completed, updated])).logEntries;
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(events.find(event => event.type === "tool.execution_start").data.input).toBe(false);
    const completions = events.filter(event => event.type === "tool.execution_complete");
    expect(completions).toHaveLength(1);
    expect(completions[0].data).toMatchObject({ output: "Updated.\n", result: { content: [] }, success: false, exitCode: 2 });
    expect(completions[0].nativeSnapshots).toEqual([completed, updated]);
  });

  it("omits overflowing costs instead of retaining a misleading partial subtotal", () => {
    const events = parseOpenCodeLog(jsonl([record("step_finish", { id: "cost-1", cost: Number.MAX_VALUE }), record("step_finish", { id: "cost-2", cost: Number.MAX_VALUE }), record("step_finish", { id: "cost-3", cost: 1 })])).logEntries;
    expect(events.at(-1).data).not.toHaveProperty("totalCostUsd");
    expect(events.at(-1).data.numTurns).toBe(3);
  });

  it("persists recovered errors and exact canonical content through the existing unified writer", () => {
    const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "opencode-session-"));
    try {
      const events = parseOpenCodeLog(`${opencodeCiStreamErrors[0]}\n${jsonl(opencodeCiExcerpt)}`).logEntries;
      fs.writeFileSync(path.join(rootDir, "agent-session.jsonl"), `${jsonl(events)}\n`);
      const outputPath = path.join(rootDir, "aw_session.jsonl");
      const warnings = [];
      const warn = message => warnings.push(message);
      writeUnifiedSession({ rootDir, engine: "opencode", outputPath, warn });
      const persisted = fs.readFileSync(outputPath, "utf8").trim().split("\n").map(JSON.parse);
      expect(persisted[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
      expect(persisted.find(event => event.type === "session.error").data.error).toBe("AI_APICallError: Too Many Requests");
      expect(persisted.find(event => event.type === "assistant.message").data.content).toBe("Example finished.\n");
      expect(persisted.find(event => event.type === "session.result").data.usage.totalTokens).toBe(46187);
      const firstWrite = fs.readFileSync(outputPath, "utf8");
      writeUnifiedSession({ rootDir, engine: "opencode", outputPath, warn });
      expect(fs.readFileSync(outputPath, "utf8")).toBe(firstWrite);
      expect(warnings).toEqual([]);
    } finally {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  });
});
