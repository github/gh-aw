// @ts-check
"use strict";
require("./shim.cjs");

const { readCompactUnifiedSession, sessionEventPriority } = require("./compact_unified_session.cjs");
const { serializeSessionArtifact } = require("./session_artifact.cjs");
const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { sessionTimestampNs } = require("./unified_session_order.cjs");
const { getOTLPSecretValues } = require("./otlp_header_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { isDurableSessionRecord } = require("./durable_session_record.cjs");
const otlp = require("./send_otlp_span.cjs");
const DEFAULT_MAX_SESSION_LOG_BYTES = 64 * 1024;

// Keep accounting metadata, not repository, user, session or filesystem identity.
const IDENTITY_FIELDS = new Set(["repository", "workflowId", "workflowCallId", "runId", "runAttempt", "ref", "sha", "sessionId", "agentId", "memoryId", "branch", "commit", "path"]);

/**
 * Drop workflow run identity nested in grader records, keeping ordinary grader IDs and counts.
 * @param {any} event
 * @returns {any}
 */
function removeRunIdentity(event) {
  if (event?.type !== "grader.result" || !event.data?.run || typeof event.data.run !== "object") return event;
  const { id, attempt, ...run } = event.data.run;
  return { ...event, data: { ...event.data, run } };
}

/**
 * Prefer exact ISO fractions, otherwise reuse the source-normalized clock.
 * Ordering anchors and export time must never become event timestamps.
 * @param {any} event
 * @returns {string | undefined}
 */
function eventTimeUnixNano(event) {
  const timestamp = event.timestamp;
  const isoTime = typeof timestamp === "string" ? sessionTimestampNs(event) : undefined;
  const hasNormalizedClock = typeof event.provenance?.timestampMs === "number";
  const exactNs = hasNormalizedClock && typeof timestamp === "string" ? sessionTimestampNs(event, "nanoseconds") : undefined;
  const normalizedTime = hasNormalizedClock ? sessionTimestampNs({ timestamp: event.provenance.timestampMs }) : undefined;
  const nativeTime = typeof timestamp !== "string" && event.provenance === undefined ? sessionTimestampNs(event) : undefined;
  const time = isoTime ?? exactNs ?? (typeof timestamp === "string" ? undefined : hasNormalizedClock ? normalizedTime : nativeTime);
  return time !== undefined && time >= 0n && time <= 18446744073709551615n ? time.toString() : undefined;
}

/**
 * @param {unknown} value
 * @returns {object}
 */
function anyValue(value) {
  if (typeof value === "boolean") return { boolValue: value };
  if (typeof value === "number") return Number.isSafeInteger(value) ? { intValue: String(value) } : { doubleValue: value };
  if (typeof value === "string") return { stringValue: value };
  if (Array.isArray(value)) return { arrayValue: { values: value.map(anyValue) } };
  if (value && typeof value === "object") {
    return {
      kvlistValue: {
        values: Object.entries(value)
          .filter(([key]) => !IDENTITY_FIELDS.has(key))
          .map(([key, nested]) => ({ key, value: anyValue(nested) })),
      },
    };
  }
  return {};
}

/**
 * Export only the persisted, redacted compact session. Never reread raw logs.
 * @param {{sessionPath?: string}} [options]
 * @returns {Promise<void>}
 */
async function sendSessionLogs({ sessionPath = "/tmp/gh-aw/usage/aw_session.jsonl" } = {}) {
  const enabled = process.env.GH_AW_OTLP_LOGS_ENABLED;
  if (!enabled || enabled === "false") return;
  if (enabled !== "true") {
    console.warn("[otlp] GH_AW_OTLP_LOGS_ENABLED must be true or false; session logs not exported");
    return;
  }
  const endpoints = otlp.parseOTLPEndpoints();
  if (!endpoints.length) return;
  const rawMaxBytes = process.env.GH_AW_OTLP_LOGS_MAX_BYTES || "";
  const maxBytes = rawMaxBytes ? Number(rawMaxBytes) : DEFAULT_MAX_SESSION_LOG_BYTES;
  if ((rawMaxBytes && !/^[1-9][0-9]*$/.test(rawMaxBytes)) || !Number.isSafeInteger(maxBytes) || maxBytes < 1) {
    console.warn("[otlp] GH_AW_OTLP_LOGS_MAX_BYTES must be a positive safe integer; session logs not exported");
    return;
  }
  try {
    const compact = readCompactUnifiedSession(sessionPath);
    const events = [...iterateJsonl(compact.session, { strict: true })];
    // Apply current exporter-secret masks as well as the publication masks already
    // applied to the source artifact. Log bodies use the compact field allowlist,
    // not the span attribute-key heuristic or guesses about secret-looking values.
    const exporterMasks = [...getOTLPSecretValues(), ...endpoints.map(endpoint => endpoint.url)];
    const redacted = serializeSessionArtifact(events, exporterMasks);
    const observedTimeUnixNano = otlp.toNanoString(Date.now());
    const traceId = process.env.GITHUB_AW_OTEL_TRACE_ID || "";
    const spanId = process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "";
    const retainedEvents = [...iterateJsonl(redacted, { strict: true })].map(removeRunIdentity);
    const logRecords = retainedEvents.map(event => {
      const timeUnixNano = eventTimeUnixNano(event);
      return {
        ...(timeUnixNano !== undefined ? { timeUnixNano } : {}),
        observedTimeUnixNano,
        severityNumber: 9,
        severityText: "INFO",
        ...(otlp.isValidTraceId(traceId) ? { traceId, ...(otlp.isValidSpanId(spanId) ? { spanId } : {}) } : {}),
        body: anyValue(event),
        attributes: [
          otlp.buildAttr("gh-aw.session.event_type", event.type),
          ...(isDurableSessionRecord(event) && ["restored", "resumed"].includes(event.data.status) && event.data.parentRunId ? [otlp.buildAttr("gh-aw.durable.parent_run_id", event.data.parentRunId)] : []),
        ],
      };
    });
    const resourceAttributes = [
      otlp.buildAttr("service.name", "gh-aw"),
      otlp.buildAttr("gh-aw.session.compact.version", compact.version),
      otlp.buildAttr("gh-aw.session.oversized_lines", compact.oversizedLines),
      otlp.buildAttr("gh-aw.session.scan_truncated", compact.scanTruncated),
    ];
    const buildPayload = (/** @type {number[]} */ indices) => ({
      resourceLogs: [
        {
          resource: {
            attributes: [
              ...resourceAttributes,
              otlp.buildAttr("gh-aw.session.included_events", indices.length),
              otlp.buildAttr("gh-aw.session.omitted_events", compact.omittedEvents + compact.includedEvents - indices.length),
              otlp.buildAttr("gh-aw.session.truncated", compact.truncated || indices.length < compact.includedEvents),
              otlp.buildAttr("gh-aw.session.untimed_events", indices.filter(i => logRecords[i].timeUnixNano === undefined).length),
            ],
          },
          scopeLogs: [{ scope: { name: "gh-aw.session", version: String(compact.version) }, logRecords: indices.map(i => logRecords[i]) }],
        },
      ],
    });
    // Admit records by priority only when the complete request still fits, then restore source order.
    /** @type {number[]} */
    let admitted = [];
    const byPriority = retainedEvents.map((event, index) => index).sort((a, b) => sessionEventPriority(retainedEvents[a]) - sessionEventPriority(retainedEvents[b]) || a - b);
    for (const index of byPriority) {
      const candidate = [...admitted, index].sort((a, b) => a - b);
      if (Buffer.byteLength(JSON.stringify(buildPayload(candidate))) <= maxBytes) admitted = candidate;
    }
    if (!admitted.length) {
      console.warn("[otlp] No compact session records fit GH_AW_OTLP_LOGS_MAX_BYTES; session logs not exported");
      return;
    }
    const payload = buildPayload(admitted);
    if (compact.truncated || admitted.length < compact.includedEvents) {
      // The shared standalone shim only writes plain text, so emit the annotation directly.
      process.stdout.write(
        `::warning::[otlp] Session export is partial: retained ${admitted.length} compact events; ${compact.omittedEvents + compact.includedEvents - admitted.length} inspected records omitted. Size or source-scan limits were reached; the full session artifact is unchanged.\n`
      );
    }
    await otlp.sendOTLPToAllEndpoints(endpoints, payload, { signal: "logs", skipJSONL: true });
  } catch (err) {
    // Only known error classes/codes are reported; messages and custom names can embed private data.
    const kind = [SyntaxError, TypeError, RangeError, ReferenceError, URIError, EvalError, AggregateError, Error].find(errorClass => err instanceof errorClass)?.name || typeof err;
    const errorCode = err && typeof err === "object" && "code" in err ? err.code : getErrorMessage(err).startsWith(`${ERR_VALIDATION}:`) ? ERR_VALIDATION : "";
    const code = typeof errorCode === "string" && new Set([ERR_VALIDATION, "ENOENT", "ELOOP", "EACCES", "EPERM", "ENOTDIR", "EISDIR", "EIO", "EBADF"]).has(errorCode) ? ` ${errorCode}` : "";
    console.warn(`[otlp] Failed to export compact session logs (${kind}${code}); check that a valid unified session artifact was published`);
  }
}

module.exports = { sendSessionLogs };
