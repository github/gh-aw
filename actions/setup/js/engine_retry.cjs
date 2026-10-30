// @ts-check
"use strict";

/**
 * Keep only observed, bounded engine-native retry counters.
 * @param {Record<string, any>} source
 * @returns {import("./types/agent_session").EngineRetryData}
 */
function normalizeEngineRetryData(source) {
  const data = {};
  if (Number.isSafeInteger(source.attempt) && source.attempt >= 1) {
    data.attempt = source.attempt;
    for (const field of ["maxRetries", "backoffMs"]) {
      if (Number.isSafeInteger(source[field]) && source[field] >= 0) data[field] = source[field];
    }
  }
  return data;
}

/**
 * Upgrade saved retry diagnostics without adding another observation.
 * @param {import("./types/agent_session").SessionEvent} event
 * @returns {import("./types/agent_session").SessionEvent}
 */
function normalizeEngineRetryEvent(event) {
  if (event.type !== "session.result") return event;
  const source = event.data;
  const retry = normalizeEngineRetryData(
    source.engineRetry && typeof source.engineRetry === "object" && !Array.isArray(source.engineRetry)
      ? source.engineRetry
      : source.subtype === "api_retry"
        ? { attempt: source.attempt, maxRetries: source.max_retries, backoffMs: source.retry_delay_ms }
        : { attempt: event.reconnectAttempt, maxRetries: event.maxReconnects }
  );
  return retry.attempt === undefined ? event : { ...event, type: "engine.retry", data: { ...source, ...retry } };
}

module.exports = { normalizeEngineRetryData, normalizeEngineRetryEvent };
