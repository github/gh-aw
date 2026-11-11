// @ts-check
"use strict";

/**
 * Read Retry-After from Copilot terminal error events or stderr, never assistant/tool output.
 * Supports JSON/Node header dumps and HTTP header lines, including headers inside a
 * JSON-encoded error message. Invalid or expired values preserve the existing retry policy.
 * @param {string} output
 * @param {number} [now]
 * @returns {number | null}
 */
function parseCopilotRetryAfterMs(output, now = Date.now()) {
  const text = output.replace(/\\[rn]/g, "\n").replace(/\\"/g, '"');
  if (!/\b429\b/.test(text)) return null;
  const headers = /(?:^|[\s{,])["']?retry-after["']?\s*:\s*(?:"([^"\r\n]*)"|'([^'\r\n]*)'|(\d+)(?=\s*(?:[,}"'\r\n]|$))|([^"'\r\n}]+))/gi;
  /** @type {number | null} */
  let delay = null;
  for (const match of text.matchAll(headers)) {
    const value = (match[1] ?? match[2] ?? match[3] ?? match[4]).trim();
    let ms;
    if (/^\d+$/.test(value)) {
      ms = Number(value) * 1000;
      // An overflowing delay must fail the bound check, not fall back to an early retry.
      if (!Number.isSafeInteger(ms)) ms = Number.MAX_SAFE_INTEGER;
    } else if (/^[A-Za-z]{3}, /.test(value)) {
      ms = Date.parse(value) - now;
    } else {
      continue;
    }
    if (Number.isFinite(ms) && ms > 0) delay = Math.max(delay ?? 0, ms);
  }
  return delay;
}

module.exports = { parseCopilotRetryAfterMs };
