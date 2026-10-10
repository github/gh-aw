// @ts-check

const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent } = require("./agent_session.cjs");
const { collectAgentExecution } = require("./agent_execution.cjs");
const { applyAddMaskRedaction, collectAddMaskedValues } = require("./add_mask_redaction.cjs");
const { stripVTControlCharacters } = require("node:util");

const main = createEngineLogParser({ parserName: "DeepSeek Harness", parseFunction: parseDeepSeekLog, supportsDirectories: false });
const HARNESS_FAILURE = /^\[deepseek-harness\] DeepSeek Harness execution failed with exit code ([1-9]\d*)(?: \(signal=[A-Z0-9]+\))?$/;
const TOKEN_CLEANUP = /^\[entrypoint\] Unset [A-Z][A-Z0-9_]* from \/proc\/1\/environ$/;
const PERMISSION_CLEANUP = /^\[entrypoint\] Relaxed \S+ group permissions for host-side post-processing$/;

/**
 * @param {string[]} lines
 * @returns {{index: number, provider: string, model: string} | undefined}
 */
function findConfiguration(lines) {
  const configurations = [];
  for (const [index, line] of lines.entries()) {
    const match = line.match(/^\[deepseek-harness\] configured provider=([a-z][a-z0-9-]*) model=(\S+)$/);
    if (match) configurations.push({ index, provider: match[1], model: match[2] });
  }
  return configurations.length === 1 ? configurations[0] : undefined;
}

/**
 * Only a completed stdout envelope attributes otherwise unlabeled text to the
 * assistant. Configuration and attributed failures remain usable independently.
 * @param {string[]} lines
 * @param {{index: number}} configuration
 * @returns {string | undefined}
 */
function extractHeadlessAnswer(lines, configuration) {
  const shutdown = lines.findIndex((line, index) => index > configuration.index && line === "[INFO] Stopping containers...");
  if (shutdown < 0) return;
  const completed = lines.findIndex((line, index) => index > shutdown && line === "[SUCCESS] Command completed successfully");
  const exit = lines.findIndex((line, index) => index > shutdown && /^Process exiting with code:/.test(line));
  if (completed < 0 || exit <= completed || lines[exit] !== "Process exiting with code: 0") return;

  let start = configuration.index + 1;
  while (start < shutdown && (lines[start].trim() === "" || lines[start] === "[entrypoint] Unsetting sensitive tokens from parent shell environment..." || TOKEN_CLEANUP.test(lines[start]))) {
    start++;
  }
  const answerLines = lines.slice(start, shutdown);
  while (answerLines.length && (answerLines[answerLines.length - 1].trim() === "" || PERMISSION_CLEANUP.test(answerLines[answerLines.length - 1]))) {
    answerLines.pop();
  }
  const answer = answerLines.join("\n");
  if (
    !answer ||
    answerLines.some(line => /^[ \t]*\[(?:entrypoint|health-check|INFO|WARN|ERROR|SUCCESS|deepseek-harness)\]/.test(line) && !HARNESS_FAILURE.test(line)) ||
    /^ (?:Container|Network|Volume) |^Process exiting with code:|^file:\/\/\/|^Node\.js v\d/m.test(answer) ||
    /^[ \t]*(?:(?:user|system)\s*(?:prompt|message)?\s*:|prompt\s*:)/im.test(answer)
  ) {
    return;
  }
  return answer;
}

/**
 * Node startup stacks require both the runtime framing and an attributed harness
 * failure. An error quoted in the assistant answer is not engine evidence.
 * @param {string[]} lines
 * @param {{index: number}} configuration
 * @returns {import("./types/agent_session").SessionEvent[]}
 */
