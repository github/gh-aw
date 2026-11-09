import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const http = require("node:http");
const { sendSessionLogs } = require("./send_otlp_session_logs.cjs");
const { encodeOTLPLogsRequest } = require("./otlp_protobuf.cjs");
const { serializeSessionArtifact } = require("./session_artifact.cjs");
const nativeFetch = globalThis.fetch;
const traceId = "0123456789abcdef0123456789abcdef";
const spanId = "0123456789abcdef";
const decode = value => {
  if (value.kvlistValue) return Object.fromEntries(value.kvlistValue.values.map(entry => [entry.key, decode(entry.value)]));
  if (value.arrayValue) return value.arrayValue.values.map(decode);
  if (value.intValue !== undefined) return Number(value.intValue);
  return value.stringValue ?? value.doubleValue ?? value.boolValue ?? null;
};

describe("compact unified session OTLP logs", () => {
  let directory;
  let sessionPath;
  let fetchMock;
  let warning;
  let partialWarning;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-session-logs-"));
    sessionPath = path.join(directory, "aw_session.jsonl");
    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
    warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    partialWarning = vi.spyOn(process.stdout, "write").mockImplementation(() => true);
    for (const name of ["HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "GH_AW_SECRET_NAMES", "OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"])
      vi.stubEnv(name, "");
    vi.stubEnv("GH_AW_OTLP_LOGS_ENABLED", "true");
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", "");
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify([{ url: "https://collector.example/otlp", headers: "Authorization=Bearer%20private-credential" }]));
    vi.stubEnv("GH_AW_OTLP_IF_MISSING", "ignore");
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", traceId);
    vi.stubEnv("GITHUB_AW_OTEL_PARENT_SPAN_ID", spanId);
    fs.writeFileSync(sessionPath, JSON.stringify({ type: "session.format", data: { version: 1 } }) + "\n");
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    fs.rmSync(directory, { recursive: true, force: true });
  });
  const write = events => fs.writeFileSync(sessionPath, events.map(JSON.stringify).join("\n") + "\n");
  const payload = (index = 0) => JSON.parse(fetchMock.mock.calls[index][1].body);
  const records = () => payload().resourceLogs[0].scopeLogs[0].logRecords;

  it("exports only the session log sender", () => {
    expect(Object.keys(require("./send_otlp_session_logs.cjs"))).toEqual(["sendSessionLogs"]);
  });

  it("exports the validated durable parent as a searchable string attribute", async () => {
    write([
      { type: "durable.restore", data: { engine: "copilot", storage: "artifact", runId: "200", runAttempt: "1", status: "restored", parentRunId: "100", artifactId: "99" } },
      { type: "durable.session", data: { engine: "copilot", storage: "artifact", runId: "200", runAttempt: "1", status: "resumed", parentRunId: "100", sessionId: "native-session" } },
      { type: "durable.restore", data: { engine: "copilot", storage: "artifact", runId: "200", runAttempt: "1", status: "fresh" } },
    ]);
    await sendSessionLogs({ sessionPath });
    for (const record of records().slice(0, 2)) expect(record.attributes).toContainEqual({ key: "gh-aw.durable.parent_run_id", value: { stringValue: "100" } });
    expect(records()[2].attributes).not.toContainEqual(expect.objectContaining({ key: "gh-aw.durable.parent_run_id" }));
  });

  it.each([
    { timestamp: "2026-10-20T19:00:00.123456789Z", expected: "1792522800123456789" },
    { timestamp: "2026-10-20T21:00:00.123456789+02:00", expected: "1792522800123456789" },
    { timestamp: "2026-10-20T17:00:00.123456789-02:00", expected: "1792522800123456789" },
    { timestamp: "2554-07-21T23:34:33.709551615Z", expected: "18446744073709551615" },
    { timestamp: 1700000000123.5, expected: "1700000000123500000" },
    { timestamp: 0, expected: "0" },
    { timestamp: 1700000000, provenance: { timestampMs: 1700000000000 }, expected: "1700000000000000000" },
    { timestamp: "1700000000123000000", provenance: { timestampMs: 1700000000123 }, expected: "1700000000123000000" },
    { provenance: { timestampMs: 1700000000123 }, expected: "1700000000123000000" },
    { timestamp: "2026-10-20T19:00:00.123456789Z", provenance: { timestampMs: 1792522800123.4568 }, expected: "1792522800123456789" },
    { timestamp: "1792522800123456789", provenance: { timestampMs: 1792522800123.4568 }, expected: "1792522800123456789" },
  ])("exports original event time separately from observation time: $timestamp / $provenance", async ({ timestamp, provenance, expected }) => {
    vi.spyOn(Date, "now").mockReturnValue(1800000000000);
    const source = { type: "usage.report", data: { aic: 0 }, ...(timestamp !== undefined ? { timestamp } : {}), ...(provenance ? { provenance } : {}) };
    write([source]);
    const original = fs.readFileSync(sessionPath, "utf8");
    await sendSessionLogs({ sessionPath });
    expect(records()[0]).toMatchObject({ timeUnixNano: expected, observedTimeUnixNano: "1800000000000000000", traceId, spanId });
    expect(decode(records()[0].body)).toEqual(source);
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.untimed_events", value: { intValue: 0 } });
    expect(fs.readFileSync(sessionPath, "utf8")).toBe(original);
  });

  it.each([undefined, null, "", "PRIVATE_INVALID_TIME", "1700000000000", "2026-10-20T19:00:00", -1, 8640000000000000, "9999-12-31T23:59:59Z", "2554-07-21T23:34:33.709551616Z"])(
    "does not replace missing or invalid event time %j with export time",
    async timestamp => {
      vi.spyOn(Date, "now").mockReturnValue(1800000000000);
      write([{ type: "usage.report", data: { aic: 0 }, timestamp }]);
      await sendSessionLogs({ sessionPath });
      expect(records()[0]).not.toHaveProperty("timeUnixNano");
      expect(records()[0].observedTimeUnixNano).toBe("1800000000000000000");
      expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.untimed_events", value: { intValue: 1 } });
      expect(warning).not.toHaveBeenCalled();
    }
  );

  it.each([-1, 8640000000000000, 8640000000000001, 1e300])("does not reinterpret an out-of-range normalized clock %j in native timestamp units", async timestampMs => {
    write([{ type: "usage.report", timestamp: 1700000000, provenance: { timestampMs }, data: { aic: 0 } }]);
    await sendSessionLogs({ sessionPath });
    expect(records()[0]).not.toHaveProperty("timeUnixNano");
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.untimed_events", value: { intValue: 1 } });
  });

  it("does not reinterpret an invalid source-normalized seconds clock as milliseconds", async () => {
    write([{ type: "usage.report", timestamp: 1e13, provenance: { component: "firewall", path: "audit.jsonl" }, data: { aic: 0 } }]);
    await sendSessionLogs({ sessionPath });
    expect(records()[0]).not.toHaveProperty("timeUnixNano");
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.untimed_events", value: { intValue: 1 } });
  });

  it("computes time only after publication masks, without exporting source identities", async () => {
    const timestamp = "2026-10-20T19:00:00Z";
    vi.stubEnv("GH_AW_SECRET_NAMES", "CLOCK");
    vi.stubEnv("SECRET_CLOCK", timestamp);
    write([{ type: "usage.report", timestamp, provenance: { timestampMs: 1792522800000, path: "PRIVATE", component: "usage" }, data: { aic: 1 } }]);
    await sendSessionLogs({ sessionPath });
    expect(fetchMock.mock.calls[0][1].body).not.toContain(timestamp);
    expect(fetchMock.mock.calls[0][1].body).not.toContain("PRIVATE");
    expect(records()[0]).not.toHaveProperty("timeUnixNano");
    expect(decode(records()[0].body).provenance).not.toHaveProperty("timestampMs");
  });

  it("suppresses merger-produced numeric clocks masked during publication", async () => {
    const timestamp = "2026-10-20T19:00:00Z";
    const event = { type: "usage.report", timestamp, provenance: { timestampMs: 1792522800000 }, data: { aic: 1 } };
    fs.writeFileSync(sessionPath, serializeSessionArtifact([event], [timestamp]));
    await sendSessionLogs({ sessionPath });
    expect(fetchMock.mock.calls[0][1].body).not.toContain(timestamp);
    expect(records()[0]).not.toHaveProperty("timeUnixNano");
    expect(decode(records()[0].body).provenance).not.toHaveProperty("timestampMs");
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.untimed_events", value: { intValue: 1 } });
  });

  it("does not fall back to a normalized clock derived from an impossible ISO date", async () => {
    write([{ type: "usage.report", timestamp: "2026-02-29T19:00:00.123456789Z", provenance: { timestampMs: 1772391600123 }, data: { aic: 0 } }]);
    await sendSessionLogs({ sessionPath });
    expect(records()[0]).not.toHaveProperty("timeUnixNano");
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.untimed_events", value: { intValue: 1 } });
  });

  it.each(["", "false", "TRUE", "1", "invalid"])("fails closed for opt-in value %j without reading the session", async value => {
    vi.stubEnv("GH_AW_OTLP_LOGS_ENABLED", value);
    await sendSessionLogs({ sessionPath: "/nonexistent/session" });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(warning).toHaveBeenCalledTimes(value && value !== "false" ? 1 : 0);
  });

  it("exports searchable typed records with correlation, not conversation or identifying fields", async () => {
    write([
      { type: "session.format", data: { version: 1 } },
      { type: "workflow.info", data: { engineId: "copilot", repository: "PRIVATE_REPO", workflowId: "PRIVATE_WORKFLOW", ref: "PRIVATE_REF", sha: "PRIVATE_SHA" } },
      { type: "assistant.message", data: { content: "PRIVATE_CONVERSATION" } },
      { type: "tool.execution_start", data: { input: "PRIVATE_TOOL" } },
      { type: "session.result", data: { numTurns: 0, sessionId: "PRIVATE_SESSION", agentId: "PRIVATE_AGENT", usage: { inputTokens: 10, inputTokensIncludeCache: false } }, provenance: { path: "PRIVATE_PATH", component: "agent" } },
      { type: "memory.push", data: { memoryId: "PRIVATE_MEMORY", branch: "PRIVATE_BRANCH", commit: "PRIVATE_COMMIT", status: "saved" } },
    ]);
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0][0]).toBe("https://collector.example/otlp/v1/logs");
    expect(fetchMock.mock.calls[0][1].headers).toEqual({ "Content-Type": "application/json", Authorization: "Bearer private-credential" });
    expect(fetchMock.mock.calls[0][1].body).not.toContain("PRIVATE");
    expect(records()).toHaveLength(4);
    expect(records()[2]).toMatchObject({ traceId, spanId, severityNumber: 9, observedTimeUnixNano: expect.stringMatching(/^[0-9]+$/) });
    expect(decode(records()[2].body)).toEqual({ type: "session.result", data: { numTurns: 0, usage: { inputTokens: 10, inputTokensIncludeCache: false } }, provenance: { component: "agent" } });
    const attributes = Object.fromEntries(payload().resourceLogs[0].resource.attributes.map(entry => [entry.key, decode(entry.value)]));
    expect(attributes).toMatchObject({ "service.name": "gh-aw", "gh-aw.session.included_events": 4, "gh-aw.session.omitted_events": 2, "gh-aw.session.truncated": false });
  });

  it("fans out with isolated credentials and normalizes signal-specific URLs", async () => {
    vi.stubEnv(
      "GH_AW_OTLP_ENDPOINTS",
      JSON.stringify([
        { url: "https://first.example/v1/traces", headers: "Authorization=Bearer%20first" },
        { url: "https://second.example/v1/logs/", headers: "x-api-key=second" },
      ])
    );
    await sendSessionLogs({ sessionPath });
    expect(fetchMock.mock.calls.map(call => call[0])).toEqual(["https://first.example/v1/logs", "https://second.example/v1/logs"]);
    expect(fetchMock.mock.calls[0][1].headers).not.toHaveProperty("x-api-key");
    expect(fetchMock.mock.calls[1][1].headers).not.toHaveProperty("Authorization");
    expect(payload(0)).toEqual(payload(1));
  });

  it.each(["OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"])("exports the compact session as protobuf through %s", async protocolVariable => {
    vi.spyOn(Date, "now").mockReturnValue(1700000000000);
    write([
      { type: "session.format", data: { version: 1 } },
      { type: "assistant.message", data: { content: "PRIVATE_CONVERSATION" } },
      { type: "session.result", data: { numTurns: 2, sessionId: "PRIVATE_SESSION", usage: { inputTokens: 10, inputTokensIncludeCache: false } } },
    ]);
    await sendSessionLogs({ sessionPath });
    const expected = encodeOTLPLogsRequest(payload());
    fetchMock.mockClear();
    vi.stubEnv(protocolVariable, "http/protobuf");
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe("https://collector.example/otlp/v1/logs");
    expect(options.headers["Content-Type"]).toBe("application/x-protobuf");
    expect(options.body).toEqual(expected);
    expect(options.body.includes(Buffer.from("PRIVATE"))).toBe(false);
    expect(warning).not.toHaveBeenCalled();
  });

  it("applies current publication and exporter credential masks", async () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "CUSTOM");
    vi.stubEnv("SECRET_CUSTOM", "private-model");
    write([{ type: "workflow.info", data: { model: "private-model", agentVersion: "private-credential" } }]);
    await sendSessionLogs({ sessionPath });
    expect(fetchMock.mock.calls[0][1].body).not.toContain("private-model");
    expect(fetchMock.mock.calls[0][1].body).not.toContain("private-credential");
  });

  it("drops unauthorized sensitive keys but preserves unconfigured secret-like metadata", async () => {
    const model = "token=nonsecret-metadata";
    write([
      {
        type: "workflow.info",
        data: {
          model,
          api_key: "PRIVATE_API_KEY",
          authorization: "PRIVATE_AUTHORIZATION",
          password: "PRIVATE_PASSWORD",
          extra: { credential: "PRIVATE_NESTED_CREDENTIAL" },
        },
      },
      { type: "firewall.token_usage", data: { model, usage: { inputTokens: 10, outputTokens: 3, secret: "PRIVATE_USAGE_SECRET" } } },
    ]);
    await sendSessionLogs({ sessionPath });
    expect(fetchMock.mock.calls[0][1].body).not.toContain("PRIVATE");
    expect(records().map(record => decode(record.body))).toEqual([
      { type: "workflow.info", data: { model } },
      { type: "firewall.token_usage", data: { model, usage: { inputTokens: 10, outputTokens: 3 } } },
    ]);
  });

  it.each(["", "not-json", JSON.stringify([{ url: "https://collector.example" }]), JSON.stringify([{ url: "https://collector.example", headers: "Authorization=Bearer" }])])(
    "does not export with missing endpoint credentials: %s",
    async endpoints => {
      vi.stubEnv("GH_AW_OTLP_ENDPOINTS", endpoints);
      await sendSessionLogs({ sessionPath });
      expect(fetchMock).not.toHaveBeenCalled();
    }
  );

  it("retains compact limits and reports truncation", async () => {
    write(Array.from({ length: 200 }, () => ({ type: "usage.report", data: { aic: 1 } })));
    await sendSessionLogs({ sessionPath });
    expect(records()).toHaveLength(128);
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.truncated", value: { boolValue: true } });
    expect(Buffer.byteLength(fetchMock.mock.calls[0][1].body)).toBeLessThanOrEqual(64 * 1024);
    expect(partialWarning).toHaveBeenCalledWith(expect.stringMatching(/^::warning::\[otlp\] Session export is partial/));
  });

  it("retains oversized-line and source-scan quality without guessing omitted population", async () => {
    const { MAX_SOURCE_LINE_BYTES, MAX_SOURCE_SCAN_BYTES } = require("./compact_unified_session.cjs");
    fs.writeFileSync(sessionPath, '{"type":"session.format","data":{"version":1}}\n' + " ".repeat(MAX_SOURCE_LINE_BYTES + 1) + "\n");
    const fd = fs.openSync(sessionPath, "r+");
    try {
      fs.ftruncateSync(fd, MAX_SOURCE_SCAN_BYTES + 1);
    } finally {
      fs.closeSync(fd);
    }
    await sendSessionLogs({ sessionPath });
    expect(records()).toHaveLength(1);
    const attributes = Object.fromEntries(payload().resourceLogs[0].resource.attributes.map(entry => [entry.key, decode(entry.value)]));
    expect(attributes).toMatchObject({
      "gh-aw.session.included_events": 1,
      "gh-aw.session.omitted_events": 1,
      "gh-aw.session.oversized_lines": 1,
      "gh-aw.session.scan_truncated": true,
      "gh-aw.session.truncated": true,
      "gh-aw.session.untimed_events": 1,
    });
    expect(partialWarning).toHaveBeenCalledWith(expect.stringContaining("Session export is partial"));
  });

  it("enforces the exact UTF-8 request byte budget with whole records and accurate counts", async () => {
    write(Array.from({ length: 20 }, () => ({ type: "usage.report", data: { aic: 1, model: "模型" } })));
    await sendSessionLogs({ sessionPath });
    const fullSize = Buffer.byteLength(fetchMock.mock.calls[0][1].body);
    fetchMock.mockClear();
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", String(fullSize));
    await sendSessionLogs({ sessionPath });
    expect(Buffer.byteLength(fetchMock.mock.calls[0][1].body)).toBe(fullSize);
    expect(records()).toHaveLength(20);
    fetchMock.mockClear();
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", String(fullSize - 1));
    await sendSessionLogs({ sessionPath });
    expect(Buffer.byteLength(fetchMock.mock.calls[0][1].body)).toBeLessThanOrEqual(fullSize - 1);
    expect(records()).toHaveLength(19);
    const attributes = Object.fromEntries(payload().resourceLogs[0].resource.attributes.map(entry => [entry.key, decode(entry.value)]));
    expect(attributes).toMatchObject({ "gh-aw.session.included_events": 19, "gh-aw.session.omitted_events": 1, "gh-aw.session.truncated": true, "gh-aw.session.untimed_events": 19 });
    expect(partialWarning).toHaveBeenCalledWith(expect.stringContaining("19 compact events; 1 inspected records omitted"));
  });

  it.each([true, false])("counts only admitted untimed records when the priority record is timed: %j", async timed => {
    const priority = { type: "session.result", data: { numTurns: 0 }, ...(timed ? { timestamp: "2026-10-20T19:00:00Z" } : {}) };
    write([priority]);
    await sendSessionLogs({ sessionPath });
    const singleRecordSize = Buffer.byteLength(fetchMock.mock.calls[0][1].body);
    fetchMock.mockClear();
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", String(singleRecordSize));
    write([{ type: "usage.report", data: { aic: 1 }, ...(!timed ? { timestamp: "2026-10-20T19:00:00Z" } : {}) }, priority]);
    await sendSessionLogs({ sessionPath });
    expect(records()).toHaveLength(1);
    expect(decode(records()[0].body).type).toBe("session.result");
    const attributes = Object.fromEntries(payload().resourceLogs[0].resource.attributes.map(entry => [entry.key, decode(entry.value)]));
    expect(attributes).toMatchObject({
      "gh-aw.session.included_events": 1,
      "gh-aw.session.omitted_events": 1,
      "gh-aw.session.untimed_events": timed ? 0 : 1,
    });
    expect(Buffer.byteLength(fetchMock.mock.calls[0][1].body)).toBeLessThanOrEqual(singleRecordSize);
  });

  it("drops low-priority usage, not late outcomes, at the transport byte cap", async () => {
    write([
      { type: "session.format", data: { version: 1 } },
      ...Array.from({ length: 200 }, (_, stepIndex) => ({ type: "usage.report", data: { stepIndex, aic: 1 } })),
      { type: "workflow.info", data: { engineId: "copilot" } },
      { type: "execution.result", data: { outcome: "failure", exitCode: 1 } },
      { type: "grader.result", data: { id: "quality", passed: false } },
    ]);
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", "3000");
    await sendSessionLogs({ sessionPath });
    const retained = records().map(record => decode(record.body));
    expect(Buffer.byteLength(fetchMock.mock.calls[0][1].body)).toBeLessThanOrEqual(3000);
    expect(retained.filter(event => event.type !== "usage.report").map(event => event.type)).toEqual(["session.format", "workflow.info", "execution.result", "grader.result"]);
    expect(retained.filter(event => event.type === "usage.report")).toHaveLength(1);
    expect(retained.find(event => event.type === "execution.result").data).toEqual({ outcome: "failure", exitCode: 1 });
    expect(partialWarning.mock.calls.filter(call => String(call[0]).startsWith("::warning::"))).toHaveLength(1);
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.truncated", value: { boolValue: true } });
  });

  it("removes grader run identity but keeps grader ids and counts", async () => {
    write([
      { type: "session.format", data: { version: 1 } },
      { type: "grader.result", data: { id: "quality", passed: true, run: { id: "PRIVATE_GRADER_RUN_ID", attempt: 2, graderCount: 3, passed: 3, failed: 0, errors: 0 } } },
    ]);
    await sendSessionLogs({ sessionPath });
    const grader = records()
      .map(record => decode(record.body))
      .find(event => event.type === "grader.result");
    expect(grader.data.id).toBe("quality");
    expect(grader.data.run).toEqual({ graderCount: 3, passed: 3, failed: 0, errors: 0 });
    expect(fetchMock.mock.calls[0][1].body).not.toContain("PRIVATE_GRADER_RUN_ID");
    expect(grader.data.run).not.toHaveProperty("attempt");
  });

  it("skips an oversized higher-priority record without evicting a smaller failure", async () => {
    const header = { type: "session.format", data: { version: 1 } };
    const failure = { type: "execution.result", data: { outcome: "failure", exitCode: 1 } };
    write([header, failure]);
    await sendSessionLogs({ sessionPath });
    // Account for the truncation/count metadata that the three-record source will produce.
    const boundedPayload = payload();
    for (const attr of boundedPayload.resourceLogs[0].resource.attributes) {
      if (attr.key === "gh-aw.session.omitted_events") attr.value.intValue = 1;
      if (attr.key === "gh-aw.session.truncated") attr.value.boolValue = true;
    }
    const maxBytes = Buffer.byteLength(JSON.stringify(boundedPayload));
    fetchMock.mockClear();
    write([header, failure, { type: "workflow.info", data: { model: "模型".repeat(40) } }]);
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", String(maxBytes));
    await sendSessionLogs({ sessionPath });
    expect(records().map(record => decode(record.body))).toEqual([header, failure]);
    expect(Buffer.byteLength(fetchMock.mock.calls[0][1].body)).toBe(maxBytes);
    expect(payload().resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.omitted_events", value: { intValue: 1 } });
    expect(partialWarning).toHaveBeenCalledExactlyOnceWith(expect.stringMatching(/^::warning::\[otlp\] Session export is partial: retained 2 compact events; 1 inspected records omitted\./));
  });

  it("does not warn about a partial export when only routine privacy filtering applies", async () => {
    write([
      { type: "session.format", data: { version: 1 } },
      { type: "assistant.message", data: { content: "private" } },
    ]);
    await sendSessionLogs({ sessionPath });
    expect(partialWarning.mock.calls.filter(call => String(call[0]).startsWith("::warning::"))).toHaveLength(0);
  });

  it.each(["0", "-1", "1.5", "NaN", "Infinity", "9007199254740992", "1e4", " "])("fails closed for invalid byte limit %j", async maxBytes => {
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", maxBytes);
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("must be a positive safe integer"));
  });

  it("warns without sending when even one record cannot fit the size limit", async () => {
    vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", "1");
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("No compact session records fit"));
  });

  it.each([
    ["missing", "Error ENOENT"],
    ["malformed", "Error ERR_VALIDATION"],
    ["symlink", "Error ELOOP"],
  ])("warns without exporting %s artifacts or private diagnostics", async (kind, diagnostic) => {
    if (kind === "missing") fs.unlinkSync(sessionPath);
    if (kind === "malformed") fs.writeFileSync(sessionPath, "PRIVATE_INVALID_JSON");
    if (kind === "symlink") {
      fs.renameSync(sessionPath, path.join(directory, "source"));
      fs.symlinkSync(path.join(directory, "source"), sessionPath);
    }
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(warning).toHaveBeenCalledOnce();
    const message = warning.mock.calls.flat().join(" ");
    expect(message).toContain(`(${diagnostic})`);
    for (const privateValue of ["PRIVATE", directory, sessionPath, "private-credential", "collector.example"]) expect(message).not.toContain(privateValue);
  });

  it("reports unexpected failures without exposing their message or custom error name", async () => {
    const error = new TypeError(`PRIVATE_SERIALIZER ${sessionPath} private-credential https://collector.example/otlp`);
    error.name = "PRIVATE_ERROR_NAME";
    error.code = "PRIVATE_ERROR_CODE";
    vi.spyOn(fs, "readSync").mockImplementation(() => {
      throw error;
    });
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(warning).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("(TypeError)"));
    const message = warning.mock.calls.flat().join(" ");
    for (const privateValue of ["PRIVATE", sessionPath, "private-credential", "collector.example"]) expect(message).not.toContain(privateValue);
  });

  it("omits invalid trace context instead of fabricating identifiers", async () => {
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "invalid");
    await sendSessionLogs({ sessionPath });
    expect(records()[0]).not.toHaveProperty("traceId");
    expect(records()[0]).not.toHaveProperty("spanId");
  });

  it("does not retry authorization failures and still delivers to other vendors", async () => {
    vi.stubEnv(
      "GH_AW_OTLP_ENDPOINTS",
      JSON.stringify([
        { url: "https://denied.example", headers: "x-api-key=denied" },
        { url: "https://ok.example", headers: "x-api-key=ok" },
      ])
    );
    fetchMock.mockResolvedValueOnce({ ok: false, status: 403 }).mockResolvedValueOnce({ ok: true, status: 200 });
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("authorization failed"));
  });

  it("retries transient failures using the existing OTLP transport", async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 503 }).mockResolvedValueOnce({ ok: true, status: 200 });
    await sendSessionLogs({ sessionPath });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it.each(["http/json", "http/protobuf"])("delivers bounded OTLP %s to a real local logs collector", async protocol => {
    let received;
    const server = http.createServer((request, response) => {
      const chunks = [];
      request.on("data", chunk => chunks.push(chunk));
      request.on("end", () => {
        received = { url: request.url, headers: request.headers, body: Buffer.concat(chunks) };
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end("{}");
      });
    });
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    try {
      write([
        { type: "session.result", timestamp: "2026-10-20T19:00:00.123456789Z", data: { numTurns: 0 } },
        { type: "session.format", data: { version: 1 } },
      ]);
      vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify([{ url: `http://127.0.0.1:${server.address().port}`, headers: "x-api-key=local-test" }]));
      vi.stubEnv("GH_AW_OTLP_LOGS_MAX_BYTES", "2048");
      vi.spyOn(Date, "now").mockReturnValue(1700000000000);
      await sendSessionLogs({ sessionPath });
      const jsonPayload = payload();
      expect(jsonPayload.resourceLogs[0].scopeLogs[0].logRecords[0]).toMatchObject({ timeUnixNano: "1792522800123456789", observedTimeUnixNano: "1700000000000000000", traceId, spanId });
      expect(jsonPayload.resourceLogs[0].resource.attributes).toContainEqual({ key: "gh-aw.session.untimed_events", value: { intValue: 1 } });
      fetchMock.mockClear();
      vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol);
      fetchMock.mockImplementation(nativeFetch);
      await sendSessionLogs({ sessionPath });
      expect(received.url).toBe("/v1/logs");
      expect(received.headers["x-api-key"]).toBe("local-test");
      expect(received.headers["content-type"]).toBe(protocol === "http/protobuf" ? "application/x-protobuf" : "application/json");
      expect(Buffer.byteLength(received.body)).toBeLessThanOrEqual(2048);
      expect(received.body).toEqual(protocol === "http/protobuf" ? encodeOTLPLogsRequest(jsonPayload) : Buffer.from(JSON.stringify(jsonPayload)));
    } finally {
      await new Promise(resolve => server.close(resolve));
    }
  });
});
