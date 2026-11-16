// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");

const OTEL_METRICS_JSONL_PATH = "/tmp/gh-aw/otel-metrics.jsonl";
const DEFAULT_HISTOGRAM_BUCKETS = [1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600];
const PROCESS_START_MS = Date.now();
const lastEndBySeries = new Map();

/**
 * @param {number} ms
 * @returns {string}
 */
function toUnixNano(ms) {
  return (BigInt(Math.round(ms * 1000)) * 1000n).toString();
}

/**
 * @param {number} value
 * @returns {{asInt: string} | {asDouble: number}}
 */
function buildNumberValue(value) {
  return Number.isSafeInteger(value) ? { asInt: String(value) } : { asDouble: value };
}

/**
 * @param {{
 *   name: string,
 *   value: number,
 *   type?: "counter" | "gauge" | "histogram",
 *   unit?: string,
 *   description?: string,
 *   attributes?: Array<{key: string, value: object}>,
 *   buckets?: number[],
 *   startMs?: number,
 *   endMs?: number,
 * }} opts
 * @returns {any}
 */
function buildOTLPMetric({ name, value, type = "gauge", unit = "", description = "", attributes = [], buckets = DEFAULT_HISTOGRAM_BUCKETS, startMs, endMs }) {
  if (typeof name !== "string" || name.trim() === "" || name.length > 255) {
    throw new Error("metric name must be a non-empty string of at most 255 characters");
  }
  if (!["counter", "gauge", "histogram"].includes(type)) {
    throw new Error(`unsupported metric type: ${type}`);
  }
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new Error(`metric ${name}: value must be a finite number`);
  }
  if (type === "counter" && value < 0) {
    throw new Error(`metric ${name}: counter value must not be negative`);
  }
  const end = endMs ?? Date.now();
  const start = Math.min(startMs ?? PROCESS_START_MS, end);
  const base = { name, ...(unit ? { unit } : {}), ...(description ? { description } : {}) };
  if (type === "gauge") {
    return { ...base, gauge: { dataPoints: [{ attributes, timeUnixNano: toUnixNano(end), ...buildNumberValue(value) }] } };
  }
  if (type === "counter") {
    return {
      ...base,
      sum: {
        aggregationTemporality: 1,
        isMonotonic: true,
        dataPoints: [{ attributes, startTimeUnixNano: toUnixNano(start), timeUnixNano: toUnixNano(end), ...buildNumberValue(value) }],
      },
    };
  }
  if (!Array.isArray(buckets) || buckets.some(bound => typeof bound !== "number" || !Number.isFinite(bound))) {
    throw new Error(`metric ${name}: histogram buckets must be finite numbers`);
  }
  const bounds = [...buckets].sort((a, b) => a - b);
  if (bounds.some((bound, index) => index > 0 && bound <= bounds[index - 1])) {
    throw new Error(`metric ${name}: histogram buckets must be strictly increasing`);
  }
  const bucketCounts = new Array(bounds.length + 1).fill("0");
  const index = bounds.findIndex(bound => value <= bound);
  bucketCounts[index < 0 ? bounds.length : index] = "1";
  return {
    ...base,
    histogram: {
      aggregationTemporality: 1,
      dataPoints: [
        {
          attributes,
          startTimeUnixNano: toUnixNano(start),
          timeUnixNano: toUnixNano(end),
          count: "1",
          sum: value,
          min: value,
          max: value,
          bucketCounts,
          explicitBounds: bounds,
        },
      ],
    },
  };
}

/**
 * @param {{serviceName: string, scopeVersion?: string, resourceAttributes?: Array<{key: string, value: object}>, metrics: any[]}} opts
 * @returns {any}
 */
function buildOTLPMetricsPayload({ serviceName, scopeVersion, resourceAttributes, metrics }) {
  const { buildOTLPResourceAttributes } = require("./send_otlp_span.cjs");
  return {
    resourceMetrics: [
      {
        resource: { attributes: buildOTLPResourceAttributes(serviceName, scopeVersion, resourceAttributes) },
        scopeMetrics: [{ scope: { name: "gh-aw", version: scopeVersion || "unknown" }, metrics }],
      },
    ],
  };
}

/**
 * @param {any} payload
 * @returns {any}
 */
