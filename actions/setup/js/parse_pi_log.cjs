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
} = require("./log_parser_shared.cjs");

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

  /** @type {Array<any>} */
  const rawEntries = [];
  for (const line of logContent.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith("{")) {
      continue;
    }
    try {
      rawEntries.push(JSON.parse(trimmed));
    } catch (_e) {
      // Non-JSON line — ignored.
    }
  }

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

  const stats = useV3Schema ? computePiV3Stats(rawEntries) : legacyPiStats(rawEntries);

  const canonicalLogEntries = convertLegacyLogEntriesToCopilotEvents(logEntries, { sourceEngine: "pi" });
  const conversationResult = generateConversationMarkdown(canonicalLogEntries, {
    formatToolCallback: (toolUse, toolResult) => formatToolUse(toolUse, toolResult, { includeDetailedParameters: false }),
    formatInitCallback: initEntry => formatInitializationSummary(initEntry, { includeSlashCommands: false }),
  });

  let markdown = conversationResult.markdown;

  if (stats) {
    const syntheticEntry = {
      usage: {
        input_tokens: stats.input_tokens || 0,
        output_tokens: stats.output_tokens || 0,
      },
      duration_ms: stats.duration_ms || 0,
      num_turns: stats.turns || 0,
      errors: stats.errors && stats.errors.length > 0 ? stats.errors : undefined,
    };
    markdown += generateInformationSection(syntheticEntry);

    // Append a normalized result entry so log_parser_bootstrap.cjs can write it
    // to agent-stdio.log for OTEL gh-aw.turns / token-usage enrichment.
    canonicalLogEntries.push({
      type: "result",
      num_turns: syntheticEntry.num_turns,
      usage: syntheticEntry.usage,
      errors: syntheticEntry.errors,
    });
  } else {
    markdown += generateInformationSection(null);
  }

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
  /** @type {Array<any>} */
  const entries = [];

  for (const raw of rawEntries) {
    if (raw.type === "init") {
      entries.push({
        type: "system",
        subtype: "init",
        model: raw.model,
        session_id: raw.session_id,
      });
    } else if (raw.type === "assistant") {
      const text = raw.content || "";
      if (!text.trim()) {
        continue;
      }
      // Merge consecutive streaming delta chunks into one assistant text entry.
      const last = entries[entries.length - 1];
      if (raw.delta === true && isConsecutiveDeltaEntry(last)) {
        last.message.content[0].text += text;
      } else {
        entries.push({
          type: "assistant",
          message: {
            content: [{ type: "text", text }],
          },
        });
      }
    } else if (raw.type === "tool_use") {
      entries.push({
        type: "assistant",
        message: {
          content: [
            {
              type: "tool_use",
              id: raw.tool_id,
              name: normalizePiToolName(raw.tool_name),
              input: raw.parameters || {},
            },
          ],
        },
      });
    } else if (raw.type === "tool_result") {
      const output = typeof raw.output === "string" ? raw.output : JSON.stringify(raw.output || "");
      entries.push({
        type: "user",
        message: {
          content: [
            {
              type: "tool_result",
              tool_use_id: raw.tool_id,
              content: output,
              is_error: raw.status !== "success",
            },
          ],
        },
      });
    }
  }

  return entries;
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
      e.type === "message_update" ||
      (e.type === "session" && typeof e.version === "number")
    ) {
      return true;
    }
  }
  return false;
}

/**
 * Transforms raw Pi v3 streaming entries into the canonical logEntries format.
 *
 * The v3 schema streams a message in fragments (message_start/message_update/message_end)
 * and finalizes each turn with a `turn_end` event carrying the complete assistant message
 * (`content[]` of `{type:"text"}` and `{type:"toolCall", id, name, arguments}`). Tool
 * results arrive as `tool_execution_end` events keyed by `toolCallId`. We render from the
 * finalized `turn_end` messages (avoiding fragment duplication) and pair each tool call
 * with its result by id.
 *
 * Ordering matters: `generateConversationMarkdown` resolves a tool result's name from the
 * preceding tool_use, so each turn emits its assistant tool_use entries first, then the
 * matching tool_result entries — even though `tool_execution_end` precedes `turn_end` in
 * the raw stream.
 *
 * @param {Array<any>} rawEntries - Raw parsed JSONL entries
 * @returns {Array<any>} Canonical log entries for generateConversationMarkdown
 */
