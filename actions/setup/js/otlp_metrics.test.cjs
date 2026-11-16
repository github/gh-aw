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
      cacheReadTokens: 5,
      cacheWriteTokens: 2,
      emitsTokenUsage: true,
      turns: 4,
      toolCalls: 4,
      failedToolCalls: 1,
      outputItemTypes: ["issue", "issue", "pull_request"],
      maxAICreditsExceeded: true,
      aiCreditsRateLimitError: true,
      exportFailures: 2,
      traceContext: { traceId: "1234567890abcdef1234567890abcdef", spanId: "1234567890abcdef" },
    });

    expect(result.map(metric => metric.name)).toEqual([
      "gh_aw.job.duration",
      "gh_aw.run.duration",
      "gh_aw.job.errors",
      "gh_aw.run.errors",
      "gen_ai.client.token.usage",
      "gh_aw.tokens.input",
      "gen_ai.client.token.usage",
      "gh_aw.tokens.output",
      "gen_ai.client.token.usage",
      "gh_aw.tokens.cache_read",
      "gen_ai.client.token.usage",
      "gh_aw.tokens.cache_write",
      "gh_aw.agent.turns",
      "gh_aw.tool_calls",
      "gh_aw.tool_calls",
      "gh_aw.ai.credits.limit_exceeded",
      "gh_aw.ai.credits.rate_limit_errors",
      "gh_aw.otlp.export.failures",
      "gh_aw.workflow.output.items",
      "gh_aw.safe_outputs",
      "gh_aw.workflow.output.items",
      "gh_aw.safe_outputs",
    ]);
    expect(result[0].histogram.dataPoints[0].sum).toBe(2);
    expect(result[0].histogram.dataPoints[0].attributes).toContainEqual({ key: "gh-aw.engine.id", value: { stringValue: "copilot" } });
    expect(result[0].histogram.dataPoints[0].attributes).toContainEqual({ key: "gen_ai.request.model", value: { stringValue: "test-model" } });
    expect(result[0].histogram.dataPoints[0].exemplars[0]).toMatchObject({ traceId: "1234567890abcdef1234567890abcdef", spanId: "1234567890abcdef" });
    expect(result[2].sum.dataPoints[0].asInt).toBe("1");
    expect(result[4].sum.dataPoints[0].attributes).toContainEqual({ key: "gen_ai.token.type", value: { stringValue: "input" } });
    expect(result[8].sum.dataPoints[0].attributes).toContainEqual({ key: "gen_ai.token.type", value: { stringValue: "input_cache_read" } });
    expect(result[12].histogram.dataPoints[0].sum).toBe(4);
    expect(result[13].sum.dataPoints[0].attributes).toContainEqual({ key: "gh-aw.tool.status", value: { stringValue: "ok" } });
    expect(result[14].sum.dataPoints[0].attributes).toContainEqual({ key: "gh-aw.tool.status", value: { stringValue: "error" } });
    expect(result[18].sum.dataPoints[0].attributes).toContainEqual({ key: "gh-aw.safe_output.type", value: { stringValue: "issue" } });
  });

  it("omits the error counter for successful runs and unavailable usage", () => {
    const result = buildBuiltinRunMetrics({ conclusion: "success", startMs: 1000, endMs: 2000 });
    expect(result.map(metric => metric.name)).toEqual(["gh_aw.job.duration", "gh_aw.run.duration"]);
  });

  it("counts cancelled conclusions as job errors", () => {
    expect(buildBuiltinRunMetrics({ conclusion: "cancelled", startMs: 1000, endMs: 2000 }).map(metric => metric.name)).toEqual(["gh_aw.job.duration", "gh_aw.run.duration", "gh_aw.job.errors", "gh_aw.run.errors"]);
  });

  it("keeps metric resource attributes to stable low-cardinality identity", () => {
    vi.stubEnv("OTEL_RESOURCE_ATTRIBUTES", "gh-aw.repository=owner%2Frepo,gh-aw.engine.id=copilot,gh-aw.run.id=123,github.run_id=123,custom.label=ignored");
    const payload = metrics.buildOTLPMetricsPayload({ serviceName: "gh-aw", metrics: [] });
    const attributes = payload.resourceMetrics[0].resource.attributes;
    expect(attributes).toContainEqual({ key: "gh-aw.repository", value: { stringValue: "owner/repo" } });
    expect(attributes).toContainEqual({ key: "gh-aw.engine.id", value: { stringValue: "copilot" } });
    expect(attributes.map(attribute => attribute.key)).not.toContain("gh-aw.run.id");
    expect(attributes.map(attribute => attribute.key)).not.toContain("github.run_id");
    expect(attributes.map(attribute => attribute.key)).not.toContain("custom.label");
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

  it("rejects unbounded custom metric attributes without throwing", async () => {
    vi.stubEnv("GH_AW_OTEL_METRICS", "true");
    const mirror = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    const logMetric = require("./otlp.cjs").logMetric;

    await logMetric("custom.items", 1, { attributes: { "gh-aw.repository": "owner/repo", "gh-aw.run.id": "123" } });
    await logMetric("custom.items", 1, { attributes: Object.fromEntries(Array.from({ length: 9 }, (_, index) => [`label.${index}`, index])) });
    await logMetric("custom.items", 1, { attributes: { ["k".repeat(129)]: "value" } });
    await logMetric("custom.items", 1, { attributes: { "label.value": "v".repeat(129) } });

    expect(mirror).not.toHaveBeenCalled();
  });

  it("records metric endpoint failures locally and exports the failure counter to healthy endpoints", async () => {
    vi.stubEnv("GH_AW_OTEL_METRICS", "true");
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify([{ url: "https://failed.example/v1/traces" }, { url: "https://healthy.example/v1/traces" }]));
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "writeFileSync").mockImplementation(() => undefined);
    const mirror = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    vi.spyOn(globalThis, "setTimeout").mockImplementation(callback => {
      callback();
      return 0;
    });
    const fetchMock = vi.fn(async url =>
      new URL(url).hostname === "failed.example" ? { ok: false, status: 500, statusText: "error", body: null, headers: new Headers() } : { ok: true, status: 200, statusText: "OK", body: null, headers: new Headers() }
    );
    vi.stubGlobal("fetch", fetchMock);
    const payload = metrics.buildOTLPMetricsPayload({ serviceName: "gh-aw", metrics: [metrics.buildOTLPMetric({ name: "custom.items", value: 1 })] });

    await metrics.sendOTLPMetrics(payload, { traceId: "1234567890abcdef1234567890abcdef", spanId: "1234567890abcdef" });

    const mirroredPayloads = mirror.mock.calls.filter(([filePath]) => filePath === metrics.OTEL_METRICS_JSONL_PATH).map(([, line]) => JSON.parse(line));
    const failurePayload = mirroredPayloads.find(entry => entry.resourceMetrics[0].scopeMetrics[0].metrics[0].name === "gh_aw.otlp.export.failures");
    expect(failurePayload).toBeDefined();
    expect(fetchMock.mock.calls.map(([url]) => new URL(url).hostname)).toContain("healthy.example");
    expect(fetchMock.mock.calls.some(([, options]) => JSON.parse(options.body).resourceMetrics[0].scopeMetrics[0].metrics[0].name === "gh_aw.otlp.export.failures")).toBe(true);
  });

  it("caps unique custom metric series per process", async () => {
    vi.stubEnv("GH_AW_OTEL_METRICS", "true");
    const mirror = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    const logMetric = require("./otlp.cjs").logMetric;

    await Promise.all(Array.from({ length: 101 }, (_, index) => logMetric(`custom.series.${index}`, 1)));

    expect(mirror.mock.calls.length).toBeLessThanOrEqual(metrics.MAX_CUSTOM_METRIC_SERIES);
  });
});
