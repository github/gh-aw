// @ts-check

const { createSessionEvent, isSessionEvent, normalizeSessionUsage, isTokenCount, isMetric, sessionToolSuccess } = require("./agent_session.cjs");
const { getMessageRefusal } = require("./provider_refusal.cjs");

/** @typedef {{event: import("./types/agent_session").SessionEvent, observations: Array<any>}} GeminiFragment */
/** @typedef {GeminiFragment & {signature: string}} GeminiDelta */
/** @typedef {{text: string, fragments: GeminiFragment[], scope: string|undefined, position: number}} GeminiMessage */

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
  const aliases = {
    input_tokens: "inputTokens",
    output_tokens: "outputTokens",
    reasoning_output_tokens: "reasoningOutputTokens",
    cache_creation_input_tokens: "cacheCreationInputTokens",
    cache_read_input_tokens: "cacheReadInputTokens",
    total_tokens: "totalTokens",
  };
  for (const [key, alias] of Object.entries(aliases)) {
    if (source.usage && Object.hasOwn(source.usage, key) && !isTokenCount(source.usage[key])) delete usage[alias];
  }
  const mapCount = (key, value) => {
    if (aliases[key]) delete usage[aliases[key]];
    if (isTokenCount(value)) {
      usage[key] = value;
      if (Array.isArray(usage.overflowed_tokens)) usage.overflowed_tokens = usage.overflowed_tokens.filter(field => field !== key);
    } else {
      delete usage[key];
    }
  };
  if (stats && typeof stats === "object" && !Array.isArray(stats)) {
    for (const key of Object.keys(aliases)) {
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
  /** @type {Map<string, GeminiMessage>} */
  const messages = new Map();
  const emptyMessages = new Map();
  let session = 0;
  let sessionId;
  /** @type {GeminiDelta|undefined} */
  let previousDelta;
  /** @type {Array<any>} */
  let retiredObservations = [];
  const correlationKey = (source, id) => JSON.stringify([session, supplied(source, ["sessionId", "session_id"], supplied(source.data ?? {}, ["sessionId", "session_id"], sessionId)), source.parent_tool_use_id, id]);
  const messageRole = source => (source.type === "message" ? source.role : source.type);
  const messageScope = source => {
    const id = supplied(source, ["messageId", "message_id"], source.message?.id);
    return id === undefined ? undefined : JSON.stringify([correlationKey(source, id), source.channel, source.type === "user" || source.role === "user"]);
  };
  const messageKey = (scope, role, position) => JSON.stringify([scope, role, position]);
  const emit = (source, type, data) => {
    const event = createSessionEvent(source, type, { ...sourceFields(source), ...data, ...(retiredObservations.length ? { observations: [...retiredObservations, structuredClone(source)] } : {}) });
    retiredObservations = [];
    events.push(event);
    return event;
  };
  /** @param {GeminiMessage} message */
  const retireMessage = message => {
    for (const fragment of message.fragments) {
      retiredObservations.push(...fragment.observations);
      events.splice(events.indexOf(fragment.event), 1);
    }
    message.fragments = [];
  };
  const reconcileContentBlocks = (source, content) => {
    const scope = messageScope(source);
    if (scope === undefined || source.delta === true) return;
    const observed = [...messages].filter(([, message]) => message.scope === scope);
    if (!observed.length) return;
    /** @type {Map<string, {text: string, position: number}>} */
    const suppliedBlocks = new Map();
    for (const [position, block] of content.entries()) {
      if (block?.type === "text" && typeof block.text === "string") suppliedBlocks.set(messageKey(scope, messageRole(source), position), { text: block.text, position });
      else if (block?.type === "thinking" && typeof block.thinking === "string") suppliedBlocks.set(messageKey(scope, "reasoning", position), { text: block.thinking, position });
    }
    const lastPosition = observed.reduce((last, [, message]) => Math.max(last, message.position), -Infinity);
    const divergent =
      observed.some(([key, message]) => {
        const block = suppliedBlocks.get(key);
        return !block || !block.text.startsWith(message.text) || (block.text !== message.text && block.position < lastPosition);
      }) || [...suppliedBlocks].some(([key, block]) => !messages.has(key) && block.position < lastPosition);
    if (!divergent) return;
    // A full array's rewrite, removal, or insertion before a sibling replaces
    // the whole visible message, so unchanged siblings keep their source order.
    for (const [key, message] of observed) {
      retireMessage(message);
      messages.delete(key);
    }
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
    return emit(source, "session.result", { sourceType: source.type, errors, permissionDenials: supplied(source, ["permissionDenials", "permission_denials"]) });
  };
  /**
   * @param {any} source
   * @param {"user"|"assistant"|"reasoning"|"refusal"} role
   * @param {string} text
   * @param {Record<string, any>} [fields]
   * @param {number} [position]
   * @param {GeminiDelta} [lastDelta]
   */
  const emitMessage = (source, role, text, fields = {}, position = 0, lastDelta = undefined) => {
    const type = role === "user" ? "user.message" : role === "reasoning" ? "assistant.reasoning" : role === "refusal" ? "assistant.refusal" : "assistant.message";
    // Flat stream-json has no message identity. Only an explicit message ID
    // establishes that a later full message covers previously observed deltas.
    const scope = messageScope(source);
    const emptyKey = scope === undefined ? undefined : messageKey(scope, role, -1);
    const empty = emptyKey === undefined ? undefined : emptyMessages.get(emptyKey);
    if (empty) {
      retiredObservations.push(...empty.data.observations);
      events.splice(events.indexOf(empty), 1);
      emptyMessages.delete(emptyKey);
    }
    const key = scope === undefined ? undefined : messageKey(scope, role, position);
    const observed = key === undefined ? undefined : messages.get(key);
    if (source.delta !== true && observed !== undefined) {
      if (text === observed.text) {
        const fragment = observed.fragments.at(-1);
        if (fragment) {
          fragment.observations.push(structuredClone(source));
          fragment.event.data.observations = structuredClone(fragment.observations);
        }
        return;
      }
      if (text.startsWith(observed.text)) {
        const event = emit(source, type, { ...fields, content: text.slice(observed.text.length), delta: true, observations: [structuredClone(source)] });
        observed.fragments.push({ event, observations: [structuredClone(source)] });
        observed.text = text;
        return;
      }
      retireMessage(observed);
    }
    const message = observed ?? { text: "", fragments: [], scope, position };
    message.text = source.delta === true ? message.text + text : text;
    if (key !== undefined) messages.set(key, message);
    const { content: ignored, ...metadata } = source;
    const signature = JSON.stringify(metadata);
    // Coalesce only indistinguishable adjacent envelopes; differing timestamps,
    // IDs, channels, or native additions retain their own ordered fragments.
    if (source.delta === true && lastDelta?.signature === signature && lastDelta.event.type === type && typeof lastDelta.event.data.content === "string") {
      lastDelta.event.data.content += text;
      lastDelta.observations.push(structuredClone(source));
      lastDelta.event.data.observations = structuredClone(lastDelta.observations);
      previousDelta = lastDelta;
    } else {
      const event = emit(source, type, { ...fields, content: text });
      const fragment = { event, observations: event.data.observations ?? [structuredClone(source)] };
      if (key !== undefined) message.fragments.push(fragment);
      if (source.delta === true) previousDelta = { signature, ...fragment };
    }
  };
  const emitBlock = (source, block, position) => {
    if (!block || typeof block !== "object" || Array.isArray(block)) return;
    if (block.type === "text" && typeof block.text === "string") {
      emitMessage(source, messageRole(source), block.text, { ...block, contentIndex: position }, position);
    } else if (block.type === "thinking" && typeof block.thinking === "string") {
      emitMessage(source, "reasoning", block.thinking, { ...block, contentIndex: position }, position);
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
    retiredObservations = [];
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    if (isSessionEvent(record)) {
      const refusal = record.type === "assistant.message" ? getMessageRefusal(record.data) : undefined;
      const event = refusal ? createSessionEvent(record, "assistant.refusal", refusal) : structuredClone(record);
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
      const role = messageRole(record);
      if (!["assistant", "user", "reasoning"].includes(role)) continue;
      if (role !== "user" && (record.error != null || record.is_error === true || record.isError === true || ["error", "failed"].includes(record.status))) {
        emit(record, "session.error", { sourceType: record.type });
        reportError(record);
        continue;
      }
      const content = record.message === undefined ? record.content : typeof record.message === "string" ? record.message : record.message?.content;
      const refusal = role === "assistant" ? getMessageRefusal(record.message && typeof record.message === "object" ? record.message : record) : undefined;
      if (refusal) {
        const fields = { ...(content === null ? { content } : {}), ...refusal, ...(record.delta === true && !Object.hasOwn(record, "partial") ? { partial: true } : {}) };
        const emitRefusal = position => {
          if (typeof refusal.content === "string") emitMessage(record, "refusal", refusal.content, fields, position, lastDelta);
          else emit(record, "assistant.refusal", fields);
        };
        if (Array.isArray(content)) {
          let emitted = false;
          for (const [position, block] of content.entries()) {
            if (block?.type === "text" || block?.type === "refusal") {
              if (!emitted) emitRefusal(position);
              emitted = true;
            } else emitBlock(record, block, position);
          }
          if (!emitted) emitRefusal(0);
        } else emitRefusal(0);
        continue;
      }
      if (Array.isArray(content)) {
        const scope = messageScope(record);
        const emptyKey = scope === undefined ? undefined : messageKey(scope, role, -1);
        const empty = emptyKey === undefined ? undefined : emptyMessages.get(emptyKey);
        if (empty && !content.length) {
          empty.data.observations = [...(empty.data.observations ?? []), structuredClone(record)];
          continue;
        }
        if (empty) {
          retiredObservations.push(...(empty.data.observations ?? []));
          events.splice(events.indexOf(empty), 1);
          emptyMessages.delete(emptyKey);
        }
        reconcileContentBlocks(record, content);
        if (!content.length) {
          const event = emit(record, role === "user" ? "user.message" : role === "reasoning" ? "assistant.reasoning" : "assistant.message", { content, observations: [structuredClone(record)] });
          if (emptyKey !== undefined) emptyMessages.set(emptyKey, event);
        }
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
        sourceType: supplied(record, ["sourceType"], record.type),
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
      emit(record, "session.error", { sourceType: record.type });
      if (!["warning", "info"].includes(record.severity)) reportError(record);
    } else if (record.type === "system" && record.subtype) {
      if (record.error != null) {
        emit(record, "session.error", { sourceType: record.type });
        reportError(record);
      } else {
        const content = typeof record.message === "string" ? record.message : record.message?.content;
        emit(record, "session.info", { sourceType: record.type, ...(content !== undefined ? { content } : {}) });
      }
    }
  }
  return events;
}

module.exports = { normalizeGeminiSession };
