import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import childProcess from "child_process";

const require = createRequire(import.meta.url);
const { MAX_OTLP_RESPONSE_BYTES, readOTLPResponse, inspectOTLPResponse } = require("./otlp_response.cjs");
const { sendOTLPSpan, sendOTLPToAllEndpoints } = require("./send_otlp_span.cjs");
// Preload the lazy encoder before the readFileSync mock prevents Node from loading it.
require("./otlp_protobuf.cjs");
const privateMessage = "Bearer secret-token user@example.com https://user:password@collector.invalid/?key=private\n::error::private";
const inspectJSON = (body, signal = "traces") => inspectOTLPResponse(Buffer.from(JSON.stringify(body)), "application/json", "http/json", signal);
const protobuf = (count, message = "") => {
  const text = Buffer.from(message);
  const fields = Buffer.concat([Buffer.from([8, count]), text.length ? Buffer.concat([Buffer.from([18, text.length]), text]) : Buffer.alloc(0)]);
  return Buffer.concat([Buffer.from([10, fields.length]), fields]);
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("OTLP response diagnostics", () => {
  it.each(["traces", "logs"])("preserves empty and full-success %s responses", signal => {
    for (const protocol of ["http/json", "http/protobuf"]) {
      expect(inspectOTLPResponse(Buffer.alloc(0), "", protocol, signal)).toEqual({ reason: "", error: false });
    }
    expect(inspectJSON({}, signal)).toEqual({ reason: "", error: false });
    expect(inspectJSON({ partialSuccess: {} }, signal)).toEqual({ reason: "", error: false });
    expect(inspectOTLPResponse(protobuf(0), "application/x-protobuf", "http/protobuf", signal)).toEqual({ reason: "", error: false });
  });

  it.each([
    ["traces", "rejectedSpans", "rejected_spans", "rejectedLogRecords"],
    ["logs", "rejectedLogRecords", "rejected_log_records", "rejectedSpans"],
  ])("ignores unknown JSON fields and defaults null %s members", (signal, camelCount, snakeCount, otherCount) => {
    for (const [partialKey, countKey, messageKey] of [
      ["partialSuccess", camelCount, "errorMessage"],
      ["partial_success", snakeCount, "error_message"],
    ]) {
      for (const partial of [null, {}, { [countKey]: null, [messageKey]: null }, { futureField: privateMessage, [otherCount]: "9" }]) {
        expect(inspectJSON({ [partialKey]: partial, futureField: privateMessage }, signal)).toEqual({ reason: "", error: false });
      }
      const rejection = inspectJSON({ futureField: privateMessage, [partialKey]: { [countKey]: "9223372036854775807", [messageKey]: null, futureField: privateMessage } }, signal);
      expect(rejection.error).toBe(true);
      expect(rejection.reason).toContain(`${snakeCount}=9223372036854775807`);
      expect(rejection.reason).not.toContain("private");
      const warning = inspectJSON({ [partialKey]: { [countKey]: null, [messageKey]: privateMessage, futureField: true } }, signal);
      expect(warning.error).toBe(false);
      expect(warning.reason).toContain("collector warning (message withheld)");
      expect(warning.reason).not.toContain("private");
    }
    expect(inspectJSON({ futureField: privateMessage }, signal)).toEqual({ reason: "", error: false });
  });

  it.each([
    ["traces", "rejectedSpans", "rejected_spans"],
    ["logs", "rejectedLogRecords", "rejected_log_records"],
  ])("reports exact %s rejection counts without disclosing messages", (signal, key, label) => {
    const result = inspectJSON({ partialSuccess: { [key]: "9223372036854775807", errorMessage: privateMessage } }, signal);
    expect(result.error).toBe(true);
    expect(result.reason).toContain(`${label}=9223372036854775807`);
    expect(result.reason).not.toContain("private");
    expect(inspectJSON({ partial_success: { [label]: 2 } }, signal).reason).toContain(`${label}=2`);
    expect(inspectOTLPResponse(protobuf(3, privateMessage), "application/x-protobuf", "http/protobuf", signal).reason).toContain(`${label}=3`);
  });

  it.each(["traces", "logs"])("reports warning-only %s responses without counting delivery loss", signal => {
    for (const result of [inspectJSON({ partialSuccess: { errorMessage: privateMessage } }, signal), inspectOTLPResponse(protobuf(0, privateMessage), "application/x-protobuf", "http/protobuf", signal)]) {
      expect(result.error).toBe(false);
      expect(result.reason).toContain("collector warning (message withheld)");
      expect(result.reason).not.toContain("secret");
    }
  });

  it.each([
    null,
    [],
    { partialSuccess: [] },
    { partialSuccess: false },
    ...[-1, 0.1, true, "not-a-count", "9223372036854775808", Number.MAX_SAFE_INTEGER + 1].map(rejectedSpans => ({ partialSuccess: { rejectedSpans } })),
    { partialSuccess: { errorMessage: 123 } },
    { partialSuccess: { rejectedSpans: "1", rejected_spans: "0" } },
    { partialSuccess: {}, partial_success: {} },
  ])("does not claim full acceptance of invalid JSON diagnostics: %j", body => {
    const result = inspectJSON(body);
    expect(result.error).toBe(true);
    expect(result.reason).toContain("acceptance unknown");
    expect(result.reason).not.toContain("private");
  });

  it.each([
    ["traces", "rejectedSpans", "rejected_spans"],
    ["logs", "rejectedLogRecords", "rejected_log_records"],
  ])("still validates known %s members alongside unknown fields", (signal, camelCount, snakeCount) => {
    for (const [partialKey, countKey, messageKey] of [
      ["partialSuccess", camelCount, "errorMessage"],
      ["partial_success", snakeCount, "error_message"],
    ]) {
      for (const fields of [...[-1, "-1", 0.1, true, {}, [], "9223372036854775808", Number.MAX_SAFE_INTEGER + 1].map(count => ({ [countKey]: count })), { [messageKey]: false }]) {
        const result = inspectJSON({ futureField: privateMessage, [partialKey]: { ...fields, futureField: privateMessage } }, signal);
        expect(result.error).toBe(true);
        expect(result.reason).toContain("acceptance unknown");
        expect(result.reason).not.toContain("private");
      }
    }
  });

  it.each(["traces", "logs"])("withholds invalid UTF-8 protobuf %s messages without obscuring acceptance", signal => {
    const warning = inspectOTLPResponse(Buffer.from("0a031201ff", "hex"), "application/x-protobuf", "http/protobuf", signal);
    expect(warning.error).toBe(false);
    expect(warning.reason).toContain("collector warning (message withheld)");
    const rejection = inspectOTLPResponse(Buffer.from("0a0508021201ff", "hex"), "application/x-protobuf", "http/protobuf", signal);
    expect(rejection.error).toBe(true);
    expect(rejection.reason).toContain(signal === "logs" ? "rejected_log_records=2" : "rejected_spans=2");
    for (const result of [warning, rejection]) expect(result.reason).not.toContain("acceptance unknown");
  });

  it.each(["0a", "0a0208", "0a0208010a00", "0a03080100", "0a020a00", "0a0b08ffffffffffffffffff01", "0a020c00", "00"])("handles malformed protobuf %s without leaking data", hex => {
    expect(inspectOTLPResponse(Buffer.from(hex, "hex"), "application/x-protobuf", "http/protobuf", "logs").reason).toContain("acceptance unknown");
  });

  it("skips valid unknown protobuf fields without mistaking them for rejection counts", () => {
    const unknowns = Buffer.from("18012100000000000000002a01783500000000", "hex");
    const body = Buffer.concat([unknowns, protobuf(2)]);
    expect(inspectOTLPResponse(body, "", "http/protobuf", "logs").reason).toContain("rejected_log_records=2");
  });

  it("preserves exact int64 protobuf rejection counts", () => {
    const body = Buffer.from("0a0a08ffffffffffffffff7f", "hex");
    expect(inspectOTLPResponse(body, "", "http/protobuf", "traces").reason).toContain("rejected_spans=9223372036854775807");
  });

  it("honors response content type and rejects unsupported, malformed, or oversized responses", () => {
    expect(inspectOTLPResponse(protobuf(2), "application/x-protobuf", "http/json", "traces").reason).toContain("rejected_spans=2");
    expect(inspectOTLPResponse(Buffer.from('{"partialSuccess":{"rejectedLogRecords":"2"}}'), "application/json; charset=utf-8", "http/protobuf", "logs").reason).toContain("rejected_log_records=2");
    for (const [body, type] of [
      [Buffer.from(privateMessage), "text/html"],
      [Buffer.from("{"), "application/json"],
      [Buffer.alloc(MAX_OTLP_RESPONSE_BYTES + 1), "application/json"],
    ]) {
      expect(inspectOTLPResponse(body, type, "http/json", "traces").reason).toContain("acceptance unknown");
    }
  });

  it("bounds streamed response reads and cancels oversized bodies", async () => {
    const cancel = vi.fn();
    const body = new ReadableStream({
      start(controller) {
        controller.enqueue(new Uint8Array(MAX_OTLP_RESPONSE_BYTES + 1));
      },
      cancel,
    });
    await expect(readOTLPResponse(body)).rejects.toThrow("size limit");
    expect(cancel).toHaveBeenCalledOnce();
    expect(await readOTLPResponse(new Response("{}").body)).toEqual(Buffer.from("{}"));
  });
});

describe("OTLP delivery diagnostics across transports", () => {
  let append, write, warn, spawn;
  beforeEach(() => {
    for (const key of ["HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"]) vi.stubEnv(key, "");
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
    vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw new Error("No prior failures");
    });
    append = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
    write = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {});
    warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    spawn = vi.spyOn(childProcess, "spawnSync");
  });

  it.each(["direct", "proxy"].flatMap(transport => ["traces", "logs"].flatMap(signal => ["http/json", "http/protobuf"].map(protocol => [transport, signal, protocol]))))(
    "records %s %s %s rejection once without retrying",
    async (transport, signal, protocol) => {
      vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol);
      const key = signal === "logs" ? "rejectedLogRecords" : "rejectedSpans";
      const type = protocol === "http/json" ? "application/json" : "application/x-protobuf";
      const body = protocol === "http/json" ? Buffer.from(JSON.stringify({ partialSuccess: { [key]: "2", errorMessage: privateMessage } })) : protobuf(2, privateMessage);
      const fetch = vi.fn().mockResolvedValue(new Response(body, { headers: { "content-type": type } }));
      vi.stubGlobal("fetch", fetch);
      if (transport === "proxy") {
        vi.stubEnv("HTTPS_PROXY", "http://proxy.invalid");
        spawn.mockReturnValue({ status: 0, stdout: Buffer.concat([Buffer.from(`HTTP/1.1 200 OK\r\nContent-Type: ${type}\r\n\r\n`), body, Buffer.from(`\n200\n${type}`)]), stderr: Buffer.alloc(0) });
      }
      await expect(sendOTLPSpan("https://collector.invalid", {}, { signal, skipJSONL: true, baseDelayMs: 1 })).resolves.toBeUndefined();
      expect(transport === "proxy" ? spawn : fetch).toHaveBeenCalledOnce();
      expect(warn).toHaveBeenCalledOnce();
      expect(write).toHaveBeenCalledOnce();
      expect(append).toHaveBeenCalledWith(expect.stringContaining("otlp-export-errors.jsonl"), expect.stringContaining(signal === "logs" ? "rejected_log_records=2" : "rejected_spans=2"));
      expect(JSON.stringify([...warn.mock.calls, ...append.mock.calls])).not.toContain("secret-token");
    }
  );

  it.each(["direct", "proxy"])("preserves endpoint credential and failure isolation over %s", async transport => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=global-secret");
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(new Response('{"partialSuccess":{"rejectedSpans":"1"}}', { headers: { "content-type": "application/json" } }))
      .mockResolvedValueOnce(new Response("{}", { headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetch);
    if (transport === "proxy") {
      vi.stubEnv("HTTPS_PROXY", "http://proxy.invalid");
      spawn.mockReturnValueOnce({ status: 0, stdout: Buffer.from('{"partialSuccess":{"rejectedSpans":"1"}}\n200\napplication/json') }).mockReturnValueOnce({ status: 0, stdout: Buffer.from("{}\n200\napplication/json") });
    }
    await sendOTLPToAllEndpoints(
      [
        { url: "https://first.invalid", headers: "Authorization=first-secret" },
        { url: "https://second.invalid", headers: "Authorization=second-secret" },
      ],
      {},
      { skipJSONL: true }
    );
    const calls = transport === "proxy" ? spawn.mock.calls.map(call => call[1].join(" ")) : fetch.mock.calls.map(call => JSON.stringify(call));
    expect(calls).toHaveLength(2);
    expect(calls[0]).toContain("first-secret");
    expect(calls[0]).not.toContain("second-secret");
    expect(calls[1]).toContain("second-secret");
    expect(calls[1]).not.toContain("first-secret");
    expect(calls.join("")).not.toContain("global-secret");
    expect(write).toHaveBeenCalledOnce();
  });

  it.each(["{}", "", '{"partialSuccess":{}}'])("preserves successful delivery without diagnostics: %s", async body => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { headers: { "content-type": "application/json" } })));
    await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true });
    expect(warn).not.toHaveBeenCalled();
    expect(write).not.toHaveBeenCalled();
  });

  it.each(["direct", "proxy"].flatMap(transport => ["traces", "logs"].flatMap(signal => ["full", "warning", "rejection"].map(kind => [transport, signal, kind]))))(
    "preserves %s %s JSON null and unknown-field %s semantics",
    async (transport, signal, kind) => {
      vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/json");
      const countKey = signal === "logs" ? "rejected_log_records" : "rejected_spans";
      const body = JSON.stringify({
        futureField: privateMessage,
        partial_success: { [countKey]: kind === "rejection" ? "9223372036854775807" : null, error_message: kind === "warning" ? privateMessage : null, futureField: privateMessage },
      });
      const fetch = vi.fn().mockResolvedValue(new Response(body, { headers: { "content-type": "application/json" } }));
      vi.stubGlobal("fetch", fetch);
      if (transport === "proxy") {
        vi.stubEnv("HTTPS_PROXY", "http://proxy.invalid");
        spawn.mockReturnValue({ status: 0, stdout: Buffer.from(`${body}\n200\napplication/json`) });
      }
      await expect(sendOTLPSpan("https://collector.invalid", {}, { signal, skipJSONL: true })).resolves.toBeUndefined();
      expect(transport === "proxy" ? spawn : fetch).toHaveBeenCalledOnce();
      expect(write).toHaveBeenCalledTimes(kind === "rejection" ? 1 : 0);
      expect(warn).toHaveBeenCalledTimes(kind === "full" ? 0 : 1);
      if (kind === "warning") expect(warn.mock.calls[0][0]).toContain("collector warning (message withheld)");
      if (kind === "rejection") expect(append).toHaveBeenCalledWith(expect.stringContaining("otlp-export-errors.jsonl"), expect.stringContaining(`${countKey}=9223372036854775807`));
      expect(JSON.stringify([...warn.mock.calls, ...append.mock.calls])).not.toContain("private");
    }
  );

  it.each(["traces", "logs"].flatMap(signal => ["http/json", "http/protobuf"].flatMap(protocol => ["full", "empty", "warning"].map(kind => [signal, protocol, kind]))))(
    "preserves proxy %s %s %s success semantics",
    async (signal, protocol, kind) => {
      vi.stubEnv("HTTPS_PROXY", "http://proxy.invalid");
      vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol);
      const type = protocol === "http/json" ? "application/json" : "application/x-protobuf";
      const body =
        kind === "empty" ? Buffer.alloc(0) : protocol === "http/protobuf" ? protobuf(0, kind === "warning" ? privateMessage : "") : Buffer.from(JSON.stringify({ partialSuccess: kind === "warning" ? { errorMessage: privateMessage } : {} }));
      spawn.mockReturnValue({ status: 0, stdout: Buffer.concat([Buffer.from(`HTTP/1.1 200 OK\r\nContent-Type: ${type}\r\n\r\n`), body, Buffer.from(`\n200\n${type}`)]) });
      await sendOTLPSpan("https://collector.invalid", {}, { signal, skipJSONL: true });
      expect(spawn).toHaveBeenCalledOnce();
      expect(write).not.toHaveBeenCalled();
      expect(warn).toHaveBeenCalledTimes(kind === "warning" ? 1 : 0);
      expect(JSON.stringify(warn.mock.calls)).not.toContain("secret-token");
      const args = spawn.mock.calls[0][1];
      expect(args).toContain("--max-filesize");
      expect(args).toContain(String(MAX_OTLP_RESPONSE_BYTES));
      expect(spawn.mock.calls[0][2].maxBuffer).toBeLessThan(2 * MAX_OTLP_RESPONSE_BYTES);
    }
  );

  it("warns without incrementing errors for warning-only partial success", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ partialSuccess: { errorMessage: privateMessage } }), { headers: { "content-type": "application/json" } })));
    await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true });
    expect(warn).toHaveBeenCalledOnce();
    expect(write).not.toHaveBeenCalled();
    expect(warn.mock.calls[0][0]).not.toContain("secret-token");
  });

  it.each(["malformed", "unsupported", "oversized", "read-error", "proxy-truncated", "proxy-malformed", "proxy-unsupported", "proxy-buffer-overflow"])("records unknown acceptance without retrying on %s diagnostics", async kind => {
    const body = kind === "oversized" ? " ".repeat(MAX_OTLP_RESPONSE_BYTES + 1) : privateMessage;
    const response = new Response(body, { headers: { "content-type": kind === "unsupported" ? "text/html" : "application/json" } });
    if (kind === "read-error")
      vi.spyOn(response.body, "getReader").mockImplementation(() => {
        throw new Error(privateMessage);
      });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    if (kind.startsWith("proxy-")) {
      vi.stubEnv("HTTPS_PROXY", "http://proxy.invalid");
      spawn.mockReturnValue({
        status: kind === "proxy-truncated" ? 63 : 0,
        stdout: Buffer.from(`HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n${kind === "proxy-truncated" ? "" : privateMessage}\n200\n${kind === "proxy-unsupported" ? "text/html" : "application/json"}`),
        stderr: Buffer.from(privateMessage),
        ...(kind === "proxy-buffer-overflow"
          ? {
              error: Object.assign(new Error(privateMessage), { code: "ENOBUFS" }),
            }
          : {}),
      });
    }
    await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 });
    expect(kind.startsWith("proxy-") ? spawn : fetch).toHaveBeenCalledOnce();
    expect(write).toHaveBeenCalledOnce();
    expect(warn.mock.calls[0][0]).toContain("acceptance unknown");
    expect(JSON.stringify([...warn.mock.calls, ...append.mock.calls])).not.toContain("secret-token");
  });
});
