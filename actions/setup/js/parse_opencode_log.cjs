// @ts-check
/// <reference types="@actions/github-script" />

const { createEngineLogParser, parseLogEntries, generateConversationMarkdown, generateInformationSection, buildStepSummaryDetailsSection, formatToolUse, formatInitializationSummary } = require("./log_parser_shared.cjs");
const { projectSessionResult } = require("./agent_session.cjs");
const { normalizeOpenCodeSession } = require("./opencode_session.cjs");

const main = createEngineLogParser({
  parserName: "OpenCode",
  parseFunction: parseOpenCodeLog,
  supportsDirectories: false,
});

/** @param {string} logContent */
function parseOpenCodeLog(logContent) {
  const logEntries = normalizeOpenCodeSession(parseLogEntries(logContent) ?? []);
  const markdown = logEntries.length
    ? generateConversationMarkdown(logEntries, { includeInformation: false, formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary }).markdown + generateInformationSection(projectSessionResult(logEntries))
    : buildStepSummaryDetailsSection("OpenCode", "Log format not recognized as OpenCode JSON events. Use run --format json; startup diagnostics and legacy bootstrap accounting are not model evidence.");
  return { markdown, logEntries, mcpFailures: [], maxTurnsHit: false };
}

module.exports = { main, parseOpenCodeLog };
