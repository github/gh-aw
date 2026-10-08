// @ts-check
"use strict";

const { utf8Prefix, isDiagnosticReport } = require("./command_diagnostics.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/** @param {string} text */
function literal(text) {
  return JSON.stringify(text)
    .slice(1, -1)
    .replaceAll("::", ": :")
    .replace(/[<>&`{}\[\]\x7f-\x9f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`);
}

/** @param {unknown} report @returns {string} */
function renderDiagnostics(report) {
  if (!isDiagnosticReport(report)) throw new Error(`${ERR_VALIDATION}: Invalid command diagnostic report`);
  if (!report.diagnostics.length && !report.issues.length && !report.truncated) return "";
  const at = location => `${location.path}:${location.line}${location.column ? `:${location.column}` : ""}`;
  const lines = report.diagnostics.flatMap(diagnostic => {
    const location = diagnostic.location ? `${diagnostic.location.path}:${diagnostic.location.line}${diagnostic.location.column ? `:${diagnostic.location.column}` : ""}` : diagnostic.language;
    return [
      `${literal(location)}: ${diagnostic.severity}${diagnostic.code ? ` ${literal(diagnostic.code)}` : ""}: ${literal(diagnostic.message)}${diagnostic.test ? ` (test: ${literal(diagnostic.test)})` : ""}`,
      ...(diagnostic.related ?? []).map(item => `  ${item.location ? literal(at(item.location)) + ": " : ""}${literal(item.message)}`),
    ];
  });
  lines.push(...report.issues.map(literal));
  const content = "Command diagnostics (untrusted tool output):\n" + lines.join("\n");
  const truncated = report.truncated || Buffer.byteLength(content) > 4000;
  return utf8Prefix(content, truncated ? 3960 : 4000) + (truncated ? "\n[diagnostics truncated]" : "");
}

module.exports = { renderDiagnostics };
