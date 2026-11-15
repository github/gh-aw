// @ts-check
"use strict";

/** The publisher and pending manifest share finding normalization and rendering. */
function prepareCodeScanningFinding(item) {
  if (!item.file || typeof item.file !== "string" || !item.message || typeof item.message !== "string") throw new Error("Code scanning finding requires file and message strings");
  const line = parseInt(item.line, 10);
  const column = item.column === undefined ? 1 : parseInt(item.column, 10);
  if (Number.isNaN(line) || line <= 0 || Number.isNaN(column) || column <= 0) throw new Error("Code scanning finding requires positive line and column");
  const severity = String(item.severity || "").toLowerCase();
  const levels = { error: "error", warning: "warning", info: "note", note: "note" };
  if (!Object.hasOwn(levels, severity)) throw new Error("Code scanning finding has invalid severity");
  const suffix = item.ruleIdSuffix === undefined ? null : typeof item.ruleIdSuffix === "string" ? item.ruleIdSuffix.trim() : "";
  if (suffix !== null && !/^[a-zA-Z0-9_-]+$/.test(suffix)) throw new Error("Code scanning finding has invalid rule ID suffix");
  return { file: item.file.trim(), line, column, severity, sarifLevel: levels[severity], message: item.message.trim(), ruleIdSuffix: suffix };
}

function buildCodeScanningSarif(findings, config = {}) {
  const workflow = config.workflow_filename || "workflow";
  return {
    $schema: "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
    version: "2.1.0",
    runs: [
      {
        tool: { driver: { name: config.driver || "GitHub Agentic Workflows Security Scanner", version: "1.0.0", informationUri: "https://github.com/github/gh-aw" } },
        results: findings.map((finding, index) => ({
          ruleId: finding.ruleIdSuffix ? `${workflow}-${finding.ruleIdSuffix}` : `${workflow}-security-finding-${index + 1}`,
          message: { text: finding.message },
          level: finding.sarifLevel,
          locations: [{ physicalLocation: { artifactLocation: { uri: finding.file }, region: { startLine: finding.line, startColumn: finding.column } } }],
        })),
      },
    ],
  };
}

module.exports = { prepareCodeScanningFinding, buildCodeScanningSarif };
