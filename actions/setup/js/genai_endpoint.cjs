// @ts-check

const { isIP } = require("net");

/**
 * Retain only an inference endpoint's hostname for telemetry. Never publish
 * URL credentials, paths, queries, or fragments, even when parsing fails.
 * @param {unknown} endpoint
 * @returns {string}
 */
function genAiServerAddress(endpoint) {
  if (typeof endpoint !== "string" || !endpoint.trim() || endpoint.includes("${{")) return "";
  try {
    const value = endpoint.trim();
    if (isIP(value)) return value;
    const url = new URL(value.includes("://") ? value : `https://${value}`);
    if (url.protocol !== "https:" && url.protocol !== "http:") return "";
    return url.hostname.replace(/^\[|\]$/g, "");
  } catch {
    return "";
  }
}

module.exports = { genAiServerAddress };
