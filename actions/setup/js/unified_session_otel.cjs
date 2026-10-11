// @ts-check

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */

/**
 * Expand OTLP JSON envelopes while retaining resource/scope and trace lineage.
 * Invalid nested records are reported without discarding adjacent observations.
 * @param {any} payload
 * @param {(code: string) => void} report
 * @returns {SessionEvent[]}
 */
function normalizeOtelEvents(payload, report) {
  /** @type {SessionEvent[]} */
  const events = [];
  const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const array = value => {
    if (Array.isArray(value)) return value;
    report("malformed_otlp");
    return [];
  };
  if (!object(payload) || (!Object.hasOwn(payload, "resourceSpans") && !Object.hasOwn(payload, "resourceLogs"))) {
    report("unrecognized_otlp");
    return events;
  }
  for (const [resourceKey, scopeKey, recordsKey, type, clock] of [
    ["resourceSpans", "scopeSpans", "spans", "otel.span", "startTimeUnixNano"],
    ["resourceLogs", "scopeLogs", "logRecords", "otel.log", "timeUnixNano"],
  ]) {
    if (!Object.hasOwn(payload, resourceKey)) continue;
    for (const resource of array(payload[resourceKey])) {
      if (!object(resource)) {
        report("malformed_otlp");
        continue;
      }
      for (const scope of array(resource[scopeKey])) {
        if (!object(scope)) {
          report("malformed_otlp");
          continue;
        }
        for (const record of array(scope[recordsKey])) {
          if (!object(record)) {
            report("malformed_otlp");
            continue;
          }
          const context = {
            ...(resource.resource !== undefined ? { resource: structuredClone(resource.resource) } : {}),
            ...(scope.scope !== undefined ? { scope: structuredClone(scope.scope) } : {}),
            ...(resource.schemaUrl !== undefined ? { resourceSchemaUrl: resource.schemaUrl } : {}),
            ...(scope.schemaUrl !== undefined ? { scopeSchemaUrl: scope.schemaUrl } : {}),
          };
          const timestamp = record[clock] ?? (type === "otel.log" ? record.observedTimeUnixNano : undefined);
          /** @type {SessionEvent} */
          const observation = {
            type: type === "otel.span" ? "otel.span" : "otel.log",
            data: { ...structuredClone(record), ...context },
            ...(timestamp !== undefined ? { timestamp } : {}),
          };
          events.push(observation);
          if (type === "otel.span" && record.events !== undefined) {
            // Span events have their own clocks; do not substitute the span start.
            delete observation.data.events;
            for (const event of array(record.events)) {
              if (!object(event)) {
                report("malformed_otlp");
                continue;
              }
              events.push({
                type: "otel.span_event",
                data: { ...structuredClone(event), ...context, ...(record.traceId !== undefined ? { traceId: record.traceId } : {}), ...(record.spanId !== undefined ? { spanId: record.spanId } : {}) },
                ...(event.timeUnixNano !== undefined ? { timestamp: event.timeUnixNano } : {}),
              });
            }
          }
        }
      }
    }
  }
  return events;
}

module.exports = { normalizeOtelEvents };
