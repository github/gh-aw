// @ts-check

const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent } = require("./agent_session.cjs");
const { stripVTControlCharacters } = require("node:util");

const main = createEngineLogParser({ parserName: "DeepSeek Harness", parseFunction: parseDeepSeekLog, supportsDirectories: false });

/**
 * Recognize the completed headless stdout envelope observed in real AWF runs.
 * Everything before configuration and after shutdown is infrastructure, not
 * conversation. Ambiguous, truncated, and prompt-labeled output is withheld.
 * @param {string} content
 * @returns {{model: string, content: string} | undefined}
 */
function extractHeadlessAnswer(content) {
  const lines = stripVTControlCharacters(content).split(/\r?\n/);
  const configurations = [];
  for (const [index, line] of lines.entries()) {
    const match = line.match(/^\[deepseek-harness\] configured provider=([a-z][a-z0-9-]*) model=(\S+)$/);
    if (match) configurations.push({ index, model: match[2] });
  }
  if (configurations.length !== 1) return;
  const configuration = configurations[0];
  const shutdown = lines.findIndex((line, index) => index > configuration.index && line === "[INFO] Stopping containers...");
  if (shutdown < 0) return;
  const completed = lines.findIndex((line, index) => index > shutdown && line === "[SUCCESS] Command completed successfully");
  if (completed < 0 || !lines.slice(completed + 1).includes("Process exiting with code: 0")) return;

  let start = configuration.index + 1;
  while (
    start < shutdown &&
    (lines[start].trim() === "" || lines[start] === "[entrypoint] Unsetting sensitive tokens from parent shell environment..." || /^\[entrypoint\] Unset [A-Z][A-Z0-9_]* from \/proc\/1\/environ$/.test(lines[start]))
  ) {
    start++;
  }
  const answerLines = lines.slice(start, shutdown);
  while (answerLines.length && answerLines[answerLines.length - 1].trim() === "") answerLines.pop();
  const answer = answerLines.join("\n");
  if (!answer || /^[ \t]*\[(?:entrypoint|INFO|WARN|SUCCESS|deepseek-harness)\]/m.test(answer) || /^[ \t]*(?:(?:user|system)\s*(?:prompt|message)?\s*:|prompt\s*:)/im.test(answer)) return;
  return { model: configuration.model, content: answer };
}

/** @param {string} content @returns {boolean} */
function isDeepSeekLog(content) {
  return extractHeadlessAnswer(content) !== undefined;
}

/**
 * Preserve the final headless answer as canonical assistant text. Native tool
 * calls, reasoning, usage, and task success are not reported by this stdout
 * format and must not be inferred from the answer or runtime MCP metadata.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseDeepSeekLog(content) {
  const answer = extractHeadlessAnswer(content);
  const logEntries = answer ? [createSessionEvent({}, "session.init", { sourceEngine: "deepseek-harness", model: answer.model }), createSessionEvent({}, "assistant.message", { content: answer.content })] : [];
  return {
    markdown: logEntries.length
      ? generateCopilotCliStyleSummary(logEntries)
      : buildStepSummaryDetailsSection("DeepSeek Harness", "Completed headless stdout envelope not recognized. Raw content is omitted because it may contain prompts or secrets."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

module.exports = { main, parseDeepSeekLog, isDeepSeekLog };
