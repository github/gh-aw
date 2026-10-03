// @ts-check
/// <reference types="@actions/github-script" />

const { createEngineLogParser, buildStepSummaryDetailsSection, parseLogEntries } = require("./log_parser_shared.cjs");
const { isSessionEvent } = require("./agent_session.cjs");

const main = createEngineLogParser({
  parserName: "Custom",
  parseFunction: parseCustomLog,
  supportsDirectories: false,
});

/**
 * Parses custom engine log content by attempting multiple parser strategies
 * @param {string} logContent - The raw log content as a string
 * @returns {{markdown: string, mcpFailures: string[], maxTurnsHit: boolean, logEntries: Array}} Result with formatted markdown content, MCP failure list, max-turns status, and parsed log entries
 */
function parseCustomLog(logContent) {
  const entries = parseLogEntries(logContent) ?? [];
  const { isOpenCodeEvent, parseOpenCodeLog } = require("./parse_opencode_log.cjs");
  if (entries.some(isOpenCodeEvent)) {
    const result = parseOpenCodeLog(logContent);
    return { ...result, markdown: `### Custom Engine Log (OpenCode format)\n\n${result.markdown}` };
  }
  const claudeSignature = entries.some(
    entry =>
      entry &&
      (isSessionEvent(entry) ||
        (entry.type === "system" && entry.subtype === "init") ||
        (["assistant", "user"].includes(entry.type) && (typeof entry.message?.content === "string" || Array.isArray(entry.message?.content))) ||
        (entry.type === "result" && ["usage", "num_turns", "errors", "duration_ms", "total_cost_usd", "permission_denials"].some(key => Object.hasOwn(entry, key))))
  );
  if (claudeSignature) {
    const claudeModule = require("./parse_claude_log.cjs");
    const claudeResult = claudeModule.parseClaudeLog(logContent);

    // If we got meaningful results from Claude parser, use them
    if (claudeResult && claudeResult.logEntries && claudeResult.logEntries.length > 0) {
      return {
        ...claudeResult,
        markdown: `### Custom Engine Log (Claude format)\n\n${claudeResult.markdown}`,
      };
    }
  }

  // Try Codex parser as fallback
  // Codex parser now returns an object { markdown, logEntries, mcpFailures, maxTurnsHit }
  const codexModule = require("./parse_codex_log.cjs");
  const codexSignature =
    codexModule.isCodexJsonlFormat([], entries) || /^(?:OpenAI Codex\b|model:\s+\S|thinking\s*$|tool\s+\w+\.\w+\(|ERROR:\s+\S)|ToolCall:\s+\w+__\w+|^\[.*?\]\s+(?:tool\s+\w+\.\w+\(|exec\s+bash\s+-lc\s+')/m.test(logContent);
  if (codexSignature) {
    const codexResult = codexModule.parseCodexLog(logContent, entries);

    // Check if we got meaningful content
    if (codexResult && codexResult.logEntries.length > 0) {
      return {
        markdown: `### Custom Engine Log (Codex format)\n\n${codexResult.markdown}`,
        mcpFailures: codexResult.mcpFailures || [],
        maxTurnsHit: codexResult.maxTurnsHit || false,
        logEntries: codexResult.logEntries || [],
      };
    }
  }

  // Fallback: Return basic log info if no structured format was detected
  const lineCount = logContent.split("\n").filter(line => line.trim().length > 0).length;
  const charCount = logContent.length;

  return {
    markdown: buildStepSummaryDetailsSection(
      "Custom Engine Log",
      `      Log format not recognized as Claude, Codex, or OpenCode format.

**Basic Statistics:**
- Lines: ${lineCount}
- Characters: ${charCount}

Raw content is omitted because unrecognized logs may contain user prompts or secrets.`
    ),
    mcpFailures: [],
    maxTurnsHit: false,
    logEntries: [],
  };
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    parseCustomLog,
  };
}
