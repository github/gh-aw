// @ts-check

"use strict";

const PERMISSION_DENIED_PATTERN = /\b(?:permission denied|permissions denied|EACCES|EPERM)\b/gi;
const NUMEROUS_PERMISSION_DENIED_THRESHOLD = 3;

/**
 * Count permission-denied indicators in process output.
 * @param {string} output
 * @returns {number}
 */
function countPermissionDeniedIssues(output) {
  if (!output) return 0;
  const matches = output.match(PERMISSION_DENIED_PATTERN);
  return matches ? matches.length : 0;
}

/**
 * Detect whether output contains numerous permission-denied issues.
 * @param {string} output
 * @returns {boolean}
 */
function hasNumerousPermissionDeniedIssues(output) {
  return countPermissionDeniedIssues(output) >= NUMEROUS_PERMISSION_DENIED_THRESHOLD;
}

/**
 * Extract the commands that were denied from process output.
 * Scans for lines using the pipe marker (│) that appear
 * within three lines before each "permission denied" occurrence.
 * Returns a deduplicated array of command strings (may be empty if
 * the output format does not contain extractable commands).
 * @param {string} output
 * @returns {string[]}
 */
function extractDeniedCommands(output) {
  if (!output) return [];
  const lines = output.split("\n");
  const deniedCommands = new Set();
  const permissionSummaryPatterns = [/permission denied by workflow tool permissions:\s*(.+?)\s*$/i, /tool denial \d+\/\d+:\s*permission denied:\s*(.+?)\s*$/i];
  for (let i = 0; i < lines.length; i++) {
    let capturedFromLine = false;
    for (const pattern of permissionSummaryPatterns) {
      const summaryMatch = lines[i].match(pattern);
      if (summaryMatch && summaryMatch[1] && summaryMatch[1].trim()) {
        deniedCommands.add(summaryMatch[1].trim());
        capturedFromLine = true;
        break;
      }
    }

    if (capturedFromLine) {
      continue;
    }

    if (/\bpermission denied\b/i.test(lines[i])) {
      // Look back up to 3 lines for a command displayed with the
      // box-drawing pipe marker (│ U+2502) or plain pipe (|).
      for (let j = i - 1; j >= Math.max(0, i - 3); j--) {
        const cmdMatch = lines[j].match(/^\s*[\u2502|]\s+(.+)\s*$/);
        if (cmdMatch && cmdMatch[1].trim()) {
          deniedCommands.add(cmdMatch[1].trim());
          break;
        }
      }
    }
  }
  return [...deniedCommands];
}

/**
 * Build a structured report_missing_tool payload for repeated permission-denied failures.
 * @param {string[]} [deniedCommands] - Commands that were denied (may be empty)
 * @returns {string}
 */
function buildMissingToolPermissionIssuePayload(deniedCommands) {
  const workflowToolDenial = deniedCommands?.some(command => /^(?:shell|read|write|mcp|custom-tool)\(/.test(command)) === true;
  return JSON.stringify({
    type: "report_missing_tool",
    tool: "tool/permission",
    reason: "missing tool/permission issue: numerous permission denied errors detected",
    alternatives: workflowToolDenial
      ? "Use only commands allowed by the workflow tools configuration; stop unsupported probes before the denial budget is exhausted. A workflow tool denial does not establish missing token scopes or invalid credentials."
      : "Verify token scopes, repository permissions, and MCP/tool access configuration.",
    denied_commands: deniedCommands && deniedCommands.length > 0 ? deniedCommands : [],
  });
}

module.exports = {
  countPermissionDeniedIssues,
  hasNumerousPermissionDeniedIssues,
  extractDeniedCommands,
  buildMissingToolPermissionIssuePayload,
};
