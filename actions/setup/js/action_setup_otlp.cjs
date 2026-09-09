// @ts-check
"use strict";
require("./shim.cjs");

/**
 * action_setup_otlp.cjs
 *
 * Sends a `gh-aw.<jobName>.setup` OTLP span and writes the trace/span IDs to
 * GITHUB_OUTPUT and GITHUB_ENV.  Used by both:
 *
 *   - actions/setup/index.js  (dev/release/action mode)
 *   - actions/setup/setup.sh  (script mode)
 *
 * Having a single .cjs file ensures the two modes behave identically.
 *
 * Environment variables read:
 *   SETUP_START_MS  – epoch ms when setup began (set by callers)
 *   GITHUB_OUTPUT   – path to the GitHub Actions output file
 *   GITHUB_ENV      – path to the GitHub Actions env file
 *   INPUT_*         – standard GitHub Actions input env vars (read by sendJobSetupSpan)
 *
 * Environment variables written:
 *   GITHUB_AW_OTEL_TRACE_ID        – resolved trace ID (for cross-job correlation)
 *   GITHUB_AW_OTEL_PARENT_SPAN_ID  – setup span ID (links conclusion span as child)
 *   GITHUB_AW_OTEL_JOB_START_MS    – epoch ms when setup finished (used by conclusion
 *                                     span to measure actual job execution duration)
 */

const { appendFileSync } = require("fs");
const { nowMs } = require("./performance_now.cjs");
const { getActionInput } = require("./action_input_utils.cjs");
const { maskSecret } = require("./actions_secret_masking.cjs");

/**
 * Append a key=value line to a GitHub Actions file (GITHUB_OUTPUT or GITHUB_ENV)
 * if the file path is set.
 * @param {string | undefined} filePath - Path to the output/env file
 * @param {string} key - The variable name
 * @param {string} value - The value to write
 * @param {string} logLabel - Label used in the confirmation log message
 * @param {string} fileLabel - Human-readable file name for the log (e.g. "GITHUB_OUTPUT")
 */
function writeEnvLine(filePath, key, value, logLabel, fileLabel) {
  if (!filePath) return;
  try {
    appendFileSync(filePath, `${key}=${value}\n`);
    core.info(`[otlp] ${logLabel} written to ${fileLabel}`);
  } catch {
    /* ignore */
  }
}

/**
 * @param {string} headers
 * @returns {boolean}
 */
function hasAuthorizationHeader(headers) {
  return /(^|,)\s*authorization\s*=/i.test(headers);
}

/**
 * @param {string} headers
 * @param {string} token
 * @returns {string}
 */
function mergeAuthorizationHeader(headers, token) {
  if (hasAuthorizationHeader(headers)) return headers;
  return (headers ? `${headers},` : "") + "Authorization=Bearer " + token;
}

/**
 * @param {string} endpointsRaw
 * @param {string} token
 * @returns {string}
 */
function mergeAuthorizationIntoOTLPEndpoints(endpointsRaw, token) {
  if (!endpointsRaw) return endpointsRaw;
  let parsed;
  try {
    parsed = JSON.parse(endpointsRaw);
  } catch {
    return endpointsRaw;
  }
  if (!Array.isArray(parsed)) return endpointsRaw;
  const updated = parsed.map(entry => {
    if (!entry || typeof entry !== "object") return entry;
    const currentHeaders = typeof entry.headers === "string" ? entry.headers : "";
    const mergedHeaders = mergeAuthorizationHeader(currentHeaders, token);
    return { ...entry, headers: mergedHeaders };
  });
  return JSON.stringify(updated);
}

/**
 * @param {string} endpointsRaw
 * @returns {{ url: string, headers: string } | null}
 */
function getPrimaryOTLPEndpoint(endpointsRaw) {
  try {
    const endpoints = JSON.parse(endpointsRaw);
    const primary = Array.isArray(endpoints) ? endpoints[0] : null;
    if (!primary || typeof primary.url !== "string") return null;
    return { url: primary.url, headers: typeof primary.headers === "string" ? primary.headers : "" };
  } catch {
    return null;
  }
}

