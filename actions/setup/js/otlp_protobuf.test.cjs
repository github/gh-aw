import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import childProcess from "node:child_process";
import fs from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { encodeOTLPTraceRequest } = require("./otlp_protobuf.cjs");
const { buildOTLPPayload, sanitizeOTLPPayload, sendOTLPSpan, sendOTLPToAllEndpoints, OTEL_JSONL_PATH } = require("./send_otlp_span.cjs");

function payload() {
  return buildOTLPPayload({
    traceId: "0123456789abcdef0123456789abcdef",
    spanId: "fedcba9876543210",
    parentSpanId: "1234567890abcdef",
    spanName: "gh-aw.test",
    serviceName: "gh-aw",
    startMs: 1700000000000,
    endMs: 1700000000123,
    attributes: [
      { key: "items", value: { intValue: 128 } },
      { key: "api.token", value: { stringValue: "must-not-export" } },
    ],
  });
}

describe("minimal OTLP protobuf encoder", () => {
  it("encodes an empty request without dependencies", () => {
    expect(encodeOTLPTraceRequest({ resourceSpans: [] })).toEqual(Buffer.alloc(0));
  });

  it("uses UTF-8 byte lengths and exact little-endian nanosecond timestamps", () => {
    const request = { resourceSpans: [{ scopeSpans: [{ spans: [{ name: "é", startTimeUnixNano: "9007199254740993" }] }] }] };
    expect(encodeOTLPTraceRequest(request).toString("hex")).toBe("0a11120f120d2a02c3a9390100000000002000");
  });

  it("encodes hexadecimal IDs as bytes rather than their text representation", () => {
    const request = payload();
    const encoded = encodeOTLPTraceRequest(request);
    expect(encoded.includes(Buffer.from("0a100123456789abcdef0123456789abcdef1208fedcba9876543210", "hex"))).toBe(true);
    expect(encoded.includes(Buffer.from(request.resourceSpans[0].scopeSpans[0].spans[0].traceId))).toBe(false);
    expect(request).toEqual(payload());
  });

  it.each([
    ["startTimeUnixNano", Number.MAX_SAFE_INTEGER + 1],
    ["startTimeUnixNano", "-1"],
    ["startTimeUnixNano", "18446744073709551616"],
    ["flags", 0x100000000],
    ["traceId", "not-a-trace-id"],
    ["spanId", "ab"],
  ])("rejects malformed or inexact %s values", (key, value) => {
    expect(() => encodeOTLPTraceRequest({ resourceSpans: [{ scopeSpans: [{ spans: [{ [key]: value }] }] }] })).toThrow();
  });

  it.each(["9223372036854775808", "-9223372036854775809", 1.5, Number.MAX_SAFE_INTEGER + 1])("rejects invalid int64 attributes: %s", value => {
    const request = { resourceSpans: [{ resource: { attributes: [{ key: "count", value: { intValue: value } }] } }] };
    expect(() => encodeOTLPTraceRequest(request)).toThrow();
  });
});

