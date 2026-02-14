// @ts-check
/// <reference types="@actions/github-script" />

const { createEngineLogParser } = require("./log_parser_shared.cjs");

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
  // Try Claude parser first (handles JSONL and JSON array formats)
  // Claude parser returns an object with markdown, logEntries, mcpFailures, maxTurnsHit
  try {
    const claudeModule = require("./parse_claude_log.cjs");
    const claudeResult = claudeModule.parseClaudeLog(logContent);

    // If we got meaningful results from Claude parser, use them
    if (claudeResult && claudeResult.logEntries && claudeResult.logEntries.length > 0) {
      return {
        ...claudeResult,
        markdown: "## Custom Engine Log (Claude format)\n\n" + claudeResult.markdown,
      };
    }
  } catch (error) {
    // Claude parser failed, continue to next strategy
  }

  // Try Codex parser as fallback
  // Codex parser now returns an object { markdown, logEntries, mcpFailures, maxTurnsHit }
  try {
    const codexModule = require("./parse_codex_log.cjs");
    const codexResult = codexModule.parseCodexLog(logContent);

    // Check if we got meaningful content
    if (codexResult && codexResult.markdown && codexResult.markdown.length > 0) {
      return {
        markdown: "## Custom Engine Log (Codex format)\n\n" + codexResult.markdown,
        mcpFailures: codexResult.mcpFailures || [],
        maxTurnsHit: codexResult.maxTurnsHit || false,
        logEntries: codexResult.logEntries || [],
      };
    }
  } catch (error) {
    // Codex parser failed, continue to fallback
  }

  // Fallback: Return basic log info if no structured format was detected
  const lineCount = logContent.split("\n").filter(line => line.trim().length > 0).length;
  const charCount = logContent.length;

  return {
    markdown: `## Custom Engine Log

Log format not recognized as Claude or Codex format.

**Basic Statistics:**
- Lines: ${lineCount}
- Characters: ${charCount}

**Raw Log Preview:**
\`\`\`
${logContent.substring(0, 1000)}${logContent.length > 1000 ? "\n... (truncated)" : ""}
\`\`\`
`,
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
