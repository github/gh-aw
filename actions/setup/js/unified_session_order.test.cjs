import { describe, expect, it } from "vitest";
import { mergeSessionSources, sessionTimestamp } from "./unified_session.cjs";
import { sessionTimestampNs } from "./unified_session_order.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const event = (id, timestamp) => ({ id, type: "vendor.observation", data: {}, ...(timestamp !== undefined ? { timestamp } : {}) });
const source = (component, events, timestampUnit = "milliseconds") => ({ component, phase: "agent", path: `${component}.jsonl`, timestampUnit, events });
const header = { type: "session.format", data: { version: 1 }, provenance: { component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 } };
const jsonl = events => [header, ...events].map(JSON.stringify).join("\n") + "\n";

describe("Unified trace ordering", () => {
  it("interleaves agent, AWF, MCPG and GitHub API events without moving untimed lifecycle observations to the tail", () => {
    const sources = [
      source("agent", [event("init"), event("start", 1000), event("tool"), event("answer", 3000), event("done")]),
      source("firewall", [event("access", 1.5), event("tracker"), event("usage", 2.5)], "seconds"),
      source("mcp", [event("request", 2000), event("response"), event("end", 4000)]),
      source("github_api", [event("retry", 2250), event("rate-limit", 3500)]),
      source("workflow", [event("metadata")]),
    ];
    const original = structuredClone(sources);
    const events = mergeSessionSources(sources);
    expect(events.map(item => item.id)).toEqual(["init", "start", "tool", "access", "tracker", "request", "response", "retry", "usage", "answer", "done", "rate-limit", "end", "metadata"]);
    for (const id of ["init", "tool", "tracker", "response", "done", "metadata"]) {
      const untimed = events.find(item => item.id === id);
      expect(untimed).not.toHaveProperty("timestamp");
      expect(untimed.provenance).not.toHaveProperty("timestampMs");
    }
    expect(sources).toEqual(original);
    expect(mergeSessionSources(sources)).toEqual(events);
    const summary = { type: "session.collection", data: { sources: sources.map(({ events, ...rest }) => ({ ...rest, events: events.length })), warnings: 0, untimedEvents: 6, absentComponents: [] }, provenance: header.provenance };
    expect(validateSession(jsonl([...events, summary]))).toBe(events.length + 2);
  });

  it("preserves source order across clock regressions and invalid clocks without rewriting timestamps", () => {
    const events = mergeSessionSources([source("agent", [event("start", 3000), event("invalid", "invalid"), event("complete", 1000), event("later", 5000)]), source("mcp", [event("rpc", 2000), event("reply", 4000)])]);
    expect(events.map(item => item.id)).toEqual(["rpc", "start", "invalid", "complete", "reply", "later"]);
    expect(events.find(item => item.id === "complete")).toMatchObject({ timestamp: 1000, provenance: { timestampMs: 1000, index: 2 } });
    expect(validateSession(jsonl(events))).toBe(7);
    const swapped = [...events];
    [swapped[1], swapped[3]] = [swapped[3], swapped[1]];
    expect(() => validateSession(jsonl(swapped))).toThrow("source ordered");
  });

  it("retains deterministic equal-time and fully untimed source order", () => {
    const sources = [source("agent", [event("a", 0), event("b"), event("c", 0)]), source("mcp", [event("d", 0)]), source("workflow", [event("e"), event("f")]), source("usage", [event("g")])];
    expect(mergeSessionSources(sources).map(item => item.id)).toEqual(["a", "b", "c", "d", "e", "f", "g"]);
  });

  it("merges OTLP nanoseconds and ISO fractional seconds without losing sub-millisecond ordering", () => {
    const events = mergeSessionSources([
      source("agent", [event("agent", "2026-10-02T00:00:00.000000002Z")]),
      source("otel", [event("exported-later", "1790899200000000003"), event("exported-first", "1790899200000000001")], "nanoseconds"),
      source("mcp", [event("gateway", 1790899200000)]),
    ]);
    expect(events.map(item => item.id)).toEqual(["gateway", "exported-first", "agent", "exported-later"]);
    expect(events.find(item => item.id === "exported-first").timestamp).toBe("1790899200000000001");
    expect(validateSession(jsonl(events))).toBe(5);
    const swapped = [...events];
    [swapped[1], swapped[3]] = [swapped[3], swapped[1]];
    expect(() => validateSession(jsonl(swapped))).toThrow("timestamp ordered");
  });

  it("validates schema-defined clocks and preserves nanosecond precision", () => {
    expect(sessionTimestampNs({ timestamp: "1790899200123456789" }, "nanoseconds")).toBe(1790899200123456789n);
    expect(sessionTimestampNs({ timestamp: "2026-10-02T00:00:00.123456789+00:00" })).toBe(1790899200123456789n);
    expect(sessionTimestampNs({ timestamp: -1.25 })).toBe(-1250000n);
    expect(sessionTimestamp({ timestamp: "0" }, "nanoseconds")).toBe(0);
    for (const timestamp of ["", "-1", "1.5", "NaN", "8640000000000000000001", 1790899200000000000]) {
      expect(sessionTimestamp({ timestamp }, "nanoseconds")).toBeUndefined();
    }
  });
});
