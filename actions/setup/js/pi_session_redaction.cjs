// @ts-check
"use strict";

/** @param {string} html @param {(text: string) => {content: string, redactionCount: number}} redact */
function redactPiSessionHTML(html, redact) {
  let redactionCount = 0;
  let matched = false;
  const content = html.replace(/(<script\b(?=[^>]*\bid\s*=\s*["']session-data["'])[^>]*>)([\s\S]*?)(<\/script\s*>)/gi, (_whole, open, payload, close) => {
    matched = true;
    const encoded = payload.trim();
    if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded) || !encoded) throw new Error("Pi session export contains invalid base64 data");
    const bytes = Buffer.from(encoded, "base64");
    const decoded = bytes.toString("utf8");
    if (!Buffer.from(decoded, "utf8").equals(bytes)) throw new Error("Pi session export contains invalid UTF-8 data");
    const session = JSON.parse(decoded);
    const sanitize = value => {
      if (typeof value === "string") {
        const result = redact(value);
        redactionCount += result.redactionCount;
        return result.content;
      }
      if (Array.isArray(value)) return value.map(sanitize);
      if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [sanitize(key), sanitize(item)]));
      return value;
    };
    return open + Buffer.from(JSON.stringify(sanitize(session)), "utf8").toString("base64") + close;
  });
  if (!matched && /\bid\s*=\s*["']session-data["']/i.test(html)) throw new Error("Pi session export has a malformed session-data element");
  return { content, redactionCount };
}

module.exports = { redactPiSessionHTML };
