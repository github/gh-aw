import { describe, expect, it } from "vitest";
import { mergeSessionSources, sessionTimestamp } from "./unified_session.cjs";
import { orderSessionSources, sessionTimestampNs } from "./unified_session_order.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const event = (id, timestamp) => ({ id, type: "vendor.observation", data: {}, ...(timestamp !== undefined ? { timestamp } : {}) });
const source = (component, events, timestampUnit = "milliseconds") => ({ component, phase: "agent", path: `${component}.jsonl`, timestampUnit, events });
const header = { type: "session.format", data: { version: 1 }, provenance: { component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 } };
const jsonl = events => [header, ...events].map(JSON.stringify).join("\n") + "\n";

describe("Unified trace ordering", () => {
  it.each([0, 2000, "2026-11-09T00:27:23.729Z"])("orders saved Pi message metadata clocks without publishing the native envelope (%s)", timestamp => {
    const clock = typeof timestamp === "number" ? timestamp : Date.parse(timestamp);
    const saved = {
      type: "tool.execution_start",
      data: { toolCallId: "call", messageId: "message", model: "served-model", input: { nested: [false, 0] }, messageMetadata: { timestamp, provider: "fixture", usage: { input: 10 } } },
    };
    const original = structuredClone(saved);
    expect(sessionTimestamp(saved)).toBe(clock);
    const merged = mergeSessionSources([source("agent", [saved]), source("mcp", [event("before", clock - 1), event("after", clock + 1)])]);
    expect(merged.map(record => record.id ?? record.data.toolCallId)).toEqual(["before", "call", "after"]);
    expect(merged[1]).toMatchObject({ timestamp, data: { messageId: "message", model: "served-model" }, provenance: { timestampMs: clock, index: 0 } });
    expect(merged[1].data).not.toHaveProperty("messageMetadata");
    expect(validateSession(jsonl(merged))).toBe(4);
    expect(saved).toEqual(original);
    expect(mergeSessionSources([source("agent", [merged[1]])])[0].timestamp).toBe(timestamp);
  });

  it("does not overwrite an explicit source clock or replace invalid clocks with message metadata", () => {
    const saved = { type: "assistant.message", timestamp: 10, data: { content: "exact", messageMetadata: { timestamp: 20 } } };
    expect(sessionTimestamp(saved)).toBe(10);
    expect(mergeSessionSources([source("agent", [saved])])[0].timestamp).toBe(10);
    expect(sessionTimestamp({ ...saved, timestamp: "invalid" })).toBeUndefined();
    expect(sessionTimestamp({ type: "assistant.message", data: { content: "untimed" } })).toBeUndefined();
  });

  it.each([[], [source("agent", [])], [source("agent", []), source("otel", [], "nanoseconds")]].map(sources => [sources]))("accepts empty source collections without inventing observations: %j", sources => {
    expect(mergeSessionSources(sources)).toEqual([]);
  });

  it.each([1, 7, 42, 2026, 65535])("matches an independent head-selection merge for generated mixed traces (seed %i)", seed => {
    let state = seed;
    const random = limit => {
      state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
      return state % limit;
    };
    const queues = [];
    const sources = ["agent", "firewall", "mcp", "github_api", "otel", "workflow"].map((component, sourceIndex) => {
      const observations = Array.from({ length: 30 + random(20) }, (_, index) => {
        const kind = random(5);
        const time = component === "workflow" || kind === 0 || kind === 1 ? undefined : random(100);
        const timestamp = time === undefined ? (kind === 1 ? "invalid" : undefined) : component === "otel" ? String(time * 1000000) : component === "firewall" ? time / 1000 : time;
        return { event: { ...event("repeated-native-id", timestamp), data: { observation: `${sourceIndex}:${index}` } }, time, index };
      });
      let anchor = observations.find(record => record.time !== undefined)?.time;
      const queue = observations.map(record => {
        if (record.time !== undefined) anchor = anchor === undefined ? record.time : Math.max(anchor, record.time);
        return { ...record, key: component === "otel" ? record.time : anchor, sourceIndex };
      });
      if (component === "otel") queue.sort((left, right) => (left.key ?? Infinity) - (right.key ?? Infinity));
      queues.push(queue);
      return source(
        component,
        observations.map(record => record.event),
        component === "otel" ? "nanoseconds" : component === "firewall" ? "seconds" : "milliseconds"
      );
    });
    const original = structuredClone(sources);
    const expected = [];
    while (queues.some(queue => queue.length)) {
      let selected;
      for (const [index, queue] of queues.entries()) {
        if (queue.length && (selected === undefined || (queue[0].key ?? Infinity) < (queues[selected][0].key ?? Infinity))) selected = index;
      }
      expected.push(queues[selected].shift());
    }
    const merged = mergeSessionSources(sources);
    expect(merged.map(record => record.data.observation)).toEqual(expected.map(record => record.event.data.observation));
    expect(merged.map(record => record.provenance.index)).toEqual(expected.map(record => record.index));
    expect(merged.map(record => record.timestamp)).toEqual(expected.map(record => record.event.timestamp));
    expect(new Set(merged.map(record => record.data.observation)).size).toBe(expected.length);
    expect(sources).toEqual(original);
    expect(mergeSessionSources(sources)).toEqual(merged);
    const summary = {
      type: "session.collection",
      data: { sources: sources.map(({ events, ...rest }) => ({ ...rest, events: events.length })), warnings: 0, untimedEvents: expected.filter(record => record.time === undefined).length, absentComponents: [] },
      provenance: header.provenance,
    };
    expect(validateSession(jsonl([...merged, summary]))).toBe(expected.length + 2);
  });

  it("merges 32000 observations across 16 sources without losing payloads, indices, or nanosecond order", () => {
    const sourceCount = 16;
    const eventsPerSource = 2000;
    const start = 1790899200000000000n;
    const sources = Array.from({ length: sourceCount }, (_, sourceIndex) => {
      const events = Array.from({ length: eventsPerSource }, (_, index) => ({
        ...event("repeated-native-id", String(start + BigInt(index * sourceCount + sourceIndex))),
        data: { observation: index * sourceCount + sourceIndex, nested: { zero: 0, flag: false, text: "  retained\npayload  " } },
      })).reverse();
      return { ...source("otel", events, "nanoseconds"), path: `otel/source-${sourceIndex}.jsonl` };
    });
    const merged = mergeSessionSources(sources);
    const expected = Array.from({ length: sourceCount * eventsPerSource }, (_, index) => index);
    expect(merged.map(record => record.data.observation)).toEqual(expected);
    expect(merged.map(record => record.provenance.index)).toEqual(expected.map(index => eventsPerSource - 1 - Math.floor(index / sourceCount)));
    expect(merged.map(record => record.provenance.path)).toEqual(expected.map(index => `otel/source-${index % sourceCount}.jsonl`));
    expect(merged.every(record => record.timestamp === String(start + BigInt(record.data.observation)) && record.data.nested.text === "  retained\npayload  " && record.data.nested.flag === false)).toBe(true);
    expect(sources.every(item => item.events[0].data.observation > item.events.at(-1).data.observation)).toBe(true);
  });

  it("reads each observation clock once when computing anchors and sorting", () => {
    let reads = 0;
    const count = 10000;
    const events = Array.from({ length: count }, (_, index) => ({
      index,
      get timestamp() {
        reads++;
        return index % 5 === 0 ? undefined : count - index;
      },
    }));
    const ordered = orderSessionSources([source("agent", events)]);
    expect(reads).toBe(count);
    expect(ordered.map(record => record.index)).toEqual(Array.from({ length: count }, (_, index) => index));
  });

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
    expect(sessionTimestampNs({ timestamp: "2026-02-29T19:00:00.123456789Z" })).toBeUndefined();
    expect(sessionTimestamp({ timestamp: "0" }, "nanoseconds")).toBe(0);
    for (const timestamp of ["", "-1", "1.5", "NaN", "8640000000000000000001", 1790899200000000000]) {
      expect(sessionTimestamp({ timestamp }, "nanoseconds")).toBeUndefined();
    }
  });
});
