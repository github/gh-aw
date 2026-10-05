// @ts-check

const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent, isSessionEvent, isTokenCount, isMetric } = require("./agent_session.cjs");

const main = createEngineLogParser({ parserName: "Goose", parseFunction: parseGooseLog, supportsDirectories: false });

/** @param {any} event @returns {boolean} */
function isGooseEvent(event) {
  if (!event || typeof event !== "object" || Array.isArray(event)) return false;
  if (event.type === "message") return typeof event.message?.id === "string" && ["assistant", "user"].includes(event.message.role) && Array.isArray(event.message.content);
  if (event.type === "error") return typeof event.error === "string";
  return event.type === "complete" && ["input_tokens", "output_tokens", "total_tokens"].some(key => Object.hasOwn(event, key));
}

/** @param {any} event @returns {Record<string, any>} */
function sourceMetadata(event) {
  const source = { ...event };
  if (typeof event.message?.id === "string") source.id = event.message.id;
  // Goose's Message.created is Unix seconds, not milliseconds.
  const seconds = event.message?.created;
  if (typeof seconds === "number" && Number.isFinite(seconds) && Math.abs(seconds * 1000) <= 8640000000000000) source.timestamp = new Date(seconds * 1000).toISOString();
  return source;
}

/**
 * Parse Goose run --output-format stream-json into canonical session events.
 * Text records are streaming deltas; complete usage is one cumulative snapshot.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseGooseLog(content) {
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const logEntries = [];
  const messages = new Map();
  const toolStarts = new Map();
  const toolCompletions = new Set();
  const turns = new Set();
  const mcpFailures = new Set();
  let initialized = false;
  let maxTurnsHit = false;
  let resultSource;
  let resultData;

  for (const [index, line] of content.split(/\r?\n/).entries()) {
    if (!line.trim().startsWith("{")) {
      const failure = line.match(/^(?:Warning:|[ \t]*⚠)\s*Failed to start extension '([^']+)'/);
      if (failure && !mcpFailures.has(failure[1])) {
        mcpFailures.add(failure[1]);
        logEntries.push({ type: "goose.mcp_failure", data: { serverName: failure[1] } });
      }
      continue;
    }
    let raw;
    try {
      raw = JSON.parse(line);
    } catch {
      logEntries.push({ type: "session.collection_warning", data: { code: "malformed_jsonl", line: index + 1 } });
      continue;
    }
    if (isSessionEvent(raw)) {
      logEntries.push(structuredClone(raw));
      if (raw.type === "session.init") initialized = true;
      if (raw.type === "goose.max_turns") maxTurnsHit = true;
      if (raw.type === "goose.mcp_failure" && typeof raw.data.serverName === "string") mcpFailures.add(raw.data.serverName);
      continue;
    }
    if (!isGooseEvent(raw)) continue;
    const source = sourceMetadata(raw);
    const emit = (type, data) => logEntries.push(createSessionEvent(source, type, data));
    if (raw.type === "message") {
      if (!initialized) {
        emit("session.init", { sourceEngine: "goose", ...(raw.message.metadata?.inference?.requestedModel ? { model: raw.message.metadata.inference.requestedModel } : {}) });
        initialized = true;
      }
      if (raw.message.role === "assistant") turns.add(raw.message.id);
      for (const item of raw.message.content) {
        if (!item || typeof item !== "object" || Array.isArray(item)) {
          emit("session.collection_warning", { code: "malformed_message_content" });
          continue;
        }
        if (item.type === "text" || item.type === "thinking") {
          const text = item.type === "text" ? item.text : item.thinking;
          if (typeof text !== "string") continue;
          const type = item.type === "thinking" ? "assistant.reasoning" : raw.message.role === "assistant" ? "assistant.message" : "user.message";
          const key = JSON.stringify([type, raw.message.id]);
          const previous = messages.get(key);
          if (previous) previous.data.content += text;
          else {
            const event = createSessionEvent(source, type, { content: text });
            messages.set(key, event);
            logEntries.push(event);
          }
        } else if (item.type === "toolRequest" && typeof item.id === "string" && !toolStarts.has(item.id)) {
          const call = item.toolCall;
          if (call?.status !== "error" && (call?.status !== "success" || typeof call.value?.name !== "string")) {
            emit("session.collection_warning", { code: "malformed_tool_request", toolCallId: item.id });
            continue;
          }
          const data = { toolCallId: item.id, ...(call?.status === "success" ? { toolName: call.value.name, input: call.value.arguments ?? {} } : {}) };
          toolStarts.set(item.id, data);
          emit("tool.execution_start", data);
          if (call?.status === "error") emit("session.error", { error: call.error, toolCallId: item.id });
        } else if (item.type === "toolResponse" && typeof item.id === "string" && !toolCompletions.has(item.id)) {
          const result = item.toolResult;
          const value = result?.value;
          const validSuccess =
            result?.status === "success" && value && typeof value === "object" && !Array.isArray(value) && (value.content === undefined || Array.isArray(value.content)) && (value.isError === undefined || typeof value.isError === "boolean");
          const validError = result?.status === "error" && typeof result.error === "string";
          if (!validSuccess && !validError) {
            emit("session.collection_warning", { code: "malformed_tool_response", toolCallId: item.id });
            continue;
          }
          const success = result?.status === "success" && result.value?.isError !== true;
          emit("tool.execution_complete", {
            toolCallId: item.id,
            toolName: toolStarts.get(item.id)?.toolName,
            success,
            ...(value?.content !== undefined ? { output: value.content } : value?.structuredContent !== undefined ? { output: value.structuredContent } : {}),
            ...(result?.error !== undefined ? { error: result.error } : {}),
          });
          toolCompletions.add(item.id);
        } else if (item.type === "error") emit("session.error", { error: item.message });
      }
    } else if (raw.type === "complete") {
      const usage = {};
      for (const key of ["input_tokens", "output_tokens", "total_tokens", "cache_read_input_tokens"]) {
        if (isTokenCount(raw[key])) usage[key] = raw[key];
        else if (raw[key] != null) emit("session.collection_warning", { code: "invalid_usage", field: key });
      }
      if (isTokenCount(raw.cache_write_input_tokens)) usage.cache_creation_input_tokens = raw.cache_write_input_tokens;
      if (Object.keys(usage).length) usage.input_tokens_include_cache = true;
      resultSource = source;
      resultData = {
        status: "completed",
        ...(turns.size ? { numTurns: turns.size } : {}),
        ...(Object.keys(usage).length ? { usage } : {}),
        ...(isMetric(raw.cost_usd) ? { totalCostUsd: raw.cost_usd } : {}),
      };
    } else if (raw.type === "error") {
      emit("session.error", { error: raw.error });
      if (/maximum.*turns|turn limit|max.?turns/i.test(raw.error)) {
        maxTurnsHit = true;
        emit("goose.max_turns", { error: raw.error });
      }
      resultSource = source;
      resultData = { status: "error", errors: [raw.error], ...(turns.size ? { numTurns: turns.size } : {}) };
    }
  }
  if (resultSource) logEntries.push(createSessionEvent(resultSource, "session.result", resultData));
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("Goose", "Log format not recognized as Goose stream JSON. Raw content is omitted."),
    logEntries,
    mcpFailures: [...mcpFailures],
    maxTurnsHit,
  };
}

module.exports = { main, parseGooseLog, isGooseEvent };
