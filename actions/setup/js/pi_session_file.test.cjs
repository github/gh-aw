import { describe, expect, it } from "vitest";
import { parsePiLog } from "./parse_pi_log.cjs";
import { normalizeAgentSession, selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";
import { mergeSessionSources, parseEngineSession } from "./unified_session.cjs";

const parse = records => parsePiLog(records.map(JSON.stringify).join("\n"));
const entry = (type, id, parentId, payload) => ({ type, id, parentId, timestamp: "2026-10-08T00:00:01.000Z", ...payload });
const assistant = (text, usage = { input: 2, output: 1 }) => ({ role: "assistant", timestamp: 1000, responseId: "shared-response", model: "physical-model", content: [{ type: "text", text }], usage });

describe("Pi persisted session files", () => {
  it.each([1, 2, 3, undefined])("recognizes a version %s session without migrating or fabricating tree metadata", version => {
    const records = [
      { type: "session", ...(version === undefined ? {} : { version }), id: "custom-session-id", cwd: "/project", timestamp: "2026-10-08T00:00:00.000Z", parentSession: "/original.jsonl" },
      { type: "message", message: { role: "user", content: "PRIVATE_USER_PROMPT", timestamp: 1000 } },
      { type: "message", message: assistant(" answer\n ") },
    ];
    const original = structuredClone(records);
    const { logEntries, markdown } = parse(records);
    expect(logEntries.find(event => event.type === "session.init")).toMatchObject({ id: "custom-session-id", data: { sourceEngine: "pi", sessionId: "custom-session-id", cwd: "/project" } });
    const message = logEntries.find(event => event.type === "assistant.message");
    expect(message.data.content).toBe(" answer\n ");
    expect(message.id).toBeUndefined();
    expect(message.parentId).toBeUndefined();
    expect(message.timestamp).toBeUndefined();
    expect(selectSessionResult(logEntries)).toMatchObject({ numTurns: 1, usage: { input_tokens: 2, output_tokens: 1 } });
    expect(markdown).not.toContain("PRIVATE_USER_PROMPT");
    expect(normalizeAgentSession(logEntries)).toEqual(logEntries);
    expect(parse(logEntries).logEntries).toEqual(logEntries);
    expect(records).toEqual(original);
  });

  it("keeps sibling branches and multiple roots, even with identical response timestamps and tool IDs", () => {
    const records = [
      entry("message", "root", null, { message: { role: "user", content: "request", timestamp: 1000 } }),
      entry("message", "left", "root", { message: assistant("same\n") }),
      entry("message", "left-tool", "left", { message: { role: "toolResult", toolCallId: "call", toolName: "bash", content: [], isError: false, usage: { input: 3 } } }),
      entry("message", "right", "root", { message: assistant("same\n") }),
      entry("message", "right-tool", "right", { message: { role: "toolResult", toolCallId: "call", toolName: "bash", content: [], isError: true, usage: { input: 5 } } }),
      entry("message", "new-root", null, { message: assistant("new root\n") }),
    ];
    const { logEntries } = parse(records);
    expect(logEntries.filter(event => event.type === "assistant.message").map(event => [event.id, event.parentId, event.data.content])).toEqual([
      ["left", "root", "same\n"],
      ["right", "root", "same\n"],
      ["new-root", null, "new root\n"],
    ]);
    expect(logEntries.filter(event => event.type === "tool.execution_complete").map(event => event.data.success)).toEqual([true, false]);
    expect(selectSessionResult(logEntries)).toMatchObject({ numTurns: 3, usage: { input_tokens: 14, output_tokens: 3 } });
  });

  it("maps reasoning, exact tool inputs/results, image content, and provider failures", () => {
    const image = { type: "image", data: "AA==", mimeType: "image/png" };
    const records = [
      entry("message", "user", null, { message: { role: "user", content: [{ type: "text", text: "" }, image] } }),
      entry("message", "answer", "user", {
        message: {
          ...assistant("unused"),
          content: [
            { type: "thinking", thinking: " reason\n", thinkingSignature: "opaque" },
            { type: "toolCall", id: "call", name: "bash", arguments: false, namespace: "tools" },
            { type: "thinking", redacted: true, thinkingSignature: "encrypted" },
          ],
          stopReason: "error",
          errorMessage: "provider failure",
          thinkingLevel: "high",
        },
      }),
      entry("message", "result", "answer", { message: { role: "toolResult", toolCallId: "call", toolName: "bash", content: [image], details: { exitCode: 2 }, isError: false } }),
    ];
    const { logEntries } = parse(records);
    expect(logEntries.find(event => event.type === "user.message").data.content).toEqual(records[0].message.content);
    expect(logEntries.find(event => event.type === "assistant.reasoning")).toMatchObject({ id: "answer", parentId: "user", data: { content: " reason\n", thinkingSignature: "opaque" } });
    expect(logEntries.find(event => event.type === "tool.execution_start")).toMatchObject({ id: "answer", data: { toolCallId: "call", toolName: "bash", input: false, namespace: "tools" } });
    expect(logEntries.find(event => event.type === "tool.execution_complete")).toMatchObject({ id: "result", data: { output: [image], success: false, isError: false } });
    expect(logEntries.find(event => event.type === "pi.message_content").data.block).toEqual(records[1].message.content[2]);
    expect(selectSessionResult(logEntries).errors).toEqual(["provider failure"]);
  });

  it("retains tree controls and extension data without applying context edits to history", () => {
    const records = [
      entry("message", "answer", null, { message: assistant("original\n") }),
      entry("model_change", "model", "answer", { provider: "anthropic", modelId: "virtual-model" }),
      entry("thinking_level_change", "thinking", "model", { thinkingLevel: "high" }),
      entry("compaction", "compact", "thinking", { summary: "earlier context", firstKeptEntryId: "answer", tokensBefore: 100, systemMessage: { role: "system", content: "PRIVATE_CHECKPOINT" } }),
      entry("context_edit", "edit", "compact", { targetId: "answer", replacement: null }),
      entry("branch_summary", "branch", "answer", { fromId: "edit", summary: "abandoned path" }),
      entry("custom", "custom", "branch", { customType: "pi.virtual-model-state", data: { provider: "anthropic", modelId: "virtual-model", state: false } }),
      entry("custom_message", "context", "custom", { customType: "extension", content: "PRIVATE_EXTENSION", display: false, details: { native: true } }),
      entry("label", "label", "context", { targetId: "answer", label: "" }),
      entry("session_info", "info", "label", { name: "Session name" }),
      entry("future_entry", "future", "info", { unknown: { value: 0 } }),
    ];
    const { logEntries, markdown } = parse(records);
    expect(logEntries.find(event => event.type === "assistant.message").data.content).toBe("original\n");
    for (const record of records.slice(1)) {
      const { type, id, parentId, timestamp, ...data } = record;
      expect(logEntries.find(event => event.type === `pi.${type}`)).toEqual({ type: `pi.${type}`, id, parentId, timestamp, ...data, data });
    }
    expect(markdown).not.toContain("PRIVATE_CHECKPOINT");
    expect(markdown).not.toContain("PRIVATE_EXTENSION");
    expect(selectSessionResult(logEntries).numTurns).toBe(1);
  });

  it("retains structured system prompt patches, legacy/custom roles, and direct bash as native observations", () => {
    const messages = [
      { role: "system", content: "", sections: { preamble: "PRIVATE_SYSTEM", skills: null }, toolsAdded: [{ name: "read", parameters: {} }], toolsRemoved: [{ name: "write" }], timestamp: 0 },
      { role: "system", content: "PRIVATE_PLAIN_SYSTEM", timestamp: 1 },
      { role: "hookMessage", content: "PRIVATE_LEGACY_CONTEXT", customType: "legacy", display: false },
      { role: "custom", content: "PRIVATE_CUSTOM_CONTEXT", customType: "extension", display: true },
      { role: "bashExecution", command: "echo ok", output: "ok\n", exitCode: 0, cancelled: false, excludeFromContext: true },
      { role: "futureRole", content: false },
    ];
    const { logEntries, markdown } = parse(messages.map((message, index) => entry("message", `message-${index}`, null, { message })));
    expect(logEntries.find(event => event.type === "pi.system_message").data).toEqual(messages[0]);
    expect(logEntries.find(event => event.type === "prompt.system").data.content).toBe("PRIVATE_PLAIN_SYSTEM");
    expect(logEntries.find(event => event.type === "pi.bash_execution").data).toEqual(messages[4]);
    expect(logEntries.filter(event => event.type === "tool.execution_start" || event.type === "tool.execution_complete")).toEqual([]);
    expect(logEntries.filter(event => event.type === "pi.message").map(event => event.data)).toEqual([messages[2], messages[3], messages[5]]);
    expect(markdown).not.toContain("PRIVATE_");
    expect(selectSessionResult(logEntries)).toBeUndefined();
  });

  it("accounts for all persisted usage kinds and summaries without adding reasoning or cache subsets twice", () => {
    const usage = { input: 10, output: 3, reasoning: 2, cacheRead: 4, cacheWrite: 2, cacheWrite1h: 1, totalTokens: 19, cost: { total: 0.1 } };
    const records = [
      entry("message", "answer", null, { message: assistant("answer", usage) }),
      entry("usage", "warm", "answer", { kind: "future_operation", provider: "anthropic", model: "physical-model", usage }),
      entry("compaction", "compact", "warm", { firstKeptEntryId: "compact", summary: "summary", tokensBefore: 100, usage }),
      entry("branch_summary", "branch", null, { fromId: "compact", summary: "branch summary", usage }),
    ];
    const result = selectSessionResult(parse(records).logEntries);
    expect(result).toMatchObject({ numTurns: 1, usage: { input_tokens: 40, output_tokens: 12, reasoning_output_tokens: 8, cache_read_input_tokens: 16, cache_creation_input_tokens: 8, total_tokens: 76, input_tokens_include_cache: false } });
    expect(result.totalCostUsd).toBeCloseTo(0.4);
    expect(sessionTokenTotal(result.usage)).toBe(76);
    expect(result.durationMs).toBeUndefined();
    const auxiliary = selectSessionResult(parse(records.slice(1)).logEntries);
    expect(auxiliary.numTurns).toBeUndefined();
    expect(auxiliary.usage.total_tokens).toBe(57);
  });

  it("counts repeated identified reports once without collapsing anonymous persisted messages or usage", () => {
    const answer = entry("message", "answer", null, { message: assistant("same", { input: 2, output: 1, cost: { total: 0 } }) });
    const tool = entry("message", "tool", "answer", { message: { role: "toolResult", toolCallId: "call", content: [], isError: false, usage: { input: 3 } } });
    const usage = entry("usage", "usage", "tool", { kind: "cache_warm", usage: { cacheRead: 5 } });
    const records = [
      answer,
      tool,
      usage,
      structuredClone(answer),
      structuredClone(tool),
      structuredClone(usage),
      { type: "message", message: assistant("same", { input: 7 }) },
      { type: "message", message: assistant("same", { input: 7 }) },
      { type: "usage", kind: "anonymous", usage: { input: 11 } },
      { type: "usage", kind: "anonymous", usage: { input: 11 } },
    ];
    expect(selectSessionResult(parse(records).logEntries)).toMatchObject({ numTurns: 3, totalCostUsd: 0, usage: { input_tokens: 41, output_tokens: 1, cache_read_input_tokens: 5 } });
  });

  it("projects persisted native evidence into unified events without losing tree or replay metadata", () => {
    const records = [
      { type: "session", version: 3, id: "session", parentSession: "/parent.jsonl" },
      entry("message", "system", null, { message: { role: "system", content: "", sections: { preamble: "system" }, toolsAdded: [] } }),
      entry("message", "answer", "system", { message: assistant("answer\n", { input: 2, output: 3, reasoning: 1, cost: { total: 0 } }) }),
      entry("context_edit", "edit", "answer", { targetId: "answer", replacement: "" }),
      entry("custom", "state", "edit", { customType: "extension", data: { empty: null } }),
    ];
    const parsed = parseEngineSession(records.map(JSON.stringify).join("\n"), "pi");
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "native-session.jsonl", events: parsed }]);
    expect(unified.find(event => event.type === "assistant.message")).toMatchObject({
      id: "answer",
      parentId: "system",
      timestamp: records[2].timestamp,
      data: { content: "answer\n" },
      provenance: { path: "native-session.jsonl", timestampMs: Date.parse(records[2].timestamp) },
    });
    expect(unified.find(event => event.type === "pi.system_message").data).toEqual(records[1].message);
    expect(unified.find(event => event.type === "pi.session_metadata").data).toEqual({ version: 3, parentSession: "/parent.jsonl" });
    expect(unified.find(event => event.type === "pi.context_edit").data).toEqual({ targetId: "answer", replacement: "" });
    expect(unified.find(event => event.type === "pi.custom").data).toEqual({ customType: "extension", data: { empty: null } });
    expect(unified.find(event => event.type === "pi.message_metadata").data).toMatchObject({ model: "physical-model", usage: { input: 2, output: 3, reasoning: 1 } });
    expect(unified.find(event => event.type === "session.result").data).toMatchObject({ usage: { inputTokens: 2, outputTokens: 3, reasoningOutputTokens: 1 }, totalCostUsd: 0 });
  });

  it("recognizes partial tree records without a header and tolerates malformed neighboring lines", () => {
    const future = entry("future_entry", "future", null, { opaque: false });
    const answer = entry("message", "answer", "future", { message: assistant("retained") });
    const { logEntries } = parsePiLog([JSON.stringify(future), '{"type":"message",', "null", JSON.stringify(answer)].join("\n"));
    expect(logEntries.find(event => event.type === "pi.future_entry")).toMatchObject({ id: "future", parentId: null, data: { opaque: false } });
    expect(logEntries.find(event => event.type === "assistant.message")).toMatchObject({ id: "answer", parentId: "future", data: { content: "retained" } });
    expect(parse([future]).logEntries).toHaveLength(1);
  });

  it("keeps flat and streaming records with tree-like metadata on their existing adapters", () => {
    const records = [
      entry("assistant", "flat", null, { content: "flat\n" }),
      entry("message_end", "stream", "flat", { message: assistant("stream\n") }),
      entry("tool_execution_end", "completion", "stream", { toolCallId: "orphan", toolName: "bash", result: false, isError: false }),
    ];
    const { logEntries } = parse(records);
    expect(logEntries.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["flat\n", "stream\n"]);
    expect(logEntries.find(event => event.type === "tool.execution_complete")).toMatchObject({ id: "completion", parentId: "stream", data: { toolCallId: "orphan", output: false, success: true } });
  });
});
