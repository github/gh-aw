import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseGeminiLog } from "./parse_gemini_log.cjs";
import { normalizeGeminiSession } from "./gemini_session.cjs";
import { writeSessionArtifact } from "./session_artifact.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { writeUnifiedSession } from "./unified_session.cjs";
const success = require("./fixtures/gemini_ci_lifecycle.cjs");
const { spendingCapSeptember29, spendingCapSeptember27 } = require("./fixtures/gemini_ci_sessions.cjs");

const jsonl = records => records.map(record => JSON.stringify(record)).join("\n") + "\n";
const readEvents = file => fs.readFileSync(file, "utf8").trimEnd().split("\n").map(JSON.parse);

describe("Gemini run-backed session publication", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gemini-session-pipeline-"));
  });
  afterEach(() => {
    fs.rmSync(root, { recursive: true, force: true });
  });

  // These historical runs did not publish either session file. The fixtures
  // retain their native observations; persistence and collection run locally.
  it.each([
    ["36078916290", success],
    ["36504829912", spendingCapSeptember29],
    ["36283760088", spendingCapSeptember27],
  ])("preserves run %s observations through both artifacts without reparsing or duplicating", (_run, source) => {
    const parsed = parseGeminiLog(jsonl(source)).logEntries;
    const canonicalPath = path.join(root, "agent-session.jsonl");
    writeSessionArtifact(canonicalPath, parsed);
    const canonical = readEvents(canonicalPath);
    expect(canonical).toEqual(JSON.parse(JSON.stringify(parsed)));
    expect(normalizeGeminiSession(canonical)).toEqual(canonical);

    fs.writeFileSync(path.join(root, "agent-stdio.log"), jsonl(source));
    const merged = writeUnifiedSession({ rootDir: root, engine: "gemini", dailyAIC: {} });
    const publishedPath = path.join(root, "usage/aw_session.jsonl");
    const published = readEvents(publishedPath);
    expect(published).toEqual(JSON.parse(JSON.stringify(merged)));
    const agent = published.filter(event => event.provenance.component === "agent" && event.type !== "agent.execution");
    expect(agent.map(event => event.type)).toEqual(canonical.map(event => event.type));
    expect(agent).toHaveLength(source.length);
    for (const [index, event] of agent.entries()) {
      expect(event.timestamp).toBe(source[index].timestamp);
      expect(event.provenance).toMatchObject({ component: "agent", phase: "agent", path: "agent-session.jsonl", index });
      if (source[index].type === "message") expect(event.data.content).toBe(source[index].content);
      if (source[index].type === "tool_use") expect(event.data).toMatchObject({ toolCallId: source[index].tool_id, toolName: source[index].tool_name, input: source[index].parameters });
      if (source[index].type === "tool_result") expect(event.data).toMatchObject({ toolCallId: source[index].tool_id, output: source[index].output, success: source[index].status === "success" });
    }
    const result = agent.at(-1);
    expect(result.data).toMatchObject({
      sourceType: "result",
      status: source.at(-1).status,
      durationMs: source.at(-1).stats.duration_ms,
      usage: {
        totalTokens: source.at(-1).stats.total_tokens,
        inputTokens: source.at(-1).stats.input_tokens,
        outputTokens: source.at(-1).stats.output_tokens,
        cacheReadInputTokens: source.at(-1).stats.cached,
        inputTokensIncludeCache: true,
      },
    });
    if (source.at(-1).error) expect(result.data.errors).toEqual([source.at(-1).error]);
    expect(result.data).not.toHaveProperty("numTurns");
    expect(result.data).not.toHaveProperty("totalCostUsd");
    expect(result.data.usage).not.toHaveProperty("reasoningOutputTokens");
    expect(result.data).not.toHaveProperty("stats");
    const original = fs.readFileSync(publishedPath, "utf8");
    writeUnifiedSession({ rootDir: root, engine: "gemini", dailyAIC: {} });
    expect(fs.readFileSync(publishedPath, "utf8")).toBe(original);

    fs.unlinkSync(canonicalPath);
    const reconstructed = writeUnifiedSession({ rootDir: root, engine: "gemini", dailyAIC: {} });
    expect(reconstructed.filter(event => event.provenance.component === "agent" && event.type !== "agent.execution").map(event => event.data)).toEqual(agent.map(event => event.data));
  });
});

