// @ts-check
"use strict";

const RESPONSE_STATUS_PATTERN = /\bHTTP(?:\/\d(?:\.\d)?)?\s+(\d{3})\b|\bCAPIError:\s*(?:(\d{3})\b|Too Many Requests\b)|\b429\s+Too Many Requests\b|["']?(?:status|statusCode|status_code)["']?\s*:\s*["']?(\d{3})["']?\b/gi;
const RETRY_AFTER_PATTERN = /(?:^|[\s{,])["']?retry-after["']?\s*:\s*(?:"([^"\r\n]*)"|'([^'\r\n]*)'|(\d+)(?=\s*(?:[,}"'\r\n]|$))|([^"'\r\n}]+))/gi;
const MONTHS = new Map(["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"].map((month, index) => [month, index]));

/**
 * Normalize escaped output and split it into status-specific response segments.
 * @param {string} output
 * @returns {{ status: number, text: string }[]}
 */
function getResponseSegments(output) {
  const text = output.replace(/\\[rn]/g, "\n").replace(/\\"/g, '"');
  const matches = [...text.matchAll(new RegExp(RESPONSE_STATUS_PATTERN))];
  return matches.map((match, index) => {
    const start = match.index ?? 0;
    const end = matches[index + 1]?.index ?? text.length;
    const status = Number(match[1] ?? match[2] ?? match[3] ?? 429);
    return { status, text: text.slice(start, end) };
  });
}

/**
 * @param {string} value
 * @param {number} now
 * @returns {number | null}
 */
function parseHttpDate(value, now) {
  let day;
  let month;
  let year;
  let hour;
  let minute;
  let second;
  let match = /^(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), (\d{2}) ([A-Z][a-z]{2}) (\d{4}) (\d{2}):(\d{2}):(\d{2}) GMT$/.exec(value);
  if (match) {
    [, day, month, year, hour, minute, second] = match;
  } else {
    match = /^(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), (\d{2})-([A-Z][a-z]{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2}) GMT$/.exec(value);
    if (match) {
      [, day, month, year, hour, minute, second] = match;
      const currentYear = new Date(now).getUTCFullYear();
      let candidateYear = Math.floor(currentYear / 100) * 100 + Number(year);
      const monthIndex = MONTHS.get(month);
      if (monthIndex === undefined) return null;
      const futureThreshold = new Date(now);
      futureThreshold.setUTCFullYear(currentYear + 50);
      const futureThresholdMs = futureThreshold.getTime();
      if (!Number.isFinite(futureThresholdMs)) return null;
      if (Date.UTC(candidateYear, monthIndex, Number(day), Number(hour), Number(minute), Number(second)) > futureThresholdMs) {
        candidateYear -= 100;
      }
      year = String(candidateYear);
    } else {
      match = /^(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) ([A-Z][a-z]{2})\s+(\d{1,2}) (\d{2}):(\d{2}):(\d{2}) (\d{4})$/.exec(value);
      if (!match) return null;
      [, month, day, hour, minute, second, year] = match;
    }
  }

  const monthIndex = MONTHS.get(month);
  if (monthIndex === undefined) return null;
  const timestamp = Date.UTC(Number(year), monthIndex, Number(day), Number(hour), Number(minute), Number(second));
  const date = new Date(timestamp);
  if (
    date.getUTCFullYear() !== Number(year) ||
    date.getUTCMonth() !== monthIndex ||
    date.getUTCDate() !== Number(day) ||
    date.getUTCHours() !== Number(hour) ||
    date.getUTCMinutes() !== Number(minute) ||
    date.getUTCSeconds() !== Number(second)
  ) {
    return null;
  }
  return timestamp - now;
}

/**
 * Determine whether a response status is present, including recognized Copilot error signatures.
 * @param {string} output
 * @returns {boolean}
 */
function hasCopilotResponseStatus(output) {
  return getResponseSegments(output).length > 0;
}

/**
 * Parse the largest valid Retry-After value associated with an HTTP 429 response.
 * Supports delta-seconds and all three HTTP-date formats.
 * @param {string} output
 * @param {number} [now]
 * @returns {number | null}
 */
function parseCopilotRetryAfterMs(output, now = Date.now()) {
  /** @type {number | null} */
  let delay = null;
  for (const { status, text } of getResponseSegments(output)) {
    if (status !== 429) continue;
    for (const match of text.matchAll(new RegExp(RETRY_AFTER_PATTERN))) {
      const value = (match[1] ?? match[2] ?? match[3] ?? match[4]).trim();
      let milliseconds;
      if (/^\d+$/.test(value)) {
        milliseconds = Number(value) * 1000;
        if (!Number.isSafeInteger(milliseconds)) milliseconds = Number.MAX_SAFE_INTEGER;
      } else {
        milliseconds = parseHttpDate(value, now);
      }
      if (milliseconds !== null && Number.isFinite(milliseconds) && milliseconds > 0) {
        delay = Math.max(delay ?? 0, milliseconds);
      }
    }
  }
  return delay;
}

module.exports = { hasCopilotResponseStatus, parseCopilotRetryAfterMs };
