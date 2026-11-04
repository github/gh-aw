import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import childProcess from "node:child_process";
import fs from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { encodeOTLPTraceRequest, encodeOTLPLogsRequest } = require("./otlp_protobuf.cjs");
const { buildAttr, buildOTLPPayload, sanitizeOTLPPayload, sendOTLPSpan, sendOTLPToAllEndpoints, OTEL_JSONL_PATH } = require("./send_otlp_span.cjs");

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

function logsPayload() {
  return {
    resourceLogs: [
      {
        resource: { attributes: [buildAttr("service.name", "gh-aw")] },
        scopeLogs: [
          {
            scope: { name: "gh-aw.session", version: "1" },
            logRecords: [
              {
                observedTimeUnixNano: "9007199254740993",
                severityNumber: 9,
                severityText: "INFO",
                traceId: "0123456789abcdef0123456789abcdef",
                spanId: "fedcba9876543210",
                body: {
                  kvlistValue: {
                    values: [
                      { key: "type", value: { stringValue: "session.result" } },
                      { key: "data", value: { kvlistValue: { values: [{ key: "numTurns", value: { intValue: "2" } }] } } },
                      { key: "checks", value: { arrayValue: { values: [{ boolValue: true }, { doubleValue: 1.5 }] } } },
                    ],
                  },
                },
                attributes: [buildAttr("gh-aw.session.event_type", "session.result")],
              },
            ],
          },
        ],
      },
    ],
  };
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

  it.each([{ bogusKey: 1 }, { resourceSpans: [{ bogusKey: 1 }] }, { resourceSpans: [{ scopeSpans: [{ spans: [{ bogusKey: 1 }] }] }] }, { resourceSpans: [{ resource: { attributes: [{ key: "nested", value: { bogusKey: 1 } }] } }] }])(
    "rejects unknown fields instead of silently dropping them: %j",
    request => {
      expect(() => encodeOTLPTraceRequest(request)).toThrow(/unknown field: bogusKey/);
    }
  );

  it.each([
    ["NaN", 0x7ff8000000000000n],
    ["Infinity", 0x7ff0000000000000n],
    ["-Infinity", 0xfff0000000000000n],
  ])("encodes OTLP JSON non-finite double token %s", (token, bits) => {
    const request = { resourceSpans: [{ resource: { attributes: [{ key: "d", value: { doubleValue: token } }] } }] };
    expect(encodeOTLPTraceRequest(request).includes(Buffer.from(BigInt.asUintN(64, bits).toString(16).padStart(16, "0"), "hex").reverse())).toBe(true);
  });

  it.each(["nan", "infinity", "+Infinity", "1.5", "invalid"])("rejects unsupported double string %s", doubleValue => {
    const request = { resourceSpans: [{ resource: { attributes: [{ key: "d", value: { doubleValue } }] } }] };
    expect(() => encodeOTLPTraceRequest(request)).toThrow(/doubleValue must be a number/);
  });

  it("encodes unsafe numeric attributes from buildAttr as doubles", () => {
    const attr = buildAttr("big", Number.MAX_SAFE_INTEGER + 10_000);
    expect(attr.value.doubleValue).toBe(Number.MAX_SAFE_INTEGER + 10_000);
    expect(() => encodeOTLPTraceRequest({ resourceSpans: [{ resource: { attributes: [attr] } }] })).not.toThrow();
  });

  it.each(["9223372036854775808", "-9223372036854775809", 1.5, Number.MAX_SAFE_INTEGER + 1])("rejects invalid int64 attributes: %s", value => {
    const request = { resourceSpans: [{ resource: { attributes: [{ key: "count", value: { intValue: value } }] } }] };
    expect(() => encodeOTLPTraceRequest(request)).toThrow();
  });
});

describe("minimal OTLP logs protobuf encoder", () => {
  it("encodes an empty logs request", () => {
    expect(encodeOTLPLogsRequest({ resourceLogs: [] })).toEqual(Buffer.alloc(0));
  });

  it("matches the LogRecord field numbers and wire types, including exact timestamps and IDs", () => {
    const request = {
      resourceLogs: [
        {
          scopeLogs: [
            {
              logRecords: [
                {
                  timeUnixNano: "9007199254740993",
                  severityNumber: 9,
                  severityText: "INFO",
                  body: { stringValue: "é" },
                  attributes: [{ key: "n", value: { intValue: "-1" } }],
                  droppedAttributesCount: 1,
                  flags: 1,
                  traceId: "11111111111111111111111111111111",
                  spanId: "2222222222222222",
                  observedTimeUnixNano: "9007199254740995",
                  eventName: "event",
                },
              ],
            },
          ],
        },
      ],
    };
    expect(encodeOTLPLogsRequest(request).toString("hex")).toBe(
      "0a60125e125c" + "09010000000000200010091a04494e464f2a040a02c3a9" + "32100a016e120b18ffffffffffffffffff0138014501000000" + "4a101111111111111111111111111111111152082222222222222222" + "59030000000000200062056576656e74"
    );
  });

  it("encodes resource, scope, and structured AnyValue log bodies without mutation", () => {
    const request = logsPayload();
    const encoded = encodeOTLPLogsRequest(request);
    for (const text of ["gh-aw", "gh-aw.session", "session.result", "numTurns"]) expect(encoded.includes(Buffer.from(text))).toBe(true);
    expect(request).toEqual(logsPayload());
  });

  it.each([
    ["observedTimeUnixNano", Number.MAX_SAFE_INTEGER + 1],
    ["timeUnixNano", "-1"],
    ["flags", 0x100000000],
    ["traceId", "not-a-trace-id"],
    ["spanId", "ab"],
    ["bogusKey", 1],
  ])("rejects malformed log record %s", (key, value) => {
    expect(() => encodeOTLPLogsRequest({ resourceLogs: [{ scopeLogs: [{ logRecords: [{ [key]: value }] }] }] })).toThrow();
  });

  it("rejects requests for the wrong signal instead of silently dropping them", () => {
    expect(() => encodeOTLPLogsRequest(payload())).toThrow(/unknown field: resourceSpans/);
    expect(() => encodeOTLPTraceRequest(logsPayload())).toThrow(/unknown field: resourceLogs/);
  });
});

