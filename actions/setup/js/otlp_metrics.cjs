// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");

const OTEL_METRICS_JSONL_PATH = "/tmp/gh-aw/otel-metrics.jsonl";
const DEFAULT_HISTOGRAM_BUCKETS = [1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600];
const GEN_AI_INVOKE_AGENT_DURATION_BUCKETS = [0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 25.6, 51.2, 102.4, 204.8, 409.6, 900, 1800, 3600, 7200, 14400, 43200, 86400];
const PROCESS_START_MS = Date.now();
const lastEndBySeries = new Map();
const customMetricSeries = new Set();
const MAX_CUSTOM_METRIC_ATTRIBUTES = 8;
const MAX_CUSTOM_METRIC_SERIES = 100;
const MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH = 128;
const CUSTOM_METRIC_FLUSH_INTERVAL_MS = 1000;
// OpenTelemetry instrument name syntax, unit, and description limits.
const CUSTOM_METRIC_NAME_RE = /^[A-Za-z][A-Za-z0-9_.\-/]{0,254}$/;
const CUSTOM_METRIC_UNIT_RE = /^[\x20-\x7e]{0,63}$/;
const MAX_CUSTOM_METRIC_DESCRIPTION_LENGTH = 1023;
const HIGH_CARDINALITY_ATTRIBUTE_KEY_RE = /(?:run[._-]?id|trace[._-]?id|span[._-]?id|commit|sha|actor|user|issue(?:[._-]?number)?|pull[._-]?request(?:[._-]?number)?|conversation|url|job[._-]?id)/i;
const METRIC_RESOURCE_ATTRIBUTE_KEYS = new Set(["gh-aw.repository", "gh-aw.engine.id", "gh-aw.workflow.name", "github.event_name", "github.job"]);
const OTLP_METRIC_FAILURES_REPORTED_PATH = "/tmp/gh-aw/otlp-metric-failures-reported.txt";
const pendingCustomMetrics = new Map();
let pendingCustomMetricTraceContext;
let customMetricFlushTimer;
let customMetricFlushPromise;

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
  // OTLP bucket_counts are per-bucket (not cumulative) and must sum to count;
  // bucket i covers (bounds[i-1], bounds[i]], with an overflow bucket at the end.
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

function getPointValue(point) {
  return "asInt" in point ? Number(point.asInt) : point.asDouble;
}

function mergeCustomMetric(existing, incoming, type) {
  if (type === "gauge") return incoming;
  const sectionName = type === "counter" ? "sum" : "histogram";
  const previous = existing[sectionName].dataPoints[0];
  const next = incoming[sectionName].dataPoints[0];
  previous.timeUnixNano = next.timeUnixNano;
  if (type === "counter") {
    const combined = getPointValue(previous) + getPointValue(next);
    delete previous.asInt;
    delete previous.asDouble;
    Object.assign(previous, buildNumberValue(combined));
    return existing;
  }
  previous.count = (BigInt(previous.count) + BigInt(next.count)).toString();
  previous.sum += next.sum;
  previous.min = Math.min(previous.min, next.min);
  previous.max = Math.max(previous.max, next.max);
  previous.bucketCounts = previous.bucketCounts.map((count, index) => (BigInt(count) + BigInt(next.bucketCounts[index])).toString());
  if (next.exemplars) previous.exemplars = [...(previous.exemplars || []), ...next.exemplars].slice(-10);
  return existing;
}

/**
 * @param {{serviceName: string, scopeVersion?: string, resourceAttributes?: Array<{key: string, value: object}>, metrics: any[]}} opts
 * @returns {any}
 */
function buildOTLPMetricsPayload({ serviceName, scopeVersion, resourceAttributes, metrics }) {
  const { buildOTLPResourceAttributes, parseOTELResourceAttributes, isGenAiV1, GEN_AI_SCHEMA_URL } = require("./send_otlp_span.cjs");
  const allowedResourceAttributes = new Map();
  for (const attribute of [...parseOTELResourceAttributes(process.env.OTEL_RESOURCE_ATTRIBUTES || ""), ...(resourceAttributes || [])]) {
    if (!METRIC_RESOURCE_ATTRIBUTE_KEYS.has(attribute.key)) continue;
    const value = attribute.value && typeof attribute.value === "object" && "stringValue" in attribute.value ? attribute.value.stringValue : undefined;
    if (typeof value !== "string" || value.length > MAX_CUSTOM_METRIC_ATTRIBUTE_LENGTH) continue;
    allowedResourceAttributes.set(attribute.key, attribute);
  }
  const scopeMetrics = {
    scope: { name: "gh-aw", version: scopeVersion || "unknown" },
    ...(isGenAiV1() ? { schemaUrl: GEN_AI_SCHEMA_URL } : {}),
    metrics,
  };
  return {
    resourceMetrics: [
      {
        resource: { attributes: buildOTLPResourceAttributes(serviceName, scopeVersion, [...allowedResourceAttributes.values()]) },
        scopeMetrics: [scopeMetrics],
      },
    ],
  };
}

