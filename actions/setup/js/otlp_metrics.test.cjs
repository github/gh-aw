// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const metrics = require("./otlp_metrics.cjs");
const { buildBuiltinRunMetrics } = metrics;

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("buildOTLPMetric", () => {
  it("builds counters, gauges, and histogram observations in OTLP JSON form", () => {
    const counter = metrics.buildOTLPMetric({ name: "requests", value: 2, type: "counter", startMs: 1000, endMs: 2000 });
    expect(counter.sum.dataPoints[0]).toMatchObject({ asInt: "2", startTimeUnixNano: "1000000000", timeUnixNano: "2000000000" });

    const gauge = metrics.buildOTLPMetric({ name: "queue.depth", value: 0.5, type: "gauge", endMs: 2000 });
    expect(gauge.gauge.dataPoints[0]).toMatchObject({ asDouble: 0.5, timeUnixNano: "2000000000" });

    const histogram = metrics.buildOTLPMetric({ name: "duration", value: 2, type: "histogram", buckets: [5, 1], startMs: 1000, endMs: 2000 });
    expect(histogram.histogram.dataPoints[0]).toMatchObject({ count: "1", bucketCounts: ["0", "1", "0"], explicitBounds: [1, 5] });
  });

  it("rejects invalid metric values and buckets", () => {
    expect(() => metrics.buildOTLPMetric({ name: "counter", value: -1, type: "counter" })).toThrow("must not be negative");
    expect(() => metrics.buildOTLPMetric({ name: "histogram", value: 1, type: "histogram", buckets: [1, 1] })).toThrow("strictly increasing");
    expect(() => metrics.buildOTLPMetric({ name: "", value: 1 })).toThrow("metric name");
  });
});

describe("buildBuiltinRunMetrics", () => {
  it("builds the opt-in duration, error, token, tool-call, and safe-output metrics", () => {
    const result = buildBuiltinRunMetrics({
      jobName: "agent",
      workflow: "CI",
      engine: "copilot",
      model: "test-model",
      conclusion: "failure",
      startMs: 1000,
      endMs: 3000,
      inputTokens: 10,
      outputTokens: 3,
      emitsTokenUsage: true,
      toolCalls: 4,
      failedToolCalls: 1,
      outputItemTypes: ["issue", "issue", "pull_request"],
    });

    expect(result.map(metric => metric.name)).toEqual(["gh_aw.run.duration", "gh_aw.run.errors", "gh_aw.tokens.input", "gh_aw.tokens.output", "gh_aw.tool_calls", "gh_aw.tool_calls", "gh_aw.safe_outputs", "gh_aw.safe_outputs"]);
    expect(result[0].histogram.dataPoints[0].sum).toBe(2);
    expect(result[1].sum.dataPoints[0].asInt).toBe("1");
    expect(result[4].sum.dataPoints[0].attributes).toContainEqual({ key: "gh-aw.tool.status", value: { stringValue: "ok" } });
    expect(result[5].sum.dataPoints[0].attributes).toContainEqual({ key: "gh-aw.tool.status", value: { stringValue: "error" } });
    expect(result[6].sum.dataPoints[0].attributes).toContainEqual({ key: "gh-aw.safe_output.type", value: { stringValue: "issue" } });
  });

  it("omits the error counter for successful runs and unavailable usage", () => {
    const result = buildBuiltinRunMetrics({ conclusion: "success", startMs: 1000, endMs: 2000 });
    expect(result.map(metric => metric.name)).toEqual(["gh_aw.run.duration"]);
  });
});

describe("OTLP metrics export", () => {
  it("mirrors before exporting JSON to the metrics endpoint", async () => {
    vi.stubEnv("GH_AW_OTEL_METRICS", "true");
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify([{ url: "https://collector.example/v1/traces", headers: "X-Trace-Endpoint=metrics" }]));
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    const mirror = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, body: null, headers: new Headers() });
    vi.stubGlobal("fetch", fetchMock);
    const payload = metrics.buildOTLPMetricsPayload({ serviceName: "gh-aw", metrics: [metrics.buildOTLPMetric({ name: "test", value: 1 })] });

    await metrics.sendOTLPMetrics(payload);

    expect(mirror).toHaveBeenCalledWith(metrics.OTEL_METRICS_JSONL_PATH, expect.stringContaining('"resourceMetrics"'));
    expect(mirror.mock.invocationCallOrder[0]).toBeLessThan(fetchMock.mock.invocationCallOrder[0]);
    expect(fetchMock.mock.calls[0][0]).toBe("https://collector.example/v1/metrics");
    expect(fetchMock.mock.calls[0][1].headers).toMatchObject({ "Content-Type": "application/json", "X-Trace-Endpoint": "metrics" });
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).resourceMetrics).toBeDefined();
  });

  it("does not mirror or export when the opt-in flag is absent", async () => {
    const mirror = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const payload = metrics.buildOTLPMetricsPayload({ serviceName: "gh-aw", metrics: [metrics.buildOTLPMetric({ name: "test", value: 1 })] });

    await metrics.sendOTLPMetrics(payload);

    expect(mirror).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("exposes a non-fatal custom metric API that is also opt-in", async () => {
    const mirror = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    await expect(require("./otlp.cjs").logMetric("custom.items", 3, { type: "counter" })).resolves.toBeUndefined();
    expect(mirror).not.toHaveBeenCalled();

    vi.stubEnv("GH_AW_OTEL_METRICS", "true");
    await expect(require("./otlp.cjs").logMetric("custom.items", 3, { type: "counter" })).resolves.toBeUndefined();
    expect(mirror).toHaveBeenCalledWith(metrics.OTEL_METRICS_JSONL_PATH, expect.stringContaining('"custom.items"'));
  });
});