function extractHeadlessErrors(lines, configuration) {
  const shutdown = lines.findIndex((line, index) => index > configuration.index && line === "[INFO] Stopping containers...");
  const body = lines.slice(configuration.index + 1, shutdown < 0 ? undefined : shutdown);
  if (body.some(line => /^[ \t]*(?:(?:user|system)\s*(?:prompt|message)?\s*:|prompt\s*:)/i.test(line))) return [];
  const failureIndex = body.findIndex(line => HARNESS_FAILURE.test(line));
  if (failureIndex < 0) return [];
  const exitCode = Number(body[failureIndex].match(HARNESS_FAILURE)?.[1]);
  if (shutdown >= 0) {
    const terminal = lines.slice(shutdown + 1);
    const exit = terminal.find(line => /^Process exiting with code:/.test(line));
    if (terminal.includes("[SUCCESS] Command completed successfully") || (exit !== undefined && exit !== `Process exiting with code: ${exitCode}`)) return [];
  }
  if (!body.slice(failureIndex + 1).every(line => line.trim() === "" || PERMISSION_CLEANUP.test(line))) return [];
  const start = body.findIndex(line => line.trim() !== "" && line !== "[entrypoint] Unsetting sensitive tokens from parent shell environment..." && !TOKEN_CLEANUP.test(line));
  const stackStart = body.findIndex(line => /^file:\/\/\/\S+:\d+(?::\d+)?$/.test(line));
  const nodeVersion = body.findIndex((line, index) => index > stackStart && /^Node\.js v\d+\.\d+\.\d+$/.test(line));
  const hasNodeStack = stackStart === start && stackStart >= 0 && nodeVersion > stackStart && nodeVersion < failureIndex && body.slice(nodeVersion + 1, failureIndex).every(line => line.trim() === "");
  if (failureIndex !== start && !hasNodeStack) return [];
  const entries = [];
  if (hasNodeStack) {
    for (let index = stackStart + 1; index < nodeVersion; index++) {
      const error = body[index].match(/^([A-Za-z]*Error): (.*)$/);
      if (!error || !/^    at /.test(body[index + 1] ?? "")) continue;
      const stack = body.slice(index, nodeVersion);
      while (stack.at(-1) === "") stack.pop();
      entries.push(createSessionEvent({}, "session.error", { message: error[2], errorType: error[1], stack: stack.join("\n") }));
      break;
    }
  }
  const message = body[failureIndex].slice("[deepseek-harness] ".length);
  entries.push(createSessionEvent({}, "session.error", { message, ...(exitCode <= 255 ? { exitCode } : {}) }));
  const execution = collectAgentExecution({ events: entries, ...(exitCode <= 255 ? { exitCode } : {}) });
  if (execution) entries.push(execution);
  return entries;
}

/** @param {string} content @returns {boolean} */
function isDeepSeekLog(content) {
  return findConfiguration(stripVTControlCharacters(content).split(/\r?\n/)) !== undefined;
}

/**
 * Preserve headless configuration, completed assistant text, and attributed
 * startup failures. Tools, reasoning, usage, and task success are not exposed by
 * this stdout profile. Canonical artifacts are handled by shared session readers,
 * not interpreted as an additional native dsh stdout format.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseDeepSeekLog(content) {
  const lines = applyAddMaskRedaction(stripVTControlCharacters(content), collectAddMaskedValues(content)).split(/\r?\n/);
  const configuration = findConfiguration(lines);
  /** @type {import("./types/agent_session").SessionEvent[]} */
  let logEntries = [];
  let notice = "";
  if (configuration) {
    logEntries.push(createSessionEvent({}, "session.init", { sourceEngine: "deepseek-harness", provider: configuration.provider, model: configuration.model }));
    const errors = extractHeadlessErrors(lines, configuration);
    const answer = errors.length ? undefined : extractHeadlessAnswer(lines, configuration);
    if (answer !== undefined) logEntries.push(createSessionEvent({}, "assistant.message", { content: answer }));
    else notice = "Headless assistant output is unavailable, incomplete, or ambiguous. Unattributed stdout is omitted; configuration and observed failures are retained.";
    logEntries.push(...errors);
  }
  return {
    markdown: logEntries.length
      ? generateCopilotCliStyleSummary(logEntries) + (notice ? `\n\n${buildStepSummaryDetailsSection("DeepSeek Harness", notice)}` : "")
      : buildStepSummaryDetailsSection("DeepSeek Harness", "Supported headless observations not recognized. Raw content is omitted because it may contain prompts or secrets."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

module.exports = { main, parseDeepSeekLog, isDeepSeekLog };