function transformPiV3Entries(rawEntries) {
  /** @type {Array<any>} */
  const entries = [];

  // Index tool execution results by their tool call id for pairing.
  /** @type {Map<string, any>} */
  const resultsById = new Map();
  for (const raw of rawEntries) {
    if (raw.type === "tool_execution_end" && raw.toolCallId) {
      resultsById.set(raw.toolCallId, raw);
    }
  }

  // Initialization entry from the session event, with the model taken from the first
  // finalized turn (v3 session events do not carry the model directly).
  const session = rawEntries.find(e => e.type === "session");
  const modeledTurn = rawEntries.find(e => e.type === "turn_end" && e.message && e.message.model);
  const model = modeledTurn ? modeledTurn.message.model : undefined;
  if (session || model) {
    entries.push({
      type: "system",
      subtype: "init",
      model: model,
      session_id: session ? session.id : undefined,
    });
  }

  /** @type {Set<string>} */
  const emittedResults = new Set();

  for (const raw of rawEntries) {
    if (raw.type !== "turn_end" || !raw.message || !Array.isArray(raw.message.content)) {
      continue;
    }

    /** @type {Array<string>} */
    const pendingToolIds = [];
    for (const part of raw.message.content) {
      if (!part || typeof part !== "object") {
        continue;
      }
      if (part.type === "text") {
        const text = typeof part.text === "string" ? part.text : "";
        if (!text.trim()) {
          continue;
        }
        entries.push({
          type: "assistant",
          message: { content: [{ type: "text", text }] },
        });
      } else if (part.type === "toolCall") {
        entries.push({
          type: "assistant",
          message: {
            content: [{ type: "tool_use", id: part.id, name: normalizePiToolName(part.name), input: part.arguments || {} }],
          },
        });
        if (part.id) {
          pendingToolIds.push(part.id);
        }
      }
    }

    // Emit tool results for this turn's calls, after their tool_use entries.
    for (const id of pendingToolIds) {
      if (emittedResults.has(id)) {
        continue;
      }
      const res = resultsById.get(id);
      if (!res) {
        continue;
      }
      emittedResults.add(id);
      entries.push({
        type: "user",
        message: {
          content: [
            {
              type: "tool_result",
              tool_use_id: id,
              content: extractPiV3ResultText(res.result),
              is_error: res.isError || isPiV3ResultError(res.result),
            },
          ],
        },
      });
    }
  }

  return entries;
}

/**
 * Extracts the textual output from a Pi v3 tool execution result.
 * @param {any} result - The `result` field of a tool_execution_end event
 * @returns {string} The concatenated text output
 */
function extractPiV3ResultText(result) {
  if (!result) {
    return "";
  }
  if (typeof result === "string") {
    return result;
  }
  if (Array.isArray(result.content)) {
    return result.content.map(c => (c && typeof c.text === "string" ? c.text : typeof c === "string" ? c : "")).join("");
  }
  if (typeof result.output === "string") {
    return result.output;
  }
  return JSON.stringify(result);
}

/**
 * Determines whether a Pi v3 tool execution result represents an error.
 * @param {any} result - The `result` field of a tool_execution_end event
 * @returns {boolean} True when the result indicates an error
 */
function isPiV3ResultError(result) {
  return !!(result && (result.isError === true || result.is_error === true || result.status === "error"));
}

/**
 * Computes aggregate stats from Pi v3 entries for the information section.
 *
 * Each `turn_end` carries a `usage` object; both output and input tokens are summed across
 * turns, matching the accumulation performed by the Pi driver. Turn count is the number of
 * finalized turns.
 *
 * @param {Array<any>} rawEntries - Raw parsed JSONL entries
 * @returns {{input_tokens:number, output_tokens:number, turns:number, duration_ms:number, errors:Array<string>}|null} Stats or null when unavailable
 */
function computePiV3Stats(rawEntries) {
  let outputTokens = 0;
  let inputTokens = 0;
  let turns = 0;
  let sawUsage = false;
  /** @type {Array<string>} */
  const errors = [];

  for (const raw of rawEntries) {
    if (raw.type !== "turn_end") {
      continue;
    }
    turns++;
    const usage = raw.message && raw.message.usage;
    if (usage && typeof usage === "object") {
      sawUsage = true;
      if (typeof usage.output === "number") {
        outputTokens += usage.output;
      }
      if (typeof usage.input === "number") {
        inputTokens += usage.input;
      }
    }
    // A turn can end with an empty content array and no toolCalls when the provider
    // request itself failed (e.g. a 400 model_not_supported response); the only trace
    // of the failure is this top-level errorMessage, which would otherwise render as a
    // silent, content-free turn in the step summary.
    const errorMessage = raw.message && typeof raw.message.errorMessage === "string" ? raw.message.errorMessage : undefined;
    if (errorMessage) {
      errors.push(errorMessage);
    }
  }

  if (turns === 0 && !sawUsage) {
    return null;
  }

  return {
    input_tokens: inputTokens,
    output_tokens: outputTokens,
    turns: turns,
    duration_ms: 0,
    errors: errors,
  };
}

/**
 * Extracts stats from a legacy Pi `result` event, preserving the original flat-schema behavior.
 * @param {Array<any>} rawEntries - Raw parsed JSONL entries
 * @returns {{input_tokens:number, output_tokens:number, turns:number, duration_ms:number, errors:Array<string>}|null} Stats or null when absent
 */
function legacyPiStats(rawEntries) {
  const resultEntry = rawEntries.find(e => e.type === "result");
  if (!resultEntry || !resultEntry.stats) {
    return null;
  }
  const stats = resultEntry.stats;
  return {
    input_tokens: stats.input_tokens || 0,
    output_tokens: stats.output_tokens || 0,
    turns: stats.turns || 0,
    duration_ms: stats.duration_ms || 0,
    errors: [],
  };
}

/**
 * Checks whether a canonical log entry is an assistant text entry eligible for merging
 * with a subsequent streaming delta chunk.
 * @param {any} entry - The candidate last entry
 * @returns {boolean} True when the entry is a mergeable assistant text entry
 */
function isConsecutiveDeltaEntry(entry) {
  return entry && entry.type === "assistant" && entry.message && Array.isArray(entry.message.content) && entry.message.content.length === 1 && entry.message.content[0].type === "text";
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
