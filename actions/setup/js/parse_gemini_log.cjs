// @ts-check
/// <reference types="@actions/github-script" />

const { createEngineLogParser, generateConversationMarkdown, generateInformationSection, buildStepSummaryDetailsSection, formatInitializationSummary, formatToolUse, parseLogEntries } = require("./log_parser_shared.cjs");
const { projectSessionResult } = require("./agent_session.cjs");
const { normalizeGeminiSession } = require("./gemini_session.cjs");

const main = createEngineLogParser({
  parserName: "Gemini",
  parseFunction: parseGeminiLog,
  supportsDirectories: false,
});

/**
 * Parse Gemini CLI JSONL log output and format as markdown.
 * Gemini CLI outputs one JSON object per line (JSONL) with typed entries:
 * - type "init": session initialization with model and session_id
 * - type "message": user/assistant messages, assistant uses delta:true for streaming chunks
 * - type "tool_use": tool invocations with tool_name, tool_id, and parameters
 * - type "tool_result": tool responses with tool_id, status, and output
 * - type "result": final stats with token usage, duration, and tool call count
 * @param {string} logContent - The raw log content to parse
 * @returns {{markdown: string, logEntries: Array, mcpFailures: Array<string>, maxTurnsHit: boolean}} Parsed log data
 */
function parseGeminiLog(logContent) {
  if (!logContent) {
    return {
      markdown: buildStepSummaryDetailsSection("Gemini", "No log content provided."),
      logEntries: [],
      mcpFailures: [],
      maxTurnsHit: false,
    };
  }

  // Parse JSONL lines
  const rawEntries = parseLogEntries(logContent) ?? [];

  const logEntries = transformGeminiEntries(rawEntries);
  if (logEntries.length === 0) {
    return {
      markdown: buildStepSummaryDetailsSection("Gemini", "Log format not recognized as Gemini JSONL."),
      logEntries: [],
      mcpFailures: [],
      maxTurnsHit: false,
    };
  }

  // Generate conversation markdown using shared function
  const conversationResult = generateConversationMarkdown(logEntries, {
    includeInformation: false,
    formatToolCallback: (toolUse, toolResult) => formatToolUse(toolUse, toolResult, { includeDetailedParameters: false }),
    formatInitCallback: initEntry => formatInitializationSummary(initEntry, { includeSlashCommands: false }),
  });

  let markdown = conversationResult.markdown;

  markdown += generateInformationSection(projectSessionResult(logEntries));

  return {
    markdown,
    logEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

/**
 * @param {Array<any>} rawEntries
 * @returns {import("./types/agent_session").AgentSession}
 */
function transformGeminiEntries(rawEntries) {
  return normalizeGeminiSession(rawEntries);
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    parseGeminiLog,
    transformGeminiEntries,
  };
}