/**
 * Redact a metric string (name, unit, description, or attribute value) with the
 * shared secret redactor: built-in credential patterns, configured secret values,
 * and add-mask values.
 * @param {string} value
 * @param {string[]} secretValues
 * @returns {string}
 */
function redactMetricString(value, secretValues) {
  const { redactArtifactMaskedValues } = require("./add_mask_redaction.cjs");
  const { redactBuiltInPatterns, redactSecrets } = require("./redact_secrets.cjs");
  const redacted = redactSecrets(redactBuiltInPatterns(value).content, secretValues).content;
  return redactArtifactMaskedValues(redacted, secretValues);
}

/**
 * @param {any} payload
 * @returns {any}
 */
function sanitizeOTLPMetricsPayload(payload) {
  if (!payload || !Array.isArray(payload.resourceMetrics)) return payload;
  const { sanitizeAttrs } = require("./send_otlp_span.cjs");
  const { getOTLPSecretValues } = require("./otlp_header_helpers.cjs");
  const secretValues = getOTLPSecretValues();
  const sanitizeMetricAttrs = attrs => {
    if (!Array.isArray(attrs)) return attrs;
    const redacted = attrs.map(attribute => {
      const value = attribute?.value?.stringValue;
      if (typeof value !== "string") return attribute;
      return { ...attribute, value: { ...attribute.value, stringValue: redactMetricString(value, secretValues) } };
    });
    return sanitizeAttrs(redacted);
  };
  const sanitizeIdentity = value => (typeof value === "string" ? redactMetricString(value, secretValues) : value);
  const sanitizeDataPoints = section => (section && Array.isArray(section.dataPoints) ? { ...section, dataPoints: section.dataPoints.map(point => ({ ...point, attributes: sanitizeMetricAttrs(point.attributes) })) } : section);
  return {
    ...payload,
    resourceMetrics: payload.resourceMetrics.map(resourceMetrics => ({
      ...resourceMetrics,
      resource: resourceMetrics.resource ? { ...resourceMetrics.resource, attributes: sanitizeMetricAttrs(resourceMetrics.resource.attributes) } : resourceMetrics.resource,
      scopeMetrics: Array.isArray(resourceMetrics.scopeMetrics)
        ? resourceMetrics.scopeMetrics.map(scopeMetrics => ({
            ...scopeMetrics,
            metrics: Array.isArray(scopeMetrics.metrics)
              ? scopeMetrics.metrics.map(metric => ({
                  ...metric,
                  name: sanitizeIdentity(metric.name),
                  ...(metric.unit !== undefined ? { unit: sanitizeIdentity(metric.unit) } : {}),
                  ...(metric.description !== undefined ? { description: sanitizeIdentity(metric.description) } : {}),
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
    const directory = path.dirname(OTEL_METRICS_JSONL_PATH);
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    fs.chmodSync(directory, 0o700);
    const fd = fs.openSync(OTEL_METRICS_JSONL_PATH, fs.constants.O_WRONLY | fs.constants.O_APPEND | fs.constants.O_CREAT | (fs.constants.O_NOFOLLOW || 0), 0o600);
    try {
      fs.fchmodSync(fd, 0o600);
      fs.writeSync(fd, JSON.stringify(payload) + "\n");
    } finally {
      fs.closeSync(fd);
    }
  } catch {
    // Mirroring is best-effort.
  }
}

function readReportedOTLPMetricFailures() {
  try {
    const count = Number.parseInt(fs.readFileSync(OTLP_METRIC_FAILURES_REPORTED_PATH, "utf8").trim(), 10);
    return Number.isInteger(count) && count >= 0 ? count : 0;
  } catch {
    return 0;
  }
}

function getUnreportedOTLPMetricFailures() {
  const { readOTLPExportErrorCount } = require("./send_otlp_span.cjs");
  return Math.max(0, readOTLPExportErrorCount() - readReportedOTLPMetricFailures());
}

/**
 * @param {any} payload
 * @returns {number} Total `gh_aw.otlp.export.failures` value carried by the payload.
 */
function countExportFailuresInPayload(payload) {
  let total = 0;
  for (const resourceMetrics of payload?.resourceMetrics || []) {
    for (const scopeMetrics of resourceMetrics?.scopeMetrics || []) {
      for (const metric of scopeMetrics?.metrics || []) {
        if (metric?.name !== "gh_aw.otlp.export.failures") continue;
        for (const point of metric.sum?.dataPoints || []) {
          const value = Number(point.asInt ?? point.asDouble);
          if (Number.isFinite(value) && value > 0) total += value;
        }
      }
    }
  }
  return total;
}

function markOTLPMetricFailuresReported(count) {
  if (count <= 0) return;
  try {
    fs.mkdirSync(path.dirname(OTLP_METRIC_FAILURES_REPORTED_PATH), { recursive: true, mode: 0o700 });
    fs.writeFileSync(OTLP_METRIC_FAILURES_REPORTED_PATH, String(readReportedOTLPMetricFailures() + count), { mode: 0o600 });
  } catch {
    // Failure reporting state is best-effort only.
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
  /** @type {{skipJSONL: true, signal: "metrics", sanitize: (payload: any) => any}} */
  const options = { skipJSONL: true, signal: "metrics", sanitize: sanitizeOTLPMetricsPayload };
  const endpoints = parseOTLPEndpoints();
  const endpointResults = await sendOTLPToAllEndpoints(endpoints, sanitizedPayload, options);
  // Failures already carried by this payload reached the healthy collectors, so
  // the follow-up counter below must only add failures not yet reported.
  if (endpointResults.some(Boolean)) {
    markOTLPMetricFailuresReported(Math.min(countExportFailuresInPayload(sanitizedPayload), getUnreportedOTLPMetricFailures()));
  }
  const failedExports = endpointResults.filter(success => !success).length;
  if (failedExports === 0) return;

  const { buildAttr } = require("./send_otlp_span.cjs");
  const reportedFailureCount = Math.max(failedExports, getUnreportedOTLPMetricFailures());
  const workflowName = process.env.GH_AW_INFO_WORKFLOW_NAME || process.env.GITHUB_WORKFLOW || "";
  const failureMetric = buildOTLPMetric({
    name: "gh_aw.otlp.export.failures",
    type: "counter",
    unit: "{failure}",
    description: "Failed OTLP endpoint exports after bounded retry",
    value: reportedFailureCount,
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
    const failureResults = await sendOTLPToAllEndpoints(successfulEndpoints, failurePayload, options);
    if (failureResults.some(Boolean)) markOTLPMetricFailuresReported(reportedFailureCount);
  }
}

/**
 * @param {{
 *   jobName?: string,
 *   workflow?: string,
 *   engine?: string,
 *   model?: string,
 *   conclusion?: string,
 *   jobConclusion?: string,
 *   runConclusion?: string,
 *   startMs: number,
 *   endMs: number,
 *   inputTokens?: number,
 *   outputTokens?: number,
 *   cacheReadTokens?: number,
 *   cacheWriteTokens?: number,
 *   provider?: string,
 *   inclusiveInputTokens?: number,
 *   agentName?: string,
 *   agentDurationSeconds?: number,
 *   agentErrorType?: string,
 *   semconvLatest?: boolean,
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
  jobConclusion = conclusion,
  runConclusion = conclusion,
  startMs,
  endMs,
  inputTokens,
  outputTokens,
  cacheReadTokens,
  cacheWriteTokens,
  provider = "",
  inclusiveInputTokens,
  agentName = "",
  agentDurationSeconds,
  agentErrorType = "",
  semconvLatest = false,
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
  const durationAttributes = result =>
    attributes(...(result ? [buildAttr("cicd.pipeline.task.run.result", bounded(result))] : []), ...(engine ? [buildAttr("gh-aw.engine.id", bounded(engine))] : []), ...(model ? [buildAttr("gen_ai.request.model", bounded(model))] : []));
  const metrics = [];
  for (const [name, result] of [
    ["gh_aw.job.duration", jobConclusion],
    ["gh_aw.run.duration", runConclusion],
  ]) {
    metrics.push(buildOTLPMetric({ name, type: "histogram", unit: "s", description: "Wall-clock job duration", value: Math.max(0, (endMs - startMs) / 1000), attributes: durationAttributes(result), startMs, endMs, traceContext }));
  }
  for (const [name, unit, result] of [
    ["gh_aw.job.errors", "{job}", jobConclusion],
    ["gh_aw.run.errors", "{run}", runConclusion],
  ]) {
    if (!["failure", "timed_out", "cancelled"].includes(result)) continue;
    metrics.push(
      buildOTLPMetric({
        name,
        type: "counter",
        unit,
        value: 1,
        attributes: attributes(buildAttr("cicd.pipeline.task.run.result", bounded(result))),
        startMs,
        endMs,
        traceContext,
      })
    );
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
        const sharedTokenAttributes = [...(model ? [buildAttr("gen_ai.request.model", bounded(model))] : []), ...(engine ? [buildAttr("gh-aw.engine.id", bounded(engine))] : [])];
        const tokenAttributes = attributes(buildAttr("gen_ai.token.type", type), ...sharedTokenAttributes);
        const aliasAttributes = attributes(...(semconvLatest ? [] : [buildAttr("gen_ai.token.type", type)]), ...sharedTokenAttributes);
        if (!semconvLatest) metrics.push(buildOTLPMetric({ name: "gen_ai.client.token.usage", type: "counter", unit: "{token}", value, attributes: tokenAttributes, startMs, endMs, traceContext }));
        if (alias) metrics.push(buildOTLPMetric({ name: alias, type: "counter", unit: "{token}", value, attributes: aliasAttributes, startMs, endMs, traceContext }));
      }
    }
    if (provider) {
      const num = value => (typeof value === "number" && Number.isFinite(value) ? value : undefined);
      const inclusiveInput = num(inclusiveInputTokens);
      const usageAttributes = attributes(
        buildAttr("gen_ai.operation.name", "chat"),
        buildAttr("gen_ai.provider.name", bounded(provider)),
        buildAttr("gen_ai.token.modality", "unknown"),
        ...(model ? [buildAttr("gen_ai.request.model", bounded(model))] : [])
      );
      /** @type {Array<[string, number | undefined]>} */
      const inferenceMetrics = [
        ["gen_ai.client.inference.usage.input_tokens", inclusiveInput],
        ["gen_ai.client.inference.usage.output_tokens", num(outputTokens)],
        ["gen_ai.client.inference.usage.cache_read.input_tokens", num(cacheReadTokens)],
        ["gen_ai.client.inference.usage.cache_write.input_tokens", num(cacheWriteTokens)],
      ];
      for (const [name, value] of inferenceMetrics) {
        if (typeof value === "number" && value > 0) {
          metrics.push(buildOTLPMetric({ name, type: "counter", unit: "{token}", value, attributes: usageAttributes, startMs, endMs, traceContext }));
        }
      }
    }
  }
  if (typeof agentDurationSeconds === "number" && Number.isFinite(agentDurationSeconds) && agentDurationSeconds >= 0) {
    metrics.push(
      buildOTLPMetric({
        name: "gen_ai.invoke_agent.duration",
        type: "histogram",
        unit: "s",
        value: agentDurationSeconds,
        buckets: GEN_AI_INVOKE_AGENT_DURATION_BUCKETS,
        attributes: attributes(
          buildAttr("gen_ai.operation.name", "invoke_agent"),
          ...(agentName ? [buildAttr("gen_ai.agent.name", bounded(agentName))] : []),
          ...(model ? [buildAttr("gen_ai.request.model", bounded(model))] : []),
          ...(agentErrorType ? [buildAttr("error.type", bounded(agentErrorType))] : [])
        ),
        startMs,
        endMs,
        traceContext,
      })
    );
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

function scheduleCustomMetricFlush() {
  if (customMetricFlushTimer || customMetricFlushPromise || pendingCustomMetrics.size === 0) return;
  customMetricFlushTimer = setTimeout(() => {
    customMetricFlushTimer = undefined;
    flushCustomMetrics().catch(() => console.warn("[otlp] failed to flush custom metrics"));
  }, CUSTOM_METRIC_FLUSH_INTERVAL_MS);
}

async function flushCustomMetrics() {
  if (customMetricFlushTimer) {
    clearTimeout(customMetricFlushTimer);
    customMetricFlushTimer = undefined;
  }
  if (customMetricFlushPromise) await customMetricFlushPromise;
  if (pendingCustomMetrics.size === 0) return;

  const { buildAttr } = require("./send_otlp_span.cjs");
  const metrics = [...pendingCustomMetrics.values()];
  pendingCustomMetrics.clear();
  const traceContext = pendingCustomMetricTraceContext;
  pendingCustomMetricTraceContext = undefined;
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
    metrics,
  });
  customMetricFlushPromise = sendOTLPMetrics(payload, traceContext).finally(() => {
    customMetricFlushPromise = undefined;
    scheduleCustomMetricFlush();
  });
  await customMetricFlushPromise;
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
    const { getOTLPSecretValues } = require("./otlp_header_helpers.cjs");
    if (typeof name !== "string" || !CUSTOM_METRIC_NAME_RE.test(name)) throw new Error("metric name must match the OpenTelemetry instrument name syntax");
    if (options.unit !== undefined && (typeof options.unit !== "string" || !CUSTOM_METRIC_UNIT_RE.test(options.unit))) throw new Error("metric unit must be printable ASCII of at most 63 characters");
    if (options.description !== undefined && (typeof options.description !== "string" || options.description.length > MAX_CUSTOM_METRIC_DESCRIPTION_LENGTH)) {
      throw new Error(`metric description must not exceed ${MAX_CUSTOM_METRIC_DESCRIPTION_LENGTH} characters`);
    }
    const secretValues = getOTLPSecretValues();
    const containsSecret = value => typeof value === "string" && redactMetricString(value, secretValues) !== value;
    if ([name, options.unit, options.description, ...Object.values(options.attributes || {})].some(containsSecret)) {
      throw new Error("metric name, unit, description, and attribute values must not contain secrets");
    }
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
    const metricType = options.type || "gauge";
    const buckets = options.buckets || DEFAULT_HISTOGRAM_BUCKETS;
    const seriesKey = JSON.stringify([name, metricType, options.unit || "", options.description || "", canonicalAttributes, metricType === "histogram" ? buckets : []]);
    if (!customMetricSeries.has(seriesKey) && customMetricSeries.size >= MAX_CUSTOM_METRIC_SERIES) {
      throw new Error(`custom metric series must not exceed ${MAX_CUSTOM_METRIC_SERIES} unique series`);
    }
    const now = Date.now();
    const isGauge = metricType === "gauge";
    const startMs = isGauge ? now : (lastEndBySeries.get(seriesKey) ?? PROCESS_START_MS);
    const endMs = isGauge ? now : Math.max(now, startMs + 0.001);
    const traceContext = {
      traceId: (process.env.GITHUB_AW_OTEL_TRACE_ID || "").trim().toLowerCase(),
      spanId: (process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "").trim().toLowerCase(),
    };
    const metric = buildOTLPMetric({ name, value, type: metricType, unit: options.unit, description: options.description, buckets, attributes, startMs, endMs, traceContext });
    customMetricSeries.add(seriesKey);
    if (!isGauge) lastEndBySeries.set(seriesKey, endMs);
    const previous = pendingCustomMetrics.get(seriesKey);
    pendingCustomMetrics.set(seriesKey, previous ? mergeCustomMetric(previous, metric, metricType) : metric);
    pendingCustomMetricTraceContext ||= traceContext;
    scheduleCustomMetricFlush();
  } catch {
    console.warn("[otlp] failed to emit custom metric");
  }
}

module.exports = {
  OTEL_METRICS_JSONL_PATH,
  DEFAULT_HISTOGRAM_BUCKETS,
  GEN_AI_INVOKE_AGENT_DURATION_BUCKETS,
  MAX_CUSTOM_METRIC_ATTRIBUTES,
  MAX_CUSTOM_METRIC_SERIES,
  buildOTLPMetric,
  buildOTLPMetricsPayload,
  sanitizeOTLPMetricsPayload,
  appendToOTLPMetricsJSONL,
  sendOTLPMetrics,
  buildBuiltinRunMetrics,
  logMetric,
  flushCustomMetrics,
  getUnreportedOTLPMetricFailures,
};