// Supplemental compatibility shapes below are synthetic, not run observations.
describe("Gemini compatibility channel and accounting gaps", () => {
  it("uses standard error observations while retaining diagnostic structures, retries, and native extensions", () => {
    const error = { code: 0, message: "", retryable: false };
    const records = [
      { type: "error", id: "attempt-1", parentId: null, timestamp: 0, error, native: { retained: false } },
      { type: "error", id: "attempt-2", parentId: null, timestamp: 1, error },
      { type: "error", severity: "warning", message: "Retry warning", timestamp: 2 },
      { type: "gemini.error", id: "old-native", data: { message: "Opaque compatibility event", nested: [false, null, 0] } },
    ];
    const events = normalizeGeminiSession(records);
    expect(events.map(event => event.type)).toEqual(["session.error", "session.result", "session.error", "session.result", "session.error", "gemini.error"]);
    for (const [index, sourceIndex] of [
      [0, 0],
      [1, 0],
      [2, 1],
      [3, 1],
    ]) {
      expect(events[index]).toMatchObject({ id: records[sourceIndex].id, parentId: null, timestamp: records[sourceIndex].timestamp, data: { sourceType: "error", error } });
    }
    expect(events[1].data.errors).toEqual([error]);
    expect(events[3].data.errors).toEqual([error]);
    expect(events[4].data).toMatchObject({ severity: "warning", message: "Retry warning" });
    expect(events[4].data).not.toHaveProperty("errors");
    expect(events.at(-1)).toEqual(records.at(-1));
    expect(normalizeGeminiSession(events)).toEqual(events);
  });

  it.each([
    { type: "assistant.message", data: { content: "", refusal: "  Declined.\n", partial: false } },
    { type: "message", role: "assistant", content: "", refusal: "  Declined.\n" },
    { type: "assistant", message: { content: [{ type: "refusal", refusal: "  Declined.\n" }] } },
    { type: "assistant", message: { content: [{ type: "text", text: "  Declined.\n" }], stop_reason: "refusal", stop_details: { category: null, explanation: "" } } },
  ])("maps a supported structured refusal to one core observation: %j", record => {
    const source = { ...record, id: "native-record", parentId: null, timestamp: 0, native: { value: false } };
    const original = structuredClone(source);
    const events = normalizeGeminiSession([source]);
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "assistant.refusal", id: "native-record", parentId: null, timestamp: 0, native: source.native, data: { reason: "refusal", content: "  Declined.\n" } });
    if (record.data?.partial === false) expect(events[0].data.partial).toBe(false);
    if (record.message?.stop_details) expect(events[0].data).toMatchObject({ policyCategory: null, explanation: "" });
    expect(normalizeUnifiedSessionEvent(events[0])).toMatchObject({ type: "assistant.refusal", id: "native-record", parentId: null, timestamp: 0, data: { reason: "refusal", content: "  Declined.\n" } });
    expect(normalizeGeminiSession(events)).toEqual(events);
    events[0].native.value = true;
    expect(source).toEqual(original);
  });

  it("retains streamed refusal text and an identified final snapshot without duplicated content", () => {
    const events = normalizeGeminiSession([
      { type: "message", role: "assistant", message_id: "refusal", delta: true, refusal: "  No" },
      { type: "message", role: "assistant", message_id: "refusal", delta: true, refusal: ".\n", timestamp: 1 },
      { type: "message", role: "assistant", message_id: "refusal", refusal: "  No.\n", timestamp: 2 },
    ]);
    expect(events.map(event => event.type)).toEqual(["assistant.refusal", "assistant.refusal"]);
    expect(
      events
        .filter(event => event.type === "assistant.refusal")
        .map(event => event.data.content)
        .join("")
    ).toBe("  No.\n");
    expect(events.slice(0, 2).every(event => event.data.partial === true)).toBe(true);
    expect(events.at(-1).data.observations.at(-1).timestamp).toBe(2);
    expect(normalizeGeminiSession(events)).toEqual(events);
  });

  it("retains reasoning and tool blocks around a structured refusal in source order", () => {
    const events = normalizeGeminiSession([
      {
        type: "assistant",
        id: "mixed-refusal",
        timestamp: 0,
        message: {
          content: [
            { type: "thinking", thinking: "  Considering.\n" },
            { type: "refusal", refusal: "  Declined.\n" },
            { type: "tool_use", id: "call", name: "lookup", input: false },
          ],
        },
      },
    ]);
    expect(events.map(event => event.type)).toEqual(["assistant.reasoning", "assistant.refusal", "tool.execution_start"]);
    expect(events[0].data.content).toBe("  Considering.\n");
    expect(events[1].data.content).toBe("  Declined.\n");
    expect(events[2].data).toMatchObject({ toolCallId: "call", toolName: "lookup", input: false });
    expect(events.every(event => event.id === "mixed-refusal" && event.timestamp === 0)).toBe(true);
  });

  it.each([null, "", undefined])("retains content-filter evidence with content %j, not an answer or a session failure", content => {
    const record = { type: "assistant", message: { finish_reason: "content_filter", ...(content === undefined ? {} : { content }) } };
    const events = normalizeGeminiSession([record]);
    expect(events.map(event => event.type)).toEqual(["assistant.refusal"]);
    expect(events[0].data.reason).toBe("content_filter");
    if (content === undefined) expect(events[0].data).not.toHaveProperty("content");
    else expect(events[0].data.content).toBe(content);
  });

  it("does not classify user/tool content, permission errors, or natural-language disclaimers as refusals", () => {
    const events = normalizeGeminiSession([
      { type: "message", role: "user", content: "I cannot help.", refusal: "user metadata" },
      { type: "tool_result", tool_id: "call", output: { refusal: "tool payload" }, status: "success" },
      { type: "message", role: "assistant", content: "I cannot help." },
      { type: "result", status: "error", error: { type: "permission_denied", refusal: "diagnostic metadata" } },
    ]);
    expect(events.map(event => event.type)).toEqual(["user.message", "tool.execution_complete", "assistant.message", "session.result"]);
  });

  it("keeps reasoning text blocks in their own channel through streaming reconciliation and projection", () => {
    const records = [
      { type: "reasoning", message_id: "thought", delta: true, content: [{ type: "text", text: "  Think" }] },
      { type: "reasoning", message_id: "thought", content: [{ type: "text", text: "  Think.\n" }] },
      { type: "reasoning", message_id: "thought", content: [{ type: "text", text: "  Think.\n" }] },
      { type: "vendor.progress", id: "opaque", timestamp: 0, data: { nested: [false, null, 0] } },
    ];
    const events = parseGeminiLog(jsonl(records)).logEntries;
    expect(events.map(event => event.type)).toEqual(["assistant.reasoning", "assistant.reasoning", "vendor.progress"]);
    expect(events.filter(event => event.type === "assistant.reasoning").map(event => event.data.content)).toEqual(["  Think", ".\n"]);
    expect(events.map(normalizeUnifiedSessionEvent).at(-1)).toEqual(records.at(-1));
    expect(normalizeGeminiSession(events)).toEqual(events);
  });

  it("keeps exact snapshot history on authoritative core events, including empty arrays and flat replacements", () => {
    const records = [
      { type: "message", role: "assistant", message_id: "answer", id: "empty", timestamp: 0, content: [], native: { no: false } },
      { type: "message", role: "assistant", message_id: "answer", id: "repeat-empty", timestamp: 1, content: [], native: { zero: 0 } },
      { type: "message", role: "assistant", message_id: "answer", id: "flat", timestamp: 2, content: "", native: { nil: null } },
      { type: "message", role: "assistant", message_id: "answer", id: "repeat-flat", timestamp: 3, content: "" },
    ];
    const events = normalizeGeminiSession(records);
    expect(events.map(event => event.type)).toEqual(["assistant.message"]);
    expect(events[0]).toMatchObject({ id: "flat", timestamp: 2, data: { content: "", observations: records } });
    expect(normalizeUnifiedSessionEvent(events[0]).data).toEqual({ content: "", messageId: "answer" });
    expect(normalizeGeminiSession(events)).toEqual(events);
    expect(JSON.parse(JSON.stringify(events))[0].data.observations).toEqual(records);
  });

  it("maps supplied reasoning-token stats, preserves zero, and never derives them from total-token differences", () => {
    const events = normalizeGeminiSession([
      { type: "result", stats: { reasoning_output_tokens: 0, total_tokens: 0 }, usage: { reasoningOutputTokens: 9, totalTokens: 9, native: false } },
      { type: "result", stats: { total_tokens: 10, input_tokens: 3, output_tokens: 2 } },
    ]);
    expect(events[0].data.usage).toMatchObject({ reasoning_output_tokens: 0, total_tokens: 0, native: false });
    expect(events[0].data.sourceType).toBe("result");
    // The engine's authoritative stats also control the camelCase projection.
    expect(normalizeUnifiedSessionEvent(events[0]).data.usage).toMatchObject({ reasoningOutputTokens: 0, totalTokens: 0 });
    expect(events[1].data.usage).not.toHaveProperty("reasoning_output_tokens");
  });

  it.each([null, -1, 0.5, Number.MAX_SAFE_INTEGER + 1])("does not restore invalid reasoning/total stats from aliases: %j", invalid => {
    const event = normalizeGeminiSession([{ type: "result", stats: { reasoning_output_tokens: invalid, total_tokens: invalid }, usage: { reasoningOutputTokens: 9, totalTokens: 9 } }])[0];
    expect(event.data.usage).toBeUndefined();
    expect(normalizeUnifiedSessionEvent(event).data).not.toHaveProperty("usage");
    expect(event.stats).toEqual({ reasoning_output_tokens: invalid, total_tokens: invalid });
  });
});
