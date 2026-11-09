import { describe, expect, it } from "vitest";
import { parseOpenCodeLog, isOpenCodeEvent, getOpenCodeMCPFailures, getOpenCodeSessionSteps } from "./parse_opencode_log.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";
import { sessionTokenTotal } from "./agent_session.cjs";
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
  it.each([true, false, undefined, "true"])("publishes observed v2 truncation and session context without provider metadata: %s", truncated => {
    const tool = record("tool_use", {
      partID: "prt_live_shape",
      id: "call_live_shape",
      tool: "grep",
      state: {
        status: "completed",
        input: { pattern: '"quoted"\\path', include: "*.cjs" },
        output: "first match\nsecond match\n",
        metadata: {
          metadata: { truncated },
          content: [{ type: "text", text: "first match\nsecond match\n" }],
          providerCall: { executed: false },
          providerResult: { executed: false },
          rawInput: '{"pattern":"private-provider-input"}',
        },
        time: { start: timestamp, end: timestamp + 10 },
      },
    });
    const events = parseOpenCodeLog(jsonl([tool, record("text", { text: "  done\n" }), record("step_finish", { reason: "stop", tokens: { input: 10, output: 2 } })])).logEntries;
    const completion = events.find(event => event.type === "tool.execution_complete");
    if (typeof truncated === "boolean") expect(completion.data.outputTruncated).toBe(truncated);
    else expect(completion.data).not.toHaveProperty("outputTruncated");
    expect(completion.data.metadata).toEqual(tool.part.state.metadata);
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
    expect(unified.every(event => event.data.sessionId === sessionID)).toBe(true);
    const published = unified.find(event => event.type === "tool.execution_complete");
    expect(published.data).toMatchObject({ toolCallId: "call_live_shape", output: tool.part.state.output, success: true });
    if (typeof truncated === "boolean") expect(published.data.outputTruncated).toBe(truncated);
    else expect(published.data).not.toHaveProperty("outputTruncated");
    expect(published.data).not.toHaveProperty("metadata");
    expect(JSON.stringify(unified)).not.toContain("private-provider-input");
    expect(JSON.parse(JSON.stringify(unified))).toEqual(unified);
  });

  it("retains v1 truncation and clears stale flags on reconciled tool snapshots", () => {
    const first = record("tool_use", { callID: "call_v1", tool: "grep", state: { status: "completed", input: {}, output: "preview\n", metadata: { truncated: true } } });
    const events = parseOpenCodeLog(jsonl([first])).logEntries;
    expect(events.find(event => event.type === "tool.execution_complete").data.outputTruncated).toBe(true);
    for (const metadata of [{ truncated: false }, {}, { truncated: "false" }]) {
      const replay = { ...first, timestamp: timestamp + 1, part: { ...first.part, state: { ...first.part.state, metadata, output: "latest\n" } } };
      const completions = parseOpenCodeLog(jsonl([first, replay])).logEntries.filter(event => event.type === "tool.execution_complete");
      expect(completions).toHaveLength(1);
      expect(completions[0].data.output).toBe("latest\n");
      if (metadata.truncated === false) expect(completions[0].data.outputTruncated).toBe(false);
      else expect(completions[0].data).not.toHaveProperty("outputTruncated");
    }
  });

  it("keeps live-shaped accounting reconciliation in source order without double-counting turns", () => {
    const first = record("step_finish", { messageID: "msg_first", tokens: { input: 10, output: 2 }, reason: "tool-calls" });
    const text = record("text", { messageID: "msg_second", text: "done\n" }, { timestamp: timestamp + 100 });
    const reconciled = { ...first, part: { ...first.part, tokens: { input: 20, output: 3 } } };
    const final = record("step_finish", { id: "gh_aw_export_msg_second", messageID: "msg_second", tokens: { input: 30, output: 4 }, reason: "stop" }, { timestamp: timestamp + 100 });
    const events = parseOpenCodeLog(jsonl([first, text, reconciled, final])).logEntries;
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", timestampUnit: "milliseconds", events }]);
    expect(unified.map(event => event.type)).toEqual(events.map(event => event.type));
    expect(unified.map(event => event.timestamp)).toEqual(events.map(event => event.timestamp));
    expect(unified.map(event => event.provenance.index)).toEqual(events.map((_, index) => index));
    expect(unified.filter(event => event.type === "opencode.step_finish")).toHaveLength(3);
    expect(unified.at(-1).data).toMatchObject({ numTurns: 2, usage: { inputTokens: 50, outputTokens: 7 }, sessionId: sessionID });
  });

  it("recovers only completed assistant-step metadata from v2 exports", () => {
    const steps = getOpenCodeSessionSteps(
      {
        info: { id: sessionID },
        messages: [
          { type: "user", text: "Never publish this prompt." },
          { type: "assistant", id: "pending", time: { created: timestamp } },
          { type: "assistant", id: "completed", finish: "stop", time: { completed: new Date(timestamp).toISOString() }, tokens: { input: 10, output: 2, cache: { read: 5 } }, cost: 0.01, providerState: { private: true } },
        ],
      },
      sessionID
    );
    expect(steps).toHaveLength(1);
    expect(steps[0]).toMatchObject({ type: "step_finish", sessionID, timestamp, part: { messageID: "completed", reason: "stop", tokens: { input: 10 }, cost: 0.01 } });
    expect(JSON.stringify(steps)).not.toContain("prompt");
    expect(JSON.stringify(steps)).not.toContain("providerState");
    expect(parseOpenCodeLog(jsonl(steps)).logEntries.at(-1)).toMatchObject({ type: "session.result", data: { numTurns: 1, usage: { input_tokens: 10, output_tokens: 2, cache_read_input_tokens: 5 }, totalCostUsd: 0.01 } });
  });

  it("includes native v2 reasoning in exported output without changing v1 artifact semantics", () => {
    const tokens = { input: 10, output: 2, reasoning: 7, cache: { read: 3, write: 0 } };
    const steps = getOpenCodeSessionSteps({ info: { id: sessionID }, messages: [{ type: "assistant", id: "reasoning", time: { completed: timestamp }, tokens }] }, sessionID);
    expect(steps[0].part.tokens).toEqual({ ...tokens, output: 9 });
    expect(tokens.output).toBe(2);
    const events = parseOpenCodeLog(jsonl(steps)).logEntries;
    expect(events.at(-1).data).toEqual({
      numTurns: 1,
      usage: { input_tokens: 10, output_tokens: 9, reasoning_output_tokens: 7, cache_read_input_tokens: 3, cache_creation_input_tokens: 0, input_tokens_include_cache: false },
    });
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
    expect(unified.at(-1).data.usage).toMatchObject({ outputTokens: 9, reasoningOutputTokens: 7 });
    expect(sessionTokenTotal(unified.at(-1).data.usage)).toBe(22);
    const native = parseOpenCodeLog(jsonl([record("step_finish", { tokens })])).logEntries;
    expect(native.at(-1).data.usage).toMatchObject({ output_tokens: 2, reasoning_output_tokens: 7 });
    expect(sessionTokenTotal(native.at(-1).data.usage)).toBe(15);
  });

  it("recovers compaction accounting without assistant turns or sensitive export fields", () => {
    const sensitive = "private-native-export-data";
    const steps = getOpenCodeSessionSteps(
      {
        info: { id: sessionID },
        messages: [
          { type: "user", text: sensitive },
          { type: "assistant", id: "assistant", time: { completed: timestamp }, tokens: { input: 30, output: 2, reasoning: 1, cache: { read: 0, write: 0 }, providerState: sensitive }, cost: 0.03, providerState: sensitive },
          {
            type: "compaction",
            id: "compaction",
            status: "completed",
            time: { created: new Date(timestamp + 1).toISOString() },
            tokens: { input: 100, output: 3, reasoning: 4, cache: { read: 5, write: 6, private: sensitive }, private: sensitive },
            cost: 0.1,
            summary: sensitive,
            recent: sensitive,
            providerState: { secret: sensitive },
            providerContext: sensitive,
            metadata: { secret: sensitive },
          },
          { type: "compaction", id: "running", status: "running", tokens: { input: 999 }, cost: 99, summary: sensitive },
        ],
      },
      sessionID
    );
    expect(steps).toHaveLength(2);
    expect(steps.filter(step => step.type === "step_finish")).toHaveLength(1);
    expect(steps[1]).toEqual({
      type: "opencode.compaction",
      timestamp: timestamp + 1,
      sessionID,
      part: { id: "gh_aw_export_compaction", sessionID, messageID: "compaction", tokens: { input: 100, output: 7, reasoning: 4, cache: { read: 5, write: 6 } }, cost: 0.1 },
    });
    expect(JSON.stringify(steps)).not.toContain(sensitive);
    expect(isOpenCodeEvent(steps[1])).toBe(true);
    const result = parseOpenCodeLog(jsonl(steps));
    expect(result.logEntries.filter(event => event.type === "opencode.step_finish")).toHaveLength(1);
    expect(result.logEntries.filter(event => event.type === "opencode.compaction")).toHaveLength(1);
    expect(result.logEntries.at(-1).data).toEqual({
      numTurns: 1,
      totalCostUsd: 0.13,
      usage: { input_tokens: 130, output_tokens: 10, reasoning_output_tokens: 5, cache_read_input_tokens: 5, cache_creation_input_tokens: 6, input_tokens_include_cache: false },
    });
    expect(JSON.stringify(result)).not.toContain(sensitive);
    expect(parseEngineSession(jsonl(steps), "opencode")).toEqual(result.logEntries);
    expect(parseCustomLog(jsonl(steps)).logEntries).toEqual(result.logEntries);
    expect(parseOpenCodeLog(jsonl(result.logEntries)).logEntries).toEqual(result.logEntries);
    expect(sessionTokenTotal(mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: result.logEntries }]).at(-1).data.usage)).toBe(151);
  });

  it.each(["completed", "failed"])("accounts for settled %s compactions alone without inventing model turns", status => {
    const steps = getOpenCodeSessionSteps(
      { info: { id: sessionID }, messages: [{ type: "compaction", id: "settled", status, time: { created: timestamp }, tokens: { input: 100, output: 0, reasoning: 0 }, cost: 0.1, error: "private error" }] },
      sessionID
    );
    expect(steps).toHaveLength(1);
    expect(steps[0].type).toBe("opencode.compaction");
    const events = parseOpenCodeLog(jsonl(steps)).logEntries;
    expect(events.at(-1).data).toEqual({ totalCostUsd: 0.1, usage: { input_tokens: 100, output_tokens: 0, reasoning_output_tokens: 0, input_tokens_include_cache: false } });
    expect(events.some(event => event.type === "opencode.step_finish" || event.type.startsWith("assistant."))).toBe(false);
    expect(JSON.stringify(events)).not.toContain("private error");
  });

  it("reconciles replayed compaction accounting once per message and independently per session", () => {
    const [compaction] = getOpenCodeSessionSteps(
      { info: { id: sessionID }, messages: [{ type: "compaction", id: "replayed", status: "completed", time: { created: timestamp }, tokens: { input: 100, output: 2, reasoning: 3 }, cost: 0.1 }] },
      sessionID
    );
    const replay = { ...compaction, timestamp: timestamp + 1, part: { ...compaction.part, id: "fresh_part", tokens: { input: 110 }, cost: 0.2 } };
    const events = parseOpenCodeLog(jsonl([compaction, compaction, replay, replay])).logEntries;
    expect(events.filter(event => event.type === "opencode.compaction")).toHaveLength(2);
    expect(events.at(-1).data).toEqual({ totalCostUsd: 0.2, usage: { input_tokens: 110, output_tokens: 5, reasoning_output_tokens: 3, input_tokens_include_cache: false } });
    expect(parseOpenCodeLog(jsonl(events)).logEntries).toEqual(events);
    const otherSession = parseOpenCodeLog(jsonl([compaction, { ...compaction, sessionID: "other_session" }])).logEntries;
    expect(otherSession.at(-1).data).toEqual({ totalCostUsd: 0.2, usage: { input_tokens: 200, output_tokens: 10, reasoning_output_tokens: 6, input_tokens_include_cache: false } });
  });

  it.each(["id", "partID"])("reconciles assistant usage by message across fresh %s projections and export recovery", idField => {
    const first = record("step_finish", { [idField]: "live_first", messageID: "msg_first", tokens: { input: 10, output: 2, reasoning: 0 }, cost: 0.01 });
    const replay = { ...first, timestamp: timestamp + 1, part: { ...first.part, [idField]: "live_replayed" } };
    const exported = getOpenCodeSessionSteps(
      {
        info: { id: sessionID },
        messages: [
          { type: "assistant", id: "msg_first", time: { completed: timestamp + 2 }, tokens: { input: 10, output: 2, reasoning: 0 }, cost: 0.01 },
          { type: "assistant", id: "msg_second", time: { completed: timestamp + 3 }, tokens: { input: 20, output: 3, reasoning: 0 }, cost: 0.02 },
        ],
      },
      sessionID
    );
    const events = parseOpenCodeLog(jsonl([first, replay, ...exported, ...exported])).logEntries;
    expect(events.at(-1).data).toEqual({ numTurns: 2, totalCostUsd: 0.03, usage: { input_tokens: 30, output_tokens: 5, reasoning_output_tokens: 0, input_tokens_include_cache: false } });
    expect(events.filter(event => event.type === "opencode.step_finish")).toHaveLength(4);
    expect(parseOpenCodeLog(jsonl(events)).logEntries).toEqual(events);
  });

  it("falls back to part identity for assistant accounting without message IDs", () => {
    const first = record("step_finish", { id: "first", tokens: { input: 10 }, cost: 0.01 });
    delete first.part.messageID;
    const replay = { ...first, timestamp: timestamp + 1, part: { ...first.part, tokens: { input: 20 }, cost: 0.02 } };
    const second = { ...first, part: { ...first.part, id: "second" } };
    const events = parseOpenCodeLog(jsonl([first, replay, second])).logEntries;
    expect(events.at(-1).data).toEqual({ numTurns: 2, totalCostUsd: 0.03, usage: { input_tokens: 30, input_tokens_include_cache: false } });
  });

  it("projects only finite accounting fields and does not publish malformed token payloads", () => {
    const steps = getOpenCodeSessionSteps(
      {
        info: { id: sessionID },
        messages: [
          {
            type: "assistant",
            id: "invalid",
            time: { completed: timestamp },
            finish: { secret: "private" },
            tokens: { input: -1, output: 2, reasoning: Number.MAX_SAFE_INTEGER, cache: { read: "private", write: 0 }, private: "private" },
            cost: { secret: "private" },
          },
          { type: "assistant", id: "array", time: { completed: timestamp }, tokens: ["private"], cost: -1 },
        ],
      },
      sessionID
    );
    expect(steps[0].part.tokens).toEqual({ reasoning: Number.MAX_SAFE_INTEGER, cache: { write: 0 } });
    expect(steps[0].part).not.toHaveProperty("reason");
    expect(steps[0].part).not.toHaveProperty("cost");
    expect(steps[1].part).not.toHaveProperty("tokens");
    expect(steps[1].part).not.toHaveProperty("cost");
    expect(JSON.stringify(steps)).not.toContain("private");
    expect(parseOpenCodeLog(jsonl(steps)).logEntries.at(-1).data.usage).not.toHaveProperty("output_tokens");
  });

  it("rejects invalid v2 session exports instead of inventing completion metadata", () => {
    for (const exported of [
      null,
      {},
      { info: { id: "other" }, messages: [] },
      { info: { id: sessionID }, messages: null },
      { info: { id: sessionID }, messages: [{ type: "assistant", id: "bad", time: { completed: "not-a-date" } }] },
      { info: { id: sessionID }, messages: [{ type: "compaction", id: "bad", status: "completed" }] },
      { info: { id: sessionID }, messages: [{ type: "compaction", status: "failed", time: { created: timestamp } }] },
    ]) {
      expect(() => getOpenCodeSessionSteps(exported, sessionID)).toThrow("OpenCode session export");
    }
  });

  it.each([0, 2])("correlates v2 tools using partID and id, retaining nested exit metadata: %s", exit => {
    const first = {
      type: "tool_use",
      timestamp,
      sessionID,
      part: {
        partID: "prt_v2_first",
        id: "call_v2",
        sessionID,
        messageID: "msg_v2",
        type: "tool",
        tool: "shell",
        state: {
          status: "completed",
          input: { command: "make build" },
          output: "build output\n",
          metadata: { metadata: { exit }, content: [{ type: "text", text: "build output\n" }], rawInput: '{"command":"make build"}' },
          time: { start: timestamp, end: timestamp + 30 },
        },
      },
    };
    const replay = { ...first, part: { ...first.part, partID: "prt_v2_replay" } };
    const content = jsonl([first, replay, record("step_finish", { reason: "stop", tokens: { input: 10, output: 2, reasoning: 0, cache: { read: 5, write: 0 } } })]);
    const result = parseOpenCodeLog(content);
    const starts = result.logEntries.filter(event => event.type === "tool.execution_start");
    const completions = result.logEntries.filter(event => event.type === "tool.execution_complete");
    expect(starts).toHaveLength(1);
    expect(starts[0]).toMatchObject({ id: "prt_v2_first", parentId: "msg_v2", data: { toolCallId: "call_v2", toolName: "shell" } });
    expect(completions).toHaveLength(1);
    expect(completions[0]).toMatchObject({ id: "prt_v2_first", data: { toolCallId: "call_v2", exitCode: exit, success: exit === 0, output: "build output\n", durationMs: 30, metadata: first.part.state.metadata } });
    expect(parseEngineSession(content, "opencode")).toEqual(result.logEntries);
    expect(parseCustomLog(content).logEntries).toEqual(result.logEntries);
    expect(parseOpenCodeLog(jsonl(result.logEntries)).logEntries).toEqual(result.logEntries);
  });

  it("retains v2 failed tool errors and does not confuse the tool call ID with its part ID", () => {
    const tool = record("tool_use", { partID: "prt_v2_error", id: "call_v2_error", tool: "github_search", state: { status: "error", input: {}, error: "Request rejected", metadata: { rawInput: "{}" } } });
    const result = parseOpenCodeLog(jsonl([tool]));
    expect(result.logEntries.find(event => event.type === "tool.execution_complete")).toMatchObject({ id: "prt_v2_error", data: { toolCallId: "call_v2_error", success: false, error: "Request rejected" } });
    expect(result.logEntries.find(event => event.type === "tool.execution_complete").data).not.toHaveProperty("exitCode");
  });

  it("recognizes only structured v2 MCP connection failure diagnostics", () => {
    const prefix = "timestamp=2026-10-18T15:00:00.000Z level=WARN fiber=#1";
    expect(getOpenCodeMCPFailures(`${prefix} message="mcp connect failed" server=github status="{\\"status\\":\\"failed\\"}"`)).toEqual(["github"]);
    expect(getOpenCodeMCPFailures(`${prefix} message="mcp connect failed" server="server with spaces" status=failed`)).toEqual(["server with spaces"]);
    for (const content of [
      "",
      "mcp connect failed server=github",
      `${prefix} message="mcp connected" server=github status=failed`,
      `${prefix} message="mcp connect failed" status=failed`,
      `${prefix} message="mcp connect failed" server=github`,
      `${prefix} message="mcp connect failed" server=github status=connecting`,
      `${prefix} message="mcp connect failed" server=github status="{\\"status\\":\\"connected\\"}"`,
      `${prefix} message="mcp connect failed" server=github status="{\\"error\\":\\"failed\\"}"`,
      `${prefix} message="mcp connect failed" server=github status="not json"`,
      `${prefix} message="mcp connect failed" server=github status=failed malformed`,
    ])
      expect(getOpenCodeMCPFailures(content)).toEqual([]);
  });

  it("does not poison a recovered MCP startup with a transient v2 warning", () => {
    const prefix = "timestamp=2026-10-18T15:00:00.000Z level=WARN fiber=#1";
    const warning = `${prefix} message="mcp connect failed" server=github`;
    const recovered = `${prefix.replace("level=WARN", "level=INFO")} message="mcp connected" server=github status=connected`;
    const content = `${warning}\n${recovered}\n${jsonl([record("step_finish", { reason: "stop", tokens: { input: 10 } })])}`;
    expect(parseOpenCodeLog(content).mcpFailures).toEqual([]);
    expect(parseOpenCodeLog(content).logEntries.at(-1).data).not.toHaveProperty("status");
    expect(getOpenCodeMCPFailures(`${warning.replace("level=WARN", "level=ERROR")}\n${recovered}`)).toEqual([]);
    expect(parseOpenCodeLog(`${warning} status="{\\"status\\":\\"failed\\"}"\n${content}`).mcpFailures).toEqual(["github"]);
  });

  it("recognizes pinned v2 flattened MCP failure status without matching transient status or error prose", () => {
    const failure = String.raw`timestamp=2026-10-18T17:22:46.585Z level=WARN run=fixture message="mcp connect failed" server=fixture status.status=failed status.error="Error POSTing to endpoint: {\"error\":\"fixture denial\"} (HTTP 403)"`;
    expect(getOpenCodeMCPFailures(failure)).toEqual(["fixture"]);
    expect(parseOpenCodeLog(`${failure}\n${failure}`).mcpFailures).toEqual(["fixture"]);
    expect(getOpenCodeMCPFailures(failure.replace("level=WARN", "level=ERROR"))).toEqual(["fixture"]);
    for (const transient of [
      failure.replace("status.status=failed", "status.status=connecting"),
      failure.replace("status.status=failed", "status.status=connected"),
      failure.replace("status.status=failed ", ""),
      failure.replace("status.status=failed", "status.status=connecting").replace("fixture denial", "failed"),
    ])
      expect(getOpenCodeMCPFailures(transient)).toEqual([]);
  });

  it("preserves text, reasoning, tools, errors, timestamps and per-step usage", () => {
    const records = [
      record("step_start", { type: "step-start" }),
      record("reasoning", { type: "reasoning", text: "Consider the repository." }),
      record("tool_use", { type: "tool", callID: "call_fixture", tool: "bash", state: { status: "completed", input: { command: "pwd" }, output: "/workspace\n", time: { start: timestamp + 10, end: timestamp + 40 } } }),
      record("step_finish", { type: "step-finish", reason: "tool-calls", cost: 0.01, tokens: { input: 10, output: 5, reasoning: 2, cache: { read: 20, write: 3 } } }),
      record("text", { type: "text", text: "Finished.\n" }, { timestamp: timestamp + 50 }),
      record("step_finish", { id: "prt_finish_2", messageID: "msg_second", type: "step-finish", reason: "stop", cost: 0, tokens: { input: 4, output: 2, reasoning: 0, cache: { read: 0, write: 0 } } }, { timestamp: timestamp + 60 }),
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
    const result = parseOpenCodeLog(
      jsonl([record("step_finish", { tokens: { input: Number.MAX_SAFE_INTEGER, output: -1, cache: { read: "12" } } }), record("step_finish", { id: "prt_other", messageID: "msg_other", tokens: { input: 1, output: 2 } })])
    );
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
      record("step_finish", { id: "first", messageID: "msg_first", tokens: { total: Number.MAX_SAFE_INTEGER, reasoning: Number.MAX_SAFE_INTEGER } }),
      record("step_finish", { id: "second", messageID: "msg_second", tokens: { total: 1, reasoning: 1 } }),
      record("step_finish", { id: "third", messageID: "msg_third", tokens: { total: 2, reasoning: 2 } }),
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
    expect(unified[0].data).toEqual({ error: "AI_APICallError: Too Many Requests", message: "stream error", sessionId: "ses_ee1e84b56ffeap38OrBZh2U7qY" });
    expect(result.logEntries[0].data).toEqual({
      timestamp: "2026-10-09T00:37:13.776Z",
      level: "ERROR",
      run: "ab01f139",
      message: "stream error",
      providerID: "awf-proxy",
      modelID: "auto",
      "session.id": "ses_ee1e84b56ffeap38OrBZh2U7qY",
      small: "false",
      agent: "build",
      mode: "primary",
      "error.error": "AI_APICallError: Too Many Requests",
      sessionId: "ses_ee1e84b56ffeap38OrBZh2U7qY",
      error: "AI_APICallError: Too Many Requests",
    });
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
    const truncatedFence = `\`\`\`log\n(truncated output)\n${native}`;
    expect(parseOpenCodeLog(truncatedFence).logEntries).toMatchObject([{ type: "session.error", data: { message: "stream error" } }]);
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
    const final = { ...first, timestamp: timestamp + 1, part: { ...first.part, id: "prt_revised", text: "  first\n\tlast \n", partial: false } };
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
    const reversed = parseOpenCodeLog(jsonl([finish, text])).logEntries;
    expect(reversed.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(reversed.find(event => event.type === "assistant.refusal").data).toEqual({ reason, content: "  partial\n", partial: true });
    expect(reversed.some(event => event.type === "assistant.message")).toBe(false);
    const other = record("text", { text: "Unrelated.", messageID: "other-message" });
    expect(parseOpenCodeLog(jsonl([other, finish])).logEntries.find(event => event.type === "assistant.message").data.content).toBe("Unrelated.");
    expect(parseOpenCodeLog(jsonl([text, record("step_finish", { reason: "length" })])).logEntries.some(event => event.type === "assistant.refusal")).toBe(false);
  });

  it("deduplicates changed tool snapshots and recovers input unavailable at the first observation", () => {
    const pending = record("tool_use", { callID: "stable_call", tool: "example", state: { status: "running" } });
    const completed = { ...pending, timestamp: timestamp + 1, part: { ...pending.part, id: "prt_completed", callID: "stable_call", state: { status: "completed", input: false, output: "", result: { content: [] }, metadata: { exit: 0 } } } };
    const updated = { ...completed, timestamp: timestamp + 2, part: { ...completed.part, id: "prt_updated", state: { ...completed.part.state, output: "Updated.\n", metadata: { exit: 2 } } } };
    const events = parseOpenCodeLog(jsonl([pending, completed, updated])).logEntries;
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(events.find(event => event.type === "tool.execution_start").data.input).toBe(false);
    const completions = events.filter(event => event.type === "tool.execution_complete");
    expect(completions).toHaveLength(1);
    expect(completions[0].data).toMatchObject({ output: "Updated.\n", result: { content: [] }, success: false, exitCode: 2 });
    expect(completions[0].nativeSnapshots).toEqual([completed, updated]);
  });

  it("clears stale tool error and exit metadata when an updated snapshot omits them", () => {
    const failed = record("tool_use", { callID: "call_revision", tool: "example", state: { status: "error", error: "old failure", metadata: { exit: 2 } } });
    const recovered = { ...failed, timestamp: timestamp + 1, part: { ...failed.part, id: "prt_recovered", state: { status: "completed", output: "ok" } } };
    const completions = parseOpenCodeLog(jsonl([failed, recovered])).logEntries.filter(event => event.type === "tool.execution_complete");
    expect(completions).toHaveLength(1);
    expect(completions[0].data).toMatchObject({ success: false, output: "ok" });
    expect(completions[0].data).not.toHaveProperty("error");
    expect(completions[0].data).not.toHaveProperty("exitCode");
    expect(completions[0].data).not.toHaveProperty("metadata");
  });

  it("omits overflowing costs instead of retaining a misleading partial subtotal", () => {
    const events = parseOpenCodeLog(
      jsonl([
        record("step_finish", { id: "cost-1", messageID: "msg_first", cost: Number.MAX_VALUE }),
        record("step_finish", { id: "cost-2", messageID: "msg_second", cost: Number.MAX_VALUE }),
        record("step_finish", { id: "cost-3", messageID: "msg_third", cost: 1 }),
      ])
    ).logEntries;
    expect(events.at(-1).data).not.toHaveProperty("totalCostUsd");
    expect(events.at(-1).data.numTurns).toBe(3);
  });

  it("persists recovered errors and exact canonical content through the existing unified writer", () => {
    const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "opencode-session-"));
    try {
      const tool = record("tool_use", { callID: "persisted_tool", tool: "example", state: { status: "completed", output: "plain output", result: { content: [{ type: "json", value: 1 }] } } });
      const events = parseOpenCodeLog(`${opencodeCiStreamErrors[0]}\n${jsonl([...opencodeCiExcerpt, tool])}`).logEntries;
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
      expect(persisted.find(event => event.type === "tool.execution_complete" && event.data.toolCallId === "persisted_tool").data).toMatchObject({
        output: "plain output",
        result: { content: [{ type: "json", value: 1 }] },
      });
      const firstWrite = fs.readFileSync(outputPath, "utf8");
      writeUnifiedSession({ rootDir, engine: "opencode", outputPath, warn });
      expect(fs.readFileSync(outputPath, "utf8")).toBe(firstWrite);
      expect(warnings).toEqual([]);
    } finally {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  });
});
