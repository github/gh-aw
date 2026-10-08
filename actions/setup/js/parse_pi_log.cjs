// @ts-check
/// <reference types="@actions/github-script" />

const {
  createEngineLogParser,
  generateConversationMarkdown,
  generateInformationSection,
  buildStepSummaryDetailsSection,
  formatInitializationSummary,
  formatToolUse,
  convertLegacyLogEntriesToCopilotEvents,
  parseLogEntries,
} = require("./log_parser_shared.cjs");
const { transformFlatSessionEntries, projectSessionResult, selectSessionResult, reconcileSessionUsage } = require("./agent_session.cjs");
const { transformPiV3Entries, computePiV3Stats } = require("./pi_session.cjs");

const main = createEngineLogParser({
  parserName: "Pi",
  parseFunction: parsePiLog,
  supportsDirectories: false,
});

/**
 * Parse Pi CLI JSONL streaming log output and format as markdown.
 * Pi CLI emits one JSON object per line (JSONL) with typed events:
 * - type "init":        session initialization with model and session_id
 * - type "assistant":   agent message content (delta:true for streaming chunks)
 * - type "tool_use":    tool invocations with tool_name, tool_id, and parameters
 * - type "tool_result": tool responses with tool_id, status, and output
 * - type "result":      final stats with token usage and duration
 * @param {string} logContent - The raw log content to parse
 * @returns {{markdown: string, logEntries: Array, mcpFailures: Array<string>, maxTurnsHit: boolean}} Parsed log data
 */
function parsePiLog(logContent) {
  if (!logContent) {
    return {
      markdown: buildStepSummaryDetailsSection("Pi", "No log content provided."),
      logEntries: [],
      mcpFailures: [],
      maxTurnsHit: false,
    };
  }

  const rawEntries = (parseLogEntries(logContent) ?? []).filter(e => e && typeof e === "object" && !Array.isArray(e));

  if (rawEntries.length === 0) {
    return {
      markdown: buildStepSummaryDetailsSection("Pi", "Log format not recognized as Pi JSONL."),
      logEntries: [],
      mcpFailures: [],
      maxTurnsHit: false,
    };
  }

  // Pi CLI's `--mode json` output schema changed over time. Current builds emit a
  // v3 streaming schema (session, turn_start, turn_end, tool_execution_start/end, agent_end)
  // while older builds emitted a flat init/assistant/tool_use/tool_result/result schema.
  // Detect which schema this log uses and transform accordingly so the step summary
  // renders the conversation and token stats for both.
  const useV3Schema = isPiV3Schema(rawEntries);
  const logEntries = useV3Schema ? transformPiV3Entries(rawEntries) : transformPiEntries(rawEntries);

  const stats = useV3Schema ? computePiV3Stats(rawEntries) : null;
  const canonicalLogEntries = convertLegacyLogEntriesToCopilotEvents(logEntries, { sourceEngine: "pi" });
  if (stats) {
    const terminal = [...canonicalLogEntries].reverse().find(event => event.type === "session.result");
    const selected = selectSessionResult(canonicalLogEntries);
    const data = {
      numTurns: stats.turns ?? selected?.numTurns,
      usage: reconcileSessionUsage(selected?.usage, stats.usage),
      totalCostUsd: stats.total_cost_usd ?? selected?.totalCostUsd,
      durationMs: stats.duration_ms ?? selected?.durationMs,
    };
    if (terminal) {
      Object.assign(terminal.data, data);
      if (stats.errors.length || stats.exposedErrors) terminal.data.errors = [...(terminal.data.errors ?? []), ...stats.errors];
    } else {
      canonicalLogEntries.push({ type: "session.result", data: { ...data, errors: stats.errors.length || stats.exposedErrors ? stats.errors : undefined } });
    }
  }
  if (canonicalLogEntries.length === 0) {
    return { markdown: buildStepSummaryDetailsSection("Pi", "Log format not recognized as Pi JSONL."), logEntries: [], mcpFailures: [], maxTurnsHit: false };
  }
  const conversationResult = generateConversationMarkdown(canonicalLogEntries, {
    includeInformation: false,
    formatToolCallback: (toolUse, toolResult) => formatToolUse(toolUse, toolResult, { includeDetailedParameters: false }),
    formatInitCallback: initEntry => formatInitializationSummary(initEntry, { includeSlashCommands: false }),
  });

  let markdown = conversationResult.markdown;

  markdown += generateInformationSection(projectSessionResult(canonicalLogEntries));

  return {
    markdown,
    logEntries: canonicalLogEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

/**
 * Transforms raw Pi JSONL entries into the canonical logEntries format
 * used by the shared generateConversationMarkdown function.
 *
 * Pi entry types and their canonical mappings:
 * - "init"        → {type:"system", subtype:"init", model, session_id}
 * - "assistant"   → merged into {type:"assistant", message:{content:[{type:"text"}]}}
 * - "tool_use"    → {type:"assistant", message:{content:[{type:"tool_use", id, name, input}]}}
 * - "tool_result" → {type:"user",      message:{content:[{type:"tool_result", tool_use_id, content, is_error}]}}
 *
 * @param {Array<any>} rawEntries - Raw parsed JSONL entries
 * @returns {Array<any>} Canonical log entries for generateConversationMarkdown
 */
function transformPiEntries(rawEntries) {
  return transformFlatSessionEntries(rawEntries);
}

/**
 * Detects whether the raw Pi entries use the v3 streaming schema.
 *
 * The v3 schema emits envelope events (session, turn_end, tool_execution_start/end, agent_end)
 * that the legacy flat schema (init/assistant/tool_use/tool_result/result) never uses.
 * A single marker event is enough to distinguish the two.
 *
 * @param {Array<any>} rawEntries - Raw parsed JSONL entries
 * @returns {boolean} True when the log uses the v3 streaming schema
 */
function isPiV3Schema(rawEntries) {
  for (const e of rawEntries) {
    if (!e || typeof e.type !== "string") {
      continue;
    }
    if (
      e.type === "turn_end" ||
      e.type === "turn_start" ||
      e.type === "agent_end" ||
      e.type === "agent_start" ||
      e.type === "tool_execution_start" ||
      e.type === "tool_execution_end" ||
      e.type === "tool_execution_update" ||
      e.type === "message_update" ||
      e.type === "message_start" ||
      e.type === "message_end" ||
      (e.type === "session" && typeof e.version === "number")
    ) {
      return true;
    }
  }
  return false;
}

/**
 * Normalizes Pi tool call names to the canonical names expected by the shared
 * formatters (e.g. log_parser_format.cjs special-cases "Bash", not "bash").
 * @param {string} name - Raw tool name as emitted by Pi
 * @returns {string} Normalized tool name
 */
function normalizePiToolName(name) {
  return typeof name === "string" && name.trim().toLowerCase() === "bash" ? "Bash" : name;
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    parsePiLog,
    transformPiEntries,
    isPiV3Schema,
    transformPiV3Entries,
    computePiV3Stats,
    normalizePiToolName,
  };
}