function sanitizeOTLPMetricsPayload(payload) {
  if (!payload || !Array.isArray(payload.resourceMetrics)) return payload;
  const { sanitizeAttrs } = require("./send_otlp_span.cjs");
  const sanitizeDataPoints = section => (section && Array.isArray(section.dataPoints) ? { ...section, dataPoints: section.dataPoints.map(point => ({ ...point, attributes: sanitizeAttrs(point.attributes) })) } : section);
  return {
    ...payload,
    resourceMetrics: payload.resourceMetrics.map(resourceMetrics => ({
      ...resourceMetrics,
      resource: resourceMetrics.resource ? { ...resourceMetrics.resource, attributes: sanitizeAttrs(resourceMetrics.resource.attributes) } : resourceMetrics.resource,
      scopeMetrics: Array.isArray(resourceMetrics.scopeMetrics)
        ? resourceMetrics.scopeMetrics.map(scopeMetrics => ({
            ...scopeMetrics,
            metrics: Array.isArray(scopeMetrics.metrics)
              ? scopeMetrics.metrics.map(metric => ({
                  ...metric,
                  gauge: sanitizeDataPoints(metric.gauge),
                  sum: sanitizeDataPoints(metric.sum),
                  histogram: sanitizeDataPoints(metric.histogram),
                }))
              : scopeMetrics.metrics,
          }))
        : resourceMetrics.scopeMetrics,
    })),
  };
}

/**
 * @param {any} payload
 */
function appendToOTLPMetricsJSONL(payload) {
  try {
    fs.mkdirSync(path.dirname(OTEL_METRICS_JSONL_PATH), { recursive: true });
    fs.appendFileSync(OTEL_METRICS_JSONL_PATH, JSON.stringify(payload) + "\n");
  } catch {
    // Mirroring is best-effort.
  }
}

/**
 * @param {any} payload
 * @returns {Promise<void>}
 */
async function sendOTLPMetrics(payload) {
  if (process.env.GH_AW_OTEL_METRICS !== "true") return;
  const { parseOTLPEndpoints, sendOTLPToAllEndpoints } = require("./send_otlp_span.cjs");
  const sanitizedPayload = sanitizeOTLPMetricsPayload(payload);
  appendToOTLPMetricsJSONL(sanitizedPayload);
  /** @type {{skipJSONL: true, signal: "metrics", sanitize: (payload: any) => any, protocolOverride: "http/json"}} */
  const options = { skipJSONL: true, signal: "metrics", sanitize: sanitizeOTLPMetricsPayload, protocolOverride: "http/json" };
  await sendOTLPToAllEndpoints(parseOTLPEndpoints(), sanitizedPayload, options);
}

/**
 * @param {{
 *   jobName?: string,
 *   workflow?: string,
 *   engine?: string,
 *   model?: string,
 *   conclusion?: string,
 *   startMs: number,
 *   endMs: number,
 *   inputTokens?: number,
 *   outputTokens?: number,
 *   emitsTokenUsage?: boolean,
 *   toolCalls?: number,
 *   failedToolCalls?: number,
 *   outputItemTypes?: string[],
 * }} opts
 * @returns {any[]}
 */
function buildBuiltinRunMetrics({ jobName = "", workflow = "", engine = "", model = "", conclusion = "", startMs, endMs, inputTokens, outputTokens, emitsTokenUsage = false, toolCalls, failedToolCalls, outputItemTypes = [] }) {
  const { buildAttr } = require("./send_otlp_span.cjs");
  const commonAttributes = [...(jobName ? [buildAttr("gh-aw.job.name", jobName)] : []), ...(workflow ? [buildAttr("gh-aw.workflow.name", workflow)] : [])];
  const attributes = (...extra) => [...commonAttributes, ...extra];
  const metrics = [
    buildOTLPMetric({
      name: "gh_aw.run.duration",
      type: "histogram",
      unit: "s",
      description: "Wall-clock job duration",
      value: Math.max(0, (endMs - startMs) / 1000),
      attributes: attributes(buildAttr("cicd.pipeline.task.run.result", conclusion)),
      startMs,
      endMs,
    }),
  ];
  if (conclusion === "failure" || conclusion === "timed_out") {
    metrics.push(buildOTLPMetric({ name: "gh_aw.run.errors", type: "counter", unit: "{run}", value: 1, attributes: attributes(buildAttr("cicd.pipeline.task.run.result", conclusion)), startMs, endMs }));
  }
  if (emitsTokenUsage) {
    const tokenMetrics = [
      { name: "gh_aw.tokens.input", value: inputTokens },
      { name: "gh_aw.tokens.output", value: outputTokens },
    ];
    for (const { name, value } of tokenMetrics) {
      if (typeof value === "number" && Number.isFinite(value) && value >= 0) {
        metrics.push(buildOTLPMetric({ name, type: "counter", unit: "{token}", value, attributes: attributes(buildAttr("gen_ai.request.model", model), buildAttr("gh-aw.engine.id", engine)), startMs, endMs }));
      }
    }
  }
  if (typeof toolCalls === "number" && Number.isFinite(toolCalls) && toolCalls > 0) {
    const failed = typeof failedToolCalls === "number" && Number.isFinite(failedToolCalls) ? Math.min(Math.max(0, failedToolCalls), toolCalls) : 0;
    const toolStatuses = [
      { status: "ok", value: toolCalls - failed },
      { status: "error", value: failed },
    ];
    for (const { status, value } of toolStatuses) {
      if (value > 0) {
        metrics.push(buildOTLPMetric({ name: "gh_aw.tool_calls", type: "counter", unit: "{call}", value, attributes: attributes(buildAttr("gh-aw.tool.status", status)), startMs, endMs }));
      }
    }
  }
  const outputCounts = new Map();
  for (const type of outputItemTypes) {
    if (typeof type === "string" && type) outputCounts.set(type, (outputCounts.get(type) || 0) + 1);
  }
  for (const [type, value] of outputCounts) {
    metrics.push(buildOTLPMetric({ name: "gh_aw.safe_outputs", type: "counter", unit: "{item}", value, attributes: attributes(buildAttr("gh-aw.safe_output.type", type)), startMs, endMs }));
  }
  return metrics;
}

