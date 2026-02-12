// @ts-check

/**
 * Recursively sanitizes an object to remove dangerous prototype pollution keys.
 * This function removes keys that could be used for prototype pollution attacks:
 * - __proto__: JavaScript's prototype chain accessor
 * - constructor: Object constructor property
 * - prototype: Function prototype property
 *
 * @param {any} obj - The object to sanitize (can be any type)
 * @returns {any} The sanitized object with dangerous keys removed
 *
 * @example
 * // Removes __proto__ key
 * sanitizePrototypePollution({name: "test", __proto__: {isAdmin: true}})
 * // Returns: {name: "test"}
 *
 * @example
 * // Recursively sanitizes nested objects
 * sanitizePrototypePollution({outer: {__proto__: {bad: true}, safe: "value"}})
 * // Returns: {outer: {safe: "value"}}
 */
function sanitizePrototypePollution(obj) {
  // Handle non-objects (primitives, null, undefined)
  if (obj === null || typeof obj !== "object") {
    return obj;
  }

  // Dangerous keys that can be used for prototype pollution
  const dangerousKeys = ["__proto__", "constructor", "prototype"];

  // Handle arrays
  if (Array.isArray(obj)) {
    return obj.map(item => sanitizePrototypePollution(item));
  }

  // Handle objects
  const sanitized = {};
  for (const key in obj) {
    // Skip dangerous keys
    if (dangerousKeys.includes(key)) {
      continue;
    }
    // Only process own properties (not inherited)
    if (Object.prototype.hasOwnProperty.call(obj, key)) {
      sanitized[key] = sanitizePrototypePollution(obj[key]);
    }
  }
  return sanitized;
}

/**
 * Attempts to repair malformed JSON strings using various heuristics.
 * This function applies multiple repair strategies to fix common JSON formatting issues:
 * - Escapes control characters
 * - Converts single quotes to double quotes
 * - Quotes unquoted object keys
 * - Escapes embedded quotes within strings
 * - Balances mismatched braces and brackets
 * - Removes trailing commas
 *
 * @param {string} jsonStr - The potentially malformed JSON string to repair
 * @returns {string} The repaired JSON string
 *
 * @example
 * // Repairs unquoted keys
 * repairJson("{name: 'value'}") // Returns: '{"name":"value"}'
 *
 * @example
 * // Balances mismatched braces
 * repairJson('{"key": "value"') // Returns: '{"key":"value"}'
 */
function repairJson(jsonStr) {
  let repaired = jsonStr.trim();

  // Escape control characters
  const _ctrl = { 8: "\\b", 9: "\\t", 10: "\\n", 12: "\\f", 13: "\\r" };
  repaired = repaired.replace(/[\u0000-\u001F]/g, ch => {
    const c = ch.charCodeAt(0);
    return _ctrl[c] || "\\u" + c.toString(16).padStart(4, "0");
  });

  // Convert single quotes to double quotes
  repaired = repaired.replace(/'/g, '"');

  // Quote unquoted object keys
  repaired = repaired.replace(/([{,]\s*)([a-zA-Z_$][a-zA-Z0-9_$]*)\s*:/g, '$1"$2":');

  // Escape newlines, returns, and tabs within string values
  repaired = repaired.replace(/"([^"\\]*)"/g, (match, content) => {
    if (content.includes("\n") || content.includes("\r") || content.includes("\t")) {
      const escaped = content.replace(/\\/g, "\\\\").replace(/\n/g, "\\n").replace(/\r/g, "\\r").replace(/\t/g, "\\t");
      return `"${escaped}"`;
    }
    return match;
  });

  // Escape embedded quotes within strings (handles patterns like "text"embedded"text")
  repaired = repaired.replace(/"([^"]*)"([^":,}\]]*)"([^"]*)"(\s*[,:}\]])/g, (match, p1, p2, p3, p4) => `"${p1}\\"${p2}\\"${p3}"${p4}`);

  // Fix arrays that are improperly closed with } instead of ]
  repaired = repaired.replace(/(\[\s*(?:"[^"]*"(?:\s*,\s*"[^"]*")*\s*),?)\s*}/g, "$1]");

  // Balance mismatched opening/closing braces
  const openBraces = (repaired.match(/\{/g) || []).length;
  const closeBraces = (repaired.match(/\}/g) || []).length;
  if (openBraces > closeBraces) {
    repaired += "}".repeat(openBraces - closeBraces);
  } else if (closeBraces > openBraces) {
    repaired = "{".repeat(closeBraces - openBraces) + repaired;
  }

  // Balance mismatched opening/closing brackets
  const openBrackets = (repaired.match(/\[/g) || []).length;
  const closeBrackets = (repaired.match(/\]/g) || []).length;
  if (openBrackets > closeBrackets) {
    repaired += "]".repeat(openBrackets - closeBrackets);
  } else if (closeBrackets > openBrackets) {
    repaired = "[".repeat(closeBrackets - openBrackets) + repaired;
  }

  // Remove trailing commas before closing braces/brackets
  repaired = repaired.replace(/,(\s*[}\]])/g, "$1");

  return repaired;
}

module.exports = { repairJson, sanitizePrototypePollution };
