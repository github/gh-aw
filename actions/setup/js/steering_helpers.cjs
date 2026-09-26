// @ts-check

const { parseJsonlContent } = require("./jsonl_helpers.cjs");

/**
 * Pre-filter pattern: only parse lines that contain the word "steering".
 * This avoids JSON.parse on unrelated log entries.
 *
 * @type {RegExp}
 */
const STEERING_EVENT_PATTERN = /steering/i;

/**
 * Resolve an event name from a firewall proxy event entry.
 *
 * Supports log schema variants used across AWF versions:
 *   - Top-level `event` field: `{ event: "token_steering", ... }`
 *   - Top-level `type` field:  `{ type: "model_steering", ... }`
 *   - Top-level `event_name` or `eventName` field
 *   - Nested payload:          `{ payload: { event: "steering" } }`
 *
 * @param {unknown} entry
 * @returns {string}
 */
function getApiProxyEventName(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
    return "";
  }
  if ("event" in entry && typeof entry.event === "string") {
    return entry.event;
  }
  if ("type" in entry && typeof entry.type === "string") {
    return entry.type;
  }
  if ("event_name" in entry && typeof entry.event_name === "string") {
    return entry.event_name;
  }
  if ("eventName" in entry && typeof entry.eventName === "string") {
    return entry.eventName;
  }
  if ("payload" in entry) {
    const payload = entry.payload;
    if (payload && typeof payload === "object" && !Array.isArray(payload)) {
      if ("event" in payload && typeof payload.event === "string") {
        return payload.event;
      }
      if ("type" in payload && typeof payload.type === "string") {
        return payload.type;
      }
    }
  }
  return "";
}

/**
 * Count steering events by normalized event name in proxy event-log JSONL content.
 *
 * Known steering event names: "steering", "token_steering", "model_steering".
 * Any event whose name is exactly "steering" or ends with "_steering" is counted.
 *
 * @param {string} jsonlContent
 * @returns {Record<string, number>}
 */
function countSteeringEventsByTypeInApiProxyJsonl(jsonlContent) {
  /** @type {Record<string, number>} */
  const counts = {};
  for (const parsed of parseJsonlContent(jsonlContent, line => STEERING_EVENT_PATTERN.test(line))) {
    const eventName = getApiProxyEventName(parsed).toLowerCase();
    if (eventName === "steering" || eventName.endsWith("_steering")) {
      counts[eventName] = (counts[eventName] || 0) + 1;
    }
  }
  return Object.fromEntries(Object.entries(counts).sort(([left], [right]) => left.localeCompare(right)));
}

/**
 * Count all steering events in proxy event-log JSONL content.
 *
 * @param {string} jsonlContent
 * @returns {number}
 */
function countSteeringEventsInApiProxyJsonl(jsonlContent) {
  return Object.values(countSteeringEventsByTypeInApiProxyJsonl(jsonlContent)).reduce((total, count) => total + count, 0);
}

module.exports = {
  STEERING_EVENT_PATTERN,
  getApiProxyEventName,
  countSteeringEventsByTypeInApiProxyJsonl,
  countSteeringEventsInApiProxyJsonl,
};
