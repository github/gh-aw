// @ts-check
"use strict";

/**
 * One diagnostic per line as `file:line:column: message [Error|Warning/rule]`.
 * ESLint 9 removed its built-in `unix` formatter; the monster pre-check selects
 * the assigned rule's diagnostics from this shape with grep.
 * @param {Array<{filePath: string, messages: Array<{ruleId: string | null, severity: number, fatal?: boolean, message: string, line?: number, column?: number}>}>} results
 */
module.exports = function unixFormatter(results) {
  const lines = [];
  for (const result of results) {
    for (const message of result.messages) {
      const severity = message.fatal || message.severity === 2 ? "Error" : "Warning";
      const rule = message.ruleId ? `/${message.ruleId}` : "";
      lines.push(`${result.filePath}:${message.line || 0}:${message.column || 0}: ${message.message.replace(/\s+/g, " ").trim()} [${severity}${rule}]`);
    }
  }
  return lines.length ? `${lines.join("\n")}\n` : "";
};
