import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, writeUnifiedSession } from "./unified_session.cjs";
import { normalizeOtelEvents } from "./unified_session_otel.cjs";
import { validateSession } from "./scripts/validate_session.cjs";
import { generatePlainTextSummary } from "./log_parser_shared.cjs";

const traceId = "0123456789abcdef0123456789abcdef";
const spanId = "0123456789abcdef";
const payload = {
  resourceSpans: [
    {
      resource: { attributes: [{ key: "service.name", value: { stringValue: "gh-aw" } }] },
      schemaUrl: "resource-schema",
      scopeSpans: [
        {
          scope: { name: "gh-aw", version: "1" },
          schemaUrl: "scope-schema",
          spans: [
            {
              traceId,
              spanId,
              parentSpanId: "abcdef0123456789",
              name: "github.api.run",
              startTimeUnixNano: "1790899200000000001",
              endTimeUnixNano: "1790899200000000009",
              status: { code: 2, message: "failed" },
              attributes: [{ key: "count", value: { intValue: "0" } }],
              events: [{ timeUnixNano: "1790899200000000004", name: "retry", attributes: [{ key: "retry", value: { boolValue: false } }] }],
              links: [{ traceId, spanId }],
            },
            { traceId, spanId: "abcdef0123456789", name: "setup", startTimeUnixNano: "1790899200000000000" },
          ],
        },
      ],
    },
  ],
  resourceLogs: [
    {
      resource: { attributes: [] },
      scopeLogs: [
        {
          scope: { name: "logger" },
          logRecords: [
            { traceId, spanId, timeUnixNano: "1790899200000000003", severityNumber: 9, severityText: "INFO", body: { stringValue: "  first line\nsecond line  " } },
            { observedTimeUnixNano: "1790899200000000005", body: { kvlistValue: { values: [{ key: "flag", value: { boolValue: false } }] } } },
            { body: { stringValue: "" } },
          ],
        },
      ],
    },
  ],
};

describe("Unified OTel observations", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-unified-otel-"));
  });
  afterEach(() => {
    fs.rmSync(root, { recursive: true, force: true });
  });
  const write = (root, file, entries) => {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, entries.map(JSON.stringify).join("\n") + "\n");
  };

  it("expands spans, span events and log messages with context, observed clocks and exact payloads", () => {
    const original = structuredClone(payload);
    const events = normalizeOtelEvents(payload, code => {
      throw new Error(code);
    });
    expect(events).toHaveLength(6);
    expect(events[0]).toMatchObject({
      type: "otel.span",
      timestamp: "1790899200000000001",
      data: {
        traceId,
        spanId,
        parentSpanId: "abcdef0123456789",
        name: "github.api.run",
        resource: payload.resourceSpans[0].resource,
        scope: { name: "gh-aw", version: "1" },
        resourceSchemaUrl: "resource-schema",
        scopeSchemaUrl: "scope-schema",
        links: [{ traceId, spanId }],
      },
    });
    expect(events[0].data).not.toHaveProperty("events");
    expect(events[1]).toMatchObject({ type: "otel.span_event", timestamp: "1790899200000000004", data: { traceId, spanId, name: "retry" } });
    expect(events[3].data.body).toEqual(payload.resourceLogs[0].scopeLogs[0].logRecords[0].body);
    expect(events[4].timestamp).toBe("1790899200000000005");
    expect(events[5]).not.toHaveProperty("timestamp");
    expect(payload).toEqual(original);
  });

  it("imports and persists a single authoritative mirror interleaved with agent, AWF, MCPG and API records", () => {
    write(root, "otel.jsonl", [payload]);
    write(root, "usage/otel.jsonl", [payload]);
    write(root, "agent-session.jsonl", [{ type: "assistant.message", timestamp: "2026-10-02T00:00:00.000000002Z", data: { content: "answer" } }]);
    write(root, "mcp-logs/rpc.jsonl", [{ event: "REQUEST", timestamp: 1790899200000, server_name: "github" }]);
    write(root, "sandbox/firewall/logs/audit.jsonl", [{ ts: 1790899200, event: "http_access", status: 200 }]);
    write(root, "github_rate_limits.jsonl", [{ ts: 1790899200000, remaining: 0 }]);
    const events = writeUnifiedSession({
      rootDir: root,
      warn: code => {
        throw new Error(code);
      },
    });
    expect(events.slice(1, 10).map(event => event.data.name ?? event.type)).toEqual(["setup", "mcp.rpc.request", "firewall.http_access", "github_api.rate_limit", "github.api.run", "assistant.message", "otel.log", "retry", "otel.log"]);
    expect(events.filter(event => event.provenance.component === "otel")).toHaveLength(6);
    expect(events.at(-1).data.sources.filter(source => source.component === "otel")).toEqual([{ component: "otel", phase: "agent", path: "otel.jsonl", events: 6, timestampUnit: "nanoseconds" }]);
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(validateSession(content)).toBe(events.length);
    expect(content).toContain("first line\\nsecond line");
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
    const summary = generatePlainTextSummary(events);
    expect(summary).toContain("name=github.api.run");
    expect(summary).toContain(`traceId=${traceId}`);
    expect(summary).toContain("severityText=INFO");
    expect(summary).not.toContain("first line");
  });

  it("uses the copied mirror only when the primary is absent, including separate detection evidence", () => {
    write(root, "usage/otel.jsonl", [payload]);
    write(root, "threat-detection/otel.jsonl", [{ resourceLogs: payload.resourceLogs }]);
    const events = collectUnifiedSession({ rootDir: root }).events;
    expect(events.filter(event => event.provenance.component === "otel")).toHaveLength(9);
    expect(events.some(event => event.provenance.path === "usage/otel.jsonl")).toBe(true);
    expect(events.filter(event => event.provenance.phase === "detection")).toHaveLength(3);
    fs.writeFileSync(path.join(root, "otel.jsonl"), "");
    expect(collectUnifiedSession({ rootDir: root }).events.filter(event => event.provenance.component === "otel")).toHaveLength(3);
  });

  it("reports malformed nested records and JSONL while retaining adjacent valid messages", () => {
    write(root, "otel.jsonl", [
      { resourceSpans: [null, { scopeSpans: [{ spans: [false, { name: "valid", events: [null, { name: "event" }] }] }] }] },
      { resourceLogs: [{ scopeLogs: "invalid" }] },
      { unsupported: true },
      { resourceLogs: payload.resourceLogs },
    ]);
    fs.appendFileSync(path.join(root, "otel.jsonl"), "{broken\n");
    const warnings = [];
    const events = collectUnifiedSession({ rootDir: root, warn: warning => warnings.push(warning) }).events;
    expect(events.filter(event => event.provenance.component === "otel")).toHaveLength(5);
    expect(warnings).toHaveLength(6);
    expect(events.at(-1).data.warnings).toBe(6);
    expect(events.at(-1).data.untimedEvents).toBe(3);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toEqual(["malformed_jsonl", "malformed_otlp", "malformed_otlp", "malformed_otlp", "malformed_otlp", "unrecognized_otlp"]);
  });

  it("does not follow a symlinked mirror", () => {
    write(root, "private.jsonl", [payload]);
    fs.symlinkSync(path.join(root, "private.jsonl"), path.join(root, "otel.jsonl"));
    const warnings = [];
    const events = collectUnifiedSession({ rootDir: root, warn: warning => warnings.push(warning) }).events;
    expect(events.some(event => event.provenance.component === "otel")).toBe(false);
    expect(warnings).toEqual([expect.stringContaining("symlink_not_read")]);
  });
});
