// @ts-check

const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const { isSessionEvent, isTokenCount } = require("./agent_session.cjs");
const { ERR_PARSE, ERR_VALIDATION } = require("./error_codes.cjs");

/**
 * @param {any} trajectory
 * @returns {import("./types/agent_session").SessionEvent[]}
 */
function normalizeLangGraphSession(trajectory) {
  if (!trajectory || !Array.isArray(trajectory.messages) || typeof trajectory.exit_status !== "string") {
    throw new Error(`${ERR_VALIDATION}: Invalid LangGraph trajectory envelope`);
  }
  const events = [];
  const tools = new Map();
  let turns = 0;
  for (const [index, message] of trajectory.messages.entries()) {
    if (!message || typeof message.type !== "string" || !message.data || typeof message.data !== "object" || Array.isArray(message.data)) {
      throw new Error(`${ERR_VALIDATION}: Invalid LangGraph message at index ${index}`);
    }
    const data = message.data;
    const metadata = {
      ...(typeof data.id === "string" ? { id: data.id } : {}),
      ...(typeof message.timestamp === "string" || typeof message.timestamp === "number" ? { timestamp: message.timestamp } : {}),
      provenance: { messageIndex: index, sourceType: message.type },
    };
    /** @param {`${string}.${string}`} type @param {Record<string, any>} payload */
    const emit = (type, payload) => events.push({ ...metadata, type, data: { sourceEngine: "langgraph", ...payload } });
    if (message.type === "human" || message.type === "ai" || message.type === "system") {
      if (data.content !== undefined) {
        emit(message.type === "human" ? "user.message" : message.type === "ai" ? "assistant.message" : "session.info", {
          content: structuredClone(data.content),
          ...(typeof data.response_metadata?.model_name === "string" ? { model: data.response_metadata.model_name } : {}),
        });
      }
      if (message.type !== "ai") continue;
      turns++;
      if ((data.tool_calls !== undefined && !Array.isArray(data.tool_calls)) || (data.invalid_tool_calls !== undefined && !Array.isArray(data.invalid_tool_calls))) {
        throw new Error(`${ERR_VALIDATION}: Invalid LangGraph tool call list at message index ${index}`);
      }
      for (const call of data.tool_calls ?? []) {
        if (!call || typeof call.id !== "string" || typeof call.name !== "string") {
          throw new Error(`${ERR_VALIDATION}: Invalid LangGraph tool call at message index ${index}`);
        }
        tools.set(call.id, call.name);
        emit("tool.execution_start", { toolCallId: call.id, toolName: call.name, ...(call.args !== undefined ? { input: structuredClone(call.args) } : {}) });
      }
      for (const call of data.invalid_tool_calls ?? []) emit("langgraph.invalid_tool_call", { record: structuredClone(call) });
      if (data.usage_metadata != null) {
        if (typeof data.usage_metadata !== "object" || Array.isArray(data.usage_metadata)) throw new Error(`${ERR_VALIDATION}: Invalid LangGraph usage at message index ${index}`);
        /** @type {Record<string, number | boolean>} */
        const usage = {};
        for (const [key, value] of [
          ["input_tokens", data.usage_metadata.input_tokens],
          ["output_tokens", data.usage_metadata.output_tokens],
          ["total_tokens", data.usage_metadata.total_tokens],
          ["cache_read_input_tokens", data.usage_metadata.input_token_details?.cache_read],
          ["cache_creation_input_tokens", data.usage_metadata.input_token_details?.cache_creation],
          ["reasoning_output_tokens", data.usage_metadata.output_token_details?.reasoning],
        ]) {
          if (value === undefined) continue;
          if (!isTokenCount(value)) throw new Error(`${ERR_VALIDATION}: Invalid LangGraph token count at message index ${index}`);
          usage[key] = value;
        }
        // LangChain input_tokens includes cache reads and writes.
        if (Object.hasOwn(usage, "input_tokens")) usage.input_tokens_include_cache = true;
        if (Object.keys(usage).length) emit("usage.report", { usage, ...(typeof data.response_metadata?.model_name === "string" ? { model: data.response_metadata.model_name } : {}) });
      }
    } else if (message.type === "tool") {
      if (typeof data.tool_call_id !== "string") throw new Error(`${ERR_VALIDATION}: Invalid LangGraph tool result at message index ${index}`);
      emit("tool.execution_complete", {
        toolCallId: data.tool_call_id,
        ...(tools.has(data.tool_call_id) ? { toolName: tools.get(data.tool_call_id) } : {}),
        ...(data.content !== undefined ? { output: structuredClone(data.content) } : {}),
        ...(data.artifact !== undefined && data.artifact !== null ? { structuredContent: structuredClone(data.artifact) } : {}),
        ...(typeof data.status === "string" ? { status: data.status } : {}),
        // The sample returns shell errors as text inside successful ToolMessages.
        ...(data.status === "error" ? { success: false } : {}),
      });
    } else {
      emit("langgraph.message", { record: structuredClone(message) });
    }
  }
  events.push({ type: "session.result", data: { sourceEngine: "langgraph", status: trajectory.exit_status, numTurns: turns } });
  return events;
}

/**
 * Parse the native JSON file or the final snapshot emitted to stdio. Do not
 * interpret JSON embedded inside message content as separate session events.
 * @param {string} content
 * @returns {{logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseLangGraphLog(content) {
  let trajectory;
  let whole;
  try {
    whole = JSON.parse(content);
  } catch {
    // Stdio also contains harness diagnostics and the final plaintext answer.
  }
  if (whole && (Object.hasOwn(whole, "messages") || Object.hasOwn(whole, "exit_status"))) trajectory = whole;
  const records = [];
  if (!trajectory) {
    for (const entry of iterateJsonlEvents(content)) {
      if (entry.type !== "json") continue;
      if (entry.value && (Object.hasOwn(entry.value, "messages") || Object.hasOwn(entry.value, "exit_status"))) trajectory = entry.value;
      else if (isSessionEvent(entry.value)) records.push(entry.value);
    }
  }
  if (!trajectory && content.trimStart().startsWith("{") && /"(?:messages|exit_status)"\s*:/.test(content)) {
    throw new Error(`${ERR_PARSE}: Malformed LangGraph trajectory JSON`);
  }
  return {
    logEntries: trajectory ? normalizeLangGraphSession(trajectory) : records,
    mcpFailures: [],
    maxTurnsHit: trajectory?.exit_status === "LimitsExceeded",
  };
}

module.exports = { normalizeLangGraphSession, parseLangGraphLog };