describe("protobuf OTLP transport", () => {
  beforeEach(() => {
    for (const key of ["HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_HEADERS"]) {
      vi.stubEnv(key, "");
    }
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
    vi.spyOn(fs, "writeFileSync").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("keeps JSON as the default transport", async () => {
    await sendOTLPSpan("https://collector.example.com", payload());
    const [, options] = fetch.mock.calls[0];
    expect(options.headers["Content-Type"]).toBe("application/json");
    expect(JSON.parse(options.body)).toEqual(sanitizeOTLPPayload(payload()));
  });

  it.each(["OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"])("selects protobuf through %s and preserves sanitized JSONL", async variable => {
    vi.stubEnv(variable, "http/protobuf");
    await sendOTLPSpan("https://collector.example.com", payload());
    const [url, options] = fetch.mock.calls[0];
    expect(url).toBe("https://collector.example.com/v1/traces");
    expect(options.headers["Content-Type"]).toBe("application/x-protobuf");
    expect(options.body).toEqual(encodeOTLPTraceRequest(sanitizeOTLPPayload(payload())));
    expect(options.body.includes(Buffer.from("must-not-export"))).toBe(false);
    expect(fs.appendFileSync).toHaveBeenCalledWith(OTEL_JSONL_PATH, `${JSON.stringify(sanitizeOTLPPayload(payload()))}\n`);
  });

  it.each([
    ["http/protobuf", "http/json", "application/json"],
    ["http/json", "http/protobuf", "application/x-protobuf"],
  ])("prefers traces protocol %s/%s", async (general, traces, contentType) => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", general);
    vi.stubEnv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", traces);
    await sendOTLPSpan("https://collector.example.com", payload());
    expect(fetch.mock.calls[0][1].headers["Content-Type"]).toBe(contentType);
  });

  it("passes binary bytes unchanged to curl stdin when a proxy is configured", async () => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
    vi.stubEnv("HTTPS_PROXY", "http://proxy.example.com");
    const spawn = vi.spyOn(childProcess, "spawnSync").mockReturnValue({ status: 0, stdout: "200", stderr: "" });
    await sendOTLPSpan("https://collector.example.com", payload());
    expect(fetch).not.toHaveBeenCalled();
    const [command, args, options] = spawn.mock.calls[0];
    expect(command).toBe("curl");
    expect(args).toContain("Content-Type: application/x-protobuf");
    expect(args).toContain("--data-binary");
    expect(args).toContain("@-");
    expect(options.input).toEqual(encodeOTLPTraceRequest(sanitizeOTLPPayload(payload())));
  });

  it.each([
    "https://collector.example.com/v1/traces",
    "https://collector.example.com/v1/traces/",
    "https://example.ingest.monitor.azure.com/datacollectionRules/dcr-test/streams/Microsoft-OTLP-Traces/otlp/v1/traces",
    "https://example.ingest.monitor.azure.com/datacollectionRules/dcr-test/streams/Microsoft-OTLP-Traces/otlp/v1/traces?api-version=2023-01-01",
  ])("preserves complete traces endpoint %s", async endpoint => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
    await sendOTLPSpan(endpoint, payload());
    expect(fetch.mock.calls[0][0]).toBe(endpoint.replace(/\/$/, ""));
  });

  it("appends the traces path before endpoint query parameters", async () => {
    await sendOTLPSpan("https://collector.example.com/otlp/?region=west", payload());
    expect(fetch.mock.calls[0][0]).toBe("https://collector.example.com/otlp/v1/traces?region=west");
  });

  it("keeps endpoint-local authorization and prevents conflicting content types", async () => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
    await sendOTLPToAllEndpoints(
      [
        { url: "https://first.example.com", headers: "Authorization=Bearer first,content-type=application/json" },
        { url: "https://second.example.com", headers: "Authorization=Bearer second,Content-Type=text/plain" },
      ],
      payload(),
      { skipJSONL: true }
    );
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[0][1].headers).toEqual({ "Content-Type": "application/x-protobuf", Authorization: "Bearer first" });
    expect(fetch.mock.calls[1][1].headers).toEqual({ "Content-Type": "application/x-protobuf", Authorization: "Bearer second" });
    expect(fs.appendFileSync).not.toHaveBeenCalled();
  });

  it("reuses the encoded body across transient retries", async () => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
    fetch.mockResolvedValueOnce({ ok: false, status: 503 }).mockResolvedValueOnce({ ok: true, status: 200 });
    await sendOTLPSpan("https://collector.example.com", payload(), { baseDelayMs: 1 });
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[0][1].body).toBe(fetch.mock.calls[1][1].body);
    expect(fs.appendFileSync).toHaveBeenCalledTimes(1);
  });

  it("does not retry authorization failures with protobuf", async () => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
    fetch.mockResolvedValue({ ok: false, status: 403 });
    await sendOTLPSpan("https://collector.example.com", payload());
    expect(fetch).toHaveBeenCalledOnce();
    expect(console.warn).toHaveBeenCalledWith(expect.stringContaining("unchanged credentials will not be retried"));
  });

  it.each(["grpc", "invalid"])("reports unsupported protocol %s without sending or losing the mirror", async protocol => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol);
    await expect(sendOTLPSpan("https://collector.example.com", payload())).resolves.toBeUndefined();
    expect(fetch).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalledWith(expect.stringContaining("Unsupported OTLP traces protocol"));
    expect(fs.appendFileSync).toHaveBeenCalledWith(OTEL_JSONL_PATH, expect.any(String));
    expect(fs.appendFileSync).toHaveBeenCalledWith("/tmp/gh-aw/otlp-export-errors.jsonl", expect.stringContaining("Unsupported OTLP traces protocol"));
  });

  it("reports encoding errors without retries or JSON fallback", async () => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
    const request = payload();
    request.resourceSpans[0].scopeSpans[0].spans[0].startTimeUnixNano = "invalid";
    await expect(sendOTLPSpan("https://collector.example.com", request)).resolves.toBeUndefined();
    expect(fetch).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalledOnce();
    expect(console.warn).toHaveBeenCalledWith(expect.stringContaining("OTLP export encoding failed"));
  });
});
