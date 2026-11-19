// @ts-check
"use strict";

const TRACE_ID_RE = /^[0-9a-f]{32}$/;
const SPAN_ID_RE = /^[0-9a-f]{16}$/;
const CICD_PIPELINE_TASK_NAME = "cicd.pipeline.task.name";

/** @param {string} id */
function isValidTraceId(id) {
  return TRACE_ID_RE.test(id) && !/^0+$/.test(id);
}

/** @param {string} id */
function isValidSpanId(id) {
  return SPAN_ID_RE.test(id) && !/^0+$/.test(id);
}

/** @param {string} value */
function isWellFormedUnicodeString(value) {
  for (let index = 0; index < value.length; index++) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      if (index + 1 >= value.length) return false;
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) return false;
      index++;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      return false;
    }
  }
  return true;
}

/** @param {string} traceId @param {string} spanId */
function buildTraceparent(traceId, spanId) {
  if (!isValidTraceId(traceId) || !isValidSpanId(spanId)) throw new Error("Invalid trace context");
  return `00-${traceId}-${spanId}-01`;
}

/**
 * Correlation metadata is diagnostic only, never an authorization binding.
 * @param {NodeJS.ProcessEnv} [env]
 * @param {string} [traceId]
 * @param {string} [spanId]
 * @returns {Record<string, string>}
 */
function buildServiceRequestHeaders(env = process.env, traceId = env.GITHUB_AW_OTEL_TRACE_ID || "", spanId = env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "") {
  /** @type {Record<string, string>} */
  const headers = {};
  if (isValidTraceId(traceId) && isValidSpanId(spanId)) headers.traceparent = buildTraceparent(traceId, spanId);
  const jobId = env.GITHUB_JOB;
  if (jobId && isWellFormedUnicodeString(jobId)) headers.baggage = `${CICD_PIPELINE_TASK_NAME}=${encodeURIComponent(jobId)}`;
  return headers;
}

module.exports = { buildTraceparent, buildServiceRequestHeaders, isValidTraceId, isValidSpanId, isWellFormedUnicodeString };
