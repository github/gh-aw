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
    const normalizeIP = host => {
      const unbracketed = host.replace(/^\[|\]$/g, "");
      if (isIP(unbracketed) === 4) return unbracketed;
      const address = unbracketed.replace(/%(?:25)?[a-z0-9_.~-]+$/i, "");
      return isIP(address) === 6 ? address : "";
    };
    const literalIP = normalizeIP(value);
    if (literalIP) return literalIP;

    const hasScheme = value.includes("://");
    if (!hasScheme && /[/?#@\\\s]/.test(value)) return "";
    if (hasScheme && !/^[a-z][a-z0-9+.-]*:\/\/[^/?#\\]+/i.test(value)) return "";
    const url = new URL(hasScheme ? value : `https://${value}`);
    if (url.protocol !== "https:" && url.protocol !== "http:") return "";
    const hostname = normalizeIP(url.hostname);
    if (hostname) return hostname;
    if (!hasScheme) {
      const host = url.hostname.toLowerCase();
      const hasPort = /:\d+$/.test(value) || /\]:\d+$/.test(value);
      if (["http", "https", "file", "ftp", "ftps", "ws", "wss", "data", "javascript", "mailto"].includes(host)) return "";
      if (host !== "localhost" && !host.includes(".") && !hasPort) return "";
    }
    return url.hostname;
  } catch {
    return "";
  }
}

module.exports = { genAiServerAddress };