/**
 * @param {string} name
 * @param {number} value
 * @param {{type?: "counter" | "gauge" | "histogram", unit?: string, description?: string, attributes?: Record<string, string | number | boolean>, buckets?: number[]}} [options]
 * @returns {Promise<void>}
 */
async function logMetric(name, value, options = {}) {
  if (process.env.GH_AW_OTEL_METRICS !== "true") return;
  try {
    const { buildAttr, sanitizeAttrs } = require("./send_otlp_span.cjs");
    const attributes = Object.entries(options.attributes || {}).map(([key, attribute]) => {
      if (!["string", "number", "boolean"].includes(typeof attribute) || (typeof attribute === "number" && !Number.isFinite(attribute))) {
        throw new Error("metric attributes must be strings, finite numbers, or booleans");
      }
      return buildAttr(key, attribute);
    });
    const sanitizedAttributes = sanitizeAttrs(attributes);
    const canonicalAttributes = /** @type {Array<[string, string]>} */ sanitizedAttributes.map(attribute => [attribute.key, JSON.stringify(attribute.value) || ""]);
    canonicalAttributes.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
    const seriesKey = JSON.stringify([name, options.type || "gauge", canonicalAttributes]);
    const now = Date.now();
    const isGauge = (options.type || "gauge") === "gauge";
    const startMs = isGauge ? now : (lastEndBySeries.get(seriesKey) ?? PROCESS_START_MS);
    const endMs = isGauge ? now : Math.max(now, startMs + 0.001);
    const metric = buildOTLPMetric({ name, value, type: options.type, unit: options.unit, description: options.description, buckets: options.buckets, attributes, startMs, endMs });
    if (!isGauge) lastEndBySeries.set(seriesKey, endMs);
    const resourceAttributes = [
      ...(process.env.GITHUB_EVENT_NAME ? [buildAttr("github.event_name", process.env.GITHUB_EVENT_NAME)] : []),
      ...(process.env.GITHUB_JOB ? [buildAttr("github.job", process.env.GITHUB_JOB)] : []),
      ...(process.env.GITHUB_WORKFLOW ? [buildAttr("gh-aw.workflow.name", process.env.GITHUB_WORKFLOW)] : []),
    ];
    const payload = buildOTLPMetricsPayload({
      serviceName: process.env.OTEL_SERVICE_NAME || "gh-aw",
      scopeVersion: process.env.GH_AW_INFO_CLI_VERSION || "unknown",
      resourceAttributes,
      metrics: [metric],
    });
    await sendOTLPMetrics(payload);
  } catch {
    console.warn("[otlp] failed to emit custom metric");
  }
}

module.exports = {
  OTEL_METRICS_JSONL_PATH,
  DEFAULT_HISTOGRAM_BUCKETS,
  buildOTLPMetric,
  buildOTLPMetricsPayload,
  sanitizeOTLPMetricsPayload,
  appendToOTLPMetricsJSONL,
  sendOTLPMetrics,
  buildBuiltinRunMetrics,
  logMetric,
};