/**
 * Send the OTLP job-setup span and propagate trace context via GITHUB_OUTPUT /
 * GITHUB_ENV.  Non-fatal: all errors are silently swallowed.
 *
 * The trace-id is ALWAYS resolved and written to GITHUB_OUTPUT / GITHUB_ENV so
 * that cross-job span correlation works even when OTEL_EXPORTER_OTLP_ENDPOINT
 * is not configured.  The span itself is only sent when the endpoint is set.
 * @returns {Promise<void>}
 */
async function run() {
  const { sendJobSetupSpan, isValidTraceId, isValidSpanId, parseOTLPEndpoints } = require("./send_otlp_span.cjs");

  const rawStartMs = process.env.SETUP_START_MS;
  const parsedMs = /^\d+$/.test(rawStartMs ?? "") ? Number(rawStartMs) : NaN;
  const startMs = Number.isSafeInteger(parsedMs) ? parsedMs : 0;

  // Explicitly read INPUT_TRACE_ID and pass it as options.traceId so the
  // activation job's trace ID is used even when process.env propagation
  // through GitHub Actions expression evaluation is unreliable.
  const inputTraceId = getActionInput("TRACE_ID").toLowerCase();
  if (inputTraceId) {
    core.info(`[otlp] INPUT_TRACE_ID=${inputTraceId} (will reuse activation trace)`);
  } else {
    core.info("[otlp] INPUT_TRACE_ID not set, a new trace ID will be generated");
  }
  const inputParentSpanId = getActionInput("PARENT_SPAN_ID").toLowerCase();
  if (inputParentSpanId) {
    core.info(`[otlp] INPUT_PARENT_SPAN_ID=${inputParentSpanId} (will parent setup span)`);
  }

  // Normalize to the canonical underscore form so sendJobSetupSpan (which
  // reads process.env.INPUT_JOB_NAME) always finds the value.
  const inputJobName = getActionInput("JOB_NAME");
  if (inputJobName) {
    process.env.INPUT_JOB_NAME = inputJobName;
  }
  if (inputParentSpanId) {
    process.env.INPUT_PARENT_SPAN_ID = inputParentSpanId;
  }

  const inputOTLPOIDCToken = getActionInput("OTLP_OIDC_TOKEN");
  if (inputOTLPOIDCToken) {
    maskSecret(inputOTLPOIDCToken);
    const existingHeaders = process.env.OTEL_EXPORTER_OTLP_HEADERS || "";
    const mergedHeaders = mergeAuthorizationHeader(existingHeaders, inputOTLPOIDCToken);

    process.env.OTEL_EXPORTER_OTLP_HEADERS = mergedHeaders;
    writeEnvLine(process.env.GITHUB_ENV, "OTEL_EXPORTER_OTLP_HEADERS", mergedHeaders, "OTEL_EXPORTER_OTLP_HEADERS", "GITHUB_ENV");

    const existingEndpoints = process.env.GH_AW_OTLP_ENDPOINTS || "";
    const mergedEndpoints = mergeAuthorizationIntoOTLPEndpoints(existingEndpoints, inputOTLPOIDCToken);
    if (mergedEndpoints && mergedEndpoints !== existingEndpoints) {
      process.env.GH_AW_OTLP_ENDPOINTS = mergedEndpoints;
      writeEnvLine(process.env.GITHUB_ENV, "GH_AW_OTLP_ENDPOINTS", mergedEndpoints, "GH_AW_OTLP_ENDPOINTS", "GITHUB_ENV");
    }
  }

  const endpoints = process.env.GH_AW_OTLP_ENDPOINTS;
  const parsedEndpoints = parseOTLPEndpoints();
  if (endpoints) {
    const primaryEndpoint = getPrimaryOTLPEndpoint(endpoints);
    const currentEndpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT || "";
    const currentHeaders = process.env.OTEL_EXPORTER_OTLP_HEADERS || "";
    if (primaryEndpoint && currentEndpoint === primaryEndpoint.url && currentHeaders === primaryEndpoint.headers) {
      const endpoint = parsedEndpoints[0]?.url || "";
      const headers = parsedEndpoints[0]?.headers || "";
      if (endpoint !== currentEndpoint) {
        process.env.OTEL_EXPORTER_OTLP_ENDPOINT = endpoint;
        writeEnvLine(process.env.GITHUB_ENV, "OTEL_EXPORTER_OTLP_ENDPOINT", endpoint, "OTEL_EXPORTER_OTLP_ENDPOINT", "GITHUB_ENV");
      }
      if (headers !== currentHeaders) {
        process.env.OTEL_EXPORTER_OTLP_HEADERS = headers;
        writeEnvLine(process.env.GITHUB_ENV, "OTEL_EXPORTER_OTLP_HEADERS", headers, "OTEL_EXPORTER_OTLP_HEADERS", "GITHUB_ENV");
      }
    }
  }

  if (!endpoints) {
    core.info("[otlp] GH_AW_OTLP_ENDPOINTS not set, skipping setup span");
  } else if (parsedEndpoints.length === 0) {
    core.info("[otlp] no OTLP endpoints have usable credentials, skipping setup span");
  } else {
    core.info(`[otlp] sending setup span to configured endpoints`);
  }

  const { traceId, spanId, parentSpanId } = await sendJobSetupSpan({
    startMs,
    traceId: inputTraceId || undefined,
    parentSpanId: inputParentSpanId || undefined,
  });

  core.info(`[otlp] resolved trace-id=${traceId}`);

  if (parsedEndpoints.length > 0) {
    core.info(`[otlp] setup span sent (traceId=${traceId}, spanId=${spanId})`);
  }

  const githubOutput = process.env.GITHUB_OUTPUT;
  const githubEnv = process.env.GITHUB_ENV;

  // Always expose trace ID as a step output for cross-job correlation, even
  // when OTLP is not configured.  This ensures needs.*.outputs.setup-trace-id
  // is populated for downstream jobs regardless of observability configuration.
  if (isValidTraceId(traceId)) writeEnvLine(githubOutput, "trace-id", traceId, `trace-id=${traceId}`, "GITHUB_OUTPUT");
  if (isValidSpanId(spanId)) writeEnvLine(githubOutput, "span-id", spanId, `span-id=${spanId}`, "GITHUB_OUTPUT");
  if (isValidSpanId(parentSpanId)) writeEnvLine(githubOutput, "parent-span-id", parentSpanId, `parent-span-id=${parentSpanId}`, "GITHUB_OUTPUT");

  // Always propagate trace/span context to subsequent steps in this job so
  // that the conclusion span can find the same trace ID.
  if (isValidTraceId(traceId)) writeEnvLine(githubEnv, "GITHUB_AW_OTEL_TRACE_ID", traceId, "GITHUB_AW_OTEL_TRACE_ID", "GITHUB_ENV");
  if (isValidSpanId(spanId)) writeEnvLine(githubEnv, "GITHUB_AW_OTEL_PARENT_SPAN_ID", spanId, "GITHUB_AW_OTEL_PARENT_SPAN_ID", "GITHUB_ENV");
  // Propagate setup-end timestamp so the conclusion span can measure actual
  // job execution duration (setup-end → conclusion-start).
  if (githubEnv) {
    const setupEndMs = String(Math.floor(nowMs()));
    writeEnvLine(githubEnv, "GITHUB_AW_OTEL_JOB_START_MS", setupEndMs, "GITHUB_AW_OTEL_JOB_START_MS", "GITHUB_ENV");
  }
}

module.exports = { run };

// When invoked directly (node action_setup_otlp.cjs) from setup.sh,
// run immediately.  Non-fatal: errors are silently swallowed.
if (require.main === module) {
  run().catch(() => {});
}
