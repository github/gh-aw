// @ts-check
"use strict";

/**
 * One diagnostic per line as `file:line:column: message [Error|Warning/rule]`.
 * ESLint 9 removed its built-in `unix` formatter; the monster pre-check selects
 * the assigned rule's diagnostics from this shape with grep.
 * @param {Array<{filePath: string, messages: Array<{ruleId: string | null, severity: number, fatal?: boolean, message: string, line?: number, column?: number}>}>} results
 */
module.exports = function unixFormatter(results) {
  /** @type {Record<string, string>} */
  const escapes = { "\\": "\\\\", "\r": "\\r", "\n": "\\n", "\t": "\\t", "[": "\\x5B", "]": "\\x5D", ":": "\\x3A", "`": "\\x60" };
  /** @param {unknown} value */
  const escapeDiagnostic = value =>
    String(value ?? "")
      .replace(/[\\\r\n\t\[\]:`]/g, character => escapes[character])
      .replace(/\s+/g, " ")
      .trim();
  const lines = [];
  for (const result of results) {
    for (const message of result.messages) {
      const severity = message.fatal || message.severity === 2 ? "Error" : "Warning";
      const rule = message.ruleId ? `/${escapeDiagnostic(message.ruleId)}` : "";
      lines.push(`${escapeDiagnostic(result.filePath)}:${message.line ?? 0}:${message.column ?? 0}: ${escapeDiagnostic(message.message)} [${severity}${rule}]`);
    }
  }
  return lines.length ? `${lines.join("\n")}\n` : "";
};
