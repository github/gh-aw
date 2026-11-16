// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");

const OTEL_METRICS_JSONL_PATH = "/tmp/gh-aw/otel-metrics.jsonl";
const DEFAULT_HISTOGRAM_BUCKETS = [1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600];
const PROCESS_START_MS = Date.now();
const lastEndBySeries = new Map();
const customMetricSeries = new Set();
const MAX_CUSTOM_METRIC_ATTRIBUTES = 8;
const MAX_CUSTOM_METRIC_SERIES = 100;
const MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH = 128;
const HIGH_CARDINALITY_ATTRIBUTE_KEY_RE = /(?:run[._-]?id|trace[._-]?id|span[._-]?id|commit|sha|actor|user|issue(?:[._-]?number)?|pull[._-]?request(?:[._-]?number)?|conversation|url|job[._-]?id)/i;
const METRIC_RESOURCE_ATTRIBUTE_KEYS = new Set(["gh-aw.repository", "gh-aw.engine.id", "gh-aw.workflow.name", "github.event_name", "github.job"]);

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
 *   traceContext?: {traceId: string, spanId: string},
 * }} opts
 * @returns {any}
 */
function buildOTLPMetric({ name, value, type = "gauge", unit = "", description = "", attributes = [], buckets = DEFAULT_HISTOGRAM_BUCKETS, startMs, endMs, traceContext }) {
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
  const exemplar =
    traceContext && /^[0-9a-f]{32}$/.test(traceContext.traceId) && !/^0+$/.test(traceContext.traceId) && /^[0-9a-f]{16}$/.test(traceContext.spanId) && !/^0+$/.test(traceContext.spanId)
      ? [{ timeUnixNano: toUnixNano(end), traceId: traceContext.traceId, spanId: traceContext.spanId, ...buildNumberValue(value) }]
      : undefined;
  if (type === "gauge") {
    return { ...base, gauge: { dataPoints: [{ attributes, timeUnixNano: toUnixNano(end), ...buildNumberValue(value), ...(exemplar ? { exemplars: exemplar } : {}) }] } };
  }
  if (type === "counter") {
    return {
      ...base,
      sum: {
        aggregationTemporality: 1,
        isMonotonic: true,
        dataPoints: [{ attributes, startTimeUnixNano: toUnixNano(start), timeUnixNano: toUnixNano(end), ...buildNumberValue(value), ...(exemplar ? { exemplars: exemplar } : {}) }],
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
          ...(exemplar ? { exemplars: exemplar } : {}),
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
  const { buildOTLPResourceAttributes, parseOTELResourceAttributes } = require("./send_otlp_span.cjs");
  const allowedResourceAttributes = new Map();
  for (const attribute of [...parseOTELResourceAttributes(process.env.OTEL_RESOURCE_ATTRIBUTES || ""), ...(resourceAttributes || [])]) {
    if (!METRIC_RESOURCE_ATTRIBUTE_KEYS.has(attribute.key)) continue;
    const value = attribute.value && typeof attribute.value === "object" && "stringValue" in attribute.value ? attribute.value.stringValue : undefined;
    if (typeof value !== "string" || value.length > MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH) continue;
    allowedResourceAttributes.set(attribute.key, attribute);
  }
  return {
    resourceMetrics: [
      {
        resource: { attributes: buildOTLPResourceAttributes(serviceName, scopeVersion, [...allowedResourceAttributes.values()]) },
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
async function sendOTLPMetrics(payload, traceContext) {
  if (process.env.GH_AW_OTEL_METRICS !== "true") return;
  const { parseOTLPEndpoints, sendOTLPToAllEndpoints } = require("./send_otlp_span.cjs");
  const sanitizedPayload = sanitizeOTLPMetricsPayload(payload);
  appendToOTLPMetricsJSONL(sanitizedPayload);
  /** @type {{skipJSONL: true, signal: "metrics", sanitize: (payload: any) => any, protocolOverride: "http/json", reportResults: true}} */
  const options = { skipJSONL: true, signal: "metrics", sanitize: sanitizeOTLPMetricsPayload, protocolOverride: "http/json", reportResults: true };
  const endpoints = parseOTLPEndpoints();
  const endpointResults = (await sendOTLPToAllEndpoints(endpoints, sanitizedPayload, options)) || [];
  const failedExports = endpointResults.filter(success => !success).length;
  if (failedExports === 0) return;

  const { buildAttr } = require("./send_otlp_span.cjs");
  const workflowName = process.env.GH_AW_INFO_WORKFLOW_NAME || process.env.GITHUB_WORKFLOW || "";
  const failureMetric = buildOTLPMetric({
    name: "gh_aw.otlp.export.failures",
    type: "counter",
    unit: "{failure}",
    description: "Failed OTLP endpoint exports after bounded retry",
    value: failedExports,
    attributes: [
      ...(process.env.GITHUB_JOB ? [buildAttr("gh-aw.job.name", process.env.GITHUB_JOB.slice(0, MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH))] : []),
      ...(workflowName ? [buildAttr("gh-aw.workflow.name", workflowName.slice(0, MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH))] : []),
      buildAttr("gh-aw.otlp.endpoint.class", "configured"),
      buildAttr("gh-aw.otlp.failure.class", "export"),
    ],
    startMs: PROCESS_START_MS,
    endMs: Date.now(),
    traceContext,
  });
  const failurePayload = sanitizeOTLPMetricsPayload({
    resourceMetrics: sanitizedPayload.resourceMetrics.map(resourceMetrics => ({
      ...resourceMetrics,
      scopeMetrics: resourceMetrics.scopeMetrics.map(scopeMetrics => ({ ...scopeMetrics, metrics: [failureMetric] })),
    })),
  });
  appendToOTLPMetricsJSONL(failurePayload);
  const successfulEndpoints = endpoints.filter((_, index) => endpointResults[index]);
  if (successfulEndpoints.length > 0) {
    await sendOTLPToAllEndpoints(successfulEndpoints, failurePayload, options);
  }
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
 *   cacheReadTokens?: number,
 *   cacheWriteTokens?: number,
 *   emitsTokenUsage?: boolean,
 *   turns?: number,
 *   toolCalls?: number,
 *   failedToolCalls?: number,
 *   outputItemTypes?: string[],
 *   maxAICreditsExceeded?: boolean,
 *   aiCreditsRateLimitError?: boolean,
 *   exportFailures?: number,
 *   traceContext?: {traceId: string, spanId: string},
 * }} opts
 * @returns {any[]}
 */
function buildBuiltinRunMetrics({
  jobName = "",
  workflow = "",
  engine = "",
  model = "",
  conclusion = "",
  startMs,
  endMs,
  inputTokens,
  outputTokens,
  cacheReadTokens,
  cacheWriteTokens,
  emitsTokenUsage = false,
  turns,
  toolCalls,
  failedToolCalls,
  outputItemTypes = [],
  maxAICreditsExceeded = false,
  aiCreditsRateLimitError = false,
  exportFailures = 0,
  traceContext,
}) {
  const { buildAttr } = require("./send_otlp_span.cjs");
  const bounded = value => (typeof value === "string" ? value.slice(0, MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH) : "");
  const commonAttributes = [...(jobName ? [buildAttr("gh-aw.job.name", bounded(jobName))] : []), ...(workflow ? [buildAttr("gh-aw.workflow.name", bounded(workflow))] : [])];
  const attributes = (...extra) => [...commonAttributes, ...extra];
  const durationAttributes = attributes(
    buildAttr("cicd.pipeline.task.run.result", bounded(conclusion)),
    ...(engine ? [buildAttr("gh-aw.engine.id", bounded(engine))] : []),
    ...(model ? [buildAttr("gen_ai.request.model", bounded(model))] : [])
  );
  const metrics = [];
  for (const name of ["gh_aw.job.duration", "gh_aw.run.duration"]) {
    metrics.push(
      buildOTLPMetric({
        name,
        type: "histogram",
        unit: "s",
        description: "Wall-clock job duration",
        value: Math.max(0, (endMs - startMs) / 1000),
        attributes: durationAttributes,
        startMs,
        endMs,
        traceContext,
      })
    );
  }
  if (["failure", "timed_out", "cancelled"].includes(conclusion)) {
    for (const [name, unit] of [
      ["gh_aw.job.errors", "{job}"],
      ["gh_aw.run.errors", "{run}"],
    ]) {
      metrics.push(
        buildOTLPMetric({
          name,
          type: "counter",
          unit,
          value: 1,
          attributes: attributes(buildAttr("cicd.pipeline.task.run.result", bounded(conclusion))),
          startMs,
          endMs,
          traceContext,
        })
      );
    }
  }
  if (emitsTokenUsage) {
    const tokenMetrics = [
      { type: "input", alias: "gh_aw.tokens.input", value: inputTokens },
      { type: "output", alias: "gh_aw.tokens.output", value: outputTokens },
      { type: "input_cache_read", alias: "gh_aw.tokens.cache_read", value: cacheReadTokens },
      { type: "input_cache_write", alias: "gh_aw.tokens.cache_write", value: cacheWriteTokens },
    ];
    for (const { type, alias, value } of tokenMetrics) {
      if (typeof value === "number" && Number.isFinite(value) && value >= 0) {
        const tokenAttributes = attributes(buildAttr("gen_ai.token.type", type), ...(model ? [buildAttr("gen_ai.request.model", bounded(model))] : []), ...(engine ? [buildAttr("gh-aw.engine.id", bounded(engine))] : []));
        metrics.push(buildOTLPMetric({ name: "gen_ai.client.token.usage", type: "counter", unit: "{token}", value, attributes: tokenAttributes, startMs, endMs, traceContext }));
        if (alias) metrics.push(buildOTLPMetric({ name: alias, type: "counter", unit: "{token}", value, attributes: tokenAttributes, startMs, endMs, traceContext }));
      }
    }
  }
  if (typeof turns === "number" && Number.isFinite(turns) && turns >= 0) {
    metrics.push(buildOTLPMetric({ name: "gh_aw.agent.turns", type: "histogram", unit: "{turn}", description: "Agent turns per completed agent invocation", value: turns, attributes: commonAttributes, startMs, endMs, traceContext }));
  }
  if (typeof toolCalls === "number" && Number.isFinite(toolCalls) && toolCalls > 0) {
    const failed = typeof failedToolCalls === "number" && Number.isFinite(failedToolCalls) ? Math.min(Math.max(0, failedToolCalls), toolCalls) : 0;
    const toolStatuses = [
      { status: "ok", value: toolCalls - failed },
      { status: "error", value: failed },
    ];
    for (const { status, value } of toolStatuses) {
      if (value > 0) {
        metrics.push(buildOTLPMetric({ name: "gh_aw.tool_calls", type: "counter", unit: "{call}", value, attributes: attributes(buildAttr("gh-aw.tool.status", status)), startMs, endMs, traceContext }));
      }
    }
  }
  if (maxAICreditsExceeded) {
    metrics.push(buildOTLPMetric({ name: "gh_aw.ai.credits.limit_exceeded", type: "counter", unit: "{event}", description: "AI credit limit exceeded", value: 1, attributes: commonAttributes, startMs, endMs, traceContext }));
  }
  if (aiCreditsRateLimitError) {
    metrics.push(buildOTLPMetric({ name: "gh_aw.ai.credits.rate_limit_errors", type: "counter", unit: "{event}", description: "AI credit rate-limit errors", value: 1, attributes: commonAttributes, startMs, endMs, traceContext }));
  }
  if (typeof exportFailures === "number" && Number.isFinite(exportFailures) && exportFailures > 0) {
    metrics.push(
      buildOTLPMetric({
        name: "gh_aw.otlp.export.failures",
        type: "counter",
        unit: "{failure}",
        description: "Failed OTLP export attempts observed before metrics export",
        value: exportFailures,
        attributes: attributes(buildAttr("gh-aw.otlp.endpoint.class", "configured"), buildAttr("gh-aw.otlp.failure.class", "export")),
        startMs,
        endMs,
        traceContext,
      })
    );
  }
  const outputCounts = new Map();
  for (const type of outputItemTypes) {
    if (typeof type === "string" && type) outputCounts.set(type, (outputCounts.get(type) || 0) + 1);
  }
  for (const [type, value] of outputCounts) {
    const outputAttributes = attributes(buildAttr("gh-aw.safe_output.type", bounded(type)));
    metrics.push(buildOTLPMetric({ name: "gh_aw.workflow.output.items", type: "counter", unit: "{item}", value, attributes: outputAttributes, startMs, endMs, traceContext }));
    metrics.push(buildOTLPMetric({ name: "gh_aw.safe_outputs", type: "counter", unit: "{item}", value, attributes: outputAttributes, startMs, endMs, traceContext }));
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
    const entries = Object.entries(options.attributes || {});
    if (entries.length > MAX_CUSTOM_METRIC_ATTRIBUTES) throw new Error(`metric attributes must not exceed ${MAX_CUSTOM_METRIC_ATTRIBUTES} keys`);
    const attributes = entries.map(([key, attribute]) => {
      if (key.length > MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH || HIGH_CARDINALITY_ATTRIBUTE_KEY_RE.test(key)) {
        throw new Error("metric attribute keys must be bounded and must not identify individual runs, users, or resources");
      }
      if (!["string", "number", "boolean"].includes(typeof attribute) || (typeof attribute === "number" && !Number.isFinite(attribute))) {
        throw new Error("metric attributes must be strings, finite numbers, or booleans");
      }
      if (typeof attribute === "string" && attribute.length > MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH) {
        throw new Error(`metric attribute values must not exceed ${MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH} characters`);
      }
      return buildAttr(key, attribute);
    });
    const sanitizedAttributes = sanitizeAttrs(attributes);
    const canonicalAttributes = /** @type {Array<[string, string]>} */ sanitizedAttributes.map(attribute => [attribute.key, JSON.stringify(attribute.value) || ""]);
    canonicalAttributes.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
    const seriesKey = JSON.stringify([name, options.type || "gauge", canonicalAttributes]);
    if (!customMetricSeries.has(seriesKey) && customMetricSeries.size >= MAX_CUSTOM_METRIC_SERIES) {
      throw new Error(`custom metric series must not exceed ${MAX_CUSTOM_METRIC_SERIES} unique series`);
    }
    const now = Date.now();
    const isGauge = (options.type || "gauge") === "gauge";
    const startMs = isGauge ? now : (lastEndBySeries.get(seriesKey) ?? PROCESS_START_MS);
    const endMs = isGauge ? now : Math.max(now, startMs + 0.001);
    const traceContext = {
      traceId: (process.env.GITHUB_AW_OTEL_TRACE_ID || "").trim().toLowerCase(),
      spanId: (process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "").trim().toLowerCase(),
    };
    const metric = buildOTLPMetric({ name, value, type: options.type, unit: options.unit, description: options.description, buckets: options.buckets, attributes, startMs, endMs, traceContext });
    customMetricSeries.add(seriesKey);
    if (!isGauge) lastEndBySeries.set(seriesKey, endMs);
    const resourceAttributes = [
      ...(process.env.GITHUB_EVENT_NAME ? [buildAttr("github.event_name", process.env.GITHUB_EVENT_NAME)] : []),
      ...(process.env.GITHUB_JOB ? [buildAttr("github.job", process.env.GITHUB_JOB)] : []),
      ...(process.env.GITHUB_WORKFLOW ? [buildAttr("gh-aw.workflow.name", process.env.GITHUB_WORKFLOW)] : []),
      ...(process.env.GH_AW_ENGINE_ID ? [buildAttr("gh-aw.engine.id", process.env.GH_AW_ENGINE_ID)] : []),
    ];
    const payload = buildOTLPMetricsPayload({
      serviceName: process.env.OTEL_SERVICE_NAME || "gh-aw",
      scopeVersion: process.env.GH_AW_INFO_CLI_VERSION || "unknown",
      resourceAttributes,
      metrics: [metric],
    });
    await sendOTLPMetrics(payload, traceContext);
  } catch {
    console.warn("[otlp] failed to emit custom metric");
  }
}

module.exports = {
  OTEL_METRICS_JSONL_PATH,
  DEFAULT_HISTOGRAM_BUCKETS,
  MAX_CUSTOM_METRIC_ATTRIBUTES,
  MAX_CUSTOM_METRIC_SERIES,
  buildOTLPMetric,
  buildOTLPMetricsPayload,
  sanitizeOTLPMetricsPayload,
  appendToOTLPMetricsJSONL,
  sendOTLPMetrics,
  buildBuiltinRunMetrics,
  logMetric,
};
