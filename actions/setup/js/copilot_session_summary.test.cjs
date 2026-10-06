import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { normalizeCopilotSession } = require("./copilot_session.cjs");
const { parseCopilotLog } = require("./parse_copilot_log.cjs");

describe("Copilot final completion summary", () => {
  const summary = "  Completed the task.\n\n- Preserved the first result.\n- Preserved the second result.\n";
  const completion = (data = {}, metadata = {}) => ({
    type: "session.task_complete",
    id: "completion",
    parentId: "parent",
    timestamp: "2026-10-05T14:54:21.575Z",
    data: { summary, success: true, ...data },
    ...metadata,
  });
  const answers = events => events.filter(event => event.type === "assistant.message" && event.copilotProjection === "session.task_complete");

  it("projects only the proven final summary and preserves exact text, original native event and metadata", () => {
    const event = completion({ detail: { retained: true } }, { nativeFlag: false, provenance: { component: "agent", phase: "agent", path: "copilot-session-state/session/events.jsonl", index: 10 } });
    const original = structuredClone(event);
    const events = normalizeCopilotSession([event]);

    expect(event).toEqual(original);
    expect(events.find(entry => entry.type === "session.task_complete")).toEqual(original);
    expect(answers(events)).toHaveLength(1);
    expect(answers(events)[0]).toMatchObject({
      id: original.id,
      parentId: original.parentId,
      timestamp: original.timestamp,
      nativeFlag: false,
      provenance: original.provenance,
      data: { summary, content: summary, success: true, detail: { retained: true } },
    });
    expect(parseCopilotLog(JSON.stringify([event])).markdown).toContain("Preserved the second result.");
  });

  it.each([true, false])("does not repeat an identical native assistant answer (assistant first: %j)", assistantFirst => {
    const assistant = { type: "assistant.message", data: { content: summary } };
    const source = assistantFirst ? [assistant, completion()] : [completion(), assistant];
    const events = normalizeCopilotSession(source);
    expect(answers(events)).toHaveLength(0);
    expect(events.filter(event => event.type === "assistant.message")).toEqual([assistant]);
    expect(events.filter(event => event.type === "session.task_complete")).toHaveLength(1);
  });

  it.each([{}, { id: undefined }, { id: "different" }])("deduplicates repeated completion summaries while retaining all native observations (%j)", metadata => {
    const events = normalizeCopilotSession([completion(), completion({}, metadata)]);
    expect(events.filter(event => event.type === "session.task_complete")).toHaveLength(2);
    expect(answers(events)).toHaveLength(1);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("deduplicates persisted projection snapshots and remains stable across reordered records and repeated normalization", () => {
    const normalized = normalizeCopilotSession([completion()]);
    const projected = answers(normalized)[0];
    const repeated = normalizeCopilotSession([...normalized, ...structuredClone(normalized)]);
    expect(answers(repeated)).toHaveLength(1);
    expect(repeated.filter(event => event.type === "session.task_complete")).toHaveLength(2);
    expect(normalizeCopilotSession(repeated)).toEqual(repeated);
    const interleaved = [projected, completion()];
    expect(normalizeCopilotSession(interleaved)).toEqual(interleaved);
  });

  it("removes a redundant managed summary projection when an exact native answer snapshot is available", () => {
    const normalized = normalizeCopilotSession([completion()]);
    const assistant = { type: "assistant.message", id: "native-answer", data: { content: summary } };
    const events = normalizeCopilotSession([...normalized, assistant]);
    expect(answers(events)).toHaveLength(0);
    expect(events.filter(event => event.type === "assistant.message")).toEqual([assistant]);
  });

  it("does not repeat a source-proven answer already emitted through message deltas", () => {
    const source = [{ type: "assistant.message_delta", data: { messageId: "answer", deltaContent: summary.slice(0, 20) } }, { type: "assistant.message_delta", data: { messageId: "answer", deltaContent: summary.slice(20) } }, completion()];
    const events = normalizeCopilotSession(source);
    expect(answers(events)).toHaveLength(0);
    expect(
      events
        .filter(event => event.type === "assistant.message")
        .map(event => event.data.content)
        .join("")
    ).toBe(summary);
    expect(normalizeCopilotSession(events)).toEqual(events);
  });

  it("does not concatenate unrelated message deltas to suppress a final summary", () => {
    const events = normalizeCopilotSession([
      { type: "assistant.message_delta", data: { messageId: "first", deltaContent: summary.slice(0, 20) } },
      { type: "assistant.message_delta", data: { messageId: "last", deltaContent: summary.slice(20) } },
      completion(),
    ]);
    expect(answers(events)).toHaveLength(1);
    expect(answers(events)[0].data.content).toBe(summary);
  });

  it("does not collapse identical final answers from independent native source scopes", () => {
    const first = completion({}, { provenance: { phase: "agent", path: "session-first/events.jsonl", index: 1 } });
    const last = completion({}, { provenance: { phase: "agent", path: "session-last/events.jsonl", index: 1 } });
    const events = normalizeCopilotSession([first, last]);
    expect(answers(events)).toHaveLength(2);
    expect(answers(events).map(event => event.provenance.path)).toEqual(["session-first/events.jsonl", "session-last/events.jsonl"]);
    expect(normalizeCopilotSession(events)).toEqual(events);
  });

  it("does not collapse independent session IDs when their records have no source metadata", () => {
    const events = normalizeCopilotSession([{ type: "session.start", data: { sessionId: "first" } }, completion(), { type: "session.start", data: { sessionId: "last" } }, completion({}, { id: "last-completion" })]);
    expect(answers(events)).toHaveLength(2);
    expect(normalizeCopilotSession(events)).toEqual(events);
  });

  it("preserves significant whitespace and distinct summary corrections", () => {
    const events = normalizeCopilotSession([{ type: "assistant.message", data: { content: summary.trim() } }, completion(), completion({ summary: `${summary}Correction.\n` }, { id: "correction" })]);
    expect(answers(events).map(event => event.data.content)).toEqual([summary, `${summary}Correction.\n`]);
  });

  it.each([undefined, null, false, 42, {}, "", " \n\t"])("does not invent an answer from an unavailable summary (%j)", unavailable => {
    expect(answers(normalizeCopilotSession([completion({ summary: unavailable })]))).toHaveLength(0);
  });

  it("never treats arbitrary tool outputs or unknown extension summaries as final answers", () => {
    const events = normalizeCopilotSession([
      { type: "tool.execution_complete", data: { toolName: "task_complete", result: { content: summary }, success: true } },
      { type: "vendor.extension", data: { summary } },
    ]);
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(0);
  });

  it("preserves a long multiline completion answer without changing reasoning or tool evidence", () => {
    const longSummary = `${summary}${"Observed source-proven detail.\n".repeat(25)}`;
    const source = [
      ...Array.from({ length: 3 }, (_, index) => ({ type: "assistant.message", id: `message-${index}`, data: { content: "", reasoningText: `Reasoning ${index}.` } })),
      ...Array.from({ length: 6 }, (_, index) => [
        { type: "tool.execution_start", data: { toolCallId: `tool-${index}`, toolName: "bash", arguments: { command: `echo ${index}` } } },
        { type: "tool.execution_complete", data: { toolCallId: `tool-${index}`, success: true, result: { content: `result ${index}` } } },
      ]).flat(),
      completion({ summary: longSummary }),
    ];
    const events = normalizeCopilotSession(source);
    expect(answers(events)[0].data.content).toBe(longSummary);
    expect(events.filter(event => event.type === "assistant.reasoning")).toHaveLength(3);
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(6);
    expect(events.filter(event => event.type === "tool.execution_complete")).toHaveLength(6);
    expect(normalizeCopilotSession(events)).toEqual(events);
  });
});
