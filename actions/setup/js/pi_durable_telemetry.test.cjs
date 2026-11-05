// @ts-check
"use strict";

import { afterEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const otlp = require("./send_otlp_span.cjs");
const { createDurableTelemetry, exportDurableTelemetry } = require("./pi_durable_telemetry.cjs");

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
});

describe("durable OTel journal and host export", () => {
  it("uses TRACEPARENT and journals sanitized native spans without sending from the sandbox", async () => {
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "");
    vi.stubEnv("GITHUB_AW_OTEL_PARENT_SPAN_ID", "");
    vi.stubEnv("TRACEPARENT", "00-11111111111111111111111111111111-2222222222222222-01");
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "existsSync").mockReturnValue(false);
    const append = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
    const send = vi.spyOn(otlp, "sendOTLPToAllEndpoints").mockResolvedValue(undefined);
    const telemetry = createDurableTelemetry();
    await telemetry.telemetry.startSpan({ name: "durable.run", attributes: { "auth.token": "private-value", "durable.resumed": true } }, async span => {
      span.addEvent("checkpoint", { "auth.token": "private-event-value" });
      span.addLink({ context: telemetry.telemetry.propagation.extract({ traceparent: "00-33333333333333333333333333333333-4444444444444444-01" }), attributes: { "auth.token": "private-link-value" } });
    });
    await telemetry.flush();
    expect(append).toHaveBeenCalledTimes(1);
    expect(append.mock.calls[0][0]).toBe("/tmp/gh-aw/agent/durable-otel.jsonl");
    const payload = JSON.parse(append.mock.calls[0][1]);
    const span = payload.resourceSpans[0].scopeSpans[0].spans[0];
    expect(span.traceId).toBe("11111111111111111111111111111111");
    expect(span.parentSpanId).toBe("2222222222222222");
    expect(span.links[0].spanId).toBe("4444444444444444");
    expect(JSON.stringify(payload)).not.toContain("private-");
    expect(send).not.toHaveBeenCalled();
  });

  it("exports the journal from the trusted host and appends the ordinary OTel mirror", async () => {
    const payload = otlp.buildOTLPPayload({ traceId: "1".repeat(32), spanId: "2".repeat(16), spanName: "durable.run", startMs: 1, endMs: 2, attributes: [] });
    vi.spyOn(fs, "existsSync").mockReturnValue(true);
    vi.spyOn(fs, "lstatSync").mockReturnValue({ isFile: () => true });
    vi.spyOn(fs, "readFileSync").mockReturnValue(JSON.stringify(payload) + "\n");
    vi.spyOn(otlp, "parseOTLPEndpoints").mockReturnValue([{ url: "https://collector.example/v1/traces" }]);
    const mirror = vi.spyOn(otlp, "appendToOTLPJSONL").mockImplementation(() => {});
    const send = vi.spyOn(otlp, "sendOTLPToAllEndpoints").mockResolvedValue(undefined);
    await exportDurableTelemetry();
    expect(mirror).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith([{ url: "https://collector.example/v1/traces" }], expect.objectContaining({ resourceSpans: expect.any(Array) }), { skipJSONL: true });
  });

  it("rejects a nonregular journal before reading or exporting it", async () => {
    vi.spyOn(fs, "existsSync").mockReturnValue(true);
    vi.spyOn(fs, "lstatSync").mockReturnValue({ isFile: () => false });
    await expect(exportDurableTelemetry()).rejects.toThrow("regular file");
  });
});