describe("protobuf OTLP transport", () => {
  beforeEach(() => {
    for (const key of [
      "HTTPS_PROXY",
      "https_proxy",
      "HTTP_PROXY",
      "http_proxy",
      "ALL_PROXY",
      "all_proxy",
      "OTEL_EXPORTER_OTLP_PROTOCOL",
      "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
      "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
      "OTEL_EXPORTER_OTLP_HEADERS",
    ]) {
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

  it.each([
    ["", "", "http/protobuf", "http/json"],
    ["http/protobuf", "", "grpc", "http/protobuf"],
    ["http/protobuf", "http/json", "http/protobuf", "http/json"],
    ["http/json", "http/protobuf", "http/json", "http/protobuf"],
  ])("selects logs encoding independently: global=%s logs=%s traces=%s", async (general, logs, traces, expected) => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", general);
    vi.stubEnv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", logs);
    vi.stubEnv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", traces);
    await sendOTLPSpan("https://collector.example.com", logsPayload(), { signal: "logs", skipJSONL: true });
    expect(fetch).toHaveBeenCalledOnce();
    const [url, options] = fetch.mock.calls[0];
    expect(url).toBe("https://collector.example.com/v1/logs");
    expect(options.headers["Content-Type"]).toBe(expected === "http/protobuf" ? "application/x-protobuf" : "application/json");
    expect(options.body).toEqual(expected === "http/protobuf" ? encodeOTLPLogsRequest(logsPayload()) : JSON.stringify(logsPayload()));
    expect(fs.appendFileSync).not.toHaveBeenCalled();
    expect(console.warn).not.toHaveBeenCalled();
  });

  it("ignores an unsupported logs protocol for traces", async () => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "grpc");
    await sendOTLPSpan("https://collector.example.com", payload());
    expect(fetch.mock.calls[0][1].headers["Content-Type"]).toBe("application/json");
    expect(console.warn).not.toHaveBeenCalled();
  });

  it("reports an unsupported logs protocol without sending or falling back to traces", async () => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "grpc");
    vi.stubEnv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/json");
    await sendOTLPSpan("https://collector.example.com", logsPayload(), { signal: "logs", skipJSONL: true });
    expect(fetch).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalledWith(expect.stringContaining("Unsupported OTLP logs protocol"));
  });

  it.each(["http/json", "http/protobuf"])("sends %s logs through the proxy without changing their bytes or endpoint credentials", async protocol => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", protocol);
    vi.stubEnv("HTTPS_PROXY", "http://proxy.example.com");
    const spawn = vi.spyOn(childProcess, "spawnSync").mockReturnValue({ status: 0, stdout: "200", stderr: "" });
    await sendOTLPToAllEndpoints(
      [
        { url: "https://first.example.com/otlp", headers: "x-api-key=first" },
        { url: "https://second.example.com/v1/traces?region=west", headers: "Authorization=second" },
      ],
      logsPayload(),
      { signal: "logs", skipJSONL: true }
    );
    expect(fetch).not.toHaveBeenCalled();
    expect(spawn).toHaveBeenCalledTimes(2);
    const [, firstArgs, firstOptions] = spawn.mock.calls[0];
    const [, secondArgs, secondOptions] = spawn.mock.calls[1];
    expect(firstArgs).toContain("https://first.example.com/otlp/v1/logs");
    expect(secondArgs).toContain("https://second.example.com/v1/logs?region=west");
    expect(firstArgs).toContain("x-api-key: first");
    expect(firstArgs).not.toContain("Authorization: second");
    expect(secondArgs).toContain("Authorization: second");
    expect(secondArgs).not.toContain("x-api-key: first");
    expect(firstArgs).toContain(`Content-Type: ${protocol === "http/protobuf" ? "application/x-protobuf" : "application/json"}`);
    expect(firstArgs).toContain("--data-binary");
    expect(firstArgs).toContain("@-");
    expect(firstOptions.input).toEqual(protocol === "http/protobuf" ? encodeOTLPLogsRequest(logsPayload()) : JSON.stringify(logsPayload()));
    expect(secondOptions.input).toEqual(firstOptions.input);
  });

  it.each(["http/json", "http/protobuf"])("uses the independent Azure logs stream with %s on both transports", async protocol => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", protocol);
    const prefix = "https://example.ingest.monitor.azure.com/datacollectionRules/dcr-test/streams/";
    const expected = `${prefix}Microsoft-OTLP-Logs/otlp/v1/logs?api-version=2023-01-01`;
    const spawn = vi.spyOn(childProcess, "spawnSync").mockReturnValue({ status: 0, stdout: "200", stderr: "" });
    for (const proxy of ["", "http://proxy.example.com"]) {
      vi.stubEnv("HTTPS_PROXY", proxy);
      for (const endpoint of [`${prefix}Microsoft-OTLP-Traces/otlp/v1/traces/?api-version=2023-01-01`, expected]) {
        await sendOTLPSpan(endpoint, logsPayload(), { signal: "logs", skipJSONL: true });
      }
    }
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls.map(call => call[0])).toEqual([expected, expected]);
    expect(spawn).toHaveBeenCalledTimes(2);
    for (const [, args] of spawn.mock.calls) expect(args).toContain(expected);
    expect(console.warn).not.toHaveBeenCalled();
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
