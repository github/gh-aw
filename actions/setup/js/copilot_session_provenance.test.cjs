import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { normalizeCopilotSession } = require("./copilot_session.cjs");

describe("Copilot canonical projection provenance", () => {
  const provenance = {
    component: "agent",
    phase: "agent",
    path: "sandbox/agent/logs/copilot-session-state/session/events.jsonl",
    native: { component: "agent", phase: "agent", path: "native/session/events.jsonl", sourceTag: "observed" },
  };
  const native = [
    { type: "session.start", id: "start", timestamp: "2026-10-03T18:23:23.178Z", data: { sessionId: "session", selectedModel: "gpt-5" } },
    { type: "assistant.turn_end", id: "turn", data: { turnId: "0" } },
    { type: "session.error", id: "error", timestamp: "2026-10-03T18:23:23.540Z", data: { errorType: "authentication", message: "Observed error." } },
    {
      type: "session.shutdown",
      id: "shutdown",
      timestamp: "2026-10-03T18:23:23.559Z",
      data: { sessionStartTime: Date.parse("2026-10-03T18:23:23.154Z"), modelMetrics: { "gpt-5": { usage: { inputTokens: 10, outputTokens: 2 } } } },
    },
    { type: "session.task_complete", id: "complete", data: { summary: "Observed final answer.\n", success: true } },
  ];
  const captured = () =>
    normalizeCopilotSession(native).map((event, index) => ({
      ...event,
      provenance: {
        ...structuredClone(provenance),
        index,
        timestampMs: index,
        persistedPath: index % 2 ? "projected-snapshot.jsonl" : "native-snapshot.jsonl",
        native: { ...provenance.native, index: index + 100, persistedPath: `capture-${index}.jsonl`, timestampMs: index + 1000 },
      },
    }));

  it("does not regenerate init, result or summary projections after positional capture metadata is assigned", () => {
    const canonical = captured();
    const original = structuredClone(canonical);
    const normalized = normalizeCopilotSession(canonical);

    expect(normalized).toEqual(original);
    expect(canonical).toEqual(original);
    expect(normalized.filter(event => event.type === "session.init")).toHaveLength(1);
    expect(normalized.filter(event => event.type === "session.result")).toHaveLength(3);
    expect(normalized.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(normalized)))).toEqual(JSON.parse(JSON.stringify(normalized)));
  });

  it("matches indexed projection snapshots when native and projected records are interleaved", () => {
    const canonical = captured();
    const interleaved = [...canonical.filter(event => !event.copilotProjection), ...canonical.filter(event => event.copilotProjection)];
    expect(normalizeCopilotSession(interleaved)).toEqual(interleaved);
  });

  it("deduplicates repeated identified observations without dropping their original provenance", () => {
    const first = { ...native[0], provenance: { ...structuredClone(provenance), index: 1, persistedPath: "first.jsonl" } };
    const last = { ...native[0], provenance: { ...structuredClone(provenance), index: 9, persistedPath: "last.jsonl" } };
    const normalized = normalizeCopilotSession([first, last]);
    expect(normalized.filter(event => event.type === "session.start").map(event => event.provenance.index)).toEqual([1, 9]);
    expect(normalized.filter(event => event.type === "session.init")).toHaveLength(1);
    expect(normalized.find(event => event.type === "session.init").provenance).toEqual(first.provenance);
  });

  it.each([
    { path: "different/session/events.jsonl" },
    { phase: "detection" },
    { component: "different" },
    { native: { ...provenance.native, path: "different/native/events.jsonl" } },
    { native: { ...provenance.native, sourceTag: "different observation" } },
  ])("retains meaningful source identity instead of matching a projection from another origin (%j)", changed => {
    const canonical = captured();
    const start = canonical.find(event => event.type === "session.start");
    const init = canonical.find(event => event.type === "session.init");
    const other = { ...init, provenance: { ...init.provenance, ...changed } };
    const normalized = normalizeCopilotSession([start, other]);

    expect(normalized.filter(event => event.type === "session.init")).toHaveLength(2);
    expect(normalized.find(event => event.type === "session.init" && event.provenance.path === start.provenance.path && event.provenance.phase === start.provenance.phase)).toBeDefined();
    expect(normalized).toContainEqual(other);
  });
});
