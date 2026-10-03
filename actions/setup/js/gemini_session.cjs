// @ts-check

const { createSessionEvent, isSessionEvent, normalizeAgentSession, normalizeSessionUsage, isTokenCount, isMetric, sessionToolSuccess } = require("./agent_session.cjs");

/** @typedef {{signature: string, event: import("./types/agent_session").SessionEvent}} GeminiDelta */

/** @param {any} source @returns {Record<string, any>} */
function sourceFields(source) {
  const { type, data, ...fields } = source;
  return { ...fields, ...data };
}

/** @param {any} source @param {string[]} keys @param {any} [fallback] @returns {any} */
function supplied(source, keys, fallback = undefined) {
  for (const key of keys) {
    if (Object.hasOwn(source, key)) return source[key];
  }
  return fallback;
}

/** @param {any} source @returns {boolean|undefined} */
function toolSuccess(source) {
  if (sessionToolSuccess({ ...source, exitCode: supplied(source, ["exitCode", "exit_code"]) }) === false || source.output?.isError === true || source.output?.is_error === true || ["denied", "permission_denied"].includes(source.status))
    return false;
  return source.status === "success" || source.is_error === false || source.isError === false ? true : typeof source.success === "boolean" ? source.success : undefined;
}

/** @param {any} source @returns {Record<string, any>|undefined} */
function geminiUsage(source) {
  const usage = { ...normalizeSessionUsage(source.usage) };
  const stats = source.stats;
  const aliases = { input_tokens: "inputTokens", output_tokens: "outputTokens", cache_creation_input_tokens: "cacheCreationInputTokens", cache_read_input_tokens: "cacheReadInputTokens" };
  for (const [key, alias] of Object.entries(aliases)) {
    if (source.usage && Object.hasOwn(source.usage, key) && !isTokenCount(source.usage[key])) delete usage[alias];
  }
  const mapCount = (key, value) => {
    if (isTokenCount(value)) {
      usage[key] = value;
      if (Array.isArray(usage.overflowed_tokens)) usage.overflowed_tokens = usage.overflowed_tokens.filter(field => field !== key);
    } else {
      delete usage[key];
      if (aliases[key]) delete usage[aliases[key]];
    }
  };
  if (stats && typeof stats === "object" && !Array.isArray(stats)) {
    for (const key of ["input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "total_tokens"]) {
      if (!Object.hasOwn(stats, key)) continue;
      mapCount(key, stats[key]);
    }
    if (!Object.hasOwn(stats, "cache_read_input_tokens") && Object.hasOwn(stats, "cached")) {
      mapCount("cache_read_input_tokens", stats.cached);
    }
    // Gemini StreamStats.cached is a subtotal of input_tokens, not additional input.
    if (isTokenCount(stats.cached)) usage.input_tokens_include_cache = true;
  }
  if (!isTokenCount(usage.total_tokens)) delete usage.total_tokens;
  if (usage.overflowed_tokens?.length === 0) delete usage.overflowed_tokens;
  return Object.keys(usage).length ? usage : undefined;
}

/**
 * Normalize Gemini's flat stream and compatible legacy records independently.
 * Results are cumulative snapshots; tools and message fragments are observations,
 * not evidence of finalized turns or terminal success.
 * @param {Array<any>} records
 * @returns {import("./types/agent_session").AgentSession}
 */
function normalizeGeminiSession(records) {
  /** @type {import("./types/agent_session").AgentSession} */
  const events = [];
  if (!Array.isArray(records)) return events;
  const tools = new Map();
  const messages = new Map();
  let session = 0;
  let sessionId;
  /** @type {GeminiDelta|undefined} */
  let previousDelta;
  let snapshotEmitted = false;
  const correlationKey = (source, id) => JSON.stringify([session, supplied(source, ["sessionId", "session_id"], supplied(source.data ?? {}, ["sessionId", "session_id"], sessionId)), source.parent_tool_use_id, id]);
  const emit = (source, type, data) => {
    const event = createSessionEvent(source, type, { ...sourceFields(source), ...data });
    events.push(event);
    return event;
  };
  const reportError = source => {
    const errors = Array.isArray(source.errors) ? [...source.errors] : [];
    if (source.error != null) errors.push(source.error);
    if (!errors.length) {
      const diagnostic = {};
      for (const key of ["status", "severity", "message", "content", "is_error", "isError", "code"]) {
        if (Object.hasOwn(source, key)) diagnostic[key] = source[key];
      }
      errors.push(Object.keys(diagnostic).length ? diagnostic : sourceFields(source));
    }
    return emit(source, "session.result", { errors, permissionDenials: supplied(source, ["permissionDenials", "permission_denials"]) });
  };
  /**
   * @param {any} source
   * @param {"user"|"assistant"|"reasoning"} role
   * @param {string} text
   * @param {Record<string, any>} [fields]
   * @param {number} [position]
   * @param {GeminiDelta} [lastDelta]
   */
  const emitMessage = (source, role, text, fields = {}, position = 0, lastDelta = undefined) => {
    const type = role === "user" ? "user.message" : role === "reasoning" ? "assistant.reasoning" : "assistant.message";
    // Flat stream-json has no message identity. Only an explicit message ID
    // establishes that a later full message covers previously observed deltas.
    const id = supplied(source, ["messageId", "message_id"], source.message?.id);
    const key = id === undefined ? undefined : JSON.stringify([correlationKey(source, id), role, position]);
    const observed = key === undefined ? undefined : messages.get(key);
    if (source.delta !== true && observed !== undefined) {
      if (!snapshotEmitted) emit(source, "gemini.message_snapshot", sourceFields(source));
      snapshotEmitted = true;
      if (text.startsWith(observed) && text !== observed) emit(source, type, { ...fields, content: text.slice(observed.length) });
      if (text.startsWith(observed)) messages.set(key, text);
      return;
    }
    if (key !== undefined) messages.set(key, source.delta === true ? (observed ?? "") + text : text);
    const { content: ignored, ...metadata } = source;
    const signature = JSON.stringify(metadata);
    // Coalesce only indistinguishable adjacent envelopes; differing timestamps,
    // IDs, channels, or native additions retain their own ordered fragments.
    if (source.delta === true && lastDelta?.signature === signature && lastDelta.event.type === type && typeof lastDelta.event.data.content === "string") {
      lastDelta.event.data.content += text;
      previousDelta = lastDelta;
    } else {
      const event = emit(source, type, { ...fields, content: text });
      if (source.delta === true) previousDelta = { signature, event };
    }
  };
  const emitBlock = (source, block, position) => {
    if (!block || typeof block !== "object" || Array.isArray(block)) return;
    if (block.type === "text" && typeof block.text === "string") {
      emitMessage(source, source.type === "user" || source.role === "user" ? "user" : "assistant", block.text, block, position);
    } else if (block.type === "thinking" && typeof block.thinking === "string") {
      emitMessage(source, "reasoning", block.thinking, block, position);
    } else if (block.type === "tool_use") {
      const event = emit(source, "tool.execution_start", { ...block, toolCallId: block.id, toolName: block.name, input: block.input });
      if (block.id !== undefined) tools.set(correlationKey(source, block.id), event.data);
    } else if (block.type === "tool_result") {
      const start = block.tool_use_id === undefined ? undefined : tools.get(correlationKey(source, block.tool_use_id));
      emit(source, "tool.execution_complete", {
        ...block,
        toolCallId: block.tool_use_id,
        toolName: supplied(block, ["toolName", "name"], start?.toolName),
        mcpServerName: supplied(block, ["mcpServerName", "server"], start?.mcpServerName),
        success: toolSuccess(block),
        output: supplied(block, ["output", "content", "result"]),
        exitCode: supplied(block, ["exitCode", "exit_code"]),
        durationMs: isMetric(supplied(block, ["durationMs", "duration_ms"])) ? supplied(block, ["durationMs", "duration_ms"]) : undefined,
      });
    } else {
      emit(source, "gemini.content_block", block);
    }
  };

  for (const record of records) {
    const lastDelta = previousDelta;
    previousDelta = undefined;
    snapshotEmitted = false;
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    if (isSessionEvent(record)) {
      const event = structuredClone(record);
      events.push(event);
      if (event.type === "session.init" || event.type === "session.start") {
        session++;
        sessionId = event.data.sessionId;
      } else if (event.type === "tool.execution_start" && event.data.toolCallId !== undefined) {
        tools.set(correlationKey(event, event.data.toolCallId), event.data);
      }
      continue;
    }
    if (record.type === "init" || (record.type === "system" && record.subtype === "init")) {
      session++;
      sessionId = supplied(record, ["sessionId", "session_id"]);
      emit(record, "session.init", {
        sourceEngine: "gemini",
        sessionId,
        model: record.model,
        cwd: record.cwd,
        tools: record.tools,
        mcpServers: supplied(record, ["mcpServers", "mcp_servers"]),
        slashCommands: supplied(record, ["slashCommands", "slash_commands"]),
        modelInfo: supplied(record, ["modelInfo", "model_info"]),
      });
    } else if (["message", "assistant", "user", "reasoning"].includes(record.type)) {
      const role = record.type === "message" ? record.role : record.type;
      if (!["assistant", "user", "reasoning"].includes(role)) continue;
      if (role !== "user" && (record.error != null || record.is_error === true || record.isError === true || ["error", "failed"].includes(record.status))) {
        emit(record, "gemini.message_error", sourceFields(record));
        reportError(record);
        continue;
      }
      const content = record.message === undefined ? record.content : typeof record.message === "string" ? record.message : record.message?.content;
      if (Array.isArray(content)) {
        for (const [position, block] of content.entries()) emitBlock(record, block, position);
        continue;
      }
      const text = record.type === "reasoning" ? (content ?? record.data?.content) : content;
      if (typeof text !== "string") continue;
      emitMessage(record, role, text, {}, 0, lastDelta);
    } else if (record.type === "tool_use") {
      const id = supplied(record, ["toolCallId", "tool_id"]);
      const event = emit(record, "tool.execution_start", {
        toolCallId: id,
        toolName: supplied(record, ["toolName", "tool_name"]),
        input: supplied(record, ["input", "parameters"]),
        command: record.command,
        mcpServerName: supplied(record, ["mcpServerName", "server"]),
      });
      if (id !== undefined) tools.set(correlationKey(record, id), event.data);
    } else if (record.type === "tool_result") {
      const id = supplied(record, ["toolCallId", "tool_id"]);
      const start = id === undefined ? undefined : tools.get(correlationKey(record, id));
      emit(record, "tool.execution_complete", {
        toolCallId: id,
        toolName: supplied(record, ["toolName", "tool_name"], start?.toolName),
        mcpServerName: supplied(record, ["mcpServerName", "server"], start?.mcpServerName),
        success: toolSuccess(record),
        output: supplied(record, ["output", "result"]),
        exitCode: supplied(record, ["exitCode", "exit_code"]),
        durationMs: isMetric(supplied(record, ["durationMs", "duration_ms"])) ? supplied(record, ["durationMs", "duration_ms"]) : undefined,
      });
    } else if (record.type === "result") {
      const metric = keys => (keys.some(key => Object.hasOwn(record.stats ?? {}, key)) ? supplied(record.stats, keys) : supplied(record, keys));
      const errors = Array.isArray(record.errors) ? [...record.errors] : undefined;
      const event = emit(record, "session.result", {
        numTurns: isTokenCount(metric(["numTurns", "turns", "num_turns"])) ? metric(["numTurns", "turns", "num_turns"]) : undefined,
        durationMs: isMetric(metric(["durationMs", "duration_ms"])) ? metric(["durationMs", "duration_ms"]) : undefined,
        totalCostUsd: isMetric(metric(["totalCostUsd", "total_cost_usd"])) ? metric(["totalCostUsd", "total_cost_usd"]) : undefined,
        usage: geminiUsage(record),
        errors: record.error != null ? [...(errors ?? []), record.error] : errors,
        permissionDenials: supplied(record, ["permissionDenials", "permission_denials"]),
      });
      if ((!event.data.errors || event.data.errors.length === 0) && (["error", "failed", "denied", "permission_denied"].includes(record.status) || record.is_error === true || record.isError === true || record.success === false)) {
        const diagnostic = {};
        for (const key of ["status", "is_error", "isError", "success", "message", "code"]) {
          if (Object.hasOwn(record, key)) diagnostic[key] = record[key];
        }
        event.data.errors = [diagnostic];
      }
    } else if (record.type === "error" && (Object.hasOwn(record, "error") || typeof record.message === "string")) {
      emit(record, "gemini.error", sourceFields(record));
      if (record.severity !== "warning") reportError(record);
    } else if (record.type === "system" && record.subtype) {
      emit(record, "gemini.system", sourceFields(record));
      if (record.error != null) reportError(record);
      else for (const mapped of normalizeAgentSession([record], { sourceEngine: "gemini" })) emit(record, mapped.type, mapped.data);
    }
  }
  return events;
}

module.exports = { normalizeGeminiSession };
