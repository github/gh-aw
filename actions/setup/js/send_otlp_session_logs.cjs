// @ts-check
"use strict";
require("./shim.cjs");

const { readCompactUnifiedSession, leastEssentialSessionEventIndex } = require("./compact_unified_session.cjs");
const { serializeSessionArtifact } = require("./session_artifact.cjs");
const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { getOTLPSecretValues } = require("./otlp_header_helpers.cjs");
const otlp = require("./send_otlp_span.cjs");
const DEFAULT_MAX_SESSION_LOG_BYTES = 64 * 1024;

// Keep accounting metadata, not repository, user, session or filesystem identity.
const IDENTITY_FIELDS = new Set(["repository", "workflowId", "workflowCallId", "runId", "runAttempt", "ref", "sha", "sessionId", "agentId", "memoryId", "branch", "commit", "path"]);

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
    // applied to the source artifact. No exception includes source record content.
    const exporterMasks = [...getOTLPSecretValues(), ...endpoints.map(endpoint => endpoint.url)];
    const redacted = serializeSessionArtifact(events, exporterMasks);
    const observedTimeUnixNano = otlp.toNanoString(Date.now());
    const traceId = process.env.GITHUB_AW_OTEL_TRACE_ID || "";
    const spanId = process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "";
    const retainedEvents = [...iterateJsonl(redacted, { strict: true })];
    const logRecords = retainedEvents.map(event => ({
      observedTimeUnixNano,
      severityNumber: 9,
      severityText: "INFO",
      ...(otlp.isValidTraceId(traceId) ? { traceId, ...(otlp.isValidSpanId(spanId) ? { spanId } : {}) } : {}),
      body: anyValue(event),
      attributes: [otlp.buildAttr("gh-aw.session.event_type", event.type)],
    }));
    const resourceAttributes = [
      otlp.buildAttr("service.name", "gh-aw"),
      otlp.buildAttr("gh-aw.session.compact.version", compact.version),
      otlp.buildAttr("gh-aw.session.oversized_lines", compact.oversizedLines),
      otlp.buildAttr("gh-aw.session.scan_truncated", compact.scanTruncated),
    ];
    const payload = {
      resourceLogs: [
        {
          resource: { attributes: resourceAttributes },
          scopeLogs: [{ scope: { name: "gh-aw.session", version: String(compact.version) }, logRecords }],
        },
      ],
    };
    const updateCounts = () => {
      payload.resourceLogs[0].resource.attributes = [
        ...resourceAttributes,
        otlp.buildAttr("gh-aw.session.included_events", logRecords.length),
        otlp.buildAttr("gh-aw.session.omitted_events", compact.omittedEvents + compact.includedEvents - logRecords.length),
        otlp.buildAttr("gh-aw.session.truncated", compact.truncated || logRecords.length < compact.includedEvents),
      ];
    };
    updateCounts();
    while (logRecords.length && Buffer.byteLength(JSON.stringify(payload)) > maxBytes) {
      const index = leastEssentialSessionEventIndex(retainedEvents);
      retainedEvents.splice(index, 1);
      logRecords.splice(index, 1);
      updateCounts();
    }
    if (!logRecords.length) {
      console.warn("[otlp] No compact session records fit GH_AW_OTLP_LOGS_MAX_BYTES; session logs not exported");
      return;
    }
    if (compact.truncated || logRecords.length < compact.includedEvents) {
      core.warning(
        `[otlp] Session export is partial: retained ${logRecords.length} compact events; ${compact.omittedEvents + compact.includedEvents - logRecords.length} inspected records omitted. Size or source-scan limits were reached; the full session artifact is unchanged.`
      );
    }
    await otlp.sendOTLPToAllEndpoints(endpoints, payload, { signal: "logs", skipJSONL: true });
  } catch {
    console.warn("[otlp] Failed to export compact session logs; check that a valid unified session artifact was published");
  }
}

module.exports = { sendSessionLogs, DEFAULT_MAX_SESSION_LOG_BYTES };
